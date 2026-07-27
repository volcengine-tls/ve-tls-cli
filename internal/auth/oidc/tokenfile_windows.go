//go:build windows

package oidc

import (
	"os"

	"golang.org/x/sys/windows"
)

// openTokenFile opens the resolved final target on Windows. It uses
// FILE_FLAG_OPEN_REPARSE_POINT to open reparse points (symlinks) without
// following them, then rejects any file that has the reparse-point attribute
// set. The handle is wrapped in *os.File and closed if wrapping fails.
func openTokenFile(path string) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(
		name,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return nil, err
	}

	// Reject reparse points (symlinks) so a target swapped in after the path
	// resolution cannot redirect the read.
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		_ = windows.CloseHandle(handle)
		return nil, err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		_ = windows.CloseHandle(handle)
		return nil, os.ErrInvalid
	}

	f := os.NewFile(uintptr(handle), path)
	if f == nil {
		// err is nil here, so use a concrete invalid-handle error.
		_ = windows.CloseHandle(handle)
		return nil, os.NewSyscallError("CreateFile", windows.ERROR_INVALID_HANDLE)
	}
	return f, nil
}
