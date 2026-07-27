package ramrole

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth"
	"github.com/volcengine-tls/ve-tls-cli/internal/auth/sts"
)

// fakeSTSClient is a test double for the STSClient interface. It records the
// number of AssumeRole calls and the last input, and returns configurable
// credentials or an error. For the concurrency test it can block the first
// call until released.
type fakeSTSClient struct {
	mu        sync.Mutex
	calls     int32
	creds     sts.Credentials
	err       error
	lastInput sts.AssumeRoleInput
	block     chan struct{} // if non-nil, first call blocks until closed
	started   chan struct{} // if non-nil, signaled when a call begins (buffered, non-blocking send)
}

func (f *fakeSTSClient) AssumeRole(ctx context.Context, in sts.AssumeRoleInput) (sts.Credentials, error) {
	atomic.AddInt32(&f.calls, 1)
	f.mu.Lock()
	f.lastInput = in
	f.mu.Unlock()
	if f.started != nil {
		select {
		case f.started <- struct{}{}:
		default:
		}
	}
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return sts.Credentials{}, ctx.Err()
		}
	}
	return f.creds, f.err
}

func (f *fakeSTSClient) callCount() int32 { return atomic.LoadInt32(&f.calls) }

func (f *fakeSTSClient) last() sts.AssumeRoleInput {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastInput
}

// fixedClock returns a deterministic time that tests can advance.
type fixedClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fixedClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fixedClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newFixedClock(t time.Time) *fixedClock {
	return &fixedClock{now: t}
}

// baseConfig returns a valid Config for tests, sharing the given fake STS
// client and clock.
func baseConfig(client STSClient, clock func() time.Time) Config {
	return Config{
		Source: sts.SourceCredential{
			AccessKeyID:     "SRC-AK",
			SecretAccessKey: "SRC-SK",
			SessionToken:    "SRC-TOKEN",
		},
		AccountID:  "2100000000",
		RoleName:   "my-role",
		Region:     "cn-beijing",
		DisableSSL: false,
		Client:     client,
		Clock:      clock,
	}
}

// okCreds builds credentials valid for `ttl` from the given start time.
func okCreds(start time.Time, ttl time.Duration) sts.Credentials {
	return sts.Credentials{
		AccessKeyID:     "TEMP-AK",
		SecretAccessKey: "TEMP-SK",
		SessionToken:    "TEMP-TOKEN",
		ExpiresAt:       start.Add(ttl),
	}
}

func TestProviderRetrievesAndCachesAssumeRoleCredentials(t *testing.T) {
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	clock := newFixedClock(start)
	stsClient := &fakeSTSClient{creds: okCreds(start, 2*time.Hour)}
	p, err := New(baseConfig(stsClient, clock.Now))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// First retrieve must call STS.
	v, err := p.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("first Retrieve: %v", err)
	}
	if v.AccessKeyID != "TEMP-AK" || v.SecretAccessKey != "TEMP-SK" || v.SessionToken != "TEMP-TOKEN" {
		t.Fatalf("unexpected credentials: %+v", v)
	}
	if v.ProviderName != ProviderName {
		t.Fatalf("ProviderName=%q, want %q", v.ProviderName, ProviderName)
	}
	if !v.ExpiresAt.Equal(start.Add(2 * time.Hour)) {
		t.Fatalf("ExpiresAt=%v, want hard expiration %v", v.ExpiresAt, start.Add(2*time.Hour))
	}
	if got := stsClient.callCount(); got != 1 {
		t.Fatalf("STS call count=%d, want 1", got)
	}

	// Second retrieve must use the cache and not call STS again.
	v2, err := p.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("second Retrieve: %v", err)
	}
	if v2.AccessKeyID != v.AccessKeyID {
		t.Fatalf("cached value mismatch: %+v", v2)
	}
	if got := stsClient.callCount(); got != 1 {
		t.Fatalf("STS call count=%d after cache hit, want 1", got)
	}
}

func TestProviderRefreshesAtUpstreamSixtySecondBoundary(t *testing.T) {
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	clock := newFixedClock(start)
	stsClient := &fakeSTSClient{creds: okCreds(start, 2*time.Hour)}
	p, err := New(baseConfig(stsClient, clock.Now))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// First retrieve caches. refreshAt = ExpiresAt - 60s = start + 1h59m.
	if _, err := p.Retrieve(context.Background()); err != nil {
		t.Fatalf("first Retrieve: %v", err)
	}
	if got := stsClient.callCount(); got != 1 {
		t.Fatalf("STS call count=%d, want 1", got)
	}

	// Advance just past the refresh boundary (1h59m + 1s).
	clock.Advance(1*time.Hour + 59*time.Minute + 1*time.Second)

	// The second STS call must return a fresh expiration that still has >60s
	// from the refresh time, otherwise the new fail-closed check would reject
	// it. Update the fake to return creds relative to the current time.
	stsClient.creds = okCreds(clock.Now(), 2*time.Hour)

	// Next retrieve must refresh: a second STS call happens.
	if _, err := p.Retrieve(context.Background()); err != nil {
		t.Fatalf("refresh Retrieve: %v", err)
	}
	if got := stsClient.callCount(); got != 2 {
		t.Fatalf("STS call count=%d, want 2 (refresh at boundary)", got)
	}
}

func TestProviderConcurrentRetrieveRefreshesOnce(t *testing.T) {
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	clock := newFixedClock(start)
	stsClient := &fakeSTSClient{
		creds:   okCreds(start, 2*time.Hour),
		block:   make(chan struct{}),
		started: make(chan struct{}, 1),
	}
	p, err := New(baseConfig(stsClient, clock.Now))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// release closes the leader's block channel exactly once.
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(stsClient.block) }) }

	// Leader starts the refresh and blocks inside the STS call.
	leaderDone := make(chan struct{})
	go func() {
		_, _ = p.Retrieve(context.Background())
		close(leaderDone)
	}()
	<-stsClient.started

	// A normal (non-canceled) waiter contends on the gate while the leader is
	// blocked. The observed channel fires when the waiter evaluates the gate
	// select's cancellation arm, proving it reached gate contention before the
	// leader is released.
	waiterCtx := &observedCtx{Context: context.Background(), observed: make(chan struct{})}
	waiterErr := make(chan error, 1)
	waiterVal := make(chan auth.Value, 1)
	waiterDone := make(chan struct{})
	go func() {
		defer close(waiterDone)
		v, e := p.Retrieve(waiterCtx)
		waiterVal <- v
		waiterErr <- e
	}()

	// Deferred cleanup: release the leader and join both goroutines on every
	// exit path so none leak. Idempotent.
	defer func() {
		release()
		select {
		case <-waiterDone:
		case <-time.After(2 * time.Second):
		}
		select {
		case <-leaderDone:
		case <-time.After(2 * time.Second):
		}
	}()

	select {
	case <-waiterCtx.observed:
	case <-time.After(2 * time.Second):
		t.Fatal("waiter never reached gate contention")
	}

	// Release the leader; the waiter must then use the leader's cached value via
	// the second cache check after acquiring the gate.
	release()

	select {
	case <-leaderDone:
	case <-time.After(2 * time.Second):
		t.Fatal("leader did not finish after release")
	}
	select {
	case e := <-waiterErr:
		if e != nil {
			t.Fatalf("waiter Retrieve: %v", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("waiter did not return after leader release")
	}

	// Waiter must have received the leader's credentials, proving the second
	// cache check was used rather than a second upstream call.
	v := <-waiterVal
	if v.AccessKeyID != "TEMP-AK" || v.ProviderName != ProviderName {
		t.Fatalf("waiter credentials=%+v, want leader credentials", v)
	}
	if got := stsClient.callCount(); got != 1 {
		t.Fatalf("STS call count=%d, want 1 (waiter used cache, not a second refresh)", got)
	}
}

// observedCtx wraps a context and signals an observed channel exactly once when
// Done() is first called. This lets a test deterministically prove that a
// goroutine reached the cancellation arm of a select without sleep-based
// scheduling guesses.
type observedCtx struct {
	context.Context
	once     sync.Once
	observed chan struct{}
}

func (c *observedCtx) Done() <-chan struct{} {
	c.once.Do(func() { close(c.observed) })
	return c.Context.Done()
}

// TestProviderBlockedWaiterReturnsContextCanceled proves that a caller whose
// context is canceled while a leader holds the refresh gate returns
// context.Canceled immediately, before the leader completes. It must never
// later return the leader's cache for that canceled wait.
func TestProviderBlockedWaiterReturnsContextCanceled(t *testing.T) {
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	clock := newFixedClock(start)
	stsClient := &fakeSTSClient{
		creds:   okCreds(start, 2*time.Hour),
		block:   make(chan struct{}),
		started: make(chan struct{}, 1),
	}
	p, err := New(baseConfig(stsClient, clock.Now))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// release closes the leader's block channel exactly once, safe to call from
	// both the success path and the deferred cleanup.
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(stsClient.block) }) }

	// Leader starts a refresh and blocks inside the STS call.
	leaderDone := make(chan struct{})
	go func() {
		_, _ = p.Retrieve(context.Background())
		close(leaderDone)
	}()
	<-stsClient.started

	// Waiter contends on the gate while the leader is blocked. The observed
	// channel fires exactly when the waiter evaluates the cancellation arm of
	// the gate select, proving it reached contention before we cancel.
	baseCtx, cancel := context.WithCancel(context.Background())
	waiterCtx := &observedCtx{Context: baseCtx, observed: make(chan struct{})}
	waiterErr := make(chan error, 1)
	waiterDone := make(chan struct{})
	go func() {
		defer close(waiterDone)
		_, e := p.Retrieve(waiterCtx)
		waiterErr <- e
	}()

	// Deferred cleanup runs on every exit path (including t.Fatal): cancel the
	// waiter, release the leader, and boundedly join both goroutines so none
	// leak even if an earlier assertion failed. Idempotent: closed channels are
	// observed immediately.
	defer func() {
		cancel()
		release()
		select {
		case <-waiterDone:
		case <-time.After(2 * time.Second):
		}
		select {
		case <-leaderDone:
		case <-time.After(2 * time.Second):
		}
	}()

	// Wait deterministically for the waiter to reach the gate select, then
	// cancel. The waiter must return context.Canceled before the leader is
	// released.
	select {
	case <-waiterCtx.observed:
	case <-time.After(2 * time.Second):
		t.Fatal("waiter never reached gate contention")
	}
	cancel()

	select {
	case e := <-waiterErr:
		if !errors.Is(e, context.Canceled) {
			t.Fatalf("waiter error=%v, want context.Canceled", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("waiter did not return after context cancellation")
	}

	// Leader is still blocked; only one STS call (the leader's) has happened.
	if got := stsClient.callCount(); got != 1 {
		t.Fatalf("STS call count=%d, want 1 (waiter must not refresh)", got)
	}

	// Explicit release and join on the success path; deferred cleanup is
	// idempotent and will observe the already-closed channels.
	release()
	select {
	case <-leaderDone:
	case <-time.After(2 * time.Second):
		t.Fatal("leader did not finish after release")
	}
}

func TestProviderRefreshFailureFailsClosed(t *testing.T) {
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	clock := newFixedClock(start)
	stsClient := &fakeSTSClient{creds: okCreds(start, 2*time.Hour)}
	p, err := New(baseConfig(stsClient, clock.Now))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// First retrieve succeeds and caches.
	if _, err := p.Retrieve(context.Background()); err != nil {
		t.Fatalf("first Retrieve: %v", err)
	}

	// Advance past refreshAt so the next retrieve must refresh.
	clock.Advance(2 * time.Hour)

	// Make STS fail on the refresh attempt.
	stsClient.err = errors.New("STS boom")

	// The provider must fail closed: it must not return the stale cached value.
	_, err = p.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected error when refresh fails past refreshAt, got nil")
	}
	if got := stsClient.callCount(); got != 2 {
		t.Fatalf("STS call count=%d, want 2 (initial + failed refresh)", got)
	}

	// Recover: change the fake to a valid response and retrieve again. The gate
	// must have been released after the failure so a new refresh can proceed;
	// the stale cache must not be silently reused.
	stsClient.err = nil
	stsClient.creds = okCreds(clock.Now(), 2*time.Hour)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	v, err := p.Retrieve(ctx)
	if err != nil {
		t.Fatalf("recover Retrieve after failure: %v", err)
	}
	if v.AccessKeyID != "TEMP-AK" {
		t.Fatalf("recovered credentials=%+v, want fresh credentials", v)
	}
	if got := stsClient.callCount(); got != 3 {
		t.Fatalf("STS call count=%d, want 3 (initial + failed + recovered refresh)", got)
	}
}

func TestProviderPropagatesSourceSessionToken(t *testing.T) {
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	clock := newFixedClock(start)
	stsClient := &fakeSTSClient{creds: okCreds(start, 2*time.Hour)}
	cfg := baseConfig(stsClient, clock.Now)
	cfg.Source.SessionToken = "CHAIN-TOKEN"
	p, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := p.Retrieve(context.Background()); err != nil {
		t.Fatalf("Retrieve: %v", err)
	}

	in := stsClient.last()
	if in.Source.SessionToken != "CHAIN-TOKEN" {
		t.Fatalf("source SessionToken=%q, want CHAIN-TOKEN", in.Source.SessionToken)
	}
	if in.Source.AccessKeyID != "SRC-AK" || in.Source.SecretAccessKey != "SRC-SK" {
		t.Fatalf("source AK/SK not propagated: %+v", in.Source)
	}
	if in.AccountID != "2100000000" || in.RoleName != "my-role" {
		t.Fatalf("AccountID/RoleName not propagated: %+v", in)
	}
	if in.Region != "cn-beijing" {
		t.Fatalf("Region=%q, want cn-beijing", in.Region)
	}
	if in.DisableSSL != false {
		t.Fatalf("DisableSSL=%v, want false", in.DisableSSL)
	}
}

func TestProviderRejectsIncompleteReturnedCredentials(t *testing.T) {
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	clock := newFixedClock(start)
	// STS returns credentials missing the session token (incomplete).
	stsClient := &fakeSTSClient{creds: sts.Credentials{
		AccessKeyID:     "TEMP-AK",
		SecretAccessKey: "TEMP-SK",
		SessionToken:    "",
		ExpiresAt:       start.Add(2 * time.Hour),
	}}
	p, err := New(baseConfig(stsClient, clock.Now))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = p.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected error for incomplete returned credentials, got nil")
	}
	var authErr *auth.Error
	if !errors.As(err, &authErr) {
		t.Fatalf("error=%T %v, want *auth.Error", err, err)
	}
}

func TestProviderRespectsCanceledContext(t *testing.T) {
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	clock := newFixedClock(start)
	stsClient := &fakeSTSClient{creds: okCreds(start, 2*time.Hour)}
	p, err := New(baseConfig(stsClient, clock.Now))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = p.Retrieve(ctx)
	if err == nil {
		t.Fatal("expected error for canceled context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v, want context.Canceled", err)
	}
	// STS must not have been called.
	if got := stsClient.callCount(); got != 0 {
		t.Fatalf("STS call count=%d, want 0 for canceled context", got)
	}
}

func TestNewValidatesConfigAndNilClient(t *testing.T) {
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	clock := newFixedClock(start)
	stsClient := &fakeSTSClient{creds: okCreds(start, 2*time.Hour)}

	t.Run("nil client", func(t *testing.T) {
		cfg := baseConfig(stsClient, clock.Now)
		cfg.Client = nil
		_, err := New(cfg)
		if err == nil {
			t.Fatal("expected error for nil client")
		}
	})

	t.Run("nil clock", func(t *testing.T) {
		cfg := baseConfig(stsClient, clock.Now)
		cfg.Clock = nil
		_, err := New(cfg)
		if err == nil {
			t.Fatal("expected error for nil clock")
		}
	})

	t.Run("missing source ak", func(t *testing.T) {
		cfg := baseConfig(stsClient, clock.Now)
		cfg.Source.AccessKeyID = ""
		_, err := New(cfg)
		if err == nil {
			t.Fatal("expected error for missing source AK")
		}
	})

	t.Run("missing role name", func(t *testing.T) {
		cfg := baseConfig(stsClient, clock.Now)
		cfg.RoleName = ""
		_, err := New(cfg)
		if err == nil {
			t.Fatal("expected error for missing role name")
		}
	})

	t.Run("missing account id", func(t *testing.T) {
		cfg := baseConfig(stsClient, clock.Now)
		cfg.AccountID = ""
		_, err := New(cfg)
		if err == nil {
			t.Fatal("expected error for missing account id")
		}
	})

	t.Run("valid config", func(t *testing.T) {
		p, err := New(baseConfig(stsClient, clock.Now))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		var _ auth.Provider = p
	})
}

// ---------------------------------------------------------------------------
// No-refresh-window fail-closed: credentials whose ExpiresAt is within 60s
// (or already past) must be rejected before any cache write.
// ---------------------------------------------------------------------------

func TestProviderRejectsCredentialsWithoutSafeRefreshWindow(t *testing.T) {
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		name      string
		expiresAt time.Time
		wantErr   bool
	}{
		{name: "already expired", expiresAt: start.Add(-1 * time.Hour), wantErr: true},
		{name: "only 30s left", expiresAt: start.Add(30 * time.Second), wantErr: true},
		{name: "exactly 60s left", expiresAt: start.Add(60 * time.Second), wantErr: true},
		{name: "more than 60s left", expiresAt: start.Add(120 * time.Second), wantErr: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clock := newFixedClock(start)
			stsClient := &fakeSTSClient{creds: sts.Credentials{
				AccessKeyID:     "TEMP-AK",
				SecretAccessKey: "TEMP-SK",
				SessionToken:    "TEMP-TOKEN",
				ExpiresAt:       tc.expiresAt,
			}}
			p, err := New(baseConfig(stsClient, clock.Now))
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			_, err = p.Retrieve(context.Background())
			if tc.wantErr && err == nil {
				t.Fatal("expected error for credentials without safe refresh window, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tc.wantErr {
				// Cache must remain empty: a subsequent retrieve must still call STS.
				if got := stsClient.callCount(); got != 1 {
					t.Fatalf("STS call count=%d, want 1 (no cache write on rejection)", got)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Typed-nil client rejection
// ---------------------------------------------------------------------------

// typedNilSTSClient is a concrete STSClient whose nil pointer is assignable to
// the STSClient interface, simulating a typed-nil passed by the caller.
type typedNilSTSClient struct{}

func (*typedNilSTSClient) AssumeRole(context.Context, sts.AssumeRoleInput) (sts.Credentials, error) {
	return sts.Credentials{}, nil
}

func TestNewRejectsTypedNilClient(t *testing.T) {
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	clock := newFixedClock(start)
	cfg := baseConfig(&fakeSTSClient{creds: okCreds(start, 2*time.Hour)}, clock.Now)
	// Assign a typed-nil: the interface is non-nil but the concrete value is nil.
	var nilClient *typedNilSTSClient
	cfg.Client = nilClient

	_, err := New(cfg)
	if err == nil {
		t.Fatal("expected error for typed-nil STS client, got nil")
	}
}

// ---------------------------------------------------------------------------
// Nil receiver and nil context safety
// ---------------------------------------------------------------------------

func TestNilProviderRetrieveReturnsError(t *testing.T) {
	var p *Provider
	_, err := p.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected error from nil *Provider, got nil")
	}
}

func TestRetrieveWithNilContextReturnsError(t *testing.T) {
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	clock := newFixedClock(start)
	stsClient := &fakeSTSClient{creds: okCreds(start, 2*time.Hour)}
	p, err := New(baseConfig(stsClient, clock.Now))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	//nolint:staticcheck // intentionally passing nil context to test fail-closed
	_, err = p.Retrieve(nil)
	if err == nil {
		t.Fatal("expected error for nil context, got nil")
	}
	if got := stsClient.callCount(); got != 0 {
		t.Fatalf("STS call count=%d, want 0 for nil context", got)
	}
}

// ---------------------------------------------------------------------------
// Cache is not polluted when short-TTL credentials are rejected
// ---------------------------------------------------------------------------

func TestRejectionDoesNotPolluteCache(t *testing.T) {
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	clock := newFixedClock(start)
	// Credential expires in 30s: refreshAt = start - 30s, already in the past.
	stsClient := &fakeSTSClient{creds: okCreds(start, 30*time.Second)}
	p, err := New(baseConfig(stsClient, clock.Now))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = p.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected error for short-TTL credential, got nil")
	}

	// Cache must remain untouched: both fields are zero values.
	if !p.cached.ExpiresAt.IsZero() {
		t.Fatalf("cached ExpiresAt=%v, want zero (cache must not be polluted)", p.cached.ExpiresAt)
	}
	if !p.refreshAt.IsZero() {
		t.Fatalf("refreshAt=%v, want zero (cache must not be polluted)", p.refreshAt)
	}
	if p.cached.AccessKeyID != "" || p.cached.SecretAccessKey != "" || p.cached.SessionToken != "" {
		t.Fatalf("cached credentials populated: %+v, want empty", p.cached)
	}
}

func TestShortTTLRefreshDoesNotClobberExistingCache(t *testing.T) {
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	clock := newFixedClock(start)
	stsClient := &fakeSTSClient{creds: okCreds(start, 2*time.Hour)}
	p, err := New(baseConfig(stsClient, clock.Now))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// First retrieve: cache a valid credential.
	v1, err := p.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("first Retrieve: %v", err)
	}
	oldCached := p.cached
	oldRefreshAt := p.refreshAt

	// Advance past refreshAt so the next retrieve must refresh.
	clock.Advance(2 * time.Hour)

	// Second STS call returns a credential with only 30s left (no safe window).
	stsClient.creds = okCreds(clock.Now(), 30*time.Second)

	v2, err := p.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected error when refresh returns short-TTL credential, got nil")
	}
	// The caller must not receive the old cached value.
	if v2.AccessKeyID != "" || v2.SecretAccessKey != "" || v2.SessionToken != "" {
		t.Fatalf("Retrieve returned stale credentials on failure: %+v", v2)
	}

	// The existing cache must be completely unchanged.
	if p.cached != oldCached {
		t.Fatalf("cached changed after failed refresh:\n got:  %+v\n want: %+v", p.cached, oldCached)
	}
	if !p.refreshAt.Equal(oldRefreshAt) {
		t.Fatalf("refreshAt changed after failed refresh: got %v, want %v", p.refreshAt, oldRefreshAt)
	}
	// v1 (the previously returned value) must still match the unchanged cache.
	if v1 != oldCached {
		t.Fatalf("previously returned value no longer matches cache")
	}
}

// ---------------------------------------------------------------------------
// Clock is re-read after the STS round-trip
// ---------------------------------------------------------------------------

// clockAdvancingSTSClient advances the shared clock by advance on every
// AssumeRole call, simulating time elapsed during the network round-trip.
type clockAdvancingSTSClient struct {
	clock   *fixedClock
	creds   sts.Credentials
	advance time.Duration
	calls   int32
}

func (c *clockAdvancingSTSClient) AssumeRole(ctx context.Context, _ sts.AssumeRoleInput) (sts.Credentials, error) {
	atomic.AddInt32(&c.calls, 1)
	c.clock.Advance(c.advance)
	return c.creds, nil
}

func (c *clockAdvancingSTSClient) callCount() int32 { return atomic.LoadInt32(&c.calls) }

func TestClockReReadAfterResponseRejectsShortTTL(t *testing.T) {
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	clock := newFixedClock(start)
	// Credential expires in 61s: refreshAt = start + 1s. Before the call the
	// clock is at start, so refreshAt is in the future — but the fake advances
	// the clock by 2s during the call, pushing it past refreshAt.
	stsClient := &clockAdvancingSTSClient{
		clock:   clock,
		creds:   okCreds(start, 61*time.Second),
		advance: 2 * time.Second,
	}
	p, err := New(baseConfig(stsClient, clock.Now))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = p.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected error: credential expired during round-trip, got nil")
	}
	// Cache must be empty.
	if !p.cached.ExpiresAt.IsZero() {
		t.Fatalf("cached ExpiresAt=%v, want zero", p.cached.ExpiresAt)
	}
}

// ---------------------------------------------------------------------------
// typed-nil context.Context is rejected without calling STS
// ---------------------------------------------------------------------------

// nilContext is a context.Context implementation whose methods panic if called
// on a nil receiver. A nil *nilContext stored in a context.Context interface is
// a typed-nil: interface comparison to nil is false, but calling methods panics.
type nilContext struct{}

func (*nilContext) Deadline() (time.Time, bool) { panic("nilContext.Deadline called") }
func (*nilContext) Done() <-chan struct{}       { panic("nilContext.Done called") }
func (*nilContext) Err() error                  { panic("nilContext.Err called") }
func (*nilContext) Value(_ any) any             { panic("nilContext.Value called") }

func TestRetrieveWithTypedNilContextReturnsError(t *testing.T) {
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	clock := newFixedClock(start)
	stsClient := &fakeSTSClient{creds: okCreds(start, 2*time.Hour)}
	p, err := New(baseConfig(stsClient, clock.Now))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Build a typed-nil context: the interface is non-nil but the concrete
	// pointer is nil. Calling any method would panic.
	var nc *nilContext
	var ctx context.Context = nc

	_, err = p.Retrieve(ctx)
	if err == nil {
		t.Fatal("expected error for typed-nil context, got nil")
	}
	if got := stsClient.callCount(); got != 0 {
		t.Fatalf("STS call count=%d, want 0 for typed-nil context", got)
	}
}
