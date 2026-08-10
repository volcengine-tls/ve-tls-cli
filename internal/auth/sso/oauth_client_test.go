package sso

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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

type clockFunc func() time.Time

func (c clockFunc) Now() time.Time { return c() }

func fixedClock(t time.Time) httpx.Clock {
	return clockFunc(func() time.Time { return t })
}

func newOAuthTestClient(t *testing.T, rt http.RoundTripper, maxAttempts int) *OAuthClient {
	t.Helper()
	client, err := NewOAuthClient(&OAuthClientConfig{
		BaseURL: "https://cloudidentity-oauth.cn-beijing.volces.com",
		RetryClient: &httpx.RetryClient{
			HTTPClient:  &http.Client{Transport: rt, Timeout: OAuthRequestTimeout},
			MaxAttempts: maxAttempts,
			Sleeper:     noopSleeper,
			Clock:       fixedClock(time.Now()),
		},
	})
	if err != nil {
		t.Fatalf("NewOAuthClient: %v", err)
	}
	return client
}

func TestOAuthRegisterClientRequest(t *testing.T) {
	var captured *http.Request
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		captured = req
		body, _ := json.Marshal(RegisterClientResponse{
			ClientID:              "client-id-123",
			ClientSecret:          "client-secret-456",
			ClientIDIssuedAt:      1700000000,
			ClientSecretExpiresAt: 0,
		})
		return newResponse(http.StatusOK, string(body), nil), nil
	})

	client := newOAuthTestClient(t, rt, 1)

	req := &RegisterClientRequest{
		ClientName: "volclog-test",
		ClientType: "public",
		GrantTypes: []string{GrantTypeDeviceCode, GrantTypeRefreshToken},
		Scopes:     []string{ScopeAccountAccess, ScopeOfflineAccess},
	}

	resp, err := client.RegisterClient(context.Background(), req)
	if err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}
	if resp.ClientID != "client-id-123" {
		t.Fatalf("client_id = %q, want client-id-123", resp.ClientID)
	}
	if resp.ClientSecret != "client-secret-456" {
		t.Fatalf("client_secret = %q, want client-secret-456", resp.ClientSecret)
	}

	// Verify the request was a POST to the register path with JSON content type.
	if captured.Method != http.MethodPost {
		t.Fatalf("method = %q, want POST", captured.Method)
	}
	if captured.URL.Path != RegisterPath {
		t.Fatalf("path = %q, want %q", captured.URL.Path, RegisterPath)
	}
	if ct := captured.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q, want application/json", ct)
	}

	// Verify the request body matches the expected JSON.
	var sent RegisterClientRequest
	if err := json.NewDecoder(captured.Body).Decode(&sent); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	if sent.ClientName != "volclog-test" {
		t.Fatalf("client_name = %q, want volclog-test", sent.ClientName)
	}
	if sent.ClientType != "public" {
		t.Fatalf("client_type = %q, want public", sent.ClientType)
	}
	if len(sent.GrantTypes) != 2 || sent.GrantTypes[0] != GrantTypeDeviceCode || sent.GrantTypes[1] != GrantTypeRefreshToken {
		t.Fatalf("grant_types = %v, want [device_code refresh_token]", sent.GrantTypes)
	}
	if len(sent.Scopes) != 2 || sent.Scopes[0] != ScopeAccountAccess || sent.Scopes[1] != ScopeOfflineAccess {
		t.Fatalf("scopes = %v, want [account:access offline_access]", sent.Scopes)
	}
}

func TestOAuthStartDeviceAuthorizationRequest(t *testing.T) {
	var captured *http.Request
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		captured = req
		body, _ := json.Marshal(StartDeviceAuthorizationResponse{
			DeviceCode:              "device-code-abc",
			UserCode:                "USER-CODE",
			VerificationURI:         "https://example.com/verify",
			VerificationURIComplete: "https://example.com/verify?code=USER-CODE",
			ExpiresIn:               900,
			Interval:                5,
		})
		return newResponse(http.StatusOK, string(body), nil), nil
	})

	client := newOAuthTestClient(t, rt, 1)

	req := &StartDeviceAuthorizationRequest{
		ClientID:     "client-id-123",
		ClientSecret: "client-secret-456",
		Scopes:       []string{ScopeAccountAccess, ScopeOfflineAccess},
	}

	resp, err := client.StartDeviceAuthorization(context.Background(), req)
	if err != nil {
		t.Fatalf("StartDeviceAuthorization: %v", err)
	}
	if resp.DeviceCode != "device-code-abc" {
		t.Fatalf("device_code = %q, want device-code-abc", resp.DeviceCode)
	}
	if resp.UserCode != "USER-CODE" {
		t.Fatalf("user_code = %q, want USER-CODE", resp.UserCode)
	}
	if resp.VerificationURI != "https://example.com/verify" {
		t.Fatalf("verification_uri = %q", resp.VerificationURI)
	}
	if resp.ExpiresIn != 900 {
		t.Fatalf("expires_in = %d, want 900", resp.ExpiresIn)
	}

	if captured.Method != http.MethodPost {
		t.Fatalf("method = %q, want POST", captured.Method)
	}
	if captured.URL.Path != DeviceAuthorizationPath {
		t.Fatalf("path = %q, want %q", captured.URL.Path, DeviceAuthorizationPath)
	}

	var sent StartDeviceAuthorizationRequest
	if err := json.NewDecoder(captured.Body).Decode(&sent); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	if sent.ClientID != "client-id-123" {
		t.Fatalf("client_id = %q", sent.ClientID)
	}
	if sent.ClientSecret != "client-secret-456" {
		t.Fatalf("client_secret = %q", sent.ClientSecret)
	}
}

func TestOAuthCreateDeviceAndRefreshTokenRequests(t *testing.T) {
	t.Run("device_code", func(t *testing.T) {
		var captured *http.Request
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			captured = req
			body, _ := json.Marshal(CreateTokenResponse{
				AccessToken:  "access-token-xyz",
				TokenType:    "Bearer",
				RefreshToken: "refresh-token-xyz",
				ExpiresIn:    3600,
			})
			return newResponse(http.StatusOK, string(body), nil), nil
		})

		client := newOAuthTestClient(t, rt, 1)

		req := &CreateTokenRequest{
			GrantType:    GrantTypeDeviceCode,
			ClientID:     "client-id-123",
			ClientSecret: "client-secret-456",
			DeviceCode:   "device-code-abc",
		}

		resp, err := client.CreateToken(context.Background(), req)
		if err != nil {
			t.Fatalf("CreateToken: %v", err)
		}
		if resp.AccessToken != "access-token-xyz" {
			t.Fatalf("access_token = %q", resp.AccessToken)
		}
		if resp.RefreshToken != "refresh-token-xyz" {
			t.Fatalf("refresh_token = %q", resp.RefreshToken)
		}

		if captured.URL.Path != TokenPath {
			t.Fatalf("path = %q, want %q", captured.URL.Path, TokenPath)
		}

		var sent CreateTokenRequest
		if err := json.NewDecoder(captured.Body).Decode(&sent); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if sent.GrantType != GrantTypeDeviceCode {
			t.Fatalf("grant_type = %q", sent.GrantType)
		}
		if sent.DeviceCode != "device-code-abc" {
			t.Fatalf("device_code = %q", sent.DeviceCode)
		}
		if sent.RefreshToken != "" {
			t.Fatalf("refresh_token should be empty for device_code grant, got %q", sent.RefreshToken)
		}
	})

	t.Run("refresh_token", func(t *testing.T) {
		var captured *http.Request
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			captured = req
			body, _ := json.Marshal(CreateTokenResponse{
				AccessToken:  "access-token-new",
				TokenType:    "Bearer",
				RefreshToken: "refresh-token-new",
				ExpiresIn:    3600,
			})
			return newResponse(http.StatusOK, string(body), nil), nil
		})

		client := newOAuthTestClient(t, rt, 1)

		req := &CreateTokenRequest{
			GrantType:    GrantTypeRefreshToken,
			ClientID:     "client-id-123",
			ClientSecret: "client-secret-456",
			RefreshToken: "refresh-token-old",
		}

		resp, err := client.CreateToken(context.Background(), req)
		if err != nil {
			t.Fatalf("CreateToken: %v", err)
		}
		if resp.AccessToken != "access-token-new" {
			t.Fatalf("access_token = %q", resp.AccessToken)
		}

		var sent CreateTokenRequest
		if err := json.NewDecoder(captured.Body).Decode(&sent); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if sent.GrantType != GrantTypeRefreshToken {
			t.Fatalf("grant_type = %q", sent.GrantType)
		}
		if sent.RefreshToken != "refresh-token-old" {
			t.Fatalf("refresh_token = %q", sent.RefreshToken)
		}
		if sent.DeviceCode != "" {
			t.Fatalf("device_code should be empty for refresh_token grant, got %q", sent.DeviceCode)
		}
	})
}

func TestOAuthRevokeRequest(t *testing.T) {
	var captured *http.Request
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		captured = req
		return newResponse(http.StatusOK, `{}`, nil), nil
	})

	client := newOAuthTestClient(t, rt, 1)

	req := &RevokeTokenRequest{
		ClientID:     "client-id-123",
		ClientSecret: "client-secret-456",
		Token:        "access-token-xyz",
	}

	if err := client.RevokeToken(context.Background(), req); err != nil {
		t.Fatalf("RevokeToken: %v", err)
	}

	if captured.Method != http.MethodPost {
		t.Fatalf("method = %q, want POST", captured.Method)
	}
	if captured.URL.Path != RevokePath {
		t.Fatalf("path = %q, want %q", captured.URL.Path, RevokePath)
	}

	var sent RevokeTokenRequest
	if err := json.NewDecoder(captured.Body).Decode(&sent); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	if sent.Token != "access-token-xyz" {
		t.Fatalf("token = %q", sent.Token)
	}
}

func TestOAuthRetries429And5xxButNotOther4xx(t *testing.T) {
	cases := []struct {
		name        string
		status      int
		wantRetry   bool
		maxAttempts int
	}{
		{"429 retries", http.StatusTooManyRequests, true, 3},
		{"408 retries", http.StatusRequestTimeout, true, 3},
		{"500 retries", http.StatusInternalServerError, true, 3},
		{"503 retries", http.StatusServiceUnavailable, true, 3},
		{"400 does not retry", http.StatusBadRequest, false, 3},
		{"401 does not retry", http.StatusUnauthorized, false, 3},
		{"403 does not retry", http.StatusForbidden, false, 3},
		{"404 does not retry", http.StatusNotFound, false, 3},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var attempts int32
			rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				atomic.AddInt32(&attempts, 1)
				return newResponse(tc.status, `{"error":"server_error"}`, nil), nil
			})

			client := newOAuthTestClient(t, rt, tc.maxAttempts)

			_, err := client.CreateToken(context.Background(), &CreateTokenRequest{
				GrantType:    GrantTypeDeviceCode,
				ClientID:     "cid",
				ClientSecret: "csec",
				DeviceCode:   "dc",
			})
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			got := atomic.LoadInt32(&attempts)
			if tc.wantRetry {
				if got != int32(tc.maxAttempts) {
					t.Fatalf("attempts = %d, want %d (retried)", got, tc.maxAttempts)
				}
			} else {
				if got != 1 {
					t.Fatalf("attempts = %d, want 1 (not retried)", got)
				}
			}
		})
	}
}

func TestOAuthRejectsIncompleteSuccessResponse(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"missing access_token", `{"token_type":"Bearer","expires_in":3600}`},
		{"missing token_type", `{"access_token":"abc","expires_in":3600}`},
		{"missing expires_in", `{"access_token":"abc","token_type":"Bearer"}`},
		{"zero expires_in", `{"access_token":"abc","token_type":"Bearer","expires_in":0}`},
		{"empty object", `{}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return newResponse(http.StatusOK, tc.body, nil), nil
			})
			client := newOAuthTestClient(t, rt, 1)

			_, err := client.CreateToken(context.Background(), &CreateTokenRequest{
				GrantType:    GrantTypeDeviceCode,
				ClientID:     "cid",
				ClientSecret: "csec",
				DeviceCode:   "dc",
			})
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
		})
	}

	// Also test incomplete register and device authorization responses.
	t.Run("register missing client_id", func(t *testing.T) {
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return newResponse(http.StatusOK, `{"client_secret":"sec"}`, nil), nil
		})
		client := newOAuthTestClient(t, rt, 1)
		_, err := client.RegisterClient(context.Background(), &RegisterClientRequest{
			ClientName: "n", ClientType: "public",
		})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("device auth missing device_code", func(t *testing.T) {
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return newResponse(http.StatusOK, `{"user_code":"UC","verification_uri":"u","expires_in":60}`, nil), nil
		})
		client := newOAuthTestClient(t, rt, 1)
		_, err := client.StartDeviceAuthorization(context.Background(), &StartDeviceAuthorizationRequest{
			ClientID: "cid", ClientSecret: "csec",
		})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestOAuthHonorsContextCancellation(t *testing.T) {
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return newResponse(http.StatusTooManyRequests, `{"error":"slow_down"}`, nil), nil
	})

	client := newOAuthTestClient(t, rt, 5)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.CreateToken(ctx, &CreateTokenRequest{
		GrantType:    GrantTypeDeviceCode,
		ClientID:     "cid",
		ClientSecret: "csec",
		DeviceCode:   "dc",
	})
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestOAuthErrorsNeverStoreRawBodyOrTokens(t *testing.T) {
	const (
		// Canaries placed in the server response that must never surface.
		descCanary    = "DESCRIPTION_CANARY_TOKEN"
		codeCanary    = "ERROR_CODE_CANARY"
		reqIDCanary   = "REQID_CANARY_SECRET"
		secretBody    = "SECRET_BODY_DO_NOT_LEAK"
		accessToken   = "ACCESS_TOKEN_CANARY"
		refreshToken  = "REFRESH_TOKEN_CANARY"
		clientSecret  = "CLIENT_SECRET_CANARY"
		deviceCode    = "DEVICE_CODE_CANARY"
		safeRequestID = "req-abc-123"
	)

	// Build a response that smuggles canaries into every field the old code
	// used to store: error, error_description, and the request ID header.
	errBody := `{"error":"` + codeCanary + `","error_description":"` + descCanary + `","internal_secret":"` + secretBody + `"}`

	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return newResponse(http.StatusBadRequest, errBody,
			map[string]string{RequestIDHeader: safeRequestID}), nil
	})

	client := newOAuthTestClient(t, rt, 1)

	_, err := client.CreateToken(context.Background(), &CreateTokenRequest{
		GrantType:    GrantTypeDeviceCode,
		ClientID:     "cid",
		ClientSecret: clientSecret,
		DeviceCode:   deviceCode,
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	canaries := []string{
		descCanary,
		codeCanary,
		secretBody,
		accessToken,
		refreshToken,
		clientSecret,
		deviceCode,
	}

	// Surface 1: Error() string.
	text := err.Error()
	for _, c := range canaries {
		if strings.Contains(text, c) {
			t.Fatalf("Error() leaked canary %q: %s", c, text)
		}
	}

	// Surface 2: fmt.Sprintf("%+v", err).
	verbose := fmt.Sprintf("%+v", err)
	for _, c := range canaries {
		if strings.Contains(verbose, c) {
			t.Fatalf("%%+v leaked canary %q: %s", c, verbose)
		}
	}

	// Surface 3: JSON marshal of the error.
	jsonBytes, jerr := json.Marshal(err)
	if jerr != nil {
		t.Fatalf("json.Marshal: %v", jerr)
	}
	for _, c := range canaries {
		if strings.Contains(string(jsonBytes), c) {
			t.Fatalf("json.Marshal leaked canary %q: %s", c, string(jsonBytes))
		}
	}

	// Surface 4: exported fields of the structured error.
	var apiErr *OAuthAPIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *OAuthAPIError, got %T", err)
	}
	if apiErr.RequestID != safeRequestID {
		t.Fatalf("request ID = %q, want %q", apiErr.RequestID, safeRequestID)
	}
	// The non-allowlisted code canary must be discarded.
	if apiErr.Code != "" {
		t.Fatalf("Code = %q, want empty (non-allowlisted code must be discarded)", apiErr.Code)
	}
	// No exported field may contain any canary.
	fields := fmt.Sprintf("%d|%s|%s", apiErr.StatusCode, apiErr.Code, apiErr.RequestID)
	for _, c := range canaries {
		if strings.Contains(fields, c) {
			t.Fatalf("exported field leaked canary %q: %s", c, fields)
		}
	}

	// The safe request ID should still be usable for diagnosis.
	if !strings.Contains(text, safeRequestID) {
		t.Fatalf("Error() should expose safe request ID: %s", text)
	}
}

func TestOAuthErrorsPreserveAllowlistedCode(t *testing.T) {
	// An allowlisted code must survive so Task 11 can errors.As for
	// authorization_pending / slow_down / expired_token.
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return newResponse(http.StatusBadRequest,
			`{"error":"authorization_pending","error_description":"DESC_CANARY"}`,
			map[string]string{RequestIDHeader: "req-pending"}), nil
	})
	client := newOAuthTestClient(t, rt, 1)

	_, err := client.CreateToken(context.Background(), &CreateTokenRequest{
		GrantType:    GrantTypeDeviceCode,
		ClientID:     "cid",
		ClientSecret: "csec",
		DeviceCode:   "dc",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var apiErr *OAuthAPIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *OAuthAPIError, got %T", err)
	}
	if apiErr.Code != "authorization_pending" {
		t.Fatalf("Code = %q, want authorization_pending", apiErr.Code)
	}
	// The description canary must never be stored or rendered.
	if strings.Contains(err.Error(), "DESC_CANARY") {
		t.Fatalf("Error() leaked description: %s", err.Error())
	}
	if strings.Contains(fmt.Sprintf("%+v", err), "DESC_CANARY") {
		t.Fatalf("%%+v leaked description")
	}
}

func TestOAuthErrorsSanitizeUnsafeRequestID(t *testing.T) {
	// A request ID containing unsafe characters must be dropped entirely, not
	// stored or rendered.
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return newResponse(http.StatusBadRequest,
			`{"error":"invalid_grant"}`,
			map[string]string{RequestIDHeader: "unsafe\r\nid\x00with\x00control"}), nil
	})
	client := newOAuthTestClient(t, rt, 1)

	_, err := client.CreateToken(context.Background(), &CreateTokenRequest{
		GrantType:    GrantTypeDeviceCode,
		ClientID:     "cid",
		ClientSecret: "csec",
		DeviceCode:   "dc",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var apiErr *OAuthAPIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *OAuthAPIError, got %T", err)
	}
	if apiErr.RequestID != "" {
		t.Fatalf("RequestID = %q, want empty for unsafe value", apiErr.RequestID)
	}
	if strings.Contains(err.Error(), "unsafe") || strings.Contains(err.Error(), "control") {
		t.Fatalf("Error() leaked unsafe request ID: %s", err.Error())
	}
}

func TestOAuthNilReceiverAndContext(t *testing.T) {
	var nilClient *OAuthClient

	if _, err := nilClient.RegisterClient(context.Background(), &RegisterClientRequest{}); err == nil {
		t.Fatal("expected error for nil client")
	}
	if _, err := nilClient.StartDeviceAuthorization(context.Background(), &StartDeviceAuthorizationRequest{}); err == nil {
		t.Fatal("expected error for nil client")
	}
	if _, err := nilClient.CreateToken(context.Background(), &CreateTokenRequest{}); err == nil {
		t.Fatal("expected error for nil client")
	}
	if err := nilClient.RevokeToken(context.Background(), &RevokeTokenRequest{}); err == nil {
		t.Fatal("expected error for nil client")
	}

	client := newOAuthTestClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return newResponse(http.StatusOK, `{}`, nil), nil
	}), 1)

	//lint:ignore SA1012 verifies RegisterClient rejects a nil context
	if _, err := client.RegisterClient(nil, &RegisterClientRequest{ClientName: "n", ClientType: "public"}); err == nil {
		t.Fatal("expected error for nil context")
	}
	//lint:ignore SA1012 verifies CreateToken rejects a nil context
	if _, err := client.CreateToken(nil, &CreateTokenRequest{GrantType: GrantTypeDeviceCode, ClientID: "c", ClientSecret: "s", DeviceCode: "d"}); err == nil {
		t.Fatal("expected error for nil context")
	}
}

func TestOAuthBaseURLValidation(t *testing.T) {
	cases := []struct {
		name    string
		baseURL string
		wantErr bool
	}{
		{"default https", "https://cloudidentity-oauth.cn-beijing.volces.com", false},
		{"with trailing slash", "https://cloudidentity-oauth.cn-beijing.volces.com/", false},
		{"http scheme", "http://cloudidentity-oauth.cn-beijing.volces.com", true},
		{"with userinfo", "https://user:pass@cloudidentity-oauth.cn-beijing.volces.com", true},
		{"with query", "https://cloudidentity-oauth.cn-beijing.volces.com?foo=bar", true},
		{"with fragment", "https://cloudidentity-oauth.cn-beijing.volces.com#frag", true},
		{"with path", "https://cloudidentity-oauth.cn-beijing.volces.com/some/path", true},
		{"no host", "https://", true},
		{"empty uses default", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewOAuthClient(&OAuthClientConfig{BaseURL: tc.baseURL})
			if tc.wantErr && err == nil {
				t.Fatalf("expected error for %q, got nil", tc.baseURL)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.baseURL, err)
			}
		})
	}

	// Default region builds the correct URL.
	client, err := NewOAuthClient(&OAuthClientConfig{Region: "ap-southeast-1"})
	if err != nil {
		t.Fatalf("NewOAuthClient: %v", err)
	}
	want := "https://cloudidentity-oauth.ap-southeast-1.volces.com"
	if client.BaseURL() != want {
		t.Fatalf("base URL = %q, want %q", client.BaseURL(), want)
	}
}

func TestOAuthMalformedJSONAndTransportError(t *testing.T) {
	t.Run("malformed JSON", func(t *testing.T) {
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return newResponse(http.StatusOK, `{not valid json`, nil), nil
		})
		client := newOAuthTestClient(t, rt, 1)
		_, err := client.CreateToken(context.Background(), &CreateTokenRequest{
			GrantType: GrantTypeDeviceCode, ClientID: "c", ClientSecret: "s", DeviceCode: "d",
		})
		if err == nil {
			t.Fatal("expected error for malformed JSON, got nil")
		}
	})

	t.Run("transport error", func(t *testing.T) {
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return nil, errors.New("dial tcp: connection refused")
		})
		client := newOAuthTestClient(t, rt, 1)
		_, err := client.CreateToken(context.Background(), &CreateTokenRequest{
			GrantType: GrantTypeDeviceCode, ClientID: "c", ClientSecret: "s", DeviceCode: "d",
		})
		if err == nil {
			t.Fatal("expected error for transport failure, got nil")
		}
	})
}

func TestOAuthRetryReplaysRequestBody(t *testing.T) {
	var bodies []string
	var attempts int32
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		n := atomic.AddInt32(&attempts, 1)
		body, _ := io.ReadAll(req.Body)
		bodies = append(bodies, string(body))
		if n < 3 {
			return newResponse(http.StatusTooManyRequests, `{"error":"slow_down"}`, nil), nil
		}
		return newResponse(http.StatusOK, `{"access_token":"at","token_type":"Bearer","expires_in":3600}`, nil), nil
	})

	client := newOAuthTestClient(t, rt, 3)

	_, err := client.CreateToken(context.Background(), &CreateTokenRequest{
		GrantType:    GrantTypeDeviceCode,
		ClientID:     "cid",
		ClientSecret: "csec",
		DeviceCode:   "dc",
	})
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}

	if len(bodies) != 3 {
		t.Fatalf("got %d bodies, want 3", len(bodies))
	}
	// Each retry must send the same body.
	for i, b := range bodies {
		if b != bodies[0] {
			t.Fatalf("body %d differs from body 0: %q vs %q", i, b, bodies[0])
		}
	}
}

func TestOAuthRegisterNotRetried(t *testing.T) {
	var attempts int32
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&attempts, 1)
		return newResponse(http.StatusTooManyRequests, `{"error":"slow_down"}`, nil), nil
	})

	// Even with maxAttempts=3, registration should only attempt once.
	client := newOAuthTestClient(t, rt, 3)

	_, err := client.RegisterClient(context.Background(), &RegisterClientRequest{
		ClientName: "n", ClientType: "public",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Fatalf("register attempts = %d, want 1 (not retried)", got)
	}
}

// TestOAuthRetryClientPrecedenceOverHTTPClient verifies that when both
// HTTPClient and RetryClient are supplied, the RetryClient (and its own
// HTTPClient/transport) takes precedence. The separate HTTPClient field must
// not override the injected transport.
func TestOAuthRetryClientPrecedenceOverHTTPClient(t *testing.T) {
	const marker = "retry-client-transport"
	retryRT := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, _ := json.Marshal(CreateTokenResponse{
			AccessToken: "at", TokenType: "Bearer", ExpiresIn: 3600,
		})
		resp := newResponse(http.StatusOK, string(body), nil)
		resp.Header.Set("X-Transport", marker)
		return resp, nil
	})
	ignoredRT := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		t.Fatal("ignored HTTPClient transport was used; RetryClient should take precedence")
		return nil, nil
	})

	client, err := NewOAuthClient(&OAuthClientConfig{
		BaseURL: "https://cloudidentity-oauth.cn-beijing.volces.com",
		HTTPClient: &http.Client{
			Transport: ignoredRT,
			Timeout:   OAuthRequestTimeout,
		},
		RetryClient: &httpx.RetryClient{
			HTTPClient:  &http.Client{Transport: retryRT, Timeout: OAuthRequestTimeout},
			MaxAttempts: 1,
			Sleeper:     noopSleeper,
			Clock:       fixedClock(time.Now()),
		},
	})
	if err != nil {
		t.Fatalf("NewOAuthClient: %v", err)
	}

	_, err = client.CreateToken(context.Background(), &CreateTokenRequest{
		GrantType: GrantTypeDeviceCode, ClientID: "c", ClientSecret: "s", DeviceCode: "d",
	})
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
}

// TestOAuthRegisterClientRejectsNegativeMaxAttempts verifies that a
// caller-supplied RetryClient with a negative MaxAttempts produces a fixed
// configuration error rather than being silently coerced to a legal value.
func TestOAuthRegisterClientRejectsNegativeMaxAttempts(t *testing.T) {
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		t.Fatal("request should not be made when config is invalid")
		return nil, nil
	})
	client, err := NewOAuthClient(&OAuthClientConfig{
		BaseURL: "https://cloudidentity-oauth.cn-beijing.volces.com",
		RetryClient: &httpx.RetryClient{
			HTTPClient:  &http.Client{Transport: rt, Timeout: OAuthRequestTimeout},
			MaxAttempts: -1,
			Sleeper:     noopSleeper,
			Clock:       fixedClock(time.Now()),
		},
	})
	if err != nil {
		t.Fatalf("NewOAuthClient: %v", err)
	}

	_, err = client.RegisterClient(context.Background(), &RegisterClientRequest{
		ClientName: "n", ClientType: "public",
	})
	if err == nil {
		t.Fatal("expected error for negative MaxAttempts, got nil")
	}
	if !strings.Contains(err.Error(), "MaxAttempts") {
		t.Fatalf("error should mention MaxAttempts: %v", err)
	}
}

// TestOAuthDeviceAuthorizationResponseBoundaries verifies that the device
// authorization response is validated for a non-negative interval and HTTPS
// absolute verification URIs with a non-empty hostname and no userinfo. Normal
// path, query, and fragment components are permitted (the upstream service may
// use a fragment for SPA routing or to embed the user code in
// verification_uri_complete). Invalid URIs are rejected with a fixed error that
// never echoes the URI (which may carry the user code).
func TestOAuthDeviceAuthorizationResponseBoundaries(t *testing.T) {
	// buildResp returns a response body with the given verification fields.
	buildResp := func(uri, uriComplete string, interval int) string {
		b, _ := json.Marshal(StartDeviceAuthorizationResponse{
			DeviceCode:              "dc",
			UserCode:                "UC",
			VerificationURI:         uri,
			VerificationURIComplete: uriComplete,
			ExpiresIn:               900,
			Interval:                interval,
		})
		return string(b)
	}

	cases := []struct {
		name        string
		uri         string
		uriComplete string
		interval    int
		wantErr     bool
	}{
		{"valid https uri", "https://example.com/verify", "", 5, false},
		{"valid https uri with path and query", "https://example.com/verify?foo=bar", "", 5, false},
		{"valid https uri with fragment", "https://example.com/verify#frag", "", 5, false},
		{"valid complete uri", "https://example.com/verify", "https://example.com/verify?code=UC", 5, false},
		{"valid complete uri with fragment", "https://example.com/verify", "https://example.com/verify#code=UC", 5, false},
		{"zero interval allowed", "https://example.com/verify", "", 0, false},
		{"negative interval rejected", "https://example.com/verify", "", -1, true},
		{"http scheme rejected", "http://example.com/verify", "", 5, true},
		{"relative path rejected", "/verify", "", 5, true},
		{"file scheme rejected", "file:///etc/passwd", "", 5, true},
		{"userinfo rejected", "https://user:pass@example.com/verify", "", 5, true},
		{"empty hostname rejected", "https://:443/verify", "", 5, true},
		{"complete uri http rejected", "https://example.com/verify", "http://example.com/verify", 5, true},
		{"complete uri userinfo rejected", "https://example.com/verify", "https://u:p@example.com/verify", 5, true},
		{"complete uri empty hostname rejected", "https://example.com/verify", "https://:443/verify", 5, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return newResponse(http.StatusOK, buildResp(tc.uri, tc.uriComplete, tc.interval), nil), nil
			})
			client := newOAuthTestClient(t, rt, 1)
			_, err := client.StartDeviceAuthorization(context.Background(), &StartDeviceAuthorizationRequest{
				ClientID: "cid", ClientSecret: "csec",
			})
			if tc.wantErr && err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error for %s: %v", tc.name, err)
			}
			// The error must never echo the URI, which may carry the user code.
			if err != nil {
				if strings.Contains(err.Error(), tc.uri) && tc.uri != "" {
					t.Fatalf("error echoed verification URI: %s", err.Error())
				}
				if strings.Contains(err.Error(), "UC") {
					t.Fatalf("error echoed user code: %s", err.Error())
				}
			}
		})
	}
}
