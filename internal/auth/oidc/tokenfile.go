// Package oidc implements a cached OIDC federation credential provider that
// reads a raw OIDC token from a file (typically a Kubernetes projected
// service-account token) and exchanges it for temporary STS credentials via
// AssumeRoleWithOIDC. Credentials are kept in process memory only; no disk
// state, environment variables, or background goroutines are used.
package oidc

import (
	"bytes"
	"io"
	"os"
	"path/filepath"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth"
)

// maxTokenSize is the maximum accepted token file size. One extra byte is read
// so that files exceeding the limit can be detected and rejected.
const maxTokenSize = 64 * 1024

// evalFn resolves all symlinks in a path. It is a parameter to
// readTokenFileWithOps so tests can deterministically simulate a target being
// replaced between the eval and the final open.
type evalFn func(string) (string, error)

// openFn opens the resolved final target and returns an *os.File. It is
// implemented per-platform (see tokenfile_unix.go / tokenfile_windows.go) and
// must reject symlinks swapped in after eval.
type openFn func(string) (*os.File, error)

// readTokenFile reads the OIDC token at path using the production platform
// opener. The raw bytes are returned verbatim, including any trailing newline.
// The file mode is never changed. Kubernetes projected symlinks are re-resolved
// on every call so rotated tokens are picked up.
func readTokenFile(path string) ([]byte, error) {
	return readTokenFileWithOps(path, filepath.EvalSymlinks, openTokenFile)
}

// InspectTokenFile verifies that the OIDC token file at path is resolvable,
// openable with the platform secure opener (O_NOFOLLOW/O_NONBLOCK, Windows
// reparse point), and is a regular file. It never reads the file contents,
// never returns token material, and never changes permissions. It is intended
// for offline readiness checks where the raw token must not be exposed.
func InspectTokenFile(path string) error {
	return inspectTokenFileWithOps(path, filepath.EvalSymlinks, openTokenFile)
}

// inspectTokenFileWithOps performs the same secure resolve/open/fstat sequence
// as readTokenFileWithOps but stops after confirming a regular file. It never
// reads contents. Tests pass custom ops to simulate TOCTOU races.
func inspectTokenFileWithOps(path string, eval evalFn, open openFn) error {
	f, err := secureOpenTokenFile(path, eval, open)
	if err != nil {
		return err
	}
	// secureOpenTokenFile already verified the descriptor is a regular file.
	// Close immediately; nothing is read.
	return f.Close()
}

// secureOpenTokenFile resolves path with eval, opens the resolved target with
// open (which must reject late symlinks), stats the descriptor, and requires a
// regular file. It returns the open *os.File on success (caller must Close) or
// a safe auth.Error on failure. It is shared by readTokenFileWithOps and
// inspectTokenFileWithOps so both use the identical security boundary.
func secureOpenTokenFile(path string, eval evalFn, open openFn) (*os.File, error) {
	resolved, err := eval(path)
	if err != nil {
		return nil, &auth.Error{
			Kind:        auth.ProtocolError,
			Description: "failed to resolve OIDC token file path",
			Cause:       err,
		}
	}

	f, err := open(resolved)
	if err != nil {
		return nil, &auth.Error{
			Kind:        auth.ProtocolError,
			Description: "failed to open OIDC token file",
			Cause:       err,
		}
	}
	if f == nil {
		// Defensive: a misbehaving openFn could return (nil, nil). Fail closed
		// rather than panic on f.Close()/f.Stat().
		return nil, &auth.Error{Kind: auth.ProtocolError, Description: "failed to open OIDC token file"}
	}

	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, &auth.Error{
			Kind:        auth.ProtocolError,
			Description: "failed to stat OIDC token file",
			Cause:       err,
		}
	}
	if !fi.Mode().IsRegular() {
		_ = f.Close()
		return nil, &auth.Error{Kind: auth.ProtocolError, Description: "OIDC token file is not a regular file"}
	}
	return f, nil
}

// readTokenFileWithOps reads the OIDC token at path using the provided eval and
// open functions. Tests pass custom ops to simulate TOCTOU races without
// touching any package-level state.
//
// Sequence:
//  1. evalFn resolves the final target.
//  2. openFn opens the resolved target (must not follow late symlinks).
//  3. Stat the descriptor and require a regular file.
//  4. Read at most 64KiB+1 bytes; reject overflow.
//  5. Reject zero length and any NUL byte.
func readTokenFileWithOps(path string, eval evalFn, open openFn) ([]byte, error) {
	f, err := secureOpenTokenFile(path, eval, open)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	body, err := io.ReadAll(io.LimitReader(f, maxTokenSize+1))
	if err != nil {
		return nil, &auth.Error{
			Kind:        auth.ProtocolError,
			Description: "failed to read OIDC token file",
			Cause:       err,
		}
	}
	if len(body) > maxTokenSize {
		return nil, &auth.Error{Kind: auth.ProtocolError, Description: "OIDC token file exceeds maximum size"}
	}
	if len(body) == 0 {
		return nil, &auth.Error{Kind: auth.ProtocolError, Description: "OIDC token file is empty"}
	}
	if bytes.IndexByte(body, 0) >= 0 {
		return nil, &auth.Error{Kind: auth.ProtocolError, Description: "OIDC token file contains NUL byte"}
	}
	return body, nil
}
