package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth"
	"github.com/volcengine-tls/ve-tls-cli/internal/auth/sso"
	"github.com/volcengine-tls/ve-tls-cli/internal/config"
	"github.com/volcengine-tls/ve-tls-cli/internal/securestore"
)

// ---------------------------------------------------------------------------
// Fakes for real-FileCache + real-SSOProvider concurrency tests.
// ---------------------------------------------------------------------------

// fakeSSOOAuth implements sso.OAuthAPI for the SSOProvider. CreateToken can be
// paused via createTokenBarrier so the provider holds the token lock while the
// test drives a concurrent CLI login/logout.
type fakeSSOOAuth struct {
	mu                 sync.Mutex
	createTokenCnt     int32
	createTokenBarrier chan struct{} // if non-nil, CreateToken closes it then blocks until proceed is closed
	proceed            chan struct{}
	// tokenOverrides, if set, is returned by CreateToken (rotated token).
	tokenOverrides *sso.CreateTokenResponse
}

func (f *fakeSSOOAuth) RegisterClient(context.Context, *sso.RegisterClientRequest) (*sso.RegisterClientResponse, error) {
	return nil, errors.New("RegisterClient not used in provider tests")
}
func (f *fakeSSOOAuth) StartDeviceAuthorization(context.Context, *sso.StartDeviceAuthorizationRequest) (*sso.StartDeviceAuthorizationResponse, error) {
	return nil, errors.New("StartDeviceAuthorization not used in provider tests")
}
func (f *fakeSSOOAuth) CreateToken(_ context.Context, _ *sso.CreateTokenRequest) (*sso.CreateTokenResponse, error) {
	atomic.AddInt32(&f.createTokenCnt, 1)
	if f.createTokenBarrier != nil {
		close(f.createTokenBarrier)
		<-f.proceed
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.tokenOverrides != nil {
		return f.tokenOverrides, nil
	}
	return &sso.CreateTokenResponse{
		AccessToken:  "rotated-access-token",
		RefreshToken: "rotated-refresh-token",
		ExpiresIn:    7200,
	}, nil
}

// fakeSSOSTSExchanger implements sso.STSExchanger for the SSOProvider.
type fakeSSOSTSExchanger struct {
	called int32
}

func (f *fakeSSOSTSExchanger) GetRoleCredentials(_ context.Context, _, _, _ string) (*sso.RoleCredentials, error) {
	atomic.AddInt32(&f.called, 1)
	return &sso.RoleCredentials{
		AccessKeyID:     "AKLTsts-fake",
		SecretAccessKey: "sts-secret-fake",
		SessionToken:    "sts-token-fake",
		Expiration:      time.Now().Add(2 * time.Hour).Unix(),
	}, nil
}

// newRealSSOProvider builds a real *sso.SSOProvider backed by the given real
// *sso.FileCache and the fake OAuth/STS exchanger.
func newRealSSOProvider(t *testing.T, cache *sso.FileCache, cfgPath, profileName string, sess config.SSOSession, oauth *fakeSSOOAuth, sts *fakeSSOSTSExchanger) *sso.SSOProvider {
	t.Helper()
	p, err := sso.NewSSOProvider(&sso.SSOProviderConfig{
		ConfigPath:  cfgPath,
		ProfileName: profileName,
		StartURL:    sess.StartURL,
		SessionName: sess.Name,
		SSORegion:   sess.Region,
		AccountID:   "acct-1",
		RoleName:    "role-1",
		Cache:       cache,
		OAuth:       oauth,
		Portal:      sts,
		Clock:       time.Now,
	})
	if err != nil {
		t.Fatalf("NewSSOProvider: %v", err)
	}
	return p
}

// seedRealSSOCaches writes a valid token + STS cache to the real FileCache.
func seedRealSSOCaches(t *testing.T, cache *sso.FileCache, sess config.SSOSession) {
	t.Helper()
	tok := &sso.TokenCache{
		StartURL:     sess.StartURL,
		SessionName:  sess.Name,
		AccessToken:  "seed-access-token",
		ExpiresAt:    time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339),
		ClientID:     "seed-client-id",
		ClientSecret: "seed-client-secret",
		RefreshToken: "seed-refresh-token",
		Region:       sess.Region,
	}
	if err := cache.WriteToken(tok); err != nil {
		t.Fatalf("seed token: %v", err)
	}
	if err := cache.WriteSTS(&sso.STSCache{
		SessionName:     sess.Name,
		AccountID:       "acct-1",
		RoleName:        "role-1",
		AccessKeyID:     "AKLTseed",
		SecretAccessKey: "seed-sts-secret",
		SessionToken:    "seed-sts-token",
		ProviderName:    sso.ProviderName,
		ExpiresAt:       time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("seed sts: %v", err)
	}
}

// realFileCacheAdapter builds an ssoAdapter backed by the real *sso.FileCache
// and a fake config store pointing at cfgPath. The device-flow factory returns
// a fake that writes the given token; the revoker factory returns a fake.
func realFileCacheAdapter(t *testing.T, cache *sso.FileCache, cfg config.Config, cfgPath string, loginToken *sso.TokenCache) *ssoAdapter {
	t.Helper()
	return &ssoAdapter{
		cache:    cache,
		cfgStore: &fakeConfigStore{cfg: cfg, path: cfgPath},
		stdout:   &bytes.Buffer{},
		stderr:   &bytes.Buffer{},
		clock:    time.Now,
		deviceFlowFn: func(_ config.SSOSession, _ bool) (ssoDeviceFlow, error) {
			return &fakeSSODeviceFlow{token: loginToken, cache: cache}, nil
		},
		bindingFn: func(_ config.SSOSession) (ssoBindingService, error) {
			return &fakeSSOBindingService{result: &sso.BindingResult{AccountID: "acct-1", RoleName: "role-1"}}, nil
		},
		revokerFn: func(_ string) (ssoOAuthRevoker, error) {
			return &fakeSSORevoker{}, nil
		},
	}
}

// ---------------------------------------------------------------------------
// 1. Region-aware factories
// ---------------------------------------------------------------------------

func TestSSOFactoryUsesSessionRegionForAllClients(t *testing.T) {
	cfg := testConfigWithSession()
	sess := cfg.SSOSessions["corp"]
	sess.Region = "ap-southeast-1"
	cfg.SSOSessions["corp"] = sess

	var seenDFRegion, seenBindingRegion, seenRevokerRegion string
	adapter := &ssoAdapter{
		cache:    newFakeSSOCache(),
		cfgStore: &fakeConfigStore{cfg: cfg, path: ""},
		stdout:   &bytes.Buffer{},
		stderr:   &bytes.Buffer{},
		clock:    time.Now,
		deviceFlowFn: func(s config.SSOSession, _ bool) (ssoDeviceFlow, error) {
			seenDFRegion = s.Region
			return &fakeSSODeviceFlow{token: testTokenCache()}, nil
		},
		bindingFn: func(s config.SSOSession) (ssoBindingService, error) {
			seenBindingRegion = s.Region
			return &fakeSSOBindingService{result: &sso.BindingResult{AccountID: "acct-1", RoleName: "role-1"}}, nil
		},
		revokerFn: func(r string) (ssoOAuthRevoker, error) {
			seenRevokerRegion = r
			return &fakeSSORevoker{}, nil
		},
	}
	if _, err := adapter.runConfigureSSO(context.Background(), ssoConfigureOpts{Profile: "default", SSOSession: "corp"}); err != nil {
		t.Fatalf("configure sso: %v", err)
	}
	if seenDFRegion != "ap-southeast-1" {
		t.Fatalf("device flow region = %q, want ap-southeast-1", seenDFRegion)
	}
	if seenBindingRegion != "ap-southeast-1" {
		t.Fatalf("binding region = %q, want ap-southeast-1", seenBindingRegion)
	}

	// Logout must build the revoker with the session region.
	cache := newFakeSSOCache()
	cache.tokens[ssoTokenKey(sess.StartURL, "corp")] = testTokenCache()
	adapter2 := &ssoAdapter{
		cache:    cache,
		cfgStore: &fakeConfigStore{cfg: cfg, path: ""},
		stdout:   &bytes.Buffer{},
		stderr:   &bytes.Buffer{},
		clock:    time.Now,
		revokerFn: func(r string) (ssoOAuthRevoker, error) {
			seenRevokerRegion = r
			return &fakeSSORevoker{}, nil
		},
	}
	if _, err := adapter2.runSSOLogout(context.Background(), ssoLogoutOpts{SSOSession: "corp"}); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if seenRevokerRegion != "ap-southeast-1" {
		t.Fatalf("revoker region = %q, want ap-southeast-1", seenRevokerRegion)
	}
}

// ---------------------------------------------------------------------------
// 2. Production account/role selectors
// ---------------------------------------------------------------------------

func TestSSOSelectorsSingleCandidateAutoSelects(t *testing.T) {
	stdin := strings.NewReader("")
	var stderr bytes.Buffer
	sel := newAccountSelector(stdin, &stderr)
	got, err := sel([]sso.AccountInfo{{AccountID: "acct-1", AccountName: "only"}})
	if err != nil {
		t.Fatalf("single account: %v", err)
	}
	if got.AccountID != "acct-1" {
		t.Fatalf("got %q, want acct-1", got.AccountID)
	}
}

func TestSSOSelectorsMultipleCandidatesReadsIndex(t *testing.T) {
	stdin := strings.NewReader("2\n")
	var stderr bytes.Buffer
	sel := newAccountSelector(stdin, &stderr)
	got, err := sel([]sso.AccountInfo{
		{AccountID: "acct-1", AccountName: "first"},
		{AccountID: "acct-2", AccountName: "second"},
	})
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if got.AccountID != "acct-2" {
		t.Fatalf("got %q, want acct-2", got.AccountID)
	}
	// Prompt must go to stderr, not stdout (stdout is nil here).
	if !strings.Contains(stderr.String(), "[1]") || !strings.Contains(stderr.String(), "[2]") {
		t.Fatalf("stderr missing numbered prompt: %q", stderr.String())
	}
}

func TestSSOSelectorsRejectInvalidEOFAndLongInput(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"out of range", "5\n"},
		{"non-integer", "abc\n"},
		{"empty", "\n"},
		{"eof", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stdin := strings.NewReader(c.input)
			var stderr bytes.Buffer
			sel := newAccountSelector(stdin, &stderr)
			_, err := sel([]sso.AccountInfo{
				{AccountID: "acct-1"}, {AccountID: "acct-2"},
			})
			if err == nil {
				t.Fatalf("expected error for input %q", c.input)
			}
			// Error must not echo the user's input (skip empty/EOF where there
			// is no input to echo).
			if c.input != "" && strings.Contains(err.Error(), c.input) {
				t.Fatalf("error echoes input: %v", err)
			}
		})
	}

	// Oversized line: readConfirmLine fails closed.
	long := strings.Repeat("x", maxConfirmLineLength+10) + "\n"
	stdin := strings.NewReader(long)
	var stderr bytes.Buffer
	sel := newRoleSelector(stdin, &stderr)
	_, err := sel([]sso.RoleInfo{{RoleName: "r1"}, {RoleName: "r2"}})
	if err == nil {
		t.Fatal("expected error for oversized input")
	}
}

func TestSSORoleSelectorUsesStderrOnly(t *testing.T) {
	stdin := strings.NewReader("1\n")
	var stderr bytes.Buffer
	sel := newRoleSelector(stdin, &stderr)
	_, err := sel([]sso.RoleInfo{{RoleName: "r1"}, {RoleName: "r2"}})
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if stderr.Len() == 0 {
		t.Fatal("expected prompt on stderr")
	}
}

// ---------------------------------------------------------------------------
// 3. No OAuth token material in output
// ---------------------------------------------------------------------------

func TestSSOLoginResultHasNoMaskedAccessKeyOrTokenFragments(t *testing.T) {
	cfg := testConfigWithSession()
	cache := newFakeSSOCache()
	cfgStore := &fakeConfigStore{cfg: cfg, path: ""}
	tok := testTokenCache()
	tok.AccessToken = "PREFIX-access-TOKEN-SUFFIX"
	tok.RefreshToken = "REFRESH-canary"
	tok.ClientSecret = "SECRET-canary"
	df := &fakeSSODeviceFlow{token: tok}
	adapter := newSSOAdapterForTest(cache, cfgStore, df, &fakeSSOBindingService{result: &sso.BindingResult{AccountID: "acct-1", RoleName: "role-1"}}, nil)
	res, err := adapter.runSSOLogin(context.Background(), ssoLoginOpts{SSOSession: "corp"})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	b, _ := json.Marshal(res)
	out := string(b)
	for _, frag := range []string{
		"masked_access_key",
		tok.AccessToken,
		tok.AccessToken[:4],
		tok.AccessToken[len(tok.AccessToken)-4:],
		tok.RefreshToken,
		tok.ClientSecret,
		"PREFIX", "SUFFIX", "REFRESH-canary", "SECRET-canary",
	} {
		if strings.Contains(out, frag) {
			t.Fatalf("output contains forbidden fragment %q: %s", frag, out)
		}
	}
}

func TestConfigureSSOResultUsesBindingAccountAndRole(t *testing.T) {
	cfg := testConfigWithSession()
	cache := newFakeSSOCache()
	cfgStore := &fakeConfigStore{cfg: cfg, path: ""}
	df := &fakeSSODeviceFlow{token: testTokenCache()}
	bs := &fakeSSOBindingService{result: &sso.BindingResult{AccountID: "bound-acct", RoleName: "bound-role"}}
	adapter := newSSOAdapterForTest(cache, cfgStore, df, bs, nil)
	res, err := adapter.runConfigureSSO(context.Background(), ssoConfigureOpts{Profile: "default", SSOSession: "corp"})
	if err != nil {
		t.Fatalf("configure: %v", err)
	}
	if res.AccountID != "bound-acct" {
		t.Fatalf("account_id = %q, want bound-acct", res.AccountID)
	}
	if res.RoleName != "bound-role" {
		t.Fatalf("role_name = %q, want bound-role", res.RoleName)
	}
}

// ---------------------------------------------------------------------------
// 4. Final result captured inside token lock
// ---------------------------------------------------------------------------

func TestSSOLoginCapturesResultInsideLockNoReread(t *testing.T) {
	cfg := testConfigWithSession()
	cache := newFakeSSOCache()
	cfgStore := &fakeConfigStore{cfg: cfg, path: ""}
	tok := testTokenCache()
	tok.AccessToken = "inside-lock-token"

	// deleteAfterLoginDF writes the token, then deletes it (simulating a
	// concurrent logout that wins the race after the write but before the
	// result is assembled). The result must still be built from the in-lock
	// snapshot, not a re-read.
	df := &deleteAfterLoginDF{token: tok, cache: cache, done: make(chan struct{})}

	adapter := newSSOAdapterForTest(cache, cfgStore, df, nil, nil)
	res, err := adapter.runSSOLogin(context.Background(), ssoLoginOpts{SSOSession: "corp"})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	<-df.done
	if res == nil {
		t.Fatal("expected result from in-lock snapshot")
	}
	// The token must have been deleted by the simulated concurrent logout, yet
	// the result was still assembled from the in-lock snapshot (no re-read).
	if _, rerr := cache.ReadToken(tok.StartURL, tok.SessionName); !errors.Is(rerr, securestore.ErrMissing) {
		t.Fatalf("token should be deleted after simulated logout, got err=%v", rerr)
	}
}

// deleteAfterLoginDF writes the token to the cache, immediately deletes it
// (simulating a concurrent logout), and returns the token. It signals done
// after the delete so the test can verify the delete happened.
type deleteAfterLoginDF struct {
	token *sso.TokenCache
	cache ssoCache
	done  chan struct{}
}

func (d *deleteAfterLoginDF) Login(_ context.Context) (*sso.TokenCache, error) {
	if err := d.cache.WriteToken(d.token); err != nil {
		return nil, err
	}
	_ = d.cache.DeleteToken(d.token.StartURL, d.token.SessionName)
	close(d.done)
	return d.token, nil
}

// ---------------------------------------------------------------------------
// 5. Rollback failure classification
// ---------------------------------------------------------------------------

func TestSSORollbackFailureClassifiedAndRestoresFullCache(t *testing.T) {
	cfg := testConfigWithSession()
	cache := newFakeSSOCache()
	oldToken := testTokenCache()
	oldToken.AccessToken = "old-access"
	oldToken.RefreshToken = "old-refresh"
	oldToken.ClientSecret = "old-secret"
	cache.tokens[ssoTokenKey(oldToken.StartURL, oldToken.SessionName)] = oldToken

	// Force ALL WriteToken calls to fail (both device flow and rollback).
	cache.failWriteToken = errors.New("disk full")
	cfgStore := &fakeConfigStore{cfg: cfg, path: ""}
	df := &fakeSSODeviceFlow{token: testTokenCache()}
	bs := &fakeSSOBindingService{result: &sso.BindingResult{AccountID: "acct-1", RoleName: "role-1"}}
	adapter := newSSOAdapterForTest(cache, cfgStore, df, bs, nil)
	_, err := adapter.runConfigureSSO(context.Background(), ssoConfigureOpts{Profile: "default", SSOSession: "corp"})
	if err == nil {
		t.Fatal("expected error")
	}
	// Since the device flow write also failed, the old token is still intact on
	// disk. restoreTokenOnFailure reads the current token, finds it matches the
	// old snapshot, and returns the business error WITHOUT attempting rollback
	// (and crucially WITHOUT deleting the old token). This is NOT a
	// ErrSSORollbackFailure because no rollback was needed.
	if errors.Is(err, ErrSSORollbackFailure) {
		t.Fatalf("error should not be ErrSSORollbackFailure when old token is intact: %v", err)
	}
	// Error text must not contain token material.
	if strings.Contains(err.Error(), "old-access") || strings.Contains(err.Error(), "old-refresh") {
		t.Fatalf("error leaks token: %v", err)
	}
	// The old token must still be present (never deleted).
	restored, rerr := cache.ReadToken(oldToken.StartURL, oldToken.SessionName)
	if rerr != nil {
		t.Fatalf("old token should still exist: %v", rerr)
	}
	if restored.AccessToken != "old-access" {
		t.Fatalf("old token changed: got %q want %q", restored.AccessToken, "old-access")
	}
}

func TestSSORollbackMissingTokenDeleteFailureClassified(t *testing.T) {
	cfg := testConfigWithSession()
	cache := newFakeSSOCache()
	// No old token; new token written by device flow, then binding fails, then
	// delete fails.
	cache.failDeleteToken = errors.New("disk full")
	cfgStore := &fakeConfigStore{cfg: cfg, path: ""}
	df := &fakeSSODeviceFlow{token: testTokenCache()}
	bs := &fakeSSOBindingService{err: errors.New("binding boom")}
	adapter := newSSOAdapterForTest(cache, cfgStore, df, bs, nil)
	_, err := adapter.runConfigureSSO(context.Background(), ssoConfigureOpts{Profile: "default", SSOSession: "corp"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrSSORollbackFailure) {
		t.Fatalf("expected ErrSSORollbackFailure, got %v", err)
	}
}

func TestSSORollbackRestoresExactOldTokenDeepEqual(t *testing.T) {
	cfg := testConfigWithSession()
	cache := newFakeSSOCache()
	oldToken := testTokenCache()
	oldToken.AccessToken = "old-access-xyz"
	oldToken.RefreshToken = "old-refresh-xyz"
	oldToken.ClientID = "old-client"
	oldToken.ClientSecret = "old-secret"
	oldToken.ExpiresAt = time.Now().Add(3 * time.Hour).UTC().Format(time.RFC3339)
	cache.tokens[ssoTokenKey(oldToken.StartURL, oldToken.SessionName)] = oldToken

	cfgStore := &fakeConfigStore{cfg: cfg, path: ""}
	cfgStore.updateErr = errors.New("config write failed")
	df := &fakeSSODeviceFlow{token: testTokenCache()}
	bs := &fakeSSOBindingService{result: &sso.BindingResult{AccountID: "acct-1", RoleName: "role-1"}}
	adapter := newSSOAdapterForTest(cache, cfgStore, df, bs, nil)
	_, err := adapter.runConfigureSSO(context.Background(), ssoConfigureOpts{Profile: "default", SSOSession: "corp"})
	if err == nil {
		t.Fatal("expected error")
	}
	restored, rerr := cache.ReadToken(oldToken.StartURL, oldToken.SessionName)
	if rerr != nil {
		t.Fatalf("old token should be restored: %v", rerr)
	}
	// Compare the WHOLE cache, not just AccessToken.
	if !reflect.DeepEqual(restored, oldToken) {
		t.Fatalf("restored token != old token:\nrestored=%+v\nold=%+v", restored, oldToken)
	}
}

// TestSSORollbackRestoresTokenExactlyOnce proves the token restore runs at most
// once: WriteToken must be called exactly once during a config-failure rollback
// (no double-restore from commitConfig + outer compensation).
func TestSSORollbackRestoresTokenExactlyOnce(t *testing.T) {
	cfg := testConfigWithSession()
	cache := newFakeSSOCache()
	oldToken := testTokenCache()
	oldToken.AccessToken = "old-token-once"
	cache.tokens[ssoTokenKey(oldToken.StartURL, oldToken.SessionName)] = oldToken
	cfgStore := &fakeConfigStore{cfg: cfg, path: ""}
	cfgStore.updateErr = errors.New("config write failed")
	df := &fakeSSODeviceFlow{token: testTokenCache()}
	bs := &fakeSSOBindingService{result: &sso.BindingResult{AccountID: "acct-1", RoleName: "role-1"}}
	adapter := newSSOAdapterForTest(cache, cfgStore, df, bs, nil)
	_, _ = adapter.runConfigureSSO(context.Background(), ssoConfigureOpts{Profile: "default", SSOSession: "corp"})
	// DeviceFlow writes once, rollback restores once = exactly 2 WriteToken calls.
	if got := atomic.LoadInt32(&cache.writeTokenCnt); got != 2 {
		t.Fatalf("WriteToken called %d times, want exactly 2 (1 device flow + 1 rollback restore)", got)
	}
}

// TestSSORebindRollbackRestoresTokenAndSTSExactlyOnce proves that in the rebind
// path, both token and STS are restored at most once when config fails.
func TestSSORebindRollbackRestoresTokenAndSTSExactlyOnce(t *testing.T) {
	cfg := testConfigWithSession()
	p := cfg.Profiles["default"]
	p.Mode = config.AuthModeSSO
	p.SSOSessionName = "corp"
	p.AccountID = "acct-1"
	p.RoleName = "role-1"
	cfg.Profiles["default"] = p
	cfg.SSOSessions["newcorp"] = config.SSOSession{
		Name: "newcorp", StartURL: "https://newcorp.example.com", Region: "cn-beijing",
		RegistrationScopes: defaultSSOScopes,
	}
	cache := newFakeSSOCache()
	oldToken := testTokenCache()
	oldToken.AccessToken = "old-token"
	cache.tokens[ssoTokenKey(oldToken.StartURL, oldToken.SessionName)] = oldToken
	// Seed a token for the NEW session so rollback must restore it (oldExisted=true).
	newSessionToken := testTokenCache()
	newSessionToken.StartURL = "https://newcorp.example.com"
	newSessionToken.SessionName = "newcorp"
	newSessionToken.AccessToken = "newcorp-existing-token"
	cache.tokens[ssoTokenKey(newSessionToken.StartURL, newSessionToken.SessionName)] = newSessionToken
	cache.sts[stsCacheKey("corp", "acct-1", "role-1")] = &sso.STSCache{SessionName: "corp", AccountID: "acct-1", RoleName: "role-1"}

	cfgStore := &fakeConfigStore{cfg: cfg, path: ""}
	cfgStore.updateErr = errors.New("config write failed")
	newToken := testTokenCache()
	newToken.StartURL = "https://newcorp.example.com"
	newToken.SessionName = "newcorp"
	df := &fakeSSODeviceFlow{token: newToken}
	bs := &fakeSSOBindingService{result: &sso.BindingResult{AccountID: "acct-2", RoleName: "role-2"}}
	adapter := newSSOAdapterForTest(cache, cfgStore, df, bs, nil)
	_, _ = adapter.runConfigureSSO(context.Background(), ssoConfigureOpts{Profile: "default", SSOSession: "newcorp"})

	// Token: device flow writes once (overwrites newcorp token), rollback restores
	// the pre-existing newcorp token once = exactly 2 WriteToken calls.
	if got := atomic.LoadInt32(&cache.writeTokenCnt); got != 2 {
		t.Fatalf("WriteToken called %d times, want 2 (1 device flow + 1 rollback restore)", got)
	}
	// STS: rollback restores old STS exactly once.
	if got := atomic.LoadInt32(&cache.writeSTSCnt); got != 1 {
		t.Fatalf("WriteSTS called %d times, want 1 (rollback restore only)", got)
	}
}

// TestSSOLogoutAggregatesAllErrorsInChain proves that when revoke, local delete,
// and config update all fail, every error is reachable via errors.Is/As from the
// returned error — none are dropped from the chain.
func TestSSOLogoutAggregatesAllErrorsInChain(t *testing.T) {
	cfg := testConfigWithSession()
	// Two profiles bound to "corp" with different account/role → two STS keys.
	// One STS delete will fail, the other will succeed (so anyCleared=true →
	// partial failure).
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
	cache.sts[stsCacheKey("corp", "acct-1", "role-1")] = &sso.STSCache{SessionName: "corp", AccountID: "acct-1", RoleName: "role-1"}
	cache.sts[stsCacheKey("corp", "acct-2", "role-2")] = &sso.STSCache{SessionName: "corp", AccountID: "acct-2", RoleName: "role-2"}

	// Four independent sentinel errors, all must be reachable via errors.Is.
	tokenDeleteErr := errors.New("token delete boom")
	stsDeleteErr := errors.New("sts delete boom")
	configErr := errors.New("config update boom")
	revokeErr := errors.New("revoke boom")

	// Token delete fails.
	cache.failDeleteToken = tokenDeleteErr
	// Only the first STS delete fails; the second (acct-2/role-2) succeeds so
	// ClearedSTSCount > 0 → anyCleared=true → partial failure.
	cache.failDeleteSTSKeys = map[string]error{
		stsCacheKey("corp", "acct-1", "role-1"): stsDeleteErr,
	}
	cfgStore := &fakeConfigStore{cfg: cfg, path: ""}
	cfgStore.updateErr = configErr
	adapter := &ssoAdapter{
		cache:    cache,
		cfgStore: cfgStore,
		stdout:   &bytes.Buffer{},
		stderr:   &bytes.Buffer{},
		clock:    time.Now,
		revokerFn: func(_ string) (ssoOAuthRevoker, error) {
			return &fakeSSORevoker{err: revokeErr}, nil
		},
	}
	_, err := adapter.runSSOLogout(context.Background(), ssoLogoutOpts{SSOSession: "corp"})
	if err == nil {
		t.Fatal("expected error from multiple failures")
	}
	// One STS delete succeeded (ClearedSTSCount > 0) so this is a partial failure.
	if !errors.Is(err, ErrSSOLogoutPartialFailure) {
		t.Fatalf("error is not ErrSSOLogoutPartialFailure: %v", err)
	}
	// All four sentinel causes must be reachable in the chain.
	if !errors.Is(err, tokenDeleteErr) {
		t.Fatalf("token delete error not in chain: %v", err)
	}
	if !errors.Is(err, stsDeleteErr) {
		t.Fatalf("sts delete error not in chain: %v", err)
	}
	if !errors.Is(err, configErr) {
		t.Fatalf("config error not in chain: %v", err)
	}
	if !errors.Is(err, revokeErr) {
		t.Fatalf("revoke error not in chain: %v", err)
	}
}

// TestSSOLogoutPartialWhenOnlyConfigMetadataSucceeds proves that when token
// delete and ALL STS deletes fail, but config.Update successfully clears the
// profile sts-expiration metadata, the result is still a partial failure
// (config metadata is local state change). Asserts ErrSSOLogoutPartialFailure,
// the partial kind, the error chain, and that ClearedProfiles is accurate.
func TestSSOLogoutPartialWhenOnlyConfigMetadataSucceeds(t *testing.T) {
	cfg := testConfigWithSession()
	p := cfg.Profiles["default"]
	p.Mode = config.AuthModeSSO
	p.SSOSessionName = "corp"
	p.AccountID = "acct-1"
	p.RoleName = "role-1"
	cfg.Profiles["default"] = p
	cache := newFakeSSOCache()
	cache.tokens[ssoTokenKey(testSSOSession().StartURL, "corp")] = testTokenCache()
	cache.sts[stsCacheKey("corp", "acct-1", "role-1")] = &sso.STSCache{SessionName: "corp", AccountID: "acct-1", RoleName: "role-1"}

	// Token delete and all STS deletes fail; config update succeeds.
	tokenDeleteErr := errors.New("token delete boom")
	stsDeleteErr := errors.New("sts delete boom")
	cache.failDeleteToken = tokenDeleteErr
	cache.failDeleteSTS = stsDeleteErr
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
	res, err := adapter.runSSOLogout(context.Background(), ssoLogoutOpts{SSOSession: "corp"})
	if err == nil {
		t.Fatal("expected error from delete failures")
	}
	// Config metadata was cleared (ClearedProfiles populated) → partial failure.
	if !errors.Is(err, ErrSSOLogoutPartialFailure) {
		t.Fatalf("error is not ErrSSOLogoutPartialFailure: %v", err)
	}
	// Both delete errors must be in the chain.
	if !errors.Is(err, tokenDeleteErr) {
		t.Fatalf("token delete error not in chain: %v", err)
	}
	if !errors.Is(err, stsDeleteErr) {
		t.Fatalf("sts delete error not in chain: %v", err)
	}
	// ClearedProfiles must be accurate (the profile bound to corp).
	if len(res.ClearedProfiles) != 1 || res.ClearedProfiles[0] != "default" {
		t.Fatalf("ClearedProfiles = %v, want [default]", res.ClearedProfiles)
	}
	// The profile's sts-expiration must actually be cleared in the config.
	updated, _ := cfgStore.Update("", func(c *config.Config) error { return nil })
	p2, _ := updated.GetProfile("default")
	if p2.STSExpiration != 0 {
		t.Fatalf("profile sts-expiration not cleared: %d", p2.STSExpiration)
	}
}

// TestConfigureSSORebindRollbackRealFileCacheByteEquivalent is a fully real
// staged rollback test: real *sso.FileCache, real config file on disk, real
// DeviceFlow that persists the target-session token, and a config commit
// failure triggered by a real filesystem fault (read-only directory). After
// the failure, every pre-existing file (config, all token caches, all STS
// caches) must be byte-for-byte identical to its pre-operation state — proving
// the rollback restored everything exactly and touched nothing else.
func TestConfigureSSORebindRollbackRealFileCacheByteEquivalent(t *testing.T) {
	cacheDir := t.TempDir()
	configDir := t.TempDir()
	// The test relies on chmod 0500 on configDir to trigger a real config
	// write fault. Probe that this actually blocks writes on this platform;
	// skip if it does not (e.g. Windows, or running as root).
	probeReadOnlyDirEnforced(t, configDir)
	cfgPath := filepath.Join(configDir, "config.json")

	// Build config: profile "default" bound to old session "corp" with
	// acct-1/role-1; a second session "newcorp" is the rebind target.
	cfg := testConfigWithSession()
	p := cfg.Profiles["default"]
	p.Mode = config.AuthModeSSO
	p.SSOSessionName = "corp"
	p.AccountID = "acct-1"
	p.RoleName = "role-1"
	cfg.Profiles["default"] = p
	cfg.SSOSessions["newcorp"] = config.SSOSession{
		Name: "newcorp", StartURL: "https://newcorp.example.com", Region: "cn-beijing",
		RegistrationScopes: []string{sso.ScopeAccountAccess, sso.ScopeOfflineAccess},
	}
	if err := config.Save(cfg, cfgPath); err != nil {
		t.Fatalf("save config: %v", err)
	}

	// Real FileCache.
	cache, err := sso.NewFileCache(cacheDir)
	if err != nil {
		t.Fatalf("cache: %v", err)
	}

	// Seed real caches: old token (corp), pre-existing target token (newcorp),
	// and old STS (corp/acct-1/role-1).
	oldToken := testTokenCache()
	oldToken.AccessToken = "old-corp-token-bytes"
	if err := cache.WriteToken(oldToken); err != nil {
		t.Fatalf("seed old token: %v", err)
	}
	newcorpExisting := testTokenCache()
	newcorpExisting.StartURL = "https://newcorp.example.com"
	newcorpExisting.SessionName = "newcorp"
	newcorpExisting.AccessToken = "existing-newcorp-token-bytes"
	if err := cache.WriteToken(newcorpExisting); err != nil {
		t.Fatalf("seed newcorp token: %v", err)
	}
	if err := cache.WriteSTS(&sso.STSCache{
		SessionName: "corp", AccountID: "acct-1", RoleName: "role-1",
		AccessKeyID: "old-sts-access-key", ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("seed old sts: %v", err)
	}

	// Snapshot every file in the cache dir by enumerating token-*.json and
	// sts-*.json — no private path computation needed.
	snapshotCacheFiles := func() map[string][]byte {
		entries, rerr := os.ReadDir(cacheDir)
		if rerr != nil {
			t.Fatalf("read cache dir: %v", rerr)
		}
		out := map[string][]byte{}
		for _, e := range entries {
			name := e.Name()
			if !strings.HasPrefix(name, "token-") && !strings.HasPrefix(name, "sts-") {
				continue
			}
			b, rerr := os.ReadFile(filepath.Join(cacheDir, name))
			if rerr != nil {
				t.Fatalf("read %s: %v", name, rerr)
			}
			out[name] = b
		}
		return out
	}
	cacheBefore := snapshotCacheFiles()
	configBefore, rerr := os.ReadFile(cfgPath)
	if rerr != nil {
		t.Fatalf("read config: %v", rerr)
	}

	// Use the real production config store (real config.Load/config.Update).
	t.Setenv("VOLCLOG_CONFIG", cfgPath)

	// DeviceFlow: writes the new target-session token to the real cache, then
	// makes the config directory read-only so the subsequent config.Update
	// fails with a real filesystem fault (cannot create lock file / write).
	// t.Cleanup restores permissions before the dir is removed.
	newToken := testTokenCache()
	newToken.StartURL = "https://newcorp.example.com"
	newToken.SessionName = "newcorp"
	newToken.AccessToken = "new-token-written-by-deviceflow"
	df := &realWriteThenCorruptDeviceFlow{
		token:      newToken,
		cache:      cache,
		corruptDir: configDir,
	}
	t.Cleanup(func() {
		if err := os.Chmod(configDir, 0o700); err != nil {
			t.Errorf("restore config dir perms in cleanup: %v", err)
		}
	})

	adapter := &ssoAdapter{
		cache:    cache,
		cfgStore: productionConfigStore{},
		stdout:   &bytes.Buffer{},
		stderr:   &bytes.Buffer{},
		clock:    time.Now,
		deviceFlowFn: func(_ config.SSOSession, _ bool) (ssoDeviceFlow, error) {
			return df, nil
		},
		bindingFn: func(_ config.SSOSession) (ssoBindingService, error) {
			return &fakeSSOBindingService{result: &sso.BindingResult{AccountID: "acct-2", RoleName: "role-2"}}, nil
		},
		revokerFn: func(_ string) (ssoOAuthRevoker, error) {
			return &fakeSSORevoker{}, nil
		},
	}

	_, err = adapter.runConfigureSSO(context.Background(), ssoConfigureOpts{
		Profile:    "default",
		SSOSession: "newcorp",
	})
	// Restore directory permissions immediately so subsequent reads work.
	if cerr := os.Chmod(configDir, 0o700); cerr != nil {
		t.Fatalf("restore config dir perms: %v", cerr)
	}
	if err == nil {
		t.Fatal("expected error from config failure")
	}

	// Re-read every file AFTER the failed operation and assert byte-for-byte
	// identical: rollback must have restored old token + old STS, and must not
	// have touched the config file or the pre-existing target token.
	cacheAfter := snapshotCacheFiles()
	if len(cacheAfter) != len(cacheBefore) {
		t.Fatalf("cache file count changed: before=%d after=%d", len(cacheBefore), len(cacheAfter))
	}
	for name, before := range cacheBefore {
		after, ok := cacheAfter[name]
		if !ok {
			t.Fatalf("cache file %s missing after failed rebind", name)
		}
		if !bytes.Equal(before, after) {
			t.Fatalf("cache file %s changed after failed rebind", name)
		}
	}
	configAfter, rerr := os.ReadFile(cfgPath)
	if rerr != nil {
		t.Fatalf("read config after: %v", rerr)
	}
	if !bytes.Equal(configBefore, configAfter) {
		t.Fatalf("config file changed after failed rebind")
	}
}

// realWriteThenCorruptDeviceFlow writes the token to the real cache (mirroring
// *sso.DeviceFlow behavior), then makes a directory read-only to trigger a real
// filesystem fault in the subsequent config.Update.
type realWriteThenCorruptDeviceFlow struct {
	token      *sso.TokenCache
	cache      *sso.FileCache
	corruptDir string
}

func (r *realWriteThenCorruptDeviceFlow) Login(_ context.Context) (*sso.TokenCache, error) {
	if err := r.cache.WriteToken(r.token); err != nil {
		return nil, err
	}
	// Real filesystem fault: make the config dir read-only so config.Update
	// cannot create its lock file or write the config. A chmod failure is
	// returned so the caller knows the corruption did not happen.
	if err := os.Chmod(r.corruptDir, 0o500); err != nil {
		return nil, err
	}
	return r.token, nil
}

// TestConfigureSSORollbackFailureFromRealFilesystemFault proves that when the
// DeviceFlow writes the token, corrupts the token file path (replaces it with a
// directory), and returns the original login error, the compensation WriteToken
// fails with a real securestore error. The error is classified as
// ErrSSORollbackFailure and the chain reaches both the original login error AND
// the real securestore error. No fake failWriteToken is used; binding/config are
// never called because the transaction fails at DeviceFlow.
func TestConfigureSSORollbackFailureFromRealFilesystemFault(t *testing.T) {
	cacheDir := t.TempDir()
	configDir := t.TempDir()
	cfgPath := filepath.Join(configDir, "config.json")

	cfg := testConfigWithSession()
	p := cfg.Profiles["default"]
	p.Mode = config.AuthModeSSO
	p.SSOSessionName = "corp"
	p.AccountID = "acct-1"
	p.RoleName = "role-1"
	cfg.Profiles["default"] = p
	if err := config.Save(cfg, cfgPath); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cache, err := sso.NewFileCache(cacheDir)
	if err != nil {
		t.Fatalf("cache: %v", err)
	}

	// Pre-existing token for the target session so oldExisted=true and rollback
	// must rewrite it.
	preExisting := testTokenCache()
	preExisting.AccessToken = "pre-existing-token"
	if err := cache.WriteToken(preExisting); err != nil {
		t.Fatalf("seed token: %v", err)
	}

	// Use the real production config store, wrapped in a counter to prove
	// config.Update is never called (Load may happen). The transaction must fail
	// at DeviceFlow, so config.Update is never called.
	t.Setenv("VOLCLOG_CONFIG", cfgPath)
	cfgStore := &countingConfigStore{inner: productionConfigStore{}}

	// DeviceFlow: writes the new token (real), precisely locates the target token
	// file and replaces it with a directory, then returns the original login
	// error. runConfigureSSO immediately enters compensation; WriteToken(oldToken)
	// fails with securestore.ErrInvalidPath.
	loginErr := errors.New("device flow network failure")
	newToken := testTokenCache()
	newToken.AccessToken = "newly-written-token"
	df := &realWriteThenCorruptTokenPathDeviceFlow{
		token:    newToken,
		cache:    cache,
		cacheDir: cacheDir,
		loginErr: loginErr,
	}

	// Counting binding service: ResolveBinding must never be invoked because the
	// transaction fails at DeviceFlow.
	bindingSvc := &countingBindingService{inner: &fakeSSOBindingService{result: &sso.BindingResult{AccountID: "acct-1", RoleName: "role-1"}}}

	adapter := &ssoAdapter{
		cache:    cache,
		cfgStore: cfgStore,
		stdout:   &bytes.Buffer{},
		stderr:   &bytes.Buffer{},
		clock:    time.Now,
		deviceFlowFn: func(_ config.SSOSession, _ bool) (ssoDeviceFlow, error) {
			return df, nil
		},
		bindingFn: func(_ config.SSOSession) (ssoBindingService, error) {
			return bindingSvc, nil
		},
		revokerFn: func(_ string) (ssoOAuthRevoker, error) {
			return &fakeSSORevoker{}, nil
		},
	}

	_, err = adapter.runConfigureSSO(context.Background(), ssoConfigureOpts{
		Profile:    "default",
		SSOSession: "corp",
	})
	if err == nil {
		t.Fatal("expected rollback failure error")
	}

	// Must be classified as ErrSSORollbackFailure.
	if !errors.Is(err, ErrSSORollbackFailure) {
		t.Fatalf("error is not ErrSSORollbackFailure: %v", err)
	}
	// The original login error must be reachable.
	if !errors.Is(err, loginErr) {
		t.Fatalf("original login error not in chain: %v", err)
	}
	// A real securestore error (from WriteToken failing because the token path
	// is now a directory) must be reachable via errors.Is.
	if !errors.Is(err, securestore.ErrInvalidPath) {
		t.Fatalf("real securestore error not in chain: %v", err)
	}
	// Explicit zero-call proof: ResolveBinding and config.Update must never run.
	if got := atomic.LoadInt32(&bindingSvc.resolveCnt); got != 0 {
		t.Fatalf("ResolveBinding called %d times, want 0", got)
	}
	if got := atomic.LoadInt32(&cfgStore.updateCnt); got != 0 {
		t.Fatalf("config.Update called %d times, want 0", got)
	}
}

// countingConfigStore delegates to an inner configStore (e.g.
// productionConfigStore) and counts Load/Update calls for test assertions.
type countingConfigStore struct {
	inner     configStore
	loadCnt   int32
	updateCnt int32
}

func (c *countingConfigStore) Load() (config.Config, string, error) {
	atomic.AddInt32(&c.loadCnt, 1)
	return c.inner.Load()
}

func (c *countingConfigStore) Update(path string, fn func(*config.Config) error) (config.Config, error) {
	atomic.AddInt32(&c.updateCnt, 1)
	return c.inner.Update(path, fn)
}

// countingBindingService wraps a ssoBindingService and counts ResolveBinding
// calls for test assertions.
type countingBindingService struct {
	inner      ssoBindingService
	resolveCnt int32
}

func (c *countingBindingService) ResolveBinding(ctx context.Context, accessToken, explicitAccountID, explicitRoleName string) (*sso.BindingResult, error) {
	atomic.AddInt32(&c.resolveCnt, 1)
	return c.inner.ResolveBinding(ctx, accessToken, explicitAccountID, explicitRoleName)
}

// deleteCountingCache wraps a real *sso.FileCache and counts DeleteToken calls.
// All other methods delegate directly to the underlying cache so the real
// filesystem behavior (including WriteToken failures) is preserved.
type deleteCountingCache struct {
	*sso.FileCache
	deleteCnt int32
}

func (d *deleteCountingCache) DeleteToken(startURL, sessionName string) error {
	atomic.AddInt32(&d.deleteCnt, 1)
	return d.FileCache.DeleteToken(startURL, sessionName)
}

// probeReadOnlyDirEnforced verifies that making dir read-only (0500) actually
// prevents file creation inside it. Some platforms (Windows) or privilege
// levels (e.g. root) bypass directory write-permission bits, which would make
// tests that rely on a real filesystem fault suffer a platform-specific
// failure (the fault premise cannot be established). The probe:
//   - chmods dir to 0500,
//   - attempts to create a temp file in dir,
//   - restores dir to 0700 (and removes the probe file if one was created),
//   - t.Skips the calling test only if the create unexpectedly succeeded, and
//     t.Fatalf's on any chmod/restore/cleanup error (never swallowed).
//
// It never guesses by GOOS and never lets a test silently pass.
func probeReadOnlyDirEnforced(t *testing.T, dir string) {
	t.Helper()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("probe: chmod %s 0500: %v", dir, err)
	}
	f, createErr := os.CreateTemp(dir, ".probe-*")
	// Restore permissions immediately, regardless of create outcome.
	if cerr := os.Chmod(dir, 0o700); cerr != nil {
		if f != nil {
			t.Fatalf("probe: restore %s 0700: %v; cleanup: %v", dir, cerr, closeAndRemove(f))
		}
		t.Fatalf("probe: restore %s 0700: %v", dir, cerr)
	}
	if createErr == nil {
		// Write succeeded: the current platform/privilege does not enforce
		// read-only directories, so the real-filesystem-fault premise cannot
		// be established and the test would hit a platform-specific failure.
		// Skip rather than assert a behavior that cannot hold.
		if cerr := closeAndRemove(f); cerr != nil {
			t.Fatalf("probe: cleanup after unexpected successful create in %s: %v", dir, cerr)
		}
		t.Skipf("skipping: platform/privilege does not enforce read-only directory %s (chmod 0500 did not block file creation)", dir)
	}
}

// closeAndRemove best-effort closes f and removes its file, returning a single
// combined error so neither step is skipped when the other fails.
func closeAndRemove(f *os.File) error {
	name := f.Name()
	closeErr := f.Close()
	removeErr := os.Remove(name)
	switch {
	case closeErr != nil && removeErr != nil:
		return fmt.Errorf("close %s: %v; remove %s: %v", name, closeErr, name, removeErr)
	case closeErr != nil:
		return fmt.Errorf("close %s: %v", name, closeErr)
	case removeErr != nil:
		return fmt.Errorf("remove %s: %v", name, removeErr)
	}
	return nil
}

// countingRevoker wraps an ssoOAuthRevoker and counts RevokeToken calls.
type countingRevoker struct {
	inner ssoOAuthRevoker
	cnt   *int32
}

func (c *countingRevoker) RevokeToken(ctx context.Context, req *sso.RevokeTokenRequest) error {
	atomic.AddInt32(c.cnt, 1)
	return c.inner.RevokeToken(ctx, req)
}

// realWriteThenCorruptTokenPathDeviceFlow writes the token to the real cache,
// precisely locates the target token file (computed from the token's
// StartURL/SessionName) and replaces it with a directory so a subsequent
// WriteToken (rollback compensation) fails with a real securestore error. Then
// returns loginErr so the caller enters compensation immediately.
type realWriteThenCorruptTokenPathDeviceFlow struct {
	token    *sso.TokenCache
	cache    *sso.FileCache
	cacheDir string
	loginErr error
}

func (r *realWriteThenCorruptTokenPathDeviceFlow) Login(_ context.Context) (*sso.TokenCache, error) {
	if err := r.cache.WriteToken(r.token); err != nil {
		return nil, err
	}
	// Precisely locate the target token file using the same algorithm as the
	// cache: DigestKey(CanonicalStartURL(startURL), sessionName). We must not
	// silently corrupt a different file.
	canonical, err := sso.CanonicalStartURL(r.token.StartURL)
	if err != nil {
		return nil, err
	}
	digest := securestore.DigestKey(canonical, r.token.SessionName)
	tokenPath := filepath.Join(r.cacheDir, "token-"+digest+".json")
	info, lerr := os.Lstat(tokenPath)
	if lerr != nil {
		return nil, fmt.Errorf("target token file not found at %s: %w", tokenPath, lerr)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("target token path %s is not a regular file", tokenPath)
	}
	// Replace the file with a directory so WriteToken fails with
	// securestore.ErrInvalidPath (path is not a regular file).
	if rerr := os.Remove(tokenPath); rerr != nil {
		return nil, rerr
	}
	if rerr := os.Mkdir(tokenPath, 0o700); rerr != nil {
		return nil, rerr
	}
	// Return the original login error so the caller enters compensation.
	return nil, r.loginErr
}

// TestConfigureSSODeviceFlowWriteFailsPreservesOldToken is a real FileCache test:
// the DeviceFlow's real atomic WriteToken fails due to a controlled filesystem
// permission fault (cache dir made read-only). The old token file must remain
// byte-for-byte unchanged, DeleteToken must never be called, and
// ResolveBinding/config.Update must never run. No fake failWriteToken is used.
func TestConfigureSSODeviceFlowWriteFailsPreservesOldToken(t *testing.T) {
	cacheDir := t.TempDir()
	// The test relies on chmod 0500 on cacheDir to trigger a real WriteToken
	// fault. Probe that this actually blocks writes on this platform; skip if
	// it does not (e.g. Windows, or running as root).
	probeReadOnlyDirEnforced(t, cacheDir)
	configDir := t.TempDir()
	cfgPath := filepath.Join(configDir, "config.json")

	cfg := testConfigWithSession()
	p := cfg.Profiles["default"]
	p.Mode = config.AuthModeSSO
	p.SSOSessionName = "corp"
	p.AccountID = "acct-1"
	p.RoleName = "role-1"
	cfg.Profiles["default"] = p
	if err := config.Save(cfg, cfgPath); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cache, err := sso.NewFileCache(cacheDir)
	if err != nil {
		t.Fatalf("cache: %v", err)
	}
	// Wrap the real cache to count DeleteToken; all other methods delegate.
	cacheWrapper := &deleteCountingCache{FileCache: cache}

	// Pre-seed the old token and snapshot its raw bytes.
	oldToken := testTokenCache()
	oldToken.AccessToken = "old-token-before-write-fail"
	if err := cacheWrapper.WriteToken(oldToken); err != nil {
		t.Fatalf("seed old token: %v", err)
	}
	canonical, _ := sso.CanonicalStartURL(oldToken.StartURL)
	tokenPath := filepath.Join(cacheDir, "token-"+securestore.DigestKey(canonical, oldToken.SessionName)+".json")
	oldBytes, rerr := os.ReadFile(tokenPath)
	if rerr != nil {
		t.Fatalf("read old token bytes: %v", rerr)
	}

	t.Setenv("VOLCLOG_CONFIG", cfgPath)
	cfgStore := &countingConfigStore{inner: productionConfigStore{}}
	bindingSvc := &countingBindingService{inner: &fakeSSOBindingService{result: &sso.BindingResult{AccountID: "acct-1", RoleName: "role-1"}}}

	// DeviceFlow: makes the cache dir read-only so the real WriteToken fails with
	// a permission error, then restores perms. The token lock is already held
	// when Login runs, so only WriteToken is affected.
	df := &fsFailWriteDeviceFlow{token: testTokenCache(), cache: cache, cacheDir: cacheDir}
	t.Cleanup(func() {
		if err := os.Chmod(cacheDir, 0o700); err != nil {
			t.Errorf("restore cache dir perms in cleanup: %v", err)
		}
	})

	adapter := &ssoAdapter{
		cache:    cacheWrapper,
		cfgStore: cfgStore,
		stdout:   &bytes.Buffer{},
		stderr:   &bytes.Buffer{},
		clock:    time.Now,
		deviceFlowFn: func(_ config.SSOSession, _ bool) (ssoDeviceFlow, error) {
			return df, nil
		},
		bindingFn: func(_ config.SSOSession) (ssoBindingService, error) {
			return bindingSvc, nil
		},
		revokerFn: func(_ string) (ssoOAuthRevoker, error) {
			return &fakeSSORevoker{}, nil
		},
	}

	_, err = adapter.runConfigureSSO(context.Background(), ssoConfigureOpts{Profile: "default", SSOSession: "corp"})
	// Restore perms immediately for subsequent reads.
	if cerr := os.Chmod(cacheDir, 0o700); cerr != nil {
		t.Fatalf("restore cache dir perms: %v", cerr)
	}
	if err == nil {
		t.Fatal("expected error from WriteToken failure")
	}

	// Old token must be byte-for-byte unchanged (WriteToken failed, restore read
	// it and found it already matched, so no delete was attempted).
	afterBytes, rerr := os.ReadFile(tokenPath)
	if rerr != nil {
		t.Fatalf("read old token after: %v", rerr)
	}
	if !bytes.Equal(oldBytes, afterBytes) {
		t.Fatalf("old token bytes changed after WriteToken failure")
	}
	// DeleteToken must never have been called (restore found the token already
	// matched and skipped restore entirely).
	if got := atomic.LoadInt32(&cacheWrapper.deleteCnt); got != 0 {
		t.Fatalf("DeleteToken called %d times, want 0", got)
	}
	// ResolveBinding and config.Update must never run (transaction failed at
	// DeviceFlow, before binding/config).
	if got := atomic.LoadInt32(&bindingSvc.resolveCnt); got != 0 {
		t.Fatalf("ResolveBinding called %d times, want 0", got)
	}
	if got := atomic.LoadInt32(&cfgStore.updateCnt); got != 0 {
		t.Fatalf("config.Update called %d times, want 0", got)
	}
}

// fsFailWriteDeviceFlow makes the cache directory read-only, then actually
// calls the real FileCache.WriteToken (which must fail with a permission error),
// restores permissions, and returns the real write error.
type fsFailWriteDeviceFlow struct {
	token    *sso.TokenCache
	cache    *sso.FileCache
	cacheDir string
}

func (f *fsFailWriteDeviceFlow) Login(_ context.Context) (*sso.TokenCache, error) {
	if err := os.Chmod(f.cacheDir, 0o500); err != nil {
		return nil, err
	}
	// Real atomic WriteToken must fail because the cache dir is now read-only.
	werr := f.cache.WriteToken(f.token)
	// Restore permissions before returning so the test can read files. A
	// restore failure is never dropped: joined with the write error when both
	// fail, or returned on its own when WriteToken unexpectedly succeeded.
	rerr := os.Chmod(f.cacheDir, 0o700)
	switch {
	case werr != nil && rerr != nil:
		return nil, errors.Join(werr, rerr)
	case rerr != nil:
		return nil, rerr
	case werr == nil:
		return nil, errors.New("expected WriteToken to fail on read-only cache dir, but it succeeded")
	}
	return nil, werr
}

// TestSSOLogoutDriftPreservesLocalStateAndSkipsRevoke is a real FileCache test:
// after logout acquires the token + STS locks and enters config.Update, the
// session URL is drifted (A→B) by directly rewriting the config file. Logout
// must detect the drift inside the config lock, abort without deleting the
// token or any STS, skip the remote revoke, and return a non-success error.
func TestSSOLogoutDriftPreservesLocalStateAndSkipsRevoke(t *testing.T) {
	cacheDir := t.TempDir()
	configDir := t.TempDir()
	cfgPath := filepath.Join(configDir, "config.json")

	cfg := testConfigWithSession()
	p := cfg.Profiles["default"]
	p.Mode = config.AuthModeSSO
	p.SSOSessionName = "corp"
	p.AccountID = "acct-1"
	p.RoleName = "role-1"
	cfg.Profiles["default"] = p
	if err := config.Save(cfg, cfgPath); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cache, err := sso.NewFileCache(cacheDir)
	if err != nil {
		t.Fatalf("cache: %v", err)
	}
	seedRealSSOCaches(t, cache, testSSOSession())

	// Counting revoker to assert revoke is never called on drift.
	revokeCalled := int32(0)
	revoker := &countingRevoker{inner: &fakeSSORevoker{}, cnt: &revokeCalled}

	// Barrier config store: blocks inside Update until released, so the test can
	// drift the config while logout holds token + STS locks.
	enterUpdate := make(chan struct{})
	releaseUpdate := make(chan struct{})
	t.Setenv("VOLCLOG_CONFIG", cfgPath)
	cfgStore := &barrierConfigStore{
		inner:       productionConfigStore{},
		enterUpdate: enterUpdate,
		release:     releaseUpdate,
	}

	adapter := &ssoAdapter{
		cache:    cache,
		cfgStore: cfgStore,
		stdout:   &bytes.Buffer{},
		stderr:   &bytes.Buffer{},
		clock:    time.Now,
		revokerFn: func(_ string) (ssoOAuthRevoker, error) {
			return revoker, nil
		},
	}

	// Run logout in a goroutine; it will block inside config.Update.
	logoutDone := make(chan error, 1)
	go func() {
		_, err := adapter.runSSOLogout(context.Background(), ssoLogoutOpts{SSOSession: "corp"})
		logoutDone <- err
	}()

	// Wait for logout to enter config.Update (token + STS locks held).
	select {
	case <-enterUpdate:
	case <-time.After(5 * time.Second):
		t.Fatal("logout did not enter config.Update")
	}

	// Drift the session URL A→B by directly rewriting the config file. This
	// simulates a concurrent configure sso-session while logout holds the locks.
	drifted := testConfigWithSession()
	drifted.SSOSessions["corp"] = config.SSOSession{
		Name: "corp", StartURL: "https://drifted.example.com/userportal", Region: "cn-beijing",
		RegistrationScopes: []string{sso.ScopeAccountAccess, sso.ScopeOfflineAccess},
	}
	dp := drifted.Profiles["default"]
	dp.Mode = config.AuthModeSSO
	dp.SSOSessionName = "corp"
	dp.AccountID = "acct-1"
	dp.RoleName = "role-1"
	drifted.Profiles["default"] = dp
	if err := config.Save(drifted, cfgPath); err != nil {
		t.Fatalf("drift config: %v", err)
	}

	// Release the barrier so logout's config.Update callback runs and detects
	// the drift.
	close(releaseUpdate)

	select {
	case err := <-logoutDone:
		if err == nil {
			t.Fatal("logout should fail on drift, got nil")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("logout did not return after drift")
	}

	// Revoke must never have been called (drift detected before revoke).
	if got := atomic.LoadInt32(&revokeCalled); got != 0 {
		t.Fatalf("revoke called %d times on drift, want 0", got)
	}

	// Token and STS must be preserved (no local deletion on drift).
	_, rerr := cache.ReadToken(testSSOSession().StartURL, "corp")
	if rerr != nil {
		t.Fatalf("token should be preserved after drift: %v", rerr)
	}
	_, serr := cache.ReadSTS("corp", "acct-1", "role-1")
	if serr != nil {
		t.Fatalf("STS should be preserved after drift: %v", serr)
	}
}

// barrierConfigStore delegates to an inner configStore but blocks inside
// Update until the release channel is closed. It closes enterUpdate when
// Update is entered.
type barrierConfigStore struct {
	inner       configStore
	enterUpdate chan struct{}
	release     chan struct{}
}

func (b *barrierConfigStore) Load() (config.Config, string, error) {
	return b.inner.Load()
}

func (b *barrierConfigStore) Update(path string, fn func(*config.Config) error) (config.Config, error) {
	close(b.enterUpdate)
	<-b.release
	return b.inner.Update(path, fn)
}

// TestCrossSessionRebindQueuedProviderCleansStaleSTS is a real FileCache test:
// a cross-session rebind (A→B) holds both old+new token locks. An old Provider
// for session A queues on the old token lock. After the rebind succeeds and
// releases the locks, the old Provider proceeds: it reads the old token,
// exchanges STS, writes an uncommitted STS, then patchConfig fails because the
// profile is now bound to B (binding mismatch). The Provider must delete the
// uncommitted STS and return no credentials. Final assertion: old STS is
// ErrMissing (no stale cache left behind).
func TestCrossSessionRebindQueuedProviderCleansStaleSTS(t *testing.T) {
	cacheDir := t.TempDir()
	configDir := t.TempDir()
	cfgPath := filepath.Join(configDir, "config.json")

	// Two sessions: A (corp) and B (newcorp). Profile "default" bound to A.
	cfg := testConfigWithSession()
	p := cfg.Profiles["default"]
	p.Mode = config.AuthModeSSO
	p.SSOSessionName = "corp"
	p.AccountID = "acct-1"
	p.RoleName = "role-1"
	cfg.Profiles["default"] = p
	cfg.SSOSessions["newcorp"] = config.SSOSession{
		Name: "newcorp", StartURL: "https://newcorp.example.com", Region: "cn-beijing",
		RegistrationScopes: []string{sso.ScopeAccountAccess, sso.ScopeOfflineAccess},
	}
	if err := config.Save(cfg, cfgPath); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cache, err := sso.NewFileCache(cacheDir)
	if err != nil {
		t.Fatalf("cache: %v", err)
	}
	// Seed the old token for session A so the old Provider can read it.
	seedRealSSOCaches(t, cache, testSSOSession())

	t.Setenv("VOLCLOG_CONFIG", cfgPath)

	// Barrier to hold the rebind inside its token locks until the old Provider
	// has started and is waiting on the old token lock.
	rebindHold := make(chan struct{})
	rebindProceed := make(chan struct{})

	// Build the rebind adapter. Its DeviceFlow writes the new token for session
	// B, then blocks on rebindHold until released.
	newToken := testTokenCache()
	newToken.StartURL = "https://newcorp.example.com"
	newToken.SessionName = "newcorp"
	rebindAdapter := &ssoAdapter{
		cache:    cache,
		cfgStore: productionConfigStore{},
		stdout:   &bytes.Buffer{},
		stderr:   &bytes.Buffer{},
		clock:    time.Now,
		deviceFlowFn: func(_ config.SSOSession, _ bool) (ssoDeviceFlow, error) {
			return &holdDeviceFlow{token: newToken, cache: cache, hold: rebindHold, proceed: rebindProceed}, nil
		},
		bindingFn: func(_ config.SSOSession) (ssoBindingService, error) {
			return &fakeSSOBindingService{result: &sso.BindingResult{AccountID: "acct-2", RoleName: "role-2"}}, nil
		},
		revokerFn: func(_ string) (ssoOAuthRevoker, error) {
			return &fakeSSORevoker{}, nil
		},
	}

	// Start the rebind in a goroutine. It will hold both old+new token locks
	// and block inside DeviceFlow.
	rebindDone := make(chan error, 1)
	go func() {
		_, err := rebindAdapter.runConfigureSSO(context.Background(), ssoConfigureOpts{Profile: "default", SSOSession: "newcorp"})
		rebindDone <- err
	}()

	// Wait for the rebind to enter DeviceFlow (holding both token locks).
	select {
	case <-rebindHold:
	case <-time.After(5 * time.Second):
		t.Fatal("rebind did not enter DeviceFlow")
	}

	// Build and start the old Provider for session A. It will block trying to
	// acquire the old token lock (held by the rebind). A context-scoped
	// WithLockContentionObserver fires exactly when the Provider's real
	// WithTokenLock call blocks on the held lock — strict proof of real lock
	// contention, not just "goroutine started".
	sessA := testSSOSession()
	lockContended := make(chan struct{})
	var obsOnce sync.Once
	provCtx := securestore.WithLockContentionObserver(context.Background(), func() {
		obsOnce.Do(func() { close(lockContended) })
	})
	oldProvider, err := sso.NewSSOProvider(&sso.SSOProviderConfig{
		ConfigPath:  cfgPath,
		ProfileName: "default",
		StartURL:    sessA.StartURL,
		SessionName: sessA.Name,
		SSORegion:   sessA.Region,
		AccountID:   "acct-1",
		RoleName:    "role-1",
		Cache:       cache,
		OAuth:       &fakeSSOOAuth{},
		Portal:      &fakeSSOSTSExchanger{},
		Clock:       time.Now,
	})
	if err != nil {
		t.Fatalf("NewSSOProvider: %v", err)
	}
	providerResult := make(chan providerRetrieveResult, 1)
	go func() {
		val, err := oldProvider.Retrieve(provCtx)
		providerResult <- providerRetrieveResult{val: val, err: err}
	}()

	// Wait for the Provider to actually block on the old token lock (queued
	// behind the rebind), then release the rebind so it can complete the
	// profile rebind to B. time.After is a deadlock-timeout guard only.
	select {
	case <-lockContended:
	case <-time.After(5 * time.Second):
		t.Fatal("old provider did not contend on the old token lock")
	}
	close(rebindProceed)

	// Wait for the rebind to finish.
	select {
	case err := <-rebindDone:
		if err != nil {
			t.Fatalf("rebind failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("rebind did not complete")
	}

	// Wait for the old Provider to finish. It should have: read old token,
	// exchanged STS, written uncommitted STS, failed patchConfig (binding
	// mismatch since profile is now B), deleted the uncommitted STS, and
	// returned no credentials.
	var provResult providerRetrieveResult
	select {
	case provResult = <-providerResult:
	case <-time.After(5 * time.Second):
		t.Fatal("old provider did not finish")
	}
	// The Provider must return an error (binding mismatch) and no credentials.
	if provResult.err == nil {
		t.Fatal("old provider should return error after rebind, got nil")
	}
	if provResult.val.AccessKeyID != "" {
		t.Fatalf("old provider returned AccessKeyID %q, want empty", provResult.val.AccessKeyID)
	}
	if provResult.val.SecretAccessKey != "" {
		t.Fatalf("old provider returned SecretAccessKey %q, want empty", provResult.val.SecretAccessKey)
	}
	if provResult.val.SessionToken != "" {
		t.Fatalf("old provider returned SessionToken %q, want empty", provResult.val.SessionToken)
	}

	// The old STS cache must be gone (the Provider deleted it on binding
	// mismatch). No stale uncommitted cache may remain.
	_, serr := cache.ReadSTS("corp", "acct-1", "role-1")
	if !errors.Is(serr, securestore.ErrMissing) {
		t.Fatalf("old STS should be missing after rebind+provider, got err=%v", serr)
	}
}

// providerRetrieveResult captures the (value, error) return of a Provider
// Retrieve call so tests can assert no credentials were returned.
type providerRetrieveResult struct {
	val auth.Value
	err error
}

// holdDeviceFlow writes the token, signals it is holding the locks, then blocks
// until proceed is closed.
type holdDeviceFlow struct {
	token   *sso.TokenCache
	cache   *sso.FileCache
	hold    chan struct{}
	proceed chan struct{}
}

func (h *holdDeviceFlow) Login(_ context.Context) (*sso.TokenCache, error) {
	if err := h.cache.WriteToken(h.token); err != nil {
		return nil, err
	}
	close(h.hold)
	<-h.proceed
	return h.token, nil
}

// ---------------------------------------------------------------------------
// 6. Logout transaction: profile+session conflict, drift, lock order
// ---------------------------------------------------------------------------

func TestSSOLogoutRejectsProfileAndSessionCombined(t *testing.T) {
	ctx, _ := newSSOTestContext(t, testConfigWithSession())
	_, err := runSSOLogoutWithFactory(ctx, []string{"--profile", "default", "--sso-session", "corp"}, func(_ *Context) (*ssoAdapter, error) {
		t.Fatal("factory should not be called")
		return nil, nil
	})
	if err == nil {
		t.Fatal("expected error for profile+session")
	}
	if !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("expected conflict error, got: %v", err)
	}
}

func TestSSOLogoutRejectsGlobalProfileWithSession(t *testing.T) {
	ctx, _ := newSSOTestContext(t, testConfigWithSession())
	ctx.Profile = "default"
	_, err := runSSOLogoutWithFactory(ctx, []string{"--sso-session", "corp"}, func(_ *Context) (*ssoAdapter, error) {
		t.Fatal("factory should not be called")
		return nil, nil
	})
	if err == nil {
		t.Fatal("expected error for global profile + session")
	}
}

func TestSSOLoginRejectsGlobalProfileWithSession(t *testing.T) {
	ctx, _ := newSSOTestContext(t, testConfigWithSession())
	ctx.Profile = "default"
	_, err := runSSOLoginWithFactory(ctx, []string{"--sso-session", "corp"}, func(_ *Context) (*ssoAdapter, error) {
		t.Fatal("factory should not be called")
		return nil, nil
	})
	if err == nil {
		t.Fatal("expected error for global profile + session")
	}
}

func TestSSOLogoutReloadsConfigInsideLockAndDetectsDrift(t *testing.T) {
	cfg := testConfigWithSession()
	cache := newFakeSSOCache()
	cache.tokens[ssoTokenKey(testSSOSession().StartURL, "corp")] = testTokenCache()
	// driftStore returns the original config on the first Load (lock-free) and a
	// mutated config on the second Load (inside the token lock), simulating a
	// concurrent configure sso-session that changed the session region.
	driftStore := &driftConfigStore{first: cfg, second: func() config.Config {
		c := cfg
		// Deep-copy the SSOSessions map so mutating `second` does not affect
		// `first` (maps are reference types).
		c.SSOSessions = make(map[string]config.SSOSession, len(cfg.SSOSessions))
		for k, v := range cfg.SSOSessions {
			c.SSOSessions[k] = v
		}
		c.SSOSessions["corp"] = config.SSOSession{
			Name: "corp", StartURL: testSSOSession().StartURL, Region: "us-east-1",
			RegistrationScopes: testSSOSession().RegistrationScopes,
		}
		return c
	}()}
	adapter := &ssoAdapter{
		cache:    cache,
		cfgStore: driftStore,
		stdout:   &bytes.Buffer{},
		stderr:   &bytes.Buffer{},
		clock:    time.Now,
		revokerFn: func(_ string) (ssoOAuthRevoker, error) {
			return &fakeSSORevoker{}, nil
		},
	}
	_, err := adapter.runSSOLogout(context.Background(), ssoLogoutOpts{SSOSession: "corp"})
	if err == nil {
		t.Fatal("expected drift error")
	}
	// Token must NOT be deleted when drift is detected.
	if _, rerr := cache.ReadToken(testSSOSession().StartURL, "corp"); rerr != nil {
		t.Fatalf("token should be preserved on drift, got err=%v", rerr)
	}
}

// driftConfigStore returns `first` on the first Load and `second` thereafter.
type driftConfigStore struct {
	first  config.Config
	second config.Config
	calls  int32
}

func (d *driftConfigStore) Load() (config.Config, string, error) {
	if atomic.AddInt32(&d.calls, 1) == 1 {
		return d.first, "", nil
	}
	return d.second, "", nil
}
func (d *driftConfigStore) Update(string, func(*config.Config) error) (config.Config, error) {
	return d.second, nil
}

// lockOrderCache records acquire/release events and the held-set at each level.
type lockOrderCache struct {
	*fakeSSOCache
	mu     sync.Mutex
	events []string
	held   map[string]int // key -> hold count
}

func newLockOrderCache() *lockOrderCache {
	c := &lockOrderCache{fakeSSOCache: newFakeSSOCache(), held: map[string]int{}}
	return c
}

func (c *lockOrderCache) recordLocked(event, key string) {
	c.events = append(c.events, fmt.Sprintf("%s %s held=%v", event, key, c.snapshotHeldLocked()))
}

func (c *lockOrderCache) snapshotHeldLocked() []string {
	var out []string
	for k, n := range c.held {
		if n > 0 {
			out = append(out, k)
		}
	}
	return out
}

func (c *lockOrderCache) WithTokenLock(ctx context.Context, startURL, sessionName string, fn func() error) error {
	key := "token:" + ssoTokenKey(startURL, sessionName)
	c.mu.Lock()
	c.held[key]++
	c.recordLocked("acquire", key)
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.held[key]--
		c.recordLocked("release", key)
		c.mu.Unlock()
	}()
	return fn()
}

func (c *lockOrderCache) WithSTSLock(ctx context.Context, sessionName, accountID, roleName string, fn func() error) error {
	key := "sts:" + stsCacheKey(sessionName, accountID, roleName)
	c.mu.Lock()
	c.held[key]++
	c.recordLocked("acquire", key)
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.held[key]--
		c.recordLocked("release", key)
		c.mu.Unlock()
	}()
	return fn()
}

func TestSSOLogoutAcquiresLocksInDigestOrderAndReleasesReverse(t *testing.T) {
	cfg := testConfigWithSession()
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
	// Duplicate binding (same acct/role as "other") must be deduplicated.
	cfg.Profiles["dup"] = config.Profile{
		Mode: config.AuthModeSSO, SSOSessionName: "corp",
		AccountID: "acct-2", RoleName: "role-2",
	}

	cache := newLockOrderCache()
	cache.tokens[ssoTokenKey(testSSOSession().StartURL, "corp")] = testTokenCache()
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
	if _, err := adapter.runSSOLogout(context.Background(), ssoLogoutOpts{SSOSession: "corp"}); err != nil {
		t.Fatalf("logout: %v", err)
	}
	ev := cache.events
	// Expect: acquire token, acquire sts (lower digest), acquire sts (higher
	// digest), [work], release sts (higher), release sts (lower), release token.
	// With 2 unique keys there are 2 acquire + 2 release STS events plus 1+1 token.
	if len(ev) != 6 {
		t.Fatalf("expected 6 lock events, got %d: %v", len(ev), ev)
	}
	if !strings.HasPrefix(ev[0], "acquire token:") {
		t.Fatalf("first event should be token acquire: %s", ev[0])
	}
	if !strings.HasPrefix(ev[len(ev)-1], "release token:") {
		t.Fatalf("last event should be token release: %s", ev[len(ev)-1])
	}
	// STS acquires must be in ascending digest order; releases in reverse.
	var acqSts, relSts []string
	for _, e := range ev {
		if strings.HasPrefix(e, "acquire sts:") {
			acqSts = append(acqSts, e)
		}
		if strings.HasPrefix(e, "release sts:") {
			relSts = append(relSts, e)
		}
	}
	if len(acqSts) != 2 || len(relSts) != 2 {
		t.Fatalf("expected 2 sts acquire + 2 release, got acq=%v rel=%v", acqSts, relSts)
	}
	// Verify the real acquire order matches the expected digest-sorted order.
	// Extract the (session, account, role) from each acquire event and compute
	// its digest, then confirm they appear in ascending digest order.
	extractSTSKey := func(event string) (string, string, string) {
		// event format: "acquire sts:corp|acct-N|role-N held=[...]"
		body := strings.TrimPrefix(event, "acquire sts:")
		body = strings.Fields(body)[0]
		parts := strings.Split(body, "|")
		return parts[0], parts[1], parts[2]
	}
	var acqDigests []string
	for _, e := range acqSts {
		sn, acct, role := extractSTSKey(e)
		acqDigests = append(acqDigests, securestore.DigestKey(sn, acct, role))
	}
	if !sort.StringsAreSorted(acqDigests) {
		t.Fatalf("STS acquires not in digest order: %v", acqDigests)
	}
	// First acquired STS must be released last (LIFO).
	firstAcqKey := strings.TrimPrefix(acqSts[0], "acquire ")
	firstAcqKey = strings.Fields(firstAcqKey)[0]
	lastRelKey := strings.TrimPrefix(relSts[len(relSts)-1], "release ")
	lastRelKey = strings.Fields(lastRelKey)[0]
	if firstAcqKey != lastRelKey {
		t.Fatalf("STS locks not released in reverse order: firstAcq=%s lastRel=%s", firstAcqKey, lastRelKey)
	}
}

// ---------------------------------------------------------------------------
// 7. Logout partial classification
// ---------------------------------------------------------------------------

func TestSSOLogoutPartialRevokeClassified(t *testing.T) {
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
	revoker := &fakeSSORevoker{err: errors.New("remote boom")}
	adapter := newSSOAdapterForTest(cache, cfgStore, nil, nil, revoker)
	res, err := adapter.runSSOLogout(context.Background(), ssoLogoutOpts{SSOSession: "corp"})
	if err == nil {
		t.Fatal("expected partial failure error")
	}
	if !errors.Is(err, ErrSSOLogoutPartialFailure) {
		t.Fatalf("expected ErrSSOLogoutPartialFailure, got %v", err)
	}
	if !res.ClearedSession {
		t.Fatal("local session should still be cleared on revoke failure")
	}
	// Error text must not contain token material.
	if strings.Contains(err.Error(), "access-token-canary") || strings.Contains(err.Error(), "refresh-token-canary") {
		t.Fatalf("partial error leaks token: %v", err)
	}
	// classifyError must return partial_failure with an SSO-appropriate hint.
	payload, code := classifyError(err, "req", 0, "sso")
	if payload.Kind != "partial_failure" {
		t.Fatalf("kind = %q, want partial_failure", payload.Kind)
	}
	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if strings.Contains(payload.Hint, "console login cache") {
		t.Fatalf("hint mentions console login cache: %q", payload.Hint)
	}
}

func TestSSOLogoutPartialConfigUpdateClassified(t *testing.T) {
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
	cfgStore.updateErr = errors.New("config boom")
	adapter := newSSOAdapterForTest(cache, cfgStore, nil, nil, &fakeSSORevoker{})
	res, err := adapter.runSSOLogout(context.Background(), ssoLogoutOpts{SSOSession: "corp"})
	if err == nil {
		t.Fatal("expected partial failure error")
	}
	if !errors.Is(err, ErrSSOLogoutPartialFailure) {
		t.Fatalf("expected ErrSSOLogoutPartialFailure, got %v", err)
	}
	// Token and STS should still be deleted even though config update failed.
	if _, rerr := cache.ReadToken(testSSOSession().StartURL, "corp"); !errors.Is(rerr, securestore.ErrMissing) {
		t.Fatalf("token should be deleted, got err=%v", rerr)
	}
	_ = res
}

func TestSSOLogoutNilRevokerFactoryNotSilentSuccessWhenRefreshTokenPresent(t *testing.T) {
	cfg := testConfigWithSession()
	p := cfg.Profiles["default"]
	p.Mode = config.AuthModeSSO
	p.SSOSessionName = "corp"
	p.AccountID = "acct-1"
	p.RoleName = "role-1"
	cfg.Profiles["default"] = p
	cache := newFakeSSOCache()
	tok := testTokenCache()
	tok.RefreshToken = "has-refresh"
	cache.tokens[ssoTokenKey(testSSOSession().StartURL, "corp")] = tok
	cfgStore := &fakeConfigStore{cfg: cfg, path: ""}
	// revokerFn is nil.
	adapter := &ssoAdapter{
		cache:    cache,
		cfgStore: cfgStore,
		stdout:   &bytes.Buffer{},
		stderr:   &bytes.Buffer{},
		clock:    time.Now,
	}
	_, err := adapter.runSSOLogout(context.Background(), ssoLogoutOpts{SSOSession: "corp"})
	if err == nil {
		t.Fatal("expected partial failure when revoker factory is nil and refresh token present")
	}
	if !errors.Is(err, ErrSSOLogoutPartialFailure) {
		t.Fatalf("expected partial failure, got %v", err)
	}
}

func TestSSOLogoutReadTokenNonMissingErrorNotIgnored(t *testing.T) {
	cfg := testConfigWithSession()
	p := cfg.Profiles["default"]
	p.Mode = config.AuthModeSSO
	p.SSOSessionName = "corp"
	p.AccountID = "acct-1"
	p.RoleName = "role-1"
	cfg.Profiles["default"] = p
	cache := newFakeSSOCache()
	cache.failReadToken = errors.New("corrupt")
	cfgStore := &fakeConfigStore{cfg: cfg, path: ""}
	adapter := newSSOAdapterForTest(cache, cfgStore, nil, nil, &fakeSSORevoker{})
	_, err := adapter.runSSOLogout(context.Background(), ssoLogoutOpts{SSOSession: "corp"})
	if err == nil {
		t.Fatal("expected error for non-missing read failure")
	}
	if errors.Is(err, ErrSSOLogoutPartialFailure) {
		t.Fatalf("non-missing read error should not be a partial failure: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 8. Real FileCache + SSOProvider concurrency
// ---------------------------------------------------------------------------

func TestSSOLoginFirstThenProviderSeesCommittedToken(t *testing.T) {
	dir := t.TempDir()
	cache, err := sso.NewFileCache(dir)
	if err != nil {
		t.Fatalf("cache: %v", err)
	}
	cfg := testConfigWithSession()
	p := cfg.Profiles["default"]
	p.Mode = config.AuthModeSSO
	p.SSOSessionName = "corp"
	p.AccountID = "acct-1"
	p.RoleName = "role-1"
	cfg.Profiles["default"] = p
	cfgPath := filepath.Join(dir, "config.json")
	if err := config.Save(cfg, cfgPath); err != nil {
		t.Fatalf("save config: %v", err)
	}

	loginToken := testTokenCache()
	loginToken.AccessToken = "login-committed-token"
	adapter := realFileCacheAdapter(t, cache, cfg, cfgPath, loginToken)

	// Barrier: pause the CLI login inside the token lock (after device flow
	// writes the new token) so the provider must wait.
	loginHolding := make(chan struct{})
	proceedLogin := make(chan struct{})
	adapter.deviceFlowFn = func(_ config.SSOSession, _ bool) (ssoDeviceFlow, error) {
		return &barrierDeviceFlow{
			token:   loginToken,
			cache:   cache,
			holding: loginHolding,
			proceed: proceedLogin,
		}, nil
	}

	oauth := &fakeSSOOAuth{}
	sts := &fakeSSOSTSExchanger{}
	provider := newRealSSOProvider(t, cache, cfgPath, "default", testSSOSession(), oauth, sts)

	loginDone := make(chan error, 1)
	go func() {
		_, err := adapter.runConfigureSSO(context.Background(), ssoConfigureOpts{Profile: "default", SSOSession: "corp"})
		loginDone <- err
	}()

	// Wait until login is holding the token lock, then start the provider
	// with a short deadline. If the provider is truly blocked waiting for the
	// token lock, it returns context.DeadlineExceeded — deterministic proof of
	// real lock contention, not just "goroutine started".
	<-loginHolding
	provCtx, provCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer provCancel()
	provDone := make(chan error, 1)
	go func() {
		_, err := provider.Retrieve(provCtx)
		provDone <- err
	}()
	provErr := <-provDone
	if !errors.Is(provErr, context.DeadlineExceeded) {
		t.Fatalf("provider should be blocked on token lock (DeadlineExceeded), got: %v", provErr)
	}
	// Release login; it commits and releases the lock. A fresh provider call
	// then proceeds and must see the login-committed token (fast path returns
	// STS, no re-create of a different token lineage).
	close(proceedLogin)
	if err := <-loginDone; err != nil {
		t.Fatalf("login: %v", err)
	}
	if _, err := provider.Retrieve(context.Background()); err != nil {
		t.Fatalf("provider retrieve after login: %v", err)
	}
}

// barrierDeviceFlow writes the token, signals it is holding the lock, then
// waits for proceed before returning.
type barrierDeviceFlow struct {
	token   *sso.TokenCache
	cache   ssoCache
	holding chan struct{}
	proceed chan struct{}
}

func (b *barrierDeviceFlow) Login(_ context.Context) (*sso.TokenCache, error) {
	if err := b.cache.WriteToken(b.token); err != nil {
		return nil, err
	}
	close(b.holding)
	<-b.proceed
	return b.token, nil
}

// writeThenFailDeviceFlow writes the token (mimicking a real device flow that
// persists before returning) then returns an error, so the caller's rollback
// path is exercised with a real persisted token that must be restored.
type writeThenFailDeviceFlow struct {
	token *sso.TokenCache
	cache ssoCache
}

func (w *writeThenFailDeviceFlow) Login(_ context.Context) (*sso.TokenCache, error) {
	if err := w.cache.WriteToken(w.token); err != nil {
		return nil, err
	}
	return nil, errors.New("simulated downstream failure after token write")
}

func TestSSOProviderFirstThenLoginDoesNotOverwriteRotatedToken(t *testing.T) {
	dir := t.TempDir()
	cache, err := sso.NewFileCache(dir)
	if err != nil {
		t.Fatalf("cache: %v", err)
	}
	cfg := testConfigWithSession()
	p := cfg.Profiles["default"]
	p.Mode = config.AuthModeSSO
	p.SSOSessionName = "corp"
	p.AccountID = "acct-1"
	p.RoleName = "role-1"
	cfg.Profiles["default"] = p
	cfgPath := filepath.Join(dir, "config.json")
	if err := config.Save(cfg, cfgPath); err != nil {
		t.Fatalf("save config: %v", err)
	}

	// Seed a near-expiry token so the provider refreshes (calls CreateToken).
	nearExpiry := time.Now().Add(30 * time.Second).UTC().Format(time.RFC3339)
	seed := testTokenCache()
	seed.ExpiresAt = nearExpiry
	seed.AccessToken = "seed-near-expiry"
	if err := cache.WriteToken(seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Barrier inside CreateToken: provider holds the token lock while paused.
	inCreate := make(chan struct{})
	proceedCreate := make(chan struct{})
	oauth := &fakeSSOOAuth{
		createTokenBarrier: inCreate,
		proceed:            proceedCreate,
		tokenOverrides: &sso.CreateTokenResponse{
			AccessToken:  "rotated-by-provider",
			RefreshToken: "rotated-refresh",
			ExpiresIn:    7200,
		},
	}
	sts := &fakeSSOSTSExchanger{}
	provider := newRealSSOProvider(t, cache, cfgPath, "default", testSSOSession(), oauth, sts)

	provDone := make(chan error, 1)
	go func() {
		_, err := provider.Retrieve(context.Background())
		provDone <- err
	}()
	<-inCreate // provider is inside CreateToken, holding token lock

	// Start login with a short deadline; it must block on the token lock.
	// DeadlineExceeded proves real lock contention, not just goroutine start.
	loginToken := testTokenCache()
	loginToken.AccessToken = "login-token"
	adapter := realFileCacheAdapter(t, cache, cfg, cfgPath, loginToken)
	loginCtx, loginCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer loginCancel()
	loginDone := make(chan error, 1)
	go func() {
		_, err := adapter.runSSOLogin(loginCtx, ssoLoginOpts{SSOSession: "corp"})
		loginDone <- err
	}()
	loginErr := <-loginDone
	if !errors.Is(loginErr, context.DeadlineExceeded) {
		t.Fatalf("login should be blocked on token lock (DeadlineExceeded), got: %v", loginErr)
	}

	// Release the provider; it persists the rotated token, then releases the
	// lock. The rotated token is now the committed state.
	close(proceedCreate)
	if err := <-provDone; err != nil {
		t.Fatalf("provider: %v", err)
	}

	// Now run login again with a device flow that writes its token but then
	// fails (e.g. binding service error). Login's rollback must restore the
	// provider's rotated token, not leave the login token or delete it.
	failAdapter := realFileCacheAdapter(t, cache, cfg, cfgPath, loginToken)
	failAdapter.deviceFlowFn = func(_ config.SSOSession, _ bool) (ssoDeviceFlow, error) {
		return &writeThenFailDeviceFlow{token: loginToken, cache: cache}, nil
	}
	if _, err := failAdapter.runSSOLogin(context.Background(), ssoLoginOpts{SSOSession: "corp"}); err == nil {
		t.Fatal("login should fail due to downstream failure")
	}

	// The provider's rotated token must survive login's failed rollback.
	final, rerr := cache.ReadToken(testSSOSession().StartURL, "corp")
	if rerr != nil {
		t.Fatalf("read final token: %v", rerr)
	}
	if final.AccessToken != "rotated-by-provider" {
		t.Fatalf("final token = %q, want rotated-by-provider (login rollback must restore provider's committed token)", final.AccessToken)
	}
}

func TestSSOLogoutFirstThenProviderRequiresLogin(t *testing.T) {
	dir := t.TempDir()
	cache, err := sso.NewFileCache(dir)
	if err != nil {
		t.Fatalf("cache: %v", err)
	}
	cfg := testConfigWithSession()
	p := cfg.Profiles["default"]
	p.Mode = config.AuthModeSSO
	p.SSOSessionName = "corp"
	p.AccountID = "acct-1"
	p.RoleName = "role-1"
	cfg.Profiles["default"] = p
	cfgPath := filepath.Join(dir, "config.json")
	if err := config.Save(cfg, cfgPath); err != nil {
		t.Fatalf("save config: %v", err)
	}
	seedRealSSOCaches(t, cache, testSSOSession())

	adapter := realFileCacheAdapter(t, cache, cfg, cfgPath, testTokenCache())
	// Pause logout inside config.Update (which runs while holding the token +
	// STS locks) so the provider must wait on the token lock.
	logoutHolding := make(chan struct{})
	proceedLogout := make(chan struct{})
	adapter.cfgStore = &barrierConfigStore{
		inner:       &fakeConfigStore{cfg: cfg, path: cfgPath},
		enterUpdate: logoutHolding,
		release:     proceedLogout,
	}

	oauth := &fakeSSOOAuth{}
	sts := &fakeSSOSTSExchanger{}
	provider := newRealSSOProvider(t, cache, cfgPath, "default", testSSOSession(), oauth, sts)

	logoutDone := make(chan error, 1)
	go func() {
		_, err := adapter.runSSOLogout(context.Background(), ssoLogoutOpts{SSOSession: "corp"})
		logoutDone <- err
	}()
	<-logoutHolding

	// Start the provider with a short deadline; it must block on the token
	// lock held by logout. DeadlineExceeded proves real lock contention.
	provCtx, provCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer provCancel()
	provDone := make(chan error, 1)
	go func() {
		_, err := provider.Retrieve(provCtx)
		provDone <- err
	}()
	provErr := <-provDone
	if !errors.Is(provErr, context.DeadlineExceeded) {
		t.Fatalf("provider should be blocked on token lock (DeadlineExceeded), got: %v", provErr)
	}
	// Release logout; it deletes the token/STS and releases the lock. A fresh
	// provider call then proceeds and must require reauth (token/STS deleted,
	// not recreated).
	close(proceedLogout)
	if err := <-logoutDone; err != nil {
		t.Fatalf("logout: %v", err)
	}
	_, freshErr := provider.Retrieve(context.Background())
	var authErr *auth.Error
	if !errors.As(freshErr, &authErr) || authErr.Kind != auth.ReauthRequired {
		t.Fatalf("expected ReauthRequired after logout, got %v", freshErr)
	}
}

type pauseRevoker struct {
	holding chan struct{}
	proceed chan struct{}
}

func (p *pauseRevoker) RevokeToken(context.Context, *sso.RevokeTokenRequest) error {
	close(p.holding)
	<-p.proceed
	return nil
}

func TestSSOProviderFirstThenLogoutClearsEverything(t *testing.T) {
	dir := t.TempDir()
	cache, err := sso.NewFileCache(dir)
	if err != nil {
		t.Fatalf("cache: %v", err)
	}
	cfg := testConfigWithSession()
	p := cfg.Profiles["default"]
	p.Mode = config.AuthModeSSO
	p.SSOSessionName = "corp"
	p.AccountID = "acct-1"
	p.RoleName = "role-1"
	cfg.Profiles["default"] = p
	cfgPath := filepath.Join(dir, "config.json")
	if err := config.Save(cfg, cfgPath); err != nil {
		t.Fatalf("save config: %v", err)
	}
	seedRealSSOCaches(t, cache, testSSOSession())

	// Provider refreshes (near-expiry not needed; we just need it inside the
	// token lock). Use the CreateToken barrier.
	inCreate := make(chan struct{})
	proceedCreate := make(chan struct{})
	oauth := &fakeSSOOAuth{
		createTokenBarrier: inCreate,
		proceed:            proceedCreate,
	}
	sts := &fakeSSOSTSExchanger{}
	provider := newRealSSOProvider(t, cache, cfgPath, "default", testSSOSession(), oauth, sts)

	// Force refresh by making the seeded token near expiry.
	tok, _ := cache.ReadToken(testSSOSession().StartURL, "corp")
	tok.ExpiresAt = time.Now().Add(30 * time.Second).UTC().Format(time.RFC3339)
	if err := cache.WriteToken(tok); err != nil {
		t.Fatalf("reseed near-expiry: %v", err)
	}

	provDone := make(chan error, 1)
	go func() {
		_, err := provider.Retrieve(context.Background())
		provDone <- err
	}()
	<-inCreate

	adapter := realFileCacheAdapter(t, cache, cfg, cfgPath, testTokenCache())
	// Start logout with a short deadline; it must block on the token lock
	// held by the provider. DeadlineExceeded proves real lock contention.
	logoutCtx, logoutCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer logoutCancel()
	logoutDone := make(chan error, 1)
	go func() {
		_, err := adapter.runSSOLogout(logoutCtx, ssoLogoutOpts{SSOSession: "corp"})
		logoutDone <- err
	}()
	logoutErr := <-logoutDone
	if !errors.Is(logoutErr, context.DeadlineExceeded) {
		t.Fatalf("logout should be blocked on token lock (DeadlineExceeded), got: %v", logoutErr)
	}
	// Release the provider; it persists and releases the lock. Logout then
	// proceeds and must clear both token and STS.
	close(proceedCreate)
	if err := <-provDone; err != nil {
		t.Fatalf("provider: %v", err)
	}
	// Re-run logout with a fresh context now that the lock is free.
	if _, err := adapter.runSSOLogout(context.Background(), ssoLogoutOpts{SSOSession: "corp"}); err != nil {
		t.Fatalf("logout: %v", err)
	}

	// After logout, token AND STS must both be missing (not revived by provider).
	if _, rerr := cache.ReadToken(testSSOSession().StartURL, "corp"); !errors.Is(rerr, securestore.ErrMissing) {
		t.Fatalf("token should be missing after logout, got err=%v", rerr)
	}
	if _, rerr := cache.ReadSTS("corp", "acct-1", "role-1"); !errors.Is(rerr, securestore.ErrMissing) {
		t.Fatalf("sts should be missing after logout, got err=%v", rerr)
	}
}

func TestSSOLogoutReturnsThenProviderRequiresLoginReal(t *testing.T) {
	dir := t.TempDir()
	cache, err := sso.NewFileCache(dir)
	if err != nil {
		t.Fatalf("cache: %v", err)
	}
	cfg := testConfigWithSession()
	p := cfg.Profiles["default"]
	p.Mode = config.AuthModeSSO
	p.SSOSessionName = "corp"
	p.AccountID = "acct-1"
	p.RoleName = "role-1"
	cfg.Profiles["default"] = p
	cfgPath := filepath.Join(dir, "config.json")
	if err := config.Save(cfg, cfgPath); err != nil {
		t.Fatalf("save config: %v", err)
	}
	seedRealSSOCaches(t, cache, testSSOSession())

	adapter := realFileCacheAdapter(t, cache, cfg, cfgPath, testTokenCache())
	if _, err := adapter.runSSOLogout(context.Background(), ssoLogoutOpts{SSOSession: "corp"}); err != nil {
		t.Fatalf("logout: %v", err)
	}

	oauth := &fakeSSOOAuth{}
	sts := &fakeSSOSTSExchanger{}
	provider := newRealSSOProvider(t, cache, cfgPath, "default", testSSOSession(), oauth, sts)
	_, err = provider.Retrieve(context.Background())
	var authErr *auth.Error
	if !errors.As(err, &authErr) || authErr.Kind != auth.ReauthRequired {
		t.Fatalf("expected ReauthRequired after logout, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// 9. configure sso-session / configure sso consistency
// ---------------------------------------------------------------------------

func TestConfigureSSOSessionRejectsEmptyScopeInList(t *testing.T) {
	ctx, _ := newSSOTestContext(t, config.DefaultConfig())
	_, err := runConfigureSSOSession(ctx, []string{
		"--name", "corp",
		"--start-url", "https://example.volccloudidentity.com/userportal",
		"--region", "cn-beijing",
		"--registration-scopes", "cloudidentity:account:access,,offline_access",
	})
	if err == nil {
		t.Fatal("expected error for empty scope in list")
	}
	if !strings.Contains(err.Error(), "empty scope") {
		t.Fatalf("expected empty scope error, got: %v", err)
	}
}

func TestConfigureSSOSessionNormalizesNameAsMapKey(t *testing.T) {
	ctx, _ := newSSOTestContext(t, config.DefaultConfig())
	_, err := runConfigureSSOSession(ctx, []string{
		"--name", "  corp  ",
		"--start-url", "https://example.volccloudidentity.com/userportal",
		"--region", "cn-beijing",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, _, _ := config.Load()
	if _, ok := got.SSOSessions["corp"]; !ok {
		t.Fatalf("session should be keyed by trimmed name %q, got keys=%v", "corp", got.SSOSessions)
	}
	if _, ok := got.SSOSessions["  corp  "]; ok {
		t.Fatalf("session should not be keyed by untrimmed name")
	}
}

func TestConfigureSSOSessionPatchRequiresStartURLAndRegion(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.SSOSessions["corp"] = config.SSOSession{
		Name: "corp", StartURL: "https://old.example.com/userportal", Region: "cn-shanghai",
		RegistrationScopes: []string{sso.ScopeAccountAccess},
	}
	ctx, _ := newSSOTestContext(t, cfg)
	// Patch with only name; start-url and region are required on every call.
	_, err := runConfigureSSOSession(ctx, []string{"--name", "corp"})
	if err == nil {
		t.Fatal("expected error when start-url/region omitted on patch")
	}
}

func TestConfigureSSOReconfirmsSessionInsideLock(t *testing.T) {
	cfg := testConfigWithSession()
	cache := newFakeSSOCache()
	// driftStore returns the original config on the first Load (lock-free) and a
	// config with mutated scopes on the second Load (inside the token lock),
	// simulating a concurrent configure sso-session.
	mutated := cfg
	mutated.SSOSessions = make(map[string]config.SSOSession, len(cfg.SSOSessions))
	for k, v := range cfg.SSOSessions {
		mutated.SSOSessions[k] = v
	}
	mutated.SSOSessions["corp"] = config.SSOSession{
		Name: "corp", StartURL: testSSOSession().StartURL, Region: testSSOSession().Region,
		RegistrationScopes: []string{sso.ScopeAccountAccess},
	}
	driftStore := &driftConfigStore{first: cfg, second: mutated}
	df := &fakeSSODeviceFlow{token: testTokenCache()}
	bs := &fakeSSOBindingService{result: &sso.BindingResult{AccountID: "acct-1", RoleName: "role-1"}}
	adapter := &ssoAdapter{
		cache:    cache,
		cfgStore: driftStore,
		stdout:   &bytes.Buffer{},
		stderr:   &bytes.Buffer{},
		clock:    time.Now,
		deviceFlowFn: func(_ config.SSOSession, _ bool) (ssoDeviceFlow, error) {
			return df, nil
		},
		bindingFn: func(_ config.SSOSession) (ssoBindingService, error) {
			return bs, nil
		},
	}

	_, err := adapter.runConfigureSSO(context.Background(), ssoConfigureOpts{Profile: "default", SSOSession: "corp"})
	if err == nil {
		t.Fatal("expected error when session changes during login")
	}
}

// ---------------------------------------------------------------------------
// 10. Full Run dispatch + frozen option pre-rejection
// ---------------------------------------------------------------------------

func TestConfigureSSOFullRunRejectsFrozenOptionsBeforeFactory(t *testing.T) {
	cfg := testConfigWithSession()
	ctx, _ := newSSOTestContext(t, cfg)
	ctx.GlobalSecretsFile = "/some/path"
	called := false
	_, err := runConfigureSSOWithFactory(ctx, []string{"--profile", "default", "--sso-session", "corp"}, func(_ *Context) (*ssoAdapter, error) {
		called = true
		return nil, nil
	})
	if err == nil {
		t.Fatal("expected error for secrets-file")
	}
	if called {
		t.Fatal("factory should not be called when frozen options are set")
	}
}

func TestSSODispatchFullRunJSONOutput(t *testing.T) {
	cfg := testConfigWithSession()
	p := cfg.Profiles["default"]
	p.Mode = config.AuthModeSSO
	p.SSOSessionName = "corp"
	p.AccountID = "acct-1"
	p.RoleName = "role-1"
	cfg.Profiles["default"] = p
	ctx, _ := newSSOTestContext(t, cfg)
	cache := newFakeSSOCache()
	factory := func(_ *Context) (*ssoAdapter, error) {
		return newSSOAdapterForTest(cache, &fakeConfigStore{cfg: cfg, path: ""}, &fakeSSODeviceFlow{token: testTokenCache()}, nil, nil), nil
	}
	out, err := runSSOLoginWithFactory(ctx, []string{"--sso-session", "corp"}, factory)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	b, _ := json.Marshal(out)
	if !json.Valid(b) {
		t.Fatalf("result is not valid JSON: %s", b)
	}
	if ctx.FormatOverride != "json" {
		t.Fatalf("format override = %q, want json", ctx.FormatOverride)
	}
}

func TestSSOFactoryNilAndReturnsNilSafe(t *testing.T) {
	ctx, _ := newSSOTestContext(t, testConfigWithSession())
	if _, err := runSSOLoginWithFactory(ctx, []string{"--sso-session", "corp"}, nil); err == nil {
		t.Fatal("expected error for nil factory")
	}
	if _, err := runSSOLoginWithFactory(ctx, []string{"--sso-session", "corp"}, func(_ *Context) (*ssoAdapter, error) {
		return nil, nil
	}); err == nil {
		t.Fatal("expected error for factory returning nil")
	}
	if _, err := runSSOLogoutWithFactory(ctx, []string{"--sso-session", "corp"}, nil); err == nil {
		t.Fatal("expected error for nil factory")
	}
	if _, err := runConfigureSSOWithFactory(ctx, []string{"--profile", "default", "--sso-session", "corp"}, nil); err == nil {
		t.Fatal("expected error for nil factory")
	}
}

// ensure unused imports are referenced
var (
	_ = io.EOF
	_ = atomic.AddInt32
)
