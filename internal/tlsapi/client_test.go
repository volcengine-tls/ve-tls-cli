package tlsapi

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/volcengine/volc-sdk-golang/base"
)

// fixedTimeSigner returns a requestSigner that signs with a fixed timestamp so
// golden signature tests are deterministic. It mirrors the headers written by
// base.Credentials.Sign using base.GetSignRequest.
func fixedTimeSigner(date time.Time) requestSigner {
	return func(creds base.Credentials, req *http.Request) *http.Request {
		query := req.URL.Query()
		req.URL.RawQuery = query.Encode()
		if req.URL.Path == "" {
			req.URL.Path += "/"
		}
		body := readAndReplaceBody(req)
		signRequest := base.GetSignRequest(base.RequestParam{
			IsSignUrl: false,
			Body:      body,
			Host:      req.Host,
			Path:      req.URL.Path,
			Method:    req.Method,
			Date:      date,
			QueryList: query,
			Headers:   req.Header,
		}, creds)
		req.Header.Set("Host", signRequest.Host)
		req.Header.Set("Content-Type", signRequest.ContentType)
		req.Header.Set("X-Date", signRequest.XDate)
		req.Header.Set("X-Content-Sha256", signRequest.XContentSha256)
		req.Header.Set("Authorization", signRequest.Authorization)
		if signRequest.XSecurityToken != "" {
			req.Header.Set("X-Security-Token", signRequest.XSecurityToken)
		}
		return req
	}
}

// readAndReplaceBody mirrors base.readAndReplaceBody so the fixed-time signer
// can hash the body while leaving it readable for the transport.
func readAndReplaceBody(req *http.Request) []byte {
	if req.Body == nil {
		return []byte{}
	}
	payload, _ := io.ReadAll(req.Body)
	req.Body = io.NopCloser(bytes.NewReader(payload))
	return payload
}

func TestResolveSigningCredentials_PrefersExplicit(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "env-ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "env-sk")
	t.Setenv("VOLCENGINE_TOKEN", "env-token")

	creds, err := resolveSigningCredentials("cn-beijing", "TLS", "arg-ak", "arg-sk", "arg-token")
	if err != nil {
		t.Fatalf("resolveSigningCredentials error: %v", err)
	}
	if creds.AccessKeyID != "arg-ak" || creds.SecretAccessKey != "arg-sk" || creds.SessionToken != "arg-token" {
		t.Fatalf("unexpected explicit creds: %+v", creds)
	}
}

func TestResolveSigningCredentials_FallsBackToEnv(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "env-ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "env-sk")
	t.Setenv("VOLCENGINE_TOKEN", "env-token")

	creds, err := resolveSigningCredentials("cn-beijing", "TLS", "", "", "")
	if err != nil {
		t.Fatalf("resolveSigningCredentials error: %v", err)
	}
	if creds.AccessKeyID != "env-ak" || creds.SecretAccessKey != "env-sk" || creds.SessionToken != "env-token" {
		t.Fatalf("unexpected env creds: %+v", creds)
	}
}

func TestResolveSigningCredentials_RequiresKeyPair(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "")
	t.Setenv("VOLCENGINE_TOKEN", "")

	if _, err := resolveSigningCredentials("cn-beijing", "TLS", "", "", ""); err == nil {
		t.Fatalf("expected missing credential error")
	}
}

// TestLegacyClientUsesCurrentPublicCredsOnEverySignature proves that a static
// New() client signs with the *current* value of the public Creds field on every
// request, not a snapshot taken at construction time.
func TestLegacyClientUsesCurrentPublicCredsOnEverySignature(t *testing.T) {
	c, err := New("https://tls-cn-beijing.volces.com", "cn-beijing", "legacy", "ak-1", "sk-1", "token-1", 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rt := &captureRoundTripper{}
	c.HTTP = &http.Client{Transport: rt, Timeout: time.Second}

	if _, err := c.Do(context.Background(), http.MethodPost, "/DescribeProjects", nil, nil, nil); err != nil {
		t.Fatalf("first Do: %v", err)
	}

	// Mutate the public Creds field; the second signature must reflect this.
	c.Creds.AccessKeyID = "ak-2"
	c.Creds.SecretAccessKey = "sk-2"
	c.Creds.SessionToken = "token-2"
	if _, err := c.Do(context.Background(), http.MethodPost, "/DescribeProjects", nil, nil, nil); err != nil {
		t.Fatalf("second Do: %v", err)
	}

	reqs := rt.snapshot()
	if len(reqs) != 2 {
		t.Fatalf("captured %d requests, want 2", len(reqs))
	}
	auth1 := reqs[0].Header.Get("Authorization")
	auth2 := reqs[1].Header.Get("Authorization")
	if !strings.Contains(auth1, "Credential=ak-1/") {
		t.Fatalf("first request must use original ak-1: %q", auth1)
	}
	if !strings.Contains(auth2, "Credential=ak-2/") {
		t.Fatalf("second request must use mutated ak-2: %q", auth2)
	}
	if got, want := reqs[0].Header.Get("X-Security-Token"), "token-1"; got != want {
		t.Fatalf("first X-Security-Token = %q, want %q", got, want)
	}
	if got, want := reqs[1].Header.Get("X-Security-Token"), "token-2"; got != want {
		t.Fatalf("second X-Security-Token = %q, want %q", got, want)
	}
}

// TestLegacyClientSigningScopeUsesCredsNotMutableClientFields proves that the
// static signing scope (Region/Service) comes from c.Creds, not from the mutable
// c.Region/c.Service fields.
func TestLegacyClientSigningScopeUsesCredsNotMutableClientFields(t *testing.T) {
	c, err := New("https://tls-cn-beijing.volces.com", "cn-beijing", "legacy", "ak-1", "sk-1", "token-1", 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Set the public Creds scope to values that differ from the client fields.
	c.Creds.Region = "ap-southeast-1"
	c.Creds.Service = "CustomSvc"
	// Pollute the mutable client fields; static signing must ignore them.
	c.Region = "us-east-1"
	c.Service = "Polluted"

	c.requestSigner = fixedTimeSigner(time.Date(2024, time.March, 14, 15, 9, 26, 0, time.UTC))
	rt := &captureRoundTripper{}
	c.HTTP = &http.Client{Transport: rt, Timeout: time.Second}

	if _, err := c.Do(context.Background(), http.MethodPost, "/DescribeProjects", nil, nil, nil); err != nil {
		t.Fatalf("Do: %v", err)
	}

	reqs := rt.snapshot()
	if len(reqs) != 1 {
		t.Fatalf("captured %d requests, want 1", len(reqs))
	}
	authz := reqs[0].Header.Get("Authorization")
	if !strings.Contains(authz, "/ap-southeast-1/CustomSvc/request") {
		t.Fatalf("scope must use c.Creds Region/Service: %q", authz)
	}
	if strings.Contains(authz, "/us-east-1/") || strings.Contains(authz, "/Polluted/") {
		t.Fatalf("scope must not reflect polluted client fields: %q", authz)
	}
}

// TestLegacyDirectClientWithCredsCanSign proves that a Client constructed
// directly (without New, with a nil provider) can still sign using its public
// Creds field, matching pre-provider behavior.
func TestLegacyDirectClientWithCredsCanSign(t *testing.T) {
	creds := base.Credentials{
		AccessKeyID:     "direct-ak",
		SecretAccessKey: "direct-sk",
		SessionToken:    "direct-token",
		Region:          "cn-beijing",
		Service:         "TLS",
	}
	c := &Client{
		Endpoint: "https://tls-cn-beijing.volces.com",
		Region:   "cn-beijing",
		Service:  "TLS",
		Creds:    creds,
		HTTP:     &http.Client{Transport: &captureRoundTripper{}, Timeout: time.Second},
	}
	// provider is nil (direct construction); Sign must fall back to c.Creds.
	req, _ := http.NewRequest(http.MethodPost, "https://tls-cn-beijing.volces.com/DescribeProjects", nil)
	signed, err := c.Sign(context.Background(), req)
	if err != nil {
		t.Fatalf("Sign on directly-constructed client: %v", err)
	}
	got := signed.Header.Get("Authorization")
	if got == "" {
		t.Fatalf("expected Authorization header, got empty")
	}
	if !strings.Contains(got, "Credential=direct-ak/") {
		t.Fatalf("Authorization must use direct creds: %q", got)
	}
}
