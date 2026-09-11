package sso

import (
	"testing"
	"time"
)

func inspectValidSTSCache() *STSCache {
	return &STSCache{
		SessionName:      "session-a",
		AccountID:        "acct-1",
		RoleName:         "role-1",
		AccessKeyID:      "AKLTvalid",
		SecretAccessKey:  "valid-secret",
		SessionToken:     "valid-token",
		ProviderName:     ProviderName,
		ExpiresAt:        time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		CommittedTargets: nil,
	}
}

func TestInspectSTSCache_ValidFuture(t *testing.T) {
	c := inspectValidSTSCache()
	now := time.Now()
	status, err := InspectSTSCache(c, "session-a", "acct-1", "role-1", now)
	if err != nil {
		t.Fatalf("expected no error for valid cache, got %v", err)
	}
	if !status.Present {
		t.Fatalf("expected Present=true for valid future cache")
	}
	if status.RefreshRequired {
		t.Fatalf("expected RefreshRequired=false for valid future cache")
	}
	if status.ExpiresAt.IsZero() {
		t.Fatalf("expected non-zero ExpiresAt")
	}
}

func TestInspectSTSCache_ValidNearExpiry(t *testing.T) {
	c := inspectValidSTSCache()
	// Expires in 30 seconds, which is within the 60s RefreshWindow.
	c.ExpiresAt = time.Now().Add(30 * time.Second).UTC().Format(time.RFC3339)
	now := time.Now()
	status, err := InspectSTSCache(c, "session-a", "acct-1", "role-1", now)
	if err != nil {
		t.Fatalf("expected no error for valid near-expiry cache, got %v", err)
	}
	if !status.Present {
		t.Fatalf("expected Present=true for valid near-expiry cache")
	}
	if !status.RefreshRequired {
		t.Fatalf("expected RefreshRequired=true for near-expiry cache")
	}
}

func TestInspectSTSCache_MissingCredentials(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*STSCache)
	}{
		{"missing_ak", func(c *STSCache) { c.AccessKeyID = "" }},
		{"missing_sk", func(c *STSCache) { c.SecretAccessKey = "" }},
		{"missing_token", func(c *STSCache) { c.SessionToken = "" }},
		{"whitespace_ak", func(c *STSCache) { c.AccessKeyID = "   " }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := inspectValidSTSCache()
			tc.mut(c)
			status, err := InspectSTSCache(c, "session-a", "acct-1", "role-1", time.Now())
			if err == nil {
				t.Fatalf("expected error for cache with %s", tc.name)
			}
			if status.Present {
				t.Fatalf("expected Present=false for cache with %s", tc.name)
			}
			if !status.RefreshRequired {
				t.Fatalf("expected RefreshRequired=true for cache with %s", tc.name)
			}
		})
	}
}

func TestInspectSTSCache_BindingMismatch(t *testing.T) {
	c := inspectValidSTSCache()
	cases := []struct {
		name        string
		sessionName string
		accountID   string
		roleName    string
	}{
		{"session_mismatch", "other-session", "acct-1", "role-1"},
		{"account_mismatch", "session-a", "other-acct", "role-1"},
		{"role_mismatch", "session-a", "acct-1", "other-role"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, err := InspectSTSCache(c, tc.sessionName, tc.accountID, tc.roleName, time.Now())
			if err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
			if status.Present {
				t.Fatalf("expected Present=false for %s", tc.name)
			}
			if !status.RefreshRequired {
				t.Fatalf("expected RefreshRequired=true for %s", tc.name)
			}
		})
	}
}

func TestInspectSTSCache_ProviderMismatch(t *testing.T) {
	c := inspectValidSTSCache()
	c.ProviderName = "some-other-provider"
	status, err := InspectSTSCache(c, "session-a", "acct-1", "role-1", time.Now())
	if err == nil {
		t.Fatalf("expected error for provider mismatch")
	}
	if status.Present {
		t.Fatalf("expected Present=false for provider mismatch")
	}
	if !status.RefreshRequired {
		t.Fatalf("expected RefreshRequired=true for provider mismatch")
	}
}

func TestInspectSTSCache_InvalidCommittedTargets(t *testing.T) {
	c := inspectValidSTSCache()
	// Not lowercase hex, wrong length.
	c.CommittedTargets = []string{"ZZZ"}
	status, err := InspectSTSCache(c, "session-a", "acct-1", "role-1", time.Now())
	if err == nil {
		t.Fatalf("expected error for invalid committed targets")
	}
	if status.Present {
		t.Fatalf("expected Present=false for invalid committed targets")
	}
	if !status.RefreshRequired {
		t.Fatalf("expected RefreshRequired=true for invalid committed targets")
	}
}

func TestInspectSTSCache_InvalidExpiration(t *testing.T) {
	c := inspectValidSTSCache()
	c.ExpiresAt = "not-a-date"
	status, err := InspectSTSCache(c, "session-a", "acct-1", "role-1", time.Now())
	if err == nil {
		t.Fatalf("expected error for invalid expiration")
	}
	if status.Present {
		t.Fatalf("expected Present=false for invalid expiration")
	}
	if !status.RefreshRequired {
		t.Fatalf("expected RefreshRequired=true for invalid expiration")
	}
}

func TestInspectSTSCache_NilCache(t *testing.T) {
	status, err := InspectSTSCache(nil, "session-a", "acct-1", "role-1", time.Now())
	if err == nil {
		t.Fatalf("expected error for nil cache")
	}
	if status.Present {
		t.Fatalf("expected Present=false for nil cache")
	}
	if !status.RefreshRequired {
		t.Fatalf("expected RefreshRequired=true for nil cache")
	}
}

func inspectValidTokenCache(now time.Time) *TokenCache {
	return &TokenCache{
		StartURL:     "https://example.com",
		SessionName:  "session-a",
		AccessToken:  "valid-access-token",
		ExpiresAt:    now.Add(time.Hour).UTC().Format(time.RFC3339),
		ClientID:     "valid-client-id",
		ClientSecret: "valid-client-secret",
		Region:       "cn-beijing",
	}
}

func TestInspectTokenCache_Nil(t *testing.T) {
	baseNow := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	status, err := InspectTokenCache(nil, "https://example.com", "session-a", "cn-beijing", baseNow)
	if err == nil {
		t.Fatalf("expected error for nil cache")
	}
	if status.Present {
		t.Fatalf("expected Present=false for nil cache")
	}
	if !status.RefreshRequired {
		t.Fatalf("expected RefreshRequired=true for nil cache")
	}
}

func TestInspectTokenCache_ValidFuture(t *testing.T) {
	baseNow := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	c := inspectValidTokenCache(baseNow)
	status, err := InspectTokenCache(c, "https://example.com", "session-a", "cn-beijing", baseNow)
	if err != nil {
		t.Fatalf("expected no error for valid cache, got %v", err)
	}
	if !status.Present {
		t.Fatalf("expected Present=true for valid future cache")
	}
	if status.RefreshRequired {
		t.Fatalf("expected RefreshRequired=false for valid future cache")
	}
	if status.ExpiresAt.IsZero() {
		t.Fatalf("expected non-zero ExpiresAt")
	}
}

func TestInspectTokenCache_NearExpiryRefreshable(t *testing.T) {
	baseNow := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	c := inspectValidTokenCache(baseNow)
	// Expires in 30 seconds, within the 60s RefreshWindow, with a refresh token.
	c.ExpiresAt = baseNow.Add(30 * time.Second).UTC().Format(time.RFC3339)
	c.RefreshToken = "valid-refresh-token"
	status, err := InspectTokenCache(c, "https://example.com", "session-a", "cn-beijing", baseNow)
	if err != nil {
		t.Fatalf("expected no error for valid near-expiry refreshable cache, got %v", err)
	}
	if !status.Present {
		t.Fatalf("expected Present=true for valid near-expiry refreshable cache")
	}
	if !status.RefreshRequired {
		t.Fatalf("expected RefreshRequired=true for near-expiry cache")
	}
}

func TestInspectTokenCache_NearExpiryMissingRefreshToken(t *testing.T) {
	baseNow := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	c := inspectValidTokenCache(baseNow)
	c.ExpiresAt = baseNow.Add(30 * time.Second).UTC().Format(time.RFC3339)
	c.RefreshToken = ""
	status, err := InspectTokenCache(c, "https://example.com", "session-a", "cn-beijing", baseNow)
	if err == nil {
		t.Fatalf("expected error for near-expiry cache missing refresh token")
	}
	if status.Present {
		t.Fatalf("expected Present=false for near-expiry cache missing refresh token")
	}
	if !status.RefreshRequired {
		t.Fatalf("expected RefreshRequired=true for near-expiry cache missing refresh token")
	}
}

func TestInspectTokenCache_NearExpiryExpiredClientSecret(t *testing.T) {
	baseNow := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	c := inspectValidTokenCache(baseNow)
	c.ExpiresAt = baseNow.Add(30 * time.Second).UTC().Format(time.RFC3339)
	c.RefreshToken = "valid-refresh-token"
	// ClientSecretExpiresAt in the past.
	c.ClientSecretExpiresAt = baseNow.Add(-time.Hour).Unix()
	status, err := InspectTokenCache(c, "https://example.com", "session-a", "cn-beijing", baseNow)
	if err == nil {
		t.Fatalf("expected error for near-expiry cache with expired client secret")
	}
	if status.Present {
		t.Fatalf("expected Present=false for near-expiry cache with expired client secret")
	}
	if !status.RefreshRequired {
		t.Fatalf("expected RefreshRequired=true for near-expiry cache with expired client secret")
	}
}

func TestInspectTokenCache_InvalidExpiration(t *testing.T) {
	baseNow := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	c := inspectValidTokenCache(baseNow)
	c.ExpiresAt = "not-a-date"
	status, err := InspectTokenCache(c, "https://example.com", "session-a", "cn-beijing", baseNow)
	if err == nil {
		t.Fatalf("expected error for invalid expiration")
	}
	if status.Present {
		t.Fatalf("expected Present=false for invalid expiration")
	}
	if !status.RefreshRequired {
		t.Fatalf("expected RefreshRequired=true for invalid expiration")
	}
}

func TestInspectTokenCache_SchemaFailures(t *testing.T) {
	baseNow := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	base := inspectValidTokenCache(baseNow)
	cases := []struct {
		name string
		mut  func(*TokenCache)
	}{
		{"nil", func(c *TokenCache) { *c = TokenCache{}; c.StartURL = "https://example.com" }},
		{"missing_start_url", func(c *TokenCache) { c.StartURL = "" }},
		{"mismatched_start_url", func(c *TokenCache) { c.StartURL = "https://other.com" }},
		{"missing_session", func(c *TokenCache) { c.SessionName = "" }},
		{"mismatched_session", func(c *TokenCache) { c.SessionName = "other" }},
		{"missing_region", func(c *TokenCache) { c.Region = "" }},
		{"mismatched_region", func(c *TokenCache) { c.Region = "us-east-1" }},
		{"missing_access_token", func(c *TokenCache) { c.AccessToken = "" }},
		{"missing_client_id", func(c *TokenCache) { c.ClientID = "" }},
		{"missing_client_secret", func(c *TokenCache) { c.ClientSecret = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := *base
			tc.mut(&c)
			status, err := InspectTokenCache(&c, "https://example.com", "session-a", "cn-beijing", baseNow)
			if err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
			if status.Present {
				t.Fatalf("expected Present=false for %s", tc.name)
			}
			if !status.RefreshRequired {
				t.Fatalf("expected RefreshRequired=true for %s", tc.name)
			}
		})
	}
}

func TestInspectTokenCache_DoesNotExposeSecrets(t *testing.T) {
	// TokenCacheStatus must not contain any credential fields.
	baseNow := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	c := inspectValidTokenCache(baseNow)
	status, _ := InspectTokenCache(c, "https://example.com", "session-a", "cn-beijing", baseNow)
	if status.ExpiresAt.IsZero() {
		t.Fatalf("expected non-zero ExpiresAt")
	}
	// The status struct only has Present, ExpiresAt, RefreshRequired — no
	// AccessToken, ClientID, or ClientSecret fields. This is a compile-time
	// guarantee via the struct definition; the test asserts the non-secret
	// fields are populated.
	_ = status.Present
	_ = status.RefreshRequired
}
