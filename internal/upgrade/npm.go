package upgrade

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

func (m *Manager) upgradeNPM(ctx context.Context, target string) error {
	packageName := strings.TrimSpace(m.Installation.PackageName)
	packageRoot := strings.TrimSpace(m.Installation.PackageRoot)
	if packageName == "" || packageRoot == "" {
		return errors.New("invalid npm installation metadata; reinstall the npm package")
	}
	globalRootOutput, err := m.Command(ctx, "npm", []string{"root", "--global"}, "")
	if err != nil {
		return fmt.Errorf("resolve npm installation scope: %w", err)
	}
	globalRoot := strings.TrimSpace(string(globalRootOutput))
	packageRoot = canonicalPath(packageRoot)
	globalRoot = canonicalPath(globalRoot)
	packageSpec := packageName + "@" + target
	if pathWithin(packageRoot, globalRoot) {
		_, err = m.Command(ctx, "npm", []string{"install", "--global", packageSpec}, "")
		if err != nil {
			return fmt.Errorf("npm upgrade failed: %w", err)
		}
		return nil
	}
	projectRoot, ok := npmProjectRoot(packageRoot)
	if !ok {
		return errors.New("cannot resolve the npm project root; run npm install manually")
	}
	_, err = m.Command(ctx, "npm", []string{"install", "--save-exact", packageSpec}, projectRoot)
	if err != nil {
		return fmt.Errorf("npm upgrade failed: %w", err)
	}
	return nil
}

func canonicalPath(value string) string {
	clean := filepath.Clean(strings.TrimSpace(value))
	resolved, err := filepath.EvalSymlinks(clean)
	if err == nil {
		return resolved
	}
	return clean
}

func npmProjectRoot(packageRoot string) (string, bool) {
	clean := filepath.Clean(packageRoot)
	for current := clean; ; current = filepath.Dir(current) {
		if filepath.Base(current) == "node_modules" {
			return filepath.Dir(current), true
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", false
		}
	}
}

func pathWithin(path, root string) bool {
	path = filepath.Clean(strings.TrimSpace(path))
	root = filepath.Clean(strings.TrimSpace(root))
	if path == "." || root == "." {
		return false
	}
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return relative != ".." && relative != "." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
