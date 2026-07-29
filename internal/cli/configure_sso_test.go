package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth/sso"
	"github.com/volcengine-tls/ve-tls-cli/internal/config"
	"github.com/volcengine-tls/ve-tls-cli/internal/output"
	"github.com/volcengine-tls/ve-tls-cli/internal/securestore"
)

// --- SSO test fakes ---

type fakeSSODeviceFlow struct {
	mu       sync.Mutex
	token    *sso.TokenCache
	err      error
	called   bool
	loginCnt int32
	// cache, if set, is written with the token on successful Login, mirroring
	// the real *sso.DeviceFlow which persists the token without holding the
	// caller's token lock.
	cache ssoCache
}

func (f *fakeSSODeviceFlow) Login(_ context.Context) (*sso.TokenCache, error) {
	atomic.AddInt32(&f.loginCnt, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.called = true
	if f.err != nil {
		return nil, f.err
	}
	if f.token == nil {
		return nil, errors.New("nil token")
	}
	if f.cache != nil {
		if werr := f.cache.WriteToken(f.token); werr != nil {
			return nil, werr
		}
	}
	return f.token, nil
}

type fakeSSOBindingService struct {
	result *sso.BindingResult
	err    error
}

func (f *fakeSSOBindingService) ResolveBinding(_ context.Context, _, _, _ string) (*sso.BindingResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

type fakeSSOCache struct {
	// mu guards the tokens/sts maps and the locks map for concurrent access. It
	// is intentionally separate from the per-key lock mutexes so that callbacks
	// running inside WithTokenLock/WithSTSLock can still call
	// ReadToken/WriteToken/etc. without deadlocking — mirroring the real
	// FileCache where the cross-process lock is independent of file I/O.
	mu sync.Mutex
	// locks holds one mutex per logical lock key, mirroring the real FileCache
	// which uses per-key file locks. This allows nested acquisition of
	// different keys (e.g. token lock + multiple STS locks) without
	// self-deadlock, which a single shared mutex could not do.
	locks map[string]*sync.Mutex
	// tokens keyed by "startURL|sessionName"
	tokens map[string]*sso.TokenCache
	// sts keyed by "sessionName|accountID|roleName"
	sts map[string]*sso.STSCache
	// lock tracking
	tokenLockCnt int32
	stsLockCnt   int32
	// write/delete call counters for verifying rollback executes at most once.
	writeTokenCnt  int32
	deleteTokenCnt int32
	writeSTSCnt    int32
	deleteSTSCnt   int32
	// blockTokenLock, if non-nil, is closed when WithTokenLock acquires the mutex
	// and the caller must close proceed to let it finish.
	blockTokenLock chan struct{}
	proceedToken   chan struct{}
	// tokenLockAttempted, if non-nil, is closed when WithTokenLock is entered
	// (before acquiring the mutex). Tests use this to prove a second participant
	// has attempted to acquire the lock instead of relying on Sleep.
	tokenLockAttempted chan struct{}
	// stsLockAttempted, if non-nil, is closed when WithSTSLock is entered
	// (before acquiring the mutex).
	stsLockAttempted chan struct{}
	// failReadToken forces ReadToken to return an error
	failReadToken error
	// failWriteToken forces WriteToken to return an error
	failWriteToken error
	// failDeleteToken forces DeleteToken to return an error
	failDeleteToken error
	// failDeleteSTS forces DeleteSTS to return an error for all keys
	failDeleteSTS error
	// failDeleteSTSKeys, if non-nil, forces DeleteSTS to return the mapped error
	// for the given "session|account|role" key. Takes precedence over failDeleteSTS.
	failDeleteSTSKeys map[string]error
}

func newFakeSSOCache() *fakeSSOCache {
	return &fakeSSOCache{
		locks:  make(map[string]*sync.Mutex),
		tokens: make(map[string]*sso.TokenCache),
		sts:    make(map[string]*sso.STSCache),
	}
}

func ssoTokenKey(startURL, sessionName string) string {
	return startURL + "|" + sessionName
}

func stsCacheKey(sessionName, accountID, roleName string) string {
	return sessionName + "|" + accountID + "|" + roleName
}

// lockFor returns the mutex for the given key, creating it if necessary.
func (f *fakeSSOCache) lockFor(key string) *sync.Mutex {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.locks[key]
	if !ok {
		m = &sync.Mutex{}
		f.locks[key] = m
	}
	return m
}

func (f *fakeSSOCache) WithTokenLock(_ context.Context, startURL, sessionName string, fn func() error) error {
	atomic.AddInt32(&f.tokenLockCnt, 1)
	if f.tokenLockAttempted != nil {
		close(f.tokenLockAttempted)
		f.tokenLockAttempted = nil
	}
	mu := f.lockFor("token:" + ssoTokenKey(startURL, sessionName))
	mu.Lock()
	defer mu.Unlock()
	if f.blockTokenLock != nil {
		close(f.blockTokenLock)
		<-f.proceedToken
	}
	return fn()
}

func (f *fakeSSOCache) ReadToken(startURL, sessionName string) (*sso.TokenCache, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failReadToken != nil {
		return nil, f.failReadToken
	}
	t, ok := f.tokens[ssoTokenKey(startURL, sessionName)]
	if !ok {
		return nil, securestore.ErrMissing
	}
	// Return a deep copy so callers cannot mutate the cached version.
	cp := *t
	return &cp, nil
}

func (f *fakeSSOCache) WriteToken(cache *sso.TokenCache) error {
	atomic.AddInt32(&f.writeTokenCnt, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failWriteToken != nil {
		return f.failWriteToken
	}
	cp := *cache
	f.tokens[ssoTokenKey(cache.StartURL, cache.SessionName)] = &cp
	return nil
}

func (f *fakeSSOCache) DeleteToken(startURL, sessionName string) error {
	atomic.AddInt32(&f.deleteTokenCnt, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failDeleteToken != nil {
		return f.failDeleteToken
	}
	delete(f.tokens, ssoTokenKey(startURL, sessionName))
	return nil
}

func (f *fakeSSOCache) WithSTSLock(_ context.Context, sessionName, accountID, roleName string, fn func() error) error {
	atomic.AddInt32(&f.stsLockCnt, 1)
	if f.stsLockAttempted != nil {
		close(f.stsLockAttempted)
		f.stsLockAttempted = nil
	}
	mu := f.lockFor("sts:" + stsCacheKey(sessionName, accountID, roleName))
	mu.Lock()
	defer mu.Unlock()
	return fn()
}

func (f *fakeSSOCache) ReadSTS(sessionName, accountID, roleName string) (*sso.STSCache, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sts[stsCacheKey(sessionName, accountID, roleName)]
	if !ok {
		return nil, securestore.ErrMissing
	}
	cp := *s
	return &cp, nil
}

func (f *fakeSSOCache) WriteSTS(cache *sso.STSCache) error {
	atomic.AddInt32(&f.writeSTSCnt, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *cache
	f.sts[stsCacheKey(cache.SessionName, cache.AccountID, cache.RoleName)] = &cp
	return nil
}

func (f *fakeSSOCache) DeleteSTS(sessionName, accountID, roleName string) error {
	atomic.AddInt32(&f.deleteSTSCnt, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failDeleteSTSKeys != nil {
		if e, ok := f.failDeleteSTSKeys[stsCacheKey(sessionName, accountID, roleName)]; ok {
			return e
		}
	}
	if f.failDeleteSTS != nil {
		return f.failDeleteSTS
	}
	delete(f.sts, stsCacheKey(sessionName, accountID, roleName))
	return nil
}

type fakeSSORevoker struct {
	err     error
	called  bool
	lastReq *sso.RevokeTokenRequest
	callCnt int32
}

func (f *fakeSSORevoker) RevokeToken(_ context.Context, req *sso.RevokeTokenRequest) error {
	atomic.AddInt32(&f.callCnt, 1)
	f.called = true
	f.lastReq = req
	return f.err
}

// --- Test helpers ---

func testSSOSession() config.SSOSession {
	return config.SSOSession{
		Name:               "corp",
		StartURL:           "https://example.volccloudidentity.com/userportal",
		Region:             "cn-beijing",
		RegistrationScopes: []string{sso.ScopeAccountAccess, sso.ScopeOfflineAccess},
	}
}

func testTokenCache() *sso.TokenCache {
	return &sso.TokenCache{
		StartURL:     "https://example.volccloudidentity.com/userportal",
		SessionName:  "corp",
		AccessToken:  "access-token-canary-12345678",
		ExpiresAt:    time.Now().Add(1 * time.Hour).UTC().Format(time.RFC3339),
		ClientID:     "client-id-canary",
		ClientSecret: "client-secret-canary",
		RefreshToken: "refresh-token-canary",
		Region:       "cn-beijing",
	}
}

func TestTokensEqual(t *testing.T) {
	token := testTokenCache()
	equalToken := *token
	differentToken := *token
	differentToken.Region = "cn-shanghai"

	tests := []struct {
		name string
		a    *sso.TokenCache
		b    *sso.TokenCache
		want bool
	}{
		{name: "both nil", want: true},
		{name: "left nil", b: token, want: false},
		{name: "right nil", a: token, want: false},
		{name: "equal non-nil", a: token, b: &equalToken, want: true},
		{name: "different field", a: token, b: &differentToken, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tokensEqual(tt.a, tt.b); got != tt.want {
				t.Fatalf("tokensEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}

func testConfigWithSession() config.Config {
	cfg := config.DefaultConfig()
	cfg.SSOSessions["corp"] = testSSOSession()
	cfg.Profiles["default"] = config.Profile{
		Region:          "cn-beijing",
		Endpoint:        "https://tls-cn-beijing.volces.com",
		TimeoutSeconds:  60,
		AccessKeyID:     "AKLTlegacy",
		SecretAccessKey: "legacy-secret",
		SecurityToken:   "legacy-token",
		CredRef:         "legacy-cred",
		Mode:            config.AuthModeConsoleLogin,
		LoginSession:    "legacy-login-session",
	}
	cfg.CurrentProfile = "default"
	return cfg
}

func newSSOTestContext(t *testing.T, cfg config.Config) (*Context, string) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	if err := config.Save(cfg, cfgPath); err != nil {
		t.Fatalf("save config: %v", err)
	}
	t.Setenv("VOLCLOG_CONFIG", cfgPath)
	ctx := newContext(&bytes.Buffer{}, &bytes.Buffer{}, output.FormatJSON, "", "")
	if err := ctx.LoadConfig(); err != nil {
		t.Fatalf("load config: %v", err)
	}
	return ctx, cfgPath
}

func newSSOAdapterForTest(cache *fakeSSOCache, cfgStore configStore, deviceFlow ssoDeviceFlow, binding ssoBindingService, revoker ssoOAuthRevoker) *ssoAdapter {
	// Wire the fake device flow to the cache so Login persists the token,
	// matching real *sso.DeviceFlow behavior.
	if df, ok := deviceFlow.(*fakeSSODeviceFlow); ok {
		df.cache = cache
	}
	return &ssoAdapter{
		cache:    cache,
		cfgStore: cfgStore,
		stdout:   &bytes.Buffer{},
		stderr:   &bytes.Buffer{},
		clock:    time.Now,
		deviceFlowFn: func(_ config.SSOSession, _ bool) (ssoDeviceFlow, error) {
			return deviceFlow, nil
		},
		bindingFn: func(_ config.SSOSession) (ssoBindingService, error) {
			return binding, nil
		},
		revokerFn: func(_ string) (ssoOAuthRevoker, error) {
			return revoker, nil
		},
	}
}

// --- configure sso-session tests ---

func TestConfigureSSOSessionDefaultsScopes(t *testing.T) {
	ctx, _ := newSSOTestContext(t, config.DefaultConfig())
	out, err := runConfigureSSOSession(ctx, []string{
		"--name", "corp",
		"--start-url", "https://example.volccloudidentity.com/userportal",
		"--region", "cn-beijing",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := out.(map[string]any)
	scopes, _ := m["registration_scopes"].([]string)
	if len(scopes) != 2 {
		t.Fatalf("expected 2 default scopes, got %v", scopes)
	}
	// Must be the frozen allowed scopes, sorted.
	want := []string{sso.ScopeAccountAccess, sso.ScopeOfflineAccess}
	for i, s := range want {
		if scopes[i] != s {
			t.Fatalf("scope[%d] = %q, want %q", i, scopes[i], s)
		}
	}
}

func TestConfigureSSOSessionExistingEmptyScopesDefaultsWhenOmitted(t *testing.T) {
	for _, tc := range []struct {
		name   string
		scopes []string
	}{
		{name: "nil", scopes: nil},
		{name: "empty", scopes: []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.SSOSessions["corp"] = config.SSOSession{
				Name:               "corp",
				StartURL:           "https://old.example.com/userportal",
				Region:             "cn-shanghai",
				RegistrationScopes: tc.scopes,
			}
			ctx, _ := newSSOTestContext(t, cfg)

			out, err := runConfigureSSOSession(ctx, []string{
				"--name", "corp",
				"--start-url", "https://new.example.com/userportal",
				"--region", "cn-beijing",
			})
			if err != nil {
				t.Fatalf("runConfigureSSOSession: %v", err)
			}

			want := []string{sso.ScopeAccountAccess, sso.ScopeOfflineAccess}
			saved, _, err := config.Load()
			if err != nil {
				t.Fatalf("config.Load: %v", err)
			}
			if got := saved.SSOSessions["corp"].RegistrationScopes; !equalStringSlice(got, want) {
				t.Fatalf("saved scopes=%v, want defaults %v", got, want)
			}
			result := out.(map[string]any)
			if got, _ := result["registration_scopes"].([]string); !equalStringSlice(got, want) {
				t.Fatalf("result scopes=%v, want defaults %v", got, want)
			}
		})
	}
}

func TestSSOScopesOrDefaultCopiesDefaultsForLegacyEmptyScopes(t *testing.T) {
	got := ssoScopesOrDefault(nil)
	want := []string{sso.ScopeAccountAccess, sso.ScopeOfflineAccess}
	if !equalStringSlice(got, want) {
		t.Fatalf("scopes=%v, want defaults %v", got, want)
	}

	got[0] = "mutated"
	if defaultSSOScopes[0] != sso.ScopeAccountAccess {
		t.Fatalf("default scopes were aliased and mutated: %v", defaultSSOScopes)
	}
}

func TestConfigureSSOSessionRejectsUnknownScope(t *testing.T) {
	ctx, _ := newSSOTestContext(t, config.DefaultConfig())
	_, err := runConfigureSSOSession(ctx, []string{
		"--name", "corp",
		"--start-url", "https://example.volccloudidentity.com/userportal",
		"--region", "cn-beijing",
		"--registration-scopes", "cloudidentity:account:access,unknown-scope",
	})
	if err == nil {
		t.Fatal("expected error for unknown scope, got nil")
	}
	if !strings.Contains(err.Error(), "unknown registration scope") {
		t.Fatalf("expected unknown scope error, got: %v", err)
	}
}

func TestConfigureSSOSessionPatchPreservesOmittedFields(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.SSOSessions["corp"] = config.SSOSession{
		Name:               "corp",
		StartURL:           "https://old.example.com/userportal",
		Region:             "cn-shanghai",
		RegistrationScopes: []string{sso.ScopeAccountAccess},
	}
	ctx, _ := newSSOTestContext(t, cfg)
	// Patch only name and region; start-url and scopes must be preserved.
	_, err := runConfigureSSOSession(ctx, []string{
		"--name", "corp",
		"--start-url", "https://new.example.com/userportal",
		"--region", "cn-beijing",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, _, _ := config.Load()
	s := got.SSOSessions["corp"]
	if s.StartURL != "https://new.example.com/userportal" {
		t.Fatalf("start-url not updated: %q", s.StartURL)
	}
	if s.Region != "cn-beijing" {
		t.Fatalf("region not updated: %q", s.Region)
	}
	// Scopes were not provided, so they should be reset to defaults (the contract
	// says omitted fields are preserved, but scopes is a special case: when the
	// flag is omitted we use defaults). Actually, re-reading the spec: "patch时
	// 遗漏字段保持旧值". So scopes should be preserved.
	if len(s.RegistrationScopes) != 1 || s.RegistrationScopes[0] != sso.ScopeAccountAccess {
		t.Fatalf("scopes should be preserved when omitted, got %v", s.RegistrationScopes)
	}
}

func TestConfigureSSODoesNotChangeCurrentProfile(t *testing.T) {
	cfg := testConfigWithSession()
	cfg.CurrentProfile = "default"
	cache := newFakeSSOCache()
	cfgStore := &fakeConfigStore{cfg: cfg, path: ""}
	df := &fakeSSODeviceFlow{token: testTokenCache()}
	bs := &fakeSSOBindingService{result: &sso.BindingResult{AccountID: "acct-1", RoleName: "role-1"}}
	adapter := newSSOAdapterForTest(cache, cfgStore, df, bs, nil)
	_, err := adapter.runConfigureSSO(context.Background(), ssoConfigureOpts{
		Profile:    "default",
		SSOSession: "corp",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// CurrentProfile must remain "default".
	if cfgStore.cfg.CurrentProfile != "default" {
		t.Fatalf("current profile changed to %q", cfgStore.cfg.CurrentProfile)
	}
}

func TestParseConfigureSSOSupportsTLSRuntimeFields(t *testing.T) {
	got, err := parseSSOConfigureFlags([]string{
		"--profile", "prod",
		"--sso-session", "corp",
		"--region", "cn-shanghai",
		"--endpoint", "https://tls-cn-shanghai.volces.com",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := ssoConfigureOpts{
		Profile:    "prod",
		SSOSession: "corp",
		Region:     "cn-shanghai",
		Endpoint:   "https://tls-cn-shanghai.volces.com",
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestConfigureSSOPreservesTLSAndDormantStaticFields(t *testing.T) {
	cfg := testConfigWithSession()
	cache := newFakeSSOCache()
	cfgStore := &fakeConfigStore{cfg: cfg, path: ""}
	df := &fakeSSODeviceFlow{token: testTokenCache()}
	bs := &fakeSSOBindingService{result: &sso.BindingResult{AccountID: "acct-1", RoleName: "role-1"}}
	adapter := newSSOAdapterForTest(cache, cfgStore, df, bs, nil)
	_, err := adapter.runConfigureSSO(context.Background(), ssoConfigureOpts{
		Profile:    "default",
		SSOSession: "corp",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	p := cfgStore.cfg.Profiles["default"]
	if p.Region != "cn-beijing" {
		t.Fatalf("TLS region overwritten: %q", p.Region)
	}
	if p.Endpoint != "https://tls-cn-beijing.volces.com" {
		t.Fatalf("TLS endpoint overwritten: %q", p.Endpoint)
	}
	if p.TimeoutSeconds != 60 {
		t.Fatalf("timeout overwritten: %d", p.TimeoutSeconds)
	}
	if p.AccessKeyID != "AKLTlegacy" {
		t.Fatalf("dormant AK overwritten: %q", p.AccessKeyID)
	}
	if p.SecretAccessKey != "legacy-secret" {
		t.Fatalf("dormant SK overwritten: %q", p.SecretAccessKey)
	}
	if p.SecurityToken != "legacy-token" {
		t.Fatalf("dormant token overwritten: %q", p.SecurityToken)
	}
	if p.CredRef != "legacy-cred" {
		t.Fatalf("dormant cred-ref overwritten: %q", p.CredRef)
	}
}

func TestConfigureSSOClearsOnlyConsoleBinding(t *testing.T) {
	cfg := testConfigWithSession()
	cache := newFakeSSOCache()
	cfgStore := &fakeConfigStore{cfg: cfg, path: ""}
	df := &fakeSSODeviceFlow{token: testTokenCache()}
	bs := &fakeSSOBindingService{result: &sso.BindingResult{AccountID: "acct-1", RoleName: "role-1"}}
	adapter := newSSOAdapterForTest(cache, cfgStore, df, bs, nil)
	_, err := adapter.runConfigureSSO(context.Background(), ssoConfigureOpts{
		Profile:    "default",
		SSOSession: "corp",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	p := cfgStore.cfg.Profiles["default"]
	if p.LoginSession != "" {
		t.Fatalf("console login-session should be cleared, got %q", p.LoginSession)
	}
	if p.Mode != config.AuthModeSSO {
		t.Fatalf("mode should be sso, got %q", p.Mode)
	}
	if p.SSOSessionName != "corp" {
		t.Fatalf("sso-session-name should be corp, got %q", p.SSOSessionName)
	}
	if p.AccountID != "acct-1" {
		t.Fatalf("account-id should be acct-1, got %q", p.AccountID)
	}
	if p.RoleName != "role-1" {
		t.Fatalf("role-name should be role-1, got %q", p.RoleName)
	}
}

func TestConfigureSSOAllowsAuthBeforeTLSRuntimeConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.SSOSessions["corp"] = testSSOSession()
	// Profile has no TLS endpoint/region yet.
	cfg.Profiles["newprof"] = config.Profile{}
	cache := newFakeSSOCache()
	cfgStore := &fakeConfigStore{cfg: cfg, path: ""}
	df := &fakeSSODeviceFlow{token: testTokenCache()}
	bs := &fakeSSOBindingService{result: &sso.BindingResult{AccountID: "acct-1", RoleName: "role-1"}}
	adapter := newSSOAdapterForTest(cache, cfgStore, df, bs, nil)
	_, err := adapter.runConfigureSSO(context.Background(), ssoConfigureOpts{
		Profile:    "newprof",
		SSOSession: "corp",
	})
	if err != nil {
		t.Fatalf("expected auth to succeed before TLS config, got: %v", err)
	}
}

func TestConfigureSSORegionNeverOverwritesTLSRegion(t *testing.T) {
	cfg := testConfigWithSession()
	// Set a different TLS region.
	p := cfg.Profiles["default"]
	p.Region = "us-east-1"
	cfg.Profiles["default"] = p
	cache := newFakeSSOCache()
	cfgStore := &fakeConfigStore{cfg: cfg, path: ""}
	df := &fakeSSODeviceFlow{token: testTokenCache()}
	bs := &fakeSSOBindingService{result: &sso.BindingResult{AccountID: "acct-1", RoleName: "role-1"}}
	adapter := newSSOAdapterForTest(cache, cfgStore, df, bs, nil)
	_, err := adapter.runConfigureSSO(context.Background(), ssoConfigureOpts{
		Profile:    "default",
		SSOSession: "corp",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := cfgStore.cfg.Profiles["default"]
	if got.Region != "us-east-1" {
		t.Fatalf("TLS region was overwritten by SSO region: got %q, want us-east-1", got.Region)
	}
}

func TestConfigureSSOPersistsExplicitTLSRuntimeFieldsSeparatelyFromSSORegion(t *testing.T) {
	cfg := testConfigWithSession()
	cache := newFakeSSOCache()
	cfgStore := &fakeConfigStore{cfg: cfg, path: ""}
	df := &fakeSSODeviceFlow{token: testTokenCache()}
	bs := &fakeSSOBindingService{result: &sso.BindingResult{AccountID: "acct-1", RoleName: "role-1"}}
	adapter := newSSOAdapterForTest(cache, cfgStore, df, bs, nil)

	result, err := adapter.runConfigureSSO(context.Background(), ssoConfigureOpts{
		Profile:    "default",
		SSOSession: "corp",
		Region:     "cn-shanghai",
		Endpoint:   "https://tls-cn-shanghai.volces.com",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	p := cfgStore.cfg.Profiles["default"]
	if p.Region != "cn-shanghai" {
		t.Fatalf("TLS region = %q, want %q", p.Region, "cn-shanghai")
	}
	if p.Endpoint != "https://tls-cn-shanghai.volces.com" {
		t.Fatalf("TLS endpoint = %q, want explicit value", p.Endpoint)
	}
	if result.Region != "cn-shanghai" || result.Endpoint != "https://tls-cn-shanghai.volces.com" {
		t.Fatalf("result TLS runtime = region %q endpoint %q", result.Region, result.Endpoint)
	}
	if result.SSORegion != "cn-beijing" {
		t.Fatalf("result SSO region = %q, want auth region %q", result.SSORegion, "cn-beijing")
	}
}

func TestConfigureSSOCreatesMissingProfileWithoutInventingTLSRuntimeConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.SSOSessions["corp"] = testSSOSession()
	cache := newFakeSSOCache()
	cfgStore := &fakeConfigStore{cfg: cfg, path: ""}
	df := &fakeSSODeviceFlow{token: testTokenCache()}
	bs := &fakeSSOBindingService{result: &sso.BindingResult{AccountID: "acct-1", RoleName: "role-1"}}
	adapter := newSSOAdapterForTest(cache, cfgStore, df, bs, nil)

	result, err := adapter.runConfigureSSO(context.Background(), ssoConfigureOpts{
		Profile:    "new-profile",
		SSOSession: "corp",
	})
	if err != nil {
		t.Fatalf("expected missing profile to be created, got: %v", err)
	}

	p, ok := cfgStore.cfg.GetProfile("new-profile")
	if !ok {
		t.Fatal("new profile was not created")
	}
	if p.Mode != config.AuthModeSSO || p.SSOSessionName != "corp" {
		t.Fatalf("unexpected new profile binding: %+v", p)
	}
	if p.Region != "" || p.Endpoint != "" {
		t.Fatalf("SSO auth region leaked into TLS runtime config: region=%q endpoint=%q", p.Region, p.Endpoint)
	}
	if result.Region != "" || result.Endpoint != "" || result.SSORegion != "cn-beijing" {
		t.Fatalf("unexpected result regions: %+v", result)
	}
}

func TestConfigureSSOFailureLeavesOldBindingAndCacheUsable(t *testing.T) {
	cfg := testConfigWithSession()
	// Pre-populate the token cache with an old token.
	cache := newFakeSSOCache()
	oldToken := testTokenCache()
	oldToken.AccessToken = "old-access-token-12345678"
	cache.tokens[ssoTokenKey(oldToken.StartURL, oldToken.SessionName)] = oldToken
	cfgStore := &fakeConfigStore{cfg: cfg, path: ""}
	// Make the binding service fail after login succeeds.
	df := &fakeSSODeviceFlow{token: testTokenCache()}
	bs := &fakeSSOBindingService{err: errors.New("binding failed")}
	adapter := newSSOAdapterForTest(cache, cfgStore, df, bs, nil)
	_, err := adapter.runConfigureSSO(context.Background(), ssoConfigureOpts{
		Profile:    "default",
		SSOSession: "corp",
	})
	if err == nil {
		t.Fatal("expected error from binding failure, got nil")
	}
	// The old token cache must be restored.
	restored, rerr := cache.ReadToken(oldToken.StartURL, oldToken.SessionName)
	if rerr != nil {
		t.Fatalf("old token cache should still exist, got error: %v", rerr)
	}
	if restored.AccessToken != "old-access-token-12345678" {
		t.Fatalf("old token not restored, got %q", restored.AccessToken)
	}
	// The old binding (console-login mode) must be unchanged.
	p := cfgStore.cfg.Profiles["default"]
	if p.Mode != config.AuthModeConsoleLogin {
		t.Fatalf("old mode should be unchanged, got %q", p.Mode)
	}
	if p.LoginSession != "legacy-login-session" {
		t.Fatalf("old login-session should be unchanged, got %q", p.LoginSession)
	}
}

// --- Token lock transaction tests ---

func TestConfigureSSOTokenLockSnapshotsAndRestoresOnFailure(t *testing.T) {
	cfg := testConfigWithSession()
	cache := newFakeSSOCache()
	oldToken := testTokenCache()
	oldToken.AccessToken = "old-token-12345678"
	cache.tokens[ssoTokenKey(oldToken.StartURL, oldToken.SessionName)] = oldToken
	cfgStore := &fakeConfigStore{cfg: cfg, path: ""}
	df := &fakeSSODeviceFlow{token: testTokenCache()}
	// Make config update fail.
	cfgStore.updateErr = errors.New("config write failed")
	bs := &fakeSSOBindingService{result: &sso.BindingResult{AccountID: "acct-1", RoleName: "role-1"}}
	adapter := newSSOAdapterForTest(cache, cfgStore, df, bs, nil)
	_, err := adapter.runConfigureSSO(context.Background(), ssoConfigureOpts{
		Profile:    "default",
		SSOSession: "corp",
	})
	if err == nil {
		t.Fatal("expected error from config failure")
	}
	// Old token must be restored exactly.
	restored, rerr := cache.ReadToken(oldToken.StartURL, oldToken.SessionName)
	if rerr != nil {
		t.Fatalf("old token should be restored: %v", rerr)
	}
	if restored.AccessToken != oldToken.AccessToken {
		t.Fatalf("old token not restored: got %q want %q", restored.AccessToken, oldToken.AccessToken)
	}
}

func TestConfigureSSOMissingTokenStaysMissingOnFailure(t *testing.T) {
	cfg := testConfigWithSession()
	cache := newFakeSSOCache() // no token pre-populated
	cfgStore := &fakeConfigStore{cfg: cfg, path: ""}
	df := &fakeSSODeviceFlow{token: testTokenCache()}
	cfgStore.updateErr = errors.New("config write failed")
	bs := &fakeSSOBindingService{result: &sso.BindingResult{AccountID: "acct-1", RoleName: "role-1"}}
	adapter := newSSOAdapterForTest(cache, cfgStore, df, bs, nil)
	_, err := adapter.runConfigureSSO(context.Background(), ssoConfigureOpts{
		Profile:    "default",
		SSOSession: "corp",
	})
	if err == nil {
		t.Fatal("expected error from config failure")
	}
	// Token should still be missing (deleted after the failed login wrote it).
	_, rerr := cache.ReadToken(testSSOSession().StartURL, testSSOSession().Name)
	if !errors.Is(rerr, securestore.ErrMissing) {
		t.Fatalf("token should be missing after failed login, got err=%v", rerr)
	}
}

// --- Output redaction test ---

func TestConfigureSSORebindRestoresOldSTSOnConfigFailure(t *testing.T) {
	cfg := testConfigWithSession()
	// Profile is bound to old session "corp" with acct-1/role-1.
	p := cfg.Profiles["default"]
	p.Mode = config.AuthModeSSO
	p.SSOSessionName = "corp"
	p.AccountID = "acct-1"
	p.RoleName = "role-1"
	cfg.Profiles["default"] = p
	// Add a second session "newcorp" that we rebind to.
	cfg.SSOSessions["newcorp"] = config.SSOSession{
		Name: "newcorp", StartURL: "https://newcorp.example.com", Region: "cn-beijing",
		RegistrationScopes: defaultSSOScopes,
	}

	cache := newFakeSSOCache()
	// Seed old token + old STS for the original binding.
	oldToken := testTokenCache()
	oldToken.AccessToken = "old-token"
	cache.tokens[ssoTokenKey(oldToken.StartURL, oldToken.SessionName)] = oldToken
	oldSTS := &sso.STSCache{SessionName: "corp", AccountID: "acct-1", RoleName: "role-1"}
	cache.sts[stsCacheKey("corp", "acct-1", "role-1")] = oldSTS

	// Force config update to fail AFTER the old STS is deleted.
	cfgStore := &fakeConfigStore{cfg: cfg, path: ""}
	cfgStore.updateErr = errors.New("config write failed")
	// Device flow writes a token for the NEW session "newcorp".
	newToken := testTokenCache()
	newToken.StartURL = "https://newcorp.example.com"
	newToken.SessionName = "newcorp"
	newToken.AccessToken = "new-token"
	df := &fakeSSODeviceFlow{token: newToken}
	bs := &fakeSSOBindingService{result: &sso.BindingResult{AccountID: "acct-2", RoleName: "role-2"}}
	adapter := newSSOAdapterForTest(cache, cfgStore, df, bs, nil)
	_, err := adapter.runConfigureSSO(context.Background(), ssoConfigureOpts{
		Profile:    "default",
		SSOSession: "newcorp",
	})
	if err == nil {
		t.Fatal("expected error from config failure")
	}
	// Old token must be restored (not the new one, not missing).
	restored, rerr := cache.ReadToken(oldToken.StartURL, oldToken.SessionName)
	if rerr != nil {
		t.Fatalf("old token should be restored: %v", rerr)
	}
	if restored.AccessToken != "old-token" {
		t.Fatalf("old token not restored: got %q want %q", restored.AccessToken, "old-token")
	}
	// Old STS must be restored (not deleted, since config failed).
	restoredSTS, serr := cache.ReadSTS("corp", "acct-1", "role-1")
	if serr != nil {
		t.Fatalf("old STS should be restored: %v", serr)
	}
	if restoredSTS == nil {
		t.Fatal("old STS should not be nil after failed rebind")
	}
}

func TestConfigureSSOLoginFailsBeforeWritePreservesOldToken(t *testing.T) {
	cfg := testConfigWithSession()
	cache := newFakeSSOCache()
	oldToken := testTokenCache()
	oldToken.AccessToken = "old-token-before-write"
	cache.tokens[ssoTokenKey(oldToken.StartURL, oldToken.SessionName)] = oldToken
	cfgStore := &fakeConfigStore{cfg: cfg, path: ""}
	// Device flow fails before writing any token (e.g. network error).
	df := &fakeSSODeviceFlow{err: errors.New("device flow network error")}
	bs := &fakeSSOBindingService{result: &sso.BindingResult{AccountID: "acct-1", RoleName: "role-1"}}
	adapter := newSSOAdapterForTest(cache, cfgStore, df, bs, nil)
	_, err := adapter.runConfigureSSO(context.Background(), ssoConfigureOpts{
		Profile:    "default",
		SSOSession: "corp",
	})
	if err == nil {
		t.Fatal("expected error from device flow failure")
	}
	// Old token must be completely unchanged — nothing was written, so no
	// rollback should have touched it.
	restored, rerr := cache.ReadToken(oldToken.StartURL, oldToken.SessionName)
	if rerr != nil {
		t.Fatalf("old token should still exist: %v", rerr)
	}
	if restored.AccessToken != "old-token-before-write" {
		t.Fatalf("old token changed: got %q want %q", restored.AccessToken, "old-token-before-write")
	}
	// Must NOT be classified as ErrSSORollbackFailure — nothing was rolled back.
	if errors.Is(err, ErrSSORollbackFailure) {
		t.Fatalf("pre-write failure should not be a rollback failure: %v", err)
	}
}

// --- Output redaction test ---

func TestSSOOutputNeverContainsOAuthOrSTSTokens(t *testing.T) {
	cfg := testConfigWithSession()
	cache := newFakeSSOCache()
	cfgStore := &fakeConfigStore{cfg: cfg, path: ""}
	tok := testTokenCache()
	df := &fakeSSODeviceFlow{token: tok}
	bs := &fakeSSOBindingService{result: &sso.BindingResult{AccountID: "acct-1", RoleName: "role-1"}}
	adapter := newSSOAdapterForTest(cache, cfgStore, df, bs, nil)
	res, err := adapter.runConfigureSSO(context.Background(), ssoConfigureOpts{
		Profile:    "default",
		SSOSession: "corp",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	b, _ := json.Marshal(res)
	out := string(b)
	for _, secret := range []string{
		tok.AccessToken,
		tok.RefreshToken,
		tok.ClientSecret,
		"access-token-canary",
		"refresh-token-canary",
		"client-secret-canary",
	} {
		if strings.Contains(out, secret) {
			t.Fatalf("output contains secret %q: %s", secret, out)
		}
	}
}

// --- Configure SSO via Run-level dispatch ---

func TestConfigureSSODispatchRejectsSecretsFile(t *testing.T) {
	cfg := testConfigWithSession()
	ctx, _ := newSSOTestContext(t, cfg)
	ctx.GlobalSecretsFile = "/some/path"
	_, err := runConfigureSSOWithFactory(ctx, []string{"--profile", "default", "--sso-session", "corp"}, func(_ *Context) (*ssoAdapter, error) {
		t.Fatal("factory should not be called when secrets-file is set")
		return nil, nil
	})
	if err == nil {
		t.Fatal("expected error for secrets-file")
	}
}

// ensure fmt import is used
var _ = fmt.Sprintf
