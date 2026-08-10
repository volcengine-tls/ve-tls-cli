package console

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth/httpx"
)

// ConsoleOAuthClient wraps HTTP calls to the Volcengine console sign-in OAuth
// endpoints. It implements the public-client PKCE authorization code flow and
// the refresh_token grant.
//
// All dependencies (HTTP client, retry client, clock, sleeper) are injectable
// so that tests can run without real network access or real delays. The zero
// value is not usable; construct one with NewConsoleOAuthClient.
type ConsoleOAuthClient struct {
	endpointURL  string
	authorizeURL string
	tokenURL     string
	retry        *httpx.RetryClient
}

// ConsoleOAuthClientConfig holds configuration for the Console OAuth client.
type ConsoleOAuthClientConfig struct {
	// EndpointURL is the base URL of the console sign-in service. If empty,
	// DefaultEndpoint is used. Must be a clean HTTPS URL with no userinfo,
	// query, fragment, or non-root path.
	EndpointURL string
	// HTTPClient is the underlying HTTP client used for token requests. If nil,
	// a new client with TokenTimeout is used.
	HTTPClient *http.Client
	// RetryClient is the retry client used for token requests. If nil, a new
	// RetryClient with RetryAttempts and the configured HTTPClient is used.
	// Injecting a RetryClient allows tests to control the clock, sleeper, and
	// attempt count without touching real time or the network.
	RetryClient *httpx.RetryClient
}

// NewConsoleOAuthClient creates a new ConsoleOAuthClient with the given
// configuration. If cfg is nil, defaults are used. The endpoint is validated
// to be a clean HTTPS URL.
func NewConsoleOAuthClient(cfg *ConsoleOAuthClientConfig) (*ConsoleOAuthClient, error) {
	endpoint := DefaultEndpoint
	var httpClient *http.Client
	var retryClient *httpx.RetryClient
	if cfg != nil {
		if strings.TrimSpace(cfg.EndpointURL) != "" {
			endpoint = strings.TrimSpace(cfg.EndpointURL)
		}
		httpClient = cfg.HTTPClient
		retryClient = cfg.RetryClient
	}

	clean, err := normalizeEndpoint(endpoint)
	if err != nil {
		return nil, err
	}

	if httpClient == nil {
		httpClient = &http.Client{Timeout: TokenTimeout}
	}
	if retryClient == nil {
		retryClient = &httpx.RetryClient{
			HTTPClient:  httpClient,
			MaxAttempts: RetryAttempts,
		}
	}

	return &ConsoleOAuthClient{
		endpointURL:  clean,
		authorizeURL: clean + AuthorizePath,
		tokenURL:     clean + TokenPath,
		retry:        retryClient,
	}, nil
}

// EndpointURL returns the normalized endpoint base URL.
func (c *ConsoleOAuthClient) EndpointURL() string {
	if c == nil {
		return ""
	}
	return c.endpointURL
}

// normalizeEndpoint validates and normalizes the endpoint URL. It must:
//   - use the https scheme
//   - have a non-empty host
//   - have no userinfo
//   - have no query or fragment
//   - have an empty path or a single "/" (which is normalized to empty)
//   - not be an opaque URL
func normalizeEndpoint(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", errors.New("endpoint URL is empty")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid endpoint URL: %w", err)
	}
	if u.Scheme != "https" {
		return "", errors.New("endpoint URL must use https scheme")
	}
	if u.Host == "" {
		return "", errors.New("endpoint URL must have a non-empty host")
	}
	if u.User != nil {
		return "", errors.New("endpoint URL must not contain userinfo")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("endpoint URL must not contain query or fragment")
	}
	if u.Opaque != "" {
		return "", errors.New("endpoint URL must not be opaque")
	}
	switch u.Path {
	case "", "/":
		u.Path = ""
	default:
		return "", errors.New("endpoint URL must have an empty or root path")
	}
	// Re-encode without path so the result is scheme://host.
	return u.Scheme + "://" + u.Host, nil
}

// BuildAuthorizeURL constructs the full authorization URL with query parameters
// for the authorization code flow with PKCE. The response_type is always
// "code". All required parameters must be present and non-empty.
//
// The ClientID must be one of the two frozen Console client IDs
// (ClientIDSameDevice or ClientIDCrossDevice). The Scope must equal the frozen
// Console scope (Scope) and the code_challenge_method must equal S256. Any
// other value is rejected to keep the protocol contract frozen.
func (c *ConsoleOAuthClient) BuildAuthorizeURL(params *AuthorizeParams) (string, error) {
	if c == nil {
		return "", errors.New("nil *ConsoleOAuthClient")
	}
	if params == nil {
		return "", errors.New("authorize params cannot be nil")
	}
	if params.ClientID != ClientIDSameDevice && params.ClientID != ClientIDCrossDevice {
		return "", errors.New("client_id must be a frozen Console client ID")
	}
	if strings.TrimSpace(params.RedirectURI) == "" {
		return "", errors.New("redirect_uri is required")
	}
	if params.Scope != Scope {
		return "", errors.New("scope must equal the frozen Console scope")
	}
	if strings.TrimSpace(params.State) == "" {
		return "", errors.New("state is required")
	}
	if strings.TrimSpace(params.CodeChallenge) == "" {
		return "", errors.New("code_challenge is required")
	}
	if params.CodeChallengeMethod != CodeChallengeMethodS256 {
		return "", errors.New("code_challenge_method must be S256")
	}

	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", params.ClientID)
	q.Set("redirect_uri", params.RedirectURI)
	q.Set("scope", params.Scope)
	q.Set("state", params.State)
	q.Set("code_challenge", params.CodeChallenge)
	q.Set("code_challenge_method", params.CodeChallengeMethod)

	return c.authorizeURL + "?" + q.Encode(), nil
}

// ExchangeToken performs the token exchange by sending a POST request to the
// token endpoint with an application/x-www-form-urlencoded body.
//
// For grant_type=authorization_code: code, client_id, scope, and code_verifier
// are required; redirect_uri is included when non-empty.
//
// For grant_type=refresh_token: refresh_token and client_id are required; the
// form never includes code, redirect_uri, or code_verifier.
//
// Retries are handled by the shared httpx.RetryClient: 429/408/5xx are retried
// up to RetryAttempts; 400 invalid_grant and other 4xx are returned on the
// first attempt. Non-2xx responses are converted to a *ConsoleOAuthAPIError
// that retains the status code, parsed OAuth error fields, and request ID but
// never the raw response body.
func (c *ConsoleOAuthClient) ExchangeToken(ctx context.Context, req *ConsoleTokenRequest) (*ConsoleTokenResponse, error) {
	if c == nil {
		return nil, errors.New("nil *ConsoleOAuthClient")
	}
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	if req == nil {
		return nil, errors.New("request cannot be nil")
	}
	if strings.TrimSpace(req.GrantType) == "" {
		return nil, errors.New("grant_type is required")
	}
	if strings.TrimSpace(req.ClientID) == "" {
		return nil, errors.New("client_id is required")
	}

	form, err := buildTokenForm(req)
	if err != nil {
		return nil, err
	}
	body := form.Encode()

	resp, err := c.retry.Do(ctx, func(ctx context.Context) (*http.Request, error) {
		// Build a fresh request (and fresh body reader) on every attempt so
		// retries never reuse a consumed body.
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenURL, strings.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return httpReq, nil
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	requestID := resp.Header.Get(RequestIDHeader)

	if resp.StatusCode/100 != 2 {
		return nil, parseErrorResponse(resp.StatusCode, requestID, resp.Body)
	}

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read token response: %w", err)
	}

	var tokenResp ConsoleTokenResponse
	if len(respBytes) > 0 {
		if err := json.Unmarshal(respBytes, &tokenResp); err != nil {
			return nil, fmt.Errorf("decode token response (status %d): %w", resp.StatusCode, err)
		}
	}

	// Validate the minimum required fields of a successful response.
	if len(bytes.TrimSpace(tokenResp.AccessToken)) == 0 {
		return nil, errors.New("token response missing access_token")
	}
	if strings.TrimSpace(tokenResp.TokenType) == "" {
		return nil, errors.New("token response missing token_type")
	}
	if tokenResp.ExpiresIn <= 0 {
		return nil, errors.New("token response missing or invalid expires_in")
	}

	// A successful response must carry a usable STS access_token. Validate it
	// through ParseSTSCredentials, which rejects null, booleans, numbers,
	// arrays, malformed JSON, and objects missing any of
	// access_key_id/secret_access_key/session_token. The original raw
	// json.RawMessage is preserved in the returned response. ParseSTSCredentials
	// never echoes the raw token or secret fields in its error.
	if _, err := ParseSTSCredentials(tokenResp.AccessToken); err != nil {
		return nil, fmt.Errorf("token response access_token is not valid STS credentials: %w", err)
	}

	return &tokenResp, nil
}

// buildTokenForm constructs the url.Values for a token request based on the
// grant type. It enforces the exact field set required by the protocol.
//
// For authorization_code: grant_type, client_id, scope, code, code_verifier,
// and redirect_uri are all required. The client_id must be a frozen Console
// client ID and the scope must equal the frozen Console scope.
//
// For refresh_token: grant_type, client_id, scope, and refresh_token are
// required. The form never includes code, redirect_uri, or code_verifier.
func buildTokenForm(req *ConsoleTokenRequest) (url.Values, error) {
	if req.ClientID != ClientIDSameDevice && req.ClientID != ClientIDCrossDevice {
		return nil, errors.New("client_id must be a frozen Console client ID")
	}
	if req.Scope != Scope {
		return nil, errors.New("scope must equal the frozen Console scope")
	}

	q := url.Values{}
	q.Set("grant_type", req.GrantType)
	q.Set("client_id", req.ClientID)
	q.Set("scope", req.Scope)

	switch req.GrantType {
	case GrantTypeAuthorizationCode:
		if strings.TrimSpace(req.Code) == "" {
			return nil, errors.New("code is required for authorization_code grant")
		}
		if strings.TrimSpace(req.CodeVerifier) == "" {
			return nil, errors.New("code_verifier is required for authorization_code grant")
		}
		if strings.TrimSpace(req.RedirectURI) == "" {
			return nil, errors.New("redirect_uri is required for authorization_code grant")
		}
		q.Set("code", req.Code)
		q.Set("code_verifier", req.CodeVerifier)
		q.Set("redirect_uri", req.RedirectURI)
	case GrantTypeRefreshToken:
		if strings.TrimSpace(req.RefreshToken) == "" {
			return nil, errors.New("refresh_token is required for refresh_token grant")
		}
		q.Set("refresh_token", req.RefreshToken)
	default:
		return nil, fmt.Errorf("unsupported grant_type: %s", req.GrantType)
	}
	return q, nil
}

// parseErrorResponse reads up to a bounded amount of the response body and
// attempts to parse it as a structured ConsoleOAuthErrorResponse. The body is
// never stored in the returned error.
func parseErrorResponse(statusCode int, requestID string, body io.Reader) *ConsoleOAuthAPIError {
	apiErr := &ConsoleOAuthAPIError{
		StatusCode: statusCode,
		RequestID:  requestID,
	}
	// Bound the read so a malicious server cannot exhaust memory.
	limited := io.LimitReader(body, 64*1024)
	respBytes, err := io.ReadAll(limited)
	if err != nil || len(respBytes) == 0 {
		return apiErr
	}
	var errResp ConsoleOAuthErrorResponse
	if json.Unmarshal(respBytes, &errResp) == nil && errResp.Error != "" {
		apiErr.Response = errResp
	}
	return apiErr
}
