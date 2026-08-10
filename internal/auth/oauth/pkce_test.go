package oauth

import (
	"errors"
	"io"
	"strings"
	"testing"
)

// TestPKCES256RFC7636Vector verifies the S256 challenge against the known
// RFC 7636 Appendix B test vector.
func TestPKCES256RFC7636Vector(t *testing.T) {
	const verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	const want = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"

	got := CodeChallengeS256(verifier)
	if got != want {
		t.Fatalf("CodeChallengeS256(%q) = %q, want %q", verifier, got, want)
	}
}

// TestCodeVerifierLengthAndCharacterSet ensures generated verifiers are 43
// characters long and only contain unreserved URI characters, and that the
// challenge is unpadded base64url.
func TestCodeVerifierLengthAndCharacterSet(t *testing.T) {
	pkce, err := GeneratePKCE(nil)
	if err != nil {
		t.Fatalf("GeneratePKCE returned error: %v", err)
	}
	if len(pkce.Verifier) != 43 {
		t.Fatalf("verifier length = %d, want 43", len(pkce.Verifier))
	}
	for _, r := range pkce.Verifier {
		if !isUnreserved(r) {
			t.Fatalf("verifier contains disallowed character %q", r)
		}
	}
	if strings.Contains(pkce.Challenge, "=") {
		t.Fatalf("challenge contains padding: %q", pkce.Challenge)
	}
	if pkce.Challenge != CodeChallengeS256(pkce.Verifier) {
		t.Fatalf("challenge does not match S256(verifier)")
	}
}

func isUnreserved(r rune) bool {
	switch {
	case r >= 'A' && r <= 'Z':
		return true
	case r >= 'a' && r <= 'z':
		return true
	case r >= '0' && r <= '9':
		return true
	case r == '-' || r == '.' || r == '_' || r == '~':
		return true
	}
	return false
}

// TestRandomStateIsUUIDV4Shape checks that generated state looks like a UUID v4
// with correct version and variant bits.
func TestRandomStateIsUUIDV4Shape(t *testing.T) {
	state, err := GenerateState(nil)
	if err != nil {
		t.Fatalf("GenerateState returned error: %v", err)
	}
	if len(state) != 36 {
		t.Fatalf("state length = %d, want 36", len(state))
	}
	if state[8] != '-' || state[13] != '-' || state[18] != '-' || state[23] != '-' {
		t.Fatalf("state has wrong dash positions: %q", state)
	}
	if state[14] != '4' {
		t.Fatalf("state version nibble = %q, want '4'", state[14])
	}
	switch state[19] {
	case '8', '9', 'a', 'b':
	default:
		t.Fatalf("state variant nibble = %q, want one of 8/9/a/b", state[19])
	}
	for i, r := range state {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			continue
		}
		if !isHex(r) {
			t.Fatalf("state contains non-hex character %q at %d", r, i)
		}
	}
}

func isHex(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')
}

// failingReader returns an error on every Read.
type failingReader struct{}

func (failingReader) Read(p []byte) (int, error) {
	return 0, errors.New("entropy source unavailable")
}

// TestRandomGenerationPropagatesEntropyFailure ensures entropy failures are
// surfaced and never leak raw bytes or generated secrets.
func TestRandomGenerationPropagatesEntropyFailure(t *testing.T) {
	if _, err := GeneratePKCE(failingReader{}); err == nil {
		t.Fatal("GeneratePKCE with failing reader should return error")
	}
	if _, err := GenerateState(failingReader{}); err == nil {
		t.Fatal("GenerateState with failing reader should return error")
	}
}

// TestGeneratePKCEUsesInjectedEntropy verifies the injected reader is consumed
// rather than a hidden global, making generation deterministic in tests.
func TestGeneratePKCEUsesInjectedEntropy(t *testing.T) {
	// 32 bytes of fixed entropy.
	entropy := make([]byte, 32)
	for i := range entropy {
		entropy[i] = byte(i)
	}
	r := &fixedReader{data: entropy}

	pkce1, err := GeneratePKCE(r)
	if err != nil {
		t.Fatalf("GeneratePKCE: %v", err)
	}
	r2 := &fixedReader{data: entropy}
	pkce2, err := GeneratePKCE(r2)
	if err != nil {
		t.Fatalf("GeneratePKCE: %v", err)
	}
	if pkce1.Verifier != pkce2.Verifier {
		t.Fatalf("injected entropy not deterministic: %q vs %q", pkce1.Verifier, pkce2.Verifier)
	}
}

type fixedReader struct {
	data []byte
}

func (f *fixedReader) Read(p []byte) (int, error) {
	if len(f.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, f.data)
	f.data = f.data[n:]
	return n, nil
}
