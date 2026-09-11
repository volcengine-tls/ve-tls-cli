package sso

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

// OAuthClientConfig holds configuration for the CloudIdentity OAuth client.
type OAuthClientConfig struct {
	// Region is the CloudIdentity region used to build the default base URL.
	// If empty, DefaultRegion is used. Ignored when BaseURL is set.
	Region string
	// BaseURL is the base URL of the CloudIdentity OAuth service. If empty,
	// the default URL for Region is used. Must be a clean HTTPS URL with no
	// userinfo, query, fragment, or non-root path.
	BaseURL string
	// HTTPClient is the underlying HTTP client used for requests. It is only
	// used when RetryClient is nil; if RetryClient is set, its HTTPClient
	// (including any nil value, which then falls back to http.DefaultClient)
	// takes precedence and this field is ignored. If both are nil, a new
	// client with OAuthRequestTimeout is created.
	HTTPClient *http.Client
	// RetryClient is the retry client used for requests. If nil, a new
	// RetryClient with OAuthRetryAttempts and the configured HTTPClient is
	// used. When provided, it is used as-is (including its own HTTPClient);
	// the separate HTTPClient field does not override it. Injecting a
	// RetryClient allows tests to control the clock, sleeper, and attempt
	// count without touching real time or the network.
	RetryClient *httpx.RetryClient
}

// OAuthClient wraps HTTP calls to the CloudIdentity OAuth endpoints. It
// implements client registration, device authorization, token exchange, and
// token revocation.
//
// All dependencies (HTTP client, retry client) are injectable so that tests can
// run without real network access or real delays. The zero value is not usable;
// construct one with NewOAuthClient.
type OAuthClient struct {
	baseURL     string
	registerURL string
	tokenURL    string
	revokeURL   string
	deviceURL   string
	retry       *httpx.RetryClient
}

// NewOAuthClient creates a new OAuthClient with the given configuration. If cfg
// is nil, defaults are used. The base URL is validated to be a clean HTTPS URL.
func NewOAuthClient(cfg *OAuthClientConfig) (*OAuthClient, error) {
	region := DefaultRegion
	var baseURL string
	var httpClient *http.Client
	var retryClient *httpx.RetryClient
	if cfg != nil {
		if strings.TrimSpace(cfg.Region) != "" {
			region = strings.TrimSpace(cfg.Region)
		}
		baseURL = strings.TrimSpace(cfg.BaseURL)
		httpClient = cfg.HTTPClient
		retryClient = cfg.RetryClient
	}
	if baseURL == "" {
		baseURL = fmt.Sprintf(oauthBaseURLTemplate, region)
	}

	clean, err := normalizeBaseURL(baseURL)
	if err != nil {
		return nil, err
	}

	if httpClient == nil && retryClient == nil {
		httpClient = &http.Client{Timeout: OAuthRequestTimeout}
	}
	if retryClient == nil {
		retryClient = &httpx.RetryClient{
			HTTPClient:  httpClient,
			MaxAttempts: OAuthRetryAttempts,
		}
	}

	return &OAuthClient{
		baseURL:     clean,
		registerURL: clean + RegisterPath,
		tokenURL:    clean + TokenPath,
		revokeURL:   clean + RevokePath,
		deviceURL:   clean + DeviceAuthorizationPath,
		retry:       retryClient,
	}, nil
}

// BaseURL returns the normalized base URL.
func (c *OAuthClient) BaseURL() string {
	if c == nil {
		return ""
	}
	return c.baseURL
}

// normalizeBaseURL validates and normalizes the base URL. It must:
//   - use the https scheme
//   - have a non-empty host
//   - have no userinfo
//   - have no query or fragment
//   - have an empty path or a single "/" (which is normalized to empty)
//   - not be an opaque URL
func normalizeBaseURL(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", errors.New("base URL is empty")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid base URL: %w", err)
	}
	if u.Scheme != "https" {
		return "", errors.New("base URL must use https scheme")
	}
	if u.Host == "" {
		return "", errors.New("base URL must have a non-empty host")
	}
	if u.User != nil {
		return "", errors.New("base URL must not contain userinfo")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("base URL must not contain query or fragment")
	}
	if u.Opaque != "" {
		return "", errors.New("base URL must not be opaque")
	}
	switch u.Path {
	case "", "/":
		u.Path = ""
	default:
		return "", errors.New("base URL must have an empty or root path")
	}
	return u.Scheme + "://" + u.Host, nil
}

// validateVerificationURI checks that uri is an HTTPS absolute URL suitable for
// a device authorization verification URI. It must:
//   - use the https scheme
//   - have a non-empty hostname (u.Hostname(), not just u.Host, so a bare port
//     like ":443" is rejected)
//   - have no userinfo (which could leak a user code or secret)
//
// Normal path, query, and fragment components are permitted: the upstream
// service may use a fragment for SPA routing or to embed the user code in
// verification_uri_complete. The URI is never echoed in the returned error
// because it may carry the user code.
func validateVerificationURI(uri string) error {
	u, err := url.Parse(uri)
	if err != nil {
		return errors.New("device authorization response has invalid verification_uri")
	}
	if u.Scheme != "https" {
		return errors.New("device authorization response has invalid verification_uri")
	}
	if u.Hostname() == "" {
		return errors.New("device authorization response has invalid verification_uri")
	}
	if u.User != nil {
		return errors.New("device authorization response has invalid verification_uri")
	}
	return nil
}

// RegisterClient registers a new OAuth client with the CloudIdentity service.
// Client registration is not retried because it is not guaranteed to be
// idempotent.
func (c *OAuthClient) RegisterClient(ctx context.Context, req *RegisterClientRequest) (*RegisterClientResponse, error) {
	if c == nil {
		return nil, errors.New("nil *OAuthClient")
	}
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	if req == nil {
		return nil, errors.New("request cannot be nil")
	}
	if strings.TrimSpace(req.ClientName) == "" {
		return nil, errors.New("client_name is required")
	}
	if strings.TrimSpace(req.ClientType) == "" {
		return nil, errors.New("client_type is required")
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// Registration is not idempotent: use a single attempt. Before forcing a
	// single attempt, reject a caller-supplied RetryClient with a negative
	// MaxAttempts as a configuration error rather than silently coercing it
	// to a legal value. Other illegal RetryClient fields are surfaced by the
	// shared retry validate() on the first Do call.
	if c.retry.MaxAttempts < 0 {
		return nil, errors.New("invalid retry client configuration: MaxAttempts must not be negative")
	}
	singleAttempt := *c.retry
	singleAttempt.MaxAttempts = 1

	resp, err := singleAttempt.Do(ctx, func(ctx context.Context) (*http.Request, error) {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.registerURL, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		return httpReq, nil
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	requestID := resp.Header.Get(RequestIDHeader)

	if resp.StatusCode/100 != 2 {
		return nil, parseOAuthErrorResponse(resp.StatusCode, requestID, resp.Body)
	}

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read register response: %w", err)
	}

	var apiResp RegisterClientResponse
	if len(respBytes) > 0 {
		if err := json.Unmarshal(respBytes, &apiResp); err != nil {
			return nil, fmt.Errorf("decode register response (status %d): %w", resp.StatusCode, err)
		}
	}

	// Validate the minimum required fields of a successful response.
	if strings.TrimSpace(apiResp.ClientID) == "" {
		return nil, errors.New("register response missing client_id")
	}
	if strings.TrimSpace(apiResp.ClientSecret) == "" {
		return nil, errors.New("register response missing client_secret")
	}

	return &apiResp, nil
}

// StartDeviceAuthorization initiates the device authorization flow.
func (c *OAuthClient) StartDeviceAuthorization(ctx context.Context, req *StartDeviceAuthorizationRequest) (*StartDeviceAuthorizationResponse, error) {
	if c == nil {
		return nil, errors.New("nil *OAuthClient")
	}
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	if req == nil {
		return nil, errors.New("request cannot be nil")
	}
	if strings.TrimSpace(req.ClientID) == "" {
		return nil, errors.New("client_id is required")
	}
	if strings.TrimSpace(req.ClientSecret) == "" {
		return nil, errors.New("client_secret is required")
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	resp, err := c.retry.Do(ctx, func(ctx context.Context) (*http.Request, error) {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.deviceURL, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		return httpReq, nil
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	requestID := resp.Header.Get(RequestIDHeader)

	if resp.StatusCode/100 != 2 {
		return nil, parseOAuthErrorResponse(resp.StatusCode, requestID, resp.Body)
	}

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read device authorization response: %w", err)
	}

	var apiResp StartDeviceAuthorizationResponse
	if len(respBytes) > 0 {
		if err := json.Unmarshal(respBytes, &apiResp); err != nil {
			return nil, fmt.Errorf("decode device authorization response (status %d): %w", resp.StatusCode, err)
		}
	}

	// Validate the minimum required fields of a successful response.
	if strings.TrimSpace(apiResp.DeviceCode) == "" {
		return nil, errors.New("device authorization response missing device_code")
	}
	if strings.TrimSpace(apiResp.UserCode) == "" {
		return nil, errors.New("device authorization response missing user_code")
	}
	if strings.TrimSpace(apiResp.VerificationURI) == "" {
		return nil, errors.New("device authorization response missing verification_uri")
	}
	if apiResp.ExpiresIn <= 0 {
		return nil, errors.New("device authorization response missing or invalid expires_in")
	}
	// Interval must not be negative. A zero interval is permitted and left to
	// Task 11 to normalize to the OAuth default polling interval.
	if apiResp.Interval < 0 {
		return nil, errors.New("device authorization response has invalid interval")
	}
	// VerificationURI must be an HTTPS absolute URL with a non-empty hostname
	// and no userinfo (which could leak a user code). Normal path, query, and
	// fragment components are permitted. The URI is never echoed in the error
	// because it may carry the user code.
	if err := validateVerificationURI(apiResp.VerificationURI); err != nil {
		return nil, err
	}
	// VerificationURIComplete is optional. When present it must satisfy the
	// same URL rules as VerificationURI.
	if strings.TrimSpace(apiResp.VerificationURIComplete) != "" {
		if err := validateVerificationURI(apiResp.VerificationURIComplete); err != nil {
			return nil, err
		}
	}

	return &apiResp, nil
}

// CreateToken exchanges a device code or refresh token for an access token.
func (c *OAuthClient) CreateToken(ctx context.Context, req *CreateTokenRequest) (*CreateTokenResponse, error) {
	if c == nil {
		return nil, errors.New("nil *OAuthClient")
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
	if strings.TrimSpace(req.ClientSecret) == "" {
		return nil, errors.New("client_secret is required")
	}

	switch req.GrantType {
	case GrantTypeDeviceCode:
		if strings.TrimSpace(req.DeviceCode) == "" {
			return nil, errors.New("device_code is required for device_code grant")
		}
	case GrantTypeRefreshToken:
		if strings.TrimSpace(req.RefreshToken) == "" {
			return nil, errors.New("refresh_token is required for refresh_token grant")
		}
	default:
		return nil, fmt.Errorf("unsupported grant_type: %s", req.GrantType)
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	resp, err := c.retry.Do(ctx, func(ctx context.Context) (*http.Request, error) {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenURL, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		return httpReq, nil
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	requestID := resp.Header.Get(RequestIDHeader)

	if resp.StatusCode/100 != 2 {
		return nil, parseOAuthErrorResponse(resp.StatusCode, requestID, resp.Body)
	}

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read token response: %w", err)
	}

	var apiResp CreateTokenResponse
	if len(respBytes) > 0 {
		if err := json.Unmarshal(respBytes, &apiResp); err != nil {
			return nil, fmt.Errorf("decode token response (status %d): %w", resp.StatusCode, err)
		}
	}

	// Validate the minimum required fields of a successful response.
	if strings.TrimSpace(apiResp.AccessToken) == "" {
		return nil, errors.New("token response missing access_token")
	}
	if strings.TrimSpace(apiResp.TokenType) == "" {
		return nil, errors.New("token response missing token_type")
	}
	if apiResp.ExpiresIn <= 0 {
		return nil, errors.New("token response missing or invalid expires_in")
	}

	return &apiResp, nil
}

// RevokeToken revokes an access or refresh token.
func (c *OAuthClient) RevokeToken(ctx context.Context, req *RevokeTokenRequest) error {
	if c == nil {
		return errors.New("nil *OAuthClient")
	}
	if ctx == nil {
		return errors.New("nil context")
	}
	if req == nil {
		return errors.New("request cannot be nil")
	}
	if strings.TrimSpace(req.ClientID) == "" {
		return errors.New("client_id is required")
	}
	if strings.TrimSpace(req.ClientSecret) == "" {
		return errors.New("client_secret is required")
	}
	if strings.TrimSpace(req.Token) == "" {
		return errors.New("token is required")
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	resp, err := c.retry.Do(ctx, func(ctx context.Context) (*http.Request, error) {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.revokeURL, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		return httpReq, nil
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	requestID := resp.Header.Get(RequestIDHeader)

	if resp.StatusCode/100 != 2 {
		return parseOAuthErrorResponse(resp.StatusCode, requestID, resp.Body)
	}

	// Drain and discard the response body; revoke returns no useful payload.
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// parseOAuthErrorResponse reads up to a bounded amount of the response body and
// attempts to parse it as a structured OAuthErrorResponse. The body and the
// server-supplied error_description are never stored in the returned error.
// Only an allowlisted OAuth error code is copied to the returned error; any
// other code (or no code) results in an empty Code field.
func parseOAuthErrorResponse(statusCode int, requestID string, body io.Reader) *OAuthAPIError {
	apiErr := &OAuthAPIError{
		StatusCode: statusCode,
		RequestID:  sanitizeRequestID(requestID),
	}
	limited := io.LimitReader(body, 64*1024)
	respBytes, err := io.ReadAll(limited)
	if err != nil || len(respBytes) == 0 {
		return apiErr
	}
	var errResp OAuthErrorResponse
	if json.Unmarshal(respBytes, &errResp) == nil {
		if _, ok := allowedOAuthErrorCodes[errResp.Error]; ok {
			apiErr.Code = errResp.Error
		}
	}
	return apiErr
}
