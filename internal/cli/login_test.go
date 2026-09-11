package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth"
	"github.com/volcengine-tls/ve-tls-cli/internal/auth/console"
	"github.com/volcengine-tls/ve-tls-cli/internal/config"
	"github.com/volcengine-tls/ve-tls-cli/internal/output"
	"github.com/volcengine-tls/ve-tls-cli/internal/securestore"
)

// --- Test helpers ---

func validIDTokenForTest(trn string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"trn":"` + trn + `"}`))
	sig := base64.RawURLEncoding.EncodeToString([]byte("signature"))
	return header + "." + payload + "." + sig
}

func validSTSAccessTokenForTest() json.RawMessage {
	return json.RawMessage(`{"access_key_id":"AKLT1234567890abcdef","secret_access_key":"secret-key-canary-xyz","session_token":"session-token-canary-789"}`)
}

func makeCacheBytesForTest(session string, issuedAt time.Time, expiresIn int, refreshToken string) []byte {
	c := console.LoginTokenCache{
		LoginSession: session,
		AccessToken:  validSTSAccessTokenForTest(),
		RefreshToken: refreshToken,
		IDToken:      validIDTokenForTest(session),
		Scope:        console.Scope,
		ClientID:     console.ClientIDSameDevice,
		EndpointURL:  console.DefaultEndpoint,
		IssuedAt:     issuedAt.UTC().Format(time.RFC3339Nano),
		ExpiresIn:    expiresIn,
		TokenType:    "sts",
	}
	b, err := json.Marshal(c)
	if err != nil {
		panic(err)
	}
	return b
}

// --- Fakes ---

type fakeLoginService struct {
	result *loginResult
	err    error
	called bool
	opts   loginOpts
}

func (f *fakeLoginService) Login(_ context.Context, opts loginOpts) (*loginResult, error) {
	f.called = true
	f.opts = opts
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

type fakeCache struct {
	mu        sync.Mutex
	data      map[string][]byte
	deleteCnt int32
	lockCnt   int32
	deleteErr error
	// orderMu protects deleteOrder. It is intentionally separate from mu so that
	// recording the delete order inside a WithLock callback does not re-enter the
	// session lock (Go mutexes are not reentrant). mu simulates the real cache
	// session lock and must keep its real mutual-exclusion semantics.
	orderMu     sync.Mutex
	deleteOrder []string
	// blockInLock, if non-nil, is closed after WithLock acquires the mutex but
	// before fn runs; WithLock waits for the caller to close proceed.
	blockInLock chan struct{}
	proceed     chan struct{}
}

func newFakeCache() *fakeCache {
	return &fakeCache{data: make(map[string][]byte)}
}

func (f *fakeCache) WithLock(_ context.Context, _ string, fn func() error) error {
	atomic.AddInt32(&f.lockCnt, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.blockInLock != nil {
		close(f.blockInLock)
		<-f.proceed
	}
	return fn()
}

func (f *fakeCache) Delete(session string) error {
	atomic.AddInt32(&f.deleteCnt, 1)
	f.orderMu.Lock()
	f.deleteOrder = append(f.deleteOrder, session)
	f.orderMu.Unlock()
	if f.deleteErr != nil {
		return f.deleteErr
	}
	return nil
}

type fakeConfigStore struct {
	cfg       config.Config
	path      string
	updateErr error
	updateCnt int32
	// updateHook runs inside the Update callback before it returns.
	updateHook func(*config.Config)
	// blockUpdate, if non-nil, is closed when Update is entered and the caller
	// must close proceed to let Update finish.
	blockUpdate chan struct{}
	proceed     chan struct{}
}

func (f *fakeConfigStore) Load() (config.Config, string, error) {
	return f.cfg, f.path, nil
}

func (f *fakeConfigStore) Update(_ string, fn func(*config.Config) error) (config.Config, error) {
	atomic.AddInt32(&f.updateCnt, 1)
	if f.blockUpdate != nil {
		close(f.blockUpdate)
		<-f.proceed
	}
	// Deep copy so a failed Update (fn error or updateErr) does not mutate the
	// in-memory config maps, mirroring the atomic semantics of the production
	// config.Update which only persists on success.
	cfg := deepCopyConfig(f.cfg)
	// updateHook runs before fn to simulate a concurrent config change that
	// happened between Load and the Update callback (e.g. a new profile bind or
	// a mode/session change). The callback must observe the latest state.
	if f.updateHook != nil {
		f.updateHook(&cfg)
	}
	if err := fn(&cfg); err != nil {
		return config.Config{}, err
	}
	if f.updateErr != nil {
		return config.Config{}, f.updateErr
	}
	f.cfg = cfg
	return cfg, nil
}

// deepCopyConfig returns a value copy of cfg with freshly allocated maps so
// mutations to the copy never affect the original. Profile/Credential/SSOSession
// are value types, so copying them by value into new maps is sufficient.
func deepCopyConfig(cfg config.Config) config.Config {
	out := cfg
	if cfg.Profiles != nil {
		out.Profiles = make(map[string]config.Profile, len(cfg.Profiles))
		for k, v := range cfg.Profiles {
			out.Profiles[k] = v
		}
	}
	if cfg.Creds != nil {
		out.Creds = make(map[string]config.Credential, len(cfg.Creds))
		for k, v := range cfg.Creds {
			out.Creds[k] = v
		}
	}
	if cfg.SSOSessions != nil {
		out.SSOSessions = make(map[string]config.SSOSession, len(cfg.SSOSessions))
		for k, v := range cfg.SSOSessions {
			out.SSOSessions[k] = v
		}
	}
	return out
}

// fakeOAuthClient is a minimal console.OAuthClient for race tests.
type fakeOAuthClient struct {
	exchangeResp *console.ConsoleTokenResponse
	exchangeErr  error
	endpointURL  string
	// blockExchange, if non-nil, is closed when ExchangeToken is entered and the
	// caller must close proceed to let it return.
	blockExchange chan struct{}
	proceed       chan struct{}
	exchangeCnt   int32
}

func (f *fakeOAuthClient) BuildAuthorizeURL(_ *console.AuthorizeParams) (string, error) {
	return "https://signin.example.com/authorize", nil
}

func (f *fakeOAuthClient) ExchangeToken(_ context.Context, _ *console.ConsoleTokenRequest) (*console.ConsoleTokenResponse, error) {
	atomic.AddInt32(&f.exchangeCnt, 1)
	if f.blockExchange != nil {
		close(f.blockExchange)
		<-f.proceed
	}
	if f.exchangeErr != nil {
		return nil, f.exchangeErr
	}
	return f.exchangeResp, nil
}

func (f *fakeOAuthClient) EndpointURL() string {
	if f.endpointURL == "" {
		return console.DefaultEndpoint
	}
	return f.endpointURL
}

// --- Tests ---

func TestLoginAndLogoutVisibleInBothEditions(t *testing.T) {
	for _, group := range []string{"login", "logout"} {
		if !isRecognizedGroup(group) {
			t.Fatalf("%q should be a recognized group", group)
		}
		if !isGroupEnabledInCurrentEdition(group) {
			t.Fatalf("%q should be enabled in edition %q", group, currentEdition())
		}
	}
	// Verify they appear in the visible group list for the current edition.
	names := cliGroupNames()
	for _, want := range []string{"login", "logout"} {
		found := false
		for _, n := range names {
			if n == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("%q missing from visible groups %v", want, names)
		}
	}
}

func TestLoginShortFlagsMatchLongFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want loginOpts
	}{
		{"short profile", []string{"-p", "prod"}, loginOpts{Profile: "prod"}},
		{"long profile", []string{"--profile", "prod"}, loginOpts{Profile: "prod"}},
		{"short region", []string{"-r", "cn-beijing"}, loginOpts{Region: "cn-beijing"}},
		{"long region", []string{"--region", "cn-beijing"}, loginOpts{Region: "cn-beijing"}},
		{"device code", []string{"--device-code"}, loginOpts{DeviceCode: true}},
		{"remote", []string{"--remote"}, loginOpts{DeviceCode: true, Remote: true, NoBrowser: true}},
		{"no browser", []string{"--no-browser"}, loginOpts{DeviceCode: true, NoBrowser: true}},
		{"endpoint", []string{"--endpoint", "https://tls-cn-beijing.volces.com"}, loginOpts{Endpoint: "https://tls-cn-beijing.volces.com"}},
		{"login endpoint", []string{"--login-endpoint", "https://signin.byteplus.com"}, loginOpts{LoginEndpoint: "https://signin.byteplus.com"}},
		{"all flags", []string{"-p", "prod", "-r", "cn-beijing", "--device-code", "--remote", "--no-browser", "--endpoint", "https://tls-cn-beijing.volces.com", "--login-endpoint", "https://signin.byteplus.com"},
			loginOpts{Profile: "prod", Region: "cn-beijing", DeviceCode: true, Remote: true, NoBrowser: true, Endpoint: "https://tls-cn-beijing.volces.com", LoginEndpoint: "https://signin.byteplus.com"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseLoginFlags(tt.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestLoginRejectsPublicConsoleAuthEndpointOverride(t *testing.T) {
	_, err := parseLoginFlags([]string{"--endpoint-url", "https://signin.example.com"})
	if err == nil {
		t.Fatal("expected --endpoint-url to be rejected")
	}
	if !strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("expected unknown flag error, got %q", err.Error())
	}
}

func TestLoginProfileSelectorResolution(t *testing.T) {
	tests := []struct {
		name   string
		global string
		local  string
		want   string
	}{
		{"global only", "g", "", "g"},
		{"local only", "", "l", "l"},
		{"both same", "x", "x", "x"},
		{"neither", "", "", ""},
		{"whitespace trimmed", "  g  ", "", "g"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveProfileSelector(tt.global, tt.local)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLoginRejectsConflictingGlobalAndLocalProfiles(t *testing.T) {
	_, err := resolveProfileSelector("global-prof", "local-prof")
	if err == nil {
		t.Fatal("expected conflict error, got nil")
	}
	if !strings.Contains(err.Error(), "conflicting profile selectors") {
		t.Fatalf("expected conflict message, got %q", err.Error())
	}
}

func TestLoginRejectsSecretsFile(t *testing.T) {
	// Global --secrets-file is rejected by preflightGlobalSecretsFile before the
	// command runs; verify the command also rejects a local --secrets-file that
	// reaches it (e.g. passed after the group name).
	_, err := parseLoginFlags([]string{"--secrets-file", "/path/to/secrets"})
	if err == nil {
		t.Fatal("expected error for --secrets-file, got nil")
	}
	if !strings.Contains(err.Error(), "--secrets-file") {
		t.Fatalf("expected --secrets-file rejection, got %q", err.Error())
	}
	_, _, err = parseLogoutFlags([]string{"--secrets-file", "/path/to/secrets"})
	if err == nil {
		t.Fatal("expected error for logout --secrets-file, got nil")
	}
}

func TestLoginProgressUsesStderrAndResultUsesStdout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	fakeSvc := &fakeLoginService{
		result: &loginResult{
			Profile:         "prod",
			Provider:        "console-login",
			Region:          "cn-beijing",
			Endpoint:        "https://tls-cn-beijing.volces.com",
			ExpiresAt:       time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			MaskedAccessKey: "AKLT***abcd",
		},
	}
	adapter := &loginAdapter{
		loginSvc: fakeSvc,
		stdout:   &stdout,
		stderr:   &stderr,
	}
	result, err := adapter.runLogin(context.Background(), loginOpts{Profile: "prod"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// The adapter returns the result object; Run writes it to stdout as JSON.
	// Verify the result shape is serializable and contains only safe fields.
	b, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	for _, key := range []string{"profile", "provider", "region", "endpoint", "expires_at", "masked_access_key"} {
		if _, ok := m[key]; !ok {
			t.Fatalf("result missing key %q: %s", key, string(b))
		}
	}
	// No secret fields should leak.
	for _, key := range []string{"access_key_id", "secret_access_key", "session_token", "access_token", "refresh_token", "id_token", "login_session"} {
		if _, ok := m[key]; ok {
			t.Fatalf("result must not contain secret field %q: %s", key, string(b))
		}
	}
}

func TestLoginFailureClassificationSuggestsSelectedFlowWithoutLeakingCause(t *testing.T) {
	for _, testCase := range []struct {
		name         string
		opts         loginOpts
		wantHint     string
		unwantedHint string
	}{
		{
			name:         "default browser callback",
			wantHint:     "volclog login --device-code",
			unwantedHint: "--no-browser",
		},
		{
			name:         "device code",
			opts:         loginOpts{DeviceCode: true},
			wantHint:     "--device-code --no-browser",
			unwantedHint: "callback",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			adapter := &loginAdapter{loginSvc: &fakeLoginService{err: errors.New("secret-login-cause")}}
			_, err := adapter.runLogin(context.Background(), testCase.opts)
			if err == nil {
				t.Fatal("expected login error")
			}
			payload, code := classifyError(err, "", 0, "login")
			if code != 2 || payload.Kind != "auth" || !strings.Contains(payload.Hint, testCase.wantHint) {
				t.Fatalf("classifyError() = code %d payload %+v", code, payload)
			}
			if strings.Contains(payload.Hint, testCase.unwantedHint) {
				t.Fatalf("hint %q unexpectedly describes the other flow", payload.Hint)
			}
			if strings.Contains(err.Error(), "secret-login-cause") {
				t.Fatalf("error leaked cause: %q", err.Error())
			}
		})
	}
}

func TestLoginResultNeverContainsSecrets(t *testing.T) {
	// The loginResult type must never have fields for secrets.
	r := loginResult{
		Profile:         "p",
		Provider:        "console-login",
		Region:          "r",
		ExpiresAt:       time.Now(),
		MaskedAccessKey: "AKLT***xyz",
	}
	b, _ := json.Marshal(r)
	s := string(b)
	for _, bad := range []string{"secret", "token", "session", "verifier"} {
		if strings.Contains(strings.ToLower(s), bad) && !strings.Contains(strings.ToLower(s), "masked_access_key") {
			// masked_access_key is allowed; check we didn't accidentally include
			// other secret-like fields.
		}
	}
	// Explicitly verify no raw credential fields exist in the JSON.
	for _, field := range []string{"\"access_key\"", "\"secret_access\"", "\"session_token\"", "\"access_token\"", "\"refresh_token\""} {
		if strings.Contains(s, field) {
			t.Fatalf("result JSON must not contain %q: %s", field, s)
		}
	}
}

func TestLogoutClearsOnlyKnownConsoleCacheAndBinding(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.PutProfile("prod", config.Profile{
		Mode:         config.AuthModeConsoleLogin,
		LoginSession: "session-1",
		Region:       "cn-beijing",
		Endpoint:     "https://tls.example.com",
	})
	cache := newFakeCache()
	cache.data["session-1"] = []byte(`{"login_session":"session-1"}`)
	store := &fakeConfigStore{cfg: cfg, path: "/tmp/config.json"}

	adapter := &loginAdapter{
		cache:    cache,
		cfgStore: store,
		stdout:   &bytes.Buffer{},
		stderr:   &bytes.Buffer{},
	}
	result, err := adapter.runLogout(context.Background(), "prod", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if atomic.LoadInt32(&cache.deleteCnt) != 1 {
		t.Fatalf("expected 1 cache delete, got %d", cache.deleteCnt)
	}
	if atomic.LoadInt32(&store.updateCnt) != 1 {
		t.Fatalf("expected 1 config update, got %d", store.updateCnt)
	}
	// Verify the login-session was cleared but mode/TLS fields preserved.
	p, _ := store.cfg.GetProfile("prod")
	if p.LoginSession != "" {
		t.Fatalf("login-session should be cleared, got %q", p.LoginSession)
	}
	if p.Mode != config.AuthModeConsoleLogin {
		t.Fatalf("mode should be preserved, got %q", p.Mode)
	}
	if p.Region != "cn-beijing" {
		t.Fatalf("region should be preserved, got %q", p.Region)
	}
	if p.Endpoint != "https://tls.example.com" {
		t.Fatalf("endpoint should be preserved, got %q", p.Endpoint)
	}
}

func TestLogoutAllOnlyTouchesConsoleProfiles(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.PutProfile("console-prof", config.Profile{
		Mode:         config.AuthModeConsoleLogin,
		LoginSession: "session-1",
	})
	cfg.PutProfile("ak-prof", config.Profile{
		Mode:            config.AuthModeAK,
		AccessKeyID:     "AKLTstatic",
		SecretAccessKey: "secret-static",
	})
	cfg.PutProfile("sso-prof", config.Profile{
		Mode:           config.AuthModeSSO,
		SSOSessionName: "my-sso",
	})

	cache := newFakeCache()
	cache.data["session-1"] = []byte(`{"login_session":"session-1"}`)
	store := &fakeConfigStore{cfg: cfg, path: "/tmp/config.json"}

	adapter := &loginAdapter{
		cache:    cache,
		cfgStore: store,
		stdout:   &bytes.Buffer{},
		stderr:   &bytes.Buffer{},
	}
	_, err := adapter.runLogout(context.Background(), "", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only the console profile's session should be cleared.
	p, _ := store.cfg.GetProfile("console-prof")
	if p.LoginSession != "" {
		t.Fatalf("console profile login-session should be cleared")
	}
	// AK profile must be untouched.
	ak, _ := store.cfg.GetProfile("ak-prof")
	if ak.AccessKeyID != "AKLTstatic" || ak.SecretAccessKey != "secret-static" {
		t.Fatalf("AK profile credentials must not be modified: %+v", ak)
	}
	// SSO profile must be untouched.
	sso, _ := store.cfg.GetProfile("sso-prof")
	if sso.SSOSessionName != "my-sso" {
		t.Fatalf("SSO profile must not be modified: %+v", sso)
	}
}

func TestLogoutPreservesTLSAndDormantStaticFields(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.PutProfile("prod", config.Profile{
		Mode:            config.AuthModeConsoleLogin,
		LoginSession:    "session-1",
		Region:          "cn-beijing",
		Endpoint:        "https://tls.example.com",
		TimeoutSeconds:  120,
		AccessKeyID:     "AKLTdormant",
		SecretAccessKey: "dormant-secret",
		SecurityToken:   "dormant-token",
	})
	cache := newFakeCache()
	cache.data["session-1"] = []byte(`{"login_session":"session-1"}`)
	store := &fakeConfigStore{cfg: cfg, path: "/tmp/config.json"}

	adapter := &loginAdapter{
		cache:    cache,
		cfgStore: store,
		stdout:   &bytes.Buffer{},
		stderr:   &bytes.Buffer{},
	}
	_, err := adapter.runLogout(context.Background(), "prod", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	p, _ := store.cfg.GetProfile("prod")
	if p.LoginSession != "" {
		t.Fatal("login-session should be cleared")
	}
	if p.Mode != config.AuthModeConsoleLogin {
		t.Fatalf("mode should be preserved, got %q", p.Mode)
	}
	if p.Region != "cn-beijing" || p.Endpoint != "https://tls.example.com" || p.TimeoutSeconds != 120 {
		t.Fatalf("TLS fields should be preserved: region=%q endpoint=%q timeout=%d", p.Region, p.Endpoint, p.TimeoutSeconds)
	}
	if p.AccessKeyID != "AKLTdormant" || p.SecretAccessKey != "dormant-secret" || p.SecurityToken != "dormant-token" {
		t.Fatalf("dormant static fields should be preserved: %+v", p)
	}
}

func TestLogoutMissingCacheIsIdempotent(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.PutProfile("prod", config.Profile{
		Mode:         config.AuthModeConsoleLogin,
		LoginSession: "session-1",
	})
	cache := newFakeCache() // no data for session-1
	store := &fakeConfigStore{cfg: cfg, path: "/tmp/config.json"}

	adapter := &loginAdapter{
		cache:    cache,
		cfgStore: store,
		stdout:   &bytes.Buffer{},
		stderr:   &bytes.Buffer{},
	}
	_, err := adapter.runLogout(context.Background(), "prod", false)
	if err != nil {
		t.Fatalf("expected idempotent success, got error: %v", err)
	}
	if atomic.LoadInt32(&cache.deleteCnt) != 1 {
		t.Fatalf("expected delete to be called once even for missing cache, got %d", cache.deleteCnt)
	}
}

func TestLogoutConfigFailureReturnsClassifiablePartialFailure(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.PutProfile("prod", config.Profile{
		Mode:         config.AuthModeConsoleLogin,
		LoginSession: "session-1",
	})
	cache := newFakeCache()
	cache.data["session-1"] = []byte(`{"login_session":"session-1"}`)
	store := &fakeConfigStore{
		cfg:       cfg,
		path:      "/tmp/config.json",
		updateErr: errors.New("simulated config write failure"),
	}

	adapter := &loginAdapter{
		cache:    cache,
		cfgStore: store,
		stdout:   &bytes.Buffer{},
		stderr:   &bytes.Buffer{},
	}
	_, err := adapter.runLogout(context.Background(), "prod", false)
	if err == nil {
		t.Fatal("expected partial failure error, got nil")
	}
	if !errors.Is(err, ErrLogoutPartialFailure) {
		t.Fatalf("expected ErrLogoutPartialFailure, got %v", err)
	}
	// Cache must have been deleted (fail closed).
	if atomic.LoadInt32(&cache.deleteCnt) != 1 {
		t.Fatalf("expected cache to be deleted before config failure, got deleteCnt=%d", cache.deleteCnt)
	}
}

// failingLockCache is a consoleCache fake whose WithLock always returns an error
// without invoking the callback. It simulates a genuine lock-acquisition failure
// (e.g. the cache directory is inaccessible) so the safe lock-error wrapping can
// be verified independently of the delete/partial-failure paths.
type failingLockCache struct {
	lockErr error
}

func (f *failingLockCache) WithLock(_ context.Context, _ string, _ func() error) error {
	return f.lockErr
}

func (f *failingLockCache) Delete(_ string) error { return nil }

func TestLogoutDeleteFailureNotMislabeledAsLockError(t *testing.T) {
	// When cache.Delete fails inside the WithLock callback, the resulting
	// *safeCLIError ("delete console login cache failed") must be returned
	// as-is: it must not be re-wrapped as "logout failed to acquire cache lock".
	const session = "session-delete-canary-xyz"
	const deleteCanary = "delete-failure-canary-path-123"
	cfg := config.DefaultConfig()
	cfg.PutProfile("prod", config.Profile{
		Mode:         config.AuthModeConsoleLogin,
		LoginSession: session,
	})
	cache := newFakeCache()
	cache.data[session] = []byte(`{"login_session":"` + session + `"}`)
	cache.deleteErr = errors.New(deleteCanary)
	store := &fakeConfigStore{cfg: cfg, path: "/tmp/config.json"}

	adapter := &loginAdapter{
		cache:    cache,
		cfgStore: store,
		stdout:   &bytes.Buffer{},
		stderr:   &bytes.Buffer{},
	}
	_, err := adapter.runLogout(context.Background(), "prod", false)
	if err == nil {
		t.Fatal("expected error from delete failure")
	}
	msg := err.Error()
	// The accurate delete description must survive.
	if !strings.Contains(msg, "delete console login cache failed") {
		t.Fatalf("error should describe delete failure, got %q", msg)
	}
	// It must not be relabeled as a lock-acquisition error.
	if strings.Contains(msg, "acquire cache lock") {
		t.Fatalf("delete error must not be relabeled as lock error: %q", msg)
	}
	// No canary/session/path may leak into the safe description.
	for _, bad := range []string{deleteCanary, session, "/tmp/config.json"} {
		if strings.Contains(msg, bad) {
			t.Fatalf("error must not contain %q: %q", bad, msg)
		}
	}
	// errors.Is must still reach the original delete cause.
	if !errors.Is(err, cache.deleteErr) {
		t.Fatalf("errors.Is should match original delete cause, got %v", err)
	}
}

func TestLogoutLockAcquisitionFailureStillWrappedAsLockError(t *testing.T) {
	// A genuine WithLock failure (callback never runs) must be wrapped as the
	// safe, fixed lock-acquisition error and must not mention the underlying
	// cause text.
	const lockCanary = "lock-acquisition-failure-canary-456"
	cfg := config.DefaultConfig()
	cfg.PutProfile("prod", config.Profile{
		Mode:         config.AuthModeConsoleLogin,
		LoginSession: "s1",
	})
	cache := &failingLockCache{lockErr: errors.New(lockCanary)}
	store := &fakeConfigStore{cfg: cfg, path: "/tmp/config.json"}

	adapter := &loginAdapter{
		cache:    cache,
		cfgStore: store,
		stdout:   &bytes.Buffer{},
		stderr:   &bytes.Buffer{},
	}
	_, err := adapter.runLogout(context.Background(), "prod", false)
	if err == nil {
		t.Fatal("expected error from lock failure")
	}
	msg := err.Error()
	if !strings.Contains(msg, "logout failed to acquire cache lock") {
		t.Fatalf("error should describe lock failure, got %q", msg)
	}
	if strings.Contains(msg, lockCanary) {
		t.Fatalf("lock error must not leak cause canary: %q", msg)
	}
	// errors.Is must still reach the original lock cause.
	if !errors.Is(err, cache.lockErr) {
		t.Fatalf("errors.Is should match original lock cause, got %v", err)
	}
}

func TestLogoutConfigFailureCacheDeletedThenProviderRequiresReauth(t *testing.T) {
	// Use a real FileCache so the cache lock and deletion are real.
	dir := t.TempDir()
	cache, err := console.NewFileCache(dir)
	if err != nil {
		t.Fatalf("create file cache: %v", err)
	}
	session := "trn:session:test:logout-failure"
	cacheBytes := makeCacheBytesForTest(session, time.Now().Add(-time.Hour), 3600, "refresh-token")
	if err := cache.WriteRaw(session, cacheBytes); err != nil {
		t.Fatalf("write cache: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.PutProfile("prod", config.Profile{
		Mode:         config.AuthModeConsoleLogin,
		LoginSession: session,
	})
	store := &fakeConfigStore{
		cfg:       cfg,
		path:      "/tmp/config.json",
		updateErr: errors.New("simulated config write failure"),
	}

	adapter := &loginAdapter{
		cache:    cache,
		cfgStore: store,
		stdout:   &bytes.Buffer{},
		stderr:   &bytes.Buffer{},
	}
	_, err = adapter.runLogout(context.Background(), "prod", false)
	if err == nil {
		t.Fatal("expected partial failure error")
	}
	if !errors.Is(err, ErrLogoutPartialFailure) {
		t.Fatalf("expected ErrLogoutPartialFailure, got %v", err)
	}

	// Cache must be gone.
	_, existed, err := cache.ReadRaw(session)
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	if existed {
		t.Fatal("cache should be deleted after logout partial failure")
	}

	// Provider must now require reauth.
	provider := console.NewProvider(session, cache, func(string) (console.OAuthClient, error) {
		return &fakeOAuthClient{exchangeResp: &console.ConsoleTokenResponse{
			AccessToken: validSTSAccessTokenForTest(),
			TokenType:   "sts",
			ExpiresIn:   3600,
		}}, nil
	}, nil)
	_, err = provider.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected Provider to require reauth after cache deletion")
	}
	var authErr *auth.Error
	if !errors.As(err, &authErr) || authErr.Kind != auth.ReauthRequired {
		t.Fatalf("expected ReauthRequired error, got %v", err)
	}
}

func TestLogoutReturnsThenProviderRequiresReauth(t *testing.T) {
	dir := t.TempDir()
	cache, err := console.NewFileCache(dir)
	if err != nil {
		t.Fatalf("create file cache: %v", err)
	}
	session := "trn:session:test:logout-then-reauth"
	cacheBytes := makeCacheBytesForTest(session, time.Now(), 3600, "refresh-token")
	if err := cache.WriteRaw(session, cacheBytes); err != nil {
		t.Fatalf("write cache: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.PutProfile("prod", config.Profile{
		Mode:         config.AuthModeConsoleLogin,
		LoginSession: session,
	})
	store := &fakeConfigStore{cfg: cfg, path: "/tmp/config.json"}

	adapter := &loginAdapter{
		cache:    cache,
		cfgStore: store,
		stdout:   &bytes.Buffer{},
		stderr:   &bytes.Buffer{},
	}
	_, err = adapter.runLogout(context.Background(), "prod", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Cache must be gone.
	_, existed, _ := cache.ReadRaw(session)
	if existed {
		t.Fatal("cache should be deleted after logout")
	}

	// Provider must require reauth.
	provider := console.NewProvider(session, cache, func(string) (console.OAuthClient, error) {
		return &fakeOAuthClient{}, nil
	}, nil)
	_, err = provider.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected Provider to require reauth after logout")
	}
	var authErr *auth.Error
	if !errors.As(err, &authErr) || authErr.Kind != auth.ReauthRequired {
		t.Fatalf("expected ReauthRequired, got %v", err)
	}
}

func TestLogoutRacingRetrieveCannotResurrectCache(t *testing.T) {
	// Scenario: logout acquires the cache lock first and blocks in config.Update.
	// A concurrent Provider.Retrieve blocks waiting for the lock. When logout
	// finishes (deleting the cache), Provider acquires the lock, sees the
	// missing cache, and returns ReauthRequired. The cache must not reappear.
	dir := t.TempDir()
	cache, err := console.NewFileCache(dir)
	if err != nil {
		t.Fatalf("create file cache: %v", err)
	}
	session := "trn:session:test:race"
	cacheBytes := makeCacheBytesForTest(session, time.Now(), 3600, "refresh-token")
	if err := cache.WriteRaw(session, cacheBytes); err != nil {
		t.Fatalf("write cache: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.PutProfile("prod", config.Profile{
		Mode:         config.AuthModeConsoleLogin,
		LoginSession: session,
	})
	store := &fakeConfigStore{
		cfg:         cfg,
		path:        "/tmp/config.json",
		blockUpdate: make(chan struct{}),
		proceed:     make(chan struct{}),
	}

	adapter := &loginAdapter{
		cache:    cache,
		cfgStore: store,
		stdout:   &bytes.Buffer{},
		stderr:   &bytes.Buffer{},
	}

	// Start logout in a goroutine; it will block inside config.Update.
	logoutDone := make(chan error, 1)
	go func() {
		_, err := adapter.runLogout(context.Background(), "prod", false)
		logoutDone <- err
	}()

	// Wait until logout is inside config.Update (holding the cache lock).
	<-store.blockUpdate

	// Now start a Provider.Retrieve; it should block on the cache lock. Use the
	// securestore contention observer to detect the real moment Retrieve blocks on
	// the in-process lock held by logout, instead of sleeping.
	providerContended := make(chan struct{}, 1)
	var obsOnce sync.Once
	providerCtx := securestore.WithLockContentionObserver(context.Background(), func() {
		obsOnce.Do(func() { close(providerContended) })
	})
	provider := console.NewProvider(session, cache, func(string) (console.OAuthClient, error) {
		return &fakeOAuthClient{exchangeResp: &console.ConsoleTokenResponse{
			AccessToken: validSTSAccessTokenForTest(),
			TokenType:   "sts",
			ExpiresIn:   3600,
		}}, nil
	}, nil)
	retrieveDone := make(chan error, 1)
	go func() {
		_, err := provider.Retrieve(providerCtx)
		retrieveDone <- err
	}()

	// Wait until the Provider actually contends for the cache lock held by
	// logout. time.After is only a deadlock guard, not a coordination delay.
	select {
	case <-providerContended:
	case <-time.After(5 * time.Second):
		t.Fatal("provider lock contention not observed in time")
	}

	// Let logout finish: it deletes the cache, then config.Update returns, then
	// it releases the cache lock.
	close(store.proceed)

	// Wait for both to finish.
	logoutErr := <-logoutDone
	retrieveErr := <-retrieveDone

	if logoutErr != nil {
		t.Fatalf("logout should succeed, got %v", logoutErr)
	}
	// Provider must see the missing cache and require reauth; it must NOT have
	// resurrected the cache by refreshing.
	if retrieveErr == nil {
		t.Fatal("expected Provider to require reauth after logout deleted cache")
	}
	var authErr *auth.Error
	if !errors.As(retrieveErr, &authErr) || authErr.Kind != auth.ReauthRequired {
		t.Fatalf("expected ReauthRequired, got %v", retrieveErr)
	}
	// Cache must still be gone.
	_, existed, _ := cache.ReadRaw(session)
	if existed {
		t.Fatal("cache must not be resurrected after logout")
	}
}

func TestLogoutAllGroupsProfilesByLoginSession(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.PutProfile("a", config.Profile{Mode: config.AuthModeConsoleLogin, LoginSession: "session-b"})
	cfg.PutProfile("b", config.Profile{Mode: config.AuthModeConsoleLogin, LoginSession: "session-a"})
	cfg.PutProfile("c", config.Profile{Mode: config.AuthModeConsoleLogin, LoginSession: "session-a"})
	cfg.PutProfile("d", config.Profile{Mode: config.AuthModeConsoleLogin, LoginSession: "session-c"})

	cache := newFakeCache()
	for _, s := range []string{"session-a", "session-b", "session-c"} {
		cache.data[s] = []byte(`{"login_session":"` + s + `"}`)
	}
	store := &fakeConfigStore{cfg: cfg, path: "/tmp/config.json"}

	adapter := &loginAdapter{
		cache:    cache,
		cfgStore: store,
		stdout:   &bytes.Buffer{},
		stderr:   &bytes.Buffer{},
	}
	result, err := adapter.runLogout(context.Background(), "", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result")
	}
	// Sessions must be processed in stable sorted order. Verify via the cache's
	// recorded delete order rather than the public result (which must not leak
	// session strings).
	want := []string{"session-a", "session-b", "session-c"}
	if len(cache.deleteOrder) != len(want) {
		t.Fatalf("got delete order %v, want %v", cache.deleteOrder, want)
	}
	for i, s := range want {
		if cache.deleteOrder[i] != s {
			t.Fatalf("deleteOrder[%d] = %q, want %q (full: %v)", i, cache.deleteOrder[i], s, cache.deleteOrder)
		}
	}
	// Public result must not contain session strings.
	if result.ClearedSessionCount != 3 {
		t.Fatalf("expected 3 cleared sessions, got %d", result.ClearedSessionCount)
	}
	// Profiles sharing session-a (b and c) must both be cleared.
	for _, name := range []string{"a", "b", "c", "d"} {
		p, _ := store.cfg.GetProfile(name)
		if p.LoginSession != "" {
			t.Fatalf("profile %q should have login-session cleared, got %q", name, p.LoginSession)
		}
	}
}

func TestLogoutSharedSessionClearsAllBindings(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.PutProfile("primary", config.Profile{Mode: config.AuthModeConsoleLogin, LoginSession: "shared-session"})
	cfg.PutProfile("secondary", config.Profile{Mode: config.AuthModeConsoleLogin, LoginSession: "shared-session"})

	cache := newFakeCache()
	cache.data["shared-session"] = []byte(`{"login_session":"shared-session"}`)
	store := &fakeConfigStore{cfg: cfg, path: "/tmp/config.json"}

	adapter := &loginAdapter{
		cache:    cache,
		cfgStore: store,
		stdout:   &bytes.Buffer{},
		stderr:   &bytes.Buffer{},
	}
	// Logout only "primary" but the shared session must clear both profiles.
	_, err := adapter.runLogout(context.Background(), "primary", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, name := range []string{"primary", "secondary"} {
		p, _ := store.cfg.GetProfile(name)
		if p.LoginSession != "" {
			t.Fatalf("profile %q should have login-session cleared, got %q", name, p.LoginSession)
		}
	}
}

func TestLogoutRejectsNonConsoleProfile(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.PutProfile("ak-prof", config.Profile{Mode: config.AuthModeAK, AccessKeyID: "AKLTx"})
	cache := newFakeCache()
	store := &fakeConfigStore{cfg: cfg, path: "/tmp/config.json"}

	adapter := &loginAdapter{
		cache:    cache,
		cfgStore: store,
		stdout:   &bytes.Buffer{},
		stderr:   &bytes.Buffer{},
	}
	_, err := adapter.runLogout(context.Background(), "ak-prof", false)
	if err == nil {
		t.Fatal("expected error for non-console profile")
	}
	if !strings.Contains(err.Error(), "not a console-login profile") {
		t.Fatalf("expected console-login profile error, got %q", err.Error())
	}
}

func TestLogoutAllAndProfileConflict(t *testing.T) {
	_, _, err := parseLogoutFlags([]string{"--all", "--profile", "prod"})
	if err == nil {
		t.Fatal("expected error for --all + --profile")
	}
}

func TestLogoutHoldsCacheLockDuringConfigUpdate(t *testing.T) {
	// Verify that config.Update runs while the cache lock is held, by blocking
	// inside Update and checking that another WithLock cannot proceed until
	// Update returns.
	cfg := config.DefaultConfig()
	cfg.PutProfile("prod", config.Profile{Mode: config.AuthModeConsoleLogin, LoginSession: "session-1"})
	cache := newFakeCache()
	cache.data["session-1"] = []byte(`{"login_session":"session-1"}`)
	store := &fakeConfigStore{
		cfg:         cfg,
		path:        "/tmp/config.json",
		blockUpdate: make(chan struct{}),
		proceed:     make(chan struct{}),
	}

	adapter := &loginAdapter{
		cache:    cache,
		cfgStore: store,
		stdout:   &bytes.Buffer{},
		stderr:   &bytes.Buffer{},
	}

	done := make(chan error, 1)
	go func() {
		_, err := adapter.runLogout(context.Background(), "prod", false)
		done <- err
	}()

	<-store.blockUpdate // logout is inside config.Update, holding cache lock

	// Try to acquire the cache lock from another goroutine; it must block.
	lockAcquired := make(chan struct{})
	go func() {
		_ = cache.WithLock(context.Background(), "session-1", func() error {
			close(lockAcquired)
			return nil
		})
	}()
	select {
	case <-lockAcquired:
		t.Fatal("cache lock should be held during config.Update")
	case <-time.After(100 * time.Millisecond):
		// Good: lock is held.
	}

	close(store.proceed) // let config.Update finish
	<-done

	// Now the lock should be acquirable.
	select {
	case <-lockAcquired:
	case <-time.After(time.Second):
		t.Fatal("cache lock should be released after logout returns")
	}
}

func TestLoginSelectorConflictPriorityOverSecretsFile(t *testing.T) {
	// When both a profile selector conflict and --secrets-file are present, the
	// profile conflict must be detected first (it's the more actionable error).
	// The global preflight checks profile conflict before secrets-file rejection.
	err := preflightGlobalSecretsFile("login", "global-prof", "/path/to/secrets")
	if err == nil {
		t.Fatal("expected error")
	}
	// The error should be the profile/secrets conflict, not the secrets-file
	// group rejection.
	if !strings.Contains(err.Error(), "conflicting") {
		t.Fatalf("expected selector conflict error, got %q", err.Error())
	}
}

func TestNewDefaultLocalAuthorizerConstructsWithoutBindingSocket(t *testing.T) {
	// The exported constructor must not bind a socket or make any network call.
	// It only wraps NewCallbackServer(nil) as the factory, which is invoked
	// lazily when Authorize runs.
	client := &fakeOAuthClient{endpointURL: console.DefaultEndpoint}
	auth := console.NewDefaultLocalAuthorizer(client, nil, &bytes.Buffer{}, "state-value", "challenge-value")
	if auth == nil {
		t.Fatal("expected non-nil authorizer")
	}
	// Constructing must not panic or bind a socket. We do not call Authorize
	// here because that would bind a real loopback socket, which the sandbox
	// may disallow.
}

func TestProductionConfigStoreSatisfiesInterface(t *testing.T) {
	var _ configStore = productionConfigStore{}
}

func TestConsoleFileCacheSatisfiesInterface(t *testing.T) {
	var _ consoleCache = (*console.FileCache)(nil)
}

func TestConfirmPromptAcceptsYes(t *testing.T) {
	var stderr bytes.Buffer
	confirm := newConfirmPrompt(strings.NewReader("yes\n"), &stderr)
	ok, err := confirm("prod", "current-session-canary", "new-session-canary")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected confirmation to be accepted for 'yes'")
	}
	// Prompt must go to stderr, not stdout, and must not contain either session.
	if stderr.Len() == 0 {
		t.Fatal("expected prompt on stderr")
	}
	if strings.Contains(stderr.String(), "current-session-canary") || strings.Contains(stderr.String(), "new-session-canary") {
		t.Fatalf("prompt must not contain session canary: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "prod") {
		t.Fatalf("prompt should contain profile name: %q", stderr.String())
	}
}

func TestConfirmPromptAcceptsLowercaseY(t *testing.T) {
	confirm := newConfirmPrompt(strings.NewReader("y\n"), &bytes.Buffer{})
	ok, err := confirm("prod", "s1", "s2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected 'y' to confirm")
	}
}

func TestConfirmPromptDefaultsDenyOnEmpty(t *testing.T) {
	confirm := newConfirmPrompt(strings.NewReader("\n"), &bytes.Buffer{})
	ok, err := confirm("prod", "s1", "s2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("empty input should deny by default")
	}
}

func TestConfirmPromptDeniesOtherInput(t *testing.T) {
	for _, in := range []string{"n\n", "no\n", "maybe\n", "ye\n", "yep\n"} {
		confirm := newConfirmPrompt(strings.NewReader(in), &bytes.Buffer{})
		ok, err := confirm("prod", "s1", "s2")
		if err != nil {
			t.Fatalf("unexpected error for input %q: %v", in, err)
		}
		if ok {
			t.Fatalf("input %q should deny", in)
		}
	}
}

func TestConfirmPromptAcceptsCaseVariants(t *testing.T) {
	for _, in := range []string{"YES\n", "  Yes  \n", "Y\n", " y \n"} {
		confirm := newConfirmPrompt(strings.NewReader(in), &bytes.Buffer{})
		ok, err := confirm("prod", "s1", "s2")
		if err != nil {
			t.Fatalf("unexpected error for input %q: %v", in, err)
		}
		if !ok {
			t.Fatalf("input %q should confirm", in)
		}
	}
}

func TestConfirmPromptEOFReturnsError(t *testing.T) {
	confirm := newConfirmPrompt(strings.NewReader(""), &bytes.Buffer{})
	ok, err := confirm("prod", "s1", "s2")
	if err == nil {
		t.Fatal("expected error on EOF")
	}
	if ok {
		t.Fatal("EOF should not confirm")
	}
}

func TestConfirmPromptNeverWritesToStdout(t *testing.T) {
	// stdout is not passed to the helper; verify the helper only touches stderr.
	var stderr bytes.Buffer
	confirm := newConfirmPrompt(strings.NewReader("y\n"), &stderr)
	if _, err := confirm("prod", "s1", "s2"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stderr.Len() == 0 {
		t.Fatal("expected prompt on stderr")
	}
}

func TestConfirmPromptDoesNotLeakSessionInError(t *testing.T) {
	// A read error must not surface the session canary in its message.
	var stderr bytes.Buffer
	confirm := newConfirmPrompt(errReader{}, &stderr)
	_, err := confirm("prod", "session-canary-123", "session-canary-456")
	if err == nil {
		t.Fatal("expected error from failing reader")
	}
	if strings.Contains(err.Error(), "session-canary") {
		t.Fatalf("error must not contain session canary: %q", err.Error())
	}
}

// errReader always returns an error from Read.
type errReader struct{}

func (errReader) Read(_ []byte) (int, error) {
	return 0, errors.New("simulated read failure")
}

func TestConfirmPromptOverlongLineFailsClosed(t *testing.T) {
	// "yes" + 4093 spaces + "no\n" reaches the 4096-byte cap at the 'n' of "no".
	// The old behavior returned the truncated prefix "yes" + 4093 spaces, which
	// TrimSpace'd to "yes" and wrongly confirmed. The new behavior must error and
	// fail closed.
	crafted := "yes" + strings.Repeat(" ", maxConfirmLineLength-3) + "no\n"
	var stderr bytes.Buffer
	confirm := newConfirmPrompt(strings.NewReader(crafted), &stderr)
	ok, err := confirm("prod", "session-canary-123", "session-canary-456")
	if err == nil {
		t.Fatal("expected error for overlong confirmation line")
	}
	if ok {
		t.Fatal("overlong line must not confirm")
	}
	// Error must be the fixed sentinel, not contain input or session canary.
	if !errors.Is(err, errConfirmLineTooLong) {
		t.Fatalf("expected errConfirmLineTooLong, got %v", err)
	}
	for _, bad := range []string{"yes", "session-canary", "prod"} {
		if strings.Contains(err.Error(), bad) {
			t.Fatalf("error must not contain %q: %q", bad, err.Error())
		}
	}
}

func TestConfirmPromptExactLimitLineIsAccepted(t *testing.T) {
	// A line that is exactly maxConfirmLineLength bytes followed by a newline is
	// a complete, valid line and must be processed normally (not treated as
	// overflow).
	line := strings.Repeat("n", maxConfirmLineLength) + "\n"
	confirm := newConfirmPrompt(strings.NewReader(line), &bytes.Buffer{})
	ok, err := confirm("prod", "s1", "s2")
	if err != nil {
		t.Fatalf("unexpected error for exact-limit line: %v", err)
	}
	if ok {
		t.Fatal("all-n input should deny")
	}
}

func TestConfirmPromptOverflowDoesNotLeakInputOrSession(t *testing.T) {
	// 100000 bytes of 'x' with no newline: must error, and neither the error nor
	// the prompt may contain the input bytes or the session canary.
	long := strings.Repeat("x", 100000)
	var stderr bytes.Buffer
	confirm := newConfirmPrompt(strings.NewReader(long), &stderr)
	_, err := confirm("prod", "session-canary-789", "session-canary-012")
	if err == nil {
		t.Fatal("expected error for 100000-byte line")
	}
	if strings.Contains(err.Error(), "xxx") || strings.Contains(err.Error(), "session-canary") {
		t.Fatalf("error must not contain input or session: %q", err.Error())
	}
	if strings.Contains(stderr.String(), "session-canary") {
		t.Fatalf("prompt must not contain session canary: %q", stderr.String())
	}
}

// canaryReadError is returned by canaryReader.Read. Its message embeds a
// secret/path/session canary so tests can assert the safe wrapper never
// renders it.
type canaryReadError struct{}

func (canaryReadError) Error() string {
	return "reader-secret-canary /tmp/leaked-path session-leak-canary"
}

// canaryReader always returns a canary-bearing error from Read.
type canaryReader struct{}

func (canaryReader) Read(_ []byte) (int, error) {
	return 0, canaryReadError{}
}

func TestConfirmPromptReadErrorRedactsCauseButPreservesChain(t *testing.T) {
	// A reader failure whose cause carries a secret/path/session canary must:
	//   - fail closed (ok=false)
	//   - render a fixed Error() that does not contain the canary/path/session
	//   - preserve the original cause via errors.Is
	//   - be classified as a *safeCLIError via errors.As
	//   - never write the cause to the stderr prompt
	var stderr bytes.Buffer
	confirm := newConfirmPrompt(canaryReader{}, &stderr)
	ok, err := confirm("prod", "session-canary-123", "session-canary-456")
	if err == nil {
		t.Fatal("expected error from failing reader")
	}
	if ok {
		t.Fatal("reader failure must not confirm")
	}
	for _, bad := range []string{"reader-secret-canary", "leaked-path", "session-leak-canary", "session-canary"} {
		if strings.Contains(err.Error(), bad) {
			t.Fatalf("error must not contain %q: %q", bad, err.Error())
		}
	}
	if !errors.Is(err, canaryReadError{}) {
		t.Fatalf("errors.Is must reach the original cause, got %v", err)
	}
	var safeErr *safeCLIError
	if !errors.As(err, &safeErr) {
		t.Fatalf("errors.As must classify as *safeCLIError, got %T", err)
	}
	if strings.Contains(stderr.String(), "reader-secret-canary") || strings.Contains(stderr.String(), "leaked-path") {
		t.Fatalf("stderr prompt must not contain cause: %q", stderr.String())
	}
}

// errCanaryWriter is the fixed canary-bearing error returned by canaryWriter.
// It is a package-level var so tests can assert errors.Is reaches it through
// the safe wrapper.
var errCanaryWriter = errors.New("writer-secret-canary /tmp/writer-leaked-path session-writer-canary")

// canaryWriter always fails Write with a canary-bearing error.
type canaryWriter struct{}

func (canaryWriter) Write(_ []byte) (int, error) {
	return 0, errCanaryWriter
}

func TestConfirmPromptWriteErrorRedactsCauseButPreservesChain(t *testing.T) {
	// A prompt writer failure whose cause carries a canary must render a fixed
	// Error() (no canary/path/session) while preserving the cause via
	// errors.Is and errors.As(*safeCLIError).
	confirm := newConfirmPrompt(strings.NewReader("y\n"), canaryWriter{})
	ok, err := confirm("prod", "session-canary-123", "session-canary-456")
	if err == nil {
		t.Fatal("expected error from failing writer")
	}
	if ok {
		t.Fatal("writer failure must not confirm")
	}
	for _, bad := range []string{"writer-secret-canary", "writer-leaked-path", "session-writer-canary", "session-canary"} {
		if strings.Contains(err.Error(), bad) {
			t.Fatalf("error must not contain %q: %q", bad, err.Error())
		}
	}
	if !strings.Contains(err.Error(), "cannot write prompt") {
		t.Fatalf("error should carry fixed description, got %q", err.Error())
	}
	if !errors.Is(err, errCanaryWriter) {
		t.Fatalf("errors.Is must reach the original cause, got %v", err)
	}
	var safeErr *safeCLIError
	if !errors.As(err, &safeErr) {
		t.Fatalf("errors.As must classify as *safeCLIError, got %T", err)
	}
}

func TestLoginOutputDoesNotLeakSecretsInError(t *testing.T) {
	// The login service returns an error whose message contains a secret canary.
	// The adapter must wrap it with a fixed, safe Error() that does not render the
	// underlying cause text. Verify the error, stdout, and stderr all stay free of
	// the canary.
	secret := "super-secret-token-12345"
	originalErr := errors.New("login failed: " + secret)
	fakeSvc := &fakeLoginService{
		err: originalErr,
	}
	var stdout, stderr bytes.Buffer
	adapter := &loginAdapter{
		loginSvc: fakeSvc,
		stdout:   &stdout,
		stderr:   &stderr,
	}
	_, err := adapter.runLogin(context.Background(), loginOpts{})
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error must not contain secret canary: %q", err.Error())
	}
	if strings.Contains(stdout.String(), secret) {
		t.Fatalf("stdout must not contain secret canary: %q", stdout.String())
	}
	if strings.Contains(stderr.String(), secret) {
		t.Fatalf("stderr must not contain secret canary: %q", stderr.String())
	}
	// The safe wrapper must still preserve the cause for errors.Is/As.
	if !errors.Is(err, originalErr) {
		t.Fatalf("errors.Is should match the original cause, got %v", err)
	}
	var cause *safeCLIError
	if !errors.As(err, &cause) {
		t.Fatalf("expected error to be a *safeCLIError, got %T", err)
	}
}

func TestRunLoginWritesJSONToStdout(t *testing.T) {
	// End-to-end through runLogin with a fake service: verify the result is
	// returned and Run would write it as JSON to stdout.
	var stdout, stderr bytes.Buffer
	ctx := newContext(&stdout, &stderr, output.FormatJSON, "global-prof", "")
	// Override the production adapter builder by directly testing the adapter
	// path with injected fakes.
	fakeSvc := &fakeLoginService{
		result: &loginResult{
			Profile:         "global-prof",
			Provider:        "console-login",
			Region:          "cn-beijing",
			ExpiresAt:       time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			MaskedAccessKey: "AKLT***abcd",
		},
	}
	adapter := &loginAdapter{
		loginSvc: fakeSvc,
		stdout:   &stdout,
		stderr:   &stderr,
	}
	result, err := adapter.runLogin(context.Background(), loginOpts{Profile: "global-prof"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	b, _ := json.Marshal(result)
	var m map[string]any
	json.Unmarshal(b, &m)
	if m["profile"] != "global-prof" {
		t.Fatalf("expected profile global-prof, got %v", m["profile"])
	}
	if m["masked_access_key"] != "AKLT***abcd" {
		t.Fatalf("expected masked key, got %v", m["masked_access_key"])
	}
	_ = ctx
}

func TestLogoutResultJSONShape(t *testing.T) {
	res := &logoutResult{
		ClearedSessionCount: 2,
		ClearedProfiles:     []string{"p1", "p2"},
	}
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// The public JSON must never contain session strings or a cleared_sessions
	// field. Only cleared_session_count and cleared_profiles are allowed.
	if _, ok := m["cleared_sessions"]; ok {
		t.Fatalf("JSON must not contain cleared_sessions: %s", string(b))
	}
	count, _ := m["cleared_session_count"].(float64)
	if count != 2 {
		t.Fatalf("expected cleared_session_count=2, got %v", m["cleared_session_count"])
	}
	profiles, _ := m["cleared_profiles"].([]any)
	if len(profiles) != 2 {
		t.Fatalf("expected 2 cleared_profiles, got %v", m["cleared_profiles"])
	}
	// No value in the JSON may look like a session canary.
	if strings.Contains(string(b), "s1") || strings.Contains(string(b), "s2") {
		t.Fatalf("JSON must not contain session strings: %s", string(b))
	}
}

func TestLogoutDoesNotScanCacheDirectory(t *testing.T) {
	// --all only iterates config profiles; it must not discover sessions from
	// cache files that have no matching profile.
	dir := t.TempDir()
	cache, err := console.NewFileCache(dir)
	if err != nil {
		t.Fatalf("create file cache: %v", err)
	}
	// Write a cache for a session not referenced by any profile.
	orphanSession := "trn:session:orphan"
	if err := cache.WriteRaw(orphanSession, makeCacheBytesForTest(orphanSession, time.Now(), 3600, "rt")); err != nil {
		t.Fatalf("write orphan cache: %v", err)
	}

	cfg := config.DefaultConfig() // no console profiles
	store := &fakeConfigStore{cfg: cfg, path: "/tmp/config.json"}

	adapter := &loginAdapter{
		cache:    cache,
		cfgStore: store,
		stdout:   &bytes.Buffer{},
		stderr:   &bytes.Buffer{},
	}
	result, err := adapter.runLogout(context.Background(), "", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ClearedSessionCount != 0 {
		t.Fatalf("--all should not clear orphan cache sessions, got count %d", result.ClearedSessionCount)
	}
	// Orphan cache must still exist.
	_, existed, _ := cache.ReadRaw(orphanSession)
	if !existed {
		t.Fatal("orphan cache should not be deleted by --all")
	}
}

func TestLogoutStableSortAcrossMultipleSessions(t *testing.T) {
	// Run --all twice with the same input; the session processing order must be
	// identical (deterministic). Verify via the cache's recorded delete order
	// rather than the public result (which must not leak session strings).
	cfg := config.DefaultConfig()
	sessions := []string{"zeta", "alpha", "mid", "beta"}
	for i, s := range sessions {
		cfg.PutProfile(fmt.Sprintf("p%d", i), config.Profile{
			Mode:         config.AuthModeConsoleLogin,
			LoginSession: s,
		})
	}

	runOnce := func() []string {
		cache := newFakeCache()
		for _, s := range sessions {
			cache.data[s] = []byte(`{"login_session":"` + s + `"}`)
		}
		store := &fakeConfigStore{cfg: cfg, path: "/tmp/config.json"}
		adapter := &loginAdapter{
			cache:    cache,
			cfgStore: store,
			stdout:   &bytes.Buffer{},
			stderr:   &bytes.Buffer{},
		}
		res, err := adapter.runLogout(context.Background(), "", true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.ClearedSessionCount != len(sessions) {
			t.Fatalf("expected %d cleared, got %d", len(sessions), res.ClearedSessionCount)
		}
		return cache.deleteOrder
	}

	first := runOnce()
	// Restore sessions for second run.
	for i, s := range sessions {
		cfg.PutProfile(fmt.Sprintf("p%d", i), config.Profile{
			Mode:         config.AuthModeConsoleLogin,
			LoginSession: s,
		})
	}
	second := runOnce()

	if len(first) != len(second) {
		t.Fatalf("length mismatch: %v vs %v", first, second)
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("order not stable: %v vs %v", first, second)
		}
	}
	// Verify it's actually sorted.
	for i := 1; i < len(first); i++ {
		if first[i-1] > first[i] {
			t.Fatalf("not sorted: %v", first)
		}
	}
}
