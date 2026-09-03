//go:build !windows

package upgrade

import "os"

func replaceFile(source, target string) error {
	return os.Rename(source, target)
}
