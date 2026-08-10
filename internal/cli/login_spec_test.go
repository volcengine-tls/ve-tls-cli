package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth"
	"github.com/volcengine-tls/ve-tls-cli/internal/auth/console"
	"github.com/volcengine-tls/ve-tls-cli/internal/config"
	"github.com/volcengine-tls/ve-tls-cli/internal/output"
	"github.com/volcengine-tls/ve-tls-cli/internal/securestore"
)

// --- Item 1: real Run logout covers production FileCache/config store/stdout ---

func setupConsoleLogoutFixture(t *testing.T, session string, profileNames ...string) (string, *console.FileCache) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	t.Setenv("VOLCLOG_CONFIG", configPath)
	t.Setenv("VOLCLOG_LOGIN_CACHE_DIRECTORY", "")

	cfg := config.DefaultConfig()
	for _, name := range profileNames {
		cfg.PutProfile(name, config.Profile{
			Mode:         config.AuthModeConsoleLogin,
			LoginSession: session,
			Region:       "cn-beijing",
		})
	}
	cfg.CurrentProfile = profileNames[0]
	if err := config.Save(cfg, configPath); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cacheDir := filepath.Join(dir, "login", "cache")
	cache, err := console.NewFileCache(cacheDir)
	if err != nil {
		t.Fatalf("create file cache: %v", err)
	}
	if err := cache.WriteRaw(session, makeCacheBytesForTest(session, time.Now(), 3600, "refresh-token")); err != nil {
		t.Fatalf("write cache: %v", err)
	}
	return configPath, cache
}

// TestRunLogoutAllViaRealRun exercises the production path: real Run dispatch,
// production FileCache, production config store, and final stdout JSON. No
// network is required because logout only deletes cache and patches config.
func TestRunLogoutAllViaRealRun(t *testing.T) {
	const session = "trn:session:real-run-all"
	configPath, cache := setupConsoleLogoutFixture(t, session, "profA", "profB")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"logout", "--all"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	// stdout must be exactly the logout JSON shape, no envelope, no file path.
	var res logoutResult
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		t.Fatalf("stdout is not logout JSON: %v\nstdout=%q", err, stdout.String())
	}
	if res.ClearedSessionCount != 1 {
		t.Fatalf("expected 1 cleared session, got %d", res.ClearedSessionCount)
	}
	if len(res.ClearedProfiles) != 2 {
		t.Fatalf("expected 2 cleared profiles, got %d (%v)", len(res.ClearedProfiles), res.ClearedProfiles)
	}
	for _, p := range []string{"profA", "profB"} {
		found := false
		for _, cp := range res.ClearedProfiles {
			if cp == p {
				found = true
			}
		}
		if !found {
			t.Fatalf("cleared_profiles missing %q: %v", p, res.ClearedProfiles)
		}
	}
	// No session canary may appear in stdout.
	if strings.Contains(stdout.String(), session) {
		t.Fatalf("stdout leaked session canary: %q", stdout.String())
	}

	// Cache must be gone.
	if _, existed, _ := cache.ReadRaw(session); existed {
		t.Fatal("cache should be deleted after logout")
	}

	// Config bindings must be cleared.
	cfg, _, err := config.Load()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	for _, name := range []string{"profA", "profB"} {
		p, ok := cfg.GetProfile(name)
		if !ok {
			t.Fatalf("profile %s missing after logout", name)
		}
		if strings.TrimSpace(p.LoginSession) != "" {
			t.Fatalf("profile %s still bound to session %q after logout", name, p.LoginSession)
		}
	}
	_ = configPath
}

// TestRunLogoutProfileViaRealRun exercises logout of a single selected profile
// through the real Run path.
func TestRunLogoutProfileViaRealRun(t *testing.T) {
	const session = "trn:session:real-run-single"
	_, cache := setupConsoleLogoutFixture(t, session, "onlyProf")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"logout", "--profile", "onlyProf"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var res logoutResult
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		t.Fatalf("stdout not logout JSON: %v\n%q", err, stdout.String())
	}
	if len(res.ClearedProfiles) != 1 || res.ClearedProfiles[0] != "onlyProf" {
		t.Fatalf("unexpected cleared profiles: %v", res.ClearedProfiles)
	}
	if _, existed, _ := cache.ReadRaw(session); existed {
		t.Fatal("cache should be deleted")
	}
}

// --- Item 1: consoleLoginServiceAdapter translation via interface fake ---

type fakeConsoleLoginService struct {
	gotOpts console.LoginOptions
	result  *console.LoginResult
	err     error
}

func (f *fakeConsoleLoginService) Login(_ context.Context, opts console.LoginOptions) (*console.LoginResult, error) {
	f.gotOpts = opts
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

func TestConsoleLoginServiceAdapterTranslatesOptionsAndResult(t *testing.T) {
	want := &console.LoginResult{
		Profile:         "p1",
		Provider:        "console-login",
		Region:          "cn-beijing",
		Endpoint:        "https://tls-cn-beijing.volces.com",
		ExpiresAt:       time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		MaskedAccessKey: "AKLT***wxyz",
	}
	svc := &fakeConsoleLoginService{result: want}
	adapter := &consoleLoginServiceAdapter{svc: svc}

	res, err := adapter.Login(context.Background(), loginOpts{
		Profile:       "p1",
		Region:        "cn-beijing",
		Endpoint:      "https://tls-cn-beijing.volces.com",
		LoginEndpoint: "https://signin.byteplus.com",
		Remote:        true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Profile != want.Profile || res.Provider != want.Provider ||
		res.Region != want.Region || !res.ExpiresAt.Equal(want.ExpiresAt) ||
		res.Endpoint != want.Endpoint || res.MaskedAccessKey != want.MaskedAccessKey {
		t.Fatalf("translation mismatch: got %+v want %+v", res, want)
	}
	if svc.gotOpts.Profile != "p1" || svc.gotOpts.Region != "cn-beijing" ||
		svc.gotOpts.Endpoint != "https://tls-cn-beijing.volces.com" ||
		!svc.gotOpts.Remote || svc.gotOpts.EndpointURL != "https://signin.byteplus.com" {
		t.Fatalf("options not translated: %+v", svc.gotOpts)
	}
}

func TestConsoleLoginServiceAdapterNilResult(t *testing.T) {
	svc := &fakeConsoleLoginService{result: nil}
	adapter := &consoleLoginServiceAdapter{svc: svc}
	if _, err := adapter.Login(context.Background(), loginOpts{}); err == nil {
		t.Fatal("expected error for nil result")
	}
}

// --- Item 1: productionConfigStore with real config file ---

func TestProductionConfigStoreLoadAndUpdate(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	t.Setenv("VOLCLOG_CONFIG", configPath)

	cfg := config.DefaultConfig()
	cfg.PutProfile("p1", config.Profile{Mode: config.AuthModeConsoleLogin, LoginSession: "s1"})
	if err := config.Save(cfg, configPath); err != nil {
		t.Fatalf("save: %v", err)
	}

	store := productionConfigStore{}
	loaded, path, err := store.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if path != configPath {
		t.Fatalf("path mismatch: %q != %q", path, configPath)
	}
	p, ok := loaded.GetProfile("p1")
	if !ok || p.LoginSession != "s1" {
		t.Fatalf("loaded profile mismatch: %+v", p)
	}

	updated, err := store.Update(configPath, func(c *config.Config) error {
		pp, _ := c.GetProfile("p1")
		pp.LoginSession = ""
		c.PutProfile("p1", pp)
		return nil
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	pp, _ := updated.GetProfile("p1")
	if pp.LoginSession != "" {
		t.Fatalf("update did not clear session: %q", pp.LoginSession)
	}
}

// --- Item 1: runLoginWithFactory with fake factory ---

func TestRunLoginWithFactoryUsesInjectedFactory(t *testing.T) {
	var stdout, stderr bytes.Buffer
	ctx := newContext(&stdout, &stderr, output.FormatJSON, "", "")
	factoryCalled := false
	factory := func(c *Context) (*loginAdapter, error) {
		factoryCalled = true
		return &loginAdapter{
			loginSvc: &fakeLoginService{
				result: &loginResult{Profile: "p", Provider: "console-login", Region: "r", MaskedAccessKey: "AK***"},
			},
			stdout: &stdout,
			stderr: &stderr,
		}, nil
	}
	out, err := runLoginWithFactory(ctx, []string{"--profile", "p"}, factory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !factoryCalled {
		t.Fatal("factory was not called")
	}
	res, ok := out.(*loginResult)
	if !ok {
		t.Fatalf("expected *loginResult, got %T", out)
	}
	if res.Profile != "p" {
		t.Fatalf("unexpected profile: %q", res.Profile)
	}
	if ctx.FormatOverride != output.FormatJSON {
		t.Fatalf("expected JSON format override, got %q", ctx.FormatOverride)
	}
}

// --- Item 3: full Run pipeline with fake factory (no network, no globals) ---

// progressFakeLoginService writes a fixed progress canary to stderr and returns
// a redacted loginResult. It simulates the production LoginService behavior of
// emitting progress to stderr while keeping stdout clean for the final JSON.
type progressFakeLoginService struct {
	stderr io.Writer
	result *loginResult
	err    error
	called bool
}

func (f *progressFakeLoginService) Login(_ context.Context, _ loginOpts) (*loginResult, error) {
	f.called = true
	if f.stderr != nil {
		_, _ = f.stderr.Write([]byte("login-progress-canary\n"))
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

// newProgressFactory builds a factory that wires a progressFakeLoginService to
// the Context's stderr. factoryCalled records whether the factory was invoked
// (it must not be for pre-factory rejections like --jmes-filter).
func newProgressFactory(t *testing.T, factoryCalled *bool, res *loginResult, loginErr error) loginAdapterFactory {
	t.Helper()
	return func(c *Context) (*loginAdapter, error) {
		*factoryCalled = true
		return &loginAdapter{
			loginSvc: &progressFakeLoginService{
				stderr: c.Stderr,
				result: res,
				err:    loginErr,
			},
			stdout: c.Stdout,
			stderr: c.Stderr,
		}, nil
	}
}

func TestRunLoginFullPipelineWithFakeFactory(t *testing.T) {
	// Exercises the complete Run pipeline (global parse, project config,
	// dispatch, FormatOverride, generic output pipeline, output.Write, exit
	// code) with an injected fake factory. No network, no process-level globals.
	res := &loginResult{
		Profile:         "myprof",
		Provider:        "console-login",
		Region:          "cn-beijing",
		Endpoint:        "https://tls-cn-beijing.volces.com",
		ExpiresAt:       time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
		MaskedAccessKey: "AKLT***wxyz",
	}
	var factoryCalled bool
	factory := newProgressFactory(t, &factoryCalled, res, nil)

	var stdout, stderr bytes.Buffer
	code := runWithLoginAdapterFactory([]string{"login", "--profile", "myprof"}, &stdout, &stderr, factory, nil)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d stderr=%q", code, stderr.String())
	}
	if !factoryCalled {
		t.Fatal("factory should have been called")
	}

	// stdout must be valid JSON with exactly the 6 allowed fields.
	var m map[string]json.RawMessage
	if err := json.Unmarshal(stdout.Bytes(), &m); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout=%q", err, stdout.String())
	}
	wantKeys := map[string]bool{
		"profile": true, "provider": true, "region": true,
		"endpoint": true, "expires_at": true, "masked_access_key": true,
	}
	if len(m) != len(wantKeys) {
		t.Fatalf("expected %d fields, got %d: %v", len(wantKeys), len(m), keysOf(m))
	}
	for k := range m {
		if !wantKeys[k] {
			t.Fatalf("unexpected field %q in stdout JSON", k)
		}
	}

	// stdout must not contain progress, session, token, or full AK/SK.
	stdoutStr := stdout.String()
	for _, bad := range []string{"login-progress-canary", "session", "token", "AKLT***wxyz"[:len("AKLT***wxyz")-4] + "FULL"} {
		if strings.Contains(stdoutStr, bad) {
			t.Fatalf("stdout must not contain %q: %q", bad, stdoutStr)
		}
	}
	// The masked key may appear, but never the full key.
	if strings.Contains(stdoutStr, "FULLKEY") {
		t.Fatalf("stdout must not contain full access key: %q", stdoutStr)
	}

	// stderr must contain progress and must not contain any secret/session.
	stderrStr := stderr.String()
	if !strings.Contains(stderrStr, "login-progress-canary") {
		t.Fatalf("stderr should contain progress canary: %q", stderrStr)
	}
	for _, bad := range []string{"session-canary", "secret", "FULLKEY", "AKLT***wxyz"[:len("AKLT***wxyz")-4] + "FULL"} {
		if strings.Contains(stderrStr, bad) {
			t.Fatalf("stderr must not contain %q: %q", bad, stderrStr)
		}
	}
}

func TestRunLoginForcesJSONWithTableAndJSONLOutput(t *testing.T) {
	// --output table and --output jsonl must still produce the exact JSON
	// result on stdout because login freezes the output format.
	res := &loginResult{
		Profile:         "p",
		Provider:        "console-login",
		Region:          "r",
		Endpoint:        "https://tls.example.com",
		ExpiresAt:       time.Now(),
		MaskedAccessKey: "AK***",
	}
	for _, outFlag := range []string{"table", "jsonl"} {
		t.Run(outFlag, func(t *testing.T) {
			var factoryCalled bool
			factory := newProgressFactory(t, &factoryCalled, res, nil)
			var stdout, stderr bytes.Buffer
			code := runWithLoginAdapterFactory([]string{"--output", outFlag, "login", "--profile", "p"}, &stdout, &stderr, factory, nil)
			if code != 0 {
				t.Fatalf("expected exit 0, got %d stderr=%q", code, stderr.String())
			}
			var m map[string]json.RawMessage
			if err := json.Unmarshal(stdout.Bytes(), &m); err != nil {
				t.Fatalf("expected JSON output for --output %s, got: %q", outFlag, stdout.String())
			}
			if _, ok := m["profile"]; !ok {
				t.Fatalf("expected profile field in JSON, got: %q", stdout.String())
			}
		})
	}
}

func TestRunLoginRejectsFilterFileTraceBeforeFactory(t *testing.T) {
	// --jmes-filter, --output-file, --output-mode file, and --trace-dir must be
	// rejected before the factory is constructed, so no login side effect runs.
	res := &loginResult{Profile: "p", Provider: "console-login", Region: "r", MaskedAccessKey: "AK***"}
	cases := []struct {
		name string
		args []string
	}{
		{"jmes-filter", []string{"--jmes-filter", "profile", "login", "--profile", "p"}},
		{"output-file", []string{"--output-file", "/tmp/out.json", "login", "--profile", "p"}},
		{"output-mode-file", []string{"--output-mode", "file", "login", "--profile", "p"}},
		{"trace-dir", []string{"--trace-dir", t.TempDir(), "login", "--profile", "p"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var factoryCalled bool
			factory := newProgressFactory(t, &factoryCalled, res, nil)
			var stdout, stderr bytes.Buffer
			code := runWithLoginAdapterFactory(tc.args, &stdout, &stderr, factory, nil)
			if code == 0 {
				t.Fatalf("expected non-zero exit for %s", tc.name)
			}
			if factoryCalled {
				t.Fatalf("factory should not be called for %s (side effect must not run)", tc.name)
			}
		})
	}
}

func TestRunLoginFactoryErrorGoesThroughRunErrorOutput(t *testing.T) {
	// If the factory returns an error, it must flow through the full Run error
	// output pipeline (writeCLIError to stderr) with a stable exit code, and
	// stdout must be empty.
	factoryErr := errors.New("factory-boom-canary")
	factory := func(c *Context) (*loginAdapter, error) {
		return nil, factoryErr
	}
	var stdout, stderr bytes.Buffer
	code := runWithLoginAdapterFactory([]string{"login", "--profile", "p"}, &stdout, &stderr, factory, nil)
	if code == 0 {
		t.Fatal("expected non-zero exit for factory error")
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout should be empty on factory error, got: %q", stdout.String())
	}
	// stderr must contain the error payload but must not leak the canary in a
	// way that bypasses redaction (the factory error is rendered as-is here
	// because it is not a *safeCLIError; verify it is at least classified).
	if stderr.Len() == 0 {
		t.Fatal("stderr should contain error output")
	}
	var payload errPayload
	if err := json.Unmarshal(stderr.Bytes(), &payload); err != nil {
		t.Fatalf("stderr is not error JSON: %v\nstderr=%q", err, stderr.String())
	}
	if payload.Kind == "" {
		t.Fatalf("expected a classified error kind, got payload=%+v", payload)
	}
}

func TestRunLoginServiceErrorIsRedactedThroughRunErrorOutput(t *testing.T) {
	// If the login service returns an error containing a secret canary, the
	// adapter wraps it as a *safeCLIError ("console login failed"). The Run
	// error output must render only the safe description, never the canary.
	loginErr := errors.New("login failed: secret-canary-xyz session-canary-789")
	res := &loginResult{Profile: "p", Provider: "console-login", Region: "r", MaskedAccessKey: "AK***"}
	var factoryCalled bool
	factory := newProgressFactory(t, &factoryCalled, res, loginErr)
	var stdout, stderr bytes.Buffer
	code := runWithLoginAdapterFactory([]string{"login", "--profile", "p"}, &stdout, &stderr, factory, nil)
	if code == 0 {
		t.Fatal("expected non-zero exit for login error")
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout should be empty on login error, got: %q", stdout.String())
	}
	stderrStr := stderr.String()
	for _, bad := range []string{"secret-canary-xyz", "session-canary-789"} {
		if strings.Contains(stderrStr, bad) {
			t.Fatalf("stderr must not leak canary %q: %q", bad, stderrStr)
		}
	}
	if !strings.Contains(stderrStr, "console login failed") {
		t.Fatalf("stderr should contain safe error description: %q", stderrStr)
	}
}

// --- Item 2: selector/flag error classification via real Run ---

func runAndClassify(t *testing.T, args ...string) (string, string, int, errPayload) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(args, &stdout, &stderr)
	var payload errPayload
	if b := stderr.Bytes(); len(b) > 0 {
		_ = json.Unmarshal(b, &payload)
	}
	return stdout.String(), stderr.String(), code, payload
}

func TestLogoutAllWithGlobalProfileIsIncompatibleFlags(t *testing.T) {
	// Real probe from the spec: volclog --profile prod logout --all
	_, _, code, payload := runAndClassify(t, "--profile", "prod", "logout", "--all")
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	if payload.Kind != "incompatible_flags" {
		t.Fatalf("expected kind incompatible_flags, got %q (stderr payload=%+v)", payload.Kind, payload)
	}
}

func TestLogoutAllWithLocalProfileIsIncompatibleFlags(t *testing.T) {
	_, _, code, payload := runAndClassify(t, "logout", "--profile", "p", "--all")
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	if payload.Kind != "incompatible_flags" {
		t.Fatalf("expected kind incompatible_flags, got %q", payload.Kind)
	}
}

func TestLoginMissingProfileValueIsUsage(t *testing.T) {
	_, _, code, payload := runAndClassify(t, "login", "--profile")
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	if payload.Kind != "usage" {
		t.Fatalf("expected kind usage, got %q", payload.Kind)
	}
}

func TestLoginMissingValueFlagsAreUsageAndDoNotCallFactory(t *testing.T) {
	// Every flag that takes a value must produce a usage error (exit 1,
	// kind=usage) when the value is omitted, and the factory must never be
	// invoked so no login side effect runs.
	cases := []struct {
		name string
		flag string
	}{
		{"short-profile", "-p"},
		{"long-profile", "--profile"},
		{"short-region", "-r"},
		{"long-region", "--region"},
		{"endpoint", "--endpoint"},
		{"login-endpoint", "--login-endpoint"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var factoryCalled bool
			factory := func(c *Context) (*loginAdapter, error) {
				factoryCalled = true
				return &loginAdapter{}, nil
			}
			var stdout, stderr bytes.Buffer
			code := runWithLoginAdapterFactory([]string{"login", tc.flag}, &stdout, &stderr, factory, nil)
			if code != 1 {
				t.Fatalf("expected exit 1 for missing %s value, got %d", tc.flag, code)
			}
			if factoryCalled {
				t.Fatalf("factory should not be called for missing %s value", tc.flag)
			}
			var payload errPayload
			if err := json.Unmarshal(stderr.Bytes(), &payload); err != nil {
				t.Fatalf("stderr is not error JSON: %v\nstderr=%q", err, stderr.String())
			}
			if payload.Kind != "usage" {
				t.Fatalf("expected kind usage for missing %s value, got %q", tc.flag, payload.Kind)
			}
		})
	}
}

func TestLoginEndpointURLIsRejectedBeforeFactory(t *testing.T) {
	var factoryCalled bool
	factory := func(c *Context) (*loginAdapter, error) {
		factoryCalled = true
		return &loginAdapter{}, nil
	}
	var stdout, stderr bytes.Buffer
	code := runWithLoginAdapterFactory(
		[]string{"login", "--endpoint-url", "https://signin.example.com"},
		&stdout,
		&stderr,
		factory,
		nil,
	)
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	if factoryCalled {
		t.Fatal("factory should not be called for unsupported --endpoint-url")
	}
}

func TestLogoutMissingProfileValueIsUsage(t *testing.T) {
	_, _, code, payload := runAndClassify(t, "logout", "--profile")
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	if payload.Kind != "usage" {
		t.Fatalf("expected kind usage, got %q", payload.Kind)
	}
}

func TestLoginLocalSecretsFileIsValidation(t *testing.T) {
	_, _, code, payload := runAndClassify(t, "login", "--secrets-file", "/tmp/x")
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	if payload.Kind != "validation" {
		t.Fatalf("expected kind validation, got %q", payload.Kind)
	}
}

func TestLogoutLocalSecretsFileIsValidation(t *testing.T) {
	_, _, code, payload := runAndClassify(t, "logout", "--secrets-file", "/tmp/x")
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	if payload.Kind != "validation" {
		t.Fatalf("expected kind validation, got %q", payload.Kind)
	}
}

func TestLoginProfileConflictIsIncompatibleFlags(t *testing.T) {
	_, _, code, payload := runAndClassify(t, "--profile", "a", "login", "--profile", "b")
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	if payload.Kind != "incompatible_flags" {
		t.Fatalf("expected kind incompatible_flags, got %q", payload.Kind)
	}
}

// --- Item 3: partial failure classification ---

func TestClassifyErrorPartialFailure(t *testing.T) {
	err := newLogoutPartialFailureError(errors.New("config write boom: cause-canary-123"))
	if !errors.Is(err, ErrLogoutPartialFailure) {
		t.Fatal("errors.Is should match ErrLogoutPartialFailure")
	}
	payload, code := classifyError(err, "", 0, "logout")
	if code != 2 {
		t.Fatalf("expected exit 2, got %d", code)
	}
	if payload.Kind != "partial_failure" {
		t.Fatalf("expected kind partial_failure, got %q", payload.Kind)
	}
	// The safe Error() must not render the cause canary.
	if strings.Contains(err.Error(), "cause-canary-123") {
		t.Fatalf("partial failure error leaked cause: %q", err.Error())
	}
	if strings.Contains(payload.ErrorMessage, "cause-canary-123") {
		t.Fatalf("payload leaked cause: %q", payload.ErrorMessage)
	}
}

func TestClassifyErrorPartialFailureHintUsesValidCommand(t *testing.T) {
	// The partial-failure hint must suggest a command that actually exists
	// (volclog configure show --profile <name>), not the non-existent
	// 'volclog configure get'.
	err := newLogoutPartialFailureError(errors.New("config write boom"))
	payload, _ := classifyError(err, "", 0, "logout")
	if !strings.Contains(payload.Hint, "volclog configure show --profile") {
		t.Fatalf("hint should suggest 'volclog configure show --profile <name>', got: %q", payload.Hint)
	}
	if strings.Contains(payload.Hint, "configure get") {
		t.Fatalf("hint must not suggest non-existent 'configure get': %q", payload.Hint)
	}
}

func TestLogoutPartialFailureDoesNotMaskAsLockError(t *testing.T) {
	// Simulate config.Update failing inside the cache lock. The returned error
	// must remain ErrLogoutPartialFailure, not be wrapped as "lock acquisition".
	cache := newFakeCache()
	store := &fakeConfigStore{
		cfg:       config.DefaultConfig(),
		path:      "/tmp/cfg.json",
		updateErr: errors.New("config disk full: session-canary-xyz"),
	}
	adapter := &loginAdapter{cache: cache, cfgStore: store}
	_, err := adapter.logoutSession(context.Background(), "/tmp/cfg.json", "s1")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrLogoutPartialFailure) {
		t.Fatalf("expected ErrLogoutPartialFailure, got %v", err)
	}
	if strings.Contains(err.Error(), "lock") {
		t.Fatalf("partial failure should not be mislabeled as lock error: %q", err.Error())
	}
	if strings.Contains(err.Error(), "session-canary-xyz") {
		t.Fatalf("error leaked session canary: %q", err.Error())
	}
}

func TestLogoutPartialFailureRetainsBindingInStore(t *testing.T) {
	// When config.Update fails (partial failure), the cache is deleted but the
	// config binding must remain intact in the store. The fakeConfigStore now
	// deep-copies before invoking the callback, so a failed Update must not
	// mutate the in-memory config maps (mirroring production atomic semantics).
	const session = "trn:session:partial-retain"
	cfg := config.DefaultConfig()
	cfg.PutProfile("p1", config.Profile{Mode: config.AuthModeConsoleLogin, LoginSession: session})
	cache := newFakeCache()
	cache.data[session] = []byte(`{"login_session":"` + session + `"}`)
	store := &fakeConfigStore{
		cfg:       cfg,
		path:      "/tmp/cfg.json",
		updateErr: errors.New("simulated config write failure"),
	}
	adapter := &loginAdapter{cache: cache, cfgStore: store}

	_, err := adapter.runLogout(context.Background(), "p1", false)
	if err == nil {
		t.Fatal("expected partial failure error")
	}
	if !errors.Is(err, ErrLogoutPartialFailure) {
		t.Fatalf("expected ErrLogoutPartialFailure, got %v", err)
	}
	// Cache is deleted (fail closed).
	if atomic.LoadInt32(&cache.deleteCnt) != 1 {
		t.Fatalf("expected cache to be deleted, got deleteCnt=%d", cache.deleteCnt)
	}
	// The binding must still be present in the store because the failed Update
	// did not commit the mutation.
	p, ok := store.cfg.GetProfile("p1")
	if !ok {
		t.Fatal("profile p1 should still exist in store after failed Update")
	}
	if strings.TrimSpace(p.LoginSession) != session {
		t.Fatalf("binding should be retained after failed Update, got LoginSession=%q", p.LoginSession)
	}
}

// --- Item 4: freeze output surface before side effects ---

func TestLogoutRejectsOutputModeFileBeforeSideEffect(t *testing.T) {
	const session = "trn:session:freeze-output-mode"
	configPath, cache := setupConsoleLogoutFixture(t, session, "p1")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--output-mode", "file", "logout", "--all"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	// Cache and config must be untouched.
	if _, existed, _ := cache.ReadRaw(session); !existed {
		t.Fatal("cache should not be deleted when output-mode is rejected")
	}
	cfg, _, _ := config.Load()
	p, _ := cfg.GetProfile("p1")
	if strings.TrimSpace(p.LoginSession) == "" {
		t.Fatal("config binding should not be cleared when output-mode is rejected")
	}
	// stdout must not contain a file path.
	if strings.Contains(strings.ToLower(stdout.String()), ".json") || strings.Contains(stdout.String(), "/") {
		t.Fatalf("stdout should not contain a file path: %q", stdout.String())
	}
	_ = configPath
}

func TestLogoutRejectsOutputFileBeforeSideEffect(t *testing.T) {
	const session = "trn:session:freeze-output-file"
	_, cache := setupConsoleLogoutFixture(t, session, "p1")
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--output-file", "/tmp/out.json", "logout", "--all"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	if _, existed, _ := cache.ReadRaw(session); !existed {
		t.Fatal("cache should not be deleted")
	}
}

func TestLogoutRejectsJmesFilterBeforeSideEffect(t *testing.T) {
	const session = "trn:session:freeze-jmes"
	_, cache := setupConsoleLogoutFixture(t, session, "p1")
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--jmes-filter", "cleared_profiles", "logout", "--all"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	if _, existed, _ := cache.ReadRaw(session); !existed {
		t.Fatal("cache should not be deleted")
	}
}

func TestLogoutRejectsTraceDirBeforeSideEffect(t *testing.T) {
	const session = "trn:session:freeze-trace"
	_, cache := setupConsoleLogoutFixture(t, session, "p1")
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--trace-dir", t.TempDir(), "logout", "--all"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	if _, existed, _ := cache.ReadRaw(session); !existed {
		t.Fatal("cache should not be deleted")
	}
	// stdout must not be wrapped in a data/meta envelope.
	if strings.Contains(stdout.String(), "\"data\"") && strings.Contains(stdout.String(), "\"meta\"") {
		t.Fatalf("stdout should not be trace-wrapped: %q", stdout.String())
	}
}

func TestLogoutForcesJSONRegardlessOfOutputFlag(t *testing.T) {
	const session = "trn:session:force-json"
	_, _ = setupConsoleLogoutFixture(t, session, "p1")
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--output", "table", "logout", "--all"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d stderr=%q", code, stderr.String())
	}
	// Must be valid JSON, not a table.
	var res logoutResult
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		t.Fatalf("expected JSON output, got: %q", stdout.String())
	}
}

// --- Item 5: logout scans latest config inside cache lock ---

// TestLogoutClearsProfileThatAppearsAfterLoad proves a new console profile
// bound to the same session after the initial Load is still cleared, because
// logoutSession scans the latest config inside the cache lock.
func TestLogoutClearsProfileThatAppearsAfterLoad(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.PutProfile("p1", config.Profile{Mode: config.AuthModeConsoleLogin, LoginSession: "s1"})

	store := &fakeConfigStore{cfg: cfg, path: "/tmp/cfg.json"}
	// Inside Update, mutate the config to add a new profile bound to s1,
	// simulating a concurrent bind between Load and the lock.
	store.updateHook = func(c *config.Config) {
		c.PutProfile("p2", config.Profile{Mode: config.AuthModeConsoleLogin, LoginSession: "s1"})
	}
	cache := newFakeCache()
	adapter := &loginAdapter{cache: cache, cfgStore: store}

	res, err := adapter.runLogout(context.Background(), "p1", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Both p1 (known at Load) and p2 (appeared in Update) must be cleared.
	if len(res.ClearedProfiles) != 2 {
		t.Fatalf("expected 2 cleared profiles, got %d (%v)", len(res.ClearedProfiles), res.ClearedProfiles)
	}
	for _, want := range []string{"p1", "p2"} {
		found := false
		for _, cp := range res.ClearedProfiles {
			if cp == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("cleared_profiles missing %q: %v", want, res.ClearedProfiles)
		}
	}
}

// TestLogoutDoesNotClearProfileThatChangedModeAfterLoad proves a profile that
// switched from console-login to AK/SK after Load is not cleared or reported.
func TestLogoutDoesNotClearProfileThatChangedModeAfterLoad(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.PutProfile("p1", config.Profile{Mode: config.AuthModeConsoleLogin, LoginSession: "s1"})
	cfg.PutProfile("p2", config.Profile{Mode: config.AuthModeConsoleLogin, LoginSession: "s1"})

	store := &fakeConfigStore{cfg: cfg, path: "/tmp/cfg.json"}
	store.updateHook = func(c *config.Config) {
		// p2 becomes AK/SK between Load and the lock; it must not be cleared.
		p2, _ := c.GetProfile("p2")
		p2.Mode = config.AuthModeAK
		p2.AccessKeyID = "AKLTnew"
		p2.LoginSession = "s1" // dormant session retained
		c.PutProfile("p2", p2)
	}
	cache := newFakeCache()
	adapter := &loginAdapter{cache: cache, cfgStore: store}

	res, err := adapter.runLogout(context.Background(), "p1", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.ClearedProfiles) != 1 || res.ClearedProfiles[0] != "p1" {
		t.Fatalf("expected only p1 cleared, got %v", res.ClearedProfiles)
	}
	// p2 must remain fully intact in the updated config.
	updated := store.cfg
	p2, _ := updated.GetProfile("p2")
	if p2.Mode != config.AuthModeAK || p2.AccessKeyID != "AKLTnew" || p2.LoginSession != "s1" {
		t.Fatalf("p2 was modified: %+v", p2)
	}
}

// TestLogoutSharedSessionOnlyConsoleChanges proves that when a console profile
// shares a dormant login-session with AK/default-chain/SSO profiles, only the
// console profile is cleared; the others are byte-for-byte unchanged.
func TestLogoutSharedSessionOnlyConsoleChanges(t *testing.T) {
	const dormant = "dormant-session-123"
	cfg := config.DefaultConfig()
	cfg.PutProfile("console", config.Profile{Mode: config.AuthModeConsoleLogin, LoginSession: dormant, Region: "cn-beijing"})
	cfg.PutProfile("ak", config.Profile{Mode: config.AuthModeAK, AccessKeyID: "AKLTak", LoginSession: dormant})
	cfg.PutProfile("sso", config.Profile{Mode: config.AuthModeSSO, LoginSession: dormant})

	store := &fakeConfigStore{cfg: cfg, path: "/tmp/cfg.json"}
	cache := newFakeCache()
	adapter := &loginAdapter{cache: cache, cfgStore: store}

	res, err := adapter.runLogout(context.Background(), "console", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.ClearedProfiles) != 1 || res.ClearedProfiles[0] != "console" {
		t.Fatalf("expected only console cleared, got %v", res.ClearedProfiles)
	}

	updated := store.cfg
	// console must have its session cleared.
	cp, _ := updated.GetProfile("console")
	if strings.TrimSpace(cp.LoginSession) != "" {
		t.Fatalf("console session not cleared: %q", cp.LoginSession)
	}
	// ak and sso must be completely unchanged.
	for _, name := range []string{"ak", "sso"} {
		orig, _ := cfg.GetProfile(name)
		got, _ := updated.GetProfile(name)
		if !reflect.DeepEqual(orig, got) {
			t.Fatalf("profile %s was modified: orig=%+v got=%+v", name, orig, got)
		}
	}
}

// --- Item 6: refresh-first race (Provider holds lock during refresh) ---

// TestLogoutRefreshCompletesBeforeLogoutDeletes proves the reverse of the
// existing logout-first race: the Provider acquires the cache lock first and
// blocks inside a fake OAuth refresh. Logout then starts and must wait on the
// SAME lock (observed via securestore.WithLockContentionObserver, no sleep).
// When the refresh completes, the Provider writes back the refreshed cache and
// releases the lock; logout then acquires it, deletes the LATEST cache, and
// clears the config binding. The cache must not survive and a subsequent
// Provider.Retrieve must require reauth.
func TestLogoutRefreshCompletesBeforeLogoutDeletes(t *testing.T) {
	dir := t.TempDir()
	cache, err := console.NewFileCache(dir)
	if err != nil {
		t.Fatalf("create file cache: %v", err)
	}
	session := "trn:session:refresh-first"
	// ExpiresIn=30s puts the cache inside the 60s RefreshBuffer, so the Provider
	// will attempt a refresh rather than returning the cached value.
	cacheBytes := makeCacheBytesForTest(session, time.Now(), 30, "refresh-token-123")
	if err := cache.WriteRaw(session, cacheBytes); err != nil {
		t.Fatalf("write cache: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.PutProfile("prod", config.Profile{
		Mode:         config.AuthModeConsoleLogin,
		LoginSession: session,
	})
	store := &fakeConfigStore{cfg: cfg, path: filepath.Join(dir, "config.json")}

	fakeClient := &fakeOAuthClient{
		exchangeResp: &console.ConsoleTokenResponse{
			AccessToken: validSTSAccessTokenForTest(),
			TokenType:   "sts",
			ExpiresIn:   3600,
		},
		blockExchange: make(chan struct{}),
		proceed:       make(chan struct{}),
	}
	provider := console.NewProvider(session, cache, func(string) (console.OAuthClient, error) {
		return fakeClient, nil
	}, nil)

	adapter := &loginAdapter{
		cache:    cache,
		cfgStore: store,
		stdout:   &bytes.Buffer{},
		stderr:   &bytes.Buffer{},
	}

	// 1. Provider.Retrieve acquires the lock and blocks inside the fake refresh.
	retrieveDone := make(chan error, 1)
	go func() {
		_, err := provider.Retrieve(context.Background())
		retrieveDone <- err
	}()

	// Wait until the Provider is actually inside ExchangeToken (holding the lock).
	select {
	case <-fakeClient.blockExchange:
	case <-time.After(5 * time.Second):
		t.Fatal("provider did not enter refresh in time")
	}

	// 2. Logout starts; it must contend for the same lock the Provider holds.
	logoutContended := make(chan struct{}, 1)
	var obsOnce sync.Once
	logoutCtx := securestore.WithLockContentionObserver(context.Background(), func() {
		obsOnce.Do(func() { close(logoutContended) })
	})
	logoutDone := make(chan error, 1)
	go func() {
		_, err := adapter.runLogout(logoutCtx, "prod", false)
		logoutDone <- err
	}()

	// Confirm logout is blocked waiting on the cache lock (no sleep).
	select {
	case <-logoutContended:
	case <-time.After(5 * time.Second):
		t.Fatal("logout lock contention not observed in time")
	}

	// 3. Release the fake refresh; Provider writes back the refreshed cache and
	// releases the lock.
	close(fakeClient.proceed)

	// 4. Both must complete.
	retrieveErr := <-retrieveDone
	logoutErr := <-logoutDone

	if retrieveErr != nil {
		t.Fatalf("provider refresh should succeed, got %v", retrieveErr)
	}
	if logoutErr != nil {
		t.Fatalf("logout should succeed, got %v", logoutErr)
	}

	// 5. The cache must be gone (logout deleted the refreshed cache).
	if _, existed, _ := cache.ReadRaw(session); existed {
		t.Fatal("cache must be deleted after logout, even though refresh wrote it back")
	}

	// 6. Config binding must be cleared.
	updated := store.cfg
	p, _ := updated.GetProfile("prod")
	if strings.TrimSpace(p.LoginSession) != "" {
		t.Fatalf("config binding not cleared: %q", p.LoginSession)
	}

	// 7. A subsequent Provider.Retrieve must require reauth (no resurrection).
	_, err = provider.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected subsequent Provider.Retrieve to require reauth")
	}
	var authErr *auth.Error
	if !errors.As(err, &authErr) || authErr.Kind != auth.ReauthRequired {
		t.Fatalf("expected ReauthRequired, got %v", err)
	}
}

// --- Item 7: confirm prompt length limit ---

func TestReadConfirmLineLimitsInputLength(t *testing.T) {
	// Feed a very long line without a newline; readConfirmLine must not grow
	// without bound and must fail closed with an error instead of returning a
	// truncated prefix that could be TrimSpace'd into an unintended confirmation.
	long := strings.Repeat("a", 100000)
	r := strings.NewReader(long)
	line, err := readConfirmLine(r)
	if err == nil {
		t.Fatalf("expected error for overlong line, got line %q", line)
	}
	if !errors.Is(err, errConfirmLineTooLong) {
		t.Fatalf("expected errConfirmLineTooLong, got %v", err)
	}
	if line != "" {
		t.Fatalf("expected empty line on overflow, got %q", line)
	}
	if strings.Contains(err.Error(), "aaaa") {
		t.Fatalf("error must not contain input: %q", err.Error())
	}
}

// --- Item 7: login JSON exact fields ---

func TestLoginResultJSONHasExactFields(t *testing.T) {
	res := &loginResult{
		Profile:         "p",
		Provider:        "console-login",
		Region:          "r",
		ExpiresAt:       time.Now(),
		MaskedAccessKey: "AK***",
	}
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	wantKeys := map[string]bool{
		"profile": true, "provider": true, "region": true,
		"endpoint": true, "expires_at": true, "masked_access_key": true,
	}
	if len(m) != len(wantKeys) {
		t.Fatalf("expected %d keys, got %d: %v", len(wantKeys), len(m), keysOf(m))
	}
	for k := range m {
		if !wantKeys[k] {
			t.Fatalf("unexpected key %q in login JSON", k)
		}
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
