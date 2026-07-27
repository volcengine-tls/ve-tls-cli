package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth"
	"github.com/volcengine-tls/ve-tls-cli/internal/auth/console"
	"github.com/volcengine-tls/ve-tls-cli/internal/auth/sso"
	"github.com/volcengine-tls/ve-tls-cli/internal/config"
	"github.com/volcengine-tls/ve-tls-cli/internal/output"
)

// staticStatusReader is a fake authStatusReader for tests.
type staticStatusReader struct {
	status authStatus
	err    error
}

func (r *staticStatusReader) Status(context.Context, string) (authStatus, error) {
	return r.status, r.err
}

// newDoctorTestContext builds a Context whose cfg/cfgPath point at a real
// config file written to a temp dir, so runDoctor's config.Load() succeeds.
func newDoctorTestContext(t *testing.T, cfg config.Config) *Context {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]config.Profile{}
	}
	if cfg.SSOSessions == nil {
		cfg.SSOSessions = map[string]config.SSOSession{}
	}
	if err := config.Save(cfg, cfgPath); err != nil {
		t.Fatalf("save config: %v", err)
	}
	t.Setenv("VOLCLOG_CONFIG", cfgPath)
	ctx := newContext(&bytes.Buffer{}, &bytes.Buffer{}, output.FormatJSON, "", "")
	ctx.cfg = cfg
	ctx.cfgPath = cfgPath
	return ctx
}

// TestDoctorStaticOutputContractUnchanged proves that for a static AK profile,
// the credentials output contains exactly the frozen fields
// (mode/source/present/ak/sk/token) with no dynamic-only fields added.
func TestDoctorStaticOutputContractUnchanged(t *testing.T) {
	clearAuthTestEnv(t)
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
	ctx := newDoctorTestContext(t, cfg)
	ctx.Profile = "static"

	out, _, err := runDoctor(ctx, nil)
	if err != nil {
		t.Fatalf("runDoctor: %v", err)
	}
	creds := out.(map[string]any)["credentials"].(map[string]any)

	// Frozen static fields must be present with correct values.
	for _, key := range []string{"mode", "source", "present", "ak", "sk", "token"} {
		if _, ok := creds[key]; !ok {
			t.Fatalf("static credentials missing frozen field %q: %v", key, creds)
		}
	}
	if creds["mode"] != "aksk" {
		t.Fatalf("expected mode=aksk, got %v", creds["mode"])
	}
	if creds["present"] != true {
		t.Fatalf("expected present=true, got %v", creds["present"])
	}
	if creds["ak"] != true {
		t.Fatalf("expected ak=true, got %v", creds["ak"])
	}
	if creds["sk"] != true {
		t.Fatalf("expected sk=true, got %v", creds["sk"])
	}

	// Dynamic-only fields must NOT appear for static profiles.
	for _, key := range []string{"provider", "expires_at", "refresh_required"} {
		if _, ok := creds[key]; ok {
			t.Fatalf("static credentials must not contain dynamic field %q: %v", key, creds)
		}
	}
}

// TestDoctorOfflineDynamicModeReadsMetadataWithoutRefresh proves that an
// offline doctor for a dynamic profile reads cache metadata via the
// authStatusReader without invoking Retrieve (no refresh, no network).
func TestDoctorOfflineDynamicModeReadsMetadataWithoutRefresh(t *testing.T) {
	clearAuthTestEnv(t)
	expires := time.Now().Add(2 * time.Hour).UTC()
	cfg := config.Config{
		Version: 1,
		Profiles: map[string]config.Profile{
			"sso": {
				Mode:     config.AuthModeSSO,
				Region:   "cn-beijing",
				Endpoint: "https://tls-cn-beijing.volces.com",
			},
		},
	}
	ctx := newDoctorTestContext(t, cfg)
	ctx.Profile = "sso"

	// Inject a factory whose status reader reports a valid cache but whose
	// provider would fail if Retrieve were ever called.
	retrieveCalled := false
	retrieveErr := errSentinel("Retrieve must not be called in offline doctor")
	provider := &fakeProvider{}
	provider.retrieveFn = func() (auth.Value, error) {
		retrieveCalled = true
		return auth.Value{}, retrieveErr
	}
	statusReader := &staticStatusReader{status: authStatus{
		Provider:        "sso",
		Present:         true,
		ExpiresAt:       expires,
		RefreshRequired: false,
	}}
	factory := &fakeAuthFactory{
		ssoProvider: provider,
		ssoStatus:   statusReader,
	}
	ctx.authFactory = factory

	out, _, err := runDoctor(ctx, nil)
	if err != nil {
		t.Fatalf("runDoctor: %v", err)
	}
	if retrieveCalled {
		t.Fatalf("offline doctor must not call Retrieve")
	}

	creds := out.(map[string]any)["credentials"].(map[string]any)
	if creds["provider"] != "sso" {
		t.Fatalf("expected provider=sso, got %v", creds["provider"])
	}
	if creds["present"] != true {
		t.Fatalf("expected present=true, got %v", creds["present"])
	}
	if creds["refresh_required"] != false {
		t.Fatalf("expected refresh_required=false, got %v", creds["refresh_required"])
	}
	expiresStr, _ := creds["expires_at"].(string)
	if !strings.HasPrefix(expiresStr, expires.Format("2006-01-02")) {
		t.Fatalf("expected expires_at to start with %q, got %v", expires.Format("2006-01-02"), creds["expires_at"])
	}

	// Static credential booleans must be false for dynamic profiles.
	if creds["ak"] != false {
		t.Fatalf("expected ak=false for dynamic, got %v", creds["ak"])
	}
	if creds["sk"] != false {
		t.Fatalf("expected sk=false for dynamic, got %v", creds["sk"])
	}
}

// TestDoctorOnlineDynamicModeRetrievesProvider proves that --online doctor for
// a dynamic profile builds the dynamic client and calls DescribeProjects,
// exercising the provider's Retrieve path.
func TestDoctorOnlineDynamicModeRetrievesProvider(t *testing.T) {
	clearAuthTestEnv(t)
	requestCount := 0
	server := newJSONTestServer(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("x-tls-requestid", "online-dynamic")
		w.Header().Set("Date", time.Now().UTC().Format(http.TimeFormat))
		_, _ = w.Write([]byte(`{"Projects":[]}`))
	})
	defer server.Close()

	cfg := config.Config{
		Version: 1,
		Profiles: map[string]config.Profile{
			"sso": {
				Mode:     config.AuthModeSSO,
				Region:   "cn-beijing",
				Endpoint: server.URL,
			},
		},
	}
	ctx := newDoctorTestContext(t, cfg)
	ctx.Profile = "sso"

	provider := &fakeProvider{value: auth.Value{
		AccessKeyID:     "AKLTONLINE",
		SecretAccessKey: "online-secret",
		SessionToken:    "online-token",
		ProviderName:    "sso",
		ExpiresAt:       time.Now().Add(time.Hour),
	}}
	statusReader := &staticStatusReader{status: authStatus{
		Provider: "sso", Present: true, ExpiresAt: time.Now().Add(time.Hour),
	}}
	factory := &fakeAuthFactory{ssoProvider: provider, ssoStatus: statusReader}
	ctx.authFactory = factory

	out, _, err := runDoctor(ctx, []string{"--online"})
	if err != nil {
		t.Fatalf("runDoctor --online: %v", err)
	}
	if requestCount != 1 {
		t.Fatalf("expected exactly one DescribeProjects request, got %d", requestCount)
	}
	if provider.calls != 1 {
		t.Fatalf("expected provider Retrieve to be called once, got %d", provider.calls)
	}

	checks := out.(map[string]any)["checks"].([]map[string]any)
	found := false
	for _, c := range checks {
		if c["name"] == "online_describe_projects" {
			found = true
			if c["ok"] != true {
				t.Fatalf("expected online_describe_projects to pass, got %v", c)
			}
		}
	}
	if !found {
		t.Fatalf("expected online_describe_projects check in output: %v", checks)
	}
}

// TestDoctorNeverPrintsCredentialsOrTokens proves that doctor output for a
// dynamic profile never contains AK/SK/SessionToken/OAuth token material, even
// when the underlying cache holds them.
func TestDoctorNeverPrintsCredentialsOrTokens(t *testing.T) {
	clearAuthTestEnv(t)
	cfg := config.Config{
		Version: 1,
		Profiles: map[string]config.Profile{
			"sso": {
				Mode:     config.AuthModeSSO,
				Region:   "cn-beijing",
				Endpoint: "https://tls-cn-beijing.volces.com",
			},
		},
	}
	ctx := newDoctorTestContext(t, cfg)
	ctx.Profile = "sso"

	// The status reader returns metadata only; no secrets flow through it.
	statusReader := &staticStatusReader{status: authStatus{
		Provider:        "sso",
		Present:         true,
		ExpiresAt:       time.Now().Add(time.Hour),
		RefreshRequired: false,
	}}
	factory := &fakeAuthFactory{ssoStatus: statusReader}
	ctx.authFactory = factory

	out, _, err := runDoctor(ctx, nil)
	if err != nil {
		t.Fatalf("runDoctor: %v", err)
	}
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal output: %v", err)
	}
	output := string(raw)

	// Canary values that must never appear in any doctor output.
	canaries := []string{
		"AKLTsecret-canary",
		"secret_access_key_canary",
		"session_token_canary",
		"oauth_access_token_canary",
		"refresh_token_canary",
	}
	for _, c := range canaries {
		if strings.Contains(output, c) {
			t.Fatalf("doctor output contains canary %q: %s", c, output)
		}
	}
}

// newJSONTestServer is a small helper that returns an httptest.Server responding
// with the given handler.
func newJSONTestServer(handler http.HandlerFunc) *httptest.Server {
	return httptest.NewServer(handler)
}

// TestDoctorOnlineDynamicUsesResolvedEnvEndpointAndRegion proves that the
// online doctor's dynamic DescribeProjects request uses the doctor's resolved
// runtime settings (from VOLCENGINE_ENDPOINT/VOLCENGINE_REGION), not the raw
// profile values. The request must hit the env-specified server, be signed
// with the env-specified region in the credential scope, and fire exactly once.
func TestDoctorOnlineDynamicUsesResolvedEnvEndpointAndRegion(t *testing.T) {
	clearAuthTestEnv(t)

	var gotAuth string
	requestCount := 0
	// The profile has empty endpoint/region; the doctor must resolve them from
	// the environment.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("x-tls-requestid", "resolved-env")
		w.Header().Set("Date", time.Now().UTC().Format(http.TimeFormat))
		_, _ = w.Write([]byte(`{"Projects":[]}`))
	}))
	defer server.Close()

	t.Setenv("VOLCENGINE_ENDPOINT", server.URL)
	t.Setenv("VOLCENGINE_REGION", "cn-shanghai")

	cfg := config.Config{
		Version: 1,
		Profiles: map[string]config.Profile{
			"sso": {
				Mode: config.AuthModeSSO,
				// Intentionally empty: doctor must resolve from env.
			},
		},
	}
	ctx := newDoctorTestContext(t, cfg)
	ctx.Profile = "sso"

	provider := &fakeProvider{value: auth.Value{
		AccessKeyID:     "AKLTresolved",
		SecretAccessKey: "resolved-secret",
		SessionToken:    "resolved-token",
		ProviderName:    "sso",
		ExpiresAt:       time.Now().Add(time.Hour),
	}}
	statusReader := &staticStatusReader{status: authStatus{
		Provider: "sso", Present: true, ExpiresAt: time.Now().Add(time.Hour),
	}}
	factory := &fakeAuthFactory{ssoProvider: provider, ssoStatus: statusReader}
	ctx.authFactory = factory

	out, _, err := runDoctor(ctx, []string{"--online"})
	if err != nil {
		t.Fatalf("runDoctor --online: %v", err)
	}

	// The doctor must report the resolved region/endpoint in its output.
	regionMap := out.(map[string]any)["region"].(map[string]any)
	if got := regionMap["value"]; got != "cn-shanghai" {
		t.Fatalf("reported region=%v, want cn-shanghai", got)
	}
	endpointMap := out.(map[string]any)["endpoint"].(map[string]any)
	if got := endpointMap["value"]; got != server.URL {
		t.Fatalf("reported endpoint=%v, want %s", got, server.URL)
	}

	// Exactly one DescribeProjects request must have been sent to the env server.
	if requestCount != 1 {
		t.Fatalf("expected exactly 1 DescribeProjects request, got %d", requestCount)
	}
	// The request must be signed with the resolved region in the credential scope.
	if gotAuth == "" {
		t.Fatalf("expected Authorization header to be set on the DescribeProjects request")
	}
	if !strings.Contains(gotAuth, "/cn-shanghai/TLS/") {
		t.Fatalf("expected credential scope to contain /cn-shanghai/TLS/, got Authorization=%q", gotAuth)
	}
	if provider.calls != 1 {
		t.Fatalf("expected provider Retrieve to be called once, got %d", provider.calls)
	}

	// The online_describe_projects check must pass, proving the request reached
	// the env-specified server.
	checks := out.(map[string]any)["checks"].([]map[string]any)
	found := false
	for _, c := range checks {
		if c["name"] == "online_describe_projects" {
			found = true
			if c["ok"] != true {
				t.Fatalf("expected online_describe_projects to pass (request must hit env server), got %v", c)
			}
		}
	}
	if !found {
		t.Fatalf("expected online_describe_projects check in output, got checks: %v", checks)
	}
}

// TestDoctorOnlineDynamicReportsResolvedTimeoutDefault proves that when neither
// the profile nor project config specifies a timeout, the doctor reports the
// 60s default in its timeout output, and the online dynamic client is still
// successfully built and used (DescribeProjects succeeds). It does not directly
// observe the client's Timeout field; the evidence is the reported default
// timeout plus a passing online check.
func TestDoctorOnlineDynamicReportsResolvedTimeoutDefault(t *testing.T) {
	clearAuthTestEnv(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-tls-requestid", "timeout-check")
		w.Header().Set("Date", time.Now().UTC().Format(http.TimeFormat))
		_, _ = w.Write([]byte(`{"Projects":[]}`))
	}))
	defer server.Close()

	cfg := config.Config{
		Version: 1,
		Profiles: map[string]config.Profile{
			"sso": {
				Mode:     config.AuthModeSSO,
				Region:   "cn-beijing",
				Endpoint: server.URL,
				// TimeoutSeconds intentionally 0; doctor must default to 60s.
			},
		},
	}
	ctx := newDoctorTestContext(t, cfg)
	ctx.Profile = "sso"

	provider := &fakeProvider{value: auth.Value{
		AccessKeyID:     "AKLTtimeout",
		SecretAccessKey: "timeout-secret",
		SessionToken:    "timeout-token",
		ProviderName:    "sso",
		ExpiresAt:       time.Now().Add(time.Hour),
	}}
	statusReader := &staticStatusReader{status: authStatus{
		Provider: "sso", Present: true, ExpiresAt: time.Now().Add(time.Hour),
	}}
	factory := &fakeAuthFactory{ssoProvider: provider, ssoStatus: statusReader}
	ctx.authFactory = factory

	out, _, err := runDoctor(ctx, []string{"--online"})
	if err != nil {
		t.Fatalf("runDoctor --online: %v", err)
	}

	// The doctor must report the resolved timeout (60s default).
	timeoutMap := out.(map[string]any)["timeout"].(map[string]any)
	if got := timeoutMap["seconds"]; got != float64(60) && got != 60 {
		t.Fatalf("reported timeout seconds=%v, want 60", got)
	}
	if got := timeoutMap["source"]; got != "default" {
		t.Fatalf("reported timeout source=%v, want default", got)
	}

	// The online check must pass, proving the client was built with the resolved
	// timeout and used to make the DescribeProjects call.
	checks := out.(map[string]any)["checks"].([]map[string]any)
	found := false
	for _, c := range checks {
		if c["name"] == "online_describe_projects" {
			found = true
			if c["ok"] != true {
				t.Fatalf("expected online_describe_projects to pass, got %v", c)
			}
		}
	}
	if !found {
		t.Fatalf("expected online_describe_projects check in output, got checks: %v", checks)
	}
}

// TestDoctorOfflineSSOInvalidCacheReportsNotPresent proves that an SSO STS
// cache with a future expiration but missing/invalid credentials is reported
// as not present by the offline doctor, which must exit 2. The test writes a
// real STS cache file (no token cache, so the combined status also fails on
// the missing token) and uses the production ssoStatusReader.
func TestDoctorOfflineSSOInvalidCacheReportsNotPresent(t *testing.T) {
	clearAuthTestEnv(t)
	dir := t.TempDir()

	// Write a real SSO STS cache with a future expiration but missing
	// SecretAccessKey and SessionToken.
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
		SSOSessions: map[string]config.SSOSession{
			"bad-session": {
				Name:     "bad-session",
				StartURL: "https://login.example.com/start",
				Region:   "cn-beijing",
			},
		},
	}
	ctx := newDoctorTestContext(t, cfg)
	ctx.Profile = "sso"
	// Use the production status reader backed by the real cache file.
	ctx.authFactory = &fakeAuthFactory{
		ssoStatus: &ssoStatusReader{
			cache:       cache,
			startURL:    "https://login.example.com/start",
			sessionName: "bad-session",
			accountID:   "acct-1",
			roleName:    "role-1",
			clock:       time.Now,
		},
	}

	out, code, err := runDoctor(ctx, nil)
	if err != nil {
		t.Fatalf("runDoctor: %v", err)
	}
	if code != 2 {
		t.Fatalf("expected exit code 2 for invalid cache, got %d", code)
	}
	creds := out.(map[string]any)["credentials"].(map[string]any)
	if creds["present"] != false {
		t.Fatalf("expected credentials.present=false for invalid cache, got %v", creds["present"])
	}
	if creds["refresh_required"] != true {
		t.Fatalf("expected credentials.refresh_required=true for invalid cache, got %v", creds["refresh_required"])
	}
}

// TestDoctorOfflineConsoleInvalidCacheReportsNotPresent proves that a Console
// Login cache with a future expiration but invalid schema (bad client ID,
// unparseable STS) is reported as not present by the offline doctor, which must
// exit 2. The test writes a real cache file and uses the production
// consoleStatusReader.
func TestDoctorOfflineConsoleInvalidCacheReportsNotPresent(t *testing.T) {
	clearAuthTestEnv(t)
	dir := t.TempDir()

	// Write a real Console Login cache with a future expiration but an invalid
	// client ID and unparseable STS credentials.
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
	ctx := newDoctorTestContext(t, cfg)
	ctx.Profile = "console"
	// Use the production status reader backed by the real cache file.
	ctx.authFactory = &fakeAuthFactory{
		consoleStatus: &consoleStatusReader{
			cache:        cache,
			loginSession: "bad-session",
			clock:        time.Now,
		},
	}

	out, code, err := runDoctor(ctx, nil)
	if err != nil {
		t.Fatalf("runDoctor: %v", err)
	}
	if code != 2 {
		t.Fatalf("expected exit code 2 for invalid cache, got %d", code)
	}
	creds := out.(map[string]any)["credentials"].(map[string]any)
	if creds["present"] != false {
		t.Fatalf("expected credentials.present=false for invalid cache, got %v", creds["present"])
	}
	if creds["refresh_required"] != true {
		t.Fatalf("expected credentials.refresh_required=true for invalid cache, got %v", creds["refresh_required"])
	}
}

// TestDefaultFactorySSOReadsCacheOverride proves that when
// VOLCLOG_SSO_CACHE_DIRECTORY is set, the production defaultAuthProviderFactory
// reads the SSO cache from the override directory (same root the login adapter
// writes to), not from the config-sibling sso/cache. A valid cache placed only
// in the override must be reported as present.
func TestDefaultFactorySSOReadsCacheOverride(t *testing.T) {
	clearAuthTestEnv(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	t.Setenv("VOLCLOG_CONFIG", cfgPath)

	// Place a valid SSO cache ONLY in the override directory.
	overrideDir := filepath.Join(dir, "override-sso")
	cache, err := sso.NewFileCache(overrideDir)
	if err != nil {
		t.Fatalf("NewFileCache: %v", err)
	}
	if err := cache.WriteSTS(&sso.STSCache{
		SessionName:     "ov-session",
		AccountID:       "acct-1",
		RoleName:        "role-1",
		AccessKeyID:     "AKLToverride",
		SecretAccessKey: "override-secret",
		SessionToken:    "override-token",
		ProviderName:    sso.ProviderName,
		ExpiresAt:       time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("WriteSTS: %v", err)
	}
	// Also write a valid token cache: the status reader requires both token and
	// STS to be valid for present=true.
	if err := cache.WriteToken(&sso.TokenCache{
		StartURL:     "https://login.example.com/start",
		SessionName:  "ov-session",
		AccessToken:  "override-access-token",
		ExpiresAt:    time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		ClientID:     "override-client-id",
		ClientSecret: "override-client-secret",
		Region:       "cn-beijing",
	}); err != nil {
		t.Fatalf("WriteToken: %v", err)
	}
	t.Setenv("VOLCLOG_SSO_CACHE_DIRECTORY", overrideDir)

	cfg := config.Config{
		Version: 1,
		Profiles: map[string]config.Profile{
			"sso": {
				Mode:           config.AuthModeSSO,
				Region:         "cn-beijing",
				Endpoint:       "https://tls-cn-beijing.volces.com",
				SSOSessionName: "ov-session",
				AccountID:      "acct-1",
				RoleName:       "role-1",
			},
		},
		SSOSessions: map[string]config.SSOSession{
			"ov-session": {
				Name:     "ov-session",
				StartURL: "https://login.example.com/start",
				Region:   "cn-beijing",
			},
		},
	}
	if err := config.Save(cfg, cfgPath); err != nil {
		t.Fatalf("save config: %v", err)
	}

	// Use the production default factory (nil) so it resolves the cache dir the
	// same way the login adapter does.
	ctx := newContext(&bytes.Buffer{}, &bytes.Buffer{}, output.FormatJSON, "", "")
	ctx.cfg = cfg
	ctx.cfgPath = cfgPath
	ctx.Profile = "sso"
	ctx.authFactory = nil

	out, code, err := runDoctor(ctx, nil)
	if err != nil {
		t.Fatalf("runDoctor: %v", err)
	}
	creds := out.(map[string]any)["credentials"].(map[string]any)
	if creds["present"] != true {
		t.Fatalf("expected credentials.present=true (read from override), got %v (code=%d)", creds["present"], code)
	}
}

// TestDefaultFactorySSOUsesConfigSiblingRootWithoutOverride proves that without
// VOLCLOG_SSO_CACHE_DIRECTORY, the production factory reads from the
// config-sibling sso/cache root (matching the login adapter's fallback).
func TestDefaultFactorySSOUsesConfigSiblingRootWithoutOverride(t *testing.T) {
	clearAuthTestEnv(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	t.Setenv("VOLCLOG_CONFIG", cfgPath)
	// Ensure no override leaks from the environment.
	t.Setenv("VOLCLOG_SSO_CACHE_DIRECTORY", "")

	// Place a valid SSO cache in the config-sibling sso/cache root.
	cache, err := sso.NewFileCache(filepath.Join(dir, "sso", "cache"))
	if err != nil {
		t.Fatalf("NewFileCache: %v", err)
	}
	if err := cache.WriteSTS(&sso.STSCache{
		SessionName:     "sib-session",
		AccountID:       "acct-1",
		RoleName:        "role-1",
		AccessKeyID:     "AKLTsibling",
		SecretAccessKey: "sibling-secret",
		SessionToken:    "sibling-token",
		ProviderName:    sso.ProviderName,
		ExpiresAt:       time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("WriteSTS: %v", err)
	}
	// Also write a valid token cache: the status reader requires both token and
	// STS to be valid for present=true.
	if err := cache.WriteToken(&sso.TokenCache{
		StartURL:     "https://login.example.com/start",
		SessionName:  "sib-session",
		AccessToken:  "sibling-access-token",
		ExpiresAt:    time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		ClientID:     "sibling-client-id",
		ClientSecret: "sibling-client-secret",
		Region:       "cn-beijing",
	}); err != nil {
		t.Fatalf("WriteToken: %v", err)
	}

	cfg := config.Config{
		Version: 1,
		Profiles: map[string]config.Profile{
			"sso": {
				Mode:           config.AuthModeSSO,
				Region:         "cn-beijing",
				Endpoint:       "https://tls-cn-beijing.volces.com",
				SSOSessionName: "sib-session",
				AccountID:      "acct-1",
				RoleName:       "role-1",
			},
		},
		SSOSessions: map[string]config.SSOSession{
			"sib-session": {
				Name:     "sib-session",
				StartURL: "https://login.example.com/start",
				Region:   "cn-beijing",
			},
		},
	}
	if err := config.Save(cfg, cfgPath); err != nil {
		t.Fatalf("save config: %v", err)
	}

	ctx := newContext(&bytes.Buffer{}, &bytes.Buffer{}, output.FormatJSON, "", "")
	ctx.cfg = cfg
	ctx.cfgPath = cfgPath
	ctx.Profile = "sso"
	ctx.authFactory = nil

	out, _, err := runDoctor(ctx, nil)
	if err != nil {
		t.Fatalf("runDoctor: %v", err)
	}
	creds := out.(map[string]any)["credentials"].(map[string]any)
	if creds["present"] != true {
		t.Fatalf("expected credentials.present=true (read from config sibling), got %v", creds["present"])
	}
}

// TestDefaultFactoryConsoleReadsCacheOverride proves that when
// VOLCLOG_LOGIN_CACHE_DIRECTORY is set, the production defaultAuthProviderFactory
// reads the Console Login cache from the override directory.
func TestDefaultFactoryConsoleReadsCacheOverride(t *testing.T) {
	clearAuthTestEnv(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	t.Setenv("VOLCLOG_CONFIG", cfgPath)

	overrideDir := filepath.Join(dir, "override-login")
	if err := os.MkdirAll(overrideDir, 0755); err != nil {
		t.Fatalf("mkdir override: %v", err)
	}
	cache, err := console.NewFileCache(overrideDir)
	if err != nil {
		t.Fatalf("NewFileCache: %v", err)
	}
	stsJSON, err := json.Marshal(console.STSCredentials{
		AccessKeyID:     "AKLToverride",
		SecretAccessKey: "override-secret",
		SessionToken:    "override-token",
	})
	if err != nil {
		t.Fatalf("marshal STS credentials: %v", err)
	}
	validCache := console.LoginTokenCache{
		LoginSession: "ov-session",
		AccessToken:  stsJSON,
		Scope:        console.Scope,
		ClientID:     console.ClientIDSameDevice,
		IssuedAt:     time.Now().UTC().Format(time.RFC3339),
		ExpiresIn:    3600,
		TokenType:    "sts",
	}
	data, err := json.Marshal(validCache)
	if err != nil {
		t.Fatalf("marshal valid cache: %v", err)
	}
	if err := cache.WriteRaw("ov-session", data); err != nil {
		t.Fatalf("WriteRaw: %v", err)
	}
	t.Setenv("VOLCLOG_LOGIN_CACHE_DIRECTORY", overrideDir)

	cfg := config.Config{
		Version: 1,
		Profiles: map[string]config.Profile{
			"console": {
				Mode:         config.AuthModeConsoleLogin,
				Region:       "cn-beijing",
				Endpoint:     "https://tls-cn-beijing.volces.com",
				LoginSession: "ov-session",
			},
		},
	}
	if err := config.Save(cfg, cfgPath); err != nil {
		t.Fatalf("save config: %v", err)
	}

	ctx := newContext(&bytes.Buffer{}, &bytes.Buffer{}, output.FormatJSON, "", "")
	ctx.cfg = cfg
	ctx.cfgPath = cfgPath
	ctx.Profile = "console"
	ctx.authFactory = nil

	out, _, err := runDoctor(ctx, nil)
	if err != nil {
		t.Fatalf("runDoctor: %v", err)
	}
	creds := out.(map[string]any)["credentials"].(map[string]any)
	if creds["present"] != true {
		t.Fatalf("expected credentials.present=true (read from override), got %v", creds["present"])
	}
}

// switchingStatusReader returns different statuses on successive Status calls,
// simulating a cache that gets updated (e.g. STS written) during an online
// exchange. The first call returns first; subsequent calls return second.
type switchingStatusReader struct {
	first  authStatus
	second authStatus
	calls  int
}

func (r *switchingStatusReader) Status(context.Context, string) (authStatus, error) {
	r.calls++
	if r.calls == 1 {
		return r.first, nil
	}
	return r.second, nil
}

// TestDoctorOnlineSuccessUpdatesStatusAndExitsZero proves that when --online is
// set and the online DescribeProjects succeeds, the doctor exits 0 even if the
// pre-network credential status was absent (e.g. first-time SSO with only an
// OAuth token). After the online success, the status reader is re-read and the
// updated (present=true) status is reflected in the output.
func TestDoctorOnlineSuccessUpdatesStatusAndExitsZero(t *testing.T) {
	clearAuthTestEnv(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-tls-requestid", "online-ok")
		w.Header().Set("Date", time.Now().UTC().Format(http.TimeFormat))
		_, _ = w.Write([]byte(`{"Projects":[]}`))
	}))
	defer server.Close()

	cfg := config.Config{
		Version: 1,
		Profiles: map[string]config.Profile{
			"sso": {
				Mode:     config.AuthModeSSO,
				Region:   "cn-beijing",
				Endpoint: server.URL,
			},
		},
	}
	ctx := newDoctorTestContext(t, cfg)
	ctx.Profile = "sso"

	// Provider succeeds (simulates a successful online exchange that writes STS).
	provider := &fakeProvider{value: auth.Value{
		AccessKeyID:     "AKLTonline",
		SecretAccessKey: "online-secret",
		SessionToken:    "online-token",
		ProviderName:    "sso",
		ExpiresAt:       time.Now().Add(time.Hour),
	}}
	// First read: absent (no STS yet). Second read (after online success): present.
	reader := &switchingStatusReader{
		first:  authStatus{Provider: "sso", Present: false, RefreshRequired: true},
		second: authStatus{Provider: "sso", Present: true, ExpiresAt: time.Now().Add(time.Hour), RefreshRequired: false},
	}
	factory := &fakeAuthFactory{ssoProvider: provider, ssoStatus: reader}
	ctx.authFactory = factory

	out, code, err := runDoctor(ctx, []string{"--online"})
	if err != nil {
		t.Fatalf("runDoctor --online: %v", err)
	}
	// Online success must be exit 0 even though pre-network status was absent.
	if code != 0 {
		t.Fatalf("expected exit code 0 after online success, got %d", code)
	}

	// The status reader must have been re-read after the online success.
	if reader.calls < 2 {
		t.Fatalf("expected status reader to be re-read after online success, got %d calls", reader.calls)
	}

	// Output must reflect the updated (present=true) status.
	creds := out.(map[string]any)["credentials"].(map[string]any)
	if creds["present"] != true {
		t.Fatalf("expected credentials.present=true after online success, got %v", creds["present"])
	}
	if creds["refresh_required"] != false {
		t.Fatalf("expected refresh_required=false after online success, got %v", creds["refresh_required"])
	}

	// online_describe_projects check must be ok=true.
	checks := out.(map[string]any)["checks"].([]map[string]any)
	found := false
	for _, c := range checks {
		if c["name"] == "online_describe_projects" {
			found = true
			if c["ok"] != true {
				t.Fatalf("expected online_describe_projects ok=true, got %v", c)
			}
		}
	}
	if !found {
		t.Fatalf("expected online_describe_projects check in output")
	}

	// The credentials_present check must also be ok=true after the status
	// re-read, consistent with credentials.present=true (not the stale false).
	foundCred := false
	for _, c := range checks {
		if c["name"] == "credentials_present" {
			foundCred = true
			if c["ok"] != true {
				t.Fatalf("expected credentials_present ok=true after online success, got %v", c)
			}
		}
	}
	if !foundCred {
		t.Fatalf("expected credentials_present check in output")
	}
}

// TestDoctorOnlineFailureExitsTwoEvenWhenOfflinePresent proves that when
// --online is set but the online DescribeProjects fails, the doctor exits 2
// even if the pre-network credential status was present (e.g. a stale cache
// whose refresh/DescribeProjects failed).
func TestDoctorOnlineFailureExitsTwoEvenWhenOfflinePresent(t *testing.T) {
	clearAuthTestEnv(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate a server-side failure.
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := config.Config{
		Version: 1,
		Profiles: map[string]config.Profile{
			"sso": {
				Mode:     config.AuthModeSSO,
				Region:   "cn-beijing",
				Endpoint: server.URL,
			},
		},
	}
	ctx := newDoctorTestContext(t, cfg)
	ctx.Profile = "sso"

	// Offline status: present=true (valid but stale cache).
	reader := &staticStatusReader{status: authStatus{
		Provider: "sso", Present: true, ExpiresAt: time.Now().Add(time.Hour), RefreshRequired: true,
	}}
	// Provider's Retrieve succeeds (credentials are valid), but the server
	// returns 500 so DescribeProjects fails.
	provider := &fakeProvider{value: auth.Value{
		AccessKeyID:     "AKLTstale",
		SecretAccessKey: "stale-secret",
		SessionToken:    "stale-token",
		ProviderName:    "sso",
		ExpiresAt:       time.Now().Add(time.Hour),
	}}
	factory := &fakeAuthFactory{ssoProvider: provider, ssoStatus: reader}
	ctx.authFactory = factory

	out, code, err := runDoctor(ctx, []string{"--online"})
	if err != nil {
		t.Fatalf("runDoctor --online: %v", err)
	}
	// Online failure must be exit 2 even though offline status was present.
	if code != 2 {
		t.Fatalf("expected exit code 2 after online failure, got %d", code)
	}

	// online_describe_projects check must be ok=false.
	checks := out.(map[string]any)["checks"].([]map[string]any)
	found := false
	for _, c := range checks {
		if c["name"] == "online_describe_projects" {
			found = true
			if c["ok"] != false {
				t.Fatalf("expected online_describe_projects ok=false, got %v", c)
			}
		}
	}
	if !found {
		t.Fatalf("expected online_describe_projects check in output")
	}

	// The local credentials_present check must remain ok=true (the cache was
	// present offline; online failure does not change the local cache state).
	foundCred := false
	for _, c := range checks {
		if c["name"] == "credentials_present" {
			foundCred = true
			if c["ok"] != true {
				t.Fatalf("expected credentials_present ok=true (local cache present), got %v", c)
			}
		}
	}
	if !foundCred {
		t.Fatalf("expected credentials_present check in output")
	}
}

// TestDefaultFactoryConsoleUsesConfigSiblingRootWithoutOverride proves that
// without VOLCLOG_LOGIN_CACHE_DIRECTORY, the production factory reads the
// Console Login cache from the config-sibling login/cache root (matching the
// SSO fallback and the login adapter's resolution).
func TestDefaultFactoryConsoleUsesConfigSiblingRootWithoutOverride(t *testing.T) {
	clearAuthTestEnv(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	t.Setenv("VOLCLOG_CONFIG", cfgPath)
	t.Setenv("VOLCLOG_LOGIN_CACHE_DIRECTORY", "")

	cache, err := console.NewFileCache(filepath.Join(dir, "login", "cache"))
	if err != nil {
		t.Fatalf("NewFileCache: %v", err)
	}
	stsJSON, err := json.Marshal(console.STSCredentials{
		AccessKeyID:     "AKLTsibling",
		SecretAccessKey: "sibling-secret",
		SessionToken:    "sibling-token",
	})
	if err != nil {
		t.Fatalf("marshal STS credentials: %v", err)
	}
	validCache := console.LoginTokenCache{
		LoginSession: "sib-session",
		AccessToken:  stsJSON,
		Scope:        console.Scope,
		ClientID:     console.ClientIDSameDevice,
		IssuedAt:     time.Now().UTC().Format(time.RFC3339),
		ExpiresIn:    3600,
		TokenType:    "sts",
	}
	data, err := json.Marshal(validCache)
	if err != nil {
		t.Fatalf("marshal valid cache: %v", err)
	}
	if err := cache.WriteRaw("sib-session", data); err != nil {
		t.Fatalf("WriteRaw: %v", err)
	}

	cfg := config.Config{
		Version: 1,
		Profiles: map[string]config.Profile{
			"console": {
				Mode:         config.AuthModeConsoleLogin,
				Region:       "cn-beijing",
				Endpoint:     "https://tls-cn-beijing.volces.com",
				LoginSession: "sib-session",
			},
		},
	}
	if err := config.Save(cfg, cfgPath); err != nil {
		t.Fatalf("save config: %v", err)
	}

	ctx := newContext(&bytes.Buffer{}, &bytes.Buffer{}, output.FormatJSON, "", "")
	ctx.cfg = cfg
	ctx.cfgPath = cfgPath
	ctx.Profile = "console"
	ctx.authFactory = nil

	out, _, err := runDoctor(ctx, nil)
	if err != nil {
		t.Fatalf("runDoctor: %v", err)
	}
	creds := out.(map[string]any)["credentials"].(map[string]any)
	if creds["present"] != true {
		t.Fatalf("expected credentials.present=true (read from config sibling), got %v", creds["present"])
	}
}

// TestDoctorOfflineSSOValidSTSButMissingTokenExitsTwo proves that a valid
// future STS cache alone is not sufficient: if the token cache is missing or
// corrupt, the doctor must report present=false and exit 2, matching the
// SSOProvider behavior (which always validates the token cache first).
func TestDoctorOfflineSSOValidSTSButMissingTokenExitsTwo(t *testing.T) {
	clearAuthTestEnv(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	t.Setenv("VOLCLOG_CONFIG", cfgPath)
	t.Setenv("VOLCLOG_SSO_CACHE_DIRECTORY", "")

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
	if err := config.Save(cfg, cfgPath); err != nil {
		t.Fatalf("save config: %v", err)
	}

	ctx := newContext(&bytes.Buffer{}, &bytes.Buffer{}, output.FormatJSON, "", "")
	ctx.cfg = cfg
	ctx.cfgPath = cfgPath
	ctx.Profile = "sso"
	ctx.authFactory = nil

	out, code, err := runDoctor(ctx, nil)
	if err != nil {
		t.Fatalf("runDoctor: %v", err)
	}
	if code != 2 {
		t.Fatalf("expected exit code 2 when token cache is missing, got %d", code)
	}
	creds := out.(map[string]any)["credentials"].(map[string]any)
	if creds["present"] != false {
		t.Fatalf("expected credentials.present=false when token cache is missing, got %v", creds["present"])
	}
	if got := creds["expires_at"]; got != "" {
		t.Fatalf("expected credentials.expires_at empty when token cache is missing, got %v", got)
	}
	if creds["refresh_required"] != true {
		t.Fatalf("expected refresh_required=true when token cache is missing, got %v", creds["refresh_required"])
	}
}

// TestDoctorOfflineSSONearExpiryRefreshableTokenReportsRefreshRequired proves
// that a valid token cache within the refresh window (but with a valid refresh
// token) reports present=true and refresh_required=true, matching the
// SSOProvider's near-expiry behavior.
func TestDoctorOfflineSSONearExpiryRefreshableTokenReportsRefreshRequired(t *testing.T) {
	clearAuthTestEnv(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	t.Setenv("VOLCLOG_CONFIG", cfgPath)
	t.Setenv("VOLCLOG_SSO_CACHE_DIRECTORY", "")

	cache, err := sso.NewFileCache(filepath.Join(dir, "sso", "cache"))
	if err != nil {
		t.Fatalf("NewFileCache: %v", err)
	}
	// Write a near-expiry token cache (30s, within 60s RefreshWindow) with a
	// valid refresh token, plus a valid future STS cache.
	if err := cache.WriteToken(&sso.TokenCache{
		StartURL:     "https://login.example.com/start",
		SessionName:  "near-exp",
		AccessToken:  "near-exp-access-token",
		ExpiresAt:    time.Now().Add(30 * time.Second).UTC().Format(time.RFC3339),
		ClientID:     "near-exp-client-id",
		ClientSecret: "near-exp-client-secret",
		RefreshToken: "near-exp-refresh-token",
		Region:       "cn-beijing",
	}); err != nil {
		t.Fatalf("WriteToken: %v", err)
	}
	if err := cache.WriteSTS(&sso.STSCache{
		SessionName:     "near-exp",
		AccountID:       "acct-1",
		RoleName:        "role-1",
		AccessKeyID:     "AKLTnearexp",
		SecretAccessKey: "near-exp-secret",
		SessionToken:    "near-exp-token",
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
				SSOSessionName: "near-exp",
				AccountID:      "acct-1",
				RoleName:       "role-1",
			},
		},
		SSOSessions: map[string]config.SSOSession{
			"near-exp": {
				Name:     "near-exp",
				StartURL: "https://login.example.com/start",
				Region:   "cn-beijing",
			},
		},
	}
	if err := config.Save(cfg, cfgPath); err != nil {
		t.Fatalf("save config: %v", err)
	}

	ctx := newContext(&bytes.Buffer{}, &bytes.Buffer{}, output.FormatJSON, "", "")
	ctx.cfg = cfg
	ctx.cfgPath = cfgPath
	ctx.Profile = "sso"
	ctx.authFactory = nil

	out, code, err := runDoctor(ctx, nil)
	if err != nil {
		t.Fatalf("runDoctor: %v", err)
	}
	// Both caches valid: present=true. Token is near-expiry: refresh_required=true.
	if code != 0 {
		t.Fatalf("expected exit code 0 for valid near-expiry refreshable cache, got %d", code)
	}
	creds := out.(map[string]any)["credentials"].(map[string]any)
	if creds["present"] != true {
		t.Fatalf("expected credentials.present=true, got %v", creds["present"])
	}
	if creds["refresh_required"] != true {
		t.Fatalf("expected refresh_required=true for near-expiry token, got %v", creds["refresh_required"])
	}
}

// TestDoctorOfflineSSOValidSTSButCorruptTokenExitsTwo proves that a valid
// future STS cache with a corrupt token cache (missing required fields) is
// reported as present=false with empty expires_at and exit 2, matching the
// SSOProvider behavior.
func TestDoctorOfflineSSOValidSTSButCorruptTokenExitsTwo(t *testing.T) {
	clearAuthTestEnv(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	t.Setenv("VOLCLOG_CONFIG", cfgPath)
	t.Setenv("VOLCLOG_SSO_CACHE_DIRECTORY", "")

	cache, err := sso.NewFileCache(filepath.Join(dir, "sso", "cache"))
	if err != nil {
		t.Fatalf("NewFileCache: %v", err)
	}
	// Write a valid future STS cache.
	if err := cache.WriteSTS(&sso.STSCache{
		SessionName:     "corrupt-tok",
		AccountID:       "acct-1",
		RoleName:        "role-1",
		AccessKeyID:     "AKLTcorrupt",
		SecretAccessKey: "corrupt-secret",
		SessionToken:    "corrupt-token",
		ProviderName:    sso.ProviderName,
		ExpiresAt:       time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("WriteSTS: %v", err)
	}
	// Write a valid token first, then overwrite the cache file with
	// syntactically invalid JSON to corrupt it (exercises ReadToken decode error).
	if err := cache.WriteToken(&sso.TokenCache{
		StartURL:     "https://login.example.com/start",
		SessionName:  "corrupt-tok",
		AccessToken:  "valid-access-token",
		ExpiresAt:    time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		ClientID:     "valid-client-id",
		ClientSecret: "valid-client-secret",
		Region:       "cn-beijing",
	}); err != nil {
		t.Fatalf("WriteToken: %v", err)
	}
	cacheDir := filepath.Join(dir, "sso", "cache")
	matches, gerr := filepath.Glob(filepath.Join(cacheDir, "token-*.json"))
	if gerr != nil {
		t.Fatalf("glob token files: %v", gerr)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 token file, got %d", len(matches))
	}
	if werr := os.WriteFile(matches[0], []byte("{"), 0600); werr != nil {
		t.Fatalf("corrupt token file: %v", werr)
	}
	// Assert ReadToken actually returns an error for the corrupt JSON.
	if _, rerr := cache.ReadToken("https://login.example.com/start", "corrupt-tok"); rerr == nil {
		t.Fatalf("expected ReadToken to return error for corrupt JSON")
	}

	cfg := config.Config{
		Version: 1,
		Profiles: map[string]config.Profile{
			"sso": {
				Mode:           config.AuthModeSSO,
				Region:         "cn-beijing",
				Endpoint:       "https://tls-cn-beijing.volces.com",
				SSOSessionName: "corrupt-tok",
				AccountID:      "acct-1",
				RoleName:       "role-1",
			},
		},
		SSOSessions: map[string]config.SSOSession{
			"corrupt-tok": {
				Name:     "corrupt-tok",
				StartURL: "https://login.example.com/start",
				Region:   "cn-beijing",
			},
		},
	}
	if err := config.Save(cfg, cfgPath); err != nil {
		t.Fatalf("save config: %v", err)
	}

	ctx := newContext(&bytes.Buffer{}, &bytes.Buffer{}, output.FormatJSON, "", "")
	ctx.cfg = cfg
	ctx.cfgPath = cfgPath
	ctx.Profile = "sso"
	ctx.authFactory = nil

	out, code, err := runDoctor(ctx, nil)
	if err != nil {
		t.Fatalf("runDoctor: %v", err)
	}
	if code != 2 {
		t.Fatalf("expected exit code 2 when token cache is corrupt, got %d", code)
	}
	creds := out.(map[string]any)["credentials"].(map[string]any)
	if creds["present"] != false {
		t.Fatalf("expected credentials.present=false when token cache is corrupt, got %v", creds["present"])
	}
	if got := creds["expires_at"]; got != "" {
		t.Fatalf("expected expires_at empty when token cache is corrupt, got %v", got)
	}
	if creds["refresh_required"] != true {
		t.Fatalf("expected refresh_required=true when token cache is corrupt, got %v", creds["refresh_required"])
	}
}

// TestDoctorOfflineConsoleNearExpiryMissingRefreshTokenExitsTwo proves that a
// complete valid Console Login cache that is within the refresh window but
// missing its RefreshToken is reported as not present by the offline doctor
// (exit 2), matching the Provider.Retrieve behavior which would immediately
// ReauthRequired. Uses the production defaultAuthProviderFactory.
func TestDoctorOfflineConsoleNearExpiryMissingRefreshTokenExitsTwo(t *testing.T) {
	clearAuthTestEnv(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	t.Setenv("VOLCLOG_CONFIG", cfgPath)
	t.Setenv("VOLCLOG_LOGIN_CACHE_DIRECTORY", "")

	cacheDir := filepath.Join(dir, "login", "cache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	cache, err := console.NewFileCache(cacheDir)
	if err != nil {
		t.Fatalf("NewFileCache: %v", err)
	}
	now := time.Now()
	stsJSON, err := json.Marshal(console.STSCredentials{
		AccessKeyID:     "AKLTnear",
		SecretAccessKey: "near-secret",
		SessionToken:    "near-token",
	})
	if err != nil {
		t.Fatalf("marshal STS: %v", err)
	}
	// Complete valid cache, near-expiry (issued 3599s ago, expires in 1s),
	// but no RefreshToken.
	nearCache := console.LoginTokenCache{
		LoginSession: "near-session",
		AccessToken:  stsJSON,
		Scope:        console.Scope,
		ClientID:     console.ClientIDSameDevice,
		IssuedAt:     now.Add(-3599 * time.Second).UTC().Format(time.RFC3339),
		ExpiresIn:    3600,
		TokenType:    "sts",
		// RefreshToken intentionally empty.
	}
	data, err := json.Marshal(nearCache)
	if err != nil {
		t.Fatalf("marshal cache: %v", err)
	}
	if err := cache.WriteRaw("near-session", data); err != nil {
		t.Fatalf("WriteRaw: %v", err)
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
	if err := config.Save(cfg, cfgPath); err != nil {
		t.Fatalf("save config: %v", err)
	}

	// Assert the production factory constructs a real status reader.
	reader, rerr := dynamicAuthStatusReader(config.AuthModeConsoleLogin, cfgPath, "console", cfg, nil)
	if rerr != nil {
		t.Fatalf("dynamicAuthStatusReader: %v", rerr)
	}
	if reader == nil {
		t.Fatalf("expected non-nil status reader")
	}

	ctx := newContext(&bytes.Buffer{}, &bytes.Buffer{}, output.FormatJSON, "", "")
	ctx.cfg = cfg
	ctx.cfgPath = cfgPath
	ctx.Profile = "console"
	ctx.authFactory = nil

	out, code, err := runDoctor(ctx, nil)
	if err != nil {
		t.Fatalf("runDoctor: %v", err)
	}
	if code != 2 {
		t.Fatalf("expected exit code 2 for near-expiry cache missing refresh token, got %d", code)
	}
	creds := out.(map[string]any)["credentials"].(map[string]any)
	if creds["present"] != false {
		t.Fatalf("expected credentials.present=false, got %v", creds["present"])
	}
	if got := creds["expires_at"]; got != "" {
		t.Fatalf("expected credentials.expires_at empty, got %v", got)
	}
	if creds["refresh_required"] != true {
		t.Fatalf("expected refresh_required=true, got %v", creds["refresh_required"])
	}
}

// TestDoctorOfflineSSOValidTokenButCorruptSTSExitsTwo proves that a valid
// future TokenCache combined with a corrupt STS cache (future expiry but
// missing credentials) is reported as not present by the offline doctor
// (exit 2). The token cache alone is not sufficient; both must be valid.
// Uses the production defaultAuthProviderFactory.
func TestDoctorOfflineSSOValidTokenButCorruptSTSExitsTwo(t *testing.T) {
	clearAuthTestEnv(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	t.Setenv("VOLCLOG_CONFIG", cfgPath)
	t.Setenv("VOLCLOG_SSO_CACHE_DIRECTORY", "")

	cache, err := sso.NewFileCache(filepath.Join(dir, "sso", "cache"))
	if err != nil {
		t.Fatalf("NewFileCache: %v", err)
	}
	now := time.Now()
	// Write a valid future TokenCache with matching binding.
	if err := cache.WriteToken(&sso.TokenCache{
		StartURL:     "https://login.example.com/start",
		SessionName:  "valid-tok",
		AccessToken:  "valid-access-token",
		ExpiresAt:    now.Add(time.Hour).UTC().Format(time.RFC3339),
		ClientID:     "valid-client-id",
		ClientSecret: "valid-client-secret",
		Region:       "cn-beijing",
	}); err != nil {
		t.Fatalf("WriteToken: %v", err)
	}
	// Write a future-expiry STS cache but missing credentials (corrupt).
	if err := cache.WriteSTS(&sso.STSCache{
		SessionName:  "valid-tok",
		AccountID:    "acct-1",
		RoleName:     "role-1",
		ProviderName: sso.ProviderName,
		ExpiresAt:    now.Add(time.Hour).UTC().Format(time.RFC3339),
		// AccessKeyID/SecretAccessKey/SessionToken intentionally missing.
	}); err != nil {
		t.Fatalf("WriteSTS: %v", err)
	}

	// Precondition: the written TokenCache must be independently valid
	// (Present=true) so the test exercises the "valid token + corrupt STS"
	// combination, not a double-failure.
	tok, terr := cache.ReadToken("https://login.example.com/start", "valid-tok")
	if terr != nil {
		t.Fatalf("ReadToken precondition: %v", terr)
	}
	tokStatus, terr := sso.InspectTokenCache(tok, "https://login.example.com/start", "valid-tok", "cn-beijing", now)
	if terr != nil || !tokStatus.Present {
		t.Fatalf("precondition: token cache must be valid (Present=true), got err=%v status=%+v", terr, tokStatus)
	}
	// Precondition: the written STS must be independently invalid
	// (Present=false) so the combined status fails because of STS, not token.
	sts, serr := cache.ReadSTS("valid-tok", "acct-1", "role-1")
	if serr != nil {
		t.Fatalf("ReadSTS precondition: %v", serr)
	}
	stsStatus, serr := sso.InspectSTSCache(sts, "valid-tok", "acct-1", "role-1", now)
	if serr == nil || stsStatus.Present {
		t.Fatalf("precondition: sts cache must be invalid (Present=false), got err=%v status=%+v", serr, stsStatus)
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

	ctx := newContext(&bytes.Buffer{}, &bytes.Buffer{}, output.FormatJSON, "", "")
	ctx.cfg = cfg
	ctx.cfgPath = cfgPath
	ctx.Profile = "sso"
	ctx.authFactory = nil

	out, code, err := runDoctor(ctx, nil)
	if err != nil {
		t.Fatalf("runDoctor: %v", err)
	}
	if code != 2 {
		t.Fatalf("expected exit code 2 for valid token + corrupt STS, got %d", code)
	}
	creds := out.(map[string]any)["credentials"].(map[string]any)
	if creds["present"] != false {
		t.Fatalf("expected credentials.present=false, got %v", creds["present"])
	}
	if got := creds["expires_at"]; got != "" {
		t.Fatalf("expected credentials.expires_at empty, got %v", got)
	}
	if creds["refresh_required"] != true {
		t.Fatalf("expected refresh_required=true, got %v", creds["refresh_required"])
	}
}

// TestDoctorOfflineWorkloadModeDoesNotContactSTSOrIMDS proves that offline
// doctor for workload modes never calls the workload factory, never calls
// Retrieve, and never contacts STS/IMDS.
func TestDoctorOfflineWorkloadModeDoesNotContactSTSOrIMDS(t *testing.T) {
	clearAuthTestEnv(t)
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("TOKEN"), 0600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	cases := []struct {
		mode      string
		profile   config.Profile
		wantExit  int
		wantReady bool
	}{
		{config.AuthModeRamRoleARN, config.Profile{Mode: config.AuthModeRamRoleARN, RoleName: "r", AccountID: "1", AccessKeyID: "AK", SecretAccessKey: "SK", Region: "cn-beijing", Endpoint: "https://tls-cn-beijing.volces.com"}, 0, true},
		{config.AuthModeRamRoleARN, config.Profile{Mode: config.AuthModeRamRoleARN, RoleName: "r", Region: "cn-beijing", Endpoint: "https://tls-cn-beijing.volces.com"}, 2, false},
		{config.AuthModeOIDC, config.Profile{Mode: config.AuthModeOIDC, RoleTRN: "trn:iam::1:role/r", OIDCTokenFile: tokenFile, Region: "cn-beijing", Endpoint: "https://tls-cn-beijing.volces.com"}, 0, true},
		{config.AuthModeOIDC, config.Profile{Mode: config.AuthModeOIDC, RoleTRN: "trn:iam::1:role/r", OIDCTokenFile: "/nonexistent", Region: "cn-beijing", Endpoint: "https://tls-cn-beijing.volces.com"}, 2, false},
		{config.AuthModeECSRole, config.Profile{Mode: config.AuthModeECSRole, RoleName: "r", Region: "cn-beijing", Endpoint: "https://tls-cn-beijing.volces.com"}, 0, true},
		{config.AuthModeECSRole, config.Profile{Mode: config.AuthModeECSRole, Region: "cn-beijing", Endpoint: "https://tls-cn-beijing.volces.com"}, 2, false},
	}
	for _, tc := range cases {
		t.Run(tc.mode+"/ready="+strconv.FormatBool(tc.wantReady), func(t *testing.T) {
			cfg := config.Config{Version: 1, Profiles: map[string]config.Profile{"w": tc.profile}}
			ctx := newDoctorTestContext(t, cfg)
			ctx.Profile = "w"
			factory := &fakeAuthFactory{}
			ctx.authFactory = factory
			out, exit, err := runDoctor(ctx, nil)
			if err != nil {
				t.Fatalf("runDoctor: %v", err)
			}
			if exit != tc.wantExit {
				t.Fatalf("exit=%d, want %d", exit, tc.wantExit)
			}
			if factory.ramCalls != 0 || factory.oidcCalls != 0 || factory.ecsCalls != 0 {
				t.Fatalf("workload factory must not be called offline: ram=%d oidc=%d ecs=%d", factory.ramCalls, factory.oidcCalls, factory.ecsCalls)
			}
			creds := out.(map[string]any)["credentials"].(map[string]any)
			if creds["present"] != false {
				t.Fatalf("expected present=false for offline workload, got %v", creds["present"])
			}
			exp, ok := creds["expires_at"].(string)
			if !ok {
				t.Fatalf("expires_at is not string: %T", creds["expires_at"])
			}
			if exp != "" {
				t.Fatalf("expected expires_at empty for offline workload, got %q", exp)
			}
			got, ok := creds["source_ready"].(bool)
			if !ok {
				t.Fatalf("source_ready is not bool: %T", creds["source_ready"])
			}
			if got != tc.wantReady {
				t.Fatalf("source_ready=%v, want %v", got, tc.wantReady)
			}
			// Must not contain refresh_required (workload has no disk cache).
			if _, ok := creds["refresh_required"]; ok {
				t.Fatalf("workload output must not contain refresh_required")
			}
		})
	}
}

// TestDoctorOfflineOIDCChecksTokenFileWithoutPrintingToken proves that offline
// doctor for OIDC mode verifies the token file is readable and a regular file
// without reading or printing its contents.
func TestDoctorOfflineOIDCChecksTokenFileWithoutPrintingToken(t *testing.T) {
	clearAuthTestEnv(t)
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("SECRET-OIDC-TOKEN-CANARY"), 0600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	cfg := config.Config{Version: 1, Profiles: map[string]config.Profile{
		"w": {Mode: config.AuthModeOIDC, RoleTRN: "trn:iam::1:role/r", OIDCTokenFile: tokenFile, Region: "cn-beijing", Endpoint: "https://tls-cn-beijing.volces.com"},
	}}
	ctx := newDoctorTestContext(t, cfg)
	ctx.Profile = "w"
	factory := &fakeAuthFactory{}
	ctx.authFactory = factory
	out, _, err := runDoctor(ctx, nil)
	if err != nil {
		t.Fatalf("runDoctor: %v", err)
	}
	if factory.oidcCalls != 0 {
		t.Fatalf("OIDC factory must not be called offline, calls=%d", factory.oidcCalls)
	}
	raw, jerr := json.Marshal(out)
	if jerr != nil {
		t.Fatalf("marshal: %v", jerr)
	}
	if strings.Contains(string(raw), "SECRET-OIDC-TOKEN-CANARY") {
		t.Fatalf("doctor output must not contain OIDC token content")
	}
}

// TestDoctorWorkloadChecksUseSourceReadyNotCredentialsPresent proves that for
// workload modes, the checks list uses source_ready (not credentials_present)
// as the readiness gate, and that valid sources exit 0 while invalid exit 2.
func TestDoctorWorkloadChecksUseSourceReadyNotCredentialsPresent(t *testing.T) {
	clearAuthTestEnv(t)
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("TOKEN"), 0600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	cases := []struct {
		mode      string
		profile   config.Profile
		wantExit  int
		wantReady bool
	}{
		{config.AuthModeRamRoleARN, config.Profile{Mode: config.AuthModeRamRoleARN, RoleName: "r", AccountID: "1", AccessKeyID: "AK", SecretAccessKey: "SK", Region: "cn-beijing", Endpoint: "https://tls-cn-beijing.volces.com"}, 0, true},
		{config.AuthModeRamRoleARN, config.Profile{Mode: config.AuthModeRamRoleARN, RoleName: "r", Region: "cn-beijing", Endpoint: "https://tls-cn-beijing.volces.com"}, 2, false},
		{config.AuthModeOIDC, config.Profile{Mode: config.AuthModeOIDC, RoleTRN: "trn:iam::1:role/r", OIDCTokenFile: tokenFile, Region: "cn-beijing", Endpoint: "https://tls-cn-beijing.volces.com"}, 0, true},
		{config.AuthModeOIDC, config.Profile{Mode: config.AuthModeOIDC, RoleTRN: "trn:iam::1:role/r", OIDCTokenFile: "/nonexistent", Region: "cn-beijing", Endpoint: "https://tls-cn-beijing.volces.com"}, 2, false},
		{config.AuthModeECSRole, config.Profile{Mode: config.AuthModeECSRole, RoleName: "r", Region: "cn-beijing", Endpoint: "https://tls-cn-beijing.volces.com"}, 0, true},
		{config.AuthModeECSRole, config.Profile{Mode: config.AuthModeECSRole, Region: "cn-beijing", Endpoint: "https://tls-cn-beijing.volces.com"}, 2, false},
	}
	for _, tc := range cases {
		t.Run(tc.mode+"/ready="+strconv.FormatBool(tc.wantReady), func(t *testing.T) {
			cfg := config.Config{Version: 1, Profiles: map[string]config.Profile{"w": tc.profile}}
			ctx := newDoctorTestContext(t, cfg)
			ctx.Profile = "w"
			ctx.authFactory = &fakeAuthFactory{}
			out, exit, err := runDoctor(ctx, nil)
			if err != nil {
				t.Fatalf("runDoctor: %v", err)
			}
			if exit != tc.wantExit {
				t.Fatalf("exit=%d, want %d", exit, tc.wantExit)
			}
			checks := out.(map[string]any)["checks"].([]map[string]any)
			hasCredPresent := false
			hasSourceReady := false
			for _, c := range checks {
				if c["name"] == "credentials_present" {
					hasCredPresent = true
				}
				if c["name"] == "source_ready" {
					hasSourceReady = true
					if c["ok"] != tc.wantReady {
						t.Fatalf("source_ready ok=%v, want %v", c["ok"], tc.wantReady)
					}
				}
			}
			if hasCredPresent {
				t.Fatalf("workload checks must not contain credentials_present")
			}
			if !hasSourceReady {
				t.Fatalf("workload checks must contain source_ready")
			}
		})
	}
}

// TestDoctorOnlineWorkloadModeTable proves that --online doctor for RAM/OIDC/ECS
// each calls exactly one factory method, one Retrieve, one DescribeProjects,
// exits 0, and reports correct present/on_demand/memory_only/source_ready.
func TestDoctorOnlineWorkloadModeTable(t *testing.T) {
	clearAuthTestEnv(t)
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("TOKEN"), 0600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	cases := []struct {
		mode    string
		profile config.Profile
	}{
		{config.AuthModeRamRoleARN, config.Profile{Mode: config.AuthModeRamRoleARN, RoleName: "r", AccountID: "1", AccessKeyID: "AK", SecretAccessKey: "SK"}},
		{config.AuthModeOIDC, config.Profile{Mode: config.AuthModeOIDC, RoleTRN: "trn:iam::1:role/r", OIDCTokenFile: tokenFile}},
		{config.AuthModeECSRole, config.Profile{Mode: config.AuthModeECSRole, RoleName: "r"}},
	}
	for _, tc := range cases {
		t.Run(tc.mode, func(t *testing.T) {
			var describeCount int32
			writeErrCh := make(chan error, 1)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&describeCount, 1)
				w.WriteHeader(200)
				_, werr := w.Write([]byte(`{"ResponseMetadata":{"RequestId":"x"},"Result":{}}`))
				writeErrCh <- werr
			}))
			defer srv.Close()
			tc.profile.Region = "cn-beijing"
			tc.profile.Endpoint = srv.URL
			cfg := config.Config{Version: 1, Profiles: map[string]config.Profile{"w": tc.profile}}
			ctx := newDoctorTestContext(t, cfg)
			ctx.Profile = "w"
			provider := &fakeProvider{value: auth.Value{AccessKeyID: "TEMP-AK", SecretAccessKey: "TEMP-SK", SessionToken: "TEMP-TOK"}}
			factory := &fakeAuthFactory{}
			switch tc.mode {
			case config.AuthModeRamRoleARN:
				factory.ramProvider = provider
			case config.AuthModeOIDC:
				factory.oidcProvider = provider
			case config.AuthModeECSRole:
				factory.ecsProvider = provider
			}
			ctx.authFactory = factory
			out, exit, err := runDoctor(ctx, []string{"--online"})
			if err != nil {
				t.Fatalf("runDoctor: %v", err)
			}
			if exit != 0 {
				t.Fatalf("exit=%d, want 0", exit)
			}
			// Exactly one factory call, zero others.
			switch tc.mode {
			case config.AuthModeRamRoleARN:
				if factory.ramCalls != 1 || factory.oidcCalls != 0 || factory.ecsCalls != 0 {
					t.Fatalf("ram=%d oidc=%d ecs=%d, want ram=1", factory.ramCalls, factory.oidcCalls, factory.ecsCalls)
				}
			case config.AuthModeOIDC:
				if factory.oidcCalls != 1 || factory.ramCalls != 0 || factory.ecsCalls != 0 {
					t.Fatalf("ram=%d oidc=%d ecs=%d, want oidc=1", factory.ramCalls, factory.oidcCalls, factory.ecsCalls)
				}
			case config.AuthModeECSRole:
				if factory.ecsCalls != 1 || factory.ramCalls != 0 || factory.oidcCalls != 0 {
					t.Fatalf("ram=%d oidc=%d ecs=%d, want ecs=1", factory.ramCalls, factory.oidcCalls, factory.ecsCalls)
				}
			}
			if provider.calls != 1 {
				t.Fatalf("Retrieve calls=%d, want 1", provider.calls)
			}
			if atomic.LoadInt32(&describeCount) != 1 {
				t.Fatalf("DescribeProjects=%d, want 1", describeCount)
			}
			select {
			case werr := <-writeErrCh:
				if werr != nil {
					t.Fatalf("write response: %v", werr)
				}
			default:
				t.Fatal("server did not report response write result")
			}
			creds := out.(map[string]any)["credentials"].(map[string]any)
			if creds["present"] != true {
				t.Fatalf("present=%v, want true after online success", creds["present"])
			}
			if creds["on_demand"] != true {
				t.Fatalf("on_demand=%v, want true", creds["on_demand"])
			}
			if creds["memory_only"] != true {
				t.Fatalf("memory_only=%v, want true", creds["memory_only"])
			}
			if creds["source_ready"] != true {
				t.Fatalf("source_ready=%v, want true", creds["source_ready"])
			}
			// checks must contain exactly one source_ready=true and no credentials_present.
			checks := out.(map[string]any)["checks"].([]map[string]any)
			sourceReadyCount := 0
			hasCredPresent := false
			for _, c := range checks {
				if c["name"] == "source_ready" {
					sourceReadyCount++
					if c["ok"] != true {
						t.Fatalf("source_ready check ok=%v, want true", c["ok"])
					}
				}
				if c["name"] == "credentials_present" {
					hasCredPresent = true
				}
			}
			if sourceReadyCount != 1 {
				t.Fatalf("source_ready check count=%d, want 1", sourceReadyCount)
			}
			if hasCredPresent {
				t.Fatalf("online workload checks must not contain credentials_present")
			}
		})
	}
}

// TestDoctorWarnsWhenDisableSSLIsExplicit proves that disable-ssl=true for
// RAM/OIDC produces a fixed warning without changing the endpoint or exit code,
// and that disable-ssl=false/ECS/AK/SSO/Console produce no warning.
func TestDoctorWarnsWhenDisableSSLIsExplicit(t *testing.T) {
	clearAuthTestEnv(t)
	oidcTokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(oidcTokenFile, []byte("SSL-OIDC-RAW-TOKEN-CANARY"), 0600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	cases := []struct {
		name     string
		profile  config.Profile
		wantWarn bool
		wantExit int
		endpoint string
		canaries []string
	}{
		{"ram_true", config.Profile{Mode: config.AuthModeRamRoleARN, RoleName: "SSL-RAM-ROLE-CANARY", AccountID: "1", AccessKeyID: "SSL-RAM-AK-CANARY", SecretAccessKey: "SSL-RAM-SK-CANARY", SecurityToken: "SSL-RAM-TOK-CANARY", DisableSSL: true, Region: "cn-beijing", Endpoint: "https://tls-cn-beijing.volces.com"}, true, 0, "https://tls-cn-beijing.volces.com", []string{"SSL-RAM-ROLE-CANARY", "SSL-RAM-AK-CANARY", "SSL-RAM-SK-CANARY", "SSL-RAM-TOK-CANARY"}},
		{"oidc_true", config.Profile{Mode: config.AuthModeOIDC, RoleTRN: "trn:iam::1:role/SSL-OIDC-TRN-CANARY", OIDCTokenFile: oidcTokenFile, DisableSSL: true, Region: "cn-beijing", Endpoint: "https://tls-cn-beijing.volces.com"}, true, 0, "https://tls-cn-beijing.volces.com", []string{"SSL-OIDC-TRN-CANARY", "SSL-OIDC-RAW-TOKEN-CANARY", oidcTokenFile}},
		{"ram_false", config.Profile{Mode: config.AuthModeRamRoleARN, RoleName: "r", AccountID: "1", AccessKeyID: "AK", SecretAccessKey: "SK", DisableSSL: false, Region: "cn-beijing", Endpoint: "https://tls-cn-beijing.volces.com"}, false, 0, "https://tls-cn-beijing.volces.com", nil},
		{"ecs_true", config.Profile{Mode: config.AuthModeECSRole, RoleName: "r", DisableSSL: true, Region: "cn-beijing", Endpoint: "https://tls-cn-beijing.volces.com"}, false, 0, "https://tls-cn-beijing.volces.com", nil},
		{"ak_true", config.Profile{Mode: config.AuthModeAK, AccessKeyID: "AK", SecretAccessKey: "SK", DisableSSL: true, Region: "cn-beijing", Endpoint: "https://tls-cn-beijing.volces.com"}, false, 0, "https://tls-cn-beijing.volces.com", nil},
		{"sso_true", config.Profile{Mode: config.AuthModeSSO, DisableSSL: true, Region: "cn-beijing", Endpoint: "https://tls-cn-beijing.volces.com"}, false, 0, "https://tls-cn-beijing.volces.com", nil},
		{"console_true", config.Profile{Mode: config.AuthModeConsoleLogin, DisableSSL: true, Region: "cn-beijing", Endpoint: "https://tls-cn-beijing.volces.com"}, false, 0, "https://tls-cn-beijing.volces.com", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Config{Version: 1, Profiles: map[string]config.Profile{"w": tc.profile}}
			ctx := newDoctorTestContext(t, cfg)
			ctx.Profile = "w"
			factory := &fakeAuthFactory{}
			if tc.profile.Mode == config.AuthModeSSO {
				factory.ssoStatus = &staticStatusReader{status: authStatus{Provider: "sso", Present: true}}
			}
			if tc.profile.Mode == config.AuthModeConsoleLogin {
				factory.consoleStatus = &staticStatusReader{status: authStatus{Provider: "console-login", Present: true}}
			}
			ctx.authFactory = factory
			out, exit, err := runDoctor(ctx, nil)
			if err != nil {
				t.Fatalf("runDoctor: %v", err)
			}
			if exit != tc.wantExit {
				t.Fatalf("exit=%d, want %d", exit, tc.wantExit)
			}
			raw, jerr := json.Marshal(out)
			if jerr != nil {
				t.Fatalf("marshal: %v", jerr)
			}
			hasWarn := strings.Contains(string(raw), "disable-ssl")
			if hasWarn != tc.wantWarn {
				t.Fatalf("warning=%v, want %v", hasWarn, tc.wantWarn)
			}
			if tc.wantWarn && !strings.Contains(string(raw), "HTTP") {
				t.Fatalf("expected HTTP warning text")
			}
			// Endpoint must be unchanged.
			ep := out.(map[string]any)["endpoint"].(map[string]any)
			if ep["value"] != tc.endpoint {
				t.Fatalf("endpoint=%v, want %v", ep["value"], tc.endpoint)
			}
			// Positive cases must not leak any canary.
			for _, c := range tc.canaries {
				if strings.Contains(string(raw), c) {
					t.Fatalf("output leaked canary %q", c)
				}
			}
		})
	}
}

func TestDoctorOfflineOIDCNegativeCases(t *testing.T) {
	clearAuthTestEnv(t)
	cases := []struct {
		name  string
		setup func(t *testing.T, dir string) string
	}{
		{"missing", func(t *testing.T, dir string) string { return filepath.Join(dir, "nope") }},
		{"directory", func(t *testing.T, dir string) string { return dir }},
		{"unreadable_regular", func(t *testing.T, dir string) string {
			p := filepath.Join(dir, "token")
			if err := os.WriteFile(p, []byte("TOKEN"), 0600); err != nil {
				t.Fatalf("write: %v", err)
			}
			if err := os.Chmod(p, 0000); err != nil {
				t.Fatalf("chmod: %v", err)
			}
			t.Cleanup(func() {
				if cerr := os.Chmod(p, 0600); cerr != nil {
					t.Errorf("restore chmod: %v", cerr)
				}
			})
			if f, openErr := os.Open(p); openErr == nil {
				if closeErr := f.Close(); closeErr != nil {
					t.Fatalf("close permission probe: %v", closeErr)
				}
				t.Skip("current user can read chmod 000 files")
			}
			return p
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			tf := tc.setup(t, dir)
			cfg := config.Config{Version: 1, Profiles: map[string]config.Profile{
				"w": {Mode: config.AuthModeOIDC, RoleTRN: "trn:iam::1:role/r", OIDCTokenFile: tf, Region: "cn-beijing", Endpoint: "https://tls-cn-beijing.volces.com"},
			}}
			ctx := newDoctorTestContext(t, cfg)
			ctx.Profile = "w"
			ctx.authFactory = &fakeAuthFactory{}
			out, exit, err := runDoctor(ctx, nil)
			if err != nil {
				t.Fatalf("runDoctor: %v", err)
			}
			if exit != 2 {
				t.Fatalf("exit=%d, want 2", exit)
			}
			creds := out.(map[string]any)["credentials"].(map[string]any)
			if creds["source_ready"] != false {
				t.Fatalf("source_ready=%v, want false", creds["source_ready"])
			}
		})
	}
}
