//go:build windows

package securestore

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	accessAllowedACEType = "A"
	fileAllAccessRights  = "FA"
	privateTempAttempts  = 100
)

var (
	windowsCreateDirectory = windows.CreateDirectory
	windowsCreateFile      = windows.CreateFile
)

type windowsACLSpec struct {
	aceType             string
	flags               string
	rights              string
	objectType          string
	inheritedObjectType string
	sid                 string
}

func createPrivateDirectories(path string, perm fs.FileMode) error {
	var missing []string
	current := path
	for {
		info, err := os.Lstat(current)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return ErrInvalidPath
			}
			if current == path {
				return secureExistingPrivatePath(path, true, perm)
			}
			break
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return ErrInvalidPath
		}
		missing = append(missing, current)
		current = parent
	}
	for i := len(missing) - 1; i >= 0; i-- {
		if err := createPrivateDirectory(missing[i], perm); err != nil {
			if !errors.Is(err, windows.ERROR_ALREADY_EXISTS) &&
				!errors.Is(err, windows.ERROR_FILE_EXISTS) {
				return err
			}
			if err := secureExistingPrivatePath(missing[i], true, perm); err != nil {
				return err
			}
		}
	}
	return secureExistingPrivatePath(path, true, perm)
}

func createPrivateDirectory(path string, _ fs.FileMode) error {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	attributes, err := privateWindowsSecurityAttributes(true)
	if err != nil {
		return err
	}
	return windowsCreateDirectory(name, attributes)
}

func createPrivateTempFile(dir, prefix string, _ fs.FileMode) (*os.File, error) {
	attributes, err := privateWindowsSecurityAttributes(false)
	if err != nil {
		return nil, err
	}
	var random [16]byte
	for range privateTempAttempts {
		if _, err := rand.Read(random[:]); err != nil {
			return nil, err
		}
		path := filepath.Join(dir, prefix+hex.EncodeToString(random[:]))
		name, err := windows.UTF16PtrFromString(path)
		if err != nil {
			return nil, err
		}
		handle, err := windowsCreateFile(
			name,
			windows.GENERIC_READ|windows.GENERIC_WRITE,
			windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
			attributes,
			windows.CREATE_NEW,
			windows.FILE_ATTRIBUTE_NORMAL,
			0,
		)
		if errors.Is(err, windows.ERROR_FILE_EXISTS) ||
			errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
			continue
		}
		if err != nil {
			return nil, err
		}
		file := os.NewFile(uintptr(handle), path)
		if file == nil {
			_ = windows.CloseHandle(handle)
			return nil, errors.New("wrap private Windows temp file handle")
		}
		return file, nil
	}
	return nil, fs.ErrExist
}

func openPrivateLockFile(path string, _ fs.FileMode) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	attributes, err := privateWindowsSecurityAttributes(false)
	if err != nil {
		return nil, err
	}
	handle, err := windowsCreateFile(
		name,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		attributes,
		windows.CREATE_NEW,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	created := err == nil
	if errors.Is(err, windows.ERROR_FILE_EXISTS) ||
		errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		handle, err = windowsCreateFile(
			name,
			windows.GENERIC_READ|windows.GENERIC_WRITE,
			windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
			nil,
			windows.OPEN_EXISTING,
			windows.FILE_ATTRIBUTE_NORMAL,
			0,
		)
	}
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(handle), path)
	if file == nil {
		_ = windows.CloseHandle(handle)
		return nil, errors.New("wrap private Windows lock file handle")
	}
	if !created {
		if err := secureExistingPrivatePath(path, false, 0o600); err != nil {
			_ = file.Close()
			return nil, err
		}
	}
	return file, nil
}

func applyPrivatePermissions(path string, isDir bool, _ fs.FileMode) error {
	descriptor, err := privateWindowsSecurityDescriptor(isDir)
	if err != nil {
		return err
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return err
	}
	if dacl == nil {
		return errors.New("private Windows security descriptor has no DACL")
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
		return err
	}
	return verifyPrivateWindowsACL(path, isDir)
}

func secureExistingPrivatePath(path string, isDir bool, _ fs.FileMode) error {
	return verifyPrivateWindowsACL(path, isDir)
}

func protectExistingUpdateTarget(path string, perm fs.FileMode) error {
	_, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return applyPrivatePermissions(path, false, perm)
}

func privateWindowsSecurityAttributes(isDir bool) (*windows.SecurityAttributes, error) {
	descriptor, err := privateWindowsSecurityDescriptor(isDir)
	if err != nil {
		return nil, err
	}
	return &windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: descriptor,
	}, nil
}

func privateWindowsSecurityDescriptor(isDir bool) (*windows.SECURITY_DESCRIPTOR, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return nil, err
	}
	administrators, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return nil, err
	}
	flags := ""
	if isDir {
		flags = "OICI"
	}
	var dacl strings.Builder
	dacl.WriteString("D:P")
	seen := make(map[string]struct{}, 3)
	for _, sid := range []*windows.SID{user.User.Sid, system, administrators} {
		value := sid.String()
		if value == "" {
			return nil, errors.New("private Windows trustee SID is empty")
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		fmt.Fprintf(&dacl, "(A;%s;FA;;;%s)", flags, value)
	}
	return windows.SecurityDescriptorFromString(dacl.String())
}

func verifyPrivateWindowsACL(path string, isDir bool) error {
	descriptor, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		return err
	}
	return verifyPrivateWindowsDescriptor(descriptor, isDir, path)
}

func verifyPrivateWindowsDescriptor(
	descriptor *windows.SECURITY_DESCRIPTOR,
	isDir bool,
	path string,
) error {
	control, _, err := descriptor.Control()
	if err != nil {
		return err
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		return fmt.Errorf("%w: Windows DACL is not protected: %s", ErrPermission, path)
	}
	specs, err := windowsACLSpecs(descriptor)
	if err != nil {
		return fmt.Errorf("%w: %v: %s", ErrPermission, err, path)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return err
	}
	administrators, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return err
	}
	expected := make(map[string]bool, 3)
	for _, sid := range []*windows.SID{user.User.Sid, system, administrators} {
		expected[sid.String()] = false
	}
	if len(specs) != len(expected) {
		return fmt.Errorf(
			"%w: Windows DACL has %d ACEs, want %d: %s",
			ErrPermission,
			len(specs),
			len(expected),
			path,
		)
	}
	wantFlags := ""
	if isDir {
		wantFlags = "OICI"
	}
	for _, spec := range specs {
		if spec.aceType != accessAllowedACEType ||
			spec.flags != wantFlags ||
			spec.rights != fileAllAccessRights ||
			spec.objectType != "" ||
			spec.inheritedObjectType != "" {
			return fmt.Errorf("%w: unexpected Windows ACE %#v: %s", ErrPermission, spec, path)
		}
		seen, ok := expected[spec.sid]
		if !ok || seen {
			return fmt.Errorf(
				"%w: unexpected or duplicate Windows trustee %q: %s",
				ErrPermission,
				spec.sid,
				path,
			)
		}
		expected[spec.sid] = true
	}
	for sid, seen := range expected {
		if !seen {
			return fmt.Errorf("%w: missing Windows trustee %q: %s", ErrPermission, sid, path)
		}
	}
	return nil
}

func windowsACLSpecs(descriptor *windows.SECURITY_DESCRIPTOR) ([]windowsACLSpec, error) {
	sddl := descriptor.String()
	daclIndex := strings.Index(sddl, "D:")
	if daclIndex < 0 {
		return nil, errors.New("Windows DACL is missing")
	}
	dacl := sddl[daclIndex+2:]
	specs := make([]windowsACLSpec, 0, 3)
	for {
		open := strings.IndexByte(dacl, '(')
		if open < 0 {
			break
		}
		closeOffset := strings.IndexByte(dacl[open+1:], ')')
		if closeOffset < 0 {
			return nil, errors.New("Windows ACE is unterminated")
		}
		closeIndex := open + 1 + closeOffset
		fields := strings.Split(dacl[open+1:closeIndex], ";")
		if len(fields) != 6 {
			return nil, errors.New("Windows ACE has an unexpected SDDL shape")
		}
		trustee, err := canonicalWindowsTrustee(fields[5])
		if err != nil {
			return nil, err
		}
		spec := windowsACLSpec{
			aceType:             fields[0],
			flags:               fields[1],
			rights:              fields[2],
			objectType:          fields[3],
			inheritedObjectType: fields[4],
			sid:                 trustee,
		}
		specs = append(specs, spec)
		dacl = dacl[closeIndex+1:]
	}
	return specs, nil
}

func canonicalWindowsTrustee(value string) (string, error) {
	switch value {
	case "SY":
		sid, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
		if err != nil {
			return "", err
		}
		return sid.String(), nil
	case "BA":
		sid, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
		if err != nil {
			return "", err
		}
		return sid.String(), nil
	default:
		if !strings.HasPrefix(value, "S-") {
			return value, nil
		}
		sid, err := windows.StringToSid(value)
		if err != nil {
			return "", err
		}
		return sid.String(), nil
	}
}
