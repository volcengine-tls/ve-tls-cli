package securestore

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestWriteJSONCreates0700DirectoryAnd0600File(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	store := New(root)

	if err := store.WriteJSON("sso", "account@example.com", map[string]string{"token": "secret"}); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	for _, dir := range []string{root, filepath.Join(root, "sso")} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("stat directory %q: %v", dir, err)
		}
		assertUnixMode(t, info, 0o700)
	}

	path := store.dataPath("sso", "account@example.com")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat cache file: %v", err)
	}
	assertUnixMode(t, info, 0o600)
}

func TestCacheFilenameHidesIdentityMetadata(t *testing.T) {
	const identity = "account-123_user@example.com"
	store := New(t.TempDir())
	if err := store.WriteJSON("sso", identity, map[string]string{"token": "secret"}); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	path := store.dataPath("sso", identity)
	base := filepath.Base(path)
	if strings.Contains(path, identity) ||
		strings.Contains(base, "account-123") ||
		strings.Contains(base, "user") ||
		strings.Contains(base, "example.com") {
		t.Fatalf("cache path leaks identity metadata: %q", path)
	}
	want := DigestKey("sso", identity) + ".json"
	if base != want {
		t.Fatalf("cache filename=%q, want digest-only %q", base, want)
	}
}

func TestAtomicWritePreservesOldFileWhenReplaceFails(t *testing.T) {
	store := New(t.TempDir())
	if err := store.WriteJSON("sso", "key", map[string]string{"value": "old"}); err != nil {
		t.Fatalf("initial WriteJSON: %v", err)
	}
	replaceErr := errors.New("injected replace failure")
	store.replace = func(_, _ string) error { return replaceErr }

	err := store.WriteJSON("sso", "key", map[string]string{"value": "new"})
	if !errors.Is(err, replaceErr) {
		t.Fatalf("WriteJSON error=%v, want injected replace failure", err)
	}
	var got map[string]string
	if err := store.ReadJSON("sso", "key", &got); err != nil {
		t.Fatalf("ReadJSON after failed replace: %v", err)
	}
	if got["value"] != "old" {
		t.Fatalf("value=%q, want old content preserved", got["value"])
	}
	assertNoTemporaryFiles(t, filepath.Join(store.root, "sso"))
}

func TestReadJSONDistinguishesMissingCorruptAndPermissionErrors(t *testing.T) {
	store := New(t.TempDir())

	var out map[string]string
	err := store.ReadJSON("sso", "missing", &out)
	if !errors.Is(err, ErrMissing) {
		t.Fatalf("missing error=%v, want ErrMissing", err)
	}
	if !strings.Contains(err.Error(), ErrMissing.Error()) {
		t.Fatalf("missing error string=%q, want classification", err.Error())
	}

	corruptPath := store.dataPath("sso", "corrupt")
	if err := os.MkdirAll(filepath.Dir(corruptPath), 0o700); err != nil {
		t.Fatalf("mkdir corrupt parent: %v", err)
	}
	if err := os.WriteFile(corruptPath, []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("write corrupt cache: %v", err)
	}
	err = store.ReadJSON("sso", "corrupt", &out)
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("corrupt error=%v, want ErrCorrupt", err)
	}
	if errors.Is(err, ErrMissing) || errors.Is(err, ErrPermission) {
		t.Fatalf("corrupt error has wrong classification: %v", err)
	}

	store.readFile = func(string) ([]byte, error) {
		return nil, &fs.PathError{Op: "open", Path: "redacted", Err: fs.ErrPermission}
	}
	err = store.ReadJSON("sso", "forbidden", &out)
	if !errors.Is(err, ErrPermission) || !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("permission error=%v, want ErrPermission and fs.ErrPermission", err)
	}
}

func TestDeleteMissingIsIdempotent(t *testing.T) {
	store := New(t.TempDir())
	if err := store.Delete("sso", "missing"); err != nil {
		t.Fatalf("Delete missing: %v", err)
	}
	if err := store.WriteJSON("sso", "present", map[string]bool{"ok": true}); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	if err := store.Delete("sso", "present"); err != nil {
		t.Fatalf("Delete present: %v", err)
	}
	if err := store.Delete("sso", "present"); err != nil {
		t.Fatalf("Delete already deleted: %v", err)
	}
}

func TestDigestKeyUsesUnambiguousParts(t *testing.T) {
	if DigestKey("ab", "c") == DigestKey("a", "bc") {
		t.Fatal("DigestKey must preserve part boundaries")
	}
	if got := len(DigestKey("value")); got != 64 {
		t.Fatalf("digest length=%d, want 64 hex characters", got)
	}
}

func TestStoreRejectsPathTraversal(t *testing.T) {
	store := New(t.TempDir())
	for _, tc := range []struct {
		namespace string
		key       string
	}{
		{namespace: "../sso", key: "key"},
		{namespace: "sso", key: "../key"},
		{namespace: "sso/child", key: "key"},
		{namespace: "sso", key: `..\key`},
	} {
		if err := store.WriteJSON(tc.namespace, tc.key, map[string]bool{}); !errors.Is(err, ErrInvalidPath) {
			t.Fatalf("WriteJSON(%q,%q) error=%v, want ErrInvalidPath", tc.namespace, tc.key, err)
		}
	}
}

func TestUpdateFileReadsCurrentAndAtomicallyReplaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "state")
	if err := UpdateFile(path, 0o600, func(current []byte) ([]byte, error) {
		if current != nil {
			t.Fatalf("initial current=%q, want nil", current)
		}
		return []byte("first"), nil
	}); err != nil {
		t.Fatalf("initial UpdateFile: %v", err)
	}
	if err := UpdateFile(path, 0o600, func(current []byte) ([]byte, error) {
		if got := string(current); got != "first" {
			t.Fatalf("current=%q, want first", got)
		}
		return []byte("second"), nil
	}); err != nil {
		t.Fatalf("second UpdateFile: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if got := string(data); got != "second" {
		t.Fatalf("content=%q, want second", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	assertUnixMode(t, info, 0o600)
}

func TestUpdateFileCallbackErrorPreservesCurrent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	sentinel := errors.New("callback failed")
	err := UpdateFile(path, 0o600, func(current []byte) ([]byte, error) {
		if got := string(current); got != "old" {
			t.Fatalf("current=%q, want old", got)
		}
		return []byte("new"), sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("UpdateFile error=%v, want callback error", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if got := string(data); got != "old" {
		t.Fatalf("content=%q, want old preserved", got)
	}
}

func TestConcurrentUpdateFileTransactionsDoNotLoseUpdates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "counter")
	const workers = 12
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- UpdateFile(path, 0o600, func(current []byte) ([]byte, error) {
				value := 0
				if len(current) > 0 {
					parsed, err := strconv.Atoi(string(current))
					if err != nil {
						return nil, err
					}
					value = parsed
				}
				return []byte(strconv.Itoa(value + 1)), nil
			})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("UpdateFile: %v", err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if got := string(data); got != strconv.Itoa(workers) {
		t.Fatalf("counter=%q, want %d", got, workers)
	}
}

func TestUpdateFileRejectsEmptyPathAndReadFailure(t *testing.T) {
	if err := UpdateFile("", 0o600, func([]byte) ([]byte, error) {
		t.Fatal("callback must not run for empty path")
		return nil, nil
	}); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("empty path error=%v, want ErrInvalidPath", err)
	}

	dir := t.TempDir()
	err := UpdateFile(dir, 0o600, func([]byte) ([]byte, error) {
		t.Fatal("callback must not run when current path cannot be read as a file")
		return nil, nil
	})
	if err == nil {
		t.Fatal("UpdateFile on directory unexpectedly succeeded")
	}
}

func TestWriteJSONReturnsMarshalErrorWithoutCreatingFile(t *testing.T) {
	store := New(t.TempDir())
	err := store.WriteJSON("sso", "key", func() {})
	if err == nil {
		t.Fatal("WriteJSON unexpectedly accepted an unsupported JSON value")
	}
	if _, statErr := os.Stat(store.dataPath("sso", "key")); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("cache file exists after marshal error: %v", statErr)
	}
}

func assertNoTemporaryFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp-") {
			t.Fatalf("temporary file was not cleaned up: %s", entry.Name())
		}
	}
}

func assertUnixMode(t *testing.T, info fs.FileInfo, want fs.FileMode) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	if got := info.Mode().Perm(); got != want.Perm() {
		t.Fatalf("mode=%#o, want %#o", got, want.Perm())
	}
}

func TestStoreCanonicalRootReturnsValidatedRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	store := New(root)

	got, err := store.CanonicalRoot()
	if err != nil {
		t.Fatalf("CanonicalRoot error: %v", err)
	}
	want, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	if got != want {
		t.Errorf("CanonicalRoot=%q, want %q", got, want)
	}
}

func TestStoreCanonicalRootReturnsRootError(t *testing.T) {
	store := New("/")
	if _, err := store.CanonicalRoot(); err == nil {
		t.Fatal("expected error from CanonicalRoot for invalid root")
	}
}

func TestStoreCanonicalRootNilStore(t *testing.T) {
	var store *Store
	if _, err := store.CanonicalRoot(); err == nil {
		t.Fatal("expected error from CanonicalRoot for nil store")
	}
}
