package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth"
	"github.com/volcengine-tls/ve-tls-cli/internal/config"
	"github.com/volcengine-tls/ve-tls-cli/internal/output"
)

// fakeAuthFactory records calls and returns configurable providers/errors so
// tests can exercise the dynamic routing seam without real SSO/Console code.
type fakeAuthFactory struct {
	ssoProvider     auth.Provider
	ssoErr          error
	ssoStatus       authStatusReader
	consoleProvider auth.Provider
	consoleErr      error
	consoleStatus   authStatusReader
	ramProvider     auth.Provider
	ramErr          error
	oidcProvider    auth.Provider
	oidcErr         error
	ecsProvider     auth.Provider
	ecsErr          error
	ssoCalls        int
	consoleCalls    int
	ramCalls        int
	oidcCalls       int
	ecsCalls        int
	lastConfigPath  string
	lastProfileName string
	lastCfg         config.Config
}

func (f *fakeAuthFactory) SSO(configPath, profileName string, cfg config.Config) (auth.Provider, authStatusReader, error) {
	f.ssoCalls++
	f.lastConfigPath = configPath
	f.lastProfileName = profileName
	f.lastCfg = cfg
	return f.ssoProvider, f.ssoStatus, f.ssoErr
}

func (f *fakeAuthFactory) Console(configPath, profileName string, cfg config.Config) (auth.Provider, authStatusReader, error) {
	f.consoleCalls++
	f.lastConfigPath = configPath
	f.lastProfileName = profileName
	f.lastCfg = cfg
	return f.consoleProvider, f.consoleStatus, f.consoleErr
}

func (f *fakeAuthFactory) RamRoleARN(configPath, profileName string, cfg config.Config) (auth.Provider, error) {
	f.ramCalls++
	f.lastConfigPath = configPath
	f.lastProfileName = profileName
	f.lastCfg = cfg
	return f.ramProvider, f.ramErr
}

func (f *fakeAuthFactory) OIDC(configPath, profileName string, cfg config.Config) (auth.Provider, error) {
	f.oidcCalls++
	f.lastConfigPath = configPath
	f.lastProfileName = profileName
	f.lastCfg = cfg
	return f.oidcProvider, f.oidcErr
}

func (f *fakeAuthFactory) ECSRole(configPath, profileName string, cfg config.Config) (auth.Provider, error) {
	f.ecsCalls++
	f.lastConfigPath = configPath
	f.lastProfileName = profileName
	f.lastCfg = cfg
	return f.ecsProvider, f.ecsErr
}

type fakeProvider struct {
	value      auth.Value
	err        error
	calls      int
	retrieveFn func() (auth.Value, error)
}

func (p *fakeProvider) Retrieve(context.Context) (auth.Value, error) {
	p.calls++
	if p.retrieveFn != nil {
		return p.retrieveFn()
	}
	return p.value, p.err
}

func newTestContext(t *testing.T, cfg config.Config, cfgPath string) *Context {
	t.Helper()
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]config.Profile{}
	}
	ctx := newContext(&bytes.Buffer{}, &bytes.Buffer{}, output.FormatJSON, "", "")
	ctx.cfg = cfg
	ctx.cfgPath = cfgPath
	return ctx
}

func clearAuthTestEnv(t *testing.T) {
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

// TestEmptyAndAKModeUseUnchangedEffectiveProfile proves that mode="" and
// mode="ak" still delegate fully to config.EffectiveProfile, including env
// AK/SK precedence, cred-ref, and project defaults.
func TestEmptyAndAKModeUseUnchangedEffectiveProfile(t *testing.T) {
	clearAuthTestEnv(t)
	cfg := config.Config{
		Version: 1,
		Profiles: map[string]config.Profile{
			"empty": {
				AccessKeyID:     "inline-ak",
				SecretAccessKey: "inline-sk",
				Region:          "cn-beijing",
				Endpoint:        "https://tls-cn-beijing.volces.com",
			},
			"ak": {
				Mode:            config.AuthModeAK,
				AccessKeyID:     "ak-mode-ak",
				SecretAccessKey: "ak-mode-sk",
				Region:          "ap-singapore-1",
				Endpoint:        "https://tls-ap-singapore-1.volces.com",
				TimeoutSeconds:  30,
			},
		},
	}

	for _, name := range []string{"empty", "ak"} {
		t.Run(name, func(t *testing.T) {
			ctx := newTestContext(t, cfg, "/tmp/test-config.json")
			ctx.Profile = name
			if err := ctx.ResolveProfile(); err != nil {
				t.Fatalf("ResolveProfile: %v", err)
			}
			want, err := config.EffectiveProfile(cfg, name, config.ProfileDefaults{})
			if err != nil {
				t.Fatalf("EffectiveProfile: %v", err)
			}
			if ctx.profile != want {
				t.Fatalf("profile mismatch:\n got: %#v\nwant: %#v", ctx.profile, want)
			}
			if !ctx.profileResolved {
				t.Fatalf("profileResolved should be true after ResolveProfile")
			}
			if ctx.profile.Mode != "" && ctx.profile.Mode != config.AuthModeAK {
				t.Fatalf("expected static mode, got %q", ctx.profile.Mode)
			}
		})
	}

	// Environment AK/SK must still override the static profile, exactly as before.
	t.Run("env_override", func(t *testing.T) {
		t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "env-ak")
		t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "env-sk")
		t.Setenv("VOLCENGINE_REGION", "cn-shanghai")
		t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-shanghai.volces.com")
		ctx := newTestContext(t, cfg, "/tmp/test-config.json")
		ctx.Profile = "empty"
		if err := ctx.ResolveProfile(); err != nil {
			t.Fatalf("ResolveProfile: %v", err)
		}
		want, err := config.EffectiveProfile(cfg, "empty", config.ProfileDefaults{})
		if err != nil {
			t.Fatalf("EffectiveProfile: %v", err)
		}
		if ctx.profile != want {
			t.Fatalf("env override profile mismatch:\n got: %#v\nwant: %#v", ctx.profile, want)
		}
		if ctx.profile.AccessKeyID != "env-ak" {
			t.Fatalf("expected env-ak, got %q", ctx.profile.AccessKeyID)
		}
	})
}

// TestSSOModeIgnoresEnvironmentAK proves that mode=sso never picks up
// environment AK/SK, even when both are set.
func TestSSOModeIgnoresEnvironmentAK(t *testing.T) {
	clearAuthTestEnv(t)
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "env-ak-must-be-ignored")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "env-sk-must-be-ignored")
	t.Setenv("VOLCENGINE_TOKEN", "env-token-must-be-ignored")

	cfg := config.Config{
		Version: 1,
		Profiles: map[string]config.Profile{
			"dyn": {
				Mode:     config.AuthModeSSO,
				Region:   "cn-beijing",
				Endpoint: "https://tls-cn-beijing.volces.com",
			},
		},
	}
	ctx := newTestContext(t, cfg, "/tmp/test-config.json")
	ctx.Profile = "dyn"
	if err := ctx.ResolveProfile(); err != nil {
		t.Fatalf("ResolveProfile: %v", err)
	}
	if ctx.profile.AccessKeyID != "" {
		t.Fatalf("dynamic profile must not carry env AK, got %q", ctx.profile.AccessKeyID)
	}
	if ctx.profile.SecretAccessKey != "" {
		t.Fatalf("dynamic profile must not carry env SK, got %q", ctx.profile.SecretAccessKey)
	}
	if ctx.profile.SecurityToken != "" {
		t.Fatalf("dynamic profile must not carry env token, got %q", ctx.profile.SecurityToken)
	}
	if ctx.profile.Region != "cn-beijing" {
		t.Fatalf("expected profile region cn-beijing, got %q", ctx.profile.Region)
	}
}

// TestConsoleModeIgnoresEnvironmentAK proves that mode=console-login never
// picks up environment AK/SK.
func TestConsoleModeIgnoresEnvironmentAK(t *testing.T) {
	clearAuthTestEnv(t)
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "env-ak-must-be-ignored")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "env-sk-must-be-ignored")

	cfg := config.Config{
		Version: 1,
		Profiles: map[string]config.Profile{
			"dyn": {
				Mode:     config.AuthModeConsoleLogin,
				Region:   "cn-beijing",
				Endpoint: "https://tls-cn-beijing.volces.com",
			},
		},
	}
	ctx := newTestContext(t, cfg, "/tmp/test-config.json")
	ctx.Profile = "dyn"
	if err := ctx.ResolveProfile(); err != nil {
		t.Fatalf("ResolveProfile: %v", err)
	}
	if ctx.profile.AccessKeyID != "" {
		t.Fatalf("dynamic profile must not carry env AK, got %q", ctx.profile.AccessKeyID)
	}
	if ctx.profile.SecretAccessKey != "" {
		t.Fatalf("dynamic profile must not carry env SK, got %q", ctx.profile.SecretAccessKey)
	}
}

// TestDynamicProviderFailureNeverFallsBackToEnvironmentAK proves that when the
// dynamic provider factory returns an error, Client() fails closed instead of
// silently using environment AK/SK.
func TestDynamicProviderFailureNeverFallsBackToEnvironmentAK(t *testing.T) {
	clearAuthTestEnv(t)
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "env-ak-fallback")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "env-sk-fallback")

	cfg := config.Config{
		Version: 1,
		Profiles: map[string]config.Profile{
			"dyn": {
				Mode:     config.AuthModeSSO,
				Region:   "cn-beijing",
				Endpoint: "https://tls-cn-beijing.volces.com",
			},
		},
	}
	factory := &fakeAuthFactory{ssoErr: errSentinel("sso cache missing")}
	ctx := newTestContext(t, cfg, "/tmp/test-config.json")
	ctx.Profile = "dyn"
	ctx.authFactory = factory

	_, err := ctx.Client()
	if err == nil {
		t.Fatalf("expected Client() to fail when provider factory errors")
	}
	if !strings.Contains(err.Error(), "sso cache missing") {
		t.Fatalf("expected provider error, got %v", err)
	}
	if factory.ssoCalls != 1 {
		t.Fatalf("expected factory to be called once, got %d", factory.ssoCalls)
	}
}

// TestGlobalSecretsFileForcesStaticMode proves that --secrets-file forces the
// static path even when the selected profile declares a dynamic mode.
func TestGlobalSecretsFileForcesStaticMode(t *testing.T) {
	clearAuthTestEnv(t)
	const (
		accessKey = "secrets-static-ak"
		secretKey = "secrets-static-sk"
	)
	server, signatureResult := newLegacyStaticSignatureServer(accessKey, secretKey)
	defer server.Close()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	t.Setenv("VOLCLOG_CONFIG", configPath)

	cfg := config.Config{
		Version:        1,
		CurrentProfile: "dyn",
		Profiles: map[string]config.Profile{
			"dyn": {
				Mode: config.AuthModeSSO,
			},
		},
	}
	if err := config.Save(cfg, configPath); err != nil {
		t.Fatalf("save config: %v", err)
	}

	secretsPath := filepath.Join(dir, "secrets.env")
	secrets := strings.Join([]string{
		"VOLCENGINE_ACCESS_KEY_ID=" + accessKey,
		"VOLCENGINE_ACCESS_KEY_SECRET=" + secretKey,
		"VOLCENGINE_REGION=cn-beijing",
		"VOLCENGINE_ENDPOINT=" + server.URL,
		"",
	}, "\n")
	if err := os.WriteFile(secretsPath, []byte(secrets), 0o600); err != nil {
		t.Fatalf("write secrets file: %v", err)
	}

	stdout, stderr, code := runLegacyCLI("--secrets-file", secretsPath, "tool", "exec", "account.get")
	if code != 0 {
		t.Fatalf("secrets-file exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if err := <-signatureResult; err != nil {
		t.Fatalf("static signature verification failed: %v", err)
	}
}

// TestContextSecretsFileForcesStaticMode proves that context.secrets_file forces
// the static path even when the selected profile declares a dynamic mode.
func TestContextSecretsFileForcesStaticMode(t *testing.T) {
	clearAuthTestEnv(t)
	const (
		accessKey = "context-secrets-ak"
		secretKey = "context-secrets-sk"
	)
	server, signatureResult := newLegacyStaticSignatureServer(accessKey, secretKey)
	defer server.Close()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	t.Setenv("VOLCLOG_CONFIG", configPath)

	cfg := config.Config{
		Version:        1,
		CurrentProfile: "dyn",
		Profiles: map[string]config.Profile{
			"dyn": {
				Mode: config.AuthModeConsoleLogin,
			},
		},
	}
	if err := config.Save(cfg, configPath); err != nil {
		t.Fatalf("save config: %v", err)
	}

	secretsPath := filepath.Join(dir, "context.env")
	secrets := strings.Join([]string{
		"VOLCENGINE_ACCESS_KEY_ID=" + accessKey,
		"VOLCENGINE_ACCESS_KEY_SECRET=" + secretKey,
		"VOLCENGINE_REGION=cn-beijing",
		"VOLCENGINE_ENDPOINT=" + server.URL,
		"",
	}, "\n")
	if err := os.WriteFile(secretsPath, []byte(secrets), 0o600); err != nil {
		t.Fatalf("write context secrets file: %v", err)
	}
	contextPath := filepath.Join(dir, "context.json")
	contextJSON, err := json.Marshal(map[string]any{"secrets_file": secretsPath})
	if err != nil {
		t.Fatalf("marshal tool context: %v", err)
	}
	if err := os.WriteFile(contextPath, contextJSON, 0o600); err != nil {
		t.Fatalf("write tool context: %v", err)
	}

	stdout, stderr, code := runLegacyCLI("tool", "exec", "account.get", "--context", "file://"+contextPath)
	if code != 0 {
		t.Fatalf("context secrets-file exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if err := <-signatureResult; err != nil {
		t.Fatalf("static signature verification failed: %v", err)
	}
}

// TestDynamicModeUsesExplicitCurrentDefaultProfileOrder proves the profile
// selection precedence for dynamic modes: explicit --profile > current_profile
// > "default".
func TestDynamicModeUsesExplicitCurrentDefaultProfileOrder(t *testing.T) {
	clearAuthTestEnv(t)
	cfg := config.Config{
		Version:        1,
		CurrentProfile: "current",
		Profiles: map[string]config.Profile{
			"default": {
				Mode:     config.AuthModeSSO,
				Region:   "cn-beijing",
				Endpoint: "https://tls-cn-beijing.volces.com",
			},
			"current": {
				Mode:     config.AuthModeSSO,
				Region:   "ap-singapore-1",
				Endpoint: "https://tls-ap-singapore-1.volces.com",
			},
			"explicit": {
				Mode:     config.AuthModeConsoleLogin,
				Region:   "cn-shanghai",
				Endpoint: "https://tls-cn-shanghai.volces.com",
			},
		},
	}

	cases := []struct {
		name        string
		profileFlag string
		wantRegion  string
	}{
		{name: "explicit_over_current", profileFlag: "explicit", wantRegion: "cn-shanghai"},
		{name: "current_over_default", profileFlag: "", wantRegion: "ap-singapore-1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := newTestContext(t, cfg, "/tmp/test-config.json")
			ctx.Profile = tc.profileFlag
			if err := ctx.ResolveProfile(); err != nil {
				t.Fatalf("ResolveProfile: %v", err)
			}
			if ctx.profile.Region != tc.wantRegion {
				t.Fatalf("region=%q, want %q", ctx.profile.Region, tc.wantRegion)
			}
		})
	}

	// When no current_profile is set and no explicit profile is given, "default"
	// must be selected.
	t.Run("default_when_no_current", func(t *testing.T) {
		cfgNoCurrent := config.Config{
			Version: 1,
			Profiles: map[string]config.Profile{
				"default": {
					Mode:     config.AuthModeSSO,
					Region:   "cn-beijing",
					Endpoint: "https://tls-cn-beijing.volces.com",
				},
			},
		}
		ctx := newTestContext(t, cfgNoCurrent, "/tmp/test-config.json")
		if err := ctx.ResolveProfile(); err != nil {
			t.Fatalf("ResolveProfile: %v", err)
		}
		if ctx.profile.Region != "cn-beijing" {
			t.Fatalf("expected default profile region cn-beijing, got %q", ctx.profile.Region)
		}
	})
}

// TestDynamicModeResolvesTLSRuntimeSettings proves the fixed precedence for
// dynamic modes: VOLCENGINE_REGION/ENDPOINT > profile > project defaults >
// timeout default 60s.
func TestDynamicModeResolvesTLSRuntimeSettings(t *testing.T) {
	clearAuthTestEnv(t)

	t.Run("env_overrides_profile", func(t *testing.T) {
		t.Setenv("VOLCENGINE_REGION", "env-region")
		t.Setenv("VOLCENGINE_ENDPOINT", "https://env-endpoint.example.com")
		cfg := config.Config{
			Version: 1,
			Profiles: map[string]config.Profile{
				"dyn": {
					Mode:     config.AuthModeSSO,
					Region:   "profile-region",
					Endpoint: "https://profile-endpoint.example.com",
				},
			},
		}
		ctx := newTestContext(t, cfg, "/tmp/test-config.json")
		ctx.Profile = "dyn"
		if err := ctx.ResolveProfile(); err != nil {
			t.Fatalf("ResolveProfile: %v", err)
		}
		if ctx.profile.Region != "env-region" {
			t.Fatalf("region=%q, want env-region", ctx.profile.Region)
		}
		if ctx.profile.Endpoint != "https://env-endpoint.example.com" {
			t.Fatalf("endpoint=%q, want env endpoint", ctx.profile.Endpoint)
		}
	})

	t.Run("profile_over_defaults", func(t *testing.T) {
		cfg := config.Config{
			Version: 1,
			Profiles: map[string]config.Profile{
				"dyn": {
					Mode:           config.AuthModeSSO,
					Region:         "profile-region",
					Endpoint:       "https://profile-endpoint.example.com",
					TimeoutSeconds: 45,
				},
			},
		}
		ctx := newTestContext(t, cfg, "/tmp/test-config.json")
		ctx.Profile = "dyn"
		ctx.SetProfileDefaults(config.ProfileDefaults{
			Region:         "default-region",
			Endpoint:       "https://default-endpoint.example.com",
			TimeoutSeconds: 90,
		})
		if err := ctx.ResolveProfile(); err != nil {
			t.Fatalf("ResolveProfile: %v", err)
		}
		if ctx.profile.Region != "profile-region" {
			t.Fatalf("region=%q, want profile-region", ctx.profile.Region)
		}
		if ctx.profile.Endpoint != "https://profile-endpoint.example.com" {
			t.Fatalf("endpoint=%q, want profile endpoint", ctx.profile.Endpoint)
		}
		if ctx.profile.TimeoutSeconds != 45 {
			t.Fatalf("timeout=%d, want 45", ctx.profile.TimeoutSeconds)
		}
	})

	t.Run("defaults_fill_missing", func(t *testing.T) {
		cfg := config.Config{
			Version: 1,
			Profiles: map[string]config.Profile{
				"dyn": {
					Mode: config.AuthModeSSO,
				},
			},
		}
		ctx := newTestContext(t, cfg, "/tmp/test-config.json")
		ctx.Profile = "dyn"
		ctx.SetProfileDefaults(config.ProfileDefaults{
			Region:         "default-region",
			Endpoint:       "https://default-endpoint.example.com",
			TimeoutSeconds: 120,
		})
		if err := ctx.ResolveProfile(); err != nil {
			t.Fatalf("ResolveProfile: %v", err)
		}
		if ctx.profile.Region != "default-region" {
			t.Fatalf("region=%q, want default-region", ctx.profile.Region)
		}
		if ctx.profile.Endpoint != "https://default-endpoint.example.com" {
			t.Fatalf("endpoint=%q, want default endpoint", ctx.profile.Endpoint)
		}
		if ctx.profile.TimeoutSeconds != 120 {
			t.Fatalf("timeout=%d, want 120", ctx.profile.TimeoutSeconds)
		}
	})

	t.Run("timeout_defaults_to_60s", func(t *testing.T) {
		cfg := config.Config{
			Version: 1,
			Profiles: map[string]config.Profile{
				"dyn": {
					Mode:     config.AuthModeSSO,
					Region:   "cn-beijing",
					Endpoint: "https://tls-cn-beijing.volces.com",
				},
			},
		}
		ctx := newTestContext(t, cfg, "/tmp/test-config.json")
		ctx.Profile = "dyn"
		if err := ctx.ResolveProfile(); err != nil {
			t.Fatalf("ResolveProfile: %v", err)
		}
		if ctx.profile.TimeoutSeconds != 60 {
			t.Fatalf("timeout=%d, want 60", ctx.profile.TimeoutSeconds)
		}
	})
}

func TestGlobalRuntimeFlagsOverrideDynamicProfileAndReachRamFactory(t *testing.T) {
	clearAuthTestEnv(t)
	cfg := config.Config{Version: 1, Profiles: map[string]config.Profile{
		"ram": {
			Mode: config.AuthModeRamRoleARN, RoleName: "r", AccountID: "1",
			AccessKeyID: "src-ak", SecretAccessKey: "src-sk",
			Region: "profile-region", Endpoint: "https://profile.example.com",
		},
	}}
	provider := &fakeProvider{value: auth.Value{AccessKeyID: "AK", SecretAccessKey: "SK", SessionToken: "ST"}}
	factory := &fakeAuthFactory{ramProvider: provider}
	ctx := newTestContext(t, cfg, "/tmp/config.json")
	ctx.Profile = "ram"
	ctx.RuntimeRegion = "global-region"
	ctx.RuntimeEndpoint = "https://global.example.com"
	ctx.authFactory = factory

	if _, err := ctx.Client(); err != nil {
		t.Fatalf("Client: %v", err)
	}
	if ctx.profile.Region != "global-region" || ctx.profile.Endpoint != "https://global.example.com" {
		t.Fatalf("resolved runtime=(%q,%q), want global values", ctx.profile.Region, ctx.profile.Endpoint)
	}
	factoryProfile := factory.lastCfg.Profiles["ram"]
	if factoryProfile.Region != "global-region" || factoryProfile.Endpoint != "https://global.example.com" {
		t.Fatalf("factory runtime=(%q,%q), want resolved global values", factoryProfile.Region, factoryProfile.Endpoint)
	}
	original := ctx.cfg.Profiles["ram"]
	if original.Region != "profile-region" || original.Endpoint != "https://profile.example.com" {
		t.Fatalf("runtime override mutated config: %+v", original)
	}
}

func TestGlobalRuntimeFlagsPreserveStaticAKIdentity(t *testing.T) {
	clearAuthTestEnv(t)
	cfg := config.Config{Version: 1, Profiles: map[string]config.Profile{
		"static": {
			Mode: config.AuthModeAK, AccessKeyID: "profile-ak", SecretAccessKey: "profile-sk",
			Region: "profile-region", Endpoint: "https://profile.example.com",
		},
	}}
	ctx := newTestContext(t, cfg, "/tmp/config.json")
	ctx.Profile = "static"
	ctx.RuntimeRegion = "global-region"
	ctx.RuntimeEndpoint = "https://global.example.com"

	if err := ctx.ResolveProfile(); err != nil {
		t.Fatalf("ResolveProfile: %v", err)
	}
	if ctx.profile.AccessKeyID != "profile-ak" || ctx.profile.SecretAccessKey != "profile-sk" {
		t.Fatalf("static identity changed: %+v", ctx.profile)
	}
	if ctx.profile.Region != "global-region" || ctx.profile.Endpoint != "https://global.example.com" {
		t.Fatalf("static runtime=(%q,%q), want global values", ctx.profile.Region, ctx.profile.Endpoint)
	}
}

func TestParseGlobalRuntimeFlags(t *testing.T) {
	group, rest, flags, ok := parseGlobal([]string{
		"--region", "cn-shanghai",
		"--endpoint", "https://tls-cn-shanghai.volces.com",
		"tool", "list",
	})
	if !ok || group != "tool" || len(rest) != 1 || rest[0] != "list" {
		t.Fatalf("parse result group=%q rest=%v ok=%v", group, rest, ok)
	}
	if flags.Region != "cn-shanghai" || flags.Endpoint != "https://tls-cn-shanghai.volces.com" {
		t.Fatalf("runtime flags=(%q,%q)", flags.Region, flags.Endpoint)
	}
}

// TestUnknownModeFailsBeforeHTTPRequest proves that an unrecognized mode
// produces an error before any HTTP request is attempted.
func TestUnknownModeFailsBeforeHTTPRequest(t *testing.T) {
	clearAuthTestEnv(t)
	cfg := config.Config{
		Version: 1,
		Profiles: map[string]config.Profile{
			"bad": {
				Mode:     "totally-unknown-mode",
				Region:   "cn-beijing",
				Endpoint: "https://tls-cn-beijing.volces.com",
			},
		},
	}

	// ResolveProfile must reject the unknown mode before Client() is ever called.
	ctx := newTestContext(t, cfg, "/tmp/test-config.json")
	ctx.Profile = "bad"
	err := ctx.ResolveProfile()
	if err == nil {
		t.Fatalf("expected ResolveProfile to reject unknown mode")
	}
	if !strings.Contains(err.Error(), "unknown auth mode") {
		t.Fatalf("expected unknown auth mode error, got %v", err)
	}

	// Even if a profile somehow carries an unknown mode into Client(), the build
	// path must fail closed rather than issuing a request.
	ctx2 := newTestContext(t, cfg, "/tmp/test-config.json")
	ctx2.Profile = "bad"
	ctx2.profile = config.Profile{
		Mode:     "totally-unknown-mode",
		Region:   "cn-beijing",
		Endpoint: "https://tls-cn-beijing.volces.com",
	}
	ctx2.profileResolved = true
	_, err = ctx2.Client()
	if err == nil {
		t.Fatalf("expected Client() to fail on unknown mode")
	}
}

// errSentinel is a simple sentinel error for tests.
type errSentinel string

func (e errSentinel) Error() string { return string(e) }

// TestRejectsSecretsFileForGroupMatrix locks the set of groups that must never
// accept --secrets-file. login/logout/sso manage their own dynamic identity and
// must not be handed long-lived static credentials; all other groups pass.
func TestRejectsSecretsFileForGroupMatrix(t *testing.T) {
	cases := []struct {
		group string
		want  bool
	}{
		{"login", true},
		{"logout", true},
		{"sso", true},
		{"tool", false},
		{"workflow", false},
		{"raw", false},
		{"configure", false},
		{"doctor", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := rejectsSecretsFileForGroup(tc.group); got != tc.want {
			t.Errorf("rejectsSecretsFileForGroup(%q) = %v, want %v", tc.group, got, tc.want)
		}
	}
}

// TestPreflightGlobalSecretsFileOrder locks the validation order required by
// Task 5: runtime selector conflicts (--profile + --secrets-file) must fail
// before the login/logout/sso group rejection. When both selectors are passed
// to a login command the user must see "conflicting runtime selectors", not
// "--secrets-file is not supported".
func TestPreflightGlobalSecretsFileOrder(t *testing.T) {
	// Both selectors set: conflict error wins, even for login/logout/sso.
	for _, group := range []string{"login", "logout", "sso"} {
		err := preflightGlobalSecretsFile(group, "my-profile", "/path/to/secrets.env")
		if err == nil {
			t.Fatalf("group %q: expected error with both selectors", group)
		}
		if !strings.Contains(err.Error(), "conflicting runtime selectors") {
			t.Fatalf("group %q: expected conflicting runtime selectors error, got: %v", group, err)
		}
	}

	// Only --secrets-file set (no conflict): login/logout/sso are rejected with
	// the group-specific message.
	for _, group := range []string{"login", "logout", "sso"} {
		err := preflightGlobalSecretsFile(group, "", "/path/to/secrets.env")
		if err == nil {
			t.Fatalf("group %q: expected rejection error with secrets-file only", group)
		}
		if !strings.Contains(err.Error(), "is not supported for") {
			t.Fatalf("group %q: expected not-supported error, got: %v", group, err)
		}
	}

	// Non-login groups with only --secrets-file pass preflight.
	for _, group := range []string{"tool", "workflow", "raw", "configure"} {
		if err := preflightGlobalSecretsFile(group, "", "/path/to/secrets.env"); err != nil {
			t.Fatalf("group %q: expected no error with secrets-file only, got: %v", group, err)
		}
	}
}

// TestDynamicProviderRetrieveFailureNeverFallsBackToEnvironmentAK proves that
// when the dynamic provider's Retrieve fails during the real Do/Sign path, the
// error is returned to the caller and no HTTP request is made — even though
// environment AK/SK are present and would otherwise be usable for the static
// path.
func TestDynamicProviderRetrieveFailureNeverFallsBackToEnvironmentAK(t *testing.T) {
	clearAuthTestEnv(t)
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "env-ak-fallback")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "env-sk-fallback")

	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	cfg := config.Config{
		Version: 1,
		Profiles: map[string]config.Profile{
			"dyn": {
				Mode:     config.AuthModeSSO,
				Region:   "cn-beijing",
				Endpoint: server.URL,
			},
		},
	}
	retrieveErr := errSentinel("sso token expired")
	factory := &fakeAuthFactory{ssoProvider: &fakeProvider{err: retrieveErr}}
	ctx := newTestContext(t, cfg, "/tmp/test-config.json")
	ctx.Profile = "dyn"
	ctx.authFactory = factory

	_, err := ctx.DoRaw("GET", "/", nil, nil, nil)
	if err == nil {
		t.Fatalf("expected DoRaw to fail when provider Retrieve errors")
	}
	if !strings.Contains(err.Error(), "sso token expired") {
		t.Fatalf("expected Retrieve error, got %v", err)
	}
	if requestCount != 0 {
		t.Fatalf("expected no HTTP requests after Retrieve failure, got %d", requestCount)
	}
	if factory.ssoCalls != 1 {
		t.Fatalf("expected factory to be called once, got %d", factory.ssoCalls)
	}
}

// TestTypedNilFactoryFailsClosed proves that a typed-nil authProviderFactory
// (a nil pointer wrapped in the interface) is rejected with a clear error for
// both SSO and Console modes instead of being dispatched to, which would panic
// on the nil receiver. It must not fall back to the default factory or to
// environment AK/SK.
func TestTypedNilFactoryFailsClosed(t *testing.T) {
	clearAuthTestEnv(t)
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "env-ak-must-not-be-used")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "env-sk-must-not-be-used")

	cfg := config.Config{
		Version: 1,
		Profiles: map[string]config.Profile{
			"sso": {
				Mode:     config.AuthModeSSO,
				Region:   "cn-beijing",
				Endpoint: "https://tls-cn-beijing.volces.com",
			},
			"console": {
				Mode:     config.AuthModeConsoleLogin,
				Region:   "cn-beijing",
				Endpoint: "https://tls-cn-beijing.volces.com",
			},
		},
	}

	// A nil *fakeAuthFactory wrapped in the authProviderFactory interface is a
	// typed-nil: factory == nil is false, but calling any method would panic.
	var typedNil *fakeAuthFactory
	for _, name := range []string{"sso", "console"} {
		t.Run(name, func(t *testing.T) {
			ctx := newTestContext(t, cfg, "/tmp/test-config.json")
			ctx.Profile = name
			ctx.authFactory = typedNil
			_, err := ctx.Client()
			if err == nil {
				t.Fatalf("expected Client() to fail on typed-nil factory")
			}
			if !strings.Contains(err.Error(), "nil auth provider factory") {
				t.Fatalf("expected 'nil auth provider factory' error, got %v", err)
			}
			if ctx.client != nil {
				t.Fatalf("expected no cached client after typed-nil factory failure")
			}
		})
	}
}

// TestDefaultFactoryFailsClosedForSSOAndConsole proves that the production
// defaultAuthProviderFactory never falls back to environment AK/SK. For SSO
// without a configured session the factory fails closed at construction time;
// for Console without a login session the provider is built but Retrieve fails
// closed with a reauth error before any request is sent.
func TestDefaultFactoryFailsClosedForSSOAndConsole(t *testing.T) {
	clearAuthTestEnv(t)
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "env-ak-must-not-be-used")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "env-sk-must-not-be-used")
	cfg := config.Config{
		Version: 1,
		Profiles: map[string]config.Profile{
			"sso": {
				Mode:     config.AuthModeSSO,
				Region:   "cn-beijing",
				Endpoint: "https://tls-cn-beijing.volces.com",
			},
			"console": {
				Mode:     config.AuthModeConsoleLogin,
				Region:   "cn-beijing",
				Endpoint: "https://tls-cn-beijing.volces.com",
			},
		},
	}

	t.Run("sso", func(t *testing.T) {
		ctx := newTestContext(t, cfg, "/tmp/test-config.json")
		ctx.Profile = "sso"
		// authFactory left nil → buildDynamicClient uses defaultAuthProviderFactory
		_, err := ctx.Client()
		if err == nil {
			t.Fatalf("expected Client() to fail when SSO session is not configured")
		}
		if !strings.Contains(err.Error(), "sso session not found") {
			t.Fatalf("expected 'sso session not found' error, got %v", err)
		}
		if ctx.client != nil {
			t.Fatalf("expected no cached client after default factory failure")
		}
	})

	t.Run("console", func(t *testing.T) {
		requestCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCount++
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		cfgConsole := config.Config{
			Version: 1,
			Profiles: map[string]config.Profile{
				"console": {
					Mode:     config.AuthModeConsoleLogin,
					Region:   "cn-beijing",
					Endpoint: server.URL,
				},
			},
		}
		ctx := newTestContext(t, cfgConsole, "/tmp/test-config.json")
		ctx.Profile = "console"
		// authFactory left nil → buildDynamicClient uses defaultAuthProviderFactory.
		// The provider is constructed (no login session), but Retrieve must fail
		// closed before any HTTP request is sent.
		_, err := ctx.DoRaw("GET", "/", nil, nil, nil)
		if err == nil {
			t.Fatalf("expected DoRaw to fail when console login cache is missing")
		}
		if requestCount != 0 {
			t.Fatalf("expected no HTTP requests after Retrieve failure, got %d", requestCount)
		}
	})
}

// TestLoadConfigInvalidatesProfileCache proves that after LoadConfig replaces
// cfg/cfgPath, a subsequent ResolveProfile re-reads the new config instead of
// returning the previously cached profile.
func TestLoadConfigInvalidatesProfileCache(t *testing.T) {
	clearAuthTestEnv(t)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	t.Setenv("VOLCLOG_CONFIG", configPath)

	writeCfg := func(region string) {
		cfg := config.Config{
			Version: 1,
			Profiles: map[string]config.Profile{
				"p1": {
					AccessKeyID:     "ak",
					SecretAccessKey: "sk",
					Region:          region,
					Endpoint:        "https://tls-cn-beijing.volces.com",
				},
			},
		}
		if err := config.Save(cfg, configPath); err != nil {
			t.Fatalf("save config: %v", err)
		}
	}

	writeCfg("cn-beijing")
	ctx := newContext(&bytes.Buffer{}, &bytes.Buffer{}, output.FormatJSON, "", "")
	if err := ctx.LoadConfig(); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	ctx.Profile = "p1"
	if err := ctx.ResolveProfile(); err != nil {
		t.Fatalf("ResolveProfile: %v", err)
	}
	if ctx.profile.Region != "cn-beijing" {
		t.Fatalf("initial region=%q, want cn-beijing", ctx.profile.Region)
	}

	// Mutate the on-disk config and reload; the cached profile must be discarded
	// so the next ResolveProfile sees the new value.
	writeCfg("ap-singapore-1")
	if err := ctx.LoadConfig(); err != nil {
		t.Fatalf("LoadConfig after mutation: %v", err)
	}
	if ctx.profileResolved {
		t.Fatalf("profileResolved should be false after LoadConfig")
	}
	if err := ctx.ResolveProfile(); err != nil {
		t.Fatalf("ResolveProfile after reload: %v", err)
	}
	if ctx.profile.Region != "ap-singapore-1" {
		t.Fatalf("reloaded region=%q, want ap-singapore-1", ctx.profile.Region)
	}
}

// TestUpdateConfigInvalidatesProfileCache proves that after UpdateConfig
// replaces cfg, a subsequent ResolveProfile re-reads the updated config instead
// of returning the previously cached profile.
func TestUpdateConfigInvalidatesProfileCache(t *testing.T) {
	clearAuthTestEnv(t)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	t.Setenv("VOLCLOG_CONFIG", configPath)

	cfg := config.Config{
		Version: 1,
		Profiles: map[string]config.Profile{
			"p1": {
				AccessKeyID:     "ak",
				SecretAccessKey: "sk",
				Region:          "cn-beijing",
				Endpoint:        "https://tls-cn-beijing.volces.com",
			},
		},
	}
	if err := config.Save(cfg, configPath); err != nil {
		t.Fatalf("save config: %v", err)
	}

	ctx := newContext(&bytes.Buffer{}, &bytes.Buffer{}, output.FormatJSON, "", "")
	if err := ctx.LoadConfig(); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	ctx.Profile = "p1"
	if err := ctx.ResolveProfile(); err != nil {
		t.Fatalf("ResolveProfile: %v", err)
	}
	if ctx.profile.Region != "cn-beijing" {
		t.Fatalf("initial region=%q, want cn-beijing", ctx.profile.Region)
	}

	// Update the region on disk via UpdateConfig; the cached profile must be
	// discarded so the next ResolveProfile sees the new value.
	if err := ctx.UpdateConfig(func(c *config.Config) error {
		if p, ok := c.Profiles["p1"]; ok {
			p.Region = "ap-singapore-1"
			c.Profiles["p1"] = p
		}
		return nil
	}); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	if ctx.profileResolved {
		t.Fatalf("profileResolved should be false after UpdateConfig")
	}
	if err := ctx.ResolveProfile(); err != nil {
		t.Fatalf("ResolveProfile after update: %v", err)
	}
	if ctx.profile.Region != "ap-singapore-1" {
		t.Fatalf("updated region=%q, want ap-singapore-1", ctx.profile.Region)
	}
}

// TestSetProfileDefaultsInvalidatesProfileCache proves that after
// SetProfileDefaults replaces defaults, a subsequent ResolveProfile re-applies
// the new defaults instead of returning the previously cached profile.
func TestSetProfileDefaultsInvalidatesProfileCache(t *testing.T) {
	clearAuthTestEnv(t)
	cfg := config.Config{
		Version: 1,
		Profiles: map[string]config.Profile{
			"dyn": {
				Mode: config.AuthModeSSO,
			},
		},
	}
	ctx := newTestContext(t, cfg, "/tmp/test-config.json")
	ctx.Profile = "dyn"

	ctx.SetProfileDefaults(config.ProfileDefaults{
		Region:   "cn-beijing",
		Endpoint: "https://tls-cn-beijing.volces.com",
	})
	if err := ctx.ResolveProfile(); err != nil {
		t.Fatalf("ResolveProfile: %v", err)
	}
	if ctx.profile.Region != "cn-beijing" {
		t.Fatalf("initial region=%q, want cn-beijing", ctx.profile.Region)
	}

	// Replace defaults; the cached profile must be discarded so the new defaults
	// are applied on the next ResolveProfile.
	ctx.SetProfileDefaults(config.ProfileDefaults{
		Region:   "ap-singapore-1",
		Endpoint: "https://tls-ap-singapore-1.volces.com",
	})
	if ctx.profileResolved {
		t.Fatalf("profileResolved should be false after SetProfileDefaults")
	}
	if err := ctx.ResolveProfile(); err != nil {
		t.Fatalf("ResolveProfile after defaults change: %v", err)
	}
	if ctx.profile.Region != "ap-singapore-1" {
		t.Fatalf("updated region=%q, want ap-singapore-1", ctx.profile.Region)
	}
}

// TestTypedNilProviderFailsClosedForSSOAndConsole proves that when the factory
// returns a typed-nil auth.Provider (a nil pointer wrapped in the interface),
// buildDynamicClient rejects it with a clear error instead of wrapping it in
// modeAwareProvider (which would bypass tlsapi's nil guard and panic in Sign).
func TestTypedNilProviderFailsClosedForSSOAndConsole(t *testing.T) {
	clearAuthTestEnv(t)
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "env-ak-must-not-be-used")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "env-sk-must-not-be-used")

	cases := []struct {
		name    string
		mode    string
		factory *fakeAuthFactory
	}{
		{
			name: "sso",
			mode: config.AuthModeSSO,
			factory: &fakeAuthFactory{
				// typed-nil *fakeProvider wrapped in the auth.Provider interface
				ssoProvider: (*fakeProvider)(nil),
			},
		},
		{
			name: "console",
			mode: config.AuthModeConsoleLogin,
			factory: &fakeAuthFactory{
				consoleProvider: (*fakeProvider)(nil),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requestCount := int32(0)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&requestCount, 1)
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			cfg := config.Config{
				Version: 1,
				Profiles: map[string]config.Profile{
					tc.name: {
						Mode:     tc.mode,
						Region:   "cn-beijing",
						Endpoint: server.URL,
					},
				},
			}
			ctx := newTestContext(t, cfg, "/tmp/test-config.json")
			ctx.Profile = tc.name
			ctx.authFactory = tc.factory

			// Client() must return a clear error, not panic.
			_, err := ctx.Client()
			if err == nil {
				t.Fatalf("expected Client() to fail on typed-nil provider")
			}
			if !strings.Contains(err.Error(), "nil auth provider") {
				t.Fatalf("expected 'nil auth provider' error, got %v", err)
			}
			if ctx.client != nil {
				t.Fatalf("expected no cached client after typed-nil provider failure")
			}

			// DoRaw must also fail cleanly without sending a request or falling
			// back to environment AK/SK.
			_, err = ctx.DoRaw("GET", "/DescribeProjects", nil, nil, nil)
			if err == nil {
				t.Fatalf("expected DoRaw to fail on typed-nil provider")
			}
			if atomic.LoadInt32(&requestCount) != 0 {
				t.Fatalf("expected no HTTP requests after typed-nil provider failure, got %d", requestCount)
			}
		})
	}
}

// TestDynamicReauthErrorMappingThroughDoRaw proves that a real ReauthRequired
// error from a dynamic provider (flowing through ctx.DoRaw -> tlsapi -> Sign)
// is classified by classifyError as kind=auth with the exact mode-aware hint.
func TestDynamicReauthErrorMappingThroughDoRaw(t *testing.T) {
	clearAuthTestEnv(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cases := []struct {
		name     string
		mode     string
		provider auth.Provider
		wantHint string
	}{
		{
			name: "sso",
			mode: config.AuthModeSSO,
			provider: &fakeProvider{err: &auth.Error{
				Kind:        auth.ReauthRequired,
				Description: "sso token cache missing; run: volclog sso login",
			}},
			wantHint: "volclog sso login --profile <name>",
		},
		{
			name: "console",
			mode: config.AuthModeConsoleLogin,
			provider: &fakeProvider{err: &auth.Error{
				Kind:        auth.ReauthRequired,
				Description: "console login cache missing; run: volclog login",
			}},
			wantHint: "volclog login --profile <name>",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Config{
				Version: 1,
				Profiles: map[string]config.Profile{
					tc.name: {
						Mode:     tc.mode,
						Region:   "cn-beijing",
						Endpoint: server.URL,
					},
				},
			}
			factory := &fakeAuthFactory{}
			if tc.mode == config.AuthModeSSO {
				factory.ssoProvider = tc.provider
			} else {
				factory.consoleProvider = tc.provider
			}
			ctx := newTestContext(t, cfg, "/tmp/test-config.json")
			ctx.Profile = tc.name
			ctx.authFactory = factory

			_, err := ctx.DoRaw("GET", "/DescribeProjects", nil, nil, nil)
			if err == nil {
				t.Fatalf("expected DoRaw to fail with reauth error")
			}

			p, code := classifyError(err, "", 0, "tool")
			if code != 2 {
				t.Fatalf("expected code 2, got %d", code)
			}
			if p.Kind != "auth" {
				t.Fatalf("expected kind=auth, got %q", p.Kind)
			}
			if p.Hint != tc.wantHint {
				t.Fatalf("expected hint %q, got %q", tc.wantHint, p.Hint)
			}
		})
	}
}

// --- Task 7: Workload provider routing tests ---

func TestWorkloadModesBuildMatchingProvider(t *testing.T) {
	cases := []struct {
		mode      string
		wantCalls func(f *fakeAuthFactory) int
	}{
		{config.AuthModeRamRoleARN, func(f *fakeAuthFactory) int { return f.ramCalls }},
		{config.AuthModeOIDC, func(f *fakeAuthFactory) int { return f.oidcCalls }},
		{config.AuthModeECSRole, func(f *fakeAuthFactory) int { return f.ecsCalls }},
	}
	for _, tc := range cases {
		t.Run(tc.mode, func(t *testing.T) {
			fp := &fakeProvider{value: auth.Value{AccessKeyID: "AK", SecretAccessKey: "SK", SessionToken: "ST"}}
			f := &fakeAuthFactory{}
			switch tc.mode {
			case config.AuthModeRamRoleARN:
				f.ramProvider = fp
			case config.AuthModeOIDC:
				f.oidcProvider = fp
			case config.AuthModeECSRole:
				f.ecsProvider = fp
			}
			cfg := config.Config{Version: 1, Profiles: map[string]config.Profile{
				"default": {Mode: tc.mode, RoleName: "r", Region: "cn-beijing", Endpoint: "http://example.com"},
			}}
			ctx := newTestContext(t, cfg, "/tmp/cfg.json")
			ctx.authFactory = f
			if _, err := ctx.Client(); err != nil {
				t.Fatalf("Client: %v", err)
			}
			if got := tc.wantCalls(f); got != 1 {
				t.Fatalf("target factory calls=%d, want 1", got)
			}
			// All four other factory counters must be exactly 0.
			if f.ssoCalls != 0 {
				t.Fatalf("ssoCalls=%d, want 0", f.ssoCalls)
			}
			if f.consoleCalls != 0 {
				t.Fatalf("consoleCalls=%d, want 0", f.consoleCalls)
			}
			switch tc.mode {
			case config.AuthModeRamRoleARN:
				if f.oidcCalls != 0 || f.ecsCalls != 0 {
					t.Fatalf("oidc=%d ecs=%d, want both 0", f.oidcCalls, f.ecsCalls)
				}
			case config.AuthModeOIDC:
				if f.ramCalls != 0 || f.ecsCalls != 0 {
					t.Fatalf("ram=%d ecs=%d, want both 0", f.ramCalls, f.ecsCalls)
				}
			case config.AuthModeECSRole:
				if f.ramCalls != 0 || f.oidcCalls != 0 {
					t.Fatalf("ram=%d oidc=%d, want both 0", f.ramCalls, f.oidcCalls)
				}
			}
		})
	}
}

func TestWorkloadModesResolveRuntimeWithoutEnvironmentCredentials(t *testing.T) {
	clearAuthTestEnv(t)
	// Poison env identity and runtime overrides.
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ENV-AK")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "ENV-SK")
	t.Setenv("VOLCENGINE_TOKEN", "ENV-TOKEN")
	t.Setenv("VOLCENGINE_REGION", "env-region")
	t.Setenv("VOLCENGINE_ENDPOINT", "http://env-endpoint")

	for _, mode := range []string{config.AuthModeRamRoleARN, config.AuthModeOIDC, config.AuthModeECSRole} {
		t.Run(mode, func(t *testing.T) {
			fp := &fakeProvider{value: auth.Value{AccessKeyID: "AK", SecretAccessKey: "SK", SessionToken: "ST"}}
			f := &fakeAuthFactory{}
			switch mode {
			case config.AuthModeRamRoleARN:
				f.ramProvider = fp
			case config.AuthModeOIDC:
				f.oidcProvider = fp
			case config.AuthModeECSRole:
				f.ecsProvider = fp
			}
			cfg := config.Config{Version: 1, Profiles: map[string]config.Profile{
				"default": {Mode: mode, RoleName: "r", Endpoint: "http://example.com", Region: "cn-beijing"},
			}}
			ctx := newTestContext(t, cfg, "/tmp/cfg.json")
			ctx.authFactory = f
			if err := ctx.ResolveProfile(); err != nil {
				t.Fatalf("ResolveProfile: %v", err)
			}
			// Profile must not absorb env identity.
			if ctx.profile.AccessKeyID != "" {
				t.Fatalf("profile AK=%q, want empty (env must not leak)", ctx.profile.AccessKeyID)
			}
			if ctx.profile.SecretAccessKey != "" {
				t.Fatalf("profile SK=%q, want empty (env must not leak)", ctx.profile.SecretAccessKey)
			}
			if ctx.profile.SecurityToken != "" {
				t.Fatalf("profile token=%q, want empty (env must not leak)", ctx.profile.SecurityToken)
			}
			// Region/endpoint runtime override behavior is preserved for TLS;
			// env VOLCENGINE_REGION may override the profile region. We only
			// assert that identity fields are not absorbed from env.
			// Client must still route to the matching workload factory.
			if _, err := ctx.Client(); err != nil {
				t.Fatalf("Client: %v", err)
			}
			switch mode {
			case config.AuthModeRamRoleARN:
				if f.ramCalls != 1 || f.oidcCalls != 0 || f.ecsCalls != 0 {
					t.Fatalf("ram=%d oidc=%d ecs=%d, want ram=1", f.ramCalls, f.oidcCalls, f.ecsCalls)
				}
			case config.AuthModeOIDC:
				if f.oidcCalls != 1 || f.ramCalls != 0 || f.ecsCalls != 0 {
					t.Fatalf("ram=%d oidc=%d ecs=%d, want oidc=1", f.ramCalls, f.oidcCalls, f.ecsCalls)
				}
			case config.AuthModeECSRole:
				if f.ecsCalls != 1 || f.ramCalls != 0 || f.oidcCalls != 0 {
					t.Fatalf("ram=%d oidc=%d ecs=%d, want ecs=1", f.ramCalls, f.oidcCalls, f.ecsCalls)
				}
			}
		})
	}
}

func TestRamRoleARNResolvesOnlyInlineOrCredRefSource(t *testing.T) {
	clearAuthTestEnv(t)
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ENV-AK")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "ENV-SK")
	// Poison HOME so any ~/.volcengine read would be visible.
	t.Setenv("HOME", "/nonexistent-home-dir-poison")

	factory := defaultAuthProviderFactory{}

	t.Run("inline_credentials", func(t *testing.T) {
		cfg := config.Config{Version: 1, Profiles: map[string]config.Profile{
			"default": {Mode: config.AuthModeRamRoleARN, RoleName: "r", AccountID: "1", AccessKeyID: "inline-AK", SecretAccessKey: "inline-SK", Endpoint: "http://example.com", Region: "cn-beijing"},
		}}
		p, err := factory.RamRoleARN("/tmp/cfg.json", "default", cfg)
		if err != nil {
			t.Fatalf("RamRoleARN: %v", err)
		}
		if p == nil {
			t.Fatal("expected non-nil provider")
		}
	})

	t.Run("explicit_cred_ref", func(t *testing.T) {
		cfg := config.Config{
			Version: 1,
			Profiles: map[string]config.Profile{
				"default": {Mode: config.AuthModeRamRoleARN, RoleName: "r", AccountID: "1", CredRef: "myref", Endpoint: "http://example.com", Region: "cn-beijing"},
			},
			Creds: map[string]config.Credential{
				"myref": {AccessKeyID: "ref-AK", SecretAccessKey: "ref-SK"},
			},
		}
		p, err := factory.RamRoleARN("/tmp/cfg.json", "default", cfg)
		if err != nil {
			t.Fatalf("RamRoleARN: %v", err)
		}
		if p == nil {
			t.Fatal("expected non-nil provider")
		}
	})

	t.Run("partial_inline_plus_cred_ref_merge", func(t *testing.T) {
		// Inline AK present, SK from cred-ref (existing partial-merge semantics).
		cfg := config.Config{
			Version: 1,
			Profiles: map[string]config.Profile{
				"default": {Mode: config.AuthModeRamRoleARN, RoleName: "r", AccountID: "1", AccessKeyID: "inline-AK", CredRef: "myref", Endpoint: "http://example.com", Region: "cn-beijing"},
			},
			Creds: map[string]config.Credential{
				"myref": {AccessKeyID: "ref-AK", SecretAccessKey: "ref-SK"},
			},
		}
		p, err := factory.RamRoleARN("/tmp/cfg.json", "default", cfg)
		if err != nil {
			t.Fatalf("RamRoleARN: %v", err)
		}
		if p == nil {
			t.Fatal("expected non-nil provider")
		}
	})

	t.Run("missing_source_fails_even_with_env_aksk", func(t *testing.T) {
		cfg := config.Config{Version: 1, Profiles: map[string]config.Profile{
			"default": {Mode: config.AuthModeRamRoleARN, RoleName: "r", AccountID: "1", Endpoint: "http://example.com", Region: "cn-beijing"},
		}}
		_, err := factory.RamRoleARN("/tmp/cfg.json", "default", cfg)
		if err == nil {
			t.Fatal("expected error when source credentials are missing")
		}
	})

	t.Run("missing_cred_ref_fails", func(t *testing.T) {
		cfg := config.Config{Version: 1, Profiles: map[string]config.Profile{
			"default": {Mode: config.AuthModeRamRoleARN, RoleName: "r", AccountID: "1", CredRef: "nonexistent", Endpoint: "http://example.com", Region: "cn-beijing"},
		}}
		_, err := factory.RamRoleARN("/tmp/cfg.json", "default", cfg)
		if err == nil {
			t.Fatal("expected error when cred_ref does not exist")
		}
	})
}

func TestWorkloadProviderSessionTokenSignsTLSRequest(t *testing.T) {
	var seenToken, seenAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenToken = r.Header.Get("X-Security-Token")
		seenAuth = r.Header.Get("Authorization")
		w.WriteHeader(200)
		w.Write([]byte(`{"ResponseMetadata":{"RequestId":"x"},"Result":{}}`))
	}))
	defer srv.Close()

	fp := &fakeProvider{value: auth.Value{AccessKeyID: "AK", SecretAccessKey: "SK", SessionToken: "SESSION-TOKEN-123"}}
	f := &fakeAuthFactory{ramProvider: fp}
	cfg := config.Config{Version: 1, Profiles: map[string]config.Profile{
		"default": {Mode: config.AuthModeRamRoleARN, RoleName: "r", Endpoint: srv.URL, Region: "cn-beijing"},
	}}
	ctx := newTestContext(t, cfg, "/tmp/cfg.json")
	ctx.authFactory = f
	cl, err := ctx.Client()
	if err != nil {
		t.Fatalf("Client: %v", err)
	}
	if _, err := cl.Do(context.Background(), "GET", "/", nil, nil, nil); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if seenToken != "SESSION-TOKEN-123" {
		t.Fatalf("X-Security-Token=%q, want SESSION-TOKEN-123", seenToken)
	}
	// Authorization must be a real TLS signature (Service=TLS scope), not empty.
	if seenAuth == "" {
		t.Fatal("Authorization header is empty, expected a real TLS signature")
	}
	if !strings.Contains(seenAuth, "/TLS/") {
		t.Fatalf("Authorization=%q, expected TLS service scope in signature", seenAuth)
	}
}

func TestSecretsFileNeverImplicitlyCombinesWithWorkloadMode(t *testing.T) {
	clearAuthTestEnv(t)
	// Full static scenario: valid inline AK/SK so the static path succeeds.
	cfg := config.Config{Version: 1, Profiles: map[string]config.Profile{
		"default": {Mode: config.AuthModeRamRoleARN, RoleName: "r", AccessKeyID: "static-AK", SecretAccessKey: "static-SK", Region: "cn-beijing", Endpoint: "http://example.com"},
	}}
	fp := &fakeProvider{value: auth.Value{AccessKeyID: "AK", SecretAccessKey: "SK", SessionToken: "ST"}}
	f := &fakeAuthFactory{ramProvider: fp}
	ctx := newTestContext(t, cfg, "/tmp/cfg.json")
	ctx.authFactory = f
	ctx.forceStaticAuth = true

	cl, err := ctx.Client()
	if err != nil {
		t.Fatalf("Client: %v", err)
	}
	if cl == nil {
		t.Fatal("expected non-nil client from static path")
	}
	// Workload factory must NOT be called when forceStaticAuth is set.
	if f.ramCalls != 0 {
		t.Fatalf("ram factory calls=%d, want 0 when forceStaticAuth is set", f.ramCalls)
	}
}

func TestStaticAndCachedLoginRoutingRemainUnchanged(t *testing.T) {
	fp := &fakeProvider{value: auth.Value{AccessKeyID: "AK", SecretAccessKey: "SK", SessionToken: "ST"}}

	cases := []struct {
		name        string
		mode        string
		wantFactory func(f *fakeAuthFactory) bool // returns true if this mode should call its factory
	}{
		{"empty", "", func(f *fakeAuthFactory) bool { return false }},
		{"ak", config.AuthModeAK, func(f *fakeAuthFactory) bool { return false }},
		{"sso", config.AuthModeSSO, func(f *fakeAuthFactory) bool { return true }},
		{"console", config.AuthModeConsoleLogin, func(f *fakeAuthFactory) bool { return true }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeAuthFactory{ssoProvider: fp, consoleProvider: fp}
			profile := config.Profile{Mode: tc.mode, Region: "cn-beijing", Endpoint: "http://example.com"}
			if tc.mode == config.AuthModeAK || tc.mode == "" {
				profile.AccessKeyID = "AK"
				profile.SecretAccessKey = "SK"
			}
			cfg := config.Config{Version: 1, Profiles: map[string]config.Profile{"default": profile}}
			ctx := newTestContext(t, cfg, "/tmp/cfg.json")
			ctx.authFactory = f
			if _, err := ctx.Client(); err != nil {
				t.Fatalf("Client: %v", err)
			}
			// Workload factories must never be called for these modes.
			if f.ramCalls != 0 || f.oidcCalls != 0 || f.ecsCalls != 0 {
				t.Fatalf("workload factories called: ram=%d oidc=%d ecs=%d, want all 0", f.ramCalls, f.oidcCalls, f.ecsCalls)
			}
			if tc.wantFactory(f) {
				// SSO/Console: only the matching factory called, not the other.
				if tc.mode == config.AuthModeSSO {
					if f.ssoCalls != 1 || f.consoleCalls != 0 {
						t.Fatalf("sso=%d console=%d, want sso=1 console=0", f.ssoCalls, f.consoleCalls)
					}
				} else {
					if f.consoleCalls != 1 || f.ssoCalls != 0 {
						t.Fatalf("sso=%d console=%d, want console=1 sso=0", f.ssoCalls, f.consoleCalls)
					}
				}
			} else {
				// Static modes: no factory called at all.
				if f.ssoCalls != 0 || f.consoleCalls != 0 {
					t.Fatalf("sso=%d console=%d, want both 0 for static mode", f.ssoCalls, f.consoleCalls)
				}
			}
		})
	}
}

// TestOIDCFactoryUsesRoleTRNNotRoleName proves the production OIDC factory maps
// profile.RoleTRN to oidc.Config.RoleTRN and never falls back to RoleName.
func TestOIDCFactoryUsesRoleTRNNotRoleName(t *testing.T) {
	factory := defaultAuthProviderFactory{}
	// RoleTRN is valid, RoleName is empty. With the bug (RoleName used), RoleTRN
	// would be empty and oidc.New fails; with the fix it succeeds.
	cfg := config.Config{Version: 1, Profiles: map[string]config.Profile{
		"p": {Mode: config.AuthModeOIDC, OIDCTokenFile: "/nonexistent/token", RoleTRN: "trn:iam::1:role/real", RoleName: ""},
	}}
	p, err := factory.OIDC("/tmp/cfg.json", "p", cfg)
	if err != nil {
		t.Fatalf("OIDC factory failed when RoleTRN is set: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider")
	}

	// RoleTRN is empty but RoleName is set; factory must fail (must not use
	// RoleName as a fallback for RoleTRN).
	cfg2 := config.Config{Version: 1, Profiles: map[string]config.Profile{
		"p": {Mode: config.AuthModeOIDC, OIDCTokenFile: "/nonexistent/token", RoleTRN: "", RoleName: "POISON-DO-NOT-USE"},
	}}
	_, err = factory.OIDC("/tmp/cfg.json", "p", cfg2)
	if err == nil {
		t.Fatal("OIDC factory should fail when RoleTRN is empty even if RoleName is set")
	}
}

// TestWorkloadProviderFailurePreventsTLSRequest proves that for all three
// workload modes, both factory-construction failure and runtime Retrieve failure
// fail closed: ctx.DoRaw returns an error, the TLS transport receives exactly
// zero requests, only the matching factory method is called, and there is no
// fallback to static/env credentials.
func TestWorkloadProviderFailurePreventsTLSRequest(t *testing.T) {
	clearAuthTestEnv(t)
	// Bait env credentials that must never be used.
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ENV-BAIT-AK")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "ENV-BAIT-SK")
	t.Setenv("VOLCENGINE_TOKEN", "ENV-BAIT-TOKEN")

	modes := []string{config.AuthModeRamRoleARN, config.AuthModeOIDC, config.AuthModeECSRole}
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			t.Run("factory_failure", func(t *testing.T) {
				var tlsRequests int32
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					atomic.AddInt32(&tlsRequests, 1)
					w.WriteHeader(200)
				}))
				defer srv.Close()

				f := &fakeAuthFactory{}
				switch mode {
				case config.AuthModeRamRoleARN:
					f.ramErr = errors.New("factory construction failed")
				case config.AuthModeOIDC:
					f.oidcErr = errors.New("factory construction failed")
				case config.AuthModeECSRole:
					f.ecsErr = errors.New("factory construction failed")
				}
				cfg := config.Config{Version: 1, Profiles: map[string]config.Profile{
					"default": {Mode: mode, RoleName: "r", Endpoint: srv.URL, Region: "cn-beijing"},
				}}
				ctx := newTestContext(t, cfg, "/tmp/cfg.json")
				ctx.authFactory = f

				_, err := ctx.DoRaw("GET", "/", nil, nil, nil)
				if err == nil {
					t.Fatal("expected error from DoRaw when factory fails")
				}
				if got := atomic.LoadInt32(&tlsRequests); got != 0 {
					t.Fatalf("TLS requests=%d, want 0", got)
				}
				// Only the matching factory was called.
				if f.ssoCalls != 0 || f.consoleCalls != 0 {
					t.Fatalf("unexpected sso=%d console=%d calls", f.ssoCalls, f.consoleCalls)
				}
				switch mode {
				case config.AuthModeRamRoleARN:
					if f.ramCalls != 1 || f.oidcCalls != 0 || f.ecsCalls != 0 {
						t.Fatalf("ram=%d oidc=%d ecs=%d, want ram=1 others=0", f.ramCalls, f.oidcCalls, f.ecsCalls)
					}
				case config.AuthModeOIDC:
					if f.oidcCalls != 1 || f.ramCalls != 0 || f.ecsCalls != 0 {
						t.Fatalf("ram=%d oidc=%d ecs=%d, want oidc=1 others=0", f.ramCalls, f.oidcCalls, f.ecsCalls)
					}
				case config.AuthModeECSRole:
					if f.ecsCalls != 1 || f.ramCalls != 0 || f.oidcCalls != 0 {
						t.Fatalf("ram=%d oidc=%d ecs=%d, want ecs=1 others=0", f.ramCalls, f.oidcCalls, f.ecsCalls)
					}
				}
			})

			t.Run("retrieve_failure", func(t *testing.T) {
				var tlsRequests int32
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					atomic.AddInt32(&tlsRequests, 1)
					w.WriteHeader(200)
				}))
				defer srv.Close()

				failingProvider := &fakeProvider{err: errors.New("Retrieve failed at runtime")}
				f := &fakeAuthFactory{}
				switch mode {
				case config.AuthModeRamRoleARN:
					f.ramProvider = failingProvider
				case config.AuthModeOIDC:
					f.oidcProvider = failingProvider
				case config.AuthModeECSRole:
					f.ecsProvider = failingProvider
				}
				cfg := config.Config{Version: 1, Profiles: map[string]config.Profile{
					"default": {Mode: mode, RoleName: "r", Endpoint: srv.URL, Region: "cn-beijing"},
				}}
				ctx := newTestContext(t, cfg, "/tmp/cfg.json")
				ctx.authFactory = f

				_, err := ctx.DoRaw("GET", "/", nil, nil, nil)
				if err == nil {
					t.Fatal("expected error from DoRaw when Retrieve fails")
				}
				if got := atomic.LoadInt32(&tlsRequests); got != 0 {
					t.Fatalf("TLS requests=%d, want 0", got)
				}
				if failingProvider.calls != 1 {
					t.Fatalf("provider Retrieve calls=%d, want 1", failingProvider.calls)
				}
			})
		})
	}
}

// TestWorkloadStatusReaderDoesNotInspectOrCallFactory proves that for workload
// modes (and static), dynamicAuthStatusReader returns (nil, nil) without
// inspecting the factory or calling any factory method, even when the factory
// is typed-nil.
func TestWorkloadStatusReaderDoesNotInspectOrCallFactory(t *testing.T) {
	for _, mode := range []string{config.AuthModeRamRoleARN, config.AuthModeOIDC, config.AuthModeECSRole} {
		t.Run(mode, func(t *testing.T) {
			// typed-nil factory: should not produce a "nil factory" error.
			var nilFactory *fakeAuthFactory
			var factory authProviderFactory = nilFactory
			reader, err := dynamicAuthStatusReader(mode, "/tmp/cfg.json", "default", config.Config{}, factory)
			if err != nil {
				t.Fatalf("expected nil error for workload mode, got: %v", err)
			}
			if reader != nil {
				t.Fatalf("expected nil reader for workload mode, got %T", reader)
			}
		})
	}
}
