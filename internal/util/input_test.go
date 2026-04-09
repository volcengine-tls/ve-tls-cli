package util

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadMaybeFile_BarePath(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "req.json")
	if err := os.WriteFile(p, []byte(`{"a":1}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, err := ReadMaybeFile(p)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(out) != `{"a":1}` {
		t.Fatalf("unexpected: %s", string(out))
	}
}

func TestReadMaybeFile_InlineJSON(t *testing.T) {
	out, err := ReadMaybeFile(`{"a":1}`)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(out) != `{"a":1}` {
		t.Fatalf("unexpected: %s", string(out))
	}
}

func TestReadMaybeFile_BarePathMissingReturnsHelpfulError(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "req.json")
	_, err := ReadMaybeFile(p)
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); got == "" || !containsAll(got, "file not found", p) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReadMaybeFile_PlainStringLiteralPreserved(t *testing.T) {
	out, err := ReadMaybeFile("not-json")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(out) != "not-json" {
		t.Fatalf("unexpected: %s", string(out))
	}
}

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

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}
