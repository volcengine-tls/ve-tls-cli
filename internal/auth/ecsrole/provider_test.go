package ecsrole

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth"
)

// fakeCredentialClient is a test double for CredentialClient. It can block on a
// channel, count calls, and return configurable credentials or errors.
type fakeCredentialClient struct {
	mu       sync.Mutex
	calls    int32
	creds    Credentials
	err      error
	block    chan struct{} // if non-nil, FetchCredentials blocks until closed
	started  chan struct{} // if non-nil, signaled when FetchCredentials begins
	roleSeen string
}

func (f *fakeCredentialClient) FetchCredentials(ctx context.Context, roleName string) (Credentials, error) {
	atomic.AddInt32(&f.calls, 1)
	f.mu.Lock()
	f.roleSeen = roleName
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
			return Credentials{}, ctx.Err()
		}
	}
	return f.creds, f.err
}

func (f *fakeCredentialClient) callCount() int32 { return atomic.LoadInt32(&f.calls) }

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

func newFixedClock(t time.Time) *fixedClock { return &fixedClock{now: t} }

func okCreds(start time.Time, ttl time.Duration) Credentials {
	return Credentials{
		AccessKeyID:     "TEMP-AK",
		SecretAccessKey: "TEMP-SK",
		SessionToken:    "TEMP-TOKEN",
		ExpiresAt:       start.Add(ttl),
	}
}

func TestProviderCachesUntilExpiredTimeMinusFiveMinutes(t *testing.T) {
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	client := &fakeCredentialClient{creds: okCreds(start, 2*time.Hour)}
	clock := newFixedClock(start)
	p, err := New(Config{RoleName: "r", Client: client, Clock: clock.Now})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	v, err := p.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("first Retrieve: %v", err)
	}
	if v.ProviderName != ProviderName {
		t.Fatalf("ProviderName=%q, want %q", v.ProviderName, ProviderName)
	}
	if !v.ExpiresAt.Equal(start.Add(2 * time.Hour)) {
		t.Fatalf("ExpiresAt=%v, want %v", v.ExpiresAt, start.Add(2*time.Hour))
	}
	if got := client.callCount(); got != 1 {
		t.Fatalf("client calls=%d, want 1", got)
	}

	// Second retrieve uses cache.
	if _, err := p.Retrieve(context.Background()); err != nil {
		t.Fatalf("second Retrieve: %v", err)
	}
	if got := client.callCount(); got != 1 {
		t.Fatalf("client calls=%d, want 1 (cache hit)", got)
	}

	// Advance past refreshAt (ExpiredTime - 5m) and retrieve again.
	clock.Advance(1*time.Hour + 55*time.Minute + 1*time.Second)
	client.creds = okCreds(clock.Now(), 2*time.Hour)
	if _, err := p.Retrieve(context.Background()); err != nil {
		t.Fatalf("refresh Retrieve: %v", err)
	}
	if got := client.callCount(); got != 2 {
		t.Fatalf("client calls=%d, want 2 (refresh)", got)
	}
}

func TestProviderFetchesFreshTokenForEveryCredentialRefresh(t *testing.T) {
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	client := &fakeCredentialClient{creds: okCreds(start, 2*time.Hour)}
	clock := newFixedClock(start)
	p, err := New(Config{RoleName: "r", Client: client, Clock: clock.Now})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := p.Retrieve(context.Background()); err != nil {
		t.Fatalf("first Retrieve: %v", err)
	}
	clock.Advance(2 * time.Hour)
	client.creds = okCreds(clock.Now(), 2*time.Hour)
	if _, err := p.Retrieve(context.Background()); err != nil {
		t.Fatalf("second Retrieve: %v", err)
	}
	if got := client.callCount(); got != 2 {
		t.Fatalf("client calls=%d, want 2 (fresh fetch each refresh)", got)
	}
}

func TestProviderConcurrentRetrieveRefreshesOnce(t *testing.T) {
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	client := &fakeCredentialClient{
		creds:   okCreds(start, 2*time.Hour),
		block:   make(chan struct{}),
		started: make(chan struct{}, 1),
	}
	clock := newFixedClock(start)
	p, err := New(Config{RoleName: "r", Client: client, Clock: clock.Now})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// release closes the leader's block channel exactly once.
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(client.block) }) }

	// Leader starts the refresh and blocks inside FetchCredentials.
	leaderDone := make(chan struct{})
	go func() {
		_, _ = p.Retrieve(context.Background())
		close(leaderDone)
	}()
	<-client.started

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

	// Release the leader; the waiter must use the cached value via the second
	// cache check after acquiring the gate.
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

	v := <-waiterVal
	if v.AccessKeyID != "TEMP-AK" || v.ProviderName != ProviderName {
		t.Fatalf("waiter credentials=%+v, want leader credentials", v)
	}
	if got := client.callCount(); got != 1 {
		t.Fatalf("client calls=%d, want 1 (waiter used cache, not a second refresh)", got)
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
// context.Canceled immediately, before the leader completes.
func TestProviderBlockedWaiterReturnsContextCanceled(t *testing.T) {
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	client := &fakeCredentialClient{
		creds:   okCreds(start, 2*time.Hour),
		block:   make(chan struct{}),
		started: make(chan struct{}, 1),
	}
	clock := newFixedClock(start)
	p, err := New(Config{RoleName: "r", Client: client, Clock: clock.Now})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// release closes the leader's block channel exactly once.
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(client.block) }) }

	leaderDone := make(chan struct{})
	go func() {
		_, _ = p.Retrieve(context.Background())
		close(leaderDone)
	}()
	<-client.started

	baseCtx, cancel := context.WithCancel(context.Background())
	waiterCtx := &observedCtx{Context: baseCtx, observed: make(chan struct{})}
	waiterErr := make(chan error, 1)
	waiterDone := make(chan struct{})
	go func() {
		defer close(waiterDone)
		_, e := p.Retrieve(waiterCtx)
		waiterErr <- e
	}()

	// Deferred cleanup runs on every exit path: cancel the waiter, release the
	// leader, and boundedly join both goroutines so none leak. Idempotent.
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

	if got := client.callCount(); got != 1 {
		t.Fatalf("client calls=%d, want 1 (waiter must not refresh)", got)
	}

	release()
	select {
	case <-leaderDone:
	case <-time.After(2 * time.Second):
		t.Fatal("leader did not finish after release")
	}
}

func TestProviderRefreshFailureFailsClosed(t *testing.T) {
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	client := &fakeCredentialClient{creds: okCreds(start, 2*time.Hour)}
	clock := newFixedClock(start)
	p, err := New(Config{RoleName: "r", Client: client, Clock: clock.Now})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := p.Retrieve(context.Background()); err != nil {
		t.Fatalf("first Retrieve: %v", err)
	}

	// Make the client fail on the next call.
	client.err = errors.New("IMDS unavailable")
	clock.Advance(2 * time.Hour)

	_, err = p.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected error when refresh fails")
	}
	if got := client.callCount(); got != 2 {
		t.Fatalf("client calls=%d, want 2 (initial + failed refresh)", got)
	}

	// Recover: clear the error and retrieve again. The gate must have been
	// released after the failure so a new refresh can proceed; the stale cache
	// must not be silently reused.
	client.err = nil
	client.creds = okCreds(clock.Now(), 2*time.Hour)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	v, err := p.Retrieve(ctx)
	if err != nil {
		t.Fatalf("recover Retrieve after failure: %v", err)
	}
	if v.AccessKeyID != "TEMP-AK" {
		t.Fatalf("recovered credentials=%+v, want fresh credentials", v)
	}
	if got := client.callCount(); got != 3 {
		t.Fatalf("client calls=%d, want 3 (initial + failed + recovered refresh)", got)
	}
}

func TestProviderNonECSFailurePreventsFallback(t *testing.T) {
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	// Client that always fails (simulates non-ECS host).
	client := &fakeCredentialClient{err: errors.New("404 not found")}
	clock := newFixedClock(start)
	p, err := New(Config{RoleName: "r", Client: client, Clock: clock.Now})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = p.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected error on non-ECS host")
	}
	// No fallback: the provider returns the error, not some other credential.
}

func TestProviderCompleteRefreshStopsAtFiveSecondBudget(t *testing.T) {
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	// Client blocks forever; the budget must cancel it.
	client := &fakeCredentialClient{block: make(chan struct{})}
	clock := newFixedClock(start)
	p, err := New(Config{RoleName: "r", Client: client, Clock: clock.Now})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Shorten the budget for a fast, deterministic test.
	p.budget = 100 * time.Millisecond

	done := make(chan error, 1)
	go func() {
		_, err := p.Retrieve(context.Background())
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error when budget expires")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Retrieve did not stop at budget deadline")
	}
}

func TestProviderErrorsNeverExposeMetadataOrCredentials(t *testing.T) {
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	client := &fakeCredentialClient{err: errors.New("IMDS token SECRET-IMDS-TOKEN and AK SECRET-AK leaked")}
	clock := newFixedClock(start)
	p, err := New(Config{RoleName: "r", Client: client, Clock: clock.Now})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = p.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	text := err.Error()
	for _, secret := range []string{"SECRET-IMDS-TOKEN", "SECRET-AK"} {
		if strings.Contains(text, secret) {
			t.Fatalf("error leaked %q: %q", secret, text)
		}
	}
}

func TestProviderRejectsShortTTLCredentials(t *testing.T) {
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	// ExpiredTime is only 1 minute away: refreshAt would be in the past.
	client := &fakeCredentialClient{creds: okCreds(start, 1*time.Minute)}
	clock := newFixedClock(start)
	p, err := New(Config{RoleName: "r", Client: client, Clock: clock.Now})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = p.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected error for short-TTL credential")
	}
	if !p.cached.ExpiresAt.IsZero() {
		t.Fatalf("cache polluted: ExpiresAt=%v", p.cached.ExpiresAt)
	}
}

func TestProviderShortTTLRefreshDoesNotPolluteCache(t *testing.T) {
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	client := &fakeCredentialClient{creds: okCreds(start, 2*time.Hour)}
	clock := newFixedClock(start)
	p, err := New(Config{RoleName: "r", Client: client, Clock: clock.Now})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := p.Retrieve(context.Background()); err != nil {
		t.Fatalf("first Retrieve: %v", err)
	}
	oldCached := p.cached
	oldRefreshAt := p.refreshAt

	// Advance clock and return a short-TTL credential on refresh.
	clock.Advance(2 * time.Hour)
	client.creds = okCreds(clock.Now(), 1*time.Minute)

	v, err := p.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected error when refresh returns short-TTL credential")
	}
	if v.AccessKeyID != "" {
		t.Fatalf("stale value returned: %+v", v)
	}
	if p.cached != oldCached {
		t.Fatalf("cache changed after failed refresh")
	}
	if !p.refreshAt.Equal(oldRefreshAt) {
		t.Fatalf("refreshAt changed after failed refresh")
	}
}

func TestProviderPassesExplicitRoleNameToClient(t *testing.T) {
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	client := &fakeCredentialClient{creds: okCreds(start, 2*time.Hour)}
	clock := newFixedClock(start)
	p, err := New(Config{RoleName: "explicit-role", Client: client, Clock: clock.Now})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := p.Retrieve(context.Background()); err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	client.mu.Lock()
	got := client.roleSeen
	client.mu.Unlock()
	if got != "explicit-role" {
		t.Fatalf("client received role=%q, want explicit-role", got)
	}
}

func TestProviderNilAndTypedNilSafety(t *testing.T) {
	t.Run("nil provider", func(t *testing.T) {
		var p *Provider
		_, err := p.Retrieve(context.Background())
		if err == nil {
			t.Fatal("expected error from nil *Provider")
		}
	})
	t.Run("nil context", func(t *testing.T) {
		start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
		client := &fakeCredentialClient{creds: okCreds(start, 2*time.Hour)}
		clock := newFixedClock(start)
		p, err := New(Config{RoleName: "r", Client: client, Clock: clock.Now})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		//nolint:staticcheck
		_, err = p.Retrieve(nil)
		if err == nil {
			t.Fatal("expected error for nil context")
		}
	})
	t.Run("typed-nil client", func(t *testing.T) {
		_, err := New(Config{RoleName: "r", Client: nil, Clock: func() time.Time { return time.Time{} }})
		if err == nil {
			t.Fatal("expected error for nil client")
		}
	})
}

func TestProviderRejectsEmptyRoleName(t *testing.T) {
	_, err := New(Config{RoleName: "", Client: &fakeCredentialClient{}, Clock: func() time.Time { return time.Time{} }})
	if err == nil {
		t.Fatal("expected error for empty role name")
	}
}

func TestProviderRespectsCanceledContext(t *testing.T) {
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	client := &fakeCredentialClient{creds: okCreds(start, 2*time.Hour)}
	clock := newFixedClock(start)
	p, err := New(Config{RoleName: "r", Client: client, Clock: clock.Now})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = p.Retrieve(ctx)
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}

// --- New validation/wrap/typed-nil tests ---

func TestProviderRejectsEachMissingCredentialField(t *testing.T) {
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	clock := newFixedClock(start)
	cases := []struct {
		name  string
		creds Credentials
	}{
		{"missing AK", Credentials{SecretAccessKey: "SK", SessionToken: "ST", ExpiresAt: start.Add(2 * time.Hour)}},
		{"missing SK", Credentials{AccessKeyID: "AK", SessionToken: "ST", ExpiresAt: start.Add(2 * time.Hour)}},
		{"missing ST", Credentials{AccessKeyID: "AK", SecretAccessKey: "SK", ExpiresAt: start.Add(2 * time.Hour)}},
		{"missing ExpiresAt", Credentials{AccessKeyID: "AK", SecretAccessKey: "SK", SessionToken: "ST"}},
		{"blank AK", Credentials{AccessKeyID: "   ", SecretAccessKey: "SK", SessionToken: "ST", ExpiresAt: start.Add(2 * time.Hour)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := &fakeCredentialClient{creds: tc.creds}
			p, err := New(Config{RoleName: "r", Client: client, Clock: clock.Now})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			_, err = p.Retrieve(context.Background())
			if err == nil {
				t.Fatal("expected error for incomplete credential")
			}
			if !p.cached.ExpiresAt.IsZero() {
				t.Fatalf("cache polluted: %+v", p.cached)
			}
		})
	}
}

func TestProviderInvalidRefreshDoesNotPolluteCache(t *testing.T) {
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	client := &fakeCredentialClient{creds: okCreds(start, 2*time.Hour)}
	clock := newFixedClock(start)
	p, err := New(Config{RoleName: "r", Client: client, Clock: clock.Now})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := p.Retrieve(context.Background()); err != nil {
		t.Fatalf("first Retrieve: %v", err)
	}
	oldCached := p.cached
	oldRefreshAt := p.refreshAt

	clock.Advance(2 * time.Hour)
	// Return an invalid credential on refresh.
	client.creds = Credentials{AccessKeyID: "", SecretAccessKey: "SK", SessionToken: "ST", ExpiresAt: clock.Now().Add(2 * time.Hour)}

	v, err := p.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected error when refresh returns invalid credential")
	}
	if v.AccessKeyID != "" {
		t.Fatalf("stale value returned: %+v", v)
	}
	if p.cached != oldCached {
		t.Fatalf("cache changed after failed refresh")
	}
	if !p.refreshAt.Equal(oldRefreshAt) {
		t.Fatalf("refreshAt changed after failed refresh")
	}
}

func TestProviderWrapClientErrorHidesSecretAndPreservesFields(t *testing.T) {
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	clock := newFixedClock(start)
	secretErr := &auth.Error{
		Kind:        auth.ProtocolError,
		Status:      429,
		RequestID:   "req-xyz",
		ServiceCode: "Throttling",
		Description: "IMDS token SECRET-TOKEN and AK SECRET-AK leaked",
	}
	client := &fakeCredentialClient{err: secretErr}
	p, err := New(Config{RoleName: "r", Client: client, Clock: clock.Now})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = p.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	text := err.Error()
	for _, secret := range []string{"SECRET-TOKEN", "SECRET-AK"} {
		if strings.Contains(text, secret) {
			t.Fatalf("error leaked %q: %q", secret, text)
		}
	}
	var got *auth.Error
	if !errors.As(err, &got) {
		t.Fatalf("wrapped is not *auth.Error: %T", err)
	}
	if got.Status != 429 {
		t.Fatalf("Status=%d, want 429", got.Status)
	}
	if got.RequestID != "req-xyz" {
		t.Fatalf("RequestID=%q, want req-xyz", got.RequestID)
	}
	if got.ServiceCode != "Throttling" {
		t.Fatalf("ServiceCode=%q, want Throttling", got.ServiceCode)
	}
	if got.Description == secretErr.Description {
		t.Fatal("Description was copied from source (must be safe text)")
	}
	if !errors.Is(err, secretErr) {
		t.Fatal("errors.Is(wrapped, secretErr) = false, want true")
	}
}

func TestProviderWrapClientErrorHandlesTypedNil(t *testing.T) {
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	clock := newFixedClock(start)
	var nilErr *auth.Error
	client := &fakeCredentialClient{err: nilErr}
	p, err := New(Config{RoleName: "r", Client: client, Clock: clock.Now})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = p.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected error for typed-nil source")
	}
	var got *auth.Error
	if !errors.As(err, &got) {
		t.Fatalf("wrapped is not *auth.Error: %T", err)
	}
	if got.Kind != auth.ProtocolError {
		t.Fatalf("Kind=%q, want default ProtocolError", got.Kind)
	}
}

func TestProviderTypedNilClientRejected(t *testing.T) {
	// A typed-nil *Client stored in the CredentialClient interface must be rejected.
	var nilClient *Client
	_, err := New(Config{RoleName: "r", Client: nilClient, Clock: func() time.Time { return time.Time{} }})
	if err == nil {
		t.Fatal("expected error for typed-nil client")
	}
}

func TestProviderTypedNilContextSafe(t *testing.T) {
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	client := &fakeCredentialClient{creds: okCreds(start, 2*time.Hour)}
	clock := newFixedClock(start)
	p, err := New(Config{RoleName: "r", Client: client, Clock: clock.Now})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// typed-nil context: interface non-nil but concrete pointer nil.
	var nc *nilContext
	var ctx context.Context = nc
	_, err = p.Retrieve(ctx)
	if err == nil {
		t.Fatal("expected error for typed-nil context")
	}
}

// nilContext implements context.Context; methods panic on nil receiver.
type nilContext struct{}

func (*nilContext) Deadline() (time.Time, bool) { panic("nilContext.Deadline") }
func (*nilContext) Done() <-chan struct{}       { panic("nilContext.Done") }
func (*nilContext) Err() error                  { panic("nilContext.Err") }
func (*nilContext) Value(_ any) any             { panic("nilContext.Value") }

// TestProviderNonECSHostFailsWithinBudget uses a blocking CredentialClient with
// a short budget to prove the refresh stops within the budget.
func TestProviderNonECSHostFailsWithinBudget(t *testing.T) {
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	client := &fakeCredentialClient{block: make(chan struct{})}
	clock := newFixedClock(start)
	p, err := New(Config{RoleName: "r", Client: client, Clock: clock.Now})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	p.budget = 100 * time.Millisecond

	done := make(chan error, 1)
	go func() {
		_, err := p.Retrieve(context.Background())
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error when budget expires")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Retrieve did not stop at budget deadline")
	}
}
