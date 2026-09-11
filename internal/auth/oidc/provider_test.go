package oidc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth"
	"github.com/volcengine-tls/ve-tls-cli/internal/auth/sts"
)

// fakeSTSClient is a test double for the STSClient interface.
type fakeSTSClient struct {
	mu        sync.Mutex
	calls     int32
	creds     sts.Credentials
	err       error
	lastInput sts.OIDCInput
	block     chan struct{} // if non-nil, first call blocks until closed
	started   chan struct{} // if non-nil, signaled when a call begins
}

func (f *fakeSTSClient) AssumeRoleWithOIDC(ctx context.Context, in sts.OIDCInput) (sts.Credentials, error) {
	atomic.AddInt32(&f.calls, 1)
	if f.started != nil {
		select {
		case f.started <- struct{}{}:
		default:
		}
	}
	f.mu.Lock()
	f.lastInput = in
	f.mu.Unlock()
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

func (f *fakeSTSClient) last() sts.OIDCInput {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastInput
}

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

func okCreds(start time.Time, ttl time.Duration) sts.Credentials {
	return sts.Credentials{
		AccessKeyID:     "TEMP-AK",
		SecretAccessKey: "TEMP-SK",
		SessionToken:    "TEMP-TOKEN",
		ExpiresAt:       start.Add(ttl),
	}
}

func baseConfig(tokenFile string, client STSClient, clock func() time.Time) Config {
	return Config{
		TokenFile:  tokenFile,
		RoleTRN:    "trn:iam::2100000000:role/oidc-role",
		DisableSSL: false,
		Client:     client,
		Clock:      clock,
	}
}

func writeToken(t *testing.T, dir, content string) string {
	t.Helper()
	p := filepath.Join(dir, "token")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	return p
}

func TestProviderReadsTokenLazily(t *testing.T) {
	dir := t.TempDir()
	// New must not read the file: create the provider before writing the token.
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	clock := newFixedClock(start)
	stsClient := &fakeSTSClient{creds: okCreds(start, 2*time.Hour)}
	tokenPath := filepath.Join(dir, "token")

	p, err := New(baseConfig(tokenPath, stsClient, clock.Now))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Token file does not exist yet, but New must succeed (lazy).
	if _, err := os.Stat(tokenPath); !os.IsNotExist(err) {
		t.Fatalf("token file should not exist yet, stat err=%v", err)
	}

	// Now write the token and retrieve; the file is read only on Retrieve.
	if err := os.WriteFile(tokenPath, []byte("header.payload.signature\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := p.Retrieve(context.Background()); err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if got := stsClient.callCount(); got != 1 {
		t.Fatalf("STS call count=%d, want 1", got)
	}
}

func TestProviderCachesCredentialsUntilExpirationMinusSixtySeconds(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	clock := newFixedClock(start)
	stsClient := &fakeSTSClient{creds: okCreds(start, 2*time.Hour)}
	tokenPath := writeToken(t, dir, "tok\n")
	p, err := New(baseConfig(tokenPath, stsClient, clock.Now))
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
	if got := stsClient.callCount(); got != 1 {
		t.Fatalf("STS call count=%d, want 1", got)
	}

	// Second retrieve uses cache.
	if _, err := p.Retrieve(context.Background()); err != nil {
		t.Fatalf("second Retrieve: %v", err)
	}
	if got := stsClient.callCount(); got != 1 {
		t.Fatalf("STS call count=%d, want 1 (cache hit)", got)
	}

	// Advance past refreshAt (ExpiresAt - 60s) and retrieve again: must refresh.
	clock.Advance(1*time.Hour + 59*time.Minute + 1*time.Second)
	stsClient.creds = okCreds(clock.Now(), 2*time.Hour)
	if _, err := p.Retrieve(context.Background()); err != nil {
		t.Fatalf("refresh Retrieve: %v", err)
	}
	if got := stsClient.callCount(); got != 2 {
		t.Fatalf("STS call count=%d, want 2 (refresh)", got)
	}
}

func TestProviderReloadsRotatedTokenOnRefresh(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	clock := newFixedClock(start)
	stsClient := &fakeSTSClient{creds: okCreds(start, 2*time.Hour)}
	tokenPath := writeToken(t, dir, "first-token\n")
	p, err := New(baseConfig(tokenPath, stsClient, clock.Now))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := p.Retrieve(context.Background()); err != nil {
		t.Fatalf("first Retrieve: %v", err)
	}
	if got := string(stsClient.last().Token); got != "first-token\n" {
		t.Fatalf("first token=%q, want first-token\\n", got)
	}

	// Rotate the token file and advance past refreshAt.
	if err := os.WriteFile(tokenPath, []byte("second-token\n"), 0o600); err != nil {
		t.Fatalf("rewrite token: %v", err)
	}
	clock.Advance(2 * time.Hour)
	stsClient.creds = okCreds(clock.Now(), 2*time.Hour)

	if _, err := p.Retrieve(context.Background()); err != nil {
		t.Fatalf("refresh Retrieve: %v", err)
	}
	if got := string(stsClient.last().Token); got != "second-token\n" {
		t.Fatalf("refreshed token=%q, want second-token\\n", got)
	}
}

func TestProviderConcurrentRetrieveRefreshesOnce(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	clock := newFixedClock(start)
	stsClient := &fakeSTSClient{
		creds:   okCreds(start, 2*time.Hour),
		block:   make(chan struct{}),
		started: make(chan struct{}, 1),
	}
	tokenPath := writeToken(t, dir, "tok\n")
	p, err := New(baseConfig(tokenPath, stsClient, clock.Now))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Wrap the instance-level reader to count token file reads.
	var reads int32
	origRead := p.readToken
	p.readToken = func(path string) ([]byte, error) {
		atomic.AddInt32(&reads, 1)
		return origRead(path)
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
	if got := stsClient.callCount(); got != 1 {
		t.Fatalf("STS call count=%d, want 1 (waiter used cache, not a second refresh)", got)
	}
	if got := atomic.LoadInt32(&reads); got != 1 {
		t.Fatalf("token file read count=%d, want 1 (single refresh)", got)
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
	dir := t.TempDir()
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	clock := newFixedClock(start)
	stsClient := &fakeSTSClient{
		creds:   okCreds(start, 2*time.Hour),
		block:   make(chan struct{}),
		started: make(chan struct{}, 1),
	}
	tokenPath := writeToken(t, dir, "tok\n")
	p, err := New(baseConfig(tokenPath, stsClient, clock.Now))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// release closes the leader's block channel exactly once.
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(stsClient.block) }) }

	leaderDone := make(chan struct{})
	go func() {
		_, _ = p.Retrieve(context.Background())
		close(leaderDone)
	}()
	<-stsClient.started

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

	if got := stsClient.callCount(); got != 1 {
		t.Fatalf("STS call count=%d, want 1 (waiter must not refresh)", got)
	}

	release()
	select {
	case <-leaderDone:
	case <-time.After(2 * time.Second):
		t.Fatal("leader did not finish after release")
	}
}

func TestProviderRefreshFailureFailsClosed(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	clock := newFixedClock(start)
	stsClient := &fakeSTSClient{creds: okCreds(start, 2*time.Hour)}
	tokenPath := writeToken(t, dir, "tok\n")
	p, err := New(baseConfig(tokenPath, stsClient, clock.Now))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := p.Retrieve(context.Background()); err != nil {
		t.Fatalf("first Retrieve: %v", err)
	}

	clock.Advance(2 * time.Hour)
	stsClient.err = errors.New("STS boom")

	_, err = p.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected error when refresh fails past refreshAt")
	}
	if got := stsClient.callCount(); got != 2 {
		t.Fatalf("STS call count=%d, want 2", got)
	}

	// Recover: clear the error and retrieve again. The gate must have been
	// released after the failure so a new refresh can proceed; the stale cache
	// must not be silently reused. The token file is re-read on refresh.
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

func TestProviderNeverFallsBackToOIDCEnvironment(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	clock := newFixedClock(start)
	stsClient := &fakeSTSClient{creds: okCreds(start, 2*time.Hour)}

	// Set upstream env vars pointing at a valid canary token and role. If the
	// provider fell back to env, it would read this file and call STS.
	canaryPath := writeToken(t, dir, "env-canary-token\n")
	t.Setenv("VOLCENGINE_OIDC_TOKEN_FILE", canaryPath)
	t.Setenv("VOLCENGINE_OIDC_ROLE_TRN", "trn:iam::9999999999:role/env-role")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")

	// Explicit Config points at a missing file and a different role.
	tokenPath := filepath.Join(dir, "does-not-exist")
	p, err := New(baseConfig(tokenPath, stsClient, clock.Now))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = p.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected error for missing token file, got nil (env fallback occurred?)")
	}
	if got := stsClient.callCount(); got != 0 {
		t.Fatalf("STS call count=%d, want 0 (no env fallback)", got)
	}
}

func TestProviderUsesExplicitConfigNotEnvironmentOnSuccess(t *testing.T) {
	canaryDir := t.TempDir()
	explicitDir := t.TempDir()
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	clock := newFixedClock(start)
	stsClient := &fakeSTSClient{creds: okCreds(start, 2*time.Hour)}

	// Set upstream env vars to canary values that must NOT be used.
	canaryToken := writeToken(t, canaryDir, "ENV-CANARY-TOKEN\n")
	t.Setenv("VOLCENGINE_OIDC_TOKEN_FILE", canaryToken)
	t.Setenv("VOLCENGINE_OIDC_ROLE_TRN", "trn:iam::9999999999:role/env-canary-role")

	// Explicit config uses a different token file and role TRN.
	explicitToken := writeToken(t, explicitDir, "EXPLICIT-TOKEN\n")
	if canaryToken == explicitToken {
		t.Fatalf("canary and explicit token paths must differ: both are %q", canaryToken)
	}
	cfg := baseConfig(explicitToken, stsClient, clock.Now)
	cfg.RoleTRN = "trn:iam::1111111111:role/explicit-role"
	p, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := p.Retrieve(context.Background()); err != nil {
		t.Fatalf("Retrieve: %v", err)
	}

	// STS must have received the explicit token and role, not the env canary.
	in := stsClient.last()
	if string(in.Token) != "EXPLICIT-TOKEN\n" {
		t.Fatalf("token=%q, want explicit token (env fallback occurred)", in.Token)
	}
	if in.RoleTRN != "trn:iam::1111111111:role/explicit-role" {
		t.Fatalf("RoleTRN=%q, want explicit role (env fallback occurred)", in.RoleTRN)
	}
}

func TestProviderErrorsDoNotExposeToken(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	clock := newFixedClock(start)
	const secret = "SECRET-OIDC-TOKEN-DO-NOT-LEAK"
	stsClient := &fakeSTSClient{err: errors.New("upstream: " + secret)}
	tokenPath := writeToken(t, dir, secret+"\n")
	p, err := New(baseConfig(tokenPath, stsClient, clock.Now))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = p.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	text := err.Error()
	if contains(text, secret) {
		t.Fatalf("error leaked token: %q", text)
	}
}

func TestSTSWrapperPreservesStructFieldsAndHidesToken(t *testing.T) {
	const secret = "SECRET-OIDC-TOKEN-DO-NOT-LEAK"
	// Simulate an STS *auth.Error that carries the token in its Description and
	// has structural fields set. The wrapper must preserve Kind/Status/
	// RequestID/ServiceCode, replace Description with safe text, and keep the
	// original error as Cause.
	srcErr := &auth.Error{
		Kind:        auth.ProtocolError,
		Status:      400,
		RequestID:   "req-123",
		ServiceCode: "InvalidIdentityToken",
		Description: "STS rejected token: " + secret,
	}

	wrapped := wrapSTSError(srcErr)
	var got *auth.Error
	if !errors.As(wrapped, &got) {
		t.Fatalf("wrapped error is not *auth.Error: %T", wrapped)
	}
	if got.Kind != srcErr.Kind {
		t.Fatalf("Kind=%q, want %q", got.Kind, srcErr.Kind)
	}
	if got.Status != srcErr.Status {
		t.Fatalf("Status=%d, want %d", got.Status, srcErr.Status)
	}
	if got.RequestID != srcErr.RequestID {
		t.Fatalf("RequestID=%q, want %q", got.RequestID, srcErr.RequestID)
	}
	if got.ServiceCode != srcErr.ServiceCode {
		t.Fatalf("ServiceCode=%q, want %q", got.ServiceCode, srcErr.ServiceCode)
	}
	// Description must be fixed safe text, not the original.
	if got.Description == srcErr.Description {
		t.Fatal("Description was copied from source error (must be safe text)")
	}
	if contains(got.Description, secret) {
		t.Fatalf("Description leaked token: %q", got.Description)
	}
	// Error() must not contain the token.
	if contains(wrapped.Error(), secret) {
		t.Fatalf("Error() leaked token: %q", wrapped.Error())
	}
	// Cause must be the original error so errors.Is/As still work.
	if got.Cause != srcErr {
		t.Fatalf("Cause=%v, want original error", got.Cause)
	}
	if !errors.Is(wrapped, srcErr) {
		t.Fatal("errors.Is(wrapped, srcErr) = false, want true")
	}
}

func TestSTSWrapperHandlesTypedNilAuthError(t *testing.T) {
	// A typed-nil *auth.Error must not panic and must produce a safe error with
	// the default Kind (ProtocolError), not a copied empty Kind.
	var nilErr *auth.Error
	wrapped := wrapSTSError(nilErr)
	if wrapped == nil {
		t.Fatal("expected non-nil wrapped error")
	}
	var got *auth.Error
	if !errors.As(wrapped, &got) {
		t.Fatalf("wrapped is not *auth.Error: %T", wrapped)
	}
	if got.Kind != auth.ProtocolError {
		t.Fatalf("Kind=%q, want default %q for typed-nil source", got.Kind, auth.ProtocolError)
	}
	if got.Description == "" {
		t.Fatal("Description must be set to safe text")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || (len(substr) > 0 && stringContains(s, substr)))
}

func stringContains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Additional boundary tests (short TTL, response-after clock, cache pollution,
// typed-nil) to match the RAM role coverage bar.
// ---------------------------------------------------------------------------

func TestProviderRejectsShortTTLCredentials(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	clock := newFixedClock(start)
	stsClient := &fakeSTSClient{creds: okCreds(start, 30*time.Second)}
	tokenPath := writeToken(t, dir, "tok\n")
	p, err := New(baseConfig(tokenPath, stsClient, clock.Now))
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

func TestProviderShortTTLRefreshDoesNotClobberCache(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	clock := newFixedClock(start)
	stsClient := &fakeSTSClient{creds: okCreds(start, 2*time.Hour)}
	tokenPath := writeToken(t, dir, "tok\n")
	p, err := New(baseConfig(tokenPath, stsClient, clock.Now))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := p.Retrieve(context.Background()); err != nil {
		t.Fatalf("first Retrieve: %v", err)
	}
	oldCached := p.cached
	oldRefreshAt := p.refreshAt

	clock.Advance(2 * time.Hour)
	stsClient.creds = okCreds(clock.Now(), 30*time.Second)

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

// clockAdvancingSTSClient advances the clock during the STS call.
type clockAdvancingSTSClient struct {
	clock   *fixedClock
	creds   sts.Credentials
	advance time.Duration
	calls   int32
}

func (c *clockAdvancingSTSClient) AssumeRoleWithOIDC(ctx context.Context, _ sts.OIDCInput) (sts.Credentials, error) {
	atomic.AddInt32(&c.calls, 1)
	c.clock.Advance(c.advance)
	return c.creds, nil
}

func TestProviderRejectsCredentialExpiredDuringRoundTrip(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	clock := newFixedClock(start)
	stsClient := &clockAdvancingSTSClient{
		clock:   clock,
		creds:   okCreds(start, 61*time.Second),
		advance: 2 * time.Second,
	}
	tokenPath := writeToken(t, dir, "tok\n")
	p, err := New(baseConfig(tokenPath, stsClient, clock.Now))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = p.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected error for credential expired during round-trip")
	}
	if !p.cached.ExpiresAt.IsZero() {
		t.Fatalf("cache polluted: ExpiresAt=%v", p.cached.ExpiresAt)
	}
}

// typedNilSTSClient is a concrete STSClient whose nil pointer is assignable to
// the STSClient interface.
type typedNilSTSClient struct{}

func (*typedNilSTSClient) AssumeRoleWithOIDC(context.Context, sts.OIDCInput) (sts.Credentials, error) {
	return sts.Credentials{}, nil
}

func TestNewRejectsTypedNilClient(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	clock := newFixedClock(start)
	tokenPath := writeToken(t, dir, "tok\n")
	cfg := baseConfig(tokenPath, &fakeSTSClient{creds: okCreds(start, 2*time.Hour)}, clock.Now)
	var nilClient *typedNilSTSClient
	cfg.Client = nilClient

	_, err := New(cfg)
	if err == nil {
		t.Fatal("expected error for typed-nil client")
	}
}

func TestNilProviderRetrieveReturnsError(t *testing.T) {
	var p *Provider
	_, err := p.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected error from nil *Provider")
	}
}

func TestRetrieveWithNilContextReturnsError(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	clock := newFixedClock(start)
	stsClient := &fakeSTSClient{creds: okCreds(start, 2*time.Hour)}
	tokenPath := writeToken(t, dir, "tok\n")
	p, err := New(baseConfig(tokenPath, stsClient, clock.Now))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	//lint:ignore SA1012 verifies Retrieve fails closed for a nil context
	_, err = p.Retrieve(nil)
	if err == nil {
		t.Fatal("expected error for nil context")
	}
	if got := stsClient.callCount(); got != 0 {
		t.Fatalf("STS call count=%d, want 0", got)
	}
}

// nilContext is a context.Context whose methods panic on a nil receiver.
type nilContext struct{}

func (*nilContext) Deadline() (time.Time, bool) { panic("nilContext.Deadline") }
func (*nilContext) Done() <-chan struct{}       { panic("nilContext.Done") }
func (*nilContext) Err() error                  { panic("nilContext.Err") }
func (*nilContext) Value(_ any) any             { panic("nilContext.Value") }

func TestRetrieveWithTypedNilContextReturnsError(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	clock := newFixedClock(start)
	stsClient := &fakeSTSClient{creds: okCreds(start, 2*time.Hour)}
	tokenPath := writeToken(t, dir, "tok\n")
	p, err := New(baseConfig(tokenPath, stsClient, clock.Now))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var nc *nilContext
	var ctx context.Context = nc

	_, err = p.Retrieve(ctx)
	if err == nil {
		t.Fatal("expected error for typed-nil context")
	}
	if got := stsClient.callCount(); got != 0 {
		t.Fatalf("STS call count=%d, want 0", got)
	}
}

func TestNewValidatesConfig(t *testing.T) {
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	clock := newFixedClock(start)
	stsClient := &fakeSTSClient{creds: okCreds(start, 2*time.Hour)}

	t.Run("nil client", func(t *testing.T) {
		_, err := New(Config{TokenFile: "t", RoleTRN: "r", Client: nil, Clock: clock.Now})
		if err == nil {
			t.Fatal("expected error for nil client")
		}
	})
	t.Run("nil clock", func(t *testing.T) {
		_, err := New(Config{TokenFile: "t", RoleTRN: "r", Client: stsClient, Clock: nil})
		if err == nil {
			t.Fatal("expected error for nil clock")
		}
	})
	t.Run("missing token file", func(t *testing.T) {
		_, err := New(Config{TokenFile: "", RoleTRN: "r", Client: stsClient, Clock: clock.Now})
		if err == nil {
			t.Fatal("expected error for missing token file")
		}
	})
	t.Run("missing role trn", func(t *testing.T) {
		_, err := New(Config{TokenFile: "t", RoleTRN: "", Client: stsClient, Clock: clock.Now})
		if err == nil {
			t.Fatal("expected error for missing role trn")
		}
	})
	t.Run("valid", func(t *testing.T) {
		dir := t.TempDir()
		tokenPath := writeToken(t, dir, "tok\n")
		p, err := New(baseConfig(tokenPath, stsClient, clock.Now))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		var _ auth.Provider = p
	})
}
