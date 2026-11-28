package console

import (
	"bytes"
	"context"
	"errors"
	"unsafe"

	"github.com/volcengine-tls/ve-tls-cli/internal/securestore"
	"golang.org/x/sys/windows"
)

func newCacheProtection(_ string, _ *securestore.Store) cacheProtection {
	return dpapiCacheProtection{}
}

// DPAPI binds the cache to the current Windows user and authenticates the
// version and session ID via optional entropy. It stores no separate key file.
type dpapiCacheProtection struct{}

func (p dpapiCacheProtection) prepare(context.Context) error {
	_, err := p.seal("", []byte("volclog"))
	return err
}

func (dpapiCacheProtection) seal(session string, data []byte) ([]byte, error) {
	input := windows.DataBlob{Size: uint32(len(data)), Data: unsafe.SliceData(data)}
	aad := []byte(cacheHeader + session)
	entropy := windows.DataBlob{Size: uint32(len(aad)), Data: unsafe.SliceData(aad)}
	var output windows.DataBlob
	if err := windows.CryptProtectData(&input, nil, &entropy, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &output); err != nil {
		return nil, errors.Join(errCacheKeyUnavailable, err)
	}
	return copyDPAPIResult(output), nil
}

func (dpapiCacheProtection) open(session string, data []byte) ([]byte, error) {
	input := windows.DataBlob{Size: uint32(len(data)), Data: unsafe.SliceData(data)}
	aad := []byte(cacheHeader + session)
	entropy := windows.DataBlob{Size: uint32(len(aad)), Data: unsafe.SliceData(aad)}
	var output windows.DataBlob
	if err := windows.CryptUnprotectData(&input, nil, &entropy, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &output); err != nil {
		return nil, errors.Join(errCacheInvalid, err)
	}
	return copyDPAPIResult(output), nil
}

func copyDPAPIResult(blob windows.DataBlob) []byte {
	data := unsafe.Slice(blob.Data, blob.Size)
	result := bytes.Clone(data)
	clear(data)
	_, _ = windows.LocalFree(windows.Handle(unsafe.Pointer(blob.Data)))
	return result
}
