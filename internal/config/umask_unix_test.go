//go:build !windows

package config

import "golang.org/x/sys/unix"

func init() {
	// Config writes fail closed for existing parents broader than 0700. Make
	// TempDir-based fixtures deterministic across developer and CI umasks.
	unix.Umask(0o077)
}
