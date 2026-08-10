package console

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth"
	"github.com/volcengine-tls/ve-tls-cli/internal/securestore"
)

// --- Helpers ---

func makeCacheBytes(session string, issuedAt time.Time, expiresIn int, refreshToken string) []byte {
	c := LoginTokenCache{
		LoginSession: session,
		AccessToken:  validSTSAccessToken(),
		RefreshToken: refreshToken,
		IDToken:      validIDToken(session),
		Scope:        Scope,
		ClientID:     ClientIDSameDevice,
		EndpointURL:  DefaultEndpoint,
		IssuedAt:     issuedAt.UTC().Format(time.RFC3339Nano),
		ExpiresIn:    expiresIn,
		TokenType:    "sts",
	}
	return mustMarshal(c)
}

// mustMarshal marshals v to JSON, panicking on the impossible marshal failure.
// It is used in test helpers that cannot accept *testing.T.
func mustMarshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic("marshal failed: " + err.Error())
	}
	return b
}

// refreshCountingClient wraps a fakeOAuthClient and counts ExchangeToken calls.
type refreshCountingClient struct {
	*fakeOAuthClient
	refreshCount int32
	refreshDelay chan struct{} // if non-nil, ExchangeToken blocks until closed
}

func (r *refreshCountingClient) ExchangeToken(ctx context.Context, req *ConsoleTokenRequest) (*ConsoleTokenResponse, error) {
	atomic.AddInt32(&r.refreshCount, 1)
	if r.refreshDelay != nil {
		select {
		case <-r.refreshDelay:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return r.fakeOAuthClient.ExchangeToken(ctx, req)
}

func newRefreshClient(session string) *refreshCountingClient {
	return &refreshCountingClient{
		fakeOAuthClient: &fakeOAuthClient{
			exchangeResp: validTokenResponse(session),
			endpointURL:  DefaultEndpoint,
		},
	}
}

// blockingExchangeClient wraps a fakeOAuthClient. It closes enter the first time
// ExchangeToken is invoked (so tests can prove the caller reached the refresh
// step while holding the cache lock), blocks until blockUntil is closed, and
// atomically increments the shared count pointed to by count.
type blockingExchangeClient struct {
	fake       *fakeOAuthClient
	enter      chan struct{}
	blockUntil chan struct{}
	count      *int32
	enterOnce  sync.Once
}

func (b *blockingExchangeClient) BuildAuthorizeURL(params *AuthorizeParams) (string, error) {
	return b.fake.BuildAuthorizeURL(params)
}

func (b *blockingExchangeClient) ExchangeToken(ctx context.Context, req *ConsoleTokenRequest) (*ConsoleTokenResponse, error) {
	b.enterOnce.Do(func() { close(b.enter) })
	if b.count != nil {
		atomic.AddInt32(b.count, 1)
	}
	if b.blockUntil != nil {
		select {
		case <-b.blockUntil:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return b.fake.ExchangeToken(ctx, req)
}

func (b *blockingExchangeClient) EndpointURL() string {
	return b.fake.EndpointURL()
}

// --- Tests ---

func TestConsoleProviderUsesValidCacheWithoutNetwork(t *testing.T) {
	const session = "trn:iam::1:user/valid"
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	cache := newFakeCache()
	cache.data[session] = makeCacheBytes(session, now, 3600, "refresh-token")

	client := newRefreshClient(session)
	// Client should not be called at all.
	p := NewProvider(session, cache, func(string) (OAuthClient, error) { return client, nil }, func() time.Time { return now })

	val, err := p.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("Retrieve error: %v", err)
	}
	if val.AccessKeyID == "" {
		t.Error("expected non-empty access key")
	}
	if val.ProviderName != ProviderName {
		t.Errorf("ProviderName = %q, want %q", val.ProviderName, ProviderName)
	}
	if atomic.LoadInt32(&client.refreshCount) != 0 {
		t.Errorf("no refresh should happen for valid cache, count=%d", client.refreshCount)
	}
}

func TestConsoleProviderFailsClosedOnBroadPermissionCache(t *testing.T) {
	const session = "trn:iam::1:user/broad"
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)

	dir := t.TempDir()
	cache, err := NewFileCache(dir)
	if err != nil {
		t.Fatalf("NewFileCache: %v", err)
	}
	// Write a valid cache, then chmod the file to 0644 (broad).
	if err := cache.WriteRaw(session, makeCacheBytes(session, now, 3600, "refresh-token")); err != nil {
		t.Fatalf("WriteRaw: %v", err)
	}
	name, err := CacheFilename(session)
	if err != nil {
		t.Fatalf("CacheFilename: %v", err)
	}
	cachePath := filepath.Join(dir, name)
	if err := os.Chmod(cachePath, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	client := newRefreshClient(session)
	p := NewProvider(session, cache, func(string) (OAuthClient, error) { return client, nil }, func() time.Time { return now })

	val, err := p.Retrieve(context.Background())
	if !errors.Is(err, securestore.ErrPermission) {
		t.Fatalf("Retrieve with 0644 cache: err=%v, want errors.Is(err, securestore.ErrPermission)", err)
	}
	if val.AccessKeyID != "" || val.SecretAccessKey != "" || val.SessionToken != "" {
		t.Fatalf("Retrieve returned credentials despite broad cache: %+v", val)
	}
	if atomic.LoadInt32(&client.refreshCount) != 0 {
		t.Fatalf("OAuth exchange should not be called for broad cache, count=%d", client.refreshCount)
	}
}

func TestConsoleProviderRefreshesInside60SecondWindow(t *testing.T) {
	const session = "trn:iam::1:user/refresh"
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	// Token issued 3599 seconds ago, expires in 1 second -> within 60s window.
	issuedAt := now.Add(-3599 * time.Second)
	cache := newFakeCache()
	cache.data[session] = makeCacheBytes(session, issuedAt, 3600, "old-refresh-token")

	client := newRefreshClient(session)
	p := NewProvider(session, cache, func(string) (OAuthClient, error) { return client, nil }, func() time.Time { return now })

	val, err := p.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("Retrieve error: %v", err)
	}
	if val.AccessKeyID == "" {
		t.Error("expected non-empty access key after refresh")
	}
	if atomic.LoadInt32(&client.refreshCount) != 1 {
		t.Errorf("expected exactly 1 refresh, count=%d", client.refreshCount)
	}
	// The rotated cache should have been persisted.
	if _, ok := cache.data[session]; !ok {
		t.Error("rotated cache should be persisted")
	}
}

func TestConsoleProviderPersistsRotatedRefreshTokenBeforeReturn(t *testing.T) {
	const session = "trn:iam::1:user/persist"
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	issuedAt := now.Add(-3599 * time.Second)
	cache := newFakeCache()
	cache.data[session] = makeCacheBytes(session, issuedAt, 3600, "old-refresh")

	// Client returns a new refresh token.
	resp := validTokenResponse(session)
	resp.RefreshToken = "new-rotated-refresh-token"
	client := &refreshCountingClient{
		fakeOAuthClient: &fakeOAuthClient{exchangeResp: resp, endpointURL: DefaultEndpoint},
	}
	p := NewProvider(session, cache, func(string) (OAuthClient, error) { return client, nil }, func() time.Time { return now })

	if _, err := p.Retrieve(context.Background()); err != nil {
		t.Fatalf("Retrieve error: %v", err)
	}

	// Verify the persisted cache contains the new refresh token.
	var stored LoginTokenCache
	if err := json.Unmarshal(cache.data[session], &stored); err != nil {
		t.Fatalf("unmarshal stored cache: %v", err)
	}
	if stored.RefreshToken != "new-rotated-refresh-token" {
		t.Errorf("stored refresh token = %q, want %q", stored.RefreshToken, "new-rotated-refresh-token")
	}
}

func TestConsoleProviderPreservesOldRefreshTokenWhenOmitted(t *testing.T) {
	const session = "trn:iam::1:user/preserve"
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	issuedAt := now.Add(-3599 * time.Second)
	cache := newFakeCache()
	cache.data[session] = makeCacheBytes(session, issuedAt, 3600, "old-refresh-token")

	// Client returns response with empty refresh token.
	resp := validTokenResponse(session)
	resp.RefreshToken = ""
	client := &refreshCountingClient{
		fakeOAuthClient: &fakeOAuthClient{exchangeResp: resp, endpointURL: DefaultEndpoint},
	}
	p := NewProvider(session, cache, func(string) (OAuthClient, error) { return client, nil }, func() time.Time { return now })

	if _, err := p.Retrieve(context.Background()); err != nil {
		t.Fatalf("Retrieve error: %v", err)
	}

	var stored LoginTokenCache
	if err := json.Unmarshal(cache.data[session], &stored); err != nil {
		t.Fatalf("unmarshal stored cache: %v", err)
	}
	if stored.RefreshToken != "old-refresh-token" {
		t.Errorf("stored refresh token = %q, want preserved %q", stored.RefreshToken, "old-refresh-token")
	}
}

func TestConsoleProviderWriteFailureFailsClosed(t *testing.T) {
	const session = "trn:iam::1:user/writefail"
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	issuedAt := now.Add(-3599 * time.Second)
	cache := newFakeCache()
	cache.data[session] = makeCacheBytes(session, issuedAt, 3600, "refresh-token")
	cache.writeErr = errors.New("disk full")

	client := newRefreshClient(session)
	p := NewProvider(session, cache, func(string) (OAuthClient, error) { return client, nil }, func() time.Time { return now })

	val, err := p.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected error when cache write fails")
	}
	if val.AccessKeyID != "" {
		t.Errorf("credentials should not be returned on write failure, got AK=%q", val.AccessKeyID)
	}
}

func TestConsoleProviderConcurrentRetrieveRefreshesOnce(t *testing.T) {
	// A no-slash session is used here for orthogonality; dedicated real
	// slash-session regressions (e.g. TestProviderSessionWithSlashRetrievesAndRefreshes)
	// already prove the SHA-1 lock key works with realistic TRNs.
	const session = "trn:iam::1:user:concurrent"
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	// Token issued 3599s ago, expires in 1s -> within the 60s refresh window.
	issuedAt := now.Add(-3599 * time.Second)

	// Two distinct real FileCache instances rooted at the same temp dir.
	dir := t.TempDir()
	cacheA, err := NewFileCache(dir)
	if err != nil {
		t.Fatalf("NewFileCache A: %v", err)
	}
	cacheB, err := NewFileCache(dir)
	if err != nil {
		t.Fatalf("NewFileCache B: %v", err)
	}

	// Seed the real SHA-1 cache file through one instance; the other instance
	// must see it via the shared directory and the cross-instance lock.
	seedBytes := makeCacheBytes(session, issuedAt, 3600, "refresh-token")
	if err := cacheA.WriteRaw(session, seedBytes); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	// The leader Provider acquires the cache lock first and blocks inside
	// ExchangeToken until the test releases the barrier. leaderInExchange is
	// closed the moment ExchangeToken is entered, proving the leader owns the
	// real cache lock before any follower is started.
	refreshBarrier := make(chan struct{})
	leaderInExchange := make(chan struct{})
	var exchangeCount int32
	leaderClient := &fakeOAuthClient{
		exchangeResp: validTokenResponse(session),
		endpointURL:  DefaultEndpoint,
	}
	leaderFactory := func(string) (OAuthClient, error) {
		return &blockingExchangeClient{
			fake:       leaderClient,
			enter:      leaderInExchange,
			blockUntil: refreshBarrier,
			count:      &exchangeCount,
		}, nil
	}
	clock := func() time.Time { return now }

	const followers = 19
	const total = 1 + followers

	// Start the single leader first and wait until it is inside ExchangeToken
	// (and therefore holding the real cache lock).
	leaderDone := make(chan struct {
		val auth.Value
		err error
	}, 1)
	go func() {
		p := NewProvider(session, cacheA, leaderFactory, clock)
		v, err := p.Retrieve(context.Background())
		leaderDone <- struct {
			val auth.Value
			err error
		}{v, err}
	}()
	select {
	case <-leaderInExchange:
	case <-time.After(5 * time.Second):
		t.Fatal("leader did not enter ExchangeToken (and acquire the cache lock) in time")
	}

	// Each follower gets a context derived with the real securestore contention
	// observer and a per-follower sync.Once observer that reports into a
	// buffered channel. The observer only fires when the follower actually
	// blocks on the in-process cache lock already held by the leader.
	contended := make(chan struct{}, followers)
	var wg sync.WaitGroup
	wg.Add(followers)
	vals := make([]auth.Value, total)
	errs := make([]error, total)
	for i := 0; i < followers; i++ {
		var real *FileCache = cacheA
		if i%2 == 1 {
			real = cacheB
		}
		var once sync.Once
		obs := func() { once.Do(func() { contended <- struct{}{} }) }
		ctx := securestore.WithLockContentionObserver(context.Background(), obs)
		go func(idx int, cache ConsoleCache, c context.Context) {
			defer wg.Done()
			p := NewProvider(session, cache, leaderFactory, clock)
			vals[idx], errs[idx] = p.Retrieve(c)
		}(1+i, real, ctx)
	}

	// Wait until all 19 actual contention notifications have been received
	// before releasing the leader OAuth barrier. These notifications are the
	// deterministic evidence of real lock contention.
	for i := 0; i < followers; i++ {
		select {
		case <-contended:
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d/%d follower contention notifications received in time", i, followers)
		}
	}
	close(refreshBarrier)

	wg.Wait()
	select {
	case res := <-leaderDone:
		vals[0] = res.val
		errs[0] = res.err
	case <-time.After(5 * time.Second):
		t.Fatal("leader did not complete after barrier release")
	}

	for i, e := range errs {
		if e != nil {
			t.Errorf("Retrieve[%d] error: %v", i, e)
		}
	}
	if count := atomic.LoadInt32(&exchangeCount); count != 1 {
		t.Errorf("expected exactly 1 refresh across %d concurrent calls, got %d", total, count)
	}
	// Every caller must get the rotated credential (non-empty access key).
	for i, v := range vals {
		if v.AccessKeyID == "" {
			t.Errorf("Retrieve[%d] returned empty access key", i)
		}
	}
	// The final raw cache must be the rotated one, not the seed.
	finalBytes, existed, rerr := cacheB.ReadRaw(session)
	if rerr != nil {
		t.Fatalf("read final cache: %v", rerr)
	}
	if !existed {
		t.Fatal("final cache should exist after refresh")
	}
	if string(finalBytes) == string(seedBytes) {
		t.Error("final cache is the seed; refresh did not persist rotated cache")
	}
}

func TestConsoleProviderRefreshesAtExact60SecondBoundary(t *testing.T) {
	const session = "trn:iam::1:user/boundary"
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	// Token expires exactly RefreshBuffer (60s) from now. At exactly
	// expiresAt - RefreshBuffer == now, the provider must refresh.
	issuedAt := now.Add(-3600 * time.Second)
	cache := newFakeCache()
	cache.data[session] = makeCacheBytes(session, issuedAt, 3600, "refresh-token")

	client := newRefreshClient(session)
	p := NewProvider(session, cache, func(string) (OAuthClient, error) { return client, nil }, func() time.Time { return now })

	val, err := p.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("Retrieve error: %v", err)
	}
	if val.AccessKeyID == "" {
		t.Error("expected non-empty access key after boundary refresh")
	}
	if atomic.LoadInt32(&client.refreshCount) != 1 {
		t.Errorf("expected exactly 1 refresh at boundary, count=%d", client.refreshCount)
	}
}

func TestConsoleProviderReloadsDiskInsideLock(t *testing.T) {
	const session = "trn:iam::1:user/reload"
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)

	cache := newFakeCache()
	// Initially no cache on disk.
	client := newRefreshClient(session)
	p := NewProvider(session, cache, func(string) (OAuthClient, error) { return client, nil }, func() time.Time { return now })

	// First retrieve: cache missing -> reauth required.
	_, err := p.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected reauth error for missing cache")
	}

	// Simulate another process writing a valid cache to disk.
	cache.data[session] = makeCacheBytes(session, now, 3600, "refresh-token")

	// Second retrieve: should re-read disk and find the valid cache (no refresh).
	val, err := p.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("Retrieve error after cache appeared: %v", err)
	}
	if val.AccessKeyID == "" {
		t.Error("expected credentials from reloaded cache")
	}
	if atomic.LoadInt32(&client.refreshCount) != 0 {
		t.Errorf("no refresh should happen for freshly loaded valid cache, count=%d", client.refreshCount)
	}
}

func TestConsoleProviderNeverFallsBackToEnvironmentAK(t *testing.T) {
	const session = "trn:iam::1:user/nofallback"
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	cache := newFakeCache()
	// No cache at all.
	client := newRefreshClient(session)
	p := NewProvider(session, cache, func(string) (OAuthClient, error) { return client, nil }, func() time.Time { return now })

	// Set environment AK/SK; the provider must NOT use them.
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ENV_AK_CANARY")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "ENV_SK_CANARY")

	val, err := p.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected error for missing cache")
	}
	if val.AccessKeyID != "" {
		t.Errorf("provider must not fall back to env AK, got %q", val.AccessKeyID)
	}
	var authErr *auth.Error
	if !errors.As(err, &authErr) {
		t.Fatalf("expected *auth.Error, got %T", err)
	}
	if authErr.Kind != auth.ReauthRequired {
		t.Errorf("error kind = %q, want %q", authErr.Kind, auth.ReauthRequired)
	}
}

func TestConsoleProviderRequiresReloginForMissingCorruptInvalidGrant(t *testing.T) {
	const session = "trn:iam::1:user/relogin"
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)

	t.Run("missing cache", func(t *testing.T) {
		cache := newFakeCache()
		p := NewProvider(session, cache, func(string) (OAuthClient, error) { return newRefreshClient(session), nil }, func() time.Time { return now })
		_, err := p.Retrieve(context.Background())
		assertReauth(t, err)
	})

	t.Run("corrupt cache", func(t *testing.T) {
		cache := newFakeCache()
		cache.data[session] = []byte("not valid json{{{")
		p := NewProvider(session, cache, func(string) (OAuthClient, error) { return newRefreshClient(session), nil }, func() time.Time { return now })
		_, err := p.Retrieve(context.Background())
		assertReauthWithCorruptCause(t, err)
	})

	t.Run("invalid_grant", func(t *testing.T) {
		issuedAt := now.Add(-3599 * time.Second)
		cache := newFakeCache()
		cache.data[session] = makeCacheBytes(session, issuedAt, 3600, "expired-refresh")
		client := &fakeOAuthClient{
			exchangeErr: &ConsoleOAuthAPIError{
				StatusCode: 400,
				Response:   ConsoleOAuthErrorResponse{Error: "invalid_grant"},
			},
			endpointURL: DefaultEndpoint,
		}
		p := NewProvider(session, cache, func(string) (OAuthClient, error) { return client, nil }, func() time.Time { return now })
		_, err := p.Retrieve(context.Background())
		assertReauth(t, err)
		// Error must not contain the refresh token or session.
		errStr := err.Error()
		if strings.Contains(errStr, "expired-refresh") {
			t.Errorf("error echoes refresh token: %s", errStr)
		}
		if strings.Contains(errStr, session) {
			t.Errorf("error echoes login session: %s", errStr)
		}
	})
}

func assertReauth(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var authErr *auth.Error
	if !errors.As(err, &authErr) {
		t.Fatalf("expected *auth.Error, got %T: %v", err, err)
	}
	if authErr.Kind != auth.ReauthRequired {
		t.Errorf("error kind = %q, want %q", authErr.Kind, auth.ReauthRequired)
	}
	hint := authErr.Description
	if !strings.Contains(hint, "volclog login") {
		t.Errorf("error description should contain login hint, got: %s", hint)
	}
}

// assertReauthWithCorruptCause asserts that err is a top-level ReauthRequired
// error (with the safe "volclog login" hint) that also carries a nested
// CacheCorrupt cause matchable via errors.Is. This is the contract for
// corrupt/unreadable/invalid-schema/expiration/STS caches: callers see
// ReauthRequired and re-login, while diagnostics remain available as a
// CacheCorrupt cause.
func assertReauthWithCorruptCause(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var authErr *auth.Error
	if !errors.As(err, &authErr) {
		t.Fatalf("expected *auth.Error, got %T: %v", err, err)
	}
	if authErr.Kind != auth.ReauthRequired {
		t.Errorf("top-level error kind = %q, want %q", authErr.Kind, auth.ReauthRequired)
	}
	if !strings.Contains(authErr.Description, "volclog login") {
		t.Errorf("error description should contain login hint, got: %s", authErr.Description)
	}
	if !errors.Is(err, &auth.Error{Kind: auth.CacheCorrupt}) {
		t.Errorf("error should have a nested CacheCorrupt cause, got: %v", err)
	}
}

func TestConsoleProviderClassifiesMissingVsCorruptCache(t *testing.T) {
	const session = "trn:iam::1:user:classify"
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)

	t.Run("missing file is reauth only", func(t *testing.T) {
		cache := newFakeCache()
		p := NewProvider(session, cache, func(string) (OAuthClient, error) { return newRefreshClient(session), nil }, func() time.Time { return now })
		_, err := p.Retrieve(context.Background())
		assertReauth(t, err)
		// Missing file must NOT carry a nested CacheCorrupt cause.
		if errors.Is(err, &auth.Error{Kind: auth.CacheCorrupt}) {
			t.Errorf("missing cache should not match CacheCorrupt, got: %v", err)
		}
	})

	t.Run("empty file is reauth with corrupt cause", func(t *testing.T) {
		cache := newFakeCache()
		cache.data[session] = []byte{} // exists but zero bytes
		p := NewProvider(session, cache, func(string) (OAuthClient, error) { return newRefreshClient(session), nil }, func() time.Time { return now })
		_, err := p.Retrieve(context.Background())
		assertReauthWithCorruptCause(t, err)
	})

	t.Run("empty cached session is reauth with corrupt cause", func(t *testing.T) {
		cache := newFakeCache()
		b := makeCacheBytes(session, now, 3600, "refresh-token")
		var c LoginTokenCache
		if err := json.Unmarshal(b, &c); err != nil {
			t.Fatalf("unmarshal cache: %v", err)
		}
		c.LoginSession = ""
		cache.data[session] = mustMarshal(c)

		p := NewProvider(session, cache, func(string) (OAuthClient, error) { return newRefreshClient(session), nil }, func() time.Time { return now })
		_, err := p.Retrieve(context.Background())
		assertReauthWithCorruptCause(t, err)
	})

	t.Run("mismatched cached session is reauth with corrupt cause", func(t *testing.T) {
		cache := newFakeCache()
		b := makeCacheBytes(session, now, 3600, "refresh-token")
		var c LoginTokenCache
		if err := json.Unmarshal(b, &c); err != nil {
			t.Fatalf("unmarshal cache: %v", err)
		}
		c.LoginSession = "trn:iam::1:user:other"
		cache.data[session] = mustMarshal(c)

		p := NewProvider(session, cache, func(string) (OAuthClient, error) { return newRefreshClient(session), nil }, func() time.Time { return now })
		_, err := p.Retrieve(context.Background())
		assertReauthWithCorruptCause(t, err)
	})
}

func TestConsoleProviderRejectsInvalidCacheSchema(t *testing.T) {
	const session = "trn:iam::1:user:schema"
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)

	t.Run("wrong client id", func(t *testing.T) {
		cache := newFakeCache()
		b := makeCacheBytes(session, now, 3600, "refresh-token")
		var c LoginTokenCache
		if err := json.Unmarshal(b, &c); err != nil {
			t.Fatalf("unmarshal cache: %v", err)
		}
		c.ClientID = "trn:signin:::evil/other"
		cache.data[session] = mustMarshal(c)

		p := NewProvider(session, cache, func(string) (OAuthClient, error) { return newRefreshClient(session), nil }, func() time.Time { return now })
		_, err := p.Retrieve(context.Background())
		assertReauthWithCorruptCause(t, err)
	})

	t.Run("wrong scope", func(t *testing.T) {
		cache := newFakeCache()
		b := makeCacheBytes(session, now, 3600, "refresh-token")
		var c LoginTokenCache
		if err := json.Unmarshal(b, &c); err != nil {
			t.Fatalf("unmarshal cache: %v", err)
		}
		c.Scope = "Wrong:Scope:Only"
		cache.data[session] = mustMarshal(c)

		p := NewProvider(session, cache, func(string) (OAuthClient, error) { return newRefreshClient(session), nil }, func() time.Time { return now })
		_, err := p.Retrieve(context.Background())
		assertReauthWithCorruptCause(t, err)
	})

	t.Run("bad endpoint", func(t *testing.T) {
		cache := newFakeCache()
		b := makeCacheBytes(session, now, 3600, "refresh-token")
		var c LoginTokenCache
		if err := json.Unmarshal(b, &c); err != nil {
			t.Fatalf("unmarshal cache: %v", err)
		}
		c.EndpointURL = "http://evil.example.com"
		cache.data[session] = mustMarshal(c)

		p := NewProvider(session, cache, func(string) (OAuthClient, error) { return newRefreshClient(session), nil }, func() time.Time { return now })
		_, err := p.Retrieve(context.Background())
		assertReauthWithCorruptCause(t, err)
	})
}

// TestConsoleProviderNearExpiryMissingOrWhitespaceRefreshTokenRejects proves
// that a near-expiry cache with a missing or whitespace-only RefreshToken
// returns a top-level ReauthRequired error WITHOUT a nested CacheCorrupt cause,
// and OAuth refresh is never called. Both cases are covered.
func TestConsoleProviderNearExpiryMissingOrWhitespaceRefreshTokenRejects(t *testing.T) {
	cases := []struct {
		name         string
		refreshToken string
	}{
		{"missing", ""},
		{"whitespace", "   "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const session = "trn:iam::1:user:rejectrefresh"
			now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
			issuedAt := now.Add(-3599 * time.Second) // near expiry -> needs refresh

			cache := newFakeCache()
			cache.data[session] = makeCacheBytes(session, issuedAt, 3600, tc.refreshToken)

			client := newRefreshClient(session)
			p := NewProvider(session, cache, func(string) (OAuthClient, error) { return client, nil }, func() time.Time { return now })

			_, err := p.Retrieve(context.Background())
			assertReauth(t, err)
			if errors.Is(err, &auth.Error{Kind: auth.CacheCorrupt}) {
				t.Errorf("%s refresh token must NOT carry a nested CacheCorrupt cause, got: %v", tc.name, err)
			}
			if atomic.LoadInt32(&client.refreshCount) != 0 {
				t.Errorf("%s: no refresh should happen, count=%d", tc.name, client.refreshCount)
			}
		})
	}
}

// makeCacheBytesWithInvalidAccessToken builds a near-expiry cache whose old
// AccessToken is a valid JSON value that is NOT valid STS credentials (a JSON
// string instead of an object). The cache is otherwise valid (correct session,
// frozen client/scope, valid refresh token) so the Provider must refresh rather
// than fail on the corrupt old STS.
func makeCacheBytesWithInvalidAccessToken(session string, issuedAt time.Time, expiresIn int, refreshToken string) []byte {
	c := LoginTokenCache{
		LoginSession: session,
		AccessToken:  json.RawMessage(`"not-a-valid-sts-object"`),
		RefreshToken: refreshToken,
		IDToken:      validIDToken(session),
		Scope:        Scope,
		ClientID:     ClientIDSameDevice,
		EndpointURL:  DefaultEndpoint,
		IssuedAt:     issuedAt.UTC().Format(time.RFC3339Nano),
		ExpiresIn:    expiresIn,
		TokenType:    "sts",
	}
	return mustMarshal(c)
}

// TestConsoleProviderRefreshesWithInvalidOldAccessToken proves that a
// near-expiry cache with a malformed old AccessToken but a valid RefreshToken
// is refreshed (OAuth called once) and returns new credentials, rather than
// failing with ReauthRequired.
func TestConsoleProviderRefreshesWithInvalidOldAccessToken(t *testing.T) {
	const session = "trn:iam::1:user:invalidoldsts"
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	issuedAt := now.Add(-3599 * time.Second) // near expiry -> needs refresh

	cache := newFakeCache()
	cache.data[session] = makeCacheBytesWithInvalidAccessToken(session, issuedAt, 3600, "valid-refresh-token")

	client := newRefreshClient(session)
	p := NewProvider(session, cache, func(string) (OAuthClient, error) { return client, nil }, func() time.Time { return now })

	val, err := p.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("expected successful refresh, got err: %v", err)
	}
	// The returned credentials must come from the refresh response, not the
	// (invalid) old cache.
	resp := validTokenResponse(session)
	expectedSTS, perr := ParseSTSCredentials(resp.AccessToken)
	if perr != nil {
		t.Fatalf("parse expected STS from refresh response: %v", perr)
	}
	if val.AccessKeyID != expectedSTS.AccessKeyID {
		t.Errorf("AccessKeyID = %q, want %q (from refresh response)", val.AccessKeyID, expectedSTS.AccessKeyID)
	}
	if val.SecretAccessKey != expectedSTS.SecretAccessKey {
		t.Errorf("SecretAccessKey = %q, want %q (from refresh response)", val.SecretAccessKey, expectedSTS.SecretAccessKey)
	}
	if val.SessionToken != expectedSTS.SessionToken {
		t.Errorf("SessionToken = %q, want %q (from refresh response)", val.SessionToken, expectedSTS.SessionToken)
	}
	if val.ProviderName != ProviderName {
		t.Errorf("ProviderName = %q, want %q", val.ProviderName, ProviderName)
	}
	if val.ExpiresAt.IsZero() {
		t.Error("expected non-zero ExpiresAt after refresh")
	}
	if atomic.LoadInt32(&client.refreshCount) != 1 {
		t.Errorf("expected exactly 1 refresh, count=%d", client.refreshCount)
	}
}

// TestConsoleProviderNonExpiryInvalidAccessTokenFailsClosedCorrupt proves
// that a non-expiring (fast-path) cache with a malformed old AccessToken fails
// closed with a nested CacheCorrupt cause (the old STS will be used directly,
// so it must be parseable).
func TestConsoleProviderNonExpiryInvalidAccessTokenFailsClosedCorrupt(t *testing.T) {
	const session = "trn:iam::1:user:nonstalecorrupt"
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	issuedAt := now // fresh, not near expiry

	cache := newFakeCache()
	cache.data[session] = makeCacheBytesWithInvalidAccessToken(session, issuedAt, 3600, "unused-refresh-token")

	client := newRefreshClient(session)
	p := NewProvider(session, cache, func(string) (OAuthClient, error) { return client, nil }, func() time.Time { return now })

	_, err := p.Retrieve(context.Background())
	assertReauthWithCorruptCause(t, err)
	if atomic.LoadInt32(&client.refreshCount) != 0 {
		t.Errorf("no refresh should happen on fast path with corrupt STS, count=%d", client.refreshCount)
	}
}

// TestConsoleProviderNonExpiryValidCacheWithoutRefreshTokenSucceeds proves
// that a non-expiring (fast-path) valid cache returns the cached STS credentials
// successfully even when the RefreshToken is missing or whitespace-only, because
// the fast path does not depend on the RefreshToken. OAuth refresh must not be
// called.
func TestConsoleProviderNonExpiryValidCacheWithoutRefreshTokenSucceeds(t *testing.T) {
	cases := []struct {
		name         string
		refreshToken string
	}{
		{"missing", ""},
		{"whitespace", "   "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const session = "trn:iam::1:user:fastpathnorefresh"
			now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
			issuedAt := now // fresh, not near expiry

			cache := newFakeCache()
			cache.data[session] = makeCacheBytes(session, issuedAt, 3600, tc.refreshToken)

			client := newRefreshClient(session)
			p := NewProvider(session, cache, func(string) (OAuthClient, error) { return client, nil }, func() time.Time { return now })

			val, err := p.Retrieve(context.Background())
			if err != nil {
				t.Fatalf("%s: expected success on fast path, got err: %v", tc.name, err)
			}
			// Credentials must come from the cached (valid) STS, not a refresh.
			expectedSTS, perr := ParseSTSCredentials(validSTSAccessToken())
			if perr != nil {
				t.Fatalf("%s: parse expected cached STS: %v", tc.name, perr)
			}
			if val.AccessKeyID != expectedSTS.AccessKeyID {
				t.Errorf("%s: AccessKeyID = %q, want cached %q", tc.name, val.AccessKeyID, expectedSTS.AccessKeyID)
			}
			if val.SecretAccessKey != expectedSTS.SecretAccessKey {
				t.Errorf("%s: SecretAccessKey = %q, want cached %q", tc.name, val.SecretAccessKey, expectedSTS.SecretAccessKey)
			}
			if val.SessionToken != expectedSTS.SessionToken {
				t.Errorf("%s: SessionToken = %q, want cached %q", tc.name, val.SessionToken, expectedSTS.SessionToken)
			}
			if atomic.LoadInt32(&client.refreshCount) != 0 {
				t.Errorf("%s: no refresh should happen on fast path, count=%d", tc.name, client.refreshCount)
			}
		})
	}
}

func TestConsoleProviderNilFactoryFailsSafely(t *testing.T) {
	const session = "trn:iam::1:user:nilfactory"
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	issuedAt := now.Add(-3599 * time.Second)

	cache := newFakeCache()
	cache.data[session] = makeCacheBytes(session, issuedAt, 3600, "refresh-token")

	p := NewProvider(session, cache, nil, func() time.Time { return now })
	_, err := p.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected error for nil oauth factory")
	}
}

// TestProviderFactoryErrorDoesNotLeakCanary verifies that an error returned by
// the injected OAuth factory (which may contain secret material) is stored as
// the Cause of an auth.Error whose Error() text never renders it.
func TestProviderFactoryErrorDoesNotLeakCanary(t *testing.T) {
	const session = "trn:iam::1:user:factcanary"
	const factoryCanary = "factory-secret-canary-999"
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	issuedAt := now.Add(-3599 * time.Second)

	cache := newFakeCache()
	cache.data[session] = makeCacheBytes(session, issuedAt, 3600, "refresh-token")

	factoryErr := errors.New(factoryCanary)
	p := NewProvider(session, cache, func(string) (OAuthClient, error) { return nil, factoryErr }, func() time.Time { return now })

	_, err := p.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected error when factory fails")
	}
	if strings.Contains(err.Error(), factoryCanary) {
		t.Errorf("Provider error leaks factory canary: %s", err.Error())
	}
	// The cause must still be matchable via errors.Is.
	if !errors.Is(err, factoryErr) {
		t.Errorf("errors.Is should match factory cause, got: %v", err)
	}
}

func TestConsoleProviderNilClockDefaultsToNow(t *testing.T) {
	const session = "trn:iam::1:user:nilclock"
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	cache := newFakeCache()
	cache.data[session] = makeCacheBytes(session, now, 3600, "refresh-token")

	client := newRefreshClient(session)
	// nil clock should default to time.Now; with a valid cache no refresh occurs.
	p := NewProvider(session, cache, func(string) (OAuthClient, error) { return client, nil }, nil)

	val, err := p.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("Retrieve error: %v", err)
	}
	if val.AccessKeyID == "" {
		t.Error("expected non-empty access key")
	}
}

func TestConsoleProviderRefreshRequestHasExactFields(t *testing.T) {
	const session = "trn:iam::1:user:reqfields"
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	issuedAt := now.Add(-3599 * time.Second)

	cache := newFakeCache()
	cache.data[session] = makeCacheBytes(session, issuedAt, 3600, "stored-refresh-token")

	client := &fakeOAuthClient{
		exchangeResp: validTokenResponse(session),
		endpointURL:  DefaultEndpoint,
	}
	p := NewProvider(session, cache, func(string) (OAuthClient, error) { return client, nil }, func() time.Time { return now })

	if _, err := p.Retrieve(context.Background()); err != nil {
		t.Fatalf("Retrieve error: %v", err)
	}

	if client.lastReq == nil {
		t.Fatal("ExchangeToken was not called")
	}
	req := client.lastReq
	if req.GrantType != GrantTypeRefreshToken {
		t.Errorf("grant_type = %q, want %q", req.GrantType, GrantTypeRefreshToken)
	}
	if req.RefreshToken != "stored-refresh-token" {
		t.Errorf("refresh_token = %q, want stored token", req.RefreshToken)
	}
	if req.ClientID != ClientIDSameDevice {
		t.Errorf("client_id = %q, want %q", req.ClientID, ClientIDSameDevice)
	}
	if req.Scope != Scope {
		t.Errorf("scope = %q, want frozen %q", req.Scope, Scope)
	}
	if req.Code != "" {
		t.Errorf("code should be empty in refresh request, got %q", req.Code)
	}
	if req.CodeVerifier != "" {
		t.Errorf("code_verifier should be empty in refresh request, got %q", req.CodeVerifier)
	}
	if req.RedirectURI != "" {
		t.Errorf("redirect_uri should be empty in refresh request, got %q", req.RedirectURI)
	}
}

func TestConsoleProviderEmptyLoginSessionFailsSafely(t *testing.T) {
	cache := newFakeCache()
	p := NewProvider("   ", cache, func(string) (OAuthClient, error) { return newRefreshClient("x"), nil }, nil)
	_, err := p.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected error for empty login session")
	}
}

// TestProviderTypedNilDepsFailClosed verifies that typed-nil interface values
// for ConsoleCache and factory-returned OAuthClient are detected and rejected
// with an error rather than panicking.
func TestProviderTypedNilDepsFailClosed(t *testing.T) {
	const session = "trn:iam::1:user:ptypednil"
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		p    *Provider
	}{
		{
			name: "typed-nil cache",
			p: &Provider{
				loginSession: session,
				cache:        (*fakeCache)(nil),
				oauthFactory: func(string) (OAuthClient, error) { return newRefreshClient(session), nil },
				clock:        func() time.Time { return now },
			},
		},
		{
			name: "factory returns typed-nil client",
			p: &Provider{
				loginSession: session,
				cache:        newFakeCache(),
				oauthFactory: func(string) (OAuthClient, error) { return (*fakeOAuthClient)(nil), nil },
				clock:        func() time.Time { return now },
			},
		},
	}
	// Seed the cache for the typed-nil client case so it reaches the factory.
	if c, ok := cases[1].p.cache.(*fakeCache); ok {
		c.data[session] = makeCacheBytes(session, now.Add(-3599*time.Second), 3600, "refresh-token")
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.p.Retrieve(context.Background())
			if err == nil {
				t.Fatal("expected error for typed-nil dependency")
			}
		})
	}
}

// TestProviderSessionWithSlashRetrievesAndRefreshes is a real FileCache
// regression proving that a Provider backed by a slash-containing session (a
// realistic TRN) can retrieve cached credentials and refresh them, persisting
// the rotated cache so a second FileCache instance observes it.
func TestProviderSessionWithSlashRetrievesAndRefreshes(t *testing.T) {
	const session = "trn:iam::123456789012:login/session/test"
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)

	dir := t.TempDir()
	cache, err := NewFileCache(dir)
	if err != nil {
		t.Fatalf("NewFileCache error: %v", err)
	}
	// Seed a valid, non-expired cache.
	if err := cache.WriteRaw(session, makeCacheBytes(session, now, 3600, "refresh-token")); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	client := newRefreshClient(session)
	p := NewProvider(session, cache, func(string) (OAuthClient, error) { return client, nil }, func() time.Time { return now })

	val, err := p.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("Retrieve error: %v", err)
	}
	if val.AccessKeyID == "" {
		t.Fatal("expected non-empty AccessKeyID")
	}
	if atomic.LoadInt32(&client.refreshCount) != 0 {
		t.Errorf("no refresh expected for valid cache, count=%d", client.refreshCount)
	}

	// Force a refresh by advancing the clock near expiry.
	refreshNow := now.Add(3599 * time.Second)
	p2 := NewProvider(session, cache, func(string) (OAuthClient, error) { return client, nil }, func() time.Time { return refreshNow })
	if _, err := p2.Retrieve(context.Background()); err != nil {
		t.Fatalf("Retrieve after refresh error: %v", err)
	}
	if atomic.LoadInt32(&client.refreshCount) != 1 {
		t.Errorf("expected one refresh, count=%d", client.refreshCount)
	}

	// The rotated cache must be visible to a second FileCache instance.
	cache2, err := NewFileCache(dir)
	if err != nil {
		t.Fatalf("NewFileCache2 error: %v", err)
	}
	_, existed, err := cache2.ReadRaw(session)
	if err != nil {
		t.Fatalf("cache2 ReadRaw error: %v", err)
	}
	if !existed {
		t.Fatal("rotated cache should be visible to a second FileCache")
	}
}

// TestProviderEmptyCachedTokenTypeRequiresReauth verifies that a cached token
// with an empty TokenType is treated as a corrupt cache requiring re-login
// (top-level ReauthRequired with a nested CacheCorrupt cause) before any
// network call.
func TestProviderEmptyCachedTokenTypeRequiresReauth(t *testing.T) {
	const session = "trn:iam::1:user:emptytokentype"
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)

	cache := newFakeCache()
	b := makeCacheBytes(session, now, 3600, "refresh-token")
	var c LoginTokenCache
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	c.TokenType = ""
	cache.data[session] = mustMarshal(c)

	client := newRefreshClient(session)
	p := NewProvider(session, cache, func(string) (OAuthClient, error) { return client, nil }, func() time.Time { return now })
	_, err := p.Retrieve(context.Background())
	assertReauthWithCorruptCause(t, err)
	if atomic.LoadInt32(&client.refreshCount) != 0 {
		t.Errorf("no refresh expected for empty cached TokenType, count=%d", client.refreshCount)
	}
}

// TestProviderEmptyRefreshedTokenTypeIsProtocolError verifies that a refresh
// response with an empty TokenType is a ProtocolError that fails closed: no
// credentials are returned and the cache is not persisted.
func TestProviderEmptyRefreshedTokenTypeIsProtocolError(t *testing.T) {
	const session = "trn:iam::1:user:refreshemptytt"
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	issuedAt := now.Add(-3599 * time.Second) // near expiry -> refresh

	cache := newFakeCache()
	cache.data[session] = makeCacheBytes(session, issuedAt, 3600, "refresh-token")

	resp := validTokenResponse(session)
	resp.TokenType = ""
	client := &fakeOAuthClient{exchangeResp: resp, endpointURL: DefaultEndpoint}
	p := NewProvider(session, cache, func(string) (OAuthClient, error) { return client, nil }, func() time.Time { return now })

	val, err := p.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected error for empty refreshed TokenType")
	}
	if val.AccessKeyID != "" {
		t.Errorf("no credentials should be returned, got %q", val.AccessKeyID)
	}
	var authErr *auth.Error
	if !errors.As(err, &authErr) {
		t.Fatalf("expected *auth.Error, got %T", err)
	}
	if authErr.Kind != auth.ProtocolError {
		t.Errorf("error kind = %q, want %q", authErr.Kind, auth.ProtocolError)
	}
	// Cache must not have been overwritten with the bad refresh.
	if atomic.LoadInt32(&cache.writeCnt) != 0 {
		t.Errorf("cache should not be persisted on empty refreshed TokenType, writeCnt=%d", cache.writeCnt)
	}
}
