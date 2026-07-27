//go:build windows

package securestore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsStoreAndConfigPathsUseProtectedPrivateDACLs(t *testing.T) {
	root := filepath.Join(t.TempDir(), "store")
	store := New(root)
	store.replace = func(source, target string) error {
		assertWindowsPrivateDACL(t, source, false)
		return replaceFile(source, target)
	}
	if err := store.WriteJSON("sso", "identity", map[string]string{"token": "secret"}); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	if err := store.WithLock(context.Background(), "sso", "identity", func() error {
		return nil
	}); err != nil {
		t.Fatalf("WithLock: %v", err)
	}
	lockPath, err := store.lockPath("sso", "identity")
	if err != nil {
		t.Fatalf("lockPath: %v", err)
	}
	for _, item := range []struct {
		path  string
		isDir bool
	}{
		{path: root, isDir: true},
		{path: filepath.Join(root, "sso"), isDir: true},
		{path: store.dataPath("sso", "identity")},
		{path: filepath.Join(root, ".locks"), isDir: true},
		{path: lockPath},
	} {
		assertWindowsPrivateDACL(t, item.path, item.isDir)
	}

	// Create a protected private parent directory first; t.TempDir() may not
	// have an exact private DACL on Windows.
	configDir := filepath.Join(t.TempDir(), "cfg")
	if err := createPrivateDirectory(configDir, 0o700); err != nil {
		t.Fatalf("createPrivateDirectory: %v", err)
	}
	configPath := filepath.Join(configDir, "config.json")
	if err := UpdateFile(configPath, 0o600, func([]byte) ([]byte, error) {
		return []byte(`{"secret":"value"}`), nil
	}); err != nil {
		t.Fatalf("UpdateFile: %v", err)
	}
	assertWindowsPrivateDACL(t, configPath, false)
}

func TestWindowsStoreRejectsSharedExistingPathsWithoutChangingDACL(t *testing.T) {
	t.Run("root", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "store")
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatalf("Mkdir root: %v", err)
		}
		setBroadWindowsDACL(t, root, true)
		before := windowsDACLString(t, root)

		err := New(root).WriteJSON("sso", "identity", map[string]string{"token": "secret"})
		if !errors.Is(err, ErrPermission) {
			t.Fatalf("WriteJSON error=%v, want ErrPermission", err)
		}
		if after := windowsDACLString(t, root); after != before {
			t.Fatalf("root DACL changed: before=%q after=%q", before, after)
		}
	})

	t.Run("namespace", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "store")
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatalf("Mkdir root: %v", err)
		}
		if err := applyPrivatePermissions(root, true, 0o700); err != nil {
			t.Fatalf("secure root: %v", err)
		}
		namespace := filepath.Join(root, "sso")
		if err := os.Mkdir(namespace, 0o700); err != nil {
			t.Fatalf("Mkdir namespace: %v", err)
		}
		setBroadWindowsDACL(t, namespace, true)
		before := windowsDACLString(t, namespace)

		err := New(root).WriteJSON("sso", "identity", map[string]string{"token": "secret"})
		if !errors.Is(err, ErrPermission) {
			t.Fatalf("WriteJSON error=%v, want ErrPermission", err)
		}
		if after := windowsDACLString(t, namespace); after != before {
			t.Fatalf("namespace DACL changed: before=%q after=%q", before, after)
		}
	})

	t.Run("data file", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "store")
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatalf("Mkdir root: %v", err)
		}
		if err := applyPrivatePermissions(root, true, 0o700); err != nil {
			t.Fatalf("secure root: %v", err)
		}
		namespace := filepath.Join(root, "sso")
		if err := os.Mkdir(namespace, 0o700); err != nil {
			t.Fatalf("Mkdir namespace: %v", err)
		}
		if err := applyPrivatePermissions(namespace, true, 0o700); err != nil {
			t.Fatalf("secure namespace: %v", err)
		}
		store := New(root)
		dataPath := store.dataPath("sso", "identity")
		if err := os.WriteFile(dataPath, []byte(`{"token":"shared"}`), 0o600); err != nil {
			t.Fatalf("WriteFile data: %v", err)
		}
		setBroadWindowsDACL(t, dataPath, false)
		before := windowsDACLString(t, dataPath)

		var value map[string]string
		err := store.ReadJSON("sso", "identity", &value)
		if !errors.Is(err, ErrPermission) {
			t.Fatalf("ReadJSON error=%v, want ErrPermission", err)
		}
		if after := windowsDACLString(t, dataPath); after != before {
			t.Fatalf("data DACL changed after ReadJSON: before=%q after=%q", before, after)
		}
		err = store.WriteJSON("sso", "identity", map[string]string{"token": "new"})
		if !errors.Is(err, ErrPermission) {
			t.Fatalf("WriteJSON error=%v, want ErrPermission", err)
		}
		if after := windowsDACLString(t, dataPath); after != before {
			t.Fatalf("data DACL changed after WriteJSON: before=%q after=%q", before, after)
		}
		err = store.Delete("sso", "identity")
		if !errors.Is(err, ErrPermission) {
			t.Fatalf("Delete error=%v, want ErrPermission", err)
		}
		if after := windowsDACLString(t, dataPath); after != before {
			t.Fatalf("data DACL changed after Delete: before=%q after=%q", before, after)
		}
	})
}

func TestWindowsPrivateCreationPassesSecurityAttributesToSyscalls(t *testing.T) {
	t.Run("directory", func(t *testing.T) {
		original := windowsCreateDirectory
		t.Cleanup(func() { windowsCreateDirectory = original })
		called := false
		windowsCreateDirectory = func(_ *uint16, attributes *windows.SecurityAttributes) error {
			called = true
			assertWindowsSecurityAttributes(t, attributes, true)
			return windows.ERROR_ACCESS_DENIED
		}

		err := createPrivateDirectory(filepath.Join(t.TempDir(), "private"), 0o700)
		if !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			t.Fatalf("createPrivateDirectory error=%v, want access denied", err)
		}
		if !called {
			t.Fatal("CreateDirectoryW seam was not called")
		}
	})

	t.Run("temp file retries unique names", func(t *testing.T) {
		original := windowsCreateFile
		t.Cleanup(func() { windowsCreateFile = original })
		var names []string
		windowsCreateFile = func(
			name *uint16,
			_ uint32,
			_ uint32,
			attributes *windows.SecurityAttributes,
			createMode uint32,
			_ uint32,
			_ windows.Handle,
		) (windows.Handle, error) {
			if createMode != windows.CREATE_NEW {
				t.Fatalf("temp create mode=%d, want CREATE_NEW", createMode)
			}
			assertWindowsSecurityAttributes(t, attributes, false)
			names = append(names, windows.UTF16PtrToString(name))
			if len(names) == 1 {
				return windows.InvalidHandle, windows.ERROR_FILE_EXISTS
			}
			return windows.InvalidHandle, windows.ERROR_ACCESS_DENIED
		}

		_, err := createPrivateTempFile(t.TempDir(), ".secret.tmp-", 0o600)
		if !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			t.Fatalf("createPrivateTempFile error=%v, want access denied", err)
		}
		if len(names) != 2 {
			t.Fatalf("CreateFileW calls=%d, want 2", len(names))
		}
		if names[0] == names[1] {
			t.Fatalf("temp retry reused name %q", names[0])
		}
	})

	t.Run("lock file", func(t *testing.T) {
		original := windowsCreateFile
		t.Cleanup(func() { windowsCreateFile = original })
		called := false
		windowsCreateFile = func(
			_ *uint16,
			_ uint32,
			_ uint32,
			attributes *windows.SecurityAttributes,
			createMode uint32,
			_ uint32,
			_ windows.Handle,
		) (windows.Handle, error) {
			called = true
			if createMode != windows.CREATE_NEW {
				t.Fatalf("lock create mode=%d, want CREATE_NEW", createMode)
			}
			assertWindowsSecurityAttributes(t, attributes, false)
			return windows.InvalidHandle, windows.ERROR_ACCESS_DENIED
		}

		_, err := openPrivateLockFile(filepath.Join(t.TempDir(), "state.lock"), 0o600)
		if !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			t.Fatalf("openPrivateLockFile error=%v, want access denied", err)
		}
		if !called {
			t.Fatal("CreateFileW seam was not called")
		}
	})
}

func assertWindowsSecurityAttributes(
	t *testing.T,
	attributes *windows.SecurityAttributes,
	isDir bool,
) {
	t.Helper()
	if attributes == nil || attributes.SecurityDescriptor == nil {
		t.Fatal("security attributes or descriptor is nil")
	}
	assertWindowsPrivateDescriptor(t, attributes.SecurityDescriptor, isDir)
}

func assertWindowsPrivateDACL(t *testing.T, path string, isDir bool) {
	t.Helper()
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("Lstat %q: %v", path, err)
	}
	descriptor, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		t.Fatalf("GetNamedSecurityInfo %q: %v", path, err)
	}
	assertWindowsPrivateDescriptor(t, descriptor, isDir)
}

func assertWindowsPrivateDescriptor(
	t *testing.T,
	descriptor *windows.SECURITY_DESCRIPTOR,
	isDir bool,
) {
	t.Helper()
	control, _, err := descriptor.Control()
	if err != nil {
		t.Fatalf("Control: %v", err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatal("DACL is not protected")
	}
	specs, err := windowsACLSpecs(descriptor)
	if err != nil {
		t.Fatalf("windowsACLSpecs: %v", err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatalf("GetTokenUser: %v", err)
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		t.Fatalf("CreateWellKnownSid system: %v", err)
	}
	administrators, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		t.Fatalf("CreateWellKnownSid administrators: %v", err)
	}
	expectedSIDs := map[string]bool{
		user.User.Sid.String():  false,
		system.String():         false,
		administrators.String(): false,
	}
	if len(specs) != len(expectedSIDs) {
		t.Fatalf("ACE count=%d, want exactly %d: %#v", len(specs), len(expectedSIDs), specs)
	}
	wantFlags := ""
	if isDir {
		wantFlags = "OICI"
	}
	for _, spec := range specs {
		if spec.aceType != accessAllowedACEType {
			t.Fatalf("ACE type=%q, want ACCESS_ALLOWED: %#v", spec.aceType, spec)
		}
		if spec.flags != wantFlags {
			t.Fatalf("ACE flags=%q, want %q: %#v", spec.flags, wantFlags, spec)
		}
		if spec.rights != fileAllAccessRights {
			t.Fatalf("ACE rights=%q, want FILE_ALL_ACCESS %q: %#v", spec.rights, fileAllAccessRights, spec)
		}
		if _, ok := expectedSIDs[spec.sid]; !ok {
			t.Fatalf("unexpected trustee %q: %#v", spec.sid, specs)
		}
		if expectedSIDs[spec.sid] {
			t.Fatalf("duplicate trustee %q: %#v", spec.sid, specs)
		}
		expectedSIDs[spec.sid] = true
	}
	for sid, seen := range expectedSIDs {
		if !seen {
			t.Fatalf("missing trustee %q: %#v", sid, specs)
		}
	}
}

func setBroadWindowsDACL(t *testing.T, path string, isDir bool) {
	t.Helper()
	flags := ""
	if isDir {
		flags = "OICI"
	}
	descriptor, err := windows.SecurityDescriptorFromString("D:P(A;" + flags + ";FA;;;WD)")
	if err != nil {
		t.Fatalf("SecurityDescriptorFromString broad: %v", err)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		t.Fatalf("DACL broad: %v", err)
	}
	if err := windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		dacl,
		nil,
	); err != nil {
		t.Fatalf("SetNamedSecurityInfo broad: %v", err)
	}
}

func windowsDACLString(t *testing.T, path string) string {
	t.Helper()
	descriptor, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		t.Fatalf("GetNamedSecurityInfo %q: %v", path, err)
	}
	return descriptor.String()
}

func TestValidatePrivateFileRejectsBroadDACL(t *testing.T) {
	// Missing file must return a not-exist error.
	missing := filepath.Join(t.TempDir(), "missing")
	err := ValidatePrivateFile(missing)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing file: err=%v, want errors.Is(err, os.ErrNotExist)", err)
	}

	// Private DACL file must pass. Create a protected private parent first,
	// then use UpdateFile to create the file with a protected private DACL.
	goodDir := filepath.Join(t.TempDir(), "good")
	if err := createPrivateDirectory(goodDir, 0o700); err != nil {
		t.Fatalf("createPrivateDirectory: %v", err)
	}
	good := filepath.Join(goodDir, "file")
	if err := UpdateFile(good, 0o600, func([]byte) ([]byte, error) {
		return []byte("x"), nil
	}); err != nil {
		t.Fatalf("UpdateFile: %v", err)
	}
	if err := ValidatePrivateFile(good); err != nil {
		t.Fatalf("private DACL file: err=%v, want nil", err)
	}

	// Broad DACL file must fail with ErrPermission and must not be modified.
	broad := filepath.Join(t.TempDir(), "broad")
	if err := os.WriteFile(broad, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	setBroadWindowsDACL(t, broad, false)
	before := windowsDACLString(t, broad)
	err = ValidatePrivateFile(broad)
	if !errors.Is(err, ErrPermission) {
		t.Fatalf("broad DACL file: err=%v, want errors.Is(err, ErrPermission)", err)
	}
	if after := windowsDACLString(t, broad); after != before {
		t.Fatalf("broad DACL changed: before=%q after=%q", before, after)
	}
}
