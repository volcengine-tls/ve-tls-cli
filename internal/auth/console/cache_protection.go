package console

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
)

const (
	cacheHeader  = "volclog-console-cache\x00\x01"
	cacheKeySize = 32
)

var (
	ErrLegacyCache         = errors.New("legacy plaintext console login cache")
	errCacheKeyMissing     = errors.New("console login cache key is missing")
	errCacheKeyLocked      = errors.New("console login Keychain requires user interaction")
	errCacheKeyUnavailable = errors.New("console login cache key is unavailable")
	errCacheInvalid        = errors.New("invalid encrypted console login cache")
)

// CacheRecovery describes a local cache failure without rendering secret data.
// An empty state means the caller should use its existing generic diagnostic.
func CacheRecovery(err error) (state, action string) {
	switch {
	case errors.Is(err, ErrLegacyCache):
		return "legacy_plaintext", "Run volclog login with this profile to upgrade the session"
	case errors.Is(err, errCacheKeyMissing):
		return "key_missing", "Run volclog logout for this profile, then volclog login"
	case errors.Is(err, errCacheKeyLocked):
		return "key_locked", "Unlock the macOS login Keychain and authorize this executable"
	case errors.Is(err, errCacheKeyUnavailable):
		return "key_unavailable", "Check the cache key permissions or unlock the system credential store"
	case errors.Is(err, errCacheInvalid):
		return "cache_invalid", "Run volclog logout for this profile, then volclog login"
	default:
		return "", ""
	}
}

type cacheProtection interface {
	prepare(context.Context) error
	seal(session string, data []byte) ([]byte, error)
	open(session string, data []byte) ([]byte, error)
}

// aesCacheProtection shares the envelope between the Linux file key and the
// macOS Keychain key. Each write authenticates the version and session ID.
type aesCacheProtection struct {
	key func(context.Context, bool) ([]byte, error)
}

func (p aesCacheProtection) prepare(ctx context.Context) error {
	key, err := p.key(ctx, true)
	clear(key)
	return err
}

func (p aesCacheProtection) seal(session string, data []byte) ([]byte, error) {
	aead, err := p.aead(true)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, data, []byte(cacheHeader+session)), nil
}

func (p aesCacheProtection) open(session string, data []byte) ([]byte, error) {
	aead, err := p.aead(false)
	if err != nil {
		return nil, err
	}
	size := aead.NonceSize()
	if len(data) < size+aead.Overhead() {
		return nil, errCacheInvalid
	}
	plain, err := aead.Open(nil, data[:size], data[size:], []byte(cacheHeader+session))
	if err != nil {
		return nil, errCacheInvalid
	}
	return plain, nil
}

func (p aesCacheProtection) aead(create bool) (cipher.AEAD, error) {
	key, err := p.key(context.Background(), create)
	if err != nil {
		return nil, err
	}
	defer clear(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errors.Join(errCacheKeyUnavailable, err)
	}
	return cipher.NewGCM(block)
}
