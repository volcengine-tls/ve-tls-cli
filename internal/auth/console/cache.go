package console

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"strings"
)

// CacheFilename returns the legacy plaintext file name for the given session.
// Its SHA-1 digest is used for naming, including by the session lock shared
// with older volclog releases.
//
// An empty or whitespace-only login session is rejected rather than silently
// hashed, to avoid producing a well-known cache key for missing sessions.
func CacheFilename(loginSession string) (string, error) {
	if strings.TrimSpace(loginSession) == "" {
		return "", errors.New("login session is empty")
	}
	sum := sha1.Sum([]byte(loginSession))
	return hex.EncodeToString(sum[:]) + ".json", nil
}
