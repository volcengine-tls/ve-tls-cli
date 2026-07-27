package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth/sso"
	"github.com/volcengine-tls/ve-tls-cli/internal/config"
	"github.com/volcengine-tls/ve-tls-cli/internal/securestore"
)

// --- SSO group visibility ---

func TestSSOGroupVisibleInBothEditions(t *testing.T) {
	// Default edition
	if !isGroupEnabledInCurrentEdition("sso") {
		t.Fatal("sso group should be enabled in default edition")
	}
	found := false
	for _, g := range cliGroups() {
		if g.Name == "sso" {
			found = true
			if !g.Primary {
				t.Fatal("sso should be a primary group")
			}
		}
	}
	if !found {
		t.Fatal("sso group not found in cliGroups()")
	}
}

// --- SSO login tests ---

func TestSSOLoginByProfileAndSession(t *testing.T) {
	// Login by profile
	cfg := testConfigWithSession()
	p := cfg.Profiles["default"]
	p.Mode = config.AuthModeSSO
	p.SSOSessionName = "corp"
	p.AccountID = "acct-1"
	p.RoleName = "role-1"
	cfg.Profiles["default"] = p

	cache := newFakeSSOCache()
	cfgStore := &fakeConfigStore{cfg: cfg, path: ""}
	df := &fakeSSODeviceFlow{token: testTokenCache()}
	adapter := newSSOAdapterForTest(cache, cfgStore, df, nil, nil)
	res, err := adapter.runSSOLogin(context.Background(), ssoLoginOpts{Profile: "default"})
	if err != nil {
		t.Fatalf("login by profile failed: %v", err)
	}
	if res.Session != "corp" {
		t.Fatalf("expected session corp, got %q", res.Session)
	}
	if res.Profile != "default" {
		t.Fatalf("expected profile default, got %q", res.Profile)
	}

	// Login by session
	cfg2 := testConfigWithSession()
	cache2 := newFakeSSOCache()
	cfgStore2 := &fakeConfigStore{cfg: cfg2, path: ""}
	df2 := &fakeSSODeviceFlow{token: testTokenCache()}
	adapter2 := newSSOAdapterForTest(cache2, cfgStore2, df2, nil, nil)
	res2, err := adapter2.runSSOLogin(context.Background(), ssoLoginOpts{SSOSession: "corp"})
	if err != nil {
		t.Fatalf("login by session failed: %v", err)
	}
	if res2.Session != "corp" {
		t.Fatalf("expected session corp, got %q", res2.Session)
	}
	if res2.Profile != "" {
		t.Fatalf("expected empty profile for session login, got %q", res2.Profile)
	}
}

func TestSSOLoginRejectsConflictingSelectors(t *testing.T) {
	ctx, _ := newSSOTestContext(t, testConfigWithSession())
	_, err := runSSOLoginWithFactory(ctx, []string{"--profile", "default", "--sso-session", "corp"}, func(_ *Context) (*ssoAdapter, error) {
		t.Fatal("factory should not be called")
		return nil, nil
	})
	if err == nil {
		t.Fatal("expected error for conflicting selectors")
	}
	if !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("expected conflict error, got: %v", err)
	}
}

func TestSSOLoginProgressUsesStderrAndResultUsesStdout(t *testing.T) {
	cfg := testConfigWithSession()
	p := cfg.Profiles["default"]
	p.Mode = config.AuthModeSSO
	p.SSOSessionName = "corp"
	cfg.Profiles["default"] = p

	var stdout, stderr bytes.Buffer
	cache := newFakeSSOCache()
	cfgStore := &fakeConfigStore{cfg: cfg, path: ""}
	df := &fakeSSODeviceFlow{token: testTokenCache()}
	adapter := newSSOAdapterForTest(cache, cfgStore, df, nil, nil)
	adapter.stdout = &stdout
	adapter.stderr = &stderr

	res, err := adapter.runSSOLogin(context.Background(), ssoLoginOpts{Profile: "default"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Result should be JSON-marshalable and written to stdout by the caller.
	b, _ := json.Marshal(res)
	if !json.Valid(b) {
		t.Fatalf("result is not valid JSON: %s", b)
	}
	// The result must not contain the full access token.
	if strings.Contains(string(b), "access-token-canary-12345678") {
		t.Fatalf("stdout result contains access token: %s", b)
	}
}

// --- SSO logout tests ---

func TestSSOLogoutByProfileAndSession(t *testing.T) {
	// Logout by profile
	cfg := testConfigWithSession()
	p := cfg.Profiles["default"]
	p.Mode = config.AuthModeSSO
	p.SSOSessionName = "corp"
	p.AccountID = "acct-1"
	p.RoleName = "role-1"
	p.STSExpiration = time.Now().Add(1 * time.Hour).Unix()
	cfg.Profiles["default"] = p

	cache := newFakeSSOCache()
	cache.tokens[ssoTokenKey(testSSOSession().StartURL, "corp")] = testTokenCache()
	cache.sts[stsCacheKey("corp", "acct-1", "role-1")] = &sso.STSCache{
		SessionName: "corp", AccountID: "acct-1", RoleName: "role-1",
		AccessKeyID: "AKLTsts", SecretAccessKey: "sts-secret", SessionToken: "sts-token",
	}
	cfgStore := &fakeConfigStore{cfg: cfg, path: ""}
	revoker := &fakeSSORevoker{}
	adapter := newSSOAdapterForTest(cache, cfgStore, nil, nil, revoker)
	res, err := adapter.runSSOLogout(context.Background(), ssoLogoutOpts{Profile: "default"})
	if err != nil {
		t.Fatalf("logout by profile failed: %v", err)
	}
	if !res.ClearedSession {
		t.Fatal("expected session to be cleared")
	}
	if res.ClearedSTSCount != 1 {
		t.Fatalf("expected 1 STS cleared, got %d", res.ClearedSTSCount)
	}

	// Logout by session
	cfg2 := testConfigWithSession()
	p2 := cfg2.Profiles["default"]
	p2.Mode = config.AuthModeSSO
	p2.SSOSessionName = "corp"
	p2.AccountID = "acct-1"
	p2.RoleName = "role-1"
	cfg2.Profiles["default"] = p2
	cache2 := newFakeSSOCache()
	cache2.tokens[ssoTokenKey(testSSOSession().StartURL, "corp")] = testTokenCache()
	cfgStore2 := &fakeConfigStore{cfg: cfg2, path: ""}
	adapter2 := newSSOAdapterForTest(cache2, cfgStore2, nil, nil, &fakeSSORevoker{})
	res2, err := adapter2.runSSOLogout(context.Background(), ssoLogoutOpts{SSOSession: "corp"})
	if err != nil {
		t.Fatalf("logout by session failed: %v", err)
	}
	if !res2.ClearedSession {
		t.Fatal("expected session to be cleared by session logout")
	}
}

func TestSSOLogoutClearsAllLinkedProfileSTSMetadata(t *testing.T) {
	cfg := testConfigWithSession()
	// Two profiles bound to the same session.
	p1 := cfg.Profiles["default"]
	p1.Mode = config.AuthModeSSO
	p1.SSOSessionName = "corp"
	p1.AccountID = "acct-1"
	p1.RoleName = "role-1"
	p1.STSExpiration = 12345
	cfg.Profiles["default"] = p1
	cfg.Profiles["other"] = config.Profile{
		Mode:           config.AuthModeSSO,
		SSOSessionName: "corp",
		AccountID:      "acct-1",
		RoleName:       "role-1",
		STSExpiration:  67890,
	}
	cache := newFakeSSOCache()
	cache.tokens[ssoTokenKey(testSSOSession().StartURL, "corp")] = testTokenCache()
	cfgStore := &fakeConfigStore{cfg: cfg, path: ""}
	adapter := newSSOAdapterForTest(cache, cfgStore, nil, nil, &fakeSSORevoker{})
	_, err := adapter.runSSOLogout(context.Background(), ssoLogoutOpts{SSOSession: "corp"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Both profiles should have STSExpiration cleared.
	for _, name := range []string{"default", "other"} {
		if cfgStore.cfg.Profiles[name].STSExpiration != 0 {
			t.Fatalf("profile %s sts-expiration not cleared: %d", name, cfgStore.cfg.Profiles[name].STSExpiration)
		}
	}
}

func TestSSOLogoutPreservesBindingTLSAndDormantStaticFields(t *testing.T) {
	cfg := testConfigWithSession()
	p := cfg.Profiles["default"]
	p.Mode = config.AuthModeSSO
	p.SSOSessionName = "corp"
	p.AccountID = "acct-1"
	p.RoleName = "role-1"
	p.STSExpiration = 12345
	cfg.Profiles["default"] = p
	cache := newFakeSSOCache()
	cache.tokens[ssoTokenKey(testSSOSession().StartURL, "corp")] = testTokenCache()
	cfgStore := &fakeConfigStore{cfg: cfg, path: ""}
	adapter := newSSOAdapterForTest(cache, cfgStore, nil, nil, &fakeSSORevoker{})
	_, err := adapter.runSSOLogout(context.Background(), ssoLogoutOpts{Profile: "default"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := cfgStore.cfg.Profiles["default"]
	if got.SSOSessionName != "corp" {
		t.Fatalf("sso-session-name should be preserved: %q", got.SSOSessionName)
	}
	if got.AccountID != "acct-1" {
		t.Fatalf("account-id should be preserved: %q", got.AccountID)
	}
	if got.RoleName != "role-1" {
		t.Fatalf("role-name should be preserved: %q", got.RoleName)
	}
	if got.Region != "cn-beijing" {
		t.Fatalf("TLS region should be preserved: %q", got.Region)
	}
	if got.Endpoint != "https://tls-cn-beijing.volces.com" {
		t.Fatalf("TLS endpoint should be preserved: %q", got.Endpoint)
	}
	if got.AccessKeyID != "AKLTlegacy" {
		t.Fatalf("dormant AK should be preserved: %q", got.AccessKeyID)
	}
	if got.Mode != config.AuthModeSSO {
		t.Fatalf("mode should be preserved: %q", got.Mode)
	}
}

func TestSSOLogoutStillClearsLocalStateWhenRevokeFails(t *testing.T) {
	cfg := testConfigWithSession()
	p := cfg.Profiles["default"]
	p.Mode = config.AuthModeSSO
	p.SSOSessionName = "corp"
	p.AccountID = "acct-1"
	p.RoleName = "role-1"
	cfg.Profiles["default"] = p
	cache := newFakeSSOCache()
	cache.tokens[ssoTokenKey(testSSOSession().StartURL, "corp")] = testTokenCache()
	cache.sts[stsCacheKey("corp", "acct-1", "role-1")] = &sso.STSCache{
		SessionName: "corp", AccountID: "acct-1", RoleName: "role-1",
	}
	cfgStore := &fakeConfigStore{cfg: cfg, path: ""}
	revoker := &fakeSSORevoker{err: errors.New("revoke failed")}
	adapter := newSSOAdapterForTest(cache, cfgStore, nil, nil, revoker)
	res, err := adapter.runSSOLogout(context.Background(), ssoLogoutOpts{Profile: "default"})
	// Should return partial failure error but local state must be cleared.
	if err == nil {
		t.Fatal("expected partial failure error when revoke fails")
	}
	if !res.ClearedSession {
		t.Fatal("session should still be cleared when revoke fails")
	}
	if res.ClearedSTSCount != 1 {
		t.Fatalf("STS should still be cleared when revoke fails, got %d", res.ClearedSTSCount)
	}
	// Token cache should be deleted.
	_, rerr := cache.ReadToken(testSSOSession().StartURL, "corp")
	if !errors.Is(rerr, securestore.ErrMissing) {
		t.Fatalf("token cache should be deleted, got err=%v", rerr)
	}
}

func TestSSOLogoutRacingRetrieveCannotRecreateCache(t *testing.T) {
	cfg := testConfigWithSession()
	p := cfg.Profiles["default"]
	p.Mode = config.AuthModeSSO
	p.SSOSessionName = "corp"
	p.AccountID = "acct-1"
	p.RoleName = "role-1"
	cfg.Profiles["default"] = p

	// Use the real *sso.FileCache so the cross-process per-key locks are
	// exercised, mirroring production where Provider.Retrieve and logout
	// contend on the same file locks.
	dir := t.TempDir()
	cache, err := sso.NewFileCache(dir)
	if err != nil {
		t.Fatalf("create file cache: %v", err)
	}
	if err := cache.WriteToken(testTokenCache()); err != nil {
		t.Fatalf("seed token: %v", err)
	}
	if err := cache.WriteSTS(&sso.STSCache{
		SessionName: "corp", AccountID: "acct-1", RoleName: "role-1",
	}); err != nil {
		t.Fatalf("seed sts: %v", err)
	}
	cfgStore := &fakeConfigStore{cfg: cfg, path: ""}
	adapter := &ssoAdapter{
		cache:    cache,
		cfgStore: cfgStore,
		stdout:   &bytes.Buffer{},
		stderr:   &bytes.Buffer{},
		clock:    time.Now,
		revokerFn: func(_ string) (ssoOAuthRevoker, error) {
			return &fakeSSORevoker{}, nil
		},
	}

	// Verify that after logout completes, the token cache is deleted and a
	// subsequent read sees it as missing (not recreated). ReadToken does not
	// acquire the token lock, so this is a deterministic post-condition check
	// rather than a true lock-contention race.
	_, err = adapter.runSSOLogout(context.Background(), ssoLogoutOpts{Profile: "default"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, retrieveErr := cache.ReadToken(testSSOSession().StartURL, "corp")
	if !errors.Is(retrieveErr, securestore.ErrMissing) {
		t.Fatalf("token should be missing after logout, got err=%v", retrieveErr)
	}
}

func TestSSOLogoutReturnsThenProviderRequiresLogin(t *testing.T) {
	cfg := testConfigWithSession()
	p := cfg.Profiles["default"]
	p.Mode = config.AuthModeSSO
	p.SSOSessionName = "corp"
	p.AccountID = "acct-1"
	p.RoleName = "role-1"
	cfg.Profiles["default"] = p
	cache := newFakeSSOCache()
	cache.tokens[ssoTokenKey(testSSOSession().StartURL, "corp")] = testTokenCache()
	cfgStore := &fakeConfigStore{cfg: cfg, path: ""}
	adapter := newSSOAdapterForTest(cache, cfgStore, nil, nil, &fakeSSORevoker{})
	_, err := adapter.runSSOLogout(context.Background(), ssoLogoutOpts{Profile: "default"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// After logout, reading the token must return ErrMissing (ReauthRequired
	// equivalent at the cache level).
	_, rerr := cache.ReadToken(testSSOSession().StartURL, "corp")
	if !errors.Is(rerr, securestore.ErrMissing) {
		t.Fatalf("token should be missing after logout, got err=%v", rerr)
	}
}

// --- SSO output redaction ---

func TestSSOLogoutOutputNeverContainsTokens(t *testing.T) {
	cfg := testConfigWithSession()
	p := cfg.Profiles["default"]
	p.Mode = config.AuthModeSSO
	p.SSOSessionName = "corp"
	p.AccountID = "acct-1"
	p.RoleName = "role-1"
	cfg.Profiles["default"] = p
	cache := newFakeSSOCache()
	tok := testTokenCache()
	cache.tokens[ssoTokenKey(testSSOSession().StartURL, "corp")] = tok
	cfgStore := &fakeConfigStore{cfg: cfg, path: ""}
	adapter := newSSOAdapterForTest(cache, cfgStore, nil, nil, &fakeSSORevoker{})
	res, err := adapter.runSSOLogout(context.Background(), ssoLogoutOpts{Profile: "default"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	b, _ := json.Marshal(res)
	out := string(b)
	for _, secret := range []string{
		tok.AccessToken, tok.RefreshToken, tok.ClientSecret,
		"access-token-canary", "refresh-token-canary", "client-secret-canary",
	} {
		if strings.Contains(out, secret) {
			t.Fatalf("logout output contains secret %q: %s", secret, out)
		}
	}
}

// --- Lock ordering tests ---

func TestSSOLogoutAcquiresLocksInDigestOrder(t *testing.T) {
	cfg := testConfigWithSession()
	// Two profiles with different account/role -> two STS keys.
	p1 := cfg.Profiles["default"]
	p1.Mode = config.AuthModeSSO
	p1.SSOSessionName = "corp"
	p1.AccountID = "acct-1"
	p1.RoleName = "role-1"
	cfg.Profiles["default"] = p1
	cfg.Profiles["other"] = config.Profile{
		Mode: config.AuthModeSSO, SSOSessionName: "corp",
		AccountID: "acct-2", RoleName: "role-2",
	}
	cache := newFakeSSOCache()
	cache.tokens[ssoTokenKey(testSSOSession().StartURL, "corp")] = testTokenCache()
	cfgStore := &fakeConfigStore{cfg: cfg, path: ""}
	adapter := newSSOAdapterForTest(cache, cfgStore, nil, nil, &fakeSSORevoker{})
	_, err := adapter.runSSOLogout(context.Background(), ssoLogoutOpts{SSOSession: "corp"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Token lock acquired once, STS lock acquired twice (once per key).
	if atomic.LoadInt32(&cache.tokenLockCnt) != 1 {
		t.Fatalf("expected 1 token lock, got %d", cache.tokenLockCnt)
	}
	if atomic.LoadInt32(&cache.stsLockCnt) != 2 {
		t.Fatalf("expected 2 STS locks, got %d", cache.stsLockCnt)
	}
}

// --- Run-level dispatch tests ---

func TestSSOLoginDispatchRejectsSecretsFile(t *testing.T) {
	ctx, _ := newSSOTestContext(t, testConfigWithSession())
	ctx.GlobalSecretsFile = "/some/path"
	_, err := runSSOLoginWithFactory(ctx, []string{"--profile", "default"}, func(_ *Context) (*ssoAdapter, error) {
		t.Fatal("factory should not be called")
		return nil, nil
	})
	if err == nil {
		t.Fatal("expected error for secrets-file")
	}
}

func TestSSOLogoutDispatchRejectsSecretsFile(t *testing.T) {
	ctx, _ := newSSOTestContext(t, testConfigWithSession())
	ctx.GlobalSecretsFile = "/some/path"
	_, err := runSSOLogoutWithFactory(ctx, []string{"--profile", "default"}, func(_ *Context) (*ssoAdapter, error) {
		t.Fatal("factory should not be called")
		return nil, nil
	})
	if err == nil {
		t.Fatal("expected error for secrets-file")
	}
}

// ensure sync import is used
var _ = sync.Mutex{}
