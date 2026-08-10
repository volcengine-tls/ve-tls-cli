//go:build !windows

package securestore

import "golang.org/x/sys/unix"

func init() {
	// Secure-store roots must be 0700. Go's TempDir mode otherwise depends on
	// the caller's umask and makes identical tests environment-dependent.
	unix.Umask(0o077)
}
