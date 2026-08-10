package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratorOnlyEmitsCanonicalOperationCatalog(t *testing.T) {
	root := filepath.Join("..", "..")
	generatorSources, err := filepath.Glob(filepath.Join(root, "internal", "openapigen", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range generatorSources {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		source := string(raw)
		for _, forbidden := range []string{
			"toolCatalogDoc",
			"toolEntry",
			"generatedToolCatalogJSON",
			"out-tool-catalog",
			"tool-version",
			"buildToolCatalogFromSource",
			"writeToolCatalogGo",
		} {
			if strings.Contains(source, forbidden) {
				t.Errorf("%s still contains legacy tool catalog symbol %q", path, forbidden)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(root, "internal", "cli", "generated_tool_catalog.go")); !os.IsNotExist(err) {
		t.Fatalf("legacy generated tool catalog still exists: %v", err)
	}
	for _, path := range []string{
		"scripts/update-operation-catalog.sh",
		"contracts/operation-catalog-v2-lock.json",
	} {
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		source := string(raw)
		if strings.Contains(source, "out-tool-catalog") ||
			strings.Contains(source, "legacy_generated_tool_catalog") {
			t.Fatalf("%s still references the legacy generated tool catalog", path)
		}
	}
}
