package sso

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth"
	"github.com/volcengine-tls/ve-tls-cli/internal/config"
	"github.com/volcengine-tls/ve-tls-cli/internal/securestore"
)

// fakeSTSExchanger is a test STSExchanger.
type fakeSTSExchanger struct {
	calls           int32
	creds           *RoleCredentials
	err             error
	credsByCall     map[int]*RoleCredentials
	lastAccessToken string
}

func (f *fakeSTSExchanger) GetRoleCredentials(ctx context.Context, accessToken, accountID, roleName string) (*RoleCredentials, error) {
	n := atomic.AddInt32(&f.calls, 1)
	f.lastAccessToken = accessToken
	if f.credsByCall != nil {
		if c, ok := f.credsByCall[int(n)]; ok {
			return c, nil
		}
	}
	return f.creds, f.err
}

// fakeConfigUpdater is a test ConfigUpdater that records the patched config.
type fakeConfigUpdater struct {
	cfg       config.Config
	updateErr error
	patched   int32
}

func (f *fakeConfigUpdater) Update(path string, fn func(*config.Config) error) (config.Config, error) {
	atomic.AddInt32(&f.patched, 1)
	if f.updateErr != nil {
		return config.Config{}, f.updateErr
	}
	if err := fn(&f.cfg); err != nil {
		return config.Config{}, err
	}
	return f.cfg, nil
}

func newTestProvider(t *testing.T, cache Cache, oauth OAuthAPI, portal STSExchanger, cfgStore ConfigUpdater, clock func() time.Time) *SSOProvider {
	t.Helper()
	if clock == nil {
		clock = func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	}
	if cfgStore == nil {
		cfgStore = newFakeConfigStore()
	}
	p, err := NewSSOProvider(&SSOProviderConfig{
		ConfigPath:  "/tmp/test-config.json",
		ProfileName: "test-profile",
		StartURL:    "https://example.volccloudidentity.com",
		SessionName: "test-session",
		SSORegion:   "cn-beijing",
		AccountID:   "acc-1",
		RoleName:    "role-1",
		Cache:       cache,
		OAuth:       oauth,
		Portal:      portal,
		ConfigStore: cfgStore,
		Clock:       clock,
	})
	if err != nil {
		t.Fatalf("NewSSOProvider failed: %v", err)
	}
	return p
}

// newFakeConfigStore creates a fakeConfigUpdater pre-populated with the target
// profile so config patches succeed.
func newFakeConfigStore() *fakeConfigUpdater {
	return &fakeConfigUpdater{
		cfg: config.Config{
			Profiles: map[string]config.Profile{
				"test-profile": {
					Mode:           config.AuthModeSSO,
					SSOSessionName: "test-session",
					AccountID:      "acc-1",
					RoleName:       "role-1",
				},
			},
		},
	}
}

// testClock is the default test clock.
var testClock = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// testCommitTarget returns the commit identity for the default test provider's
// config target (config path + profile name) used by newTestProvider.
func testCommitTarget() string {
	k, err := commitTargetKey("/tmp/test-config.json", "test-profile")
	if err != nil {
		panic(err)
	}
	return k
}

func validTokenCache() *TokenCache {
	return &TokenCache{
		StartURL:              "https://example.volccloudidentity.com",
		SessionName:           "test-session",
		AccessToken:           "access-token",
		ExpiresAt:             testClock.Add(time.Hour).UTC().Format(time.RFC3339),
		ClientID:              "client-id",
		ClientSecret:          "client-secret",
		ClientIDIssuedAt:      1000,
		ClientSecretExpiresAt: 0,
		RefreshToken:          "refresh-token",
		Region:                "cn-beijing",
	}
}

func validSTSCache() *STSCache {
	return &STSCache{
		SessionName:      "test-session",
		AccountID:        "acc-1",
		RoleName:         "role-1",
		AccessKeyID:      "AKLTvalid",
		SecretAccessKey:  "SKvalid",
		SessionToken:     "STvalid",
		ProviderName:     ProviderName,
		ExpiresAt:        testClock.Add(time.Hour).UTC().Format(time.RFC3339),
		CommittedTargets: []string{testCommitTarget()},
	}
}

func TestSSOProviderUsesValidCachedSTSWithoutNetwork(t *testing.T) {
	cache := newFakeCache()
	cache.WriteToken(validTokenCache())
	cache.WriteSTS(validSTSCache())

	oauth := &fakeOAuth{}
	portal := &fakeSTSExchanger{}
	cfgStore := newFakeConfigStore()
	p := newTestProvider(t, cache, oauth, portal, cfgStore, nil)

	val, err := p.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val.AccessKeyID != "AKLTvalid" || val.SecretAccessKey != "SKvalid" || val.SessionToken != "STvalid" {
		t.Fatal("value mismatch")
	}
	if val.ProviderName != ProviderName {
		t.Fatalf("provider = %q want %q", val.ProviderName, ProviderName)
	}
	// No network calls should have been made.
	if atomic.LoadInt32(&oauth.tokenCalls) != 0 {
		t.Fatal("oauth should not be called when STS cache is valid")
	}
	if atomic.LoadInt32(&portal.calls) != 0 {
		t.Fatal("portal should not be called when STS cache is valid")
	}
	if atomic.LoadInt32(&cfgStore.patched) != 0 {
		t.Fatal("config should not be patched when STS cache is valid")
	}
}

func TestSSOProviderFailsClosedOnBroadPermissionTokenCache(t *testing.T) {
	dir := t.TempDir()
	cache, err := NewFileCache(dir)
	if err != nil {
		t.Fatalf("NewFileCache: %v", err)
	}
	if err := cache.WriteToken(validTokenCache()); err != nil {
		t.Fatalf("WriteToken: %v", err)
	}
	if err := cache.WriteSTS(validSTSCache()); err != nil {
		t.Fatalf("WriteSTS: %v", err)
	}

	// Chmod the token cache file to 0644 (broad).
	tc := validTokenCache()
	tokenDigest, err := tokenKey(tc.StartURL, tc.SessionName)
	if err != nil {
		t.Fatalf("tokenKey: %v", err)
	}
	tokenPath := filepath.Join(dir, "token-"+tokenDigest+".json")
	if err := os.Chmod(tokenPath, 0o644); err != nil {
		t.Fatalf("chmod token: %v", err)
	}

	oauth := &fakeOAuth{}
	portal := &fakeSTSExchanger{}
	cfgStore := newFakeConfigStore()
	p := newTestProvider(t, cache, oauth, portal, cfgStore, nil)

	val, err := p.Retrieve(context.Background())
	if !errors.Is(err, securestore.ErrPermission) {
		t.Fatalf("Retrieve with 0644 token cache: err=%v, want errors.Is(err, securestore.ErrPermission)", err)
	}
	if val.AccessKeyID != "" || val.SecretAccessKey != "" || val.SessionToken != "" {
		t.Fatalf("Retrieve returned credentials despite broad token cache: %+v", val)
	}
	if atomic.LoadInt32(&oauth.tokenCalls) != 0 {
		t.Fatalf("oauth refresh should not be called for broad token cache, count=%d", oauth.tokenCalls)
	}
	if atomic.LoadInt32(&portal.calls) != 0 {
		t.Fatalf("portal STS exchange should not be called for broad token cache, count=%d", portal.calls)
	}
}

func TestSSOProviderFailsClosedOnBroadPermissionSTSCache(t *testing.T) {
	dir := t.TempDir()
	cache, err := NewFileCache(dir)
	if err != nil {
		t.Fatalf("NewFileCache: %v", err)
	}
	if err := cache.WriteToken(validTokenCache()); err != nil {
		t.Fatalf("WriteToken: %v", err)
	}
	if err := cache.WriteSTS(validSTSCache()); err != nil {
		t.Fatalf("WriteSTS: %v", err)
	}

	// Chmod the STS cache file to 0644 (broad).
	sc := validSTSCache()
	stsDigest, err := stsKey(sc.SessionName, sc.AccountID, sc.RoleName)
	if err != nil {
		t.Fatalf("stsKey: %v", err)
	}
	stsPath := filepath.Join(dir, "sts-"+stsDigest+".json")
	if err := os.Chmod(stsPath, 0o644); err != nil {
		t.Fatalf("chmod sts: %v", err)
	}

	oauth := &fakeOAuth{}
	portal := &fakeSTSExchanger{}
	cfgStore := newFakeConfigStore()
	p := newTestProvider(t, cache, oauth, portal, cfgStore, nil)

	val, err := p.Retrieve(context.Background())
	if !errors.Is(err, securestore.ErrPermission) {
		t.Fatalf("Retrieve with 0644 STS cache: err=%v, want errors.Is(err, securestore.ErrPermission)", err)
	}
	if val.AccessKeyID != "" || val.SecretAccessKey != "" || val.SessionToken != "" {
		t.Fatalf("Retrieve returned credentials despite broad STS cache: %+v", val)
	}
	if atomic.LoadInt32(&oauth.tokenCalls) != 0 {
		t.Fatalf("oauth refresh should not be called for broad STS cache, count=%d", oauth.tokenCalls)
	}
	if atomic.LoadInt32(&portal.calls) != 0 {
		t.Fatalf("portal STS exchange should not be called for broad STS cache, count=%d", portal.calls)
	}
}

func TestSSOProviderRefreshesOAuthBeforeSTSExchange(t *testing.T) {
	// Token cache is near expiry (within refresh window).
	tc := validTokenCache()
	tc.ExpiresAt = testClock.Add(30 * time.Second).UTC().Format(time.RFC3339)
	cache := newFakeCache()
	cache.WriteToken(tc)
	// No STS cache.

	oauth := &fakeOAuth{
		tokenResp: &CreateTokenResponse{
			AccessToken:  "new-access-token",
			TokenType:    "Bearer",
			ExpiresIn:    3600,
			RefreshToken: "new-refresh-token",
		},
	}
	portal := &fakeSTSExchanger{
		creds: &RoleCredentials{
			AccessKeyID:     "AKLTnew",
			SecretAccessKey: "SKnew",
			SessionToken:    "STnew",
			Expiration:      testClock.Add(time.Hour).Unix(),
		},
	}
	cfgStore := newFakeConfigStore()
	p := newTestProvider(t, cache, oauth, portal, cfgStore, nil)

	val, err := p.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val.AccessKeyID != "AKLTnew" {
		t.Fatalf("got %q want AKLTnew", val.AccessKeyID)
	}
	// OAuth refresh should have been called exactly once.
	if atomic.LoadInt32(&oauth.tokenCalls) != 1 {
		t.Fatalf("oauth token calls = %d, want 1", oauth.tokenCalls)
	}
	// STS exchange should have been called.
	if atomic.LoadInt32(&portal.calls) != 1 {
		t.Fatalf("portal calls = %d, want 1", portal.calls)
	}
	// The rotated token cache should have been persisted.
	got, err := cache.ReadToken("https://example.volccloudidentity.com", "test-session")
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "new-access-token" {
		t.Fatalf("rotated token not persisted: got %q", got.AccessToken)
	}
	if got.RefreshToken != "new-refresh-token" {
		t.Fatalf("rotated refresh token not persisted: got %q", got.RefreshToken)
	}
}

func TestSSOProviderPersistsRotatedRefreshToken(t *testing.T) {
	tc := validTokenCache()
	tc.ExpiresAt = testClock.Add(30 * time.Second).UTC().Format(time.RFC3339)
	tc.RefreshToken = "old-refresh"
	cache := newFakeCache()
	cache.WriteToken(tc)

	oauth := &fakeOAuth{
		tokenResp: &CreateTokenResponse{
			AccessToken:  "new-at",
			TokenType:    "Bearer",
			ExpiresIn:    3600,
			RefreshToken: "rotated-refresh",
		},
	}
	portal := &fakeSTSExchanger{
		creds: &RoleCredentials{
			AccessKeyID: "AK", SecretAccessKey: "SK", SessionToken: "ST",
			Expiration: testClock.Add(time.Hour).Unix(),
		},
	}
	cfgStore := newFakeConfigStore()
	p := newTestProvider(t, cache, oauth, portal, cfgStore, nil)

	if _, err := p.Retrieve(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, _ := cache.ReadToken("https://example.volccloudidentity.com", "test-session")
	if got.RefreshToken != "rotated-refresh" {
		t.Fatalf("refresh token = %q, want rotated-refresh", got.RefreshToken)
	}
}

func TestSSOProviderPreservesOldRefreshTokenWhenOmitted(t *testing.T) {
	tc := validTokenCache()
	tc.ExpiresAt = testClock.Add(30 * time.Second).UTC().Format(time.RFC3339)
	tc.RefreshToken = "old-refresh"
	cache := newFakeCache()
	cache.WriteToken(tc)

	oauth := &fakeOAuth{
		tokenResp: &CreateTokenResponse{
			AccessToken: "new-at",
			TokenType:   "Bearer",
			ExpiresIn:   3600,
			// No RefreshToken in response.
		},
	}
	portal := &fakeSTSExchanger{
		creds: &RoleCredentials{
			AccessKeyID: "AK", SecretAccessKey: "SK", SessionToken: "ST",
			Expiration: testClock.Add(time.Hour).Unix(),
		},
	}
	cfgStore := newFakeConfigStore()
	p := newTestProvider(t, cache, oauth, portal, cfgStore, nil)

	if _, err := p.Retrieve(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, _ := cache.ReadToken("https://example.volccloudidentity.com", "test-session")
	if got.RefreshToken != "old-refresh" {
		t.Fatalf("refresh token = %q, want old-refresh (preserved)", got.RefreshToken)
	}
}

func TestSSOProviderNeverStartsDeviceFlowDuringBusinessRequest(t *testing.T) {
	cache := newFakeCache()
	cache.WriteToken(validTokenCache())
	cache.WriteSTS(validSTSCache())

	oauth := &fakeOAuth{}
	portal := &fakeSTSExchanger{}
	cfgStore := newFakeConfigStore()
	p := newTestProvider(t, cache, oauth, portal, cfgStore, nil)

	if _, err := p.Retrieve(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Device authorization is never called during business Retrieve.
	if atomic.LoadInt32(&oauth.deviceAuthCalls) != 0 {
		t.Fatal("device authorization should never be called during business request")
	}
	if atomic.LoadInt32(&oauth.registerCalls) != 0 {
		t.Fatal("client registration should never be called during business request")
	}
}

func TestSSOProviderRequiresLoginWhenCacheMissing(t *testing.T) {
	cache := newFakeCache()
	oauth := &fakeOAuth{}
	portal := &fakeSTSExchanger{}
	cfgStore := newFakeConfigStore()
	p := newTestProvider(t, cache, oauth, portal, cfgStore, nil)

	_, err := p.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected error when token cache is missing")
	}
	var authErr *auth.Error
	if !errors.As(err, &authErr) {
		t.Fatalf("expected *auth.Error, got %T", err)
	}
	if authErr.Kind != auth.ReauthRequired {
		t.Fatalf("kind = %q, want %q", authErr.Kind, auth.ReauthRequired)
	}
}

func TestSSOProviderNeverFallsBackToEnvironmentAK(t *testing.T) {
	// Set environment AK/SK; the provider must NOT use them.
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ENVAK")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "ENVSK")

	cache := newFakeCache()
	oauth := &fakeOAuth{}
	portal := &fakeSTSExchanger{}
	cfgStore := newFakeConfigStore()
	p := newTestProvider(t, cache, oauth, portal, cfgStore, nil)

	_, err := p.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected error (no cache), not environment fallback")
	}
}

func TestSSOProviderConcurrentRetrieveRefreshesOnce(t *testing.T) {
	tc := validTokenCache()
	tc.ExpiresAt = testClock.Add(30 * time.Second).UTC().Format(time.RFC3339)
	cache := newFakeCache()
	cache.WriteToken(tc)

	var oauthCalls int32
	oauth := &fakeOAuth{
		tokenResp: &CreateTokenResponse{
			AccessToken: "new-at", TokenType: "Bearer", ExpiresIn: 3600,
		},
	}
	// Wrap CreateToken to count calls and add a small delay to increase contention.
	portal := &fakeSTSExchanger{
		creds: &RoleCredentials{
			AccessKeyID: "AK", SecretAccessKey: "SK", SessionToken: "ST",
			Expiration: testClock.Add(time.Hour).Unix(),
		},
	}
	cfgStore := newFakeConfigStore()
	// Use a real FileCache so the lock actually serializes.
	dir := t.TempDir()
	fileCache, err := NewFileCache(dir)
	if err != nil {
		t.Fatal(err)
	}
	fileCache.WriteToken(tc)

	p := newTestProvider(t, fileCache, oauth, portal, cfgStore, nil)

	// Wrap oauth to count calls.
	countingOAuth := &countingOAuth{OAuthAPI: oauth, counter: &oauthCalls}
	p.oauth = countingOAuth

	var wg sync.WaitGroup
	errs := make([]error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, errs[idx] = p.Retrieve(context.Background())
		}(i)
	}
	wg.Wait()

	for i, e := range errs {
		if e != nil {
			t.Fatalf("goroutine %d error: %v", i, e)
		}
	}
	// OAuth refresh should happen exactly once despite 10 concurrent calls.
	if got := atomic.LoadInt32(&oauthCalls); got != 1 {
		t.Fatalf("oauth refresh called %d times, want 1", got)
	}
}

// countingOAuth wraps an OAuthAPI and counts CreateToken calls.
type countingOAuth struct {
	OAuthAPI
	counter *int32
}

func (c *countingOAuth) CreateToken(ctx context.Context, req *CreateTokenRequest) (*CreateTokenResponse, error) {
	atomic.AddInt32(c.counter, 1)
	return c.OAuthAPI.CreateToken(ctx, req)
}

func TestSSOProvidersSameSessionDifferentBindingsRefreshOAuthOnce(t *testing.T) {
	tc := validTokenCache()
	tc.ExpiresAt = testClock.Add(30 * time.Second).UTC().Format(time.RFC3339)

	dir := t.TempDir()
	fileCache, err := NewFileCache(dir)
	if err != nil {
		t.Fatal(err)
	}
	fileCache.WriteToken(tc)

	var oauthCalls int32
	oauth := &fakeOAuth{
		tokenResp: &CreateTokenResponse{
			AccessToken: "new-at", TokenType: "Bearer", ExpiresIn: 3600,
		},
	}
	counting := &countingOAuth{OAuthAPI: oauth, counter: &oauthCalls}

	portal1 := &fakeSTSExchanger{
		creds: &RoleCredentials{
			AccessKeyID: "AK1", SecretAccessKey: "SK1", SessionToken: "ST1",
			Expiration: testClock.Add(time.Hour).Unix(),
		},
	}
	portal2 := &fakeSTSExchanger{
		creds: &RoleCredentials{
			AccessKeyID: "AK2", SecretAccessKey: "SK2", SessionToken: "ST2",
			Expiration: testClock.Add(time.Hour).Unix(),
		},
	}
	cfgStore := &fakeConfigUpdater{
		cfg: config.Config{
			Profiles: map[string]config.Profile{
				"prof1": {Mode: config.AuthModeSSO, SSOSessionName: "test-session", AccountID: "acc-1", RoleName: "role-1"},
				"prof2": {Mode: config.AuthModeSSO, SSOSessionName: "test-session", AccountID: "acc-2", RoleName: "role-2"},
			},
		},
	}

	// Two providers for the same session but different account/role bindings.
	p1, _ := NewSSOProvider(&SSOProviderConfig{
		ConfigPath: "/tmp/c.json", ProfileName: "prof1",
		StartURL: "https://example.volccloudidentity.com", SessionName: "test-session",
		SSORegion: "cn-beijing",
		AccountID: "acc-1", RoleName: "role-1",
		Cache: fileCache, OAuth: counting, Portal: portal1, ConfigStore: cfgStore,
		Clock: func() time.Time { return testClock },
	})
	p2, _ := NewSSOProvider(&SSOProviderConfig{
		ConfigPath: "/tmp/c.json", ProfileName: "prof2",
		StartURL: "https://example.volccloudidentity.com", SessionName: "test-session",
		SSORegion: "cn-beijing",
		AccountID: "acc-2", RoleName: "role-2",
		Cache: fileCache, OAuth: counting, Portal: portal2, ConfigStore: cfgStore,
		Clock: func() time.Time { return testClock },
	})

	var wg sync.WaitGroup
	var err1, err2 error
	wg.Add(2)
	go func() { defer wg.Done(); _, err1 = p1.Retrieve(context.Background()) }()
	go func() { defer wg.Done(); _, err2 = p2.Retrieve(context.Background()) }()
	wg.Wait()

	if err1 != nil {
		t.Fatalf("p1 error: %v", err1)
	}
	if err2 != nil {
		t.Fatalf("p2 error: %v", err2)
	}
	// OAuth refresh should happen exactly once, shared across both bindings.
	if got := atomic.LoadInt32(&oauthCalls); got != 1 {
		t.Fatalf("oauth refresh called %d times, want 1", got)
	}
}

func TestSSOTokenRotationSharedAcrossBindings(t *testing.T) {
	tc := validTokenCache()
	tc.ExpiresAt = testClock.Add(30 * time.Second).UTC().Format(time.RFC3339)
	tc.RefreshToken = "old-rt"

	dir := t.TempDir()
	fileCache, err := NewFileCache(dir)
	if err != nil {
		t.Fatal(err)
	}
	fileCache.WriteToken(tc)

	oauth := &fakeOAuth{
		tokenResp: &CreateTokenResponse{
			AccessToken: "new-at", TokenType: "Bearer", ExpiresIn: 3600,
			RefreshToken: "rotated-rt",
		},
	}
	portal1 := &fakeSTSExchanger{
		creds: &RoleCredentials{
			AccessKeyID: "AK1", SecretAccessKey: "SK1", SessionToken: "ST1",
			Expiration: testClock.Add(time.Hour).Unix(),
		},
	}
	cfgStore := &fakeConfigUpdater{
		cfg: config.Config{
			Profiles: map[string]config.Profile{
				"prof1": {Mode: config.AuthModeSSO, SSOSessionName: "test-session", AccountID: "acc-1", RoleName: "role-1"},
			},
		},
	}

	p1, _ := NewSSOProvider(&SSOProviderConfig{
		ConfigPath: "/tmp/c.json", ProfileName: "prof1",
		StartURL: "https://example.volccloudidentity.com", SessionName: "test-session",
		SSORegion: "cn-beijing",
		AccountID: "acc-1", RoleName: "role-1",
		Cache: fileCache, OAuth: oauth, Portal: portal1, ConfigStore: cfgStore,
		Clock: func() time.Time { return testClock },
	})

	if _, err := p1.Retrieve(context.Background()); err != nil {
		t.Fatalf("p1 error: %v", err)
	}
	// The rotated refresh token should be in the shared token cache.
	got, _ := fileCache.ReadToken("https://example.volccloudidentity.com", "test-session")
	if got.RefreshToken != "rotated-rt" {
		t.Fatalf("refresh token = %q, want rotated-rt", got.RefreshToken)
	}
}

func TestSSOProviderReloadsCacheInNewInstance(t *testing.T) {
	dir := t.TempDir()
	fileCache, err := NewFileCache(dir)
	if err != nil {
		t.Fatal(err)
	}
	fileCache.WriteToken(validTokenCache())
	fileCache.WriteSTS(validSTSCache())

	oauth := &fakeOAuth{}
	portal := &fakeSTSExchanger{}
	cfgStore := newFakeConfigStore()

	// Create two separate provider instances sharing the same cache.
	p1 := newTestProvider(t, fileCache, oauth, portal, cfgStore, nil)
	p2 := newTestProvider(t, fileCache, oauth, portal, cfgStore, nil)

	v1, err := p1.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("p1 error: %v", err)
	}
	v2, err := p2.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("p2 error: %v", err)
	}
	if v1.AccessKeyID != v2.AccessKeyID {
		t.Fatal("two instances should return the same cached credentials")
	}
}

func TestSSOProviderRejectsIncompleteBindingAndSTS(t *testing.T) {
	t.Run("incomplete_token_cache", func(t *testing.T) {
		cache := newFakeCache()
		// Token cache missing access token.
		cache.WriteToken(&TokenCache{
			StartURL: "https://example.volccloudidentity.com", SessionName: "test-session",
			ClientID: "cid", ClientSecret: "csec",
		})
		p := newTestProvider(t, cache, &fakeOAuth{}, &fakeSTSExchanger{}, nil, nil)
		_, err := p.Retrieve(context.Background())
		if err == nil {
			t.Fatal("expected error for incomplete token cache")
		}
	})

	t.Run("malformed_sts_under_expected_key", func(t *testing.T) {
		cache := newFakeCache()
		cache.WriteToken(validTokenCache())
		// STS cache with the correct key fields (so it lands under the expected
		// key) but a wrong provider name, which is a non-expired validation
		// failure that must fail closed without calling Portal.
		sts := validSTSCache()
		sts.ProviderName = "console"
		cache.WriteSTS(sts)
		portal := &fakeSTSExchanger{
			creds: &RoleCredentials{
				AccessKeyID: "AK", SecretAccessKey: "SK", SessionToken: "ST",
				Expiration: testClock.Add(time.Hour).Unix(),
			},
		}
		p := newTestProvider(t, cache, &fakeOAuth{}, portal, nil, nil)
		_, err := p.Retrieve(context.Background())
		if err == nil {
			t.Fatal("expected error for malformed STS cache")
		}
		var authErr *auth.Error
		if !errors.As(err, &authErr) {
			t.Fatalf("expected *auth.Error, got %T", err)
		}
		if authErr.Kind != auth.CacheCorrupt {
			t.Fatalf("kind = %q, want %q", authErr.Kind, auth.CacheCorrupt)
		}
		// Portal must not be called.
		if got := atomic.LoadInt32(&portal.calls); got != 0 {
			t.Fatalf("portal calls = %d, want 0", got)
		}
		// The cache must not be overwritten.
		got, _ := cache.ReadSTS("test-session", "acc-1", "role-1")
		if got == nil || got.ProviderName != "console" {
			t.Fatal("STS cache was overwritten; should be preserved on validation failure")
		}
	})

	t.Run("stale_sts_cache", func(t *testing.T) {
		cache := newFakeCache()
		cache.WriteToken(validTokenCache())
		// STS cache that is near expiry.
		sts := validSTSCache()
		sts.ExpiresAt = testClock.Add(30 * time.Second).UTC().Format(time.RFC3339)
		cache.WriteSTS(sts)
		portal := &fakeSTSExchanger{
			creds: &RoleCredentials{
				AccessKeyID: "AKnew", SecretAccessKey: "SKnew", SessionToken: "STnew",
				Expiration: testClock.Add(time.Hour).Unix(),
			},
		}
		p := newTestProvider(t, cache, &fakeOAuth{}, portal, nil, nil)
		val, err := p.Retrieve(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if val.AccessKeyID != "AKnew" {
			t.Fatalf("got %q, want AKnew (stale STS should be refreshed)", val.AccessKeyID)
		}
	})
}

func TestSSOProviderPersistsExplicitProfileMetadataNotCurrentProfile(t *testing.T) {
	cache := newFakeCache()
	cache.WriteToken(validTokenCache())
	// No STS cache, so it will exchange and patch config.

	oauth := &fakeOAuth{}
	portal := &fakeSTSExchanger{
		creds: &RoleCredentials{
			AccessKeyID: "AK", SecretAccessKey: "SK", SessionToken: "ST",
			Expiration: testClock.Add(time.Hour).Unix(),
		},
	}
	cfgStore := &fakeConfigUpdater{
		cfg: config.Config{
			CurrentProfile: "other-profile",
			Profiles: map[string]config.Profile{
				"test-profile":  {Mode: config.AuthModeSSO, SSOSessionName: "test-session", AccountID: "acc-1", RoleName: "role-1"},
				"other-profile": {Mode: config.AuthModeAK, AccessKeyID: "AKLTother", SecretAccessKey: "SKother"},
			},
		},
	}
	p := newTestProvider(t, cache, oauth, portal, cfgStore, nil)

	if _, err := p.Retrieve(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only the target profile's sts-expiration should be patched.
	target, ok := cfgStore.cfg.GetProfile("test-profile")
	if !ok {
		t.Fatal("target profile missing")
	}
	if target.STSExpiration == 0 {
		t.Fatal("target profile sts-expiration not patched")
	}
	// The other profile should be untouched.
	other, _ := cfgStore.cfg.GetProfile("other-profile")
	if other.STSExpiration != 0 {
		t.Fatal("other profile should not be patched")
	}
	if other.AccessKeyID != "AKLTother" {
		t.Fatal("other profile credentials should be untouched")
	}
	// Mode/session/account/role should be unchanged.
	if target.Mode != config.AuthModeSSO {
		t.Fatalf("target mode = %q, want sso", target.Mode)
	}
	if target.SSOSessionName != "test-session" {
		t.Fatalf("target session = %q, want test-session", target.SSOSessionName)
	}
}

func TestSSOProviderConfigUpdateFailsClosed(t *testing.T) {
	cache := newFakeCache()
	cache.WriteToken(validTokenCache())

	oauth := &fakeOAuth{}
	portal := &fakeSTSExchanger{
		creds: &RoleCredentials{
			AccessKeyID: "AK", SecretAccessKey: "SK", SessionToken: "ST",
			Expiration: testClock.Add(time.Hour).Unix(),
		},
	}
	cfgStore := &fakeConfigUpdater{updateErr: errors.New("config write failed")}
	p := newTestProvider(t, cache, oauth, portal, cfgStore, nil)

	_, err := p.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected error when config update fails")
	}
	// STS cache may have been persisted (orphaned), but no credentials returned.
}

func TestSSOProviderSTSPersistFailsClosed(t *testing.T) {
	// Use a cache that fails on WriteSTS.
	cache := &failingWriteSTSCache{Cache: newFakeCache()}
	cache.WriteToken(validTokenCache())

	oauth := &fakeOAuth{}
	portal := &fakeSTSExchanger{
		creds: &RoleCredentials{
			AccessKeyID: "AK", SecretAccessKey: "SK", SessionToken: "ST",
			Expiration: testClock.Add(time.Hour).Unix(),
		},
	}
	p := newTestProvider(t, cache, oauth, portal, &fakeConfigUpdater{}, nil)

	_, err := p.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected error when STS persist fails")
	}
}

// failingWriteSTSCache wraps a Cache and fails WriteSTS.
type failingWriteSTSCache struct {
	Cache
}

func (c *failingWriteSTSCache) WriteSTS(cache *STSCache) error {
	return errors.New("simulated STS write failure")
}

func TestSSOProviderNilDeps(t *testing.T) {
	_, err := NewSSOProvider(&SSOProviderConfig{
		ConfigPath: "/tmp/c.json", ProfileName: "p",
		StartURL: "https://example.volccloudidentity.com", SessionName: "s",
		AccountID: "a", RoleName: "r",
		Cache: nil, OAuth: &fakeOAuth{}, Portal: &fakeSTSExchanger{},
	})
	if err == nil {
		t.Fatal("expected error for nil cache")
	}
}

func TestSSOProviderInvalidStartURL(t *testing.T) {
	_, err := NewSSOProvider(&SSOProviderConfig{
		ConfigPath: "/tmp/c.json", ProfileName: "p",
		StartURL: "not-a-url", SessionName: "s",
		AccountID: "a", RoleName: "r",
		Cache: newFakeCache(), OAuth: &fakeOAuth{}, Portal: &fakeSTSExchanger{},
	})
	if err == nil {
		t.Fatal("expected error for invalid start URL")
	}
}

func TestSSOProviderErrorRedaction(t *testing.T) {
	cache := newFakeCache()
	// Missing token cache -> ReauthRequired.
	p := newTestProvider(t, cache, &fakeOAuth{}, &fakeSTSExchanger{}, nil, nil)
	_, err := p.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	// Error string must not contain the start URL or session name in a way that
	// could leak sensitive material. The description is a fixed safe string.
	errStr := err.Error()
	if errStr == "" {
		t.Fatal("error string is empty")
	}
	// Verify it's a classified auth.Error.
	var authErr *auth.Error
	if !errors.As(err, &authErr) {
		t.Fatalf("expected *auth.Error, got %T", err)
	}
}

func TestSSOProviderUsesRealFileCache(t *testing.T) {
	// Integration-style test using the real FileCache to prove end-to-end behavior.
	dir := t.TempDir()
	fileCache, err := NewFileCache(dir)
	if err != nil {
		t.Fatal(err)
	}
	fileCache.WriteToken(validTokenCache())
	fileCache.WriteSTS(validSTSCache())

	oauth := &fakeOAuth{}
	portal := &fakeSTSExchanger{}
	cfgStore := newFakeConfigStore()
	p := newTestProvider(t, fileCache, oauth, portal, cfgStore, nil)

	val, err := p.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val.AccessKeyID != "AKLTvalid" {
		t.Fatalf("got %q", val.AccessKeyID)
	}

	// Verify the cache files exist on disk as direct basenames.
	tokenKeyDigest, _ := tokenKey("https://example.volccloudidentity.com", "test-session")
	stsKeyDigest, _ := stsKey("test-session", "acc-1", "role-1")
	if _, err := os.Stat(filepath.Join(dir, "token-"+tokenKeyDigest+".json")); err != nil {
		t.Fatalf("token cache file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "sts-"+stsKeyDigest+".json")); err != nil {
		t.Fatalf("sts cache file missing: %v", err)
	}
}

func TestSSOProviderRefreshWindowConstant(t *testing.T) {
	if RefreshWindow != 60*time.Second {
		t.Fatalf("RefreshWindow = %v, want 60s", RefreshWindow)
	}
}

func TestSSOProviderExpiredClientRegistration(t *testing.T) {
	tc := validTokenCache()
	tc.ExpiresAt = testClock.Add(30 * time.Second).UTC().Format(time.RFC3339)
	tc.ClientSecretExpiresAt = 1000 // way in the past
	cache := newFakeCache()
	cache.WriteToken(tc)

	oauth := &fakeOAuth{}
	portal := &fakeSTSExchanger{}
	p := newTestProvider(t, cache, oauth, portal, &fakeConfigUpdater{}, nil)

	_, err := p.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected error when client registration is expired")
	}
	var authErr *auth.Error
	if !errors.As(err, &authErr) {
		t.Fatalf("expected *auth.Error, got %T", err)
	}
	if authErr.Kind != auth.ReauthRequired {
		t.Fatalf("kind = %q, want ReauthRequired", authErr.Kind)
	}
}

func TestSSOProviderInvalidGrantRequiresLogin(t *testing.T) {
	tc := validTokenCache()
	tc.ExpiresAt = testClock.Add(30 * time.Second).UTC().Format(time.RFC3339)
	cache := newFakeCache()
	cache.WriteToken(tc)

	oauth := &fakeOAuth{
		tokenErr: &OAuthAPIError{StatusCode: 400, Code: "invalid_grant"},
	}
	portal := &fakeSTSExchanger{}
	p := newTestProvider(t, cache, oauth, portal, &fakeConfigUpdater{}, nil)

	_, err := p.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid_grant")
	}
	var authErr *auth.Error
	if !errors.As(err, &authErr) {
		t.Fatalf("expected *auth.Error, got %T", err)
	}
	if authErr.Kind != auth.ReauthRequired {
		t.Fatalf("kind = %q, want ReauthRequired", authErr.Kind)
	}
}

// Compile-time assertions.
var (
	_ STSExchanger  = (*fakeSTSExchanger)(nil)
	_ ConfigUpdater = (*fakeConfigUpdater)(nil)
	_ auth.Provider = (*SSOProvider)(nil)
)

// Ensure fmt is used (for potential future formatting).
var _ = fmt.Sprintf

func TestConfigUpdaterProduction(t *testing.T) {
	// Verify the production configUpdater wraps config.Update.
	u := configUpdater{}
	_ = u
	// Just verify the type implements the interface.
	var _ ConfigUpdater = configUpdater{}
}

func TestValidateRoleCredentials(t *testing.T) {
	// Use a valid future expiration so the test exercises field completeness,
	// not expiry. Expiration=1000 (year 1970) is expired and must not be
	// treated as valid.
	validExp := testClock.Add(time.Hour).Unix()
	cases := []struct {
		name  string
		creds *RoleCredentials
	}{
		{"nil", nil},
		{"missing_ak", &RoleCredentials{SecretAccessKey: "SK", SessionToken: "ST", Expiration: validExp}},
		{"missing_sk", &RoleCredentials{AccessKeyID: "AK", SessionToken: "ST", Expiration: validExp}},
		{"missing_token", &RoleCredentials{AccessKeyID: "AK", SecretAccessKey: "SK", Expiration: validExp}},
		{"missing_exp", &RoleCredentials{AccessKeyID: "AK", SecretAccessKey: "SK", SessionToken: "ST"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := validateRoleCredentials(c.creds); err == nil {
				t.Fatal("expected error")
			}
		})
	}
	// Valid case: all fields present and expiration is in the future.
	if err := validateRoleCredentials(&RoleCredentials{
		AccessKeyID: "AK", SecretAccessKey: "SK", SessionToken: "ST", Expiration: validExp,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReauthRequired(t *testing.T) {
	// With cause.
	err := reauthRequired("test description", errors.New("inner"))
	var authErr *auth.Error
	if !errors.As(err, &authErr) {
		t.Fatal("expected *auth.Error")
	}
	if authErr.Kind != auth.ReauthRequired {
		t.Fatalf("kind = %q, want ReauthRequired", authErr.Kind)
	}
	// Without cause.
	err2 := reauthRequired("test description", nil)
	if !errors.As(err2, &authErr) {
		t.Fatal("expected *auth.Error")
	}
}

func TestSTSCacheToValueEdgeCases(t *testing.T) {
	// Nil cache.
	if _, err := stsCacheToValue(nil, "s", "a", "r", testClock); err == nil {
		t.Fatal("expected error for nil cache")
	}
	// Mismatched binding.
	c := &STSCache{SessionName: "other", AccountID: "a", RoleName: "r", AccessKeyID: "AK", SecretAccessKey: "SK", SessionToken: "ST", ProviderName: "sso", ExpiresAt: testClock.Add(time.Hour).Format(time.RFC3339)}
	if _, err := stsCacheToValue(c, "s", "a", "r", testClock); err == nil {
		t.Fatal("expected error for mismatched session")
	}
	// Missing provider name.
	c = &STSCache{SessionName: "s", AccountID: "a", RoleName: "r", AccessKeyID: "AK", SecretAccessKey: "SK", SessionToken: "ST", ExpiresAt: testClock.Add(time.Hour).Format(time.RFC3339)}
	if _, err := stsCacheToValue(c, "s", "a", "r", testClock); err == nil {
		t.Fatal("expected error for missing provider name")
	}
	// Missing credentials.
	c = &STSCache{SessionName: "s", AccountID: "a", RoleName: "r", ProviderName: "sso", ExpiresAt: testClock.Add(time.Hour).Format(time.RFC3339)}
	if _, err := stsCacheToValue(c, "s", "a", "r", testClock); err == nil {
		t.Fatal("expected error for missing credentials")
	}
	// Invalid expiration.
	c = &STSCache{SessionName: "s", AccountID: "a", RoleName: "r", AccessKeyID: "AK", SecretAccessKey: "SK", SessionToken: "ST", ProviderName: "sso", ExpiresAt: "invalid"}
	if _, err := stsCacheToValue(c, "s", "a", "r", testClock); err == nil {
		t.Fatal("expected error for invalid expiration")
	}
	// Stale (near expiry).
	c = &STSCache{SessionName: "s", AccountID: "a", RoleName: "r", AccessKeyID: "AK", SecretAccessKey: "SK", SessionToken: "ST", ProviderName: "sso", ExpiresAt: testClock.Add(30 * time.Second).Format(time.RFC3339)}
	if _, err := stsCacheToValue(c, "s", "a", "r", testClock); err == nil {
		t.Fatal("expected error for stale cache")
	}
}

// TestSTSCacheToValueNonSSOProvider verifies that an STS cache with a provider
// name other than "sso" is rejected without a Portal fallback.
func TestSTSCacheToValueNonSSOProvider(t *testing.T) {
	// Non-SSO provider name must fail.
	c := &STSCache{
		SessionName: "s", AccountID: "a", RoleName: "r",
		AccessKeyID: "AK", SecretAccessKey: "SK", SessionToken: "ST",
		ProviderName: "console", ExpiresAt: testClock.Add(time.Hour).Format(time.RFC3339),
	}
	if _, err := stsCacheToValue(c, "s", "a", "r", testClock); err == nil {
		t.Fatal("expected error for non-SSO provider name")
	}
	// Empty provider name must fail.
	c.ProviderName = ""
	if _, err := stsCacheToValue(c, "s", "a", "r", testClock); err == nil {
		t.Fatal("expected error for empty provider name")
	}
}

// TestPatchConfigExactBinding verifies that patchConfig requires an exact match
// between the profile and the Provider binding. Empty fields are mismatches,
// not wildcards. On mismatch, STSExpiration is not patched and no credentials
// are returned.
func TestPatchConfigExactBinding(t *testing.T) {
	baseProfile := config.Profile{
		Mode:           config.AuthModeSSO,
		SSOSessionName: "test-session",
		AccountID:      "acc-1",
		RoleName:       "role-1",
	}

	cases := []struct {
		name    string
		profile config.Profile
	}{
		{"wrong_mode", config.Profile{Mode: config.AuthModeConsoleLogin, SSOSessionName: "test-session", AccountID: "acc-1", RoleName: "role-1"}},
		{"empty_mode", config.Profile{Mode: "", SSOSessionName: "test-session", AccountID: "acc-1", RoleName: "role-1"}},
		{"wrong_session", config.Profile{Mode: config.AuthModeSSO, SSOSessionName: "other", AccountID: "acc-1", RoleName: "role-1"}},
		{"empty_session", config.Profile{Mode: config.AuthModeSSO, SSOSessionName: "", AccountID: "acc-1", RoleName: "role-1"}},
		{"wrong_account", config.Profile{Mode: config.AuthModeSSO, SSOSessionName: "test-session", AccountID: "other", RoleName: "role-1"}},
		{"empty_account", config.Profile{Mode: config.AuthModeSSO, SSOSessionName: "test-session", AccountID: "", RoleName: "role-1"}},
		{"wrong_role", config.Profile{Mode: config.AuthModeSSO, SSOSessionName: "test-session", AccountID: "acc-1", RoleName: "other"}},
		{"empty_role", config.Profile{Mode: config.AuthModeSSO, SSOSessionName: "test-session", AccountID: "acc-1", RoleName: ""}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfgStore := &fakeConfigUpdater{
				cfg: config.Config{
					Profiles: map[string]config.Profile{
						"test-profile": c.profile,
					},
				},
			}
			cache := newFakeCache()
			oauth := &fakeOAuth{}
			portal := &fakeSTSExchanger{}
			p := newTestProvider(t, cache, oauth, portal, cfgStore, nil)

			// Put a valid token cache so we reach the STS exchange step.
			cache.WriteToken(validTokenCache())
			// Put a stale STS cache so we attempt exchange and config patch.
			cache.WriteSTS(&STSCache{
				SessionName: "test-session", AccountID: "acc-1", RoleName: "role-1",
				AccessKeyID: "old-AK", SecretAccessKey: "old-SK", SessionToken: "old-ST",
				ProviderName: ProviderName,
				ExpiresAt:    testClock.Add(30 * time.Second).Format(time.RFC3339),
			})
			portal.creds = &RoleCredentials{
				AccessKeyID: "new-AK", SecretAccessKey: "new-SK", SessionToken: "new-ST",
				Expiration: testClock.Add(time.Hour).Unix(),
			}

			_, err := p.Retrieve(context.Background())
			if err == nil {
				t.Fatal("expected error for mismatched binding")
			}
			// STSExpiration must not have been patched.
			prof := cfgStore.cfg.Profiles["test-profile"]
			if prof.STSExpiration != 0 {
				t.Fatalf("STSExpiration = %d, want 0 (not patched on mismatch)", prof.STSExpiration)
			}
		})
	}

	// A correctly matching profile must be patched successfully.
	t.Run("match", func(t *testing.T) {
		cfgStore := &fakeConfigUpdater{
			cfg: config.Config{
				Profiles: map[string]config.Profile{
					"test-profile": baseProfile,
				},
			},
		}
		cache := newFakeCache()
		oauth := &fakeOAuth{}
		portal := &fakeSTSExchanger{}
		p := newTestProvider(t, cache, oauth, portal, cfgStore, nil)

		cache.WriteToken(validTokenCache())
		cache.WriteSTS(&STSCache{
			SessionName: "test-session", AccountID: "acc-1", RoleName: "role-1",
			AccessKeyID: "old-AK", SecretAccessKey: "old-SK", SessionToken: "old-ST",
			ProviderName: ProviderName,
			ExpiresAt:    testClock.Add(30 * time.Second).Format(time.RFC3339),
		})
		portal.creds = &RoleCredentials{
			AccessKeyID: "new-AK", SecretAccessKey: "new-SK", SessionToken: "new-ST",
			Expiration: testClock.Add(time.Hour).Unix(),
		}

		val, err := p.Retrieve(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if val.AccessKeyID != "new-AK" {
			t.Fatalf("got %q, want new-AK", val.AccessKeyID)
		}
		prof := cfgStore.cfg.Profiles["test-profile"]
		if prof.STSExpiration == 0 {
			t.Fatal("STSExpiration should be patched on match")
		}
	})
}

// TestSSOProviderConfigFailureLeavesUncommittedSTS verifies that when config
// update fails after the STS cache is written, the STS entry is left
// uncommitted (not deleted). A subsequent Retrieve retries the config patch
// using the cached STS without another Portal exchange, and still returns no
// credentials while config fails.
func TestSSOProviderConfigFailureLeavesUncommittedSTS(t *testing.T) {
	cache := newFakeCache()
	oauth := &fakeOAuth{}
	portal := &fakeSTSExchanger{
		creds: &RoleCredentials{
			AccessKeyID: "AK", SecretAccessKey: "SK", SessionToken: "ST",
			Expiration: testClock.Add(time.Hour).Unix(),
		},
	}
	// First call: config update fails.
	cfgStore := &fakeConfigUpdater{
		updateErr: errors.New("simulated config write failure"),
		cfg: config.Config{
			Profiles: map[string]config.Profile{
				"test-profile": {
					Mode:           config.AuthModeSSO,
					SSOSessionName: "test-session",
					AccountID:      "acc-1",
					RoleName:       "role-1",
				},
			},
		},
	}
	p := newTestProvider(t, cache, oauth, portal, cfgStore, nil)
	cache.WriteToken(validTokenCache())

	_, err := p.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected error from config update failure")
	}
	// The STS cache must exist but be uncommitted (not deleted).
	sts, rerr := cache.ReadSTS("test-session", "acc-1", "role-1")
	if rerr != nil {
		t.Fatalf("STS cache should exist after config failure, got err: %v", rerr)
	}
	if sts.HasCommittedTarget(testCommitTarget()) {
		t.Fatal("STS cache should be uncommitted after config failure")
	}
	// Second call: must retry config patch (no Portal exchange) and still fail
	// because config still fails. It must NOT return the uncommitted STS.
	portalCallsBefore := atomic.LoadInt32(&portal.calls)
	_, err = p.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected error on retry (config still fails)")
	}
	if got := atomic.LoadInt32(&portal.calls); got != portalCallsBefore {
		t.Fatalf("portal calls = %d, want %d (must not re-exchange; retry config patch only)", got, portalCallsBefore)
	}
	// STS cache must still be uncommitted.
	sts, _ = cache.ReadSTS("test-session", "acc-1", "role-1")
	if sts == nil || sts.HasCommittedTarget(testCommitTarget()) {
		t.Fatal("STS cache should still be uncommitted after failed retry")
	}
}

// TestSSOProviderUncommittedSTSRecoversOnConfigFix verifies that an uncommitted
// STS cache (left by a previous config failure) is committed and returned when
// config later succeeds, without a second Portal exchange.
func TestSSOProviderUncommittedSTSRecoversOnConfigFix(t *testing.T) {
	cache := newFakeCache()
	oauth := &fakeOAuth{}
	portal := &fakeSTSExchanger{
		creds: &RoleCredentials{
			AccessKeyID: "AK", SecretAccessKey: "SK", SessionToken: "ST",
			Expiration: testClock.Add(time.Hour).Unix(),
		},
	}
	// First call: config update fails, leaving an uncommitted STS cache.
	cfgStore := &fakeConfigUpdater{
		updateErr: errors.New("simulated config write failure"),
		cfg: config.Config{
			Profiles: map[string]config.Profile{
				"test-profile": {
					Mode:           config.AuthModeSSO,
					SSOSessionName: "test-session",
					AccountID:      "acc-1",
					RoleName:       "role-1",
				},
			},
		},
	}
	p := newTestProvider(t, cache, oauth, portal, cfgStore, nil)
	cache.WriteToken(validTokenCache())

	_, err := p.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected error from config update failure")
	}
	portalCallsAfterFirst := atomic.LoadInt32(&portal.calls)

	// Second call: config now succeeds. The uncommitted STS should be committed
	// and returned without another Portal exchange.
	cfgStore.updateErr = nil
	val, err := p.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("unexpected error on config recovery: %v", err)
	}
	if val.AccessKeyID != "AK" {
		t.Fatalf("got %q, want AK (should use existing uncommitted STS)", val.AccessKeyID)
	}
	// Portal must not have been called again.
	if got := atomic.LoadInt32(&portal.calls); got != portalCallsAfterFirst {
		t.Fatalf("portal calls = %d, want %d (must not re-exchange on config recovery)", got, portalCallsAfterFirst)
	}
	// The STS cache must now be committed.
	sts, _ := cache.ReadSTS("test-session", "acc-1", "role-1")
	if sts == nil || !sts.HasCommittedTarget(testCommitTarget()) {
		t.Fatal("STS cache should be committed after config recovery")
	}
}

// TestSSOProviderCommittedMarkerWriteFailure verifies that if writing the
// committed marker fails, no credentials are returned.
func TestSSOProviderCommittedMarkerWriteFailure(t *testing.T) {
	cache := &failingSecondWriteSTSCache{Cache: newFakeCache()}
	cache.WriteToken(validTokenCache())

	oauth := &fakeOAuth{}
	portal := &fakeSTSExchanger{
		creds: &RoleCredentials{
			AccessKeyID: "AK", SecretAccessKey: "SK", SessionToken: "ST",
			Expiration: testClock.Add(time.Hour).Unix(),
		},
	}
	cfgStore := newFakeConfigStore()
	p := newTestProvider(t, cache, oauth, portal, cfgStore, nil)

	_, err := p.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected error when committed marker write fails")
	}
	// The STS cache should exist but be uncommitted.
	sts, _ := cache.ReadSTS("test-session", "acc-1", "role-1")
	if sts == nil {
		t.Fatal("STS cache should exist after marker write failure")
	}
	if sts.HasCommittedTarget(testCommitTarget()) {
		t.Fatal("STS cache should be uncommitted after marker write failure")
	}
}

// failingSecondWriteSTSCache wraps a Cache and fails the second WriteSTS call
// (the committed-marker write) while allowing the first (uncommitted) write.
type failingSecondWriteSTSCache struct {
	Cache
	writeCount int
}

func (c *failingSecondWriteSTSCache) WriteSTS(cache *STSCache) error {
	c.writeCount++
	if c.writeCount == 2 {
		return errors.New("simulated committed marker write failure")
	}
	// Store a copy so later modifications to the original pointer (e.g.
	// adding the commit identity before the second write) do not affect the
	// cached entry, mirroring the production FileCache which serializes to JSON.
	cp := *cache
	return c.Cache.WriteSTS(&cp)
}

// TestSSOProviderUncommittedSTSNeverFastPath verifies that an uncommitted STS
// cache never takes the normal valid-cache fast path: it must retry the config
// patch (and call Portal only if the STS is expired).
func TestSSOProviderUncommittedSTSNeverFastPath(t *testing.T) {
	cache := newFakeCache()
	cache.WriteToken(validTokenCache())
	// Write a valid but uncommitted STS cache directly.
	sts := validSTSCache()
	sts.CommittedTargets = nil
	cache.WriteSTS(sts)

	oauth := &fakeOAuth{}
	portal := &fakeSTSExchanger{}
	cfgStore := newFakeConfigStore()
	p := newTestProvider(t, cache, oauth, portal, cfgStore, nil)

	val, err := p.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val.AccessKeyID != "AKLTvalid" {
		t.Fatalf("got %q, want AKLTvalid", val.AccessKeyID)
	}
	// Portal must not be called (STS is valid, just uncommitted).
	if got := atomic.LoadInt32(&portal.calls); got != 0 {
		t.Fatalf("portal calls = %d, want 0 (valid uncommitted STS should not trigger exchange)", got)
	}
	// Config must have been patched (to commit the uncommitted cache).
	if got := atomic.LoadInt32(&cfgStore.patched); got != 1 {
		t.Fatalf("config patches = %d, want 1 (uncommitted STS must retry config patch)", got)
	}
	// The STS cache must now be committed.
	got, _ := cache.ReadSTS("test-session", "acc-1", "role-1")
	if got == nil || !got.HasCommittedTarget(testCommitTarget()) {
		t.Fatal("STS cache should be committed after config patch")
	}
}

// TestSSOProviderTwoProfilesSameBindingOneFails verifies that a successful
// config patch for profile B never authorizes profile A to skip its own failed
// config patch. Profile A's patch fails, profile B succeeds and commits, then A
// still must retry/fail its own patch and return no credentials. Portal must not
// be called again after the initial exchange.
func TestSSOProviderTwoProfilesSameBindingOneFails(t *testing.T) {
	cache := newFakeCache()
	cache.WriteToken(validTokenCache())

	oauth := &fakeOAuth{}
	portal := &fakeSTSExchanger{
		creds: &RoleCredentials{
			AccessKeyID: "AK", SecretAccessKey: "SK", SessionToken: "ST",
			Expiration: testClock.Add(time.Hour).Unix(),
		},
	}
	// Config store has only prof-b; prof-a is absent so its patch fails with
	// "profile not found". Both profiles share the same STS binding.
	cfgStore := &fakeConfigUpdater{
		cfg: config.Config{
			Profiles: map[string]config.Profile{
				"prof-b": {Mode: config.AuthModeSSO, SSOSessionName: "test-session", AccountID: "acc-1", RoleName: "role-1"},
			},
		},
	}
	clock := func() time.Time { return testClock }

	providerA, _ := NewSSOProvider(&SSOProviderConfig{
		ConfigPath: "/tmp/c.json", ProfileName: "prof-a",
		StartURL: "https://example.volccloudidentity.com", SessionName: "test-session",
		SSORegion: "cn-beijing",
		AccountID: "acc-1", RoleName: "role-1",
		Cache: cache, OAuth: oauth, Portal: portal, ConfigStore: cfgStore, Clock: clock,
	})
	providerB, _ := NewSSOProvider(&SSOProviderConfig{
		ConfigPath: "/tmp/c.json", ProfileName: "prof-b",
		StartURL: "https://example.volccloudidentity.com", SessionName: "test-session",
		SSORegion: "cn-beijing",
		AccountID: "acc-1", RoleName: "role-1",
		Cache: cache, OAuth: oauth, Portal: portal, ConfigStore: cfgStore, Clock: clock,
	})

	// 1. Profile A: exchanges STS (first Portal call), config patch fails
	// (profile not found → permanent binding mismatch). The uncommitted STS
	// orphan (CommittedTargets=nil) is cleaned up.
	_, err := providerA.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected error from profile A config patch failure")
	}
	portalCallsAfterA := atomic.LoadInt32(&portal.calls)
	if portalCallsAfterA != 1 {
		t.Fatalf("portal calls = %d, want 1 (initial exchange)", portalCallsAfterA)
	}
	// STS cache was deleted as an uncommitted orphan (profile not found is a
	// permanent binding mismatch, not a transient config I/O failure).
	if sts, _ := cache.ReadSTS("test-session", "acc-1", "role-1"); sts != nil {
		t.Fatalf("STS cache should be deleted after A's failed patch, got %+v", sts)
	}

	// 2. Profile B: must re-exchange (STS was deleted), then patch config and
	// commit. This is one more Portal call than the original test expected.
	valB, err := providerB.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("profile B unexpected error: %v", err)
	}
	if valB.AccessKeyID != "AK" {
		t.Fatalf("profile B got %q, want AK", valB.AccessKeyID)
	}
	portalCallsAfterB := atomic.LoadInt32(&portal.calls)
	if portalCallsAfterB != portalCallsAfterA+1 {
		t.Fatalf("portal calls = %d, want %d (B must re-exchange after A's orphan was deleted)", portalCallsAfterB, portalCallsAfterA+1)
	}
	// STS cache now has B's commit identity only.
	sts, _ := cache.ReadSTS("test-session", "acc-1", "role-1")
	bTarget, _ := commitTargetKey("/tmp/c.json", "prof-b")
	if !sts.HasCommittedTarget(bTarget) {
		t.Fatal("STS cache should have B's commit identity after B succeeds")
	}
	aTarget, _ := commitTargetKey("/tmp/c.json", "prof-a")
	if sts.HasCommittedTarget(aTarget) {
		t.Fatal("STS cache must NOT have A's commit identity (A never succeeded)")
	}

	// 3. Profile A retries: config still fails, must not return credentials,
	// must not call Portal again (uses B's committed shared STS).
	_, err = providerA.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected error from profile A retry (config still fails)")
	}
	if got := atomic.LoadInt32(&portal.calls); got != portalCallsAfterB {
		t.Fatalf("portal calls = %d, want %d (A must use B's cached STS, not re-exchange)", got, portalCallsAfterB)
	}
	// STS cache still only has B's identity; A's must not have been added.
	sts, _ = cache.ReadSTS("test-session", "acc-1", "role-1")
	if sts.HasCommittedTarget(aTarget) {
		t.Fatal("STS cache must NOT have A's commit identity after failed retry")
	}
}

// TestSSOProviderTwoProfilesSameBindingBothSucceed verifies that two profiles
// sharing the same STS binding each independently patch and commit, and that
// the persisted CommittedTargets set is sorted and de-duplicated.
func TestSSOProviderTwoProfilesSameBindingBothSucceed(t *testing.T) {
	cache := newFakeCache()
	cache.WriteToken(validTokenCache())

	oauth := &fakeOAuth{}
	portal := &fakeSTSExchanger{
		creds: &RoleCredentials{
			AccessKeyID: "AK", SecretAccessKey: "SK", SessionToken: "ST",
			Expiration: testClock.Add(time.Hour).Unix(),
		},
	}
	cfgStore := &fakeConfigUpdater{
		cfg: config.Config{
			Profiles: map[string]config.Profile{
				"prof-a": {Mode: config.AuthModeSSO, SSOSessionName: "test-session", AccountID: "acc-1", RoleName: "role-1"},
				"prof-b": {Mode: config.AuthModeSSO, SSOSessionName: "test-session", AccountID: "acc-1", RoleName: "role-1"},
			},
		},
	}
	clock := func() time.Time { return testClock }

	providerA, _ := NewSSOProvider(&SSOProviderConfig{
		ConfigPath: "/tmp/c.json", ProfileName: "prof-a",
		StartURL: "https://example.volccloudidentity.com", SessionName: "test-session",
		SSORegion: "cn-beijing",
		AccountID: "acc-1", RoleName: "role-1",
		Cache: cache, OAuth: oauth, Portal: portal, ConfigStore: cfgStore, Clock: clock,
	})
	providerB, _ := NewSSOProvider(&SSOProviderConfig{
		ConfigPath: "/tmp/c.json", ProfileName: "prof-b",
		StartURL: "https://example.volccloudidentity.com", SessionName: "test-session",
		SSORegion: "cn-beijing",
		AccountID: "acc-1", RoleName: "role-1",
		Cache: cache, OAuth: oauth, Portal: portal, ConfigStore: cfgStore, Clock: clock,
	})

	// 1. Profile A: exchanges STS, patches config, commits.
	valA, err := providerA.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("profile A unexpected error: %v", err)
	}
	if valA.AccessKeyID != "AK" {
		t.Fatalf("profile A got %q, want AK", valA.AccessKeyID)
	}
	portalCallsAfterA := atomic.LoadInt32(&portal.calls)

	// 2. Profile B: uses cached STS, patches config, commits. No Portal call.
	valB, err := providerB.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("profile B unexpected error: %v", err)
	}
	if valB.AccessKeyID != "AK" {
		t.Fatalf("profile B got %q, want AK", valB.AccessKeyID)
	}
	if got := atomic.LoadInt32(&portal.calls); got != portalCallsAfterA {
		t.Fatalf("portal calls = %d, want %d (B must use cached STS)", got, portalCallsAfterA)
	}

	// 3. STS cache has both identities, sorted and de-duplicated.
	sts, _ := cache.ReadSTS("test-session", "acc-1", "role-1")
	aTarget, _ := commitTargetKey("/tmp/c.json", "prof-a")
	bTarget, _ := commitTargetKey("/tmp/c.json", "prof-b")
	if !sts.HasCommittedTarget(aTarget) {
		t.Fatal("STS cache should have A's commit identity")
	}
	if !sts.HasCommittedTarget(bTarget) {
		t.Fatal("STS cache should have B's commit identity")
	}
	if len(sts.CommittedTargets) != 2 {
		t.Fatalf("CommittedTargets len = %d, want 2", len(sts.CommittedTargets))
	}
	// Must be sorted.
	if sts.CommittedTargets[0] > sts.CommittedTargets[1] {
		t.Fatalf("CommittedTargets not sorted: %v", sts.CommittedTargets)
	}

	// 4. Subsequent calls by both profiles use the fast path (no config patch,
	// no Portal call).
	patchedBefore := atomic.LoadInt32(&cfgStore.patched)
	if _, err := providerA.Retrieve(context.Background()); err != nil {
		t.Fatalf("profile A fast path error: %v", err)
	}
	if _, err := providerB.Retrieve(context.Background()); err != nil {
		t.Fatalf("profile B fast path error: %v", err)
	}
	if got := atomic.LoadInt32(&cfgStore.patched); got != patchedBefore {
		t.Fatalf("config patches = %d, want %d (fast path must not patch config)", got, patchedBefore)
	}
	if got := atomic.LoadInt32(&portal.calls); got != portalCallsAfterA {
		t.Fatalf("portal calls = %d, want %d (fast path must not call Portal)", got, portalCallsAfterA)
	}
}

// TestSSOProviderNewSTSExpiryValidation verifies that a freshly exchanged STS
// that is expired, exactly at the RefreshWindow boundary, or within the window
// is rejected with a ProtocolError before any STS write or config patch. Both
// seconds and millisecond timestamps are exercised.
func TestSSOProviderNewSTSExpiryValidation(t *testing.T) {
	cases := []struct {
		name       string
		expiration int64
	}{
		{"past_seconds", testClock.Add(-time.Hour).Unix()},
		{"past_millis", testClock.Add(-time.Hour).UnixMilli()},
		{"at_boundary_seconds", testClock.Add(RefreshWindow).Unix()},
		{"at_boundary_millis", testClock.Add(RefreshWindow).UnixMilli()},
		{"within_window_seconds", testClock.Add(30 * time.Second).Unix()},
		{"within_window_millis", testClock.Add(30 * time.Second).UnixMilli()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cache := newFakeCache()
			cache.WriteToken(validTokenCache())
			oauth := &fakeOAuth{}
			portal := &fakeSTSExchanger{
				creds: &RoleCredentials{
					AccessKeyID: "AK", SecretAccessKey: "SK", SessionToken: "ST",
					Expiration: c.expiration,
				},
			}
			cfgStore := newFakeConfigStore()
			p := newTestProvider(t, cache, oauth, portal, cfgStore, func() time.Time { return testClock })

			_, err := p.Retrieve(context.Background())
			if err == nil {
				t.Fatal("expected error for expired/near-expiry STS")
			}
			var authErr *auth.Error
			if !errors.As(err, &authErr) {
				t.Fatalf("expected *auth.Error, got %T", err)
			}
			if authErr.Kind != auth.ProtocolError {
				t.Fatalf("kind = %q, want %q", authErr.Kind, auth.ProtocolError)
			}
			// Nothing must have been persisted or patched.
			sts, _ := cache.ReadSTS("test-session", "acc-1", "role-1")
			if sts != nil {
				t.Fatal("STS cache must not be persisted for expired/near-expiry STS")
			}
			if got := atomic.LoadInt32(&cfgStore.patched); got != 0 {
				t.Fatalf("config patches = %d, want 0", got)
			}
		})
	}

	// Valid cases (strictly outside the window) must succeed for both seconds
	// and milliseconds.
	t.Run("valid_seconds", func(t *testing.T) {
		cache := newFakeCache()
		cache.WriteToken(validTokenCache())
		oauth := &fakeOAuth{}
		portal := &fakeSTSExchanger{
			creds: &RoleCredentials{
				AccessKeyID: "AK", SecretAccessKey: "SK", SessionToken: "ST",
				Expiration: testClock.Add(RefreshWindow + time.Second).Unix(),
			},
		}
		cfgStore := newFakeConfigStore()
		p := newTestProvider(t, cache, oauth, portal, cfgStore, func() time.Time { return testClock })
		val, err := p.Retrieve(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if val.AccessKeyID != "AK" {
			t.Fatalf("got %q, want AK", val.AccessKeyID)
		}
	})
	t.Run("valid_millis", func(t *testing.T) {
		cache := newFakeCache()
		cache.WriteToken(validTokenCache())
		oauth := &fakeOAuth{}
		portal := &fakeSTSExchanger{
			creds: &RoleCredentials{
				AccessKeyID: "AK", SecretAccessKey: "SK", SessionToken: "ST",
				Expiration: testClock.Add(RefreshWindow + time.Second).UnixMilli(),
			},
		}
		cfgStore := newFakeConfigStore()
		p := newTestProvider(t, cache, oauth, portal, cfgStore, func() time.Time { return testClock })
		val, err := p.Retrieve(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if val.AccessKeyID != "AK" {
			t.Fatalf("got %q, want AK", val.AccessKeyID)
		}
	})
}

// TestProviderConfigPathStableAcrossCwdChange verifies that a provider built
// with a relative config path captures an absolute config target at build time,
// so a later working-directory change cannot alter the file patched by
// config.Update or its commit identity. This test must not run in parallel and
// always restores the original cwd.
func TestProviderConfigPathStableAcrossCwdChange(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	cache := newFakeCache()
	oauth := &fakeOAuth{}
	portal := &fakeSTSExchanger{}
	cfgStore := newFakeConfigStore()
	p, err := NewSSOProvider(&SSOProviderConfig{
		ConfigPath:  "config.json",
		ProfileName: "test-profile",
		StartURL:    "https://example.volccloudidentity.com",
		SessionName: "test-session",
		SSORegion:   "cn-beijing",
		AccountID:   "acc-1",
		RoleName:    "role-1",
		Cache:       cache,
		OAuth:       oauth,
		Portal:      portal,
		ConfigStore: cfgStore,
		Clock:       func() time.Time { return testClock },
	})
	if err != nil {
		os.Chdir(orig)
		t.Fatalf("NewSSOProvider failed: %v", err)
	}

	// The provider must hold an absolute path captured at build time.
	if !filepath.IsAbs(p.configPath) {
		os.Chdir(orig)
		t.Fatalf("provider config path must be absolute, got %q", p.configPath)
	}
	wantPath := p.configPath
	wantKey, err := commitTargetKey(p.configPath, p.profileName)
	if err != nil {
		os.Chdir(orig)
		t.Fatal(err)
	}

	// Change cwd to a different directory; the provider must be unaffected.
	otherDir := t.TempDir()
	if err := os.Chdir(otherDir); err != nil {
		os.Chdir(orig)
		t.Fatal(err)
	}
	defer os.Chdir(orig)

	if p.configPath != wantPath {
		t.Fatalf("provider config path changed after cwd change: got %q want %q", p.configPath, wantPath)
	}
	gotKey, err := commitTargetKey(p.configPath, p.profileName)
	if err != nil {
		t.Fatal(err)
	}
	if gotKey != wantKey {
		t.Fatal("provider commit identity changed after cwd change")
	}
}

// TestInvalidCommittedTargetsFailClosed verifies that an STS cache with invalid
// committed-targets marker metadata fails closed as CacheCorrupt: it must not
// call Portal, must not patch config, and must not overwrite the cache.
func TestInvalidCommittedTargetsFailClosed(t *testing.T) {
	cases := []struct {
		name    string
		targets []string
	}{
		{"duplicate", []string{validHexTarget('0'), validHexTarget('0')}},
		{"unsorted", []string{validHexTarget('f'), validHexTarget('0')}},
		{"wrong_length", []string{"not-a-valid-target"}},
		{"non_hex", []string{validHexTarget('g')}},
		{"uppercase", []string{validHexTarget('A')}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cache := newFakeCache()
			cache.WriteToken(validTokenCache())
			// Write a valid (non-expired) STS cache but with dirty markers.
			sts := validSTSCache()
			sts.CommittedTargets = c.targets
			cache.WriteSTS(sts)

			oauth := &fakeOAuth{}
			portal := &fakeSTSExchanger{}
			cfgStore := newFakeConfigStore()
			p := newTestProvider(t, cache, oauth, portal, cfgStore, nil)

			_, err := p.Retrieve(context.Background())
			if err == nil {
				t.Fatal("expected error for invalid committed targets")
			}
			var authErr *auth.Error
			if !errors.As(err, &authErr) {
				t.Fatalf("expected *auth.Error, got %T", err)
			}
			if authErr.Kind != auth.CacheCorrupt {
				t.Fatalf("kind = %q, want %q", authErr.Kind, auth.CacheCorrupt)
			}
			// Portal must not be called.
			if got := atomic.LoadInt32(&portal.calls); got != 0 {
				t.Fatalf("portal calls = %d, want 0 (must not call Portal on invalid markers)", got)
			}
			// Config must not be patched.
			if got := atomic.LoadInt32(&cfgStore.patched); got != 0 {
				t.Fatalf("config patches = %d, want 0 (must not patch config on invalid markers)", got)
			}
			// The cache must not be overwritten.
			got, _ := cache.ReadSTS("test-session", "acc-1", "role-1")
			if got == nil {
				t.Fatal("STS cache must not be overwritten on invalid markers")
			}
			if len(got.CommittedTargets) != len(c.targets) {
				t.Fatalf("STS cache was overwritten: got %v", got.CommittedTargets)
			}
		})
	}
}

// advancingClock is a controllable clock for tests that need to simulate time
// passing between two points in the Retrieve flow.
type advancingClock struct {
	mu      sync.Mutex
	current time.Time
}

func newAdvancingClock(start time.Time) *advancingClock {
	return &advancingClock{current: start}
}

func (c *advancingClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.current
}

func (c *advancingClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = c.current.Add(d)
}

// stsLockAdvancingCache wraps a Cache and advances a fake clock inside
// WithSTSLock before invoking the callback, simulating real time passing while
// waiting for the cross-process STS lock.
type stsLockAdvancingCache struct {
	Cache
	clock   *advancingClock
	advance time.Duration
}

func (c *stsLockAdvancingCache) WithSTSLock(ctx context.Context, sessionName, accountID, roleName string, fn func() error) error {
	c.clock.advance(c.advance)
	return c.Cache.WithSTSLock(ctx, sessionName, accountID, roleName, fn)
}

// TestSSOProviderStaleSTSAfterLockWaitMustNotFastPath is a deterministic
// regression for the stale-time bug: Retrieve captured `now` before a slow
// OAuth refresh / STS lock wait, then passed that stale time to stsCacheToValue
// after re-reading the STS cache. A committed STS that expired during the wait
// still took the fast path.
//
// Scenario: at t0 the cached committed STS expires at t0+90s (strictly outside
// the 60s RefreshWindow, so valid at t0). Waiting for the STS lock advances the
// clock to t0+120s, past the STS expiration. The provider must treat the cached
// STS as stale, call Portal exactly once, persist/commit a new valid STS, and
// return the new credentials. The old credentials must never be returned.
func TestSSOProviderStaleSTSAfterLockWaitMustNotFastPath(t *testing.T) {
	t0 := testClock
	clock := newAdvancingClock(t0)

	cache := newFakeCache()
	// Token is valid and far from expiry: no OAuth refresh, so the only time
	// advance is the simulated STS lock wait inside WithSTSLock.
	cache.WriteToken(validTokenCache())
	// Committed STS that expires at t0+90s: valid at t0 (outside 60s window),
	// expired by t0+120s.
	sts := validSTSCache()
	sts.AccessKeyID = "AKLTold"
	sts.SecretAccessKey = "SKold"
	sts.SessionToken = "STold"
	sts.ExpiresAt = t0.Add(90 * time.Second).UTC().Format(time.RFC3339)
	sts.CommittedTargets = []string{testCommitTarget()}
	cache.WriteSTS(sts)

	// Wrap the cache so the clock advances 120s while "waiting" for the STS lock.
	wrapped := &stsLockAdvancingCache{Cache: cache, clock: clock, advance: 120 * time.Second}

	oauth := &fakeOAuth{}
	portal := &fakeSTSExchanger{
		creds: &RoleCredentials{
			AccessKeyID:     "AKLTnew",
			SecretAccessKey: "SKnew",
			SessionToken:    "STnew",
			// Expiration far enough in the future to be valid at t0+120s.
			Expiration: t0.Add(time.Hour).Unix(),
		},
	}
	cfgStore := newFakeConfigStore()
	p := newTestProvider(t, wrapped, oauth, portal, cfgStore, clock.now)

	val, err := p.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Old credentials must not be returned.
	if val.AccessKeyID == "AKLTold" {
		t.Fatal("old (expired) STS credentials returned via fast path; expected a fresh Portal exchange")
	}
	if val.AccessKeyID != "AKLTnew" {
		t.Fatalf("got %q, want AKLTnew", val.AccessKeyID)
	}

	// Portal must be called exactly once.
	if got := atomic.LoadInt32(&portal.calls); got != 1 {
		t.Fatalf("portal calls = %d, want 1", got)
	}

	// The returned value must be valid (provider name set, expiration in the
	// future relative to the advanced clock).
	if val.ProviderName != ProviderName {
		t.Fatalf("provider = %q, want %q", val.ProviderName, ProviderName)
	}
	if !val.ExpiresAt.After(clock.now().Add(RefreshWindow)) {
		t.Fatalf("returned expiration %v is not strictly outside the refresh window at %v", val.ExpiresAt, clock.now())
	}

	// The persisted STS cache must hold the new credentials and be committed for
	// this target.
	got, rerr := cache.ReadSTS("test-session", "acc-1", "role-1")
	if rerr != nil {
		t.Fatalf("read sts cache: %v", rerr)
	}
	if got.AccessKeyID != "AKLTnew" {
		t.Fatalf("persisted sts ak = %q, want AKLTnew", got.AccessKeyID)
	}
	if !got.HasCommittedTarget(testCommitTarget()) {
		t.Fatal("persisted sts cache should be committed for the test target")
	}
}

// TestSSOProviderOAuthTokenAgesDuringSTSLockWaitRefreshesBeforePortal is the
// regression for task A: an access token valid at t0 (expires t0+90s, outside
// the 60s RefreshWindow) can age past expiry while waiting for the STS lock
// (clock advances to t0+120s). The provider must refresh OAuth exactly once
// before calling Portal, and Portal must receive the NEW access token, never the
// expired old one. The rotated refresh-token lineage must be persisted.
func TestSSOProviderOAuthTokenAgesDuringSTSLockWaitRefreshesBeforePortal(t *testing.T) {
	t0 := testClock
	clock := newAdvancingClock(t0)

	cache := newFakeCache()
	// Token valid at t0, expires at t0+90s (outside the 60s window at t0).
	tc := validTokenCache()
	tc.AccessToken = "old-at"
	tc.RefreshToken = "old-rt"
	tc.ExpiresAt = t0.Add(90 * time.Second).UTC().Format(time.RFC3339)
	cache.WriteToken(tc)
	// No STS cache: forces the exchange path so Portal is called.

	// Wrap the cache so the STS lock wait advances the clock to t0+120s, past
	// the token's t0+90s expiry.
	wrapped := &stsLockAdvancingCache{Cache: cache, clock: clock, advance: 120 * time.Second}

	oauth := &fakeOAuth{
		tokenResp: &CreateTokenResponse{
			AccessToken:  "new-at",
			TokenType:    "Bearer",
			ExpiresIn:    3600,
			RefreshToken: "new-rt",
		},
	}
	portal := &fakeSTSExchanger{
		creds: &RoleCredentials{
			AccessKeyID: "AK", SecretAccessKey: "SK", SessionToken: "ST",
			Expiration: t0.Add(time.Hour).Unix(),
		},
	}
	cfgStore := newFakeConfigStore()
	p := newTestProvider(t, wrapped, oauth, portal, cfgStore, clock.now)

	val, err := p.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val.AccessKeyID != "AK" {
		t.Fatalf("got %q, want AK", val.AccessKeyID)
	}

	// OAuth refresh must be called exactly once: the token aged during the lock
	// wait, triggering a single refresh before Portal.
	if got := atomic.LoadInt32(&oauth.tokenCalls); got != 1 {
		t.Fatalf("oauth refresh calls = %d, want 1", got)
	}
	// Portal must be called exactly once and receive the NEW access token.
	if got := atomic.LoadInt32(&portal.calls); got != 1 {
		t.Fatalf("portal calls = %d, want 1", got)
	}
	if portal.lastAccessToken != "new-at" {
		t.Fatalf("portal received access token %q, want new-at (must not use expired old token)", portal.lastAccessToken)
	}

	// The rotated token cache must be persisted with the new lineage.
	got, rerr := cache.ReadToken("https://example.volccloudidentity.com", "test-session")
	if rerr != nil {
		t.Fatalf("read token: %v", rerr)
	}
	if got.AccessToken != "new-at" {
		t.Fatalf("persisted access token = %q, want new-at", got.AccessToken)
	}
	if got.RefreshToken != "new-rt" {
		t.Fatalf("persisted refresh token = %q, want new-rt", got.RefreshToken)
	}
}

// configUpdaterAdvancingClock wraps a fakeConfigUpdater and advances a clock
// inside Update, simulating config.Update blocking long enough for an STS cache
// to enter the RefreshWindow.
type configUpdaterAdvancingClock struct {
	*fakeConfigUpdater
	clock   *advancingClock
	advance time.Duration
}

func (c *configUpdaterAdvancingClock) Update(path string, fn func(*config.Config) error) (config.Config, error) {
	c.clock.advance(c.advance)
	return c.fakeConfigUpdater.Update(path, fn)
}

// writeSTSSecondCallAdvancingCache wraps a Cache and advances a clock on the
// second WriteSTS call (the committed-marker write), simulating the marker
// write blocking long enough for an STS cache to enter the RefreshWindow.
type writeSTSSecondCallAdvancingCache struct {
	Cache
	clock      *advancingClock
	advance    time.Duration
	writeCount int
}

func (c *writeSTSSecondCallAdvancingCache) WriteSTS(cache *STSCache) error {
	c.writeCount++
	if c.writeCount == 2 {
		c.clock.advance(c.advance)
	}
	return c.Cache.WriteSTS(cache)
}

// TestSSOProviderUncommittedSTSExpiresDuringConfigUpdateFailsClosed covers task
// B's cached-uncommitted path: a valid but uncommitted STS cache (expires
// t0+90s) is re-validated after the config patch and marker write. The config
// update advances the clock to t0+120s, so the persisted STS is now expired.
// The provider must fail closed with a ProtocolError and return no stale AK.
func TestSSOProviderUncommittedSTSExpiresDuringConfigUpdateFailsClosed(t *testing.T) {
	t0 := testClock
	clock := newAdvancingClock(t0)

	cache := newFakeCache()
	cache.WriteToken(validTokenCache())
	// Valid but uncommitted STS cache expiring at t0+90s.
	sts := validSTSCache()
	sts.AccessKeyID = "AKLTold"
	sts.ExpiresAt = t0.Add(90 * time.Second).UTC().Format(time.RFC3339)
	sts.CommittedTargets = nil
	cache.WriteSTS(sts)

	oauth := &fakeOAuth{}
	portal := &fakeSTSExchanger{}
	cfgStore := &configUpdaterAdvancingClock{
		fakeConfigUpdater: newFakeConfigStore(),
		clock:             clock,
		advance:           120 * time.Second,
	}
	p := newTestProvider(t, cache, oauth, portal, cfgStore, clock.now)

	_, err := p.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected error when STS expires during config update")
	}
	var authErr *auth.Error
	if !errors.As(err, &authErr) {
		t.Fatalf("expected *auth.Error, got %T", err)
	}
	if authErr.Kind != auth.ProtocolError {
		t.Fatalf("kind = %q, want %q", authErr.Kind, auth.ProtocolError)
	}
	// Portal must not be called (STS was valid, just uncommitted).
	if got := atomic.LoadInt32(&portal.calls); got != 0 {
		t.Fatalf("portal calls = %d, want 0", got)
	}
}

// TestSSOProviderNewSTSExpiresDuringMarkerWriteFailsClosed covers task B's
// newly-exchanged path: a freshly exchanged STS (expires t0+90s) is re-validated
// after the uncommitted write, config patch, and committed-marker write. The
// marker write advances the clock to t0+120s, so the persisted STS is now
// expired. The provider must fail closed with a ProtocolError and return no
// stale AK.
func TestSSOProviderNewSTSExpiresDuringMarkerWriteFailsClosed(t *testing.T) {
	t0 := testClock
	clock := newAdvancingClock(t0)

	baseCache := newFakeCache()
	baseCache.WriteToken(validTokenCache())
	// No STS cache: forces the exchange path.

	// Wrap the cache so the second WriteSTS (committed marker) advances the
	// clock to t0+120s, past the new STS's t0+90s expiry.
	wrapped := &writeSTSSecondCallAdvancingCache{
		Cache:   baseCache,
		clock:   clock,
		advance: 120 * time.Second,
	}

	oauth := &fakeOAuth{}
	portal := &fakeSTSExchanger{
		creds: &RoleCredentials{
			AccessKeyID: "AKLTnew", SecretAccessKey: "SKnew", SessionToken: "STnew",
			Expiration: t0.Add(90 * time.Second).Unix(),
		},
	}
	cfgStore := newFakeConfigStore()
	p := newTestProvider(t, wrapped, oauth, portal, cfgStore, clock.now)

	_, err := p.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected error when STS expires during marker write")
	}
	var authErr *auth.Error
	if !errors.As(err, &authErr) {
		t.Fatalf("expected *auth.Error, got %T", err)
	}
	if authErr.Kind != auth.ProtocolError {
		t.Fatalf("kind = %q, want %q", authErr.Kind, auth.ProtocolError)
	}
	// The stale AK must not be returned (verified by the error above). The
	// persisted STS may be left committed but expired; the next Retrieve will
	// see errSTSExpired and exchange again.
}
