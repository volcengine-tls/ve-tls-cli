package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func loadSecretsFile(path string) error {
	p := strings.TrimSpace(path)
	if p == "" {
		return errors.New("empty secrets file")
	}
	b, err := os.ReadFile(filepath.Clean(p))
	if err != nil {
		return err
	}
	lines := strings.Split(string(b), "\n")
	for _, line := range lines {
		l := strings.TrimSpace(line)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		if strings.HasPrefix(l, "export ") {
			l = strings.TrimSpace(strings.TrimPrefix(l, "export "))
		}
		k, v, ok := strings.Cut(l, "=")
		if !ok {
			continue
		}
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		val := strings.TrimSpace(v)
		val = strings.Trim(val, "\"")
		val = strings.Trim(val, "'")
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, val); err != nil {
			return err
		}
	}
	return nil
}
