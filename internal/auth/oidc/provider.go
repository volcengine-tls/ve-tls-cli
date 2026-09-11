package oidc

import (
	"context"
	"errors"
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

// ProviderName identifies this provider in auth.Value.ProviderName.
const ProviderName = "oidc"

// STSClient is the subset of the STS client the provider depends on.
type STSClient interface {
	AssumeRoleWithOIDC(context.Context, sts.OIDCInput) (sts.Credentials, error)
}

// Config carries the explicit inputs for an OIDC provider. No field is read
// from the environment.
type Config struct {
	TokenFile  string
	RoleTRN    string
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

	tokenFile  string
	roleTRN    string
	disableSSL bool
	client     STSClient
	clock      func() time.Time
	readToken  func(string) ([]byte, error)
}

// New validates the configuration and returns a ready-to-use Provider. It does
// not read the token file or perform any network call; the first Retrieve does.
func New(cfg Config) (*Provider, error) {
	if isNilClient(cfg.Client) {
		return nil, &auth.Error{Kind: auth.ConfigInvalid, Description: "STS client must not be nil"}
	}
	if cfg.Clock == nil {
		return nil, &auth.Error{Kind: auth.ConfigInvalid, Description: "clock must not be nil"}
	}
	if strings.TrimSpace(cfg.TokenFile) == "" {
		return nil, &auth.Error{Kind: auth.ConfigInvalid, Description: "token file must be non-empty"}
	}
	if strings.TrimSpace(cfg.RoleTRN) == "" {
		return nil, &auth.Error{Kind: auth.ConfigInvalid, Description: "role TRN must be non-empty"}
	}
	return &Provider{
		tokenFile:  cfg.TokenFile,
		roleTRN:    cfg.RoleTRN,
		disableSSL: cfg.DisableSSL,
		client:     cfg.Client,
		clock:      cfg.Clock,
		readToken:  readTokenFile,
		gate:       make(chan struct{}, 1),
	}, nil
}

// Retrieve returns the current cached credentials, refreshing them if they are
// at or past the refresh boundary. The token file is re-read on every refresh.
// A refresh failure is fail-closed: the old value is never returned once
// refreshAt is reached. A context-aware refresh gate ensures exactly one leader
// refreshes at a time; a waiter whose context is canceled while a leader
// refreshes returns context.Canceled/DeadlineExceeded immediately.
func (p *Provider) Retrieve(ctx context.Context) (auth.Value, error) {
	if p == nil {
		return auth.Value{}, &auth.Error{Kind: auth.ConfigInvalid, Description: "nil OIDC provider"}
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

	token, err := p.readToken(p.tokenFile)
	if err != nil {
		// Fail closed: do not return the stale cached value.
		return auth.Value{}, err
	}

	creds, err := p.client.AssumeRoleWithOIDC(ctx, sts.OIDCInput{
		Token:      token,
		RoleTRN:    p.roleTRN,
		DisableSSL: p.disableSSL,
	})
	if err != nil {
		// Wrap the STS error so the token (which may appear in the upstream
		// error message) is never rendered. The cause is preserved for
		// errors.Is/As but never appears in Error().
		return auth.Value{}, wrapSTSError(err)
	}

	if err := validateCredentials(creds); err != nil {
		return auth.Value{}, err
	}

	// Re-read the clock after the round-trip and verify the credential still has
	// a safe refresh window. Fail closed without modifying the existing cache.
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
	p.refreshAt = candidateRefreshAt
	v := p.cached
	p.mu.Unlock()

	return v, nil
}

// validateCredentials rejects temporary credentials missing any required field.
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

// wrapSTSError wraps an STS error in a safe auth.Error that never renders the
// token. If the original error is already an *auth.Error, its structural fields
// (Kind, Status, RequestID, ServiceCode) are preserved; Description is always
// replaced with fixed safe text and the original error is stored as Cause so
// errors.Is/errors.As still work. The original Description/body is never copied.
func wrapSTSError(err error) error {
	wrapped := &auth.Error{
		Kind:        auth.ProtocolError,
		Description: "STS AssumeRoleWithOIDC failed",
		Cause:       err,
	}
	var src *auth.Error
	// Guard against typed-nil *auth.Error: errors.As can succeed with a nil
	// pointer, which would panic on field access. Only copy when src is non-nil.
	if errors.As(err, &src) && src != nil {
		// Only override the default Kind when the source explicitly set one.
		if src.Kind != "" {
			wrapped.Kind = src.Kind
		}
		wrapped.Status = src.Status
		wrapped.RequestID = src.RequestID
		wrapped.ServiceCode = src.ServiceCode
	}
	return wrapped
}

// isNilClient reports whether c is nil or a typed-nil STSClient.
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
