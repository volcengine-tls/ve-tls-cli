//go:build darwin

package cli

import (
	"os"

	"golang.org/x/sys/unix"
)

func inputIsTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	raw, err := f.SyscallConn()
	if err != nil {
		return false
	}
	isTerminal := false
	err = raw.Control(func(fd uintptr) {
		_, ioctlErr := unix.IoctlGetTermios(int(fd), unix.TIOCGETA)
		isTerminal = ioctlErr == nil
	})
	return err == nil && isTerminal
}
