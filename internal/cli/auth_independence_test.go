package cli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth"
	"github.com/volcengine-tls/ve-tls-cli/internal/auth/console"
	"github.com/volcengine-tls/ve-tls-cli/internal/auth/oauth"
	"github.com/volcengine-tls/ve-tls-cli/internal/auth/sso"
	"github.com/volcengine-tls/ve-tls-cli/internal/config"
)

// indepFixedTime returns the shared fixed clock for all independence tests so
// time windows are deterministic and never depend on wall-clock jitter. It is a
// fixed point in the past relative to typical test runs; tokens issued relative
// to it are clearly valid when the same clock is injected into the provider.
func indepFixedTime() time.Time { return time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC) }

// --- Local minimal fakes for the SSO refresh/STS-exchange flow ---

type indepFakeSSOOAuth struct {
	createTokenCalls     int32
	registerClientCalls  int32
	startDeviceAuthCalls int32
	lastCreateTokenReq   *sso.CreateTokenRequest
}

func (f *indepFakeSSOOAuth) RegisterClient(ctx context.Context, req *sso.RegisterClientRequest) (*sso.RegisterClientResponse, error) {
	atomic.AddInt32(&f.registerClientCalls, 1)
	return nil, errors.New("RegisterClient must not be called during refresh")
}
func (f *indepFakeSSOOAuth) StartDeviceAuthorization(ctx context.Context, req *sso.StartDeviceAuthorizationRequest) (*sso.StartDeviceAuthorizationResponse, error) {
	atomic.AddInt32(&f.startDeviceAuthCalls, 1)
	return nil, errors.New("StartDeviceAuthorization must not be called during refresh")
}
func (f *indepFakeSSOOAuth) CreateToken(ctx context.Context, req *sso.CreateTokenRequest) (*sso.CreateTokenResponse, error) {
	atomic.AddInt32(&f.createTokenCalls, 1)
	f.lastCreateTokenReq = req
	// Validate the refresh request carries the expected binding fields.
	if req.GrantType != "refresh_token" {
		return nil, errors.New("expected grant_type=refresh_token, got " + req.GrantType)
	}
	if req.ClientID != "indep-client-id" {
		return nil, errors.New("unexpected client id: " + req.ClientID)
	}
	if req.ClientSecret != "indep-client-secret" {
		return nil, errors.New("unexpected client secret")
	}
	if req.RefreshToken != "valid-refresh-token" {
		return nil, errors.New("unexpected refresh token: " + req.RefreshToken)
	}
	return &sso.CreateTokenResponse{
		AccessToken:  "rotated-access-token",
		TokenType:    "Bearer",
		RefreshToken: "rotated-refresh-token",
		ExpiresIn:    3600,
	}, nil
}

type indepFakeSSOPortal struct {
	getRoleCredCalls int32
	lastAccessToken  string
	lastAccountID    string
	lastRoleName     string
	clock            func() time.Time
}

func (f *indepFakeSSOPortal) GetRoleCredentials(ctx context.Context, accessToken, accountID, roleName string) (*sso.RoleCredentials, error) {
	atomic.AddInt32(&f.getRoleCredCalls, 1)
	f.lastAccessToken = accessToken
	f.lastAccountID = accountID
	f.lastRoleName = roleName
	// Validate the exchange uses the freshly-rotated access token and the
	// expected account/role binding from the profile.
	if accessToken != "rotated-access-token" {
		return nil, errors.New("unexpected access token: " + accessToken)
	}
	if accountID != "acct-1" {
		return nil, errors.New("unexpected account id: " + accountID)
	}
	if roleName != "role-1" {
		return nil, errors.New("unexpected role name: " + roleName)
	}
	return &sso.RoleCredentials{
		AccessKeyID:     "AKLTfakeportal",
		SecretAccessKey: "fake-portal-secret",
		SessionToken:    "fake-portal-token",
		Expiration:      f.clock().Add(time.Hour).Unix(),
	}, nil
}

// indepFakeSSOFactory injects a real sso.SSOProvider (with fake OAuth/Portal and
// a real FileCache) into buildDynamicClient so the full refresh + STS-exchange +
// TLS-sign path is exercised without real network. It also builds a real
// Console provider with the same fixed clock.
type indepFakeSSOFactory struct {
	oauth  *indepFakeSSOOAuth
	portal *indepFakeSSOPortal
	clock  func() time.Time
}

func (f *indepFakeSSOFactory) SSO(configPath, profileName string, cfg config.Config) (auth.Provider, authStatusReader, error) {
	p, ok := cfg.GetProfile(profileName)
	if !ok {
		return nil, nil, errSentinel("profile not found: " + profileName)
	}
	sessName := strings.TrimSpace(p.SSOSessionName)
	sess, ok := cfg.SSOSessions[sessName]
	if !ok {
		return nil, nil, errSentinel("sso session not found: " + sessName)
	}
	cacheDir := resolveSSOCacheDir(configPath)
	cache, err := sso.NewFileCache(cacheDir)
	if err != nil {
		return nil, nil, err
	}
	provider, err := sso.NewSSOProvider(&sso.SSOProviderConfig{
		ConfigPath:  configPath,
		ProfileName: profileName,
		StartURL:    sess.StartURL,
		SessionName: sess.Name,
		SSORegion:   sess.Region,
		AccountID:   p.AccountID,
		RoleName:    p.RoleName,
		Cache:       cache,
		OAuth:       f.oauth,
		Portal:      f.portal,
		Clock:       f.clock,
	})
	if err != nil {
		return nil, nil, err
	}
	reader := &ssoStatusReader{
		cache:       cache,
		startURL:    sess.StartURL,
		sessionName: sess.Name,
		accountID:   p.AccountID,
		roleName:    p.RoleName,
		region:      sess.Region,
		clock:       f.clock,
	}
	return provider, reader, nil
}

func (f *indepFakeSSOFactory) Console(configPath, profileName string, cfg config.Config) (auth.Provider, authStatusReader, error) {
	p, ok := cfg.GetProfile(profileName)
	if !ok {
		return nil, nil, errSentinel("profile not found: " + profileName)
	}
	loginSession := strings.TrimSpace(p.LoginSession)
	cacheDir := resolveLoginCacheDir(configPath)
	cache, err := console.NewFileCache(cacheDir)
	if err != nil {
		return nil, nil, err
	}
	oauthFactory := func(endpointURL string) (console.OAuthClient, error) {
		return console.NewConsoleOAuthClient(&console.ConsoleOAuthClientConfig{EndpointURL: endpointURL})
	}
	provider := console.NewProvider(loginSession, cache, oauthFactory, f.clock)
	reader := &consoleStatusReader{
		cache:        cache,
		loginSession: loginSession,
		clock:        f.clock,
	}
	return provider, reader, nil
}

// RamRoleARN/OIDC/ECSRole are not used by the SSO/Console independence tests;
// they return a sentinel error so any accidental dispatch is visible.
func (f *indepFakeSSOFactory) RamRoleARN(configPath, profileName string, cfg config.Config) (auth.Provider, error) {
	return nil, errSentinel("indepFakeSSOFactory: RamRoleARN not implemented")
}

func (f *indepFakeSSOFactory) OIDC(configPath, profileName string, cfg config.Config) (auth.Provider, error) {
	return nil, errSentinel("indepFakeSSOFactory: OIDC not implemented")
}

func (f *indepFakeSSOFactory) ECSRole(configPath, profileName string, cfg config.Config) (auth.Provider, error) {
	return nil, errSentinel("indepFakeSSOFactory: ECSRole not implemented")
}

// --- Fakes for driving real SSO/Console login entry points ---

// indepLoginSSOOAuth implements sso.OAuthAPI for the real DeviceFlow.Login path.
// It validates every request parameter before returning success so a broken
// device-flow contract fails the login rather than passing silently. It counts
// calls so the test can prove the login entry point actually ran.
type indepLoginSSOOAuth struct {
	registerCalls   int32
	deviceAuthCalls int32
	tokenCalls      int32
	startURL        string
}

func (f *indepLoginSSOOAuth) RegisterClient(ctx context.Context, req *sso.RegisterClientRequest) (*sso.RegisterClientResponse, error) {
	atomic.AddInt32(&f.registerCalls, 1)
	if req == nil {
		return nil, errors.New("RegisterClient: nil request")
	}
	if req.ClientName != "volclog" {
		return nil, errors.New("RegisterClient: unexpected ClientName: " + req.ClientName)
	}
	if req.ClientType != "public" {
		return nil, errors.New("RegisterClient: unexpected ClientType: " + req.ClientType)
	}
	if len(req.GrantTypes) != 2 || req.GrantTypes[0] != sso.GrantTypeDeviceCode || req.GrantTypes[1] != sso.GrantTypeRefreshToken {
		return nil, errors.New("RegisterClient: unexpected GrantTypes")
	}
	if len(req.Scopes) != 2 || req.Scopes[0] != sso.ScopeAccountAccess || req.Scopes[1] != sso.ScopeOfflineAccess {
		return nil, errors.New("RegisterClient: unexpected Scopes")
	}
	return &sso.RegisterClientResponse{ClientID: "login-client-id", ClientSecret: "login-client-secret"}, nil
}
func (f *indepLoginSSOOAuth) StartDeviceAuthorization(ctx context.Context, req *sso.StartDeviceAuthorizationRequest) (*sso.StartDeviceAuthorizationResponse, error) {
	atomic.AddInt32(&f.deviceAuthCalls, 1)
	if req == nil {
		return nil, errors.New("StartDeviceAuthorization: nil request")
	}
	if req.ClientID != "login-client-id" || req.ClientSecret != "login-client-secret" {
		return nil, errors.New("StartDeviceAuthorization: client id/secret mismatch")
	}
	if len(req.Scopes) != 2 || req.Scopes[0] != sso.ScopeAccountAccess || req.Scopes[1] != sso.ScopeOfflineAccess {
		return nil, errors.New("StartDeviceAuthorization: unexpected Scopes")
	}
	canonical, _ := sso.CanonicalStartURL(f.startURL)
	if req.PortalURL != canonical {
		return nil, errors.New("StartDeviceAuthorization: unexpected PortalURL: " + req.PortalURL)
	}
	return &sso.StartDeviceAuthorizationResponse{
		DeviceCode:              "login-device-code",
		UserCode:                "UC123",
		VerificationURI:         "https://example.com/verify",
		VerificationURIComplete: "https://example.com/verify?user_code=UC123",
		ExpiresIn:               600,
		Interval:                1,
	}, nil
}
func (f *indepLoginSSOOAuth) CreateToken(ctx context.Context, req *sso.CreateTokenRequest) (*sso.CreateTokenResponse, error) {
	atomic.AddInt32(&f.tokenCalls, 1)
	if req == nil {
		return nil, errors.New("CreateToken: nil request")
	}
	if req.GrantType != sso.GrantTypeDeviceCode {
		return nil, errors.New("CreateToken: unexpected GrantType: " + req.GrantType)
	}
	if req.ClientID != "login-client-id" || req.ClientSecret != "login-client-secret" {
		return nil, errors.New("CreateToken: client id/secret mismatch")
	}
	if req.DeviceCode != "login-device-code" {
		return nil, errors.New("CreateToken: unexpected DeviceCode: " + req.DeviceCode)
	}
	if req.RefreshToken != "" {
		return nil, errors.New("CreateToken: RefreshToken must be empty for device_code grant")
	}
	return &sso.CreateTokenResponse{
		AccessToken:  "login-access-token",
		TokenType:    "Bearer",
		RefreshToken: "login-refresh-token",
		ExpiresIn:    3600,
	}, nil
}

// indepLoginConsoleOAuthClient implements console.OAuthClient for the real
// LoginService.Login path. It validates BuildAuthorizeURL and ExchangeToken
// parameters (including the PKCE relationship) and returns a valid
// ConsoleTokenResponse (with STS embedded) on token exchange.
type indepLoginConsoleOAuthClient struct {
	buildCalls     int32
	exchangeCalls  int32
	endpointURL    string
	savedState     string
	savedChallenge string
}

func (f *indepLoginConsoleOAuthClient) BuildAuthorizeURL(params *console.AuthorizeParams) (string, error) {
	atomic.AddInt32(&f.buildCalls, 1)
	if params == nil {
		return "", errors.New("BuildAuthorizeURL: nil params")
	}
	if params.ClientID != console.ClientIDCrossDevice {
		return "", errors.New("BuildAuthorizeURL: unexpected ClientID: " + params.ClientID)
	}
	expectedRedirect := strings.TrimRight(f.endpointURL, "/") + console.AuthorizePath
	if params.RedirectURI != expectedRedirect {
		return "", errors.New("BuildAuthorizeURL: unexpected RedirectURI: " + params.RedirectURI)
	}
	if params.Scope != console.Scope {
		return "", errors.New("BuildAuthorizeURL: unexpected Scope: " + params.Scope)
	}
	if params.State == "" {
		return "", errors.New("BuildAuthorizeURL: empty State")
	}
	if params.CodeChallenge == "" {
		return "", errors.New("BuildAuthorizeURL: empty CodeChallenge")
	}
	if params.CodeChallengeMethod != console.CodeChallengeMethodS256 {
		return "", errors.New("BuildAuthorizeURL: unexpected CodeChallengeMethod: " + params.CodeChallengeMethod)
	}
	f.savedState = params.State
	f.savedChallenge = params.CodeChallenge
	return f.endpointURL + console.AuthorizePath + "?state=" + params.State, nil
}
func (f *indepLoginConsoleOAuthClient) ExchangeToken(ctx context.Context, req *console.ConsoleTokenRequest) (*console.ConsoleTokenResponse, error) {
	atomic.AddInt32(&f.exchangeCalls, 1)
	if req == nil {
		return nil, errors.New("ExchangeToken: nil request")
	}
	if req.GrantType != console.GrantTypeAuthorizationCode {
		return nil, errors.New("ExchangeToken: unexpected GrantType: " + req.GrantType)
	}
	if req.Code != "console-auth-code" {
		return nil, errors.New("ExchangeToken: unexpected Code: " + req.Code)
	}
	expectedRedirect := strings.TrimRight(f.endpointURL, "/") + console.AuthorizePath
	if req.RedirectURI != expectedRedirect {
		return nil, errors.New("ExchangeToken: unexpected RedirectURI: " + req.RedirectURI)
	}
	if req.ClientID != console.ClientIDCrossDevice {
		return nil, errors.New("ExchangeToken: unexpected ClientID: " + req.ClientID)
	}
	if req.Scope != console.Scope {
		return nil, errors.New("ExchangeToken: unexpected Scope: " + req.Scope)
	}
	if req.CodeVerifier == "" {
		return nil, errors.New("ExchangeToken: empty CodeVerifier")
	}
	if req.RefreshToken != "" {
		return nil, errors.New("ExchangeToken: RefreshToken must be empty for authorization_code grant")
	}
	// Prove the PKCE relationship: the S256 challenge of the verifier must equal
	// the challenge saved from BuildAuthorizeURL.
	if got := oauth.CodeChallengeS256(req.CodeVerifier); got != f.savedChallenge {
		return nil, errors.New("ExchangeToken: PKCE challenge mismatch")
	}
	stsJSON, _ := json.Marshal(console.STSCredentials{
		AccessKeyID:     "AKLTconsolelogin",
		SecretAccessKey: "console-login-secret",
		SessionToken:    "console-login-token",
	})
	idToken := "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0." +
		base64.RawURLEncoding.EncodeToString([]byte(`{"trn":"trn:iam::1:user:console-login-session"}`)) +
		".c2ln"
	return &console.ConsoleTokenResponse{
		AccessToken:  stsJSON,
		TokenType:    "sts",
		ExpiresIn:    3600,
		RefreshToken: "console-login-refresh",
		Scope:        console.Scope,
		IDToken:      idToken,
	}, nil
}
func (f *indepLoginConsoleOAuthClient) EndpointURL() string { return f.endpointURL }

// runIndepSSOLogin drives the real sso.DeviceFlow.Login entry point with fakes
// and a real FileCache rooted at volclogRoot. It validates the login result and
// re-reads the persisted token cache to prove the real login path wrote it. No
// real network or browser is used.
func runIndepSSOLogin(t *testing.T, volclogRoot, startURL, sessionName, region string) *indepLoginSSOOAuth {
	t.Helper()
	cacheDir := filepath.Join(volclogRoot, "sso", "cache")
	assertPathWithinRoot(t, volclogRoot, cacheDir)
	cache, err := sso.NewFileCache(cacheDir)
	if err != nil {
		t.Fatalf("NewFileCache: %v", err)
	}
	oauth := &indepLoginSSOOAuth{startURL: startURL}
	df := sso.NewDeviceFlow(&sso.DeviceFlowConfig{
		OAuth:       oauth,
		Cache:       cache,
		Clock:       time.Now,
		Sleeper:     func(ctx context.Context, d time.Duration) error { return nil },
		NoBrowser:   true,
		ClientName:  "volclog",
		StartURL:    startURL,
		SessionName: sessionName,
		Region:      region,
		Scopes:      []string{sso.ScopeAccountAccess, sso.ScopeOfflineAccess},
	})
	token, err := df.Login(context.Background())
	if err != nil {
		t.Fatalf("DeviceFlow.Login: %v", err)
	}
	// Validate the login result bindings and token fields.
	if token == nil {
		t.Fatalf("DeviceFlow.Login returned nil token cache")
	}
	if token.StartURL != startURL || token.SessionName != sessionName || token.Region != region {
		t.Fatalf("login result bindings mismatch: start=%q session=%q region=%q", token.StartURL, token.SessionName, token.Region)
	}
	if token.AccessToken != "login-access-token" || token.RefreshToken != "login-refresh-token" {
		t.Fatalf("login result token fields mismatch: access=%q refresh=%q", token.AccessToken, token.RefreshToken)
	}
	if token.ClientID != "login-client-id" || token.ClientSecret != "login-client-secret" {
		t.Fatalf("login result client fields mismatch: id=%q secret=%q", token.ClientID, token.ClientSecret)
	}
	// Each OAuth step must have been called exactly once.
	if got := atomic.LoadInt32(&oauth.registerCalls); got != 1 {
		t.Fatalf("RegisterClient calls = %d, want 1", got)
	}
	if got := atomic.LoadInt32(&oauth.deviceAuthCalls); got != 1 {
		t.Fatalf("StartDeviceAuthorization calls = %d, want 1", got)
	}
	if got := atomic.LoadInt32(&oauth.tokenCalls); got != 1 {
		t.Fatalf("CreateToken calls = %d, want 1", got)
	}
	// Re-read the persisted token cache from the real FileCache and validate it
	// matches the login result.
	persisted, rerr := cache.ReadToken(startURL, sessionName)
	if rerr != nil {
		t.Fatalf("ReadToken after login: %v", rerr)
	}
	if persisted.StartURL != token.StartURL || persisted.SessionName != token.SessionName ||
		persisted.Region != token.Region || persisted.AccessToken != token.AccessToken ||
		persisted.RefreshToken != token.RefreshToken || persisted.ClientID != token.ClientID ||
		persisted.ClientSecret != token.ClientSecret {
		t.Fatalf("persisted token cache does not match login result")
	}
	return oauth
}

// indepFailingAuthorizer is an Authorizer that fails loudly if called. It is
// injected as the LocalAuthorizer factory to prove the remote path is used and
// the local path is never exercised.
type indepFailingAuthorizer struct{}

func (indepFailingAuthorizer) Authorize(ctx context.Context) (string, string, error) {
	return "", "", errors.New("local authorizer must not be called when Remote=true")
}

// runIndepConsoleLogin drives the real console.LoginService.Login entry point
// (remote/cross-device authorization-code flow) with fakes and a real FileCache
// rooted at volclogRoot. The authorization code is fed via a strings.Reader so
// no real browser or network is used. It validates the login result, re-reads
// the persisted cache, and returns the OAuth client so the test can check counts.
func runIndepConsoleLogin(t *testing.T, volclogRoot, endpointURL, profileName, region string) *indepLoginConsoleOAuthClient {
	t.Helper()
	cacheDir := filepath.Join(volclogRoot, "login", "cache")
	assertPathWithinRoot(t, volclogRoot, cacheDir)
	cache, err := console.NewFileCache(cacheDir)
	if err != nil {
		t.Fatalf("NewFileCache: %v", err)
	}
	oauthClient := &indepLoginConsoleOAuthClient{endpointURL: endpointURL}
	var factoryURL string
	svc := console.NewLoginService(&console.LoginServiceConfig{
		OAuthClientFactory: func(url string) (console.OAuthClient, error) {
			factoryURL = url
			return oauthClient, nil
		},
		LocalAuthorizer: func(client console.OAuthClient, state, codeChallenge string) console.Authorizer {
			return indepFailingAuthorizer{}
		},
		RemoteAuthorizer: func(client console.OAuthClient, state, codeChallenge string) console.Authorizer {
			// The remote flow reads a base64-encoded "code=...&state=..." line.
			payload := "code=console-auth-code&state=" + state
			encoded := base64.StdEncoding.EncodeToString([]byte(payload))
			return console.NewRemoteAuthorizer(client, strings.NewReader(encoded+"\n"), io.Discard, state, codeChallenge)
		},
		Cache:   cache,
		Clock:   time.Now,
		Confirm: func(profileName, currentSession, newSession string) (bool, error) { return true, nil },
	})
	result, err := svc.Login(context.Background(), console.LoginOptions{
		Remote:      true,
		EndpointURL: endpointURL,
		Profile:     profileName,
		Region:      region,
	})
	if err != nil {
		t.Fatalf("LoginService.Login: %v", err)
	}
	// Validate the OAuthClientFactory received the expected endpoint URL.
	if factoryURL != endpointURL {
		t.Fatalf("OAuthClientFactory received URL %q, want %q", factoryURL, endpointURL)
	}
	// BuildAuthorizeURL and ExchangeToken must each be called exactly once.
	if got := atomic.LoadInt32(&oauthClient.buildCalls); got != 1 {
		t.Fatalf("BuildAuthorizeURL calls = %d, want 1", got)
	}
	if got := atomic.LoadInt32(&oauthClient.exchangeCalls); got != 1 {
		t.Fatalf("ExchangeToken calls = %d, want 1", got)
	}
	// Validate the login result fields.
	if result == nil {
		t.Fatalf("LoginService.Login returned nil result")
	}
	if result.Profile != profileName {
		t.Fatalf("login result profile = %q, want %q", result.Profile, profileName)
	}
	if result.Provider != "console-login" {
		t.Fatalf("login result provider = %q, want console-login", result.Provider)
	}
	if result.Region != region {
		t.Fatalf("login result region = %q, want %q", result.Region, region)
	}
	if result.MaskedAccessKey == "" {
		t.Fatalf("login result has empty MaskedAccessKey")
	}
	if result.ExpiresAt.IsZero() {
		t.Fatalf("login result has zero ExpiresAt")
	}
	// Re-read the persisted login cache and validate its fields.
	const loginSession = "trn:iam::1:user:console-login-session"
	raw, ok, rerr := cache.ReadRaw(loginSession)
	if rerr != nil {
		t.Fatalf("ReadRaw after login: %v", rerr)
	}
	if !ok {
		t.Fatalf("login cache not found for session %q", loginSession)
	}
	var tc console.LoginTokenCache
	if jerr := json.Unmarshal(raw, &tc); jerr != nil {
		t.Fatalf("unmarshal login cache: %v", jerr)
	}
	if tc.LoginSession != loginSession {
		t.Fatalf("login cache session = %q, want %q", tc.LoginSession, loginSession)
	}
	if tc.ClientID != console.ClientIDCrossDevice {
		t.Fatalf("login cache ClientID = %q, want cross-device", tc.ClientID)
	}
	if tc.Scope != console.Scope {
		t.Fatalf("login cache Scope = %q, want %q", tc.Scope, console.Scope)
	}
	if tc.EndpointURL != endpointURL {
		t.Fatalf("login cache EndpointURL = %q, want %q", tc.EndpointURL, endpointURL)
	}
	if tc.TokenType != "sts" {
		t.Fatalf("login cache TokenType = %q, want sts", tc.TokenType)
	}
	var sts console.STSCredentials
	if jerr := json.Unmarshal(tc.AccessToken, &sts); jerr != nil {
		t.Fatalf("unmarshal STS from login cache: %v", jerr)
	}
	if sts.AccessKeyID != "AKLTconsolelogin" || sts.SecretAccessKey != "console-login-secret" || sts.SessionToken != "console-login-token" {
		t.Fatalf("login cache STS = %+v, want expected credentials", sts)
	}
	return oauthClient
}

// capturingRoundTripper is an http.RoundTripper that records the Authorization
// and X-Security-Token headers of the outgoing request and returns a canned 200
// response. It never makes a real network call. callCount is race-safe.
type capturingRoundTripper struct {
	callCount  int32
	seenAuth   string
	seenToken  string
	seenMethod string
	seenPath   string
}

func (c *capturingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	atomic.AddInt32(&c.callCount, 1)
	c.seenAuth = req.Header.Get("Authorization")
	c.seenToken = req.Header.Get("X-Security-Token")
	c.seenMethod = req.Method
	c.seenPath = req.URL.Path
	return &http.Response{
		StatusCode: 200,
		Header:     http.Header{"X-Tls-Requestid": []string{"captured-rt"}},
		Body:       io.NopCloser(strings.NewReader(`{"Projects":[]}`)),
	}, nil
}

// assertCapturedRequest fails t if the capturing round tripper did not observe
// exactly one GET request to /DescribeProjects.
func assertCapturedRequest(t *testing.T, c *capturingRoundTripper) {
	t.Helper()
	if got := atomic.LoadInt32(&c.callCount); got != 1 {
		t.Fatalf("expected exactly 1 captured request, got %d", got)
	}
	if c.seenMethod != http.MethodGet {
		t.Fatalf("captured method = %q, want GET", c.seenMethod)
	}
	if c.seenPath != "/DescribeProjects" {
		t.Fatalf("captured path = %q, want /DescribeProjects", c.seenPath)
	}
}

// assertPathWithinRoot fails t if target is not inside root.
func assertPathWithinRoot(t *testing.T, root, target string) {
	t.Helper()
	rel, err := filepath.Rel(root, target)
	if err != nil {
		t.Fatalf("filepath.Rel(%q, %q): %v", root, target, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		t.Fatalf("path %q escapes root %q (rel=%q)", target, root, rel)
	}
}

// dirSnapshot is a stable, sorted snapshot of every entry under a directory
// tree: relative path, explicit entry type, and type-specific payload (regular
// file content or symlink target). It never follows symlinks. It is used to
// prove a poison directory is untouched by the auth flow (no new, deleted, or
// modified entries, including symlink target rewrites).
type dirSnapshot struct {
	entries []dirEntry
}

// entryType classifies a filesystem entry without following symlinks.
type entryType string

const (
	typeDir     entryType = "dir"
	typeRegular entryType = "regular"
	typeSymlink entryType = "symlink"
	typeOther   entryType = "other"
)

type dirEntry struct {
	relPath string
	typ     entryType
	content string // regular file content; empty for non-regular
	target  string // symlink target (os.Readlink); empty for non-symlink
}

// classifyEntry returns the entry type using Lstat (never following symlinks).
func classifyEntry(path string, d fs.DirEntry) (entryType, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return typeOther, err
	}
	m := info.Mode()
	switch {
	case m.IsDir():
		return typeDir, nil
	case m.IsRegular():
		return typeRegular, nil
	case m&os.ModeSymlink != 0:
		return typeSymlink, nil
	default:
		return typeOther, nil
	}
}

// snapshotDir recursively captures a stable snapshot of root. It does not
// follow symlinks: symlinks are recorded with their target (os.Readlink) and
// never traversed. All test data is fake, so no real secrets are captured.
func snapshotDir(t *testing.T, root string) dirSnapshot {
	t.Helper()
	var snap dirSnapshot
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		if rel == "." {
			return nil
		}
		typ, terr := classifyEntry(path, d)
		if terr != nil {
			return terr
		}
		e := dirEntry{relPath: filepath.ToSlash(rel), typ: typ}
		switch typ {
		case typeRegular:
			b, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			e.content = string(b)
		case typeSymlink:
			tgt, rerr := os.Readlink(path)
			if rerr != nil {
				return rerr
			}
			e.target = tgt
		}
		snap.entries = append(snap.entries, e)
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %q: %v", root, err)
	}
	sort.Slice(snap.entries, func(i, j int) bool {
		return snap.entries[i].relPath < snap.entries[j].relPath
	})
	return snap
}

// assertDirUnchanged fails t if the directory tree changed between before and
// after (entries added, removed, or modified, including symlink target
// rewrites).
func assertDirUnchanged(t *testing.T, before, after dirSnapshot) {
	t.Helper()
	if len(before.entries) != len(after.entries) {
		t.Fatalf("poison dir entry count changed: before=%d after=%d", len(before.entries), len(after.entries))
	}
	for i := range before.entries {
		b, a := before.entries[i], after.entries[i]
		if b.relPath != a.relPath || b.typ != a.typ || b.content != a.content || b.target != a.target {
			t.Fatalf("poison dir entry changed:\n  before: %+v\n  after:  %+v", b, a)
		}
	}
}

// TestAuthUsesOnlyVolclogStateRoot proves that the SSO and Console dynamic auth
// flows read and write only the VOLCLOG_CONFIG state root, never ~/.volcengine
// or any VOLCENGINE_* path. For SSO it drives the full refresh + STS-exchange +
// TLS-sign path using fake OAuth/Portal clients injected into a real
// sso.SSOProvider, then re-reads the cache to prove rotation was persisted and
// the TLS request used the new credentials. For Console it drives a full TLS
// signing request. A capturing RoundTripper (no real server) records the signed
// headers.
func TestAuthUsesOnlyVolclogStateRoot(t *testing.T) {
	clearAuthTestEnv(t)
	volclogRoot := t.TempDir()
	cfgPath := filepath.Join(volclogRoot, "config.json")
	t.Setenv("VOLCLOG_CONFIG", cfgPath)
	// The default config path must resolve to the VOLCLOG_CONFIG we set.
	gotDefaultPath, err := config.DefaultConfigPath()
	if err != nil {
		t.Fatalf("DefaultConfigPath: %v", err)
	}
	if gotDefaultPath != cfgPath {
		t.Fatalf("DefaultConfigPath = %q, want %q", gotDefaultPath, cfgPath)
	}
	// Isolate HOME to a known, created directory so we can scan exactly that path.
	homeRoot := filepath.Join(t.TempDir(), "empty-home")
	if err := os.MkdirAll(homeRoot, 0755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	t.Setenv("HOME", homeRoot)
	t.Setenv("VOLCLOG_SSO_CACHE_DIRECTORY", "")
	t.Setenv("VOLCLOG_LOGIN_CACHE_DIRECTORY", "")
	// Set static/env AK to a distinct canary; the dynamic path must NOT use it.
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "AKLTENVSTATIC")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "env-static-secret")

	// --- SSO: seed a near-expiry token with a valid refresh token but NO STS cache. ---
	ssoCacheDir := filepath.Join(volclogRoot, "sso", "cache")
	ssoCache, err := sso.NewFileCache(ssoCacheDir)
	if err != nil {
		t.Fatalf("NewFileCache: %v", err)
	}
	assertPathWithinRoot(t, volclogRoot, ssoCacheDir)
	if err := ssoCache.WriteToken(&sso.TokenCache{
		StartURL:     "https://login.example.com/start",
		SessionName:  "indep-session",
		AccessToken:  "old-access-token",
		ExpiresAt:    indepFixedTime().Add(30 * time.Second).UTC().Format(time.RFC3339), // near expiry -> refresh
		RefreshToken: "valid-refresh-token",
		ClientID:     "indep-client-id",
		ClientSecret: "indep-client-secret",
		Region:       "cn-beijing",
	}); err != nil {
		t.Fatalf("WriteToken: %v", err)
	}
	// Intentionally do NOT write an STS cache so the provider must exchange.

	ssoCfg := config.Config{
		Version: 1,
		Profiles: map[string]config.Profile{
			"sso": {
				Mode:           config.AuthModeSSO,
				Region:         "cn-beijing",
				Endpoint:       "https://tls-cn-beijing.volces.com",
				SSOSessionName: "indep-session",
				AccountID:      "acct-1",
				RoleName:       "role-1",
			},
		},
		SSOSessions: map[string]config.SSOSession{
			"indep-session": {
				Name:     "indep-session",
				StartURL: "https://login.example.com/start",
				Region:   "cn-beijing",
			},
		},
	}
	if err := config.Save(ssoCfg, cfgPath); err != nil {
		t.Fatalf("save sso config: %v", err)
	}
	assertPathWithinRoot(t, volclogRoot, cfgPath)

	// Drive a DescribeProjects request through the dynamic client using the fake
	// OAuth/Portal factory and a capturing RoundTripper (no real server).
	fakeOAuth := &indepFakeSSOOAuth{}
	fakePortal := &indepFakeSSOPortal{clock: func() time.Time { return indepFixedTime() }}
	rt := &capturingRoundTripper{}
	resolvedP := ssoCfg.Profiles["sso"]
	resolvedP.Endpoint = "https://tls-cn-beijing.volces.com"
	cl, err := buildDynamicClient(config.AuthModeSSO, cfgPath, "sso", ssoCfg, resolvedP, &indepFakeSSOFactory{oauth: fakeOAuth, portal: fakePortal, clock: func() time.Time { return indepFixedTime() }})
	if err != nil {
		t.Fatalf("buildDynamicClient: %v", err)
	}
	cl.HTTP.Transport = rt
	body, _ := json.Marshal(map[string]any{})
	resp, err := cl.Do(context.Background(), "GET", "/DescribeProjects", map[string]string{
		"PageSize": "1", "PageNumber": "1",
	}, nil, body)
	if err != nil {
		t.Fatalf("DescribeProjects: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	assertCapturedRequest(t, rt)
	// Both fake OAuth refresh and fake Portal STS exchange must have been called.
	if atomic.LoadInt32(&fakeOAuth.createTokenCalls) != 1 {
		t.Fatalf("expected fake OAuth CreateToken to be called once, got %d", fakeOAuth.createTokenCalls)
	}
	if atomic.LoadInt32(&fakeOAuth.registerClientCalls) != 0 {
		t.Fatalf("RegisterClient must not be called, got %d", fakeOAuth.registerClientCalls)
	}
	if atomic.LoadInt32(&fakeOAuth.startDeviceAuthCalls) != 0 {
		t.Fatalf("StartDeviceAuthorization must not be called, got %d", fakeOAuth.startDeviceAuthCalls)
	}
	if atomic.LoadInt32(&fakePortal.getRoleCredCalls) != 1 {
		t.Fatalf("expected fake Portal GetRoleCredentials to be called once, got %d", fakePortal.getRoleCredCalls)
	}
	// The signed request must carry the fake Portal's new AK, not the env AK.
	if !strings.Contains(rt.seenAuth, "AKLTfakeportal") {
		t.Fatalf("Authorization header does not use fake Portal AK %q, got %q", "AKLTfakeportal", rt.seenAuth)
	}
	if strings.Contains(rt.seenAuth, "AKLTENVSTATIC") {
		t.Fatalf("Authorization header fell back to env AK: %q", rt.seenAuth)
	}
	if rt.seenToken != "fake-portal-token" {
		t.Fatalf("X-Security-Token = %q, want fake Portal %q", rt.seenToken, "fake-portal-token")
	}

	// Re-read the cache from the same real FileCache and assert the token was
	// rotated and the new STS was persisted, both inside the VOLCLOG root.
	rotatedToken, rerr := ssoCache.ReadToken("https://login.example.com/start", "indep-session")
	if rerr != nil {
		t.Fatalf("ReadToken after refresh: %v", rerr)
	}
	if rotatedToken.AccessToken != "rotated-access-token" {
		t.Fatalf("rotated AccessToken = %q, want rotated-access-token", rotatedToken.AccessToken)
	}
	if rotatedToken.RefreshToken != "rotated-refresh-token" {
		t.Fatalf("rotated RefreshToken = %q, want rotated-refresh-token", rotatedToken.RefreshToken)
	}
	if rotatedToken.SessionName != "indep-session" || rotatedToken.Region != "cn-beijing" {
		t.Fatalf("rotated token binding changed: session=%q region=%q", rotatedToken.SessionName, rotatedToken.Region)
	}
	newSTS, serr := ssoCache.ReadSTS("indep-session", "acct-1", "role-1")
	if serr != nil {
		t.Fatalf("ReadSTS after exchange: %v", serr)
	}
	if newSTS.AccessKeyID != "AKLTfakeportal" || newSTS.SecretAccessKey != "fake-portal-secret" || newSTS.SessionToken != "fake-portal-token" {
		t.Fatalf("persisted STS = %+v, want fake Portal credentials", newSTS)
	}
	if newSTS.ProviderName != sso.ProviderName {
		t.Fatalf("persisted STS provider = %q, want %q", newSTS.ProviderName, sso.ProviderName)
	}
	if newSTS.ExpiresAt == "" {
		t.Fatalf("persisted STS has empty ExpiresAt")
	}

	// --- Console: write a valid cache to the VOLCLOG root and drive a full TLS request. ---
	consoleCacheDir := filepath.Join(volclogRoot, "login", "cache")
	consoleCache, err := console.NewFileCache(consoleCacheDir)
	if err != nil {
		t.Fatalf("NewFileCache: %v", err)
	}
	assertPathWithinRoot(t, volclogRoot, consoleCacheDir)
	stsJSON, err := json.Marshal(console.STSCredentials{
		AccessKeyID:     "AKLTconsoleindep",
		SecretAccessKey: "console-indep-secret",
		SessionToken:    "console-indep-token",
	})
	if err != nil {
		t.Fatalf("marshal STS: %v", err)
	}
	if err := consoleCache.WriteRaw("indep-console-session", mustMarshal(t, console.LoginTokenCache{
		LoginSession: "indep-console-session",
		AccessToken:  stsJSON,
		Scope:        console.Scope,
		ClientID:     console.ClientIDSameDevice,
		IssuedAt:     indepFixedTime().UTC().Format(time.RFC3339),
		ExpiresIn:    3600,
		TokenType:    "sts",
	})); err != nil {
		t.Fatalf("WriteRaw: %v", err)
	}

	consoleCfg := config.Config{
		Version: 1,
		Profiles: map[string]config.Profile{
			"console": {
				Mode:         config.AuthModeConsoleLogin,
				Region:       "cn-beijing",
				Endpoint:     "https://tls-cn-beijing.volces.com",
				LoginSession: "indep-console-session",
			},
		},
	}
	consoleRT := &capturingRoundTripper{}
	consoleResolved := consoleCfg.Profiles["console"]
	consoleResolved.Endpoint = "https://tls-cn-beijing.volces.com"
	consoleFactory := &indepFakeSSOFactory{clock: func() time.Time { return indepFixedTime() }}
	ccl, err := buildDynamicClient(config.AuthModeConsoleLogin, cfgPath, "console", consoleCfg, consoleResolved, consoleFactory)
	if err != nil {
		t.Fatalf("buildDynamicClient console: %v", err)
	}
	ccl.HTTP.Transport = consoleRT
	cresp, err := ccl.Do(context.Background(), "GET", "/DescribeProjects", map[string]string{
		"PageSize": "1", "PageNumber": "1",
	}, nil, body)
	if err != nil {
		t.Fatalf("Console DescribeProjects: %v", err)
	}
	if cresp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", cresp.StatusCode)
	}
	assertCapturedRequest(t, consoleRT)
	if !strings.Contains(consoleRT.seenAuth, "AKLTconsoleindep") {
		t.Fatalf("Console Authorization header does not use dynamic AK %q, got %q", "AKLTconsoleindep", consoleRT.seenAuth)
	}
	if strings.Contains(consoleRT.seenAuth, "AKLTENVSTATIC") {
		t.Fatalf("Console Authorization header fell back to env AK: %q", consoleRT.seenAuth)
	}
	if consoleRT.seenToken != "console-indep-token" {
		t.Fatalf("Console X-Security-Token = %q, want %q", consoleRT.seenToken, "console-indep-token")
	}

	// --- Verify no state leaked outside the VOLCLOG root. ---
	homeFiles := []string{}
	err = filepath.WalkDir(homeRoot, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if !d.IsDir() {
			homeFiles = append(homeFiles, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk home: %v", err)
	}
	if len(homeFiles) > 0 {
		t.Fatalf("auth wrote state outside VOLCLOG root: %v", homeFiles)
	}
}

// TestAuthIgnoresPoisonedVolcengineHomeAndFakeVEBinary proves that the dynamic
// auth flows do not adopt VOLCENGINE_* config/cache paths or ~/.volcengine and do
// not execute the `ve` binary. Poison data is intentionally invalid: if the auth
// flow adopted any of these paths it would fail. The test succeeds using only
// the VOLCLOG root cache, the fake `ve` marker is never created, and the poison
// canaries remain unmodified (proving they were not adopted/overwritten).
func TestAuthIgnoresPoisonedVolcengineHomeAndFakeVEBinary(t *testing.T) {
	clearAuthTestEnv(t)
	// Use wall-clock "now" with a far-future validity so the production
	// defaultAuthProviderFactory (which uses time.Now) takes the fast path and
	// never triggers real OAuth/Portal network calls.
	now := time.Now()
	future := now.Add(24 * time.Hour).UTC().Format(time.RFC3339)
	volclogRoot := t.TempDir()
	cfgPath := filepath.Join(volclogRoot, "config.json")
	t.Setenv("VOLCLOG_CONFIG", cfgPath)
	t.Setenv("VOLCLOG_SSO_CACHE_DIRECTORY", "")
	t.Setenv("VOLCLOG_LOGIN_CACHE_DIRECTORY", "")
	// Set static/env AK to a distinct canary; the dynamic path must NOT use it.
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "AKLTENVSTATIC")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "env-static-secret")

	// Set an independent HOME containing a ~/.volcengine directory with an
	// invalid config and a canary cache file. If the auth flow read ~/.volcengine
	// as a config root, it would parse the invalid config and fail.
	poisonHome := t.TempDir()
	t.Setenv("HOME", poisonHome)
	volcengineHomeDir := filepath.Join(poisonHome, ".volcengine")
	if err := os.MkdirAll(volcengineHomeDir, 0755); err != nil {
		t.Fatalf("mkdir ~/.volcengine: %v", err)
	}
	// Invalid JSON config in ~/.volcengine — if adopted as config, parsing fails.
	homeConfigCanary := []byte("{{{{HOME_CONFIG_POISON")
	if err := os.WriteFile(filepath.Join(volcengineHomeDir, "config.json"), homeConfigCanary, 0600); err != nil {
		t.Fatalf("write ~/.volcengine/config.json: %v", err)
	}
	// Canary cache file in ~/.volcengine — if adopted as cache root, its content
	// would be treated as a cache and fail.
	homeCacheCanary := []byte("HOME_CACHE_POISON_CONTENT")
	if err := os.WriteFile(filepath.Join(volcengineHomeDir, "canary-cache"), homeCacheCanary, 0600); err != nil {
		t.Fatalf("write ~/.volcengine canary: %v", err)
	}
	// Symlink canary pointing outside volcengineHomeDir. The snapshot must
	// record the link itself (type + target) without following it; the target
	// must not appear as a separate entry inside the poison tree.
	symlinkTarget := filepath.Join(poisonHome, "outside-target")
	if err := os.WriteFile(symlinkTarget, []byte("OUTSIDE_TARGET"), 0600); err != nil {
		t.Fatalf("write symlink target: %v", err)
	}
	if err := os.Symlink(symlinkTarget, filepath.Join(volcengineHomeDir, "canary-link")); err != nil {
		t.Fatalf("write ~/.volcengine symlink canary: %v", err)
	}

	// Poison VOLCENGINE_* env paths with content that is intentionally unusable.
	poisonDir := t.TempDir()
	// VOLCENGINE_CLI_CONFIG_FILE points at unparseable JSON.
	poisonConfig := filepath.Join(poisonDir, "volcengine-config.json")
	poisonConfigContent := []byte("{{{{NOT VALID CONFIG POISON 98765")
	if err := os.WriteFile(poisonConfig, poisonConfigContent, 0600); err != nil {
		t.Fatalf("write poison config: %v", err)
	}
	t.Setenv("VOLCENGINE_CLI_CONFIG_FILE", poisonConfig)
	// VOLCENGINE_CACHE_DIR points at a real directory containing a canary file.
	// If the auth flow adopted it as a cache root, it would create new files here;
	// we assert the canary is unchanged and no new files appear.
	poisonCacheDir := filepath.Join(poisonDir, "volcengine-cache")
	if err := os.MkdirAll(poisonCacheDir, 0755); err != nil {
		t.Fatalf("mkdir poison cache dir: %v", err)
	}
	poisonCacheCanary := filepath.Join(poisonCacheDir, "canary")
	poisonCacheCanaryContent := []byte("POISON_CACHE_CANARY")
	if err := os.WriteFile(poisonCacheCanary, poisonCacheCanaryContent, 0600); err != nil {
		t.Fatalf("write poison cache canary: %v", err)
	}
	t.Setenv("VOLCENGINE_CACHE_DIR", poisonCacheDir)
	// VOLCENGINE_LOGIN_CACHE_DIRECTORY points at a regular file, not a directory,
	// so any attempt to use it as a cache root would fail.
	poisonLoginCacheFile := filepath.Join(poisonDir, "not-a-directory")
	if err := os.WriteFile(poisonLoginCacheFile, []byte("POISON_LOGIN_CACHE_CONTENT"), 0600); err != nil {
		t.Fatalf("write poison login cache file: %v", err)
	}
	t.Setenv("VOLCENGINE_LOGIN_CACHE_DIRECTORY", poisonLoginCacheFile)

	// Place a fake `ve` binary in PATH that writes a marker if executed.
	fakeBinDir := t.TempDir()
	markerPath := filepath.Join(t.TempDir(), "ve-executed-marker")
	fakeVe := filepath.Join(fakeBinDir, "ve")
	// Use the shell builtin `printf` with a safely-quoted marker path so the
	// script works even when PATH only contains fakeBinDir (no `touch` available).
	fakeScript := "#!/bin/sh\nprintf '1' > '" + markerPath + "'\nexit 1\n"
	if err := os.WriteFile(fakeVe, []byte(fakeScript), 0755); err != nil {
		t.Fatalf("write fake ve: %v", err)
	}
	t.Setenv("PATH", fakeBinDir)
	// Precondition: the fake ve must be resolvable via PATH.
	if _, err := exec.LookPath("ve"); err != nil {
		t.Fatalf("fake ve not resolvable in PATH: %v", err)
	}

	// Set up config with SSO and Console profiles so the real login entry points
	// can resolve their bindings.
	cfg := config.Config{
		Version: 1,
		Profiles: map[string]config.Profile{
			"sso": {
				Mode:           config.AuthModeSSO,
				Region:         "cn-beijing",
				Endpoint:       "https://tls-cn-beijing.volces.com",
				SSOSessionName: "poison-session",
				AccountID:      "acct-1",
				RoleName:       "role-1",
			},
			"console": {
				Mode:         config.AuthModeConsoleLogin,
				Region:       "cn-beijing",
				Endpoint:     "https://tls-cn-beijing.volces.com",
				LoginSession: "",
			},
		},
		SSOSessions: map[string]config.SSOSession{
			"poison-session": {
				Name:     "poison-session",
				StartURL: "https://login.example.com/start",
				Region:   "cn-beijing",
			},
		},
	}
	if err := config.Save(cfg, cfgPath); err != nil {
		t.Fatalf("save config: %v", err)
	}

	// Snapshot the poison ~/.volcengine tree before any production auth flow runs.
	beforeHome := snapshotDir(t, volcengineHomeDir)

	// Drive the real SSO DeviceFlow.Login entry point with fakes. It writes the
	// token cache to the VOLCLOG root (no real network/browser).
	ssoLoginOAuth := runIndepSSOLogin(t, volclogRoot, "https://login.example.com/start", "poison-session", "cn-beijing")
	if atomic.LoadInt32(&ssoLoginOAuth.tokenCalls) != 1 {
		t.Fatalf("SSO login entry point did not run: token calls = %d, want 1", ssoLoginOAuth.tokenCalls)
	}

	// The login only obtains the OAuth token; write a valid STS cache so the
	// production Provider (factory=nil) takes the fast path without calling the
	// real Portal.
	ssoCache, err := sso.NewFileCache(filepath.Join(volclogRoot, "sso", "cache"))
	if err != nil {
		t.Fatalf("NewFileCache: %v", err)
	}
	if err := ssoCache.WriteSTS(&sso.STSCache{
		SessionName:     "poison-session",
		AccountID:       "acct-1",
		RoleName:        "role-1",
		AccessKeyID:     "AKLTpoison",
		SecretAccessKey: "poison-secret",
		SessionToken:    "poison-token",
		ProviderName:    sso.ProviderName,
		ExpiresAt:       future,
	}); err != nil {
		t.Fatalf("WriteSTS: %v", err)
	}

	// Drive the real Console LoginService.Login entry point (remote/cross-device
	// authorization-code flow) with fakes. It writes the login cache to the
	// VOLCLOG root (no real network/browser).
	consoleOAuth := runIndepConsoleLogin(t, volclogRoot, "https://login.example.com", "console", "cn-beijing")
	if got := atomic.LoadInt32(&consoleOAuth.exchangeCalls); got != 1 {
		t.Fatalf("Console login entry point did not run: exchange calls = %d, want 1", got)
	}

	// Self-check: the symlink canary must be captured as type=symlink with its
	// target recorded, and the snapshot must not contain the link target as a
	// separate entry (proving the helper never follows links).
	var foundLink bool
	for _, e := range beforeHome.entries {
		if e.relPath == "canary-link" {
			foundLink = true
			if e.typ != typeSymlink {
				t.Fatalf("canary-link type = %q, want symlink", e.typ)
			}
			if e.target != symlinkTarget {
				t.Fatalf("canary-link target = %q, want %q", e.target, symlinkTarget)
			}
		}
		if e.relPath == "outside-target" {
			t.Fatalf("snapshot followed symlink: target appeared as entry %q", e.relPath)
		}
	}
	if !foundLink {
		t.Fatalf("snapshot did not capture canary-link symlink")
	}

	// Drive a DescribeProjects request through the dynamic client with a
	// capturing RoundTripper and prove the request uses the dynamic AK. The
	// production defaultAuthProviderFactory (factory=nil) takes the fast path
	// because the token+STS are valid for 24h, so no real OAuth/Portal is called.
	rt := &capturingRoundTripper{}
	resolvedP := cfg.Profiles["sso"]
	resolvedP.Endpoint = "https://tls-cn-beijing.volces.com"
	cl, err := buildDynamicClient(config.AuthModeSSO, cfgPath, "sso", cfg, resolvedP, nil)
	if err != nil {
		t.Fatalf("buildDynamicClient: %v", err)
	}
	cl.HTTP.Transport = rt
	body, _ := json.Marshal(map[string]any{})
	resp, err := cl.Do(context.Background(), "GET", "/DescribeProjects", map[string]string{
		"PageSize": "1", "PageNumber": "1",
	}, nil, body)
	if err != nil {
		t.Fatalf("DescribeProjects: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	assertCapturedRequest(t, rt)
	if !strings.Contains(rt.seenAuth, "AKLTpoison") {
		t.Fatalf("Authorization header does not use dynamic AK %q, got %q", "AKLTpoison", rt.seenAuth)
	}
	if strings.Contains(rt.seenAuth, "AKLTENVSTATIC") {
		t.Fatalf("Authorization header fell back to env AK: %q", rt.seenAuth)
	}

	// --- Console Login: the real login above already wrote the login cache and
	// patched the config's login-session. Reload the patched config and drive a
	// full TLS request in the same poison env. This proves the Console path does
	// not adopt VOLCENGINE_LOGIN_CACHE_DIRECTORY (which points at a regular file). ---
	reloadedCfg, _, rerr := config.Load()
	if rerr != nil {
		t.Fatalf("reload config after console login: %v", rerr)
	}
	consoleRT := &capturingRoundTripper{}
	consoleResolved := reloadedCfg.Profiles["console"]
	consoleResolved.Endpoint = "https://tls-cn-beijing.volces.com"
	ccl, err := buildDynamicClient(config.AuthModeConsoleLogin, cfgPath, "console", reloadedCfg, consoleResolved, nil)
	if err != nil {
		t.Fatalf("buildDynamicClient console: %v", err)
	}
	ccl.HTTP.Transport = consoleRT
	cbody, _ := json.Marshal(map[string]any{})
	cresp, err := ccl.Do(context.Background(), "GET", "/DescribeProjects", map[string]string{
		"PageSize": "1", "PageNumber": "1",
	}, nil, cbody)
	if err != nil {
		t.Fatalf("Console DescribeProjects: %v", err)
	}
	if cresp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", cresp.StatusCode)
	}
	assertCapturedRequest(t, consoleRT)
	if !strings.Contains(consoleRT.seenAuth, "AKLTconsolelogin") {
		t.Fatalf("Console Authorization header does not use dynamic AK %q, got %q", "AKLTconsolelogin", consoleRT.seenAuth)
	}
	if strings.Contains(consoleRT.seenAuth, "AKLTENVSTATIC") {
		t.Fatalf("Console Authorization header fell back to env AK: %q", consoleRT.seenAuth)
	}
	if consoleRT.seenToken != "console-login-token" {
		t.Fatalf("Console X-Security-Token = %q, want %q", consoleRT.seenToken, "console-login-token")
	}

	// Assert `ve` was never executed. Only os.ErrNotExist means the marker was
	// never created; any other error is unexpected and must fail the test.
	if _, err := os.Stat(markerPath); err == nil {
		t.Fatalf("fake ve binary was executed; marker exists at %s", markerPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stat marker: unexpected error: %v", err)
	}

	// Re-snapshot the poison ~/.volcengine tree after both SSO and Console
	// production flows ran; it must be byte-for-byte identical (no new, deleted,
	// or modified entries including subdirectories).
	afterHome := snapshotDir(t, volcengineHomeDir)
	assertDirUnchanged(t, beforeHome, afterHome)

	// Assert the poison files were not overwritten by the auth flow. If any had
	// been adopted, the flow would have failed; success plus unchanged canaries
	// proves independence.
	gotConfig, err := os.ReadFile(poisonConfig)
	if err != nil {
		t.Fatalf("read poison config: %v", err)
	}
	if string(gotConfig) != string(poisonConfigContent) {
		t.Fatalf("poison config was modified by auth flow; got %q want %q", gotConfig, poisonConfigContent)
	}
	gotLoginCache, err := os.ReadFile(poisonLoginCacheFile)
	if err != nil {
		t.Fatalf("read poison login cache file: %v", err)
	}
	if string(gotLoginCache) != "POISON_LOGIN_CACHE_CONTENT" {
		t.Fatalf("poison login cache file was modified by auth flow; got %q", gotLoginCache)
	}
	gotHomeConfig, err := os.ReadFile(filepath.Join(volcengineHomeDir, "config.json"))
	if err != nil {
		t.Fatalf("read ~/.volcengine config: %v", err)
	}
	if string(gotHomeConfig) != string(homeConfigCanary) {
		t.Fatalf("~/.volcengine config was modified; got %q want %q", gotHomeConfig, homeConfigCanary)
	}
	gotHomeCache, err := os.ReadFile(filepath.Join(volcengineHomeDir, "canary-cache"))
	if err != nil {
		t.Fatalf("read ~/.volcengine canary: %v", err)
	}
	if string(gotHomeCache) != string(homeCacheCanary) {
		t.Fatalf("~/.volcengine canary was modified; got %q want %q", gotHomeCache, homeCacheCanary)
	}

	// Assert the VOLCENGINE_CACHE_DIR poison canary is unchanged and no new files
	// were added to that directory by the auth flow.
	gotCacheCanary, err := os.ReadFile(poisonCacheCanary)
	if err != nil {
		t.Fatalf("read poison cache canary: %v", err)
	}
	if string(gotCacheCanary) != string(poisonCacheCanaryContent) {
		t.Fatalf("poison cache canary was modified; got %q want %q", gotCacheCanary, poisonCacheCanaryContent)
	}
	entries, err := os.ReadDir(poisonCacheDir)
	if err != nil {
		t.Fatalf("read poison cache dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "canary" {
		t.Fatalf("poison cache dir has unexpected entries; got %d entries, want only canary", len(entries))
	}
}

// TestAuthCoreDoesNotImportCLIOrTLSAPI uses the Go parser/AST to recursively
// verify that internal/auth production source code does not import
// internal/cli, internal/tlsapi, or github.com/volcengine/volcengine-cli. The
// auth core must remain independent of the CLI dispatcher and TLS client so it
// can be reused or extracted without dragging those dependencies. The auth root
// is located via runtime.Caller(0) so the test does not depend on the process
// working directory.
func TestAuthCoreDoesNotImportCLIOrTLSAPI(t *testing.T) {
	forbidden := []string{
		"github.com/volcengine-tls/ve-tls-cli/internal/cli",
		"github.com/volcengine-tls/ve-tls-cli/internal/tlsapi",
		"github.com/volcengine/volcengine-cli",
	}
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller failed")
	}
	// thisFile is <repo>/internal/cli/auth_independence_test.go;
	// auth root is <repo>/internal/auth.
	authRoot := filepath.Join(filepath.Dir(thisFile), "..", "auth")
	fset := token.NewFileSet()
	var violations []string
	err := filepath.WalkDir(authRoot, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		node, perr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if perr != nil {
			t.Fatalf("parse %s: %v", path, perr)
		}
		for _, imp := range node.Imports {
			impPath := strings.Trim(imp.Path.Value, `"`)
			for _, f := range forbidden {
				if impPath == f || strings.HasPrefix(impPath, f+"/") {
					violations = append(violations, path+" imports "+impPath)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk auth root: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("auth core imports forbidden packages:\n%s", strings.Join(violations, "\n"))
	}
}

// TestAuthCoreDoesNotReadProcessEnvironmentOrHome uses the Go AST to verify
// that internal/auth production source code does not call os environment/home
// functions (os.Getenv, os.LookupEnv, os.UserHomeDir, os.Environ, os.ExpandEnv)
// and does not hardcode .volclog/.volcengine paths or VOLCLOG_* env names. The
// auth core must receive cache directories explicitly from the CLI layer. The
// auth root is located via runtime.Caller(0) so the test is cwd-independent.
func TestAuthCoreDoesNotReadProcessEnvironmentOrHome(t *testing.T) {
	forbiddenCalls := map[string]bool{
		"Getenv":      true,
		"LookupEnv":   true,
		"UserHomeDir": true,
		"Environ":     true,
		"ExpandEnv":   true,
	}
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller failed")
	}
	authRoot := filepath.Join(filepath.Dir(thisFile), "..", "auth")
	fset := token.NewFileSet()
	var violations []string
	err := filepath.WalkDir(authRoot, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		node, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", path, perr)
		}
		// Collect string constants so os.Getenv(constName) can be resolved.
		consts := collectStringConstants(node)
		ast.Inspect(node, func(n ast.Node) bool {
			// Check for forbidden os.* calls. The ECS client is permitted to read
			// VOLCENGINE_ECS_METADATA_DISABLED (the only env var a workload
			// provider may consult); the call argument must resolve to that exact
			// string, either as a literal or a constant.
			if call, ok := n.(*ast.CallExpr); ok {
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
					if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "os" {
						if forbiddenCalls[sel.Sel.Name] {
							if strings.HasSuffix(path, "ecsrole/client.go") && sel.Sel.Name == "Getenv" && resolveStringArg(call, consts) == "VOLCENGINE_ECS_METADATA_DISABLED" {
								return true
							}
							violations = append(violations, path+" calls os."+sel.Sel.Name)
						}
					}
				}
			}
			// Check for hardcoded .volclog/.volcengine paths or VOLCLOG_* literals.
			// Only the exact production host literals sts.volcengineapi.com and
			// signin.volcengine.com are allowed; path-like strings (e.g.
			// ~/.volcengine/...) must still be flagged.
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				val := strings.Trim(lit.Value, `"`+"`")
				if strings.Contains(val, ".volclog") || strings.Contains(val, ".volcengine") {
					if !isAllowedVolcengineLiteral(val) {
						violations = append(violations, path+" hardcodes path: "+val)
					}
				}
				if strings.HasPrefix(val, "VOLCLOG_") {
					violations = append(violations, path+" hardcodes env name: "+val)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk auth root: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("auth core reads env/home or hardcodes paths:\n%s", strings.Join(violations, "\n"))
	}
}

// collectStringConstants returns a map of identifier name to string value for
// all untyped string constants declared at file scope.
func collectStringConstants(node *ast.File) map[string]string {
	out := map[string]string{}
	for _, decl := range node.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if i < len(vs.Values) {
					if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						out[name.Name] = strings.Trim(lit.Value, `"`+"`")
					}
				}
			}
		}
	}
	return out
}

// resolveStringArg returns the string value of call's first argument, resolving
// identifiers through the consts map. Returns "" if it cannot be resolved.
func resolveStringArg(call *ast.CallExpr, consts map[string]string) string {
	if len(call.Args) == 0 {
		return ""
	}
	switch arg := call.Args[0].(type) {
	case *ast.BasicLit:
		if arg.Kind == token.STRING {
			return strings.Trim(arg.Value, `"`+"`")
		}
	case *ast.Ident:
		if v, ok := consts[arg.Name]; ok {
			return v
		}
	}
	return ""
}

// isAllowedVolcengineLiteral reports whether val is one of the two exact
// production literals: the fixed STS host "sts.volcengineapi.com" and the
// console DefaultEndpoint "https://signin.volcengine.com". Any variant with a
// path, query, fragment, userinfo, or different scheme/host is rejected so
// strings like ~/.volcengine/... cannot escape by sharing a host.
func isAllowedVolcengineLiteral(val string) bool {
	return val == "sts.volcengineapi.com" || val == "https://signin.volcengine.com"
}

// mustMarshal marshals v to JSON, failing the test on error rather than
// panicking, so test setup errors are reported through the test runner.
func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	return b
}

// TestAllowedVolcengineLiteralIsExact proves that isAllowedVolcengineLiteral
// only accepts the exact production host literals and rejects any
// path/query/fragment/userinfo variants, even when the host matches.
func TestAllowedVolcengineLiteralIsExact(t *testing.T) {
	cases := []struct {
		val  string
		want bool
	}{
		// The two exact production literals.
		{"sts.volcengineapi.com", true},
		{"https://signin.volcengine.com", true},
		// Everything else, even with the correct host, must be rejected.
		{"https://sts.volcengineapi.com", false},
		{"https://sts.volcengineapi.com/.volcengine", false},
		{"https://sts.volcengineapi.com/?x=.volcengine", false},
		{"https://user@sts.volcengineapi.com", false},
		{"https://sts.volcengineapi.com/path", false},
		{"signin.volcengine.com", false},
		{"https://signin.volcengine.com/", false},
		{"https://signin.volcengine.com/extra", false},
		{"~/.volcengine/cache", false},
		{".volcengine", false},
		{"sts.volcengineapi.com.evil.com", false},
	}
	for _, tc := range cases {
		t.Run(tc.val, func(t *testing.T) {
			if got := isAllowedVolcengineLiteral(tc.val); got != tc.want {
				t.Fatalf("isAllowedVolcengineLiteral(%q)=%v, want %v", tc.val, got, tc.want)
			}
		})
	}
}
