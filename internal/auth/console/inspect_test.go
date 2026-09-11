package console

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func validLoginCache(now time.Time) *LoginTokenCache {
	stsJSON, _ := json.Marshal(STSCredentials{
		AccessKeyID:     "AKLTvalid",
		SecretAccessKey: "valid-secret",
		SessionToken:    "valid-token",
	})
	return &LoginTokenCache{
		LoginSession: "session-a",
		AccessToken:  stsJSON,
		Scope:        Scope,
		ClientID:     ClientIDSameDevice,
		IssuedAt:     now.UTC().Format(time.RFC3339),
		ExpiresIn:    3600,
		TokenType:    "sts",
	}
}

func TestInspectLoginCache_ValidFuture(t *testing.T) {
	baseNow := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	c := validLoginCache(baseNow)
	status, err := InspectLoginCache(c, "session-a", baseNow)
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

func TestInspectLoginCache_ValidNearExpiry(t *testing.T) {
	baseNow := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	c := validLoginCache(baseNow)
	// Issued 3599s before baseNow, expires in 1s -> within the 60s RefreshBuffer.
	c.IssuedAt = baseNow.Add(-3599 * time.Second).UTC().Format(time.RFC3339)
	c.ExpiresIn = 3600
	c.RefreshToken = "valid-refresh-token"
	status, err := InspectLoginCache(c, "session-a", baseNow)
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

func TestInspectLoginCache_NearExpiryInvalidOldAccessTokenValidRefreshToken(t *testing.T) {
	baseNow := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	c := validLoginCache(baseNow)
	// Near-expiry: issued 3599s ago, expires in 1s.
	c.IssuedAt = baseNow.Add(-3599 * time.Second).UTC().Format(time.RFC3339)
	c.ExpiresIn = 3600
	c.RefreshToken = "valid-refresh-token"
	// Old AccessToken is a valid JSON value but not valid STS credentials —
	// must NOT block refresh on the near-expiry path.
	c.AccessToken = json.RawMessage(`"not-a-valid-sts-object"`)
	status, err := InspectLoginCache(c, "session-a", baseNow)
	if err != nil {
		t.Fatalf("expected no error for near-expiry cache with invalid old STS + valid refresh token, got %v", err)
	}
	if !status.Present {
		t.Fatalf("expected Present=true for near-expiry cache with valid refresh token")
	}
	if status.ExpiresAt.IsZero() {
		t.Fatalf("expected non-zero ExpiresAt for near-expiry cache with valid refresh token")
	}
	if !status.RefreshRequired {
		t.Fatalf("expected RefreshRequired=true for near-expiry cache")
	}
}

func TestInspectLoginCache_ExpiredInvalidOldAccessTokenValidRefreshToken(t *testing.T) {
	baseNow := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	c := validLoginCache(baseNow)
	// Fully expired: issued 2h ago, lifetime 1h -> expired 1h ago.
	c.IssuedAt = baseNow.Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	c.ExpiresIn = 3600
	c.RefreshToken = "valid-refresh-token"
	c.AccessToken = json.RawMessage(`{not valid json`)
	status, err := InspectLoginCache(c, "session-a", baseNow)
	if err != nil {
		t.Fatalf("expected no error for expired cache with invalid old STS + valid refresh token, got %v", err)
	}
	if !status.Present {
		t.Fatalf("expected Present=true for expired cache with valid refresh token")
	}
	if status.ExpiresAt.IsZero() {
		t.Fatalf("expected non-zero ExpiresAt for expired cache with valid refresh token")
	}
	if !status.RefreshRequired {
		t.Fatalf("expected RefreshRequired=true for expired cache")
	}
}

func TestInspectLoginCache_NearExpiryMissingRefreshToken(t *testing.T) {
	baseNow := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	c := validLoginCache(baseNow)
	// Near-expiry: issued 3599s ago, expires in 1s.
	c.IssuedAt = baseNow.Add(-3599 * time.Second).UTC().Format(time.RFC3339)
	c.ExpiresIn = 3600
	c.RefreshToken = ""
	status, err := InspectLoginCache(c, "session-a", baseNow)
	if err == nil {
		t.Fatalf("expected error for near-expiry cache missing refresh token")
	}
	if !errors.Is(err, errLoginCacheMissingRefreshToken) {
		t.Fatalf("expected errors.Is(err, errLoginCacheMissingRefreshToken), got %v", err)
	}
	if err.Error() != "console login cache missing refresh token" {
		t.Fatalf("expected fixed error text, got %q", err.Error())
	}
	if status.Present {
		t.Fatalf("expected Present=false for near-expiry cache missing refresh token")
	}
	if !status.ExpiresAt.IsZero() {
		t.Fatalf("expected zero ExpiresAt for near-expiry cache missing refresh token")
	}
	if !status.RefreshRequired {
		t.Fatalf("expected RefreshRequired=true for near-expiry cache missing refresh token")
	}
	// Error must be fixed, secret-free, and not contain any token material.
	if strings.Contains(err.Error(), "AKLTvalid") || strings.Contains(err.Error(), "valid-secret") || strings.Contains(err.Error(), "valid-token") {
		t.Fatalf("error must not leak credentials: %v", err)
	}
}

func TestInspectLoginCache_NearExpiryWhitespaceRefreshToken(t *testing.T) {
	baseNow := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	c := validLoginCache(baseNow)
	c.IssuedAt = baseNow.Add(-3599 * time.Second).UTC().Format(time.RFC3339)
	c.ExpiresIn = 3600
	c.RefreshToken = "   "
	status, err := InspectLoginCache(c, "session-a", baseNow)
	if err == nil {
		t.Fatalf("expected error for near-expiry cache with whitespace refresh token")
	}
	if !errors.Is(err, errLoginCacheMissingRefreshToken) {
		t.Fatalf("expected errors.Is(err, errLoginCacheMissingRefreshToken), got %v", err)
	}
	if err.Error() != "console login cache missing refresh token" {
		t.Fatalf("expected fixed error text, got %q", err.Error())
	}
	if status.Present {
		t.Fatalf("expected Present=false for near-expiry cache with whitespace refresh token")
	}
	if !status.ExpiresAt.IsZero() {
		t.Fatalf("expected zero ExpiresAt for near-expiry cache with whitespace refresh token")
	}
	if !status.RefreshRequired {
		t.Fatalf("expected RefreshRequired=true for near-expiry cache with whitespace refresh token")
	}
}

func TestInspectLoginCache_SessionMismatch(t *testing.T) {
	baseNow := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	c := validLoginCache(baseNow)
	status, err := InspectLoginCache(c, "other-session", baseNow)
	if err == nil {
		t.Fatalf("expected error for session mismatch")
	}
	if status.Present {
		t.Fatalf("expected Present=false for session mismatch")
	}
	if !status.RefreshRequired {
		t.Fatalf("expected RefreshRequired=true for session mismatch")
	}
}

func TestInspectLoginCache_EmptySession(t *testing.T) {
	baseNow := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	c := validLoginCache(baseNow)
	c.LoginSession = ""
	status, err := InspectLoginCache(c, "session-a", baseNow)
	if err == nil {
		t.Fatalf("expected error for empty session")
	}
	if status.Present {
		t.Fatalf("expected Present=false for empty session")
	}
	if !status.RefreshRequired {
		t.Fatalf("expected RefreshRequired=true for empty session")
	}
}

func TestInspectLoginCache_InvalidClientID(t *testing.T) {
	baseNow := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	c := validLoginCache(baseNow)
	c.ClientID = "not-a-frozen-client-id"
	status, err := InspectLoginCache(c, "session-a", baseNow)
	if err == nil {
		t.Fatalf("expected error for invalid client id")
	}
	if status.Present {
		t.Fatalf("expected Present=false for invalid client id")
	}
	if !status.RefreshRequired {
		t.Fatalf("expected RefreshRequired=true for invalid client id")
	}
}

func TestInspectLoginCache_InvalidScope(t *testing.T) {
	baseNow := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	c := validLoginCache(baseNow)
	c.Scope = "not-the-frozen-scope"
	status, err := InspectLoginCache(c, "session-a", baseNow)
	if err == nil {
		t.Fatalf("expected error for invalid scope")
	}
	if status.Present {
		t.Fatalf("expected Present=false for invalid scope")
	}
	if !status.RefreshRequired {
		t.Fatalf("expected RefreshRequired=true for invalid scope")
	}
}

func TestInspectLoginCache_InvalidEndpoint(t *testing.T) {
	baseNow := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	c := validLoginCache(baseNow)
	c.EndpointURL = "http://not-https.example.com"
	status, err := InspectLoginCache(c, "session-a", baseNow)
	if err == nil {
		t.Fatalf("expected error for invalid endpoint")
	}
	if status.Present {
		t.Fatalf("expected Present=false for invalid endpoint")
	}
	if !status.RefreshRequired {
		t.Fatalf("expected RefreshRequired=true for invalid endpoint")
	}
}

func TestInspectLoginCache_EmptyTokenType(t *testing.T) {
	baseNow := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	c := validLoginCache(baseNow)
	c.TokenType = ""
	status, err := InspectLoginCache(c, "session-a", baseNow)
	if err == nil {
		t.Fatalf("expected error for empty token type")
	}
	if status.Present {
		t.Fatalf("expected Present=false for empty token type")
	}
	if !status.RefreshRequired {
		t.Fatalf("expected RefreshRequired=true for empty token type")
	}
}

func TestInspectLoginCache_InvalidExpiration(t *testing.T) {
	baseNow := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	c := validLoginCache(baseNow)
	c.IssuedAt = "not-a-date"
	status, err := InspectLoginCache(c, "session-a", baseNow)
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

func TestInspectLoginCache_InvalidSTSCredentials(t *testing.T) {
	baseNow := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	c := validLoginCache(baseNow)
	// AccessToken is not valid STS JSON.
	c.AccessToken = json.RawMessage(`{"not":"sts"}`)
	status, err := InspectLoginCache(c, "session-a", baseNow)
	if err == nil {
		t.Fatalf("expected error for invalid STS credentials")
	}
	if status.Present {
		t.Fatalf("expected Present=false for invalid STS credentials")
	}
	if !status.RefreshRequired {
		t.Fatalf("expected RefreshRequired=true for invalid STS credentials")
	}
}

func TestInspectLoginCache_NilCache(t *testing.T) {
	baseNow := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	status, err := InspectLoginCache(nil, "session-a", baseNow)
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
