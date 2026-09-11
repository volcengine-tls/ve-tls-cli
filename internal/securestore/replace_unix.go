//go:build !windows

package securestore

import "os"

func replaceFile(source, target string) error {
	return os.Rename(source, target)
}
