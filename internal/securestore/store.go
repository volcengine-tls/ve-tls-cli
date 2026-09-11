package securestore

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	ErrMissing     = errors.New("secure store entry missing")
	ErrCorrupt     = errors.New("secure store entry corrupt")
	ErrPermission  = errors.New("secure store permission denied")
	ErrInvalidPath = errors.New("secure store invalid namespace or key")
)

type classifiedError struct {
	kind error
	err  error
}

func (e *classifiedError) Error() string {
	return fmt.Sprintf("%v: %v", e.kind, e.err)
}

func (e *classifiedError) Unwrap() []error {
	return []error{e.kind, e.err}
}

type Store struct {
	root     string
	rootErr  error
	replace  func(source, target string) error
	readFile func(string) ([]byte, error)
}

func New(root string) *Store {
	store := &Store{
		replace:  replaceFile,
		readFile: os.ReadFile,
	}
	if strings.TrimSpace(root) == "" {
		store.rootErr = ErrInvalidPath
		return store
	}
	absolute, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		store.rootErr = errors.Join(ErrInvalidPath, err)
		return store
	}
	info, err := os.Lstat(absolute)
	if err == nil && info.Mode()&os.ModeSymlink != 0 {
		store.rootErr = ErrInvalidPath
		return store
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		store.rootErr = errors.Join(ErrInvalidPath, err)
		return store
	}
	canonical, err := canonicalMissingDirectory(absolute)
	if err != nil {
		store.rootErr = errors.Join(ErrInvalidPath, err)
		return store
	}
	current, err := os.Getwd()
	if err != nil {
		store.rootErr = err
		return store
	}
	current, err = filepath.EvalSymlinks(current)
	if err != nil {
		store.rootErr = err
		return store
	}
	if filepath.Dir(canonical) == canonical || samePath(canonical, current) {
		store.rootErr = ErrInvalidPath
		return store
	}
	store.root = canonical
	return store
}

// CanonicalRoot returns the already validated, canonical absolute root of the
// store, or the root error captured at construction time. It is a read-only
// accessor intended for callers (such as FileCache) that need to share the
// exact same root the store uses for locks, so the lock root and the data root
// can never diverge. It does not change normal Store read/write/lock semantics
// and exposes no mutable root state.
func (s *Store) CanonicalRoot() (string, error) {
	if s == nil {
		return "", ErrInvalidPath
	}
	if s.rootErr != nil {
		return "", s.rootErr
	}
	return s.root, nil
}

// ReadJSON is an unlocked file primitive. It is safe to call inside WithLock.
func (s *Store) ReadJSON(namespace, key string, out any) error {
	path, err := s.path(namespace, key)
	if err != nil {
		return err
	}
	if err := s.validateExistingNamespace(namespace); err != nil {
		return err
	}
	if err := validateStoreFile(path); err != nil {
		return err
	}
	data, err := s.readFile(path)
	if err != nil {
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return &classifiedError{kind: ErrMissing, err: err}
		case errors.Is(err, fs.ErrPermission):
			return &classifiedError{kind: ErrPermission, err: err}
		default:
			return err
		}
	}
	if err := json.Unmarshal(data, out); err != nil {
		return &classifiedError{kind: ErrCorrupt, err: err}
	}
	return nil
}

// WriteJSON is an unlocked file primitive. It atomically replaces one file,
// while compound read-refresh-write serialization belongs to WithLock.
func (s *Store) WriteJSON(namespace, key string, value any) error {
	path, err := s.path(namespace, key)
	if err != nil {
		return err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err := ensureStoreDirectory(s.root, true); err != nil {
		return err
	}
	if err := ensureStoreDirectory(filepath.Dir(path), false); err != nil {
		return err
	}
	if err := rejectSymlink(path); err != nil {
		return err
	}
	if err := validateStoreFile(path); err != nil {
		return err
	}
	return atomicWrite(path, 0o600, data, s.replace)
}

// Delete is an unlocked file primitive and deliberately does not reacquire the
// cache key lock, so callers may use it inside WithLock without self-deadlock.
func (s *Store) Delete(namespace, key string) error {
	path, err := s.path(namespace, key)
	if err != nil {
		return err
	}
	if err := s.validateExistingNamespace(namespace); err != nil {
		return err
	}
	if err := rejectSymlink(path); err != nil {
		return err
	}
	if err := validateStoreFile(path); err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// WithLock serializes compound cache operations for one namespace/key across
// Store instances and processes. Different digest keys use different locks.
func (s *Store) WithLock(ctx context.Context, namespace, key string, fn func() error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	lockPath, err := s.lockPath(namespace, key)
	if err != nil {
		return err
	}
	if err := ensureStoreDirectory(s.root, true); err != nil {
		return err
	}
	if err := ensureStoreDirectory(filepath.Dir(lockPath), false); err != nil {
		return err
	}
	if err := rejectSymlink(lockPath); err != nil {
		return err
	}
	return withPathLock(ctx, lockPath, fn)
}

// DigestKey returns a SHA-256 digest while preserving part boundaries.
func DigestKey(parts ...string) string {
	hash := sha256.New()
	var length [8]byte
	for _, part := range parts {
		binary.BigEndian.PutUint64(length[:], uint64(len(part)))
		_, _ = hash.Write(length[:])
		_, _ = hash.Write([]byte(part))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// UpdateFile performs one locked read-modify-write transaction for an explicit
// path. The callback must not recursively call UpdateFile or a Save function
// backed by UpdateFile for the same path.
func UpdateFile(path string, perm fs.FileMode, fn func(current []byte) ([]byte, error)) error {
	if strings.TrimSpace(path) == "" {
		return ErrInvalidPath
	}
	canonical, err := canonicalPath(path)
	if err != nil {
		return err
	}
	if err := ensureUpdateParent(filepath.Dir(canonical)); err != nil {
		return err
	}
	return withPathLock(context.Background(), canonical+".lock", func() error {
		if err := protectExistingUpdateTarget(canonical, perm); err != nil {
			return err
		}
		current, err := os.ReadFile(canonical)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if errors.Is(err, fs.ErrNotExist) {
			current = nil
		}
		var snapshot []byte
		if current != nil {
			snapshot = make([]byte, len(current))
			copy(snapshot, current)
		}
		next, err := fn(snapshot)
		if err != nil {
			return err
		}
		return atomicWrite(canonical, perm, next, replaceFile)
	})
}

func (s *Store) dataPath(namespace, key string) string {
	path, _ := s.path(namespace, key)
	return path
}

func (s *Store) path(namespace, key string) (string, error) {
	if s == nil || s.rootErr != nil {
		if s == nil {
			return "", ErrInvalidPath
		}
		return "", s.rootErr
	}
	if err := validatePart(namespace); err != nil {
		return "", err
	}
	if err := validatePart(key); err != nil {
		return "", err
	}
	return filepath.Join(s.root, namespace, DigestKey(namespace, key)+".json"), nil
}

func (s *Store) lockPath(namespace, key string) (string, error) {
	if s == nil || s.rootErr != nil {
		if s == nil {
			return "", ErrInvalidPath
		}
		return "", s.rootErr
	}
	if err := validatePart(namespace); err != nil {
		return "", err
	}
	if err := validatePart(key); err != nil {
		return "", err
	}
	return filepath.Join(s.root, ".locks", DigestKey(namespace, key)+".lock"), nil
}

func validatePart(value string) error {
	if value == "" || value == "." || value == ".." ||
		strings.Contains(value, "/") || strings.Contains(value, `\`) {
		return ErrInvalidPath
	}
	return nil
}

func atomicWrite(path string, perm fs.FileMode, data []byte, replace func(string, string) error) error {
	file, err := createPrivateTempFile(
		filepath.Dir(path),
		"."+filepath.Base(path)+".tmp-",
		perm,
	)
	if err != nil {
		return err
	}
	tempPath := file.Name()
	cleanup := func() {
		_ = file.Close()
		_ = os.Remove(tempPath)
	}
	defer cleanup()

	n, err := file.Write(data)
	if err != nil {
		return err
	}
	if n != len(data) {
		return io.ErrShortWrite
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := replace(tempPath, path); err != nil {
		return err
	}
	return syncParentDirectory(path)
}

type processLock struct {
	token chan struct{}
	refs  int
}

var processLockRegistry = struct {
	sync.Mutex
	entries map[string]*processLock
}{
	entries: make(map[string]*processLock),
}

var lockContendedHook = func(string) {}

// lockContentionObserverKey is the private context key for the diagnostic
// contention observer installed by WithLockContentionObserver.
type lockContentionObserverKey struct{}

// WithLockContentionObserver returns a derived context carrying observer, which
// is invoked at most once when the caller's WithLock attempt blocks on an
// in-process (same-process) lock already held by another goroutine. It is
// diagnostic and coordination observation only: observer must return promptly
// and must not acquire locks or perform blocking work. It does not change lock
// ownership or ordering. A nil ctx follows the existing Store.WithLock
// nil-context behavior; a nil observer is harmless.
func WithLockContentionObserver(ctx context.Context, observer func()) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if observer == nil {
		return ctx
	}
	return context.WithValue(ctx, lockContentionObserverKey{}, observer)
}

func withPathLock(ctx context.Context, path string, fn func() error) (err error) {
	canonical, err := canonicalLockPath(path)
	if err != nil {
		return err
	}
	lock := retainProcessLock(canonical)
	defer releaseProcessLock(canonical, lock)
	select {
	case lock.token <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	default:
		lockContendedHook(canonical)
		if obs, ok := ctx.Value(lockContentionObserverKey{}).(func()); ok && obs != nil {
			obs()
		}
		select {
		case lock.token <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	defer func() { <-lock.token }()

	release, err := acquireOSFileLock(ctx, canonical)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, release())
	}()
	return fn()
}

func canonicalLockPath(path string) (string, error) {
	info, err := os.Lstat(path)
	if err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", ErrInvalidPath
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	return canonicalPath(path)
}

func retainProcessLock(path string) *processLock {
	processLockRegistry.Lock()
	defer processLockRegistry.Unlock()
	lock := processLockRegistry.entries[path]
	if lock == nil {
		lock = &processLock{token: make(chan struct{}, 1)}
		processLockRegistry.entries[path] = lock
	}
	lock.refs++
	return lock
}

func releaseProcessLock(path string, lock *processLock) {
	processLockRegistry.Lock()
	defer processLockRegistry.Unlock()
	lock.refs--
	if lock.refs == 0 && processLockRegistry.entries[path] == lock {
		delete(processLockRegistry.entries, path)
	}
}

func processLockRegistrySize() int {
	processLockRegistry.Lock()
	defer processLockRegistry.Unlock()
	return len(processLockRegistry.entries)
}

func canonicalPath(path string) (string, error) {
	absolute, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	info, lstatErr := os.Lstat(absolute)
	if lstatErr == nil {
		resolved, err := filepath.EvalSymlinks(absolute)
		if err != nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return "", errors.Join(ErrInvalidPath, err)
			}
			return "", err
		}
		return resolved, nil
	}
	if !errors.Is(lstatErr, fs.ErrNotExist) {
		return "", lstatErr
	}
	resolvedDir, err := canonicalMissingDirectory(filepath.Dir(absolute))
	if err != nil {
		return "", err
	}
	return filepath.Join(resolvedDir, filepath.Base(absolute)), nil
}

func canonicalMissingDirectory(dir string) (string, error) {
	var missing []string
	for {
		info, err := os.Lstat(dir)
		if err == nil {
			resolved, evalErr := filepath.EvalSymlinks(dir)
			if evalErr != nil {
				if info.Mode()&os.ModeSymlink != 0 {
					return "", errors.Join(ErrInvalidPath, evalErr)
				}
				return "", evalErr
			}
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return resolved, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ErrInvalidPath
		}
		missing = append(missing, filepath.Base(dir))
		dir = parent
	}
}

func (s *Store) validateExistingNamespace(namespace string) error {
	if err := validateStoreDirectory(s.root); err != nil {
		return err
	}
	return validateStoreDirectory(filepath.Join(s.root, namespace))
}

func validateStoreDirectory(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return ErrInvalidPath
	}
	if !info.IsDir() {
		return ErrInvalidPath
	}
	return secureExistingPrivatePath(path, true, 0o700)
}

func ensureStoreDirectory(path string, parents bool) error {
	info, err := os.Lstat(path)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return ErrInvalidPath
		}
		return secureExistingPrivatePath(path, true, 0o700)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if parents {
		err = createPrivateDirectories(path, 0o700)
	} else {
		err = createPrivateDirectory(path, 0o700)
	}
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			info, statErr := os.Lstat(path)
			if statErr != nil {
				return errors.Join(ErrInvalidPath, err, statErr)
			}
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return ErrInvalidPath
			}
			return secureExistingPrivatePath(path, true, 0o700)
		}
		return err
	}
	if err := rejectSymlink(path); err != nil {
		return err
	}
	return nil
}

func ensureUpdateParent(path string) error {
	info, err := os.Lstat(path)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return ErrInvalidPath
		}
		// Fail closed: an existing parent directory must already be 0700.
		// We never chmod existing directories (which could be /tmp or shared
		// locations); if the permissions are wrong, return ErrPermission.
		return secureExistingPrivatePath(path, true, 0o700)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := createPrivateDirectories(path, 0o700); err != nil {
		return err
	}
	if err := rejectSymlink(path); err != nil {
		return err
	}
	return nil
}

func rejectSymlink(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return ErrInvalidPath
	}
	return nil
}

func validateStoreFile(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return ErrInvalidPath
	}
	return secureExistingPrivatePath(path, false, 0o600)
}

// ValidatePrivateFile verifies that an existing regular file at path has strict
// private permissions (0600 on Unix; protected private DACL on Windows). It is
// used before reading sensitive cache files so broad-permission caches cannot be
// used on the fast path. It never chmods or modifies the file.
//
// Error semantics (all errors.Is-classifiable):
//   - fs.ErrNotExist: the file does not exist
//   - ErrInvalidPath: path is a symlink or not a regular file
//   - ErrPermission: the file exists but its permissions/DACL are not private
func ValidatePrivateFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return ErrInvalidPath
	}
	return secureExistingPrivatePath(path, false, 0o600)
}

func samePath(first, second string) bool {
	if filepath.Clean(first) == filepath.Clean(second) {
		return true
	}
	firstInfo, firstErr := os.Stat(first)
	secondInfo, secondErr := os.Stat(second)
	return firstErr == nil && secondErr == nil && os.SameFile(firstInfo, secondInfo)
}
