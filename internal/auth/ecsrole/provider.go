package ecsrole

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth"
)

// refreshWindow is how long before the hard expiration the provider attempts
// to refresh, matching the upstream five-minute safety boundary.
const refreshWindow = 5 * time.Minute

// refreshBudget is the fixed total time allowed for a complete token-plus-
// credential refresh. It is instance-private and not configurable.
const refreshBudget = 5 * time.Second

// ProviderName identifies this provider in auth.Value.ProviderName.
const ProviderName = "ecsrole"

// Config carries the explicit inputs for an ECS role provider.
type Config struct {
	RoleName string
	Client   CredentialClient
	Clock    func() time.Time
}

// Provider caches temporary ECS credentials in memory and refreshes them no
// later than 5 minutes before their hard expiration. It implements
// auth.Provider.
type Provider struct {
	mu        sync.Mutex // protects cached and refreshAt only
	cached    auth.Value
	refreshAt time.Time
	gate      chan struct{} // context-aware refresh gate: created empty,
	// sending acquires the capacity-1 slot, receiving releases it

	roleName string
	client   CredentialClient
	clock    func() time.Time
	budget   time.Duration
}

// New validates the configuration and returns a ready-to-use Provider. It does
// not perform any network call; the first Retrieve fetches credentials.
func New(cfg Config) (*Provider, error) {
	if isNilClient(cfg.Client) {
		return nil, &auth.Error{Kind: auth.ConfigInvalid, Description: "ECS client must not be nil"}
	}
	if cfg.Clock == nil {
		return nil, &auth.Error{Kind: auth.ConfigInvalid, Description: "clock must not be nil"}
	}
	if strings.TrimSpace(cfg.RoleName) == "" {
		return nil, &auth.Error{Kind: auth.ConfigInvalid, Description: "ECS role name must be non-empty"}
	}
	return &Provider{
		roleName: cfg.RoleName,
		client:   cfg.Client,
		clock:    cfg.Clock,
		budget:   refreshBudget,
		gate:     make(chan struct{}, 1),
	}, nil
}

// Retrieve returns the current cached credentials, refreshing them if they are
// at or past the refresh boundary. A refresh failure is fail-closed: the old
// value is never returned once refreshAt is reached. A context-aware refresh
// gate ensures exactly one leader refreshes at a time; a waiter whose context
// is canceled while a leader refreshes returns context.Canceled/DeadlineExceeded
// immediately.
func (p *Provider) Retrieve(ctx context.Context) (auth.Value, error) {
	if p == nil {
		return auth.Value{}, &auth.Error{Kind: auth.ConfigInvalid, Description: "nil ECS role provider"}
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

	// The complete refresh runs within the fixed budget. A caller deadline that
	// is shorter still takes precedence.
	budget := p.budget
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining < budget {
			budget = remaining
		}
	}
	refreshCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	creds, err := p.client.FetchCredentials(refreshCtx, p.roleName)
	if err != nil {
		// Fail closed: do not return the stale cached value. Wrap the error so
		// any sensitive content from a non-auth.Error is hidden in Cause.
		return auth.Value{}, wrapClientError(err)
	}

	// Validate credential completeness before considering the refresh window.
	// Any missing/blank field fails closed without touching the cache.
	if err := validateCredentials(creds); err != nil {
		return auth.Value{}, err
	}

	// Re-read the clock after the round-trip and verify the credential still has
	// a safe refresh window. Fail closed without modifying the existing cache.
	candidateRefreshAt := creds.ExpiresAt.Add(-refreshWindow)
	if !candidateRefreshAt.After(p.clock()) {
		return auth.Value{}, &auth.Error{Kind: auth.ProtocolError, Description: "ECS credential expires too soon to safely refresh"}
	}

	p.mu.Lock()
	p.cached = auth.Value{
		AccessKeyID:     creds.AccessKeyID,
		SecretAccessKey: creds.SecretAccessKey,
		SessionToken:    creds.SessionToken,
		ProviderName:    ProviderName,
		ExpiresAt:       creds.ExpiresAt,
	}
	p.refreshAt = candidateRefreshAt
	v := p.cached
	p.mu.Unlock()

	return v, nil
}

// validateCredentials rejects credentials missing any required field. It runs
// before the refresh-window check so incomplete credentials never reach the
// cache.
func validateCredentials(c Credentials) error {
	if strings.TrimSpace(c.AccessKeyID) == "" ||
		strings.TrimSpace(c.SecretAccessKey) == "" ||
		strings.TrimSpace(c.SessionToken) == "" ||
		c.ExpiresAt.IsZero() {
		return &auth.Error{Kind: auth.ProtocolError, Description: "ECS credential response missing required fields"}
	}
	return nil
}

// isNilClient reports whether c is nil or a typed-nil CredentialClient.
func isNilClient(c CredentialClient) bool {
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

// wrapClientError returns a safe *auth.Error. If the source is a non-nil
// *auth.Error, its structural fields (Kind when non-empty, Status, RequestID,
// ServiceCode) are copied; Description is always replaced with fixed safe text
// and the original error is stored as Cause so it never appears in Error().
// Safe for typed-nil *auth.Error values.
func wrapClientError(err error) error {
	if err == nil {
		return nil
	}
	wrapped := &auth.Error{
		Kind:        auth.ProtocolError,
		Description: "ECS credential refresh failed",
		Cause:       err,
	}
	var ae *auth.Error
	if errors.As(err, &ae) && ae != nil {
		if ae.Kind != "" {
			wrapped.Kind = ae.Kind
		}
		wrapped.Status = ae.Status
		wrapped.RequestID = ae.RequestID
		wrapped.ServiceCode = ae.ServiceCode
	}
	return wrapped
}
