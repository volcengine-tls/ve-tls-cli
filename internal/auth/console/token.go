package console

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// ParseSTSCredentials parses the access_token field of a token response into
// STSCredentials. The accessToken may be either a JSON object
//
//	{"access_key_id":"...","secret_access_key":"...","session_token":"..."}
//
// or a JSON string whose content is that object (the format historically
// returned by the upstream token endpoint):
//
//	"{\"access_key_id\":\"...\",...}"
//
// All three fields (AccessKeyID, SecretAccessKey, SessionToken) must be
// non-empty after trimming whitespace, and the returned values are trimmed.
// The returned error never includes the raw access_token, secret access key,
// session token, or raw JSON.
func ParseSTSCredentials(accessToken json.RawMessage) (*STSCredentials, error) {
	trimmed := bytes.TrimSpace(accessToken)
	if len(trimmed) == 0 {
		return nil, errors.New("access_token is empty")
	}

	raw := trimmed
	// If the outer value is a JSON string, unmarshal it once to obtain the
	// inner JSON object bytes. This handles the upstream "string containing
	// JSON" format.
	var innerString string
	if err := json.Unmarshal(raw, &innerString); err == nil {
		innerTrimmed := strings.TrimSpace(innerString)
		if innerTrimmed != "" {
			raw = []byte(innerTrimmed)
		}
	}

	var creds STSCredentials
	if err := json.Unmarshal(raw, &creds); err != nil {
		return nil, errors.New("failed to parse STS credentials from access_token")
	}

	creds.AccessKeyID = strings.TrimSpace(creds.AccessKeyID)
	creds.SecretAccessKey = strings.TrimSpace(creds.SecretAccessKey)
	creds.SessionToken = strings.TrimSpace(creds.SessionToken)

	if creds.AccessKeyID == "" {
		return nil, errors.New("parsed STS credentials missing access_key_id")
	}
	if creds.SecretAccessKey == "" {
		return nil, errors.New("parsed STS credentials missing secret_access_key")
	}
	if creds.SessionToken == "" {
		return nil, errors.New("parsed STS credentials missing session_token")
	}
	return &creds, nil
}

// ExtractLoginSession extracts the login session identifier from the `trn`
// claim of the given ID token (a JWT).
//
// The token is parsed without signature verification: it is received directly
// from the trusted HTTPS token endpoint as part of the token exchange
// response, so its integrity is protected by TLS. Only the payload claims are
// inspected; no authorization or permission decisions are made here.
//
// The JWT must use Raw URL Base64 encoding (no padding) for all three of its
// segments (header, payload, signature), have exactly three segments, and the
// payload must be valid JSON containing a non-empty string `trn` claim. The
// signature is syntax-checked but not cryptographically verified. Malformed
// tokens are rejected with an error that never includes the raw token.
func ExtractLoginSession(idToken string) (string, error) {
	if strings.TrimSpace(idToken) == "" {
		return "", errors.New("id_token is empty")
	}

	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return "", errors.New("id_token is not a valid JWT: expected 3 segments")
	}
	// Each of the three segments (header, payload, signature) must be nonempty.
	for i, p := range parts {
		if p == "" {
			return "", fmt.Errorf("id_token segment %d is empty", i)
		}
	}

	// All three segments must use Raw URL Base64 encoding (no padding). We
	// validate the syntax of the header and signature here; the payload is
	// validated below when it is decoded. The signature is not verified
	// cryptographically.
	if _, err := base64.RawURLEncoding.DecodeString(parts[0]); err != nil {
		return "", errors.New("id_token header is not valid base64url")
	}
	if _, err := base64.RawURLEncoding.DecodeString(parts[2]); err != nil {
		return "", errors.New("id_token signature is not valid base64url")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", errors.New("id_token payload is not valid base64url")
	}

	var claims map[string]json.RawMessage
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", errors.New("id_token payload is not valid JSON")
	}

	trnRaw, ok := claims["trn"]
	if !ok || len(bytes.TrimSpace(trnRaw)) == 0 {
		return "", errors.New("id_token missing trn claim")
	}

	var trn string
	if err := json.Unmarshal(trnRaw, &trn); err != nil {
		return "", errors.New("id_token trn claim is not a string")
	}
	trn = strings.TrimSpace(trn)
	if trn == "" {
		return "", errors.New("id_token trn claim is empty")
	}
	return trn, nil
}

// CacheExpiration computes the absolute expiration time of a cached token from
// its IssuedAt (RFC3339 or RFC3339Nano) and ExpiresIn (seconds) fields.
//
// ExpiresIn must be strictly positive and small enough that
// expiresIn * time.Second fits in a time.Duration (int64 nanoseconds). Values
// that would overflow return an error rather than being silently clamped to a
// ~292 year maximum.
func CacheExpiration(issuedAt string, expiresIn int) (time.Time, error) {
	if strings.TrimSpace(issuedAt) == "" {
		return time.Time{}, errors.New("issued_at is empty")
	}
	if expiresIn <= 0 {
		return time.Time{}, fmt.Errorf("expires_in must be positive, got %d", expiresIn)
	}

	issued, err := time.Parse(time.RFC3339Nano, issuedAt)
	if err != nil {
		issued, err = time.Parse(time.RFC3339, issuedAt)
		if err != nil {
			return time.Time{}, errors.New("issued_at is not a valid RFC3339 timestamp")
		}
	}

	// Guard against overflow when converting seconds to a duration. The
	// maximum representable Duration is roughly 292 years; any ExpiresIn
	// larger than that is rejected rather than silently clamped.
	if int64(expiresIn) > int64(math.MaxInt64)/int64(time.Second) {
		return time.Time{}, fmt.Errorf("expires_in %d would overflow duration", expiresIn)
	}
	return issued.Add(time.Duration(expiresIn) * time.Second), nil
}
