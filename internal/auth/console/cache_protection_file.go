//go:build !darwin && !windows

package console

import (
	"context"
	"crypto/rand"
	"errors"
	"path/filepath"

	"github.com/volcengine-tls/ve-tls-cli/internal/securestore"
)

func newCacheProtection(dir string, store *securestore.Store) cacheProtection {
	path := filepath.Join(dir, "master.key")
	return aesCacheProtection{key: func(ctx context.Context, create bool) ([]byte, error) {
		key, err := readCacheKey(path)
		if !create || !errors.Is(err, errCacheKeyMissing) {
			return key, err
		}

		// Different sessions can be created concurrently in the same directory.
		// Keep the directory key stable across every process that writes them.
		err = store.WithLock(ctx, "console", "master.key", func() error {
			key, err = readCacheKey(path)
			if !errors.Is(err, errCacheKeyMissing) {
				return err
			}
			key = make([]byte, cacheKeySize)
			if _, err := rand.Read(key); err != nil {
				return err
			}
			return securestore.UpdateFile(path, 0o600, func([]byte) ([]byte, error) {
				return key, nil
			})
		})
		if err != nil {
			return nil, errors.Join(errCacheKeyUnavailable, err)
		}
		return key, nil
	}}
}

func readCacheKey(path string) ([]byte, error) {
	key, exists, err := readPrivateCacheFile(path)
	if err != nil {
		return nil, errors.Join(errCacheKeyUnavailable, err)
	}
	if !exists {
		return nil, errCacheKeyMissing
	}
	if len(key) != cacheKeySize {
		return nil, errCacheKeyUnavailable
	}
	return key, nil
}
