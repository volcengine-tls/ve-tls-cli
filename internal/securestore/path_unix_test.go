//go:build !windows

package securestore

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
)

func TestEmptyStoreRootFailsClosedWithoutChangingCurrentDirectory(t *testing.T) {
	cwd := filepath.Join(t.TempDir(), "cwd")
	if err := os.Mkdir(cwd, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if err := os.Chmod(cwd, 0o755); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	restoreWorkingDirectory(t, cwd)
	before, err := os.Stat(cwd)
	if err != nil {
		t.Fatalf("Stat before: %v", err)
	}

	err = New("").WriteJSON("sso", "key", map[string]bool{"ok": true})
	if !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("WriteJSON error=%v, want ErrInvalidPath", err)
	}
	after, err := os.Stat(cwd)
	if err != nil {
		t.Fatalf("Stat after: %v", err)
	}
	if !os.SameFile(before, after) || after.Mode().Perm() != before.Mode().Perm() {
		t.Fatalf("current directory changed: before=%v after=%v", before.Mode(), after.Mode())
	}
}

func TestRelativeStoreRootIsFrozenAtConstruction(t *testing.T) {
	base := t.TempDir()
	other := t.TempDir()
	restoreWorkingDirectory(t, base)
	store := New("state")
	if !filepath.IsAbs(store.root) {
		t.Fatalf("store root=%q, want absolute path", store.root)
	}
	if err := os.Chdir(other); err != nil {
		t.Fatalf("Chdir other: %v", err)
	}
	if err := store.WriteJSON("sso", "key", map[string]bool{"ok": true}); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "state", "sso")); err != nil {
		t.Fatalf("frozen root was not used: %v", err)
	}
	if _, err := os.Stat(filepath.Join(other, "state")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("store drifted after chdir: %v", err)
	}
}

func TestStoreCanonicalizesExistingAncestorAtConstruction(t *testing.T) {
	parent := t.TempDir()
	realParent := filepath.Join(parent, "real")
	if err := os.Mkdir(realParent, 0o700); err != nil {
		t.Fatalf("Mkdir real parent: %v", err)
	}
	aliasParent := filepath.Join(parent, "alias")
	if err := os.Symlink(realParent, aliasParent); err != nil {
		t.Fatalf("Symlink alias parent: %v", err)
	}

	store := New(filepath.Join(aliasParent, "future", "store"))
	wantRoot := filepath.Join(realParent, "future", "store")
	if store.root != wantRoot {
		t.Fatalf("store root=%q, want canonical frozen root %q", store.root, wantRoot)
	}
	if err := store.WriteJSON("sso", "key", map[string]bool{"ok": true}); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wantRoot, "sso")); err != nil {
		t.Fatalf("canonical root was not used: %v", err)
	}
}

func TestStoreRootDoesNotDriftWhenAncestorSymlinkIsRetargeted(t *testing.T) {
	parent := t.TempDir()
	first := filepath.Join(parent, "first")
	second := filepath.Join(parent, "second")
	for _, path := range []string{first, second} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatalf("Mkdir %q: %v", path, err)
		}
	}
	alias := filepath.Join(parent, "alias")
	if err := os.Symlink(first, alias); err != nil {
		t.Fatalf("Symlink first: %v", err)
	}
	store := New(filepath.Join(alias, "store"))
	if err := os.Remove(alias); err != nil {
		t.Fatalf("Remove alias: %v", err)
	}
	if err := os.Symlink(second, alias); err != nil {
		t.Fatalf("Symlink second: %v", err)
	}

	if err := store.WriteJSON("sso", "key", map[string]bool{"ok": true}); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	if _, err := os.Stat(filepath.Join(first, "store", "sso")); err != nil {
		t.Fatalf("frozen first root was not used: %v", err)
	}
	if _, err := os.Stat(filepath.Join(second, "store")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("store drifted to retargeted ancestor: %v", err)
	}
}

func TestWithLockAndCallbackWriteStayUnderFrozenRootAfterAncestorRetarget(t *testing.T) {
	parent := t.TempDir()
	first := filepath.Join(parent, "first")
	second := filepath.Join(parent, "second")
	for _, path := range []string{first, second} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatalf("Mkdir %q: %v", path, err)
		}
	}
	alias := filepath.Join(parent, "alias")
	if err := os.Symlink(first, alias); err != nil {
		t.Fatalf("Symlink first: %v", err)
	}
	store := New(filepath.Join(alias, "store"))

	err := store.WithLock(context.Background(), "sso", "key", func() error {
		if err := os.Remove(alias); err != nil {
			return err
		}
		if err := os.Symlink(second, alias); err != nil {
			return err
		}
		return store.WriteJSON("sso", "key", map[string]bool{"ok": true})
	})
	if err != nil {
		t.Fatalf("WithLock: %v", err)
	}

	firstStore := filepath.Join(first, "store")
	if _, err := os.Stat(filepath.Join(firstStore, ".locks")); err != nil {
		t.Fatalf("lock was not created under frozen root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(firstStore, "sso")); err != nil {
		t.Fatalf("data was not written under frozen root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(second, "store")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("callback write drifted away from lock root: %v", err)
	}
}

func TestStoreRejectsBrokenAndLoopingAncestorSymlinks(t *testing.T) {
	parent := t.TempDir()
	broken := filepath.Join(parent, "broken")
	if err := os.Symlink(filepath.Join(parent, "missing"), broken); err != nil {
		t.Fatalf("Symlink broken ancestor: %v", err)
	}
	if err := New(filepath.Join(broken, "store")).WriteJSON(
		"sso",
		"key",
		map[string]bool{"ok": true},
	); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("broken ancestor WriteJSON error=%v, want ErrInvalidPath", err)
	}

	loopA := filepath.Join(parent, "loop-a")
	loopB := filepath.Join(parent, "loop-b")
	if err := os.Symlink(loopB, loopA); err != nil {
		t.Fatalf("Symlink loop-a: %v", err)
	}
	if err := os.Symlink(loopA, loopB); err != nil {
		t.Fatalf("Symlink loop-b: %v", err)
	}
	called := false
	err := New(filepath.Join(loopA, "store")).WithLock(
		context.Background(),
		"sso",
		"key",
		func() error {
			called = true
			return nil
		},
	)
	if !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("looping ancestor WithLock error=%v, want ErrInvalidPath", err)
	}
	if called {
		t.Fatal("callback ran for looping ancestor")
	}
}

func TestFilesystemRootAndCurrentDirectoryStoreRootsFailClosed(t *testing.T) {
	cwd := filepath.Join(t.TempDir(), "cwd")
	if err := os.Mkdir(cwd, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if err := os.Chmod(cwd, 0o755); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	restoreWorkingDirectory(t, cwd)
	filesystemRoot := filepath.VolumeName(cwd) + string(os.PathSeparator)

	for _, root := range []string{filesystemRoot, cwd, "."} {
		store := New(root)
		err := store.WriteJSON("sso", "key", map[string]bool{"ok": true})
		if !errors.Is(err, ErrInvalidPath) {
			t.Fatalf("New(%q).WriteJSON error=%v, want ErrInvalidPath", root, err)
		}
	}
	info, err := os.Stat(cwd)
	if err != nil {
		t.Fatalf("Stat cwd: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Fatalf("cwd mode=%#o, want unchanged 0755", got)
	}
}

func TestExistingSharedStoreRootFailsClosedWithoutChmod(t *testing.T) {
	root := filepath.Join(t.TempDir(), "shared")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	err := New(root).WriteJSON("sso", "key", map[string]bool{"ok": true})
	if !errors.Is(err, ErrPermission) {
		t.Fatalf("WriteJSON error=%v, want ErrPermission", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		t.Fatalf("Stat root: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Fatalf("shared root mode=%#o, want unchanged 0755", got)
	}
	if _, err := os.Stat(filepath.Join(root, "sso")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("namespace was created under rejected root: %v", err)
	}
}

func TestStoreRejectsRootNamespaceAndDataSymlinks(t *testing.T) {
	t.Run("root", func(t *testing.T) {
		parent := t.TempDir()
		outside := filepath.Join(parent, "outside")
		if err := os.Mkdir(outside, 0o700); err != nil {
			t.Fatalf("Mkdir outside: %v", err)
		}
		link := filepath.Join(parent, "root-link")
		if err := os.Symlink(outside, link); err != nil {
			t.Fatalf("Symlink root: %v", err)
		}
		err := New(link).WriteJSON("sso", "key", map[string]bool{"ok": true})
		if !errors.Is(err, ErrInvalidPath) {
			t.Fatalf("WriteJSON error=%v, want ErrInvalidPath", err)
		}
		if _, err := os.Stat(filepath.Join(outside, "sso")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("root symlink escaped store: %v", err)
		}
	})

	t.Run("namespace", func(t *testing.T) {
		parent := t.TempDir()
		root := filepath.Join(parent, "root")
		outside := filepath.Join(parent, "outside")
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatalf("Mkdir root: %v", err)
		}
		if err := os.Mkdir(outside, 0o700); err != nil {
			t.Fatalf("Mkdir outside: %v", err)
		}
		if err := os.Symlink(outside, filepath.Join(root, "sso")); err != nil {
			t.Fatalf("Symlink namespace: %v", err)
		}
		err := New(root).WriteJSON("sso", "key", map[string]bool{"ok": true})
		if !errors.Is(err, ErrInvalidPath) {
			t.Fatalf("WriteJSON error=%v, want ErrInvalidPath", err)
		}
		entries, err := os.ReadDir(outside)
		if err != nil {
			t.Fatalf("ReadDir outside: %v", err)
		}
		if len(entries) != 0 {
			t.Fatalf("namespace symlink escaped store: %v", entries)
		}
	})

	t.Run("data", func(t *testing.T) {
		parent := t.TempDir()
		root := filepath.Join(parent, "root")
		namespace := filepath.Join(root, "sso")
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatalf("Mkdir root: %v", err)
		}
		if err := os.Mkdir(namespace, 0o700); err != nil {
			t.Fatalf("Mkdir namespace: %v", err)
		}
		target := filepath.Join(parent, "outside.json")
		if err := os.WriteFile(target, []byte(`{"value":"outside"}`), 0o600); err != nil {
			t.Fatalf("WriteFile target: %v", err)
		}
		store := New(root)
		link := store.dataPath("sso", "key")
		if err := os.Symlink(target, link); err != nil {
			t.Fatalf("Symlink data: %v", err)
		}

		var out map[string]string
		if err := store.ReadJSON("sso", "key", &out); !errors.Is(err, ErrInvalidPath) {
			t.Fatalf("ReadJSON error=%v, want ErrInvalidPath", err)
		}
		if err := store.WriteJSON("sso", "key", map[string]string{"value": "new"}); !errors.Is(err, ErrInvalidPath) {
			t.Fatalf("WriteJSON error=%v, want ErrInvalidPath", err)
		}
		if err := store.Delete("sso", "key"); !errors.Is(err, ErrInvalidPath) {
			t.Fatalf("Delete error=%v, want ErrInvalidPath", err)
		}
		data, err := os.ReadFile(target)
		if err != nil {
			t.Fatalf("ReadFile target: %v", err)
		}
		if got := string(data); got != `{"value":"outside"}` {
			t.Fatalf("outside target changed: %q", got)
		}
		info, err := os.Lstat(link)
		if err != nil {
			t.Fatalf("Lstat data link: %v", err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Fatal("data symlink was removed or replaced")
		}
	})
}

func TestStoreDeleteRejectsSharedDataFileWithoutRemovingIt(t *testing.T) {
	root := filepath.Join(t.TempDir(), "store")
	namespace := filepath.Join(root, "sso")
	if err := os.MkdirAll(namespace, 0o700); err != nil {
		t.Fatalf("MkdirAll namespace: %v", err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatalf("Chmod root: %v", err)
	}
	if err := os.Chmod(namespace, 0o700); err != nil {
		t.Fatalf("Chmod namespace: %v", err)
	}
	store := New(root)
	dataPath := store.dataPath("sso", "identity")
	if err := os.WriteFile(dataPath, []byte(`{"token":"shared"}`), 0o644); err != nil {
		t.Fatalf("WriteFile data: %v", err)
	}
	if err := os.Chmod(dataPath, 0o644); err != nil {
		t.Fatalf("Chmod data: %v", err)
	}

	err := store.Delete("sso", "identity")
	if !errors.Is(err, ErrPermission) {
		t.Fatalf("Delete error=%v, want ErrPermission", err)
	}
	info, err := os.Stat(dataPath)
	if err != nil {
		t.Fatalf("shared data file was removed: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Fatalf("shared data mode=%#o, want unchanged 0644", got)
	}
}

func TestUpdateFileDoesNotChmodExistingParentAndSecuresNewParent(t *testing.T) {
	// Existing parent with 0755 (too permissive): UpdateFile must fail closed with
	// ErrPermission, must not create or modify the target file, and must not chmod
	// the existing parent.
	existingParent := filepath.Join(t.TempDir(), "shared")
	if err := os.Mkdir(existingParent, 0o755); err != nil {
		t.Fatalf("Mkdir existing parent: %v", err)
	}
	if err := os.Chmod(existingParent, 0o755); err != nil {
		t.Fatalf("Chmod existing parent: %v", err)
	}
	existingPath := filepath.Join(existingParent, "config.json")
	err := UpdateFile(existingPath, 0o600, func([]byte) ([]byte, error) {
		return []byte("existing"), nil
	})
	if !errors.Is(err, ErrPermission) {
		t.Fatalf("UpdateFile with 0755 parent: err=%v, want errors.Is(err, ErrPermission)", err)
	}
	// Target file must not have been created.
	if _, statErr := os.Stat(existingPath); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("target file should not exist after failed update, stat err=%v", statErr)
	}
	// Parent mode must remain unchanged (no chmod).
	parentInfo, err := os.Stat(existingParent)
	if err != nil {
		t.Fatalf("Stat existing parent: %v", err)
	}
	if got := parentInfo.Mode().Perm(); got != 0o755 {
		t.Fatalf("existing parent mode=%#o, want unchanged 0755", got)
	}

	// Existing parent with 0700 (correct): UpdateFile must succeed.
	secureParent := filepath.Join(t.TempDir(), "secure")
	if err := os.Mkdir(secureParent, 0o700); err != nil {
		t.Fatalf("Mkdir secure parent: %v", err)
	}
	securePath := filepath.Join(secureParent, "config.json")
	if err := UpdateFile(securePath, 0o600, func([]byte) ([]byte, error) {
		return []byte("secure"), nil
	}); err != nil {
		t.Fatalf("UpdateFile with 0700 parent: %v", err)
	}
	fileInfo, err := os.Stat(securePath)
	if err != nil {
		t.Fatalf("Stat config: %v", err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("config mode=%#o, want 0600", got)
	}

	// New parent must be created with 0700.
	newParent := filepath.Join(t.TempDir(), "new-parent")
	newPath := filepath.Join(newParent, "config.json")
	if err := UpdateFile(newPath, 0o600, func([]byte) ([]byte, error) {
		return []byte("new"), nil
	}); err != nil {
		t.Fatalf("UpdateFile new parent: %v", err)
	}
	parentInfo, err = os.Stat(newParent)
	if err != nil {
		t.Fatalf("Stat new parent: %v", err)
	}
	if got := parentInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("new parent mode=%#o, want 0700", got)
	}
}

func TestUpdateFileCanonicalizesTargetSymlinkAndPreservesAlias(t *testing.T) {
	parent := t.TempDir()
	realPath := filepath.Join(parent, "real.json")
	aliasPath := filepath.Join(parent, "alias.json")
	if err := os.WriteFile(realPath, []byte("old"), 0o600); err != nil {
		t.Fatalf("WriteFile real: %v", err)
	}
	if err := os.Symlink(realPath, aliasPath); err != nil {
		t.Fatalf("Symlink alias: %v", err)
	}

	if err := UpdateFile(aliasPath, 0o600, func(current []byte) ([]byte, error) {
		if got := string(current); got != "old" {
			t.Fatalf("current=%q, want old", got)
		}
		return []byte("new"), nil
	}); err != nil {
		t.Fatalf("UpdateFile alias: %v", err)
	}
	data, err := os.ReadFile(realPath)
	if err != nil {
		t.Fatalf("ReadFile real: %v", err)
	}
	if got := string(data); got != "new" {
		t.Fatalf("real content=%q, want new", got)
	}
	info, err := os.Lstat(aliasPath)
	if err != nil {
		t.Fatalf("Lstat alias: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("UpdateFile replaced the alias symlink")
	}
}

func TestConcurrentUpdateFileAliasAndRealPathShareOneLock(t *testing.T) {
	parent := t.TempDir()
	realPath := filepath.Join(parent, "real")
	aliasPath := filepath.Join(parent, "alias")
	if err := os.WriteFile(realPath, []byte("0"), 0o600); err != nil {
		t.Fatalf("WriteFile real: %v", err)
	}
	if err := os.Symlink(realPath, aliasPath); err != nil {
		t.Fatalf("Symlink alias: %v", err)
	}

	const updates = 20
	var wg sync.WaitGroup
	errs := make(chan error, updates)
	for i := range updates {
		wg.Add(1)
		go func() {
			defer wg.Done()
			path := realPath
			if i%2 == 0 {
				path = aliasPath
			}
			errs <- UpdateFile(path, 0o600, func(current []byte) ([]byte, error) {
				value, err := strconv.Atoi(string(current))
				if err != nil {
					return nil, err
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
	data, err := os.ReadFile(realPath)
	if err != nil {
		t.Fatalf("ReadFile real: %v", err)
	}
	if got := string(data); got != strconv.Itoa(updates) {
		t.Fatalf("counter=%q, want %d", got, updates)
	}
	info, err := os.Lstat(aliasPath)
	if err != nil {
		t.Fatalf("Lstat alias: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("concurrent UpdateFile replaced the alias symlink")
	}
}

func TestUpdateFileRejectsBrokenAndLoopingTargetSymlinks(t *testing.T) {
	parent := t.TempDir()
	broken := filepath.Join(parent, "broken")
	if err := os.Symlink(filepath.Join(parent, "missing"), broken); err != nil {
		t.Fatalf("Symlink broken: %v", err)
	}
	if err := UpdateFile(broken, 0o600, func([]byte) ([]byte, error) {
		return []byte("must-not-write"), nil
	}); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("UpdateFile broken error=%v, want ErrInvalidPath", err)
	}
	if info, err := os.Lstat(broken); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("broken symlink was replaced: info=%v err=%v", info, err)
	}

	loopA := filepath.Join(parent, "loop-a")
	loopB := filepath.Join(parent, "loop-b")
	if err := os.Symlink(loopB, loopA); err != nil {
		t.Fatalf("Symlink loop-a: %v", err)
	}
	if err := os.Symlink(loopA, loopB); err != nil {
		t.Fatalf("Symlink loop-b: %v", err)
	}
	if err := UpdateFile(loopA, 0o600, func([]byte) ([]byte, error) {
		return []byte("must-not-write"), nil
	}); err == nil {
		t.Fatal("UpdateFile looping symlink unexpectedly succeeded")
	}
	if info, err := os.Lstat(loopA); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("looping symlink was replaced: info=%v err=%v", info, err)
	}
}

func TestUpdateFileRejectsSymlinkLockFile(t *testing.T) {
	parent := t.TempDir()
	path := filepath.Join(parent, "config.json")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}
	outsideLock := filepath.Join(parent, "outside.lock")
	if err := os.WriteFile(outsideLock, []byte("outside"), 0o644); err != nil {
		t.Fatalf("WriteFile outside lock: %v", err)
	}
	if err := os.Chmod(outsideLock, 0o644); err != nil {
		t.Fatalf("Chmod outside lock: %v", err)
	}
	if err := os.Symlink(outsideLock, path+".lock"); err != nil {
		t.Fatalf("Symlink lock: %v", err)
	}
	err := UpdateFile(path, 0o600, func([]byte) ([]byte, error) {
		return []byte("new"), nil
	})
	if !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("UpdateFile error=%v, want ErrInvalidPath", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile config: %v", err)
	}
	if got := string(data); got != "old" {
		t.Fatalf("config changed through symlink lock rejection: %q", got)
	}
	info, err := os.Stat(outsideLock)
	if err != nil {
		t.Fatalf("Stat outside lock: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Fatalf("outside lock mode=%#o, want unchanged 0644", got)
	}
}

func restoreWorkingDirectory(t *testing.T, next string) {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(next); err != nil {
		t.Fatalf("Chdir %q: %v", next, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})
}

func TestValidatePrivateFile(t *testing.T) {
	// Missing file must return a not-exist error.
	missing := filepath.Join(t.TempDir(), "missing")
	err := ValidatePrivateFile(missing)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing file: err=%v, want errors.Is(err, fs.ErrNotExist)", err)
	}

	// 0600 file must pass.
	good := filepath.Join(t.TempDir(), "good")
	if err := os.WriteFile(good, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := ValidatePrivateFile(good); err != nil {
		t.Fatalf("0600 file: err=%v, want nil", err)
	}

	// 0644 (broad) file must fail with ErrPermission, must not be chmod'd.
	broad := filepath.Join(t.TempDir(), "broad")
	if err := os.WriteFile(broad, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Chmod(broad, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	err = ValidatePrivateFile(broad)
	if !errors.Is(err, ErrPermission) {
		t.Fatalf("0644 file: err=%v, want errors.Is(err, ErrPermission)", err)
	}
	info, _ := os.Stat(broad)
	if got := info.Mode().Perm(); got != 0o644 {
		t.Fatalf("broad file mode changed to %#o, want 0644 (no chmod)", got)
	}

	// Symlink must fail with ErrInvalidPath.
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(good, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	err = ValidatePrivateFile(link)
	if !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("symlink: err=%v, want errors.Is(err, ErrInvalidPath)", err)
	}

	// Directory must fail with ErrInvalidPath.
	dir := t.TempDir()
	err = ValidatePrivateFile(dir)
	if !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("directory: err=%v, want errors.Is(err, ErrInvalidPath)", err)
	}
}
