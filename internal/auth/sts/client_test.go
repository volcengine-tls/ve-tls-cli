package sts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth"
	"github.com/volcengine/volc-sdk-golang/base"
)

// fixedClock returns a deterministic time for ExpiresAt calculations.
func fixedClock() time.Time {
	return time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
}

// fixedUUID returns a deterministic role session name.
func fixedUUID() string { return "00000000-0000-0000-0000-000000000000" }

// recordingTransport is a fake http.RoundTripper that records every request and
// returns a canned response (or error) from a queue. It is safe for concurrent
// use because the client serializes attempts.
type recordingTransport struct {
	requests  []*http.Request
	responses []http.Response
	errs      []error
	mu        chan struct{} // binary semaphore protecting slices
}

func newRecordingTransport() *recordingTransport {
	return &recordingTransport{mu: make(chan struct{}, 1)}
}

func (t *recordingTransport) lock()   { t.mu <- struct{}{} }
func (t *recordingTransport) unlock() { <-t.mu }

func (t *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.lock()
	defer t.unlock()
	// Clone the request body so the test can inspect it after the client closes it.
	if req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		req.Body = io.NopCloser(bytes.NewReader(b))
		// Store a copy on the request for later inspection.
		req = req.Clone(req.Context())
		req.Body = io.NopCloser(bytes.NewReader(b))
	} else {
		req = req.Clone(req.Context())
	}
	t.requests = append(t.requests, req)

	idx := len(t.requests) - 1
	if idx < len(t.errs) && t.errs[idx] != nil {
		return nil, t.errs[idx]
	}
	if idx < len(t.responses) {
		resp := t.responses[idx]
		// Copy the body so each call gets an independent reader.
		if resp.Body != nil {
			b, _ := io.ReadAll(resp.Body)
			resp.Body = io.NopCloser(bytes.NewReader(b))
		}
		return &resp, nil
	}
	// Default: empty 200 response.
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("")),
	}, nil
}

func (t *recordingTransport) requestCount() int {
	t.lock()
	defer t.unlock()
	return len(t.requests)
}

func (t *recordingTransport) lastRequest() *http.Request {
	t.lock()
	defer t.unlock()
	if len(t.requests) == 0 {
		return nil
	}
	return t.requests[len(t.requests)-1]
}

func (t *recordingTransport) allRequests() []*http.Request {
	t.lock()
	defer t.unlock()
	out := make([]*http.Request, len(t.requests))
	copy(out, t.requests)
	return out
}

// stsRAMResponse builds a minimal successful AssumeRole JSON response using the
// ExpiredTime field (RAM contract).
func stsRAMResponse(ak, sk, token, expiredTime string) []byte {
	resp := map[string]interface{}{
		"ResponseMetadata": map[string]interface{}{
			"RequestId": "req-123",
		},
		"Result": map[string]interface{}{
			"Credentials": map[string]interface{}{
				"AccessKeyId":     ak,
				"SecretAccessKey": sk,
				"SessionToken":    token,
				"ExpiredTime":     expiredTime,
			},
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

// stsOIDCResponse builds a minimal successful AssumeRoleWithOIDC JSON response
// using the Expiration field (OIDC contract).
func stsOIDCResponse(ak, sk, token, expiration string) []byte {
	resp := map[string]interface{}{
		"ResponseMetadata": map[string]interface{}{
			"RequestId": "req-123",
		},
		"Result": map[string]interface{}{
			"Credentials": map[string]interface{}{
				"AccessKeyId":     ak,
				"SecretAccessKey": sk,
				"SessionToken":    token,
				"Expiration":      expiration,
			},
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

// stsErrorResponse builds an STS JSON response carrying a service error code.
func stsErrorResponse(code, message string) []byte {
	resp := map[string]interface{}{
		"ResponseMetadata": map[string]interface{}{
			"RequestId": "req-err",
			"Error": map[string]interface{}{
				"Code":    code,
				"Message": message,
			},
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

func newTestClient(rt http.RoundTripper) *Client {
	return &Client{
		endpoint:   "sts.volcengineapi.com",
		httpClient: &http.Client{Transport: rt},
		signer:     func(c base.Credentials, r *http.Request) *http.Request { return c.Sign(r) },
		clock:      fixedClock,
		uuid:       fixedUUID,
		sleeper:    func(context.Context, time.Duration) error { return nil },
	}
}

// ---------------------------------------------------------------------------
// RAM AssumeRole
// ---------------------------------------------------------------------------

func TestAssumeRoleBuildsSignedGETQuery(t *testing.T) {
	rt := newRecordingTransport()
	rt.responses = []http.Response{{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(stsRAMResponse("AK", "SK", "TOKEN", "2026-07-25T13:00:00Z"))),
	}}
	c := newTestClient(rt)

	creds, err := c.AssumeRole(context.Background(), AssumeRoleInput{
		Source:    SourceCredential{AccessKeyID: "AK", SecretAccessKey: "SK"},
		AccountID: "2100000000",
		RoleName:  "my-role",
		Region:    "cn-beijing",
	})
	if err != nil {
		t.Fatalf("AssumeRole returned error: %v", err)
	}
	if creds.AccessKeyID != "AK" || creds.SecretAccessKey != "SK" || creds.SessionToken != "TOKEN" {
		t.Fatalf("unexpected credentials: %+v", creds)
	}

	req := rt.lastRequest()
	if req == nil {
		t.Fatal("no request was sent")
	}
	if req.Method != http.MethodGet {
		t.Fatalf("method=%s, want GET", req.Method)
	}
	q := req.URL.Query()
	for key, want := range map[string]string{
		"Action":          "AssumeRole",
		"Version":         "2018-01-01",
		"DurationSeconds": "3600",
		"RoleTrn":         "trn:iam::2100000000:role/my-role",
		"RoleSessionName": fixedUUID(),
	} {
		if got := q.Get(key); got != want {
			t.Fatalf("query %s=%q, want %q", key, got, want)
		}
	}
	if req.URL.Host != "sts.volcengineapi.com" {
		t.Fatalf("host=%q, want sts.volcengineapi.com", req.URL.Host)
	}
	if req.URL.Scheme != "https" {
		t.Fatalf("scheme=%q, want https", req.URL.Scheme)
	}
	if req.Header.Get("Authorization") == "" {
		t.Fatal("expected Authorization header to be set by signer")
	}
}

func TestAssumeRoleUsesSourceSessionToken(t *testing.T) {
	rt := newRecordingTransport()
	rt.responses = []http.Response{{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(stsRAMResponse("AK", "SK", "TOK", "2026-07-25T13:00:00Z"))),
	}}
	c := newTestClient(rt)

	if _, err := c.AssumeRole(context.Background(), AssumeRoleInput{
		Source:    SourceCredential{AccessKeyID: "AK", SecretAccessKey: "SK", SessionToken: "SOURCE-SESSION-TOKEN"},
		Region:    "cn-beijing",
		RoleName:  "r",
		AccountID: "123456789012",
	}); err != nil {
		t.Fatalf("AssumeRole error: %v", err)
	}

	req := rt.lastRequest()
	if got := req.Header.Get("X-Security-Token"); got != "SOURCE-SESSION-TOKEN" {
		t.Fatalf("X-Security-Token=%q, want SOURCE-SESSION-TOKEN", got)
	}
}

func TestAssumeRoleUsesProfileSigningRegion(t *testing.T) {
	rt := newRecordingTransport()
	rt.responses = []http.Response{{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(stsRAMResponse("AK", "SK", "T", "2026-07-25T13:00:00Z"))),
	}}
	c := newTestClient(rt)

	if _, err := c.AssumeRole(context.Background(), AssumeRoleInput{
		Source:    SourceCredential{AccessKeyID: "AK", SecretAccessKey: "SK"},
		Region:    "ap-southeast-1",
		RoleName:  "r",
		AccountID: "123456789012",
	}); err != nil {
		t.Fatalf("AssumeRole error: %v", err)
	}

	auth := rt.lastRequest().Header.Get("Authorization")
	if !strings.Contains(auth, "/ap-southeast-1/sts/request") {
		t.Fatalf("Authorization=%q, want scope containing /ap-southeast-1/sts/request", auth)
	}
}

func TestAssumeRoleDefaultsSigningRegionToBeijing(t *testing.T) {
	rt := newRecordingTransport()
	rt.responses = []http.Response{{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(stsRAMResponse("AK", "SK", "T", "2026-07-25T13:00:00Z"))),
	}}
	c := newTestClient(rt)

	if _, err := c.AssumeRole(context.Background(), AssumeRoleInput{
		Source:    SourceCredential{AccessKeyID: "AK", SecretAccessKey: "SK"},
		Region:    "",
		RoleName:  "r",
		AccountID: "123456789012",
	}); err != nil {
		t.Fatalf("AssumeRole error: %v", err)
	}

	auth := rt.lastRequest().Header.Get("Authorization")
	if !strings.Contains(auth, "/cn-beijing/sts/request") {
		t.Fatalf("Authorization=%q, want scope containing /cn-beijing/sts/request", auth)
	}
}

// ---------------------------------------------------------------------------
// OIDC AssumeRoleWithOIDC
// ---------------------------------------------------------------------------

func TestAssumeRoleWithOIDCBuildsUnsignedFormPOST(t *testing.T) {
	rt := newRecordingTransport()
	rt.responses = []http.Response{{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body: io.NopCloser(bytes.NewReader(stsOIDCResponse(
			"AK", "SK", "TOKEN", "2026-07-25T13:00:00Z",
		))),
	}}
	c := newTestClient(rt)

	rawToken := []byte("header.payload.signature\n")
	if _, err := c.AssumeRoleWithOIDC(context.Background(), OIDCInput{
		Token:   rawToken,
		RoleTRN: "trn:iam::2100000000:role/oidc-role",
	}); err != nil {
		t.Fatalf("AssumeRoleWithOIDC error: %v", err)
	}

	req := rt.lastRequest()
	if req == nil {
		t.Fatal("no request was sent")
	}
	if req.Method != http.MethodPost {
		t.Fatalf("method=%s, want POST", req.Method)
	}
	if got := req.URL.Query().Get("Action"); got != "AssumeRoleWithOIDC" {
		t.Fatalf("Action=%q, want AssumeRoleWithOIDC", got)
	}
	if got := req.URL.Query().Get("Version"); got != "2018-01-01" {
		t.Fatalf("Version=%q, want 2018-01-01", got)
	}
	if got := req.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
		t.Fatalf("Content-Type=%q, want application/x-www-form-urlencoded", got)
	}
	if req.Header.Get("Authorization") != "" {
		t.Fatalf("OIDC request must be unsigned, got Authorization=%q", req.Header.Get("Authorization"))
	}

	body, _ := io.ReadAll(req.Body)
	form, err := url.ParseQuery(string(body))
	if err != nil {
		t.Fatalf("parse form body: %v", err)
	}
	if got := form.Get("RoleTrn"); got != "trn:iam::2100000000:role/oidc-role" {
		t.Fatalf("RoleTrn=%q, want user value", got)
	}
	if got := form.Get("OIDCToken"); got != string(rawToken) {
		t.Fatalf("OIDCToken=%q, want raw token bytes including newline", got)
	}
	if got := form.Get("RoleSessionName"); got != "volcengine-go-sdk-oidc-session" {
		t.Fatalf("RoleSessionName=%q, want volcengine-go-sdk-oidc-session", got)
	}
	if got := form.Get("DurationSeconds"); got != "3660" {
		t.Fatalf("DurationSeconds=%q, want 3660", got)
	}
}

// ---------------------------------------------------------------------------
// Scheme / DisableSSL
// ---------------------------------------------------------------------------

func TestSTSUsesHTTPSUnlessDisableSSL(t *testing.T) {
	tests := []struct {
		name       string
		disableSSL bool
		wantScheme string
	}{
		{"default https", false, "https"},
		{"disable ssl http", true, "http"},
	}
	for _, tt := range tests {
		t.Run(tt.name+"/ram", func(t *testing.T) {
			rt := newRecordingTransport()
			rt.responses = []http.Response{{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(bytes.NewReader(stsRAMResponse("A", "B", "C", "2026-07-25T13:00:00Z"))),
			}}
			c := newTestClient(rt)
			if _, err := c.AssumeRole(context.Background(), AssumeRoleInput{
				Source:     SourceCredential{AccessKeyID: "A", SecretAccessKey: "B"},
				AccountID:  "123456789012",
				Region:     "cn-beijing",
				RoleName:   "r",
				DisableSSL: tt.disableSSL,
			}); err != nil {
				t.Fatalf("AssumeRole error: %v", err)
			}
			if got := rt.lastRequest().URL.Scheme; got != tt.wantScheme {
				t.Fatalf("scheme=%q, want %q", got, tt.wantScheme)
			}
			if got := rt.lastRequest().URL.Host; got != "sts.volcengineapi.com" {
				t.Fatalf("host=%q, want sts.volcengineapi.com", got)
			}
		})
		t.Run(tt.name+"/oidc", func(t *testing.T) {
			rt := newRecordingTransport()
			rt.responses = []http.Response{{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(bytes.NewReader(stsOIDCResponse("A", "B", "C", "2026-07-25T13:00:00Z"))),
			}}
			c := newTestClient(rt)
			if _, err := c.AssumeRoleWithOIDC(context.Background(), OIDCInput{
				Token:      []byte("tok"),
				RoleTRN:    "trn:iam::1:role/r",
				DisableSSL: tt.disableSSL,
			}); err != nil {
				t.Fatalf("AssumeRoleWithOIDC error: %v", err)
			}
			if got := rt.lastRequest().URL.Scheme; got != tt.wantScheme {
				t.Fatalf("scheme=%q, want %q", got, tt.wantScheme)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Parsing and validation
// ---------------------------------------------------------------------------

func TestSTSParsesAndValidatesCredentialResponses(t *testing.T) {
	t.Run("ram hard expires at min of request+1h and server time", func(t *testing.T) {
		rt := newRecordingTransport()
		// Server returns 2h in the future; client must clamp to request start + 1h.
		rt.responses = []http.Response{{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(stsRAMResponse("A", "B", "C", "2026-07-25T14:00:00Z"))),
		}}
		c := newTestClient(rt)
		creds, err := c.AssumeRole(context.Background(), AssumeRoleInput{
			Source:    SourceCredential{AccessKeyID: "A", SecretAccessKey: "B"},
			Region:    "cn-beijing",
			RoleName:  "r",
			AccountID: "123456789012",
		})
		if err != nil {
			t.Fatalf("AssumeRole error: %v", err)
		}
		want := fixedClock().Add(time.Hour)
		if !creds.ExpiresAt.Equal(want) {
			t.Fatalf("ExpiresAt=%v, want %v (request start + 1h)", creds.ExpiresAt, want)
		}
	})

	t.Run("ram uses server time when earlier", func(t *testing.T) {
		rt := newRecordingTransport()
		// Server returns 30m in the future; client must use that.
		rt.responses = []http.Response{{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(stsRAMResponse("A", "B", "C", "2026-07-25T12:30:00Z"))),
		}}
		c := newTestClient(rt)
		creds, err := c.AssumeRole(context.Background(), AssumeRoleInput{
			Source:    SourceCredential{AccessKeyID: "A", SecretAccessKey: "B"},
			Region:    "cn-beijing",
			RoleName:  "r",
			AccountID: "123456789012",
		})
		if err != nil {
			t.Fatalf("AssumeRole error: %v", err)
		}
		want := time.Date(2026, 7, 25, 12, 30, 0, 0, time.UTC)
		if !creds.ExpiresAt.Equal(want) {
			t.Fatalf("ExpiresAt=%v, want %v (server time)", creds.ExpiresAt, want)
		}
	})

	t.Run("oidc hard expires at server expiration", func(t *testing.T) {
		rt := newRecordingTransport()
		rt.responses = []http.Response{{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(stsOIDCResponse("A", "B", "C", "2026-07-25T13:00:00Z"))),
		}}
		c := newTestClient(rt)
		creds, err := c.AssumeRoleWithOIDC(context.Background(), OIDCInput{
			Token:   []byte("tok"),
			RoleTRN: "trn:iam::1:role/r",
		})
		if err != nil {
			t.Fatalf("AssumeRoleWithOIDC error: %v", err)
		}
		want := time.Date(2026, 7, 25, 13, 0, 0, 0, time.UTC)
		if !creds.ExpiresAt.Equal(want) {
			t.Fatalf("ExpiresAt=%v, want %v (server expiration)", creds.ExpiresAt, want)
		}
	})

	t.Run("missing expired time fails closed", func(t *testing.T) {
		rt := newRecordingTransport()
		rt.responses = []http.Response{{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(stsRAMResponse("A", "B", "C", ""))),
		}}
		c := newTestClient(rt)
		if _, err := c.AssumeRole(context.Background(), AssumeRoleInput{
			Source:    SourceCredential{AccessKeyID: "A", SecretAccessKey: "B"},
			Region:    "cn-beijing",
			RoleName:  "r",
			AccountID: "123456789012",
		}); err == nil {
			t.Fatal("expected error for missing ExpiredTime, got nil")
		}
	})

	t.Run("empty ak fails closed", func(t *testing.T) {
		rt := newRecordingTransport()
		rt.responses = []http.Response{{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(stsRAMResponse("", "B", "C", "2026-07-25T13:00:00Z"))),
		}}
		c := newTestClient(rt)
		if _, err := c.AssumeRole(context.Background(), AssumeRoleInput{
			Source:    SourceCredential{AccessKeyID: "A", SecretAccessKey: "B"},
			Region:    "cn-beijing",
			RoleName:  "r",
			AccountID: "123456789012",
		}); err == nil {
			t.Fatal("expected error for empty AccessKeyId in response")
		}
	})

	t.Run("empty sk fails closed", func(t *testing.T) {
		rt := newRecordingTransport()
		rt.responses = []http.Response{{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(stsRAMResponse("A", "", "C", "2026-07-25T13:00:00Z"))),
		}}
		c := newTestClient(rt)
		if _, err := c.AssumeRole(context.Background(), AssumeRoleInput{
			Source:    SourceCredential{AccessKeyID: "A", SecretAccessKey: "B"},
			Region:    "cn-beijing",
			RoleName:  "r",
			AccountID: "123456789012",
		}); err == nil {
			t.Fatal("expected error for empty SecretAccessKey in response")
		}
	})

	t.Run("empty session token fails closed", func(t *testing.T) {
		rt := newRecordingTransport()
		rt.responses = []http.Response{{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(stsRAMResponse("A", "B", "", "2026-07-25T13:00:00Z"))),
		}}
		c := newTestClient(rt)
		if _, err := c.AssumeRole(context.Background(), AssumeRoleInput{
			Source:    SourceCredential{AccessKeyID: "A", SecretAccessKey: "B"},
			Region:    "cn-beijing",
			RoleName:  "r",
			AccountID: "123456789012",
		}); err == nil {
			t.Fatal("expected error for empty SessionToken in response")
		}
	})

	t.Run("malformed expired time fails closed", func(t *testing.T) {
		rt := newRecordingTransport()
		rt.responses = []http.Response{{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(stsRAMResponse("A", "B", "C", "not-a-time"))),
		}}
		c := newTestClient(rt)
		if _, err := c.AssumeRole(context.Background(), AssumeRoleInput{
			Source:    SourceCredential{AccessKeyID: "A", SecretAccessKey: "B"},
			Region:    "cn-beijing",
			RoleName:  "r",
			AccountID: "123456789012",
		}); err == nil {
			t.Fatal("expected error for malformed ExpiredTime")
		}
	})

	t.Run("already expired fails closed", func(t *testing.T) {
		rt := newRecordingTransport()
		rt.responses = []http.Response{{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(stsRAMResponse("A", "B", "C", "2020-01-01T00:00:00Z"))),
		}}
		c := newTestClient(rt)
		if _, err := c.AssumeRole(context.Background(), AssumeRoleInput{
			Source:    SourceCredential{AccessKeyID: "A", SecretAccessKey: "B"},
			Region:    "cn-beijing",
			RoleName:  "r",
			AccountID: "123456789012",
		}); err == nil {
			t.Fatal("expected error for already-expired credential")
		}
	})

	t.Run("nil context fails closed", func(t *testing.T) {
		rt := newRecordingTransport()
		c := newTestClient(rt)
		//lint:ignore SA1012 verifies AssumeRole fails closed for a nil context
		if _, err := c.AssumeRole(nil, AssumeRoleInput{
			Source:    SourceCredential{AccessKeyID: "A", SecretAccessKey: "B"},
			Region:    "cn-beijing",
			RoleName:  "r",
			AccountID: "123456789012",
		}); err == nil {
			t.Fatal("expected error for nil context")
		}
	})

	t.Run("missing source ak fails closed", func(t *testing.T) {
		rt := newRecordingTransport()
		c := newTestClient(rt)
		if _, err := c.AssumeRole(context.Background(), AssumeRoleInput{
			Source:    SourceCredential{SecretAccessKey: "B"},
			Region:    "cn-beijing",
			RoleName:  "r",
			AccountID: "123456789012",
		}); err == nil {
			t.Fatal("expected error for missing source AccessKeyID")
		}
	})

	t.Run("missing role name fails closed", func(t *testing.T) {
		rt := newRecordingTransport()
		c := newTestClient(rt)
		if _, err := c.AssumeRole(context.Background(), AssumeRoleInput{
			Source:   SourceCredential{AccessKeyID: "A", SecretAccessKey: "B"},
			Region:   "cn-beijing",
			RoleName: "",
		}); err == nil {
			t.Fatal("expected error for missing RoleName")
		}
	})

	t.Run("missing oidc token fails closed", func(t *testing.T) {
		rt := newRecordingTransport()
		c := newTestClient(rt)
		if _, err := c.AssumeRoleWithOIDC(context.Background(), OIDCInput{
			Token:   nil,
			RoleTRN: "trn:iam::1:role/r",
		}); err == nil {
			t.Fatal("expected error for nil OIDC token")
		}
	})

	t.Run("missing oidc role trn fails closed", func(t *testing.T) {
		rt := newRecordingTransport()
		c := newTestClient(rt)
		if _, err := c.AssumeRoleWithOIDC(context.Background(), OIDCInput{
			Token:   []byte("tok"),
			RoleTRN: "",
		}); err == nil {
			t.Fatal("expected error for empty RoleTRN")
		}
	})
}

// ---------------------------------------------------------------------------
// Retry classification
// ---------------------------------------------------------------------------

func TestSTSClassifiesRetryableAndTerminalFailures(t *testing.T) {
	t.Run("http 429 retries four times", func(t *testing.T) {
		rt := newRecordingTransport()
		for i := 0; i < 4; i++ {
			rt.responses = append(rt.responses, http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     make(http.Header),
				Body:       io.NopCloser(bytes.NewReader(stsErrorResponse("Throttling", "slow down"))),
			})
		}
		c := newTestClient(rt)
		_, err := c.AssumeRole(context.Background(), AssumeRoleInput{
			Source:    SourceCredential{AccessKeyID: "A", SecretAccessKey: "B"},
			Region:    "cn-beijing",
			RoleName:  "r",
			AccountID: "123456789012",
		})
		if err == nil {
			t.Fatal("expected error after exhausting retries")
		}
		if got := rt.requestCount(); got != 4 {
			t.Fatalf("request count=%d, want 4", got)
		}
	})

	t.Run("throttle service code retries four times", func(t *testing.T) {
		rt := newRecordingTransport()
		for i := 0; i < 4; i++ {
			rt.responses = append(rt.responses, http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(bytes.NewReader(stsErrorResponse("Throttling", "slow"))),
			})
		}
		c := newTestClient(rt)
		_, err := c.AssumeRole(context.Background(), AssumeRoleInput{
			Source:    SourceCredential{AccessKeyID: "A", SecretAccessKey: "B"},
			Region:    "cn-beijing",
			RoleName:  "r",
			AccountID: "123456789012",
		})
		if err == nil {
			t.Fatal("expected error after exhausting retries")
		}
		if got := rt.requestCount(); got != 4 {
			t.Fatalf("request count=%d, want 4", got)
		}
	})

	t.Run("http 400 is terminal", func(t *testing.T) {
		rt := newRecordingTransport()
		rt.responses = []http.Response{{
			StatusCode: http.StatusBadRequest,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(stsErrorResponse("InvalidParameter", "bad"))),
		}}
		c := newTestClient(rt)
		_, err := c.AssumeRole(context.Background(), AssumeRoleInput{
			Source:    SourceCredential{AccessKeyID: "A", SecretAccessKey: "B"},
			Region:    "cn-beijing",
			RoleName:  "r",
			AccountID: "123456789012",
		})
		if err == nil {
			t.Fatal("expected error for 400")
		}
		if got := rt.requestCount(); got != 1 {
			t.Fatalf("request count=%d, want 1", got)
		}
	})

	t.Run("http 500 is terminal", func(t *testing.T) {
		rt := newRecordingTransport()
		rt.responses = []http.Response{{
			StatusCode: http.StatusInternalServerError,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(stsErrorResponse("InternalError", "boom"))),
		}}
		c := newTestClient(rt)
		_, err := c.AssumeRole(context.Background(), AssumeRoleInput{
			Source:    SourceCredential{AccessKeyID: "A", SecretAccessKey: "B"},
			Region:    "cn-beijing",
			RoleName:  "r",
			AccountID: "123456789012",
		})
		if err == nil {
			t.Fatal("expected error for 500")
		}
		if got := rt.requestCount(); got != 1 {
			t.Fatalf("request count=%d, want 1", got)
		}
	})

	t.Run("terminal service code retries once", func(t *testing.T) {
		rt := newRecordingTransport()
		rt.responses = []http.Response{{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(stsErrorResponse("AccessDenied", "no"))),
		}}
		c := newTestClient(rt)
		_, err := c.AssumeRole(context.Background(), AssumeRoleInput{
			Source:    SourceCredential{AccessKeyID: "A", SecretAccessKey: "B"},
			Region:    "cn-beijing",
			RoleName:  "r",
			AccountID: "123456789012",
		})
		if err == nil {
			t.Fatal("expected error for terminal service code")
		}
		if got := rt.requestCount(); got != 1 {
			t.Fatalf("request count=%d, want 1", got)
		}
	})

	t.Run("network error retries four times", func(t *testing.T) {
		rt := newRecordingTransport()
		for i := 0; i < 4; i++ {
			rt.errs = append(rt.errs, errors.New("connection reset"))
		}
		c := newTestClient(rt)
		_, err := c.AssumeRole(context.Background(), AssumeRoleInput{
			Source:    SourceCredential{AccessKeyID: "A", SecretAccessKey: "B"},
			Region:    "cn-beijing",
			RoleName:  "r",
			AccountID: "123456789012",
		})
		if err == nil {
			t.Fatal("expected error after network failures")
		}
		if got := rt.requestCount(); got != 4 {
			t.Fatalf("request count=%d, want 4", got)
		}
	})

	t.Run("timeout service code retries four times", func(t *testing.T) {
		rt := newRecordingTransport()
		for i := 0; i < 4; i++ {
			rt.responses = append(rt.responses, http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(bytes.NewReader(stsErrorResponse("RequestTimeout", "slow"))),
			})
		}
		c := newTestClient(rt)
		_, err := c.AssumeRole(context.Background(), AssumeRoleInput{
			Source:    SourceCredential{AccessKeyID: "A", SecretAccessKey: "B"},
			Region:    "cn-beijing",
			RoleName:  "r",
			AccountID: "123456789012",
		})
		if err == nil {
			t.Fatal("expected error after exhausting retries")
		}
		if got := rt.requestCount(); got != 4 {
			t.Fatalf("request count=%d, want 4", got)
		}
	})

	t.Run("retry then success", func(t *testing.T) {
		rt := newRecordingTransport()
		rt.responses = []http.Response{
			{StatusCode: http.StatusTooManyRequests, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(stsErrorResponse("Throttling", "slow")))},
			{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(stsRAMResponse("A", "B", "C", "2026-07-25T13:00:00Z")))},
		}
		c := newTestClient(rt)
		creds, err := c.AssumeRole(context.Background(), AssumeRoleInput{
			Source:    SourceCredential{AccessKeyID: "A", SecretAccessKey: "B"},
			Region:    "cn-beijing",
			RoleName:  "r",
			AccountID: "123456789012",
		})
		if err != nil {
			t.Fatalf("expected success after retry, got %v", err)
		}
		if creds.AccessKeyID != "A" {
			t.Fatalf("unexpected creds: %+v", creds)
		}
		if got := rt.requestCount(); got != 2 {
			t.Fatalf("request count=%d, want 2", got)
		}
	})

	t.Run("ram role session name is stable across retries but changes per call", func(t *testing.T) {
		rt := newRecordingTransport()
		for i := 0; i < 3; i++ {
			rt.responses = append(rt.responses, http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     make(http.Header),
				Body:       io.NopCloser(bytes.NewReader(stsErrorResponse("Throttling", "slow"))),
			})
		}
		rt.responses = append(rt.responses, http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(stsRAMResponse("A", "B", "C", "2026-07-25T13:00:00Z"))),
		})
		// UUID generator returns a different value on each logical call.
		var counter int32
		c := newTestClient(rt)
		c.uuid = func() string {
			n := atomic.AddInt32(&counter, 1)
			return fmt.Sprintf("uuid-%d", n)
		}
		if _, err := c.AssumeRole(context.Background(), AssumeRoleInput{
			Source:    SourceCredential{AccessKeyID: "A", SecretAccessKey: "B"},
			Region:    "cn-beijing",
			RoleName:  "r",
			AccountID: "123456789012",
		}); err != nil {
			t.Fatalf("AssumeRole error: %v", err)
		}
		reqs := rt.allRequests()
		if len(reqs) != 4 {
			t.Fatalf("request count=%d, want 4", len(reqs))
		}
		// All four attempts must carry the SAME RoleSessionName: the UUID is
		// generated once per logical AssumeRole call, not once per retry.
		first := reqs[0].URL.Query().Get("RoleSessionName")
		if first != "uuid-1" {
			t.Fatalf("first RoleSessionName=%q, want uuid-1", first)
		}
		for i, r := range reqs {
			if got := r.URL.Query().Get("RoleSessionName"); got != first {
				t.Fatalf("attempt %d RoleSessionName=%q, want %q (stable across retries)", i, got, first)
			}
		}

		// A second independent AssumeRole call must produce a new UUID.
		rt2 := newRecordingTransport()
		rt2.responses = []http.Response{{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(stsRAMResponse("A", "B", "C", "2026-07-25T13:00:00Z"))),
		}}
		c2 := newTestClient(rt2)
		c2.uuid = c.uuid
		if _, err := c2.AssumeRole(context.Background(), AssumeRoleInput{
			Source:    SourceCredential{AccessKeyID: "A", SecretAccessKey: "B"},
			Region:    "cn-beijing",
			RoleName:  "r",
			AccountID: "123456789012",
		}); err != nil {
			t.Fatalf("second AssumeRole error: %v", err)
		}
		second := rt2.lastRequest().URL.Query().Get("RoleSessionName")
		if second != "uuid-2" {
			t.Fatalf("second call RoleSessionName=%q, want uuid-2 (new UUID per logical call)", second)
		}
	})

	t.Run("ram rebuilds and re-signs every attempt", func(t *testing.T) {
		rt := newRecordingTransport()
		for i := 0; i < 3; i++ {
			rt.responses = append(rt.responses, http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     make(http.Header),
				Body:       io.NopCloser(bytes.NewReader(stsErrorResponse("Throttling", "slow"))),
			})
		}
		rt.responses = append(rt.responses, http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(stsRAMResponse("A", "B", "C", "2026-07-25T13:00:00Z"))),
		})
		var signCount int32
		c := newTestClient(rt)
		realSigner := c.signer
		c.signer = func(creds base.Credentials, req *http.Request) *http.Request {
			atomic.AddInt32(&signCount, 1)
			return realSigner(creds, req)
		}
		if _, err := c.AssumeRole(context.Background(), AssumeRoleInput{
			Source:    SourceCredential{AccessKeyID: "A", SecretAccessKey: "B"},
			Region:    "cn-beijing",
			RoleName:  "r",
			AccountID: "123456789012",
		}); err != nil {
			t.Fatalf("AssumeRole error: %v", err)
		}
		if got := rt.requestCount(); got != 4 {
			t.Fatalf("transport call count=%d, want 4", got)
		}
		if got := atomic.LoadInt32(&signCount); got != 4 {
			t.Fatalf("signer call count=%d, want 4 (re-sign every attempt)", got)
		}
	})
}

// ---------------------------------------------------------------------------
// Error redaction
// ---------------------------------------------------------------------------

func TestSTSErrorsNeverExposeSecretsOrRawBody(t *testing.T) {
	const (
		sourceAK     = "SOURCE-AK-CANARY"
		sourceSK     = "SOURCE-SK-CANARY"
		sourceToken  = "SOURCE-TOKEN-CANARY"
		tempAK       = "TEMP-AK-CANARY"
		tempSK       = "TEMP-SK-CANARY"
		tempToken    = "TEMP-TOKEN-CANARY"
		oidcToken    = "OIDC-TOKEN-CANARY"
		responseBody = "RESPONSE-BODY-CANARY"
	)

	t.Run("service error does not leak body or credentials", func(t *testing.T) {
		rt := newRecordingTransport()
		rt.responses = []http.Response{{
			StatusCode: http.StatusBadRequest,
			Header:     make(http.Header),
			Body: io.NopCloser(bytes.NewReader([]byte(
				fmt.Sprintf(`{"ResponseMetadata":{"RequestId":"r","Error":{"Code":"InvalidParameter","Message":"%s"}},"Result":{"Credentials":{"AccessKeyId":"%s","SecretAccessKey":"%s","SessionToken":"%s"}}}`,
					responseBody, tempAK, tempSK, tempToken)))),
		}}
		c := newTestClient(rt)
		_, err := c.AssumeRole(context.Background(), AssumeRoleInput{
			Source:    SourceCredential{AccessKeyID: sourceAK, SecretAccessKey: sourceSK, SessionToken: sourceToken},
			Region:    "cn-beijing",
			RoleName:  "r",
			AccountID: "123456789012",
		})
		if err == nil {
			t.Fatal("expected error")
		}
		text := err.Error()
		for _, canary := range []string{sourceAK, sourceSK, sourceToken, tempAK, tempSK, tempToken, responseBody} {
			if strings.Contains(text, canary) {
				t.Fatalf("error leaked canary %q: %q", canary, text)
			}
		}
	})

	t.Run("transport error does not leak query secret", func(t *testing.T) {
		rt := newRecordingTransport()
		rt.errs = []error{&url.Error{
			Op:  "Get",
			URL: "https://sts.volcengineapi.com/?Action=AssumeRole&RoleSessionName=" + sourceToken,
			Err: errors.New("dial tcp: connection refused"),
		}}
		c := newTestClient(rt)
		_, err := c.AssumeRole(context.Background(), AssumeRoleInput{
			Source:    SourceCredential{AccessKeyID: sourceAK, SecretAccessKey: sourceSK, SessionToken: sourceToken},
			Region:    "cn-beijing",
			RoleName:  "r",
			AccountID: "123456789012",
		})
		if err == nil {
			t.Fatal("expected error")
		}
		text := err.Error()
		if strings.Contains(text, sourceToken) {
			t.Fatalf("error leaked session token from query: %q", text)
		}
		if strings.Contains(text, "RoleSessionName=") {
			t.Fatalf("error leaked query string: %q", text)
		}
	})

	t.Run("oidc error does not leak token", func(t *testing.T) {
		rt := newRecordingTransport()
		rt.responses = []http.Response{{
			StatusCode: http.StatusBadRequest,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(stsErrorResponse("InvalidParameter", oidcToken))),
		}}
		c := newTestClient(rt)
		_, err := c.AssumeRoleWithOIDC(context.Background(), OIDCInput{
			Token:   []byte(oidcToken),
			RoleTRN: "trn:iam::1:role/r",
		})
		if err == nil {
			t.Fatal("expected error")
		}
		text := err.Error()
		if strings.Contains(text, oidcToken) {
			t.Fatalf("error leaked OIDC token: %q", text)
		}
	})
}

// ---------------------------------------------------------------------------
// Context cancellation and body limit
// ---------------------------------------------------------------------------

func TestSTSRespectsContextCancellationAndBodyLimit(t *testing.T) {
	t.Run("canceled context before request fails closed", func(t *testing.T) {
		rt := newRecordingTransport()
		c := newTestClient(rt)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := c.AssumeRole(ctx, AssumeRoleInput{
			Source:    SourceCredential{AccessKeyID: "A", SecretAccessKey: "B"},
			Region:    "cn-beijing",
			RoleName:  "r",
			AccountID: "123456789012",
		}); err == nil {
			t.Fatal("expected error for canceled context")
		}
		if got := rt.requestCount(); got != 0 {
			t.Fatalf("request count=%d, want 0 for canceled context", got)
		}
	})

	t.Run("context cancellation stops retry sleep", func(t *testing.T) {
		rt := newRecordingTransport()
		rt.responses = []http.Response{{
			StatusCode: http.StatusTooManyRequests,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(stsErrorResponse("Throttling", "slow"))),
		}}
		c := newTestClient(rt)
		// Sleeper simulates context cancellation during the retry wait.
		c.sleeper = func(ctx context.Context, _ time.Duration) error {
			return context.Canceled
		}
		_, err := c.AssumeRole(context.Background(), AssumeRoleInput{
			Source:    SourceCredential{AccessKeyID: "A", SecretAccessKey: "B"},
			Region:    "cn-beijing",
			RoleName:  "r",
			AccountID: "123456789012",
		})
		if err == nil {
			t.Fatal("expected error when sleep is interrupted by context")
		}
		// Only one attempt should have been made; the retry sleep was canceled.
		if got := rt.requestCount(); got != 1 {
			t.Fatalf("request count=%d, want 1", got)
		}
	})

	t.Run("body over 64kib is rejected", func(t *testing.T) {
		rt := newRecordingTransport()
		bigBody := make([]byte, 64*1024+10)
		for i := range bigBody {
			bigBody[i] = 'x'
		}
		rt.responses = []http.Response{{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(bigBody)),
		}}
		c := newTestClient(rt)
		_, err := c.AssumeRole(context.Background(), AssumeRoleInput{
			Source:    SourceCredential{AccessKeyID: "A", SecretAccessKey: "B"},
			Region:    "cn-beijing",
			RoleName:  "r",
			AccountID: "123456789012",
		})
		if err == nil {
			t.Fatal("expected error for oversized body")
		}
	})
}

// ---------------------------------------------------------------------------
// RAM host set before signing; exact field counts; source token in signed headers
// ---------------------------------------------------------------------------

func TestAssumeRoleSetsHostBeforeSigning(t *testing.T) {
	rt := newRecordingTransport()
	rt.responses = []http.Response{{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(stsRAMResponse("AK", "SK", "TOK", "2026-07-25T13:00:00Z"))),
	}}
	c := newTestClient(rt)
	var seenHost string
	realSigner := c.signer
	c.signer = func(creds base.Credentials, req *http.Request) *http.Request {
		seenHost = req.Host
		return realSigner(creds, req)
	}
	if _, err := c.AssumeRole(context.Background(), AssumeRoleInput{
		Source:    SourceCredential{AccessKeyID: "AK", SecretAccessKey: "SK"},
		Region:    "cn-beijing",
		RoleName:  "r",
		AccountID: "123456789012",
	}); err != nil {
		t.Fatalf("AssumeRole error: %v", err)
	}
	if seenHost != "sts.volcengineapi.com" {
		t.Fatalf("signer received host=%q, want sts.volcengineapi.com", seenHost)
	}
	// The Authorization header must have been produced with a non-empty canonical host.
	auth := rt.lastRequest().Header.Get("Authorization")
	if auth == "" {
		t.Fatal("expected Authorization header to be set")
	}
	if !strings.Contains(auth, "Credential=") {
		t.Fatalf("Authorization=%q, want Credential= scope", auth)
	}
}

func TestAssumeRoleQueryHasExactlyFiveFields(t *testing.T) {
	rt := newRecordingTransport()
	rt.responses = []http.Response{{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(stsRAMResponse("AK", "SK", "TOK", "2026-07-25T13:00:00Z"))),
	}}
	c := newTestClient(rt)
	if _, err := c.AssumeRole(context.Background(), AssumeRoleInput{
		Source:    SourceCredential{AccessKeyID: "AK", SecretAccessKey: "SK"},
		AccountID: "2100000000",
		RoleName:  "my-role",
		Region:    "cn-beijing",
	}); err != nil {
		t.Fatalf("AssumeRole error: %v", err)
	}
	q := rt.lastRequest().URL.Query()
	if got := len(q); got != 5 {
		t.Fatalf("query field count=%d, want 5 (Action, Version, DurationSeconds, RoleTrn, RoleSessionName); query=%v", got, q)
	}
	for _, key := range []string{"Action", "Version", "DurationSeconds", "RoleTrn", "RoleSessionName"} {
		if q.Get(key) == "" {
			t.Fatalf("query missing expected field %q", key)
		}
	}
}

func TestAssumeRoleRejectsEmptyAccountIDBeforeNetwork(t *testing.T) {
	for _, accountID := range []string{"", "   ", "\t"} {
		rt := newRecordingTransport()
		c := newTestClient(rt)
		_, err := c.AssumeRole(context.Background(), AssumeRoleInput{
			Source:    SourceCredential{AccessKeyID: "A", SecretAccessKey: "B"},
			AccountID: accountID,
			RoleName:  "r",
			Region:    "cn-beijing",
		})
		if err == nil {
			t.Fatalf("expected error for empty/whitespace AccountID=%q", accountID)
		}
		if got := rt.requestCount(); got != 0 {
			t.Fatalf("request count=%d for AccountID=%q, want 0 (no network before validation)", got, accountID)
		}
	}
}

func TestAssumeRoleSourceTokenInSignedHeaders(t *testing.T) {
	rt := newRecordingTransport()
	rt.responses = []http.Response{{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(stsRAMResponse("AK", "SK", "TOK", "2026-07-25T13:00:00Z"))),
	}}
	c := newTestClient(rt)
	if _, err := c.AssumeRole(context.Background(), AssumeRoleInput{
		Source:    SourceCredential{AccessKeyID: "AK", SecretAccessKey: "SK", SessionToken: "SOURCE-SESSION-TOKEN"},
		Region:    "cn-beijing",
		RoleName:  "r",
		AccountID: "123456789012",
	}); err != nil {
		t.Fatalf("AssumeRole error: %v", err)
	}
	req := rt.lastRequest()
	if got := req.Header.Get("X-Security-Token"); got != "SOURCE-SESSION-TOKEN" {
		t.Fatalf("X-Security-Token=%q, want SOURCE-SESSION-TOKEN", got)
	}
	auth := req.Header.Get("Authorization")
	if !strings.Contains(auth, "x-security-token") {
		t.Fatalf("Authorization=%q, want SignedHeaders to include x-security-token", auth)
	}
}

// ---------------------------------------------------------------------------
// OIDC exact field counts; fully unsigned; Expiration-only
// ---------------------------------------------------------------------------

func TestAssumeRoleWithOIDCQueryAndFormFieldCounts(t *testing.T) {
	rt := newRecordingTransport()
	rt.responses = []http.Response{{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(stsOIDCResponse("AK", "SK", "TOK", "2026-07-25T13:00:00Z"))),
	}}
	c := newTestClient(rt)
	if _, err := c.AssumeRoleWithOIDC(context.Background(), OIDCInput{
		Token:   []byte("header.payload.signature\n"),
		RoleTRN: "trn:iam::2100000000:role/oidc-role",
	}); err != nil {
		t.Fatalf("AssumeRoleWithOIDC error: %v", err)
	}
	req := rt.lastRequest()
	q := req.URL.Query()
	if got := len(q); got != 2 {
		t.Fatalf("query field count=%d, want 2 (Action, Version); query=%v", got, q)
	}
	body, _ := io.ReadAll(req.Body)
	form, err := url.ParseQuery(string(body))
	if err != nil {
		t.Fatalf("parse form: %v", err)
	}
	if got := len(form); got != 4 {
		t.Fatalf("form field count=%d, want 4 (RoleTrn, OIDCToken, RoleSessionName, DurationSeconds); form=%v", got, form)
	}
}

func TestAssumeRoleWithOIDCIsFullyUnsigned(t *testing.T) {
	rt := newRecordingTransport()
	rt.responses = []http.Response{{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(stsOIDCResponse("AK", "SK", "TOK", "2026-07-25T13:00:00Z"))),
	}}
	c := newTestClient(rt)
	if _, err := c.AssumeRoleWithOIDC(context.Background(), OIDCInput{
		Token:   []byte("tok"),
		RoleTRN: "trn:iam::1:role/r",
	}); err != nil {
		t.Fatalf("AssumeRoleWithOIDC error: %v", err)
	}
	req := rt.lastRequest()
	for _, h := range []string{"Authorization", "X-Date", "X-Content-Sha256", "X-Security-Token"} {
		if got := req.Header.Get(h); got != "" {
			t.Fatalf("OIDC request must be unsigned, but %s=%q", h, got)
		}
	}
}

func TestAssumeRoleWithOIDCRejectsResponseWithOnlyExpiredTime(t *testing.T) {
	rt := newRecordingTransport()
	// Response carries ExpiredTime but no Expiration; OIDC must reject it.
	rt.responses = []http.Response{{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(stsRAMResponse("AK", "SK", "TOK", "2026-07-25T13:00:00Z"))),
	}}
	c := newTestClient(rt)
	if _, err := c.AssumeRoleWithOIDC(context.Background(), OIDCInput{
		Token:   []byte("tok"),
		RoleTRN: "trn:iam::1:role/r",
	}); err == nil {
		t.Fatal("expected error for OIDC response lacking Expiration field")
	}
}

// ---------------------------------------------------------------------------
// Bounded per-request timeouts
// ---------------------------------------------------------------------------

func TestProductionClientSetsBoundedPerRequestTimeouts(t *testing.T) {
	c := NewClient()
	if c.ramTimeout != 5*time.Second {
		t.Fatalf("ramTimeout=%v, want 5s", c.ramTimeout)
	}
	if c.oidcTimeout != 10*time.Second {
		t.Fatalf("oidcTimeout=%v, want 10s", c.oidcTimeout)
	}
}

func TestAssumeRoleAppliesFiveSecondTimeout(t *testing.T) {
	rt := newRecordingTransport()
	var seenTimeout time.Duration
	rt.responses = []http.Response{{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(stsRAMResponse("AK", "SK", "TOK", "2026-07-25T13:00:00Z"))),
	}}
	c := newTestClient(rt)
	c.ramTimeout = 5 * time.Second
	// Wrap the transport to observe the request context deadline.
	wrapped := &timeoutObservingTransport{rt: rt, observed: &seenTimeout}
	c.httpClient = &http.Client{Transport: wrapped}
	if _, err := c.AssumeRole(context.Background(), AssumeRoleInput{
		Source:    SourceCredential{AccessKeyID: "AK", SecretAccessKey: "SK"},
		Region:    "cn-beijing",
		RoleName:  "r",
		AccountID: "123456789012",
	}); err != nil {
		t.Fatalf("AssumeRole error: %v", err)
	}
	if seenTimeout == 0 {
		t.Fatal("expected a deadline to be set on the request context")
	}
	if seenTimeout > 5*time.Second {
		t.Fatalf("observed timeout=%v, want <=5s", seenTimeout)
	}
}

func TestAssumeRoleWithOIDCAppliesTenSecondTimeout(t *testing.T) {
	rt := newRecordingTransport()
	var seenTimeout time.Duration
	rt.responses = []http.Response{{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(stsOIDCResponse("AK", "SK", "TOK", "2026-07-25T13:00:00Z"))),
	}}
	c := newTestClient(rt)
	c.oidcTimeout = 10 * time.Second
	wrapped := &timeoutObservingTransport{rt: rt, observed: &seenTimeout}
	c.httpClient = &http.Client{Transport: wrapped}
	if _, err := c.AssumeRoleWithOIDC(context.Background(), OIDCInput{
		Token:   []byte("tok"),
		RoleTRN: "trn:iam::1:role/r",
	}); err != nil {
		t.Fatalf("AssumeRoleWithOIDC error: %v", err)
	}
	if seenTimeout == 0 {
		t.Fatal("expected a deadline to be set on the request context")
	}
	if seenTimeout > 10*time.Second {
		t.Fatalf("observed timeout=%v, want <=10s", seenTimeout)
	}
}

func TestCallerShorterDeadlineTakesPrecedenceOverSTS(t *testing.T) {
	rt := newRecordingTransport()
	var seenTimeout time.Duration
	rt.responses = []http.Response{{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(stsRAMResponse("AK", "SK", "TOK", "2026-07-25T13:00:00Z"))),
	}}
	c := newTestClient(rt)
	c.ramTimeout = 5 * time.Second
	wrapped := &timeoutObservingTransport{rt: rt, observed: &seenTimeout}
	c.httpClient = &http.Client{Transport: wrapped}
	// Caller imposes a 100ms deadline, shorter than the 5s STS timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := c.AssumeRole(ctx, AssumeRoleInput{
		Source:    SourceCredential{AccessKeyID: "AK", SecretAccessKey: "SK"},
		Region:    "cn-beijing",
		RoleName:  "r",
		AccountID: "123456789012",
	}); err != nil {
		t.Fatalf("AssumeRole error: %v", err)
	}
	if seenTimeout > 100*time.Millisecond {
		t.Fatalf("observed timeout=%v, want <=100ms (caller deadline must win)", seenTimeout)
	}
}

// timeoutObservingTransport wraps a RoundTripper and records the remaining
// deadline on the request context, if any.
type timeoutObservingTransport struct {
	rt       http.RoundTripper
	observed *time.Duration
}

func (t *timeoutObservingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if deadline, ok := req.Context().Deadline(); ok {
		*t.observed = time.Until(deadline)
	}
	return t.rt.RoundTrip(req)
}

// ---------------------------------------------------------------------------
// ErrBodyTooLarge handling and io.ReadAll error
// ---------------------------------------------------------------------------

func TestSTSErrBodyTooLargeFailsClosedWithoutRetry(t *testing.T) {
	// Build a response that is valid JSON followed by whitespace padding so the
	// total exceeds 64KiB. The JSON itself decodes to a successful response, but
	// the body cap must reject it before decoding.
	validJSON := stsRAMResponse("AK", "SK", "TOK", "2026-07-25T13:00:00Z")
	padding := bytes.Repeat([]byte(" "), 64*1024+10)
	oversized := append(validJSON, padding...)

	t.Run("2xx oversized body rejected", func(t *testing.T) {
		rt := newRecordingTransport()
		rt.responses = []http.Response{{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(oversized)),
		}}
		c := newTestClient(rt)
		_, err := c.AssumeRole(context.Background(), AssumeRoleInput{
			Source:    SourceCredential{AccessKeyID: "A", SecretAccessKey: "B"},
			Region:    "cn-beijing",
			RoleName:  "r",
			AccountID: "123456789012",
		})
		if err == nil {
			t.Fatal("expected error for oversized 2xx body")
		}
		if got := rt.requestCount(); got != 1 {
			t.Fatalf("request count=%d, want 1 (no retry on body cap)", got)
		}
	})

	t.Run("non-2xx oversized body rejected", func(t *testing.T) {
		rt := newRecordingTransport()
		rt.responses = []http.Response{{
			StatusCode: http.StatusTooManyRequests,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(oversized)),
		}}
		c := newTestClient(rt)
		_, err := c.AssumeRole(context.Background(), AssumeRoleInput{
			Source:    SourceCredential{AccessKeyID: "A", SecretAccessKey: "B"},
			Region:    "cn-beijing",
			RoleName:  "r",
			AccountID: "123456789012",
		})
		if err == nil {
			t.Fatal("expected error for oversized non-2xx body")
		}
		if got := rt.requestCount(); got != 1 {
			t.Fatalf("request count=%d, want 1 (no retry on body cap)", got)
		}
	})
}

// ---------------------------------------------------------------------------
// ServiceCode is set on STS service errors
// ---------------------------------------------------------------------------

func TestSTSServiceErrorsPopulateServiceCode(t *testing.T) {
	rt := newRecordingTransport()
	rt.responses = []http.Response{{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(stsErrorResponse("AccessDenied", "slow down"))),
	}}
	c := newTestClient(rt)
	_, err := c.AssumeRole(context.Background(), AssumeRoleInput{
		Source:    SourceCredential{AccessKeyID: "A", SecretAccessKey: "B"},
		Region:    "cn-beijing",
		RoleName:  "r",
		AccountID: "123456789012",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	var authErr *auth.Error
	if !errors.As(err, &authErr) {
		t.Fatalf("expected *auth.Error, got %T", err)
	}
	if authErr.ServiceCode != "AccessDenied" {
		t.Fatalf("ServiceCode=%q, want AccessDenied", authErr.ServiceCode)
	}
	// Description must be fixed local text, not the server message.
	if strings.Contains(authErr.Description, "slow down") {
		t.Fatalf("Description leaked server message: %q", authErr.Description)
	}
	if strings.Contains(authErr.Error(), "slow down") {
		t.Fatalf("Error() leaked server message: %q", authErr.Error())
	}
}

// ---------------------------------------------------------------------------
// Per-attempt timeout: each HTTP request gets an independent deadline
// ---------------------------------------------------------------------------

// perAttemptTimeoutTransport blocks the first call until its attempt context
// expires, then returns that context's error. The second call returns success
// immediately. This proves each attempt gets a fresh, full timeout rather than
// sharing one deadline across all attempts + sleeps.
type perAttemptTimeoutTransport struct {
	rt        http.RoundTripper
	callCount int32
}

func (t *perAttemptTimeoutTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	n := atomic.AddInt32(&t.callCount, 1)
	if n == 1 {
		// Block until the per-attempt context deadline fires.
		<-req.Context().Done()
		return nil, req.Context().Err()
	}
	return t.rt.RoundTrip(req)
}

func TestPerAttemptTimeoutIsIndependentPerRequest(t *testing.T) {
	rt := newRecordingTransport()
	rt.responses = []http.Response{{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(stsRAMResponse("AK", "SK", "TOK", "2026-07-25T13:00:00Z"))),
	}}
	c := newTestClient(rt)
	// Use a very short timeout so the test runs fast.
	c.ramTimeout = 20 * time.Millisecond
	wrapped := &perAttemptTimeoutTransport{rt: rt}
	c.httpClient = &http.Client{Transport: wrapped}

	creds, err := c.AssumeRole(context.Background(), AssumeRoleInput{
		Source:    SourceCredential{AccessKeyID: "AK", SecretAccessKey: "SK"},
		AccountID: "123456789012",
		Region:    "cn-beijing",
		RoleName:  "r",
	})
	if err != nil {
		t.Fatalf("expected success after retry, got %v", err)
	}
	if creds.AccessKeyID != "AK" {
		t.Fatalf("unexpected creds: %+v", creds)
	}
	// First attempt timed out (transient), second attempt succeeded: 2 calls total.
	if got := atomic.LoadInt32(&wrapped.callCount); got != 2 {
		t.Fatalf("transport call count=%d, want 2 (timeout then success)", got)
	}
}

func TestPerAttemptTimeoutAppliesToOIDC(t *testing.T) {
	rt := newRecordingTransport()
	rt.responses = []http.Response{{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(stsOIDCResponse("AK", "SK", "TOK", "2026-07-25T13:00:00Z"))),
	}}
	c := newTestClient(rt)
	c.oidcTimeout = 20 * time.Millisecond
	wrapped := &perAttemptTimeoutTransport{rt: rt}
	c.httpClient = &http.Client{Transport: wrapped}

	creds, err := c.AssumeRoleWithOIDC(context.Background(), OIDCInput{
		Token:   []byte("tok"),
		RoleTRN: "trn:iam::1:role/r",
	})
	if err != nil {
		t.Fatalf("expected success after retry, got %v", err)
	}
	if creds.AccessKeyID != "AK" {
		t.Fatalf("unexpected creds: %+v", creds)
	}
	if got := atomic.LoadInt32(&wrapped.callCount); got != 2 {
		t.Fatalf("transport call count=%d, want 2", got)
	}
}

// ---------------------------------------------------------------------------
// Attempt context must stay alive through response body read
// ---------------------------------------------------------------------------

// contextAwareBody wraps a response body and fails Read if the request context
// is done, simulating real net/http where cancelling the request context
// interrupts streaming body reads.
type contextAwareBody struct {
	inner io.ReadCloser
	ctx   context.Context
}

func (b *contextAwareBody) Read(p []byte) (int, error) {
	if err := b.ctx.Err(); err != nil {
		return 0, err
	}
	return b.inner.Read(p)
}

func (b *contextAwareBody) Close() error {
	return b.inner.Close()
}

// contextAwareTransport returns a response whose body is tied to the request
// context. If the context is cancelled before the body is fully read, Read
// fails — exactly like real net/http.
type contextAwareTransport struct {
	body []byte
}

func (t *contextAwareTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       &contextAwareBody{inner: io.NopCloser(bytes.NewReader(t.body)), ctx: req.Context()},
	}, nil
}

func TestAttemptContextKeptAliveThroughBodyRead(t *testing.T) {
	rt := &contextAwareTransport{body: stsRAMResponse("AK", "SK", "TOK", "2026-07-25T13:00:00Z")}
	c := newTestClient(rt)
	c.ramTimeout = 5 * time.Second
	creds, err := c.AssumeRole(context.Background(), AssumeRoleInput{
		Source:    SourceCredential{AccessKeyID: "AK", SecretAccessKey: "SK"},
		AccountID: "123456789012",
		Region:    "cn-beijing",
		RoleName:  "r",
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if creds.AccessKeyID != "AK" {
		t.Fatalf("unexpected creds: %+v", creds)
	}
}

func TestCallerContextCancelStopsRetriesImmediately(t *testing.T) {
	rt := newRecordingTransport()
	rt.responses = []http.Response{{
		StatusCode: http.StatusTooManyRequests,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(stsErrorResponse("Throttling", "slow"))),
	}}
	c := newTestClient(rt)
	c.ramTimeout = 5 * time.Second

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel the real caller context during the first retry sleep. The loop
	// must then stop: the next iteration's ctx.Err() check returns the error.
	c.sleeper = func(_ context.Context, _ time.Duration) error {
		cancel()
		return nil
	}

	_, err := c.AssumeRole(ctx, AssumeRoleInput{
		Source:    SourceCredential{AccessKeyID: "A", SecretAccessKey: "B"},
		AccountID: "123456789012",
		Region:    "cn-beijing",
		RoleName:  "r",
	})
	if err == nil {
		t.Fatal("expected error when caller context is canceled")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	// Only one attempt: cancellation during sleep prevents further attempts.
	if got := rt.requestCount(); got != 1 {
		t.Fatalf("request count=%d, want 1", got)
	}
}

// ---------------------------------------------------------------------------
// Response-time expiration: validated against clock at parse time
// ---------------------------------------------------------------------------

// advancingClock returns startAt on the first call, then advances by step on
// each subsequent call.
type advancingClock struct {
	current time.Time
	step    time.Duration
	calls   int32
}

func (c *advancingClock) now() time.Time {
	n := atomic.AddInt32(&c.calls, 1)
	if n == 1 {
		return c.current
	}
	c.current = c.current.Add(c.step)
	return c.current
}

func TestRAMExpirationRejectedWhenExpiredAtValidationTime(t *testing.T) {
	// requestStarted = T0. Server returns ExpiredTime = T0 + 30m (valid at start).
	// But validationNow = T0 + 1h, so the credential is already expired by the
	// time we validate it. Must fail closed.
	rt := newRecordingTransport()
	rt.responses = []http.Response{{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(stsRAMResponse("AK", "SK", "TOK", "2026-07-25T12:30:00Z"))),
	}}
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	clock := &advancingClock{current: start, step: time.Hour}
	c := newTestClient(rt)
	c.clock = clock.now

	_, err := c.AssumeRole(context.Background(), AssumeRoleInput{
		Source:    SourceCredential{AccessKeyID: "AK", SecretAccessKey: "SK"},
		AccountID: "123456789012",
		Region:    "cn-beijing",
		RoleName:  "r",
	})
	if err == nil {
		t.Fatal("expected error: credential expired at validation time")
	}
}

func TestRAMHardTTLAnchoredToRequestStartNotValidationTime(t *testing.T) {
	// requestStarted = T0. Server returns ExpiredTime = T0 + 2h.
	// validationNow = T0 + 30m.
	// ExpiresAt = min(T0+1h, T0+2h) = T0+1h. T0+1h > T0+30m (validationNow), so valid.
	// If hard TTL were anchored to validationNow, it would be T0+30m+1h = T0+90m,
	// which is also > T0+30m, so this test checks the returned ExpiresAt value.
	rt := newRecordingTransport()
	rt.responses = []http.Response{{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(stsRAMResponse("AK", "SK", "TOK", "2026-07-25T14:00:00Z"))),
	}}
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	clock := &advancingClock{current: start, step: 30 * time.Minute}
	c := newTestClient(rt)
	c.clock = clock.now

	creds, err := c.AssumeRole(context.Background(), AssumeRoleInput{
		Source:    SourceCredential{AccessKeyID: "AK", SecretAccessKey: "SK"},
		AccountID: "123456789012",
		Region:    "cn-beijing",
		RoleName:  "r",
	})
	if err != nil {
		t.Fatalf("AssumeRole error: %v", err)
	}
	// Hard TTL = requestStarted + 1h = 13:00, NOT validationNow + 1h = 13:30.
	want := start.Add(time.Hour)
	if !creds.ExpiresAt.Equal(want) {
		t.Fatalf("ExpiresAt=%v, want %v (hard TTL anchored to request start)", creds.ExpiresAt, want)
	}
}

func TestOIDCExpirationRejectedWhenExpiredAtValidationTime(t *testing.T) {
	// requestStarted = T0. Server returns Expiration = T0 + 30m (valid at start).
	// validationNow = T0 + 1h → credential already expired. Must fail closed.
	rt := newRecordingTransport()
	rt.responses = []http.Response{{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(stsOIDCResponse("AK", "SK", "TOK", "2026-07-25T12:30:00Z"))),
	}}
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	clock := &advancingClock{current: start, step: time.Hour}
	c := newTestClient(rt)
	c.clock = clock.now

	_, err := c.AssumeRoleWithOIDC(context.Background(), OIDCInput{
		Token:   []byte("tok"),
		RoleTRN: "trn:iam::1:role/r",
	})
	if err == nil {
		t.Fatal("expected error: OIDC credential expired at validation time")
	}
}

// ---------------------------------------------------------------------------
// Signer nil fail-closed
// ---------------------------------------------------------------------------

func TestAssumeRoleFailsClosedWhenSignerReturnsNil(t *testing.T) {
	rt := newRecordingTransport()
	c := newTestClient(rt)
	c.signer = func(base.Credentials, *http.Request) *http.Request { return nil }
	_, err := c.AssumeRole(context.Background(), AssumeRoleInput{
		Source:    SourceCredential{AccessKeyID: "AK", SecretAccessKey: "SK"},
		AccountID: "123456789012",
		Region:    "cn-beijing",
		RoleName:  "r",
	})
	if err == nil {
		t.Fatal("expected error when signer returns nil")
	}
	if got := rt.requestCount(); got != 0 {
		t.Fatalf("request count=%d, want 0 (no network when signing fails)", got)
	}
}

// ---------------------------------------------------------------------------
// Production constructor
// ---------------------------------------------------------------------------

func TestNewClientFixesHostAndDefaults(t *testing.T) {
	c := NewClient()
	if c.endpoint != "sts.volcengineapi.com" {
		t.Fatalf("endpoint=%q, want sts.volcengineapi.com", c.endpoint)
	}
	if c.httpClient == nil {
		t.Fatal("httpClient must not be nil")
	}
	if c.signer == nil {
		t.Fatal("signer must not be nil")
	}
	if c.clock == nil {
		t.Fatal("clock must not be nil")
	}
	if c.uuid == nil {
		t.Fatal("uuid must not be nil")
	}
	if c.sleeper == nil {
		t.Fatal("sleeper must not be nil")
	}
}

// TestSTSDoesNotFollowRedirects proves that the production STS client never
// follows HTTP redirects. A 307/308 from the source server must be treated as a
// terminal non-2xx protocol error: the source is hit exactly once, the target
// is never contacted, no retry/sleep occurs, and the error string contains no
// source AK/SK, session token, or OIDC token.
func TestSTSDoesNotFollowRedirects(t *testing.T) {
	cases := []struct {
		name   string
		status int
	}{
		{"assume_role_307", http.StatusTemporaryRedirect},
		{"assume_role_308", http.StatusPermanentRedirect},
		{"oidc_307", http.StatusTemporaryRedirect},
		{"oidc_308", http.StatusPermanentRedirect},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var sourceHits, targetHits int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&targetHits, 1)
				w.WriteHeader(http.StatusOK)
			}))
			defer target.Close()
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&sourceHits, 1)
				w.Header().Set("Location", target.URL)
				w.WriteHeader(tc.status)
			}))
			defer source.Close()

			// Production client with only the private endpoint overridden.
			c := NewClient()
			sourceURL, _ := url.Parse(source.URL)
			c.endpoint = sourceURL.Host

			var err error
			if strings.HasPrefix(tc.name, "oidc") {
				_, err = c.AssumeRoleWithOIDC(context.Background(), OIDCInput{
					Token:      []byte("OIDC-RAW-TOKEN-CANARY"),
					RoleTRN:    "trn:iam::1:role/r",
					DisableSSL: true,
				})
			} else {
				_, err = c.AssumeRole(context.Background(), AssumeRoleInput{
					Source:     SourceCredential{AccessKeyID: "SRC-AK-CANARY", SecretAccessKey: "SRC-SK-CANARY", SessionToken: "SRC-SESSION-TOKEN-CANARY"},
					AccountID:  "123456789012",
					RoleName:   "r",
					DisableSSL: true,
				})
			}
			if err == nil {
				t.Fatal("expected terminal error from redirect, got nil")
			}
			// source hit==1 proves the request was attempted exactly once: the
			// non-follow redirect policy turns 307/308 into a terminal non-2xx
			// error that cannot enter the transport retry layer, so no outer
			// retry or backoff sleep occurs.
			if atomic.LoadInt32(&sourceHits) != 1 {
				t.Fatalf("source hits=%d, want 1", sourceHits)
			}
			if atomic.LoadInt32(&targetHits) != 0 {
				t.Fatalf("target hits=%d, want 0 (redirect must not be followed)", targetHits)
			}
			errStr := err.Error()
			for _, secret := range []string{"SRC-AK-CANARY", "SRC-SK-CANARY", "SRC-SESSION-TOKEN-CANARY", "OIDC-RAW-TOKEN-CANARY"} {
				if strings.Contains(errStr, secret) {
					t.Fatalf("error string leaked secret %q: %s", secret, errStr)
				}
			}
		})
	}
}
