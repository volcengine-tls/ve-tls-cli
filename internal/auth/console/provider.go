package console

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth"
)

// ProviderName is the provider name returned in auth.Value for Console Login.
const ProviderName = "console-login"

// Provider is a refreshable auth.Provider backed by the Console Login token
// cache. It reads the cache on every Retrieve, refreshes the token when it is
// within RefreshBuffer of expiry, and atomically persists the rotated cache
// before returning credentials.
//
// All refresh operations are serialized through the per-session cache lock so
// that concurrent Retrieve calls (even across processes) produce exactly one
// refresh request. The Provider never falls back to environment AK/SK or
// dormant profile credentials.
type Provider struct {
	loginSession string
	cache        ConsoleCache
	oauthFactory OAuthClientFactory
	clock        func() time.Time
}

// NewProvider constructs a Console Provider for the given login session. If
// clock is nil, time.Now is used.
func NewProvider(loginSession string, cache ConsoleCache, oauthFactory OAuthClientFactory, clock func() time.Time) *Provider {
	if clock == nil {
		clock = time.Now
	}
	return &Provider{
		loginSession: loginSession,
		cache:        cache,
		oauthFactory: oauthFactory,
		clock:        clock,
	}
}

// Retrieve returns valid STS credentials, refreshing the cached token if it is
// within RefreshBuffer of expiry. It implements auth.Provider.
//
// The retrieve algorithm is frozen:
//
//	WithLock(login-session)
//	-> re-read cache from disk inside the lock
//	-> validate cache/login-session/schema and compute expiration
//	-> if now < expiresAt - RefreshBuffer, parse cached STS and return it
//	-> otherwise refresh inside the same lock
//	-> validate refreshed STS
//	-> preserve old RefreshToken/IDToken/scope/endpoint when response omits them
//	-> set IssuedAt/ExpiresIn/TokenType/raw AccessToken
//	-> atomically persist the complete rotated cache
//	-> only then return the new auth.Value
//
// A cache write failure fails closed: the in-memory credentials are never
// returned. Missing, corrupt, or invalid-grant caches return a safe
// ReauthRequired error with a login hint that contains no secret material.
func (p *Provider) Retrieve(ctx context.Context) (auth.Value, error) {
	if p == nil {
		return auth.Value{}, errors.New("nil *Provider")
	}
	if isNilInterface(ctx) {
		return auth.Value{}, errors.New("nil context")
	}
	if strings.TrimSpace(p.loginSession) == "" {
		return auth.Value{}, &auth.Error{Kind: auth.ReauthRequired, Description: "console login cache missing; run: volclog login"}
	}
	if isNilInterface(p.cache) {
		return auth.Value{}, errors.New("nil cache")
	}
	if p.oauthFactory == nil {
		return auth.Value{}, errors.New("nil oauth client factory")
	}
	if p.clock == nil {
		return auth.Value{}, errors.New("nil clock")
	}

	var val auth.Value
	err := p.cache.WithLock(ctx, p.loginSession, func() error {
		// Re-read cache from disk inside the lock.
		data, existed, rerr := p.cache.ReadRaw(p.loginSession)
		if rerr != nil {
			return reauthRequiredWithCorruptCause("console login cache is unreadable; run: volclog login", rerr)
		}
		// A cache file that does not exist is a clean re-login signal: top-level
		// ReauthRequired only, no nested CacheCorrupt.
		if !existed {
			return &auth.Error{Kind: auth.ReauthRequired, Description: "console login cache missing; run: volclog login"}
		}
		// A cache file that exists but is empty is corrupt/misplaced content:
		// top-level ReauthRequired with a nested CacheCorrupt cause.
		if len(data) == 0 {
			return reauthRequiredWithCorruptCause("console login cache is empty; run: volclog login", nil)
		}

		// Validate cache schema and login-session.
		var cache LoginTokenCache
		if jerr := json.Unmarshal(data, &cache); jerr != nil {
			return reauthRequiredWithCorruptCause("console login cache is corrupt; run: volclog login", jerr)
		}

		now := p.clock()

		// InspectLoginCache validates the schema (login session, frozen client
		// ID, frozen scope, clean endpoint, non-empty token type, expiration)
		// and returns a secret-free status. It never refreshes or networks.
		//   - For near-expiry caches it only requires a non-empty RefreshToken
		//     (the old STS is not parsed). A missing/whitespace RefreshToken
		//     returns the stable sentinel errLoginCacheMissingRefreshToken.
		//   - For fast-path (non-expiring) caches it requires parseable STS.
		// Other validation errors become the corrupt cause.
		status, ierr := InspectLoginCache(&cache, p.loginSession, now)
		if ierr != nil {
			if errors.Is(ierr, errLoginCacheMissingRefreshToken) {
				// Recoverable stale cache: top-level ReauthRequired only, no
				// nested CacheCorrupt, so the user is prompted to re-login
				// rather than the cache being treated as corrupt data.
				return &auth.Error{Kind: auth.ReauthRequired, Description: "console login cache missing refresh token; run: volclog login"}
			}
			return reauthRequiredWithCorruptCause("console login cache is invalid; run: volclog login", ierr)
		}
		if !status.Present {
			// InspectLoginCache returns an error for invalid caches, so this
			// branch is unreachable in practice; kept for defensive completeness.
			return reauthRequiredWithCorruptCause("console login cache is invalid; run: volclog login", nil)
		}

		// If the cache is still valid (outside the refresh window), return it.
		// At exactly the boundary (now == expiresAt - RefreshBuffer), refresh.
		if !status.RefreshRequired {
			sts, serr := ParseSTSCredentials(cache.AccessToken)
			if serr != nil {
				// Unreachable: InspectLoginCache already validated STS on the
				// fast path.
				return reauthRequiredWithCorruptCause("console login cache has invalid STS; run: volclog login", serr)
			}
			val = auth.Value{
				AccessKeyID:     sts.AccessKeyID,
				SecretAccessKey: sts.SecretAccessKey,
				SessionToken:    sts.SessionToken,
				ProviderName:    ProviderName,
				ExpiresAt:       status.ExpiresAt,
			}
			return nil
		}

		// Refresh inside the same lock. InspectLoginCache already verified a
		// non-empty RefreshToken for the near-expiry path, so this is the
		// refreshable case.
		endpoint := cache.EndpointURL
		if endpoint == "" {
			endpoint = DefaultEndpoint
		}
		client, cerr := p.oauthFactory(endpoint)
		if cerr != nil {
			return &auth.Error{Kind: auth.ProtocolError, Description: "console login refresh failed; run: volclog login", Cause: cerr}
		}
		if isNilInterface(client) {
			return &auth.Error{Kind: auth.ProtocolError, Description: "console login refresh failed; run: volclog login"}
		}

		tokenResp, terr := client.ExchangeToken(ctx, &ConsoleTokenRequest{
			GrantType:    GrantTypeRefreshToken,
			RefreshToken: cache.RefreshToken,
			ClientID:     cache.ClientID,
			Scope:        Scope,
		})
		if terr != nil {
			// invalid_grant means the refresh token is expired/revoked.
			var apiErr *ConsoleOAuthAPIError
			if errors.As(terr, &apiErr) && apiErr.Response.Error == "invalid_grant" {
				return &auth.Error{Kind: auth.ReauthRequired, Description: "console login expired; run: volclog login"}
			}
			return &auth.Error{Kind: auth.ProtocolError, Description: "console login refresh failed; run: volclog login", Cause: terr}
		}
		if tokenResp == nil {
			return &auth.Error{Kind: auth.ProtocolError, Description: "console login refresh returned empty response; run: volclog login"}
		}

		// Validate refreshed STS.
		sts, serr := ParseSTSCredentials(tokenResp.AccessToken)
		if serr != nil {
			return &auth.Error{Kind: auth.ProtocolError, Description: "refreshed token has invalid STS"}
		}

		// A refreshed token must carry a non-empty TokenType. An empty one is a
		// protocol error: fail closed without persisting or returning credentials.
		if strings.TrimSpace(tokenResp.TokenType) == "" {
			return &auth.Error{Kind: auth.ProtocolError, Description: "refreshed token has empty token type; run: volclog login"}
		}

		// Build the rotated cache, preserving old fields when the response
		// omits optional replacements.
		rotated := cache
		rotated.AccessToken = tokenResp.AccessToken
		rotated.IssuedAt = p.clock().UTC().Format(time.RFC3339Nano)
		rotated.ExpiresIn = tokenResp.ExpiresIn
		rotated.TokenType = tokenResp.TokenType
		if tokenResp.RefreshToken != "" {
			rotated.RefreshToken = tokenResp.RefreshToken
		}
		if tokenResp.IDToken != "" {
			rotated.IDToken = tokenResp.IDToken
		}
		if sc := strings.TrimSpace(tokenResp.Scope); sc != "" && scopeSatisfiesFrozen(sc) {
			rotated.Scope = sc
		}

		// Validate the rotated cache expiration once and keep the checked value
		// so it is persisted and returned exactly, never recomputed.
		newExpiresAt, eerr := CacheExpiration(rotated.IssuedAt, rotated.ExpiresIn)
		if eerr != nil {
			return &auth.Error{Kind: auth.ProtocolError, Description: "refreshed token has invalid expiration"}
		}

		// Atomically persist the complete rotated cache BEFORE returning.
		rotatedBytes, merr := json.Marshal(rotated)
		if merr != nil {
			return &auth.Error{Kind: auth.ProtocolError, Description: "marshal rotated cache failed", Cause: merr}
		}
		if werr := p.cache.WriteRaw(p.loginSession, rotatedBytes); werr != nil {
			// Fail closed: do NOT return the in-memory new credentials.
			return &auth.Error{Kind: auth.ProtocolError, Description: "persist rotated cache failed; run: volclog login", Cause: werr}
		}

		// Only now return the new auth.Value using the exact checked expiration.
		val = auth.Value{
			AccessKeyID:     sts.AccessKeyID,
			SecretAccessKey: sts.SecretAccessKey,
			SessionToken:    sts.SessionToken,
			ProviderName:    ProviderName,
			ExpiresAt:       newExpiresAt,
		}
		return nil
	})

	if err != nil {
		return auth.Value{}, err
	}
	return val, nil
}

// reauthRequiredWithCorruptCause returns a top-level ReauthRequired error whose
// Cause is a CacheCorrupt error carrying the underlying diagnostic. errors.As
// sees the top-level ReauthRequired first, while errors.Is(err,
// &auth.Error{Kind: auth.CacheCorrupt}) still matches the nested cause. The
// description always carries the safe "run: volclog login" hint and never
// contains raw cache/session/token material.
func reauthRequiredWithCorruptCause(description string, cause error) error {
	return &auth.Error{
		Kind:        auth.ReauthRequired,
		Description: description,
		Cause: &auth.Error{
			Kind:        auth.CacheCorrupt,
			Description: description,
			Cause:       cause,
		},
	}
}

// isFrozenClientID reports whether id is one of the two frozen Console Login
// client IDs.
func isFrozenClientID(id string) bool {
	return id == ClientIDSameDevice || id == ClientIDCrossDevice
}

// isCleanEndpoint reports whether rawURL is a clean HTTPS URL: exact https
// scheme, non-empty host, no userinfo, no query, no fragment, and no opaque
// URL. A normal path is allowed. Surrounding whitespace is rejected rather
// than silently trimmed.
func isCleanEndpoint(rawURL string) bool {
	if rawURL == "" || rawURL != strings.TrimSpace(rawURL) {
		return false
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if u.Scheme != "https" {
		return false
	}
	if u.Opaque != "" {
		return false
	}
	if u.Host == "" {
		return false
	}
	if u.User != nil {
		return false
	}
	if u.RawQuery != "" {
		return false
	}
	if u.Fragment != "" {
		return false
	}
	return true
}

// Compile-time assertion that Provider implements auth.Provider.
var _ auth.Provider = (*Provider)(nil)
