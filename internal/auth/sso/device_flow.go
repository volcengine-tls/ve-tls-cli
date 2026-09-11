package sso

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"strings"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth"
	"github.com/volcengine-tls/ve-tls-cli/internal/auth/browser"
	"github.com/volcengine-tls/ve-tls-cli/internal/securestore"
)

// OAuthAPI is the narrow subset of the CloudIdentity OAuth client used by the
// device flow. It is an injectable seam so tests never touch the network.
type OAuthAPI interface {
	RegisterClient(ctx context.Context, req *RegisterClientRequest) (*RegisterClientResponse, error)
	StartDeviceAuthorization(ctx context.Context, req *StartDeviceAuthorizationRequest) (*StartDeviceAuthorizationResponse, error)
	CreateToken(ctx context.Context, req *CreateTokenRequest) (*CreateTokenResponse, error)
}

// Sleeper blocks for the given duration or until ctx is cancelled. It is an
// injectable seam so tests never sleep for real.
type Sleeper func(ctx context.Context, d time.Duration) error

// ProgressWriter receives user-facing progress messages (verification URL,
// polling status). It is an injectable seam; messages never go to os.Stdout or
// os.Stderr directly.
type ProgressWriter io.Writer

// defaultSleeper sleeps using time.Sleep, honoring context cancellation.
func defaultSleeper(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// DeviceFlowConfig holds the injectable dependencies for DeviceFlow.
type DeviceFlowConfig struct {
	OAuth       OAuthAPI
	Cache       Cache
	Clock       func() time.Time
	Sleeper     Sleeper
	Browser     browser.Opener
	Progress    ProgressWriter
	NoBrowser   bool
	ClientName  string
	StartURL    string
	SessionName string
	Region      string
	Scopes      []string
}

// DeviceFlow orchestrates the explicit SSO device authorization flow: client
// registration (reused or freshly registered), device authorization, user
// notification, polling, and token cache persistence. All dependencies are
// injectable so tests run without network, browser, or real delays.
type DeviceFlow struct {
	oauth       OAuthAPI
	cache       Cache
	clock       func() time.Time
	sleeper     Sleeper
	browser     browser.Opener
	progress    ProgressWriter
	noBrowser   bool
	clientName  string
	startURL    string
	sessionName string
	region      string
	scopes      []string
}

// NewDeviceFlow constructs a DeviceFlow from the given config. Nil dependencies
// are replaced with production defaults where possible. A nil cfg is treated as
// an empty config.
func NewDeviceFlow(cfg *DeviceFlowConfig) *DeviceFlow {
	if cfg == nil {
		cfg = &DeviceFlowConfig{}
	}
	df := &DeviceFlow{
		oauth:       cfg.OAuth,
		cache:       cfg.Cache,
		clock:       cfg.Clock,
		sleeper:     cfg.Sleeper,
		browser:     cfg.Browser,
		progress:    cfg.Progress,
		noBrowser:   cfg.NoBrowser,
		clientName:  cfg.ClientName,
		startURL:    cfg.StartURL,
		sessionName: cfg.SessionName,
		region:      cfg.Region,
		scopes:      cfg.Scopes,
	}
	if df.clock == nil {
		df.clock = time.Now
	}
	if df.sleeper == nil {
		df.sleeper = defaultSleeper
	}
	if df.clientName == "" {
		df.clientName = "volclog"
	}
	return df
}

// Login runs the full explicit device authorization flow and returns the
// resulting token cache. It always starts a new device authorization, even if a
// valid token cache exists. On success it atomically persists the token cache
// with the exact client registration used and the canonical StartURL.
//
// TOKEN-LOCK CONTRACT: Login intentionally does NOT acquire the token lock
// (Cache.WithTokenLock) internally. Production callers MUST hold the matching
// Cache.WithTokenLock(startURL, sessionName) for the entire transaction that
// snapshots the old cache, calls Login, commits the config, and rolls back on
// failure. An internal self-lock would recursively deadlock that transaction.
// Isolated unit tests may invoke Login directly without holding the lock.
func (f *DeviceFlow) Login(ctx context.Context) (*TokenCache, error) {
	if f == nil {
		return nil, errors.New("nil *DeviceFlow")
	}
	if isNilInterface(ctx) {
		return nil, errors.New("nil context")
	}
	if isNilInterface(f.oauth) {
		return nil, errors.New("nil oauth client")
	}
	if isNilInterface(f.cache) {
		return nil, errors.New("nil cache")
	}
	if f.clock == nil {
		return nil, errors.New("nil clock")
	}
	if f.sleeper == nil {
		return nil, errors.New("nil sleeper")
	}
	// Canonicalize the StartURL once so the persisted cache and all cache keys
	// use the same identity regardless of user-supplied spelling (trailing
	// slash, /userportal/, host case, etc.).
	canonicalStart, err := CanonicalStartURL(f.startURL)
	if err != nil {
		return nil, err
	}
	// Trim and canonicalize session name and region once so the same logical
	// values are used for the client key, token cache key/content, Portal
	// request context, and persisted TokenCache. A login with " corp " must
	// produce SessionName:"corp" and be immediately accepted by a Provider
	// configured with "corp".
	session := strings.TrimSpace(f.sessionName)
	if session == "" {
		return nil, errors.New("session name is empty")
	}
	region := strings.TrimSpace(f.region)
	if region == "" {
		return nil, errors.New("sso region is empty")
	}
	scopes, err := NormalizeScopes(f.scopes)
	if err != nil {
		return nil, err
	}

	// 1. Resolve or register the client under the client registration lock.
	client, err := f.ensureClient(ctx, canonicalStart, region, scopes, session)
	if err != nil {
		return nil, err
	}

	// 2. Start device authorization.
	authResp, err := f.oauth.StartDeviceAuthorization(ctx, &StartDeviceAuthorizationRequest{
		ClientID:     client.ClientID,
		ClientSecret: client.ClientSecret,
		Scopes:       scopes,
		PortalURL:    canonicalStart,
	})
	if err != nil {
		return nil, &auth.Error{Kind: auth.ProtocolError, Description: "start device authorization failed", Cause: err}
	}
	if authResp == nil {
		return nil, errors.New("start device authorization returned nil response")
	}

	// 3. Emit the verification instruction to the progress writer only.
	verifyURL := verificationURL(authResp)
	if verifyURL == "" {
		return nil, errors.New("device authorization response has no verification URL")
	}
	f.emitVerification(ctx, verifyURL, authResp.UserCode)

	// 4. Poll for the token.
	tokenResp, err := f.pollForToken(ctx, authResp, client)
	if err != nil {
		return nil, err
	}
	if tokenResp == nil {
		return nil, errors.New("token exchange returned nil response")
	}

	// 5. Build and persist the token cache atomically. Copy the exact client
	// registration used so the token cache is self-contained for refresh. The
	// canonical StartURL and trimmed region are persisted so the cache identity
	// is stable across user-supplied spellings.
	tokenTTL, derr := secondsToDuration(tokenResp.ExpiresIn)
	if derr != nil {
		return nil, &auth.Error{Kind: auth.ProtocolError, Description: "token lifetime is invalid", Cause: derr}
	}
	expiresAt := f.clock().Add(tokenTTL).UTC().Format(time.RFC3339)
	tokenCache := &TokenCache{
		StartURL:              canonicalStart,
		SessionName:           session,
		AccessToken:           tokenResp.AccessToken,
		ExpiresAt:             expiresAt,
		ClientID:              client.ClientID,
		ClientSecret:          client.ClientSecret,
		ClientIDIssuedAt:      client.ClientIDIssuedAt,
		ClientSecretExpiresAt: client.ClientSecretExpiresAt,
		RefreshToken:          tokenResp.RefreshToken,
		Region:                region,
	}
	if err := f.cache.WriteToken(tokenCache); err != nil {
		return nil, &auth.Error{Kind: auth.ProtocolError, Description: "persist token cache failed", Cause: err}
	}
	return tokenCache, nil
}

// ensureClient returns a valid client registration, reusing the cached one when
// it is complete and unexpired, or registering a new public client when the
// cache is missing or expired. A corrupt, permission-denied, or parsed-but-
// incomplete cache fails closed: no RegisterClient call is made and the file is
// not overwritten. A newly registered response is validated before use, and a
// WriteClient failure fails closed before StartDeviceAuthorization.
func (f *DeviceFlow) ensureClient(ctx context.Context, canonicalStart, region string, scopes []string, session string) (*RegisterClientResponse, error) {
	var client *RegisterClientResponse
	err := f.cache.WithClientLock(ctx, canonicalStart, region, scopes, session, func() error {
		cached, rerr := f.cache.ReadClient(canonicalStart, region, scopes, session)
		now := f.clock()

		switch {
		case rerr == nil && cached != nil && isCompleteClientRegistration(cached) && isValidClientRegistration(cached, now):
			// Complete and unexpired: reuse.
			client = &RegisterClientResponse{
				ClientID:              cached.ClientID,
				ClientSecret:          cached.ClientSecret,
				ClientIDIssuedAt:      cached.ClientIDIssuedAt,
				ClientSecretExpiresAt: cached.ClientSecretExpiresAt,
			}
			return nil
		case rerr == nil && cached != nil && isCompleteClientRegistration(cached) && !isValidClientRegistration(cached, now):
			// Complete but expired: re-register (fall through to registration).
		case errors.Is(rerr, securestore.ErrMissing):
			// Missing: register a new client.
		case rerr != nil:
			// Corrupt, permission, or other I/O error: fail closed without
			// registering or overwriting the file.
			return &auth.Error{Kind: auth.CacheCorrupt, Description: "client registration cache is unreadable; run: volclog sso login", Cause: rerr}
		default:
			// Parsed but incomplete (missing client id/secret): fail closed.
			return &auth.Error{Kind: auth.CacheCorrupt, Description: "client registration cache is invalid; run: volclog sso login"}
		}

		// Register a new client.
		resp, rerr := f.oauth.RegisterClient(ctx, &RegisterClientRequest{
			ClientName: f.clientName,
			ClientType: "public",
			GrantTypes: []string{GrantTypeDeviceCode, GrantTypeRefreshToken},
			Scopes:     scopes,
		})
		if rerr != nil {
			return &auth.Error{Kind: auth.ProtocolError, Description: "register client failed", Cause: rerr}
		}
		if resp == nil {
			return errors.New("register client returned nil response")
		}
		// Validate the newly registered response before use. A response with
		// an already-expired or nonsensical nonzero expiry is rejected so we
		// never persist or use a dead client. The clock is read fresh AFTER
		// RegisterClient returns so a call that advances time past expiry is
		// detected rather than validated against a stale pre-call timestamp.
		registerNow := f.clock()
		if err := validateRegisterClientResponse(resp, registerNow); err != nil {
			return err
		}
		client = resp

		// Persist the new registration. A write failure fails closed before
		// StartDeviceAuthorization so we never proceed with an unpersisted
		// client that another process cannot reuse.
		if werr := f.cache.WriteClient(&ClientRegistrationCache{
			ClientName:            f.clientName,
			ClientID:              resp.ClientID,
			ClientSecret:          resp.ClientSecret,
			ClientIDIssuedAt:      resp.ClientIDIssuedAt,
			ClientSecretExpiresAt: resp.ClientSecretExpiresAt,
		}, canonicalStart, region, scopes, session); werr != nil {
			return &auth.Error{Kind: auth.ProtocolError, Description: "persist client registration failed", Cause: werr}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return client, nil
}

// isCompleteClientRegistration reports whether the cached client registration
// has the minimum required fields (non-empty client id and secret). It does not
// check expiry.
func isCompleteClientRegistration(c *ClientRegistrationCache) bool {
	if c == nil {
		return false
	}
	return strings.TrimSpace(c.ClientID) != "" && strings.TrimSpace(c.ClientSecret) != ""
}

// isValidClientRegistration reports whether the cached client registration is
// complete and unexpired. A ClientSecretExpiresAt of 0 means never expires.
// Timestamps are accepted in either seconds or milliseconds.
func isValidClientRegistration(c *ClientRegistrationCache, now time.Time) bool {
	if !isCompleteClientRegistration(c) {
		return false
	}
	if c.ClientSecretExpiresAt == 0 {
		return true
	}
	exp := normalizeExpiry(c.ClientSecretExpiresAt)
	return now.Before(exp)
}

// validateRegisterClientResponse validates the minimum required fields of a
// RegisterClientResponse before it is used or persisted. It is clock-aware: a
// ClientSecretExpiresAt of 0 means never expires; any nonzero value must be in
// the future (normalized from seconds or milliseconds). An already-expired or
// nonsensical nonzero expiry is a safe protocol error so a dead client is never
// persisted or used.
func validateRegisterClientResponse(r *RegisterClientResponse, now time.Time) error {
	if r == nil {
		return errors.New("nil register client response")
	}
	if strings.TrimSpace(r.ClientID) == "" {
		return errors.New("register client response missing client id")
	}
	if strings.TrimSpace(r.ClientSecret) == "" {
		return errors.New("register client response missing client secret")
	}
	// 0 means never expires. Otherwise the expiry must be strictly in the
	// future. Negative or zero-epoch values normalize to the zero time and are
	// rejected.
	if r.ClientSecretExpiresAt != 0 {
		exp := normalizeExpiry(r.ClientSecretExpiresAt)
		if !now.Before(exp) {
			return &auth.Error{Kind: auth.ProtocolError, Description: "register client response has expired client secret"}
		}
	}
	return nil
}

// normalizeExpiry converts a Unix timestamp that may be in seconds or
// milliseconds into a time.Time. Values >= 1e12 are treated as milliseconds.
func normalizeExpiry(ts int64) time.Time {
	if ts <= 0 {
		return time.Time{}
	}
	if ts >= 1e12 {
		return time.UnixMilli(ts)
	}
	return time.Unix(ts, 0)
}

// secondsToDuration converts a positive protocol second value to a time.Duration.
// It rejects values that would overflow time.Duration (int64 nanoseconds) when
// multiplied by time.Second, so an excessively large server-supplied lifetime
// cannot wrap into a negative or tiny duration. This is overflow defense only;
// it does not impose an arbitrary product lifetime limit.
func secondsToDuration(seconds int) (time.Duration, error) {
	if seconds <= 0 {
		return 0, errors.New("seconds must be positive")
	}
	const maxSeconds = int64(math.MaxInt64) / int64(time.Second)
	if int64(seconds) > maxSeconds {
		return 0, errors.New("seconds value too large for time.Duration")
	}
	return time.Duration(seconds) * time.Second, nil
}

// verificationURL returns the best available verification URL: prefer
// verification_uri_complete, otherwise construct one from verification_uri and
// user_code using net/url, preserving existing query and escaping the code.
func verificationURL(resp *StartDeviceAuthorizationResponse) string {
	if resp == nil {
		return ""
	}
	if complete := strings.TrimSpace(resp.VerificationURIComplete); complete != "" {
		return complete
	}
	base := strings.TrimSpace(resp.VerificationURI)
	if base == "" {
		return ""
	}
	u, err := url.Parse(base)
	if err != nil {
		return ""
	}
	q := u.Query()
	q.Set("user_code", resp.UserCode)
	u.RawQuery = q.Encode()
	return u.String()
}

// emitVerification writes the verification instruction to the progress writer
// and, unless --no-browser is set, attempts to open the browser using the
// caller context. If the browser fails, the fallback URL is still emitted. The
// opener error is never echoed because it may contain the URL.
func (f *DeviceFlow) emitVerification(ctx context.Context, verifyURL, userCode string) {
	if f.progress != nil {
		if userCode != "" {
			fmt.Fprintf(f.progress, "To authorize, visit the following URL and enter code %s:\n\n%s\n\n", userCode, verifyURL)
		} else {
			fmt.Fprintf(f.progress, "To authorize, visit the following URL:\n\n%s\n\n", verifyURL)
		}
	}
	if f.noBrowser {
		return
	}
	if isNilInterface(f.browser) {
		return
	}
	if err := f.browser.Open(ctx, verifyURL); err != nil {
		if f.progress != nil {
			fmt.Fprintf(f.progress, "If the browser did not open, visit the URL above manually.\n")
		}
	}
}

// pollForToken polls the token endpoint until authorization completes, the
// device code expires, or ctx is cancelled. It honors the server-suggested
// interval (defaulting to 5s when zero) and increases the interval by 5s on
// slow_down. Each sleep is bounded to the remaining device-code lifetime, and
// the context and deadline are re-checked after the sleeper returns (even if an
// injected sleeper incorrectly returns nil after cancellation) so no
// CreateToken call is made after the deadline.
func (f *DeviceFlow) pollForToken(ctx context.Context, authResp *StartDeviceAuthorizationResponse, client *RegisterClientResponse) (*CreateTokenResponse, error) {
	// A zero or negative interval falls back to the default 5s; a positive
	// interval is converted with overflow defense.
	interval := 5 * time.Second
	if authResp.Interval > 0 {
		converted, derr := secondsToDuration(authResp.Interval)
		if derr != nil {
			return nil, &auth.Error{Kind: auth.ProtocolError, Description: "device authorization interval is invalid", Cause: derr}
		}
		interval = converted
	}
	expiresIn, derr := secondsToDuration(authResp.ExpiresIn)
	if derr != nil {
		return nil, &auth.Error{Kind: auth.ProtocolError, Description: "device authorization lifetime is invalid", Cause: derr}
	}
	deadline := f.clock().Add(expiresIn)

	for {
		now := f.clock()
		// Honor the earlier of context cancellation and the device-code deadline.
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !now.Before(deadline) {
			return nil, errors.New("device authorization timed out")
		}

		// Bound the sleep to the remaining device-code lifetime so we never
		// oversleep the deadline.
		remaining := deadline.Sub(now)
		sleep := interval
		if remaining < sleep {
			sleep = remaining
		}
		if sleep <= 0 {
			return nil, errors.New("device authorization timed out")
		}

		// Sleep before polling, honoring context cancellation.
		if err := f.sleeper(ctx, sleep); err != nil {
			return nil, err
		}

		// Re-check context and deadline after the sleeper returns, even if an
		// injected sleeper incorrectly returns nil after cancellation.
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !f.clock().Before(deadline) {
			return nil, errors.New("device authorization timed out")
		}

		tokenResp, err := f.oauth.CreateToken(ctx, &CreateTokenRequest{
			GrantType:    GrantTypeDeviceCode,
			ClientID:     client.ClientID,
			ClientSecret: client.ClientSecret,
			DeviceCode:   authResp.DeviceCode,
		})
		if err == nil {
			return tokenResp, nil
		}

		// Classify the OAuth error to decide whether to continue polling.
		var apiErr *OAuthAPIError
		if errors.As(err, &apiErr) {
			switch apiErr.Code {
			case "authorization_pending":
				continue
			case "slow_down":
				// Guard against interval growth wrapping time.Duration. If
				// adding 5s would overflow, fail closed with a safe protocol
				// error rather than polling with a wrapped (tiny/negative)
				// interval.
				if interval > time.Duration(math.MaxInt64)-5*time.Second {
					return nil, &auth.Error{Kind: auth.ProtocolError, Description: "polling interval overflow"}
				}
				interval += 5 * time.Second
				continue
			}
		}
		return nil, &auth.Error{Kind: auth.ProtocolError, Description: "poll access token failed", Cause: err}
	}
}
