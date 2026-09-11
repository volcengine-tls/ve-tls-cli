package sso

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth"
	"github.com/volcengine-tls/ve-tls-cli/internal/config"
	"github.com/volcengine-tls/ve-tls-cli/internal/securestore"
)

// ProviderName is the provider name returned in auth.Value for SSO.
const ProviderName = "sso"

// RefreshWindow is how long before expiry the access token is proactively
// refreshed. It is a named constant so tests and callers can reason about the
// refresh boundary deterministically.
const RefreshWindow = 60 * time.Second

// errSTSExpired is returned by stsCacheToValue when the STS cache is otherwise
// valid (complete, correctly bound, correct provider) but expired or within the
// refresh window. It is the only stsCacheToValue error that permits a Portal
// exchange; all other errors fail closed without calling Portal.
var errSTSExpired = errors.New("sts cache expired or near expiry")

// errBindingMismatch is returned by patchConfig when the target profile's
// binding (mode/session/account/role) no longer matches the Provider's binding,
// typically because a concurrent rebind changed the profile. Callers can detect
// it via errors.Is to distinguish a permanent binding mismatch (which requires
// cleaning up the uncommitted STS cache) from a transient config I/O failure
// (which is retryable).
var errBindingMismatch = errors.New("profile binding does not match provider")

// STSExchanger is the narrow subset of the Portal client used to exchange an
// access token for temporary STS credentials. It is an injectable seam.
type STSExchanger interface {
	GetRoleCredentials(ctx context.Context, accessToken, accountID, roleName string) (*RoleCredentials, error)
}

// ConfigUpdater abstracts config.Update for the provider. The production
// implementation wraps config.Update; tests inject fakes.
type ConfigUpdater interface {
	Update(path string, fn func(*config.Config) error) (config.Config, error)
}

// configUpdater is the production ConfigUpdater.
type configUpdater struct{}

func (configUpdater) Update(path string, fn func(*config.Config) error) (config.Config, error) {
	return config.Update(path, fn)
}

// SSOProviderConfig holds the injectable dependencies and explicit binding for
// SSOProvider.
type SSOProviderConfig struct {
	ConfigPath  string
	ProfileName string
	StartURL    string
	SessionName string
	SSORegion   string
	AccountID   string
	RoleName    string
	Cache       Cache
	OAuth       OAuthAPI
	Portal      STSExchanger
	ConfigStore ConfigUpdater
	Clock       func() time.Time
}

// SSOProvider is a refreshable auth.Provider backed by the SSO token and STS
// caches. On every Retrieve it reloads the caches from disk (never in-memory
// state), refreshes the OAuth token when near expiry, exchanges it for STS
// credentials when the STS cache is stale, and patches the profile's
// sts-expiration. It never starts device flow, never registers a client, never
// opens a browser, and never falls back to environment or static AK/SK.
//
// All refresh operations are serialized through the token lock so that
// concurrent Retrieve calls (even across processes and Provider instances for
// the same session) produce exactly one OAuth refresh.
type SSOProvider struct {
	configPath  string
	profileName string
	startURL    string
	sessionName string
	ssoRegion   string
	accountID   string
	roleName    string
	cache       Cache
	oauth       OAuthAPI
	portal      STSExchanger
	configStore ConfigUpdater
	clock       func() time.Time
}

// NewSSOProvider constructs an SSOProvider from the given config. It validates
// all binding fields up front, including a non-empty SSO region, and never
// consults the current profile implicitly. If clock is nil, time.Now is used.
func NewSSOProvider(cfg *SSOProviderConfig) (*SSOProvider, error) {
	if cfg == nil {
		return nil, errors.New("nil config")
	}
	// Canonicalize the config path to a cleaned absolute path once, so later
	// working-directory changes cannot change either the file patched by
	// config.Update or its commit identity.
	configPath, err := normalizeConfigPath(cfg.ConfigPath)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.ProfileName) == "" {
		return nil, errors.New("profile name is empty")
	}
	canonical, err := CanonicalStartURL(cfg.StartURL)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.SessionName) == "" {
		return nil, errors.New("session name is empty")
	}
	region := strings.TrimSpace(cfg.SSORegion)
	if region == "" {
		return nil, errors.New("sso region is empty")
	}
	if strings.TrimSpace(cfg.AccountID) == "" {
		return nil, errors.New("account id is empty")
	}
	if strings.TrimSpace(cfg.RoleName) == "" {
		return nil, errors.New("role name is empty")
	}
	if isNilInterface(cfg.Cache) {
		return nil, errors.New("nil cache")
	}
	if isNilInterface(cfg.OAuth) {
		return nil, errors.New("nil oauth client")
	}
	if isNilInterface(cfg.Portal) {
		return nil, errors.New("nil portal client")
	}
	clock := cfg.Clock
	if clock == nil {
		clock = time.Now
	}
	configStore := cfg.ConfigStore
	if isNilInterface(configStore) {
		configStore = configUpdater{}
	}
	return &SSOProvider{
		configPath:  configPath,
		profileName: strings.TrimSpace(cfg.ProfileName),
		startURL:    canonical,
		sessionName: strings.TrimSpace(cfg.SessionName),
		ssoRegion:   region,
		accountID:   strings.TrimSpace(cfg.AccountID),
		roleName:    strings.TrimSpace(cfg.RoleName),
		cache:       cfg.Cache,
		oauth:       cfg.OAuth,
		portal:      cfg.Portal,
		configStore: configStore,
		clock:       clock,
	}, nil
}

// Retrieve returns valid STS credentials, refreshing the OAuth token and
// exchanging for STS as needed. It implements auth.Provider.
//
// The retrieve algorithm is frozen:
//
//	WithTokenLock(startURL, sessionName)
//	-> re-read token cache from disk
//	-> validate cache schema, binding identity, and region
//	-> if access token is within RefreshWindow of expiry, refresh using the
//	   client ID/secret/refresh token stored in the token cache
//	-> persist rotated token cache atomically before proceeding
//	-> WithSTSLock(sessionName, accountID, roleName)
//	-> re-read STS cache from disk
//	-> if STS cache is valid (not near expiry, binding matches):
//	   -> if the current config target's commit identity is present, return it
//	   -> otherwise patch THIS target's profile, add its identity, persist the
//	      updated STS cache atomically, then return
//	-> otherwise exchange access token for STS via Portal
//	-> validate STS credentials and that expiration is strictly outside the
//	   RefreshWindow (seconds and milliseconds both accepted)
//	-> persist STS cache atomically with no commit identities
//	-> patch profile sts-expiration via config.Update
//	-> add the current target's commit identity, persist the STS cache
//	   atomically, then return auth.Value
//
// Lock order is strict: token -> STS -> config. Any persistence or config
// failure fails closed: no in-memory credentials are returned. If config
// patching fails after the STS cache is written, the just-written STS entry is
// preserved (not deleted) with an uncommitted per-target marker state. A
// subsequent Retrieve for the same target retries the config patch using the
// cached STS without another Portal exchange; a different target sharing the
// same STS binding must independently patch and commit its own profile.
func (p *SSOProvider) Retrieve(ctx context.Context) (auth.Value, error) {
	if p == nil {
		return auth.Value{}, errors.New("nil *SSOProvider")
	}
	if isNilInterface(ctx) {
		return auth.Value{}, errors.New("nil context")
	}

	var val auth.Value
	err := p.cache.WithTokenLock(ctx, p.startURL, p.sessionName, func() error {
		// 1. Re-read token cache from disk inside the lock.
		tokenCache, rerr := p.cache.ReadToken(p.startURL, p.sessionName)
		if rerr != nil {
			return reauthRequired("sso token cache is unreadable; run: volclog sso login", rerr)
		}
		if tokenCache == nil {
			return &auth.Error{Kind: auth.ReauthRequired, Description: "sso token cache missing; run: volclog sso login"}
		}

		// 2. Validate token cache schema, binding identity, region, and expiration
		// via the shared inspector. The returned status carries the parsed
		// ExpiresAt so we do not re-parse it here.
		tokenStatus, verr := InspectTokenCache(tokenCache, p.startURL, p.sessionName, p.ssoRegion, p.clock())
		if verr != nil {
			return reauthRequired("sso token cache is invalid; run: volclog sso login", verr)
		}

		// 3. Refresh if near expiry. The local tokenCache variable is updated to
		// the rotated cache so a later refresh (e.g. after the STS lock wait)
		// uses the new refresh-token lineage rather than the stale pre-rotation
		// snapshot.
		accessToken := tokenCache.AccessToken
		expiresAt := tokenStatus.ExpiresAt
		rotated, newAccess, newExp, rerr := p.refreshIfNearExpiry(ctx, tokenCache, accessToken, expiresAt)
		if rerr != nil {
			return rerr
		}
		tokenCache = rotated
		accessToken = newAccess
		expiresAt = newExp

		// 4. Acquire STS lock while holding token lock (order: token -> STS).
		return p.cache.WithSTSLock(ctx, p.sessionName, p.accountID, p.roleName, func() error {
			// 5. Re-read STS cache from disk inside the lock.
			stsCache, rerr := p.cache.ReadSTS(p.sessionName, p.accountID, p.roleName)
			switch {
			case errors.Is(rerr, securestore.ErrMissing):
				// Missing: exchange.
			case rerr != nil:
				// Corrupt, permission, or other I/O error: fail closed without
				// calling Portal or overwriting the file.
				return &auth.Error{Kind: auth.CacheCorrupt, Description: "sts cache is unreadable; run: volclog sso login", Cause: rerr}
			default:
				// Cache exists: validate binding, provider, completeness, expiry.
				// Use a fresh clock value here: the OAuth refresh above and the
				// STS lock acquisition may have advanced real time, so the `now`
				// captured before the refresh/lock is stale. A committed STS that
				// expired during the refresh or lock wait must not take the fast
				// path.
				stsNow := p.clock()
				v, verr := stsCacheToValue(stsCache, p.sessionName, p.accountID, p.roleName, stsNow)
				if verr == nil {
					// Cache is valid (not expired). Use the fast path only when
					// THIS config target's commit identity is present. Another
					// profile sharing the same STS binding must independently
					// patch and commit its own profile.
					target, terr := commitTargetKey(p.configPath, p.profileName)
					if terr != nil {
						return &auth.Error{Kind: auth.ProtocolError, Description: "derive sts commit identity failed; run: volclog sso login", Cause: terr}
					}
					if stsCache.HasCommittedTarget(target) {
						// Fast path: config already committed for this target.
						val = v
						return nil
					}
					// Valid but not committed for this target: retry the config
					// patch using the cached STS expiration, then atomically add
					// this target's commit identity. No Portal exchange is
					// needed. This recovers from a crash between STS write and
					// config patch, and from a different target having committed
					// first.
					expiresAt, perr := time.Parse(time.RFC3339, stsCache.ExpiresAt)
					if perr != nil {
						return &auth.Error{Kind: auth.CacheCorrupt, Description: "sts cache has invalid expiration; run: volclog sso login", Cause: perr}
					}
					if uerr := p.patchConfig(expiresAt); uerr != nil {
						// If the profile binding no longer matches (concurrent
						// rebind), and this STS cache has no other committed
						// targets (it is a global orphan), delete it so a stale
						// cache cannot be reused. If other profiles have committed
						// this STS, the shared cache must be preserved. Transient
						// config I/O failures preserve the cache for retry.
						if errors.Is(uerr, errBindingMismatch) && len(stsCache.CommittedTargets) == 0 {
							if derr := p.cache.DeleteSTS(p.sessionName, p.accountID, p.roleName); derr != nil && !errors.Is(derr, securestore.ErrMissing) {
								return errors.Join(uerr, derr)
							}
						}
						return uerr
					}
					stsCache.AddCommittedTarget(target)
					if werr := p.cache.WriteSTS(stsCache); werr != nil {
						return &auth.Error{Kind: auth.ProtocolError, Description: "persist sts cache failed; run: volclog sso login", Cause: werr}
					}
					// Re-read the persisted STS and validate it again with a fresh
					// clock: the config patch and marker write may have advanced
					// time past the refresh window. Do not return the stale
					// in-memory value; the next Retrieve may exchange again.
					persisted, rerr := p.cache.ReadSTS(p.sessionName, p.accountID, p.roleName)
					if rerr != nil {
						return &auth.Error{Kind: auth.CacheCorrupt, Description: "sts cache is unreadable after commit; run: volclog sso login", Cause: rerr}
					}
					recheckNow := p.clock()
					v2, verr := stsCacheToValue(persisted, p.sessionName, p.accountID, p.roleName, recheckNow)
					if verr != nil {
						return &auth.Error{Kind: auth.ProtocolError, Description: "sts cache expired or invalid during commit; run: volclog sso login"}
					}
					val = v2
					return nil
				}
				if errors.Is(verr, errSTSExpired) {
					// Valid but expired/near expiry: exchange.
				} else {
					// Incomplete, binding mismatch, or provider mismatch: fail
					// closed without calling Portal or overwriting the file.
					return &auth.Error{Kind: auth.CacheCorrupt, Description: "sts cache is invalid; run: volclog sso login", Cause: verr}
				}
			}

			// 6. STS cache is missing or stale: exchange. Before calling Portal,
			// re-check access token freshness with a fresh clock: the STS lock
			// wait may have advanced time past the refresh window. Refresh under
			// the already-held token -> STS lock order if needed, using the
			// (possibly rotated) tokenCache's refresh-token lineage.
			rotated, newAccess, newExp, rerr := p.refreshIfNearExpiry(ctx, tokenCache, accessToken, expiresAt)
			if rerr != nil {
				return rerr
			}
			tokenCache = rotated
			accessToken = newAccess
			expiresAt = newExp

			creds, cerr := p.portal.GetRoleCredentials(ctx, accessToken, p.accountID, p.roleName)
			if cerr != nil {
				return &auth.Error{Kind: auth.ProtocolError, Description: "sso sts exchange failed; run: volclog sso login", Cause: cerr}
			}
			if creds == nil {
				return &auth.Error{Kind: auth.ProtocolError, Description: "sso sts exchange returned empty response"}
			}
			if err := validateRoleCredentials(creds); err != nil {
				return &auth.Error{Kind: auth.ProtocolError, Description: "sso sts exchange returned invalid credentials", Cause: err}
			}

			stsExp := creds.ExpirationTime()
			if stsExp.IsZero() {
				return &auth.Error{Kind: auth.ProtocolError, Description: "sso sts exchange returned invalid expiration"}
			}

			// Validate the returned STS is strictly outside the same
			// RefreshWindow used by cached STS validation. A freshly exchanged
			// STS that is already expired, exactly at the boundary, or within
			// the window must not be persisted, patched, or returned. Seconds
			// and milliseconds are both handled by ExpirationTime.
			exchangeNow := p.clock()
			if !exchangeNow.Before(stsExp.Add(-RefreshWindow)) {
				return &auth.Error{Kind: auth.ProtocolError, Description: "sso sts exchange returned expired or near-expiry credentials"}
			}

			// 7. Persist STS cache with no commit identities. This is the
			// durable commit marker: a subsequent Retrieve will not return
			// this cache directly until the current target's config is patched
			// and its identity is added.
			newSTSCache := &STSCache{
				SessionName:      p.sessionName,
				AccountID:        p.accountID,
				RoleName:         p.roleName,
				AccessKeyID:      creds.AccessKeyID,
				SecretAccessKey:  creds.SecretAccessKey,
				SessionToken:     creds.SessionToken,
				ProviderName:     ProviderName,
				ExpiresAt:        stsExp.UTC().Format(time.RFC3339),
				CommittedTargets: nil,
			}
			if werr := p.cache.WriteSTS(newSTSCache); werr != nil {
				// Fail closed: do not return in-memory credentials.
				return &auth.Error{Kind: auth.ProtocolError, Description: "persist sts cache failed; run: volclog sso login", Cause: werr}
			}

			// 8. Patch profile sts-expiration via config.Update (order: STS -> config).
			if uerr := p.patchConfig(stsExp); uerr != nil {
				// If the profile binding no longer matches (concurrent rebind),
				// the just-written uncommitted STS cache is orphaned: delete it
				// so a stale cache cannot be reused. Transient config I/O
				// failures preserve the cache for retry.
				if errors.Is(uerr, errBindingMismatch) {
					if derr := p.cache.DeleteSTS(p.sessionName, p.accountID, p.roleName); derr != nil && !errors.Is(derr, securestore.ErrMissing) {
						return errors.Join(uerr, derr)
					}
				}
				return uerr
			}

			// 9. Add this target's commit identity. Only after this succeeds do
			// we return credentials.
			target, terr := commitTargetKey(p.configPath, p.profileName)
			if terr != nil {
				return &auth.Error{Kind: auth.ProtocolError, Description: "derive sts commit identity failed; run: volclog sso login", Cause: terr}
			}
			newSTSCache.AddCommittedTarget(target)
			if werr := p.cache.WriteSTS(newSTSCache); werr != nil {
				return &auth.Error{Kind: auth.ProtocolError, Description: "persist sts cache failed; run: volclog sso login", Cause: werr}
			}

			// Re-read the persisted STS and validate it again with a fresh clock:
			// the exchange, uncommitted write, config patch, and marker write may
			// have advanced time past the refresh window. Do not return stale
			// in-memory credentials; the next Retrieve may exchange again.
			persisted, rerr := p.cache.ReadSTS(p.sessionName, p.accountID, p.roleName)
			if rerr != nil {
				return &auth.Error{Kind: auth.CacheCorrupt, Description: "sts cache is unreadable after commit; run: volclog sso login", Cause: rerr}
			}
			recheckNow := p.clock()
			v2, verr := stsCacheToValue(persisted, p.sessionName, p.accountID, p.roleName, recheckNow)
			if verr != nil {
				return &auth.Error{Kind: auth.ProtocolError, Description: "sts cache expired or invalid during commit; run: volclog sso login"}
			}
			val = v2
			return nil
		})
	})

	if err != nil {
		return auth.Value{}, err
	}
	return val, nil
}

// refreshToken refreshes the access token using the client ID/secret/refresh
// token stored in the token cache. It validates the client registration expiry
// before refreshing, preserves the old refresh token if the response omits a
// new one, and persists the rotated token cache atomically before returning.
func (p *SSOProvider) refreshToken(ctx context.Context, tokenCache *TokenCache) (*TokenCache, error) {
	// Validate client registration expiry before refresh.
	if tokenCache.ClientSecretExpiresAt != 0 {
		exp := normalizeExpiry(tokenCache.ClientSecretExpiresAt)
		if !p.clock().Before(exp) {
			return nil, &auth.Error{Kind: auth.ReauthRequired, Description: "sso client registration expired; run: volclog sso login"}
		}
	}
	if strings.TrimSpace(tokenCache.RefreshToken) == "" {
		return nil, &auth.Error{Kind: auth.ReauthRequired, Description: "sso token cache missing refresh token; run: volclog sso login"}
	}

	resp, err := p.oauth.CreateToken(ctx, &CreateTokenRequest{
		GrantType:    GrantTypeRefreshToken,
		ClientID:     tokenCache.ClientID,
		ClientSecret: tokenCache.ClientSecret,
		RefreshToken: tokenCache.RefreshToken,
	})
	if err != nil {
		var apiErr *OAuthAPIError
		if errors.As(err, &apiErr) && apiErr.Code == "invalid_grant" {
			return nil, &auth.Error{Kind: auth.ReauthRequired, Description: "sso login expired; run: volclog sso login"}
		}
		return nil, &auth.Error{Kind: auth.ProtocolError, Description: "sso token refresh failed; run: volclog sso login", Cause: err}
	}
	if resp == nil {
		return nil, &auth.Error{Kind: auth.ProtocolError, Description: "sso token refresh returned empty response"}
	}
	if strings.TrimSpace(resp.AccessToken) == "" {
		return nil, &auth.Error{Kind: auth.ProtocolError, Description: "sso token refresh returned empty access token"}
	}
	if resp.ExpiresIn <= 0 {
		return nil, &auth.Error{Kind: auth.ProtocolError, Description: "sso token refresh returned invalid expiration"}
	}
	refreshTTL, derr := secondsToDuration(resp.ExpiresIn)
	if derr != nil {
		return nil, &auth.Error{Kind: auth.ProtocolError, Description: "sso token refresh returned invalid expiration", Cause: derr}
	}

	// Build the rotated cache, preserving old fields when the response omits
	// optional replacements.
	rotated := *tokenCache
	rotated.AccessToken = resp.AccessToken
	rotated.ExpiresAt = p.clock().Add(refreshTTL).UTC().Format(time.RFC3339)
	if resp.RefreshToken != "" {
		rotated.RefreshToken = resp.RefreshToken
	}

	// Persist the rotated token cache atomically before returning.
	if werr := p.cache.WriteToken(&rotated); werr != nil {
		return nil, &auth.Error{Kind: auth.ProtocolError, Description: "persist rotated token cache failed; run: volclog sso login", Cause: werr}
	}
	return &rotated, nil
}

// refreshIfNearExpiry checks whether the held access token (with the given
// expiry) is within RefreshWindow of expiry using a fresh clock. If it is, it
// refreshes the token using the refresh token stored in tokenCache, persists the
// rotated cache atomically, and returns the rotated cache along with the new
// access token and expiry. After refresh it re-checks with a fresh clock: if the
// new token is already near expiry (short response lifetime or a persist that
// blocked long enough), it fails closed with a ProtocolError and never returns
// the stale token.
//
// The caller MUST hold the token lock so the refresh is serialized; when called
// before the Portal exchange the STS lock is also held (order: token -> STS).
// The returned *TokenCache is the rotated cache so callers can thread the new
// refresh-token lineage into subsequent refreshes.
func (p *SSOProvider) refreshIfNearExpiry(ctx context.Context, tokenCache *TokenCache, accessToken string, expiresAt time.Time) (*TokenCache, string, time.Time, error) {
	now := p.clock()
	if now.Before(expiresAt.Add(-RefreshWindow)) {
		return tokenCache, accessToken, expiresAt, nil
	}
	refreshed, err := p.refreshToken(ctx, tokenCache)
	if err != nil {
		return nil, "", time.Time{}, err
	}
	newExp, perr := time.Parse(time.RFC3339, refreshed.ExpiresAt)
	if perr != nil {
		return nil, "", time.Time{}, &auth.Error{Kind: auth.ProtocolError, Description: "refreshed token has invalid expiration", Cause: perr}
	}
	// Re-check with a fresh clock. If the response lifetime was too short or the
	// persist blocked long enough for the new token to enter the window, fail
	// closed rather than using a near-expiry token for the Portal exchange.
	recheck := p.clock()
	if !recheck.Before(newExp.Add(-RefreshWindow)) {
		return nil, "", time.Time{}, &auth.Error{Kind: auth.ProtocolError, Description: "refreshed token expired or near expiry before use; run: volclog sso login"}
	}
	return refreshed, refreshed.AccessToken, newExp, nil
}

// patchConfig patches only the explicit target profile's nonsecret
// sts-expiration. It requires an exact match between the profile and the
// Provider binding: mode must be sso, and the session/account/role must match
// exactly. Empty fields are mismatches, not wildcards. On mismatch it does not
// patch STSExpiration and returns an error so no credentials are returned.
func (p *SSOProvider) patchConfig(expiresAt time.Time) error {
	_, err := p.configStore.Update(p.configPath, func(c *config.Config) error {
		profile, ok := c.GetProfile(p.profileName)
		if !ok {
			// Profile removed concurrently: a permanent binding mismatch, so
			// any uncommitted STS orphan is cleaned up (not preserved for retry).
			return errBindingMismatch
		}
		// Require exact binding match. Empty fields are mismatches, not
		// wildcards: a profile without mode=sso or without the exact
		// session/account/role must not be patched.
		if profile.Mode != config.AuthModeSSO {
			return errBindingMismatch
		}
		if profile.SSOSessionName != p.sessionName {
			return errBindingMismatch
		}
		if profile.AccountID != p.accountID {
			return errBindingMismatch
		}
		if profile.RoleName != p.roleName {
			return errBindingMismatch
		}
		// Patch only sts-expiration.
		profile.STSExpiration = expiresAt.Unix()
		c.PutProfile(p.profileName, profile)
		return nil
	})
	if err != nil {
		// Preserve the original cause in the error chain while keeping the
		// sentinel detectable via errors.Is.
		if errors.Is(err, errBindingMismatch) {
			return &auth.Error{Kind: auth.ProtocolError, Description: "profile binding does not match provider; run: volclog sso login", Cause: err}
		}
		return &auth.Error{Kind: auth.ProtocolError, Description: "update profile sts-expiration failed", Cause: err}
	}
	return nil
}

// stsCacheToValue converts a valid STS cache to an auth.Value, validating the
// binding identity, provider name, completeness, and expiration. It returns
// errSTSExpired (which permits a Portal exchange) only when the cache is
// otherwise valid but expired or within the refresh window. All other errors
// (incomplete, binding mismatch, provider mismatch) fail closed: the caller
// must not call Portal or overwrite the file.
func stsCacheToValue(c *STSCache, sessionName, accountID, roleName string, now time.Time) (auth.Value, error) {
	status, err := InspectSTSCache(c, sessionName, accountID, roleName, now)
	if err != nil {
		return auth.Value{}, err
	}
	if !status.Present {
		// InspectSTSCache returns an error for invalid caches, so this branch
		// is unreachable in practice; kept for defensive completeness.
		return auth.Value{}, errors.New("sts cache invalid")
	}
	if status.RefreshRequired {
		// Valid but within the refresh window: the caller may exchange.
		return auth.Value{}, errSTSExpired
	}
	return auth.Value{
		AccessKeyID:     c.AccessKeyID,
		SecretAccessKey: c.SecretAccessKey,
		SessionToken:    c.SessionToken,
		ProviderName:    c.ProviderName,
		ExpiresAt:       status.ExpiresAt,
	}, nil
}

// validateRoleCredentials validates the minimum required fields of a
// GetRoleCredentials response.
func validateRoleCredentials(c *RoleCredentials) error {
	if c == nil {
		return errors.New("nil role credentials")
	}
	if strings.TrimSpace(c.AccessKeyID) == "" {
		return errors.New("role credentials missing access key id")
	}
	if strings.TrimSpace(c.SecretAccessKey) == "" {
		return errors.New("role credentials missing secret access key")
	}
	if strings.TrimSpace(c.SessionToken) == "" {
		return errors.New("role credentials missing session token")
	}
	if c.Expiration <= 0 {
		return errors.New("role credentials missing expiration")
	}
	return nil
}

// reauthRequired returns a top-level ReauthRequired error. When cause is
// non-nil, it is preserved via Unwrap so errors.Is/errors.As still match it,
// but it is never rendered in Error().
func reauthRequired(description string, cause error) error {
	if cause == nil {
		return &auth.Error{Kind: auth.ReauthRequired, Description: description}
	}
	return &auth.Error{Kind: auth.ReauthRequired, Description: description, Cause: cause}
}

// Compile-time assertion that SSOProvider implements auth.Provider.
var _ auth.Provider = (*SSOProvider)(nil)
