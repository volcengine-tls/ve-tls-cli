package cli

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

const cliImportPath = "github.com/volcengine-tls/ve-tls-cli/internal/cli"

func TestCorePackagesDoNotImportCLI(t *testing.T) {
	t.Parallel()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
	roots := []string{
		"internal/contract",
		"internal/execution",
		"internal/app/runtime",
		"internal/auth",
		"internal/config",
		"internal/tlsapi",
		"internal/output",
	}

	violations, err := findCLIImports(repoRoot, roots)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("lower-level packages must not import internal/cli:\n%s", strings.Join(violations, "\n"))
	}
}

func findCLIImports(repoRoot string, roots []string) ([]string, error) {
	var violations []string
	for _, root := range roots {
		err := filepath.WalkDir(filepath.Join(repoRoot, root), func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || filepath.Ext(path) != ".go" {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			for _, spec := range file.Imports {
				importPath, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					return err
				}
				if importPath != cliImportPath && !strings.HasPrefix(importPath, cliImportPath+"/") {
					continue
				}
				relative, err := filepath.Rel(repoRoot, path)
				if err != nil {
					return err
				}
				violations = append(violations, filepath.ToSlash(relative)+": "+importPath)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return violations, nil
}
