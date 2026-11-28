//go:build darwin && !cgo

package console

import (
	"context"
	"fmt"

	"github.com/volcengine-tls/ve-tls-cli/internal/securestore"
)

func newCacheProtection(_ string, _ *securestore.Store) cacheProtection {
	return aesCacheProtection{key: func(context.Context, bool) ([]byte, error) {
		return nil, fmt.Errorf("%w: use a macOS build with Keychain support (CGO_ENABLED=1)", errCacheKeyUnavailable)
	}}
}
