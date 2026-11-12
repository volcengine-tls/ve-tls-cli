package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth"
	"github.com/volcengine-tls/ve-tls-cli/internal/config"
)

type explicitProfileSigningIdentity struct {
	accessKey string
	secretKey string
	token     string
}

type explicitProfileTransport struct {
	wantHost   string
	identities []explicitProfileSigningIdentity
	calls      int
	err        error
}

func (rt *explicitProfileTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	index := rt.calls
	rt.calls++
	if req.URL.Host != rt.wantHost {
		rt.err = fmt.Errorf("request host=%q, want %q", req.URL.Host, rt.wantHost)
	} else if index >= len(rt.identities) {
		rt.err = errors.New("unexpected request")
	} else {
		identity := rt.identities[index]
		if err := validateAuthContractRequestSignature(req, identity.accessKey, identity.secretKey, identity.token); err != nil {
			rt.err = err
		}
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"X-Tls-Requestid": []string{"fake-request"}},
		Body:       io.NopCloser(strings.NewReader(`{"Projects":[],"Total":0,"Status":"Activated","ArchVersion":"2.0"}`)),
		Request:    req,
	}, nil
}

func TestRunExplicitProfileUsesProfileSignatureWithoutEnvironmentFallback(t *testing.T) {
	const profileEndpoint = "https://profile.example.invalid"
	profile := config.Profile{
		AccessKeyID: "profile-ak", SecretAccessKey: "profile-sk", SecurityToken: "profile-token",
		Region: "cn-beijing", Endpoint: profileEndpoint,
	}
	tests := []struct {
		name      string
		profile   config.Profile
		creds     map[string]config.Credential
		args      []string
		wantHost  string
		wantAuth  explicitProfileSigningIdentity
		wantError string
		envRegion string
	}{
		{
			name: "global profile with token", profile: profile,
			args:     []string{"--profile", "selected", "raw", "--path", "/DescribeProjects"},
			wantHost: "profile.example.invalid", wantAuth: explicitProfileSigningIdentity{"profile-ak", "profile-sk", "profile-token"},
		},
		{
			name:     "doctor example supplies target flags with explicit profile",
			profile:  config.Profile{AccessKeyID: "profile-ak", SecretAccessKey: "profile-sk"},
			args:     []string{"--profile", "selected", "--region", "cn-beijing", "--endpoint", "https://flags.example.invalid", "doctor", "--online"},
			wantHost: "flags.example.invalid", wantAuth: explicitProfileSigningIdentity{"profile-ak", "profile-sk", ""},
		},
		{
			name:     "context profile with credential reference",
			profile:  config.Profile{CredRef: "ref", SecurityToken: "profile-token", Region: "cn-beijing", Endpoint: profileEndpoint},
			creds:    map[string]config.Credential{"ref": {AccessKeyID: "ref-ak", SecretAccessKey: "ref-sk"}},
			args:     []string{"tool", "exec", "account.get", "--context", `{"profile":"selected"}`},
			wantHost: "profile.example.invalid", wantAuth: explicitProfileSigningIdentity{"ref-ak", "ref-sk", "profile-token"},
		},
		{
			name:     "no token does not inherit environment token",
			profile:  config.Profile{AccessKeyID: "profile-ak", SecretAccessKey: "profile-sk", Region: "cn-beijing", Endpoint: profileEndpoint},
			args:     []string{"--profile", "selected", "raw", "--path", "/DescribeProjects"},
			wantHost: "profile.example.invalid", wantAuth: explicitProfileSigningIdentity{"profile-ak", "profile-sk", ""},
		},
		{
			name:      "missing secret fails before sending",
			profile:   config.Profile{AccessKeyID: "profile-ak", Region: "cn-beijing", Endpoint: profileEndpoint},
			args:      []string{"--profile", "selected", "raw", "--path", "/DescribeProjects"},
			wantError: "missing access key id or secret access key",
		},
		{
			name:      "missing access key fails before sending",
			profile:   config.Profile{SecretAccessKey: "profile-sk", Region: "cn-beijing", Endpoint: profileEndpoint},
			args:      []string{"--profile", "selected", "raw", "--path", "/DescribeProjects"},
			wantError: "missing access key id or secret access key",
		},
		{
			name:      "missing reference fails before sending",
			profile:   config.Profile{CredRef: "missing", Region: "cn-beijing", Endpoint: profileEndpoint},
			args:      []string{"--profile", "selected", "raw", "--path", "/DescribeProjects"},
			wantError: "credential not found: missing",
		},
		{
			name: "misspelled profile fails before sending", profile: profile,
			args:      []string{"--profile", "typo", "raw", "--path", "/DescribeProjects"},
			wantError: "profile not found: typo",
		},
		{
			name: "implicit profile retains complete environment precedence", profile: profile,
			args:     []string{"raw", "--path", "/DescribeProjects"},
			wantHost: "env.example.invalid", wantAuth: explicitProfileSigningIdentity{"env-ak", "env-sk", "env-token"},
			envRegion: "cn-beijing",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearAuthTestEnv(t)
			t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "env-ak")
			t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "env-sk")
			t.Setenv("VOLCENGINE_TOKEN", "env-token")
			envRegion := tc.envRegion
			if envRegion == "" {
				envRegion = "cn-hangzhou"
			}
			t.Setenv("VOLCENGINE_REGION", envRegion)
			t.Setenv("VOLCENGINE_ENDPOINT", "https://env.example.invalid")
			// Let the fake transport handle doctor without DNS or TCP checks.
			t.Setenv("HTTPS_PROXY", "http://proxy.example.invalid")

			path := filepath.Join(t.TempDir(), "config.json")
			t.Setenv("VOLCLOG_CONFIG", path)
			cfg := config.Config{Version: 1, CurrentProfile: "selected", Profiles: map[string]config.Profile{"selected": tc.profile}, Creds: tc.creds}
			if err := config.Save(cfg, path); err != nil {
				t.Fatalf("save config: %v", err)
			}
			transport := &explicitProfileTransport{wantHost: tc.wantHost, identities: []explicitProfileSigningIdentity{tc.wantAuth}}
			originalTransport := http.DefaultTransport
			http.DefaultTransport = transport
			t.Cleanup(func() { http.DefaultTransport = originalTransport })

			var stdout, stderr bytes.Buffer
			code := Run(tc.args, &stdout, &stderr)
			if tc.wantError != "" {
				if code == 0 || !strings.Contains(stdout.String()+stderr.String(), tc.wantError) {
					t.Fatalf("Run exit=%d, expected error %q", code, tc.wantError)
				}
				if transport.calls != 0 {
					t.Fatalf("sent %d requests on failure, want none", transport.calls)
				}
				return
			}
			if code != 0 {
				t.Fatalf("Run exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			if transport.calls != 1 || transport.err != nil {
				t.Fatalf("requests=%d, signature/target error=%v", transport.calls, transport.err)
			}
		})
	}
}

func TestExplicitDynamicProfileRuntimeOverrideAndRefreshPerSignature(t *testing.T) {
	clearAuthTestEnv(t)
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "env-ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "env-sk")
	t.Setenv("VOLCENGINE_TOKEN", "env-token")
	t.Setenv("VOLCENGINE_REGION", "env-region")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://env.example.invalid")

	identities := []explicitProfileSigningIdentity{{"dynamic-ak-1", "dynamic-sk-1", "dynamic-token-1"}, {"dynamic-ak-2", "dynamic-sk-2", "dynamic-token-2"}}
	provider := &fakeProvider{}
	provider.retrieveFn = func() (auth.Value, error) {
		// Retrieve is called once per request; fail the test through the normal
		// error path rather than indexing beyond the expected calls.
		index := provider.calls - 1
		if index >= len(identities) {
			return auth.Value{}, errors.New("unexpected provider retrieval")
		}
		identity := identities[index]
		return auth.Value{AccessKeyID: identity.accessKey, SecretAccessKey: identity.secretKey, SessionToken: identity.token, ProviderName: "sso", ExpiresAt: time.Now().Add(time.Hour)}, nil
	}
	factory := &fakeAuthFactory{ssoProvider: provider}
	cfg := config.Config{Profiles: map[string]config.Profile{
		"selected": {Mode: config.AuthModeSSO, Region: "profile-region", Endpoint: "https://profile.example.invalid"},
	}}
	ctx := newTestContext(t, cfg, "/tmp/test-config.json")
	ctx.Profile = "selected"
	ctx.RuntimeRegion = "cn-beijing"
	ctx.RuntimeEndpoint = "https://runtime.example.invalid"
	ctx.authFactory = factory
	client, err := ctx.Client()
	if err != nil {
		t.Fatalf("Client: %v", err)
	}
	transport := &explicitProfileTransport{wantHost: "runtime.example.invalid", identities: identities}
	client.HTTP.Transport = transport
	for i := 0; i < len(identities); i++ {
		if _, err := ctx.DoRaw("GET", "/DescribeProjects", nil, nil, nil); err != nil {
			t.Fatalf("DoRaw request %d: %v", i+1, err)
		}
		if transport.err != nil {
			t.Fatalf("request %d signature/target: %v", i+1, transport.err)
		}
	}
	if factory.ssoCalls != 1 || factory.lastProfileName != "selected" || provider.calls != 2 || transport.calls != 2 {
		t.Fatalf("factory=%d profile=%q retrievals=%d requests=%d", factory.ssoCalls, factory.lastProfileName, provider.calls, transport.calls)
	}
	resolved := factory.lastCfg.Profiles["selected"]
	if resolved.Region != "cn-beijing" || resolved.Endpoint != "https://runtime.example.invalid" {
		t.Fatalf("factory target=(%q, %q), want runtime overrides", resolved.Region, resolved.Endpoint)
	}
}
