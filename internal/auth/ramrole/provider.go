// Package ramrole implements a cached RAM role credential provider that
// exchanges a long-lived source identity for temporary STS credentials via
// AssumeRole. Credentials are kept in process memory only; no disk state,
// environment variables, or background goroutines are used.
package ramrole

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth"
	"github.com/volcengine-tls/ve-tls-cli/internal/auth/sts"
)

// refreshWindow is how long before the hard expiration the provider attempts
// to refresh, matching the upstream 60-second safety boundary.
const refreshWindow = 60 * time.Second

// ProviderName identifies this provider in auth.Value.ProviderName. It matches
// the auth mode string used by SSO/Console providers.
const ProviderName = "ramrolearn"

// STSClient is the subset of the STS client the provider depends on. It is an
// interface so tests can inject a fake without importing the concrete client.
type STSClient interface {
	AssumeRole(context.Context, sts.AssumeRoleInput) (sts.Credentials, error)
}

// Config carries the explicit inputs for a RAM role provider. No field is read
// from the environment.
type Config struct {
	Source     sts.SourceCredential
	AccountID  string
	RoleName   string
	Region     string
	DisableSSL bool
	Client     STSClient
	Clock      func() time.Time
}

// Provider caches temporary STS credentials in memory and refreshes them no
// later than 60 seconds before their hard expiration. It implements
// auth.Provider.
type Provider struct {
	mu        sync.Mutex // protects cached and refreshAt only
	cached    auth.Value
	refreshAt time.Time
	gate      chan struct{} // context-aware refresh gate: created empty,
	// sending acquires the capacity-1 slot, receiving releases it

	source     sts.SourceCredential
	accountID  string
	roleName   string
	region     string
	disableSSL bool
	client     STSClient
	clock      func() time.Time
}

// New validates the configuration and returns a ready-to-use Provider. It does
// not perform any network call; the first Retrieve fetches credentials.
func New(cfg Config) (*Provider, error) {
	if isNilClient(cfg.Client) {
		return nil, &auth.Error{Kind: auth.ConfigInvalid, Description: "STS client must not be nil"}
	}
	if cfg.Clock == nil {
		return nil, &auth.Error{Kind: auth.ConfigInvalid, Description: "clock must not be nil"}
	}
	if strings.TrimSpace(cfg.Source.AccessKeyID) == "" || strings.TrimSpace(cfg.Source.SecretAccessKey) == "" {
		return nil, &auth.Error{Kind: auth.ConfigInvalid, Description: "source access key id and secret access key must both be non-empty"}
	}
	if strings.TrimSpace(cfg.RoleName) == "" {
		return nil, &auth.Error{Kind: auth.ConfigInvalid, Description: "role name must be non-empty"}
	}
	if strings.TrimSpace(cfg.AccountID) == "" {
		return nil, &auth.Error{Kind: auth.ConfigInvalid, Description: "account id must be non-empty"}
	}
	return &Provider{
		source:     cfg.Source,
		accountID:  cfg.AccountID,
		roleName:   cfg.RoleName,
		region:     cfg.Region,
		disableSSL: cfg.DisableSSL,
		client:     cfg.Client,
		clock:      cfg.Clock,
		gate:       make(chan struct{}, 1),
	}, nil
}

// Retrieve returns the current cached credentials, refreshing them if they are
// at or past the refresh boundary. A refresh failure is fail-closed: the old
// value is never returned once refreshAt is reached. A context-aware refresh
// gate ensures exactly one leader refreshes at a time; a waiter whose context
// is canceled while a leader refreshes returns context.Canceled/DeadlineExceeded
// immediately instead of blocking until the leader completes.
func (p *Provider) Retrieve(ctx context.Context) (auth.Value, error) {
	if p == nil {
		return auth.Value{}, &auth.Error{Kind: auth.ConfigInvalid, Description: "nil RAM role provider"}
	}
	if isNilContext(ctx) {
		return auth.Value{}, &auth.Error{Kind: auth.ConfigInvalid, Description: "nil context"}
	}

	// Fast path: return fresh cache without contending for the refresh gate.
	p.mu.Lock()
	if !p.cached.ExpiresAt.IsZero() && p.clock().Before(p.refreshAt) {
		v := p.cached
		p.mu.Unlock()
		return v, nil
	}
	p.mu.Unlock()

	// Honor a canceled context before contending for the gate.
	if err := ctx.Err(); err != nil {
		return auth.Value{}, err
	}

	// Acquire the refresh gate, honoring context cancellation so a waiter
	// blocked behind an in-flight refresh can exit immediately.
	select {
	case p.gate <- struct{}{}:
	case <-ctx.Done():
		return auth.Value{}, ctx.Err()
	}
	defer func() { <-p.gate }()

	// Re-check context after gate acquisition: a simultaneous release and
	// cancel must not let us return the leader's cache for a canceled wait.
	if err := ctx.Err(); err != nil {
		return auth.Value{}, err
	}

	// Second cache check: the leader may have refreshed while we waited.
	p.mu.Lock()
	if !p.cached.ExpiresAt.IsZero() && p.clock().Before(p.refreshAt) {
		v := p.cached
		p.mu.Unlock()
		return v, nil
	}
	p.mu.Unlock()

	creds, err := p.client.AssumeRole(ctx, sts.AssumeRoleInput{
		Source:     p.source,
		AccountID:  p.accountID,
		RoleName:   p.roleName,
		Region:     p.region,
		DisableSSL: p.disableSSL,
	})
	if err != nil {
		// Fail closed: do not return the stale cached value.
		return auth.Value{}, err
	}

	if err := validateCredentials(creds); err != nil {
		return auth.Value{}, err
	}

	// Re-read the clock after the round-trip and verify the credential still has
	// a safe refresh window. If ExpiresAt is within 60s (or already past), the
	// credential is unusable: fail closed without modifying the existing cache.
	candidateRefreshAt := creds.ExpiresAt.Add(-refreshWindow)
	if !candidateRefreshAt.After(p.clock()) {
		return auth.Value{}, &auth.Error{Kind: auth.ProtocolError, Description: "STS credential expires too soon to safely refresh"}
	}

	p.mu.Lock()
	p.cached = auth.Value{
		AccessKeyID:     creds.AccessKeyID,
		SecretAccessKey: creds.SecretAccessKey,
		SessionToken:    creds.SessionToken,
		ProviderName:    ProviderName,
		ExpiresAt:       creds.ExpiresAt,
	}
	// refreshAt is private; the hard ExpiresAt returned to callers is unchanged.
	p.refreshAt = candidateRefreshAt
	v := p.cached
	p.mu.Unlock()

	return v, nil
}

// validateCredentials rejects temporary credentials missing any required field.
// STS temporary credentials always carry a session token.
func validateCredentials(c sts.Credentials) error {
	if strings.TrimSpace(c.AccessKeyID) == "" ||
		strings.TrimSpace(c.SecretAccessKey) == "" ||
		strings.TrimSpace(c.SessionToken) == "" {
		return &auth.Error{Kind: auth.ProtocolError, Description: "STS response missing required credential fields"}
	}
	if c.ExpiresAt.IsZero() {
		return &auth.Error{Kind: auth.ProtocolError, Description: "STS response missing credential expiration"}
	}
	return nil
}

// isNilClient reports whether c is nil or a typed-nil STSClient (e.g. a nil
// *sts.Client stored in the STSClient interface). It uses reflect only on
// nil-capable kinds so it never panics.
func isNilClient(c STSClient) bool {
	if c == nil {
		return true
	}
	rv := reflect.ValueOf(c)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return rv.IsNil()
	}
	return false
}

// isNilContext reports whether ctx is nil or a typed-nil context.Context.
func isNilContext(ctx context.Context) bool {
	if ctx == nil {
		return true
	}
	rv := reflect.ValueOf(ctx)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return rv.IsNil()
	}
	return false
}
