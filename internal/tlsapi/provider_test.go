package tlsapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth"
	"github.com/volcengine/volc-sdk-golang/base"
)

// fakeProvider returns a fresh set of credentials on every Retrieve call so tests
// can prove the client consults the provider before each signature.
type fakeProvider struct {
	mu       sync.Mutex
	values   []auth.Value
	calls    int32
	retrieve func(ctx context.Context) (auth.Value, error)
}

func (p *fakeProvider) Retrieve(ctx context.Context) (auth.Value, error) {
	atomic.AddInt32(&p.calls, 1)
	if p.retrieve != nil {
		return p.retrieve(ctx)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.values) == 0 {
		return auth.Value{}, errors.New("no values queued")
	}
	v := p.values[0]
	p.values = p.values[1:]
	return v, nil
}

func (p *fakeProvider) callCount() int32 { return atomic.LoadInt32(&p.calls) }

type captureRoundTripper struct {
	mu       sync.Mutex
	requests []*http.Request
}

func (rt *captureRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	rt.mu.Lock()
	rt.requests = append(rt.requests, clone)
	rt.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       http.NoBody,
	}, nil
}

func (rt *captureRoundTripper) snapshot() []*http.Request {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	out := make([]*http.Request, len(rt.requests))
	copy(out, rt.requests)
	return out
}

func newProviderClient(t *testing.T, provider auth.Provider) *Client {
	t.Helper()
	c, err := NewWithProvider("https://tls-cn-beijing.volces.com", "cn-beijing", provider, time.Second)
	if err != nil {
		t.Fatalf("NewWithProvider: %v", err)
	}
	return c
}

func TestClientRetrievesProviderBeforeEverySignature(t *testing.T) {
	provider := &fakeProvider{
		values: []auth.Value{
			{AccessKeyID: "ak-1", SecretAccessKey: "sk-1", SessionToken: "token-1"},
			{AccessKeyID: "ak-2", SecretAccessKey: "sk-2", SessionToken: "token-2"},
		},
	}
	c := newProviderClient(t, provider)
	rt := &captureRoundTripper{}
	c.HTTP = &http.Client{Transport: rt, Timeout: time.Second}

	if _, err := c.Do(context.Background(), http.MethodPost, "/DescribeProjects", nil, nil, nil); err != nil {
		t.Fatalf("first Do: %v", err)
	}
	if _, err := c.Do(context.Background(), http.MethodPost, "/DescribeProjects", nil, nil, nil); err != nil {
		t.Fatalf("second Do: %v", err)
	}

	if got := provider.callCount(); got != 2 {
		t.Fatalf("provider retrieve calls = %d, want 2 (one per request)", got)
	}

	reqs := rt.snapshot()
	if len(reqs) != 2 {
		t.Fatalf("captured %d requests, want 2", len(reqs))
	}

	auth1 := reqs[0].Header.Get("Authorization")
	auth2 := reqs[1].Header.Get("Authorization")
	if auth1 == "" || auth2 == "" {
		t.Fatalf("missing Authorization header: req1=%q req2=%q", auth1, auth2)
	}
	if !strings.Contains(auth1, "Credential=ak-1/") {
		t.Fatalf("first request identity not from provider: %q", auth1)
	}
	if !strings.Contains(auth2, "Credential=ak-2/") {
		t.Fatalf("second request identity not from provider: %q", auth2)
	}
	if auth1 == auth2 {
		t.Fatalf("two requests used identical credentials; provider was not consulted per-request")
	}
	if got, want := reqs[0].Header.Get("X-Security-Token"), "token-1"; got != want {
		t.Fatalf("first X-Security-Token = %q, want %q", got, want)
	}
	if got, want := reqs[1].Header.Get("X-Security-Token"), "token-2"; got != want {
		t.Fatalf("second X-Security-Token = %q, want %q", got, want)
	}
}

func TestProviderFailurePreventsHTTPRequest(t *testing.T) {
	provider := &fakeProvider{
		retrieve: func(context.Context) (auth.Value, error) {
			return auth.Value{}, errors.New("provider unavailable")
		},
	}
	c := newProviderClient(t, provider)
	rt := &captureRoundTripper{}
	c.HTTP = &http.Client{Transport: rt, Timeout: time.Second}

	_, err := c.Do(context.Background(), http.MethodPost, "/DescribeProjects", nil, nil, nil)
	if err == nil {
		t.Fatalf("expected error when provider fails, got nil")
	}
	if !strings.Contains(err.Error(), "provider") {
		t.Fatalf("error should mention provider failure: %v", err)
	}

	if got := len(rt.snapshot()); got != 0 {
		t.Fatalf("round tripper called %d times after provider failure, want 0", got)
	}
	if got := provider.callCount(); got != 1 {
		t.Fatalf("provider retrieve calls = %d, want 1", got)
	}
}

func TestConcurrentRequestsDoNotShareMutableCredentials(t *testing.T) {
	const n = 16
	values := make([]auth.Value, 0, n)
	for i := 0; i < n; i++ {
		values = append(values, auth.Value{
			AccessKeyID:     fmt.Sprintf("ak-%02d", i),
			SecretAccessKey: fmt.Sprintf("sk-%02d", i),
			SessionToken:    fmt.Sprintf("token-%02d", i),
		})
	}
	provider := &fakeProvider{values: values}
	c := newProviderClient(t, provider)
	rt := &captureRoundTripper{}
	c.HTTP = &http.Client{Transport: rt, Timeout: 5 * time.Second}

	var (
		wg    sync.WaitGroup
		errMu sync.Mutex
		errs  []error
	)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.Do(context.Background(), http.MethodPost, "/DescribeProjects", nil, nil, nil)
			errMu.Lock()
			errs = append(errs, err)
			errMu.Unlock()
		}()
	}
	wg.Wait()

	// No Do call may fail; provider must serve every concurrent request.
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Do[%d] returned error: %v", i, err)
		}
	}

	reqs := rt.snapshot()
	if got := len(reqs); got != n {
		t.Fatalf("captured %d requests, want %d", got, n)
	}
	if got := provider.callCount(); got != n {
		t.Fatalf("provider retrieve calls = %d, want %d (one per concurrent request)", got, n)
	}

	// Creds field on the client must remain the zero value; dynamic signing must
	// never write back shared mutable credentials.
	if c.Creds.AccessKeyID != "" || c.Creds.SecretAccessKey != "" || c.Creds.SessionToken != "" {
		t.Fatalf("client shared Creds mutated by dynamic signing: %+v", c.Creds)
	}

	// Each request must carry a (ak, token) pair whose numeric suffixes match, and
	// every pair must appear exactly once. This catches implementations that share
	// a single mutable credentials slot guarded by a lock/clear: a shared slot
	// would either duplicate one pair or drop others, or mismatch ak/token suffixes.
	type pair struct{ ak, token string }
	seen := make(map[pair]int, n)
	for _, req := range reqs {
		ak := parseCredentialAK(req.Header.Get("Authorization"))
		token := req.Header.Get("X-Security-Token")
		if ak == "" {
			t.Fatalf("request missing Authorization AK: %q", req.Header.Get("Authorization"))
		}
		if token == "" {
			t.Fatalf("request %s missing X-Security-Token", ak)
		}
		// ak is "ak-NN", token is "token-NN"; the suffixes must agree.
		akSuffix := strings.TrimPrefix(ak, "ak-")
		tokenSuffix := strings.TrimPrefix(token, "token-")
		if akSuffix != tokenSuffix {
			t.Fatalf("ak/token suffix mismatch for request: ak=%q token=%q", ak, token)
		}
		seen[pair{ak, token}]++
	}
	if got := len(seen); got != n {
		t.Fatalf("saw %d unique (ak,token) pairs, want %d", got, n)
	}
	for p, count := range seen {
		if count != 1 {
			t.Fatalf("pair %+v appeared %d times, want exactly once", p, count)
		}
	}
}

// parseCredentialAK extracts the AccessKeyID from a Volcengine Authorization
// header of the form "HMAC-SHA256 Credential=<ak>/<date>/<region>/<svc>/request, ...".
func parseCredentialAK(authorization string) string {
	const prefix = "Credential="
	idx := strings.Index(authorization, prefix)
	if idx < 0 {
		return ""
	}
	rest := authorization[idx+len(prefix):]
	if slash := strings.IndexByte(rest, '/'); slash >= 0 {
		rest = rest[:slash]
	}
	return rest
}

func TestDynamicSignedRequestUsesSecurityTokenAndTLSService(t *testing.T) {
	provider := &fakeProvider{
		values: []auth.Value{
			{AccessKeyID: "dyn-ak", SecretAccessKey: "dyn-sk", SessionToken: "dyn-token"},
		},
	}
	c := newProviderClient(t, provider)
	rt := &captureRoundTripper{}
	c.HTTP = &http.Client{Transport: rt, Timeout: time.Second}

	// Deliberately pollute the mutable Service field. Sign must ignore it and use
	// the hardcoded "TLS" service scope; this test no longer asserts c.Service.
	c.Service = "NOT-TLS"

	if _, err := c.Do(context.Background(), http.MethodPost, "/DescribeProjects", nil, nil, nil); err != nil {
		t.Fatalf("Do: %v", err)
	}

	reqs := rt.snapshot()
	if len(reqs) != 1 {
		t.Fatalf("captured %d requests, want 1", len(reqs))
	}
	req := reqs[0]

	if got, want := req.Header.Get("X-Security-Token"), "dyn-token"; got != want {
		t.Fatalf("X-Security-Token = %q, want %q", got, want)
	}
	authz := req.Header.Get("Authorization")
	// Scope must be the hardcoded /TLS/request even though c.Service was polluted.
	if !strings.Contains(authz, "/TLS/request") {
		t.Fatalf("credential scope must use hardcoded Service=TLS: %q", authz)
	}
	if strings.Contains(authz, "/NOT-TLS/") {
		t.Fatalf("credential scope must not reflect polluted c.Service: %q", authz)
	}
	if !strings.Contains(authz, "Credential=dyn-ak/") {
		t.Fatalf("Authorization must use provider identity: %q", authz)
	}
}

func TestLegacyNewStillUsesOriginalStaticResolution(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "env-ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "env-sk")
	t.Setenv("VOLCENGINE_TOKEN", "env-token")

	c, err := New("https://tls-cn-beijing.volces.com", "cn-beijing", "legacy", "", "", "", 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Static resolution must still populate the observable Creds field exactly as before.
	if c.Creds.AccessKeyID != "env-ak" || c.Creds.SecretAccessKey != "env-sk" || c.Creds.SessionToken != "env-token" {
		t.Fatalf("static Creds not resolved from environment: %+v", c.Creds)
	}
	if c.Creds.Service != "TLS" || c.Creds.Region != "cn-beijing" {
		t.Fatalf("static Creds scope changed: %+v", c.Creds)
	}
	// Legacy New does not wrap credentials in a provider; Sign reads c.Creds
	// directly so mutating the public field affects subsequent signatures.
	if c.provider != nil {
		t.Fatalf("legacy New must not set a provider; got %T", c.provider)
	}

	// Signing must use the current c.Creds value, not a construction-time snapshot.
	c.Creds.AccessKeyID = "mutated-ak"
	req, _ := http.NewRequest(http.MethodPost, "https://tls-cn-beijing.volces.com/DescribeProjects", nil)
	signed, err := c.Sign(context.Background(), req)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if got := signed.Header.Get("Authorization"); !strings.Contains(got, "Credential=mutated-ak/") {
		t.Fatalf("Sign must use current c.Creds, not a snapshot: %q", got)
	}
}

func TestLegacyClientDoSigningGoldenAtFixedTime(t *testing.T) {
	const token = "legacy-session-token"
	c, err := New("https://tls-cn-beijing.volces.com/", "cn-beijing", "legacy", "legacy-ak", "legacy-sk", token, 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Inject the package-private fixed-time signer seam; production default is unchanged.
	c.requestSigner = fixedTimeSigner(time.Date(2024, time.March, 14, 15, 9, 26, 0, time.UTC))

	rt := &captureRoundTripper{}
	c.HTTP = &http.Client{Transport: rt, Timeout: time.Second}

	body := []byte(`{"TopicId":"topic-legacy"}`)
	if _, err := c.Do(context.Background(), http.MethodPost, "/PutLogs", map[string]string{"topic_id": "topic-legacy"}, nil, body); err != nil {
		t.Fatalf("Do: %v", err)
	}

	reqs := rt.snapshot()
	if len(reqs) != 1 {
		t.Fatalf("captured %d requests, want 1", len(reqs))
	}
	req := reqs[0]

	// Hardcoded golden captured from the real Do static chain at the fixed time
	// above. Do injects Content-Md5 for a non-empty body, so this golden is NOT
	// byte-identical to the Task 1 no-Content-Md5 fixture; it is the literal
	// output of New(...).Do(...) with the package-private fixed-time signer.
	const wantAuthorization = "HMAC-SHA256 Credential=legacy-ak/20240314/cn-beijing/TLS/request, SignedHeaders=content-md5;content-type;host;x-content-sha256;x-date;x-security-token;x-tls-apiversion, Signature=8ff2ab8492549a7abf70fbf1ca4f80dd1d5ab3b60f9d257ae66f8238f639e936"
	const wantContentSHA256 = "f8e2d052b7bd4663968f9c7f3d0fd21150949d4c63e1ee4e8ffbb3daf4b8b9f6"
	const wantXSecurityToken = "legacy-session-token"
	const wantXDate = "20240314T150926Z"
	const wantCredentialScope = "Credential=legacy-ak/20240314/cn-beijing/TLS/request"

	if got := req.Header.Get("Authorization"); got != wantAuthorization {
		t.Fatalf("Authorization mismatch:\n got: %s\nwant: %s", got, wantAuthorization)
	}
	if got := req.Header.Get("X-Content-Sha256"); got != wantContentSHA256 {
		t.Fatalf("X-Content-Sha256 mismatch:\n got: %s\nwant: %s", got, wantContentSHA256)
	}
	if got := req.Header.Get("X-Security-Token"); got != wantXSecurityToken {
		t.Fatalf("X-Security-Token mismatch:\n got: %s\nwant: %s", got, wantXSecurityToken)
	}
	if got := req.Header.Get("X-Date"); got != wantXDate {
		t.Fatalf("X-Date mismatch:\n got: %s\nwant: %s", got, wantXDate)
	}
	if got := req.Header.Get("Authorization"); !strings.Contains(got, wantCredentialScope) {
		t.Fatalf("credential scope %q missing from Authorization: %q", wantCredentialScope, got)
	}
}

func TestNewWithProviderValidatesInputs(t *testing.T) {
	provider := &fakeProvider{values: []auth.Value{{AccessKeyID: "ak", SecretAccessKey: "sk"}}}

	for _, tc := range []struct {
		name     string
		endpoint string
		region   string
		provider auth.Provider
		wantErr  string
	}{
		{name: "empty endpoint", endpoint: "", region: "cn-beijing", provider: provider, wantErr: "empty endpoint"},
		{name: "empty region", endpoint: "https://tls-cn-beijing.volces.com", region: "", provider: provider, wantErr: "empty region"},
		{name: "nil provider", endpoint: "https://tls-cn-beijing.volces.com", region: "cn-beijing", provider: nil, wantErr: "nil provider"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewWithProvider(tc.endpoint, tc.region, tc.provider, 0)
			if err == nil {
				t.Fatalf("expected error %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %q, want to contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestNewWithProviderDefaultsTimeoutAndNormalizesEndpoint(t *testing.T) {
	provider := &fakeProvider{values: []auth.Value{{AccessKeyID: "ak", SecretAccessKey: "sk"}}}
	c, err := NewWithProvider(" tls-cn-beijing.volces.com/// ", " cn-beijing ", provider, 0)
	if err != nil {
		t.Fatalf("NewWithProvider: %v", err)
	}
	if c.Endpoint != "https://tls-cn-beijing.volces.com" {
		t.Fatalf("endpoint = %q, want normalized", c.Endpoint)
	}
	if c.Timeout != 60*time.Second || c.HTTP.Timeout != 60*time.Second {
		t.Fatalf("timeouts = (%s, %s), want 60s default", c.Timeout, c.HTTP.Timeout)
	}
	if c.Service != "TLS" || c.Region != "cn-beijing" {
		t.Fatalf("scope inputs changed: service=%q region=%q", c.Service, c.Region)
	}
}

// TestSignMissingStaticCredentialsFailsClosed ensures a static client (no
// provider) with empty Creds fails closed instead of producing an unsigned
// request. Renamed from TestSignWithoutProviderFailsClosed to clarify that the
// failure is about missing static credentials, not a missing provider per se.
func TestSignMissingStaticCredentialsFailsClosed(t *testing.T) {
	c := &Client{Region: "cn-beijing", Service: "TLS"}
	req, _ := http.NewRequest(http.MethodPost, "https://tls-cn-beijing.volces.com/DescribeProjects", nil)
	if _, err := c.Sign(context.Background(), req); err == nil {
		t.Fatalf("expected error when static credentials are missing, got nil")
	}
}

func TestSignRejectsInvalidProviderValue(t *testing.T) {
	provider := &fakeProvider{
		retrieve: func(context.Context) (auth.Value, error) {
			return auth.Value{AccessKeyID: "only-ak"}, nil
		},
	}
	c := newProviderClient(t, provider)
	req, _ := http.NewRequest(http.MethodPost, "https://tls-cn-beijing.volces.com/DescribeProjects", nil)
	if _, err := c.Sign(context.Background(), req); err == nil {
		t.Fatalf("expected validation error for incomplete credentials, got nil")
	}
}

// TestNewWithProviderRejectsTypedNilProvider ensures a typed-nil provider
// (e.g. var p *fakeProvider = nil) is rejected at construction time rather than
// panicking later during Retrieve.
func TestNewWithProviderRejectsTypedNilProvider(t *testing.T) {
	var p *fakeProvider = nil
	_, err := NewWithProvider("https://tls-cn-beijing.volces.com", "cn-beijing", p, 0)
	if err == nil {
		t.Fatalf("expected error for typed-nil provider, got nil")
	}
	if !strings.Contains(err.Error(), "nil provider") {
		t.Fatalf("error = %q, want to contain 'nil provider'", err.Error())
	}
}

// TestSignWithTypedNilProviderFailsClosed ensures Sign fail-closes instead of
// panicking if a typed-nil provider is assigned to the client after construction.
func TestSignWithTypedNilProviderFailsClosed(t *testing.T) {
	c := &Client{Region: "cn-beijing", provider: (*fakeProvider)(nil)}
	req, _ := http.NewRequest(http.MethodPost, "https://tls-cn-beijing.volces.com/DescribeProjects", nil)
	if _, err := c.Sign(context.Background(), req); err == nil {
		t.Fatalf("expected error for typed-nil provider in Sign, got nil")
	}
}

// TestSignNilRequestFailsClosed ensures Sign(ctx, nil) returns an error instead
// of panicking inside the signer.
func TestSignNilRequestFailsClosed(t *testing.T) {
	provider := &fakeProvider{values: []auth.Value{{AccessKeyID: "ak", SecretAccessKey: "sk"}}}
	c := newProviderClient(t, provider)
	if _, err := c.Sign(context.Background(), nil); err == nil {
		t.Fatalf("expected error for nil request, got nil")
	}
}

// TestSignNilSignerResultFailsClosed ensures a signer that returns nil does not
// cause a nil-pointer dereference when checking the Authorization header.
func TestSignNilSignerResultFailsClosed(t *testing.T) {
	provider := &fakeProvider{values: []auth.Value{{AccessKeyID: "ak", SecretAccessKey: "sk"}}}
	c := newProviderClient(t, provider)
	c.requestSigner = func(base.Credentials, *http.Request) *http.Request { return nil }
	req, _ := http.NewRequest(http.MethodPost, "https://tls-cn-beijing.volces.com/DescribeProjects", nil)
	if _, err := c.Sign(context.Background(), req); err == nil {
		t.Fatalf("expected error when signer returns nil, got nil")
	}
}

// TestSignNilSignerFallsBackToDefault ensures a nil requestSigner falls back to
// the production default and successfully signs the request.
func TestSignNilSignerFallsBackToDefault(t *testing.T) {
	provider := &fakeProvider{values: []auth.Value{{AccessKeyID: "ak", SecretAccessKey: "sk"}}}
	c := newProviderClient(t, provider)
	c.requestSigner = nil
	req, _ := http.NewRequest(http.MethodPost, "https://tls-cn-beijing.volces.com/DescribeProjects", nil)
	signed, err := c.Sign(context.Background(), req)
	if err != nil {
		t.Fatalf("Sign with nil signer: %v", err)
	}
	if got := signed.Header.Get("Authorization"); got == "" {
		t.Fatalf("expected Authorization from default signer, got empty")
	}
}

// TestSignMissingAuthorizationFailsClosed ensures a signer that returns a
// request without an Authorization header produces a clear error.
func TestSignMissingAuthorizationFailsClosed(t *testing.T) {
	provider := &fakeProvider{values: []auth.Value{{AccessKeyID: "ak", SecretAccessKey: "sk"}}}
	c := newProviderClient(t, provider)
	c.requestSigner = func(_ base.Credentials, req *http.Request) *http.Request { return req }
	req, _ := http.NewRequest(http.MethodPost, "https://tls-cn-beijing.volces.com/DescribeProjects", nil)
	if _, err := c.Sign(context.Background(), req); err == nil {
		t.Fatalf("expected error for missing Authorization, got nil")
	}
}

// TestSignPassesContextToProvider ensures the ctx passed to Sign is forwarded
// verbatim to the provider's Retrieve method.
func TestSignPassesContextToProvider(t *testing.T) {
	type ctxKey struct{}
	ctx := context.WithValue(context.Background(), ctxKey{}, "marker")
	var received context.Context
	provider := &fakeProvider{
		retrieve: func(c context.Context) (auth.Value, error) {
			received = c
			return auth.Value{AccessKeyID: "ak", SecretAccessKey: "sk"}, nil
		},
	}
	c := newProviderClient(t, provider)
	req, _ := http.NewRequest(http.MethodPost, "https://tls-cn-beijing.volces.com/DescribeProjects", nil)
	if _, err := c.Sign(ctx, req); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if received != ctx {
		t.Fatalf("provider received different context")
	}
	if got := received.Value(ctxKey{}); got != "marker" {
		t.Fatalf("provider context value = %v, want 'marker'", got)
	}
}

// TestSignNilHeaderInitializesSafely ensures a request with a nil Header map is
// handled without panicking and still produces a signed request.
func TestSignNilHeaderInitializesSafely(t *testing.T) {
	provider := &fakeProvider{values: []auth.Value{{AccessKeyID: "ak", SecretAccessKey: "sk"}}}
	c := newProviderClient(t, provider)
	req, _ := http.NewRequest(http.MethodPost, "https://tls-cn-beijing.volces.com/DescribeProjects", nil)
	req.Header = nil
	signed, err := c.Sign(context.Background(), req)
	if err != nil {
		t.Fatalf("Sign with nil header: %v", err)
	}
	if signed.Header == nil {
		t.Fatalf("expected Header to be initialized, got nil")
	}
	if got := signed.Header.Get("Authorization"); got == "" {
		t.Fatalf("expected Authorization header, got empty")
	}
}

// TestSignClearsStaleSecurityToken ensures that re-signing the same request with
// credentials that have no token removes any X-Security-Token left by a previous
// signature.
func TestSignClearsStaleSecurityToken(t *testing.T) {
	provider := &fakeProvider{
		values: []auth.Value{
			{AccessKeyID: "ak-1", SecretAccessKey: "sk-1", SessionToken: "token-1"},
			{AccessKeyID: "ak-2", SecretAccessKey: "sk-2"}, // no token
		},
	}
	c := newProviderClient(t, provider)
	req, _ := http.NewRequest(http.MethodPost, "https://tls-cn-beijing.volces.com/DescribeProjects", nil)

	if _, err := c.Sign(context.Background(), req); err != nil {
		t.Fatalf("first Sign: %v", err)
	}
	if got, want := req.Header.Get("X-Security-Token"), "token-1"; got != want {
		t.Fatalf("first X-Security-Token = %q, want %q", got, want)
	}

	// Second sign without a token; the stale token must be cleared.
	if _, err := c.Sign(context.Background(), req); err != nil {
		t.Fatalf("second Sign: %v", err)
	}
	if got := req.Header.Get("X-Security-Token"); got != "" {
		t.Fatalf("stale X-Security-Token = %q, want empty", got)
	}
}

// functionProvider is a named function type that implements auth.Provider. Its
// nil value is a typed-nil interface, which must be rejected by NewWithProvider
// and fail-closed by Sign rather than panicking or silently falling back to
// static credentials.
type functionProvider func(context.Context) (auth.Value, error)

func (f functionProvider) Retrieve(ctx context.Context) (auth.Value, error) {
	return f(ctx)
}

// TestNewWithProviderRejectsTypedNilFunctionProvider ensures a typed-nil
// function provider (var p functionProvider = nil) is rejected at construction
// time with a clear "nil provider" error and does not panic.
func TestNewWithProviderRejectsTypedNilFunctionProvider(t *testing.T) {
	var p functionProvider = nil
	_, err := NewWithProvider("https://tls-cn-beijing.volces.com", "cn-beijing", p, 0)
	if err == nil {
		t.Fatalf("expected error for typed-nil function provider, got nil")
	}
	if !strings.Contains(err.Error(), "nil provider") {
		t.Fatalf("error = %q, want to contain 'nil provider'", err.Error())
	}
}

// TestSignWithTypedNilProviderAndValidCredsFailsClosed ensures that a typed-nil
// provider assigned after construction fails closed even when c.Creds holds
// valid dormant AK/SK/token. Sign must never silently fall back to static
// credentials for a typed-nil dynamic provider, and the signer/HTTP must not be
// invoked.
func TestSignWithTypedNilProviderAndValidCredsFailsClosed(t *testing.T) {
	c := &Client{
		Region:  "cn-beijing",
		Service: "TLS",
		Creds: base.Credentials{
			AccessKeyID:     "dormant-ak",
			SecretAccessKey: "dormant-sk",
			SessionToken:    "dormant-token",
			Region:          "cn-beijing",
			Service:         "TLS",
		},
		provider: (*fakeProvider)(nil),
	}
	signerCalled := false
	c.requestSigner = func(base.Credentials, *http.Request) *http.Request {
		signerCalled = true
		return nil
	}
	req, _ := http.NewRequest(http.MethodPost, "https://tls-cn-beijing.volces.com/DescribeProjects", nil)
	_, err := c.Sign(context.Background(), req)
	if err == nil {
		t.Fatalf("expected error for typed-nil provider, got nil")
	}
	if !strings.Contains(err.Error(), "nil provider") {
		t.Fatalf("error = %q, want to contain 'nil provider'", err.Error())
	}
	if signerCalled {
		t.Fatalf("signer must not be called for typed-nil provider")
	}
}

// TestLegacyTokenlessCredentialsPreserveCallerSecurityTokenHeader ensures that
// in static/legacy mode with tokenless c.Creds, a caller-supplied
// X-Security-Token header (passed via Do's header map) is preserved, included
// in the signature's SignedHeaders, and sent on the wire. This matches the old
// SDK behavior where c.Creds.Sign never stripped a caller-provided token.
func TestLegacyTokenlessCredentialsPreserveCallerSecurityTokenHeader(t *testing.T) {
	c, err := New("https://tls-cn-beijing.volces.com", "cn-beijing", "legacy", "ak-1", "sk-1", "", 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rt := &captureRoundTripper{}
	c.HTTP = &http.Client{Transport: rt, Timeout: time.Second}

	callerToken := "caller-supplied-token"
	if _, err := c.Do(context.Background(), http.MethodPost, "/DescribeProjects", nil, map[string]string{"X-Security-Token": callerToken}, nil); err != nil {
		t.Fatalf("Do: %v", err)
	}

	reqs := rt.snapshot()
	if len(reqs) != 1 {
		t.Fatalf("captured %d requests, want 1", len(reqs))
	}
	req := reqs[0]
	if got, want := req.Header.Get("X-Security-Token"), callerToken; got != want {
		t.Fatalf("X-Security-Token = %q, want caller-supplied %q", got, want)
	}
	authz := req.Header.Get("Authorization")
	if !strings.Contains(strings.ToLower(authz), "x-security-token") {
		t.Fatalf("Authorization SignedHeaders must include x-security-token: %q", authz)
	}
}
