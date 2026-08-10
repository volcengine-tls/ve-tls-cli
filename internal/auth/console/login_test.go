package console

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth/oauth"
	"github.com/volcengine-tls/ve-tls-cli/internal/config"
	"github.com/volcengine-tls/ve-tls-cli/internal/securestore"
)

// --- Test fixtures ---

func validIDToken(trn string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"trn":"` + trn + `"}`))
	sig := base64.RawURLEncoding.EncodeToString([]byte("signature"))
	return header + "." + payload + "." + sig
}

func validSTSAccessToken() json.RawMessage {
	return json.RawMessage(`{"access_key_id":"AKLT1234567890abcdef","secret_access_key":"secret-key-canary-xyz","session_token":"session-token-canary-789"}`)
}

func validTokenResponse(trn string) *ConsoleTokenResponse {
	return &ConsoleTokenResponse{
		AccessToken:  validSTSAccessToken(),
		TokenType:    "sts",
		ExpiresIn:    3600,
		RefreshToken: "refresh-token-canary",
		Scope:        Scope,
		IDToken:      validIDToken(trn),
	}
}

// --- Fakes ---

type fakeCache struct {
	mu          sync.Mutex
	data        map[string][]byte
	withLockCnt int32
	writeCnt    int32
	deleteCnt   int32
	readCnt     int32
	writeErr    error
	deleteErr   error
	readErr     error
	// blockWrite, if non-nil, is signaled after WriteRaw acquires the lock but
	// before it writes; the write waits for the caller to close the channel.
	blockWrite chan struct{}
}

func newFakeCache() *fakeCache {
	return &fakeCache{data: make(map[string][]byte)}
}

func (f *fakeCache) WithLock(ctx context.Context, loginSession string, fn func() error) error {
	atomic.AddInt32(&f.withLockCnt, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	return fn()
}

func (f *fakeCache) ReadRaw(loginSession string) ([]byte, bool, error) {
	atomic.AddInt32(&f.readCnt, 1)
	if f.readErr != nil {
		return nil, false, f.readErr
	}
	b, ok := f.data[loginSession]
	if !ok {
		return nil, false, nil
	}
	return b, true, nil
}

func (f *fakeCache) WriteRaw(loginSession string, data []byte) error {
	atomic.AddInt32(&f.writeCnt, 1)
	if f.blockWrite != nil {
		<-f.blockWrite
	}
	if f.writeErr != nil {
		return f.writeErr
	}
	f.data[loginSession] = make([]byte, len(data))
	copy(f.data[loginSession], data)
	return nil
}

func (f *fakeCache) Delete(loginSession string) error {
	atomic.AddInt32(&f.deleteCnt, 1)
	if f.deleteErr != nil {
		return f.deleteErr
	}
	delete(f.data, loginSession)
	return nil
}

type fakeProfileStore struct {
	cfg     config.Config
	path    string
	loadErr error
	updErr  error
	// updateFn, if non-nil, replaces the default Update behavior.
	updateFn func(path string, fn func(*config.Config) error) (config.Config, error)
}

func (f *fakeProfileStore) Load() (config.Config, string, error) {
	if f.loadErr != nil {
		return config.Config{}, "", f.loadErr
	}
	return deepCopyConfig(f.cfg), f.path, nil
}

func (f *fakeProfileStore) Update(path string, fn func(*config.Config) error) (config.Config, error) {
	if f.updateFn != nil {
		return f.updateFn(path, fn)
	}
	if f.updErr != nil {
		return config.Config{}, f.updErr
	}
	cfg := deepCopyConfig(f.cfg)
	if err := fn(&cfg); err != nil {
		return config.Config{}, err
	}
	f.cfg = cfg
	return deepCopyConfig(cfg), nil
}

// deepCopyConfig returns a deep copy of cfg so that callbacks cannot mutate
// the committed fake state through shared map references.
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

type fakeAuthorizer struct {
	code        string
	redirectURI string
	err         error
	called      bool
}

func (f *fakeAuthorizer) Authorize(ctx context.Context) (string, string, error) {
	f.called = true
	return f.code, f.redirectURI, f.err
}

func loginFixedClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

func fixedPKCE() func() (oauth.PKCE, error) {
	return func() (oauth.PKCE, error) {
		return oauth.PKCE{Verifier: "verifier-canary-123", Challenge: "challenge-canary-456"}, nil
	}
}

func fixedState(s string) func() (string, error) {
	return func() (string, error) { return s, nil }
}

func newLoginService(t *testing.T, client *fakeOAuthClient, cache ConsoleCache, store *fakeProfileStore) *LoginService {
	t.Helper()
	return &LoginService{
		oauthClientFactory: func(endpointURL string) (OAuthClient, error) {
			return client, nil
		},
		localAuthorizer: func(c OAuthClient, state, challenge string) Authorizer {
			return &fakeAuthorizer{code: "auth-code-canary", redirectURI: "http://127.0.0.1:0/oauth/callback"}
		},
		remoteAuthorizer: func(c OAuthClient, state, challenge string) Authorizer {
			return &fakeAuthorizer{code: "auth-code-canary", redirectURI: "https://signin.example.com/authorize/oauth/authorize"}
		},
		cache:          cache,
		profileStore:   store,
		clock:          loginFixedClock(time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)),
		pkceGenerator:  fixedPKCE(),
		stateGenerator: fixedState("state-canary-789"),
		confirm: func(profileName, current, new string) (bool, error) {
			return true, nil
		},
	}
}

// --- Tests ---

func TestLoginUsesLocalAndRemoteClientIDs(t *testing.T) {
	const session = "trn:iam::1:user/local"

	t.Run("local uses same-device", func(t *testing.T) {
		client := &fakeOAuthClient{exchangeResp: validTokenResponse(session), endpointURL: DefaultEndpoint}
		store := &fakeProfileStore{cfg: config.DefaultConfig(), path: "/tmp/config.json"}
		svc := newLoginService(t, client, newFakeCache(), store)
		_, err := svc.Login(context.Background(), LoginOptions{Profile: "default"})
		if err != nil {
			t.Fatalf("Login error: %v", err)
		}
		if client.lastReq == nil {
			t.Fatal("ExchangeToken was not called")
		}
		if client.lastReq.ClientID != ClientIDSameDevice {
			t.Errorf("local client_id = %q, want %q", client.lastReq.ClientID, ClientIDSameDevice)
		}
	})

	t.Run("remote uses cross-device", func(t *testing.T) {
		client := &fakeOAuthClient{exchangeResp: validTokenResponse(session), endpointURL: DefaultEndpoint}
		store := &fakeProfileStore{cfg: config.DefaultConfig(), path: "/tmp/config.json"}
		svc := newLoginService(t, client, newFakeCache(), store)
		_, err := svc.Login(context.Background(), LoginOptions{Profile: "default", Remote: true})
		if err != nil {
			t.Fatalf("Login error: %v", err)
		}
		if client.lastReq == nil {
			t.Fatal("ExchangeToken was not called")
		}
		if client.lastReq.ClientID != ClientIDCrossDevice {
			t.Errorf("remote client_id = %q, want %q", client.lastReq.ClientID, ClientIDCrossDevice)
		}
	})
}

func TestLoginSameSessionNeedsNoConfirmation(t *testing.T) {
	const session = "trn:iam::1:user/same"
	cfg := config.DefaultConfig()
	cfg.PutProfile("default", config.Profile{
		Mode:         config.AuthModeConsoleLogin,
		LoginSession: session,
		Region:       "cn-beijing",
	})
	client := &fakeOAuthClient{exchangeResp: validTokenResponse(session), endpointURL: DefaultEndpoint}
	store := &fakeProfileStore{cfg: cfg, path: "/tmp/config.json"}

	confirmCalled := false
	svc := newLoginService(t, client, newFakeCache(), store)
	svc.confirm = func(profileName, current, new string) (bool, error) {
		confirmCalled = true
		return true, nil
	}

	if _, err := svc.Login(context.Background(), LoginOptions{Profile: "default"}); err != nil {
		t.Fatalf("Login error: %v", err)
	}
	if confirmCalled {
		t.Error("confirm callback should not be called for same session")
	}
}

func TestLoginDifferentSessionHonorsConfirmation(t *testing.T) {
	const oldSession = "trn:iam::1:user/old"
	const newSession = "trn:iam::1:user/new"

	cfg := config.DefaultConfig()
	cfg.PutProfile("default", config.Profile{
		Mode:         config.AuthModeConsoleLogin,
		LoginSession: oldSession,
		Region:       "cn-beijing",
	})

	t.Run("rejection cancels without mutation", func(t *testing.T) {
		client := &fakeOAuthClient{exchangeResp: validTokenResponse(newSession), endpointURL: DefaultEndpoint}
		cache := newFakeCache()
		store := &fakeProfileStore{cfg: cfg, path: "/tmp/config.json"}
		svc := newLoginService(t, client, cache, store)
		svc.confirm = func(profileName, current, new string) (bool, error) {
			return false, nil
		}

		_, err := svc.Login(context.Background(), LoginOptions{Profile: "default"})
		if err == nil {
			t.Fatal("expected error when confirmation rejected")
		}
		if atomic.LoadInt32(&cache.writeCnt) != 0 {
			t.Errorf("cache should not be written when confirmation rejected, writeCnt=%d", cache.writeCnt)
		}
		if store.cfg.Profiles["default"].LoginSession != oldSession {
			t.Errorf("profile login-session should remain %q, got %q", oldSession, store.cfg.Profiles["default"].LoginSession)
		}
	})

	t.Run("acceptance proceeds", func(t *testing.T) {
		client := &fakeOAuthClient{exchangeResp: validTokenResponse(newSession), endpointURL: DefaultEndpoint}
		cache := newFakeCache()
		store := &fakeProfileStore{cfg: cfg, path: "/tmp/config.json"}
		svc := newLoginService(t, client, cache, store)
		svc.confirm = func(profileName, current, new string) (bool, error) {
			if current != oldSession || new != newSession {
				t.Errorf("confirm args: current=%q new=%q", current, new)
			}
			return true, nil
		}

		if _, err := svc.Login(context.Background(), LoginOptions{Profile: "default"}); err != nil {
			t.Fatalf("Login error: %v", err)
		}
		if store.cfg.Profiles["default"].LoginSession != newSession {
			t.Errorf("profile login-session should be %q, got %q", newSession, store.cfg.Profiles["default"].LoginSession)
		}
	})
}

func TestLoginPreservesTLSAndDormantStaticFields(t *testing.T) {
	const session = "trn:iam::1:user/preserve"
	cfg := config.DefaultConfig()
	cfg.PutProfile("default", config.Profile{
		AccessKeyID:     "AKLToldaccesskey",
		SecretAccessKey: "old-secret-key",
		SecurityToken:   "old-session-token",
		Region:          "cn-shanghai",
		Endpoint:        "https://tls-cn-shanghai.volces.com",
		TimeoutSeconds:  120,
		CredRef:         "my-cred-ref",
	})

	client := &fakeOAuthClient{exchangeResp: validTokenResponse(session), endpointURL: DefaultEndpoint}
	cache := newFakeCache()
	store := &fakeProfileStore{cfg: cfg, path: "/tmp/config.json"}
	svc := newLoginService(t, client, cache, store)

	if _, err := svc.Login(context.Background(), LoginOptions{Profile: "default"}); err != nil {
		t.Fatalf("Login error: %v", err)
	}

	p := store.cfg.Profiles["default"]
	if p.AccessKeyID != "AKLToldaccesskey" {
		t.Errorf("AccessKeyID overwritten: %q", p.AccessKeyID)
	}
	if p.SecretAccessKey != "old-secret-key" {
		t.Errorf("SecretAccessKey overwritten: %q", p.SecretAccessKey)
	}
	if p.SecurityToken != "old-session-token" {
		t.Errorf("SecurityToken overwritten: %q", p.SecurityToken)
	}
	if p.Endpoint != "https://tls-cn-shanghai.volces.com" {
		t.Errorf("Endpoint overwritten: %q", p.Endpoint)
	}
	if p.TimeoutSeconds != 120 {
		t.Errorf("TimeoutSeconds overwritten: %d", p.TimeoutSeconds)
	}
	if p.CredRef != "my-cred-ref" {
		t.Errorf("CredRef overwritten: %q", p.CredRef)
	}
	if p.Mode != config.AuthModeConsoleLogin {
		t.Errorf("Mode = %q, want %q", p.Mode, config.AuthModeConsoleLogin)
	}
	if p.LoginSession != session {
		t.Errorf("LoginSession = %q, want %q", p.LoginSession, session)
	}
	// Region was not explicitly set and profile already had one; must be preserved.
	if p.Region != "cn-shanghai" {
		t.Errorf("Region = %q, want preserved %q", p.Region, "cn-shanghai")
	}
}

func TestLoginPersistsExplicitTLSRuntimeFields(t *testing.T) {
	const session = "trn:iam::1:user/explicit-runtime"
	cfg := config.DefaultConfig()
	cfg.PutProfile("default", config.Profile{
		Region:   "cn-shanghai",
		Endpoint: "https://tls-cn-shanghai.volces.com",
	})

	client := &fakeOAuthClient{exchangeResp: validTokenResponse(session), endpointURL: DefaultEndpoint}
	store := &fakeProfileStore{cfg: cfg, path: "/tmp/config.json"}
	svc := newLoginService(t, client, newFakeCache(), store)

	result, err := svc.Login(context.Background(), LoginOptions{
		Profile:  "default",
		Region:   "cn-beijing",
		Endpoint: "https://tls-cn-beijing.volces.com",
	})
	if err != nil {
		t.Fatalf("Login error: %v", err)
	}

	p := store.cfg.Profiles["default"]
	if p.Region != "cn-beijing" {
		t.Fatalf("Region = %q, want %q", p.Region, "cn-beijing")
	}
	if p.Endpoint != "https://tls-cn-beijing.volces.com" {
		t.Fatalf("Endpoint = %q, want explicit TLS endpoint", p.Endpoint)
	}
	if result.Region != "cn-beijing" {
		t.Fatalf("result Region = %q, want %q", result.Region, "cn-beijing")
	}
	if result.Endpoint != "https://tls-cn-beijing.volces.com" {
		t.Fatalf("result Endpoint = %q, want explicit TLS endpoint", result.Endpoint)
	}
}

func TestLoginDoesNotInventTLSRuntimeConfig(t *testing.T) {
	const session = "trn:iam::1:user/no-runtime"
	client := &fakeOAuthClient{exchangeResp: validTokenResponse(session), endpointURL: DefaultEndpoint}
	store := &fakeProfileStore{cfg: config.DefaultConfig(), path: "/tmp/config.json"}
	svc := newLoginService(t, client, newFakeCache(), store)

	result, err := svc.Login(context.Background(), LoginOptions{Profile: "new-profile"})
	if err != nil {
		t.Fatalf("Login error: %v", err)
	}

	p := store.cfg.Profiles["new-profile"]
	if p.Region != "" || p.Endpoint != "" {
		t.Fatalf("login invented TLS runtime config: region=%q endpoint=%q", p.Region, p.Endpoint)
	}
	if result.Region != "" {
		t.Fatalf("result Region = %q, want empty", result.Region)
	}
	if result.Endpoint != "" {
		t.Fatalf("result Endpoint = %q, want empty", result.Endpoint)
	}
}

func TestLoginConfigFailureRestoresCacheSnapshot(t *testing.T) {
	const session = "trn:iam::1:user/rollback"
	client := &fakeOAuthClient{exchangeResp: validTokenResponse(session), endpointURL: DefaultEndpoint}
	cache := newFakeCache()
	store := &fakeProfileStore{
		cfg:    config.DefaultConfig(),
		path:   "/tmp/config.json",
		updErr: errors.New("config write failed"),
	}
	svc := newLoginService(t, client, cache, store)

	_, err := svc.Login(context.Background(), LoginOptions{Profile: "default"})
	if err == nil {
		t.Fatal("expected error when config update fails")
	}
	// Cache did not exist before; it should have been deleted (not left with new data).
	if _, ok := cache.data[session]; ok {
		t.Error("cache should be deleted when config fails and cache did not exist before")
	}
}

func TestLoginConfigFailureRestoresExistingSameSessionCache(t *testing.T) {
	const session = "trn:iam::1:user/existing"
	priorCache := []byte(`{"login_session":"prior","access_token":"prior-token"}`)

	client := &fakeOAuthClient{exchangeResp: validTokenResponse(session), endpointURL: DefaultEndpoint}
	cache := newFakeCache()
	cache.data[session] = priorCache

	store := &fakeProfileStore{
		cfg:    config.DefaultConfig(),
		path:   "/tmp/config.json",
		updErr: errors.New("config write failed"),
	}
	svc := newLoginService(t, client, cache, store)

	_, err := svc.Login(context.Background(), LoginOptions{Profile: "default"})
	if err == nil {
		t.Fatal("expected error when config update fails")
	}
	restored, ok := cache.data[session]
	if !ok {
		t.Fatal("existing cache should be restored, not deleted")
	}
	if string(restored) != string(priorCache) {
		t.Errorf("restored cache = %s, want prior bytes %s", restored, priorCache)
	}
}

func TestLoginConcurrentRefreshNeverRollsBackNewerCache(t *testing.T) {
	// Real login-session values contain "/" (e.g. TRNs); FileCache derives the
	// safe SHA-1 lock key so securestore accepts it.
	const session = "trn:iam::123456789012:login/session/test"
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)

	// Two real FileCache instances rooted at the same temp dir.
	dir := t.TempDir()
	cacheA, err := NewFileCache(dir)
	if err != nil {
		t.Fatalf("NewFileCache A: %v", err)
	}
	cacheB, err := NewFileCache(dir)
	if err != nil {
		t.Fatalf("NewFileCache B: %v", err)
	}

	// Seed an old, near-expiry snapshot so the Provider will refresh once it
	// acquires the lock.
	oldSnapshot := makeCacheBytes(session, now.Add(-3599*time.Second), 3600, "old-refresh-token")
	if err := cacheA.WriteRaw(session, oldSnapshot); err != nil {
		t.Fatalf("seed old cache: %v", err)
	}

	// Channels for deterministic coordination.
	loginInConfig := make(chan struct{}) // closed when Login is inside config update
	releaseConfig := make(chan struct{}) // closed to release the blocked config update

	// Login uses a fake client that returns a candidate token.
	loginClient := &fakeOAuthClient{exchangeResp: validTokenResponse(session), endpointURL: DefaultEndpoint}
	blockingStore := &fakeProfileStore{
		cfg:  config.DefaultConfig(),
		path: "/tmp/config.json",
	}
	blockingStore.updateFn = func(path string, fn func(*config.Config) error) (config.Config, error) {
		// Signal that Login has written its candidate and is now in config
		// update, holding the cache lock.
		close(loginInConfig)
		<-releaseConfig
		// Fail the config update to trigger rollback of the candidate cache.
		return config.Config{}, errors.New("config write failed")
	}
	svc := newLoginService(t, loginClient, cacheA, blockingStore)

	loginDone := make(chan error, 1)
	go func() {
		_, err := svc.Login(context.Background(), LoginOptions{Profile: "default"})
		loginDone <- err
	}()

	// Wait until Login is inside the config update (holding the cache lock).
	<-loginInConfig

	// Launch the Provider with real cache B directly and a context carrying the
	// securestore contention observer. It will block on the same cache lock until
	// Login releases it. The observer fires only when the Provider actually
	// contends for the in-process lock held by Login.
	providerContended := make(chan struct{}, 1)
	var obsOnce sync.Once
	providerCtx := securestore.WithLockContentionObserver(context.Background(), func() {
		obsOnce.Do(func() { close(providerContended) })
	})
	refreshClient := &fakeOAuthClient{
		exchangeResp: &ConsoleTokenResponse{
			AccessToken:  json.RawMessage(`{"access_key_id":"AKLTrefreshcanary","secret_access_key":"sk-refresh-canary","session_token":"st-refresh-canary"}`),
			TokenType:    "sts",
			ExpiresIn:    3600,
			RefreshToken: "refresh-token-canary-unique",
			Scope:        Scope,
			IDToken:      validIDToken(session),
		},
		endpointURL: DefaultEndpoint,
	}
	providerDone := make(chan error, 1)
	go func() {
		p := NewProvider(session, cacheB, func(string) (OAuthClient, error) { return refreshClient, nil }, func() time.Time { return now })
		_, err := p.Retrieve(providerCtx)
		providerDone <- err
	}()

	// Do not release Login until the Provider's real lock contention is observed.
	select {
	case <-providerContended:
	case <-time.After(5 * time.Second):
		t.Fatal("provider lock contention not observed in time")
	}

	// Release the Login's config update; it fails, restores the exact old
	// snapshot, and releases the cache lock. The Provider then acquires the
	// lock, re-reads the old (near-expiry) snapshot, and refreshes.
	close(releaseConfig)

	// Both must complete. Use time.After only as a deadlock guard.
	select {
	case err := <-loginDone:
		if err == nil {
			t.Error("login should fail due to config error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("login did not complete in time")
	}
	select {
	case err := <-providerDone:
		if err != nil {
			t.Fatalf("provider refresh should succeed, got error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("provider did not complete in time")
	}

	// The final raw cache must contain the Provider's refresh canaries and be
	// neither the failed-login candidate nor the old snapshot.
	finalBytes, existed, err := cacheB.ReadRaw(session)
	if err != nil {
		t.Fatalf("read final cache: %v", err)
	}
	if !existed {
		t.Fatal("final cache should exist after provider refresh")
	}
	if string(finalBytes) == string(oldSnapshot) {
		t.Error("final cache is the old snapshot; provider refresh did not write")
	}
	var finalCache LoginTokenCache
	if err := json.Unmarshal(finalBytes, &finalCache); err != nil {
		t.Fatalf("unmarshal final cache: %v", err)
	}
	if finalCache.RefreshToken != "refresh-token-canary-unique" {
		t.Errorf("final refresh token = %q, want refresh canary", finalCache.RefreshToken)
	}
	if finalCache.AccessToken == nil || !strings.Contains(string(finalCache.AccessToken), "AKLTrefreshcanary") {
		t.Errorf("final access token does not contain refresh canary: %s", string(finalCache.AccessToken))
	}
}

func TestLoginOutputNeverContainsTokensOrVerifier(t *testing.T) {
	const session = "trn:iam::1:user/redact"
	client := &fakeOAuthClient{exchangeResp: validTokenResponse(session), endpointURL: DefaultEndpoint}
	store := &fakeProfileStore{cfg: config.DefaultConfig(), path: "/tmp/config.json"}
	svc := newLoginService(t, client, newFakeCache(), store)

	result, err := svc.Login(context.Background(), LoginOptions{Profile: "default"})
	if err != nil {
		t.Fatalf("Login error: %v", err)
	}

	canaries := []string{
		"secret-key-canary-xyz",
		"session-token-canary-789",
		"refresh-token-canary",
		"verifier-canary-123",
		"auth-code-canary",
		"state-canary-789",
	}
	resultStr := fmt.Sprintf("%+v", result)
	for _, c := range canaries {
		if strings.Contains(resultStr, c) {
			t.Errorf("LoginResult contains canary %q: %s", c, resultStr)
		}
	}
	if result.Provider != "console-login" {
		t.Errorf("Provider = %q, want console-login", result.Provider)
	}
	if result.Profile != "default" {
		t.Errorf("Profile = %q, want default", result.Profile)
	}
}

func TestFileCacheWriteReadDeleteRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cache, err := NewFileCache(dir)
	if err != nil {
		t.Fatalf("NewFileCache error: %v", err)
	}
	const session = "trn:iam::1:user-filecache"
	data := []byte(`{"login_session":"x","access_token":"y"}`)

	// Initially missing.
	_, existed, err := cache.ReadRaw(session)
	if err != nil {
		t.Fatalf("ReadRaw error: %v", err)
	}
	if existed {
		t.Error("cache should not exist before write")
	}

	// Write and read back.
	if err := cache.WriteRaw(session, data); err != nil {
		t.Fatalf("WriteRaw error: %v", err)
	}
	got, existed, err := cache.ReadRaw(session)
	if err != nil {
		t.Fatalf("ReadRaw error: %v", err)
	}
	if !existed {
		t.Error("cache should exist after write")
	}
	if string(got) != string(data) {
		t.Errorf("ReadRaw = %s, want %s", got, data)
	}

	// Delete is idempotent.
	if err := cache.Delete(session); err != nil {
		t.Fatalf("Delete error: %v", err)
	}
	if err := cache.Delete(session); err != nil {
		t.Fatalf("Delete idempotent error: %v", err)
	}
	_, existed, err = cache.ReadRaw(session)
	if err != nil {
		t.Fatalf("ReadRaw after delete: %v", err)
	}
	if existed {
		t.Error("cache should not exist after delete")
	}
}

func TestFileCacheWithLockSerializesWrites(t *testing.T) {
	dir := t.TempDir()
	cache, err := NewFileCache(dir)
	if err != nil {
		t.Fatalf("NewFileCache error: %v", err)
	}
	// Real login-session values contain "/" (e.g. TRNs); the lock key must be
	// derived from the safe SHA-1 cache filename so securestore accepts it.
	const session = "trn:iam::123456789012:login/session/test"

	var counter int32
	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := cache.WithLock(context.Background(), session, func() error {
				atomic.AddInt32(&counter, 1)
				return nil
			}); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("WithLock error: %v", err)
	}
	if atomic.LoadInt32(&counter) != 10 {
		t.Errorf("counter = %d, want 10", counter)
	}
}

func TestNewLoginServiceSetsDefaults(t *testing.T) {
	svc := NewLoginService(&LoginServiceConfig{})
	if svc.oauthClientFactory == nil {
		t.Error("oauthClientFactory should default")
	}
	if svc.profileStore == nil {
		t.Error("profileStore should default")
	}
	if svc.clock == nil {
		t.Error("clock should default")
	}
	if svc.pkceGenerator == nil {
		t.Error("pkceGenerator should default")
	}
	if svc.stateGenerator == nil {
		t.Error("stateGenerator should default")
	}
}

func TestNewLocalAndRemoteAuthorizerConstructors(t *testing.T) {
	client := &fakeOAuthClient{endpointURL: DefaultEndpoint}
	factory := func() (callbackServer, error) {
		return &fakeCallbackServer{port: 1, redirectURI: "http://127.0.0.1:1/oauth/callback"}, nil
	}
	la := NewLocalAuthorizer(client, factory, &fakeOpener{}, io.Discard, "s", "c")
	if la == nil {
		t.Fatal("NewLocalAuthorizer returned nil")
	}
	ra := NewRemoteAuthorizer(client, strings.NewReader(""), io.Discard, "s", "c")
	if ra == nil {
		t.Fatal("NewRemoteAuthorizer returned nil")
	}
}

func TestMaskAccessKeyShortAndLong(t *testing.T) {
	if got := config.MaskAK("short"); got != "***" {
		t.Errorf("config.MaskAK(short) = %q, want ***", got)
	}
	long := "AKLT1234567890abcdef"
	got := config.MaskAK(long)
	if got != "AKL****def" {
		t.Errorf("config.MaskAK(long) = %q, want AKL****def", got)
	}
}

func TestNewLoginServiceNilConfigIsSafe(t *testing.T) {
	svc := NewLoginService(nil)
	if svc == nil {
		t.Fatal("NewLoginService(nil) returned nil")
	}
	if svc.oauthClientFactory == nil {
		t.Error("oauthClientFactory should default")
	}
	if svc.profileStore == nil {
		t.Error("profileStore should default")
	}
	if svc.clock == nil {
		t.Error("clock should default")
	}
}

func TestLoginNilTokenResponseFailsSafely(t *testing.T) {
	client := &fakeOAuthClient{exchangeResp: nil, endpointURL: DefaultEndpoint}
	store := &fakeProfileStore{cfg: config.DefaultConfig(), path: "/tmp/config.json"}
	svc := newLoginService(t, client, newFakeCache(), store)

	_, err := svc.Login(context.Background(), LoginOptions{Profile: "default"})
	if err == nil {
		t.Fatal("expected error for nil token response")
	}
}

func TestLoginEmptyTokenTypeFailsClosedWithoutMutation(t *testing.T) {
	const session = "trn:iam::1:user:emptytt"
	resp := validTokenResponse(session)
	resp.TokenType = "" // empty token type must fail closed
	client := &fakeOAuthClient{exchangeResp: resp, endpointURL: DefaultEndpoint}
	cache := newFakeCache()
	store := &fakeProfileStore{cfg: config.DefaultConfig(), path: "/tmp/config.json"}
	svc := newLoginService(t, client, cache, store)

	_, err := svc.Login(context.Background(), LoginOptions{Profile: "default"})
	if err == nil {
		t.Fatal("expected error for empty token type")
	}
	// No cache write must occur.
	if atomic.LoadInt32(&cache.writeCnt) != 0 {
		t.Errorf("cache should not be written on empty token type, writeCnt=%d", cache.writeCnt)
	}
	// No config update must occur: the default profile must remain absent.
	if _, ok := store.cfg.Profiles["default"]; ok {
		t.Errorf("config should not be updated on empty token type, profile present: %+v", store.cfg.Profiles["default"])
	}
}

func TestLoginNilAuthorizerFactoryFailsSafely(t *testing.T) {
	const session = "trn:iam::1:user:nilauth"
	client := &fakeOAuthClient{exchangeResp: validTokenResponse(session), endpointURL: DefaultEndpoint}
	store := &fakeProfileStore{cfg: config.DefaultConfig(), path: "/tmp/config.json"}
	svc := newLoginService(t, client, newFakeCache(), store)
	svc.localAuthorizer = nil

	_, err := svc.Login(context.Background(), LoginOptions{Profile: "default"})
	if err == nil {
		t.Fatal("expected error for nil authorizer factory")
	}
}

func TestFileCacheDataFilenameIsSHA1(t *testing.T) {
	dir := t.TempDir()
	cache, err := NewFileCache(dir)
	if err != nil {
		t.Fatalf("NewFileCache: %v", err)
	}
	const session = "trn:iam::1:user:filename"
	data := []byte(`{"login_session":"x"}`)
	if err := cache.WriteRaw(session, data); err != nil {
		t.Fatalf("WriteRaw: %v", err)
	}

	wantName, err := CacheFilename(session)
	if err != nil {
		t.Fatalf("CacheFilename: %v", err)
	}
	// The actual file on disk must be exactly <dir>/<sha1hex>.json.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var jsonFiles []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			jsonFiles = append(jsonFiles, e.Name())
		}
	}
	if len(jsonFiles) != 1 {
		t.Fatalf("expected 1 .json file in cache dir, got %d", len(jsonFiles))
	}
	if jsonFiles[0] != wantName {
		t.Errorf("cache filename = %q, want %q", jsonFiles[0], wantName)
	}
}

func TestFileCacheDirectoryAndFilePermissions(t *testing.T) {
	dir := t.TempDir()
	cache, err := NewFileCache(dir)
	if err != nil {
		t.Fatalf("NewFileCache: %v", err)
	}
	const session = "trn:iam::1:user:perms"
	if err := cache.WriteRaw(session, []byte(`{}`)); err != nil {
		t.Fatalf("WriteRaw: %v", err)
	}

	// Directory must be 0700.
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat dir: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("dir perm = %#o, want 0700", perm)
	}

	// File must be 0600.
	name, err := CacheFilename(session)
	if err != nil {
		t.Fatalf("CacheFilename: %v", err)
	}
	finfo, err := os.Stat(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("Stat file: %v", err)
	}
	if perm := finfo.Mode().Perm(); perm != 0o600 {
		t.Errorf("file perm = %#o, want 0600", perm)
	}
}

func TestFileCacheRejectsSymlinkInReadRaw(t *testing.T) {
	dir := t.TempDir()
	cache, err := NewFileCache(dir)
	if err != nil {
		t.Fatalf("NewFileCache: %v", err)
	}
	const session = "trn:iam::1:user:symlink"

	// Create the real cache file, then replace it with a symlink to another file.
	target := filepath.Join(dir, "target.json")
	if err := os.WriteFile(target, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	name, err := CacheFilename(session)
	if err != nil {
		t.Fatalf("CacheFilename: %v", err)
	}
	linkPath := filepath.Join(dir, name)
	if err := os.Symlink(target, linkPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	_, _, err = cache.ReadRaw(session)
	if err == nil {
		t.Error("ReadRaw should reject symlink path")
	}
}

func TestFileCacheRejectsBroadPermissionFileInReadRaw(t *testing.T) {
	dir := t.TempDir()
	cache, err := NewFileCache(dir)
	if err != nil {
		t.Fatalf("NewFileCache: %v", err)
	}
	const session = "trn:iam::1:user:broad"
	name, err := CacheFilename(session)
	if err != nil {
		t.Fatalf("CacheFilename: %v", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(`{"login_session":"trn:iam::1:user:broad"}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	data, ok, err := cache.ReadRaw(session)
	if err == nil {
		t.Fatalf("ReadRaw with 0644 cache: err=nil, want error; data=%q", data)
	}
	if ok {
		t.Fatalf("ReadRaw with 0644 cache: ok=true, want false")
	}
	if data != nil {
		t.Fatalf("ReadRaw with 0644 cache returned data: %q", data)
	}
	// File mode must not be changed (no chmod).
	info, _ := os.Stat(path)
	if got := info.Mode().Perm(); got != 0o644 {
		t.Fatalf("cache mode changed to %#o, want 0644", got)
	}
}

func TestFileCacheRejectsSymlinkInDelete(t *testing.T) {
	dir := t.TempDir()
	cache, err := NewFileCache(dir)
	if err != nil {
		t.Fatalf("NewFileCache: %v", err)
	}
	const session = "trn:iam::1:user:symlinkdel"

	target := filepath.Join(dir, "target.json")
	if err := os.WriteFile(target, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	name, err := CacheFilename(session)
	if err != nil {
		t.Fatalf("CacheFilename: %v", err)
	}
	linkPath := filepath.Join(dir, name)
	if err := os.Symlink(target, linkPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	err = cache.Delete(session)
	if err == nil {
		t.Error("Delete should reject symlink path")
	}
}

func TestFileCacheWriteRawRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	cache, err := NewFileCache(dir)
	if err != nil {
		t.Fatalf("NewFileCache: %v", err)
	}
	const session = "trn:iam::1:user:symlinkwrite"

	// Create a target file with sentinel bytes, then make the SHA-1 cache path
	// a symlink to it. WriteRaw must reject the symlink and must not modify
	// the target's sentinel bytes.
	const sentinel = "sentinel-bytes-do-not-touch"
	target := filepath.Join(dir, "target.json")
	if err := os.WriteFile(target, []byte(sentinel), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	name, err := CacheFilename(session)
	if err != nil {
		t.Fatalf("CacheFilename: %v", err)
	}
	linkPath := filepath.Join(dir, name)
	if err := os.Symlink(target, linkPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	err = cache.WriteRaw(session, []byte(`{"login_session":"x"}`))
	if err == nil {
		t.Error("WriteRaw should reject symlink path")
	}

	// The target sentinel bytes must be unchanged.
	got, rerr := os.ReadFile(target)
	if rerr != nil {
		t.Fatalf("read target: %v", rerr)
	}
	if string(got) != sentinel {
		t.Errorf("target bytes changed: got %q, want %q", string(got), sentinel)
	}
}

func TestFileCacheWithLockValidatesInputs(t *testing.T) {
	dir := t.TempDir()
	cache, err := NewFileCache(dir)
	if err != nil {
		t.Fatalf("NewFileCache: %v", err)
	}

	t.Run("nil context", func(t *testing.T) {
		//lint:ignore SA1012 verifies WithLock rejects a nil context
		err := cache.WithLock(nil, "session", func() error { return nil })
		if err == nil {
			t.Error("expected error for nil context")
		}
	})

	t.Run("empty session", func(t *testing.T) {
		err := cache.WithLock(context.Background(), "  ", func() error { return nil })
		if err == nil {
			t.Error("expected error for empty session")
		}
	})

	t.Run("nil fn", func(t *testing.T) {
		err := cache.WithLock(context.Background(), "session", nil)
		if err == nil {
			t.Error("expected error for nil fn")
		}
	})
}

func TestFileCacheTwoInstancesSerializeLock(t *testing.T) {
	dir := t.TempDir()
	cacheA, err := NewFileCache(dir)
	if err != nil {
		t.Fatalf("NewFileCache A: %v", err)
	}
	cacheB, err := NewFileCache(dir)
	if err != nil {
		t.Fatalf("NewFileCache B: %v", err)
	}
	const session = "trn:iam::123456789012:login/session/test"

	// A holds the lock; B must block until A releases. Use channels for
	// deterministic coordination.
	aEntered := make(chan struct{})
	releaseA := make(chan struct{})
	aDone := make(chan error, 1)

	go func() {
		aDone <- cacheA.WithLock(context.Background(), session, func() error {
			close(aEntered)
			<-releaseA
			return nil
		})
	}()

	select {
	case <-aEntered:
	case err := <-aDone:
		t.Fatalf("cacheA failed before entering the lock: %v", err)
	case <-time.After(time.Second):
		t.Fatal("cacheA did not enter the lock")
	}

	// Start B with a cancellable context carrying the real securestore
	// contention observer. Wait until B's actual lock contention is observed
	// (proving it reached the lock and is blocked), then cancel B while A still
	// holds the lock. B must return context cancellation without its callback
	// ever entering. Only then release A.
	bContended := make(chan struct{}, 1)
	var bObsOnce sync.Once
	ctx, cancel := context.WithCancel(
		securestore.WithLockContentionObserver(context.Background(), func() {
			bObsOnce.Do(func() { close(bContended) })
		}),
	)
	bCallbackEntered := make(chan struct{})
	bDone := make(chan error, 1)
	go func() {
		bDone <- cacheB.WithLock(ctx, session, func() error {
			close(bCallbackEntered)
			return nil
		})
	}()

	select {
	case <-bContended:
	case <-time.After(5 * time.Second):
		t.Fatal("cacheB lock contention not observed in time")
	}

	// B has reached the lock and is blocked. Cancel it; it must return the
	// context error without ever entering its callback.
	cancel()

	select {
	case err := <-bDone:
		if err == nil {
			t.Fatal("cacheB should return context cancellation error, got nil")
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cacheB should return context.Canceled, got: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cacheB did not return after context cancellation")
	}

	select {
	case <-bCallbackEntered:
		t.Fatal("cacheB callback should not have entered while A held the lock")
	default:
		// Good, callback never entered.
	}

	// Now release A.
	close(releaseA)
	select {
	case err := <-aDone:
		if err != nil {
			t.Fatalf("cacheA WithLock: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cacheA did not return after release")
	}
}

func TestLoginNilClientFromFactoryFailsSafely(t *testing.T) {
	store := &fakeProfileStore{cfg: config.DefaultConfig(), path: "/tmp/config.json"}
	svc := newLoginService(t, &fakeOAuthClient{endpointURL: DefaultEndpoint}, newFakeCache(), store)
	svc.oauthClientFactory = func(string) (OAuthClient, error) { return nil, nil }

	_, err := svc.Login(context.Background(), LoginOptions{Profile: "default"})
	if err == nil {
		t.Fatal("expected error when factory returns nil client")
	}
}

func TestLoginInvalidExpiresInFailsSafely(t *testing.T) {
	const session = "trn:iam::1:user:badexpiry"
	resp := validTokenResponse(session)
	resp.ExpiresIn = 0 // invalid -> CacheExpiration error
	client := &fakeOAuthClient{exchangeResp: resp, endpointURL: DefaultEndpoint}
	store := &fakeProfileStore{cfg: config.DefaultConfig(), path: "/tmp/config.json"}
	svc := newLoginService(t, client, newFakeCache(), store)

	_, err := svc.Login(context.Background(), LoginOptions{Profile: "default"})
	if err == nil {
		t.Fatal("expected error for invalid ExpiresIn")
	}
}

func TestLoginEmptyScopeUsesFrozenScope(t *testing.T) {
	const session = "trn:iam::1:user:emptyscope"
	resp := validTokenResponse(session)
	resp.Scope = "" // server omits scope -> must use frozen Scope
	client := &fakeOAuthClient{exchangeResp: resp, endpointURL: DefaultEndpoint}
	cache := newFakeCache()
	store := &fakeProfileStore{cfg: config.DefaultConfig(), path: "/tmp/config.json"}
	svc := newLoginService(t, client, cache, store)

	if _, err := svc.Login(context.Background(), LoginOptions{Profile: "default"}); err != nil {
		t.Fatalf("Login error: %v", err)
	}
	var stored LoginTokenCache
	if err := json.Unmarshal(cache.data[session], &stored); err != nil {
		t.Fatalf("unmarshal cache: %v", err)
	}
	if stored.Scope != Scope {
		t.Errorf("stored scope = %q, want frozen %q", stored.Scope, Scope)
	}
}

func TestLoginPKCEGeneratorFailureFailsSafely(t *testing.T) {
	const session = "trn:iam::1:user:pkcefail"
	client := &fakeOAuthClient{exchangeResp: validTokenResponse(session), endpointURL: DefaultEndpoint}
	store := &fakeProfileStore{cfg: config.DefaultConfig(), path: "/tmp/config.json"}
	svc := newLoginService(t, client, newFakeCache(), store)
	svc.pkceGenerator = func() (oauth.PKCE, error) { return oauth.PKCE{}, errors.New("pkce boom") }

	_, err := svc.Login(context.Background(), LoginOptions{Profile: "default"})
	if err == nil {
		t.Fatal("expected error when PKCE generator fails")
	}
}

func TestLoginStateGeneratorFailureFailsSafely(t *testing.T) {
	const session = "trn:iam::1:user:statefail"
	client := &fakeOAuthClient{exchangeResp: validTokenResponse(session), endpointURL: DefaultEndpoint}
	store := &fakeProfileStore{cfg: config.DefaultConfig(), path: "/tmp/config.json"}
	svc := newLoginService(t, client, newFakeCache(), store)
	svc.stateGenerator = func() (string, error) { return "", errors.New("state boom") }

	_, err := svc.Login(context.Background(), LoginOptions{Profile: "default"})
	if err == nil {
		t.Fatal("expected error when state generator fails")
	}
}

func TestLoginAuthorizerErrorPropagates(t *testing.T) {
	const session = "trn:iam::1:user:autherr"
	client := &fakeOAuthClient{exchangeResp: validTokenResponse(session), endpointURL: DefaultEndpoint}
	store := &fakeProfileStore{cfg: config.DefaultConfig(), path: "/tmp/config.json"}
	svc := newLoginService(t, client, newFakeCache(), store)
	svc.localAuthorizer = func(c OAuthClient, state, challenge string) Authorizer {
		return &fakeAuthorizer{err: errors.New("authorize failed")}
	}

	_, err := svc.Login(context.Background(), LoginOptions{Profile: "default"})
	if err == nil {
		t.Fatal("expected error when authorizer fails")
	}
}

// TestLoginAuthorizerErrorDoesNotLeakCanary verifies that an error returned by
// the injected Authorizer (which may contain secret material) is wrapped with a
// fixed safe description at the Login boundary, so the canary never appears in
// the returned error text.
func TestLoginAuthorizerErrorDoesNotLeakCanary(t *testing.T) {
	const session = "trn:iam::1:user:authcanary"
	const authCanary = "authorizer-secret-canary-777"
	client := &fakeOAuthClient{exchangeResp: validTokenResponse(session), endpointURL: DefaultEndpoint}
	store := &fakeProfileStore{cfg: config.DefaultConfig(), path: "/tmp/config.json"}
	svc := newLoginService(t, client, newFakeCache(), store)
	svc.localAuthorizer = func(c OAuthClient, state, challenge string) Authorizer {
		return &fakeAuthorizer{err: errors.New(authCanary)}
	}

	_, err := svc.Login(context.Background(), LoginOptions{Profile: "default"})
	if err == nil {
		t.Fatal("expected error when authorizer fails")
	}
	if strings.Contains(err.Error(), authCanary) {
		t.Errorf("Login error leaks authorizer canary: %s", err.Error())
	}
}

// TestLoginExchangeErrorDoesNotLeakCanary verifies that an error returned by
// the injected OAuth client's ExchangeToken is wrapped safely at the Login
// boundary.
func TestLoginExchangeErrorDoesNotLeakCanary(t *testing.T) {
	const exchCanary = "exchange-secret-canary-888"
	client := &fakeOAuthClient{
		exchangeErr: errors.New(exchCanary),
		endpointURL: DefaultEndpoint,
	}
	store := &fakeProfileStore{cfg: config.DefaultConfig(), path: "/tmp/config.json"}
	svc := newLoginService(t, client, newFakeCache(), store)

	_, err := svc.Login(context.Background(), LoginOptions{Profile: "default"})
	if err == nil {
		t.Fatal("expected error when exchange fails")
	}
	if strings.Contains(err.Error(), exchCanary) {
		t.Errorf("Login error leaks exchange canary: %s", err.Error())
	}
}

func TestNewFileCacheEmptyDir(t *testing.T) {
	// Empty dir must return an error; the auth core must not read env/HOME/.volclog.
	_, err := NewFileCache("")
	if err == nil {
		t.Fatal("expected error for empty dir, got nil")
	}
}

func TestLoginErrorsNeverLeakSecrets(t *testing.T) {
	const session = "trn:iam::1:user:leaktest"
	// Use a cache with an invalid refresh token to trigger a reauth error.
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	issuedAt := now.Add(-3599 * time.Second)
	cache := newFakeCache()
	cache.data[session] = makeCacheBytes(session, issuedAt, 3600, "")

	client := newRefreshClient(session)
	p := NewProvider(session, cache, func(string) (OAuthClient, error) { return client, nil }, func() time.Time { return now })

	_, err := p.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected error for missing refresh token")
	}
	errStr := err.Error()
	canaries := []string{
		"secret-key-canary-xyz",
		"session-token-canary-789",
		"refresh-token-canary",
		"verifier-canary-123",
		"auth-code-canary",
	}
	for _, c := range canaries {
		if strings.Contains(errStr, c) {
			t.Errorf("error leaks canary %q: %s", c, errStr)
		}
	}
}

// TestLoginTypedNilDepsFailClosed verifies that typed-nil interface values for
// ConsoleCache, ProfileStore, factory-returned OAuthClient, and Authorizer are
// detected and rejected with an error rather than panicking.
func TestLoginTypedNilDepsFailClosed(t *testing.T) {
	const session = "trn:iam::1:user:typednil"
	baseClient := &fakeOAuthClient{exchangeResp: validTokenResponse(session), endpointURL: DefaultEndpoint}
	baseStore := &fakeProfileStore{cfg: config.DefaultConfig(), path: "/tmp/config.json"}

	cases := []struct {
		name string
		svc  *LoginService
	}{
		{
			name: "typed-nil cache",
			svc: &LoginService{
				oauthClientFactory: func(string) (OAuthClient, error) { return baseClient, nil },
				localAuthorizer:    func(c OAuthClient, s, ch string) Authorizer { return &fakeAuthorizer{code: "c", redirectURI: "r"} },
				cache:              (*fakeCache)(nil),
				profileStore:       baseStore,
				clock:              time.Now,
				pkceGenerator:      fixedPKCE(),
				stateGenerator:     fixedState("s"),
			},
		},
		{
			name: "typed-nil profile store",
			svc: &LoginService{
				oauthClientFactory: func(string) (OAuthClient, error) { return baseClient, nil },
				localAuthorizer:    func(c OAuthClient, s, ch string) Authorizer { return &fakeAuthorizer{code: "c", redirectURI: "r"} },
				cache:              newFakeCache(),
				profileStore:       (*fakeProfileStore)(nil),
				clock:              time.Now,
				pkceGenerator:      fixedPKCE(),
				stateGenerator:     fixedState("s"),
			},
		},
		{
			name: "factory returns typed-nil client",
			svc: &LoginService{
				oauthClientFactory: func(string) (OAuthClient, error) { return (*fakeOAuthClient)(nil), nil },
				localAuthorizer:    func(c OAuthClient, s, ch string) Authorizer { return &fakeAuthorizer{code: "c", redirectURI: "r"} },
				cache:              newFakeCache(),
				profileStore:       baseStore,
				clock:              time.Now,
				pkceGenerator:      fixedPKCE(),
				stateGenerator:     fixedState("s"),
			},
		},
		{
			name: "authorizer factory returns typed-nil authorizer",
			svc: &LoginService{
				oauthClientFactory: func(string) (OAuthClient, error) { return baseClient, nil },
				localAuthorizer:    func(c OAuthClient, s, ch string) Authorizer { return (*fakeAuthorizer)(nil) },
				cache:              newFakeCache(),
				profileStore:       baseStore,
				clock:              time.Now,
				pkceGenerator:      fixedPKCE(),
				stateGenerator:     fixedState("s"),
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.svc.Login(context.Background(), LoginOptions{Profile: "default"})
			if err == nil {
				t.Fatal("expected error for typed-nil dependency")
			}
		})
	}
}

// TestFileCacheTypedNilStoreFailsClosed verifies that a FileCache with a nil
// internal store is rejected before any dereference.
func TestFileCacheTypedNilStoreFailsClosed(t *testing.T) {
	fc := &FileCache{store: nil, dir: t.TempDir()}
	const session = "trn:iam::1:user:nilstore"

	if err := fc.WithLock(context.Background(), session, func() error { return nil }); err == nil {
		t.Error("expected error for nil store in WithLock")
	}
	if _, _, err := fc.ReadRaw(session); err == nil {
		t.Error("expected error for nil store in ReadRaw")
	}
	if err := fc.WriteRaw(session, []byte("x")); err == nil {
		t.Error("expected error for nil store in WriteRaw")
	}
	if err := fc.Delete(session); err == nil {
		t.Error("expected error for nil store in Delete")
	}
}

// TestFileCacheSessionWithSlashLocksAndReads is a real FileCache regression
// proving that a login session containing "/" (a realistic TRN) can acquire the
// per-session lock, write the cache, and read it back. The lock key is derived
// from the safe SHA-1 cache filename so securestore's slash-rejecting key
// validation accepts it.
func TestFileCacheSessionWithSlashLocksAndReads(t *testing.T) {
	dir := t.TempDir()
	cache, err := NewFileCache(dir)
	if err != nil {
		t.Fatalf("NewFileCache error: %v", err)
	}
	const session = "trn:iam::123456789012:login/session/test"
	payload := []byte(`{"login_session":"` + session + `","access_token":"at"}`)

	if err := cache.WithLock(context.Background(), session, func() error {
		return cache.WriteRaw(session, payload)
	}); err != nil {
		t.Fatalf("WithLock+WriteRaw error: %v", err)
	}

	got, existed, err := cache.ReadRaw(session)
	if err != nil {
		t.Fatalf("ReadRaw error: %v", err)
	}
	if !existed {
		t.Fatal("cache should exist after write")
	}
	if string(got) != string(payload) {
		t.Errorf("ReadRaw = %s, want %s", got, payload)
	}
}

// TestFileCacheTwoInstancesSessionWithSlash proves that two real FileCache
// instances rooted at the same directory serialize on the same slash-containing
// session: the second instance must observe the value written by the first.
func TestFileCacheTwoInstancesSessionWithSlash(t *testing.T) {
	dir := t.TempDir()
	cacheA, err := NewFileCache(dir)
	if err != nil {
		t.Fatalf("NewFileCache A: %v", err)
	}
	cacheB, err := NewFileCache(dir)
	if err != nil {
		t.Fatalf("NewFileCache B: %v", err)
	}
	const session = "trn:iam::123456789012:login/session/test"
	payload := []byte(`{"login_session":"` + session + `","access_token":"shared"}`)

	if err := cacheA.WithLock(context.Background(), session, func() error {
		return cacheA.WriteRaw(session, payload)
	}); err != nil {
		t.Fatalf("cacheA write error: %v", err)
	}

	got, existed, err := cacheB.ReadRaw(session)
	if err != nil {
		t.Fatalf("cacheB ReadRaw error: %v", err)
	}
	if !existed {
		t.Fatal("cacheB should see cache written by cacheA")
	}
	if string(got) != string(payload) {
		t.Errorf("cacheB ReadRaw = %s, want %s", got, payload)
	}
}

// TestLoginServiceSessionWithSlash proves the full Login flow works end-to-end
// with a real FileCache and a login session containing "/".
func TestLoginServiceSessionWithSlash(t *testing.T) {
	const session = "trn:iam::123456789012:login/session/test"
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)

	dir := t.TempDir()
	cache, err := NewFileCache(dir)
	if err != nil {
		t.Fatalf("NewFileCache error: %v", err)
	}
	client := &fakeOAuthClient{exchangeResp: validTokenResponse(session), endpointURL: DefaultEndpoint}
	store := &fakeProfileStore{cfg: config.DefaultConfig(), path: filepath.Join(dir, "config.json")}
	svc := &LoginService{
		oauthClientFactory: func(string) (OAuthClient, error) { return client, nil },
		localAuthorizer: func(c OAuthClient, state, challenge string) Authorizer {
			return &fakeAuthorizer{code: "auth-code", redirectURI: "https://signin.example.com/authorize/oauth/authorize"}
		},
		remoteAuthorizer: func(c OAuthClient, state, challenge string) Authorizer {
			return &fakeAuthorizer{code: "auth-code", redirectURI: "https://signin.example.com/authorize/oauth/authorize"}
		},
		cache:          cache,
		profileStore:   store,
		clock:          func() time.Time { return now },
		pkceGenerator:  fixedPKCE(),
		stateGenerator: fixedState("state-1"),
	}

	res, err := svc.Login(context.Background(), LoginOptions{Profile: "default"})
	if err != nil {
		t.Fatalf("Login error: %v", err)
	}
	if res == nil {
		t.Fatal("Login returned nil result")
	}

	// The cache must be readable back through a second FileCache instance.
	cache2, err := NewFileCache(dir)
	if err != nil {
		t.Fatalf("NewFileCache2 error: %v", err)
	}
	_, existed, err := cache2.ReadRaw(session)
	if err != nil {
		t.Fatalf("cache2 ReadRaw error: %v", err)
	}
	if !existed {
		t.Fatal("cache written by Login should be visible to a second FileCache")
	}
}

// TestFileCacheRelativeRootFrozenAfterChdir verifies that a relative cache
// directory is canonicalized to an absolute root at construction time and stays
// anchored there even after the process working directory changes.
func TestFileCacheRelativeRootFrozenAfterChdir(t *testing.T) {
	base := t.TempDir()
	other := t.TempDir()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(base); err != nil {
		t.Fatalf("Chdir base: %v", err)
	}
	defer os.Chdir(origWd)

	cache, err := NewFileCache("cache")
	if err != nil {
		t.Fatalf("NewFileCache error: %v", err)
	}
	if !filepath.IsAbs(cache.dir) {
		t.Fatalf("cache.dir=%q, want absolute path", cache.dir)
	}
	wantRoot := canonicalConsoleTestPath(t, base, "cache")
	if cache.dir != wantRoot {
		t.Fatalf("cache.dir=%q, want %q", cache.dir, wantRoot)
	}

	// Change cwd; the cache must still write to the frozen root.
	if err := os.Chdir(other); err != nil {
		t.Fatalf("Chdir other: %v", err)
	}
	const session = "trn:iam::1:user:frozen"
	if err := cache.WriteRaw(session, []byte("{}")); err != nil {
		t.Fatalf("WriteRaw: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wantRoot)); err != nil {
		t.Fatalf("frozen root was not used: %v", err)
	}
	if _, err := os.Stat(filepath.Join(other, "cache")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cache drifted after chdir: %v", err)
	}
}

// TestFileCacheRejectsDirectSymlinkRoot verifies that a cache root that is
// itself a symlink is rejected at NewFileCache time.
func TestFileCacheRejectsDirectSymlinkRoot(t *testing.T) {
	parent := t.TempDir()
	real := filepath.Join(parent, "real")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatalf("Mkdir real: %v", err)
	}
	link := filepath.Join(parent, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	if _, err := NewFileCache(link); err == nil {
		t.Fatal("expected error for symlink cache root")
	}
}

// TestFileCacheCanonicalizesAncestorSymlink verifies that an existing ancestor
// symlink in the cache root path is canonicalized consistently: both the lock
// root (from securestore) and the data root (FileCache.dir) resolve to the same
// canonical absolute path.
func TestFileCacheCanonicalizesAncestorSymlink(t *testing.T) {
	parent := t.TempDir()
	realParent := filepath.Join(parent, "real")
	if err := os.Mkdir(realParent, 0o700); err != nil {
		t.Fatalf("Mkdir real parent: %v", err)
	}
	aliasParent := filepath.Join(parent, "alias")
	if err := os.Symlink(realParent, aliasParent); err != nil {
		t.Fatalf("Symlink alias parent: %v", err)
	}

	cache, err := NewFileCache(filepath.Join(aliasParent, "store"))
	if err != nil {
		t.Fatalf("NewFileCache error: %v", err)
	}
	wantRoot := canonicalConsoleTestPath(t, realParent, "store")
	if cache.dir != wantRoot {
		t.Fatalf("cache.dir=%q, want canonical %q", cache.dir, wantRoot)
	}

	// Writing must land in the canonical real path, not the alias path.
	const session = "trn:iam::1:user:ancestor"
	if err := cache.WriteRaw(session, []byte("{}")); err != nil {
		t.Fatalf("WriteRaw: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wantRoot)); err != nil {
		t.Fatalf("canonical root was not used: %v", err)
	}
}

func canonicalConsoleTestPath(t *testing.T, existingAncestor string, missingSuffix ...string) string {
	t.Helper()
	canonicalAncestor, err := filepath.EvalSymlinks(existingAncestor)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", existingAncestor, err)
	}
	return filepath.Join(append([]string{canonicalAncestor}, missingSuffix...)...)
}

// TestFileCacheRejectsInvalidRootAtConstruction verifies that an invalid or
// overly broad root (e.g. "/" or the current directory) is rejected at
// NewFileCache time rather than deferred to the first operation.
func TestFileCacheRejectsInvalidRootAtConstruction(t *testing.T) {
	cases := []struct {
		name string
		dir  string
	}{
		{"filesystem root", "/"},
		{"current directory", "."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewFileCache(tc.dir)
			if err == nil {
				t.Fatalf("expected error for invalid root %q", tc.dir)
			}
		})
	}
}
