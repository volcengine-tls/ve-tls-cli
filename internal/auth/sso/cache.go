package sso

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth"
	"github.com/volcengine-tls/ve-tls-cli/internal/securestore"
)

// isNilInterface reports whether v is nil or a typed-nil interface value (e.g.
// a nil *FileCache stored in a Cache interface). It uses reflect only on
// nil-capable kinds so it never panics on non-interface values.
func isNilInterface(v interface{}) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return rv.IsNil()
	}
	return false
}

// TokenCache is the persisted OAuth token cache for an SSO session. Its JSON
// field names match the upstream volcengine-cli SsoTokenCache so that caches
// written by either tool are mutually readable. The ClientID/ClientSecret
// snapshot stored here is the sole authority for ordinary business refresh once
// a token cache exists; the client-* registration cache is only consulted
// during explicit login before any token cache exists.
type TokenCache struct {
	StartURL              string `json:"start_url"`
	SessionName           string `json:"session_name"`
	AccessToken           string `json:"access_token"`
	ExpiresAt             string `json:"expires_at"`
	ClientID              string `json:"client_id"`
	ClientSecret          string `json:"client_secret"`
	ClientIDIssuedAt      int64  `json:"client_id_issued_at"`
	ClientSecretExpiresAt int64  `json:"client_secret_expires_at"`
	RefreshToken          string `json:"refresh_token"`
	Region                string `json:"region"`
}

// ClientRegistrationCache is the persisted OAuth client registration reused
// across explicit logins for the same start URL, region, scope set, and session
// name. It is authoritative only during explicit login before a token cache
// exists; it is never merged into a token cache by the Provider.
type ClientRegistrationCache struct {
	ClientName            string `json:"client_name"`
	ClientID              string `json:"client_id"`
	ClientSecret          string `json:"client_secret"`
	ClientIDIssuedAt      int64  `json:"client_id_issued_at"`
	ClientSecretExpiresAt int64  `json:"client_secret_expires_at"`
}

// STSCache is the persisted temporary STS credential cache for an SSO session
// bound to a specific account and role. It carries the complete auth.Value plus
// the binding identity so a stale or mismatched cache can be rejected.
//
// CommittedTargets is a sorted, de-duplicated set of SHA-256 digest strings, each
// identifying an exact config target (config path + profile name) whose
// sts-expiration has been successfully patched to match this STS entry. The raw
// config path is never persisted; only the digest is stored. Because the STS
// cache key is session/account/role and can be shared by multiple profiles and
// config files, each target must independently patch and commit before it can
// use the fast path. A target whose digest is absent from CommittedTargets must
// patch its own profile and add its digest before credentials are returned.
type STSCache struct {
	SessionName      string   `json:"session_name"`
	AccountID        string   `json:"account_id"`
	RoleName         string   `json:"role_name"`
	AccessKeyID      string   `json:"access_key_id"`
	SecretAccessKey  string   `json:"secret_access_key"`
	SessionToken     string   `json:"session_token"`
	ProviderName     string   `json:"provider_name"`
	ExpiresAt        string   `json:"expires_at"`
	CommittedTargets []string `json:"committed_targets"`
}

// ToValue converts the STS cache into an auth.Value. The caller is responsible
// for validating the binding identity and expiration before use.
func (c *STSCache) ToValue() (auth.Value, error) {
	if c == nil {
		return auth.Value{}, errors.New("nil sts cache")
	}
	expiresAt, err := time.Parse(time.RFC3339, c.ExpiresAt)
	if err != nil {
		return auth.Value{}, err
	}
	return auth.Value{
		AccessKeyID:     c.AccessKeyID,
		SecretAccessKey: c.SecretAccessKey,
		SessionToken:    c.SessionToken,
		ProviderName:    c.ProviderName,
		ExpiresAt:       expiresAt,
	}, nil
}

// Cache abstracts the SSO token, client registration, and STS caches with
// per-key cross-process locking. The production implementation (FileCache) uses
// securestore primitives; tests inject fakes.
//
// Lock order is strict and global: token key -> sts key(s) sorted by digest ->
// config path. Callers must never acquire a token lock while holding an STS
// lock. The With*Lock methods are safe to nest for different keys.
type Cache interface {
	// WithTokenLock acquires the cross-process lock for the token cache key
	// derived from canonical startURL + sessionName and runs fn.
	WithTokenLock(ctx context.Context, startURL, sessionName string, fn func() error) error
	// ReadToken reads the token cache for the given key. Returns an error
	// wrapping ErrMissing when the file does not exist and ErrCorrupt when it
	// cannot be parsed.
	ReadToken(startURL, sessionName string) (*TokenCache, error)
	// WriteToken atomically writes the token cache.
	WriteToken(cache *TokenCache) error
	// DeleteToken removes the token cache. Missing is idempotent.
	DeleteToken(startURL, sessionName string) error

	// WithClientLock acquires the cross-process lock for the client
	// registration cache key derived from canonical startURL + region + scopes
	// + sessionName and runs fn.
	WithClientLock(ctx context.Context, startURL, region string, scopes []string, sessionName string, fn func() error) error
	// ReadClient reads the client registration cache.
	ReadClient(startURL, region string, scopes []string, sessionName string) (*ClientRegistrationCache, error)
	// WriteClient atomically writes the client registration cache.
	WriteClient(cache *ClientRegistrationCache, startURL, region string, scopes []string, sessionName string) error
	// DeleteClient removes the client registration cache.
	DeleteClient(startURL, region string, scopes []string, sessionName string) error

	// WithSTSLock acquires the cross-process lock for the STS cache key derived
	// from sessionName + accountID + roleName and runs fn.
	WithSTSLock(ctx context.Context, sessionName, accountID, roleName string, fn func() error) error
	// ReadSTS reads the STS cache.
	ReadSTS(sessionName, accountID, roleName string) (*STSCache, error)
	// WriteSTS atomically writes the STS cache.
	WriteSTS(cache *STSCache) error
	// DeleteSTS removes the STS cache.
	DeleteSTS(sessionName, accountID, roleName string) error
}

// CanonicalStartURL validates and normalizes startURL to a clean HTTPS URL.
// Unlike the CloudIdentity API base URL validator, it preserves a meaningful
// non-root path (e.g. /userportal) because the SSO user portal URL formally
// supports one. It normalizes only semantically safe path variations so the
// same logical StartURL has one canonical form and therefore one cache key.
//
// Rules:
//   - absolute HTTPS URL (scheme compared case-insensitively; canonical output
//     is always lowercase "https")
//   - non-empty hostname (u.Hostname(), not merely u.Host, so ":443" is rejected)
//   - no userinfo
//   - no query or fragment
//   - not opaque
//   - no escaped path (non-empty RawPath is rejected; %2F, encoded dot-segments
//     and other ambiguous escaped paths are never decoded)
//   - preserve a meaningful non-root path; clean "." / ".." segments and
//     duplicate slashes
//   - normalize root and trailing slash: "/" or "" -> no path; "/x/" -> "/x"
//
// Error text never echoes the raw URL.
func CanonicalStartURL(startURL string) (string, error) {
	raw := strings.TrimSpace(startURL)
	if raw == "" {
		return "", errors.New("start URL is empty")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("start URL is invalid")
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return "", errors.New("start URL must use https scheme")
	}
	if u.Hostname() == "" {
		return "", errors.New("start URL must have a non-empty host")
	}
	if u.User != nil {
		return "", errors.New("start URL must not contain userinfo")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("start URL must not contain query or fragment")
	}
	if u.Opaque != "" {
		return "", errors.New("start URL must not be opaque")
	}
	// Reject any escaped-path spelling. Escaped paths such as %2F or %2e%2e
	// are ambiguous: decoding them changes semantics and can cause cache-key
	// collisions. We only accept plain, unescaped paths.
	if u.RawPath != "" {
		return "", errors.New("start URL must not contain an escaped path")
	}
	// Normalize the path: clean semantically safe variations only.
	clean := path.Clean(u.Path)
	switch clean {
	case "", ".", "/":
		clean = ""
	}
	// Lowercase the host; DNS is case-insensitive and canonicalization must be
	// deterministic so the same logical StartURL maps to one cache key.
	return "https://" + strings.ToLower(u.Host) + clean, nil
}

// NormalizeScopes trims each scope, rejects empty entries, de-duplicates, and
// sorts the result so the scope set contributes to cache keys as a stable,
// order-independent value.
func NormalizeScopes(scopes []string) ([]string, error) {
	seen := make(map[string]struct{}, len(scopes))
	out := make([]string, 0, len(scopes))
	for _, s := range scopes {
		s = strings.TrimSpace(s)
		if s == "" {
			return nil, errors.New("scope list contains an empty entry")
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out, nil
}

// tokenKey derives the deterministic, collision-safe token cache key digest
// from the canonical start URL and session name using length-prefixed SHA-256.
func tokenKey(startURL, sessionName string) (string, error) {
	canonical, err := CanonicalStartURL(startURL)
	if err != nil {
		return "", err
	}
	session := strings.TrimSpace(sessionName)
	if session == "" {
		return "", errors.New("session name is empty")
	}
	return securestore.DigestKey(canonical, session), nil
}

// clientKey derives the deterministic, collision-safe client registration cache
// key digest from the canonical start URL, trimmed region, normalized sorted
// scope set, and session name, in that exact order.
func clientKey(startURL, region string, scopes []string, sessionName string) (string, error) {
	canonical, err := CanonicalStartURL(startURL)
	if err != nil {
		return "", err
	}
	normalized, err := NormalizeScopes(scopes)
	if err != nil {
		return "", err
	}
	session := strings.TrimSpace(sessionName)
	if session == "" {
		return "", errors.New("session name is empty")
	}
	region = strings.TrimSpace(region)
	parts := make([]string, 0, len(normalized)+3)
	parts = append(parts, canonical, region)
	parts = append(parts, normalized...)
	parts = append(parts, session)
	return securestore.DigestKey(parts...), nil
}

// stsKey derives the deterministic, collision-safe STS cache key digest from
// the session name, account ID, and role name, in that exact order.
func stsKey(sessionName, accountID, roleName string) (string, error) {
	session := strings.TrimSpace(sessionName)
	account := strings.TrimSpace(accountID)
	role := strings.TrimSpace(roleName)
	if session == "" {
		return "", errors.New("session name is empty")
	}
	if account == "" {
		return "", errors.New("account id is empty")
	}
	if role == "" {
		return "", errors.New("role name is empty")
	}
	return securestore.DigestKey(session, account, role), nil
}

// normalizeConfigPath resolves a config file path to a cleaned absolute path
// deterministically so that equivalent ordinary path spellings used by this CLI
// produce the same commit identity, while the same relative spelling in
// different working directories produces different identities (because they
// address different files). It trims whitespace, resolves relative paths
// against the current working directory, cleans "." / ".." segments and
// duplicate slashes, and removes a trailing separator. It does not use
// EvalSymlinks because the target may not exist yet. The raw path is never
// persisted or rendered.
func normalizeConfigPath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", errors.New("config path is empty")
	}
	// filepath.Abs resolves a relative path against the working directory
	// without following symlinks, so it is safe even when the target does not
	// exist yet.
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", errors.New("config path is invalid")
	}
	cleaned := filepath.Clean(abs)
	if cleaned == "." {
		return "", errors.New("config path is empty")
	}
	return cleaned, nil
}

// commitTargetKey derives the deterministic, collision-safe commit identity for
// an exact config target (config path + profile name) using length-prefixed
// SHA-256. The raw config path is never persisted; only the digest is stored in
// the STS cache. Empty values are rejected so an unconfigured provider can never
// produce a valid identity.
func commitTargetKey(configPath, profileName string) (string, error) {
	normalized, err := normalizeConfigPath(configPath)
	if err != nil {
		return "", err
	}
	profile := strings.TrimSpace(profileName)
	if profile == "" {
		return "", errors.New("profile name is empty")
	}
	return securestore.DigestKey(normalized, profile), nil
}

// HasCommittedTarget reports whether the given commit identity is present in the
// cache's CommittedTargets set. The set is expected to be sorted and de-duplicated
// but a defensive linear scan is used so a manually-edited or corrupt cache can
// never accidentally authorize a target.
func (c *STSCache) HasCommittedTarget(target string) bool {
	if c == nil || target == "" {
		return false
	}
	for _, t := range c.CommittedTargets {
		if t == target {
			return true
		}
	}
	return false
}

// AddCommittedTarget adds the given commit identity to the cache's
// CommittedTargets set, then produces a sorted, de-duplicated representation so
// the persisted form is always deterministic. Adding an already-present
// identity is a no-op. A dirty pre-existing slice (duplicates, unsorted) is
// canonicalized so production writes always emit the canonical sorted unique
// representation. Empty target input remains a no-op.
func (c *STSCache) AddCommittedTarget(target string) {
	if c == nil || target == "" {
		return
	}
	seen := make(map[string]struct{}, len(c.CommittedTargets)+1)
	seen[target] = struct{}{}
	for _, t := range c.CommittedTargets {
		if t == "" {
			continue
		}
		seen[t] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for t := range seen {
		out = append(out, t)
	}
	sort.Strings(out)
	c.CommittedTargets = out
}

// validateCommittedTargets validates the schema of the persisted
// CommittedTargets marker set. Every item must be exactly 64 lowercase hex
// characters, and the slice must be strictly sorted with no duplicates.
// nil/empty is valid for old/uncommitted caches. Invalid marker metadata must
// fail closed: it must not authorize a fast path, must not call Portal, must
// not patch config, and must not overwrite the cache.
func validateCommittedTargets(targets []string) error {
	if len(targets) == 0 {
		return nil
	}
	prev := ""
	for _, t := range targets {
		if len(t) != 64 {
			return errors.New("sts cache committed target has invalid length")
		}
		for i := 0; i < len(t); i++ {
			c := t[i]
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
				return errors.New("sts cache committed target is not lowercase hex")
			}
		}
		if prev != "" && t <= prev {
			return errors.New("sts cache committed targets are not strictly sorted")
		}
		prev = t
	}
	return nil
}

// FileCache is the production Cache implementation. It stores cache files as
// direct basenames under the cache root:
//
//	token-<sha256>.json
//	client-<sha256>.json
//	sts-<sha256>.json
//
// It uses securestore.Store.WithLock for per-key cross-process locks (coupled
// to the exact logical filename), securestore.UpdateFile for atomic 0600
// replacement, and explicit symlink/regular-file checks. Missing, corrupt, and
// permission errors are classified with the securestore sentinels.
type FileCache struct {
	store *securestore.Store
	dir   string
}

// NewFileCache creates a FileCache rooted at dir. The dir must be explicitly
// provided; the auth core never reads env/HOME/.volcenv to resolve a default.
// The directory is immediately canonicalized through securestore so the lock
// root and the data root share the same validated absolute path.
func NewFileCache(dir string) (*FileCache, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("cache dir is required; auth core does not read env/HOME defaults")
	}
	store := securestore.New(dir)
	root, err := store.CanonicalRoot()
	if err != nil {
		return nil, err
	}
	return &FileCache{store: store, dir: root}, nil
}

// cacheFilename returns the direct basename for the given prefix and digest.
func cacheFilename(prefix, digest string) string {
	return prefix + "-" + digest + ".json"
}

// dataPath returns the absolute path to the cache file for the given prefix
// and digest.
func (c *FileCache) dataPath(prefix, digest string) string {
	return filepath.Join(c.dir, cacheFilename(prefix, digest))
}

// classifyReadError maps a filesystem read error to the securestore sentinel
// classifications. It maps only not-exist -> ErrMissing and permission ->
// ErrPermission. JSON decode/schema errors are mapped to ErrCorrupt directly by
// readCacheFile. General I/O errors (e.g. syscall.EIO, transient filesystem
// errors) are NOT classified as corrupt; they are wrapped in a cacheIOError
// with a fixed safe message that preserves the cause for errors.Is/errors.As
// but does not match securestore.ErrCorrupt.
func classifyReadError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return &cacheClassifiedError{kind: securestore.ErrMissing, cause: err}
	case errors.Is(err, fs.ErrPermission):
		return &cacheClassifiedError{kind: securestore.ErrPermission, cause: err}
	default:
		return &cacheIOError{cause: err}
	}
}

// cacheIOError wraps a general filesystem I/O error with a fixed safe message.
// It does not match any securestore sentinel; the cause is preserved for
// errors.Is/errors.As classification.
type cacheIOError struct {
	cause error
}

func (e *cacheIOError) Error() string {
	return "cache read failed due to a filesystem error"
}

func (e *cacheIOError) Unwrap() error {
	return e.cause
}

// cacheClassifiedError wraps a securestore sentinel kind with an underlying
// cause. The Error() string is fixed to the kind's message so raw cause text
// (which may contain sensitive data) is never rendered; the cause is preserved
// for errors.Is/errors.As classification.
type cacheClassifiedError struct {
	kind  error
	cause error
}

func (e *cacheClassifiedError) Error() string {
	return e.kind.Error()
}

func (e *cacheClassifiedError) Unwrap() []error {
	if e.cause != nil {
		return []error{e.kind, e.cause}
	}
	return []error{e.kind}
}

// readCacheFile reads and JSON-decodes the cache file at path into out. It
// rejects symlinks and non-regular files, and classifies missing/permission/
// corrupt errors with the securestore sentinels.
func (c *FileCache) readCacheFile(path string, out any) error {
	// Validate the file is a regular private (0600) file before reading.
	// Broad-permission caches fail closed and are never used on the fast path.
	if verr := securestore.ValidatePrivateFile(path); verr != nil {
		return classifyReadError(verr)
	}
	data, rerr := os.ReadFile(path)
	if rerr != nil {
		return classifyReadError(rerr)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return &cacheClassifiedError{kind: securestore.ErrCorrupt, cause: err}
	}
	return nil
}

// writeCacheFile atomically writes data to path with 0600 permissions using
// securestore.UpdateFile. It rejects symlink and non-regular targets before
// writing so an attacker cannot redirect the write through a pre-placed symlink.
func (c *FileCache) writeCacheFile(path string, data []byte) error {
	if info, lerr := os.Lstat(path); lerr == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return securestore.ErrInvalidPath
		}
		if !info.Mode().IsRegular() {
			return securestore.ErrInvalidPath
		}
	} else if !errors.Is(lerr, fs.ErrNotExist) {
		return lerr
	}
	return securestore.UpdateFile(path, 0o600, func([]byte) ([]byte, error) {
		return data, nil
	})
}

// deleteCacheFile removes the cache file at path; missing is idempotent. It
// rejects symlink targets rather than following them.
func (c *FileCache) deleteCacheFile(path string) error {
	if info, lerr := os.Lstat(path); lerr == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return securestore.ErrInvalidPath
		}
	} else if !errors.Is(lerr, fs.ErrNotExist) {
		return lerr
	}
	err := os.Remove(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

func (c *FileCache) WithTokenLock(ctx context.Context, startURL, sessionName string, fn func() error) error {
	if c == nil || c.store == nil {
		return errors.New("nil *FileCache")
	}
	if fn == nil {
		return errors.New("nil callback")
	}
	digest, err := tokenKey(startURL, sessionName)
	if err != nil {
		return err
	}
	return c.store.WithLock(ctx, "token", digest, fn)
}

func (c *FileCache) ReadToken(startURL, sessionName string) (*TokenCache, error) {
	if c == nil || c.store == nil {
		return nil, errors.New("nil *FileCache")
	}
	digest, err := tokenKey(startURL, sessionName)
	if err != nil {
		return nil, err
	}
	var cache TokenCache
	if err := c.readCacheFile(c.dataPath("token", digest), &cache); err != nil {
		return nil, err
	}
	return &cache, nil
}

func (c *FileCache) WriteToken(cache *TokenCache) error {
	if c == nil || c.store == nil {
		return errors.New("nil *FileCache")
	}
	if cache == nil {
		return errors.New("nil token cache")
	}
	digest, err := tokenKey(cache.StartURL, cache.SessionName)
	if err != nil {
		return err
	}
	data, err := json.Marshal(cache)
	if err != nil {
		return err
	}
	return c.writeCacheFile(c.dataPath("token", digest), data)
}

func (c *FileCache) DeleteToken(startURL, sessionName string) error {
	if c == nil || c.store == nil {
		return errors.New("nil *FileCache")
	}
	digest, err := tokenKey(startURL, sessionName)
	if err != nil {
		return err
	}
	return c.deleteCacheFile(c.dataPath("token", digest))
}

func (c *FileCache) WithClientLock(ctx context.Context, startURL, region string, scopes []string, sessionName string, fn func() error) error {
	if c == nil || c.store == nil {
		return errors.New("nil *FileCache")
	}
	if fn == nil {
		return errors.New("nil callback")
	}
	digest, err := clientKey(startURL, region, scopes, sessionName)
	if err != nil {
		return err
	}
	return c.store.WithLock(ctx, "client", digest, fn)
}

func (c *FileCache) ReadClient(startURL, region string, scopes []string, sessionName string) (*ClientRegistrationCache, error) {
	if c == nil || c.store == nil {
		return nil, errors.New("nil *FileCache")
	}
	digest, err := clientKey(startURL, region, scopes, sessionName)
	if err != nil {
		return nil, err
	}
	var cache ClientRegistrationCache
	if err := c.readCacheFile(c.dataPath("client", digest), &cache); err != nil {
		return nil, err
	}
	return &cache, nil
}

func (c *FileCache) WriteClient(cache *ClientRegistrationCache, startURL, region string, scopes []string, sessionName string) error {
	if c == nil || c.store == nil {
		return errors.New("nil *FileCache")
	}
	if cache == nil {
		return errors.New("nil client cache")
	}
	digest, err := clientKey(startURL, region, scopes, sessionName)
	if err != nil {
		return err
	}
	data, err := json.Marshal(cache)
	if err != nil {
		return err
	}
	return c.writeCacheFile(c.dataPath("client", digest), data)
}

func (c *FileCache) DeleteClient(startURL, region string, scopes []string, sessionName string) error {
	if c == nil || c.store == nil {
		return errors.New("nil *FileCache")
	}
	digest, err := clientKey(startURL, region, scopes, sessionName)
	if err != nil {
		return err
	}
	return c.deleteCacheFile(c.dataPath("client", digest))
}

func (c *FileCache) WithSTSLock(ctx context.Context, sessionName, accountID, roleName string, fn func() error) error {
	if c == nil || c.store == nil {
		return errors.New("nil *FileCache")
	}
	if fn == nil {
		return errors.New("nil callback")
	}
	digest, err := stsKey(sessionName, accountID, roleName)
	if err != nil {
		return err
	}
	return c.store.WithLock(ctx, "sts", digest, fn)
}

func (c *FileCache) ReadSTS(sessionName, accountID, roleName string) (*STSCache, error) {
	if c == nil || c.store == nil {
		return nil, errors.New("nil *FileCache")
	}
	digest, err := stsKey(sessionName, accountID, roleName)
	if err != nil {
		return nil, err
	}
	var cache STSCache
	if err := c.readCacheFile(c.dataPath("sts", digest), &cache); err != nil {
		return nil, err
	}
	return &cache, nil
}

func (c *FileCache) WriteSTS(cache *STSCache) error {
	if c == nil || c.store == nil {
		return errors.New("nil *FileCache")
	}
	if cache == nil {
		return errors.New("nil sts cache")
	}
	digest, err := stsKey(cache.SessionName, cache.AccountID, cache.RoleName)
	if err != nil {
		return err
	}
	data, err := json.Marshal(cache)
	if err != nil {
		return err
	}
	return c.writeCacheFile(c.dataPath("sts", digest), data)
}

func (c *FileCache) DeleteSTS(sessionName, accountID, roleName string) error {
	if c == nil || c.store == nil {
		return errors.New("nil *FileCache")
	}
	digest, err := stsKey(sessionName, accountID, roleName)
	if err != nil {
		return err
	}
	return c.deleteCacheFile(c.dataPath("sts", digest))
}

// Compile-time assertion that FileCache satisfies Cache.
var _ Cache = (*FileCache)(nil)
