package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth"
	"github.com/volcengine-tls/ve-tls-cli/internal/config"
	"github.com/volcengine-tls/ve-tls-cli/internal/execution"
)

// capturedRequest records the Authorization header, X-Security-Token, and
// PageNumber query param of a request so tests can verify which identity was
// used to sign it and which page was fetched.
type capturedRequest struct {
	authorization string
	securityToken string
	pageNumber    string
}

// newCapturingServer returns a test server that records each request's
// Authorization and X-Security-Token headers and responds 200 with an empty
// JSON body. The returned channel receives one capturedRequest per request.
func newCapturingServer(t *testing.T) (*httptest.Server, <-chan capturedRequest) {
	t.Helper()
	ch := make(chan capturedRequest, 16)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cr := capturedRequest{
			authorization: r.Header.Get("Authorization"),
			securityToken: r.Header.Get("X-Security-Token"),
		}
		select {
		case ch <- cr:
		default:
		}
		w.Header().Set("x-tls-requestid", "captured-"+r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(server.Close)
	return server, ch
}

// fnProvider is a fakeProvider variant that lets tests control the returned
// value per call via a function.
type fnProvider struct {
	retrieveFn func() (auth.Value, error)
	calls      int32
}

func (p *fnProvider) Retrieve(context.Context) (auth.Value, error) {
	atomic.AddInt32(&p.calls, 1)
	if p.retrieveFn != nil {
		return p.retrieveFn()
	}
	return auth.Value{}, nil
}

// TestConsoleModeUsesConsoleIdentityWithEnvironmentAKPresent proves that when
// environment AK/SK are present, a console-login profile still signs requests
// with the console provider's STS credentials, never the environment AK.
func TestConsoleModeUsesConsoleIdentityWithEnvironmentAKPresent(t *testing.T) {
	clearAuthTestEnv(t)
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "env-ak-must-be-ignored")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "env-sk-must-be-ignored")

	server, ch := newCapturingServer(t)

	cfg := config.Config{
		Version: 1,
		Profiles: map[string]config.Profile{
			"console": {
				Mode:     config.AuthModeConsoleLogin,
				Region:   "cn-beijing",
				Endpoint: server.URL,
			},
		},
	}
	provider := &fakeProvider{value: auth.Value{
		AccessKeyID:     "AKLTconsole-identity",
		SecretAccessKey: "console-secret",
		SessionToken:    "console-session-token",
		ProviderName:    "console-login",
		ExpiresAt:       time.Now().Add(time.Hour),
	}}
	factory := &fakeAuthFactory{consoleProvider: provider}
	ctx := newTestContext(t, cfg, "/tmp/test-config.json")
	ctx.Profile = "console"
	ctx.authFactory = factory

	if _, err := ctx.DoRaw("GET", "/DescribeProjects", nil, nil, nil); err != nil {
		t.Fatalf("DoRaw: %v", err)
	}
	cr := <-ch
	if cr.authorization == "" {
		t.Fatalf("expected Authorization header to be set")
	}
	if !strings.Contains(cr.authorization, "AKLTconsole-identity") {
		t.Fatalf("expected console identity in Authorization, got %q", cr.authorization)
	}
	if strings.Contains(cr.authorization, "env-ak-must-be-ignored") {
		t.Fatalf("environment AK leaked into Authorization: %q", cr.authorization)
	}
	if cr.securityToken == "" {
		t.Fatalf("expected X-Security-Token from console STS credentials")
	}
	if factory.consoleCalls != 1 {
		t.Fatalf("expected console factory to be called once, got %d", factory.consoleCalls)
	}
	if provider.calls != 1 {
		t.Fatalf("expected provider Retrieve to be called once, got %d", provider.calls)
	}
}

// TestSSOModeUsesSSOIdentityWithEnvironmentAKPresent proves that when
// environment AK/SK are present, an sso profile still signs requests with the
// SSO provider's STS credentials, never the environment AK.
func TestSSOModeUsesSSOIdentityWithEnvironmentAKPresent(t *testing.T) {
	clearAuthTestEnv(t)
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "env-ak-must-be-ignored")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "env-sk-must-be-ignored")

	server, ch := newCapturingServer(t)

	cfg := config.Config{
		Version: 1,
		Profiles: map[string]config.Profile{
			"sso": {
				Mode:     config.AuthModeSSO,
				Region:   "cn-beijing",
				Endpoint: server.URL,
			},
		},
	}
	provider := &fakeProvider{value: auth.Value{
		AccessKeyID:     "AKLTsso-identity",
		SecretAccessKey: "sso-secret",
		SessionToken:    "sso-session-token",
		ProviderName:    "sso",
		ExpiresAt:       time.Now().Add(time.Hour),
	}}
	factory := &fakeAuthFactory{ssoProvider: provider}
	ctx := newTestContext(t, cfg, "/tmp/test-config.json")
	ctx.Profile = "sso"
	ctx.authFactory = factory

	if _, err := ctx.DoRaw("GET", "/DescribeProjects", nil, nil, nil); err != nil {
		t.Fatalf("DoRaw: %v", err)
	}
	cr := <-ch
	if cr.authorization == "" {
		t.Fatalf("expected Authorization header to be set")
	}
	if !strings.Contains(cr.authorization, "AKLTsso-identity") {
		t.Fatalf("expected sso identity in Authorization, got %q", cr.authorization)
	}
	if strings.Contains(cr.authorization, "env-ak-must-be-ignored") {
		t.Fatalf("environment AK leaked into Authorization: %q", cr.authorization)
	}
	if factory.ssoCalls != 1 {
		t.Fatalf("expected sso factory to be called once, got %d", factory.ssoCalls)
	}
	if provider.calls != 1 {
		t.Fatalf("expected provider Retrieve to be called once, got %d", provider.calls)
	}
}

// TestDynamicFailureDoesNotSendOrFallback proves that when the dynamic
// provider's Retrieve fails, no HTTP request is sent and the CLI does not fall
// back to environment AK/SK, even though they are present.
func TestDynamicFailureDoesNotSendOrFallback(t *testing.T) {
	clearAuthTestEnv(t)
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "env-ak-fallback")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "env-sk-fallback")

	requestCount := int32(0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := config.Config{
		Version: 1,
		Profiles: map[string]config.Profile{
			"sso": {
				Mode:     config.AuthModeSSO,
				Region:   "cn-beijing",
				Endpoint: server.URL,
			},
		},
	}
	provider := &fakeProvider{err: &auth.Error{Kind: auth.ReauthRequired, Description: "sso token cache missing; run: volclog sso login"}}
	factory := &fakeAuthFactory{ssoProvider: provider}
	ctx := newTestContext(t, cfg, "/tmp/test-config.json")
	ctx.Profile = "sso"
	ctx.authFactory = factory

	_, err := ctx.DoRaw("GET", "/DescribeProjects", nil, nil, nil)
	if err == nil {
		t.Fatalf("expected DoRaw to fail when provider Retrieve errors")
	}
	if atomic.LoadInt32(&requestCount) != 0 {
		t.Fatalf("expected no HTTP requests after Retrieve failure, got %d", requestCount)
	}
	if factory.ssoCalls != 1 {
		t.Fatalf("expected factory to be called once, got %d", factory.ssoCalls)
	}
	if provider.calls != 1 {
		t.Fatalf("expected provider Retrieve to be called once, got %d", provider.calls)
	}
}

// TestPaginationRefreshUsesNewTokenOnNextRequest proves that the unified
// shortcut executor re-invokes Retrieve on every page, so a refreshed token is
// picked up on the next page request. The first page is signed with token A;
// after the provider rotates to token B, the second page is signed with token B.
func TestPaginationRefreshUsesNewTokenOnNextRequest(t *testing.T) {
	clearAuthTestEnv(t)

	var mu sync.Mutex
	var captured []capturedRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		captured = append(captured, capturedRequest{
			authorization: r.Header.Get("Authorization"),
			securityToken: r.Header.Get("X-Security-Token"),
			pageNumber:    r.URL.Query().Get("PageNumber"),
		})
		mu.Unlock()
		w.Header().Set("x-tls-requestid", "page-"+r.URL.Query().Get("PageNumber"))
		w.WriteHeader(http.StatusOK)
		// Return one project per page with Total=2 so the loop fetches exactly
		// two pages before stopping.
		_, _ = w.Write([]byte(`{"Projects":[{"id":` + r.URL.Query().Get("PageNumber") + `}],"Total":2}`))
	}))
	t.Cleanup(server.Close)

	cfg := config.Config{
		Version: 1,
		Profiles: map[string]config.Profile{
			"sso": {
				Mode:     config.AuthModeSSO,
				Region:   "cn-beijing",
				Endpoint: server.URL,
			},
		},
	}

	tokenA := auth.Value{
		AccessKeyID:     "AKLTtoken-a",
		SecretAccessKey: "secret-a",
		SessionToken:    "token-a-session",
		ProviderName:    "sso",
		ExpiresAt:       time.Now().Add(time.Hour),
	}
	tokenB := auth.Value{
		AccessKeyID:     "AKLTtoken-b",
		SecretAccessKey: "secret-b",
		SessionToken:    "token-b-session",
		ProviderName:    "sso",
		ExpiresAt:       time.Now().Add(time.Hour),
	}

	provider := &fnProvider{}
	provider.retrieveFn = func() (auth.Value, error) {
		if atomic.LoadInt32(&provider.calls) == 1 {
			return tokenA, nil
		}
		return tokenB, nil
	}
	factory := &fakeAuthFactory{ssoProvider: provider}
	ctx := newTestContext(t, cfg, "/tmp/test-config.json")
	ctx.Profile = "sso"
	ctx.authFactory = factory

	operation, ok := loadToolOperation("project.describe-projects")
	if !ok {
		t.Fatal("project.describe-projects operation is unavailable")
	}
	codecs, err := newToolExecutionCodecRegistry(ctx)
	if err != nil {
		t.Fatalf("create execution codecs: %v", err)
	}
	result, err := execution.NewExecutor(
		newContextExecutionTransport(ctx),
		codecs,
	).Execute(context.Background(), execution.Invocation{
		Operation: operation,
		Input: execution.Input{
			Query: map[string]any{"PageSize": "1"},
			Body: execution.Payload{
				JSON:    map[string]any{},
				Format:  execution.BodyFormatJSON,
				Present: true,
			},
		},
		Options: execution.Options{
			PageAll:          true,
			ValidationPolicy: execution.ValidationCallerLegacy,
		},
	})
	if err != nil {
		t.Fatalf("execute project.describe-projects page-all: %v", err)
	}
	out, ok := result.Data.(map[string]any)
	if !ok {
		t.Fatalf("page-all result has type %T, want object", result.Data)
	}
	projects, _ := out["Projects"].([]any)
	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(projects))
	}

	// Wait for exactly two captured requests with a timeout.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(captured)
		mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(captured) != 2 {
		t.Fatalf("expected exactly 2 paginated requests, got %d", len(captured))
	}

	// First page: PageNumber=1, signed with token A.
	if captured[0].pageNumber != "1" {
		t.Fatalf("first request PageNumber=%q, want 1", captured[0].pageNumber)
	}
	if !strings.Contains(captured[0].authorization, "AKLTtoken-a") {
		t.Fatalf("first page expected token A identity, got %q", captured[0].authorization)
	}
	if captured[0].securityToken != "token-a-session" {
		t.Fatalf("first page X-Security-Token=%q, want token-a-session", captured[0].securityToken)
	}
	// Second page: PageNumber=2, signed with token B.
	if captured[1].pageNumber != "2" {
		t.Fatalf("second request PageNumber=%q, want 2", captured[1].pageNumber)
	}
	if !strings.Contains(captured[1].authorization, "AKLTtoken-b") {
		t.Fatalf("second page expected token B identity, got %q", captured[1].authorization)
	}
	if captured[1].securityToken != "token-b-session" {
		t.Fatalf("second page X-Security-Token=%q, want token-b-session", captured[1].securityToken)
	}

	if atomic.LoadInt32(&provider.calls) != 2 {
		t.Fatalf("expected Retrieve to be called twice (once per page), got %d", provider.calls)
	}
}

// TestDynamicRequestsKeepTLSServiceAndResolvedRegion proves that dynamic
// requests are signed with the TLS service and the resolved region, even when
// the provider returns credentials from a different source.
func TestDynamicRequestsKeepTLSServiceAndResolvedRegion(t *testing.T) {
	clearAuthTestEnv(t)
	t.Setenv("VOLCENGINE_REGION", "cn-shanghai")
	server, ch := newCapturingServer(t)

	cfg := config.Config{
		Version: 1,
		Profiles: map[string]config.Profile{
			"sso": {
				Mode:     config.AuthModeSSO,
				Endpoint: server.URL,
			},
		},
	}
	provider := &fakeProvider{value: auth.Value{
		AccessKeyID:     "AKLTdynamic",
		SecretAccessKey: "dynamic-secret",
		SessionToken:    "dynamic-token",
		ProviderName:    "sso",
		ExpiresAt:       time.Now().Add(time.Hour),
	}}
	factory := &fakeAuthFactory{ssoProvider: provider}
	ctx := newTestContext(t, cfg, "/tmp/test-config.json")
	ctx.Profile = "sso"
	ctx.authFactory = factory

	if _, err := ctx.DoRaw("GET", "/DescribeProjects", nil, nil, nil); err != nil {
		t.Fatalf("DoRaw: %v", err)
	}
	cr := <-ch
	// The Authorization header's credential scope must contain the resolved
	// region (cn-shanghai from env) and the TLS service.
	if !strings.Contains(cr.authorization, "cn-shanghai") {
		t.Fatalf("expected resolved region cn-shanghai in Authorization, got %q", cr.authorization)
	}
	if !strings.Contains(cr.authorization, "tls") {
		t.Fatalf("expected TLS service in Authorization scope, got %q", cr.authorization)
	}
}

// TestStaticRequestsRemainByteEquivalent proves that static AK/SK requests
// produce byte-identical output to the legacy static path. It uses the
// existing static signature server to verify the exact Authorization header.
func TestStaticRequestsRemainByteEquivalent(t *testing.T) {
	clearAuthTestEnv(t)
	const (
		accessKey = "AKLTstatic-byte-equiv"
		secretKey = "static-secret-key"
	)
	server, signatureResult := newLegacyStaticSignatureServer(accessKey, secretKey)
	defer server.Close()

	cfg := config.Config{
		Version: 1,
		Profiles: map[string]config.Profile{
			"static": {
				Mode:            config.AuthModeAK,
				AccessKeyID:     accessKey,
				SecretAccessKey: secretKey,
				Region:          "cn-beijing",
				Endpoint:        server.URL,
			},
		},
	}
	ctx := newTestContext(t, cfg, "/tmp/test-config.json")
	ctx.Profile = "static"

	resp, err := ctx.DoRaw("GET", "/", nil, nil, nil)
	if err != nil {
		t.Fatalf("DoRaw: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if err := <-signatureResult; err != nil {
		t.Fatalf("static signature verification failed: %v", err)
	}
}
