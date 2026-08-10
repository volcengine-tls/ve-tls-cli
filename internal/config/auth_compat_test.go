package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyProfileWithoutModeUsesStaticAK(t *testing.T) {
	clearLegacyAuthEnvironment(t)

	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("VOLCLOG_CONFIG", path)
	const legacyJSON = `{
  "version": 1,
  "current_profile": "legacy",
  "profiles": {
    "legacy": {
      "access_key_id": "legacy-ak",
      "secret_access_key": "legacy-sk",
      "region": "cn-beijing",
      "endpoint": "https://tls-cn-beijing.volces.com"
    }
  }
}
`
	if err := os.WriteFile(path, []byte(legacyJSON), 0o600); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}

	cfg, loadedPath, err := Load()
	if err != nil {
		t.Fatalf("load legacy config: %v", err)
	}
	if loadedPath != path {
		t.Fatalf("loaded path=%q, want %q", loadedPath, path)
	}
	got, err := EffectiveProfile(cfg, "", ProfileDefaults{})
	if err != nil {
		t.Fatalf("resolve legacy profile: %v", err)
	}
	if got.AccessKeyID != "legacy-ak" || got.SecretAccessKey != "legacy-sk" {
		t.Fatalf("legacy credentials changed: %+v", got)
	}

	if err := Save(cfg, path); err != nil {
		t.Fatalf("save loaded legacy config: %v", err)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved legacy config: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(saved, &raw); err != nil {
		t.Fatalf("decode saved legacy config: %v", err)
	}
	profiles, ok := raw["profiles"].(map[string]any)
	if !ok {
		t.Fatalf("saved profiles=%T, want object", raw["profiles"])
	}
	profile, ok := profiles["legacy"].(map[string]any)
	if !ok {
		t.Fatalf("saved legacy profile=%T, want object", profiles["legacy"])
	}
	if _, exists := profile["mode"]; exists {
		t.Fatalf("loading and saving a legacy profile must not synthesize mode: %s", saved)
	}
}

func TestLegacyEnvironmentPairOverridesStaticProfile(t *testing.T) {
	clearLegacyAuthEnvironment(t)
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "env-ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "env-sk")
	t.Setenv("VOLCENGINE_TOKEN", "env-token")
	t.Setenv("VOLCENGINE_REGION", "ap-singapore-1")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-ap-singapore-1.volces.com")

	cfg := legacyStaticConfig()
	got, err := EffectiveProfile(cfg, "explicit", ProfileDefaults{TimeoutSeconds: 37})
	if err != nil {
		t.Fatalf("resolve environment credentials: %v", err)
	}
	if got.AccessKeyID != "env-ak" || got.SecretAccessKey != "env-sk" || got.SecurityToken != "env-token" {
		t.Fatalf("environment credential pair did not replace the static profile as a group: %+v", got)
	}
	if got.Region != "ap-singapore-1" || got.Endpoint != "https://tls-ap-singapore-1.volces.com" {
		t.Fatalf("environment routing did not replace profile routing: %+v", got)
	}
	if got.TimeoutSeconds != 37 {
		t.Fatalf("environment profile timeout=%d, want project default 37", got.TimeoutSeconds)
	}
}

func TestLegacyPartialEnvironmentPairDoesNotOverrideProfile(t *testing.T) {
	for _, tc := range []struct {
		name string
		ak   string
		sk   string
	}{
		{name: "access key only", ak: "partial-env-ak"},
		{name: "secret key only", sk: "partial-env-sk"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clearLegacyAuthEnvironment(t)
			t.Setenv("VOLCENGINE_ACCESS_KEY_ID", tc.ak)
			t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", tc.sk)
			t.Setenv("VOLCENGINE_TOKEN", "partial-env-token")
			t.Setenv("VOLCENGINE_REGION", "ap-singapore-1")
			t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-ap-singapore-1.volces.com")

			got, err := EffectiveProfile(legacyStaticConfig(), "explicit", ProfileDefaults{})
			if err != nil {
				t.Fatalf("resolve static profile with partial environment pair: %v", err)
			}
			if got.AccessKeyID != "explicit-ak" || got.SecretAccessKey != "explicit-sk" || got.SecurityToken != "explicit-token" {
				t.Fatalf("partial environment pair must not replace static credentials: %+v", got)
			}
			if got.Region != "cn-beijing" || got.Endpoint != "https://explicit.example.com" {
				t.Fatalf("partial environment pair must not replace static routing: %+v", got)
			}
		})
	}
}

func TestLegacyProfileSelectionOrder(t *testing.T) {
	clearLegacyAuthEnvironment(t)
	cfg := legacyStaticConfig()

	got, err := EffectiveProfile(cfg, "explicit", ProfileDefaults{})
	if err != nil {
		t.Fatalf("resolve explicit profile: %v", err)
	}
	if got.AccessKeyID != "explicit-ak" {
		t.Fatalf("explicit profile lost precedence: %+v", got)
	}

	got, err = EffectiveProfile(cfg, "", ProfileDefaults{})
	if err != nil {
		t.Fatalf("resolve current profile: %v", err)
	}
	if got.AccessKeyID != "current-ak" {
		t.Fatalf("current profile lost precedence: %+v", got)
	}

	cfg.CurrentProfile = ""
	got, err = EffectiveProfile(cfg, "", ProfileDefaults{})
	if err != nil {
		t.Fatalf("resolve default profile: %v", err)
	}
	if got.AccessKeyID != "default-ak" {
		t.Fatalf("default profile fallback changed: %+v", got)
	}
}

func TestLegacyCredRefResolutionUnchanged(t *testing.T) {
	clearLegacyAuthEnvironment(t)
	cfg := DefaultConfig()
	cfg.PutCred("shared", Credential{
		AccessKeyID:     "ref-ak",
		SecretAccessKey: "ref-sk",
	})
	cfg.PutProfile("partial-inline", Profile{
		AccessKeyID:   "inline-ak",
		SecurityToken: "inline-token",
		Region:        "cn-beijing",
		Endpoint:      "https://tls-cn-beijing.volces.com",
		CredRef:       "shared",
	})
	cfg.PutProfile("complete-inline", Profile{
		AccessKeyID:     "inline-ak",
		SecretAccessKey: "inline-sk",
		Region:          "cn-beijing",
		Endpoint:        "https://tls-cn-beijing.volces.com",
		CredRef:         "shared",
	})

	partial, err := EffectiveProfile(cfg, "partial-inline", ProfileDefaults{})
	if err != nil {
		t.Fatalf("resolve partial inline profile: %v", err)
	}
	if partial.AccessKeyID != "inline-ak" || partial.SecretAccessKey != "ref-sk" || partial.SecurityToken != "inline-token" {
		t.Fatalf("cred-ref must only fill missing inline fields: %+v", partial)
	}

	complete, err := EffectiveProfile(cfg, "complete-inline", ProfileDefaults{})
	if err != nil {
		t.Fatalf("resolve complete inline profile: %v", err)
	}
	if complete.AccessKeyID != "inline-ak" || complete.SecretAccessKey != "inline-sk" {
		t.Fatalf("complete inline credentials must win over cred-ref: %+v", complete)
	}
}

func TestLegacyProjectDefaultsUnchanged(t *testing.T) {
	clearLegacyAuthEnvironment(t)
	dir := t.TempDir()
	projectDir := filepath.Join(dir, ".volclog")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatalf("create project config directory: %v", err)
	}
	projectPath := filepath.Join(projectDir, "cli.config.json")
	const projectJSON = `{
  "region": " cn-beijing ",
  "endpoint": " https://tls-cn-beijing.volces.com ",
  "timeout_seconds": 41
}
`
	if err := os.WriteFile(projectPath, []byte(projectJSON), 0o600); err != nil {
		t.Fatalf("write project config: %v", err)
	}
	project, loadedPath, err := LoadProjectConfig(dir)
	if err != nil {
		t.Fatalf("load project defaults: %v", err)
	}
	if loadedPath != projectPath {
		t.Fatalf("project config path=%q, want %q", loadedPath, projectPath)
	}

	cfg := DefaultConfig()
	cfg.PutProfile("default", Profile{
		AccessKeyID:     "legacy-ak",
		SecretAccessKey: "legacy-sk",
	})
	got, err := EffectiveProfile(cfg, "default", ProfileDefaults{
		Region:         project.Region,
		Endpoint:       project.Endpoint,
		TimeoutSeconds: project.TimeoutSeconds,
	})
	if err != nil {
		t.Fatalf("apply project defaults: %v", err)
	}
	if got.Region != "cn-beijing" || got.Endpoint != "https://tls-cn-beijing.volces.com" || got.TimeoutSeconds != 41 {
		t.Fatalf("project defaults changed: %+v", got)
	}

	cfg.PutProfile("profile-values", Profile{
		AccessKeyID:     "legacy-ak",
		SecretAccessKey: "legacy-sk",
		Region:          "ap-singapore-1",
		Endpoint:        "https://profile.example.com",
		TimeoutSeconds:  19,
	})
	got, err = EffectiveProfile(cfg, "profile-values", ProfileDefaults{
		Region:         project.Region,
		Endpoint:       project.Endpoint,
		TimeoutSeconds: project.TimeoutSeconds,
	})
	if err != nil {
		t.Fatalf("resolve profile values over project defaults: %v", err)
	}
	if got.Region != "ap-singapore-1" || got.Endpoint != "https://profile.example.com" || got.TimeoutSeconds != 19 {
		t.Fatalf("profile routing must retain precedence over project defaults: %+v", got)
	}
}

func clearLegacyAuthEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"VOLCENGINE_ACCESS_KEY_ID",
		"VOLCENGINE_ACCESS_KEY_SECRET",
		"VOLCENGINE_TOKEN",
		"VOLCENGINE_REGION",
		"VOLCENGINE_ENDPOINT",
	} {
		t.Setenv(key, "")
	}
}

func legacyStaticConfig() Config {
	cfg := DefaultConfig()
	cfg.CurrentProfile = "current"
	cfg.PutProfile("explicit", Profile{
		AccessKeyID:     "explicit-ak",
		SecretAccessKey: "explicit-sk",
		SecurityToken:   "explicit-token",
		Region:          "cn-beijing",
		Endpoint:        "https://explicit.example.com",
		TimeoutSeconds:  23,
	})
	cfg.PutProfile("current", Profile{
		AccessKeyID:     "current-ak",
		SecretAccessKey: "current-sk",
		Region:          "cn-beijing",
		Endpoint:        "https://current.example.com",
	})
	cfg.PutProfile("default", Profile{
		AccessKeyID:     "default-ak",
		SecretAccessKey: "default-sk",
		Region:          "cn-beijing",
		Endpoint:        "https://default.example.com",
	})
	return cfg
}

func TestLegacyConfigWithoutWorkloadFieldsStillLoads(t *testing.T) {
	clearLegacyAuthEnvironment(t)

	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("VOLCLOG_CONFIG", path)
	const legacyJSON = `{
  "version": 1,
  "current_profile": "legacy",
  "profiles": {
    "legacy": {
      "access_key_id": "legacy-ak",
      "secret_access_key": "legacy-sk",
      "region": "cn-beijing",
      "endpoint": "https://tls-cn-beijing.volces.com"
    }
  }
}
`
	if err := os.WriteFile(path, []byte(legacyJSON), 0o600); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}

	cfg, _, err := Load()
	if err != nil {
		t.Fatalf("load legacy config: %v", err)
	}
	got, ok := cfg.GetProfile("legacy")
	if !ok {
		t.Fatal("legacy profile not loaded")
	}
	if got.OIDCTokenFile != "" {
		t.Fatalf("legacy profile OIDCTokenFile=%q, want empty", got.OIDCTokenFile)
	}
	if got.RoleTRN != "" {
		t.Fatalf("legacy profile RoleTRN=%q, want empty", got.RoleTRN)
	}
	if got.DisableSSL {
		t.Fatalf("legacy profile DisableSSL=%v, want false", got.DisableSSL)
	}

	resolved, err := EffectiveProfile(cfg, "", ProfileDefaults{})
	if err != nil {
		t.Fatalf("resolve legacy profile: %v", err)
	}
	if resolved.AccessKeyID != "legacy-ak" || resolved.SecretAccessKey != "legacy-sk" {
		t.Fatalf("legacy credentials changed: %+v", resolved)
	}
}
