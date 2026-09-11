package sso

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/securestore"
)

func TestCanonicalStartURLRejectsUnsafe(t *testing.T) {
	cases := []struct {
		name string
		url  string
	}{
		{"empty", ""},
		{"http", "http://example.com"},
		{"userinfo", "https://user:pass@example.com"},
		{"query", "https://example.com?x=1"},
		{"fragment", "https://example.com#frag"},
		{"opaque", "https:example.com"},
		{"no host", "https:///path"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := CanonicalStartURL(c.url); err == nil {
				t.Fatalf("expected error for %q", c.url)
			}
		})
	}
}

func TestCanonicalStartURLPreservesNonRootPath(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"userportal", "https://example.volccloudidentity.com/userportal", "https://example.volccloudidentity.com/userportal"},
		{"trailing slash", "https://example.volccloudidentity.com/userportal/", "https://example.volccloudidentity.com/userportal"},
		{"root path", "https://example.volccloudidentity.com/", "https://example.volccloudidentity.com"},
		{"empty path", "https://example.volccloudidentity.com", "https://example.volccloudidentity.com"},
		{"host case", "https://EXAMPLE.volccloudidentity.com/userportal", "https://example.volccloudidentity.com/userportal"},
		{"whitespace", "  https://example.volccloudidentity.com/userportal  ", "https://example.volccloudidentity.com/userportal"},
		{"dot segment", "https://example.volccloudidentity.com/a/./b", "https://example.volccloudidentity.com/a/b"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := CanonicalStartURL(c.in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
		})
	}
}

func TestCanonicalStartURLNormalizes(t *testing.T) {
	got, err := CanonicalStartURL("  https://example.volccloudidentity.com  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "https://example.volccloudidentity.com"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestNormalizeScopes(t *testing.T) {
	got, err := NormalizeScopes([]string{" b ", "a", "a", "c "})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestNormalizeScopesRejectsEmpty(t *testing.T) {
	if _, err := NormalizeScopes([]string{"a", ""}); err == nil {
		t.Fatal("expected error for empty scope")
	}
}

func TestTokenKeyDeterministicAndCollisionSafe(t *testing.T) {
	k1, err := tokenKey("https://example.com", "session1")
	if err != nil {
		t.Fatal(err)
	}
	k2, err := tokenKey("https://example.com", "session1")
	if err != nil {
		t.Fatal(err)
	}
	if k1 != k2 {
		t.Fatal("token key not deterministic")
	}
	// Different session -> different key.
	k3, err := tokenKey("https://example.com", "session2")
	if err != nil {
		t.Fatal(err)
	}
	if k1 == k3 {
		t.Fatal("token key collision for different session")
	}
	// Ambiguous concatenation check: "ab"+"c" vs "a"+"bc" must differ.
	ka, _ := tokenKey("https://example.com", "bc")
	kb, _ := tokenKey("https://example.co", "c")
	// Different start URLs should produce different keys.
	if ka == kb {
		t.Fatal("token key collision for ambiguous inputs")
	}
}

func TestClientKeyStableAcrossScopeOrder(t *testing.T) {
	k1, err := clientKey("https://example.com", "cn-beijing", []string{"a", "b"}, "s1")
	if err != nil {
		t.Fatal(err)
	}
	k2, err := clientKey("https://example.com", "cn-beijing", []string{"b", "a"}, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if k1 != k2 {
		t.Fatal("client key not stable across scope order")
	}
}

func TestSTSKeyDeterministic(t *testing.T) {
	k1, err := stsKey("s1", "a1", "r1")
	if err != nil {
		t.Fatal(err)
	}
	k2, err := stsKey("s1", "a1", "r1")
	if err != nil {
		t.Fatal(err)
	}
	if k1 != k2 {
		t.Fatal("sts key not deterministic")
	}
	k3, err := stsKey("s1", "a1", "r2")
	if err != nil {
		t.Fatal(err)
	}
	if k1 == k3 {
		t.Fatal("sts key collision for different role")
	}
}

func TestFileCacheMissingVsCorrupt(t *testing.T) {
	dir := t.TempDir()
	cache, err := NewFileCache(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Missing -> ErrMissing.
	_, err = cache.ReadToken("https://example.com", "s1")
	if !errors.Is(err, securestore.ErrMissing) {
		t.Fatalf("expected ErrMissing, got %v", err)
	}
	// Write a valid token cache.
	tc := &TokenCache{
		StartURL:     "https://example.com",
		SessionName:  "s1",
		AccessToken:  "tok",
		ExpiresAt:    time.Now().Add(time.Hour).Format(time.RFC3339),
		ClientID:     "cid",
		ClientSecret: "csec",
	}
	if err := cache.WriteToken(tc); err != nil {
		t.Fatal(err)
	}
	// Read it back.
	got, err := cache.ReadToken("https://example.com", "s1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.AccessToken != "tok" {
		t.Fatalf("got %q want %q", got.AccessToken, "tok")
	}
	// Corrupt the file manually.
	key, _ := tokenKey("https://example.com", "s1")
	actualPath := filepath.Join(dir, "token-"+key+".json")
	if err := os.WriteFile(actualPath, []byte("{not valid json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = cache.ReadToken("https://example.com", "s1")
	if !errors.Is(err, securestore.ErrCorrupt) {
		t.Fatalf("expected ErrCorrupt, got %v", err)
	}
}

func TestFileCacheRejectsBroadPermissionTokenFile(t *testing.T) {
	dir := t.TempDir()
	cache, err := NewFileCache(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Write a valid token cache, then chmod it to 0644 (broad).
	tc := &TokenCache{
		StartURL:    "https://example.com",
		SessionName: "s1",
		AccessToken: "tok",
		ExpiresAt:   time.Now().Add(time.Hour).Format(time.RFC3339),
	}
	if err := cache.WriteToken(tc); err != nil {
		t.Fatalf("WriteToken: %v", err)
	}
	key, _ := tokenKey("https://example.com", "s1")
	actualPath := filepath.Join(dir, "token-"+key+".json")
	if err := os.Chmod(actualPath, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	_, err = cache.ReadToken("https://example.com", "s1")
	if !errors.Is(err, securestore.ErrPermission) {
		t.Fatalf("ReadToken with 0644 cache: err=%v, want errors.Is(err, ErrPermission)", err)
	}
	// File mode must not be changed.
	info, _ := os.Stat(actualPath)
	if got := info.Mode().Perm(); got != 0o644 {
		t.Fatalf("cache mode changed to %#o, want 0644", got)
	}
}

func TestFileCachePermissions(t *testing.T) {
	dir := t.TempDir()
	cache, err := NewFileCache(dir)
	if err != nil {
		t.Fatal(err)
	}
	tc := &TokenCache{
		StartURL:     "https://example.com",
		SessionName:  "s1",
		AccessToken:  "tok",
		ExpiresAt:    time.Now().Add(time.Hour).Format(time.RFC3339),
		ClientID:     "cid",
		ClientSecret: "csec",
	}
	if err := cache.WriteToken(tc); err != nil {
		t.Fatal(err)
	}
	// Check directory permissions.
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("dir perm %o want 700", info.Mode().Perm())
	}
	// Check file permissions: the file is a direct basename under dir.
	key, _ := tokenKey("https://example.com", "s1")
	finfo, err := os.Stat(filepath.Join(dir, "token-"+key+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if finfo.Mode().Perm() != 0o600 {
		t.Fatalf("file perm %o want 600", finfo.Mode().Perm())
	}
}

func TestFileCacheAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	cache, err := NewFileCache(dir)
	if err != nil {
		t.Fatal(err)
	}
	tc := &TokenCache{
		StartURL:     "https://example.com",
		SessionName:  "s1",
		AccessToken:  "tok1",
		ExpiresAt:    time.Now().Add(time.Hour).Format(time.RFC3339),
		ClientID:     "cid",
		ClientSecret: "csec",
	}
	if err := cache.WriteToken(tc); err != nil {
		t.Fatal(err)
	}
	// Overwrite; the old value must never be partially visible.
	tc.AccessToken = "tok2"
	if err := cache.WriteToken(tc); err != nil {
		t.Fatal(err)
	}
	got, err := cache.ReadToken("https://example.com", "s1")
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "tok2" {
		t.Fatalf("got %q want tok2", got.AccessToken)
	}
}

func TestFileCacheLockSerializes(t *testing.T) {
	dir := t.TempDir()
	cache, err := NewFileCache(dir)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- cache.WithTokenLock(context.Background(), "https://example.com", "s1", func() error {
			close(started)
			<-release
			return nil
		})
	}()
	select {
	case <-started:
	case err := <-firstDone:
		t.Fatalf("first lock failed before entering: %v", err)
	case <-time.After(time.Second):
		t.Fatal("first lock did not enter")
	}
	// Second lock attempt should block until the first releases.
	done := make(chan error, 1)
	go func() {
		done <- cache.WithTokenLock(context.Background(), "https://example.com", "s1", func() error {
			return nil
		})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("second lock failed while first was held: %v", err)
		}
		t.Fatal("second lock acquired before first released")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("second lock never acquired")
	}
	if err := <-firstDone; err != nil {
		t.Fatalf("first lock: %v", err)
	}
}

func TestClientRegistrationAuthorityBoundary(t *testing.T) {
	dir := t.TempDir()
	cache, err := NewFileCache(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Write a client registration.
	cc := &ClientRegistrationCache{
		ClientName:            "volclog",
		ClientID:              "cid-client",
		ClientSecret:          "csec-client",
		ClientIDIssuedAt:      1000,
		ClientSecretExpiresAt: 0,
	}
	if err := cache.WriteClient(cc, "https://example.com", "cn-beijing", []string{"a"}, "s1"); err != nil {
		t.Fatal(err)
	}
	// Write a token cache with a DIFFERENT client id/secret.
	tc := &TokenCache{
		StartURL:     "https://example.com",
		SessionName:  "s1",
		AccessToken:  "tok",
		ExpiresAt:    time.Now().Add(time.Hour).Format(time.RFC3339),
		ClientID:     "cid-token",
		ClientSecret: "csec-token",
	}
	if err := cache.WriteToken(tc); err != nil {
		t.Fatal(err)
	}
	// The token cache must be the sole authority: reading it returns the token's
	// client id/secret, not the client cache's.
	got, err := cache.ReadToken("https://example.com", "s1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ClientID != "cid-token" {
		t.Fatalf("token cache client id = %q, want cid-token", got.ClientID)
	}
	// The client cache is independent and unchanged.
	gotClient, err := cache.ReadClient("https://example.com", "cn-beijing", []string{"a"}, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if gotClient.ClientID != "cid-client" {
		t.Fatalf("client cache client id = %q, want cid-client", gotClient.ClientID)
	}
}

func TestSTSCacheToValue(t *testing.T) {
	exp := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	c := &STSCache{
		SessionName:     "s1",
		AccountID:       "a1",
		RoleName:        "r1",
		AccessKeyID:     "AK",
		SecretAccessKey: "SK",
		SessionToken:    "ST",
		ProviderName:    ProviderName,
		ExpiresAt:       exp,
	}
	v, err := c.ToValue()
	if err != nil {
		t.Fatal(err)
	}
	if v.AccessKeyID != "AK" || v.SecretAccessKey != "SK" || v.SessionToken != "ST" {
		t.Fatal("value mismatch")
	}
	if v.ProviderName != ProviderName {
		t.Fatalf("provider = %q want %q", v.ProviderName, ProviderName)
	}
}

func TestFileCacheDeleteOperations(t *testing.T) {
	dir := t.TempDir()
	cache, err := NewFileCache(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Write and delete token cache.
	tc := &TokenCache{
		StartURL:     "https://example.com",
		SessionName:  "s1",
		AccessToken:  "tok",
		ExpiresAt:    time.Now().Add(time.Hour).Format(time.RFC3339),
		ClientID:     "cid",
		ClientSecret: "csec",
	}
	if err := cache.WriteToken(tc); err != nil {
		t.Fatal(err)
	}
	if err := cache.DeleteToken("https://example.com", "s1"); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.ReadToken("https://example.com", "s1"); !errors.Is(err, securestore.ErrMissing) {
		t.Fatalf("expected ErrMissing after delete, got %v", err)
	}
	// Delete again is idempotent.
	if err := cache.DeleteToken("https://example.com", "s1"); err != nil {
		t.Fatalf("delete should be idempotent, got %v", err)
	}

	// Write and delete client cache.
	cc := &ClientRegistrationCache{
		ClientName: "volclog", ClientID: "cid", ClientSecret: "csec",
	}
	if err := cache.WriteClient(cc, "https://example.com", "cn-beijing", []string{"a"}, "s1"); err != nil {
		t.Fatal(err)
	}
	if err := cache.DeleteClient("https://example.com", "cn-beijing", []string{"a"}, "s1"); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.ReadClient("https://example.com", "cn-beijing", []string{"a"}, "s1"); !errors.Is(err, securestore.ErrMissing) {
		t.Fatalf("expected ErrMissing after delete, got %v", err)
	}

	// Write and delete STS cache.
	sc := &STSCache{
		SessionName: "s1", AccountID: "a1", RoleName: "r1",
		AccessKeyID: "AK", SecretAccessKey: "SK", SessionToken: "ST",
		ProviderName: ProviderName, ExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339),
	}
	if err := cache.WriteSTS(sc); err != nil {
		t.Fatal(err)
	}
	if err := cache.DeleteSTS("s1", "a1", "r1"); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.ReadSTS("s1", "a1", "r1"); !errors.Is(err, securestore.ErrMissing) {
		t.Fatalf("expected ErrMissing after delete, got %v", err)
	}
}

func TestFileCacheWithClientLock(t *testing.T) {
	dir := t.TempDir()
	cache, err := NewFileCache(dir)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	err = cache.WithClientLock(context.Background(), "https://example.com", "cn-beijing", []string{"a"}, "s1", func() error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("function not called under client lock")
	}
}

func TestFileCacheNil(t *testing.T) {
	var c *FileCache
	if _, err := c.ReadToken("https://example.com", "s1"); err == nil {
		t.Fatal("expected error for nil cache")
	}
	if err := c.WriteToken(&TokenCache{}); err == nil {
		t.Fatal("expected error for nil cache")
	}
	if err := c.WithTokenLock(context.Background(), "https://example.com", "s1", func() error { return nil }); err == nil {
		t.Fatal("expected error for nil cache")
	}
}

func TestNewFileCacheEmptyDir(t *testing.T) {
	// Empty dir must return an error; the auth core must not read env/HOME/.volclog.
	_, err := NewFileCache("")
	if err == nil {
		t.Fatal("expected error for empty dir, got nil")
	}
}

func TestSTSCacheToValueInvalid(t *testing.T) {
	c := &STSCache{ExpiresAt: "not-a-time"}
	if _, err := c.ToValue(); err == nil {
		t.Fatal("expected error for invalid expiration")
	}
	var nilCache *STSCache
	if _, err := nilCache.ToValue(); err == nil {
		t.Fatal("expected error for nil cache")
	}
}

// TestCacheFileBasenames verifies the production FileCache writes files as
// direct basenames under the cache root with the exact required prefixes, and
// that no identity or secret material appears in the filenames.
func TestCacheFileBasenames(t *testing.T) {
	dir := t.TempDir()
	cache, err := NewFileCache(dir)
	if err != nil {
		t.Fatal(err)
	}
	const startURL = "https://example.volccloudidentity.com/userportal"
	const session = "corp-session"
	const region = "cn-beijing"
	const account = "acc-123456789012"
	const role = "admin-role"

	// Write token cache.
	if err := cache.WriteToken(&TokenCache{
		StartURL: startURL, SessionName: session,
		AccessToken: "secret-access-token", ClientID: "cid", ClientSecret: "csec",
		ExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	// Write client cache.
	if err := cache.WriteClient(&ClientRegistrationCache{
		ClientID: "cid", ClientSecret: "csec",
	}, startURL, region, []string{"a", "b"}, session); err != nil {
		t.Fatal(err)
	}
	// Write STS cache.
	if err := cache.WriteSTS(&STSCache{
		SessionName: session, AccountID: account, RoleName: role,
		AccessKeyID: "AK", SecretAccessKey: "SK", SessionToken: "ST",
		ProviderName: ProviderName, ExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}

	// All three cache files must be direct basenames; no data subdirectories.
	// securestore also creates a .locks directory and .lock files which we
	// ignore here; only token/client/sts data subdirectories are forbidden.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Exact basename pattern: prefix + 64 lowercase hex chars + .json
	filenameRE := regexp.MustCompile(`^(token|client|sts)-[0-9a-f]{64}\.json$`)
	prefixes := map[string]bool{"token-": false, "client-": false, "sts-": false}
	jsonCount := 0
	for _, e := range entries {
		if e.IsDir() {
			// .locks is expected; only data subdirs named token/client/sts
			// are forbidden.
			if e.Name() == "token" || e.Name() == "client" || e.Name() == "sts" {
				t.Fatalf("unexpected data subdirectory: %s", e.Name())
			}
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".json") {
			continue // ignore .lock and other auxiliary files
		}
		jsonCount++
		// Assert the exact basename format, not just prefix/suffix.
		if !filenameRE.MatchString(name) {
			t.Fatalf("filename %q does not match exact pattern token|client|sts-<64hex>.json", name)
		}
		found := false
		for p := range prefixes {
			if strings.HasPrefix(name, p) {
				prefixes[p] = true
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("unexpected file name: %s", name)
		}
		// No identity or secret material may appear in the filename.
		for _, secret := range []string{startURL, session, account, role, "secret-access-token", "cid", "csec", "AK", "SK", "ST"} {
			if strings.Contains(name, secret) {
				t.Fatalf("filename %q contains secret/identity %q", name, secret)
			}
		}
	}
	if jsonCount != 3 {
		t.Fatalf("expected 3 cache .json files, got %d", jsonCount)
	}
	for p, seen := range prefixes {
		if !seen {
			t.Fatalf("missing file with prefix %q", p)
		}
	}
}

// TestClientKeyOrder verifies the client registration key is composed in the
// exact order: canonical StartURL, trimmed region, normalized sorted scopes,
// session name. Swapping the order of scopes and session must produce a
// different key.
func TestClientKeyOrder(t *testing.T) {
	// Key with scopes before session (the required order).
	k1, err := clientKey("https://example.com", "cn-beijing", []string{"scope-a", "scope-b"}, "session-x")
	if err != nil {
		t.Fatal(err)
	}
	// A key composed with session before scopes must differ.
	k2, err := clientKeySessionFirst("https://example.com", "cn-beijing", []string{"scope-a", "scope-b"}, "session-x")
	if err != nil {
		t.Fatal(err)
	}
	if k1 == k2 {
		t.Fatal("client key must change when scope/session order changes")
	}
	// Same inputs in different scope order produce the same key (scopes are
	// normalized/sorted before hashing).
	k3, err := clientKey("https://example.com", "cn-beijing", []string{"scope-b", "scope-a"}, "session-x")
	if err != nil {
		t.Fatal(err)
	}
	if k1 != k3 {
		t.Fatal("client key must be stable across scope input order")
	}
}

// clientKeySessionFirst composes a key with session before scopes to prove the
// production order (scopes before session) matters. It mirrors clientKey but
// swaps the last two part groups.
func clientKeySessionFirst(startURL, region string, scopes []string, sessionName string) (string, error) {
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
	parts = append(parts, canonical, region, session)
	parts = append(parts, normalized...)
	return securestore.DigestKey(parts...), nil
}

// TestCanonicalStartURLRejectsEscapedPathAndEmptyHostname verifies that
// CanonicalStartURL rejects empty Hostname (e.g. https://:443/...), escaped
// paths (%2F, encoded dot-segments), and accepts uppercase HTTPS scheme while
// canonicalizing to lowercase.
func TestCanonicalStartURLRejectsEscapedPathAndEmptyHostname(t *testing.T) {
	rejectCases := []struct {
		name string
		url  string
	}{
		{"empty hostname with port", "https://:443/userportal"},
		{"escaped slash", "https://example.com/user%2Fportal"},
		{"encoded dot segment", "https://example.com/%2e%2e/path"},
		{"encoded dot", "https://example.com/a%2eb"},
	}
	for _, c := range rejectCases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := CanonicalStartURL(c.url); err == nil {
				t.Fatalf("expected error for %q", c.url)
			}
		})
	}

	// Uppercase HTTPS scheme is accepted and canonicalized to lowercase.
	got, err := CanonicalStartURL("HTTPS://EXAMPLE.com/userportal")
	if err != nil {
		t.Fatalf("unexpected error for uppercase scheme: %v", err)
	}
	want := "https://example.com/userportal"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}

	// Ordinary /userportal still works.
	got, err = CanonicalStartURL("https://example.volccloudidentity.com/userportal")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "https://example.volccloudidentity.com/userportal" {
		t.Fatalf("got %q", got)
	}
}

// TestClassifyReadError verifies that classifyReadError maps only not-exist to
// ErrMissing and permission to ErrPermission. General I/O errors (e.g. EIO)
// must NOT match ErrCorrupt, and the error string must not render injected
// canary/path text.
func TestClassifyReadError(t *testing.T) {
	// not-exist -> ErrMissing
	err := classifyReadError(fs.ErrNotExist)
	if !errors.Is(err, securestore.ErrMissing) {
		t.Fatalf("expected ErrMissing, got %v", err)
	}
	if errors.Is(err, securestore.ErrCorrupt) {
		t.Fatal("not-exist must not match ErrCorrupt")
	}

	// permission -> ErrPermission
	err = classifyReadError(fs.ErrPermission)
	if !errors.Is(err, securestore.ErrPermission) {
		t.Fatalf("expected ErrPermission, got %v", err)
	}
	if errors.Is(err, securestore.ErrCorrupt) {
		t.Fatal("permission must not match ErrCorrupt")
	}

	// general I/O (EIO) -> must NOT match ErrCorrupt or any sentinel
	err = classifyReadError(syscall.EIO)
	if errors.Is(err, securestore.ErrCorrupt) {
		t.Fatal("EIO must not match ErrCorrupt")
	}
	if errors.Is(err, securestore.ErrMissing) {
		t.Fatal("EIO must not match ErrMissing")
	}
	if errors.Is(err, securestore.ErrPermission) {
		t.Fatal("EIO must not match ErrPermission")
	}
	// The original cause must be preserved for errors.Is/errors.As.
	if !errors.Is(err, syscall.EIO) {
		t.Fatal("EIO cause must be preserved")
	}
	// Error string must be a fixed safe message, not the raw cause.
	if strings.Contains(err.Error(), "EIO") || strings.Contains(err.Error(), "input/output") {
		t.Fatalf("error string must not render raw cause text: %q", err.Error())
	}
}

// TestClassifyReadErrorJSONCorrupt verifies that JSON decode errors are
// classified as ErrCorrupt (handled by readCacheFile, not classifyReadError).
func TestClassifyReadErrorJSONCorrupt(t *testing.T) {
	dir := t.TempDir()
	cache, err := NewFileCache(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Write a corrupt JSON file directly at the expected token path.
	key, _ := tokenKey("https://example.com", "s1")
	actualPath := filepath.Join(dir, "token-"+key+".json")
	if err := os.WriteFile(actualPath, []byte("{not valid json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = cache.ReadToken("https://example.com", "s1")
	if !errors.Is(err, securestore.ErrCorrupt) {
		t.Fatalf("expected ErrCorrupt for JSON decode error, got %v", err)
	}
}

// TestFileCacheWithLockRejectsNilCallback verifies that the With*Lock methods
// reject a nil callback rather than panicking.
func TestFileCacheWithLockRejectsNilCallback(t *testing.T) {
	dir := t.TempDir()
	cache, err := NewFileCache(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.WithTokenLock(context.Background(), "https://example.com", "s1", nil); err == nil {
		t.Fatal("expected error for nil callback in WithTokenLock")
	}
	if err := cache.WithClientLock(context.Background(), "https://example.com", "cn-beijing", []string{"a"}, "s1", nil); err == nil {
		t.Fatal("expected error for nil callback in WithClientLock")
	}
	if err := cache.WithSTSLock(context.Background(), "s1", "a1", "r1", nil); err == nil {
		t.Fatal("expected error for nil callback in WithSTSLock")
	}
}

// TestCommitTargetKeyRelativePathDiffersAcrossCwd verifies the Owner invariant:
// the same relative path spelling in two different working directories must
// produce different commit identities, because they address different files.
// This test must not run in parallel and always restores the original cwd.
func TestCommitTargetKeyRelativePathDiffersAcrossCwd(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()

	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	if err := os.Chdir(dirA); err != nil {
		t.Fatal(err)
	}
	keyA, err := commitTargetKey("config.json", "profile")
	if err != nil {
		os.Chdir(orig)
		t.Fatal(err)
	}

	if err := os.Chdir(dirB); err != nil {
		os.Chdir(orig)
		t.Fatal(err)
	}
	keyB, err := commitTargetKey("config.json", "profile")
	if err != nil {
		os.Chdir(orig)
		t.Fatal(err)
	}

	// Restore before assertions so a failure never leaks the cwd.
	if err := os.Chdir(orig); err != nil {
		t.Fatalf("restore working directory: %v", err)
	}

	if keyA == keyB {
		t.Fatal("same relative path in different cwd must produce different commit identities")
	}
}

// TestCommitTargetKeyEquivalentSpellingsSameCwd verifies that equivalent
// spellings for the same target in the same working directory produce the same
// digest. This test must not run in parallel and always restores the original
// cwd.
func TestCommitTargetKeyEquivalentSpellingsSameCwd(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(orig)

	k1, err := commitTargetKey("config.json", "p")
	if err != nil {
		t.Fatal(err)
	}
	k2, err := commitTargetKey("./config.json", "p")
	if err != nil {
		t.Fatal(err)
	}
	if k1 != k2 {
		t.Fatal("equivalent relative spellings in same cwd must produce same identity")
	}
	// An already-absolute spelling of the same file must match too.
	abs, err := filepath.Abs("config.json")
	if err != nil {
		t.Fatal(err)
	}
	k3, err := commitTargetKey(abs, "p")
	if err != nil {
		t.Fatal(err)
	}
	if k1 != k3 {
		t.Fatal("absolute and relative spellings of same file must match")
	}
	// A different relative path addressing a different file must differ.
	k4, err := commitTargetKey("other.json", "p")
	if err != nil {
		t.Fatal(err)
	}
	if k1 == k4 {
		t.Fatal("different files must produce different identities")
	}
}

// TestAddCommittedTargetDeduplicatesDirtySlice verifies the Owner invariant:
// AddCommittedTarget must produce a sorted, de-duplicated set even when the
// pre-existing slice is dirty (duplicates, unsorted).
func TestAddCommittedTargetDeduplicatesDirtySlice(t *testing.T) {
	c := &STSCache{CommittedTargets: []string{"b", "b", "a"}}
	c.AddCommittedTarget("c")
	want := []string{"a", "b", "c"}
	if len(c.CommittedTargets) != len(want) {
		t.Fatalf("got %v, want %v", c.CommittedTargets, want)
	}
	for i := range want {
		if c.CommittedTargets[i] != want[i] {
			t.Fatalf("got %v, want %v", c.CommittedTargets, want)
		}
	}

	// Adding an already-present target is a no-op (still canonical).
	c.AddCommittedTarget("a")
	if len(c.CommittedTargets) != 3 {
		t.Fatalf("adding existing target must not grow set: got %v", c.CommittedTargets)
	}

	// A fully dirty slice with the new target already present is canonicalized.
	c2 := &STSCache{CommittedTargets: []string{"z", "a", "z", "m", "a"}}
	c2.AddCommittedTarget("m")
	want2 := []string{"a", "m", "z"}
	if len(c2.CommittedTargets) != len(want2) {
		t.Fatalf("got %v, want %v", c2.CommittedTargets, want2)
	}
	for i := range want2 {
		if c2.CommittedTargets[i] != want2[i] {
			t.Fatalf("got %v, want %v", c2.CommittedTargets, want2)
		}
	}
}

// TestAddCommittedTargetEmptyNoOp verifies that empty target input is a no-op
// and that an empty/nil pre-existing slice stays empty.
func TestAddCommittedTargetEmptyNoOp(t *testing.T) {
	c := &STSCache{CommittedTargets: []string{"a"}}
	c.AddCommittedTarget("")
	if len(c.CommittedTargets) != 1 || c.CommittedTargets[0] != "a" {
		t.Fatalf("empty target must be no-op: got %v", c.CommittedTargets)
	}

	// nil receiver is safe.
	var nilC *STSCache
	nilC.AddCommittedTarget("a") // must not panic

	// empty pre-existing slice stays empty when adding empty.
	c2 := &STSCache{}
	c2.AddCommittedTarget("")
	if len(c2.CommittedTargets) != 0 {
		t.Fatalf("empty input on empty slice must stay empty: got %v", c2.CommittedTargets)
	}
}

// validHexTarget returns a 64-char lowercase hex string for tests.
func validHexTarget(b byte) string {
	s := make([]byte, 64)
	for i := range s {
		s[i] = b
	}
	return string(s)
}

// TestValidateCommittedTargets verifies that valid marker sets (nil, empty,
// canonical sorted unique 64-hex) pass and that invalid sets (duplicate,
// unsorted, wrong length, non-hex, uppercase) fail.
func TestValidateCommittedTargets(t *testing.T) {
	zero := validHexTarget('0')
	one := validHexTarget('1')
	af := validHexTarget('a')
	ff := validHexTarget('f')

	validCases := []struct {
		name string
		in   []string
	}{
		{"nil", nil},
		{"empty", []string{}},
		{"single", []string{zero}},
		{"sorted_unique", []string{zero, one, af, ff}},
	}
	for _, c := range validCases {
		t.Run("valid/"+c.name, func(t *testing.T) {
			if err := validateCommittedTargets(c.in); err != nil {
				t.Fatalf("expected valid, got error: %v", err)
			}
		})
	}

	invalidCases := []struct {
		name string
		in   []string
	}{
		{"duplicate", []string{zero, zero}},
		{"unsorted", []string{ff, zero}},
		{"wrong_length_short", []string{"abc"}},
		{"wrong_length_long", []string{zero + "0"}},
		{"non_hex", []string{validHexTarget('g')}},
		{"uppercase", []string{validHexTarget('A')}},
		{"mixed_valid_and_invalid_length", []string{zero, "short"}},
		{"empty_string_in_slice", []string{""}},
	}
	for _, c := range invalidCases {
		t.Run("invalid/"+c.name, func(t *testing.T) {
			if err := validateCommittedTargets(c.in); err == nil {
				t.Fatalf("expected error for %v", c.in)
			}
		})
	}
}
