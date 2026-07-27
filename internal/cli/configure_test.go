package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth/console"
	"github.com/volcengine-tls/ve-tls-cli/internal/auth/sso"
	"github.com/volcengine-tls/ve-tls-cli/internal/config"
	"github.com/volcengine-tls/ve-tls-cli/internal/output"
)

func TestConfigureSetPreservesExistingAuthFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("VOLCLOG_CONFIG", path)
	cfg := config.DefaultConfig()
	cfg.CurrentProfile = "existing"
	cfg.PutProfile("existing", config.Profile{
		AccessKeyID:     "old-ak",
		SecretAccessKey: "old-sk",
		SecurityToken:   "old-token",
		Region:          "cn-beijing",
		Endpoint:        "https://old.example.com",
		TimeoutSeconds:  29,
		CredRef:         "old-ref",
		Mode:            config.AuthModeSSO,
		SSOSessionName:  "corp-sso",
		AccountID:       "account-1",
		RoleName:        "role-1",
		LoginSession:    "console-cache",
		STSExpiration:   1893456000,
	})
	cfg.PutCred("dormant-cred", config.Credential{
		AccessKeyID:     "cached-ak",
		SecretAccessKey: "cached-sk",
	})
	cfg.SSOSessions["corp-sso"] = config.SSOSession{
		Name:               "corp-sso",
		StartURL:           "https://login.example.com/start",
		Region:             "cn-beijing",
		RegistrationScopes: []string{"openid"},
	}
	if err := config.Save(cfg, path); err != nil {
		t.Fatalf("save initial config: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"configure", "set",
		"--profile", "existing",
		"--ak", "new-ak",
		"--sk", "new-sk",
		"--region", "ap-singapore-1",
		"--endpoint", "https://tls-ap-singapore-1.volces.com",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	loaded, _, err := config.Load()
	if err != nil {
		t.Fatalf("load updated config: %v", err)
	}
	profile, ok := loaded.GetProfile("existing")
	if !ok {
		t.Fatal("updated profile not found")
	}
	if profile.AccessKeyID != "new-ak" ||
		profile.SecretAccessKey != "new-sk" ||
		profile.Region != "ap-singapore-1" ||
		profile.Endpoint != "https://tls-ap-singapore-1.volces.com" {
		t.Fatalf("static fields not reconfigured: %+v", profile)
	}
	if profile.Mode != config.AuthModeAK {
		t.Fatalf("updated profile mode=%q, want %q", profile.Mode, config.AuthModeAK)
	}
	if profile.SSOSessionName != "corp-sso" ||
		profile.AccountID != "account-1" ||
		profile.RoleName != "role-1" ||
		profile.LoginSession != "console-cache" ||
		profile.STSExpiration != 1893456000 {
		t.Fatalf("static reconfiguration cleared dormant auth fields: %+v", profile)
	}
	if _, ok := loaded.GetCred("dormant-cred"); !ok {
		t.Fatal("static reconfiguration deleted dormant credential cache")
	}
	if got, ok := loaded.SSOSessions["corp-sso"]; !ok || got.StartURL != "https://login.example.com/start" {
		t.Fatalf("static reconfiguration deleted SSO session cache: %+v, exists=%t", got, ok)
	}
	if loaded.CurrentProfile != "existing" {
		t.Fatalf("current profile=%q, want existing", loaded.CurrentProfile)
	}
}

func TestConfigure_ListAndDelete(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	t.Setenv("VOLCLOG_CONFIG", cfgPath)

	run := func(args ...string) map[string]any {
		t.Helper()
		var stdout, stderr bytes.Buffer
		code := Run(args, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
		}
		var m map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &m); err != nil {
			t.Fatalf("invalid json: %v; stdout=%q", err, stdout.String())
		}
		return m
	}

	run("configure", "set", "--profile", "tenant-a-cn", "--ak", "akA", "--sk", "skA", "--region", "cn-beijing", "--endpoint", "https://tls-cn-beijing.volces.com")
	run("configure", "set", "--profile", "tenant-b-cn", "--ak", "akB", "--sk", "skB", "--region", "cn-beijing", "--endpoint", "https://tls-cn-beijing.volces.com")
	run("configure", "use", "tenant-b-cn")

	out := run("configure", "list")
	if out["current_profile"] != "tenant-b-cn" {
		t.Fatalf("current_profile=%v", out["current_profile"])
	}
	profiles, ok := out["profiles"].([]any)
	if !ok || len(profiles) != 2 {
		t.Fatalf("profiles=%T %v", out["profiles"], out["profiles"])
	}

	out = run("configure", "list", "--prefix", "tenant-a")
	profiles, _ = out["profiles"].([]any)
	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile, got %v", out["profiles"])
	}

	out = run("configure", "delete", "tenant-a-cn")
	if out["deleted"] != "tenant-a-cn" {
		t.Fatalf("deleted=%v", out["deleted"])
	}
	out = run("configure", "list")
	profiles, _ = out["profiles"].([]any)
	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile after delete, got %v", out["profiles"])
	}

	out = run("configure", "delete", "tenant-b-cn")
	if out["current_profile"] != "" {
		t.Fatalf("current_profile=%v", out["current_profile"])
	}
	out = run("configure", "list")
	profiles, _ = out["profiles"].([]any)
	if len(profiles) != 0 {
		t.Fatalf("expected empty profiles, got %v", out["profiles"])
	}

	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("config file missing: %v", err)
	}
}

func TestConfigure_DeletePrefixRequiresYes(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	t.Setenv("VOLCLOG_CONFIG", cfgPath)

	runOK := func(args ...string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		code := Run(args, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
		}
	}
	runErr := func(args ...string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		code := Run(args, &stdout, &stderr)
		if code == 0 {
			t.Fatalf("expected non-zero exit; stdout=%q stderr=%q", stdout.String(), stderr.String())
		}
	}

	runOK("configure", "set", "--profile", "tenant-a-cn", "--ak", "akA", "--sk", "skA", "--region", "cn-beijing", "--endpoint", "https://tls-cn-beijing.volces.com")
	runOK("configure", "set", "--profile", "tenant-a-sg", "--ak", "akA", "--sk", "skA", "--region", "ap-singapore-1", "--endpoint", "https://tls-ap-singapore-1.volces.com")

	runErr("configure", "delete", "--prefix", "tenant-a")
	runOK("configure", "delete", "--prefix", "tenant-a", "--yes")
}

func TestConfigure_CredRefReuseRequiresExplicitRegion(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	t.Setenv("VOLCLOG_CONFIG", cfgPath)

	run := func(args ...string) map[string]any {
		t.Helper()
		var stdout, stderr bytes.Buffer
		code := Run(args, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
		}
		var m map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &m); err != nil {
			t.Fatalf("invalid json: %v; stdout=%q", err, stdout.String())
		}
		return m
	}

	run("configure", "set", "--profile", "abc-bj", "--cred-ref", "ma-abc-root", "--ak", "akA", "--sk", "skA", "--region", "cn-beijing", "--endpoint", "https://tls-cn-beijing.volces.com")
	out := run("configure", "show", "--profile", "abc-bj")
	if out["cred_ref"] != "ma-abc-root" {
		t.Fatalf("cred_ref=%v", out["cred_ref"])
	}
	if out["region"] != "cn-beijing" {
		t.Fatalf("region=%v", out["region"])
	}
	if out["credential_present"] != true {
		t.Fatalf("credential_present=%v", out["credential_present"])
	}

	run("configure", "set", "--profile", "abc-sg", "--cred-ref", "ma-abc-root", "--region", "ap-singapore-1", "--endpoint", "https://tls-ap-singapore-1.volces.com")
	out = run("configure", "show", "--profile", "abc-sg")
	if out["cred_ref"] != "ma-abc-root" {
		t.Fatalf("cred_ref=%v", out["cred_ref"])
	}
	if out["region"] != "ap-singapore-1" {
		t.Fatalf("region=%v", out["region"])
	}
	if out["credential_present"] != true {
		t.Fatalf("credential_present=%v", out["credential_present"])
	}
}

func TestConfigure_SetRequiresExplicitRegionEvenWhenEndpointLooksCanonical(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	t.Setenv("VOLCLOG_CONFIG", cfgPath)

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"configure", "set",
		"--profile", "abc-bj",
		"--ak", "akA",
		"--sk", "skA",
		"--endpoint", "https://tls-cn-beijing.volces.com",
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected non-zero exit; stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	out := stdout.String() + stderr.String()
	if !strings.Contains(out, "missing required fields: --region") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestConfigure_ProfileAliasAddUse(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	t.Setenv("VOLCLOG_CONFIG", cfgPath)

	run := func(args ...string) map[string]any {
		t.Helper()
		var stdout, stderr bytes.Buffer
		code := Run(args, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
		}
		var m map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &m); err != nil {
			t.Fatalf("invalid json: %v; stdout=%q", err, stdout.String())
		}
		return m
	}

	run("configure", "profile", "add", "stage", "--ak", "akS", "--sk", "skS", "--region", "cn-beijing", "--endpoint", "https://tls-cn-beijing.volces.com")
	out := run("configure", "profile", "use", "stage")
	if out["current_profile"] != "stage" {
		t.Fatalf("current_profile=%v", out["current_profile"])
	}
}

func TestConfigure_ShowAndListExposeAgentContextFields(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	t.Setenv("VOLCLOG_CONFIG", cfgPath)

	run := func(args ...string) map[string]any {
		t.Helper()
		var stdout, stderr bytes.Buffer
		code := Run(args, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
		}
		var m map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &m); err != nil {
			t.Fatalf("invalid json: %v; stdout=%q", err, stdout.String())
		}
		return m
	}

	run("configure", "set", "--profile", "inline-cn", "--ak", "akA", "--sk", "skA", "--region", "cn-beijing", "--endpoint", "https://tls-cn-beijing.volces.com")
	run("configure", "set", "--profile", "ref-cn", "--cred-ref", "ma-root", "--ak", "akR", "--sk", "skR", "--region", "cn-beijing", "--endpoint", "https://tls-cn-beijing.volces.com")

	show := run("configure", "show", "--profile", "inline-cn")
	if show["effective_profile"] != "inline-cn" {
		t.Fatalf("effective_profile=%v", show["effective_profile"])
	}
	if show["credential_source"] != "profile_inline" {
		t.Fatalf("credential_source=%v", show["credential_source"])
	}

	list := run("configure", "list")
	items, ok := list["profiles"].([]any)
	if !ok {
		t.Fatalf("profiles=%T", list["profiles"])
	}
	foundRef := false
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if m["profile"] == "ref-cn" {
			foundRef = true
			if m["effective_profile"] != "ref-cn" {
				t.Fatalf("effective_profile=%v", m["effective_profile"])
			}
			if m["credential_source"] != "profile_cred_ref" {
				t.Fatalf("credential_source=%v", m["credential_source"])
			}
		}
	}
	if !foundRef {
		t.Fatalf("profile ref-cn not found in list: %v", list["profiles"])
	}
}

func TestConfigure_CredDelete_SuccessAndInUseGuard(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	t.Setenv("VOLCLOG_CONFIG", cfgPath)

	runOK := func(args ...string) map[string]any {
		t.Helper()
		var stdout, stderr bytes.Buffer
		code := Run(args, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
		}
		var m map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &m); err != nil {
			t.Fatalf("invalid json: %v; stdout=%q", err, stdout.String())
		}
		return m
	}
	runErr := func(args ...string) string {
		t.Helper()
		var stdout, stderr bytes.Buffer
		code := Run(args, &stdout, &stderr)
		if code == 0 {
			t.Fatalf("expected non-zero exit; stdout=%q stderr=%q", stdout.String(), stderr.String())
		}
		return stdout.String() + stderr.String()
	}

	runOK("configure", "set", "--profile", "ref-cn", "--cred-ref", "ma-root", "--ak", "akR", "--sk", "skR", "--region", "cn-beijing", "--endpoint", "https://tls-cn-beijing.volces.com")

	errOut := runErr("configure", "cred", "delete", "ma-root")
	if !strings.Contains(errOut, "credential in use by profiles") {
		t.Fatalf("expected in-use error, got: %q", errOut)
	}

	runOK("configure", "delete", "ref-cn")
	out := runOK("configure", "cred", "delete", "ma-root")
	if out["deleted"] != "ma-root" {
		t.Fatalf("deleted=%v", out["deleted"])
	}
}

func TestConfigure_CredDelete_RequiresName(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	t.Setenv("VOLCLOG_CONFIG", cfgPath)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"configure", "cred", "delete"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected non-zero exit; stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	out := stdout.String() + stderr.String()
	if !strings.Contains(out, "missing credential name") {
		t.Fatalf("unexpected error output: %q", out)
	}
}

func TestContextUpdateConfigRefreshesOnlyAfterSuccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := config.DefaultConfig()
	cfg.CurrentProfile = "old"
	cfg.PutProfile("old", config.Profile{Region: "cn-beijing"})
	if err := config.Save(cfg, path); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	ctx := newContext(&bytes.Buffer{}, &bytes.Buffer{}, "", "", "")
	ctx.cfg = cfg
	ctx.cfgPath = path

	if err := ctx.UpdateConfig(func(latest *config.Config) error {
		latest.CurrentProfile = "new"
		latest.PutProfile("new", config.Profile{Region: "ap-singapore-1"})
		return nil
	}); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	if ctx.cfg.CurrentProfile != "new" {
		t.Fatalf("context current profile=%q, want refreshed new config", ctx.cfg.CurrentProfile)
	}

	sentinel := errors.New("stop update")
	err := ctx.UpdateConfig(func(latest *config.Config) error {
		latest.CurrentProfile = "must-not-persist"
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("UpdateConfig error=%v, want sentinel", err)
	}
	if ctx.cfg.CurrentProfile != "new" {
		t.Fatalf("failed update refreshed context to %q", ctx.cfg.CurrentProfile)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var persisted config.Config
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if persisted.CurrentProfile != "new" {
		t.Fatalf("failed update persisted current profile %q", persisted.CurrentProfile)
	}
}

func TestConfigureSetConcurrentWithDynamicLoginPatchPreservesBoth(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("VOLCLOG_CONFIG", path)
	cfg := config.DefaultConfig()
	cfg.CurrentProfile = "static"
	cfg.PutProfile("static", config.Profile{
		Mode:           config.AuthModeSSO,
		SSOSessionName: "dormant-sso",
		Region:         "cn-beijing",
		Endpoint:       "https://tls-cn-beijing.volces.com",
	})
	cfg.PutProfile("dynamic", config.Profile{
		Mode:     config.AuthModeConsoleLogin,
		Region:   "ap-singapore-1",
		Endpoint: "https://tls-ap-singapore-1.volces.com",
	})
	if err := config.Save(cfg, path); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		var stdout, stderr bytes.Buffer
		code := Run([]string{
			"configure", "set",
			"--profile", "static",
			"--ak", "new-ak",
			"--sk", "new-sk",
			"--region", "cn-beijing",
			"--endpoint", "https://tls-cn-beijing.volces.com",
		}, &stdout, &stderr)
		if code != 0 {
			errs <- errors.New(stdout.String() + stderr.String())
			return
		}
		errs <- nil
	}()
	go func() {
		defer wg.Done()
		<-start
		_, err := config.Update(path, func(latest *config.Config) error {
			return latest.PatchProfile("dynamic", func(profile *config.Profile) error {
				profile.LoginSession = "fresh-login"
				profile.STSExpiration = 1_900_000_000
				return nil
			})
		})
		errs <- err
	}()
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent update: %v", err)
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read final config: %v", err)
	}
	var got config.Config
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode final config: %v", err)
	}
	static, _ := got.GetProfile("static")
	if static.AccessKeyID != "new-ak" || static.SecretAccessKey != "new-sk" || static.Mode != config.AuthModeAK {
		t.Fatalf("static configure set missing: %+v", static)
	}
	if static.SSOSessionName != "dormant-sso" {
		t.Fatalf("static configure set cleared dormant auth metadata: %+v", static)
	}
	dynamic, _ := got.GetProfile("dynamic")
	if dynamic.LoginSession != "fresh-login" || dynamic.STSExpiration != 1_900_000_000 {
		t.Fatalf("dynamic login patch missing: %+v", dynamic)
	}
	if dynamic.Region != "ap-singapore-1" || dynamic.Mode != config.AuthModeConsoleLogin {
		t.Fatalf("dynamic login patch lost non-conflicting fields: %+v", dynamic)
	}
}

func TestConfigureUseAndCredDeleteRecomputeAgainstLatestConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := config.DefaultConfig()
	cfg.PutCred("shared", config.Credential{AccessKeyID: "ak", SecretAccessKey: "sk"})
	if err := config.Save(cfg, path); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	ctx := newContext(&bytes.Buffer{}, &bytes.Buffer{}, "", "", "")
	ctx.cfg = cfg
	ctx.cfgPath = path

	if _, err := config.Update(path, func(latest *config.Config) error {
		latest.PutProfile("new-profile", config.Profile{CredRef: "shared"})
		return nil
	}); err != nil {
		t.Fatalf("external update: %v", err)
	}
	if _, err := configureUse(ctx, []string{"new-profile"}); err != nil {
		t.Fatalf("configureUse rejected profile added after context load: %v", err)
	}
	if _, err := configureCredDelete(ctx, []string{"shared"}); err == nil ||
		!strings.Contains(err.Error(), "credential in use by profiles: new-profile") {
		t.Fatalf("configureCredDelete error=%v, want latest in-use guard", err)
	}
}

func TestConfigureDeletePrefixNoMatchIsNoOp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := config.DefaultConfig()
	cfg.CurrentProfile = "stale-current"
	cfg.PutProfile("kept", config.Profile{Region: "cn-beijing"})
	if err := config.Save(cfg, path); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat before: %v", err)
	}
	ctx := newContext(&bytes.Buffer{}, &bytes.Buffer{}, "", "", "")
	ctx.cfg = cfg
	ctx.cfgPath = path

	out, err := configureDelete(ctx, []string{"--prefix", "missing-", "--yes"})
	if err != nil {
		t.Fatalf("configureDelete: %v", err)
	}
	result := out.(map[string]any)
	deleted, ok := result["deleted"].([]string)
	if !ok || deleted == nil || len(deleted) != 0 {
		t.Fatalf("deleted=%T %#v, want non-nil empty []string", result["deleted"], result["deleted"])
	}
	if got := result["current_profile"]; got != "stale-current" {
		t.Fatalf("current_profile=%v, want unchanged stale-current", got)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after: %v", err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("no-match prefix delete replaced the config file")
	}
}

// failingStatusReader always returns an error from Status, simulating a cache
// read failure. It is used to verify that dynamic profile fields are still
// emitted with safe defaults.
type failingStatusReader struct{}

func (failingStatusReader) Status(context.Context, string) (authStatus, error) {
	return authStatus{}, errors.New("cache read failed: secret_access_key_canary")
}

// TestConfigureShowDynamicProfileEmitsAuthFields proves that configure show for
// a dynamic profile emits auth_mode, provider, auth_present, expires_at, and
// refresh_required with values from the status reader.
func TestConfigureShowDynamicProfileEmitsAuthFields(t *testing.T) {
	expires := time.Now().Add(2 * time.Hour).UTC()
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
	ctx := newContext(&bytes.Buffer{}, &bytes.Buffer{}, output.FormatJSON, "", "")
	ctx.cfg = cfg
	ctx.cfgPath = "/tmp/test-config.json"
	ctx.Profile = "dyn"
	ctx.authFactory = &fakeAuthFactory{
		ssoStatus: &staticStatusReader{status: authStatus{
			Provider:        "sso",
			Present:         true,
			ExpiresAt:       expires,
			RefreshRequired: false,
		}},
	}

	out, err := configureShow(ctx, []string{"--profile", "dyn"})
	if err != nil {
		t.Fatalf("configureShow: %v", err)
	}
	m := out.(map[string]any)

	if got := m["auth_mode"]; got != config.AuthModeSSO {
		t.Fatalf("auth_mode=%v, want %s", got, config.AuthModeSSO)
	}
	if got := m["provider"]; got != "sso" {
		t.Fatalf("provider=%v, want sso", got)
	}
	if got := m["auth_present"]; got != true {
		t.Fatalf("auth_present=%v, want true", got)
	}
	if got := m["expires_at"]; got != expires.UTC().Format(time.RFC3339) {
		t.Fatalf("expires_at=%v, want %s", got, expires.UTC().Format(time.RFC3339))
	}
	if got := m["refresh_required"]; got != false {
		t.Fatalf("refresh_required=%v, want false", got)
	}
}

// TestConfigureListDynamicProfileEmitsAuthFields proves that configure list for
// a dynamic profile emits the same auth fields as show.
func TestConfigureListDynamicProfileEmitsAuthFields(t *testing.T) {
	expires := time.Now().Add(2 * time.Hour).UTC()
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
	ctx := newContext(&bytes.Buffer{}, &bytes.Buffer{}, output.FormatJSON, "", "")
	ctx.cfg = cfg
	ctx.cfgPath = "/tmp/test-config.json"
	ctx.authFactory = &fakeAuthFactory{
		consoleStatus: &staticStatusReader{status: authStatus{
			Provider:        "console-login",
			Present:         true,
			ExpiresAt:       expires,
			RefreshRequired: true,
		}},
	}

	out, err := configureList(ctx, nil)
	if err != nil {
		t.Fatalf("configureList: %v", err)
	}
	m := out.(map[string]any)
	profiles := m["profiles"].([]map[string]any)
	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(profiles))
	}
	p := profiles[0]
	if got := p["auth_mode"]; got != config.AuthModeConsoleLogin {
		t.Fatalf("auth_mode=%v, want %s", got, config.AuthModeConsoleLogin)
	}
	if got := p["provider"]; got != "console-login" {
		t.Fatalf("provider=%v, want console-login", got)
	}
	if got := p["auth_present"]; got != true {
		t.Fatalf("auth_present=%v, want true", got)
	}
	if got := p["expires_at"]; got != expires.UTC().Format(time.RFC3339) {
		t.Fatalf("expires_at=%v, want %s", got, expires.UTC().Format(time.RFC3339))
	}
	if got := p["refresh_required"]; got != true {
		t.Fatalf("refresh_required=%v, want true", got)
	}
}

// TestConfigureDynamicProfileFieldsDeterministicOnReaderFailure proves that
// when the status reader fails, the dynamic auth fields are still emitted with
// safe defaults: provider from mode, auth_present=false, expires_at="",
// refresh_required=true. No fields are silently omitted.
func TestConfigureDynamicProfileFieldsDeterministicOnReaderFailure(t *testing.T) {
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
	ctx := newContext(&bytes.Buffer{}, &bytes.Buffer{}, output.FormatJSON, "", "")
	ctx.cfg = cfg
	ctx.cfgPath = "/tmp/test-config.json"
	ctx.Profile = "dyn"
	ctx.authFactory = &fakeAuthFactory{ssoStatus: failingStatusReader{}}

	out, err := configureShow(ctx, []string{"--profile", "dyn"})
	if err != nil {
		t.Fatalf("configureShow: %v", err)
	}
	m := out.(map[string]any)

	// All dynamic fields must be present even though the reader failed.
	for _, key := range []string{"auth_mode", "provider", "auth_present", "expires_at", "refresh_required"} {
		if _, ok := m[key]; !ok {
			t.Fatalf("expected dynamic field %q to be present on reader failure, got keys: %v", key, m)
		}
	}
	if got := m["auth_mode"]; got != config.AuthModeSSO {
		t.Fatalf("auth_mode=%v, want %s", got, config.AuthModeSSO)
	}
	if got := m["provider"]; got != "sso" {
		t.Fatalf("provider=%v, want sso (from mode)", got)
	}
	if got := m["auth_present"]; got != false {
		t.Fatalf("auth_present=%v, want false", got)
	}
	if got := m["expires_at"]; got != "" {
		t.Fatalf("expires_at=%v, want empty", got)
	}
	if got := m["refresh_required"]; got != true {
		t.Fatalf("refresh_required=%v, want true", got)
	}
}

// TestConfigureDynamicProfileFieldsDeterministicOnFactoryFailure proves that
// when the factory itself returns an error (e.g. SSO session not found), the
// dynamic fields are still emitted with safe defaults.
func TestConfigureDynamicProfileFieldsDeterministicOnFactoryFailure(t *testing.T) {
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
	ctx := newContext(&bytes.Buffer{}, &bytes.Buffer{}, output.FormatJSON, "", "")
	ctx.cfg = cfg
	ctx.cfgPath = "/tmp/test-config.json"
	ctx.Profile = "dyn"
	// Factory returns an error; no reader is produced.
	ctx.authFactory = &fakeAuthFactory{ssoErr: errors.New("sso session not found")}

	out, err := configureShow(ctx, []string{"--profile", "dyn"})
	if err != nil {
		t.Fatalf("configureShow: %v", err)
	}
	m := out.(map[string]any)

	for _, key := range []string{"auth_mode", "provider", "auth_present", "expires_at", "refresh_required"} {
		if _, ok := m[key]; !ok {
			t.Fatalf("expected dynamic field %q to be present on factory failure, got keys: %v", key, m)
		}
	}
	if got := m["provider"]; got != "sso" {
		t.Fatalf("provider=%v, want sso (from mode)", got)
	}
	if got := m["auth_present"]; got != false {
		t.Fatalf("auth_present=%v, want false", got)
	}
	if got := m["refresh_required"]; got != true {
		t.Fatalf("refresh_required=%v, want true", got)
	}
}

// TestConfigureStaticProfileHasNoDynamicFields proves that static profiles do
// not emit the dynamic-only fields, preserving the existing output contract.
func TestConfigureStaticProfileHasNoDynamicFields(t *testing.T) {
	cfg := config.Config{
		Version: 1,
		Profiles: map[string]config.Profile{
			"static": {
				Mode:            config.AuthModeAK,
				AccessKeyID:     "AKLTstatic",
				SecretAccessKey: "static-secret",
				Region:          "cn-beijing",
				Endpoint:        "https://tls-cn-beijing.volces.com",
			},
		},
	}
	ctx := newContext(&bytes.Buffer{}, &bytes.Buffer{}, output.FormatJSON, "", "")
	ctx.cfg = cfg
	ctx.cfgPath = "/tmp/test-config.json"
	ctx.Profile = "static"

	out, err := configureShow(ctx, []string{"--profile", "static"})
	if err != nil {
		t.Fatalf("configureShow: %v", err)
	}
	m := out.(map[string]any)

	for _, key := range []string{"auth_mode", "provider", "auth_present", "expires_at", "refresh_required"} {
		if _, ok := m[key]; ok {
			t.Fatalf("static profile must not contain dynamic field %q: %v", key, m)
		}
	}
	// Static fields must be unchanged.
	if got := m["credential_source"]; got != "profile_inline" {
		t.Fatalf("credential_source=%v, want profile_inline", got)
	}
	if got := m["access_key_id"]; got != "AKL****tic" {
		t.Fatalf("access_key_id=%v, want masked AKL****tic", got)
	}
}

// TestConfigureShowSSOInvalidCacheReportsNotPresent proves that configure show
// for a dynamic SSO profile whose cache has a future expiration but missing
// credentials reports auth_present=false and refresh_required=true. The test
// writes a real cache file and uses the production ssoStatusReader.
func TestConfigureShowSSOInvalidCacheReportsNotPresent(t *testing.T) {
	clearAuthTestEnv(t)
	dir := t.TempDir()

	cache, err := sso.NewFileCache(filepath.Join(dir, "sso", "cache"))
	if err != nil {
		t.Fatalf("NewFileCache: %v", err)
	}
	if err := cache.WriteSTS(&sso.STSCache{
		SessionName:  "bad-session",
		AccountID:    "acct-1",
		RoleName:     "role-1",
		AccessKeyID:  "AKLTpartial",
		ProviderName: sso.ProviderName,
		ExpiresAt:    time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("WriteSTS: %v", err)
	}

	cfg := config.Config{
		Version: 1,
		Profiles: map[string]config.Profile{
			"sso": {
				Mode:           config.AuthModeSSO,
				Region:         "cn-beijing",
				Endpoint:       "https://tls-cn-beijing.volces.com",
				SSOSessionName: "bad-session",
				AccountID:      "acct-1",
				RoleName:       "role-1",
			},
		},
	}
	var stdout, stderr bytes.Buffer
	ctx := newContext(&stdout, &stderr, output.FormatJSON, "", "")
	ctx.cfg = cfg
	ctx.cfgPath = filepath.Join(dir, "config.json")
	ctx.Profile = "sso"
	ctx.authFactory = &fakeAuthFactory{
		ssoStatus: &ssoStatusReader{
			cache:       cache,
			sessionName: "bad-session",
			accountID:   "acct-1",
			roleName:    "role-1",
			clock:       time.Now,
		},
	}

	out, err := configureShow(ctx, []string{"--profile", "sso"})
	if err != nil {
		t.Fatalf("configureShow: %v", err)
	}
	m := out.(map[string]any)
	if got := m["auth_present"]; got != false {
		t.Fatalf("auth_present=%v, want false", got)
	}
	if got := m["refresh_required"]; got != true {
		t.Fatalf("refresh_required=%v, want true", got)
	}
	if got := m["provider"]; got != "sso" {
		t.Fatalf("provider=%v, want sso", got)
	}
}

// TestConfigureShowConsoleInvalidCacheReportsNotPresent proves that configure
// show for a dynamic Console profile whose cache has a future expiration but
// invalid schema reports auth_present=false and refresh_required=true.
func TestConfigureShowConsoleInvalidCacheReportsNotPresent(t *testing.T) {
	clearAuthTestEnv(t)
	dir := t.TempDir()

	cacheDir := filepath.Join(dir, "login", "cache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	cache, err := console.NewFileCache(cacheDir)
	if err != nil {
		t.Fatalf("NewFileCache: %v", err)
	}
	invalidCache := console.LoginTokenCache{
		LoginSession: "bad-session",
		AccessToken:  json.RawMessage(`{"not":"sts"}`),
		Scope:        console.Scope,
		ClientID:     "not-a-frozen-client-id",
		IssuedAt:     time.Now().UTC().Format(time.RFC3339),
		ExpiresIn:    3600,
		TokenType:    "sts",
	}
	data, err := json.Marshal(invalidCache)
	if err != nil {
		t.Fatalf("marshal invalid cache: %v", err)
	}
	if err := cache.WriteRaw("bad-session", data); err != nil {
		t.Fatalf("WriteRaw: %v", err)
	}

	cfg := config.Config{
		Version: 1,
		Profiles: map[string]config.Profile{
			"console": {
				Mode:         config.AuthModeConsoleLogin,
				Region:       "cn-beijing",
				Endpoint:     "https://tls-cn-beijing.volces.com",
				LoginSession: "bad-session",
			},
		},
	}
	var stdout, stderr bytes.Buffer
	ctx := newContext(&stdout, &stderr, output.FormatJSON, "", "")
	ctx.cfg = cfg
	ctx.cfgPath = filepath.Join(dir, "config.json")
	ctx.Profile = "console"
	ctx.authFactory = &fakeAuthFactory{
		consoleStatus: &consoleStatusReader{
			cache:        cache,
			loginSession: "bad-session",
			clock:        time.Now,
		},
	}

	out, err := configureShow(ctx, []string{"--profile", "console"})
	if err != nil {
		t.Fatalf("configureShow: %v", err)
	}
	m := out.(map[string]any)
	if got := m["auth_present"]; got != false {
		t.Fatalf("auth_present=%v, want false", got)
	}
	if got := m["refresh_required"]; got != true {
		t.Fatalf("refresh_required=%v, want true", got)
	}
	if got := m["provider"]; got != "console-login" {
		t.Fatalf("provider=%v, want console-login", got)
	}
}

// TestConfigureListSSOInvalidCacheReportsNotPresent proves that configure list
// for a dynamic SSO profile whose cache has a future expiration but missing
// credentials reports auth_present=false, expires_at="", and
// refresh_required=true. The test writes a real cache file and uses the
// production ssoStatusReader.
func TestConfigureListSSOInvalidCacheReportsNotPresent(t *testing.T) {
	clearAuthTestEnv(t)
	dir := t.TempDir()

	cache, err := sso.NewFileCache(filepath.Join(dir, "sso", "cache"))
	if err != nil {
		t.Fatalf("NewFileCache: %v", err)
	}
	if err := cache.WriteSTS(&sso.STSCache{
		SessionName:  "bad-session",
		AccountID:    "acct-1",
		RoleName:     "role-1",
		AccessKeyID:  "AKLTpartial",
		ProviderName: sso.ProviderName,
		ExpiresAt:    time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("WriteSTS: %v", err)
	}

	cfg := config.Config{
		Version: 1,
		Profiles: map[string]config.Profile{
			"sso": {
				Mode:           config.AuthModeSSO,
				Region:         "cn-beijing",
				Endpoint:       "https://tls-cn-beijing.volces.com",
				SSOSessionName: "bad-session",
				AccountID:      "acct-1",
				RoleName:       "role-1",
			},
		},
	}
	var stdout, stderr bytes.Buffer
	ctx := newContext(&stdout, &stderr, output.FormatJSON, "", "")
	ctx.cfg = cfg
	ctx.cfgPath = filepath.Join(dir, "config.json")
	ctx.authFactory = &fakeAuthFactory{
		ssoStatus: &ssoStatusReader{
			cache:       cache,
			sessionName: "bad-session",
			accountID:   "acct-1",
			roleName:    "role-1",
			clock:       time.Now,
		},
	}

	out, err := configureList(ctx, nil)
	if err != nil {
		t.Fatalf("configureList: %v", err)
	}
	m := out.(map[string]any)
	profiles := m["profiles"].([]map[string]any)
	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(profiles))
	}
	p := profiles[0]
	if got := p["auth_present"]; got != false {
		t.Fatalf("auth_present=%v, want false", got)
	}
	if got := p["expires_at"]; got != "" {
		t.Fatalf("expires_at=%v, want empty", got)
	}
	if got := p["refresh_required"]; got != true {
		t.Fatalf("refresh_required=%v, want true", got)
	}
	if got := p["provider"]; got != "sso" {
		t.Fatalf("provider=%v, want sso", got)
	}
}

// TestConfigureListConsoleInvalidCacheReportsNotPresent proves that configure
// list for a dynamic Console profile whose cache has a future expiration but
// invalid schema reports auth_present=false, expires_at="", and
// refresh_required=true. The test writes a real cache file and uses the
// production consoleStatusReader.
func TestConfigureListConsoleInvalidCacheReportsNotPresent(t *testing.T) {
	clearAuthTestEnv(t)
	dir := t.TempDir()

	cacheDir := filepath.Join(dir, "login", "cache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	cache, err := console.NewFileCache(cacheDir)
	if err != nil {
		t.Fatalf("NewFileCache: %v", err)
	}
	invalidCache := console.LoginTokenCache{
		LoginSession: "bad-session",
		AccessToken:  json.RawMessage(`{"not":"sts"}`),
		Scope:        console.Scope,
		ClientID:     "not-a-frozen-client-id",
		IssuedAt:     time.Now().UTC().Format(time.RFC3339),
		ExpiresIn:    3600,
		TokenType:    "sts",
	}
	data, err := json.Marshal(invalidCache)
	if err != nil {
		t.Fatalf("marshal invalid cache: %v", err)
	}
	if err := cache.WriteRaw("bad-session", data); err != nil {
		t.Fatalf("WriteRaw: %v", err)
	}

	cfg := config.Config{
		Version: 1,
		Profiles: map[string]config.Profile{
			"console": {
				Mode:         config.AuthModeConsoleLogin,
				Region:       "cn-beijing",
				Endpoint:     "https://tls-cn-beijing.volces.com",
				LoginSession: "bad-session",
			},
		},
	}
	var stdout, stderr bytes.Buffer
	ctx := newContext(&stdout, &stderr, output.FormatJSON, "", "")
	ctx.cfg = cfg
	ctx.cfgPath = filepath.Join(dir, "config.json")
	ctx.authFactory = &fakeAuthFactory{
		consoleStatus: &consoleStatusReader{
			cache:        cache,
			loginSession: "bad-session",
			clock:        time.Now,
		},
	}

	out, err := configureList(ctx, nil)
	if err != nil {
		t.Fatalf("configureList: %v", err)
	}
	m := out.(map[string]any)
	profiles := m["profiles"].([]map[string]any)
	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(profiles))
	}
	p := profiles[0]
	if got := p["auth_present"]; got != false {
		t.Fatalf("auth_present=%v, want false", got)
	}
	if got := p["expires_at"]; got != "" {
		t.Fatalf("expires_at=%v, want empty", got)
	}
	if got := p["refresh_required"]; got != true {
		t.Fatalf("refresh_required=%v, want true", got)
	}
	if got := p["provider"]; got != "console-login" {
		t.Fatalf("provider=%v, want console-login", got)
	}
}

// TestConfigureShowSSOValidSTSButMissingTokenReportsNotPresent proves that
// configure show for a dynamic SSO profile reports auth_present=false when the
// STS cache is valid but the token cache is missing, matching the doctor and
// SSOProvider behavior. Uses the production defaultAuthProviderFactory with a
// real sso.FileCache and asserts the status reader is actually constructed.
func TestConfigureShowSSOValidSTSButMissingTokenReportsNotPresent(t *testing.T) {
	clearAuthTestEnv(t)
	dir := t.TempDir()

	cache, err := sso.NewFileCache(filepath.Join(dir, "sso", "cache"))
	if err != nil {
		t.Fatalf("NewFileCache: %v", err)
	}
	// Write a valid future STS cache but NO token cache.
	if err := cache.WriteSTS(&sso.STSCache{
		SessionName:     "sts-only",
		AccountID:       "acct-1",
		RoleName:        "role-1",
		AccessKeyID:     "AKLTstsonly",
		SecretAccessKey: "sts-only-secret",
		SessionToken:    "sts-only-token",
		ProviderName:    sso.ProviderName,
		ExpiresAt:       time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("WriteSTS: %v", err)
	}

	cfg := config.Config{
		Version: 1,
		Profiles: map[string]config.Profile{
			"sso": {
				Mode:           config.AuthModeSSO,
				Region:         "cn-beijing",
				Endpoint:       "https://tls-cn-beijing.volces.com",
				SSOSessionName: "sts-only",
				AccountID:      "acct-1",
				RoleName:       "role-1",
			},
		},
		SSOSessions: map[string]config.SSOSession{
			"sts-only": {
				Name:     "sts-only",
				StartURL: "https://login.example.com/start",
				Region:   "cn-beijing",
			},
		},
	}
	cfgPath := filepath.Join(dir, "config.json")
	if err := config.Save(cfg, cfgPath); err != nil {
		t.Fatalf("save config: %v", err)
	}

	// Assert the production factory constructs a real status reader (no error,
	// non-nil) before checking output — proves the test reaches the real reader
	// rather than failing early on "sso session not found".
	reader, rerr := dynamicAuthStatusReader(config.AuthModeSSO, cfgPath, "sso", cfg, nil)
	if rerr != nil {
		t.Fatalf("dynamicAuthStatusReader: %v", rerr)
	}
	if reader == nil {
		t.Fatalf("expected non-nil status reader")
	}

	var stdout, stderr bytes.Buffer
	ctx := newContext(&stdout, &stderr, output.FormatJSON, "", "")
	ctx.cfg = cfg
	ctx.cfgPath = cfgPath
	ctx.Profile = "sso"
	ctx.authFactory = nil

	out, err := configureShow(ctx, []string{"--profile", "sso"})
	if err != nil {
		t.Fatalf("configureShow: %v", err)
	}
	m := out.(map[string]any)
	if got := m["auth_present"]; got != false {
		t.Fatalf("auth_present=%v, want false (token cache missing)", got)
	}
	if got := m["expires_at"]; got != "" {
		t.Fatalf("expires_at=%v, want empty", got)
	}
	if got := m["refresh_required"]; got != true {
		t.Fatalf("refresh_required=%v, want true", got)
	}
}

// TestConfigureSSOValidSTSButInvalidTokenFailClosed proves that both configure
// show and configure list report auth_present=false, expires_at empty, and
// refresh_required=true when the STS cache is valid but the token cache is
// either missing or JSON-corrupt. Uses the production defaultAuthProviderFactory
// with a real sso.FileCache. The corrupt case writes syntactically invalid JSON
// directly to the cache file to exercise the ReadToken decode error path.
func TestConfigureSSOValidSTSButInvalidTokenFailClosed(t *testing.T) {
	cases := []struct {
		name       string
		writeToken func(cacheDir string) error
	}{
		{
			name: "missing_token",
			writeToken: func(cacheDir string) error {
				return nil // no token cache written
			},
		},
		{
			name: "corrupt_token_json",
			writeToken: func(cacheDir string) error {
				// Write a valid token first so the file exists, then overwrite it
				// with syntactically invalid JSON to corrupt it.
				cache, err := sso.NewFileCache(cacheDir)
				if err != nil {
					return err
				}
				if err := cache.WriteToken(&sso.TokenCache{
					StartURL:     "https://login.example.com/start",
					SessionName:  "bad-tok",
					AccessToken:  "valid-access-token",
					ExpiresAt:    time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
					ClientID:     "valid-client-id",
					ClientSecret: "valid-client-secret",
					Region:       "cn-beijing",
				}); err != nil {
					return err
				}
				// Locate the token-*.json file and overwrite with invalid JSON.
				matches, gerr := filepath.Glob(filepath.Join(cacheDir, "token-*.json"))
				if gerr != nil {
					return gerr
				}
				if len(matches) != 1 {
					return fmt.Errorf("expected 1 token file, got %d", len(matches))
				}
				return os.WriteFile(matches[0], []byte("{"), 0600)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearAuthTestEnv(t)
			dir := t.TempDir()
			cacheDir := filepath.Join(dir, "sso", "cache")

			cache, err := sso.NewFileCache(cacheDir)
			if err != nil {
				t.Fatalf("NewFileCache: %v", err)
			}
			// Valid future STS cache.
			if err := cache.WriteSTS(&sso.STSCache{
				SessionName:     "bad-tok",
				AccountID:       "acct-1",
				RoleName:        "role-1",
				AccessKeyID:     "AKLTbadtok",
				SecretAccessKey: "bad-tok-secret",
				SessionToken:    "bad-tok-token",
				ProviderName:    sso.ProviderName,
				ExpiresAt:       time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
			}); err != nil {
				t.Fatalf("WriteSTS: %v", err)
			}
			if err := tc.writeToken(cacheDir); err != nil {
				t.Fatalf("writeToken: %v", err)
			}

			// For the corrupt case, assert ReadToken actually returns an error,
			// proving the precondition is real.
			if tc.name == "corrupt_token_json" {
				if _, rerr := cache.ReadToken("https://login.example.com/start", "bad-tok"); rerr == nil {
					t.Fatalf("expected ReadToken to return error for corrupt JSON")
				}
			}

			cfg := config.Config{
				Version: 1,
				Profiles: map[string]config.Profile{
					"sso": {
						Mode:           config.AuthModeSSO,
						Region:         "cn-beijing",
						Endpoint:       "https://tls-cn-beijing.volces.com",
						SSOSessionName: "bad-tok",
						AccountID:      "acct-1",
						RoleName:       "role-1",
					},
				},
				SSOSessions: map[string]config.SSOSession{
					"bad-tok": {
						Name:     "bad-tok",
						StartURL: "https://login.example.com/start",
						Region:   "cn-beijing",
					},
				},
			}
			cfgPath := filepath.Join(dir, "config.json")
			if err := config.Save(cfg, cfgPath); err != nil {
				t.Fatalf("save config: %v", err)
			}

			// Assert the production factory constructs a real status reader.
			reader, rerr := dynamicAuthStatusReader(config.AuthModeSSO, cfgPath, "sso", cfg, nil)
			if rerr != nil {
				t.Fatalf("dynamicAuthStatusReader: %v", rerr)
			}
			if reader == nil {
				t.Fatalf("expected non-nil status reader")
			}

			newCtx := func() *Context {
				c := newContext(&bytes.Buffer{}, &bytes.Buffer{}, output.FormatJSON, "", "")
				c.cfg = cfg
				c.cfgPath = cfgPath
				c.Profile = "sso"
				c.authFactory = nil
				return c
			}

			// configure show must fail closed.
			showOut, err := configureShow(newCtx(), []string{"--profile", "sso"})
			if err != nil {
				t.Fatalf("configureShow: %v", err)
			}
			sm := showOut.(map[string]any)
			if got := sm["auth_present"]; got != false {
				t.Fatalf("show auth_present=%v, want false", got)
			}
			if got := sm["expires_at"]; got != "" {
				t.Fatalf("show expires_at=%v, want empty", got)
			}
			if got := sm["refresh_required"]; got != true {
				t.Fatalf("show refresh_required=%v, want true", got)
			}

			// configure list must also fail closed.
			listOut, err := configureList(newCtx(), nil)
			if err != nil {
				t.Fatalf("configureList: %v", err)
			}
			lm := listOut.(map[string]any)
			profiles := lm["profiles"].([]map[string]any)
			if len(profiles) != 1 {
				t.Fatalf("expected 1 profile, got %d", len(profiles))
			}
			p := profiles[0]
			if got := p["auth_present"]; got != false {
				t.Fatalf("list auth_present=%v, want false", got)
			}
			if got := p["expires_at"]; got != "" {
				t.Fatalf("list expires_at=%v, want empty", got)
			}
			if got := p["refresh_required"]; got != true {
				t.Fatalf("list refresh_required=%v, want true", got)
			}
		})
	}
}

// TestConfigureDynamicValidTokenButCorruptOtherFailClosed covers two scenarios
// where one cache is valid but the other is invalid, proving the combined
// status fails closed for both configure show and configure list:
//   - Console: complete valid cache but near-expiry with missing RefreshToken
//   - SSO: valid future TokenCache but corrupt (missing-credentials) STS cache
//
// Uses the production defaultAuthProviderFactory with real FileCaches.
func TestConfigureDynamicValidTokenButCorruptOtherFailClosed(t *testing.T) {
	cases := []struct {
		name  string
		mode  string
		setup func(dir string) (config.Config, string, error)
	}{
		{
			name: "console_near_expiry_missing_refresh_token",
			mode: config.AuthModeConsoleLogin,
			setup: func(dir string) (config.Config, string, error) {
				cacheDir := filepath.Join(dir, "login", "cache")
				if err := os.MkdirAll(cacheDir, 0755); err != nil {
					return config.Config{}, "", err
				}
				cache, err := console.NewFileCache(cacheDir)
				if err != nil {
					return config.Config{}, "", err
				}
				now := time.Now()
				stsJSON, err := json.Marshal(console.STSCredentials{
					AccessKeyID:     "AKLTnear",
					SecretAccessKey: "near-secret",
					SessionToken:    "near-token",
				})
				if err != nil {
					return config.Config{}, "", err
				}
				nearCache := console.LoginTokenCache{
					LoginSession: "near-session",
					AccessToken:  stsJSON,
					Scope:        console.Scope,
					ClientID:     console.ClientIDSameDevice,
					IssuedAt:     now.Add(-3599 * time.Second).UTC().Format(time.RFC3339),
					ExpiresIn:    3600,
					TokenType:    "sts",
				}
				data, err := json.Marshal(nearCache)
				if err != nil {
					return config.Config{}, "", err
				}
				if err := cache.WriteRaw("near-session", data); err != nil {
					return config.Config{}, "", err
				}
				cfg := config.Config{
					Version: 1,
					Profiles: map[string]config.Profile{
						"console": {
							Mode:         config.AuthModeConsoleLogin,
							Region:       "cn-beijing",
							Endpoint:     "https://tls-cn-beijing.volces.com",
							LoginSession: "near-session",
						},
					},
				}
				return cfg, "console", nil
			},
		},
		{
			name: "sso_valid_token_corrupt_sts",
			mode: config.AuthModeSSO,
			setup: func(dir string) (config.Config, string, error) {
				cache, err := sso.NewFileCache(filepath.Join(dir, "sso", "cache"))
				if err != nil {
					return config.Config{}, "", err
				}
				now := time.Now()
				if err := cache.WriteToken(&sso.TokenCache{
					StartURL:     "https://login.example.com/start",
					SessionName:  "valid-tok",
					AccessToken:  "valid-access-token",
					ExpiresAt:    now.Add(time.Hour).UTC().Format(time.RFC3339),
					ClientID:     "valid-client-id",
					ClientSecret: "valid-client-secret",
					Region:       "cn-beijing",
				}); err != nil {
					return config.Config{}, "", err
				}
				if err := cache.WriteSTS(&sso.STSCache{
					SessionName:  "valid-tok",
					AccountID:    "acct-1",
					RoleName:     "role-1",
					ProviderName: sso.ProviderName,
					ExpiresAt:    now.Add(time.Hour).UTC().Format(time.RFC3339),
				}); err != nil {
					return config.Config{}, "", err
				}
				// Precondition: token must be valid, STS must be invalid.
				tok, terr := cache.ReadToken("https://login.example.com/start", "valid-tok")
				if terr != nil {
					return config.Config{}, "", fmt.Errorf("ReadToken precondition: %w", terr)
				}
				tokStatus, terr := sso.InspectTokenCache(tok, "https://login.example.com/start", "valid-tok", "cn-beijing", now)
				if terr != nil || !tokStatus.Present {
					return config.Config{}, "", fmt.Errorf("precondition: token must be valid (Present=true), err=%v", terr)
				}
				sts, serr := cache.ReadSTS("valid-tok", "acct-1", "role-1")
				if serr != nil {
					return config.Config{}, "", fmt.Errorf("ReadSTS precondition: %w", serr)
				}
				stsStatus, serr := sso.InspectSTSCache(sts, "valid-tok", "acct-1", "role-1", now)
				if serr == nil || stsStatus.Present {
					return config.Config{}, "", fmt.Errorf("precondition: sts must be invalid (Present=false)")
				}
				cfg := config.Config{
					Version: 1,
					Profiles: map[string]config.Profile{
						"sso": {
							Mode:           config.AuthModeSSO,
							Region:         "cn-beijing",
							Endpoint:       "https://tls-cn-beijing.volces.com",
							SSOSessionName: "valid-tok",
							AccountID:      "acct-1",
							RoleName:       "role-1",
						},
					},
					SSOSessions: map[string]config.SSOSession{
						"valid-tok": {
							Name:     "valid-tok",
							StartURL: "https://login.example.com/start",
							Region:   "cn-beijing",
						},
					},
				}
				return cfg, "sso", nil
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearAuthTestEnv(t)
			dir := t.TempDir()
			cfg, profileName, serr := tc.setup(dir)
			if serr != nil {
				t.Fatalf("setup: %v", serr)
			}
			cfgPath := filepath.Join(dir, "config.json")
			if err := config.Save(cfg, cfgPath); err != nil {
				t.Fatalf("save config: %v", err)
			}

			// Assert the production factory constructs a real status reader.
			reader, rerr := dynamicAuthStatusReader(tc.mode, cfgPath, profileName, cfg, nil)
			if rerr != nil {
				t.Fatalf("dynamicAuthStatusReader: %v", rerr)
			}
			if reader == nil {
				t.Fatalf("expected non-nil status reader")
			}

			newCtx := func() *Context {
				c := newContext(&bytes.Buffer{}, &bytes.Buffer{}, output.FormatJSON, "", "")
				c.cfg = cfg
				c.cfgPath = cfgPath
				c.Profile = profileName
				c.authFactory = nil
				return c
			}

			// configure show must fail closed.
			showOut, err := configureShow(newCtx(), []string{"--profile", profileName})
			if err != nil {
				t.Fatalf("configureShow: %v", err)
			}
			sm := showOut.(map[string]any)
			if got := sm["auth_present"]; got != false {
				t.Fatalf("show auth_present=%v, want false", got)
			}
			if got := sm["expires_at"]; got != "" {
				t.Fatalf("show expires_at=%v, want empty", got)
			}
			if got := sm["refresh_required"]; got != true {
				t.Fatalf("show refresh_required=%v, want true", got)
			}

			// configure list must also fail closed.
			listOut, err := configureList(newCtx(), nil)
			if err != nil {
				t.Fatalf("configureList: %v", err)
			}
			lm := listOut.(map[string]any)
			profiles := lm["profiles"].([]map[string]any)
			if len(profiles) != 1 {
				t.Fatalf("expected 1 profile, got %d", len(profiles))
			}
			p := profiles[0]
			if got := p["auth_present"]; got != false {
				t.Fatalf("list auth_present=%v, want false", got)
			}
			if got := p["expires_at"]; got != "" {
				t.Fatalf("list expires_at=%v, want empty", got)
			}
			if got := p["refresh_required"]; got != true {
				t.Fatalf("list refresh_required=%v, want true", got)
			}
		})
	}
}

func TestConfigureSetWithoutModePreservesLegacyAKBehavior(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("VOLCLOG_CONFIG", path)

	// Without --mode, the legacy path must require --ak/--sk (or --cred-ref)
	// and set the profile mode to ak exactly as before.
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"configure", "set",
		"--profile", "legacy",
		"--region", "cn-beijing",
		"--endpoint", "https://tls-cn-beijing.volces.com",
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected non-zero exit without ak/sk; stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	out := stdout.String() + stderr.String()
	if !strings.Contains(out, "missing required fields: --ak --sk") {
		t.Fatalf("expected legacy ak/sk required error, got: %q", out)
	}

	// With ak/sk it must succeed and set mode=ak.
	stdout.Reset()
	stderr.Reset()
	code = Run([]string{
		"configure", "set",
		"--profile", "legacy",
		"--ak", "AKLTlegacy",
		"--sk", "sk-legacy",
		"--region", "cn-beijing",
		"--endpoint", "https://tls-cn-beijing.volces.com",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	loaded, _, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	p, ok := loaded.GetProfile("legacy")
	if !ok {
		t.Fatal("profile not found")
	}
	if p.Mode != config.AuthModeAK {
		t.Fatalf("mode=%q, want %q", p.Mode, config.AuthModeAK)
	}
	if p.AccessKeyID != "AKLTlegacy" || p.SecretAccessKey != "sk-legacy" {
		t.Fatalf("ak/sk not set: %+v", p)
	}
}

func TestConfigureSetExplicitModeMergesOnlyChangedFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("VOLCLOG_CONFIG", path)

	cfg := config.DefaultConfig()
	cfg.PutProfile("p1", config.Profile{
		AccessKeyID:     "AKLTold",
		SecretAccessKey: "sk-old",
		Region:          "cn-beijing",
		Endpoint:        "https://tls-cn-beijing.volces.com",
		Mode:            config.AuthModeAK,
	})
	if err := config.Save(cfg, path); err != nil {
		t.Fatalf("save config: %v", err)
	}

	// Explicit --mode ak with only --region supplied: existing ak/sk must be
	// preserved (merge), not overwritten with empty strings.
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"configure", "set",
		"--profile", "p1",
		"--mode", "ak",
		"--region", "ap-singapore-1",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	loaded, _, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	p, ok := loaded.GetProfile("p1")
	if !ok {
		t.Fatal("profile not found")
	}
	if p.AccessKeyID != "AKLTold" || p.SecretAccessKey != "sk-old" {
		t.Fatalf("existing ak/sk were overwritten: %+v", p)
	}
	if p.Region != "ap-singapore-1" {
		t.Fatalf("region=%q, want ap-singapore-1", p.Region)
	}
	if p.Endpoint != "https://tls-cn-beijing.volces.com" {
		t.Fatalf("endpoint changed unexpectedly: %q", p.Endpoint)
	}
	if p.Mode != config.AuthModeAK {
		t.Fatalf("mode=%q, want ak", p.Mode)
	}
}

func TestConfigureSetRamRoleARNRequiresSourceAndBinding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("VOLCLOG_CONFIG", path)

	runExpectFail := func(args ...string) string {
		t.Helper()
		var stdout, stderr bytes.Buffer
		code := Run(args, &stdout, &stderr)
		if code == 0 {
			t.Fatalf("expected failure for %v; stdout=%q", args, stdout.String())
		}
		return stdout.String() + stderr.String()
	}

	// Missing source (no ak/sk, no cred-ref).
	out := runExpectFail("configure", "set", "--profile", "r1",
		"--mode", "ramrolearn",
		"--account-id", "2100000000",
		"--role-name", "TLSAdminRole",
		"--region", "cn-beijing",
		"--endpoint", "https://tls-cn-beijing.volces.com")
	if !strings.Contains(out, "source") {
		t.Fatalf("expected source credential error, got: %q", out)
	}

	// Missing account-id / role-name binding.
	out = runExpectFail("configure", "set", "--profile", "r1",
		"--mode", "ramrolearn",
		"--ak", "AKLTsrc", "--sk", "sk-src",
		"--region", "cn-beijing",
		"--endpoint", "https://tls-cn-beijing.volces.com")
	if !strings.Contains(out, "account-id") || !strings.Contains(out, "role-name") {
		t.Fatalf("expected account-id/role-name required error, got: %q", out)
	}

	// All required supplied: must succeed.
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"configure", "set", "--profile", "r1",
		"--mode", "ramrolearn",
		"--ak", "AKLTsrc", "--sk", "sk-src",
		"--account-id", "2100000000",
		"--role-name", "TLSAdminRole",
		"--token", "sts-token",
		"--region", "cn-beijing",
		"--endpoint", "https://tls-cn-beijing.volces.com",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	loaded, _, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	p, ok := loaded.GetProfile("r1")
	if !ok {
		t.Fatal("profile not found")
	}
	if p.Mode != config.AuthModeRamRoleARN {
		t.Fatalf("mode=%q, want ramrolearn", p.Mode)
	}
	if p.AccountID != "2100000000" || p.RoleName != "TLSAdminRole" {
		t.Fatalf("binding not stored: %+v", p)
	}
	if p.SecurityToken != "sts-token" {
		t.Fatalf("source token not stored: %q", p.SecurityToken)
	}
}

func TestConfigureSetOIDCRequiresTokenFileAndRoleTRN(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("VOLCLOG_CONFIG", path)

	runExpectFail := func(args ...string) string {
		t.Helper()
		var stdout, stderr bytes.Buffer
		code := Run(args, &stdout, &stderr)
		if code == 0 {
			t.Fatalf("expected failure for %v; stdout=%q", args, stdout.String())
		}
		return stdout.String() + stderr.String()
	}

	// Missing oidc-token-file and role-trn.
	out := runExpectFail("configure", "set", "--profile", "o1",
		"--mode", "oidc",
		"--region", "cn-beijing",
		"--endpoint", "https://tls-cn-beijing.volces.com")
	if !strings.Contains(out, "oidc-token-file") || !strings.Contains(out, "role-trn") {
		t.Fatalf("expected oidc-token-file/role-trn required error, got: %q", out)
	}

	// All required supplied: must succeed.
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"configure", "set", "--profile", "o1",
		"--mode", "oidc",
		"--oidc-token-file", "/var/run/secrets/token",
		"--role-trn", "trn:iam::2100000000:role/TLSAdminRole",
		"--region", "cn-beijing",
		"--endpoint", "https://tls-cn-beijing.volces.com",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	loaded, _, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	p, ok := loaded.GetProfile("o1")
	if !ok {
		t.Fatal("profile not found")
	}
	if p.Mode != config.AuthModeOIDC {
		t.Fatalf("mode=%q, want oidc", p.Mode)
	}
	if p.OIDCTokenFile != "/var/run/secrets/token" {
		t.Fatalf("oidc-token-file=%q", p.OIDCTokenFile)
	}
	if p.RoleTRN != "trn:iam::2100000000:role/TLSAdminRole" {
		t.Fatalf("role-trn=%q", p.RoleTRN)
	}
}

func TestConfigureSetECSRoleRequiresRoleName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("VOLCLOG_CONFIG", path)

	// Missing role-name.
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"configure", "set", "--profile", "e1",
		"--mode", "ecsrole",
		"--region", "cn-beijing",
		"--endpoint", "https://tls-cn-beijing.volces.com",
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected failure without role-name; stdout=%q", stdout.String())
	}
	out := stdout.String() + stderr.String()
	if !strings.Contains(out, "role-name") {
		t.Fatalf("expected role-name required error, got: %q", out)
	}

	// With role-name: must succeed.
	stdout.Reset()
	stderr.Reset()
	code = Run([]string{
		"configure", "set", "--profile", "e1",
		"--mode", "ecsrole",
		"--role-name", "TLSAdminRole",
		"--region", "cn-beijing",
		"--endpoint", "https://tls-cn-beijing.volces.com",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	loaded, _, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	p, ok := loaded.GetProfile("e1")
	if !ok {
		t.Fatal("profile not found")
	}
	if p.Mode != config.AuthModeECSRole {
		t.Fatalf("mode=%q, want ecsrole", p.Mode)
	}
	if p.RoleName != "TLSAdminRole" {
		t.Fatalf("role-name=%q", p.RoleName)
	}
}

func TestConfigureSetExplicitAKValidatesMergedProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("VOLCLOG_CONFIG", path)

	cfg := config.DefaultConfig()
	cfg.PutProfile("a1", config.Profile{
		Region:   "cn-beijing",
		Endpoint: "https://tls-cn-beijing.volces.com",
		Mode:     config.AuthModeAK,
	})
	if err := config.Save(cfg, path); err != nil {
		t.Fatalf("save config: %v", err)
	}

	// Explicit --mode ak without ak/sk and without cred-ref must fail validation
	// on the merged profile (which has no source credentials).
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"configure", "set", "--profile", "a1",
		"--mode", "ak",
		"--region", "ap-singapore-1",
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected failure without ak/sk; stdout=%q", stdout.String())
	}
	out := stdout.String() + stderr.String()
	if !strings.Contains(out, "--ak") || !strings.Contains(out, "--sk") {
		t.Fatalf("expected ak/sk required error, got: %q", out)
	}

	// Now supply ak/sk: merged profile validates and succeeds.
	stdout.Reset()
	stderr.Reset()
	code = Run([]string{
		"configure", "set", "--profile", "a1",
		"--mode", "ak",
		"--ak", "AKLTnew", "--sk", "sk-new",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	loaded, _, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	p, ok := loaded.GetProfile("a1")
	if !ok {
		t.Fatal("profile not found")
	}
	if p.AccessKeyID != "AKLTnew" || p.SecretAccessKey != "sk-new" {
		t.Fatalf("ak/sk not set: %+v", p)
	}
	// region/endpoint from the existing profile must be preserved.
	if p.Region != "cn-beijing" || p.Endpoint != "https://tls-cn-beijing.volces.com" {
		t.Fatalf("region/endpoint not preserved: region=%q endpoint=%q", p.Region, p.Endpoint)
	}
}

func TestConfigureSetRejectsInteractiveModesWithoutChangingProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("VOLCLOG_CONFIG", path)

	cfg := config.DefaultConfig()
	cfg.PutProfile("s1", config.Profile{
		AccessKeyID:     "AKLTkeep",
		SecretAccessKey: "sk-keep",
		Region:          "cn-beijing",
		Endpoint:        "https://tls-cn-beijing.volces.com",
		Mode:            config.AuthModeAK,
	})
	if err := config.Save(cfg, path); err != nil {
		t.Fatalf("save config: %v", err)
	}

	for _, m := range []string{"sso", "console-login"} {
		var stdout, stderr bytes.Buffer
		code := Run([]string{
			"configure", "set", "--profile", "s1",
			"--mode", m,
			"--region", "ap-singapore-1",
		}, &stdout, &stderr)
		if code == 0 {
			t.Fatalf("expected failure for mode=%q; stdout=%q", m, stdout.String())
		}
		out := stdout.String() + stderr.String()
		if !strings.Contains(out, "dedicated") {
			t.Fatalf("mode=%q: expected hint about dedicated flow, got: %q", m, out)
		}
	}

	// Profile must be unchanged after the rejected attempts.
	loaded, _, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	p, ok := loaded.GetProfile("s1")
	if !ok {
		t.Fatal("profile not found")
	}
	if p.Mode != config.AuthModeAK {
		t.Fatalf("mode changed to %q, want ak (unchanged)", p.Mode)
	}
	if p.Region != "cn-beijing" {
		t.Fatalf("region changed to %q, want cn-beijing (unchanged)", p.Region)
	}
	if p.AccessKeyID != "AKLTkeep" {
		t.Fatalf("ak changed, profile was modified despite rejection")
	}
}

func TestConfigureSetDisableSSLTriState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("VOLCLOG_CONFIG", path)

	// Seed a profile with DisableSSL=true.
	cfg := config.DefaultConfig()
	cfg.PutProfile("d1", config.Profile{
		AccessKeyID:     "AKLTx",
		SecretAccessKey: "skx",
		Region:          "cn-beijing",
		Endpoint:        "https://tls-cn-beijing.volces.com",
		Mode:            config.AuthModeAK,
		DisableSSL:      true,
	})
	if err := config.Save(cfg, path); err != nil {
		t.Fatalf("save config: %v", err)
	}

	loadProfile := func() config.Profile {
		t.Helper()
		loaded, _, err := config.Load()
		if err != nil {
			t.Fatalf("load config: %v", err)
		}
		p, ok := loaded.GetProfile("d1")
		if !ok {
			t.Fatal("profile not found")
		}
		return p
	}

	// Omitted --disable-ssl: existing value (true) must be preserved.
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"configure", "set", "--profile", "d1",
		"--mode", "ak",
		"--region", "ap-singapore-1",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if p := loadProfile(); !p.DisableSSL {
		t.Fatalf("omitted disable-ssl reset to false, want true (preserved)")
	}

	// --disable-ssl=false: must explicitly set to false (differs from omitted).
	stdout.Reset()
	stderr.Reset()
	code = Run([]string{
		"configure", "set", "--profile", "d1",
		"--mode", "ak",
		"--disable-ssl=false",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if p := loadProfile(); p.DisableSSL {
		t.Fatalf("--disable-ssl=false did not set false; got true")
	}

	// --disable-ssl (no value): must set to true.
	stdout.Reset()
	stderr.Reset()
	code = Run([]string{
		"configure", "set", "--profile", "d1",
		"--mode", "ak",
		"--disable-ssl",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if p := loadProfile(); !p.DisableSSL {
		t.Fatalf("--disable-ssl did not set true; got false")
	}
}

func TestConfigureSetModeChangePreservesDormantFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("VOLCLOG_CONFIG", path)

	cfg := config.DefaultConfig()
	cfg.PutProfile("m1", config.Profile{
		AccessKeyID:     "AKLTsrc",
		SecretAccessKey: "sk-src",
		Region:          "cn-beijing",
		Endpoint:        "https://tls-cn-beijing.volces.com",
		Mode:            config.AuthModeRamRoleARN,
		AccountID:       "2100000000",
		RoleName:        "TLSAdminRole",
		OIDCTokenFile:   "/var/run/secrets/token",
		RoleTRN:         "trn:iam::2100000000:role/TLSAdminRole",
	})
	if err := config.Save(cfg, path); err != nil {
		t.Fatalf("save config: %v", err)
	}

	// Switch to ak mode; dormant ramrolearn/oidc fields must be preserved.
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"configure", "set", "--profile", "m1",
		"--mode", "ak",
		"--ak", "AKLTnew", "--sk", "sk-new",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	loaded, _, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	p, ok := loaded.GetProfile("m1")
	if !ok {
		t.Fatal("profile not found")
	}
	if p.Mode != config.AuthModeAK {
		t.Fatalf("mode=%q, want ak", p.Mode)
	}
	if p.AccountID != "2100000000" {
		t.Fatalf("dormant account-id cleared: %q", p.AccountID)
	}
	if p.RoleName != "TLSAdminRole" {
		t.Fatalf("dormant role-name cleared: %q", p.RoleName)
	}
	if p.OIDCTokenFile != "/var/run/secrets/token" {
		t.Fatalf("dormant oidc-token-file cleared: %q", p.OIDCTokenFile)
	}
	if p.RoleTRN != "trn:iam::2100000000:role/TLSAdminRole" {
		t.Fatalf("dormant role-trn cleared: %q", p.RoleTRN)
	}
}

func TestConfigureSetExplicitModeRotatesExistingCredRef(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("VOLCLOG_CONFIG", path)

	cfg := config.DefaultConfig()
	cfg.PutProfile("p1", config.Profile{
		Region:   "cn-beijing",
		Endpoint: "https://tls-cn-beijing.volces.com",
		Mode:     config.AuthModeAK,
		CredRef:  "ma-abc",
	})
	cfg.PutCred("ma-abc", config.Credential{
		AccessKeyID:     "AKLTold",
		SecretAccessKey: "sk-old",
	})
	if err := config.Save(cfg, path); err != nil {
		t.Fatalf("save config: %v", err)
	}

	// Rotate the stored credential by supplying new AK/SK while keeping the
	// existing cred-ref. The stored credential must be replaced, inline fields
	// must remain clear, and the output must mask the stored AK.
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"configure", "set", "--profile", "p1",
		"--mode", "ak",
		"--ak", "AKLTnew", "--sk", "sk-new",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	loaded, _, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	p, ok := loaded.GetProfile("p1")
	if !ok {
		t.Fatal("profile not found")
	}
	if p.AccessKeyID != "" || p.SecretAccessKey != "" {
		t.Fatalf("inline creds must be cleared when cred-ref is set: %+v", p)
	}
	cred, ok := loaded.GetCred("ma-abc")
	if !ok {
		t.Fatal("stored credential not found")
	}
	if cred.AccessKeyID != "AKLTnew" || cred.SecretAccessKey != "sk-new" {
		t.Fatalf("stored credential not rotated: %+v", cred)
	}

	// Output must mask the stored AK exactly and never expose raw AK/SK.
	var m map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &m); err != nil {
		t.Fatalf("invalid json output: %v; stdout=%q", err, stdout.String())
	}
	wantMasked := config.MaskAK("AKLTnew")
	if got, _ := m["access_key_id"].(string); got != wantMasked {
		t.Fatalf("access_key_id=%q, want masked %q", got, wantMasked)
	}
	combined := stdout.String() + stderr.String()
	for _, raw := range []string{"AKLTnew", "sk-new", "AKLTold", "sk-old"} {
		if strings.Contains(combined, raw) {
			t.Fatalf("raw credential %q leaked in output: %q", raw, combined)
		}
	}
}

func TestConfigureSetExplicitModePartialCredRefUpdate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("VOLCLOG_CONFIG", path)

	cfg := config.DefaultConfig()
	cfg.PutProfile("p1", config.Profile{
		Region:   "cn-beijing",
		Endpoint: "https://tls-cn-beijing.volces.com",
		Mode:     config.AuthModeAK,
		CredRef:  "ma-abc",
	})
	cfg.PutCred("ma-abc", config.Credential{
		AccessKeyID:     "AKLTkeep",
		SecretAccessKey: "sk-keep",
	})
	if err := config.Save(cfg, path); err != nil {
		t.Fatalf("save config: %v", err)
	}

	// Supply only a new SK; the stored AK must be preserved (merge).
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"configure", "set", "--profile", "p1",
		"--mode", "ak",
		"--sk", "sk-rotated",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	loaded, _, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cred, ok := loaded.GetCred("ma-abc")
	if !ok {
		t.Fatal("stored credential not found")
	}
	if cred.AccessKeyID != "AKLTkeep" {
		t.Fatalf("stored AK was not preserved during partial update: %q", cred.AccessKeyID)
	}
	if cred.SecretAccessKey != "sk-rotated" {
		t.Fatalf("stored SK was not updated: %q", cred.SecretAccessKey)
	}
}

func TestConfigureSetExplicitModeBrokenCredRefFailsWithoutMutation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("VOLCLOG_CONFIG", path)

	cfg := config.DefaultConfig()
	cfg.PutProfile("p1", config.Profile{
		Region:   "cn-beijing",
		Endpoint: "https://tls-cn-beijing.volces.com",
		Mode:     config.AuthModeAK,
		CredRef:  "ma-missing",
	})
	if err := config.Save(cfg, path); err != nil {
		t.Fatalf("save config: %v", err)
	}

	// Profile references a cred-ref that does not exist in the store. The call
	// must fail and must not mutate the on-disk profile.
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"configure", "set", "--profile", "p1",
		"--mode", "ak",
		"--region", "ap-singapore-1",
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected failure for broken cred-ref; stdout=%q", stdout.String())
	}
	out := stdout.String() + stderr.String()
	if !strings.Contains(out, "credential not found") {
		t.Fatalf("expected credential not found error, got: %q", out)
	}

	loaded, _, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	p, ok := loaded.GetProfile("p1")
	if !ok {
		t.Fatal("profile not found")
	}
	if p.Region != "cn-beijing" {
		t.Fatalf("profile was mutated despite failure: region=%q", p.Region)
	}
	if p.CredRef != "ma-missing" {
		t.Fatalf("profile cred-ref was mutated: %q", p.CredRef)
	}
}

func TestConfigureSetModeFlagValueNamedModeDoesNotSelectExplicitPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("VOLCLOG_CONFIG", path)

	// Seed a profile literally named "--mode" with a security token so we can
	// distinguish legacy (clears token) from explicit (preserves token).
	cfg := config.DefaultConfig()
	cfg.PutProfile("--mode", config.Profile{
		AccessKeyID:     "AKLTseed",
		SecretAccessKey: "sk-seed",
		SecurityToken:   "canary-token",
		Region:          "cn-beijing",
		Endpoint:        "https://tls-cn-beijing.volces.com",
		Mode:            config.AuthModeAK,
	})
	if err := config.Save(cfg, path); err != nil {
		t.Fatalf("save config: %v", err)
	}

	// "--mode" is the value of --profile, not a flag. Must route to legacy path,
	// which clears SecurityToken when --token is not supplied.
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"configure", "set", "--profile", "--mode",
		"--ak", "AKLTnew", "--sk", "sk-new",
		"--region", "ap-singapore-1",
		"--endpoint", "https://tls-ap-singapore-1.volces.com",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	loaded, _, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	p, ok := loaded.GetProfile("--mode")
	if !ok {
		t.Fatal("profile --mode not found")
	}
	if p.SecurityToken != "" {
		t.Fatalf("SecurityToken was preserved (explicit path), expected legacy clear: %q", p.SecurityToken)
	}
}

func TestConfigureSetExplicitModeEqualsForm(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("VOLCLOG_CONFIG", path)

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"configure", "set", "--profile", "p1",
		"--mode=ak",
		"--ak", "AKLTx", "--sk", "skx",
		"--region", "cn-beijing",
		"--endpoint", "https://tls-cn-beijing.volces.com",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	loaded, _, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	p, ok := loaded.GetProfile("p1")
	if !ok {
		t.Fatal("profile not found")
	}
	if p.Mode != config.AuthModeAK {
		t.Fatalf("mode=%q, want ak", p.Mode)
	}
}

func TestConfigureSetExplicitModeOutputFieldSetAndNoSecretCanary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("VOLCLOG_CONFIG", path)

	const (
		akCanary    = "AKLTcanary123"
		skCanary    = "sk-canary-super-secret"
		tokenCanary = "sts-canary-token"
	)

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"configure", "set", "--profile", "p1",
		"--mode", "ak",
		"--ak", akCanary, "--sk", skCanary,
		"--token", tokenCanary,
		"--region", "cn-beijing",
		"--endpoint", "https://tls-cn-beijing.volces.com",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	// Output must contain exactly the expected top-level key set with exact values.
	var m map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &m); err != nil {
		t.Fatalf("invalid json output: %v; stdout=%q", err, stdout.String())
	}
	wantKeys := map[string]struct{}{
		"profile":       {},
		"region":        {},
		"endpoint":      {},
		"cred_ref":      {},
		"access_key_id": {},
	}
	if len(m) != len(wantKeys) {
		t.Fatalf("output has %d keys, want %d: %+v", len(m), len(wantKeys), m)
	}
	for k := range m {
		if _, ok := wantKeys[k]; !ok {
			t.Fatalf("output has unexpected key %q: %+v", k, m)
		}
	}
	if got, _ := m["profile"].(string); got != "p1" {
		t.Fatalf("profile=%q, want p1", got)
	}
	if got, _ := m["region"].(string); got != "cn-beijing" {
		t.Fatalf("region=%q, want cn-beijing", got)
	}
	if got, _ := m["endpoint"].(string); got != "https://tls-cn-beijing.volces.com" {
		t.Fatalf("endpoint=%q", got)
	}
	if got, _ := m["cred_ref"].(string); got != "" {
		t.Fatalf("cred_ref=%q, want empty", got)
	}
	if got, _ := m["access_key_id"].(string); got != config.MaskAK(akCanary) {
		t.Fatalf("access_key_id=%q, want masked %q", got, config.MaskAK(akCanary))
	}

	// Raw AK, SK, and source SessionToken canaries must never appear in output.
	combined := stdout.String() + stderr.String()
	for _, raw := range []string{akCanary, skCanary, tokenCanary} {
		if strings.Contains(combined, raw) {
			t.Fatalf("canary %q leaked in output: %q", raw, combined)
		}
	}
}

func TestConfigureSetExplicitModeEmptyEqualsFormRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("VOLCLOG_CONFIG", path)

	cfg := config.DefaultConfig()
	cfg.PutProfile("p1", config.Profile{
		AccessKeyID:     "AKLTkeep",
		SecretAccessKey: "sk-keep",
		Region:          "cn-beijing",
		Endpoint:        "https://tls-cn-beijing.volces.com",
		Mode:            config.AuthModeAK,
	})
	if err := config.Save(cfg, path); err != nil {
		t.Fatalf("save config: %v", err)
	}

	// --mode= with empty value must be rejected, not normalized to ak.
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"configure", "set", "--profile", "p1",
		"--mode=",
		"--region", "ap-singapore-1",
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected failure for empty --mode=; stdout=%q", stdout.String())
	}

	// Profile must be unchanged (no mutation).
	loaded, _, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	p, ok := loaded.GetProfile("p1")
	if !ok {
		t.Fatal("profile not found")
	}
	if p.Region != "cn-beijing" {
		t.Fatalf("profile mutated despite rejection: region=%q", p.Region)
	}
	if p.Mode != config.AuthModeAK {
		t.Fatalf("mode mutated: %q", p.Mode)
	}
}

func TestConfigureSetExplicitModeEmptySpaceFormRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("VOLCLOG_CONFIG", path)

	cfg := config.DefaultConfig()
	cfg.PutProfile("p1", config.Profile{
		AccessKeyID:     "AKLTkeep",
		SecretAccessKey: "sk-keep",
		Region:          "cn-beijing",
		Endpoint:        "https://tls-cn-beijing.volces.com",
		Mode:            config.AuthModeAK,
	})
	if err := config.Save(cfg, path); err != nil {
		t.Fatalf("save config: %v", err)
	}

	// --mode '' (empty value) must be rejected, not normalized to ak.
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"configure", "set", "--profile", "p1",
		"--mode", "",
		"--region", "ap-singapore-1",
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected failure for empty --mode ''; stdout=%q", stdout.String())
	}

	// Profile must be unchanged (no mutation).
	loaded, _, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	p, ok := loaded.GetProfile("p1")
	if !ok {
		t.Fatal("profile not found")
	}
	if p.Region != "cn-beijing" {
		t.Fatalf("profile mutated despite rejection: region=%q", p.Region)
	}
	if p.Mode != config.AuthModeAK {
		t.Fatalf("mode mutated: %q", p.Mode)
	}
}

func TestConfigureSetExplicitModeCredRefEmptyAKFailsWithoutMutation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("VOLCLOG_CONFIG", path)

	cfg := config.DefaultConfig()
	cfg.PutProfile("p1", config.Profile{
		Region:   "cn-beijing",
		Endpoint: "https://tls-cn-beijing.volces.com",
		Mode:     config.AuthModeAK,
		CredRef:  "ma-abc",
	})
	cfg.PutCred("ma-abc", config.Credential{
		AccessKeyID:     "AKLTkeep",
		SecretAccessKey: "sk-keep",
	})
	if err := config.Save(cfg, path); err != nil {
		t.Fatalf("save config: %v", err)
	}

	// Supplying --ak '' must not corrupt the stored credential with an empty AK.
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"configure", "set", "--profile", "p1",
		"--mode", "ak",
		"--ak", "",
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected failure for empty --ak with cred-ref; stdout=%q", stdout.String())
	}

	// Stored credential must be unchanged.
	loaded, _, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cred, ok := loaded.GetCred("ma-abc")
	if !ok {
		t.Fatal("stored credential not found")
	}
	if cred.AccessKeyID != "AKLTkeep" || cred.SecretAccessKey != "sk-keep" {
		t.Fatalf("stored credential was corrupted: %+v", cred)
	}
}

func TestConfigureSetExplicitModeCredRefEmptySKFailsWithoutMutation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("VOLCLOG_CONFIG", path)

	cfg := config.DefaultConfig()
	cfg.PutProfile("p1", config.Profile{
		Region:   "cn-beijing",
		Endpoint: "https://tls-cn-beijing.volces.com",
		Mode:     config.AuthModeAK,
		CredRef:  "ma-abc",
	})
	cfg.PutCred("ma-abc", config.Credential{
		AccessKeyID:     "AKLTkeep",
		SecretAccessKey: "sk-keep",
	})
	if err := config.Save(cfg, path); err != nil {
		t.Fatalf("save config: %v", err)
	}

	// Supplying --sk '' must not corrupt the stored credential with an empty SK.
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"configure", "set", "--profile", "p1",
		"--mode", "ak",
		"--sk", "",
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected failure for empty --sk with cred-ref; stdout=%q", stdout.String())
	}

	// Stored credential must be unchanged.
	loaded, _, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cred, ok := loaded.GetCred("ma-abc")
	if !ok {
		t.Fatal("stored credential not found")
	}
	if cred.AccessKeyID != "AKLTkeep" || cred.SecretAccessKey != "sk-keep" {
		t.Fatalf("stored credential was corrupted: %+v", cred)
	}
}

func TestConfigureSetExplicitModePreExistingBrokenCredRefFailsWithoutMutation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("VOLCLOG_CONFIG", path)

	// A pre-existing stored credential with an empty AK (broken/incomplete).
	cfg := config.DefaultConfig()
	cfg.PutProfile("p1", config.Profile{
		Region:   "cn-beijing",
		Endpoint: "https://tls-cn-beijing.volces.com",
		Mode:     config.AuthModeAK,
		CredRef:  "ma-broken",
	})
	cfg.PutCred("ma-broken", config.Credential{
		AccessKeyID:     "",
		SecretAccessKey: "sk-only",
	})
	if err := config.Save(cfg, path); err != nil {
		t.Fatalf("save config: %v", err)
	}

	// Touching the profile must fail because the referenced credential is broken.
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"configure", "set", "--profile", "p1",
		"--mode", "ak",
		"--region", "ap-singapore-1",
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected failure for broken pre-existing cred-ref; stdout=%q", stdout.String())
	}

	// Stored credential and profile must be unchanged.
	loaded, _, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cred, ok := loaded.GetCred("ma-broken")
	if !ok {
		t.Fatal("stored credential not found")
	}
	if cred.AccessKeyID != "" || cred.SecretAccessKey != "sk-only" {
		t.Fatalf("broken credential was modified: %+v", cred)
	}
	p, ok := loaded.GetProfile("p1")
	if !ok {
		t.Fatal("profile not found")
	}
	if p.Region != "cn-beijing" {
		t.Fatalf("profile mutated despite rejection: region=%q", p.Region)
	}
}

func TestConfigureSetOIDCPreservesMissingDormantCredRef(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("VOLCLOG_CONFIG", path)

	// Profile carries a dormant cred-ref whose credential does not exist in the
	// store. Switching to OIDC must succeed: the dormant cred-ref is not used by
	// OIDC and must not block the update or be mutated.
	cfg := config.DefaultConfig()
	cfg.PutProfile("p1", config.Profile{
		AccessKeyID:     "AKLTold",
		SecretAccessKey: "sk-old",
		Region:          "cn-beijing",
		Endpoint:        "https://tls-cn-beijing.volces.com",
		Mode:            config.AuthModeAK,
		CredRef:         "ma-dormant-missing",
	})
	if err := config.Save(cfg, path); err != nil {
		t.Fatalf("save config: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"configure", "set", "--profile", "p1",
		"--mode", "oidc",
		"--oidc-token-file", "/var/run/secrets/token",
		"--role-trn", "trn:iam::2100000000:role/TLSAdminRole",
		"--region", "ap-singapore-1",
		"--endpoint", "https://tls-ap-singapore-1.volces.com",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	loaded, _, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	p, ok := loaded.GetProfile("p1")
	if !ok {
		t.Fatal("profile not found")
	}
	if p.Mode != config.AuthModeOIDC {
		t.Fatalf("mode=%q, want oidc", p.Mode)
	}
	// Dormant cred-ref must be preserved untouched.
	if p.CredRef != "ma-dormant-missing" {
		t.Fatalf("dormant cred-ref was mutated: %q", p.CredRef)
	}
	// The missing credential must not have been created in the store.
	if _, ok := loaded.GetCred("ma-dormant-missing"); ok {
		t.Fatal("missing dormant credential was unexpectedly created")
	}
}

func TestConfigureSetECSRolePreservesBrokenDormantCredRef(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("VOLCLOG_CONFIG", path)

	// Profile carries a dormant cred-ref whose stored credential is broken
	// (empty AK). Switching to ECS role must succeed: the dormant cred-ref is
	// not used by ECS and the broken credential must not be touched.
	cfg := config.DefaultConfig()
	cfg.PutProfile("p1", config.Profile{
		AccessKeyID:     "AKLTold",
		SecretAccessKey: "sk-old",
		Region:          "cn-beijing",
		Endpoint:        "https://tls-cn-beijing.volces.com",
		Mode:            config.AuthModeAK,
		CredRef:         "ma-dormant-broken",
	})
	cfg.PutCred("ma-dormant-broken", config.Credential{
		AccessKeyID:     "",
		SecretAccessKey: "sk-only",
	})
	if err := config.Save(cfg, path); err != nil {
		t.Fatalf("save config: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"configure", "set", "--profile", "p1",
		"--mode", "ecsrole",
		"--role-name", "TLSAdminRole",
		"--region", "ap-singapore-1",
		"--endpoint", "https://tls-ap-singapore-1.volces.com",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	loaded, _, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	p, ok := loaded.GetProfile("p1")
	if !ok {
		t.Fatal("profile not found")
	}
	if p.Mode != config.AuthModeECSRole {
		t.Fatalf("mode=%q, want ecsrole", p.Mode)
	}
	// Dormant cred-ref must be preserved untouched.
	if p.CredRef != "ma-dormant-broken" {
		t.Fatalf("dormant cred-ref was mutated: %q", p.CredRef)
	}
	// The broken credential must remain unchanged in the store.
	cred, ok := loaded.GetCred("ma-dormant-broken")
	if !ok {
		t.Fatal("dormant broken credential was unexpectedly removed")
	}
	if cred.AccessKeyID != "" || cred.SecretAccessKey != "sk-only" {
		t.Fatalf("broken dormant credential was mutated: %+v", cred)
	}
}

// TestConfigureShowAndListWorkloadReady covers show+list for RAM/OIDC/ECS with
// ready true/false, asserting consistent fields and no secret leakage.
func TestConfigureShowAndListWorkloadReady(t *testing.T) {
	clearAuthTestEnv(t)
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("TOKEN"), 0600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	cases := []struct {
		mode       string
		provider   string
		sourceType string
		ready      bool
		profile    config.Profile
	}{
		{config.AuthModeRamRoleARN, "ramrolearn", "profile_inline", true, config.Profile{Mode: config.AuthModeRamRoleARN, RoleName: "SECRET-RAM-ROLE", AccountID: "1", AccessKeyID: "SECRET-AK", SecretAccessKey: "SECRET-SK"}},
		{config.AuthModeRamRoleARN, "ramrolearn", "profile_inline", false, config.Profile{Mode: config.AuthModeRamRoleARN, RoleName: "SECRET-RAM-ROLE"}},
		{config.AuthModeOIDC, "oidc", "token_file", true, config.Profile{Mode: config.AuthModeOIDC, RoleTRN: "trn:iam::1:role/SECRET-OIDC-ROLE", OIDCTokenFile: tokenFile}},
		{config.AuthModeOIDC, "oidc", "token_file", false, config.Profile{Mode: config.AuthModeOIDC, RoleTRN: "trn:iam::1:role/SECRET-OIDC-ROLE", OIDCTokenFile: "/nonexistent"}},
		{config.AuthModeECSRole, "ecsrole", "instance_metadata", true, config.Profile{Mode: config.AuthModeECSRole, RoleName: "SECRET-ECS-ROLE"}},
		{config.AuthModeECSRole, "ecsrole", "instance_metadata", false, config.Profile{Mode: config.AuthModeECSRole}},
	}
	canaries := []string{"SECRET-RAM-ROLE", "SECRET-ECS-ROLE", "trn:iam::1:role/SECRET-OIDC-ROLE", tokenFile, "SECRET-AK", "SECRET-SK", "TOKEN"}
	assertWorkloadFields := func(t *testing.T, m map[string]any, tc struct {
		mode       string
		provider   string
		sourceType string
		ready      bool
		profile    config.Profile
	}) {
		t.Helper()
		if m["auth_mode"] != tc.mode {
			t.Fatalf("auth_mode=%v, want %s", m["auth_mode"], tc.mode)
		}
		if m["provider"] != tc.provider {
			t.Fatalf("provider=%v, want %s", m["provider"], tc.provider)
		}
		if m["source"] != tc.sourceType {
			t.Fatalf("source=%v, want %s", m["source"], tc.sourceType)
		}
		if m["auth_present"] != false {
			t.Fatalf("auth_present=%v, want false", m["auth_present"])
		}
		if m["expires_at"] != "" {
			t.Fatalf("expires_at=%v, want empty", m["expires_at"])
		}
		if m["on_demand"] != true {
			t.Fatalf("on_demand=%v, want true", m["on_demand"])
		}
		if m["memory_only"] != true {
			t.Fatalf("memory_only=%v, want true", m["memory_only"])
		}
		got, ok := m["source_ready"].(bool)
		if !ok {
			t.Fatalf("source_ready is not bool: %T", m["source_ready"])
		}
		if got != tc.ready {
			t.Fatalf("source_ready=%v, want %v", got, tc.ready)
		}
		if _, ok := m["refresh_required"]; ok {
			t.Fatalf("must not contain refresh_required")
		}
	}
	for _, tc := range cases {
		t.Run(tc.mode+"/show/ready="+strconv.FormatBool(tc.ready), func(t *testing.T) {
			cfg := config.Config{Version: 1, Profiles: map[string]config.Profile{"w": tc.profile}}
			ctx := newTestContext(t, cfg, "/tmp/cfg.json")
			ctx.Profile = "w"
			ctx.authFactory = &fakeAuthFactory{}
			out, err := configureShow(ctx, []string{"--profile", "w"})
			if err != nil {
				t.Fatalf("configureShow: %v", err)
			}
			m := out.(map[string]any)
			assertWorkloadFields(t, m, tc)
			raw, jerr := json.Marshal(m)
			if jerr != nil {
				t.Fatalf("marshal: %v", jerr)
			}
			for _, c := range canaries {
				if strings.Contains(string(raw), c) {
					t.Fatalf("show leaked canary %q", c)
				}
			}
		})
		t.Run(tc.mode+"/list/ready="+strconv.FormatBool(tc.ready), func(t *testing.T) {
			cfg := config.Config{Version: 1, Profiles: map[string]config.Profile{"w": tc.profile}}
			ctx := newTestContext(t, cfg, "/tmp/cfg.json")
			ctx.authFactory = &fakeAuthFactory{}
			out, err := configureList(ctx, nil)
			if err != nil {
				t.Fatalf("configureList: %v", err)
			}
			// Parse list output and find the workload profile row.
			listOut := out.(map[string]any)
			profiles, ok := listOut["profiles"].([]map[string]any)
			if !ok {
				t.Fatalf("profiles is not []map[string]any: %T", listOut["profiles"])
			}
			if len(profiles) == 0 {
				t.Fatal("expected at least one profile in list output")
			}
			var row map[string]any
			for _, p := range profiles {
				if p["name"] == "w" || p["profile"] == "w" {
					row = p
					break
				}
			}
			if row == nil {
				t.Fatalf("workload profile row not found in list output: %v", profiles)
			}
			assertWorkloadFields(t, row, tc)
			raw, jerr := json.Marshal(out)
			if jerr != nil {
				t.Fatalf("marshal: %v", jerr)
			}
			for _, c := range canaries {
				if strings.Contains(string(raw), c) {
					t.Fatalf("list leaked canary %q", c)
				}
			}
		})
	}
}

func TestConfigureMarksRAMAndOIDCInsecureWhenDisableSSL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("VOLCLOG_CONFIG", path)

	cases := []struct {
		name      string
		mode      string
		extraArgs []string
	}{
		{"ram", config.AuthModeRamRoleARN, []string{"--account-id", "123456789012", "--role-name", "r", "--ak", "RAM-AK-CANARY", "--sk", "RAM-SK-CANARY"}},
		{"oidc", config.AuthModeOIDC, []string{"--role-trn", "trn:iam::1:role/SECRET-TRN-CANARY", "--oidc-token-file", "/SECRET/PATH/CANARY/token"}},
	}
	for _, tc := range cases {
		t.Run(tc.name+"/set", func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			args := append([]string{"configure", "set", "--profile", "p-" + tc.name, "--mode", tc.mode, "--region", "cn-beijing", "--endpoint", "https://tls-cn-beijing.volces.com", "--disable-ssl=true"}, tc.extraArgs...)
			code := Run(args, &stdout, &stderr)
			if code != 0 {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			var m map[string]any
			if err := json.Unmarshal(stdout.Bytes(), &m); err != nil {
				t.Fatalf("invalid json: %v; stdout=%q", err, stdout.String())
			}
			if m["disable_ssl"] != true {
				t.Fatalf("disable_ssl=%v, want true", m["disable_ssl"])
			}
			if m["insecure"] != true {
				t.Fatalf("insecure=%v, want true", m["insecure"])
			}
			if m["warning"] != insecureSSLWarning {
				t.Fatalf("warning=%v, want %q", m["warning"], insecureSSLWarning)
			}
			for _, secret := range []string{"RAM-AK-CANARY", "RAM-SK-CANARY", "SECRET-TRN-CANARY", "SECRET/PATH/CANARY"} {
				if strings.Contains(stdout.String(), secret) {
					t.Fatalf("output leaked secret %q", secret)
				}
			}
		})

		t.Run(tc.name+"/show", func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run([]string{"configure", "show", "--profile", "p-" + tc.name}, &stdout, &stderr)
			if code != 0 {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			var m map[string]any
			if err := json.Unmarshal(stdout.Bytes(), &m); err != nil {
				t.Fatalf("invalid json: %v; stdout=%q", err, stdout.String())
			}
			if m["disable_ssl"] != true {
				t.Fatalf("disable_ssl=%v, want true", m["disable_ssl"])
			}
			if m["insecure"] != true {
				t.Fatalf("insecure=%v, want true", m["insecure"])
			}
			if m["warning"] != insecureSSLWarning {
				t.Fatalf("warning=%v, want %q", m["warning"], insecureSSLWarning)
			}
			for _, secret := range []string{"RAM-AK-CANARY", "RAM-SK-CANARY", "SECRET-TRN-CANARY", "SECRET/PATH/CANARY"} {
				if strings.Contains(stdout.String(), secret) {
					t.Fatalf("show output leaked secret %q", secret)
				}
			}
		})

		t.Run(tc.name+"/list", func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run([]string{"configure", "list"}, &stdout, &stderr)
			if code != 0 {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			var m map[string]any
			if err := json.Unmarshal(stdout.Bytes(), &m); err != nil {
				t.Fatalf("invalid json: %v; stdout=%q", err, stdout.String())
			}
			profiles, _ := m["profiles"].([]any)
			var row map[string]any
			for _, p := range profiles {
				pm, _ := p.(map[string]any)
				if pm["profile"] == "p-"+tc.name {
					row = pm
					break
				}
			}
			if row == nil {
				t.Fatalf("profile p-%s not found in list", tc.name)
			}
			if row["disable_ssl"] != true {
				t.Fatalf("disable_ssl=%v, want true", row["disable_ssl"])
			}
			if row["insecure"] != true {
				t.Fatalf("insecure=%v, want true", row["insecure"])
			}
			if row["warning"] != insecureSSLWarning {
				t.Fatalf("warning=%v, want %q", row["warning"], insecureSSLWarning)
			}
			for _, secret := range []string{"RAM-AK-CANARY", "RAM-SK-CANARY", "SECRET-TRN-CANARY", "SECRET/PATH/CANARY"} {
				if strings.Contains(stdout.String(), secret) {
					t.Fatalf("list output leaked secret %q", secret)
				}
			}
		})
	}

	// Negative cases: disable-ssl=false and non-RAM/OIDC modes must not have marker fields.
	t.Run("negative/no_marker_when_disable_ssl_false", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := Run([]string{"configure", "set", "--profile", "p-ram-false", "--mode", config.AuthModeRamRoleARN, "--region", "cn-beijing", "--endpoint", "https://tls-cn-beijing.volces.com", "--account-id", "123456789012", "--role-name", "r", "--ak", "AK", "--sk", "SK", "--disable-ssl=false"}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
		}
		var m map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &m); err != nil {
			t.Fatalf("invalid json: %v", err)
		}
		if _, ok := m["disable_ssl"]; ok {
			t.Fatalf("disable_ssl field present when false, want absent")
		}
		if _, ok := m["insecure"]; ok {
			t.Fatalf("insecure field present when false, want absent")
		}
		if _, ok := m["warning"]; ok {
			t.Fatalf("warning field present when false, want absent")
		}
	})

	t.Run("negative/no_marker_for_ecs", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := Run([]string{"configure", "set", "--profile", "p-ecs", "--mode", config.AuthModeECSRole, "--region", "cn-beijing", "--endpoint", "https://tls-cn-beijing.volces.com", "--role-name", "r", "--disable-ssl=true"}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
		}
		var m map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &m); err != nil {
			t.Fatalf("invalid json: %v", err)
		}
		if _, ok := m["disable_ssl"]; ok {
			t.Fatalf("disable_ssl field present for ecs, want absent")
		}
		if _, ok := m["insecure"]; ok {
			t.Fatalf("insecure field present for ecs, want absent")
		}
		if _, ok := m["warning"]; ok {
			t.Fatalf("warning field present for ecs, want absent")
		}
	})
}

// TestInsecureSSLWarningPinnedToExactLiteral pins the shared production warning
// constant to the approved literal so it cannot drift between configure and
// doctor output.
func TestInsecureSSLWarningPinnedToExactLiteral(t *testing.T) {
	const want = "STS requests will use HTTP; authentication material may be transmitted in plaintext. TLS business endpoint is unaffected."
	if insecureSSLWarning != want {
		t.Fatalf("insecureSSLWarning=%q, want %q", insecureSSLWarning, want)
	}
}

// TestInsecureSSLConditionNegativeCases exercises the pure marker condition at
// the helper level so the set/show/list positive coverage can stay focused.
// Every negative row must leave the output map unchanged (no marker fields).
func TestInsecureSSLConditionNegativeCases(t *testing.T) {
	cases := []struct {
		name       string
		mode       string
		disableSSL bool
	}{
		{"ram_false", config.AuthModeRamRoleARN, false},
		{"oidc_false", config.AuthModeOIDC, false},
		{"ecs_true", config.AuthModeECSRole, true},
		{"ak_true", config.AuthModeAK, true},
		{"sso_true", config.AuthModeSSO, true},
		{"console_true", config.AuthModeConsoleLogin, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if insecureSSLCondition(tc.mode, tc.disableSSL) {
				t.Fatalf("insecureSSLCondition(%q, %v)=true, want false", tc.mode, tc.disableSSL)
			}
			out := map[string]any{"profile": "x"}
			applyInsecureMarker(out, tc.mode, tc.disableSSL)
			if _, ok := out["disable_ssl"]; ok {
				t.Fatalf("disable_ssl present for %s, want absent", tc.name)
			}
			if _, ok := out["insecure"]; ok {
				t.Fatalf("insecure present for %s, want absent", tc.name)
			}
			if _, ok := out["warning"]; ok {
				t.Fatalf("warning present for %s, want absent", tc.name)
			}
			if out["profile"] != "x" {
				t.Fatalf("output map mutated for %s", tc.name)
			}
		})
	}
}
