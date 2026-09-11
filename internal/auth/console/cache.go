package console

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"strings"
)

// CacheFilename returns the upstream-compatible cache file name for the given
// login session: the lowercase hex SHA-1 digest of the login session followed
// by ".json".
//
// SHA-1 is used here only to hide the login session identity in a file name,
// not as a cryptographic security primitive. This matches the upstream
// volcengine-cli cache naming convention so that caches written by either tool
// are mutually readable.
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
