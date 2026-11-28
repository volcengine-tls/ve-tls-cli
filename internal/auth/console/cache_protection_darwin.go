//go:build cgo

package console

/*
#cgo LDFLAGS: -framework CoreFoundation -framework Security
#include <CoreFoundation/CoreFoundation.h>
#include <Security/Security.h>
#include <stdlib.h>

static OSStatus consoleCacheKey(const char *path, const char *account, const UInt8 *key, CFIndex keyLength, CFTypeRef *result) {
	SecKeychainRef keychain = NULL;
	#pragma clang diagnostic push
	#pragma clang diagnostic ignored "-Wdeprecated-declarations"
	OSStatus status = SecKeychainOpen(path, &keychain);
	#pragma clang diagnostic pop
	if (status != errSecSuccess) {
		return status;
	}
	CFStringRef name = CFStringCreateWithCString(NULL, account, kCFStringEncodingUTF8);
	CFMutableDictionaryRef query = CFDictionaryCreateMutable(NULL, 0,
		&kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	CFDictionarySetValue(query, kSecClass, kSecClassGenericPassword);
	CFDictionarySetValue(query, kSecAttrService, CFSTR("volclog.console-login"));
	CFDictionarySetValue(query, kSecAttrAccount, name);
	// The file-based Keychain supports the non-interactive SecItem option.
	#pragma clang diagnostic push
	#pragma clang diagnostic ignored "-Wdeprecated-declarations"
	CFDictionarySetValue(query, kSecUseAuthenticationUI, kSecUseAuthenticationUIFail);
	#pragma clang diagnostic pop
	CFRelease(name);

	if (key == NULL) {
		const void *items[] = {keychain};
		CFArrayRef search = CFArrayCreate(NULL, items, 1, &kCFTypeArrayCallBacks);
		CFDictionarySetValue(query, kSecMatchSearchList, search);
		CFRelease(search);
		CFDictionarySetValue(query, kSecReturnData, kCFBooleanTrue);
		status = SecItemCopyMatching(query, result);
	} else {
		CFDictionarySetValue(query, kSecUseKeychain, keychain);
		CFDataRef data = CFDataCreate(NULL, key, keyLength);
		CFDictionarySetValue(query, kSecValueData, data);
		CFRelease(data);
		status = SecItemAdd(query, NULL);
	}
	CFRelease(query);
	CFRelease(keychain);
	return status;
}
*/
import "C"

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"unsafe"

	"github.com/volcengine-tls/ve-tls-cli/internal/securestore"
)

func newCacheProtection(dir string, _ *securestore.Store) cacheProtection {
	account := securestore.DigestKey(dir)
	return aesCacheProtection{key: func(_ context.Context, create bool) ([]byte, error) {
		// Resolve the login keychain from the macOS user record so a shell's
		// HOME override cannot send Security.framework into keychain setup.
		owner, err := user.Current()
		if err != nil {
			return nil, errors.Join(errCacheKeyUnavailable, err)
		}
		keychainPath := filepath.Join(owner.HomeDir, "Library", "Keychains", "login.keychain-db")
		if _, err := os.Stat(keychainPath); err != nil {
			return nil, errors.Join(errCacheKeyUnavailable, err)
		}
		path := C.CString(keychainPath)
		defer C.free(unsafe.Pointer(path))
		name := C.CString(account)
		defer C.free(unsafe.Pointer(name))

		var result C.CFTypeRef
		status := C.consoleCacheKey(path, name, nil, 0, &result)
		if status == C.errSecItemNotFound && create {
			key := make([]byte, cacheKeySize)
			if _, err := rand.Read(key); err != nil {
				return nil, err
			}
			status = C.consoleCacheKey(path, name, (*C.UInt8)(unsafe.Pointer(&key[0])), cacheKeySize, nil)
			if status == C.errSecSuccess {
				return key, nil
			}
			clear(key)
			if status == C.errSecDuplicateItem {
				status = C.consoleCacheKey(path, name, nil, 0, &result)
			}
		}
		switch status {
		case C.errSecItemNotFound:
			return nil, errCacheKeyMissing
		case C.errSecInteractionNotAllowed:
			return nil, errCacheKeyLocked
		case C.errSecSuccess:
			defer C.CFRelease(result)
			data := C.CFDataRef(result)
			if C.CFDataGetLength(data) != cacheKeySize {
				return nil, errCacheKeyUnavailable
			}
			return C.GoBytes(unsafe.Pointer(C.CFDataGetBytePtr(data)), cacheKeySize), nil
		default:
			return nil, fmt.Errorf("%w (Keychain status %d)", errCacheKeyUnavailable, status)
		}
	}}
}
