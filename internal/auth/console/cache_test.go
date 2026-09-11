package console

import (
	"crypto/sha1"
	"encoding/hex"
	"strings"
	"testing"
)

func TestTokenCacheFilenameUsesLowercaseHexSHA1(t *testing.T) {
	const session = "trn:iam::2100000000:user/example"
	sum := sha1.Sum([]byte(session))
	want := hex.EncodeToString(sum[:]) + ".json"

	got, err := CacheFilename(session)
	if err != nil {
		t.Fatalf("CacheFilename error: %v", err)
	}
	if got != want {
		t.Fatalf("CacheFilename = %q, want %q", got, want)
	}
	// Must be all lowercase hex.
	if got != strings.ToLower(got) {
		t.Fatalf("CacheFilename %q is not lowercase", got)
	}
	if !strings.HasSuffix(got, ".json") {
		t.Fatalf("CacheFilename %q does not end with .json", got)
	}
}

func TestTokenCacheFilenameRejectsEmptyAndWhitespace(t *testing.T) {
	cases := []string{"", "   ", "\t", "\n"}
	for _, s := range cases {
		_, err := CacheFilename(s)
		if err == nil {
			t.Fatalf("expected error for empty/whitespace session %q, got nil", s)
		}
	}
}

func TestTokenCacheFilenameIsDeterministic(t *testing.T) {
	const session = "my-login-session"
	a, err := CacheFilename(session)
	if err != nil {
		t.Fatalf("first CacheFilename: %v", err)
	}
	b, err := CacheFilename(session)
	if err != nil {
		t.Fatalf("second CacheFilename: %v", err)
	}
	if a != b {
		t.Fatalf("CacheFilename not deterministic: %q vs %q", a, b)
	}
}
