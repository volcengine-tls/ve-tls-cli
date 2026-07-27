//go:build !windows

package oidc

import (
	"os"

	"golang.org/x/sys/unix"
)

// openTokenFile opens the resolved final target with O_RDONLY|O_CLOEXEC|O_NOFOLLOW
// so a symlink swapped in after evalSymlinks is rejected. O_NONBLOCK ensures
// that opening a FIFO with no writer does not block; the subsequent Stat check
// rejects non-regular files. The descriptor is wrapped in *os.File and closed if
// wrapping fails.
func openTokenFile(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	if f == nil {
		// os.NewFile returns nil only for invalid fds; close to avoid a leak.
		// err is nil here, so use a concrete bad-file-descriptor error.
		_ = unix.Close(fd)
		return nil, os.NewSyscallError("open", unix.EBADF)
	}
	return f, nil
}
