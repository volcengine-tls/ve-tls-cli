//go:build windows

package cli

import (
	"os"

	"golang.org/x/sys/windows"
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
		var mode uint32
		isTerminal = windows.GetConsoleMode(windows.Handle(fd), &mode) == nil
	})
	return err == nil && isTerminal
}
