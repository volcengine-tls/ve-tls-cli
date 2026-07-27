package sso

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth/httpx"
)

func newPortalTestClient(t *testing.T, rt http.RoundTripper, maxAttempts int) *PortalClient {
	t.Helper()
	client, err := NewPortalClient(&PortalClientConfig{
		BaseURL: "https://cloudidentity-portal.cn-beijing.volces.com",
		RetryClient: &httpx.RetryClient{
			HTTPClient:  &http.Client{Transport: rt, Timeout: PortalRequestTimeout},
			MaxAttempts: maxAttempts,
			Sleeper:     noopSleeper,
			Clock:       fixedClock(time.Now()),
		},
	})
	if err != nil {
		t.Fatalf("NewPortalClient: %v", err)
	}
	return client
}

func makePortalEnvelope(result json.RawMessage, requestID string) string {
	env := map[string]any{
		"ResponseMetadata": map[string]any{"RequestId": requestID},
		"Result":           result,
	}
	b, _ := json.Marshal(env)
	return string(b)
}

func TestPortalListAccountsPaginatesAllPages(t *testing.T) {
	var requests []*http.Request
	page := 0
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests = append(requests, req)
		page++
		var result json.RawMessage
		if page == 1 {
			result, _ = json.Marshal(map[string]any{
				"Total":       3,
				"PageNumber":  1,
				"PageSize":    2,
				"AccountList": []map[string]string{{"AccountId": "a1", "AccountName": "Account 1"}, {"AccountId": "a2", "AccountName": "Account 2"}},
			})
		} else {
			result, _ = json.Marshal(map[string]any{
				"Total":       3,
				"PageNumber":  2,
				"PageSize":    2,
				"AccountList": []map[string]string{{"AccountId": "a3", "AccountName": "Account 3"}},
			})
		}
		return newResponse(http.StatusOK, makePortalEnvelope(result, "req-list-accounts"), nil), nil
	})

	client := newPortalTestClient(t, rt, 1)

	accounts, err := client.ListAccounts(context.Background(), "access-token-xyz")
	if err != nil {
		t.Fatalf("ListAccounts: %v", err)
	}
	if len(accounts) != 3 {
		t.Fatalf("got %d accounts, want 3", len(accounts))
	}
	if accounts[0].AccountID != "a1" || accounts[2].AccountID != "a3" {
		t.Fatalf("unexpected accounts: %+v", accounts)
	}
	if len(requests) != 2 {
		t.Fatalf("got %d requests, want 2", len(requests))
	}

	// Verify each request has the correct path and query parameters.
	for i, req := range requests {
		if req.URL.Path != ListAccountsPath {
			t.Fatalf("request %d path = %q, want %q", i, req.URL.Path, ListAccountsPath)
		}
		if req.URL.Query().Get("page_size") == "" {
			t.Fatalf("request %d missing page_size", i)
		}
		if req.URL.Query().Get("page_number") == "" {
			t.Fatalf("request %d missing page_number", i)
		}
	}
}

func TestPortalListRolesPaginatesAllPagesAndEncodesAccount(t *testing.T) {
	var requests []*http.Request
	page := 0
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests = append(requests, req)
		page++
		var result json.RawMessage
		if page == 1 {
			result, _ = json.Marshal(map[string]any{
				"Total":      2,
				"PageNumber": 1,
				"PageSize":   1,
				"RoleList":   []map[string]string{{"AccountId": "acc-1", "RoleName": "Role 1"}},
			})
		} else {
			result, _ = json.Marshal(map[string]any{
				"Total":      2,
				"PageNumber": 2,
				"PageSize":   1,
				"RoleList":   []map[string]string{{"AccountId": "acc-1", "RoleName": "Role 2"}},
			})
		}
		return newResponse(http.StatusOK, makePortalEnvelope(result, "req-list-roles"), nil), nil
	})

	client := newPortalTestClient(t, rt, 1)

	roles, err := client.ListAccountRoles(context.Background(), "access-token-xyz", "acc-1")
	if err != nil {
		t.Fatalf("ListAccountRoles: %v", err)
	}
	if len(roles) != 2 {
		t.Fatalf("got %d roles, want 2", len(roles))
	}
	if roles[0].RoleName != "Role 1" || roles[1].RoleName != "Role 2" {
		t.Fatalf("unexpected roles: %+v", roles)
	}

	// Verify account_id is encoded in the query string of every request.
	for i, req := range requests {
		q := req.URL.Query()
		if q.Get("account_id") != "acc-1" {
			t.Fatalf("request %d account_id = %q, want acc-1", i, q.Get("account_id"))
		}
		if req.URL.Path != ListRolesPath {
			t.Fatalf("request %d path = %q, want %q", i, req.URL.Path, ListRolesPath)
		}
	}

	// Verify that a special-character account ID is properly URL-encoded.
	t.Run("encodes special characters", func(t *testing.T) {
		var captured *http.Request
		rt2 := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			captured = req
			result, _ := json.Marshal(map[string]any{
				"Total":      0,
				"PageNumber": 1,
				"PageSize":   50,
				"RoleList":   []map[string]string{},
			})
			return newResponse(http.StatusOK, makePortalEnvelope(result, "req-enc"), nil), nil
		})
		c2 := newPortalTestClient(t, rt2, 1)
		_, err := c2.ListAccountRoles(context.Background(), "tok", "acct/with spaces&special=chars")
		if err != nil {
			t.Fatalf("ListAccountRoles: %v", err)
		}
		// The raw query should contain percent-encoding, not the raw special chars.
		raw := captured.URL.RawQuery
		if strings.Contains(raw, " ") || strings.Contains(raw, "&special=") {
			t.Fatalf("account_id not properly encoded in query: %q", raw)
		}
		// But the decoded value should round-trip.
		decoded, err := url.QueryUnescape(captured.URL.Query().Get("account_id"))
		if err != nil {
			t.Fatalf("unescape: %v", err)
		}
		if decoded != "acct/with spaces&special=chars" {
			t.Fatalf("decoded account_id = %q", decoded)
		}
	})
}

func TestPortalUsesBearerHeader(t *testing.T) {
	var captured *http.Request
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		captured = req
		result, _ := json.Marshal(map[string]any{
			"Total":       0,
			"PageNumber":  1,
			"PageSize":    50,
			"AccountList": []map[string]string{},
		})
		return newResponse(http.StatusOK, makePortalEnvelope(result, "req-bearer"), nil), nil
	})

	client := newPortalTestClient(t, rt, 1)

	_, err := client.ListAccounts(context.Background(), "my-secret-token")
	if err != nil {
		t.Fatalf("ListAccounts: %v", err)
	}

	if got := captured.Header.Get(BearerTokenHeader); got != "my-secret-token" {
		t.Fatalf("bearer header = %q, want my-secret-token", got)
	}
	if got := captured.Header.Get("Accept"); got != "application/json" {
		t.Fatalf("accept header = %q, want application/json", got)
	}
}

func TestPortalGetRoleCredentialsRequest(t *testing.T) {
	var captured *http.Request
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		captured = req
		result, _ := json.Marshal(map[string]any{
			"RoleCredentials": map[string]any{
				"AccessKeyId":     "AK-123",
				"SecretAccessKey": "SK-456",
				"sessionToken":    "ST-789",
				"Expiration":      1700000000,
			},
		})
		return newResponse(http.StatusOK, makePortalEnvelope(result, "req-creds"), nil), nil
	})

	client := newPortalTestClient(t, rt, 1)

	creds, err := client.GetRoleCredentials(context.Background(), "tok", "acc-1", "role-1")
	if err != nil {
		t.Fatalf("GetRoleCredentials: %v", err)
	}
	if creds.AccessKeyID != "AK-123" {
		t.Fatalf("access key = %q", creds.AccessKeyID)
	}
	if creds.SecretAccessKey != "SK-456" {
		t.Fatalf("secret key = %q", creds.SecretAccessKey)
	}
	if creds.SessionToken != "ST-789" {
		t.Fatalf("session token = %q", creds.SessionToken)
	}

	if captured.URL.Path != RoleCredentialsPath {
		t.Fatalf("path = %q, want %q", captured.URL.Path, RoleCredentialsPath)
	}
	q := captured.URL.Query()
	if q.Get("account_id") != "acc-1" {
		t.Fatalf("account_id = %q", q.Get("account_id"))
	}
	if q.Get("role_name") != "role-1" {
		t.Fatalf("role_name = %q", q.Get("role_name"))
	}
	if captured.Header.Get(BearerTokenHeader) != "tok" {
		t.Fatalf("bearer header missing or wrong")
	}
}

func TestPortalRejectsIncompleteRoleCredentials(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"missing AccessKeyId", `{"RoleCredentials":{"SecretAccessKey":"s","sessionToken":"t","Expiration":1}}`},
		{"missing SecretAccessKey", `{"RoleCredentials":{"AccessKeyId":"a","sessionToken":"t","Expiration":1}}`},
		{"missing sessionToken", `{"RoleCredentials":{"AccessKeyId":"a","SecretAccessKey":"s","Expiration":1}}`},
		{"missing Expiration", `{"RoleCredentials":{"AccessKeyId":"a","SecretAccessKey":"s","sessionToken":"t"}}`},
		{"zero Expiration", `{"RoleCredentials":{"AccessKeyId":"a","SecretAccessKey":"s","sessionToken":"t","Expiration":0}}`},
		{"empty RoleCredentials", `{"RoleCredentials":{}}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return newResponse(http.StatusOK, makePortalEnvelope(json.RawMessage(tc.body), "req-incomplete"), nil), nil
			})
			client := newPortalTestClient(t, rt, 1)
			_, err := client.GetRoleCredentials(context.Background(), "tok", "acc", "role")
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
		})
	}
}

func TestPortalExpirationSupportsSecondsAndMilliseconds(t *testing.T) {
	cases := []struct {
		name       string
		expiration int64
		want       time.Time
	}{
		{"seconds", 1700000000, time.Unix(1700000000, 0)},
		{"milliseconds", 1700000000000, time.UnixMilli(1700000000000)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			creds := RoleCredentials{
				AccessKeyID:     "a",
				SecretAccessKey: "s",
				SessionToken:    "t",
				Expiration:      tc.expiration,
			}
			got := creds.ExpirationTime()
			if !got.Equal(tc.want) {
				t.Fatalf("ExpirationTime() = %v, want %v", got, tc.want)
			}
		})
	}

	// Verify the full flow returns a normalized time.
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		result, _ := json.Marshal(map[string]any{
			"RoleCredentials": map[string]any{
				"AccessKeyId":     "a",
				"SecretAccessKey": "s",
				"sessionToken":    "t",
				"Expiration":      1700000000000, // milliseconds
			},
		})
		return newResponse(http.StatusOK, makePortalEnvelope(result, "req-ms"), nil), nil
	})
	client := newPortalTestClient(t, rt, 1)
	creds, err := client.GetRoleCredentials(context.Background(), "tok", "acc", "role")
	if err != nil {
		t.Fatalf("GetRoleCredentials: %v", err)
	}
	want := time.UnixMilli(1700000000000)
	if !creds.ExpirationTime().Equal(want) {
		t.Fatalf("ExpirationTime() = %v, want %v", creds.ExpirationTime(), want)
	}
}

func TestPortalErrorIncludesRequestIDButNotRawBody(t *testing.T) {
	const secretBody = `{"ResponseMetadata":{"RequestId":"req-err-123","Error":{"Code":"InternalError","Message":"something broke"}},"internal_secret":"SECRET_BODY_DO_NOT_LEAK"}`

	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return newResponse(http.StatusInternalServerError, secretBody,
			map[string]string{RequestIDHeader: "req-err-123"}), nil
	})

	client := newPortalTestClient(t, rt, 1)

	_, err := client.ListAccounts(context.Background(), "tok")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	text := err.Error()
	if !strings.Contains(text, "req-err-123") {
		t.Fatalf("error should expose request ID: %s", text)
	}
	if strings.Contains(text, "SECRET_BODY_DO_NOT_LEAK") {
		t.Fatalf("error leaked raw body: %s", text)
	}

	// Also test a 2xx response with an error in ResponseMetadata.
	t.Run("2xx with metadata error", func(t *testing.T) {
		const secretBody2 = `{"ResponseMetadata":{"RequestId":"req-meta-456","Error":{"Code":"InvalidParameter","Message":"bad param"}},"secret":"SECRET2"}`
		rt2 := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return newResponse(http.StatusOK, secretBody2, nil), nil
		})
		c2 := newPortalTestClient(t, rt2, 1)
		_, err := c2.ListAccounts(context.Background(), "tok")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		text := err.Error()
		if !strings.Contains(text, "req-meta-456") {
			t.Fatalf("error should expose request ID: %s", text)
		}
		if strings.Contains(text, "SECRET2") {
			t.Fatalf("error leaked raw body: %s", text)
		}
	})
}

func TestPortalRetriesRetryableResponses(t *testing.T) {
	cases := []struct {
		name        string
		status      int
		wantRetry   bool
		maxAttempts int
	}{
		{"429 retries", http.StatusTooManyRequests, true, 3},
		{"500 retries", http.StatusInternalServerError, true, 3},
		{"400 does not retry", http.StatusBadRequest, false, 3},
		{"404 does not retry", http.StatusNotFound, false, 3},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var attempts int32
			rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				atomic.AddInt32(&attempts, 1)
				return newResponse(tc.status, `{"ResponseMetadata":{"RequestId":"r"}}`, nil), nil
			})
			client := newPortalTestClient(t, rt, tc.maxAttempts)
			_, err := client.ListAccounts(context.Background(), "tok")
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			got := atomic.LoadInt32(&attempts)
			if tc.wantRetry {
				if got != int32(tc.maxAttempts) {
					t.Fatalf("attempts = %d, want %d", got, tc.maxAttempts)
				}
			} else {
				if got != 1 {
					t.Fatalf("attempts = %d, want 1", got)
				}
			}
		})
	}
}

func TestPortalNilReceiverAndContext(t *testing.T) {
	var nilClient *PortalClient

	if _, err := nilClient.ListAccounts(context.Background(), "tok"); err == nil {
		t.Fatal("expected error for nil client")
	}
	if _, err := nilClient.ListAccountRoles(context.Background(), "tok", "acc"); err == nil {
		t.Fatal("expected error for nil client")
	}
	if _, err := nilClient.GetRoleCredentials(context.Background(), "tok", "acc", "role"); err == nil {
		t.Fatal("expected error for nil client")
	}

	client := newPortalTestClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return newResponse(http.StatusOK, `{}`, nil), nil
	}), 1)

	if _, err := client.ListAccounts(nil, "tok"); err == nil {
		t.Fatal("expected error for nil context")
	}
	if _, err := client.GetRoleCredentials(nil, "tok", "acc", "role"); err == nil {
		t.Fatal("expected error for nil context")
	}
}

func TestPortalBaseURLValidation(t *testing.T) {
	cases := []struct {
		name    string
		baseURL string
		wantErr bool
	}{
		{"default https", "https://cloudidentity-portal.cn-beijing.volces.com", false},
		{"with trailing slash", "https://cloudidentity-portal.cn-beijing.volces.com/", false},
		{"http scheme", "http://cloudidentity-portal.cn-beijing.volces.com", true},
		{"with userinfo", "https://user:pass@cloudidentity-portal.cn-beijing.volces.com", true},
		{"with query", "https://cloudidentity-portal.cn-beijing.volces.com?foo=bar", true},
		{"with fragment", "https://cloudidentity-portal.cn-beijing.volces.com#frag", true},
		{"with path", "https://cloudidentity-portal.cn-beijing.volces.com/some/path", true},
		{"no host", "https://", true},
		{"empty uses default", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewPortalClient(&PortalClientConfig{BaseURL: tc.baseURL})
			if tc.wantErr && err == nil {
				t.Fatalf("expected error for %q, got nil", tc.baseURL)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.baseURL, err)
			}
		})
	}

	// Default region builds the correct URL.
	client, err := NewPortalClient(&PortalClientConfig{Region: "ap-southeast-1"})
	if err != nil {
		t.Fatalf("NewPortalClient: %v", err)
	}
	want := "https://cloudidentity-portal.ap-southeast-1.volces.com"
	if client.BaseURL() != want {
		t.Fatalf("base URL = %q, want %q", client.BaseURL(), want)
	}
}

func TestPortalMalformedJSONAndTransportError(t *testing.T) {
	t.Run("malformed JSON", func(t *testing.T) {
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return newResponse(http.StatusOK, `{not valid json`, nil), nil
		})
		client := newPortalTestClient(t, rt, 1)
		_, err := client.ListAccounts(context.Background(), "tok")
		if err == nil {
			t.Fatal("expected error for malformed JSON, got nil")
		}
	})

	t.Run("transport error", func(t *testing.T) {
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return nil, errors.New("dial tcp: connection refused")
		})
		client := newPortalTestClient(t, rt, 1)
		_, err := client.ListAccounts(context.Background(), "tok")
		if err == nil {
			t.Fatal("expected error for transport failure, got nil")
		}
	})
}

func TestPortalPaginationNoProgressProtection(t *testing.T) {
	// Server always returns PageNumber=1 regardless of the requested page,
	// which is a metadata mismatch that must be rejected rather than silently
	// looping forever.
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		result, _ := json.Marshal(map[string]any{
			"Total":       100,
			"PageNumber":  1,
			"PageSize":    2,
			"AccountList": []map[string]string{{"AccountId": "a1", "AccountName": "Account 1"}},
		})
		return newResponse(http.StatusOK, makePortalEnvelope(result, "req-loop"), nil), nil
	})

	client := newPortalTestClient(t, rt, 1)

	_, err := client.ListAccounts(context.Background(), "tok")
	if err == nil {
		t.Fatal("expected error for misbehaving pagination, got nil")
	}
	if !strings.Contains(err.Error(), "invalid pagination metadata") {
		t.Fatalf("error should mention invalid pagination metadata: %v", err)
	}
}

func TestPortalErrorsNeverLeakTokens(t *testing.T) {
	const (
		accessToken = "ACCESS_TOKEN_CANARY"
		secretKey   = "SECRET_KEY_CANARY"
		sessionTok  = "SESSION_TOKEN_CANARY"
		// Message canary: always discarded regardless of content.
		msgCanary = "MESSAGE_CANARY_SECRET"
		// RequestID canary: contains characters outside the safety allowlist so
		// it must be rejected, not stored or rendered.
		reqCanary = "REQID CANARY SECRET"
		safeReqID = "req-safe-123"
	)

	canaries := []string{accessToken, secretKey, sessionTok, msgCanary, reqCanary}

	// scanAll checks every required observation surface for any canary.
	scanAll := func(t *testing.T, err error) {
		t.Helper()
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		// Surface 1: Error()
		for _, c := range canaries {
			if strings.Contains(err.Error(), c) {
				t.Fatalf("Error() leaked canary %q: %s", c, err.Error())
			}
		}
		// Surface 2: %+v
		verbose := fmt.Sprintf("%+v", err)
		for _, c := range canaries {
			if strings.Contains(verbose, c) {
				t.Fatalf("%%+v leaked canary %q: %s", c, verbose)
			}
		}
		// Surface 3: JSON marshal
		jsonBytes, jerr := json.Marshal(err)
		if jerr != nil {
			t.Fatalf("json.Marshal: %v", jerr)
		}
		for _, c := range canaries {
			if strings.Contains(string(jsonBytes), c) {
				t.Fatalf("json.Marshal leaked canary %q: %s", c, string(jsonBytes))
			}
		}
		// Surface 4: exported fields
		var apiErr *PortalAPIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("expected *PortalAPIError, got %T", err)
		}
		fields := fmt.Sprintf("%d|%s", apiErr.StatusCode, apiErr.RequestID)
		for _, c := range canaries {
			if strings.Contains(fields, c) {
				t.Fatalf("exported field leaked canary %q: %s", c, fields)
			}
		}
	}

	// Case 1: non-2xx response with the access token echoed back as the error
	// Code (pure letters/underscore, no unsafe chars) and session/secret
	// canaries in the Message. All must be permanently discarded.
	t.Run("non-2xx metadata error", func(t *testing.T) {
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			errBody := `{"ResponseMetadata":{"RequestId":"` + reqCanary + `","Error":{"Code":"` + accessToken + `","Message":"` + sessionTok + " " + secretKey + " " + msgCanary + `"}}}`
			return newResponse(http.StatusBadRequest, errBody,
				map[string]string{RequestIDHeader: safeReqID}), nil
		})
		client := newPortalTestClient(t, rt, 1)
		_, err := client.GetRoleCredentials(context.Background(), accessToken, "acc", "role")
		scanAll(t, err)
	})

	// Case 2: 2xx response carrying an error in ResponseMetadata.
	t.Run("2xx with metadata error", func(t *testing.T) {
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			errBody := `{"ResponseMetadata":{"RequestId":"` + reqCanary + `","Error":{"Code":"` + accessToken + `","Message":"` + sessionTok + " " + secretKey + " " + msgCanary + `"}},"Result":{}}`
			return newResponse(http.StatusOK, errBody, nil), nil
		})
		client := newPortalTestClient(t, rt, 1)
		_, err := client.ListAccounts(context.Background(), accessToken)
		scanAll(t, err)
	})

	// Case 3: a safe RequestID must still be usable for diagnosis. Code and
	// Message are always discarded.
	t.Run("safe request id preserved", func(t *testing.T) {
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			errBody := `{"ResponseMetadata":{"RequestId":"` + safeReqID + `","Error":{"Code":"` + accessToken + `","Message":"` + msgCanary + `"}}}`
			return newResponse(http.StatusInternalServerError, errBody,
				map[string]string{RequestIDHeader: safeReqID}), nil
		})
		client := newPortalTestClient(t, rt, 1)
		_, err := client.ListAccounts(context.Background(), accessToken)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), safeReqID) {
			t.Fatalf("Error() should expose safe request ID: %s", err.Error())
		}
		var apiErr *PortalAPIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("expected *PortalAPIError, got %T", err)
		}
		if apiErr.RequestID != safeReqID {
			t.Fatalf("RequestID = %q, want %q", apiErr.RequestID, safeReqID)
		}
		// Code and message canaries must never appear.
		if strings.Contains(err.Error(), accessToken) {
			t.Fatalf("Error() leaked code canary: %s", err.Error())
		}
		if strings.Contains(err.Error(), msgCanary) {
			t.Fatalf("Error() leaked message canary: %s", err.Error())
		}
	})
}

func TestPortalNon2xxRequestIDFallback(t *testing.T) {
	const (
		accessToken = "ACCESS_TOKEN_CANARY"
		safeReqID   = "req-safe-123"
		unsafeReqID = "REQID CANARY SECRET"
	)

	// buildBody returns a non-2xx error body with the given RequestId and a
	// Code/Message that must never be surfaced.
	buildBody := func(reqID string) string {
		return `{"ResponseMetadata":{"RequestId":"` + reqID + `","Error":{"Code":"` + accessToken + `","Message":"MESSAGE_CANARY"}}}`
	}

	t.Run("body request id empty falls back to header", func(t *testing.T) {
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return newResponse(http.StatusBadRequest, buildBody(""),
				map[string]string{RequestIDHeader: safeReqID}), nil
		})
		client := newPortalTestClient(t, rt, 1)
		_, err := client.ListAccounts(context.Background(), accessToken)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var apiErr *PortalAPIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("expected *PortalAPIError, got %T", err)
		}
		if apiErr.RequestID != safeReqID {
			t.Fatalf("RequestID = %q, want %q (header fallback)", apiErr.RequestID, safeReqID)
		}
	})

	t.Run("body request id unsafe falls back to header", func(t *testing.T) {
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return newResponse(http.StatusBadRequest, buildBody(unsafeReqID),
				map[string]string{RequestIDHeader: safeReqID}), nil
		})
		client := newPortalTestClient(t, rt, 1)
		_, err := client.ListAccounts(context.Background(), accessToken)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var apiErr *PortalAPIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("expected *PortalAPIError, got %T", err)
		}
		if apiErr.RequestID != safeReqID {
			t.Fatalf("RequestID = %q, want %q (header fallback)", apiErr.RequestID, safeReqID)
		}
		if strings.Contains(err.Error(), unsafeReqID) {
			t.Fatalf("Error() leaked unsafe body request id: %s", err.Error())
		}
	})

	t.Run("header request id unsafe is discarded", func(t *testing.T) {
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return newResponse(http.StatusBadRequest, buildBody(""),
				map[string]string{RequestIDHeader: unsafeReqID}), nil
		})
		client := newPortalTestClient(t, rt, 1)
		_, err := client.ListAccounts(context.Background(), accessToken)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var apiErr *PortalAPIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("expected *PortalAPIError, got %T", err)
		}
		if apiErr.RequestID != "" {
			t.Fatalf("RequestID = %q, want empty (unsafe header discarded)", apiErr.RequestID)
		}
		if strings.Contains(err.Error(), unsafeReqID) {
			t.Fatalf("Error() leaked unsafe header request id: %s", err.Error())
		}
	})
}

func TestPortalHonorsContextCancellation(t *testing.T) {
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return newResponse(http.StatusTooManyRequests, `{"ResponseMetadata":{"RequestId":"r"}}`, nil), nil
	})

	client := newPortalTestClient(t, rt, 5)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.ListAccounts(ctx, "tok")
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestPortalResponseBodyLimited(t *testing.T) {
	// A response body larger than the default limit should not cause unbounded
	// memory growth; the httpx.RetryClient limits reads.
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		// Return a 200 with a huge body that is not valid JSON.
		big := strings.Repeat("x", 1024*128)
		return newResponse(http.StatusOK, big, nil), nil
	})

	client := newPortalTestClient(t, rt, 1)

	_, err := client.ListAccounts(context.Background(), "tok")
	if err == nil {
		t.Fatal("expected error for oversized/non-JSON body, got nil")
	}
	// The error must not contain the body content.
	if strings.Contains(err.Error(), "xxx") {
		t.Fatalf("error leaked body content: %s", err.Error())
	}
}

// Ensure the body is always closed even on error paths.
func TestPortalAlwaysClosesResponseBody(t *testing.T) {
	var closed int32
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		resp := newResponse(http.StatusOK, `{"ResponseMetadata":{"RequestId":"r"},"Result":{"Total":0,"PageNumber":1,"PageSize":50,"AccountList":[]}}`, nil)
		resp.Body = &countingReadCloser{ReadCloser: resp.Body, closed: &closed}
		return resp, nil
	})

	client := newPortalTestClient(t, rt, 1)

	_, err := client.ListAccounts(context.Background(), "tok")
	if err != nil {
		t.Fatalf("ListAccounts: %v", err)
	}
	if atomic.LoadInt32(&closed) != 1 {
		t.Fatalf("body closed %d times, want 1", closed)
	}
}

type countingReadCloser struct {
	io.ReadCloser
	closed *int32
}

func (c *countingReadCloser) Close() error {
	atomic.AddInt32(c.closed, 1)
	return c.ReadCloser.Close()
}

// TestPortalListAccountsPaginationLimitExceeded verifies that when the server
// keeps indicating more pages exist and the pagination bound is reached, the
// client returns a clear error instead of silently returning partial results.
func TestPortalListAccountsPaginationLimitExceeded(t *testing.T) {
	var requests int32
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&requests, 1)
		// Always report a huge total with a single item per page so there is
		// always a "next page". The page number echoes back the requested one.
		page, _ := strconv.Atoi(req.URL.Query().Get("page_number"))
		result, _ := json.Marshal(map[string]any{
			"Total":       1000000,
			"PageNumber":  page,
			"PageSize":    1,
			"AccountList": []map[string]string{{"AccountId": "a", "AccountName": "A"}},
		})
		return newResponse(http.StatusOK, makePortalEnvelope(result, "req-limit"), nil), nil
	})

	client := newPortalTestClient(t, rt, 1)

	accounts, err := client.ListAccounts(context.Background(), "tok")
	if err == nil {
		t.Fatalf("expected pagination limit error, got nil with %d accounts", len(accounts))
	}
	if !strings.Contains(err.Error(), "pagination limit exceeded") {
		t.Fatalf("error should mention pagination limit exceeded: %v", err)
	}
	// Must not return partial results that could be mistaken for a complete set.
	if accounts != nil {
		t.Fatalf("expected nil accounts on error, got %d", len(accounts))
	}
	// The error must not echo the token or any request value.
	if strings.Contains(err.Error(), "tok") {
		t.Fatalf("error leaked token: %s", err.Error())
	}
}

// TestPortalListRolesPaginationLimitExceeded verifies the same pagination limit
// behavior for ListAccountRoles.
func TestPortalListRolesPaginationLimitExceeded(t *testing.T) {
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		page, _ := strconv.Atoi(req.URL.Query().Get("page_number"))
		result, _ := json.Marshal(map[string]any{
			"Total":      1000000,
			"PageNumber": page,
			"PageSize":   1,
			"RoleList":   []map[string]string{{"AccountId": "acc", "RoleName": "R"}},
		})
		return newResponse(http.StatusOK, makePortalEnvelope(result, "req-limit"), nil), nil
	})

	client := newPortalTestClient(t, rt, 1)

	roles, err := client.ListAccountRoles(context.Background(), "tok", "acc-1")
	if err == nil {
		t.Fatalf("expected pagination limit error, got nil with %d roles", len(roles))
	}
	if !strings.Contains(err.Error(), "pagination limit exceeded") {
		t.Fatalf("error should mention pagination limit exceeded: %v", err)
	}
	if roles != nil {
		t.Fatalf("expected nil roles on error, got %d", len(roles))
	}
	if strings.Contains(err.Error(), "tok") || strings.Contains(err.Error(), "acc-1") {
		t.Fatalf("error leaked request values: %s", err.Error())
	}
}

// TestPortalListAccountsInvalidMetadata verifies that inconsistent pagination
// metadata is rejected with a fixed error message that does not echo response
// or request values.
func TestPortalListAccountsInvalidMetadata(t *testing.T) {
	cases := []struct {
		name   string
		result map[string]any
	}{
		{"negative total", map[string]any{"Total": -1, "PageNumber": 1, "PageSize": 50, "AccountList": []any{}}},
		{"zero page number", map[string]any{"Total": 0, "PageNumber": 0, "PageSize": 50, "AccountList": []any{}}},
		{"zero page size", map[string]any{"Total": 0, "PageNumber": 1, "PageSize": 0, "AccountList": []any{}}},
		{"mismatched page number", map[string]any{"Total": 0, "PageNumber": 99, "PageSize": 50, "AccountList": []any{}}},
		{"total with zero page size not treated as complete", map[string]any{"Total": 10, "PageNumber": 1, "PageSize": 0, "AccountList": []any{}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				result, _ := json.Marshal(tc.result)
				return newResponse(http.StatusOK, makePortalEnvelope(result, "req-meta"), nil), nil
			})
			client := newPortalTestClient(t, rt, 1)
			_, err := client.ListAccounts(context.Background(), "tok")
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
			if !strings.Contains(err.Error(), "invalid pagination metadata") {
				t.Fatalf("error should mention invalid pagination metadata: %v", err)
			}
			// Error text must not echo the token or the bad metadata values.
			if strings.Contains(err.Error(), "tok") {
				t.Fatalf("error leaked token: %s", err.Error())
			}
		})
	}
}

// TestPortalListRolesInvalidMetadata verifies the same metadata validation for
// ListAccountRoles.
func TestPortalListRolesInvalidMetadata(t *testing.T) {
	cases := []struct {
		name   string
		result map[string]any
	}{
		{"negative total", map[string]any{"Total": -1, "PageNumber": 1, "PageSize": 50, "RoleList": []any{}}},
		{"zero page number", map[string]any{"Total": 0, "PageNumber": 0, "PageSize": 50, "RoleList": []any{}}},
		{"zero page size", map[string]any{"Total": 0, "PageNumber": 1, "PageSize": 0, "RoleList": []any{}}},
		{"mismatched page number", map[string]any{"Total": 0, "PageNumber": 42, "PageSize": 50, "RoleList": []any{}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				result, _ := json.Marshal(tc.result)
				return newResponse(http.StatusOK, makePortalEnvelope(result, "req-meta"), nil), nil
			})
			client := newPortalTestClient(t, rt, 1)
			_, err := client.ListAccountRoles(context.Background(), "tok", "acc-1")
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
			if !strings.Contains(err.Error(), "invalid pagination metadata") {
				t.Fatalf("error should mention invalid pagination metadata: %v", err)
			}
			if strings.Contains(err.Error(), "tok") || strings.Contains(err.Error(), "acc-1") {
				t.Fatalf("error leaked request values: %s", err.Error())
			}
		})
	}
}

// TestPortalListAccountsToleratesCrossPageMetadataChanges verifies that the
// upstream Portal's lack of a snapshot/consistency token is honored: Total and
// PageSize may change across pages while the account set mutates, and the
// client must continue paginating using each page's own metadata, aggregate all
// returned items, and terminate only when the current page reports no next
// page. It must not reject such changes as a protocol error.
func TestPortalListAccountsToleratesCrossPageMetadataChanges(t *testing.T) {
	cases := []struct {
		name      string
		page1     map[string]any
		page2     map[string]any
		wantCount int
	}{
		{
			name: "total changes across pages",
			page1: map[string]any{
				"Total":       10,
				"PageNumber":  1,
				"PageSize":    2,
				"AccountList": []map[string]string{{"AccountId": "a1", "AccountName": "A1"}, {"AccountId": "a2", "AccountName": "A2"}},
			},
			page2: map[string]any{
				"Total":       3,
				"PageNumber":  2,
				"PageSize":    2,
				"AccountList": []map[string]string{{"AccountId": "a3", "AccountName": "A3"}, {"AccountId": "a4", "AccountName": "A4"}},
			},
			wantCount: 4,
		},
		{
			name: "page_size changes across pages",
			page1: map[string]any{
				"Total":       10,
				"PageNumber":  1,
				"PageSize":    1,
				"AccountList": []map[string]string{{"AccountId": "a1", "AccountName": "A1"}},
			},
			page2: map[string]any{
				"Total":       10,
				"PageNumber":  2,
				"PageSize":    5,
				"AccountList": []map[string]string{{"AccountId": "a2", "AccountName": "A2"}, {"AccountId": "a3", "AccountName": "A3"}},
			},
			wantCount: 3,
		},
		{
			name: "total shrinks and signals end on page 2",
			page1: map[string]any{
				"Total":       10,
				"PageNumber":  1,
				"PageSize":    2,
				"AccountList": []map[string]string{{"AccountId": "a1", "AccountName": "A1"}, {"AccountId": "a2", "AccountName": "A2"}},
			},
			page2: map[string]any{
				"Total":       1,
				"PageNumber":  2,
				"PageSize":    2,
				"AccountList": []map[string]string{},
			},
			wantCount: 2,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			page := 0
			rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				page++
				var result json.RawMessage
				if page == 1 {
					result, _ = json.Marshal(tc.page1)
				} else {
					result, _ = json.Marshal(tc.page2)
				}
				return newResponse(http.StatusOK, makePortalEnvelope(result, "req-mut"), nil), nil
			})
			client := newPortalTestClient(t, rt, 1)
			accounts, err := client.ListAccounts(context.Background(), "tok")
			if err != nil {
				t.Fatalf("ListAccounts: %v", err)
			}
			if len(accounts) != tc.wantCount {
				t.Fatalf("got %d accounts, want %d", len(accounts), tc.wantCount)
			}
		})
	}
}

// TestPortalListRolesToleratesCrossPageMetadataChanges verifies the same
// tolerance for ListAccountRoles: Total and PageSize may vary across pages and
// the client aggregates all roles, terminating when the current page reports no
// next page, without treating the change as a protocol error.
func TestPortalListRolesToleratesCrossPageMetadataChanges(t *testing.T) {
	page := 0
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		page++
		var result json.RawMessage
		if page == 1 {
			result, _ = json.Marshal(map[string]any{
				"Total":      3,
				"PageNumber": 1,
				"PageSize":   2,
				"RoleList":   []map[string]string{{"AccountId": "acc", "RoleName": "R1"}, {"AccountId": "acc", "RoleName": "R2"}},
			})
		} else {
			// Total shrinks and PageSize changes on page 2; page 2 is the last page.
			result, _ = json.Marshal(map[string]any{
				"Total":      2,
				"PageNumber": 2,
				"PageSize":   1,
				"RoleList":   []map[string]string{{"AccountId": "acc", "RoleName": "R3"}},
			})
		}
		return newResponse(http.StatusOK, makePortalEnvelope(result, "req-mut"), nil), nil
	})
	client := newPortalTestClient(t, rt, 1)
	roles, err := client.ListAccountRoles(context.Background(), "tok", "acc")
	if err != nil {
		t.Fatalf("ListAccountRoles: %v", err)
	}
	if len(roles) != 3 {
		t.Fatalf("got %d roles, want 3", len(roles))
	}
	if roles[0].RoleName != "R1" || roles[2].RoleName != "R3" {
		t.Fatalf("unexpected roles: %+v", roles)
	}
}

// TestComputeNextTokenNoOverflow verifies that computeNextToken does not
// overflow int when Total and PageSize are near math.MaxInt. The old
// implementation computed pageNumber*pageSize, which overflowed and produced
// wrong results; the new implementation uses an overflow-safe comparison.
func TestComputeNextTokenNoOverflow(t *testing.T) {
	// Total=MaxInt, PageSize=MaxInt-1.
	// Page 1: (MaxInt-1)/(MaxInt-1) = 1, 1 <= 1 -> next page exists.
	if got := computeNextToken(math.MaxInt, 1, math.MaxInt-1); got != "2" {
		t.Fatalf("computeNextToken(MaxInt, 1, MaxInt-1) = %q, want \"2\"", got)
	}
	// Page 2: (MaxInt-1)/(MaxInt-1) = 1, 2 <= 1 -> no next page.
	if got := computeNextToken(math.MaxInt, 2, math.MaxInt-1); got != "" {
		t.Fatalf("computeNextToken(MaxInt, 2, MaxInt-1) = %q, want empty (complete)", got)
	}
	// Sanity: a normal small case still works.
	if got := computeNextToken(3, 1, 2); got != "2" {
		t.Fatalf("computeNextToken(3, 1, 2) = %q, want \"2\"", got)
	}
	if got := computeNextToken(3, 2, 2); got != "" {
		t.Fatalf("computeNextToken(3, 2, 2) = %q, want empty", got)
	}
	// Zero/negative guards.
	if got := computeNextToken(0, 1, 1); got != "" {
		t.Fatalf("computeNextToken(0, 1, 1) = %q, want empty", got)
	}
	if got := computeNextToken(5, 0, 1); got != "" {
		t.Fatalf("computeNextToken(5, 0, 1) = %q, want empty", got)
	}
}

// TestPortalListAccountsOverflowTerminates proves the integration path does not
// advance to the pagination limit when Total/PageSize are near MaxInt. With the
// overflow-safe computeNextToken, page 2 is correctly identified as the final
// page and the client returns the aggregated results instead of looping 1000
// times.
func TestPortalListAccountsOverflowTerminates(t *testing.T) {
	var requests int32
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		n := atomic.AddInt32(&requests, 1)
		list := []map[string]string{{"AccountId": fmt.Sprintf("a%d", n), "AccountName": "A"}}
		result, _ := json.Marshal(map[string]any{
			"Total":       math.MaxInt,
			"PageNumber":  int(n),
			"PageSize":    math.MaxInt - 1,
			"AccountList": list,
		})
		return newResponse(http.StatusOK, makePortalEnvelope(result, "req-overflow"), nil), nil
	})
	client := newPortalTestClient(t, rt, 1)
	accounts, err := client.ListAccounts(context.Background(), "tok")
	if err != nil {
		t.Fatalf("ListAccounts: %v", err)
	}
	if got := atomic.LoadInt32(&requests); got != 2 {
		t.Fatalf("made %d requests, want 2 (overflow-safe termination)", got)
	}
	if len(accounts) != 2 {
		t.Fatalf("got %d accounts, want 2", len(accounts))
	}
}

// TestPortalNon2xxOversizedBodyReturnsPortalAPIError verifies that when a
// non-2xx response body cannot be fully read (e.g. it exceeds the size limit),
// the client still returns a classifiable *PortalAPIError carrying only the
// HTTP status and the safety-validated header request ID. The body content,
// the read error, and any server text must never leak.
func TestPortalNon2xxOversizedBodyReturnsPortalAPIError(t *testing.T) {
	const (
		bodyCanary = "OVERSIZED_BODY_CANARY_SECRET"
		safeReqID  = "req-safe-456"
	)
	cases := []struct {
		name   string
		status int
	}{
		{"oversized 400", http.StatusBadRequest},
		{"oversized 500", http.StatusInternalServerError},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				// Body larger than the default 64KB limit so io.ReadAll on the
				// size-limited body returns ErrBodyTooLarge.
				big := strings.Repeat("x", 1024*128) + bodyCanary
				return newResponse(tc.status, big,
					map[string]string{RequestIDHeader: safeReqID}), nil
			})
			client := newPortalTestClient(t, rt, 1)
			_, err := client.ListAccounts(context.Background(), "tok")
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			// Must be a *PortalAPIError with correct status and request ID.
			var apiErr *PortalAPIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("expected *PortalAPIError, got %T", err)
			}
			if apiErr.StatusCode != tc.status {
				t.Fatalf("StatusCode = %d, want %d", apiErr.StatusCode, tc.status)
			}
			if apiErr.RequestID != safeReqID {
				t.Fatalf("RequestID = %q, want %q", apiErr.RequestID, safeReqID)
			}

			// No surface may leak the body canary.
			for _, c := range []string{bodyCanary, "xxx"} {
				if strings.Contains(err.Error(), c) {
					t.Fatalf("Error() leaked canary %q: %s", c, err.Error())
				}
			}
			verbose := fmt.Sprintf("%+v", err)
			if strings.Contains(verbose, bodyCanary) {
				t.Fatalf("%%+v leaked canary: %s", verbose)
			}
			jsonBytes, jerr := json.Marshal(err)
			if jerr != nil {
				t.Fatalf("json.Marshal: %v", jerr)
			}
			if strings.Contains(string(jsonBytes), bodyCanary) {
				t.Fatalf("json.Marshal leaked canary: %s", string(jsonBytes))
			}
			// The read error (ErrBodyTooLarge) must not be wrapped.
			if errors.Is(err, httpx.ErrBodyTooLarge) {
				t.Fatalf("error wraps ErrBodyTooLarge: %v", err)
			}
		})
	}
}

// TestPortal2xxMetadataErrorRequestIDFallback verifies that for a 2xx response
// carrying an error in ResponseMetadata, the body RequestID is preferred but
// falls back to the safe header RequestID when empty or unsafe, and is
// discarded when both are unsafe.
func TestPortal2xxMetadataErrorRequestIDFallback(t *testing.T) {
	const (
		accessToken = "ACCESS_TOKEN_CANARY"
		safeReqID   = "req-safe-789"
		unsafeReqID = "REQID CANARY SECRET"
	)

	t.Run("body request id empty falls back to header", func(t *testing.T) {
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body := `{"ResponseMetadata":{"RequestId":"","Error":{"Code":"X","Message":"M"}},"Result":{}}`
			return newResponse(http.StatusOK, body,
				map[string]string{RequestIDHeader: safeReqID}), nil
		})
		client := newPortalTestClient(t, rt, 1)
		_, err := client.ListAccounts(context.Background(), accessToken)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var apiErr *PortalAPIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("expected *PortalAPIError, got %T", err)
		}
		if apiErr.RequestID != safeReqID {
			t.Fatalf("RequestID = %q, want %q (header fallback)", apiErr.RequestID, safeReqID)
		}
	})

	t.Run("body request id unsafe falls back to header", func(t *testing.T) {
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body := `{"ResponseMetadata":{"RequestId":"` + unsafeReqID + `","Error":{"Code":"X","Message":"M"}},"Result":{}}`
			return newResponse(http.StatusOK, body,
				map[string]string{RequestIDHeader: safeReqID}), nil
		})
		client := newPortalTestClient(t, rt, 1)
		_, err := client.ListAccounts(context.Background(), accessToken)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var apiErr *PortalAPIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("expected *PortalAPIError, got %T", err)
		}
		if apiErr.RequestID != safeReqID {
			t.Fatalf("RequestID = %q, want %q (header fallback)", apiErr.RequestID, safeReqID)
		}
		if strings.Contains(err.Error(), unsafeReqID) {
			t.Fatalf("Error() leaked unsafe body request id: %s", err.Error())
		}
	})

	t.Run("header request id unsafe is discarded", func(t *testing.T) {
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body := `{"ResponseMetadata":{"RequestId":"","Error":{"Code":"X","Message":"M"}},"Result":{}}`
			return newResponse(http.StatusOK, body,
				map[string]string{RequestIDHeader: unsafeReqID}), nil
		})
		client := newPortalTestClient(t, rt, 1)
		_, err := client.ListAccounts(context.Background(), accessToken)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var apiErr *PortalAPIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("expected *PortalAPIError, got %T", err)
		}
		if apiErr.RequestID != "" {
			t.Fatalf("RequestID = %q, want empty (unsafe header discarded)", apiErr.RequestID)
		}
		if strings.Contains(err.Error(), unsafeReqID) {
			t.Fatalf("Error() leaked unsafe header request id: %s", err.Error())
		}
	})
}
