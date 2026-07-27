//go:build windows

package securestore

// MoveFileEx with MOVEFILE_WRITE_THROUGH provides the Windows durability
// boundary; opening directories for fsync is not portable on Windows.
func syncParentDirectory(string) error {
	return nil
}
