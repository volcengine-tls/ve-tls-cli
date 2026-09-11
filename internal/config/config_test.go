package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestLoadLegacyConfigMissingModeRemainsAK(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("VOLCLOG_CONFIG", path)
	const legacyJSON = `{
  "version": 1,
  "current_profile": "legacy",
  "profiles": {
    "legacy": {
      "access_key_id": "legacy-ak",
      "secret_access_key": "legacy-sk",
      "security_token": "legacy-token",
      "region": "cn-beijing",
      "endpoint": "https://tls-cn-beijing.volces.com",
      "timeout_seconds": 37,
      "cred_ref": "legacy-ref"
    }
  }
}`
	if err := os.WriteFile(path, []byte(legacyJSON), 0o600); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}

	cfg, _, err := Load()
	if err != nil {
		t.Fatalf("load legacy config: %v", err)
	}
	profile, ok := cfg.GetProfile("legacy")
	if !ok {
		t.Fatal("legacy profile not loaded")
	}
	if profile.Mode != "" {
		t.Fatalf("legacy profile mode=%q, want empty storage value", profile.Mode)
	}
	mode, err := NormalizeAuthMode(profile.Mode)
	if err != nil {
		t.Fatalf("normalize legacy mode: %v", err)
	}
	if mode != AuthModeAK {
		t.Fatalf("normalized legacy mode=%q, want %q", mode, AuthModeAK)
	}
}

func TestSaveLoadAuthFieldsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("VOLCLOG_CONFIG", path)
	cfg := DefaultConfig()
	want := fullAuthProfile()
	cfg.PutProfile("full", want)

	if err := Save(cfg, path); err != nil {
		t.Fatalf("save config: %v", err)
	}
	loaded, _, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	got, ok := loaded.GetProfile("full")
	if !ok {
		t.Fatal("round-tripped profile not found")
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round-tripped profile mismatch:\ngot:  %+v\nwant: %+v", got, want)
	}
}

func TestWorkloadProfileFieldsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("VOLCLOG_CONFIG", path)
	cfg := DefaultConfig()
	want := Profile{
		Mode:          AuthModeOIDC,
		Region:        "cn-beijing",
		Endpoint:      "https://tls-cn-beijing.volces.com",
		OIDCTokenFile: "/var/run/secrets/token",
		RoleTRN:       "trn:iam::123456789012:role/my-role",
		DisableSSL:    true,
		AccountID:     "123456789012",
		RoleName:      "my-role",
	}
	cfg.PutProfile("workload", want)

	if err := Save(cfg, path); err != nil {
		t.Fatalf("save config: %v", err)
	}
	loaded, _, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	got, ok := loaded.GetProfile("workload")
	if !ok {
		t.Fatal("round-tripped profile not found")
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round-tripped profile mismatch:\ngot:  %+v\nwant: %+v", got, want)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	profiles, ok := raw["profiles"].(map[string]any)
	if !ok {
		t.Fatalf("profiles=%T, want object", raw["profiles"])
	}
	profile, ok := profiles["workload"].(map[string]any)
	if !ok {
		t.Fatalf("workload profile=%T, want object", profiles["workload"])
	}
	for _, key := range []string{"oidc-token-file", "role-trn", "disable-ssl"} {
		if _, exists := profile[key]; !exists {
			t.Fatalf("expected JSON key %q in profile: %s", key, data)
		}
	}
}

func TestSaveLoadSSOSessionsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("VOLCLOG_CONFIG", path)
	cfg := DefaultConfig()
	if cfg.SSOSessions == nil {
		t.Fatal("DefaultConfig must initialize SSO sessions")
	}
	want := SSOSession{
		Name:               "corp",
		StartURL:           "https://login.example.com/start",
		Region:             "cn-beijing",
		RegistrationScopes: []string{"openid", "profile"},
	}
	cfg.SSOSessions["corp"] = want

	if err := Save(cfg, path); err != nil {
		t.Fatalf("save config: %v", err)
	}
	loaded, _, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !reflect.DeepEqual(loaded.SSOSessions["corp"], want) {
		t.Fatalf("round-tripped SSO session mismatch:\ngot:  %+v\nwant: %+v", loaded.SSOSessions["corp"], want)
	}

	if err := os.WriteFile(path, []byte(`{"version":1}`), 0o600); err != nil {
		t.Fatalf("write config without SSO sessions: %v", err)
	}
	loaded, _, err = Load()
	if err != nil {
		t.Fatalf("load config without SSO sessions: %v", err)
	}
	if loaded.SSOSessions == nil {
		t.Fatal("Load must initialize missing SSO sessions")
	}
}

func TestValidateProfileModeRejectsUnknownMode(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want string
	}{
		{raw: "", want: AuthModeAK},
		{raw: " AK ", want: AuthModeAK},
		{raw: " SSO ", want: AuthModeSSO},
		{raw: " Console-Login ", want: AuthModeConsoleLogin},
	} {
		got, err := NormalizeAuthMode(tc.raw)
		if err != nil {
			t.Fatalf("NormalizeAuthMode(%q): %v", tc.raw, err)
		}
		if got != tc.want {
			t.Fatalf("NormalizeAuthMode(%q)=%q, want %q", tc.raw, got, tc.want)
		}
	}
	if _, err := NormalizeAuthMode("future-provider"); err == nil {
		t.Fatal("NormalizeAuthMode must reject an unknown mode")
	}
}

func TestNormalizeAuthModeSupportsWorkloadModes(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want string
	}{
		{raw: "", want: AuthModeAK},
		{raw: "ak", want: AuthModeAK},
		{raw: " AK ", want: AuthModeAK},
		{raw: "ramrolearn", want: AuthModeRamRoleARN},
		{raw: " RamRoleARN ", want: AuthModeRamRoleARN},
		{raw: "oidc", want: AuthModeOIDC},
		{raw: " OIDC ", want: AuthModeOIDC},
		{raw: "ecsrole", want: AuthModeECSRole},
		{raw: " ECSRole ", want: AuthModeECSRole},
		{raw: "sso", want: AuthModeSSO},
		{raw: "console-login", want: AuthModeConsoleLogin},
	} {
		got, err := NormalizeAuthMode(tc.raw)
		if err != nil {
			t.Fatalf("NormalizeAuthMode(%q): %v", tc.raw, err)
		}
		if got != tc.want {
			t.Fatalf("NormalizeAuthMode(%q)=%q, want %q", tc.raw, got, tc.want)
		}
	}
	if _, err := NormalizeAuthMode("unknown-mode"); err == nil {
		t.Fatal("NormalizeAuthMode must reject an unknown mode")
	}
}

func TestAuthModeClassification(t *testing.T) {
	for _, tc := range []struct {
		mode        string
		cachedLogin bool
		workload    bool
	}{
		{mode: AuthModeAK, cachedLogin: false, workload: false},
		{mode: AuthModeSSO, cachedLogin: true, workload: false},
		{mode: AuthModeConsoleLogin, cachedLogin: true, workload: false},
		{mode: AuthModeRamRoleARN, cachedLogin: false, workload: true},
		{mode: AuthModeOIDC, cachedLogin: false, workload: true},
		{mode: AuthModeECSRole, cachedLogin: false, workload: true},
		{mode: "unknown", cachedLogin: false, workload: false},
	} {
		if got := IsCachedLoginAuthMode(tc.mode); got != tc.cachedLogin {
			t.Fatalf("IsCachedLoginAuthMode(%q)=%v, want %v", tc.mode, got, tc.cachedLogin)
		}
		if got := IsWorkloadAuthMode(tc.mode); got != tc.workload {
			t.Fatalf("IsWorkloadAuthMode(%q)=%v, want %v", tc.mode, got, tc.workload)
		}
		wantProvider := tc.cachedLogin || tc.workload
		if got := IsProviderAuthMode(tc.mode); got != wantProvider {
			t.Fatalf("IsProviderAuthMode(%q)=%v, want %v", tc.mode, got, wantProvider)
		}
	}
}

func TestPatchAuthFieldsPreservesLegacyAndTLSFields(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CurrentProfile = "full"
	original := fullAuthProfile()
	cfg.PutProfile("full", original)

	if got := cfg.SelectedProfileName(" explicit "); got != "explicit" {
		t.Fatalf("explicit selected profile=%q, want explicit", got)
	}
	if got := cfg.SelectedProfileName(""); got != "full" {
		t.Fatalf("current selected profile=%q, want full", got)
	}
	cfg.CurrentProfile = ""
	if got := cfg.SelectedProfileName(""); got != "default" {
		t.Fatalf("fallback selected profile=%q, want default", got)
	}

	err := cfg.PatchProfile("full", func(profile *Profile) error {
		profile.Mode = AuthModeSSO
		profile.SSOSessionName = "patched-sso"
		profile.AccountID = "patched-account"
		profile.RoleName = "patched-role"
		return nil
	})
	if err != nil {
		t.Fatalf("patch SSO fields: %v", err)
	}
	afterSSO, _ := cfg.GetProfile("full")
	assertLegacyProfileFields(t, afterSSO, original)
	if afterSSO.LoginSession != original.LoginSession || afterSSO.STSExpiration != original.STSExpiration {
		t.Fatalf("SSO patch cleared console fields: %+v", afterSSO)
	}

	err = cfg.PatchProfile("full", func(profile *Profile) error {
		profile.Mode = AuthModeConsoleLogin
		profile.LoginSession = "patched-console"
		profile.STSExpiration = 1924992000
		return nil
	})
	if err != nil {
		t.Fatalf("patch console fields: %v", err)
	}
	afterConsole, _ := cfg.GetProfile("full")
	assertLegacyProfileFields(t, afterConsole, original)
	if afterConsole.SSOSessionName != "patched-sso" || afterConsole.AccountID != "patched-account" || afterConsole.RoleName != "patched-role" {
		t.Fatalf("console patch cleared SSO fields: %+v", afterConsole)
	}

	err = cfg.PatchProfile("full", func(profile *Profile) error {
		profile.Mode = AuthModeAK
		profile.AccessKeyID = "patched-ak"
		profile.SecretAccessKey = "patched-sk"
		return nil
	})
	if err != nil {
		t.Fatalf("patch static fields: %v", err)
	}
	afterStatic, _ := cfg.GetProfile("full")
	if afterStatic.AccessKeyID != "patched-ak" || afterStatic.SecretAccessKey != "patched-sk" {
		t.Fatalf("static fields not patched: %+v", afterStatic)
	}
	if afterStatic.SecurityToken != original.SecurityToken ||
		afterStatic.Region != original.Region ||
		afterStatic.Endpoint != original.Endpoint ||
		afterStatic.TimeoutSeconds != original.TimeoutSeconds ||
		afterStatic.CredRef != original.CredRef {
		t.Fatalf("static patch cleared unspecified legacy/TLS fields: %+v", afterStatic)
	}
	if afterStatic.SSOSessionName != "patched-sso" ||
		afterStatic.AccountID != "patched-account" ||
		afterStatic.RoleName != "patched-role" ||
		afterStatic.LoginSession != "patched-console" ||
		afterStatic.STSExpiration != 1924992000 {
		t.Fatalf("static patch cleared dormant auth fields: %+v", afterStatic)
	}

	beforeError := afterStatic
	sentinel := errors.New("stop patch")
	err = cfg.PatchProfile("full", func(profile *Profile) error {
		profile.AccessKeyID = "must-not-persist"
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("patch error=%v, want sentinel", err)
	}
	got, _ := cfg.GetProfile("full")
	if !reflect.DeepEqual(got, beforeError) {
		t.Fatalf("failed patch wrote profile:\ngot:  %+v\nwant: %+v", got, beforeError)
	}

	err = cfg.PatchProfile("new", func(profile *Profile) error {
		profile.Mode = AuthModeSSO
		profile.SSOSessionName = "new-sso"
		return nil
	})
	if err != nil {
		t.Fatalf("patch new profile: %v", err)
	}
	if got, ok := cfg.GetProfile("new"); !ok || got.SSOSessionName != "new-sso" {
		t.Fatalf("new profile patch not written: %+v, exists=%t", got, ok)
	}
}

func TestEffectiveProfile_ResolveCredRefRequiresExplicitRegion(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "")
	t.Setenv("VOLCENGINE_REGION", "")
	t.Setenv("VOLCENGINE_ENDPOINT", "")

	cfg := DefaultConfig()
	cfg.PutCred("ma-abc-root", Credential{AccessKeyID: "ak", SecretAccessKey: "sk"})
	cfg.PutProfile("p1", Profile{
		CredRef:  "ma-abc-root",
		Endpoint: "https://tls-cn-beijing.volces.com",
	})
	_, err := EffectiveProfile(cfg, "p1", ProfileDefaults{})
	if err == nil || err.Error() != "missing region" {
		t.Fatalf("expected missing region, got %v", err)
	}
}

func TestEffectiveProfile_EnvEndpointRequiresExplicitRegion(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "env-ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "env-sk")
	t.Setenv("VOLCENGINE_REGION", "")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")

	_, err := EffectiveProfile(DefaultConfig(), "", ProfileDefaults{})
	if err == nil || err.Error() != "missing region" {
		t.Fatalf("expected missing region, got %v", err)
	}
}

func TestResolveProfileCredentialStatus_UsesCredRef(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PutCred("shared", Credential{AccessKeyID: "ak", SecretAccessKey: "sk"})
	status := ResolveProfileCredentialStatus(cfg, Profile{
		CredRef: "shared",
	})
	if !status.Present || !status.AK || !status.SK {
		t.Fatalf("unexpected status: %+v", status)
	}
	if status.Source != "profile_cred_ref" {
		t.Fatalf("unexpected source: %q", status.Source)
	}
	if status.AccessKeyID != "ak" || status.SecretAccessKey != "sk" {
		t.Fatalf("unexpected credentials: %+v", status)
	}
}

func TestResolveEnvCredentialStatus(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "env-ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "env-sk")
	t.Setenv("VOLCENGINE_TOKEN", "env-token")
	status := ResolveEnvCredentialStatus()
	if !status.Present || !status.Token {
		t.Fatalf("unexpected status: %+v", status)
	}
	if status.Source != "env" || status.Mode != "sts" {
		t.Fatalf("unexpected env credential source/mode: %+v", status)
	}
}

func TestSaveDoesNotEscapeAngleBrackets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cfg := DefaultConfig()
	cfg.CurrentProfile = "prod<main>"
	if err := Save(cfg, path); err != nil {
		t.Fatalf("save error: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	out := string(b)
	if strings.Contains(out, `\u003c`) || strings.Contains(out, `\u003e`) {
		t.Fatalf("angle brackets should not be escaped: %q", out)
	}
}

func TestConfigSaveUsesUniqueAtomicReplacement(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "config")
	path := filepath.Join(dir, "config.json")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	fixedTemp := path + ".tmp"
	if err := os.WriteFile(fixedTemp, []byte("unrelated sentinel"), 0o600); err != nil {
		t.Fatalf("write fixed temp sentinel: %v", err)
	}
	cfg := DefaultConfig()
	cfg.CurrentProfile = "prod<main>"
	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	sentinel, err := os.ReadFile(fixedTemp)
	if err != nil {
		t.Fatalf("read fixed temp sentinel: %v", err)
	}
	if got := string(sentinel); got != "unrelated sentinel" {
		t.Fatalf("fixed .tmp file was reused: %q", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("config mode=%#o, want 0600", got)
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat config directory: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("config directory mode=%#o, want 0700", got)
	}
}

func TestConcurrentConfigPatchesDoNotProducePartialJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Save(DefaultConfig(), path); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	stop := make(chan struct{})
	readerErr := make(chan error, 1)
	var reads atomic.Int64
	go func() {
		for {
			select {
			case <-stop:
				readerErr <- nil
				return
			default:
			}
			data, err := os.ReadFile(path)
			if err != nil {
				readerErr <- fmt.Errorf("read config: %w", err)
				return
			}
			var cfg Config
			if err := json.Unmarshal(data, &cfg); err != nil {
				readerErr <- fmt.Errorf("partial JSON %q: %w", data, err)
				return
			}
			if cfg.Version != 1 {
				readerErr <- fmt.Errorf("version=%d, want 1", cfg.Version)
				return
			}
			reads.Add(1)
		}
	}()

	const writers = 16
	var wg sync.WaitGroup
	writerErr := make(chan error, writers)
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := Update(path, func(cfg *Config) error {
				cfg.PutProfile(fmt.Sprintf("profile-%02d", i), Profile{
					AccessKeyID:     fmt.Sprintf("ak-%02d", i),
					SecretAccessKey: fmt.Sprintf("sk-%02d", i),
				})
				return nil
			})
			writerErr <- err
		}()
	}
	wg.Wait()
	close(writerErr)
	close(stop)
	for err := range writerErr {
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
	}
	if err := <-readerErr; err != nil {
		t.Fatal(err)
	}
	if reads.Load() == 0 {
		t.Fatal("concurrent reader did not observe any config snapshots")
	}
}

func TestConcurrentConfigUpdatesPreserveDisjointProfiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := DefaultConfig()
	cfg.PutProfile("static", Profile{
		Mode:           AuthModeSSO,
		SSOSessionName: "dormant-sso",
		Region:         "cn-beijing",
	})
	cfg.PutProfile("dynamic", Profile{
		Mode:   AuthModeConsoleLogin,
		Region: "ap-singapore-1",
	})
	if err := Save(cfg, path); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	start := make(chan struct{})
	errs := make(chan error, 2)
	go func() {
		<-start
		_, err := Update(path, func(latest *Config) error {
			return latest.PatchProfile("static", func(profile *Profile) error {
				profile.Mode = AuthModeAK
				profile.AccessKeyID = "new-ak"
				profile.SecretAccessKey = "new-sk"
				return nil
			})
		})
		errs <- err
	}()
	go func() {
		<-start
		_, err := Update(path, func(latest *Config) error {
			return latest.PatchProfile("dynamic", func(profile *Profile) error {
				profile.LoginSession = "fresh-login"
				profile.STSExpiration = 1_900_000_000
				return nil
			})
		})
		errs <- err
	}()
	close(start)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("Update: %v", err)
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read final config: %v", err)
	}
	var got Config
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode final config: %v", err)
	}
	static, _ := got.GetProfile("static")
	if static.AccessKeyID != "new-ak" || static.SecretAccessKey != "new-sk" || static.Mode != AuthModeAK {
		t.Fatalf("static update missing: %+v", static)
	}
	if static.SSOSessionName != "dormant-sso" || static.Region != "cn-beijing" {
		t.Fatalf("static update lost dormant/non-conflicting fields: %+v", static)
	}
	dynamic, _ := got.GetProfile("dynamic")
	if dynamic.LoginSession != "fresh-login" || dynamic.STSExpiration != 1_900_000_000 {
		t.Fatalf("dynamic update missing: %+v", dynamic)
	}
	if dynamic.Region != "ap-singapore-1" || dynamic.Mode != AuthModeConsoleLogin {
		t.Fatalf("dynamic update lost non-conflicting fields: %+v", dynamic)
	}
}

func TestConcurrentConfigUpdatesOnSameProfilePreserveDisjointFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := DefaultConfig()
	cfg.PutProfile("shared", Profile{Mode: AuthModeConsoleLogin})
	if err := Save(cfg, path); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	start := make(chan struct{})
	errs := make(chan error, 2)
	patches := []func(*Profile){
		func(profile *Profile) { profile.Region = "cn-beijing" },
		func(profile *Profile) { profile.LoginSession = "session-1" },
	}
	for _, patch := range patches {
		go func() {
			<-start
			_, err := Update(path, func(latest *Config) error {
				return latest.PatchProfile("shared", func(profile *Profile) error {
					patch(profile)
					return nil
				})
			})
			errs <- err
		}()
	}
	close(start)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("Update: %v", err)
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read final config: %v", err)
	}
	var got Config
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode final config: %v", err)
	}
	profile, _ := got.GetProfile("shared")
	if profile.Region != "cn-beijing" || profile.LoginSession != "session-1" {
		t.Fatalf("same-profile disjoint patches were lost: %+v", profile)
	}
}

func TestConfigSaveFailurePreservesPreviousConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	old := DefaultConfig()
	old.CurrentProfile = "old"
	if err := Save(old, path); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	originalUpdateFile := updateFile
	t.Cleanup(func() { updateFile = originalUpdateFile })
	replaceErr := errors.New("injected atomic replacement failure")
	updateFile = func(string, os.FileMode, func([]byte) ([]byte, error)) error {
		return replaceErr
	}
	next := DefaultConfig()
	next.CurrentProfile = "new"
	err := Save(next, path)
	if !errors.Is(err, replaceErr) {
		t.Fatalf("Save error=%v, want injected failure", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read preserved config: %v", err)
	}
	var got Config
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode preserved config: %v", err)
	}
	if got.CurrentProfile != "old" {
		t.Fatalf("current profile=%q, want old config preserved", got.CurrentProfile)
	}
}

func TestConfigUpdateRejectsCorruptFileAndDoesNotRunCallback(t *testing.T) {
	for _, content := range [][]byte{[]byte{}, []byte("{not-json")} {
		path := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatalf("write corrupt config: %v", err)
		}
		called := false
		_, err := Update(path, func(*Config) error {
			called = true
			return nil
		})
		if err == nil {
			t.Fatalf("Update accepted corrupt content %q", content)
		}
		if called {
			t.Fatalf("callback ran for corrupt content %q", content)
		}
	}
}

func fullAuthProfile() Profile {
	return Profile{
		AccessKeyID:     "legacy-ak",
		SecretAccessKey: "legacy-sk",
		SecurityToken:   "legacy-token",
		Region:          "cn-beijing",
		Endpoint:        "https://tls-cn-beijing.volces.com",
		TimeoutSeconds:  37,
		CredRef:         "legacy-ref",
		Mode:            AuthModeConsoleLogin,
		SSOSessionName:  "legacy-sso",
		AccountID:       "legacy-account",
		RoleName:        "legacy-role",
		LoginSession:    "legacy-console",
		STSExpiration:   1893456000,
	}
}

func assertLegacyProfileFields(t *testing.T, got, want Profile) {
	t.Helper()
	if got.AccessKeyID != want.AccessKeyID ||
		got.SecretAccessKey != want.SecretAccessKey ||
		got.SecurityToken != want.SecurityToken ||
		got.Region != want.Region ||
		got.Endpoint != want.Endpoint ||
		got.TimeoutSeconds != want.TimeoutSeconds ||
		got.CredRef != want.CredRef {
		t.Fatalf("legacy/TLS fields changed:\ngot:  %+v\nwant: %+v", got, want)
	}
}

func TestSaveFailsClosedOnWidePermissionParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission semantics only")
	}
	parent := filepath.Join(t.TempDir(), "wide")
	if err := os.Mkdir(parent, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if err := os.Chmod(parent, 0o755); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	path := filepath.Join(parent, "config.json")
	err := Save(Config{Version: 1}, path)
	if err == nil {
		t.Fatalf("Save with 0755 parent: expected error, got nil")
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("config file should not exist after failed Save, stat err=%v", statErr)
	}
	parentInfo, err := os.Stat(parent)
	if err != nil {
		t.Fatalf("Stat parent: %v", err)
	}
	if got := parentInfo.Mode().Perm(); got != 0o755 {
		t.Fatalf("parent mode=%#o, want unchanged 0755", got)
	}
}
