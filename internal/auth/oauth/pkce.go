// Package oauth provides small, dependency-free OAuth 2.0 primitives used by
// the Console and SSO authentication flows. It only relies on the Go standard
// library.
package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
)

// PKCE holds a generated code verifier and its S256 code challenge as defined
// by RFC 7636.
type PKCE struct {
	// Verifier is the high-entropy cryptographic random string.
	Verifier string
	// Challenge is the S256 transformation of Verifier.
	Challenge string
}

// GeneratePKCE generates a new PKCE pair. If entropy is nil,
// crypto/rand.Reader is used. The verifier uses 32 bytes of entropy encoded as
// unpadded base64url (43 characters), which satisfies the RFC 7636 length
// requirement of 43 to 128 characters.
func GeneratePKCE(entropy io.Reader) (PKCE, error) {
	if entropy == nil {
		entropy = rand.Reader
	}
	buf := make([]byte, 32)
	if _, err := io.ReadFull(entropy, buf); err != nil {
		return PKCE{}, fmt.Errorf("generate code_verifier: %w", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(buf)
	return PKCE{
		Verifier:  verifier,
		Challenge: CodeChallengeS256(verifier),
	}, nil
}

// CodeChallengeS256 computes the S256 code challenge for the given verifier:
// BASE64URL(SHA256(verifier)) without padding, per RFC 7636 Section 4.2.
func CodeChallengeS256(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

// GenerateState generates a UUID v4 string suitable for use as an OAuth state
// parameter. If entropy is nil, crypto/rand.Reader is used. The version and
// variant bits are set per RFC 4122.
func GenerateState(entropy io.Reader) (string, error) {
	if entropy == nil {
		entropy = rand.Reader
	}
	var uuid [16]byte
	if _, err := io.ReadFull(entropy, uuid[:]); err != nil {
		return "", fmt.Errorf("generate state: %w", err)
	}
	// Set version 4 (random) and variant 10 (RFC 4122) bits.
	uuid[6] = (uuid[6] & 0x0f) | 0x40
	uuid[8] = (uuid[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uuid[0:4],
		uuid[4:6],
		uuid[6:8],
		uuid[8:10],
		uuid[10:16],
	), nil
}
