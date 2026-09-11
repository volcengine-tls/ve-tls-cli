//go:build !windows

package securestore

import (
	"fmt"
	"io/fs"
	"os"
)

func createPrivateDirectories(path string, perm fs.FileMode) error {
	if err := os.MkdirAll(path, perm.Perm()); err != nil {
		return err
	}
	return applyPrivatePermissions(path, true, perm)
}

func createPrivateDirectory(path string, perm fs.FileMode) error {
	if err := os.Mkdir(path, perm.Perm()); err != nil {
		return err
	}
	return applyPrivatePermissions(path, true, perm)
}

func createPrivateTempFile(dir, pattern string, perm fs.FileMode) (*os.File, error) {
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return nil, err
	}
	if err := applyPrivatePermissions(file.Name(), false, perm); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		return nil, err
	}
	return file, nil
}

func openPrivateLockFile(path string, perm fs.FileMode) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, perm.Perm())
	if err != nil {
		return nil, err
	}
	if err := applyPrivatePermissions(path, false, perm); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func applyPrivatePermissions(path string, _ bool, perm fs.FileMode) error {
	return os.Chmod(path, perm.Perm())
}

func secureExistingPrivatePath(path string, _ bool, perm fs.FileMode) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if got := info.Mode().Perm(); got != perm.Perm() {
		return fmt.Errorf("%w: %s mode is %#o, want %#o", ErrPermission, path, got, perm.Perm())
	}
	return nil
}

func protectExistingUpdateTarget(string, fs.FileMode) error {
	return nil
}
