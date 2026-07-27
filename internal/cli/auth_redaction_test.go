package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth"
	"github.com/volcengine-tls/ve-tls-cli/internal/auth/console"
	"github.com/volcengine-tls/ve-tls-cli/internal/auth/sso"
	"github.com/volcengine-tls/ve-tls-cli/internal/config"
	"github.com/volcengine-tls/ve-tls-cli/internal/output"
)

// authCanaries are the unique secret values that must never appear in any CLI
// output (error strings, structured envelopes, stdout/stderr, doctor, trace,
// configure show/list, or callback HTML).
var authCanaries = []string{
	"secret_access_key_canary",
	"session_token_canary",
	"oauth_access_token_canary",
	"refresh_token_canary",
	"authorization_code_canary",
	"pkce_verifier_canary",
}

// assertNoCanaries scans all provided strings and fails the test if any canary
// value appears in any of them.
func assertNoCanaries(t *testing.T, label string, blobs ...string) {
	t.Helper()
	for _, blob := range blobs {
		for _, c := range authCanaries {
			if strings.Contains(blob, c) {
				t.Fatalf("%s: output contains canary %q:\n%s", label, c, blob)
			}
		}
	}
}

// canarySTSProvider returns a fakeProvider whose value carries canary secrets
// in the SecretAccessKey and SessionToken fields. If a request is signed, the
// canary would be incorporated into the Authorization header.
func canarySTSProvider() *fakeProvider {
	return &fakeProvider{value: auth.Value{
		AccessKeyID:     "AKLTcanary-ak",
		SecretAccessKey: "secret_access_key_canary",
		SessionToken:    "session_token_canary",
		ProviderName:    "sso",
		ExpiresAt:       time.Now().Add(time.Hour),
	}}
}

// canaryConfig builds a config with a single sso profile pointed at the given
// endpoint.
func canaryConfig(endpoint string) config.Config {
	return config.Config{
		Version: 1,
		Profiles: map[string]config.Profile{
			"dyn": {
				Mode:     config.AuthModeSSO,
				Region:   "cn-beijing",
				Endpoint: endpoint,
			},
		},
	}
}

// writeCanarySSOCache writes a real SSO FileCache with canary values in both
// the token cache (OAuth access/refresh tokens) and the STS cache (secret
// access key / session token). The cache is rooted at dir/sso/cache.
func writeCanarySSOCache(t *testing.T, dir string) {
	t.Helper()
	cache, err := sso.NewFileCache(filepath.Join(dir, "sso", "cache"))
	if err != nil {
		t.Fatalf("NewFileCache: %v", err)
	}
	if err := cache.WriteToken(&sso.TokenCache{
		StartURL:     "https://login.example.com/start",
		SessionName:  "canary-session",
		AccessToken:  "oauth_access_token_canary",
		RefreshToken: "refresh_token_canary",
		ExpiresAt:    time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		ClientID:     "canary-client",
		Region:       "cn-beijing",
	}); err != nil {
		t.Fatalf("WriteToken: %v", err)
	}
	if err := cache.WriteSTS(&sso.STSCache{
		SessionName:     "canary-session",
		AccountID:       "acct-1",
		RoleName:        "role-1",
		AccessKeyID:     "AKLTcanary-ak",
		SecretAccessKey: "secret_access_key_canary",
		SessionToken:    "session_token_canary",
		ProviderName:    "sso",
		ExpiresAt:       time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("WriteSTS: %v", err)
	}
}

// TestRedaction_DoctorOnlineHidesSecrets proves that an online doctor for a
// dynamic profile signs a real DescribeProjects request with canary STS
// credentials (verifying the secret actually enters the request path via
// Authorization and X-Security-Token), but the doctor output (out map + stdout
// serialized through the production output.Write path + stderr) never contains
// the canary values.
func TestRedaction_DoctorOnlineHidesSecrets(t *testing.T) {
	clearAuthTestEnv(t)
	var gotAuth, gotToken string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotToken = r.Header.Get("X-Security-Token")
		w.Header().Set("x-tls-requestid", "online-canary")
		w.Header().Set("Date", time.Now().UTC().Format(http.TimeFormat))
		_, _ = w.Write([]byte(`{"Projects":[]}`))
	}))
	defer server.Close()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	cfg := canaryConfig(server.URL)
	if err := config.Save(cfg, cfgPath); err != nil {
		t.Fatalf("save config: %v", err)
	}
	t.Setenv("VOLCLOG_CONFIG", cfgPath)

	var stdout, stderr bytes.Buffer
	ctx := newContext(&stdout, &stderr, output.FormatJSON, "", "")
	ctx.cfg = cfg
	ctx.cfgPath = cfgPath
	ctx.Profile = "dyn"
	ctx.authFactory = &fakeAuthFactory{
		ssoProvider: canarySTSProvider(),
		ssoStatus: &staticStatusReader{status: authStatus{
			Provider:        "sso",
			Present:         true,
			ExpiresAt:       time.Now().Add(time.Hour),
			RefreshRequired: false,
		}},
	}

	out, _, err := runDoctor(ctx, []string{"--online"})
	if err != nil {
		t.Fatalf("runDoctor --online: %v", err)
	}

	// The canary STS credentials must have been used to sign the real request.
	if gotAuth == "" {
		t.Fatalf("expected online doctor to send an Authorization header")
	}
	if gotToken != "session_token_canary" {
		t.Fatalf("expected X-Security-Token to be the canary session token, got %q", gotToken)
	}

	// Serialize the output through the production output path so stdout is
	// non-empty and exercises the real rendering code.
	if werr := output.Write(&stdout, out, output.FormatJSON); werr != nil {
		t.Fatalf("output.Write: %v", werr)
	}
	if stdout.Len() == 0 {
		t.Fatalf("expected stdout to be non-empty after output.Write")
	}

	raw, _ := json.Marshal(out)
	assertNoCanaries(t, "doctor online", string(raw), stdout.String(), stderr.String())
}

// TestRedaction_DoctorOfflineMetadataOnly proves that an offline doctor for a
// dynamic profile reads only cache metadata (via the status reader) and never
// calls Retrieve. A real SSO cache is seeded with canary OAuth/STS values; the
// doctor output must not contain them, and the provider's Retrieve must not be
// invoked.
func TestRedaction_DoctorOfflineMetadataOnly(t *testing.T) {
	clearAuthTestEnv(t)
	dir := t.TempDir()
	writeCanarySSOCache(t, dir)

	cfgPath := filepath.Join(dir, "config.json")
	cfg := canaryConfig("https://tls-cn-beijing.volces.com")
	if err := config.Save(cfg, cfgPath); err != nil {
		t.Fatalf("save config: %v", err)
	}
	t.Setenv("VOLCLOG_CONFIG", cfgPath)

	retrieveCalled := false
	provider := &fakeProvider{}
	provider.retrieveFn = func() (auth.Value, error) {
		retrieveCalled = true
		return auth.Value{SecretAccessKey: "secret_access_key_canary"}, nil
	}

	var stdout, stderr bytes.Buffer
	ctx := newContext(&stdout, &stderr, output.FormatJSON, "", "")
	ctx.cfg = cfg
	ctx.cfgPath = cfgPath
	ctx.Profile = "dyn"
	// Use the real default factory so the status reader reads the actual cache
	// file seeded above. The provider is never called in offline mode.
	ctx.authFactory = &fakeAuthFactory{
		ssoProvider: provider,
		ssoStatus: &ssoStatusReader{
			cache:       mustOpenSSOCache(t, filepath.Join(dir, "sso", "cache")),
			startURL:    "https://login.example.com/start",
			sessionName: "canary-session",
			accountID:   "acct-1",
			roleName:    "role-1",
			clock:       time.Now,
		},
	}

	out, _, err := runDoctor(ctx, nil)
	if err != nil {
		t.Fatalf("runDoctor: %v", err)
	}
	if retrieveCalled {
		t.Fatalf("offline doctor must not call Retrieve")
	}
	// Serialize through the production output path so stdout is non-empty.
	if werr := output.Write(&stdout, out, output.FormatJSON); werr != nil {
		t.Fatalf("output.Write: %v", werr)
	}
	if stdout.Len() == 0 {
		t.Fatalf("expected stdout to be non-empty after output.Write")
	}
	raw, _ := json.Marshal(out)
	assertNoCanaries(t, "doctor offline", string(raw), stdout.String(), stderr.String())
}

// mustOpenSSOCache opens a real SSO FileCache at dir, failing the test on error.
func mustOpenSSOCache(t *testing.T, dir string) *sso.FileCache {
	t.Helper()
	c, err := sso.NewFileCache(dir)
	if err != nil {
		t.Fatalf("NewFileCache(%s): %v", dir, err)
	}
	return c
}

// TestRedaction_ConfigureShowHidesSecrets proves that configure show for a
// dynamic profile whose cache holds canary OAuth/STS values never exposes them
// in the output.
func TestRedaction_ConfigureShowHidesSecrets(t *testing.T) {
	clearAuthTestEnv(t)
	dir := t.TempDir()
	writeCanarySSOCache(t, dir)

	cfg := canaryConfig("https://tls-cn-beijing.volces.com")
	var stdout, stderr bytes.Buffer
	ctx := newContext(&stdout, &stderr, output.FormatJSON, "", "")
	ctx.cfg = cfg
	ctx.cfgPath = filepath.Join(dir, "config.json")
	ctx.Profile = "dyn"
	ctx.authFactory = &fakeAuthFactory{
		ssoProvider: canarySTSProvider(),
		ssoStatus: &ssoStatusReader{
			cache:       mustOpenSSOCache(t, filepath.Join(dir, "sso", "cache")),
			startURL:    "https://login.example.com/start",
			sessionName: "canary-session",
			accountID:   "acct-1",
			roleName:    "role-1",
			clock:       time.Now,
		},
	}

	out, err := configureShow(ctx, []string{"--profile", "dyn"})
	if err != nil {
		t.Fatalf("configureShow: %v", err)
	}
	if werr := output.Write(&stdout, out, output.FormatJSON); werr != nil {
		t.Fatalf("output.Write: %v", werr)
	}
	if stdout.Len() == 0 {
		t.Fatalf("expected stdout to be non-empty after output.Write")
	}
	raw, _ := json.Marshal(out)
	assertNoCanaries(t, "configure show", string(raw), stdout.String(), stderr.String())
}

// TestRedaction_ConfigureListHidesSecrets proves that configure list for a
// dynamic profile whose cache holds canary values never exposes them.
func TestRedaction_ConfigureListHidesSecrets(t *testing.T) {
	clearAuthTestEnv(t)
	dir := t.TempDir()
	writeCanarySSOCache(t, dir)

	cfg := canaryConfig("https://tls-cn-beijing.volces.com")
	var stdout, stderr bytes.Buffer
	ctx := newContext(&stdout, &stderr, output.FormatJSON, "", "")
	ctx.cfg = cfg
	ctx.cfgPath = filepath.Join(dir, "config.json")
	ctx.authFactory = &fakeAuthFactory{
		ssoProvider: canarySTSProvider(),
		ssoStatus: &ssoStatusReader{
			cache:       mustOpenSSOCache(t, filepath.Join(dir, "sso", "cache")),
			startURL:    "https://login.example.com/start",
			sessionName: "canary-session",
			accountID:   "acct-1",
			roleName:    "role-1",
			clock:       time.Now,
		},
	}

	out, err := configureList(ctx, nil)
	if err != nil {
		t.Fatalf("configureList: %v", err)
	}
	if werr := output.Write(&stdout, out, output.FormatJSON); werr != nil {
		t.Fatalf("output.Write: %v", werr)
	}
	if stdout.Len() == 0 {
		t.Fatalf("expected stdout to be non-empty after output.Write")
	}
	raw, _ := json.Marshal(out)
	assertNoCanaries(t, "configure list", string(raw), stdout.String(), stderr.String())
}

// TestRedaction_ErrorEnvelopeDoesNotRenderCause proves that when an auth.Error
// has a Cause containing all canary values, neither the Error() string nor the
// structured envelope renders the Cause. The auth package intentionally never
// renders Cause in Error(); this test locks that contract at the CLI layer.
func TestRedaction_ErrorEnvelopeDoesNotRenderCause(t *testing.T) {
	cause := canaryCauseError(strings.Join(authCanaries, "|"))
	err := &auth.Error{
		Kind:        auth.ReauthRequired,
		Description: "sso token cache missing; run: volclog sso login",
		Cause:       cause,
	}
	// Raw error string must not contain canaries (Cause is not rendered).
	assertNoCanaries(t, "raw error string", err.Error())

	// Structured envelope must not contain canaries.
	var stderr bytes.Buffer
	writeStructuredError(&bytes.Buffer{}, &stderr, &dynamicAuthError{mode: config.AuthModeSSO, err: err}, "", 0, "tool", nil)
	assertNoCanaries(t, "error envelope", stderr.String())
}

// TestRedaction_RequestFailureDoesNotLeakCause proves that when a dynamic
// provider's Retrieve returns an error whose Cause contains canary values, the
// error surfaced to the caller and the trace file do not contain the canaries.
func TestRedaction_RequestFailureDoesNotLeakCause(t *testing.T) {
	clearAuthTestEnv(t)
	traceDir := t.TempDir()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := canaryConfig(server.URL)
	ctx := newContext(&bytes.Buffer{}, &bytes.Buffer{}, output.FormatJSON, "", "")
	ctx.cfg = cfg
	ctx.cfgPath = "/tmp/test-config.json"
	ctx.Profile = "dyn"
	ctx.TraceDir = traceDir

	cause := canaryCauseError(strings.Join(authCanaries, "|"))
	provider := &fakeProvider{}
	provider.retrieveFn = func() (auth.Value, error) {
		return auth.Value{}, &auth.Error{
			Kind:        auth.ReauthRequired,
			Description: "sso token cache missing; run: volclog sso login",
			Cause:       cause,
		}
	}
	ctx.authFactory = &fakeAuthFactory{ssoProvider: provider}

	_, err := ctx.DoRaw("GET", "/DescribeProjects", nil, nil, nil)
	if err == nil {
		t.Fatalf("expected DoRaw to fail")
	}
	// The surfaced error must not contain canaries.
	assertNoCanaries(t, "request failure error", err.Error())
	ctx.Close()

	// The trace file must not contain canaries either.
	entries, rerr := os.ReadDir(traceDir)
	if rerr != nil {
		t.Fatalf("read trace dir: %v", rerr)
	}
	for _, e := range entries {
		data, rerr := os.ReadFile(filepath.Join(traceDir, e.Name()))
		if rerr != nil {
			t.Fatalf("read trace file: %v", rerr)
		}
		assertNoCanaries(t, "trace file "+e.Name(), string(data))
	}
}

// TestRedaction_TraceSuccessfulSigningDoesNotWriteCredentials proves that when
// a dynamic request succeeds (signed with canary STS credentials), the trace
// file does not contain the Authorization header or X-Security-Token value.
// Trace only stores redacted header keys and body SHA256, never raw headers.
func TestRedaction_TraceSuccessfulSigningDoesNotWriteCredentials(t *testing.T) {
	clearAuthTestEnv(t)
	traceDir := t.TempDir()

	server, ch := newCapturingServer(t)

	cfg := canaryConfig(server.URL)
	ctx := newContext(&bytes.Buffer{}, &bytes.Buffer{}, output.FormatJSON, "", "")
	ctx.cfg = cfg
	ctx.cfgPath = "/tmp/test-config.json"
	ctx.Profile = "dyn"
	ctx.TraceDir = traceDir
	ctx.authFactory = &fakeAuthFactory{ssoProvider: canarySTSProvider()}

	if _, err := ctx.DoRaw("GET", "/DescribeProjects", nil, nil, nil); err != nil {
		t.Fatalf("DoRaw: %v", err)
	}
	ctx.Close()

	// The canary STS credentials were used to sign the request (sanity check).
	cr := <-ch
	if cr.authorization == "" {
		t.Fatalf("expected Authorization header to be set on the request")
	}

	// The trace file must not contain the canary secret or the raw
	// Authorization/Security-Token values.
	entries, err := os.ReadDir(traceDir)
	if err != nil {
		t.Fatalf("read trace dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("expected trace file to be created")
	}
	for _, e := range entries {
		data, rerr := os.ReadFile(filepath.Join(traceDir, e.Name()))
		if rerr != nil {
			t.Fatalf("read trace file: %v", rerr)
		}
		content := string(data)
		assertNoCanaries(t, "trace file "+e.Name(), content)
		// Trace must store only redacted header keys, not the Authorization value.
		if strings.Contains(content, "AKLTcanary-ak") {
			t.Fatalf("trace file %s contains the access key id:\n%s", e.Name(), content)
		}
	}
}

// TestRedaction_ConsoleCallbackHTMLHidesSecrets proves that the Console Login
// callback server, when sent a request whose code and state parameters carry
// all canary values, delivers them to Wait() but does not echo any of them in
// the HTML response body.
func TestRedaction_ConsoleCallbackHTMLHidesSecrets(t *testing.T) {
	cs, err := console.NewCallbackServer(nil)
	if err != nil {
		t.Fatalf("NewCallbackServer: %v", err)
	}
	cs.Start()
	defer cs.Close()

	// Inject ALL canaries into the code and state query parameters so every
	// secret type is exercised through the callback entry point.
	allCanaries := strings.Join(authCanaries, "-")
	callbackURL := cs.RedirectURI() +
		"?code=" + allCanaries +
		"&state=" + allCanaries
	resp, err := http.Get(callbackURL)
	if err != nil {
		t.Fatalf("GET callback: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read callback body: %v", err)
	}
	if len(body) == 0 {
		t.Fatalf("expected non-empty HTML response body")
	}

	// Wait must receive the canary-laden code and state.
	waitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, werr := cs.Wait(waitCtx)
	if werr != nil {
		t.Fatalf("Wait: %v", werr)
	}
	if result.Code != allCanaries {
		t.Fatalf("Wait code=%q, want %q", result.Code, allCanaries)
	}
	if result.State != allCanaries {
		t.Fatalf("Wait state=%q, want %q", result.State, allCanaries)
	}

	// The HTML body must NOT echo any canary.
	assertNoCanaries(t, "callback html", string(body))
}

// TestRedaction_ConsoleCallbackHTMLHidesErrorQueryCanaries proves that the
// callback HTML page never echoes raw OAuth error/error_description query
// parameters, while the internal AuthorizationResult still carries them with
// the existing error priority (error > Error > error_description) intact.
func TestRedaction_ConsoleCallbackHTMLHidesErrorQueryCanaries(t *testing.T) {
	cases := []struct {
		name      string
		query     string
		wantError string
	}{
		{
			name:      "error canary",
			query:     "?error=" + "authorization_code_canary",
			wantError: "authorization_code_canary",
		},
		{
			name:      "Error canary",
			query:     "?Error=" + "refresh_token_canary",
			wantError: "refresh_token_canary",
		},
		{
			name:      "error_description canary",
			query:     "?error_description=" + "oauth_access_token_canary",
			wantError: "oauth_access_token_canary",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cs, err := console.NewCallbackServer(nil)
			if err != nil {
				t.Fatalf("NewCallbackServer: %v", err)
			}
			cs.Start()
			defer cs.Close()

			callbackURL := cs.RedirectURI() + tc.query
			resp, err := http.Get(callbackURL)
			if err != nil {
				t.Fatalf("GET callback: %v", err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("read callback body: %v", err)
			}
			if len(body) == 0 {
				t.Fatalf("expected non-empty HTML response body")
			}

			// The HTML body must NOT echo the canary.
			assertNoCanaries(t, "callback html", string(body))

			// The internal result must still carry the error for flow judgment.
			waitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			result, werr := cs.Wait(waitCtx)
			if werr != nil {
				t.Fatalf("Wait: %v", werr)
			}
			if result.Error != tc.wantError {
				t.Fatalf("Wait error=%q, want %q", result.Error, tc.wantError)
			}
		})
	}
}

// canaryCauseError is a simple error type used as a Cause carrying canary text.
type canaryCauseError string

func (e canaryCauseError) Error() string { return string(e) }

// TestWorkloadDoctorRedactAllCredentialCanaries proves that doctor output never
// leaks source/temp credentials, role/TRN/token path, or OIDC raw token for
// RAM/OIDC/ECS workload modes. It only exercises the doctor path; trace and
// error-envelope redaction are covered by dedicated tests.
func TestWorkloadDoctorRedactAllCredentialCanaries(t *testing.T) {
	clearAuthTestEnv(t)

	t.Run("ram_online_doctor_no_leak", func(t *testing.T) {
		var gotToken string
		writeErrCh := make(chan error, 1)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotToken = r.Header.Get("X-Security-Token")
			w.WriteHeader(200)
			_, werr := w.Write([]byte(`{"ResponseMetadata":{"RequestId":"x"},"Result":{}}`))
			writeErrCh <- werr
		}))
		defer srv.Close()
		canaries := []string{"SRC-AK-1", "SRC-SK-2", "SRC-TOK-3", "TEMP-AK-4", "TEMP-SK-5", "TEMP-TOK-6", "secret-role"}
		cfg := config.Config{Version: 1, Profiles: map[string]config.Profile{
			"ram": {Mode: config.AuthModeRamRoleARN, RoleName: "secret-role", AccountID: "1", AccessKeyID: "SRC-AK-1", SecretAccessKey: "SRC-SK-2", SecurityToken: "SRC-TOK-3", Region: "cn-beijing", Endpoint: srv.URL},
		}}
		ctx := newDoctorTestContext(t, cfg)
		ctx.Profile = "ram"
		provider := &fakeProvider{value: auth.Value{AccessKeyID: "TEMP-AK-4", SecretAccessKey: "TEMP-SK-5", SessionToken: "TEMP-TOK-6"}}
		ctx.authFactory = &fakeAuthFactory{ramProvider: provider}
		out, exit, err := runDoctor(ctx, []string{"--online"})
		if err != nil {
			t.Fatalf("runDoctor: %v", err)
		}
		if exit != 0 {
			t.Fatalf("exit=%d, want 0", exit)
		}
		select {
		case werr := <-writeErrCh:
			if werr != nil {
				t.Fatalf("write response: %v", werr)
			}
		default:
			t.Fatal("server did not report response write result")
		}
		if gotToken != "TEMP-TOK-6" {
			t.Fatalf("server did not receive provider token; sanity check failed")
		}
		raw, jerr := json.Marshal(out)
		if jerr != nil {
			t.Fatalf("marshal: %v", jerr)
		}
		for _, c := range canaries {
			if strings.Contains(string(raw), c) {
				t.Fatalf("doctor output leaked canary %q", c)
			}
		}
	})

	t.Run("oidc_offline_doctor_no_leak", func(t *testing.T) {
		tokenFile := filepath.Join(t.TempDir(), "token")
		if werr := os.WriteFile(tokenFile, []byte("OIDC-RAW-TOKEN-CANARY"), 0600); werr != nil {
			t.Fatalf("write token: %v", werr)
		}
		canaries := []string{"OIDC-RAW-TOKEN-CANARY", "trn:iam::1:role/secret-trn", tokenFile}
		cfg := config.Config{Version: 1, Profiles: map[string]config.Profile{
			"oidc": {Mode: config.AuthModeOIDC, RoleTRN: "trn:iam::1:role/secret-trn", OIDCTokenFile: tokenFile, Region: "cn-beijing", Endpoint: "https://tls-cn-beijing.volces.com"},
		}}
		ctx := newDoctorTestContext(t, cfg)
		ctx.Profile = "oidc"
		factory := &fakeAuthFactory{}
		ctx.authFactory = factory
		out, exit, err := runDoctor(ctx, nil)
		if err != nil {
			t.Fatalf("runDoctor: %v", err)
		}
		if exit != 0 {
			t.Fatalf("exit=%d, want 0", exit)
		}
		if factory.oidcCalls != 0 {
			t.Fatalf("OIDC factory must not be called offline")
		}
		raw, jerr := json.Marshal(out)
		if jerr != nil {
			t.Fatalf("marshal: %v", jerr)
		}
		for _, c := range canaries {
			if strings.Contains(string(raw), c) {
				t.Fatalf("doctor output leaked canary %q", c)
			}
		}
	})

	t.Run("ecs_offline_doctor_no_leak", func(t *testing.T) {
		canary := "secret-ecs-role"
		cfg := config.Config{Version: 1, Profiles: map[string]config.Profile{
			"ecs": {Mode: config.AuthModeECSRole, RoleName: canary, Region: "cn-beijing", Endpoint: "https://tls-cn-beijing.volces.com"},
		}}
		ctx := newDoctorTestContext(t, cfg)
		ctx.Profile = "ecs"
		ctx.authFactory = &fakeAuthFactory{}
		out, exit, err := runDoctor(ctx, nil)
		if err != nil {
			t.Fatalf("runDoctor: %v", err)
		}
		if exit != 0 {
			t.Fatalf("exit=%d, want 0", exit)
		}
		raw, jerr := json.Marshal(out)
		if jerr != nil {
			t.Fatalf("marshal: %v", jerr)
		}
		if strings.Contains(string(raw), canary) {
			t.Fatalf("doctor output leaked role canary %q", canary)
		}
	})
}

// TestWorkloadErrorEnvelopeRedactsCanaries proves that when a workload provider's
// Retrieve fails with an *auth.Error whose Cause carries canary material, the
// structured error envelope (stderr) and err.Error() never leak the canaries,
// and the hint is mode-specific without login guidance. Each case defines its
// own canaries, all of which enter the wrapped Cause.
func TestWorkloadErrorEnvelopeRedactsCanaries(t *testing.T) {
	clearAuthTestEnv(t)
	oidcTokenFile := filepath.Join(t.TempDir(), "oidc-token")
	if err := os.WriteFile(oidcTokenFile, []byte("OIDC-RAW-TOKEN-CANARY"), 0600); err != nil {
		t.Fatalf("write OIDC token: %v", err)
	}
	cases := []struct {
		mode     string
		profile  config.Profile
		canaries []string
		wantHint string
	}{
		{
			mode:     config.AuthModeRamRoleARN,
			profile:  config.Profile{Mode: config.AuthModeRamRoleARN, RoleName: "RAM-ROLE-CANARY", AccountID: "1", AccessKeyID: "RAM-AK-CANARY", SecretAccessKey: "RAM-SK-CANARY", SecurityToken: "RAM-TOKEN-CANARY", Region: "cn-beijing", Endpoint: "https://tls-cn-beijing.volces.com"},
			canaries: []string{"RAM-CAUSE-CANARY", "RAM-ROLE-CANARY", "RAM-AK-CANARY", "RAM-SK-CANARY", "RAM-TOKEN-CANARY"},
			wantHint: "source credential",
		},
		{
			mode:     config.AuthModeOIDC,
			profile:  config.Profile{Mode: config.AuthModeOIDC, RoleTRN: "trn:iam::1:role/OIDC-TRN-CANARY", OIDCTokenFile: oidcTokenFile, Region: "cn-beijing", Endpoint: "https://tls-cn-beijing.volces.com"},
			canaries: []string{"OIDC-CAUSE-CANARY", "OIDC-TRN-CANARY", oidcTokenFile, "OIDC-RAW-TOKEN-CANARY"},
			wantHint: "token file",
		},
		{
			mode:     config.AuthModeECSRole,
			profile:  config.Profile{Mode: config.AuthModeECSRole, RoleName: "ECS-ROLE-CANARY", Region: "cn-beijing", Endpoint: "https://tls-cn-beijing.volces.com"},
			canaries: []string{"ECS-CAUSE-CANARY", "ECS-ROLE-CANARY"},
			wantHint: "IMDS",
		},
	}
	for _, tc := range cases {
		t.Run(tc.mode, func(t *testing.T) {
			cfg := config.Config{Version: 1, Profiles: map[string]config.Profile{"w": tc.profile}}
			ctx := newDoctorTestContext(t, cfg)
			ctx.Profile = "w"
			causeErr := errors.New(strings.Join(tc.canaries, "|"))
			provider := &fakeProvider{err: &auth.Error{Kind: auth.ProtocolError, Description: "provider failed", Cause: causeErr}}
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

			// Trigger Retrieve failure via DoRaw.
			_, err := ctx.DoRaw("GET", "/", nil, nil, nil)
			if err == nil {
				t.Fatal("expected error from provider")
			}

			// Assert errors.As finds *dynamicAuthError.
			var dae *dynamicAuthError
			if !errors.As(err, &dae) {
				t.Fatal("expected *dynamicAuthError in error chain")
			}
			if dae.mode != tc.mode {
				t.Fatalf("dae.mode=%q, want %q", dae.mode, tc.mode)
			}

			// err.Error() must not contain any canary.
			for _, c := range tc.canaries {
				if strings.Contains(err.Error(), c) {
					t.Fatalf("err.Error() leaked canary %q", c)
				}
			}

			// Call real writeStructuredError and check return code (must be 2 for auth).
			var stdout, stderr bytes.Buffer
			code := writeStructuredError(&stdout, &stderr, err, "", 0, "tool", nil)
			if code != 2 {
				t.Fatalf("writeStructuredError code=%d, want 2", code)
			}

			// Parse stderr JSON and assert the hint field contains the mode-specific hint.
			var env map[string]any
			if jerr := json.Unmarshal(stderr.Bytes(), &env); jerr != nil {
				t.Fatalf("stderr is not valid JSON: %v\n%s", jerr, stderr.String())
			}
			hint, ok := env["hint"].(string)
			if !ok {
				t.Fatalf("stderr hint is not a string: %T", env["hint"])
			}
			if !strings.Contains(hint, tc.wantHint) {
				t.Fatalf("stderr hint=%q, want it to contain %q", hint, tc.wantHint)
			}
			if strings.Contains(hint, "login") {
				t.Fatalf("stderr hint=%q must not suggest login", hint)
			}

			// stderr must not contain any canary.
			for _, c := range tc.canaries {
				if strings.Contains(stderr.String(), c) {
					t.Fatalf("stderr leaked canary %q: %s", c, stderr.String())
				}
			}
		})
	}
}

// TestWorkloadTraceRedactsCanaries proves that trace files never contain
// credential canaries, whether the request succeeds or fails.
func TestWorkloadTraceRedactsCanaries(t *testing.T) {
	clearAuthTestEnv(t)
	t.Run("success_trace_no_leak", func(t *testing.T) {
		var gotToken string
		writeErrCh := make(chan error, 1)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotToken = r.Header.Get("X-Security-Token")
			w.WriteHeader(200)
			_, werr := w.Write([]byte(`{"ResponseMetadata":{"RequestId":"x"},"Result":{}}`))
			writeErrCh <- werr
		}))
		defer srv.Close()
		canaries := []string{"SRC-AK-1", "SRC-SK-2", "SRC-TOK-3", "SRC-ROLE-4", "TEMP-AK-5", "TEMP-SK-6", "TEMP-TOK-7"}
		cfg := config.Config{Version: 1, Profiles: map[string]config.Profile{
			"ram": {Mode: config.AuthModeRamRoleARN, RoleName: "SRC-ROLE-4", AccountID: "1", AccessKeyID: "SRC-AK-1", SecretAccessKey: "SRC-SK-2", SecurityToken: "SRC-TOK-3", Region: "cn-beijing", Endpoint: srv.URL},
		}}
		ctx := newDoctorTestContext(t, cfg)
		ctx.Profile = "ram"
		ctx.TraceDir = t.TempDir()
		provider := &fakeProvider{value: auth.Value{AccessKeyID: "TEMP-AK-5", SecretAccessKey: "TEMP-SK-6", SessionToken: "TEMP-TOK-7"}}
		ctx.authFactory = &fakeAuthFactory{ramProvider: provider}
		if _, err := ctx.DoRaw("GET", "/", nil, nil, nil); err != nil {
			t.Fatalf("DoRaw: %v", err)
		}
		select {
		case werr := <-writeErrCh:
			if werr != nil {
				t.Fatalf("write response: %v", werr)
			}
		default:
			t.Fatal("server did not report response write result")
		}
		if gotToken != "TEMP-TOK-7" {
			t.Fatalf("server did not receive token; sanity check failed")
		}
		entries, rerr := os.ReadDir(ctx.TraceDir)
		if rerr != nil {
			t.Fatalf("ReadDir: %v", rerr)
		}
		if len(entries) == 0 {
			t.Fatal("expected at least one trace file")
		}
		for _, e := range entries {
			b, ferr := os.ReadFile(filepath.Join(ctx.TraceDir, e.Name()))
			if ferr != nil {
				t.Fatalf("ReadFile %s: %v", e.Name(), ferr)
			}
			for _, c := range canaries {
				if strings.Contains(string(b), c) {
					t.Fatalf("trace file %s leaked canary %q", e.Name(), c)
				}
			}
		}
	})

	t.Run("failure_trace_no_leak", func(t *testing.T) {
		canaries := []string{"FAIL-SRC-AK-1", "FAIL-SRC-SK-2", "FAIL-SRC-TOK-3", "FAIL-ROLE-4", "FAIL-CAUSE-5"}
		cfg := config.Config{Version: 1, Profiles: map[string]config.Profile{
			"ram": {Mode: config.AuthModeRamRoleARN, RoleName: "FAIL-ROLE-4", AccountID: "1", AccessKeyID: "FAIL-SRC-AK-1", SecretAccessKey: "FAIL-SRC-SK-2", SecurityToken: "FAIL-SRC-TOK-3", Region: "cn-beijing", Endpoint: "https://tls-cn-beijing.volces.com"},
		}}
		ctx := newDoctorTestContext(t, cfg)
		ctx.Profile = "ram"
		ctx.TraceDir = t.TempDir()
		cause := errors.New(strings.Join(canaries, "|"))
		provider := &fakeProvider{err: &auth.Error{Kind: auth.ProtocolError, Description: "failed", Cause: cause}}
		ctx.authFactory = &fakeAuthFactory{ramProvider: provider}
		_, err := ctx.DoRaw("GET", "/", nil, nil, nil)
		if err == nil {
			t.Fatal("expected error from provider")
		}
		var dae *dynamicAuthError
		if !errors.As(err, &dae) {
			t.Fatal("expected *dynamicAuthError in error chain")
		}
		for _, c := range canaries {
			if strings.Contains(err.Error(), c) {
				t.Fatalf("err.Error() leaked canary %q", c)
			}
		}
		entries, rerr := os.ReadDir(ctx.TraceDir)
		if rerr != nil {
			t.Fatalf("ReadDir: %v", rerr)
		}
		if len(entries) == 0 {
			t.Fatal("expected at least one trace file")
		}
		for _, e := range entries {
			b, ferr := os.ReadFile(filepath.Join(ctx.TraceDir, e.Name()))
			if ferr != nil {
				t.Fatalf("ReadFile %s: %v", e.Name(), ferr)
			}
			for _, c := range canaries {
				if strings.Contains(string(b), c) {
					t.Fatalf("failure trace file %s leaked canary %q", e.Name(), c)
				}
			}
		}
	})
}
