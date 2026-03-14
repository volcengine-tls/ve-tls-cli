package util

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadStringListMaybeFile_JSONArray(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.json")
	if err := os.WriteFile(p, []byte(`["a","b"]`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, err := ReadStringListMaybeFile("file://" + p)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(out) != 2 || out[0] != "a" || out[1] != "b" {
		t.Fatalf("unexpected: %#v", out)
	}
}

func TestReadStringListMaybeFile_Lines(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(p, []byte("a\n\nb\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, err := ReadStringListMaybeFile("file://" + p)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(out) != 2 || out[0] != "a" || out[1] != "b" {
		t.Fatalf("unexpected: %#v", out)
	}
}
