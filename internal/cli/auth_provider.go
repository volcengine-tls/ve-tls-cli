package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth"
	"github.com/volcengine-tls/ve-tls-cli/internal/auth/console"
	"github.com/volcengine-tls/ve-tls-cli/internal/auth/ecsrole"
	"github.com/volcengine-tls/ve-tls-cli/internal/auth/oidc"
	"github.com/volcengine-tls/ve-tls-cli/internal/auth/ramrole"
	"github.com/volcengine-tls/ve-tls-cli/internal/auth/sso"
	"github.com/volcengine-tls/ve-tls-cli/internal/auth/sts"
	"github.com/volcengine-tls/ve-tls-cli/internal/config"
	"github.com/volcengine-tls/ve-tls-cli/internal/tlsapi"
)

// authStatus is the read-only snapshot of a dynamic profile's authentication
// state. It is produced without refreshing tokens, making network calls, or
// writing to disk, so it is safe for offline doctor and configure show/list.
// No secret material is ever stored here.
type authStatus struct {
	// Provider is the human-readable provider name ("sso" or "console-login").
	Provider string
	// Present reports whether a valid cache entry exists for the profile.
	Present bool
	// ExpiresAt is the cache entry's expiration time. It is the zero value when
	// no cache entry exists or the expiration cannot be determined.
	ExpiresAt time.Time
	// RefreshRequired reports whether the cache entry is expired or within the
	// refresh window, meaning the next Retrieve would attempt a refresh.
	RefreshRequired bool
}

// authStatusReader reads the offline authentication status for a profile
// without refreshing, networking, or writing. Both the SSO and Console factory
// implementations provide a reader alongside the Provider.
type authStatusReader interface {
	Status(ctx context.Context, profileName string) (authStatus, error)
}

// authProviderFactory is the seam for constructing dynamic SSO, Console Login,
// and standalone workload (RAM Role ARN / OIDC / ECS Role) providers. SSO and
// Console also return an authStatusReader for offline diagnostics; workload
// providers have no cache status reader. Tests inject a fake implementation;
// production uses defaultAuthProviderFactory.
type authProviderFactory interface {
	SSO(configPath, profileName string, cfg config.Config) (auth.Provider, authStatusReader, error)
	Console(configPath, profileName string, cfg config.Config) (auth.Provider, authStatusReader, error)
	RamRoleARN(configPath, profileName string, cfg config.Config) (auth.Provider, error)
	OIDC(configPath, profileName string, cfg config.Config) (auth.Provider, error)
	ECSRole(configPath, profileName string, cfg config.Config) (auth.Provider, error)
}

// defaultAuthProviderFactory constructs real providers. SSO and Console Login
// use disk-backed cache directories resolved by
// resolveSSOCacheDir/resolveLoginCacheDir (VOLCLOG_SSO_CACHE_DIRECTORY /
// VOLCLOG_LOGIN_CACHE_DIRECTORY take exact precedence; otherwise derived from
// the config path's parent). They never read ~/.volcengine and never use
// VOLCENGINE_* env vars, matching the login adapters so login writes and
// TLS/Doctor/configure reads target the same root. The standalone workload
// providers (RAM Role ARN, OIDC, ECS Role) use production STS/IMDS clients and
// have no cache directory; the ECS client is explicitly permitted to read
// VOLCENGINE_ECS_METADATA_DISABLED to fail closed before network access.
type defaultAuthProviderFactory struct{}

func (defaultAuthProviderFactory) SSO(configPath, profileName string, cfg config.Config) (auth.Provider, authStatusReader, error) {
	p, ok := cfg.GetProfile(profileName)
	if !ok {
		return nil, nil, errors.New("profile not found: " + profileName)
	}
	sessName := strings.TrimSpace(p.SSOSessionName)
	sess, ok := cfg.SSOSessions[sessName]
	if !ok {
		return nil, nil, errors.New("sso session not found: " + sessName)
	}
	cacheDir := resolveSSOCacheDir(configPath)
	cache, err := sso.NewFileCache(cacheDir)
	if err != nil {
		return nil, nil, err
	}
	oauthClient, err := sso.NewOAuthClient(&sso.OAuthClientConfig{Region: sess.Region})
	if err != nil {
		return nil, nil, err
	}
	portalClient, err := sso.NewPortalClient(&sso.PortalClientConfig{Region: sess.Region})
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
		OAuth:       oauthClient,
		Portal:      portalClient,
		Clock:       time.Now,
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
		clock:       time.Now,
	}
	return provider, reader, nil
}

func (defaultAuthProviderFactory) Console(configPath, profileName string, cfg config.Config) (auth.Provider, authStatusReader, error) {
	p, ok := cfg.GetProfile(profileName)
	if !ok {
		return nil, nil, errors.New("profile not found: " + profileName)
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
	provider := console.NewProvider(loginSession, cache, oauthFactory, time.Now)
	reader := &consoleStatusReader{
		cache:        cache,
		loginSession: loginSession,
		clock:        time.Now,
	}
	return provider, reader, nil
}

// resolveWorkloadSourceCredential builds an sts.SourceCredential from the
// profile's inline AK/SK or its explicit CredRef. It never reads environment
// AK/SK, never consults ~/.volcengine, and never falls back to other profiles.
// Missing credentials produce a clear error rather than a silent empty source.
func resolveWorkloadSourceCredential(p config.Profile, cfg config.Config) (sts.SourceCredential, error) {
	ak := strings.TrimSpace(p.AccessKeyID)
	sk := strings.TrimSpace(p.SecretAccessKey)
	token := strings.TrimSpace(p.SecurityToken)
	if credRef := strings.TrimSpace(p.CredRef); credRef != "" {
		cred, ok := cfg.GetCred(credRef)
		if !ok {
			return sts.SourceCredential{}, errors.New("credential not found: " + credRef)
		}
		if ak == "" {
			ak = strings.TrimSpace(cred.AccessKeyID)
		}
		if sk == "" {
			sk = strings.TrimSpace(cred.SecretAccessKey)
		}
	}
	if ak == "" || sk == "" {
		return sts.SourceCredential{}, errors.New("RAM role source credentials are missing: set inline access_key_id/secret_access_key or cred_ref on the profile")
	}
	return sts.SourceCredential{AccessKeyID: ak, SecretAccessKey: sk, SessionToken: token}, nil
}

func (defaultAuthProviderFactory) RamRoleARN(configPath, profileName string, cfg config.Config) (auth.Provider, error) {
	p, ok := cfg.GetProfile(profileName)
	if !ok {
		return nil, errors.New("profile not found: " + profileName)
	}
	source, err := resolveWorkloadSourceCredential(p, cfg)
	if err != nil {
		return nil, err
	}
	return ramrole.New(ramrole.Config{
		Source:     source,
		AccountID:  strings.TrimSpace(p.AccountID),
		RoleName:   strings.TrimSpace(p.RoleName),
		Region:     strings.TrimSpace(p.Region),
		DisableSSL: p.DisableSSL,
		Client:     sts.NewClient(),
		Clock:      time.Now,
	})
}

func (defaultAuthProviderFactory) OIDC(configPath, profileName string, cfg config.Config) (auth.Provider, error) {
	p, ok := cfg.GetProfile(profileName)
	if !ok {
		return nil, errors.New("profile not found: " + profileName)
	}
	tokenFile := strings.TrimSpace(p.OIDCTokenFile)
	if tokenFile == "" {
		return nil, errors.New("OIDC token file is not configured: set oidc-token-file on the profile")
	}
	return oidc.New(oidc.Config{
		TokenFile:  tokenFile,
		RoleTRN:    strings.TrimSpace(p.RoleTRN),
		DisableSSL: p.DisableSSL,
		Client:     sts.NewClient(),
		Clock:      time.Now,
	})
}

func (defaultAuthProviderFactory) ECSRole(configPath, profileName string, cfg config.Config) (auth.Provider, error) {
	p, ok := cfg.GetProfile(profileName)
	if !ok {
		return nil, errors.New("profile not found: " + profileName)
	}
	roleName := strings.TrimSpace(p.RoleName)
	if roleName == "" {
		return nil, errors.New("ECS role name is not configured: set role-name on the profile")
	}
	return ecsrole.New(ecsrole.Config{
		RoleName: roleName,
		Client:   ecsrole.NewClient(),
		Clock:    time.Now,
	})
}

// resolveSSOCacheDir returns the SSO cache directory, sharing the same
// resolution logic between the login adapter (which writes the cache) and the
// dynamic provider factory / status reader (which read it). If
// VOLCLOG_SSO_CACHE_DIRECTORY is set it is used verbatim; otherwise the
// directory is derived from the supplied config path's parent (sso/cache).
// This prevents a split-brain where login writes to an override directory but
// TLS/Doctor/configure read from the config-sibling root.
func resolveSSOCacheDir(configPath string) string {
	if dir := strings.TrimSpace(os.Getenv("VOLCLOG_SSO_CACHE_DIRECTORY")); dir != "" {
		return dir
	}
	return filepath.Join(filepath.Dir(configPath), "sso", "cache")
}

// resolveLoginCacheDir returns the Console Login cache directory, sharing the
// same resolution logic between the login adapter and the dynamic provider
// factory / status reader. If VOLCLOG_LOGIN_CACHE_DIRECTORY is set it is used
// verbatim; otherwise the directory is derived from the supplied config path's
// parent (login/cache).
func resolveLoginCacheDir(configPath string) string {
	if dir := strings.TrimSpace(os.Getenv("VOLCLOG_LOGIN_CACHE_DIRECTORY")); dir != "" {
		return dir
	}
	return filepath.Join(filepath.Dir(configPath), "login", "cache")
}

// ssoStatusReader reads SSO cache metadata without refreshing or networking.
// It inspects both the token cache and the STS cache, combining their status:
// the identity is present only when both caches are valid; refresh is required
// when either cache needs it; ExpiresAt is the earlier of the two non-zero
// expirations.
type ssoStatusReader struct {
	cache       sso.Cache
	startURL    string
	sessionName string
	accountID   string
	roleName    string
	region      string
	clock       func() time.Time
}

func (r *ssoStatusReader) Status(ctx context.Context, _ string) (authStatus, error) {
	if r == nil || r.cache == nil {
		return authStatus{Provider: sso.ProviderName, Present: false, RefreshRequired: true}, nil
	}
	now := r.clock()

	// Inspect the token cache. A missing/corrupt/invalid token cache fails
	// closed: the identity is not present.
	tokenCache, terr := r.cache.ReadToken(r.startURL, r.sessionName)
	tokenPresent := false
	tokenRefresh := true
	var tokenExpires time.Time
	if terr == nil && tokenCache != nil {
		if ts, ierr := sso.InspectTokenCache(tokenCache, r.startURL, r.sessionName, r.region, now); ierr == nil {
			tokenPresent = ts.Present
			tokenRefresh = ts.RefreshRequired
			tokenExpires = ts.ExpiresAt
		}
	}

	// Inspect the STS cache.
	sts, serr := r.cache.ReadSTS(r.sessionName, r.accountID, r.roleName)
	stsPresent := false
	stsRefresh := true
	var stsExpires time.Time
	if serr == nil {
		if ss, ierr := sso.InspectSTSCache(sts, r.sessionName, r.accountID, r.roleName, now); ierr == nil {
			stsPresent = ss.Present
			stsRefresh = ss.RefreshRequired
			stsExpires = ss.ExpiresAt
		}
	}

	// Combine: present only when both caches are valid; refresh required when
	// either needs it. ExpiresAt is only meaningful when both caches are valid;
	// if either is invalid/missing, ExpiresAt must be zero (not the valid one's
	// value, which would be misleading).
	present := tokenPresent && stsPresent
	refreshRequired := tokenRefresh || stsRefresh
	expiresAt := time.Time{}
	if present {
		if !tokenExpires.IsZero() && (stsExpires.IsZero() || tokenExpires.Before(stsExpires)) {
			expiresAt = tokenExpires
		} else if !stsExpires.IsZero() {
			expiresAt = stsExpires
		}
	}
	return authStatus{
		Provider:        sso.ProviderName,
		Present:         present,
		ExpiresAt:       expiresAt,
		RefreshRequired: refreshRequired,
	}, nil
}

// consoleStatusReader reads Console Login cache metadata without refreshing
// or networking.
type consoleStatusReader struct {
	cache        console.ConsoleCache
	loginSession string
	clock        func() time.Time
}

func (r *consoleStatusReader) Status(ctx context.Context, _ string) (authStatus, error) {
	if r == nil || r.cache == nil || strings.TrimSpace(r.loginSession) == "" {
		return authStatus{Provider: console.ProviderName, Present: false, RefreshRequired: true}, nil
	}
	data, existed, err := r.cache.ReadRaw(r.loginSession)
	if err != nil || !existed || len(data) == 0 {
		return authStatus{Provider: console.ProviderName, Present: false, RefreshRequired: true}, nil
	}
	var cache console.LoginTokenCache
	if jerr := json.Unmarshal(data, &cache); jerr != nil {
		// Corrupt JSON: not present, refresh required.
		return authStatus{Provider: console.ProviderName, Present: false, RefreshRequired: true}, nil
	}
	// Use the shared validation helper so the status reader and the Provider
	// agree on what counts as a valid cache. Invalid caches (bad session,
	// client ID, scope, endpoint, token type, expiration, or STS) report
	// Present=false rather than Present=true based solely on a future expiry.
	// Any inspection error is treated as fail-closed: Present=false, zero
	// ExpiresAt, RefreshRequired=true.
	status, ierr := console.InspectLoginCache(&cache, r.loginSession, r.clock())
	if ierr != nil {
		return authStatus{Provider: console.ProviderName, Present: false, RefreshRequired: true}, nil
	}
	return authStatus{
		Provider:        console.ProviderName,
		Present:         status.Present,
		ExpiresAt:       status.ExpiresAt,
		RefreshRequired: status.RefreshRequired,
	}, nil
}

// resolveDynamicRuntimeSettings resolves region/endpoint/timeout for dynamic
// modes with the fixed precedence:
//
//	VOLCENGINE_REGION / VOLCENGINE_ENDPOINT
//	> selected profile
//	> project defaults
//	> timeout default 60s
//
// Environment AK/SK are intentionally ignored for dynamic modes.
func resolveDynamicRuntimeSettings(p config.Profile, defaults config.ProfileDefaults) config.Profile {
	if envRegion := strings.TrimSpace(os.Getenv("VOLCENGINE_REGION")); envRegion != "" {
		p.Region = envRegion
	} else if strings.TrimSpace(p.Region) == "" && strings.TrimSpace(defaults.Region) != "" {
		p.Region = strings.TrimSpace(defaults.Region)
	}
	if envEndpoint := strings.TrimSpace(os.Getenv("VOLCENGINE_ENDPOINT")); envEndpoint != "" {
		p.Endpoint = envEndpoint
	} else if strings.TrimSpace(p.Endpoint) == "" && strings.TrimSpace(defaults.Endpoint) != "" {
		p.Endpoint = strings.TrimSpace(defaults.Endpoint)
	}
	if p.TimeoutSeconds <= 0 {
		if defaults.TimeoutSeconds > 0 {
			p.TimeoutSeconds = defaults.TimeoutSeconds
		} else {
			p.TimeoutSeconds = 60
		}
	}
	p.Region = strings.TrimSpace(p.Region)
	p.Endpoint = strings.TrimSpace(p.Endpoint)
	return p
}

// isNilFactory reports whether f is nil, including the typed-nil case where a
// non-nil interface wraps a nil pointer (e.g. var f *SomeFactory = nil). Such
// values must be rejected so buildDynamicClient never dispatches a method call
// to a nil receiver and panics. It mirrors tlsapi.isNilProvider but operates on
// the authProviderFactory seam. Only nil-able kinds are inspected so IsNil is
// never called on a value that cannot be nil.
func isNilFactory(f authProviderFactory) bool {
	if f == nil {
		return true
	}
	v := reflect.ValueOf(f)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return v.IsNil()
	}
	return false
}

// isNilProvider reports whether p is nil, including the typed-nil case where a
// non-nil interface wraps a nil pointer (e.g. var p *sso.SSOProvider = nil).
// It must be checked before wrapping p in modeAwareProvider, because the
// wrapper is always non-nil and would otherwise bypass tlsapi.NewWithProvider's
// own typed-nil guard, leading to a panic in Sign -> Retrieve.
func isNilProvider(p auth.Provider) bool {
	if p == nil {
		return true
	}
	v := reflect.ValueOf(p)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return v.IsNil()
	}
	return false
}

// buildDynamicClient constructs a tlsapi.Client backed by the dynamic provider
// for the supplied mode. It fails closed on any provider construction error and
// never falls back to environment AK/SK or static profile credentials.
//
// A plain nil factory interface falls back to defaultAuthProviderFactory (the
// production seam). A typed-nil factory (e.g. a nil pointer wrapped in the
// interface) is rejected with a clear error instead of being called, which
// would panic on the nil receiver.
func buildDynamicClient(mode string, cfgPath string, profileName string, cfg config.Config, p config.Profile, factory authProviderFactory) (*tlsapi.Client, error) {
	switch {
	case factory == nil:
		factory = defaultAuthProviderFactory{}
	case isNilFactory(factory):
		return nil, errors.New("nil auth provider factory")
	}
	var provider auth.Provider
	var err error
	providerCfg := configWithResolvedRuntimeProfile(cfg, profileName, p)
	switch mode {
	case config.AuthModeSSO:
		provider, _, err = factory.SSO(cfgPath, profileName, providerCfg)
	case config.AuthModeConsoleLogin:
		provider, _, err = factory.Console(cfgPath, profileName, providerCfg)
	case config.AuthModeRamRoleARN:
		provider, err = factory.RamRoleARN(cfgPath, profileName, providerCfg)
	case config.AuthModeOIDC:
		provider, err = factory.OIDC(cfgPath, profileName, providerCfg)
	case config.AuthModeECSRole:
		provider, err = factory.ECSRole(cfgPath, profileName, providerCfg)
	default:
		return nil, errors.New("unsupported auth mode: " + mode)
	}
	if err != nil {
		return nil, err
	}
	// Reject nil/typed-nil providers before wrapping. A typed-nil provider
	// wrapped in modeAwareProvider would appear non-nil to tlsapi.NewWithProvider
	// and bypass its guard, causing a panic in Sign -> Retrieve.
	if isNilProvider(provider) {
		return nil, errors.New("nil auth provider")
	}
	timeout := time.Duration(p.TimeoutSeconds) * time.Second
	return tlsapi.NewWithProvider(p.Endpoint, p.Region, &modeAwareProvider{mode: mode, p: provider}, timeout)
}

func configWithResolvedRuntimeProfile(cfg config.Config, profileName string, resolved config.Profile) config.Config {
	profiles := make(map[string]config.Profile, len(cfg.Profiles))
	for name, profile := range cfg.Profiles {
		profiles[name] = profile
	}
	if profile, ok := profiles[profileName]; ok {
		profile.Region = resolved.Region
		profile.Endpoint = resolved.Endpoint
		profile.TimeoutSeconds = resolved.TimeoutSeconds
		profiles[profileName] = profile
	}
	cfg.Profiles = profiles
	return cfg
}

// dynamicAuthError wraps an error returned by a dynamic provider's Retrieve
// with the profile mode so classifyError can produce a precise, mode-specific
// hint (re-login command for SSO/Console; local diagnostic guidance for
// workload modes) without guessing from free-text descriptions. The wrapped
// error's Error() delegates to the underlying error so existing message-based
// logic still works; errors.As/Is reach the underlying cause.
type dynamicAuthError struct {
	mode string
	err  error
}

func (e *dynamicAuthError) Error() string {
	if e == nil || e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e *dynamicAuthError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

// modeAwareProvider wraps a dynamic auth.Provider so that Retrieve errors are
// annotated with the profile mode. This lets classifyError emit a precise
// mode-specific hint—login recovery for cached modes or local source guidance
// for workload modes—without guessing from the error text.
type modeAwareProvider struct {
	mode string
	p    auth.Provider
}

func (m *modeAwareProvider) Retrieve(ctx context.Context) (auth.Value, error) {
	v, err := m.p.Retrieve(ctx)
	if err != nil {
		return auth.Value{}, &dynamicAuthError{mode: m.mode, err: err}
	}
	return v, nil
}

// dynamicAuthStatusReader resolves the authStatusReader for the current
// profile's dynamic mode using the supplied factory. It is used by doctor and
// configure show/list for offline status. Returns nil for static modes.
//
// A typed-nil factory is rejected with a clear error rather than being
// dispatched to (which would panic). A plain nil factory falls back to the
// default production factory.
func dynamicAuthStatusReader(mode string, cfgPath string, profileName string, cfg config.Config, factory authProviderFactory) (authStatusReader, error) {
	// Only SSO/Console have a disk-backed status reader. All other modes
	// (static, ramrolearn, oidc, ecsrole) have no cache status and must return
	// (nil, nil) without inspecting the factory or constructing any provider.
	if !config.IsCachedLoginAuthMode(mode) {
		return nil, nil
	}
	// SSO/Console: validate the factory before constructing the status reader.
	switch {
	case factory == nil:
		factory = defaultAuthProviderFactory{}
	case isNilFactory(factory):
		return nil, errors.New("nil auth provider factory")
	}
	switch mode {
	case config.AuthModeSSO:
		_, reader, err := factory.SSO(cfgPath, profileName, cfg)
		return reader, err
	case config.AuthModeConsoleLogin:
		_, reader, err := factory.Console(cfgPath, profileName, cfg)
		return reader, err
	}
	return nil, nil
}
