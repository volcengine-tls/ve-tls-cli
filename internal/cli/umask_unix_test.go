//go:build !windows

package cli

import "golang.org/x/sys/unix"

func init() {
	// CLI tests persist credentials beneath TempDir paths, which must satisfy
	// the same private-directory contract as production state.
	unix.Umask(0o077)
}
