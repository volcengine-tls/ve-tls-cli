// Package ecsrole implements a cached ECS IMDSv2 credential provider that
// fetches temporary credentials from the ECS metadata service. Credentials are
// kept in process memory only; no disk state, environment variables (except the
// opt-out VOLCENGINE_ECS_METADATA_DISABLED), or background goroutines are used.
package ecsrole

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth"
)

// imdsBaseURL is the fixed production ECS metadata endpoint. It cannot be
// overridden by config, flags, or environment.
const imdsBaseURL = "http://100.96.0.96"

// tokenTTLSeconds is the TTL requested for each IMDSv2 session token.
const tokenTTLSeconds = "21600"

// maxAttempts is the total number of HTTP attempts per subrequest (initial + 3
// retries), matching the upstream retry budget.
const maxAttempts = 4

// perRequestTimeout caps each individual HTTP attempt.
const perRequestTimeout = 2 * time.Second

// defaultRetrySleep is the production delay between retry attempts.
const defaultRetrySleep = 1 * time.Second

// maxBodySize is the maximum accepted response body size. One extra byte is
// read so oversized bodies can be detected and rejected.
const maxBodySize = 64 * 1024

// disabledEnv is the only environment variable the client consults. When set to
// "true" (case-insensitive) the client fails closed before any network call.
const disabledEnv = "VOLCENGINE_ECS_METADATA_DISABLED"

// errRedirectSentinel is returned by CheckRedirect so doRequest can classify a
// redirect as a non-retryable protocol error.
var errRedirectSentinel = errors.New("redirects disabled")

// Credentials are the temporary credentials returned by the ECS metadata
// service. ExpiresAt is the hard expiration parsed from ExpiredTime.
type Credentials struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	ExpiresAt       time.Time
}

// CredentialClient is the subset of the ECS client the provider depends on. The
// role name is passed per call so the provider is the single source of truth
// for the configured role.
type CredentialClient interface {
	FetchCredentials(ctx context.Context, roleName string) (Credentials, error)
}

// Client fetches credentials from the ECS IMDSv2 metadata service. It does not
// store a role name; the caller supplies it per FetchCredentials call.
type Client struct {
	baseURL        string
	httpClient     *http.Client
	sleeper        func(ctx context.Context, d time.Duration) error
	attemptTimeout time.Duration
}

// requestError wraps an error with a retryable classification so the retry
// loops can decide whether to retry without re-parsing the error.
type requestError struct {
	err       error
	retryable bool
}

func (e *requestError) Error() string { return e.err.Error() }
func (e *requestError) Unwrap() error { return e.err }

// NewClient returns a production ECS metadata client pinned to
// 100.96.0.96 over HTTP. The endpoint cannot be overridden. The transport
// ignores proxy environment variables and the client rejects redirects.
func NewClient() *Client {
	return &Client{
		baseURL: imdsBaseURL,
		httpClient: &http.Client{
			Transport: &http.Transport{Proxy: nil},
			Timeout:   perRequestTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return errRedirectSentinel
			},
		},
		sleeper:        defaultSleeper,
		attemptTimeout: perRequestTimeout,
	}
}

// newClientForTest builds a Client with a custom base URL, HTTP client,
// sleeper, and per-attempt timeout. It is package-private so tests cannot
// expose the endpoint through production config.
func newClientForTest(baseURL string, httpClient *http.Client, sleeper func(ctx context.Context, d time.Duration) error, attemptTimeout time.Duration) *Client {
	if sleeper == nil {
		sleeper = defaultSleeper
	}
	if attemptTimeout <= 0 {
		attemptTimeout = perRequestTimeout
	}
	return &Client{
		baseURL:        baseURL,
		httpClient:     httpClient,
		sleeper:        sleeper,
		attemptTimeout: attemptTimeout,
	}
}

// FetchCredentials retrieves a fresh IMDSv2 token and then the role
// credentials. Both subrequests are retried on transient failures. The token is
// fetched fresh on every call.
func (c *Client) FetchCredentials(ctx context.Context, roleName string) (Credentials, error) {
	if c == nil {
		return Credentials{}, &auth.Error{Kind: auth.ConfigInvalid, Description: "nil ECS client"}
	}
	if isNilContext(ctx) {
		return Credentials{}, &auth.Error{Kind: auth.ConfigInvalid, Description: "nil context"}
	}
	if c.httpClient == nil || c.sleeper == nil {
		return Credentials{}, &auth.Error{Kind: auth.ConfigInvalid, Description: "ECS client is not fully initialized"}
	}
	if strings.TrimSpace(roleName) == "" {
		return Credentials{}, &auth.Error{Kind: auth.ConfigInvalid, Description: "ECS role name must be non-empty"}
	}
	if isDisabled() {
		return Credentials{}, &auth.Error{Kind: auth.ConfigInvalid, Description: "ECS metadata service is disabled by environment"}
	}

	token, err := c.fetchToken(ctx)
	if err != nil {
		return Credentials{}, err
	}
	return c.fetchCredentials(ctx, token, roleName)
}

// fetchToken performs the PUT to obtain a session token.
func (c *Client) fetchToken(ctx context.Context) (string, error) {
	tokenURL := c.baseURL + "/latest/api/token"
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		body, status, rerr := c.doRequest(ctx, http.MethodPut, tokenURL, "", nil)
		if rerr != nil {
			lastErr = rerr.err
			// Caller cancellation/deadline is terminal: return immediately.
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			if !rerr.retryable || attempt == maxAttempts {
				break
			}
			if sleepErr := c.sleeper(ctx, defaultRetrySleep); sleepErr != nil {
				return "", sleepErr
			}
			continue
		}
		if status >= 200 && status < 300 {
			token := strings.TrimSpace(string(body))
			if token == "" {
				return "", &auth.Error{Kind: auth.ProtocolError, Status: status, Description: "ECS metadata returned empty token"}
			}
			return token, nil
		}
		lastErr = &auth.Error{Kind: auth.ProtocolError, Status: status, Description: "ECS metadata token request failed"}
		if !isRetryableStatus(status) || attempt == maxAttempts {
			break
		}
		if sleepErr := c.sleeper(ctx, defaultRetrySleep); sleepErr != nil {
			return "", sleepErr
		}
	}
	return "", wrapError(lastErr)
}

// fetchCredentials performs the GET to obtain role credentials.
func (c *Client) fetchCredentials(ctx context.Context, token, roleName string) (Credentials, error) {
	credURL := c.baseURL + "/volcstack/latest/iam/security_credentials/" + url.PathEscape(roleName)
	headers := map[string]string{"X-volc-ecs-metadata-token": token}
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return Credentials{}, err
		}
		body, status, rerr := c.doRequest(ctx, http.MethodGet, credURL, "", headers)
		if rerr != nil {
			lastErr = rerr.err
			if ctx.Err() != nil {
				return Credentials{}, ctx.Err()
			}
			if !rerr.retryable || attempt == maxAttempts {
				break
			}
			if sleepErr := c.sleeper(ctx, defaultRetrySleep); sleepErr != nil {
				return Credentials{}, sleepErr
			}
			continue
		}
		if status >= 200 && status < 300 {
			return parseCredentials(body)
		}
		lastErr = &auth.Error{Kind: auth.ProtocolError, Status: status, Description: "ECS metadata credential request failed"}
		if !isRetryableStatus(status) || attempt == maxAttempts {
			break
		}
		if sleepErr := c.sleeper(ctx, defaultRetrySleep); sleepErr != nil {
			return Credentials{}, sleepErr
		}
	}
	return Credentials{}, wrapError(lastErr)
}

// doRequest executes a single HTTP request with an independent per-attempt
// timeout derived from ctx. It returns a *requestError that classifies whether
// the failure is retryable. Transport/network errors (including per-attempt
// timeouts while the caller context is still alive) are retryable; request
// build, redirect, body read, and oversize errors are terminal.
func (c *Client) doRequest(ctx context.Context, method, rawURL, body string, headers map[string]string) ([]byte, int, *requestError) {
	reqCtx, cancel := context.WithTimeout(ctx, c.attemptTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, method, rawURL, strings.NewReader(body))
	if err != nil {
		return nil, 0, &requestError{err: &auth.Error{Kind: auth.ProtocolError, Description: "failed to build ECS metadata request", Cause: err}, retryable: false}
	}
	if method == http.MethodPut && strings.HasSuffix(rawURL, "/latest/api/token") {
		req.Header.Set("X-volc-ecs-metadata-token-ttl-seconds", tokenTTLSeconds)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Redirects surface as *url.Error wrapping errRedirectSentinel.
		if errors.Is(err, errRedirectSentinel) {
			return nil, 0, &requestError{err: &auth.Error{Kind: auth.ProtocolError, Description: "ECS metadata request rejected redirect", Cause: err}, retryable: false}
		}
		// Transport/network errors (incl. per-attempt deadline) are retryable
		// as long as the caller context is still alive.
		return nil, 0, &requestError{err: &auth.Error{Kind: auth.ProtocolError, Description: "ECS metadata request failed", Cause: err}, retryable: true}
	}
	defer resp.Body.Close()

	respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBodySize+1))
	if readErr != nil {
		// A body read can fail because the per-attempt deadline fired after
		// response headers arrived, or because the connection was interrupted.
		// Both are transient while the caller context is still alive; the
		// outer retry loop checks ctx.Err() before starting another attempt.
		return nil, resp.StatusCode, &requestError{err: &auth.Error{Kind: auth.ProtocolError, Status: resp.StatusCode, Description: "failed to read ECS metadata response", Cause: readErr}, retryable: true}
	}
	if len(respBody) > maxBodySize {
		return nil, resp.StatusCode, &requestError{err: &auth.Error{Kind: auth.ProtocolError, Status: resp.StatusCode, Description: "ECS metadata response exceeds maximum size"}, retryable: false}
	}
	return respBody, resp.StatusCode, nil
}

// parseCredentials decodes and validates the credential JSON.
func parseCredentials(body []byte) (Credentials, error) {
	var raw struct {
		AccessKeyId     string `json:"AccessKeyId"`
		SecretAccessKey string `json:"SecretAccessKey"`
		SessionToken    string `json:"SessionToken"`
		ExpiredTime     string `json:"ExpiredTime"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return Credentials{}, &auth.Error{Kind: auth.ProtocolError, Description: "failed to decode ECS credential response"}
	}
	if strings.TrimSpace(raw.AccessKeyId) == "" ||
		strings.TrimSpace(raw.SecretAccessKey) == "" ||
		strings.TrimSpace(raw.SessionToken) == "" ||
		strings.TrimSpace(raw.ExpiredTime) == "" {
		return Credentials{}, &auth.Error{Kind: auth.ProtocolError, Description: "ECS credential response missing required fields"}
	}
	expired, err := time.Parse(time.RFC3339, raw.ExpiredTime)
	if err != nil {
		return Credentials{}, &auth.Error{Kind: auth.ProtocolError, Description: "ECS credential response has invalid ExpiredTime"}
	}
	return Credentials{
		AccessKeyID:     raw.AccessKeyId,
		SecretAccessKey: raw.SecretAccessKey,
		SessionToken:    raw.SessionToken,
		ExpiresAt:       expired,
	}, nil
}

// isRetryableStatus reports whether an HTTP status should be retried.
func isRetryableStatus(status int) bool {
	switch status {
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// wrapError ensures the error is an *auth.Error, preserving structural fields
// from an existing *auth.Error. It is safe for typed-nil *auth.Error values.
func wrapError(err error) error {
	if err == nil {
		return &auth.Error{Kind: auth.ProtocolError, Description: "ECS metadata request failed"}
	}
	var ae *auth.Error
	if errors.As(err, &ae) && ae != nil {
		return ae
	}
	return &auth.Error{Kind: auth.ProtocolError, Description: "ECS metadata request failed", Cause: err}
}

// isDisabled reports whether the metadata-disabled env var is set to "true".
func isDisabled() bool {
	return strings.EqualFold(os.Getenv(disabledEnv), "true")
}

// defaultSleeper blocks for d or until ctx is done.
func defaultSleeper(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// isNilContext reports whether ctx is nil or a typed-nil context.Context.
func isNilContext(ctx context.Context) bool {
	if ctx == nil {
		return true
	}
	rv := reflect.ValueOf(ctx)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return rv.IsNil()
	}
	return false
}
