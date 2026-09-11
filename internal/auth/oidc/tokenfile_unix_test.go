//go:build !windows

package oidc

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestReadTokenFileFinalOpenDoesNotFollowAnotherSymlink(t *testing.T) {
	dir := t.TempDir()
	real := writeFile(t, dir, "real", "token\n")
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// eval returns the link; the final open must use O_NOFOLLOW and fail.
	_, err := readTokenFileWithOps(link, func(string) (string, error) { return link, nil }, openTokenFile)
	if err == nil {
		t.Fatal("expected ELOOP/error: final open must not follow symlinks")
	}
	// Must be a syscall error indicating the symlink was not followed.
	if !errors.Is(err, unix.ELOOP) {
		t.Fatalf("error=%v, want ELOOP", err)
	}
}

func TestReadTokenFileRejectsFIFOWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	fifoPath := filepath.Join(dir, "fifo")
	if err := unix.Mkfifo(fifoPath, 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}

	// Opening a FIFO with no writer would block without O_NONBLOCK. The opener
	// must return quickly and the Stat check must reject it as non-regular.
	done := make(chan error, 1)
	go func() {
		_, err := readTokenFile(fifoPath)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error for FIFO token file")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("readTokenFile blocked on FIFO open (O_NONBLOCK missing)")
	}
}
