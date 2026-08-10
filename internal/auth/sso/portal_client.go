package sso

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth/httpx"
)

// PortalClientConfig holds configuration for the CloudIdentity Portal client.
type PortalClientConfig struct {
	// Region is the CloudIdentity region used to build the default base URL.
	// If empty, DefaultRegion is used. Ignored when BaseURL is set.
	Region string
	// BaseURL is the base URL of the CloudIdentity Portal service. If empty,
	// the default URL for Region is used. Must be a clean HTTPS URL with no
	// userinfo, query, fragment, or non-root path.
	BaseURL string
	// HTTPClient is the underlying HTTP client used for requests. It is only
	// used when RetryClient is nil; if RetryClient is set, its HTTPClient
	// (including any nil value, which then falls back to http.DefaultClient)
	// takes precedence and this field is ignored. If both are nil, a new
	// client with PortalRequestTimeout is created.
	HTTPClient *http.Client
	// RetryClient is the retry client used for requests. If nil, a new
	// RetryClient with PortalRetryAttempts and the configured HTTPClient is
	// used. When provided, it is used as-is (including its own HTTPClient);
	// the separate HTTPClient field does not override it.
	RetryClient *httpx.RetryClient
	// DefaultPageSize is the default page size for paginated requests. If
	// zero or negative, DefaultPageSize is used.
	DefaultPageSize int
}

// PortalClient wraps HTTP calls to the CloudIdentity Portal endpoints. It
// implements account listing, role listing, and role credential exchange.
//
// All dependencies (HTTP client, retry client) are injectable so that tests can
// run without real network access or real delays. The zero value is not usable;
// construct one with NewPortalClient.
type PortalClient struct {
	baseURL            string
	listAccountsURL    string
	listRolesURL       string
	roleCredentialsURL string
	retry              *httpx.RetryClient
	defaultPageSize    int
}

// NewPortalClient creates a new PortalClient with the given configuration. If
// cfg is nil, defaults are used. The base URL is validated to be a clean HTTPS
// URL.
func NewPortalClient(cfg *PortalClientConfig) (*PortalClient, error) {
	region := DefaultRegion
	var baseURL string
	var httpClient *http.Client
	var retryClient *httpx.RetryClient
	pageSize := DefaultPageSize
	if cfg != nil {
		if strings.TrimSpace(cfg.Region) != "" {
			region = strings.TrimSpace(cfg.Region)
		}
		baseURL = strings.TrimSpace(cfg.BaseURL)
		httpClient = cfg.HTTPClient
		retryClient = cfg.RetryClient
		if cfg.DefaultPageSize > 0 {
			pageSize = cfg.DefaultPageSize
		}
	}
	if baseURL == "" {
		baseURL = fmt.Sprintf(portalBaseURLTemplate, region)
	}

	clean, err := normalizeBaseURL(baseURL)
	if err != nil {
		return nil, err
	}

	if httpClient == nil && retryClient == nil {
		httpClient = &http.Client{Timeout: PortalRequestTimeout}
	}
	if retryClient == nil {
		retryClient = &httpx.RetryClient{
			HTTPClient:  httpClient,
			MaxAttempts: PortalRetryAttempts,
		}
	}

	return &PortalClient{
		baseURL:            clean,
		listAccountsURL:    clean + ListAccountsPath,
		listRolesURL:       clean + ListRolesPath,
		roleCredentialsURL: clean + RoleCredentialsPath,
		retry:              retryClient,
		defaultPageSize:    pageSize,
	}, nil
}

// BaseURL returns the normalized base URL.
func (c *PortalClient) BaseURL() string {
	if c == nil {
		return ""
	}
	return c.baseURL
}

// ListAccounts lists all accounts accessible to the given access token. It
// internally paginates through all available pages and returns the aggregated
// result. Pagination is bounded by maxPaginationPages to defend against a
// server that never terminates pagination.
func (c *PortalClient) ListAccounts(ctx context.Context, accessToken string) ([]AccountInfo, error) {
	if c == nil {
		return nil, errors.New("nil *PortalClient")
	}
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	token := strings.TrimSpace(accessToken)
	if token == "" {
		return nil, errors.New("access token is required")
	}

	var allAccounts []AccountInfo
	pageNumber := 1
	seenTokens := make(map[string]struct{})

	for page := 0; page < maxPaginationPages; page++ {
		q := url.Values{}
		q.Set("page_size", strconv.Itoa(c.defaultPageSize))
		q.Set("page_number", strconv.Itoa(pageNumber))
		fullURL := c.listAccountsURL + "?" + q.Encode()

		env, err := c.doPortalGet(ctx, token, fullURL)
		if err != nil {
			return nil, err
		}

		var result listAccountsResult
		if len(env.Result) == 0 {
			return nil, errors.New("ListAccounts succeeded but response was empty")
		}
		if err := json.Unmarshal(env.Result, &result); err != nil {
			return nil, fmt.Errorf("decode ListAccounts result: %w", err)
		}

		if err := validatePageMetadata(result.Total, result.PageNumber, result.PageSize, pageNumber); err != nil {
			return nil, err
		}

		// The upstream Portal does not provide a snapshot/consistency token: the
		// account/role set may change while paginating, so Total and PageSize can
		// vary across pages. We aggregate every page's items and compute the next
		// page from the current page's metadata, terminating only when the server
		// reports no further page. We never require the aggregated count to match
		// any single page's Total.
		allAccounts = append(allAccounts, result.AccountList...)

		nextToken := computeNextToken(result.Total, result.PageNumber, result.PageSize)
		if nextToken == "" {
			return allAccounts, nil
		}
		// Defend against a server that returns the same next token forever
		// (no progress).
		if _, dup := seenTokens[nextToken]; dup {
			return nil, errors.New("ListAccounts pagination stalled on repeated next token")
		}
		seenTokens[nextToken] = struct{}{}

		next, err := strconv.Atoi(nextToken)
		if err != nil || next < 1 {
			return nil, errors.New("ListAccounts returned invalid next token")
		}
		pageNumber = next
	}

	return nil, errors.New("ListAccounts pagination limit exceeded")
}

// ListAccountRoles lists all roles for the given account that are accessible to
// the given access token. It internally paginates through all available pages
// and returns the aggregated result. The account ID is URL-encoded in the
// request query.
func (c *PortalClient) ListAccountRoles(ctx context.Context, accessToken, accountID string) ([]RoleInfo, error) {
	if c == nil {
		return nil, errors.New("nil *PortalClient")
	}
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	token := strings.TrimSpace(accessToken)
	if token == "" {
		return nil, errors.New("access token is required")
	}
	if strings.TrimSpace(accountID) == "" {
		return nil, errors.New("account_id is required")
	}

	var allRoles []RoleInfo
	pageNumber := 1
	seenTokens := make(map[string]struct{})

	for page := 0; page < maxPaginationPages; page++ {
		q := url.Values{}
		q.Set("account_id", accountID)
		q.Set("page_size", strconv.Itoa(c.defaultPageSize))
		q.Set("page_number", strconv.Itoa(pageNumber))
		fullURL := c.listRolesURL + "?" + q.Encode()

		env, err := c.doPortalGet(ctx, token, fullURL)
		if err != nil {
			return nil, err
		}

		var result listAccountRolesResult
		if len(env.Result) == 0 {
			return nil, errors.New("ListAccountRoles succeeded but response was empty")
		}
		if err := json.Unmarshal(env.Result, &result); err != nil {
			return nil, fmt.Errorf("decode ListAccountRoles result: %w", err)
		}

		if err := validatePageMetadata(result.Total, result.PageNumber, result.PageSize, pageNumber); err != nil {
			return nil, err
		}

		// The upstream Portal does not provide a snapshot/consistency token: the
		// role set may change while paginating, so Total and PageSize can vary
		// across pages. We aggregate every page's items and compute the next page
		// from the current page's metadata, terminating only when the server
		// reports no further page. We never require the aggregated count to match
		// any single page's Total.
		allRoles = append(allRoles, result.RoleList...)

		nextToken := computeNextToken(result.Total, result.PageNumber, result.PageSize)
		if nextToken == "" {
			return allRoles, nil
		}
		if _, dup := seenTokens[nextToken]; dup {
			return nil, errors.New("ListAccountRoles pagination stalled on repeated next token")
		}
		seenTokens[nextToken] = struct{}{}

		next, err := strconv.Atoi(nextToken)
		if err != nil || next < 1 {
			return nil, errors.New("ListAccountRoles returned invalid next token")
		}
		pageNumber = next
	}

	return nil, errors.New("ListAccountRoles pagination limit exceeded")
}

// GetRoleCredentials exchanges the access token for temporary STS credentials
// for the given account and role.
func (c *PortalClient) GetRoleCredentials(ctx context.Context, accessToken, accountID, roleName string) (*RoleCredentials, error) {
	if c == nil {
		return nil, errors.New("nil *PortalClient")
	}
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	token := strings.TrimSpace(accessToken)
	if token == "" {
		return nil, errors.New("access token is required")
	}
	if strings.TrimSpace(accountID) == "" {
		return nil, errors.New("account_id is required")
	}
	if strings.TrimSpace(roleName) == "" {
		return nil, errors.New("role_name is required")
	}

	q := url.Values{}
	q.Set("account_id", accountID)
	q.Set("role_name", roleName)
	fullURL := c.roleCredentialsURL + "?" + q.Encode()

	env, err := c.doPortalGet(ctx, token, fullURL)
	if err != nil {
		return nil, err
	}

	var result getRoleCredentialsResult
	if len(env.Result) == 0 {
		return nil, errors.New("GetRoleCredentials succeeded but response was empty")
	}
	if err := json.Unmarshal(env.Result, &result); err != nil {
		return nil, fmt.Errorf("decode GetRoleCredentials result: %w", err)
	}

	creds := result.RoleCredentials
	// Validate the minimum required fields of a successful response.
	if strings.TrimSpace(creds.AccessKeyID) == "" {
		return nil, errors.New("role credentials missing AccessKeyId")
	}
	if strings.TrimSpace(creds.SecretAccessKey) == "" {
		return nil, errors.New("role credentials missing SecretAccessKey")
	}
	if strings.TrimSpace(creds.SessionToken) == "" {
		return nil, errors.New("role credentials missing sessionToken")
	}
	if creds.Expiration <= 0 {
		return nil, errors.New("role credentials missing or invalid Expiration")
	}

	return &creds, nil
}

// doPortalGet performs a single GET request to the Portal endpoint with the
// bearer token header, decodes the response envelope, and checks for errors in
// ResponseMetadata.
func (c *PortalClient) doPortalGet(ctx context.Context, token, fullURL string) (*portalEnvelope, error) {
	resp, err := c.retry.Do(ctx, func(ctx context.Context) (*http.Request, error) {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		httpReq.Header.Set("Accept", "application/json")
		httpReq.Header.Set(BearerTokenHeader, token)
		return httpReq, nil
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	requestID := resp.Header.Get(RequestIDHeader)
	safeHeaderReqID := sanitizeRequestID(requestID)

	// Read the body before deciding how to handle the status. A non-2xx
	// response whose body cannot be read (e.g. it exceeds the size limit and
	// the wrapped body returns ErrBodyTooLarge) must still surface a
	// classifiable *PortalAPIError carrying only the HTTP status and the
	// safety-validated header request ID. The body, the read error, and any
	// server-supplied text are permanently discarded.
	respBytes, readErr := io.ReadAll(resp.Body)

	if resp.StatusCode/100 != 2 {
		if readErr != nil {
			return nil, &PortalAPIError{
				StatusCode: resp.StatusCode,
				RequestID:  safeHeaderReqID,
			}
		}
		return nil, parsePortalError(resp.StatusCode, requestID, respBytes)
	}

	if readErr != nil {
		// 2xx response but the body could not be read. Return a fixed, safe
		// protocol/read error; never the raw body or server text.
		return nil, fmt.Errorf("read portal response: %w", readErr)
	}

	var env portalEnvelope
	if len(respBytes) > 0 {
		if err := json.Unmarshal(respBytes, &env); err != nil {
			return nil, fmt.Errorf("decode portal response (status %d): %w", resp.StatusCode, err)
		}
	}

	// A 2xx response may still carry an error in ResponseMetadata.
	if env.ResponseMetadata.Error != nil {
		return nil, portalErrorFromMetadata(resp.StatusCode, env.ResponseMetadata, safeHeaderReqID)
	}

	return &env, nil
}

// parsePortalError parses a non-2xx response body and returns a *PortalAPIError
// that never stores the raw body or the server-supplied error Code/Message. For
// the request ID, it prefers a safe value from the response body's
// ResponseMetadata; when that is empty or unsafe it falls back to the
// safety-validated header request ID.
func parsePortalError(statusCode int, requestID string, body []byte) error {
	var parsed portalErrorResponse
	if len(body) > 0 {
		_ = json.Unmarshal(body, &parsed)
	}
	if parsed.ResponseMetadata.Error != nil {
		rid := sanitizeRequestID(parsed.ResponseMetadata.RequestID)
		if rid == "" {
			rid = sanitizeRequestID(requestID)
		}
		return &PortalAPIError{
			StatusCode: statusCode,
			RequestID:  rid,
		}
	}
	return &PortalAPIError{
		StatusCode: statusCode,
		RequestID:  sanitizeRequestID(requestID),
	}
}

// portalErrorFromMetadata converts a ResponseMetadata.Error into a
// *PortalAPIError. Returns nil if there is no error. The server-supplied Code
// and Message are permanently discarded; only a safety-validated RequestID is
// retained. When the body RequestID is empty or unsafe, it falls back to the
// safety-validated header request ID; if that is also unsafe, the RequestID is
// discarded (empty).
func portalErrorFromMetadata(statusCode int, meta ResponseMetadata, headerRequestID string) error {
	if meta.Error == nil {
		return nil
	}
	rid := sanitizeRequestID(meta.RequestID)
	if rid == "" {
		rid = headerRequestID
	}
	return &PortalAPIError{
		StatusCode: statusCode,
		RequestID:  rid,
	}
}

// validatePageMetadata checks the consistency of pagination fields returned by
// the Portal list endpoints. It rejects negative totals, non-positive page
// numbers/sizes, and mismatches between the requested page and the
// server-reported page number. The error messages are fixed and never echo
// request or response sensitive values.
func validatePageMetadata(total, pageNumber, pageSize, requestedPage int) error {
	if total < 0 {
		return errors.New("invalid pagination metadata: total must not be negative")
	}
	if pageNumber <= 0 {
		return errors.New("invalid pagination metadata: page_number must be positive")
	}
	if pageSize <= 0 {
		return errors.New("invalid pagination metadata: page_size must be positive")
	}
	if pageNumber != requestedPage {
		return errors.New("invalid pagination metadata: page_number does not match requested page")
	}
	return nil
}

// computeNextToken computes the next page token (as a page number string) based
// on the total count, current page number, and page size. Returns an empty
// string when there are no more pages. The caller must validate the metadata
// with validatePageMetadata before calling this function so that missing or
// zero pagination fields are never silently interpreted as "complete" when
// Total indicates there is still data.
//
// The comparison is overflow-safe: it never computes pageNumber*pageSize, which
// can overflow int when both fields are large (e.g. math.MaxInt). Instead it
// checks pageNumber <= (total-1)/pageSize, which is equivalent to
// pageNumber*pageSize < total for positive values but cannot overflow.
func computeNextToken(total, pageNumber, pageSize int) string {
	if pageSize <= 0 || pageNumber <= 0 || total <= 0 {
		return ""
	}
	if pageNumber <= (total-1)/pageSize {
		return strconv.Itoa(pageNumber + 1)
	}
	return ""
}

// listAccountsResult is the internal decode structure for ListAccounts.
type listAccountsResult struct {
	Total       int           `json:"Total"`
	PageNumber  int           `json:"PageNumber"`
	PageSize    int           `json:"PageSize"`
	AccountList []AccountInfo `json:"AccountList"`
}

// listAccountRolesResult is the internal decode structure for ListAccountRoles.
type listAccountRolesResult struct {
	Total      int        `json:"Total"`
	PageNumber int        `json:"PageNumber"`
	PageSize   int        `json:"PageSize"`
	RoleList   []RoleInfo `json:"RoleList"`
}

// getRoleCredentialsResult is the internal decode structure for
// GetRoleCredentials.
type getRoleCredentialsResult struct {
	RoleCredentials RoleCredentials `json:"RoleCredentials"`
}

// portalErrorResponse is the outermost structure of a Portal error response.
type portalErrorResponse struct {
	ResponseMetadata ResponseMetadata `json:"ResponseMetadata"`
}

// portalEnvelope is the unified response structure containing ResponseMetadata
// and Result.
type portalEnvelope struct {
	ResponseMetadata ResponseMetadata `json:"ResponseMetadata"`
	Result           json.RawMessage  `json:"Result"`
}
