//go:build !windows

package sso

import "golang.org/x/sys/unix"

func init() {
	// Go's testing.TempDir uses 0777 masked by the process umask. SSO caches
	// intentionally reject existing directories broader than 0700.
	unix.Umask(0o077)
}
