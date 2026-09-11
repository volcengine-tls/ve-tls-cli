package oidc

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// writeFile creates a regular file with the given content and returns its path.
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

func TestReadTokenFilePreservesRawBytes(t *testing.T) {
	dir := t.TempDir()
	// Token with a trailing newline that must reach STS unchanged.
	token := "header.payload.signature\n"
	p := writeFile(t, dir, "token", token)

	got, err := readTokenFile(p)
	if err != nil {
		t.Fatalf("readTokenFile: %v", err)
	}
	if !bytes.Equal(got, []byte(token)) {
		t.Fatalf("token=%q, want %q (raw bytes including newline must be preserved)", got, token)
	}
}

func TestReadTokenFileSupportsRotatingProjectedSymlink(t *testing.T) {
	dir := t.TempDir()
	// Kubernetes projects tokens as a symlink that is atomically replaced.
	link := filepath.Join(dir, "token")
	target1 := writeFile(t, dir, "token.v1", "first-token\n")
	if err := os.Symlink(target1, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	got1, err := readTokenFile(link)
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	if string(got1) != "first-token\n" {
		t.Fatalf("first token=%q, want first-token\\n", got1)
	}

	// Rotate: replace the symlink target with a new file (K8s atomic rename).
	target2 := writeFile(t, dir, "token.v2", "second-token\n")
	tmpLink := link + ".tmp"
	if err := os.Symlink(target2, tmpLink); err != nil {
		t.Fatalf("symlink tmp: %v", err)
	}
	if err := os.Rename(tmpLink, link); err != nil {
		t.Fatalf("rename: %v", err)
	}

	got2, err := readTokenFile(link)
	if err != nil {
		t.Fatalf("second read: %v", err)
	}
	if string(got2) != "second-token\n" {
		t.Fatalf("second token=%q, want second-token\\n", got2)
	}
}

func TestReadTokenFileRejectsEmptyNULNonRegularAndOversized(t *testing.T) {
	dir := t.TempDir()

	t.Run("empty file rejected", func(t *testing.T) {
		p := writeFile(t, dir, "empty", "")
		_, err := readTokenFile(p)
		if err == nil {
			t.Fatal("expected error for empty token file")
		}
	})

	t.Run("NUL byte rejected", func(t *testing.T) {
		p := writeFile(t, dir, "nul", "abc\x00def")
		_, err := readTokenFile(p)
		if err == nil {
			t.Fatal("expected error for token containing NUL")
		}
	})

	t.Run("symlink as final target rejected by O_NOFOLLOW", func(t *testing.T) {
		// EvalSymlinks resolves to a regular file, but we replace the resolved
		// path with a symlink before the final open to simulate a race.
		real := writeFile(t, dir, "real", "token\n")
		link := filepath.Join(dir, "link")
		if err := os.Symlink(real, link); err != nil {
			t.Fatalf("symlink: %v", err)
		}
		// eval returns the link path; the final open with O_NOFOLLOW must reject it.
		_, err := readTokenFileWithOps(link, func(string) (string, error) { return link, nil }, openTokenFile)
		if err == nil {
			t.Fatal("expected error when final target is a symlink (O_NOFOLLOW)")
		}
	})

	t.Run("oversize rejected", func(t *testing.T) {
		// 64KiB + 1 byte must be rejected.
		big := make([]byte, 64*1024+1)
		for i := range big {
			big[i] = 'x'
		}
		p := filepath.Join(dir, "big")
		if err := os.WriteFile(p, big, 0o600); err != nil {
			t.Fatalf("write big: %v", err)
		}
		_, err := readTokenFile(p)
		if err == nil {
			t.Fatal("expected error for oversize token file")
		}
	})
}

func TestReadTokenFileDoesNotChangePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission test is unix-only")
	}
	dir := t.TempDir()
	p := writeFile(t, dir, "token", "tok\n")
	origInfo, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	origMode := origInfo.Mode()

	if _, err := readTokenFile(p); err != nil {
		t.Fatalf("readTokenFile: %v", err)
	}

	newInfo, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat after: %v", err)
	}
	if newInfo.Mode() != origMode {
		t.Fatalf("mode changed from %v to %v", origMode, newInfo.Mode())
	}
}

func TestReadTokenFileRejectsDirectory(t *testing.T) {
	dir := t.TempDir()
	// Pass the directory itself as the token file path.
	_, err := readTokenFile(dir)
	if err == nil {
		t.Fatal("expected error for directory token file")
	}
}

func TestReadTokenFileWithOpsDefendsAgainstNilFile(t *testing.T) {
	// A misbehaving openFn that returns (nil, nil) must not panic; it must
	// return a safe auth.ProtocolError.
	nilOpen := func(string) (*os.File, error) { return nil, nil }
	_, err := readTokenFileWithOps("any", func(string) (string, error) { return "any", nil }, nilOpen)
	if err == nil {
		t.Fatal("expected error when open returns (nil, nil)")
	}
}

// TestInspectTokenFileRegularFile verifies that InspectTokenFile succeeds for a
// regular file without reading or returning its contents.
func TestInspectTokenFileRegularFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "token")
	if err := os.WriteFile(p, []byte("SECRET-TOKEN-MUST-NOT-BE-READ"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := InspectTokenFile(p); err != nil {
		t.Fatalf("InspectTokenFile: %v", err)
	}
}

// TestInspectTokenFileMissing verifies that a missing file produces an error.
func TestInspectTokenFileMissing(t *testing.T) {
	if err := InspectTokenFile(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("expected error for missing file")
	}
}

// TestInspectTokenFileRejectsDirectory verifies that a directory is rejected.
func TestInspectTokenFileRejectsDirectory(t *testing.T) {
	if err := InspectTokenFile(t.TempDir()); err == nil {
		t.Fatal("expected error for directory")
	}
}

// TestInspectTokenFileRejectsSymlinkToDirectory verifies that a symlink pointing
// to a directory is rejected (not a regular file).
func TestInspectTokenFileRejectsSymlinkToDirectory(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if err := InspectTokenFile(link); err == nil {
		t.Fatal("expected error for symlink to directory")
	}
}

// TestInspectTokenFileWithOpsDefendsAgainstNilFile verifies that a misbehaving
// openFn returning (nil, nil) does not panic.
func TestInspectTokenFileWithOpsDefendsAgainstNilFile(t *testing.T) {
	nilOpen := func(string) (*os.File, error) { return nil, nil }
	if err := inspectTokenFileWithOps("any", func(string) (string, error) { return "any", nil }, nilOpen); err == nil {
		t.Fatal("expected error for nil file")
	}
}

// TestInspectTokenFileWithOpsPermissionError verifies that an open error from
// the injected opener is surfaced as an error (not a panic, not success).
func TestInspectTokenFileWithOpsPermissionError(t *testing.T) {
	permErr := errors.New("permission denied")
	open := func(string) (*os.File, error) { return nil, permErr }
	err := inspectTokenFileWithOps("any", func(string) (string, error) { return "any", nil }, open)
	if err == nil {
		t.Fatal("expected error from failing open")
	}
}

// TestInspectTokenFileWithOpsLateSymlink verifies that when eval resolves to a
// path that is then swapped for a symlink before the final open, the secure
// opener rejects it (O_NOFOLLOW on unix).
func TestInspectTokenFileWithOpsLateSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.WriteFile(real, []byte("token"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	// eval returns the link; the final production open must reject the symlink.
	err := inspectTokenFileWithOps(link, func(string) (string, error) { return link, nil }, openTokenFile)
	if err == nil {
		t.Fatal("expected error when final target is a symlink")
	}
}
