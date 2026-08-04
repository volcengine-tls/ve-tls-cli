//go:build human

package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestIndexBodyFieldSpecUsesOperationCatalog(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "index_schema.go"))
	if err != nil {
		t.Fatalf("read index_schema.go: %v", err)
	}
	for _, forbidden := range []string{"loadAPICapabilities", "buildAPIIndex"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("index_schema.go should not reference %q", forbidden)
		}
	}

	fields, required, err := indexBodyFieldSpec("CreateIndex")
	if err != nil {
		t.Fatalf("indexBodyFieldSpec: %v", err)
	}
	for _, allowed := range []string{
		"TopicId",
		"FullText",
		"KeyValue",
		"UserInnerKeyValue",
		"MaxTextLen",
		"EnableAutoIndex",
	} {
		if _, ok := fields[allowed]; !ok {
			t.Errorf("missing allowed index field %q", allowed)
		}
	}
	for _, unsupported := range []string{
		"EnablePhraseIndex",
		"LogReduce",
		"LogReduceBlackList",
		"LogReduceWhiteList",
	} {
		if _, ok := fields[unsupported]; ok {
			t.Errorf("legacy-unsupported index field %q became allowed", unsupported)
		}
	}
	if len(required) != 1 || required[0] != "TopicId" {
		t.Fatalf("required fields = %#v, want [TopicId]", required)
	}
}
