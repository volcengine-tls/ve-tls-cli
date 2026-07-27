package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth"
	"github.com/volcengine-tls/ve-tls-cli/internal/config"
	"github.com/volcengine/volc-sdk-golang/base"
)

func TestLegacySecretsFileStillSelectsStaticCredentials(t *testing.T) {
	clearLegacyCLIAuthEnvironment(t)
	const (
		accessKey = "legacy-global-ak"
		secretKey = "legacy-global-sk"
	)
	server, signatureResult := newLegacyStaticSignatureServer(accessKey, secretKey)
	defer server.Close()

	dir := t.TempDir()
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(dir, "config.json"))
	secretsPath := filepath.Join(dir, "legacy.env")
	secrets := strings.Join([]string{
		"export VOLCENGINE_ACCESS_KEY_ID='" + accessKey + "'",
		"VOLCENGINE_ACCESS_KEY_SECRET=\"" + secretKey + "\"",
		"VOLCENGINE_REGION=cn-beijing",
		"VOLCENGINE_ENDPOINT=" + server.URL,
		"",
	}, "\n")
	if err := os.WriteFile(secretsPath, []byte(secrets), 0o600); err != nil {
		t.Fatalf("write global secrets file: %v", err)
	}

	stdout, stderr, code := runLegacyCLI("--secrets-file", secretsPath, "tool", "exec", "account.get")
	if code != 0 {
		t.Fatalf("global secrets-file exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if err := <-signatureResult; err != nil {
		t.Fatal(err)
	}
}

func TestLegacyContextSecretsFileStillSelectsStaticCredentials(t *testing.T) {
	clearLegacyCLIAuthEnvironment(t)
	const (
		accessKey = "legacy-context-ak"
		secretKey = "legacy-context-sk"
	)
	server, signatureResult := newLegacyStaticSignatureServer(accessKey, secretKey)
	defer server.Close()

	dir := t.TempDir()
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(dir, "config.json"))
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
		t.Fatal(err)
	}
}

func TestLegacyProfileSecretsSelectorConflictsFailBeforeFileRead(t *testing.T) {
	clearLegacyCLIAuthEnvironment(t)
	dir := t.TempDir()
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(dir, "config.json"))
	missingSecrets := filepath.Join(dir, "must-not-be-read.env")

	writeContext := func(name string, value map[string]any) string {
		t.Helper()
		path := filepath.Join(dir, name+".json")
		b, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("marshal %s context: %v", name, err)
		}
		if err := os.WriteFile(path, b, 0o600); err != nil {
			t.Fatalf("write %s context: %v", name, err)
		}
		return path
	}
	contextSecrets := writeContext("context-secrets", map[string]any{"secrets_file": missingSecrets})
	contextProfile := writeContext("context-profile", map[string]any{"profile": "legacy"})
	contextBoth := writeContext("context-both", map[string]any{
		"profile":      "legacy",
		"secrets_file": missingSecrets,
	})

	for _, tc := range []struct {
		name string
		args []string
	}{
		{
			name: "global profile and global secrets",
			args: []string{"--profile", "legacy", "--secrets-file", missingSecrets, "tool", "exec", "account.get"},
		},
		{
			name: "global profile and context secrets",
			args: []string{"--profile", "legacy", "tool", "exec", "account.get", "--context", "file://" + contextSecrets},
		},
		{
			name: "global secrets and context profile",
			args: []string{"--secrets-file", missingSecrets, "tool", "exec", "account.get", "--context", "file://" + contextProfile},
		},
		{
			name: "context profile and context secrets",
			args: []string{"tool", "exec", "account.get", "--context", "file://" + contextBoth},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, code := runLegacyCLI(tc.args...)
			if code != 1 {
				t.Fatalf("selector conflict exit=%d, want 1; stdout=%q stderr=%q", code, stdout, stderr)
			}
			if strings.TrimSpace(stderr) != "" {
				t.Fatalf("selector conflict stderr=%q, want empty structured output", stderr)
			}
			var envelope map[string]any
			if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
				t.Fatalf("decode selector conflict envelope: %v; stdout=%q", err, stdout)
			}
			errObject, ok := envelope["error"].(map[string]any)
			if !ok {
				t.Fatalf("selector conflict error=%T, want object: %#v", envelope["error"], envelope)
			}
			if errObject["kind"] != "incompatible_flags" {
				t.Fatalf("selector conflict kind=%#v, want incompatible_flags", errObject["kind"])
			}
			message, _ := errObject["message"].(string)
			if !strings.Contains(message, "conflicting runtime selectors") {
				t.Fatalf("selector conflict message=%q", message)
			}
			if strings.Contains(message, "failed to read secrets file") || strings.Contains(message, "no such file") {
				t.Fatalf("selector conflict read the secrets file before failing: %q", message)
			}
		})
	}
}

func TestLegacyConfigureShowListOutputUnchanged(t *testing.T) {
	clearLegacyCLIAuthEnvironment(t)
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("VOLCLOG_CONFIG", path)
	const configJSON = `{
  "version": 1,
  "current_profile": "inline",
  "profiles": {
    "inline": {
      "access_key_id": "legacy-ak-123456",
      "secret_access_key": "legacy-sk",
      "security_token": "legacy-token",
      "region": "cn-beijing",
      "endpoint": "https://tls-cn-beijing.volces.com",
      "timeout_seconds": 45
    },
    "ref": {
      "region": "ap-singapore-1",
      "endpoint": "https://tls-ap-singapore-1.volces.com",
      "cred_ref": "shared"
    }
  },
  "creds": {
    "shared": {
      "access_key_id": "shared-ak-654321",
      "secret_access_key": "shared-sk"
    }
  }
}
`
	if err := os.WriteFile(path, []byte(configJSON), 0o600); err != nil {
		t.Fatalf("write legacy CLI config: %v", err)
	}

	showStdout, showStderr, code := runLegacyCLI("configure", "show", "--profile", "inline")
	if code != 0 {
		t.Fatalf("configure show exit=%d stdout=%q stderr=%q", code, showStdout, showStderr)
	}
	assertLegacyJSONEqual(t, showStdout, `{
	  "access_key_id": "leg****456",
	  "cred_ref": "",
	  "credential_present": true,
	  "credential_source": "profile_inline",
	  "effective_profile": "inline",
	  "endpoint": "https://tls-cn-beijing.volces.com",
	  "has_security_token": true,
	  "profile": "inline",
	  "region": "cn-beijing",
	  "timeout_seconds": 45
	}`)

	listStdout, listStderr, code := runLegacyCLI("configure", "list")
	if code != 0 {
		t.Fatalf("configure list exit=%d stdout=%q stderr=%q", code, listStdout, listStderr)
	}
	assertLegacyJSONEqual(t, listStdout, `{
	  "current_profile": "inline",
	  "profiles": [
	    {
	      "access_key_id": "leg****456",
	      "cred_ref": "",
	      "credential_present": true,
	      "credential_source": "profile_inline",
	      "effective_profile": "inline",
	      "endpoint": "https://tls-cn-beijing.volces.com",
	      "has_security_token": true,
	      "profile": "inline",
	      "region": "cn-beijing",
	      "timeout_seconds": 45
	    },
	    {
	      "access_key_id": "sha****321",
	      "cred_ref": "shared",
	      "credential_present": true,
	      "credential_source": "profile_cred_ref",
	      "effective_profile": "ref",
	      "endpoint": "https://tls-ap-singapore-1.volces.com",
	      "has_security_token": false,
	      "profile": "ref",
	      "region": "ap-singapore-1",
	      "timeout_seconds": 0
	    }
	  ]
	}`)
}

func clearLegacyCLIAuthEnvironment(t *testing.T) {
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

func runLegacyCLI(args ...string) (stdout string, stderr string, code int) {
	var stdoutBuffer, stderrBuffer bytes.Buffer
	code = Run(args, &stdoutBuffer, &stderrBuffer)
	return stdoutBuffer.String(), stderrBuffer.String(), code
}

func newLegacyStaticSignatureServer(accessKey, secretKey string) (*httptest.Server, <-chan error) {
	signatureResult := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		signatureResult <- validateLegacyStaticRequestSignature(r, accessKey, secretKey)
		w.Header().Set("x-tls-requestid", "legacy-static-signature")
		_, _ = w.Write([]byte(`{"Status":"Activated","ArchVersion":"2.0"}`))
	}))
	return server, signatureResult
}

// validateLegacyStaticRequestSignature verifies that a static AK/SK request was
// signed with the given credentials, region cn-beijing, service TLS, and no
// session token. It delegates to the generic validator with an empty token.
func validateLegacyStaticRequestSignature(r *http.Request, accessKey, secretKey string) error {
	return validateAuthContractRequestSignature(r, accessKey, secretKey, "")
}

// validateAuthContractRequestSignature verifies that r was signed with exactly
// the given AK/SK/SessionToken, region cn-beijing, service TLS. It reconstructs
// the expected Authorization via base.GetSignRequest (with SessionToken placed
// in base.Credentials so the signer includes X-Security-Token in the signed
// headers) and requires an exact match. The received X-Security-Token must
// equal token exactly (or be empty when token is empty).
func validateAuthContractRequestSignature(r *http.Request, accessKey, secretKey, token string) error {
	authorization := strings.TrimSpace(r.Header.Get("Authorization"))
	if authorization == "" {
		return fmt.Errorf("request is missing Authorization")
	}
	gotToken := strings.TrimSpace(r.Header.Get("X-Security-Token"))
	if gotToken != token {
		return fmt.Errorf("X-Security-Token=%q, want %q", gotToken, token)
	}

	xDate := strings.TrimSpace(r.Header.Get("X-Date"))
	signedAt, err := time.Parse("20060102T150405Z", xDate)
	if err != nil {
		return fmt.Errorf("parse X-Date %q: %w", xDate, err)
	}

	var body []byte
	if r.Body != nil {
		body, err = io.ReadAll(r.Body)
		closeErr := r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("read signed request body: %w", err)
		}
		if closeErr != nil {
			return fmt.Errorf("close signed request body: %w", closeErr)
		}
	}

	signingHeaders := r.Header.Clone()
	signingHeaders.Del("Authorization")
	expected := base.GetSignRequest(base.RequestParam{
		Body:      body,
		Method:    r.Method,
		Date:      signedAt,
		Path:      r.URL.Path,
		Host:      r.Host,
		QueryList: r.URL.Query(),
		Headers:   signingHeaders,
	}, base.Credentials{
		AccessKeyID:     accessKey,
		SecretAccessKey: secretKey,
		SessionToken:    token,
		Region:          "cn-beijing",
		Service:         "TLS",
	})

	if authorization != expected.Authorization {
		return fmt.Errorf("Authorization mismatch:\n got: %s\nwant: %s", authorization, expected.Authorization)
	}
	if got := strings.TrimSpace(r.Header.Get("X-Content-Sha256")); got != expected.XContentSha256 {
		return fmt.Errorf("X-Content-Sha256=%q, want %q", got, expected.XContentSha256)
	}
	wantScope := "Credential=" + accessKey + "/" + signedAt.Format("20060102") + "/cn-beijing/TLS/request"
	if !strings.Contains(authorization, wantScope) {
		return fmt.Errorf("TLS scope missing %q from Authorization", wantScope)
	}
	if expected.XDate != xDate {
		return fmt.Errorf("X-Date=%q, want %q", xDate, expected.XDate)
	}
	return nil
}

func assertLegacyJSONEqual(t *testing.T, got, want string) {
	t.Helper()
	var gotValue, wantValue any
	if err := json.Unmarshal([]byte(got), &gotValue); err != nil {
		t.Fatalf("decode actual JSON: %v; actual=%q", err, got)
	}
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatalf("decode expected JSON: %v; expected=%q", err, want)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("legacy JSON output changed:\n got: %s\nwant: %s", got, want)
	}
}

// newContractSignatureServer returns a test server that validates each request's
// full signature against the expected AK/SK/SessionToken (region cn-beijing,
// service TLS) via validateAuthContractRequestSignature. The handler never
// calls t.Fatal; it sends the validation error (nil on success) on the returned
// channel and increments the atomic request counter. It responds 200 with no
// body.
func newContractSignatureServer(t *testing.T, ak, sk, token string) (*httptest.Server, <-chan error, *int32) {
	t.Helper()
	var count int32
	ch := make(chan error, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&count, 1)
		ch <- validateAuthContractRequestSignature(r, ak, sk, token)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	return server, ch, &count
}

// installProviderForMode installs the given provider into the one factory slot
// selected by mode.
func installProviderForMode(factory *fakeAuthFactory, mode string, provider auth.Provider) {
	switch mode {
	case config.AuthModeSSO:
		factory.ssoProvider = provider
	case config.AuthModeConsoleLogin:
		factory.consoleProvider = provider
	case config.AuthModeRamRoleARN:
		factory.ramProvider = provider
	case config.AuthModeOIDC:
		factory.oidcProvider = provider
	case config.AuthModeECSRole:
		factory.ecsProvider = provider
	}
}

// assertFactoryCalls asserts that exactly the factory method for mode was called
// once and all other factory methods were called zero times.
func assertFactoryCalls(t *testing.T, factory *fakeAuthFactory, mode string) {
	t.Helper()
	want := map[string]int{
		config.AuthModeSSO:          factory.ssoCalls,
		config.AuthModeConsoleLogin: factory.consoleCalls,
		config.AuthModeRamRoleARN:   factory.ramCalls,
		config.AuthModeOIDC:         factory.oidcCalls,
		config.AuthModeECSRole:      factory.ecsCalls,
	}
	for m, c := range want {
		if m == mode {
			if c != 1 {
				t.Fatalf("factory %s calls=%d, want 1", m, c)
			}
		} else if c != 0 {
			t.Fatalf("factory %s calls=%d, want 0", m, c)
		}
	}
}

// TestAllAuthModesPreserveIdentityAndFallbackContract is a table-driven
// acceptance test over all seven identities (legacy empty mode, AK, SSO,
// Console Login, RAM Role ARN, OIDC, ECS Role). Each row runs in a unified
// loop: static rows (empty, AK) prove legacy environment AK/SK precedence with
// full signature validation; provider rows prove success (Retrieve=1, one TLS
// request signed by the provider's credentials, Service=TLS) and failure
// (Retrieve=1, zero TLS requests, no fallback, dynamicAuthError with correct
// mode). Poison VOLCENGINE AK/SK/token are set for every row and proven unused
// by the exact signature match.
func TestAllAuthModesPreserveIdentityAndFallbackContract(t *testing.T) {
	clearAuthTestEnv(t)
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "POISON-ENV-AK")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "POISON-ENV-SK")
	t.Setenv("VOLCENGINE_TOKEN", "POISON-ENV-TOKEN")

	type row struct {
		name     string
		mode     string
		profile  config.Profile
		provider auth.Provider // nil for static rows
	}
	rows := []row{
		{
			name:    "empty",
			mode:    "",
			profile: config.Profile{Mode: "", AccessKeyID: "DORMANT-EMPTY-AK", SecretAccessKey: "DORMANT-EMPTY-SK"},
		},
		{
			name:    "ak",
			mode:    config.AuthModeAK,
			profile: config.Profile{Mode: config.AuthModeAK, AccessKeyID: "DORMANT-AK-AK", SecretAccessKey: "DORMANT-AK-SK"},
		},
		{
			name:     "sso",
			mode:     config.AuthModeSSO,
			profile:  config.Profile{Mode: config.AuthModeSSO, Region: "cn-beijing"},
			provider: &fakeProvider{value: auth.Value{AccessKeyID: "AKLTsso-identity", SecretAccessKey: "sso-secret", SessionToken: "sso-session-token", ProviderName: "sso", ExpiresAt: time.Now().Add(time.Hour)}},
		},
		{
			name:     "console-login",
			mode:     config.AuthModeConsoleLogin,
			profile:  config.Profile{Mode: config.AuthModeConsoleLogin, Region: "cn-beijing"},
			provider: &fakeProvider{value: auth.Value{AccessKeyID: "AKLTconsole-identity", SecretAccessKey: "console-secret", SessionToken: "console-session-token", ProviderName: "console-login", ExpiresAt: time.Now().Add(time.Hour)}},
		},
		{
			name:     "ramrolearn",
			mode:     config.AuthModeRamRoleARN,
			profile:  config.Profile{Mode: config.AuthModeRamRoleARN, RoleName: "r", AccountID: "1", AccessKeyID: "src-ak", SecretAccessKey: "src-sk", Region: "cn-beijing"},
			provider: &fakeProvider{value: auth.Value{AccessKeyID: "AKLTram-identity", SecretAccessKey: "ram-secret", SessionToken: "ram-session-token", ProviderName: "ramrolearn", ExpiresAt: time.Now().Add(time.Hour)}},
		},
		{
			name:     "oidc",
			mode:     config.AuthModeOIDC,
			profile:  config.Profile{Mode: config.AuthModeOIDC, RoleTRN: "trn:iam::1:role/r", OIDCTokenFile: "/tmp/token", Region: "cn-beijing"},
			provider: &fakeProvider{value: auth.Value{AccessKeyID: "AKLToidc-identity", SecretAccessKey: "oidc-secret", SessionToken: "oidc-session-token", ProviderName: "oidc", ExpiresAt: time.Now().Add(time.Hour)}},
		},
		{
			name:     "ecsrole",
			mode:     config.AuthModeECSRole,
			profile:  config.Profile{Mode: config.AuthModeECSRole, RoleName: "r", Region: "cn-beijing"},
			provider: &fakeProvider{value: auth.Value{AccessKeyID: "AKLTecs-identity", SecretAccessKey: "ecs-secret", SessionToken: "ecs-session-token", ProviderName: "ecsrole", ExpiresAt: time.Now().Add(time.Hour)}},
		},
	}

	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			if tc.provider == nil {
				// Static row: legacy env AK/SK precedence, full signature, factory total calls 0.
				t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ENV-AK-"+tc.name)
				t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "ENV-SK-"+tc.name)
				t.Setenv("VOLCENGINE_TOKEN", "")
				t.Setenv("VOLCENGINE_REGION", "cn-beijing")
				server, sigCh, count := newContractSignatureServer(t, "ENV-AK-"+tc.name, "ENV-SK-"+tc.name, "")
				t.Setenv("VOLCENGINE_ENDPOINT", server.URL)
				cfg := config.Config{Version: 1, Profiles: map[string]config.Profile{"w": tc.profile}}
				factory := &fakeAuthFactory{}
				ctx := newTestContext(t, cfg, "/tmp/test-config.json")
				ctx.Profile = "w"
				ctx.authFactory = factory

				if _, err := ctx.DoRaw("GET", "/DescribeProjects", nil, nil, nil); err != nil {
					t.Fatalf("DoRaw: %v", err)
				}
				if atomic.LoadInt32(count) != 1 {
					t.Fatalf("TLS requests=%d, want 1", atomic.LoadInt32(count))
				}
				if err := <-sigCh; err != nil {
					t.Fatalf("signature validation failed: %v", err)
				}
				if factory.ssoCalls+factory.consoleCalls+factory.ramCalls+factory.oidcCalls+factory.ecsCalls != 0 {
					t.Fatalf("factory total calls=%d, want 0 for static mode", factory.ssoCalls+factory.consoleCalls+factory.ramCalls+factory.oidcCalls+factory.ecsCalls)
				}
				return
			}

			// Provider row: success + failure.
			fp := tc.provider.(*fakeProvider)
			wantAK := fp.value.AccessKeyID
			wantSK := fp.value.SecretAccessKey
			wantToken := fp.value.SessionToken

			t.Run("success", func(t *testing.T) {
				server, sigCh, count := newContractSignatureServer(t, wantAK, wantSK, wantToken)
				tc.profile.Endpoint = server.URL
				cfg := config.Config{Version: 1, Profiles: map[string]config.Profile{"w": tc.profile}}
				factory := &fakeAuthFactory{}
				installProviderForMode(factory, tc.mode, tc.provider)
				ctx := newTestContext(t, cfg, "/tmp/test-config.json")
				ctx.Profile = "w"
				ctx.authFactory = factory

				if _, err := ctx.DoRaw("GET", "/DescribeProjects", nil, nil, nil); err != nil {
					t.Fatalf("DoRaw: %v", err)
				}
				if atomic.LoadInt32(count) != 1 {
					t.Fatalf("TLS requests=%d, want 1", atomic.LoadInt32(count))
				}
				if err := <-sigCh; err != nil {
					t.Fatalf("signature validation failed: %v", err)
				}
				if fp.calls != 1 {
					t.Fatalf("provider Retrieve calls=%d, want 1", fp.calls)
				}
				assertFactoryCalls(t, factory, tc.mode)
			})

			t.Run("failure_no_fallback", func(t *testing.T) {
				server, _, count := newContractSignatureServer(t, wantAK, wantSK, wantToken)
				tc.profile.Endpoint = server.URL
				cfg := config.Config{Version: 1, Profiles: map[string]config.Profile{"w": tc.profile}}
				failProvider := &fakeProvider{err: &auth.Error{Kind: auth.ProtocolError, Description: "provider failed"}}
				factory := &fakeAuthFactory{}
				installProviderForMode(factory, tc.mode, failProvider)
				ctx := newTestContext(t, cfg, "/tmp/test-config.json")
				ctx.Profile = "w"
				ctx.authFactory = factory

				_, err := ctx.DoRaw("GET", "/DescribeProjects", nil, nil, nil)
				if err == nil {
					t.Fatal("expected error from failing provider")
				}
				var dae *dynamicAuthError
				if !errors.As(err, &dae) {
					t.Fatalf("expected *dynamicAuthError, got %T", err)
				}
				if dae.mode != tc.mode {
					t.Fatalf("dynamicAuthError mode=%q, want %q", dae.mode, tc.mode)
				}
				if atomic.LoadInt32(count) != 0 {
					t.Fatalf("TLS requests=%d, want 0 on provider failure", atomic.LoadInt32(count))
				}
				if failProvider.calls != 1 {
					t.Fatalf("provider Retrieve calls=%d, want 1", failProvider.calls)
				}
				assertFactoryCalls(t, factory, tc.mode)
			})
		})
	}
}
