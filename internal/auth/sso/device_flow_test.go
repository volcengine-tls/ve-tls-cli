package sso

import (
	"bytes"
	"context"
	"errors"
	"math/bits"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth"
	"github.com/volcengine-tls/ve-tls-cli/internal/auth/browser"
	"github.com/volcengine-tls/ve-tls-cli/internal/securestore"
)

// fakeOAuth is a test OAuthAPI that records calls and returns scripted responses.
type fakeOAuth struct {
	registerCalls   int32
	deviceAuthCalls int32
	tokenCalls      int32
	registerResp    *RegisterClientResponse
	deviceAuthResp  *StartDeviceAuthorizationResponse
	tokenResp       *CreateTokenResponse
	registerErr     error
	deviceAuthErr   error
	tokenErr        error
	tokenErrByCall  map[int]error // 1-indexed call number -> error
	tokenRespByCall map[int]*CreateTokenResponse
}

func (f *fakeOAuth) RegisterClient(ctx context.Context, req *RegisterClientRequest) (*RegisterClientResponse, error) {
	atomic.AddInt32(&f.registerCalls, 1)
	return f.registerResp, f.registerErr
}

func (f *fakeOAuth) StartDeviceAuthorization(ctx context.Context, req *StartDeviceAuthorizationRequest) (*StartDeviceAuthorizationResponse, error) {
	atomic.AddInt32(&f.deviceAuthCalls, 1)
	return f.deviceAuthResp, f.deviceAuthErr
}

func (f *fakeOAuth) CreateToken(ctx context.Context, req *CreateTokenRequest) (*CreateTokenResponse, error) {
	n := atomic.AddInt32(&f.tokenCalls, 1)
	if f.tokenErrByCall != nil {
		if err, ok := f.tokenErrByCall[int(n)]; ok {
			return nil, err
		}
	}
	if f.tokenRespByCall != nil {
		if resp, ok := f.tokenRespByCall[int(n)]; ok {
			return resp, nil
		}
	}
	return f.tokenResp, f.tokenErr
}

// fakeCache is a test Cache that stores data in maps.
type fakeCache struct {
	tokens        map[string]*TokenCache
	clients       map[string]*ClientRegistrationCache
	stss          map[string]*STSCache
	tokenReadErr  error
	clientReadErr error
	stsReadErr    error
}

func newFakeCache() *fakeCache {
	return &fakeCache{
		tokens:  map[string]*TokenCache{},
		clients: map[string]*ClientRegistrationCache{},
		stss:    map[string]*STSCache{},
	}
}

func (c *fakeCache) WithTokenLock(ctx context.Context, startURL, sessionName string, fn func() error) error {
	return fn()
}
func (c *fakeCache) ReadToken(startURL, sessionName string) (*TokenCache, error) {
	if c.tokenReadErr != nil {
		return nil, c.tokenReadErr
	}
	k, _ := tokenKey(startURL, sessionName)
	v, ok := c.tokens[k]
	if !ok {
		return nil, errFakeMissing
	}
	return v, nil
}
func (c *fakeCache) WriteToken(cache *TokenCache) error {
	k, _ := tokenKey(cache.StartURL, cache.SessionName)
	c.tokens[k] = cache
	return nil
}
func (c *fakeCache) DeleteToken(startURL, sessionName string) error {
	k, _ := tokenKey(startURL, sessionName)
	delete(c.tokens, k)
	return nil
}

func (c *fakeCache) WithClientLock(ctx context.Context, startURL, region string, scopes []string, sessionName string, fn func() error) error {
	return fn()
}
func (c *fakeCache) ReadClient(startURL, region string, scopes []string, sessionName string) (*ClientRegistrationCache, error) {
	if c.clientReadErr != nil {
		return nil, c.clientReadErr
	}
	k, _ := clientKey(startURL, region, scopes, sessionName)
	v, ok := c.clients[k]
	if !ok {
		return nil, errFakeMissing
	}
	return v, nil
}
func (c *fakeCache) WriteClient(cache *ClientRegistrationCache, startURL, region string, scopes []string, sessionName string) error {
	k, _ := clientKey(startURL, region, scopes, sessionName)
	c.clients[k] = cache
	return nil
}
func (c *fakeCache) DeleteClient(startURL, region string, scopes []string, sessionName string) error {
	k, _ := clientKey(startURL, region, scopes, sessionName)
	delete(c.clients, k)
	return nil
}

func (c *fakeCache) WithSTSLock(ctx context.Context, sessionName, accountID, roleName string, fn func() error) error {
	return fn()
}
func (c *fakeCache) ReadSTS(sessionName, accountID, roleName string) (*STSCache, error) {
	if c.stsReadErr != nil {
		return nil, c.stsReadErr
	}
	k, _ := stsKey(sessionName, accountID, roleName)
	v, ok := c.stss[k]
	if !ok {
		return nil, errFakeMissing
	}
	return v, nil
}
func (c *fakeCache) WriteSTS(cache *STSCache) error {
	k, _ := stsKey(cache.SessionName, cache.AccountID, cache.RoleName)
	c.stss[k] = cache
	return nil
}
func (c *fakeCache) DeleteSTS(sessionName, accountID, roleName string) error {
	k, _ := stsKey(sessionName, accountID, roleName)
	delete(c.stss, k)
	return nil
}

var errFakeMissing = securestore.ErrMissing

// fakeOpener is a test browser.Opener.
type fakeOpener struct {
	openErr error
	opened  string
}

func (o *fakeOpener) Open(ctx context.Context, url string) error {
	o.opened = url
	return o.openErr
}

// noSleepSleeper returns immediately.
func noSleepSleeper(ctx context.Context, d time.Duration) error {
	return nil
}

func newDeviceFlow(t *testing.T, oauth OAuthAPI, cache Cache, opts ...func(*DeviceFlowConfig)) *DeviceFlow {
	t.Helper()
	cfg := &DeviceFlowConfig{
		OAuth:       oauth,
		Cache:       cache,
		Clock:       func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
		Sleeper:     noSleepSleeper,
		StartURL:    "https://example.volccloudidentity.com",
		SessionName: "test-session",
		Region:      "cn-beijing",
		Scopes:      []string{ScopeAccountAccess, ScopeOfflineAccess},
		ClientName:  "volclog",
	}
	for _, o := range opts {
		o(cfg)
	}
	return NewDeviceFlow(cfg)
}

func TestExplicitLoginAlwaysStartsDeviceAuthorization(t *testing.T) {
	oauth := &fakeOAuth{
		registerResp: &RegisterClientResponse{ClientID: "cid", ClientSecret: "csec"},
		deviceAuthResp: &StartDeviceAuthorizationResponse{
			DeviceCode: "dc", UserCode: "UC123",
			VerificationURI:         "https://example.com/verify",
			VerificationURIComplete: "https://example.com/verify?user_code=UC123",
			ExpiresIn:               600, Interval: 5,
		},
		tokenResp: &CreateTokenResponse{AccessToken: "at", TokenType: "Bearer", ExpiresIn: 3600, RefreshToken: "rt"},
	}
	cache := newFakeCache()
	// Pre-populate a valid token cache to prove login still starts device auth.
	cache.WriteToken(&TokenCache{
		StartURL: "https://example.volccloudidentity.com", SessionName: "test-session",
		AccessToken: "old-at", ExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339),
		ClientID: "old-cid", ClientSecret: "old-csec",
	})

	df := newDeviceFlow(t, oauth, cache)
	if _, err := df.Login(context.Background()); err != nil {
		t.Fatalf("login failed: %v", err)
	}
	if atomic.LoadInt32(&oauth.deviceAuthCalls) != 1 {
		t.Fatalf("device auth calls = %d, want 1", oauth.deviceAuthCalls)
	}
}

func TestExplicitLoginReusesValidClientRegistration(t *testing.T) {
	oauth := &fakeOAuth{
		deviceAuthResp: &StartDeviceAuthorizationResponse{
			DeviceCode: "dc", UserCode: "UC123",
			VerificationURIComplete: "https://example.com/verify?user_code=UC123",
			ExpiresIn:               600, Interval: 5,
		},
		tokenResp: &CreateTokenResponse{AccessToken: "at", TokenType: "Bearer", ExpiresIn: 3600},
	}
	cache := newFakeCache()
	// Pre-populate a valid client registration.
	cache.WriteClient(&ClientRegistrationCache{
		ClientName: "volclog", ClientID: "cached-cid", ClientSecret: "cached-csec",
		ClientSecretExpiresAt: 0,
	}, "https://example.volccloudidentity.com", "cn-beijing", []string{ScopeAccountAccess, ScopeOfflineAccess}, "test-session")

	df := newDeviceFlow(t, oauth, cache)
	if _, err := df.Login(context.Background()); err != nil {
		t.Fatalf("login failed: %v", err)
	}
	if atomic.LoadInt32(&oauth.registerCalls) != 0 {
		t.Fatalf("register calls = %d, want 0 (should reuse cached client)", oauth.registerCalls)
	}
}

func TestExplicitLoginRegistersWhenClientExpired(t *testing.T) {
	oauth := &fakeOAuth{
		registerResp: &RegisterClientResponse{ClientID: "new-cid", ClientSecret: "new-csec"},
		deviceAuthResp: &StartDeviceAuthorizationResponse{
			DeviceCode: "dc", UserCode: "UC123",
			VerificationURIComplete: "https://example.com/verify?user_code=UC123",
			ExpiresIn:               600, Interval: 5,
		},
		tokenResp: &CreateTokenResponse{AccessToken: "at", TokenType: "Bearer", ExpiresIn: 3600},
	}
	cache := newFakeCache()
	// Pre-populate an EXPIRED client registration (expiry in the past, ms).
	cache.WriteClient(&ClientRegistrationCache{
		ClientName: "volclog", ClientID: "old-cid", ClientSecret: "old-csec",
		ClientSecretExpiresAt: 1000, // way in the past
	}, "https://example.volccloudidentity.com", "cn-beijing", []string{ScopeAccountAccess, ScopeOfflineAccess}, "test-session")

	df := newDeviceFlow(t, oauth, cache)
	if _, err := df.Login(context.Background()); err != nil {
		t.Fatalf("login failed: %v", err)
	}
	if atomic.LoadInt32(&oauth.registerCalls) != 1 {
		t.Fatalf("register calls = %d, want 1 (should re-register)", oauth.registerCalls)
	}
}

func TestNoBrowserEmitsVerificationURL(t *testing.T) {
	oauth := &fakeOAuth{
		registerResp: &RegisterClientResponse{ClientID: "cid", ClientSecret: "csec"},
		deviceAuthResp: &StartDeviceAuthorizationResponse{
			DeviceCode: "dc", UserCode: "UC123",
			VerificationURIComplete: "https://example.com/verify?user_code=UC123",
			ExpiresIn:               600, Interval: 5,
		},
		tokenResp: &CreateTokenResponse{AccessToken: "at", TokenType: "Bearer", ExpiresIn: 3600},
	}
	cache := newFakeCache()
	var buf bytes.Buffer
	opener := &fakeOpener{}
	df := newDeviceFlow(t, oauth, cache, func(c *DeviceFlowConfig) {
		c.NoBrowser = true
		c.Progress = &buf
		c.Browser = opener
	})
	if _, err := df.Login(context.Background()); err != nil {
		t.Fatalf("login failed: %v", err)
	}
	if opener.opened != "" {
		t.Fatalf("browser should not open when NoBrowser is set, opened %q", opener.opened)
	}
	if !strings.Contains(buf.String(), "https://example.com/verify") {
		t.Fatalf("progress missing verification URL: %q", buf.String())
	}
}

func TestBrowserFailureStillEmitsFallbackURL(t *testing.T) {
	oauth := &fakeOAuth{
		registerResp: &RegisterClientResponse{ClientID: "cid", ClientSecret: "csec"},
		deviceAuthResp: &StartDeviceAuthorizationResponse{
			DeviceCode: "dc", UserCode: "UC123",
			VerificationURIComplete: "https://example.com/verify?user_code=UC123",
			ExpiresIn:               600, Interval: 5,
		},
		tokenResp: &CreateTokenResponse{AccessToken: "at", TokenType: "Bearer", ExpiresIn: 3600},
	}
	cache := newFakeCache()
	var buf bytes.Buffer
	opener := &fakeOpener{openErr: errors.New("browser launch failed: https://example.com/verify?user_code=SECRET")}
	df := newDeviceFlow(t, oauth, cache, func(c *DeviceFlowConfig) {
		c.Progress = &buf
		c.Browser = opener
	})
	if _, err := df.Login(context.Background()); err != nil {
		t.Fatalf("login failed: %v", err)
	}
	// The fallback URL must still be in the progress output.
	if !strings.Contains(buf.String(), "https://example.com/verify") {
		t.Fatalf("progress missing fallback URL: %q", buf.String())
	}
	// The opener error text (which contains the URL/secret) must NOT be echoed.
	if strings.Contains(buf.String(), "browser launch failed") {
		t.Fatalf("progress leaked opener error: %q", buf.String())
	}
}

func TestDeviceFlowHandlesAuthorizationPendingAndSlowDown(t *testing.T) {
	oauth := &fakeOAuth{
		registerResp: &RegisterClientResponse{ClientID: "cid", ClientSecret: "csec"},
		deviceAuthResp: &StartDeviceAuthorizationResponse{
			DeviceCode: "dc", UserCode: "UC123",
			VerificationURIComplete: "https://example.com/verify?user_code=UC123",
			ExpiresIn:               600, Interval: 5,
		},
		tokenRespByCall: map[int]*CreateTokenResponse{
			3: {AccessToken: "at", TokenType: "Bearer", ExpiresIn: 3600},
		},
		tokenErrByCall: map[int]error{
			1: &OAuthAPIError{StatusCode: 400, Code: "authorization_pending"},
			2: &OAuthAPIError{StatusCode: 400, Code: "slow_down"},
		},
	}
	cache := newFakeCache()
	df := newDeviceFlow(t, oauth, cache)
	if _, err := df.Login(context.Background()); err != nil {
		t.Fatalf("login failed: %v", err)
	}
	if atomic.LoadInt32(&oauth.tokenCalls) != 3 {
		t.Fatalf("token calls = %d, want 3", oauth.tokenCalls)
	}
}

func TestDeviceFlowStopsAtDeadlineAndContextCancellation(t *testing.T) {
	t.Run("deadline", func(t *testing.T) {
		now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		var mu sync.Mutex
		clock := func() time.Time {
			mu.Lock()
			defer mu.Unlock()
			return now
		}
		// Sleeper advances the clock so the deadline is eventually reached.
		sleeper := func(ctx context.Context, d time.Duration) error {
			mu.Lock()
			now = now.Add(d)
			mu.Unlock()
			return nil
		}
		oauth := &fakeOAuth{
			registerResp: &RegisterClientResponse{ClientID: "cid", ClientSecret: "csec"},
			deviceAuthResp: &StartDeviceAuthorizationResponse{
				DeviceCode: "dc", UserCode: "UC123",
				VerificationURIComplete: "https://example.com/verify?user_code=UC123",
				ExpiresIn:               2, Interval: 1, // 2 second deadline, 1 second interval
			},
			tokenErr: &OAuthAPIError{StatusCode: 400, Code: "authorization_pending"},
		}
		cache := newFakeCache()
		df := newDeviceFlow(t, oauth, cache, func(c *DeviceFlowConfig) {
			c.Clock = clock
			c.Sleeper = sleeper
		})
		_, err := df.Login(context.Background())
		if err == nil {
			t.Fatal("expected timeout error")
		}
		if !strings.Contains(err.Error(), "timed out") {
			t.Fatalf("expected timeout error, got %v", err)
		}
	})

	t.Run("context_cancellation", func(t *testing.T) {
		oauth := &fakeOAuth{
			registerResp: &RegisterClientResponse{ClientID: "cid", ClientSecret: "csec"},
			deviceAuthResp: &StartDeviceAuthorizationResponse{
				DeviceCode: "dc", UserCode: "UC123",
				VerificationURIComplete: "https://example.com/verify?user_code=UC123",
				ExpiresIn:               600, Interval: 5,
			},
			tokenErr: &OAuthAPIError{StatusCode: 400, Code: "authorization_pending"},
		}
		cache := newFakeCache()
		// Sleeper that always returns context.Canceled.
		cancelSleeper := func(ctx context.Context, d time.Duration) error {
			return context.Canceled
		}
		df := newDeviceFlow(t, oauth, cache, func(c *DeviceFlowConfig) {
			c.Sleeper = cancelSleeper
		})
		_, err := df.Login(context.Background())
		if err == nil {
			t.Fatal("expected context cancellation error")
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	})
}

func TestDeviceFlowPersistsOnlyAfterSuccess(t *testing.T) {
	t.Run("token_error_no_persist", func(t *testing.T) {
		oauth := &fakeOAuth{
			registerResp: &RegisterClientResponse{ClientID: "cid", ClientSecret: "csec"},
			deviceAuthResp: &StartDeviceAuthorizationResponse{
				DeviceCode: "dc", UserCode: "UC123",
				VerificationURIComplete: "https://example.com/verify?user_code=UC123",
				ExpiresIn:               600, Interval: 5,
			},
			tokenErr: errors.New("fatal token error"),
		}
		cache := newFakeCache()
		df := newDeviceFlow(t, oauth, cache)
		_, err := df.Login(context.Background())
		if err == nil {
			t.Fatal("expected error")
		}
		// No token cache should have been written.
		if len(cache.tokens) != 0 {
			t.Fatalf("token cache should not be persisted on failure, got %d", len(cache.tokens))
		}
	})

	t.Run("success_persists", func(t *testing.T) {
		oauth := &fakeOAuth{
			registerResp: &RegisterClientResponse{ClientID: "cid", ClientSecret: "csec", ClientIDIssuedAt: 1000, ClientSecretExpiresAt: 0},
			deviceAuthResp: &StartDeviceAuthorizationResponse{
				DeviceCode: "dc", UserCode: "UC123",
				VerificationURIComplete: "https://example.com/verify?user_code=UC123",
				ExpiresIn:               600, Interval: 5,
			},
			tokenResp: &CreateTokenResponse{AccessToken: "at", TokenType: "Bearer", ExpiresIn: 3600, RefreshToken: "rt"},
		}
		cache := newFakeCache()
		df := newDeviceFlow(t, oauth, cache)
		tc, err := df.Login(context.Background())
		if err != nil {
			t.Fatalf("login failed: %v", err)
		}
		if tc.ClientID != "cid" || tc.ClientSecret != "csec" {
			t.Fatal("token cache should copy the exact client registration used")
		}
		if len(cache.tokens) != 1 {
			t.Fatalf("token cache should be persisted on success, got %d", len(cache.tokens))
		}
	})
}

func TestDeviceFlowProgressNeverContainsSecrets(t *testing.T) {
	secretAccessToken := "SECRET-ACCESS-TOKEN"
	secretRefreshToken := "SECRET-REFRESH-TOKEN"
	secretClientSecret := "SECRET-CLIENT-SECRET"
	oauth := &fakeOAuth{
		registerResp: &RegisterClientResponse{ClientID: "cid", ClientSecret: secretClientSecret},
		deviceAuthResp: &StartDeviceAuthorizationResponse{
			DeviceCode: "dc", UserCode: "UC123",
			VerificationURIComplete: "https://example.com/verify?user_code=UC123",
			ExpiresIn:               600, Interval: 5,
		},
		tokenResp: &CreateTokenResponse{AccessToken: secretAccessToken, TokenType: "Bearer", ExpiresIn: 3600, RefreshToken: secretRefreshToken},
	}
	cache := newFakeCache()
	var buf bytes.Buffer
	df := newDeviceFlow(t, oauth, cache, func(c *DeviceFlowConfig) {
		c.Progress = &buf
		c.NoBrowser = true
	})
	_, err := df.Login(context.Background())
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, secretAccessToken) {
		t.Fatal("progress leaked access token")
	}
	if strings.Contains(out, secretRefreshToken) {
		t.Fatal("progress leaked refresh token")
	}
	if strings.Contains(out, secretClientSecret) {
		t.Fatal("progress leaked client secret")
	}
}

func TestVerificationURLConstruction(t *testing.T) {
	// Prefer verification_uri_complete.
	resp := &StartDeviceAuthorizationResponse{
		VerificationURI:         "https://example.com/verify",
		VerificationURIComplete: "https://example.com/verify?user_code=ABC",
		UserCode:                "ABC",
	}
	if got := verificationURL(resp); got != "https://example.com/verify?user_code=ABC" {
		t.Fatalf("got %q", got)
	}
	// Construct from verification_uri + user_code when complete is missing.
	resp = &StartDeviceAuthorizationResponse{
		VerificationURI: "https://example.com/verify?foo=bar",
		UserCode:        "ABC",
	}
	got := verificationURL(resp)
	if !strings.Contains(got, "user_code=ABC") || !strings.Contains(got, "foo=bar") {
		t.Fatalf("constructed URL missing params: %q", got)
	}
}

// Compile-time assertion that fakes satisfy the interfaces.
var (
	_ OAuthAPI       = (*fakeOAuth)(nil)
	_ Cache          = (*fakeCache)(nil)
	_ browser.Opener = (*fakeOpener)(nil)
)

func TestDefaultSleeper(t *testing.T) {
	// Zero duration returns immediately.
	if err := defaultSleeper(context.Background(), 0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Negative duration returns immediately.
	if err := defaultSleeper(context.Background(), -1*time.Second); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Canceled context returns context error.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := defaultSleeper(ctx, time.Hour); err == nil {
		t.Fatal("expected error for canceled context")
	}
}

func TestNewDeviceFlowDefaults(t *testing.T) {
	df := NewDeviceFlow(nil)
	if df == nil {
		t.Fatal("expected non-nil DeviceFlow")
	}
	if df.clock == nil {
		t.Fatal("clock should default to time.Now")
	}
	if df.sleeper == nil {
		t.Fatal("sleeper should default to defaultSleeper")
	}
	if df.clientName != "volclog" {
		t.Fatalf("clientName = %q, want volclog", df.clientName)
	}
}

func TestDeviceFlowNilDeps(t *testing.T) {
	df := NewDeviceFlow(&DeviceFlowConfig{
		StartURL:    "https://example.com",
		SessionName: "s1",
	})
	_, err := df.Login(context.Background())
	if err == nil {
		t.Fatal("expected error for nil oauth/cache")
	}
}

func TestDeviceFlowInvalidScopes(t *testing.T) {
	df := NewDeviceFlow(&DeviceFlowConfig{
		OAuth:       &fakeOAuth{},
		Cache:       newFakeCache(),
		StartURL:    "https://example.com",
		SessionName: "s1",
		Scopes:      []string{""},
	})
	_, err := df.Login(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid scopes")
	}
}

// TestDeviceFlowDeadlineBoundsPolling verifies that with ExpiresIn=1 and
// Interval=5, exactly zero CreateToken calls are made after the one-second
// deadline. Uses a deterministic fake clock and sleeper; no real sleep.
func TestDeviceFlowDeadlineBoundsPolling(t *testing.T) {
	var current time.Time
	clock := func() time.Time { return current }
	// Sleeper advances the fake clock by the requested duration (capped at the
	// remaining lifetime by the production code).
	sleeper := func(ctx context.Context, d time.Duration) error {
		current = current.Add(d)
		return nil
	}

	oauth := &fakeOAuth{
		registerResp: &RegisterClientResponse{ClientID: "cid", ClientSecret: "csec"},
		deviceAuthResp: &StartDeviceAuthorizationResponse{
			DeviceCode: "dc", UserCode: "UC123",
			VerificationURIComplete: "https://example.com/verify?user_code=UC123",
			ExpiresIn:               1, Interval: 5, // 1s deadline, 5s interval
		},
		tokenErr: &OAuthAPIError{StatusCode: 400, Code: "authorization_pending"},
	}
	cache := newFakeCache()
	df := newDeviceFlow(t, oauth, cache, func(c *DeviceFlowConfig) {
		c.Clock = clock
		c.Sleeper = sleeper
	})

	_, err := df.Login(context.Background())
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout error, got %v", err)
	}
	// The first (and only) sleep is bounded to the 1s remaining lifetime, so
	// after the sleeper returns the deadline has arrived and no CreateToken
	// call should have been made.
	if got := atomic.LoadInt32(&oauth.tokenCalls); got != 0 {
		t.Fatalf("CreateToken called %d times after deadline, want 0", got)
	}
}

// TestDeviceFlowCorruptClientCacheFailsClosed verifies that a corrupt client
// registration cache fails closed: RegisterClient is not called and the file is
// not overwritten.
func TestDeviceFlowCorruptClientCacheFailsClosed(t *testing.T) {
	oauth := &fakeOAuth{
		registerResp: &RegisterClientResponse{ClientID: "cid", ClientSecret: "csec"},
	}
	cache := newFakeCache()
	// Inject a corrupt read error that is not ErrMissing.
	cache.clientReadErr = errors.New("simulated corrupt cache")
	df := newDeviceFlow(t, oauth, cache)

	_, err := df.Login(context.Background())
	if err == nil {
		t.Fatal("expected error for corrupt client cache")
	}
	// RegisterClient must not have been called.
	if got := atomic.LoadInt32(&oauth.registerCalls); got != 0 {
		t.Fatalf("RegisterClient called %d times, want 0", got)
	}
}

// TestDeviceFlowIncompleteClientCacheFailsClosed verifies that a parsed-but-
// incomplete client registration cache (missing client id/secret) fails closed
// without registering or overwriting.
func TestDeviceFlowIncompleteClientCacheFailsClosed(t *testing.T) {
	oauth := &fakeOAuth{
		registerResp: &RegisterClientResponse{ClientID: "cid", ClientSecret: "csec"},
	}
	cache := newFakeCache()
	// Pre-populate an incomplete client registration (missing secret).
	cache.WriteClient(&ClientRegistrationCache{
		ClientID: "cid-but-no-secret",
	}, "https://example.volccloudidentity.com", "cn-beijing",
		[]string{ScopeAccountAccess, ScopeOfflineAccess}, "test-session")

	df := newDeviceFlow(t, oauth, cache)
	_, err := df.Login(context.Background())
	if err == nil {
		t.Fatal("expected error for incomplete client cache")
	}
	// RegisterClient must not have been called.
	if got := atomic.LoadInt32(&oauth.registerCalls); got != 0 {
		t.Fatalf("RegisterClient called %d times, want 0", got)
	}
}

// TestDeviceFlowRejectsExpiredNewClientRegistration verifies that a newly
// registered client with an already-expired ClientSecretExpiresAt is rejected:
// zero WriteClient, zero StartDeviceAuthorization, and zero token persistence.
func TestDeviceFlowRejectsExpiredNewClientRegistration(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	// ClientSecretExpiresAt is in the past (seconds).
	oauth := &fakeOAuth{
		registerResp: &RegisterClientResponse{
			ClientID:              "cid",
			ClientSecret:          "csec",
			ClientSecretExpiresAt: 1000, // way in the past
		},
	}
	cache := newFakeCache()
	df := newDeviceFlow(t, oauth, cache, func(c *DeviceFlowConfig) {
		c.Clock = clock
	})

	_, err := df.Login(context.Background())
	if err == nil {
		t.Fatal("expected error for expired client registration")
	}
	// Zero WriteClient: no client cache should have been written.
	if len(cache.clients) != 0 {
		t.Fatalf("client cache should not be persisted, got %d", len(cache.clients))
	}
	// Zero StartDeviceAuthorization.
	if got := atomic.LoadInt32(&oauth.deviceAuthCalls); got != 0 {
		t.Fatalf("device auth calls = %d, want 0", got)
	}
	// Zero token persistence.
	if len(cache.tokens) != 0 {
		t.Fatalf("token cache should not be persisted, got %d", len(cache.tokens))
	}
}

// registerAdvancingOAuth wraps an OAuthAPI and flips a shared flag when
// RegisterClient is called, simulating the call advancing wall-clock time.
type registerAdvancingOAuth struct {
	OAuthAPI
	advanced *bool
}

func (r *registerAdvancingOAuth) RegisterClient(ctx context.Context, req *RegisterClientRequest) (*RegisterClientResponse, error) {
	*r.advanced = true
	return r.OAuthAPI.RegisterClient(ctx, req)
}

// TestDeviceFlowValidatesNewClientRegistrationWithFreshClock verifies that a
// newly registered response is validated using a fresh clock read AFTER
// RegisterClient returns, not the pre-call timestamp. If the call advances time
// past the response's short expiry, the response is rejected and nothing is
// persisted.
func TestDeviceFlowValidatesNewClientRegistrationWithFreshClock(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	advanced := false
	clock := func() time.Time {
		if advanced {
			// RegisterClient "took 40s"; the response's 10s expiry is now past.
			return base.Add(40 * time.Second)
		}
		return base
	}
	oauth := &fakeOAuth{
		registerResp: &RegisterClientResponse{
			ClientID:              "cid",
			ClientSecret:          "csec",
			ClientSecretExpiresAt: base.Add(10 * time.Second).Unix(), // valid at base, expired at base+40s
		},
	}
	cache := newFakeCache()
	df := newDeviceFlow(t, &registerAdvancingOAuth{OAuthAPI: oauth, advanced: &advanced}, cache, func(c *DeviceFlowConfig) {
		c.Clock = clock
	})

	_, err := df.Login(context.Background())
	if err == nil {
		t.Fatal("expected error when RegisterClient advances time past expiry")
	}
	// No client cache should have been written.
	if len(cache.clients) != 0 {
		t.Fatalf("client cache should not be persisted, got %d", len(cache.clients))
	}
	// No device authorization should have been started.
	if got := atomic.LoadInt32(&oauth.deviceAuthCalls); got != 0 {
		t.Fatalf("device auth calls = %d, want 0", got)
	}
}

// TestDeviceFlowCanonicalizesSessionAndRegion verifies that a login with
// surrounding whitespace in session name and region trims them once and uses
// the canonical values for the persisted token cache so a Provider configured
// with the trimmed values immediately accepts it.
func TestDeviceFlowCanonicalizesSessionAndRegion(t *testing.T) {
	oauth := &fakeOAuth{
		registerResp: &RegisterClientResponse{ClientID: "cid", ClientSecret: "csec"},
		deviceAuthResp: &StartDeviceAuthorizationResponse{
			DeviceCode: "dc", UserCode: "UC123",
			VerificationURIComplete: "https://example.com/verify?user_code=UC123",
			ExpiresIn:               600, Interval: 5,
		},
		tokenResp: &CreateTokenResponse{AccessToken: "at", TokenType: "Bearer", ExpiresIn: 3600, RefreshToken: "rt"},
	}
	cache := newFakeCache()
	df := newDeviceFlow(t, oauth, cache, func(c *DeviceFlowConfig) {
		c.SessionName = "  corp  "
		c.Region = "  cn-beijing  "
	})

	tc, err := df.Login(context.Background())
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	// Session name and region must be trimmed in the persisted token cache.
	if tc.SessionName != "corp" {
		t.Fatalf("session name = %q, want %q", tc.SessionName, "corp")
	}
	if tc.Region != "cn-beijing" {
		t.Fatalf("region = %q, want %q", tc.Region, "cn-beijing")
	}
	// A Provider configured with the trimmed values must immediately accept
	// the token cache (InspectTokenCache must pass).
	if _, err := InspectTokenCache(tc, "https://example.volccloudidentity.com", "corp", "cn-beijing", time.Now()); err != nil {
		t.Fatalf("token cache should be valid for trimmed session/region: %v", err)
	}
}

// TestDeviceFlowWriteClientFailureFailsClosed verifies that a WriteClient
// failure fails closed: no StartDeviceAuthorization, safe error, cause
// preserved.
func TestDeviceFlowWriteClientFailureFailsClosed(t *testing.T) {
	oauth := &fakeOAuth{
		registerResp: &RegisterClientResponse{ClientID: "cid", ClientSecret: "csec"},
	}
	cache := &failingWriteClientCache{Cache: newFakeCache()}
	df := newDeviceFlow(t, oauth, cache)

	_, err := df.Login(context.Background())
	if err == nil {
		t.Fatal("expected error for WriteClient failure")
	}
	// No StartDeviceAuthorization should have been called.
	if got := atomic.LoadInt32(&oauth.deviceAuthCalls); got != 0 {
		t.Fatalf("device auth calls = %d, want 0", got)
	}
	// The error must be a safe protocol error wrapping the write cause.
	var authErr *auth.Error
	if !errors.As(err, &authErr) {
		t.Fatalf("expected *auth.Error, got %T", err)
	}
	if authErr.Kind != auth.ProtocolError {
		t.Fatalf("kind = %q, want %q", authErr.Kind, auth.ProtocolError)
	}
}

// failingWriteClientCache wraps a Cache and fails WriteClient.
type failingWriteClientCache struct {
	Cache
}

func (c *failingWriteClientCache) WriteClient(cache *ClientRegistrationCache, startURL, region string, scopes []string, sessionName string) error {
	return errors.New("simulated client write failure")
}

// TestSecondsToDuration verifies the checked conversion helper.
func TestSecondsToDuration(t *testing.T) {
	// Normal value converts correctly.
	d, err := secondsToDuration(3600)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != 3600*time.Second {
		t.Fatalf("got %v, want 3600s", d)
	}
	// Zero is rejected.
	if _, err := secondsToDuration(0); err == nil {
		t.Fatal("expected error for 0 seconds")
	}
	// Negative is rejected.
	if _, err := secondsToDuration(-1); err == nil {
		t.Fatal("expected error for negative seconds")
	}
}

// TestSecondsToDurationExcessiveValue verifies that a value too large to fit in
// time.Duration when multiplied by time.Second is rejected. Skipped on 32-bit
// platforms where int cannot represent such a value.
func TestSecondsToDurationExcessiveValue(t *testing.T) {
	if bits.UintSize == 32 {
		t.Skip("int cannot represent an overflowing value on 32-bit platforms")
	}
	// maxDurationSeconds = math.MaxInt64 / 1e9 = 9223372036. One more overflows.
	if _, err := secondsToDuration(9223372037); err == nil {
		t.Fatal("expected error for excessive seconds value")
	}
	// The maximum safe value must still convert without error.
	if _, err := secondsToDuration(9223372036); err != nil {
		t.Fatalf("unexpected error for max safe seconds: %v", err)
	}
}

// TestDeviceFlowRejectsExcessiveTokenExpiresIn verifies that an excessively
// large token ExpiresIn (which would overflow time.Duration) fails closed with a
// ProtocolError during Login.
func TestDeviceFlowRejectsExcessiveTokenExpiresIn(t *testing.T) {
	if bits.UintSize == 32 {
		t.Skip("int cannot represent an overflowing value on 32-bit platforms")
	}
	oauth := &fakeOAuth{
		registerResp: &RegisterClientResponse{ClientID: "cid", ClientSecret: "csec"},
		deviceAuthResp: &StartDeviceAuthorizationResponse{
			DeviceCode: "dc", UserCode: "UC123",
			VerificationURIComplete: "https://example.com/verify?user_code=UC123",
			ExpiresIn:               600, Interval: 5,
		},
		tokenResp: &CreateTokenResponse{
			AccessToken: "at", TokenType: "Bearer",
			ExpiresIn: 9223372037, // overflows time.Duration
		},
	}
	cache := newFakeCache()
	df := newDeviceFlow(t, oauth, cache)
	_, err := df.Login(context.Background())
	if err == nil {
		t.Fatal("expected error for excessive token ExpiresIn")
	}
	var authErr *auth.Error
	if !errors.As(err, &authErr) {
		t.Fatalf("expected *auth.Error, got %T", err)
	}
	if authErr.Kind != auth.ProtocolError {
		t.Fatalf("kind = %q, want %q", authErr.Kind, auth.ProtocolError)
	}
}

// TestDeviceFlowRejectsExcessiveDeviceAuthExpiresIn verifies that an excessively
// large device authorization ExpiresIn fails closed with a ProtocolError.
func TestDeviceFlowRejectsExcessiveDeviceAuthExpiresIn(t *testing.T) {
	if bits.UintSize == 32 {
		t.Skip("int cannot represent an overflowing value on 32-bit platforms")
	}
	oauth := &fakeOAuth{
		registerResp: &RegisterClientResponse{ClientID: "cid", ClientSecret: "csec"},
		deviceAuthResp: &StartDeviceAuthorizationResponse{
			DeviceCode: "dc", UserCode: "UC123",
			VerificationURIComplete: "https://example.com/verify?user_code=UC123",
			ExpiresIn:               9223372037, Interval: 5,
		},
		tokenResp: &CreateTokenResponse{AccessToken: "at", TokenType: "Bearer", ExpiresIn: 3600},
	}
	cache := newFakeCache()
	df := newDeviceFlow(t, oauth, cache)
	_, err := df.Login(context.Background())
	if err == nil {
		t.Fatal("expected error for excessive device auth ExpiresIn")
	}
	var authErr *auth.Error
	if !errors.As(err, &authErr) {
		t.Fatalf("expected *auth.Error, got %T", err)
	}
	if authErr.Kind != auth.ProtocolError {
		t.Fatalf("kind = %q, want %q", authErr.Kind, auth.ProtocolError)
	}
}

// TestDeviceFlowRejectsExcessiveInterval verifies that an excessively large
// polling Interval fails closed with a ProtocolError.
func TestDeviceFlowRejectsExcessiveInterval(t *testing.T) {
	if bits.UintSize == 32 {
		t.Skip("int cannot represent an overflowing value on 32-bit platforms")
	}
	oauth := &fakeOAuth{
		registerResp: &RegisterClientResponse{ClientID: "cid", ClientSecret: "csec"},
		deviceAuthResp: &StartDeviceAuthorizationResponse{
			DeviceCode: "dc", UserCode: "UC123",
			VerificationURIComplete: "https://example.com/verify?user_code=UC123",
			ExpiresIn:               600, Interval: 9223372037,
		},
		tokenResp: &CreateTokenResponse{AccessToken: "at", TokenType: "Bearer", ExpiresIn: 3600},
	}
	cache := newFakeCache()
	df := newDeviceFlow(t, oauth, cache)
	_, err := df.Login(context.Background())
	if err == nil {
		t.Fatal("expected error for excessive Interval")
	}
	var authErr *auth.Error
	if !errors.As(err, &authErr) {
		t.Fatalf("expected *auth.Error, got %T", err)
	}
	if authErr.Kind != auth.ProtocolError {
		t.Fatalf("kind = %q, want %q", authErr.Kind, auth.ProtocolError)
	}
}

// TestDeviceFlowSlowDownIntervalOverflow verifies that when the interval is
// already near the time.Duration maximum, a slow_down response that would
// overflow the interval fails closed with a ProtocolError rather than wrapping.
func TestDeviceFlowSlowDownIntervalOverflow(t *testing.T) {
	if bits.UintSize == 32 {
		t.Skip("int cannot represent an overflowing value on 32-bit platforms")
	}
	// 9223372032s converts to a duration just above the overflow threshold
	// (MaxInt64 - 5s), so the first slow_down triggers the overflow guard.
	oauth := &fakeOAuth{
		registerResp: &RegisterClientResponse{ClientID: "cid", ClientSecret: "csec"},
		deviceAuthResp: &StartDeviceAuthorizationResponse{
			DeviceCode: "dc", UserCode: "UC123",
			VerificationURIComplete: "https://example.com/verify?user_code=UC123",
			ExpiresIn:               600, Interval: 9223372032,
		},
		tokenErr: &OAuthAPIError{StatusCode: 400, Code: "slow_down"},
	}
	cache := newFakeCache()
	df := newDeviceFlow(t, oauth, cache)
	_, err := df.Login(context.Background())
	if err == nil {
		t.Fatal("expected error for slow_down interval overflow")
	}
	var authErr *auth.Error
	if !errors.As(err, &authErr) {
		t.Fatalf("expected *auth.Error, got %T", err)
	}
	if authErr.Kind != auth.ProtocolError {
		t.Fatalf("kind = %q, want %q", authErr.Kind, auth.ProtocolError)
	}
}
