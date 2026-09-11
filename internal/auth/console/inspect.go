package console

import (
	"errors"
	"strings"
	"time"
)

// errLoginCacheMissingRefreshToken is the stable sentinel returned by
// InspectLoginCache when a near-expiry/expired cache has a missing or
// whitespace-only RefreshToken. The Provider classifies this as a top-level
// ReauthRequired (not a nested CacheCorrupt) so a stale-but-recoverable cache
// prompts a re-login rather than being treated as corrupt data.
var errLoginCacheMissingRefreshToken = errors.New("console login cache missing refresh token")

// LoginCacheStatus is the secret-free result of inspecting a Console Login
// cache. It is used by both the Provider (to decide fast-path vs refresh) and
// by offline status readers (to report auth_present/refresh_required without
// exposing credentials).
type LoginCacheStatus struct {
	// Present is true only when the cache passes base schema/binding/expiration
	// validation AND either:
	//   - it is outside the refresh window (fast path) with parseable STS
	//     credentials, or
	//   - it is within the refresh window with a non-empty RefreshToken (so it
	//     can be refreshed; the old AccessToken is not parsed).
	// A near-expiry cache with a missing/whitespace RefreshToken reports
	// Present=false because it cannot be refreshed.
	Present bool
	// ExpiresAt is the computed expiration. It is the zero time when Present is
	// false.
	ExpiresAt time.Time
	// RefreshRequired is true when the cache is within the refresh window
	// (stale) or when the cache is invalid (Present=false).
	RefreshRequired bool
}

// InspectLoginCache validates the supplied Console Login cache against the
// expected login session and returns a secret-free status. It never refreshes,
// networks, or writes.
//
// Validation rules match Provider.Retrieve exactly:
//   - LoginSession must be non-empty and equal to loginSession
//   - ClientID must be one of the frozen Console IDs
//   - Scope must satisfy the frozen contract
//   - EndpointURL (if present) must be a clean HTTPS URL
//   - TokenType must be non-empty
//   - IssuedAt/ExpiresIn must compute a valid CacheExpiration
//
// After base validation the refresh window is computed:
//   - If within the refresh window (refreshRequired=true): only a non-empty
//     RefreshToken is required. The old AccessToken/STS is NOT parsed because
//     it is about to be discarded by refresh; a malformed old STS must not
//     block recovery. A missing/whitespace RefreshToken returns the stable
//     sentinel errLoginCacheMissingRefreshToken (Present=false).
//   - If outside the refresh window (fast path): the old AccessToken must parse
//     as valid STS credentials (ParseSTSCredentials); a corrupt old STS fails
//     closed (Present=false) with the parse error.
func InspectLoginCache(cache *LoginTokenCache, loginSession string, now time.Time) (LoginCacheStatus, error) {
	if cache == nil {
		return LoginCacheStatus{Present: false, RefreshRequired: true}, errors.New("nil login cache")
	}
	if strings.TrimSpace(cache.LoginSession) == "" || cache.LoginSession != loginSession {
		return LoginCacheStatus{Present: false, RefreshRequired: true}, errors.New("console login cache session mismatch")
	}
	if !isFrozenClientID(cache.ClientID) {
		return LoginCacheStatus{Present: false, RefreshRequired: true}, errors.New("console login cache has invalid client id")
	}
	if !scopeSatisfiesFrozen(cache.Scope) {
		return LoginCacheStatus{Present: false, RefreshRequired: true}, errors.New("console login cache has invalid scope")
	}
	if cache.EndpointURL != "" && !isCleanEndpoint(cache.EndpointURL) {
		return LoginCacheStatus{Present: false, RefreshRequired: true}, errors.New("console login cache has invalid endpoint")
	}
	if strings.TrimSpace(cache.TokenType) == "" {
		return LoginCacheStatus{Present: false, RefreshRequired: true}, errors.New("console login cache has empty token type")
	}
	expiresAt, eerr := CacheExpiration(cache.IssuedAt, cache.ExpiresIn)
	if eerr != nil {
		return LoginCacheStatus{Present: false, RefreshRequired: true}, eerr
	}
	refreshRequired := !now.Before(expiresAt.Add(-RefreshBuffer))
	if refreshRequired {
		// Near-expiry/expired: the old AccessToken is about to be discarded by
		// refresh, so do not require it to be parseable. Only a usable
		// RefreshToken is needed to recover.
		if strings.TrimSpace(cache.RefreshToken) == "" {
			return LoginCacheStatus{Present: false, RefreshRequired: true}, errLoginCacheMissingRefreshToken
		}
		return LoginCacheStatus{
			Present:         true,
			ExpiresAt:       expiresAt,
			RefreshRequired: true,
		}, nil
	}
	// Fast path: the old STS credentials will be used directly, so they must be
	// parseable. A corrupt old STS fails closed.
	if _, serr := ParseSTSCredentials(cache.AccessToken); serr != nil {
		return LoginCacheStatus{Present: false, RefreshRequired: true}, serr
	}
	return LoginCacheStatus{
		Present:         true,
		ExpiresAt:       expiresAt,
		RefreshRequired: false,
	}, nil
}
