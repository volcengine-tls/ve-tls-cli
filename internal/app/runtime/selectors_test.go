package runtime

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/volcengine-tls/ve-tls-cli/internal/config"
)

func TestResolveSelectors(t *testing.T) {
	tests := []struct {
		name    string
		spec    SelectorSet
		want    ResolvedSelectors
		wantErr string
	}{
		{
			name: "global profile",
			spec: SelectorSet{GlobalProfile: " global "},
			want: ResolvedSelectors{Profile: "global"},
		},
		{
			name: "same profile selectors coexist",
			spec: SelectorSet{GlobalProfile: "shared", ContextProfile: " shared "},
			want: ResolvedSelectors{Profile: "shared"},
		},
		{
			name:    "different profiles conflict",
			spec:    SelectorSet{GlobalProfile: "one", ContextProfile: "two"},
			wantErr: "conflicting profile selectors: global --profile=one conflicts with context.profile=two",
		},
		{
			name:    "global profile conflicts with global secrets",
			spec:    SelectorSet{GlobalProfile: "one", GlobalSecretsFile: "creds.env"},
			wantErr: "conflicting runtime selectors: global --profile=one conflicts with global --secrets-file=creds.env",
		},
		{
			name:    "context profile conflicts with context secrets",
			spec:    SelectorSet{ContextProfile: "one", ContextSecretsFile: "creds.env"},
			wantErr: "conflicting runtime selectors: context.profile=one conflicts with context.secrets_file=creds.env",
		},
		{
			name:    "global profile conflicts with context secrets",
			spec:    SelectorSet{GlobalProfile: "one", ContextSecretsFile: "creds.env"},
			wantErr: "conflicting runtime selectors: global --profile=one conflicts with context.secrets_file=creds.env",
		},
		{
			name:    "global secrets conflicts with context profile",
			spec:    SelectorSet{GlobalSecretsFile: "creds.env", ContextProfile: "one"},
			wantErr: "conflicting runtime selectors: global --secrets-file=creds.env conflicts with context.profile=one",
		},
		{
			name:    "secrets selectors conflict",
			spec:    SelectorSet{GlobalSecretsFile: "one.env", ContextSecretsFile: "two.env"},
			wantErr: "conflicting runtime selectors: global --secrets-file=one.env conflicts with context.secrets_file=two.env",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveSelectors(tc.spec)
			if tc.wantErr != "" {
				if err == nil || err.Error() != tc.wantErr {
					t.Fatalf("ResolveSelectors error=%v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveSelectors: %v", err)
			}
			if got != tc.want {
				t.Fatalf("ResolveSelectors=%+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestResolverProfileSelectionPrecedence(t *testing.T) {
	cfg := config.Config{
		CurrentProfile: "current",
		Profiles: map[string]config.Profile{
			"explicit": staticProfile("explicit"),
			"current":  staticProfile("current"),
			"default":  staticProfile("default"),
		},
	}
	resolver := Resolver{LookupEnv: emptyEnv}

	got, err := resolver.Resolve(ResolveRequest{Config: cfg, ExplicitProfile: " explicit "})
	if err != nil {
		t.Fatalf("explicit Resolve: %v", err)
	}
	if got.ProfileName != "explicit" || got.Profile.AccessKeyID != "explicit-ak" {
		t.Fatalf("explicit resolution=%+v", got)
	}

	got, err = resolver.Resolve(ResolveRequest{Config: cfg})
	if err != nil {
		t.Fatalf("current Resolve: %v", err)
	}
	if got.ProfileName != "current" || got.Profile.AccessKeyID != "current-ak" {
		t.Fatalf("current resolution=%+v", got)
	}

	cfg.CurrentProfile = ""
	got, err = resolver.Resolve(ResolveRequest{Config: cfg})
	if err != nil {
		t.Fatalf("default Resolve: %v", err)
	}
	if got.ProfileName != "default" || got.Profile.AccessKeyID != "default-ak" {
		t.Fatalf("default resolution=%+v", got)
	}
}

func TestResolverStaticEnvironmentCredentials(t *testing.T) {
	cfg := config.Config{Profiles: map[string]config.Profile{
		"default": {
			Mode:            config.AuthModeAK,
			AccessKeyID:     "profile-ak",
			SecretAccessKey: "profile-sk",
			SecurityToken:   "profile-token",
			Region:          "profile-region",
			Endpoint:        "profile-endpoint",
		},
	}}
	tests := []struct {
		name  string
		env   map[string]string
		wantA string
		wantS string
		wantT string
	}{
		{
			name: "complete environment overrides credentials",
			env: map[string]string{
				"VOLCENGINE_ACCESS_KEY_ID":     "env-ak",
				"VOLCENGINE_ACCESS_KEY_SECRET": "env-sk",
				"VOLCENGINE_TOKEN":             "env-token",
				"VOLCENGINE_REGION":            "env-region",
				"VOLCENGINE_ENDPOINT":          "env-endpoint",
			},
			wantA: "env-ak",
			wantS: "env-sk",
			wantT: "env-token",
		},
		{
			name: "partial environment does not override profile",
			env: map[string]string{
				"VOLCENGINE_ACCESS_KEY_ID": "partial-ak",
				"VOLCENGINE_TOKEN":         "partial-token",
			},
			wantA: "profile-ak",
			wantS: "profile-sk",
			wantT: "profile-token",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := (Resolver{LookupEnv: mapEnv(tc.env)}).Resolve(ResolveRequest{Config: cfg})
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if got.Profile.AccessKeyID != tc.wantA ||
				got.Profile.SecretAccessKey != tc.wantS ||
				got.Profile.SecurityToken != tc.wantT {
				t.Fatalf("credentials=%+v, want ak=%q sk=%q token=%q", got.Profile, tc.wantA, tc.wantS, tc.wantT)
			}
		})
	}
}

func TestResolverRuntimeSettingPrecedence(t *testing.T) {
	cfg := config.Config{Profiles: map[string]config.Profile{
		"default": {
			Mode:           config.AuthModeConsoleLogin,
			Region:         "profile-region",
			Endpoint:       "profile-endpoint",
			TimeoutSeconds: 7,
		},
	}}
	defaults := config.ProfileDefaults{
		Region:         "project-region",
		Endpoint:       "project-endpoint",
		TimeoutSeconds: 11,
	}

	tests := []struct {
		name         string
		profile      config.Profile
		env          map[string]string
		runtime      ResolveRequest
		wantRegion   string
		wantEndpoint string
		wantTimeout  int
	}{
		{
			name:         "flags override environment and profile",
			env:          map[string]string{"VOLCENGINE_REGION": "env-region", "VOLCENGINE_ENDPOINT": "env-endpoint"},
			runtime:      ResolveRequest{RuntimeRegion: "flag-region", RuntimeEndpoint: "flag-endpoint"},
			wantRegion:   "flag-region",
			wantEndpoint: "flag-endpoint",
			wantTimeout:  7,
		},
		{
			name:         "environment overrides profile",
			env:          map[string]string{"VOLCENGINE_REGION": "env-region", "VOLCENGINE_ENDPOINT": "env-endpoint"},
			wantRegion:   "env-region",
			wantEndpoint: "env-endpoint",
			wantTimeout:  7,
		},
		{
			name:         "profile overrides project",
			wantRegion:   "profile-region",
			wantEndpoint: "profile-endpoint",
			wantTimeout:  7,
		},
		{
			name: "project fills empty profile",
			profile: config.Profile{
				Mode: config.AuthModeConsoleLogin,
			},
			wantRegion:   "project-region",
			wantEndpoint: "project-endpoint",
			wantTimeout:  11,
		},
		{
			name: "built-in timeout is sixty",
			profile: config.Profile{
				Mode:     config.AuthModeConsoleLogin,
				Region:   "profile-region",
				Endpoint: "profile-endpoint",
			},
			runtime:      ResolveRequest{Defaults: config.ProfileDefaults{}},
			wantRegion:   "profile-region",
			wantEndpoint: "profile-endpoint",
			wantTimeout:  60,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testCfg := cfg
			testCfg.Profiles = map[string]config.Profile{"default": cfg.Profiles["default"]}
			if tc.profile != (config.Profile{}) {
				testCfg.Profiles["default"] = tc.profile
			}
			req := tc.runtime
			req.Config = testCfg
			if req.Defaults == (config.ProfileDefaults{}) && tc.name != "built-in timeout is sixty" {
				req.Defaults = defaults
			}
			got, err := (Resolver{LookupEnv: mapEnv(tc.env)}).Resolve(req)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if got.Profile.Region != tc.wantRegion ||
				got.Profile.Endpoint != tc.wantEndpoint ||
				got.Profile.TimeoutSeconds != tc.wantTimeout {
				t.Fatalf("runtime=%+v, want region=%q endpoint=%q timeout=%d", got.Profile, tc.wantRegion, tc.wantEndpoint, tc.wantTimeout)
			}
		})
	}
}

func TestResolverStaticRuntimeCompatibility(t *testing.T) {
	profile := config.Profile{
		Mode:            config.AuthModeAK,
		AccessKeyID:     "profile-ak",
		SecretAccessKey: "profile-sk",
		SecurityToken:   "profile-token",
		Region:          "profile-region",
		Endpoint:        "profile-endpoint",
		TimeoutSeconds:  7,
	}
	cfg := config.Config{Profiles: map[string]config.Profile{"default": profile}}
	defaults := config.ProfileDefaults{
		Region:         "project-region",
		Endpoint:       "project-endpoint",
		TimeoutSeconds: 11,
	}
	tests := []struct {
		name         string
		env          map[string]string
		runtime      ResolveRequest
		wantAK       string
		wantToken    string
		wantRegion   string
		wantEndpoint string
		wantTimeout  int
	}{
		{
			name: "complete environment uses environment-only profile",
			env: map[string]string{
				"VOLCENGINE_ACCESS_KEY_ID":     "env-ak",
				"VOLCENGINE_ACCESS_KEY_SECRET": "env-sk",
				"VOLCENGINE_TOKEN":             "env-token",
				"VOLCENGINE_REGION":            "env-region",
				"VOLCENGINE_ENDPOINT":          "env-endpoint",
			},
			wantAK:       "env-ak",
			wantToken:    "env-token",
			wantRegion:   "env-region",
			wantEndpoint: "env-endpoint",
			wantTimeout:  11,
		},
		{
			name: "complete environment falls back to project not profile",
			env: map[string]string{
				"VOLCENGINE_ACCESS_KEY_ID":     "env-ak",
				"VOLCENGINE_ACCESS_KEY_SECRET": "env-sk",
			},
			wantAK:       "env-ak",
			wantRegion:   "project-region",
			wantEndpoint: "project-endpoint",
			wantTimeout:  11,
		},
		{
			name: "runtime flags override complete environment",
			env: map[string]string{
				"VOLCENGINE_ACCESS_KEY_ID":     "env-ak",
				"VOLCENGINE_ACCESS_KEY_SECRET": "env-sk",
				"VOLCENGINE_REGION":            "env-region",
				"VOLCENGINE_ENDPOINT":          "env-endpoint",
			},
			runtime:      ResolveRequest{RuntimeRegion: "flag-region", RuntimeEndpoint: "flag-endpoint"},
			wantAK:       "env-ak",
			wantRegion:   "flag-region",
			wantEndpoint: "flag-endpoint",
			wantTimeout:  11,
		},
		{
			name: "partial credentials and environment runtime are ignored",
			env: map[string]string{
				"VOLCENGINE_ACCESS_KEY_ID": "partial-ak",
				"VOLCENGINE_TOKEN":         "partial-token",
				"VOLCENGINE_REGION":        "env-region",
				"VOLCENGINE_ENDPOINT":      "env-endpoint",
			},
			wantAK:       "profile-ak",
			wantToken:    "profile-token",
			wantRegion:   "profile-region",
			wantEndpoint: "profile-endpoint",
			wantTimeout:  7,
		},
		{
			name: "environment runtime alone is ignored",
			env: map[string]string{
				"VOLCENGINE_REGION":   "env-region",
				"VOLCENGINE_ENDPOINT": "env-endpoint",
			},
			wantAK:       "profile-ak",
			wantToken:    "profile-token",
			wantRegion:   "profile-region",
			wantEndpoint: "profile-endpoint",
			wantTimeout:  7,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := tc.runtime
			req.Config = cfg
			req.Defaults = defaults
			got, err := (Resolver{LookupEnv: mapEnv(tc.env)}).Resolve(req)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if got.Profile.AccessKeyID != tc.wantAK ||
				got.Profile.SecurityToken != tc.wantToken ||
				got.Profile.Region != tc.wantRegion ||
				got.Profile.Endpoint != tc.wantEndpoint ||
				got.Profile.TimeoutSeconds != tc.wantTimeout {
				t.Fatalf("profile=%+v, want ak=%q token=%q region=%q endpoint=%q timeout=%d",
					got.Profile, tc.wantAK, tc.wantToken, tc.wantRegion, tc.wantEndpoint, tc.wantTimeout)
			}
		})
	}
}

func TestResolverDynamicIgnoresEnvironmentCredentials(t *testing.T) {
	cfg := config.Config{Profiles: map[string]config.Profile{
		"dynamic": {
			Mode:            config.AuthModeConsoleLogin,
			AccessKeyID:     "profile-ak",
			SecretAccessKey: "profile-sk",
			Region:          "profile-region",
			Endpoint:        "profile-endpoint",
		},
	}}
	env := map[string]string{
		"VOLCENGINE_ACCESS_KEY_ID":     "env-ak",
		"VOLCENGINE_ACCESS_KEY_SECRET": "env-sk",
		"VOLCENGINE_TOKEN":             "env-token",
		"VOLCENGINE_REGION":            "env-region",
		"VOLCENGINE_ENDPOINT":          "env-endpoint",
	}
	got, err := (Resolver{LookupEnv: mapEnv(env)}).Resolve(ResolveRequest{
		Config:          cfg,
		ExplicitProfile: "dynamic",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !got.Dynamic || got.Mode != config.AuthModeConsoleLogin {
		t.Fatalf("resolution=%+v, want dynamic console", got)
	}
	if got.Profile.AccessKeyID != "profile-ak" ||
		got.Profile.SecretAccessKey != "profile-sk" ||
		got.Profile.SecurityToken != "" {
		t.Fatalf("dynamic profile credentials changed: %+v", got.Profile)
	}
	if got.Profile.Region != "env-region" || got.Profile.Endpoint != "env-endpoint" {
		t.Fatalf("dynamic runtime settings=%+v", got.Profile)
	}
}

func TestResolverForceStaticUsesEnvironmentCredentials(t *testing.T) {
	cfg := config.Config{Profiles: map[string]config.Profile{
		"dynamic": {
			Mode:         config.AuthModeConsoleLogin,
			Region:       "profile-region",
			Endpoint:     "profile-endpoint",
			LoginSession: "session",
		},
	}}
	env := map[string]string{
		"VOLCENGINE_ACCESS_KEY_ID":     "file-ak",
		"VOLCENGINE_ACCESS_KEY_SECRET": "file-sk",
		"VOLCENGINE_TOKEN":             "file-token",
	}
	got, err := (Resolver{LookupEnv: mapEnv(env)}).Resolve(ResolveRequest{
		Config:          cfg,
		ExplicitProfile: "dynamic",
		ForceStatic:     true,
		Defaults: config.ProfileDefaults{
			Region:   "project-region",
			Endpoint: "project-endpoint",
		},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Dynamic || !got.ForceStatic {
		t.Fatalf("resolution=%+v, want forced static", got)
	}
	if got.Profile.AccessKeyID != "file-ak" ||
		got.Profile.SecretAccessKey != "file-sk" ||
		got.Profile.SecurityToken != "file-token" {
		t.Fatalf("forced static credentials=%+v", got.Profile)
	}
}

func TestLoadSecretsFile(t *testing.T) {
	for _, key := range supportedSecretsEnvKeys {
		t.Setenv(key, "")
	}
	path := filepath.Join(t.TempDir(), "secrets.env")
	if err := os.WriteFile(path, []byte(`
# comment
export VOLCENGINE_ACCESS_KEY_ID="ak"
VOLCENGINE_ACCESS_KEY_SECRET='sk'
VOLCENGINE_TOKEN=token
UNRELATED=ignored
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := LoadSecretsFile(path); err != nil {
		t.Fatalf("LoadSecretsFile: %v", err)
	}
	if os.Getenv("VOLCENGINE_ACCESS_KEY_ID") != "ak" ||
		os.Getenv("VOLCENGINE_ACCESS_KEY_SECRET") != "sk" ||
		os.Getenv("VOLCENGINE_TOKEN") != "token" {
		t.Fatal("supported assignments were not applied")
	}
	if os.Getenv("UNRELATED") != "" {
		t.Fatal("unsupported assignment must not be applied")
	}
}

func TestLoadSecretsFileErrors(t *testing.T) {
	err := LoadSecretsFile("")
	var target *SecretsFileError
	if !errors.As(err, &target) || err.Error() != "empty secrets file" {
		t.Fatalf("empty error=%v", err)
	}

	path := filepath.Join(t.TempDir(), "empty.env")
	if err := os.WriteFile(path, []byte("UNRELATED=value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err = LoadSecretsFile(path)
	if !errors.As(err, &target) ||
		err.Error() != "secrets file does not contain any supported VOLCENGINE_* assignments" {
		t.Fatalf("unsupported error=%v", err)
	}
}

func staticProfile(name string) config.Profile {
	return config.Profile{
		Mode:            config.AuthModeAK,
		AccessKeyID:     name + "-ak",
		SecretAccessKey: name + "-sk",
		Region:          "region",
		Endpoint:        "endpoint",
	}
}

func emptyEnv(string) string { return "" }

func mapEnv(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}
