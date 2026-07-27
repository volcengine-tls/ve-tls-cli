package console

import (
	"encoding/base64"
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestParseSTSCredentialsAcceptsObjectAndJSONString(t *testing.T) {
	creds := STSCredentials{
		AccessKeyID:     "AKLT-test-access-key",
		SecretAccessKey: "test-secret-key",
		SessionToken:    "test-session-token",
	}
	objBytes, err := json.Marshal(creds)
	if err != nil {
		t.Fatalf("marshal creds: %v", err)
	}

	// Format 1: raw JSON object.
	got, err := ParseSTSCredentials(objBytes)
	if err != nil {
		t.Fatalf("ParseSTSCredentials(object) error: %v", err)
	}
	if *got != creds {
		t.Fatalf("object creds = %+v, want %+v", *got, creds)
	}

	// Format 2: JSON string whose content is the JSON object (upstream format).
	strBytes, err := json.Marshal(string(objBytes))
	if err != nil {
		t.Fatalf("marshal string: %v", err)
	}
	got, err = ParseSTSCredentials(strBytes)
	if err != nil {
		t.Fatalf("ParseSTSCredentials(string) error: %v", err)
	}
	if *got != creds {
		t.Fatalf("string creds = %+v, want %+v", *got, creds)
	}
}

func TestParseSTSCredentialsRequiresAllThreeFields(t *testing.T) {
	cases := []struct {
		name string
		json string
	}{
		{"missing access_key_id", `{"secret_access_key":"sk","session_token":"st"}`},
		{"missing secret_access_key", `{"access_key_id":"ak","session_token":"st"}`},
		{"missing session_token", `{"access_key_id":"ak","secret_access_key":"sk"}`},
		{"empty access_key_id", `{"access_key_id":"  ","secret_access_key":"sk","session_token":"st"}`},
		{"empty object", `{}`},
		{"not json", `not-json-at-all`},
		{"empty string", ``},
		{"whitespace only", `   `},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseSTSCredentials(json.RawMessage(tc.json))
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tc.json)
			}
			// Error must never echo the secret-bearing input.
			if strings.Contains(err.Error(), "sk") || strings.Contains(err.Error(), "st") {
				t.Fatalf("error leaked secret fields: %q", err.Error())
			}
		})
	}
}

func TestExtractLoginSessionUsesTRNClaim(t *testing.T) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	wantTRN := "trn:iam::2100000000:user/example"
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"trn":"` + wantTRN + `","sub":"user"}`))
	sig := base64.RawURLEncoding.EncodeToString([]byte("signature"))
	token := header + "." + payload + "." + sig

	got, err := ExtractLoginSession(token)
	if err != nil {
		t.Fatalf("ExtractLoginSession error: %v", err)
	}
	if got != wantTRN {
		t.Fatalf("trn = %q, want %q", got, wantTRN)
	}
}

func TestExtractLoginSessionRejectsMalformedJWT(t *testing.T) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	validPayload := base64.RawURLEncoding.EncodeToString([]byte(`{"trn":"trn:iam::1:user/x"}`))

	cases := []struct {
		name  string
		token string
	}{
		{"empty", ""},
		{"only header", header},
		{"two segments", header + "." + validPayload},
		{"four segments", header + "." + validPayload + ".sig.extra"},
		{"payload not base64url", header + ".!!!not-base64!!!.sig"},
		{"payload not json", header + "." + base64.RawURLEncoding.EncodeToString([]byte("not-json")) + ".sig"},
		{"missing trn claim", header + "." + base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"user"}`)) + ".sig"},
		{"trn not a string", header + "." + base64.RawURLEncoding.EncodeToString([]byte(`{"trn":123}`)) + ".sig"},
		{"trn empty string", header + "." + base64.RawURLEncoding.EncodeToString([]byte(`{"trn":"  "}`)) + ".sig"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ExtractLoginSession(tc.token)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tc.name)
			}
			// Error must never echo the raw token.
			if tc.token != "" && strings.Contains(err.Error(), tc.token) {
				t.Fatalf("error echoed raw token: %q", err.Error())
			}
		})
	}
}

func TestTokenCacheExpirationUsesIssuedAtAndExpiresIn(t *testing.T) {
	issued := "2026-07-24T10:00:00Z"
	exp, err := CacheExpiration(issued, 900)
	if err != nil {
		t.Fatalf("CacheExpiration error: %v", err)
	}
	want, err := time.Parse(time.RFC3339, issued)
	if err != nil {
		t.Fatalf("parse issued: %v", err)
	}
	want = want.Add(900 * time.Second)
	if !exp.Equal(want) {
		t.Fatalf("expiration = %v, want %v", exp, want)
	}

	// RFC3339Nano must also be accepted.
	nano := "2026-07-24T10:00:00.123456789Z"
	expNano, err := CacheExpiration(nano, 60)
	if err != nil {
		t.Fatalf("CacheExpiration(nano) error: %v", err)
	}
	wantNano, err := time.Parse(time.RFC3339Nano, nano)
	if err != nil {
		t.Fatalf("parse nano: %v", err)
	}
	wantNano = wantNano.Add(60 * time.Second)
	if !expNano.Equal(wantNano) {
		t.Fatalf("nano expiration = %v, want %v", expNano, wantNano)
	}

	// Negative and zero ExpiresIn are rejected.
	if _, err := CacheExpiration(issued, 0); err == nil {
		t.Fatal("expected error for expires_in=0")
	}
	if _, err := CacheExpiration(issued, -10); err == nil {
		t.Fatal("expected error for expires_in=-10")
	}

	// Invalid IssuedAt is rejected.
	if _, err := CacheExpiration("not-a-timestamp", 900); err == nil {
		t.Fatal("expected error for invalid issued_at")
	}
	if _, err := CacheExpiration("", 900); err == nil {
		t.Fatal("expected error for empty issued_at")
	}
}

func TestParseSTSCredentialsTrimsFields(t *testing.T) {
	// Fields with surrounding whitespace must be trimmed before returning.
	input := `{"access_key_id":"  AKLT-trim  ","secret_access_key":"  sk-trim  ","session_token":"  st-trim  "}`
	got, err := ParseSTSCredentials(json.RawMessage(input))
	if err != nil {
		t.Fatalf("ParseSTSCredentials: %v", err)
	}
	if got.AccessKeyID != "AKLT-trim" {
		t.Fatalf("access_key_id = %q, want AKLT-trim", got.AccessKeyID)
	}
	if got.SecretAccessKey != "sk-trim" {
		t.Fatalf("secret_access_key = %q, want sk-trim", got.SecretAccessKey)
	}
	if got.SessionToken != "st-trim" {
		t.Fatalf("session_token = %q, want st-trim", got.SessionToken)
	}
}

func TestParseSTSCredentialsTrimsOuterWhitespace(t *testing.T) {
	// Leading/trailing whitespace around the JSON must not break parsing.
	input := `  {"access_key_id":"ak","secret_access_key":"sk","session_token":"st"}  `
	if _, err := ParseSTSCredentials(json.RawMessage(input)); err != nil {
		t.Fatalf("ParseSTSCredentials with outer whitespace: %v", err)
	}
}

func TestExtractLoginSessionTrimsTRN(t *testing.T) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	wantTRN := "trn:iam::1:user/x"
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"trn":"  ` + wantTRN + `  "}`))
	sig := base64.RawURLEncoding.EncodeToString([]byte("signature"))
	token := header + "." + payload + "." + sig

	got, err := ExtractLoginSession(token)
	if err != nil {
		t.Fatalf("ExtractLoginSession: %v", err)
	}
	if got != wantTRN {
		t.Fatalf("trn = %q, want %q (not trimmed)", got, wantTRN)
	}
}

func TestExtractLoginSessionRejectsEmptySegments(t *testing.T) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"trn":"trn:iam::1:user/x"}`))

	cases := []struct {
		name  string
		token string
	}{
		{"empty header", "." + payload + ".sig"},
		{"empty payload", header + "..sig"},
		{"empty signature", header + "." + payload + "."},
		{"all empty", ".."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ExtractLoginSession(tc.token)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tc.name)
			}
		})
	}
}

func TestCacheExpirationRejectsOverflow(t *testing.T) {
	// Guard so the test remains portable across 32-bit and 64-bit platforms:
	// we only test values that exceed int64(time.Second) capacity.
	if strconv.IntSize < 64 {
		t.Skip("overflow boundary test requires 64-bit int")
	}
	issued := "2026-07-24T10:00:00Z"
	// expiresIn * time.Second would overflow int64.
	huge := int(int64(math.MaxInt64)/int64(time.Second) + 1)
	_, err := CacheExpiration(issued, huge)
	if err == nil {
		t.Fatal("expected error for overflowing expires_in, got nil")
	}
}

func TestExtractLoginSessionValidatesHeaderAndSignatureBase64(t *testing.T) {
	validPayload := base64.RawURLEncoding.EncodeToString([]byte(`{"trn":"trn:iam::1:user/x"}`))
	validHeader := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))

	cases := []struct {
		name  string
		token string
	}{
		{"header not base64url", "!!!not-base64!!!." + validPayload + ".sig"},
		{"header with padding", base64.StdEncoding.EncodeToString([]byte(`{"alg":"none"}`)) + "." + validPayload + ".sig"},
		{"signature not base64url", validHeader + "." + validPayload + ".!!!bad-sig!!!"},
		{"signature with padding", validHeader + "." + validPayload + "." + base64.StdEncoding.EncodeToString([]byte("sig2"))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ExtractLoginSession(tc.token)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tc.name)
			}
			// Error must never echo the raw token.
			if strings.Contains(err.Error(), tc.token) {
				t.Fatalf("error echoed raw token: %q", err.Error())
			}
		})
	}
}
