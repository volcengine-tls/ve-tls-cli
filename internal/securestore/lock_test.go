package securestore

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTwoStoreInstancesSerializeSameKey(t *testing.T) {
	root := t.TempDir()
	first := New(root)
	second := New(filepath.Clean(root))
	originalHook := lockContendedHook
	t.Cleanup(func() { lockContendedHook = originalHook })
	secondAttempting := make(chan struct{})
	var attempted sync.Once
	lockContendedHook = func(string) {
		attempted.Do(func() { close(secondAttempting) })
	}
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- first.WithLock(context.Background(), "sso", "same-key", func() error {
			close(firstEntered)
			<-releaseFirst
			return nil
		})
	}()
	waitForLockEntry(t, firstEntered, firstDone, "first same-key lock")

	secondEntered := make(chan struct{})
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- second.WithLock(context.Background(), "sso", "same-key", func() error {
			close(secondEntered)
			return nil
		})
	}()

	select {
	case <-secondAttempting:
	case <-secondEntered:
		t.Fatal("second store entered the same-key lock before the first released it")
	case <-time.After(time.Second):
		t.Fatal("second store did not attempt the contended same-key lock")
	}
	select {
	case <-secondEntered:
		t.Fatal("second store entered after observing contention but before the first released it")
	default:
	}
	close(releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatalf("first WithLock: %v", err)
	}
	select {
	case <-secondEntered:
	case <-time.After(time.Second):
		t.Fatal("second store did not enter after same-key lock was released")
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second WithLock: %v", err)
	}
}

func TestDifferentKeysDoNotBlockEachOther(t *testing.T) {
	store := New(t.TempDir())
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- store.WithLock(context.Background(), "sso", "key-a", func() error {
			close(firstEntered)
			<-releaseFirst
			return nil
		})
	}()
	waitForLockEntry(t, firstEntered, firstDone, "first key-a lock")

	secondEntered := make(chan struct{})
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- store.WithLock(context.Background(), "sso", "key-b", func() error {
			close(secondEntered)
			return nil
		})
	}()
	select {
	case <-secondEntered:
	case <-time.After(time.Second):
		t.Fatal("different key was blocked by unrelated lock")
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second WithLock: %v", err)
	}
	close(releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatalf("first WithLock: %v", err)
	}
}

func TestWithLockWaitCanBeCanceled(t *testing.T) {
	store := New(t.TempDir())
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- store.WithLock(context.Background(), "sso", "key", func() error {
			close(firstEntered)
			<-releaseFirst
			return nil
		})
	}()
	waitForLockEntry(t, firstEntered, firstDone, "first cancelable lock")

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	called := false
	err := store.WithLock(ctx, "sso", "key", func() error {
		called = true
		return nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("WithLock error=%v, want context deadline exceeded", err)
	}
	if called {
		t.Fatal("callback ran after lock wait was canceled")
	}

	close(releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatalf("first WithLock: %v", err)
	}
}

func TestWithLockReturnsCallbackError(t *testing.T) {
	store := New(t.TempDir())
	sentinel := errors.New("callback failed")
	err := store.WithLock(nil, "sso", "key", func() error {
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("WithLock error=%v, want callback error", err)
	}
}

func TestOSFileLockWaitCanBeCanceled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "direct.lock")
	release, err := acquireOSFileLock(context.Background(), path)
	if err != nil {
		t.Fatalf("first acquireOSFileLock: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	_, err = acquireOSFileLock(ctx, path)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second acquireOSFileLock error=%v, want deadline exceeded", err)
	}
	if err := release(); err != nil {
		t.Fatalf("release: %v", err)
	}
}

func TestWithLockRejectsInvalidKey(t *testing.T) {
	store := New(t.TempDir())
	err := store.WithLock(context.Background(), "sso", "../key", func() error {
		t.Fatal("callback must not run for invalid key")
		return nil
	})
	if !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("WithLock error=%v, want ErrInvalidPath", err)
	}
}

func TestWithLockReleasesAfterCallbackPanic(t *testing.T) {
	store := New(t.TempDir())
	func() {
		defer func() {
			if recovered := recover(); recovered == nil {
				t.Fatal("WithLock callback did not panic")
			}
		}()
		_ = store.WithLock(context.Background(), "sso", "panic-key", func() error {
			panic("injected callback panic")
		})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	if err := store.WithLock(ctx, "sso", "panic-key", func() error { return nil }); err != nil {
		t.Fatalf("lock remained held after callback panic: %v", err)
	}
}

func TestWithLockSerializesAcrossProcesses(t *testing.T) {
	if os.Getenv("SECURESTORE_LOCK_HELPER") == "1" {
		root := os.Getenv("SECURESTORE_LOCK_ROOT")
		marker := os.Getenv("SECURESTORE_LOCK_MARKER")
		ready := os.Getenv("SECURESTORE_LOCK_READY")
		var attempted sync.Once
		lockContendedHook = func(string) {
			attempted.Do(func() {
				if err := os.WriteFile(ready, []byte("attempting"), 0o600); err != nil {
					t.Fatalf("write attempting marker: %v", err)
				}
			})
		}
		if err := New(root).WithLock(context.Background(), "sso", "cross-process", func() error {
			return os.WriteFile(marker, []byte("entered"), 0o600)
		}); err != nil {
			t.Fatalf("child WithLock: %v", err)
		}
		return
	}

	root := t.TempDir()
	marker := filepath.Join(root, "entered")
	ready := filepath.Join(root, "ready")
	store := New(root)
	var child *exec.Cmd
	childWaited := false
	t.Cleanup(func() {
		if child == nil || child.Process == nil || childWaited {
			return
		}
		_ = child.Process.Kill()
		_ = child.Wait()
	})
	err := store.WithLock(context.Background(), "sso", "cross-process", func() error {
		child = exec.Command(os.Args[0], "-test.run=^TestWithLockSerializesAcrossProcesses$")
		child.Env = append(os.Environ(),
			"SECURESTORE_LOCK_HELPER=1",
			"SECURESTORE_LOCK_ROOT="+root,
			"SECURESTORE_LOCK_MARKER="+marker,
			"SECURESTORE_LOCK_READY="+ready,
		)
		if err := child.Start(); err != nil {
			return err
		}
		waitForFile(t, ready, time.Second)
		if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("child entered cross-process lock before parent release: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("parent WithLock: %v", err)
	}
	if child == nil {
		t.Fatal("child process was not started")
	}
	if err := child.Wait(); err != nil {
		t.Fatalf("child process: %v", err)
	}
	childWaited = true
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("child did not enter after parent released lock: %v", err)
	}
}

func TestProcessLockRegistryReleasesIdleEntries(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "store"))
	for _, key := range []string{"one", "two", "three"} {
		if err := store.WithLock(context.Background(), "sso", key, func() error { return nil }); err != nil {
			t.Fatalf("WithLock(%q): %v", key, err)
		}
	}
	if got := processLockRegistrySize(); got != 0 {
		t.Fatalf("process lock registry has %d idle entries, want 0", got)
	}
}

// TestWithLockContentionObserver verifies the context-scoped diagnostic
// observer: it must not fire on immediate (uncontended) acquisition, must fire
// exactly once when a same-process waiter blocks on a held lock, and a nil
// observer must be harmless.
func TestWithLockContentionObserver(t *testing.T) {
	root := t.TempDir()
	first := New(root)
	second := New(filepath.Clean(root))

	t.Run("no observer on uncontended acquisition", func(t *testing.T) {
		called := make(chan struct{}, 1)
		ctx := WithLockContentionObserver(context.Background(), func() {
			called <- struct{}{}
		})
		if err := first.WithLock(ctx, "sso", "uncontended", func() error { return nil }); err != nil {
			t.Fatalf("WithLock: %v", err)
		}
		select {
		case <-called:
			t.Fatal("observer fired on uncontended acquisition")
		default:
		}
	})

	t.Run("observer fires once for blocked waiter", func(t *testing.T) {
		firstEntered := make(chan struct{})
		releaseFirst := make(chan struct{})
		firstDone := make(chan error, 1)
		go func() {
			firstDone <- first.WithLock(context.Background(), "sso", "contended", func() error {
				close(firstEntered)
				<-releaseFirst
				return nil
			})
		}()
		waitForLockEntry(t, firstEntered, firstDone, "first observed lock")

		var count int32
		notified := make(chan struct{}, 1)
		var once sync.Once
		ctx := WithLockContentionObserver(context.Background(), func() {
			atomic.AddInt32(&count, 1)
			once.Do(func() { close(notified) })
		})
		secondDone := make(chan error, 1)
		go func() {
			secondDone <- second.WithLock(ctx, "sso", "contended", func() error { return nil })
		}()

		select {
		case <-notified:
		case <-time.After(time.Second):
			t.Fatal("observer did not fire for blocked waiter")
		}
		close(releaseFirst)
		if err := <-firstDone; err != nil {
			t.Fatalf("first WithLock: %v", err)
		}
		if err := <-secondDone; err != nil {
			t.Fatalf("second WithLock: %v", err)
		}
		if got := atomic.LoadInt32(&count); got != 1 {
			t.Fatalf("observer fired %d times, want exactly 1", got)
		}
	})

	t.Run("nil observer is harmless", func(t *testing.T) {
		ctx := WithLockContentionObserver(context.Background(), nil)
		if err := first.WithLock(ctx, "sso", "nil-obs", func() error { return nil }); err != nil {
			t.Fatalf("WithLock with nil observer: %v", err)
		}
	})

	t.Run("nil context follows existing behavior", func(t *testing.T) {
		ctx := WithLockContentionObserver(nil, func() {})
		if ctx == nil {
			t.Fatal("WithLockContentionObserver(nil, ...) returned nil context")
		}
		if err := first.WithLock(ctx, "sso", "nil-ctx", func() error { return nil }); err != nil {
			t.Fatalf("WithLock with nil-derived context: %v", err)
		}
	})
}

func waitForLockEntry(t *testing.T, entered <-chan struct{}, done <-chan error, name string) {
	t.Helper()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("%s failed before entering: %v", name, err)
	case <-time.After(time.Second):
		t.Fatalf("%s did not enter", name)
	}
}

func waitForFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %q", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
