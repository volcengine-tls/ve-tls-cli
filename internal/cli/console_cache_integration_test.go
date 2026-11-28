package cli

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth/console"
	"github.com/volcengine-tls/ve-tls-cli/internal/config"
	"github.com/volcengine-tls/ve-tls-cli/internal/securestore"
)

func TestConsoleLoginCacheAcrossProcesses(t *testing.T) {
	const certEnv = "VOLCLOG_TEST_CONSOLE_CACHE_CERT"
	if encoded := os.Getenv(certEnv); encoded != "" {
		der, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			t.Fatal("decode mock server certificate")
		}
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			t.Fatal("parse mock server certificate")
		}
		roots := x509.NewCertPool()
		roots.AddCert(cert)
		http.DefaultTransport = &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}}
		index := slices.Index(os.Args, "--")
		os.Exit(Run(os.Args[index+1:], os.Stdout, os.Stderr))
	}

	clearAuthTestEnv(t)
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "cache")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(dir, "config.json"))
	t.Setenv("VOLCLOG_LOGIN_CACHE_DIRECTORY", cacheDir)

	const profile = "console-cache-test"
	const session = "trn:iam::1:user/console-cache-test"
	const refreshToken = "synthetic-refresh-cached-only"
	const secretKey = "synthetic-secret-key-cached-only"
	idToken := validIDTokenForTest(session)
	var refreshes atomic.Int32
	var queries atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case console.DeviceAuthorizationPath:
			fmt.Fprintf(w, `{"device_code":"synthetic-device-code","user_code":"TEST-ONLY","verification_uri":"https://%s/verify","expires_in":90,"interval":1}`, r.Host)
		case console.TokenPath:
			round := int32(0)
			response := map[string]any{"expires_in": 10}
			switch r.FormValue("grant_type") {
			case console.GrantTypeDeviceCode:
				response["id_token"] = idToken
				response["token_type"] = "sts"
				response["refresh_token"] = refreshToken
			case console.GrantTypeRefreshToken:
				if r.FormValue("refresh_token") != refreshToken {
					t.Error("refresh used the wrong token")
					http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
					return
				}
				round = refreshes.Add(1)
			default:
				t.Error("unexpected token grant")
				http.Error(w, "unexpected grant", http.StatusBadRequest)
				return
			}
			response["access_token"] = console.STSCredentials{
				AccessKeyID:     fmt.Sprintf("AKLTsynthetic%04d", round),
				SecretAccessKey: secretKey,
				SessionToken:    fmt.Sprintf("synthetic-session-token-%d", round),
			}
			_ = json.NewEncoder(w).Encode(response)
		case "/DescribeProjects":
			round := queries.Add(1)
			if got := r.Header.Get("X-Security-Token"); got != fmt.Sprintf("synthetic-session-token-%d", round) {
				t.Error("business request did not use the latest refreshed STS token")
			}
			if !strings.Contains(r.Header.Get("Authorization"), fmt.Sprintf("AKLTsynthetic%04d", round)) {
				t.Error("business request was not signed with the latest STS key")
			}
			fmt.Fprint(w, `{"Projects":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	assertRedacted := func(data []byte) {
		t.Helper()
		for _, secret := range []string{refreshToken, secretKey, idToken, "synthetic-session-token-", "AKLTsynthetic000", "synthetic-device-code"} {
			if bytes.Contains(data, []byte(secret)) {
				t.Fatal("synthetic credential found in persisted data or CLI output")
			}
		}
	}
	runForProfile := func(want int, name string, args ...string) []byte {
		t.Helper()
		childArgs := append([]string{"-test.run=^TestConsoleLoginCacheAcrossProcesses$", "--", "--profile", name, "--output", "json"}, args...)
		cmd := exec.Command(binary, childArgs...)
		cmd.Env = append(os.Environ(), certEnv+"="+base64.StdEncoding.EncodeToString(server.Certificate().Raw))
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		code := 0
		if err != nil {
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) {
				t.Fatal(err)
			}
			code = exitErr.ExitCode()
		}
		assertRedacted(stdout.Bytes())
		assertRedacted(stderr.Bytes())
		if code != want {
			t.Fatalf("%v exited %d, want %d\nstdout: %s\nstderr: %s", args, code, want, stdout.Bytes(), stderr.Bytes())
		}
		return stdout.Bytes()
	}
	run := func(want int, args ...string) []byte {
		t.Helper()
		return runForProfile(want, profile, args...)
	}

	run(0, "login", "--no-browser", "--login-endpoint", server.URL, "--region", "cn-beijing", "--endpoint", server.URL)
	if runtime.GOOS == "darwin" {
		canonical, err := filepath.EvalSymlinks(cacheDir)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			// This item belongs to the unique test directory.
			_ = exec.Command("/usr/bin/security", "delete-generic-password", "-s", "volclog.console-login", "-a", securestore.DigestKey(canonical)).Run()
		})
	}
	traceDir := filepath.Join(dir, "trace")
	for i := 0; i < 2; i++ {
		run(0, "--trace-dir", traceDir, "raw", "--method", "GET", "--path", "/DescribeProjects")
	}
	if refreshes.Load() != 2 || queries.Load() != 2 {
		t.Fatalf("refreshes=%d queries=%d, want two of each", refreshes.Load(), queries.Load())
	}
	run(0, "doctor")
	if refreshes.Load() != 2 {
		t.Fatal("offline doctor refreshed the session")
	}
	if err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		assertRedacted(data)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Model two existing profiles that share a pre-upgrade session.
	const sharedProfile = "console-shared"
	cfg, cfgPath, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	original, ok := cfg.GetProfile(profile)
	if !ok {
		t.Fatal("fixture profile missing")
	}
	original.TimeoutSeconds = 19
	shared := original
	shared.TimeoutSeconds = 23
	if _, err := config.Update(cfgPath, func(c *config.Config) error {
		c.PutProfile(profile, original)
		c.PutProfile(sharedProfile, shared)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	legacyName, err := console.CacheFilename(session)
	if err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(cacheDir, legacyName)
	encryptedPath := strings.TrimSuffix(legacyPath, ".json") + ".enc"
	if err := os.Remove(encryptedPath); err != nil {
		t.Fatal(err)
	}
	if err := securestore.UpdateFile(legacyPath, 0o600, func([]byte) ([]byte, error) {
		return []byte(`{"refresh_token":"synthetic-legacy-token"}`), nil
	}); err != nil {
		t.Fatal(err)
	}
	doctor := run(2, "doctor")
	if !bytes.Contains(doctor, []byte("legacy_plaintext")) || !bytes.Contains(doctor, []byte("login with this profile")) {
		t.Fatal("doctor did not explain how to upgrade the legacy plaintext cache")
	}
	run(0, "login", "--no-browser", "--login-endpoint", server.URL)
	if _, err := os.Stat(legacyPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("login did not remove the legacy plaintext cache: %v", err)
	}
	upgraded, _, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if current, ok := upgraded.GetProfile(profile); !ok || current != original {
		t.Fatal("migration changed the original profile settings")
	}
	if current, ok := upgraded.GetProfile(sharedProfile); !ok || current != shared {
		t.Fatal("migration changed the shared profile settings")
	}
	runForProfile(0, sharedProfile, "raw", "--method", "GET", "--path", "/DescribeProjects")
	if refreshes.Load() != 3 || queries.Load() != 3 {
		t.Fatalf("shared profile failed to use the migrated cache: refreshes=%d queries=%d", refreshes.Load(), queries.Load())
	}
	run(0, "logout")
	if _, err := os.Stat(encryptedPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("logout did not remove the encrypted cache: %v", err)
	}
	run(2, "doctor")
	runForProfile(2, sharedProfile, "doctor")
}
