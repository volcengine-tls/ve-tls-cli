package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth/console"
	"github.com/volcengine-tls/ve-tls-cli/internal/auth/sso"
	"github.com/volcengine-tls/ve-tls-cli/internal/config"
	"github.com/volcengine-tls/ve-tls-cli/internal/output"
	"github.com/volcengine-tls/ve-tls-cli/internal/securestore"
)

// readProbeC6 wraps an open pipe but fails immediately if a callback attempts
// to read it, so a regression is observable without leaving a blocked reader.
type readProbeC6 struct {
	pipe  io.Reader
	reads int
}

func (r *readProbeC6) Read([]byte) (int, error) {
	r.reads++
	return 0, errors.New("unexpected read from open pipe")
}

func openProbeC6(t *testing.T) *readProbeC6 {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = reader.Close()
		_ = writer.Close()
	})
	return &readProbeC6{pipe: reader}
}

func requireInteractionC6(t *testing.T, err error, hint string) {
	t.Helper()
	var interaction *NonInteractiveInteractionError
	if !errors.As(err, &interaction) || interaction.ErrorKind() != "interaction_required" || interaction.ErrorCode() != "InteractionRequired" || !strings.Contains(interaction.Hint, hint) {
		t.Fatalf("interaction error = %#v", err)
	}
}

func TestNonInteractiveCallbacksDoNotReadOpenPipeC6(t *testing.T) {
	probe := openProbeC6(t)
	var stderr bytes.Buffer
	confirm := newInvocationConfirmPrompt(context.Background(), true, probe, &stderr)
	accepted, err := confirm("profile", "old-session", "new-session")
	if accepted {
		t.Fatal("session replacement was implicitly accepted")
	}
	requireInteractionC6(t, err, "--profile")
	if strings.Contains(err.Error(), "old-session") || strings.Contains(err.Error(), "new-session") {
		t.Fatal("confirmation error disclosed a session")
	}

	accounts := []sso.AccountInfo{{AccountID: "a1"}, {AccountID: "a2"}}
	_, err = newInvocationAccountSelector(context.Background(), true, probe, &stderr)(accounts)
	requireInteractionC6(t, err, "--account-id")
	roles := []sso.RoleInfo{{RoleName: "r1"}, {RoleName: "r2"}}
	_, err = newInvocationRoleSelector(context.Background(), true, probe, &stderr)(roles)
	requireInteractionC6(t, err, "--role-name")
	if probe.reads != 0 || stderr.Len() != 0 {
		t.Fatalf("unexpected input or prompt: reads=%d stderr=%q", probe.reads, stderr.String())
	}
}

func TestNonInteractiveSelectorsPreserveZeroAndSingleC6(t *testing.T) {
	probe := openProbeC6(t)
	var stderr bytes.Buffer
	account := newInvocationAccountSelector(context.Background(), true, probe, &stderr)
	role := newInvocationRoleSelector(context.Background(), true, probe, &stderr)
	if _, err := account(nil); err == nil {
		t.Fatal("empty account list succeeded")
	}
	if _, err := role(nil); err == nil {
		t.Fatal("empty role list succeeded")
	}
	if got, err := account([]sso.AccountInfo{{AccountID: "a1"}}); err != nil || got.AccountID != "a1" {
		t.Fatalf("single account=%#v err=%v", got, err)
	}
	if got, err := role([]sso.RoleInfo{{RoleName: "r1"}}); err != nil || got.RoleName != "r1" {
		t.Fatalf("single role=%#v err=%v", got, err)
	}
	if probe.reads != 0 || stderr.Len() != 0 {
		t.Fatalf("single selection read input or printed: reads=%d stderr=%q", probe.reads, stderr.String())
	}
}

func TestInteractionCallbacksHonorCanceledContextC6(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	probe := openProbeC6(t)
	var stderr bytes.Buffer
	if _, err := newInvocationConfirmPrompt(ctx, false, probe, &stderr)("profile", "old", "new"); !errors.Is(err, context.Canceled) {
		t.Fatalf("confirmation error=%v", err)
	}
	if _, err := newInvocationAccountSelector(ctx, true, probe, &stderr)([]sso.AccountInfo{{AccountID: "a1"}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("account error=%v", err)
	}
	if _, err := newInvocationRoleSelector(ctx, true, probe, &stderr)([]sso.RoleInfo{{RoleName: "r1"}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("role error=%v", err)
	}
	if probe.reads != 0 || stderr.Len() != 0 {
		t.Fatalf("canceled callback read or prompted: reads=%d stderr=%q", probe.reads, stderr.String())
	}
}

type cancelingReaderC6 struct {
	reader io.Reader
	cancel context.CancelFunc
}

func (r cancelingReaderC6) Read(p []byte) (int, error) {
	r.cancel()
	return r.reader.Read(p)
}

func TestInteractiveCallbackRechecksContextC6(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader := cancelingReaderC6{reader: strings.NewReader("yes\n"), cancel: cancel}
	accepted, err := newInvocationConfirmPrompt(ctx, false, reader, &bytes.Buffer{})("profile", "old", "new")
	if accepted || !errors.Is(err, context.Canceled) {
		t.Fatalf("accepted=%v err=%v", accepted, err)
	}
}

func TestNonInteractiveLoginUsesDeviceCodeC6(t *testing.T) {
	for _, tc := range []struct {
		name           string
		args           []string
		nonInteractive bool
		device         bool
		noBrowser      bool
		remote         bool
	}{
		{"noninteractive", nil, true, true, true, false},
		{"remote preserved", []string{"--remote"}, true, true, true, true},
		{"interactive default", nil, false, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := newContext(&bytes.Buffer{}, &bytes.Buffer{}, output.FormatJSON, "", "")
			ctx.NonInteractive = tc.nonInteractive
			service := &fakeLoginService{result: &loginResult{Profile: "default"}}
			_, err := runLoginWithFactory(ctx, tc.args, func(*Context) (*loginAdapter, error) {
				return &loginAdapter{loginSvc: service}, nil
			})
			if err != nil || !service.called || service.opts.DeviceCode != tc.device || service.opts.NoBrowser != tc.noBrowser || service.opts.Remote != tc.remote {
				t.Fatalf("opts=%#v err=%v", service.opts, err)
			}
		})
	}
}

type portalC6 struct {
	accounts []sso.AccountInfo
	roles    []sso.RoleInfo
}

func (p *portalC6) ListAccounts(context.Context, string) ([]sso.AccountInfo, error) {
	return p.accounts, nil
}

func (p *portalC6) ListAccountRoles(context.Context, string, string) ([]sso.RoleInfo, error) {
	return p.roles, nil
}

func TestNonInteractiveConfigureSelectionTransactionC6(t *testing.T) {
	for _, tc := range []struct {
		name     string
		accounts []sso.AccountInfo
		roles    []sso.RoleInfo
		args     []string
		wantHint string
		hadToken bool
	}{
		{"multiple accounts", []sso.AccountInfo{{AccountID: "a1"}, {AccountID: "a2"}}, []sso.RoleInfo{{AccountID: "a1", RoleName: "r1"}}, nil, "--account-id", false},
		{"multiple accounts with prior token", []sso.AccountInfo{{AccountID: "a1"}, {AccountID: "a2"}}, []sso.RoleInfo{{AccountID: "a1", RoleName: "r1"}}, nil, "--account-id", true},
		{"multiple roles", []sso.AccountInfo{{AccountID: "a1"}}, []sso.RoleInfo{{AccountID: "a1", RoleName: "r1"}, {AccountID: "a1", RoleName: "r2"}}, nil, "--role-name", false},
		{"single candidates", []sso.AccountInfo{{AccountID: "a1"}}, []sso.RoleInfo{{AccountID: "a1", RoleName: "r1"}}, nil, "", false},
		{"explicit candidates", []sso.AccountInfo{{AccountID: "a1"}, {AccountID: "a2"}}, []sso.RoleInfo{{AccountID: "a1", RoleName: "r1"}, {AccountID: "a1", RoleName: "r2"}}, []string{"--account-id", "a1", "--role-name", "r1"}, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			probe := openProbeC6(t)
			var stdout, stderr bytes.Buffer
			ctx := newContext(&stdout, &stderr, output.FormatJSON, "", "")
			ctx.NonInteractive = true
			cfg := testConfigWithSession()
			originalProfile := cfg.Profiles["default"]
			store := &fakeConfigStore{cfg: cfg}
			cache := newFakeSSOCache()
			priorToken := testTokenCache()
			priorToken.AccessToken = "previous-token"
			if tc.hadToken {
				cache.tokens[ssoTokenKey(priorToken.StartURL, priorToken.SessionName)] = priorToken
			}
			flow := &fakeSSODeviceFlow{token: testTokenCache()}
			binding := sso.NewBindingService(&sso.BindingServiceConfig{
				Portal:        &portalC6{accounts: tc.accounts, roles: tc.roles},
				SelectAccount: newInvocationAccountSelector(ctx.ExecutionContext(), true, probe, &stderr),
				SelectRole:    newInvocationRoleSelector(ctx.ExecutionContext(), true, probe, &stderr),
			})
			adapter := newSSOAdapterForTest(cache, store, flow, binding, nil)
			var noBrowser bool
			adapter.deviceFlowFn = func(_ config.SSOSession, noBrowserArg bool) (ssoDeviceFlow, error) {
				noBrowser = noBrowserArg
				return flow, nil
			}
			args := append([]string{"--sso-session", "corp"}, tc.args...)
			_, err := runConfigureSSOWithFactory(ctx, args, func(*Context) (*ssoAdapter, error) { return adapter, nil })
			if !noBrowser {
				t.Fatal("noninteractive configure did not suppress browser")
			}
			if tc.wantHint != "" {
				requireInteractionC6(t, err, tc.wantHint)
				if atomic.LoadInt32(&store.updateCnt) != 0 || !reflect.DeepEqual(store.cfg.Profiles["default"], originalProfile) {
					t.Fatal("failed selection committed profile changes")
				}
				restored, cacheErr := cache.ReadToken(testSSOSession().StartURL, testSSOSession().Name)
				// Device flow writes a token before selection; failure rolls it back.
				if tc.hadToken {
					if cacheErr != nil || !tokensEqual(restored, priorToken) || atomic.LoadInt32(&cache.writeTokenCnt) != 2 || atomic.LoadInt32(&cache.deleteTokenCnt) != 0 {
						t.Fatalf("prior token not restored: err=%v writes=%d deletes=%d", cacheErr, cache.writeTokenCnt, cache.deleteTokenCnt)
					}
				} else if !errors.Is(cacheErr, securestore.ErrMissing) || atomic.LoadInt32(&cache.writeTokenCnt) != 1 || atomic.LoadInt32(&cache.deleteTokenCnt) != 1 {
					t.Fatalf("missing token state not restored: err=%v writes=%d deletes=%d", cacheErr, cache.writeTokenCnt, cache.deleteTokenCnt)
				}
			} else if err != nil || atomic.LoadInt32(&store.updateCnt) != 1 {
				t.Fatalf("configure error=%v updates=%d", err, store.updateCnt)
			}
			if probe.reads != 0 || stderr.Len() != 0 {
				t.Fatalf("unexpected input or prompt: reads=%d stderr=%q", probe.reads, stderr.String())
			}
		})
	}
}

type browserC6 struct{ calls int }

func (b *browserC6) Open(context.Context, string) error {
	b.calls++
	return nil
}

type consoleOAuthC6 struct{}

func (*consoleOAuthC6) BuildAuthorizeURL(*console.AuthorizeParams) (string, error) {
	return "https://example.com/authorize", nil
}

func (*consoleOAuthC6) EndpointURL() string { return "https://example.com" }

func (*consoleOAuthC6) StartDeviceAuthorization(context.Context, *console.ConsoleDeviceAuthorizationRequest) (*console.ConsoleDeviceAuthorizationResponse, error) {
	return &console.ConsoleDeviceAuthorizationResponse{DeviceCode: "device", UserCode: "code", VerificationURI: "https://example.com/verify", ExpiresIn: 30, Interval: 1}, nil
}

func (*consoleOAuthC6) ExchangeToken(context.Context, *console.ConsoleTokenRequest) (*console.ConsoleTokenResponse, error) {
	return &console.ConsoleTokenResponse{AccessToken: validSTSAccessTokenForTest(), IDToken: validIDTokenForTest("trn:iam::1:user/new"), TokenType: "Bearer", ExpiresIn: 3600}, nil
}

func TestNonInteractiveConsoleDeviceFlowC6(t *testing.T) {
	for _, existingSession := range []string{"", "trn:iam::1:user/old"} {
		t.Run(existingSession, func(t *testing.T) {
			cache, err := console.NewFileCache(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			cfg := config.DefaultConfig()
			cfg.Profiles["default"] = config.Profile{LoginSession: existingSession}
			store := &fakeConfigStore{cfg: cfg}
			probe := openProbeC6(t)
			var prompt bytes.Buffer
			browser := &browserC6{}
			ctx := newContext(&bytes.Buffer{}, &bytes.Buffer{}, output.FormatJSON, "", "")
			ctx.NonInteractive = true
			svc := console.NewLoginService(&console.LoginServiceConfig{
				OAuthClientFactory: func(string) (console.OAuthClient, error) { return &consoleOAuthC6{}, nil },
				Browser:            browser, Cache: cache, ProfileStore: store, Clock: time.Now,
				Sleeper: func(context.Context, time.Duration) error { return nil },
				Confirm: newInvocationConfirmPrompt(ctx.ExecutionContext(), true, probe, &prompt),
			})
			_, err = runLoginWithFactory(ctx, nil, func(*Context) (*loginAdapter, error) {
				return &loginAdapter{loginSvc: &consoleLoginServiceAdapter{svc: svc}}, nil
			})
			if existingSession != "" {
				requireInteractionC6(t, err, "--profile")
				if atomic.LoadInt32(&store.updateCnt) != 0 || store.cfg.Profiles["default"].LoginSession != existingSession {
					t.Fatal("unconfirmed session replacement changed profile")
				}
			} else if err != nil || atomic.LoadInt32(&store.updateCnt) != 1 {
				t.Fatalf("login error=%v updates=%d", err, store.updateCnt)
			}
			if browser.calls != 0 || probe.reads != 0 || prompt.Len() != 0 {
				t.Fatalf("unexpected interaction: browser=%d reads=%d prompt=%q", browser.calls, probe.reads, prompt.String())
			}
		})
	}
}

type oauthC6 struct{}

func (*oauthC6) RegisterClient(context.Context, *sso.RegisterClientRequest) (*sso.RegisterClientResponse, error) {
	return &sso.RegisterClientResponse{ClientID: "client", ClientSecret: "secret"}, nil
}

func (*oauthC6) StartDeviceAuthorization(context.Context, *sso.StartDeviceAuthorizationRequest) (*sso.StartDeviceAuthorizationResponse, error) {
	return &sso.StartDeviceAuthorizationResponse{DeviceCode: "device", UserCode: "code", VerificationURIComplete: "https://example.com/verify", ExpiresIn: 30, Interval: 1}, nil
}

func (*oauthC6) CreateToken(context.Context, *sso.CreateTokenRequest) (*sso.CreateTokenResponse, error) {
	return &sso.CreateTokenResponse{AccessToken: "token", TokenType: "Bearer", ExpiresIn: 3600}, nil
}

func TestNonInteractiveSSOLoginDoesNotOpenBrowserC6(t *testing.T) {
	cache, err := sso.NewFileCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := newContext(&bytes.Buffer{}, &bytes.Buffer{}, output.FormatJSON, "", "")
	ctx.NonInteractive = true
	browser := &browserC6{}
	store := &fakeConfigStore{cfg: testConfigWithSession()}
	adapter := &ssoAdapter{cache: cache, cfgStore: store, clock: time.Now}
	adapter.deviceFlowFn = func(session config.SSOSession, noBrowser bool) (ssoDeviceFlow, error) {
		return sso.NewDeviceFlow(&sso.DeviceFlowConfig{
			OAuth: &oauthC6{}, Cache: cache, Browser: browser, NoBrowser: noBrowser,
			Clock: time.Now, Sleeper: func(context.Context, time.Duration) error { return nil },
			StartURL: session.StartURL, SessionName: session.Name, Region: session.Region,
			Scopes: session.RegistrationScopes,
		}), nil
	}
	_, err = runSSOLoginWithFactory(ctx, []string{"--sso-session", "corp"}, func(*Context) (*ssoAdapter, error) { return adapter, nil })
	if err != nil || browser.calls != 0 {
		t.Fatalf("sso login error=%v browser calls=%d", err, browser.calls)
	}
}

func TestInputIsTerminalRejectsNullAndPipeC6(t *testing.T) {
	if inputIsTerminal(nil) {
		t.Fatal("nil input reported as terminal")
	}
	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	if inputIsTerminal(null) {
		t.Fatal("null device reported as terminal")
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	if inputIsTerminal(reader) || inputIsTerminal(writer) {
		t.Fatal("pipe reported as terminal")
	}
}
