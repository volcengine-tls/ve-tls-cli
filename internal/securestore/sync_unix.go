//go:build !windows

package securestore

import (
	"errors"
	"os"
	"path/filepath"
)

func syncParentDirectory(path string) error {
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}
