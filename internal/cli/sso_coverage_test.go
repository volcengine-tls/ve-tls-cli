package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/volcengine-tls/ve-tls-cli/internal/config"
)

// --- runSSOGroup dispatch ---

func TestSSOGroupDispatchNoArgsShowsUsage(t *testing.T) {
	ctx, _ := newSSOTestContext(t, testConfigWithSession())
	_, err := runSSOGroup(ctx, nil, func(_ *Context) (*ssoAdapter, error) {
		t.Fatal("factory should not be called")
		return nil, nil
	})
	if err == nil {
		t.Fatal("expected usage error for no args")
	}
	var ue *usageError
	if !errors.As(err, &ue) {
		t.Fatalf("expected usageError, got %T", err)
	}
}

func TestSSOGroupDispatchHelp(t *testing.T) {
	ctx, _ := newSSOTestContext(t, testConfigWithSession())
	_, err := runSSOGroup(ctx, []string{"--help"}, func(_ *Context) (*ssoAdapter, error) {
		t.Fatal("factory should not be called")
		return nil, nil
	})
	var ue *usageError
	if !errors.As(err, &ue) || ue.ExitCode != 0 {
		t.Fatalf("expected usageError exit 0, got %v", err)
	}
}

func TestSSOGroupDispatchUnknownCommand(t *testing.T) {
	ctx, _ := newSSOTestContext(t, testConfigWithSession())
	_, err := runSSOGroup(ctx, []string{"bogus"}, func(_ *Context) (*ssoAdapter, error) {
		t.Fatal("factory should not be called")
		return nil, nil
	})
	if err == nil || !strings.Contains(err.Error(), "unknown sso command") {
		t.Fatalf("expected unknown command error, got %v", err)
	}
}

func TestSSOGroupDispatchLoginAndLogout(t *testing.T) {
	ctx, _ := newSSOTestContext(t, testConfigWithSession())
	loginCalled := false
	_, err := runSSOGroup(ctx, []string{"login", "--sso-session", "corp"}, func(_ *Context) (*ssoAdapter, error) {
		loginCalled = true
		return nil, errors.New("stop")
	})
	if !loginCalled {
		t.Fatal("login factory should be called")
	}
	if err == nil {
		t.Fatal("expected error from login")
	}

	logoutCalled := false
	_, err = runSSOGroup(ctx, []string{"logout", "--sso-session", "corp"}, func(_ *Context) (*ssoAdapter, error) {
		logoutCalled = true
		return nil, errors.New("stop")
	})
	if !logoutCalled {
		t.Fatal("logout factory should be called")
	}
	if err == nil {
		t.Fatal("expected error from logout")
	}
}

// --- Flag parsing: missing values / unknown flags ---

func TestSSOLoginFlagsMissingValues(t *testing.T) {
	cases := [][]string{
		{"--profile"},
		{"--sso-session"},
		{"--unknown-flag"},
		{"positional"},
	}
	for _, args := range cases {
		_, err := parseSSOLoginFlags(args)
		if err == nil {
			t.Fatalf("expected error for args %v", args)
		}
	}
}

func TestSSOLogoutFlagsMissingValues(t *testing.T) {
	cases := [][]string{
		{"--profile"},
		{"--sso-session"},
		{"--unknown-flag"},
		{"positional"},
	}
	for _, args := range cases {
		_, err := parseSSOLogoutFlags(args)
		if err == nil {
			t.Fatalf("expected error for args %v", args)
		}
	}
}

func TestSSOConfigureFlagsMissingValues(t *testing.T) {
	cases := [][]string{
		{"--profile"},
		{"--sso-session"},
		{"--account-id"},
		{"--role-name"},
		{"--unknown-flag"},
		{"positional"},
	}
	for _, args := range cases {
		_, err := parseSSOConfigureFlags(args)
		if err == nil {
			t.Fatalf("expected error for args %v", args)
		}
	}
}

func TestSSOSessionFlagsMissingValues(t *testing.T) {
	cases := [][]string{
		{"--name"},
		{"--start-url"},
		{"--region"},
		{"--registration-scopes"},
		{"--unknown-flag"},
		{"positional"},
	}
	for _, args := range cases {
		_, err := parseSSOSessionFlags(args)
		if err == nil {
			t.Fatalf("expected error for args %v", args)
		}
	}
}

// --- runSSOLogin error paths ---

func TestSSOLoginNilAdapterAndDeps(t *testing.T) {
	// nil adapter
	var a *ssoAdapter
	_, err := a.runSSOLogin(context.Background(), ssoLoginOpts{SSOSession: "corp"})
	if err == nil {
		t.Fatal("expected error for nil adapter")
	}

	// nil cache
	a = &ssoAdapter{}
	_, err = a.runSSOLogin(context.Background(), ssoLoginOpts{SSOSession: "corp"})
	if err == nil {
		t.Fatal("expected error for nil cache")
	}

	// nil cfgStore
	a = &ssoAdapter{cache: newFakeSSOCache()}
	_, err = a.runSSOLogin(context.Background(), ssoLoginOpts{SSOSession: "corp"})
	if err == nil {
		t.Fatal("expected error for nil cfgStore")
	}
}

func TestSSOLoginProfileNotFoundOrNotSSO(t *testing.T) {
	cfg := testConfigWithSession()
	cache := newFakeSSOCache()
	cfgStore := &fakeConfigStore{cfg: cfg, path: ""}
	adapter := newSSOAdapterForTest(cache, cfgStore, &fakeSSODeviceFlow{token: testTokenCache()}, nil, nil)

	_, err := adapter.runSSOLogin(context.Background(), ssoLoginOpts{Profile: "nonexistent"})
	if err == nil || !strings.Contains(err.Error(), "profile not found") {
		t.Fatalf("expected profile not found, got %v", err)
	}

	// default profile is console-login mode, not sso
	_, err = adapter.runSSOLogin(context.Background(), ssoLoginOpts{Profile: "default"})
	if err == nil || !strings.Contains(err.Error(), "not an sso profile") {
		t.Fatalf("expected not sso profile error, got %v", err)
	}
}

func TestSSOLoginProfileWithoutSessionBinding(t *testing.T) {
	cfg := testConfigWithSession()
	p := cfg.Profiles["default"]
	p.Mode = config.AuthModeSSO
	p.SSOSessionName = ""
	cfg.Profiles["default"] = p
	cache := newFakeSSOCache()
	cfgStore := &fakeConfigStore{cfg: cfg, path: ""}
	adapter := newSSOAdapterForTest(cache, cfgStore, &fakeSSODeviceFlow{token: testTokenCache()}, nil, nil)
	_, err := adapter.runSSOLogin(context.Background(), ssoLoginOpts{Profile: "default"})
	if err == nil || !strings.Contains(err.Error(), "no sso-session binding") {
		t.Fatalf("expected no binding error, got %v", err)
	}
}

func TestSSOLoginSessionNotFound(t *testing.T) {
	cfg := testConfigWithSession()
	cache := newFakeSSOCache()
	cfgStore := &fakeConfigStore{cfg: cfg, path: ""}
	adapter := newSSOAdapterForTest(cache, cfgStore, &fakeSSODeviceFlow{token: testTokenCache()}, nil, nil)
	_, err := adapter.runSSOLogin(context.Background(), ssoLoginOpts{SSOSession: "missing"})
	if err == nil || !strings.Contains(err.Error(), "sso session not found") {
		t.Fatalf("expected session not found, got %v", err)
	}
}

func TestSSOLoginNilDeviceFlowFactory(t *testing.T) {
	cfg := testConfigWithSession()
	cache := newFakeSSOCache()
	cfgStore := &fakeConfigStore{cfg: cfg, path: ""}
	adapter := &ssoAdapter{cache: cache, cfgStore: cfgStore}
	_, err := adapter.runSSOLogin(context.Background(), ssoLoginOpts{SSOSession: "corp"})
	if err == nil || !strings.Contains(err.Error(), "nil device flow factory") {
		t.Fatalf("expected nil factory error, got %v", err)
	}
}

func TestSSOLoginFactoryReturnsError(t *testing.T) {
	cfg := testConfigWithSession()
	cache := newFakeSSOCache()
	cfgStore := &fakeConfigStore{cfg: cfg, path: ""}
	adapter := &ssoAdapter{
		cache:    cache,
		cfgStore: cfgStore,
		deviceFlowFn: func(_ config.SSOSession, _ bool) (ssoDeviceFlow, error) {
			return nil, errors.New("factory boom")
		},
	}
	_, err := adapter.runSSOLogin(context.Background(), ssoLoginOpts{SSOSession: "corp"})
	if err == nil || !strings.Contains(err.Error(), "build sso device flow failed") {
		t.Fatalf("expected factory error, got %v", err)
	}
}

func TestSSOLoginMissingSelectorDefaultsToCurrentProfile(t *testing.T) {
	ctx, _ := newSSOTestContext(t, testConfigWithSession())
	called := false
	_, err := runSSOLoginWithFactory(ctx, nil, func(_ *Context) (*ssoAdapter, error) {
		called = true
		return nil, errors.New("factory invoked")
	})
	// The factory must be called: missing selector now defaults to the current
	// profile instead of being rejected before any side effect.
	if !called {
		t.Fatal("factory should be called when no selector is provided")
	}
	if err == nil {
		t.Fatal("expected error from factory")
	}
}

// --- runSSOLogout error paths ---

func TestSSOLogoutNilAdapterAndDeps(t *testing.T) {
	var a *ssoAdapter
	_, err := a.runSSOLogout(context.Background(), ssoLogoutOpts{SSOSession: "corp"})
	if err == nil {
		t.Fatal("expected error for nil adapter")
	}
	a = &ssoAdapter{}
	_, err = a.runSSOLogout(context.Background(), ssoLogoutOpts{SSOSession: "corp"})
	if err == nil {
		t.Fatal("expected error for nil cache")
	}
	a = &ssoAdapter{cache: newFakeSSOCache()}
	_, err = a.runSSOLogout(context.Background(), ssoLogoutOpts{SSOSession: "corp"})
	if err == nil {
		t.Fatal("expected error for nil cfgStore")
	}
}

func TestSSOLogoutMissingSelectorDefaultsToCurrentProfile(t *testing.T) {
	ctx, _ := newSSOTestContext(t, testConfigWithSession())
	called := false
	_, err := runSSOLogoutWithFactory(ctx, nil, func(_ *Context) (*ssoAdapter, error) {
		called = true
		return nil, errors.New("factory invoked")
	})
	// The factory must be called: missing selector now defaults to the current
	// profile instead of being rejected before any side effect.
	if !called {
		t.Fatal("factory should be called when no selector is provided")
	}
	if err == nil {
		t.Fatal("expected error from factory")
	}
}

func TestSSOLogoutProfileNotFoundReturnsEmpty(t *testing.T) {
	cfg := testConfigWithSession()
	cache := newFakeSSOCache()
	cfgStore := &fakeConfigStore{cfg: cfg, path: ""}
	adapter := newSSOAdapterForTest(cache, cfgStore, nil, nil, &fakeSSORevoker{})
	res, err := adapter.runSSOLogout(context.Background(), ssoLogoutOpts{Profile: "nonexistent"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.ClearedSession {
		t.Fatal("should not clear session for missing profile")
	}
}

func TestSSOLogoutProfileWithoutBindingReturnsEmpty(t *testing.T) {
	cfg := testConfigWithSession()
	p := cfg.Profiles["default"]
	p.Mode = config.AuthModeSSO
	p.SSOSessionName = ""
	cfg.Profiles["default"] = p
	cache := newFakeSSOCache()
	cfgStore := &fakeConfigStore{cfg: cfg, path: ""}
	adapter := newSSOAdapterForTest(cache, cfgStore, nil, nil, &fakeSSORevoker{})
	res, err := adapter.runSSOLogout(context.Background(), ssoLogoutOpts{Profile: "default"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.ClearedSession {
		t.Fatal("should not clear session for profile without binding")
	}
}

func TestSSOLogoutSessionNotFoundReturnsEmpty(t *testing.T) {
	cfg := testConfigWithSession()
	cache := newFakeSSOCache()
	cfgStore := &fakeConfigStore{cfg: cfg, path: ""}
	adapter := newSSOAdapterForTest(cache, cfgStore, nil, nil, &fakeSSORevoker{})
	res, err := adapter.runSSOLogout(context.Background(), ssoLogoutOpts{SSOSession: "missing"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.ClearedSession {
		t.Fatal("should not clear session for missing session")
	}
}

// --- runConfigureSSO error paths ---

func TestConfigureSSONilAdapterAndDeps(t *testing.T) {
	var a *ssoAdapter
	_, err := a.runConfigureSSO(context.Background(), ssoConfigureOpts{SSOSession: "corp"})
	if err == nil {
		t.Fatal("expected error for nil adapter")
	}
	a = &ssoAdapter{}
	_, err = a.runConfigureSSO(context.Background(), ssoConfigureOpts{SSOSession: "corp"})
	if err == nil {
		t.Fatal("expected error for nil cache")
	}
	a = &ssoAdapter{cache: newFakeSSOCache()}
	_, err = a.runConfigureSSO(context.Background(), ssoConfigureOpts{SSOSession: "corp"})
	if err == nil {
		t.Fatal("expected error for nil cfgStore")
	}
}

func TestConfigureSSOSessionNotFound(t *testing.T) {
	cfg := testConfigWithSession()
	cache := newFakeSSOCache()
	cfgStore := &fakeConfigStore{cfg: cfg, path: ""}
	adapter := newSSOAdapterForTest(cache, cfgStore, &fakeSSODeviceFlow{token: testTokenCache()}, &fakeSSOBindingService{}, nil)
	_, err := adapter.runConfigureSSO(context.Background(), ssoConfigureOpts{Profile: "default", SSOSession: "missing"})
	if err == nil || !strings.Contains(err.Error(), "sso session not found") {
		t.Fatalf("expected session not found, got %v", err)
	}
}

func TestConfigureSSOMissingSession(t *testing.T) {
	ctx, _ := newSSOTestContext(t, testConfigWithSession())
	_, err := runConfigureSSOWithFactory(ctx, []string{"--profile", "default"}, func(_ *Context) (*ssoAdapter, error) {
		t.Fatal("factory should not be called")
		return nil, nil
	})
	if err == nil || !strings.Contains(err.Error(), "missing required field: --sso-session") {
		t.Fatalf("expected missing sso-session error, got %v", err)
	}
}

func TestConfigureSSONilDeviceFlowFactory(t *testing.T) {
	cfg := testConfigWithSession()
	cache := newFakeSSOCache()
	cfgStore := &fakeConfigStore{cfg: cfg, path: ""}
	adapter := &ssoAdapter{cache: cache, cfgStore: cfgStore}
	_, err := adapter.runConfigureSSO(context.Background(), ssoConfigureOpts{Profile: "default", SSOSession: "corp"})
	if err == nil || !strings.Contains(err.Error(), "nil device flow factory") {
		t.Fatalf("expected nil factory error, got %v", err)
	}
}

func TestConfigureSSONilBindingFactory(t *testing.T) {
	cfg := testConfigWithSession()
	cache := newFakeSSOCache()
	cfgStore := &fakeConfigStore{cfg: cfg, path: ""}
	adapter := &ssoAdapter{
		cache:    cache,
		cfgStore: cfgStore,
		deviceFlowFn: func(_ config.SSOSession, _ bool) (ssoDeviceFlow, error) {
			return &fakeSSODeviceFlow{token: testTokenCache()}, nil
		},
	}
	_, err := adapter.runConfigureSSO(context.Background(), ssoConfigureOpts{Profile: "default", SSOSession: "corp"})
	if err == nil || !strings.Contains(err.Error(), "nil binding service factory") {
		t.Fatalf("expected nil binding factory error, got %v", err)
	}
}

func TestConfigureSSOFactoryErrors(t *testing.T) {
	cfg := testConfigWithSession()
	cache := newFakeSSOCache()
	cfgStore := &fakeConfigStore{cfg: cfg, path: ""}

	// device flow factory error
	adapter := &ssoAdapter{
		cache:    cache,
		cfgStore: cfgStore,
		deviceFlowFn: func(_ config.SSOSession, _ bool) (ssoDeviceFlow, error) {
			return nil, errors.New("df boom")
		},
		bindingFn: func(_ config.SSOSession) (ssoBindingService, error) {
			return &fakeSSOBindingService{}, nil
		},
	}
	_, err := adapter.runConfigureSSO(context.Background(), ssoConfigureOpts{Profile: "default", SSOSession: "corp"})
	if err == nil || !strings.Contains(err.Error(), "build sso device flow failed") {
		t.Fatalf("expected df factory error, got %v", err)
	}

	// binding factory error
	adapter = &ssoAdapter{
		cache:    cache,
		cfgStore: cfgStore,
		deviceFlowFn: func(_ config.SSOSession, _ bool) (ssoDeviceFlow, error) {
			return &fakeSSODeviceFlow{token: testTokenCache()}, nil
		},
		bindingFn: func(_ config.SSOSession) (ssoBindingService, error) {
			return nil, errors.New("binding boom")
		},
	}
	_, err = adapter.runConfigureSSO(context.Background(), ssoConfigureOpts{Profile: "default", SSOSession: "corp"})
	if err == nil || !strings.Contains(err.Error(), "build sso binding service failed") {
		t.Fatalf("expected binding factory error, got %v", err)
	}
}

// --- runConfigureSSOSession error paths ---

func TestConfigureSSOSessionMissingRequired(t *testing.T) {
	ctx, _ := newSSOTestContext(t, config.DefaultConfig())
	cases := [][]string{
		{},
		{"--name", "corp"},
		{"--name", "corp", "--start-url", "https://example.volccloudidentity.com/userportal"},
	}
	for _, args := range cases {
		_, err := runConfigureSSOSession(ctx, args)
		if err == nil {
			t.Fatalf("expected error for args %v", args)
		}
	}
}

func TestConfigureSSOSessionInvalidStartURL(t *testing.T) {
	ctx, _ := newSSOTestContext(t, config.DefaultConfig())
	_, err := runConfigureSSOSession(ctx, []string{
		"--name", "corp",
		"--start-url", "not-a-url",
		"--region", "cn-beijing",
	})
	if err == nil {
		t.Fatal("expected error for invalid start-url")
	}
}

// --- Production adapter factory builds without panic (region wiring) ---

func TestNewProductionSSOAdapterBuilds(t *testing.T) {
	ctx, _ := newSSOTestContext(t, testConfigWithSession())
	t.Setenv("HOME", t.TempDir())
	a, err := newProductionSSOAdapter(ctx)
	if err != nil {
		t.Fatalf("newProductionSSOAdapter: %v", err)
	}
	if a == nil {
		t.Fatal("nil adapter")
	}
	if a.cache == nil || a.cfgStore == nil || a.deviceFlowFn == nil || a.bindingFn == nil || a.revokerFn == nil {
		t.Fatal("production adapter missing dependencies")
	}
}
