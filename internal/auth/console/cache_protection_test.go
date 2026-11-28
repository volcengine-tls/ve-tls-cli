package console

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth"
	"github.com/volcengine-tls/ve-tls-cli/internal/config"
	"github.com/volcengine-tls/ve-tls-cli/internal/securestore"
)

func TestFileCacheEncryptsAndAuthenticatesSession(t *testing.T) {
	const session = "trn:iam::1:login/session/encrypted"
	const otherSession = "trn:iam::1:login/session/other"
	dir := t.TempDir()
	cache, err := NewFileCache(dir)
	if err != nil {
		t.Fatal(err)
	}
	payload := makeCacheBytes(session, time.Now(), 3600, "refresh-token-canary")
	path, err := cache.dataPath(session)
	if err != nil {
		t.Fatal(err)
	}
	var previous []byte
	for i := 0; i < 2; i++ {
		if err := cache.WriteRaw(session, payload); err != nil {
			t.Fatal(err)
		}
		disk, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.HasPrefix(disk, []byte(cacheHeader)) || bytes.Contains(disk, []byte("refresh-token-canary")) {
			t.Fatal("expected versioned ciphertext without the refresh token")
		}
		if bytes.Equal(previous, disk) {
			t.Fatal("rewriting the same session reused the previous ciphertext")
		}
		previous = disk
	}

	reopened, err := NewFileCache(dir)
	if err != nil {
		t.Fatal(err)
	}
	data, exists, err := reopened.ReadRaw(session)
	if err != nil || !exists || !bytes.Equal(data, payload) {
		t.Fatalf("reopen encrypted session: present=%v err=%v", exists, err)
	}

	otherPath, err := cache.dataPath(otherSession)
	if err != nil {
		t.Fatal(err)
	}
	if err := securestore.UpdateFile(otherPath, 0o600, func([]byte) ([]byte, error) {
		return previous, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := reopened.ReadRaw(otherSession); !errors.Is(err, errCacheInvalid) {
		t.Fatalf("read a different session's ciphertext: %v", err)
	}

	previous[len(previous)-1] ^= 1
	if err := securestore.UpdateFile(path, 0o600, func([]byte) ([]byte, error) {
		return previous, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := reopened.ReadRaw(session); !errors.Is(err, errCacheInvalid) {
		t.Fatalf("read tampered ciphertext: %v", err)
	}
}

func TestFileCacheRejectsLegacyPlaintextInBusinessRequests(t *testing.T) {
	const session = "trn:iam::1:login/session/legacy"
	dir := t.TempDir()
	cache, err := NewFileCache(dir)
	if err != nil {
		t.Fatal(err)
	}
	filename, err := CacheFilename(session)
	if err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(dir, filename)
	now := time.Now()
	if err := securestore.UpdateFile(legacyPath, 0o600, func([]byte) ([]byte, error) {
		return makeCacheBytes(session, now, 3600, "legacy-refresh-token"), nil
	}); err != nil {
		t.Fatal(err)
	}
	if data, exists, err := cache.ReadRaw(session); !errors.Is(err, ErrLegacyCache) || exists || data != nil {
		t.Fatalf("legacy file should be recognized without returning its payload: present=%v err=%v", exists, err)
	}
	provider := NewProvider(session, cache, func(string) (OAuthClient, error) {
		t.Fatal("legacy cache must not be used to refresh")
		return nil, nil
	}, time.Now)
	var authErr *auth.Error
	if _, err := provider.Retrieve(context.Background()); !errors.Is(err, ErrLegacyCache) || !errors.As(err, &authErr) || authErr.Kind != auth.ReauthRequired {
		t.Fatalf("retrieve legacy cache: %v", err)
	}
	if err := cache.WithLock(context.Background(), session, func() error {
		return cache.Delete(session)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legacyPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy file not removed: %v", err)
	}
}

func TestLoginLegacyCacheSurvivesConfigFailure(t *testing.T) {
	const session = "trn:iam::1:user/legacy-config-failure"
	const profile = "existing"
	dir := t.TempDir()
	cache, err := NewFileCache(dir)
	if err != nil {
		t.Fatal(err)
	}
	legacyName, err := CacheFilename(session)
	if err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(dir, legacyName)
	cipherPath, err := cache.dataPath(session)
	if err != nil {
		t.Fatal(err)
	}
	legacy := []byte(`{"refresh_token":"synthetic-legacy-token"}`)
	if err := securestore.UpdateFile(legacyPath, 0o600, func([]byte) ([]byte, error) {
		return legacy, nil
	}); err != nil {
		t.Fatal(err)
	}

	cfg := config.DefaultConfig()
	cfg.PutProfile(profile, config.Profile{Mode: config.AuthModeConsoleLogin, LoginSession: session})
	updateErr := errors.New("config cannot be saved")
	store := &fakeProfileStore{
		cfg: cfg,
		updateFn: func(_ string, _ func(*config.Config) error) (config.Config, error) {
			ciphertext, err := os.ReadFile(cipherPath)
			if err != nil || !bytes.HasPrefix(ciphertext, []byte(cacheHeader)) {
				t.Fatalf("encrypted cache must exist before config update: %v", err)
			}
			old, err := os.ReadFile(legacyPath)
			if err != nil || !bytes.Equal(old, legacy) {
				t.Fatalf("legacy cache must survive until config update: %v", err)
			}
			return config.Config{}, updateErr
		},
	}
	client := &fakeOAuthClient{exchangeResp: validTokenResponse(session), endpointURL: DefaultEndpoint}
	service := newLoginService(t, client, cache, store)
	if _, err := service.Login(context.Background(), LoginOptions{Profile: profile}); !errors.Is(err, updateErr) {
		t.Fatalf("login should report the config update failure: %v", err)
	}
	old, err := os.ReadFile(legacyPath)
	if err != nil || !bytes.Equal(old, legacy) {
		t.Fatalf("config rollback should retain the legacy cache: %v", err)
	}
	if _, err := os.Stat(cipherPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("config rollback should remove the new encrypted cache: %v", err)
	}
}

func TestLoginKeepsEncryptedSessionWhenLegacyCleanupFails(t *testing.T) {
	const session = "trn:iam::1:user/legacy-cleanup-failure"
	const profile = "existing"
	cache, err := NewFileCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.WriteRaw(session, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	name, err := CacheFilename(session)
	if err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(cache.dir, name)
	if err := os.Mkdir(legacyPath, 0o700); err != nil {
		t.Fatal(err)
	}

	cfg := config.DefaultConfig()
	cfg.PutProfile(profile, config.Profile{Mode: config.AuthModeConsoleLogin, LoginSession: session})
	store := &fakeProfileStore{cfg: cfg}
	token := validTokenResponse(session)
	client := &fakeOAuthClient{exchangeResp: token, endpointURL: DefaultEndpoint}
	service := newLoginService(t, client, cache, store)
	options := LoginOptions{Profile: profile, Region: "cn-beijing"}

	_, err = service.Login(context.Background(), options)
	diagnostic := DiagnoseError(err)
	if !errors.Is(err, securestore.ErrInvalidPath) || diagnostic.Stage != LoginStageCache || diagnostic.Message != diagnosticLegacyCleanup {
		t.Fatalf("login should report its committed state and cleanup failure: %v, %+v", err, diagnostic)
	}
	updated, ok := store.cfg.GetProfile(profile)
	if !ok || updated.LoginSession != session || updated.Region != options.Region {
		t.Fatal("config update should remain committed after legacy cleanup fails")
	}
	data, present, err := cache.ReadRaw(session)
	if err != nil || !present || !bytes.Contains(data, []byte(token.RefreshToken)) {
		t.Fatalf("new encrypted session should remain usable: present=%v err=%v", present, err)
	}

	if err := os.Remove(legacyPath); err != nil {
		t.Fatal(err)
	}
	if err := securestore.UpdateFile(legacyPath, 0o600, func([]byte) ([]byte, error) {
		return []byte(`{"refresh_token":"synthetic-legacy-token"}`), nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Login(context.Background(), options); err != nil {
		t.Fatalf("retry login: %v", err)
	}
	if _, err := os.Stat(legacyPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("retry should clean up the remaining legacy file: %v", err)
	}
}
