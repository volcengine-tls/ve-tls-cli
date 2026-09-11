package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth"
	"github.com/volcengine-tls/ve-tls-cli/internal/auth/console"
	"github.com/volcengine-tls/ve-tls-cli/internal/auth/sso"
	"github.com/volcengine-tls/ve-tls-cli/internal/auth/sts"
	"github.com/volcengine-tls/ve-tls-cli/internal/config"
)

type fakeProvider struct {
	values []auth.Value
	err    error
	calls  int
}

func (p *fakeProvider) Retrieve(context.Context) (auth.Value, error) {
	p.calls++
	if p.err != nil {
		return auth.Value{}, p.err
	}
	if len(p.values) == 0 {
		return auth.Value{}, errors.New("no credentials")
	}
	index := p.calls - 1
	if index >= len(p.values) {
		index = len(p.values) - 1
	}
	return p.values[index], nil
}

type fakeProviderFactory struct {
	provider  auth.Provider
	err       error
	gotCfg    config.Config
	gotMode   string
	ssoRead   AuthStatusReader
	loginRead AuthStatusReader
}

func (f *fakeProviderFactory) SSO(_ string, profileName string, cfg config.Config) (auth.Provider, AuthStatusReader, error) {
	f.gotCfg, f.gotMode = cfg, config.AuthModeSSO
	return f.provider, f.ssoRead, f.err
}

func (f *fakeProviderFactory) Console(_ string, profileName string, cfg config.Config) (auth.Provider, AuthStatusReader, error) {
	f.gotCfg, f.gotMode = cfg, config.AuthModeConsoleLogin
	return f.provider, f.loginRead, f.err
}

func (f *fakeProviderFactory) RamRoleARN(_ string, profileName string, cfg config.Config) (auth.Provider, error) {
	f.gotCfg, f.gotMode = cfg, config.AuthModeRamRoleARN
	return f.provider, f.err
}

func (f *fakeProviderFactory) OIDC(_ string, profileName string, cfg config.Config) (auth.Provider, error) {
	f.gotCfg, f.gotMode = cfg, config.AuthModeOIDC
	return f.provider, f.err
}

func (f *fakeProviderFactory) ECSRole(_ string, profileName string, cfg config.Config) (auth.Provider, error) {
	f.gotCfg, f.gotMode = cfg, config.AuthModeECSRole
	return f.provider, f.err
}

type typedNilFactory struct{}

func (*typedNilFactory) SSO(string, string, config.Config) (auth.Provider, AuthStatusReader, error) {
	panic("typed-nil factory must not be called")
}
func (*typedNilFactory) Console(string, string, config.Config) (auth.Provider, AuthStatusReader, error) {
	panic("typed-nil factory must not be called")
}
func (*typedNilFactory) RamRoleARN(string, string, config.Config) (auth.Provider, error) {
	panic("typed-nil factory must not be called")
}
func (*typedNilFactory) OIDC(string, string, config.Config) (auth.Provider, error) {
	panic("typed-nil factory must not be called")
}
func (*typedNilFactory) ECSRole(string, string, config.Config) (auth.Provider, error) {
	panic("typed-nil factory must not be called")
}

func TestBuildClientRejectsTypedNilFactory(t *testing.T) {
	var factory *typedNilFactory
	_, err := BuildClient(BuildClientRequest{
		Mode:        config.AuthModeSSO,
		ProfileName: "profile",
		Profile:     dynamicRuntimeProfile(),
		Factory:     factory,
	})
	if err == nil || err.Error() != "nil auth provider factory" {
		t.Fatalf("BuildClient error=%v", err)
	}
}

func TestBuildClientRejectsTypedNilProvider(t *testing.T) {
	var provider *fakeProvider
	factory := &fakeProviderFactory{provider: provider}
	_, err := BuildClient(BuildClientRequest{
		Mode:        config.AuthModeSSO,
		ProfileName: "profile",
		Profile:     dynamicRuntimeProfile(),
		Factory:     factory,
	})
	if err == nil || err.Error() != "nil auth provider" {
		t.Fatalf("BuildClient error=%v", err)
	}
}

func TestBuildClientInjectsResolvedRuntimeIntoClonedConfig(t *testing.T) {
	original := config.Config{Profiles: map[string]config.Profile{
		"profile": {
			Mode:           config.AuthModeECSRole,
			Region:         "old-region",
			Endpoint:       "old-endpoint",
			TimeoutSeconds: 1,
		},
	}}
	provider := &fakeProvider{values: []auth.Value{validAuthValue("one")}}
	factory := &fakeProviderFactory{provider: provider}
	client, err := BuildClient(BuildClientRequest{
		Mode:        config.AuthModeECSRole,
		ConfigPath:  "/tmp/config.json",
		ProfileName: "profile",
		Config:      original,
		Profile:     dynamicRuntimeProfile(),
		Factory:     factory,
	})
	if err != nil {
		t.Fatalf("BuildClient: %v", err)
	}
	if client.Endpoint != "https://runtime-endpoint" ||
		client.Region != "runtime-region" ||
		client.Service != "TLS" ||
		client.Timeout != 17*time.Second {
		t.Fatalf("client=%+v", client)
	}
	got := factory.gotCfg.Profiles["profile"]
	if got.Region != "runtime-region" || got.Endpoint != "runtime-endpoint" || got.TimeoutSeconds != 17 {
		t.Fatalf("factory profile=%+v", got)
	}
	if original.Profiles["profile"].Region != "old-region" {
		t.Fatal("BuildClient mutated caller config")
	}
}

func TestBuildClientProviderErrorFailsClosed(t *testing.T) {
	providerErr := errors.New("refresh failed")
	provider := &fakeProvider{err: providerErr}
	factory := &fakeProviderFactory{provider: provider}
	client, err := BuildClient(BuildClientRequest{
		Mode:        config.AuthModeConsoleLogin,
		ProfileName: "profile",
		Profile:     dynamicRuntimeProfile(),
		Factory:     factory,
	})
	if err != nil {
		t.Fatalf("BuildClient: %v", err)
	}
	rt := &countingRoundTripper{}
	client.HTTP = &http.Client{Transport: rt}
	_, err = client.Do(context.Background(), http.MethodGet, "/x", nil, nil, nil)
	if !errors.Is(err, providerErr) {
		t.Fatalf("Do error=%v, want wrapped provider error", err)
	}
	var dynamicErr *DynamicAuthError
	if !errors.As(err, &dynamicErr) || dynamicErr.AuthMode() != config.AuthModeConsoleLogin {
		t.Fatalf("Do error=%T %v, want DynamicAuthError", err, err)
	}
	if err.Error() != providerErr.Error() {
		t.Fatalf("public error=%q, want %q", err.Error(), providerErr.Error())
	}
	if rt.calls != 0 {
		t.Fatalf("HTTP calls=%d, want 0", rt.calls)
	}
}

func TestDynamicAuthErrorConstructionAndAccessors(t *testing.T) {
	cause := errors.New("refresh failed")
	err := NewDynamicAuthError(" console-login ", cause)
	if err.Error() != cause.Error() || !errors.Is(err, cause) {
		t.Fatalf("error=%v unwrap=%v", err, errors.Unwrap(err))
	}
	if err.AuthMode() != config.AuthModeConsoleLogin {
		t.Fatalf("authMode=%q", err.AuthMode())
	}
}

func TestBuildClientRetrievesCredentialsForEveryRequest(t *testing.T) {
	provider := &fakeProvider{values: []auth.Value{
		validAuthValue("first"),
		validAuthValue("second"),
	}}
	factory := &fakeProviderFactory{provider: provider}
	client, err := BuildClient(BuildClientRequest{
		Mode:        config.AuthModeOIDC,
		ProfileName: "profile",
		Profile:     dynamicRuntimeProfile(),
		Factory:     factory,
	})
	if err != nil {
		t.Fatalf("BuildClient: %v", err)
	}
	rt := &countingRoundTripper{}
	client.HTTP = &http.Client{Transport: rt}
	for i := 0; i < 2; i++ {
		if _, err := client.Do(context.Background(), http.MethodGet, "/x", nil, nil, nil); err != nil {
			t.Fatalf("Do %d: %v", i, err)
		}
	}
	if provider.calls != 2 || rt.calls != 2 {
		t.Fatalf("provider calls=%d HTTP calls=%d, want 2/2", provider.calls, rt.calls)
	}
}

func TestBuildClientRejectsUnsupportedModeAndFactoryError(t *testing.T) {
	_, err := BuildClient(BuildClientRequest{Mode: "future", Profile: dynamicRuntimeProfile(), Factory: &fakeProviderFactory{}})
	if err == nil || err.Error() != "unsupported auth mode: future" {
		t.Fatalf("unsupported error=%v", err)
	}
	want := errors.New("factory failed")
	_, err = BuildClient(BuildClientRequest{
		Mode:        config.AuthModeSSO,
		ProfileName: "profile",
		Profile:     dynamicRuntimeProfile(),
		Factory:     &fakeProviderFactory{err: want},
	})
	if !errors.Is(err, want) {
		t.Fatalf("factory error=%v", err)
	}
}

func TestBuildClientStaticCompatibility(t *testing.T) {
	client, err := BuildClient(BuildClientRequest{
		Mode:       config.AuthModeAK,
		SDKProfile: "explicit-selector",
		Profile: config.Profile{
			AccessKeyID:     "ak",
			SecretAccessKey: "sk",
			SecurityToken:   "token",
			Region:          "cn-test",
			Endpoint:        "endpoint.example.com",
			TimeoutSeconds:  9,
		},
	})
	if err != nil {
		t.Fatalf("BuildClient static: %v", err)
	}
	if client.Endpoint != "https://endpoint.example.com" ||
		client.Region != "cn-test" ||
		client.Service != "TLS" ||
		client.Timeout != 9*time.Second {
		t.Fatalf("client=%+v", client)
	}
}

func TestCacheDirectoryResolution(t *testing.T) {
	t.Setenv("VOLCLOG_SSO_CACHE_DIRECTORY", "")
	t.Setenv("VOLCLOG_LOGIN_CACHE_DIRECTORY", "")
	configPath := "/tmp/volclog/config.json"
	if got := ResolveSSOCacheDir(configPath); got != "/tmp/volclog/sso/cache" {
		t.Fatalf("ResolveSSOCacheDir=%q", got)
	}
	if got := ResolveLoginCacheDir(configPath); got != "/tmp/volclog/login/cache" {
		t.Fatalf("ResolveLoginCacheDir=%q", got)
	}
	t.Setenv("VOLCLOG_SSO_CACHE_DIRECTORY", " /custom/sso ")
	t.Setenv("VOLCLOG_LOGIN_CACHE_DIRECTORY", " /custom/login ")
	if got := ResolveSSOCacheDir(configPath); got != "/custom/sso" {
		t.Fatalf("override SSO=%q", got)
	}
	if got := ResolveLoginCacheDir(configPath); got != "/custom/login" {
		t.Fatalf("override login=%q", got)
	}
}

type fixedStatusReader struct{ status AuthStatus }

func (r fixedStatusReader) Status(context.Context, string) (AuthStatus, error) {
	return r.status, nil
}

type typedNilStatusReader struct{}

func (*typedNilStatusReader) Status(context.Context, string) (AuthStatus, error) {
	panic("typed-nil status reader must not be called")
}

func TestDynamicAuthStatusReader(t *testing.T) {
	status := AuthStatus{Provider: sso.ProviderName, Present: true}
	reader := fixedStatusReader{status: status}
	factory := &fakeProviderFactory{ssoRead: reader}

	got, err := DynamicAuthStatusReader(config.AuthModeSSO, "/tmp/config.json", "profile", config.Config{}, factory)
	if err != nil {
		t.Fatalf("DynamicAuthStatusReader: %v", err)
	}
	if got == nil {
		t.Fatal("expected SSO status reader")
	}
	resolved, err := got.Status(context.Background(), "profile")
	if err != nil || resolved != status {
		t.Fatalf("status=%+v err=%v", resolved, err)
	}

	var typedNil *typedNilFactory
	for _, mode := range []string{
		config.AuthModeAK,
		config.AuthModeRamRoleARN,
		config.AuthModeOIDC,
		config.AuthModeECSRole,
	} {
		got, err := DynamicAuthStatusReader(mode, "", "", config.Config{}, typedNil)
		if err != nil || got != nil {
			t.Fatalf("mode=%q reader=%v err=%v, want nil/nil", mode, got, err)
		}
	}
	_, err = DynamicAuthStatusReader(config.AuthModeConsoleLogin, "", "", config.Config{}, typedNil)
	if err == nil || err.Error() != "nil auth provider factory" {
		t.Fatalf("typed-nil cached-mode error=%v", err)
	}
	consoleStatus := AuthStatus{Provider: console.ProviderName, RefreshRequired: true}
	factory.loginRead = fixedStatusReader{status: consoleStatus}
	got, err = DynamicAuthStatusReader(config.AuthModeConsoleLogin, "", "profile", config.Config{}, factory)
	if err != nil || got == nil {
		t.Fatalf("DynamicAuthStatusReader reader=%v err=%v", got, err)
	}
}

func TestDynamicAuthStatusReaderRejectsTypedNilReader(t *testing.T) {
	var reader *typedNilStatusReader
	factory := &fakeProviderFactory{
		ssoRead:   reader,
		loginRead: reader,
	}

	for _, mode := range []string{config.AuthModeSSO, config.AuthModeConsoleLogin} {
		got, err := DynamicAuthStatusReader(mode, "", "profile", config.Config{}, factory)
		if got != nil || err == nil || err.Error() != "nil auth status reader" {
			t.Fatalf("mode=%q reader=%v error=%v", mode, got, err)
		}
	}
}

func TestSSOAuthStatusReaderOfflineStatus(t *testing.T) {
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	cache, err := sso.NewFileCache(filepath.Join(t.TempDir(), "sso"))
	if err != nil {
		t.Fatal(err)
	}
	reader := NewSSOAuthStatusReader(SSOAuthStatusReaderConfig{
		Cache:       cache,
		StartURL:    "https://example.com",
		SessionName: "session-a",
		AccountID:   "acct-1",
		RoleName:    "role-1",
		Region:      "cn-beijing",
		Clock:       func() time.Time { return now },
	})
	missing, err := reader.Status(context.Background(), "ignored")
	if err != nil {
		t.Fatalf("missing Status: %v", err)
	}
	if missing.Present || !missing.RefreshRequired || !missing.ExpiresAt.IsZero() {
		t.Fatalf("missing status=%+v", missing)
	}
	tokenExpiry := now.Add(2 * time.Hour)
	stsExpiry := now.Add(time.Hour)
	if err := cache.WriteToken(&sso.TokenCache{
		StartURL:     "https://example.com",
		SessionName:  "session-a",
		AccessToken:  "access-token",
		ExpiresAt:    tokenExpiry.Format(time.RFC3339),
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		Region:       "cn-beijing",
	}); err != nil {
		t.Fatal(err)
	}
	if err := cache.WriteSTS(&sso.STSCache{
		SessionName:     "session-a",
		AccountID:       "acct-1",
		RoleName:        "role-1",
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
		SessionToken:    "token",
		ProviderName:    sso.ProviderName,
		ExpiresAt:       stsExpiry.Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	present, err := reader.Status(context.Background(), "ignored")
	if err != nil {
		t.Fatalf("present Status: %v", err)
	}
	if !present.Present || present.RefreshRequired || !present.ExpiresAt.Equal(stsExpiry) {
		t.Fatalf("present status=%+v", present)
	}
}

func TestConsoleAuthStatusReaderOfflineStatus(t *testing.T) {
	now := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	cache, err := console.NewFileCache(filepath.Join(t.TempDir(), "login"))
	if err != nil {
		t.Fatal(err)
	}
	reader := NewConsoleAuthStatusReader(ConsoleAuthStatusReaderConfig{
		Cache:        cache,
		LoginSession: "session-a",
		Clock:        func() time.Time { return now },
	})
	missing, err := reader.Status(context.Background(), "ignored")
	if err != nil {
		t.Fatalf("missing Status: %v", err)
	}
	if missing.Present || !missing.RefreshRequired || !missing.ExpiresAt.IsZero() {
		t.Fatalf("missing status=%+v", missing)
	}
	sts, err := json.Marshal(console.STSCredentials{
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
		SessionToken:    "token",
	})
	if err != nil {
		t.Fatal(err)
	}
	cacheValue := console.LoginTokenCache{
		LoginSession: "session-a",
		AccessToken:  sts,
		Scope:        console.Scope,
		ClientID:     console.ClientIDSameDevice,
		IssuedAt:     now.Format(time.RFC3339),
		ExpiresIn:    3600,
		TokenType:    "sts",
	}
	data, err := json.Marshal(cacheValue)
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.WriteRaw("session-a", data); err != nil {
		t.Fatal(err)
	}
	present, err := reader.Status(context.Background(), "ignored")
	if err != nil {
		t.Fatalf("present Status: %v", err)
	}
	if !present.Present || present.RefreshRequired || !present.ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("present status=%+v", present)
	}
	if err := cache.WriteRaw("session-a", []byte(`not-json`)); err != nil {
		t.Fatal(err)
	}
	corrupt, err := reader.Status(context.Background(), "ignored")
	if err != nil {
		t.Fatalf("corrupt Status: %v", err)
	}
	if corrupt.Present || !corrupt.RefreshRequired || !corrupt.ExpiresAt.IsZero() {
		t.Fatalf("corrupt status=%+v", corrupt)
	}
}

func TestDefaultProviderFactoryValidationAndConstruction(t *testing.T) {
	factory := DefaultProviderFactory{}
	configPath := filepath.Join(t.TempDir(), "config.json")

	if _, _, err := factory.SSO(configPath, "missing", config.Config{}); err == nil || err.Error() != "profile not found: missing" {
		t.Fatalf("SSO missing profile error=%v", err)
	}
	ssoCfg := config.Config{Profiles: map[string]config.Profile{
		"sso": {SSOSessionName: "corp", AccountID: "2100", RoleName: "TLSReadOnly"},
	}}
	if _, _, err := factory.SSO(configPath, "sso", ssoCfg); err == nil || err.Error() != "sso session not found: corp" {
		t.Fatalf("SSO missing session error=%v", err)
	}
	ssoCfg.SSOSessions = map[string]config.SSOSession{
		"corp": {
			Name:     "corp",
			StartURL: "https://example.com/start",
			Region:   "cn-beijing",
		},
	}
	provider, reader, err := factory.SSO(configPath, "sso", ssoCfg)
	if err != nil || provider == nil || reader == nil {
		t.Fatalf("SSO construction provider=%v reader=%v err=%v", provider, reader, err)
	}

	if _, _, err := factory.Console(configPath, "missing", config.Config{}); err == nil || err.Error() != "profile not found: missing" {
		t.Fatalf("Console missing profile error=%v", err)
	}
	consoleCfg := config.Config{Profiles: map[string]config.Profile{
		"console": {LoginSession: "session"},
	}}
	provider, reader, err = factory.Console(configPath, "console", consoleCfg)
	if err != nil || provider == nil || reader == nil {
		t.Fatalf("Console construction provider=%v reader=%v err=%v", provider, reader, err)
	}

	if _, err := factory.RamRoleARN(configPath, "missing", config.Config{}); err == nil || err.Error() != "profile not found: missing" {
		t.Fatalf("RAM missing profile error=%v", err)
	}
	ramCfg := config.Config{Profiles: map[string]config.Profile{
		"ram": {AccountID: "2100", RoleName: "TLSReadOnly"},
	}}
	if _, err := factory.RamRoleARN(configPath, "ram", ramCfg); err == nil ||
		err.Error() != "RAM role source credentials are missing: set inline access_key_id/secret_access_key or cred_ref on the profile" {
		t.Fatalf("RAM missing source error=%v", err)
	}
	ram := ramCfg.Profiles["ram"]
	ram.AccessKeyID = "ak"
	ram.SecretAccessKey = "sk"
	ramCfg.Profiles["ram"] = ram
	if provider, err := factory.RamRoleARN(configPath, "ram", ramCfg); err != nil || provider == nil {
		t.Fatalf("RAM construction provider=%v err=%v", provider, err)
	}

	if _, err := factory.OIDC(configPath, "missing", config.Config{}); err == nil || err.Error() != "profile not found: missing" {
		t.Fatalf("OIDC missing profile error=%v", err)
	}
	oidcCfg := config.Config{Profiles: map[string]config.Profile{"oidc": {}}}
	if _, err := factory.OIDC(configPath, "oidc", oidcCfg); err == nil ||
		err.Error() != "OIDC token file is not configured: set oidc-token-file on the profile" {
		t.Fatalf("OIDC missing token error=%v", err)
	}
	oidcCfg.Profiles["oidc"] = config.Profile{
		OIDCTokenFile: "/tmp/token",
		RoleTRN:       "trn:iam::2100:role/TLSReadOnly",
	}
	if provider, err := factory.OIDC(configPath, "oidc", oidcCfg); err != nil || provider == nil {
		t.Fatalf("OIDC construction provider=%v err=%v", provider, err)
	}

	if _, err := factory.ECSRole(configPath, "missing", config.Config{}); err == nil || err.Error() != "profile not found: missing" {
		t.Fatalf("ECS missing profile error=%v", err)
	}
	ecsCfg := config.Config{Profiles: map[string]config.Profile{"ecs": {}}}
	if _, err := factory.ECSRole(configPath, "ecs", ecsCfg); err == nil ||
		err.Error() != "ECS role name is not configured: set role-name on the profile" {
		t.Fatalf("ECS missing role error=%v", err)
	}
	ecsCfg.Profiles["ecs"] = config.Profile{RoleName: "TLSReadOnly"}
	if provider, err := factory.ECSRole(configPath, "ecs", ecsCfg); err != nil || provider == nil {
		t.Fatalf("ECS construction provider=%v err=%v", provider, err)
	}
}

func TestResolveWorkloadSourceCredential(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ENV-POISON-AK")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "ENV-POISON-SK")
	tests := []struct {
		name    string
		profile config.Profile
		creds   map[string]config.Credential
		want    sts.SourceCredential
		wantErr bool
	}{
		{
			name:    "inline credentials and token",
			profile: config.Profile{AccessKeyID: " inline-ak ", SecretAccessKey: " inline-sk ", SecurityToken: " inline-token "},
			want:    sts.SourceCredential{AccessKeyID: "inline-ak", SecretAccessKey: "inline-sk", SessionToken: "inline-token"},
		},
		{
			name:    "credential reference",
			profile: config.Profile{CredRef: " source ", SecurityToken: "token"},
			creds:   map[string]config.Credential{"source": {AccessKeyID: "ref-ak", SecretAccessKey: "ref-sk"}},
			want:    sts.SourceCredential{AccessKeyID: "ref-ak", SecretAccessKey: "ref-sk", SessionToken: "token"},
		},
		{
			name:    "inline access key wins over reference",
			profile: config.Profile{AccessKeyID: "inline-ak", CredRef: "source"},
			creds:   map[string]config.Credential{"source": {AccessKeyID: "ref-ak", SecretAccessKey: "ref-sk"}},
			want:    sts.SourceCredential{AccessKeyID: "inline-ak", SecretAccessKey: "ref-sk"},
		},
		{
			name:    "inline secret wins over reference",
			profile: config.Profile{SecretAccessKey: "inline-sk", CredRef: "source"},
			creds:   map[string]config.Credential{"source": {AccessKeyID: "ref-ak", SecretAccessKey: "ref-sk"}},
			want:    sts.SourceCredential{AccessKeyID: "ref-ak", SecretAccessKey: "inline-sk"},
		},
		{name: "missing source ignores environment", wantErr: true},
		{name: "missing reference", profile: config.Profile{CredRef: "missing"}, wantErr: true},
		{
			name:    "reference missing access key",
			profile: config.Profile{CredRef: "source"},
			creds:   map[string]config.Credential{"source": {SecretAccessKey: "ref-sk"}},
			wantErr: true,
		},
		{
			name:    "reference missing secret",
			profile: config.Profile{CredRef: "source"},
			creds:   map[string]config.Credential{"source": {AccessKeyID: "ref-ak"}},
			wantErr: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := resolveWorkloadSourceCredential(test.profile, config.Config{Creds: test.creds})
			if test.wantErr {
				if err == nil {
					t.Fatal("error=nil, want failure")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("credential=%+v, want %+v", got, test.want)
			}
		})
	}
}

func dynamicRuntimeProfile() config.Profile {
	return config.Profile{
		Region:         "runtime-region",
		Endpoint:       "runtime-endpoint",
		TimeoutSeconds: 17,
	}
}

func validAuthValue(suffix string) auth.Value {
	return auth.Value{
		AccessKeyID:     "ak-" + suffix,
		SecretAccessKey: "sk-" + suffix,
		SessionToken:    "token-" + suffix,
	}
}

type countingRoundTripper struct{ calls int }

func (r *countingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	r.calls++
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{}`)),
	}, nil
}
