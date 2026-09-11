package console

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth/httpx"
)

// roundTripFunc is a function type implementing http.RoundTripper for tests.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newResponse(status int, body string, headers map[string]string) *http.Response {
	resp := &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	for k, v := range headers {
		resp.Header.Set(k, v)
	}
	return resp
}

func noopSleeper(context.Context, time.Duration) error { return nil }

func fixedClock(t time.Time) httpx.Clock {
	return clockFunc(func() time.Time { return t })
}

type clockFunc func() time.Time

func (c clockFunc) Now() time.Time { return c() }

func TestBuildAuthorizeURLHasExactParameters(t *testing.T) {
	client, err := NewConsoleOAuthClient(nil)
	if err != nil {
		t.Fatalf("NewConsoleOAuthClient: %v", err)
	}

	params := &AuthorizeParams{
		ClientID:            ClientIDSameDevice,
		RedirectURI:         "http://127.0.0.1:12345/oauth/callback",
		Scope:               Scope,
		State:               "state-value",
		CodeChallenge:       "challenge-value",
		CodeChallengeMethod: CodeChallengeMethodS256,
	}

	authURL, err := client.BuildAuthorizeURL(params)
	if err != nil {
		t.Fatalf("BuildAuthorizeURL: %v", err)
	}

	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parse authorize URL: %v", err)
	}
	if u.Scheme != "https" || u.Host != "signin.volcengine.com" {
		t.Fatalf("authorize URL host = %s://%s, want https://signin.volcengine.com", u.Scheme, u.Host)
	}
	if u.Path != AuthorizePath {
		t.Fatalf("authorize URL path = %q, want %q", u.Path, AuthorizePath)
	}

	q := u.Query()
	want := map[string]string{
		"response_type":         "code",
		"client_id":             ClientIDSameDevice,
		"redirect_uri":          params.RedirectURI,
		"scope":                 Scope,
		"state":                 "state-value",
		"code_challenge":        "challenge-value",
		"code_challenge_method": "S256",
	}
	for k, v := range want {
		if got := q.Get(k); got != v {
			t.Fatalf("query %q = %q, want %q", k, got, v)
		}
	}
	// No extra parameters beyond the expected set.
	if len(q) != len(want) {
		t.Fatalf("query has %d params, want %d: %v", len(q), len(want), q)
	}

	// Missing required fields must error without echoing secrets.
	missingCases := []*AuthorizeParams{
		{ClientID: "", RedirectURI: "r", Scope: "s", State: "st", CodeChallenge: "c", CodeChallengeMethod: "m"},
		{ClientID: "c", RedirectURI: "", Scope: "s", State: "st", CodeChallenge: "c", CodeChallengeMethod: "m"},
		{ClientID: "c", RedirectURI: "r", Scope: "", State: "st", CodeChallenge: "c", CodeChallengeMethod: "m"},
		{ClientID: "c", RedirectURI: "r", Scope: "s", State: "", CodeChallenge: "c", CodeChallengeMethod: "m"},
		{ClientID: "c", RedirectURI: "r", Scope: "s", State: "st", CodeChallenge: "", CodeChallengeMethod: "m"},
		{ClientID: "c", RedirectURI: "r", Scope: "s", State: "st", CodeChallenge: "c", CodeChallengeMethod: ""},
	}
	for i, p := range missingCases {
		_, err := client.BuildAuthorizeURL(p)
		if err == nil {
			t.Fatalf("case %d: expected error for missing fields, got nil", i)
		}
		if strings.Contains(err.Error(), "state-value") || strings.Contains(err.Error(), "challenge-value") {
			t.Fatalf("case %d: error echoed secret: %q", i, err.Error())
		}
	}
}

func TestConsoleOAuthAuthorizationCodeFormHasExactFields(t *testing.T) {
	req := &ConsoleTokenRequest{
		GrantType:    GrantTypeAuthorizationCode,
		Code:         "auth-code",
		RedirectURI:  "http://127.0.0.1:12345/oauth/callback",
		ClientID:     ClientIDSameDevice,
		Scope:        Scope,
		CodeVerifier: "verifier-value",
	}

	form, err := buildTokenForm(req)
	if err != nil {
		t.Fatalf("buildTokenForm: %v", err)
	}

	want := map[string]string{
		"grant_type":    GrantTypeAuthorizationCode,
		"code":          "auth-code",
		"redirect_uri":  req.RedirectURI,
		"client_id":     ClientIDSameDevice,
		"scope":         Scope,
		"code_verifier": "verifier-value",
	}
	for k, v := range want {
		if got := form.Get(k); got != v {
			t.Fatalf("form %q = %q, want %q", k, got, v)
		}
	}
	if len(form) != len(want) {
		t.Fatalf("form has %d fields, want %d: %v", len(form), len(want), form)
	}
}

func TestConsoleOAuthRefreshTokenFormOmitsCodeAndVerifier(t *testing.T) {
	req := &ConsoleTokenRequest{
		GrantType:    GrantTypeRefreshToken,
		RefreshToken: "refresh-token-value",
		ClientID:     ClientIDSameDevice,
		Scope:        Scope,
	}

	form, err := buildTokenForm(req)
	if err != nil {
		t.Fatalf("buildTokenForm: %v", err)
	}

	// Must not contain code, redirect_uri, or code_verifier.
	for _, forbidden := range []string{"code", "redirect_uri", "code_verifier"} {
		if _, ok := form[forbidden]; ok {
			t.Fatalf("refresh form must not contain %q", forbidden)
		}
	}

	want := map[string]string{
		"grant_type":    GrantTypeRefreshToken,
		"refresh_token": "refresh-token-value",
		"client_id":     ClientIDSameDevice,
		"scope":         Scope,
	}
	for k, v := range want {
		if got := form.Get(k); got != v {
			t.Fatalf("form %q = %q, want %q", k, got, v)
		}
	}
	if len(form) != len(want) {
		t.Fatalf("form has %d fields, want %d: %v", len(form), len(want), form)
	}
}

func TestConsoleOAuthRetriesRetryableErrors(t *testing.T) {
	var attempts int32
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			return newResponse(http.StatusTooManyRequests,
				`{"error":"slow_down","error_description":"try again later"}`,
				map[string]string{RequestIDHeader: "req-429"}), nil
		}
		body, _ := json.Marshal(ConsoleTokenResponse{
			AccessToken:  json.RawMessage(`{"access_key_id":"ak","secret_access_key":"sk","session_token":"st"}`),
			TokenType:    "sts",
			ExpiresIn:    900,
			RefreshToken: "refresh-new",
			Scope:        Scope,
		})
		return newResponse(http.StatusOK, string(body), nil), nil
	})

	client, err := NewConsoleOAuthClient(&ConsoleOAuthClientConfig{
		HTTPClient: &http.Client{Transport: rt, Timeout: TokenTimeout},
		RetryClient: &httpx.RetryClient{
			HTTPClient:  &http.Client{Transport: rt, Timeout: TokenTimeout},
			MaxAttempts: 3,
			Sleeper:     noopSleeper,
			Clock:       fixedClock(time.Now()),
		},
	})
	if err != nil {
		t.Fatalf("NewConsoleOAuthClient: %v", err)
	}

	resp, err := client.ExchangeToken(context.Background(), &ConsoleTokenRequest{
		GrantType:    GrantTypeRefreshToken,
		RefreshToken: "refresh-old",
		ClientID:     ClientIDSameDevice,
		Scope:        Scope,
	})
	if err != nil {
		t.Fatalf("ExchangeToken: %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Fatalf("attempts = %d, want 3", got)
	}
	if resp.RefreshToken != "refresh-new" {
		t.Fatalf("refresh token = %q, want refresh-new", resp.RefreshToken)
	}
}

func TestConsoleOAuthDoesNotRetryInvalidGrant(t *testing.T) {
	var attempts int32
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&attempts, 1)
		return newResponse(http.StatusBadRequest,
			`{"error":"invalid_grant","error_description":"bad code"}`,
			map[string]string{RequestIDHeader: "req-400"}), nil
	})

	client, err := NewConsoleOAuthClient(&ConsoleOAuthClientConfig{
		RetryClient: &httpx.RetryClient{
			HTTPClient:  &http.Client{Transport: rt, Timeout: TokenTimeout},
			MaxAttempts: 3,
			Sleeper:     noopSleeper,
			Clock:       fixedClock(time.Now()),
		},
	})
	if err != nil {
		t.Fatalf("NewConsoleOAuthClient: %v", err)
	}

	_, err = client.ExchangeToken(context.Background(), &ConsoleTokenRequest{
		GrantType:    GrantTypeRefreshToken,
		RefreshToken: "refresh-old",
		ClientID:     ClientIDSameDevice,
		Scope:        Scope,
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Fatalf("attempts = %d, want 1", got)
	}

	var apiErr *ConsoleOAuthAPIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *ConsoleOAuthAPIError, got %T", err)
	}
	if apiErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", apiErr.StatusCode)
	}
	if apiErr.Response.Error != "invalid_grant" {
		t.Fatalf("error code = %q, want invalid_grant", apiErr.Response.Error)
	}
}

func TestConsoleOAuthErrorExposesRequestIDButNotRawBody(t *testing.T) {
	// The raw body contains a secret in a field that is NOT part of the
	// structured ConsoleOAuthErrorResponse, so it must never appear in the
	// error string. The error_description is a parsed field and may appear.
	const secretBody = `{"error":"invalid_grant","error_description":"bad code","internal_secret":"SECRET_BODY_DO_NOT_LEAK"}`
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return newResponse(http.StatusBadRequest, secretBody,
			map[string]string{RequestIDHeader: "req-abc-123"}), nil
	})

	client, err := NewConsoleOAuthClient(&ConsoleOAuthClientConfig{
		RetryClient: &httpx.RetryClient{
			HTTPClient:  &http.Client{Transport: rt, Timeout: TokenTimeout},
			MaxAttempts: 1,
			Sleeper:     noopSleeper,
			Clock:       fixedClock(time.Now()),
		},
	})
	if err != nil {
		t.Fatalf("NewConsoleOAuthClient: %v", err)
	}

	_, err = client.ExchangeToken(context.Background(), &ConsoleTokenRequest{
		GrantType:    GrantTypeRefreshToken,
		RefreshToken: "refresh-old",
		ClientID:     ClientIDSameDevice,
		Scope:        Scope,
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	text := err.Error()
	if !strings.Contains(text, "req-abc-123") {
		t.Fatalf("error %q must expose request ID", text)
	}
	if strings.Contains(text, "SECRET_BODY_DO_NOT_LEAK") {
		t.Fatalf("error %q leaked raw response body", text)
	}
	if strings.Contains(text, "refresh-old") {
		t.Fatalf("error %q leaked refresh token", text)
	}

	var apiErr *ConsoleOAuthAPIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *ConsoleOAuthAPIError, got %T", err)
	}
	if apiErr.RequestID != "req-abc-123" {
		t.Fatalf("request ID = %q, want req-abc-123", apiErr.RequestID)
	}
}

func TestConsoleEndpointRequiresCleanHTTPSURL(t *testing.T) {
	cases := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"default https", "https://signin.volcengine.com", false},
		{"with trailing slash", "https://signin.volcengine.com/", false},
		{"empty uses default", "", false},
		{"http scheme", "http://signin.volcengine.com", true},
		{"with userinfo", "https://user:pass@signin.volcengine.com", true},
		{"with query", "https://signin.volcengine.com?foo=bar", true},
		{"with fragment", "https://signin.volcengine.com#frag", true},
		{"with path", "https://signin.volcengine.com/some/path", true},
		{"opaque", "https:signin.volcengine.com", true},
		{"no host", "https://", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewConsoleOAuthClient(&ConsoleOAuthClientConfig{EndpointURL: tc.url})
			if tc.wantErr && err == nil {
				t.Fatalf("expected error for %q, got nil", tc.url)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.url, err)
			}
		})
	}

	// Trailing slash is normalized away.
	client, err := NewConsoleOAuthClient(&ConsoleOAuthClientConfig{EndpointURL: "https://signin.volcengine.com/"})
	if err != nil {
		t.Fatalf("NewConsoleOAuthClient: %v", err)
	}
	if client.EndpointURL() != "https://signin.volcengine.com" {
		t.Fatalf("endpoint = %q, want https://signin.volcengine.com", client.EndpointURL())
	}
}

func TestBuildAuthorizeURLRejectsInvalidClientID(t *testing.T) {
	client, err := NewConsoleOAuthClient(nil)
	if err != nil {
		t.Fatalf("NewConsoleOAuthClient: %v", err)
	}
	params := &AuthorizeParams{
		ClientID:            "some-other-client-id",
		RedirectURI:         "http://127.0.0.1:12345/oauth/callback",
		Scope:               Scope,
		State:               "state-value",
		CodeChallenge:       "challenge-value",
		CodeChallengeMethod: CodeChallengeMethodS256,
	}
	if _, err := client.BuildAuthorizeURL(params); err == nil {
		t.Fatal("expected error for invalid client_id, got nil")
	}
}

func TestBuildAuthorizeURLAcceptsBothFrozenClientIDs(t *testing.T) {
	client, err := NewConsoleOAuthClient(nil)
	if err != nil {
		t.Fatalf("NewConsoleOAuthClient: %v", err)
	}
	for _, id := range []string{ClientIDSameDevice, ClientIDCrossDevice} {
		params := &AuthorizeParams{
			ClientID:            id,
			RedirectURI:         "http://127.0.0.1:12345/oauth/callback",
			Scope:               Scope,
			State:               "state-value",
			CodeChallenge:       "challenge-value",
			CodeChallengeMethod: CodeChallengeMethodS256,
		}
		if _, err := client.BuildAuthorizeURL(params); err != nil {
			t.Fatalf("BuildAuthorizeURL for %q: %v", id, err)
		}
	}
}

func TestBuildAuthorizeURLRejectsInvalidScope(t *testing.T) {
	client, err := NewConsoleOAuthClient(nil)
	if err != nil {
		t.Fatalf("NewConsoleOAuthClient: %v", err)
	}
	params := &AuthorizeParams{
		ClientID:            ClientIDSameDevice,
		RedirectURI:         "http://127.0.0.1:12345/oauth/callback",
		Scope:               "wrong:scope",
		State:               "state-value",
		CodeChallenge:       "challenge-value",
		CodeChallengeMethod: CodeChallengeMethodS256,
	}
	if _, err := client.BuildAuthorizeURL(params); err == nil {
		t.Fatal("expected error for invalid scope, got nil")
	}
}

func TestBuildAuthorizeURLRejectsInvalidMethod(t *testing.T) {
	client, err := NewConsoleOAuthClient(nil)
	if err != nil {
		t.Fatalf("NewConsoleOAuthClient: %v", err)
	}
	params := &AuthorizeParams{
		ClientID:            ClientIDSameDevice,
		RedirectURI:         "http://127.0.0.1:12345/oauth/callback",
		Scope:               Scope,
		State:               "state-value",
		CodeChallenge:       "challenge-value",
		CodeChallengeMethod: "plain",
	}
	if _, err := client.BuildAuthorizeURL(params); err == nil {
		t.Fatal("expected error for invalid code_challenge_method, got nil")
	}
}

func TestBuildAuthorizeURLRejectsMissingRedirectURI(t *testing.T) {
	client, err := NewConsoleOAuthClient(nil)
	if err != nil {
		t.Fatalf("NewConsoleOAuthClient: %v", err)
	}
	params := &AuthorizeParams{
		ClientID:            ClientIDSameDevice,
		RedirectURI:         "",
		Scope:               Scope,
		State:               "state-value",
		CodeChallenge:       "challenge-value",
		CodeChallengeMethod: CodeChallengeMethodS256,
	}
	if _, err := client.BuildAuthorizeURL(params); err == nil {
		t.Fatal("expected error for missing redirect_uri, got nil")
	}
}

func TestConsoleOAuthAuthorizationCodeFormRejectsMissingRedirectURI(t *testing.T) {
	req := &ConsoleTokenRequest{
		GrantType:    GrantTypeAuthorizationCode,
		Code:         "auth-code",
		RedirectURI:  "",
		ClientID:     ClientIDSameDevice,
		Scope:        Scope,
		CodeVerifier: "verifier-value",
	}
	if _, err := buildTokenForm(req); err == nil {
		t.Fatal("expected error for missing redirect_uri, got nil")
	}
}

func TestConsoleOAuthAuthorizationCodeFormRejectsMissingScope(t *testing.T) {
	req := &ConsoleTokenRequest{
		GrantType:    GrantTypeAuthorizationCode,
		Code:         "auth-code",
		RedirectURI:  "http://127.0.0.1:12345/oauth/callback",
		ClientID:     ClientIDSameDevice,
		Scope:        "",
		CodeVerifier: "verifier-value",
	}
	if _, err := buildTokenForm(req); err == nil {
		t.Fatal("expected error for missing scope, got nil")
	}
}

func TestConsoleOAuthTokenFormRejectsInvalidClientID(t *testing.T) {
	req := &ConsoleTokenRequest{
		GrantType:    GrantTypeRefreshToken,
		RefreshToken: "refresh-token",
		ClientID:     "invalid-client",
		Scope:        Scope,
	}
	if _, err := buildTokenForm(req); err == nil {
		t.Fatal("expected error for invalid client_id, got nil")
	}
}

func TestConsoleOAuthTokenFormRejectsInvalidScope(t *testing.T) {
	req := &ConsoleTokenRequest{
		GrantType:    GrantTypeRefreshToken,
		RefreshToken: "refresh-token",
		ClientID:     ClientIDSameDevice,
		Scope:        "wrong:scope",
	}
	if _, err := buildTokenForm(req); err == nil {
		t.Fatal("expected error for invalid scope, got nil")
	}
}

func TestExchangeTokenAcceptsObjectAccessToken(t *testing.T) {
	const accessObj = `{"access_key_id":"AKLT-obj","secret_access_key":"sk-obj","session_token":"st-obj"}`
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, _ := json.Marshal(ConsoleTokenResponse{
			AccessToken: json.RawMessage(accessObj),
			TokenType:   "sts",
			ExpiresIn:   900,
			Scope:       Scope,
		})
		return newResponse(http.StatusOK, string(body), nil), nil
	})

	client, err := NewConsoleOAuthClient(&ConsoleOAuthClientConfig{
		RetryClient: &httpx.RetryClient{
			HTTPClient:  &http.Client{Transport: rt, Timeout: TokenTimeout},
			MaxAttempts: 1,
			Sleeper:     noopSleeper,
			Clock:       fixedClock(time.Now()),
		},
	})
	if err != nil {
		t.Fatalf("NewConsoleOAuthClient: %v", err)
	}

	resp, err := client.ExchangeToken(context.Background(), &ConsoleTokenRequest{
		GrantType:    GrantTypeAuthorizationCode,
		Code:         "auth-code",
		RedirectURI:  "http://127.0.0.1:12345/oauth/callback",
		ClientID:     ClientIDSameDevice,
		Scope:        Scope,
		CodeVerifier: "verifier-value",
	})
	if err != nil {
		t.Fatalf("ExchangeToken: %v", err)
	}
	creds, err := ParseSTSCredentials(resp.AccessToken)
	if err != nil {
		t.Fatalf("ParseSTSCredentials: %v", err)
	}
	if creds.AccessKeyID != "AKLT-obj" {
		t.Fatalf("access_key_id = %q, want AKLT-obj", creds.AccessKeyID)
	}
}

func TestExchangeTokenAcceptsJSONStringAccessToken(t *testing.T) {
	// The upstream format: access_token is a JSON string containing the object.
	const accessObj = `{"access_key_id":"AKLT-str","secret_access_key":"sk-str","session_token":"st-str"}`
	inner, _ := json.Marshal(accessObj)
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, _ := json.Marshal(ConsoleTokenResponse{
			AccessToken: inner,
			TokenType:   "sts",
			ExpiresIn:   900,
			Scope:       Scope,
		})
		return newResponse(http.StatusOK, string(body), nil), nil
	})

	client, err := NewConsoleOAuthClient(&ConsoleOAuthClientConfig{
		RetryClient: &httpx.RetryClient{
			HTTPClient:  &http.Client{Transport: rt, Timeout: TokenTimeout},
			MaxAttempts: 1,
			Sleeper:     noopSleeper,
			Clock:       fixedClock(time.Now()),
		},
	})
	if err != nil {
		t.Fatalf("NewConsoleOAuthClient: %v", err)
	}

	resp, err := client.ExchangeToken(context.Background(), &ConsoleTokenRequest{
		GrantType:    GrantTypeRefreshToken,
		RefreshToken: "refresh-token",
		ClientID:     ClientIDSameDevice,
		Scope:        Scope,
	})
	if err != nil {
		t.Fatalf("ExchangeToken: %v", err)
	}
	creds, err := ParseSTSCredentials(resp.AccessToken)
	if err != nil {
		t.Fatalf("ParseSTSCredentials: %v", err)
	}
	if creds.AccessKeyID != "AKLT-str" {
		t.Fatalf("access_key_id = %q, want AKLT-str", creds.AccessKeyID)
	}
}

func TestConsoleOAuthErrorDoesNotEchoErrorDescription(t *testing.T) {
	// The error_description contains a unique secret marker that must never
	// appear in the error string, even though it is a parsed structured field.
	const secretMarker = "SECRET_ERROR_DESC_MARKER_12345"
	const secretBody = `{"error":"invalid_grant","error_description":"` + secretMarker + `"}`
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return newResponse(http.StatusBadRequest, secretBody,
			map[string]string{RequestIDHeader: "req-safe-1"}), nil
	})

	client, err := NewConsoleOAuthClient(&ConsoleOAuthClientConfig{
		RetryClient: &httpx.RetryClient{
			HTTPClient:  &http.Client{Transport: rt, Timeout: TokenTimeout},
			MaxAttempts: 1,
			Sleeper:     noopSleeper,
			Clock:       fixedClock(time.Now()),
		},
	})
	if err != nil {
		t.Fatalf("NewConsoleOAuthClient: %v", err)
	}

	_, err = client.ExchangeToken(context.Background(), &ConsoleTokenRequest{
		GrantType:    GrantTypeRefreshToken,
		RefreshToken: "refresh-old",
		ClientID:     ClientIDSameDevice,
		Scope:        Scope,
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	text := err.Error()
	if strings.Contains(text, secretMarker) {
		t.Fatalf("error %q leaked error_description content", text)
	}
	// The structured field should still retain the description for inspection.
	var apiErr *ConsoleOAuthAPIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *ConsoleOAuthAPIError, got %T", err)
	}
	if apiErr.Response.ErrorDescription != secretMarker {
		t.Fatalf("structured ErrorDescription = %q, want %q", apiErr.Response.ErrorDescription, secretMarker)
	}
}

func TestConsoleOAuthAPIErrorDoesNotRenderArbitraryErrorValue(t *testing.T) {
	// A server can mirror a token or inject newlines into the `error` field.
	// Error() must never render the raw value; only allowlisted codes pass.
	const secretCanary = "SECRET_TOKEN_CANARY_98765"
	const injected = secretCanary + "\r\nSet-Cookie: hacked=1\r\n"

	cases := []struct {
		name  string
		error string
	}{
		{"secret canary in error", secretCanary},
		{"CRLF injection in error", injected},
		{"unknown error code", "totally_made_up_code"},
		{"empty error", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := &ConsoleOAuthAPIError{
				StatusCode: 400,
				Response:   ConsoleOAuthErrorResponse{Error: tc.error},
				RequestID:  "req-safe",
			}
			text := e.Error()
			if strings.Contains(text, secretCanary) {
				t.Fatalf("error %q leaked secret canary", text)
			}
			if strings.Contains(text, "Set-Cookie") {
				t.Fatalf("error %q allowed header injection", text)
			}
			if strings.ContainsAny(text, "\r\n") {
				t.Fatalf("error %q contains CR/LF", text)
			}
			// The raw value must still be available for programmatic inspection.
			if e.Response.Error != tc.error {
				t.Fatalf("structured Error = %q, want %q", e.Response.Error, tc.error)
			}
		})
	}
}

func TestConsoleOAuthAPIErrorRendersAllowlistedCodes(t *testing.T) {
	allowlisted := []string{
		"invalid_request", "invalid_client", "invalid_grant",
		"unauthorized_client", "unsupported_grant_type", "access_denied",
		"unsupported_response_type", "invalid_scope", "server_error",
		"temporarily_unavailable",
	}
	for _, code := range allowlisted {
		t.Run(code, func(t *testing.T) {
			e := &ConsoleOAuthAPIError{
				StatusCode: 400,
				Response:   ConsoleOAuthErrorResponse{Error: code},
			}
			text := e.Error()
			if !strings.Contains(text, code) {
				t.Fatalf("error %q does not contain allowlisted code %q", text, code)
			}
			if strings.ContainsAny(text, "\r\n") {
				t.Fatalf("error %q contains CR/LF", text)
			}
		})
	}
}

func TestConsoleOAuthAPIErrorDoesNotRenderUnsafeRequestID(t *testing.T) {
	// A server can mirror a token or inject newlines into the request ID header.
	// Only request IDs that look like valid diagnostic identifiers (bounded,
	// single-line, conservative charset) are rendered; everything else is
	// omitted from Error() while the raw value is preserved on the struct.
	const secretCanary = "SECRET+TOKEN/=CANARY_42"
	const injected = "REQID" + "\r\nX-Injected: true\r\n"

	cases := []struct {
		name      string
		requestID string
	}{
		{"secret canary with unsafe chars in request id", secretCanary},
		{"CRLF injection in request id", injected},
		{"request id too long", strings.Repeat("a", 129)},
		{"request id with spaces", "req id with spaces"},
		{"request id with control chars", "req\x00id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := &ConsoleOAuthAPIError{
				StatusCode: 400,
				Response:   ConsoleOAuthErrorResponse{Error: "invalid_grant"},
				RequestID:  tc.requestID,
			}
			text := e.Error()
			if strings.Contains(text, secretCanary) {
				t.Fatalf("error %q leaked request id canary", text)
			}
			if strings.Contains(text, "X-Injected") {
				t.Fatalf("error %q allowed header injection via request id", text)
			}
			if strings.ContainsAny(text, "\r\n") {
				t.Fatalf("error %q contains CR/LF", text)
			}
			// Raw RequestID must still be preserved for programmatic inspection.
			if e.RequestID != tc.requestID {
				t.Fatalf("structured RequestID = %q, want %q", e.RequestID, tc.requestID)
			}
		})
	}
}

func TestConsoleOAuthAPIErrorRendersSafeRequestID(t *testing.T) {
	// A short, single-line, conservatively-charset request ID should appear.
	const safeID = "req-abc_123.DEF/456:789"
	e := &ConsoleOAuthAPIError{
		StatusCode: 429,
		Response:   ConsoleOAuthErrorResponse{Error: "slow_down"},
		RequestID:  safeID,
	}
	text := e.Error()
	if !strings.Contains(text, safeID) {
		t.Fatalf("error %q does not contain safe request id %q", text, safeID)
	}
	if strings.ContainsAny(text, "\r\n") {
		t.Fatalf("error %q contains CR/LF", text)
	}
}

func TestExchangeTokenRejectsInvalidAccessToken(t *testing.T) {
	// A successful (2xx) response must carry a usable STS access_token. Values
	// such as null, true, [], or objects missing AK/SK/SessionToken must be
	// rejected. The outer JSON is always valid; only the inner access_token is
	// invalid. The error must never echo the raw access token.
	cases := []struct {
		name        string
		accessToken string
	}{
		{"null", `null`},
		{"empty string", `""`},
		{"boolean true", `true`},
		{"boolean false", `false`},
		{"number", `42`},
		{"array", `[]`},
		{"object missing access_key_id", `{"secret_access_key":"sk","session_token":"st"}`},
		{"object missing secret_access_key", `{"access_key_id":"ak","session_token":"st"}`},
		{"object missing session_token", `{"access_key_id":"ak","secret_access_key":"sk"}`},
		{"whitespace only fields", `{"access_key_id":"  ","secret_access_key":"  ","session_token":"  "}`},
		{"invalid inner json string", `"not-valid-json{{{"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The outer response must always marshal successfully so the test
			// exercises access_token validation, not outer JSON parse failure.
			body, err := json.Marshal(ConsoleTokenResponse{
				AccessToken: json.RawMessage(tc.accessToken),
				TokenType:   "sts",
				ExpiresIn:   900,
				Scope:       Scope,
			})
			if err != nil {
				t.Fatalf("fixture marshal failed for access_token=%q: %v", tc.accessToken, err)
			}
			rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return newResponse(http.StatusOK, string(body), nil), nil
			})
			client, err := NewConsoleOAuthClient(&ConsoleOAuthClientConfig{
				RetryClient: &httpx.RetryClient{
					HTTPClient:  &http.Client{Transport: rt, Timeout: TokenTimeout},
					MaxAttempts: 1,
					Sleeper:     noopSleeper,
					Clock:       fixedClock(time.Now()),
				},
			})
			if err != nil {
				t.Fatalf("NewConsoleOAuthClient: %v", err)
			}

			_, err = client.ExchangeToken(context.Background(), &ConsoleTokenRequest{
				GrantType:    GrantTypeRefreshToken,
				RefreshToken: "refresh-token",
				ClientID:     ClientIDSameDevice,
				Scope:        Scope,
			})
			if err == nil {
				t.Fatalf("expected error for access_token=%q, got nil", tc.accessToken)
			}
			// Error must never contain the raw access token or secret fields.
			if strings.Contains(err.Error(), tc.accessToken) {
				t.Fatalf("error %q echoed raw access token", err.Error())
			}
			if strings.Contains(err.Error(), "sk") || strings.Contains(err.Error(), "st") {
				t.Fatalf("error %q leaked secret fields", err.Error())
			}
		})
	}
}

func TestConsoleOAuthAPIErrorIsRetryable(t *testing.T) {
	cases := []struct {
		name      string
		err       *ConsoleOAuthAPIError
		retryable bool
	}{
		{"nil receiver", nil, false},
		{"408 request timeout", &ConsoleOAuthAPIError{StatusCode: 408}, true},
		{"429 too many requests", &ConsoleOAuthAPIError{StatusCode: 429}, true},
		{"500 internal server error", &ConsoleOAuthAPIError{StatusCode: 500}, true},
		{"502 bad gateway", &ConsoleOAuthAPIError{StatusCode: 502}, true},
		{"503 service unavailable", &ConsoleOAuthAPIError{StatusCode: 503}, true},
		{"400 bad request", &ConsoleOAuthAPIError{StatusCode: 400}, false},
		{"401 unauthorized", &ConsoleOAuthAPIError{StatusCode: 401}, false},
		{"403 forbidden", &ConsoleOAuthAPIError{StatusCode: 403}, false},
		{"404 not found", &ConsoleOAuthAPIError{StatusCode: 404}, false},
		{"200 ok (not an error)", &ConsoleOAuthAPIError{StatusCode: 200}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.IsRetryable(); got != tc.retryable {
				t.Fatalf("IsRetryable() = %v, want %v", got, tc.retryable)
			}
		})
	}
}
