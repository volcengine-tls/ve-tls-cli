package runner

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func tryEmbeddedTLSCTL() (string, bool, error) {
	name, data, ok := embeddedTLSCTL()
	if !ok || len(data) == 0 {
		return "", false, nil
	}
	cacheDir := strings.TrimSpace(os.Getenv("TLSCTL_CACHE_DIR"))
	if cacheDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false, err
		}
		cacheDir = filepath.Join(home, ".cache", "tlsctl-runner")
	}
	sum := sha256.Sum256(data)
	dir := filepath.Join(cacheDir, "embedded", hex.EncodeToString(sum[:8]))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", false, err
	}
	out := filepath.Join(dir, name)
	if st, err := os.Stat(out); err == nil && !st.IsDir() {
		return out, true, nil
	}
	tmp := out + ".tmp"
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		return "", false, err
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(tmp, 0o755)
	}
	if err := os.Rename(tmp, out); err != nil {
		_ = os.Remove(tmp)
		return "", false, err
	}
	if st, err := os.Stat(out); err != nil || st.IsDir() {
		return "", false, errors.New("embedded tlsctl extract failed")
	}
	return out, true, nil
}

