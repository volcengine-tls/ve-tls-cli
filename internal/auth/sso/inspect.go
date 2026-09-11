package sso

import (
	"errors"
	"strings"
	"time"
)

// STSCacheStatus is the secret-free result of inspecting an STS cache. It is
// used by both the Provider (to decide fast-path vs exchange) and by offline
// status readers (to report auth_present/refresh_required without exposing
// credentials).
type STSCacheStatus struct {
	// Present is true only when the cache passes all schema, binding, and
	// credential validation. A near-expiry cache that is otherwise valid still
	// reports Present=true (RefreshRequired=true).
	Present bool
	// ExpiresAt is the parsed expiration. It is the zero time when Present is
	// false.
	ExpiresAt time.Time
	// RefreshRequired is true when the cache is within the refresh window
	// (stale) or when the cache is invalid (Present=false).
	RefreshRequired bool
}

// InspectSTSCache validates the supplied STS cache against the binding and
// returns a secret-free status. It never refreshes, networks, or writes.
//
// Validation rules match stsCacheToValue exactly:
//   - committed-targets marker set (if present) must be well-formed
//   - session/account/role binding must match
//   - ProviderName must be the exact SSO provider name
//   - AccessKeyID, SecretAccessKey, SessionToken must be non-empty
//   - ExpiresAt must parse as RFC3339
//
// A cache that passes validation but is within the refresh window reports
// Present=true, RefreshRequired=true. A cache that fails any validation reports
// Present=false, RefreshRequired=true, along with the validation error so the
// caller can classify it.
func InspectSTSCache(c *STSCache, sessionName, accountID, roleName string, now time.Time) (STSCacheStatus, error) {
	if c == nil {
		return STSCacheStatus{Present: false, RefreshRequired: true}, errors.New("nil sts cache")
	}
	if err := validateCommittedTargets(c.CommittedTargets); err != nil {
		return STSCacheStatus{Present: false, RefreshRequired: true}, err
	}
	if c.SessionName != sessionName || c.AccountID != accountID || c.RoleName != roleName {
		return STSCacheStatus{Present: false, RefreshRequired: true}, errors.New("sts cache binding mismatch")
	}
	if c.ProviderName != ProviderName {
		return STSCacheStatus{Present: false, RefreshRequired: true}, errors.New("sts cache provider name mismatch")
	}
	if strings.TrimSpace(c.AccessKeyID) == "" || strings.TrimSpace(c.SecretAccessKey) == "" || strings.TrimSpace(c.SessionToken) == "" {
		return STSCacheStatus{Present: false, RefreshRequired: true}, errors.New("sts cache missing credentials")
	}
	expiresAt, err := time.Parse(time.RFC3339, c.ExpiresAt)
	if err != nil {
		return STSCacheStatus{Present: false, RefreshRequired: true}, errors.New("sts cache has invalid expiration")
	}
	refreshRequired := !now.Before(expiresAt.Add(-RefreshWindow))
	return STSCacheStatus{
		Present:         true,
		ExpiresAt:       expiresAt,
		RefreshRequired: refreshRequired,
	}, nil
}

// TokenCacheStatus is the secret-free result of inspecting an SSO token cache.
// It is used by both the Provider (to decide fast-path vs refresh) and by
// offline status readers (to report auth_present/refresh_required without
// exposing credentials).
type TokenCacheStatus struct {
	// Present is true only when the cache passes all schema, binding, and
	// credential validation. A near-expiry cache that is otherwise valid and
	// refreshable still reports Present=true (RefreshRequired=true).
	Present bool
	// ExpiresAt is the parsed token expiration. It is the zero time when
	// Present is false.
	ExpiresAt time.Time
	// RefreshRequired is true when the cache is within the refresh window
	// (stale) or when the cache is invalid (Present=false).
	RefreshRequired bool
}

// InspectTokenCache validates the supplied SSO token cache against the binding
// and returns a secret-free status. It never refreshes, networks, or writes.
//
// Validation rules match SSOProvider.Retrieve exactly:
//   - StartURL must match (compared in canonical form)
//   - SessionName must match
//   - Region must match (compared in trimmed form)
//   - AccessToken, ClientID, ClientSecret must be non-empty
//   - ExpiresAt must parse as RFC3339
//
// A cache that passes validation but is within the refresh window additionally
// requires a non-empty RefreshToken and (if set) a still-valid
// ClientSecretExpiresAt to be refreshable; if those fail the cache is reported
// as Present=false (fail closed). A cache that fails any base validation
// reports Present=false, RefreshRequired=true, along with the validation error.
func InspectTokenCache(c *TokenCache, startURL, sessionName, region string, now time.Time) (TokenCacheStatus, error) {
	if c == nil {
		return TokenCacheStatus{Present: false, RefreshRequired: true}, errors.New("nil token cache")
	}
	// Compare canonical StartURL identities rather than raw spelling.
	cacheStart, err := CanonicalStartURL(c.StartURL)
	if err != nil {
		return TokenCacheStatus{Present: false, RefreshRequired: true}, errors.New("token cache has invalid start URL")
	}
	canonical, err := CanonicalStartURL(startURL)
	if err != nil {
		return TokenCacheStatus{Present: false, RefreshRequired: true}, errors.New("provider start URL is invalid")
	}
	if cacheStart != canonical {
		return TokenCacheStatus{Present: false, RefreshRequired: true}, errors.New("token cache start URL mismatch")
	}
	if strings.TrimSpace(c.SessionName) == "" || c.SessionName != sessionName {
		return TokenCacheStatus{Present: false, RefreshRequired: true}, errors.New("token cache session name mismatch")
	}
	cacheRegion := strings.TrimSpace(c.Region)
	if cacheRegion == "" {
		return TokenCacheStatus{Present: false, RefreshRequired: true}, errors.New("token cache missing region")
	}
	if cacheRegion != strings.TrimSpace(region) {
		return TokenCacheStatus{Present: false, RefreshRequired: true}, errors.New("token cache region mismatch")
	}
	if strings.TrimSpace(c.AccessToken) == "" {
		return TokenCacheStatus{Present: false, RefreshRequired: true}, errors.New("token cache missing access token")
	}
	if strings.TrimSpace(c.ClientID) == "" {
		return TokenCacheStatus{Present: false, RefreshRequired: true}, errors.New("token cache missing client id")
	}
	if strings.TrimSpace(c.ClientSecret) == "" {
		return TokenCacheStatus{Present: false, RefreshRequired: true}, errors.New("token cache missing client secret")
	}
	expiresAt, perr := time.Parse(time.RFC3339, c.ExpiresAt)
	if perr != nil {
		return TokenCacheStatus{Present: false, RefreshRequired: true}, errors.New("token cache has invalid expiration")
	}
	nearExpiry := !now.Before(expiresAt.Add(-RefreshWindow))
	if nearExpiry {
		// Within the refresh window: the cache is only usable if it can be
		// refreshed. A missing refresh token or expired client registration
		// fails closed (Present=false) rather than Present=true.
		if c.ClientSecretExpiresAt != 0 {
			exp := normalizeExpiry(c.ClientSecretExpiresAt)
			if !now.Before(exp) {
				return TokenCacheStatus{Present: false, RefreshRequired: true}, errors.New("token cache client registration expired")
			}
		}
		if strings.TrimSpace(c.RefreshToken) == "" {
			return TokenCacheStatus{Present: false, RefreshRequired: true}, errors.New("token cache missing refresh token")
		}
	}
	return TokenCacheStatus{
		Present:         true,
		ExpiresAt:       expiresAt,
		RefreshRequired: nearExpiry,
	}, nil
}
