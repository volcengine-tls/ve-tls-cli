package runtime

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

// ProviderFactory constructs the provider for each dynamic authentication
// mode. Implementations must not fall back to environment AK/SK.
type ProviderFactory interface {
	SSO(configPath, profileName string, cfg config.Config) (auth.Provider, AuthStatusReader, error)
	Console(configPath, profileName string, cfg config.Config) (auth.Provider, AuthStatusReader, error)
	RamRoleARN(configPath, profileName string, cfg config.Config) (auth.Provider, error)
	OIDC(configPath, profileName string, cfg config.Config) (auth.Provider, error)
	ECSRole(configPath, profileName string, cfg config.Config) (auth.Provider, error)
}

// AuthStatus is an offline, secret-free snapshot of cached authentication
// state.
type AuthStatus struct {
	Provider        string
	Present         bool
	ExpiresAt       time.Time
	RefreshRequired bool
}

// AuthStatusReader reads authentication status without refreshing, networking,
// or writing.
type AuthStatusReader interface {
	Status(context.Context, string) (AuthStatus, error)
}

// DefaultProviderFactory constructs production SSO, Console Login, RAM role,
// OIDC, and ECS role providers.
type DefaultProviderFactory struct{}

var _ ProviderFactory = DefaultProviderFactory{}

func (DefaultProviderFactory) SSO(configPath, profileName string, cfg config.Config) (auth.Provider, AuthStatusReader, error) {
	profile, ok := cfg.GetProfile(profileName)
	if !ok {
		return nil, nil, errors.New("profile not found: " + profileName)
	}
	sessionName := strings.TrimSpace(profile.SSOSessionName)
	session, ok := cfg.SSOSessions[sessionName]
	if !ok {
		return nil, nil, errors.New("sso session not found: " + sessionName)
	}
	cache, err := sso.NewFileCache(ResolveSSOCacheDir(configPath))
	if err != nil {
		return nil, nil, err
	}
	oauthClient, err := sso.NewOAuthClient(&sso.OAuthClientConfig{Region: session.Region})
	if err != nil {
		return nil, nil, err
	}
	portalClient, err := sso.NewPortalClient(&sso.PortalClientConfig{Region: session.Region})
	if err != nil {
		return nil, nil, err
	}
	provider, err := sso.NewSSOProvider(&sso.SSOProviderConfig{
		ConfigPath:  configPath,
		ProfileName: profileName,
		StartURL:    session.StartURL,
		SessionName: session.Name,
		SSORegion:   session.Region,
		AccountID:   profile.AccountID,
		RoleName:    profile.RoleName,
		Cache:       cache,
		OAuth:       oauthClient,
		Portal:      portalClient,
		Clock:       time.Now,
	})
	if err != nil {
		return nil, nil, err
	}
	reader := NewSSOAuthStatusReader(SSOAuthStatusReaderConfig{
		Cache:       cache,
		StartURL:    session.StartURL,
		SessionName: session.Name,
		AccountID:   profile.AccountID,
		RoleName:    profile.RoleName,
		Region:      session.Region,
		Clock:       time.Now,
	})
	return provider, reader, nil
}

func (DefaultProviderFactory) Console(configPath, profileName string, cfg config.Config) (auth.Provider, AuthStatusReader, error) {
	profile, ok := cfg.GetProfile(profileName)
	if !ok {
		return nil, nil, errors.New("profile not found: " + profileName)
	}
	cache, err := console.NewFileCache(ResolveLoginCacheDir(configPath))
	if err != nil {
		return nil, nil, err
	}
	oauthFactory := func(endpointURL string) (console.OAuthClient, error) {
		return console.NewConsoleOAuthClient(&console.ConsoleOAuthClientConfig{EndpointURL: endpointURL})
	}
	provider := console.NewProvider(strings.TrimSpace(profile.LoginSession), cache, oauthFactory, time.Now)
	reader := NewConsoleAuthStatusReader(ConsoleAuthStatusReaderConfig{
		Cache:        cache,
		LoginSession: strings.TrimSpace(profile.LoginSession),
		Clock:        time.Now,
	})
	return provider, reader, nil
}

func (DefaultProviderFactory) RamRoleARN(_ string, profileName string, cfg config.Config) (auth.Provider, error) {
	profile, ok := cfg.GetProfile(profileName)
	if !ok {
		return nil, errors.New("profile not found: " + profileName)
	}
	source, err := resolveWorkloadSourceCredential(profile, cfg)
	if err != nil {
		return nil, err
	}
	return ramrole.New(ramrole.Config{
		Source:     source,
		AccountID:  strings.TrimSpace(profile.AccountID),
		RoleName:   strings.TrimSpace(profile.RoleName),
		Region:     strings.TrimSpace(profile.Region),
		DisableSSL: profile.DisableSSL,
		Client:     sts.NewClient(),
		Clock:      time.Now,
	})
}

func (DefaultProviderFactory) OIDC(_ string, profileName string, cfg config.Config) (auth.Provider, error) {
	profile, ok := cfg.GetProfile(profileName)
	if !ok {
		return nil, errors.New("profile not found: " + profileName)
	}
	tokenFile := strings.TrimSpace(profile.OIDCTokenFile)
	if tokenFile == "" {
		return nil, errors.New("OIDC token file is not configured: set oidc-token-file on the profile")
	}
	return oidc.New(oidc.Config{
		TokenFile:  tokenFile,
		RoleTRN:    strings.TrimSpace(profile.RoleTRN),
		DisableSSL: profile.DisableSSL,
		Client:     sts.NewClient(),
		Clock:      time.Now,
	})
}

func (DefaultProviderFactory) ECSRole(_ string, profileName string, cfg config.Config) (auth.Provider, error) {
	profile, ok := cfg.GetProfile(profileName)
	if !ok {
		return nil, errors.New("profile not found: " + profileName)
	}
	roleName := strings.TrimSpace(profile.RoleName)
	if roleName == "" {
		return nil, errors.New("ECS role name is not configured: set role-name on the profile")
	}
	return ecsrole.New(ecsrole.Config{
		RoleName: roleName,
		Client:   ecsrole.NewClient(),
		Clock:    time.Now,
	})
}

func resolveWorkloadSourceCredential(profile config.Profile, cfg config.Config) (sts.SourceCredential, error) {
	accessKeyID := strings.TrimSpace(profile.AccessKeyID)
	secretAccessKey := strings.TrimSpace(profile.SecretAccessKey)
	sessionToken := strings.TrimSpace(profile.SecurityToken)
	if credentialName := strings.TrimSpace(profile.CredRef); credentialName != "" {
		credential, ok := cfg.GetCred(credentialName)
		if !ok {
			return sts.SourceCredential{}, errors.New("credential not found: " + credentialName)
		}
		if accessKeyID == "" {
			accessKeyID = strings.TrimSpace(credential.AccessKeyID)
		}
		if secretAccessKey == "" {
			secretAccessKey = strings.TrimSpace(credential.SecretAccessKey)
		}
	}
	if accessKeyID == "" || secretAccessKey == "" {
		return sts.SourceCredential{}, errors.New("RAM role source credentials are missing: set inline access_key_id/secret_access_key or cred_ref on the profile")
	}
	return sts.SourceCredential{
		AccessKeyID:     accessKeyID,
		SecretAccessKey: secretAccessKey,
		SessionToken:    sessionToken,
	}, nil
}

// ResolveSSOCacheDir returns the configured SSO cache directory or the
// config-sibling default.
func ResolveSSOCacheDir(configPath string) string {
	if directory := strings.TrimSpace(os.Getenv("VOLCLOG_SSO_CACHE_DIRECTORY")); directory != "" {
		return directory
	}
	return filepath.Join(filepath.Dir(configPath), "sso", "cache")
}

// ResolveLoginCacheDir returns the configured Console Login cache directory or
// the config-sibling default.
func ResolveLoginCacheDir(configPath string) string {
	if directory := strings.TrimSpace(os.Getenv("VOLCLOG_LOGIN_CACHE_DIRECTORY")); directory != "" {
		return directory
	}
	return filepath.Join(filepath.Dir(configPath), "login", "cache")
}

// BuildClientRequest contains the resolved runtime settings and provider
// assembly inputs for one client.
type BuildClientRequest struct {
	Mode        string
	ConfigPath  string
	ProfileName string
	Config      config.Config
	Profile     config.Profile
	// SDKProfile is the original explicit SDK profile selector passed through
	// to the legacy static client constructor. tlsapi currently ignores this
	// compatibility parameter, but keeping it separate avoids conflating it
	// with the resolved ProfileName.
	SDKProfile  string
	ForceStatic bool
	Factory     ProviderFactory
}

// BuildClient constructs either the legacy static client or a fail-closed
// provider-backed client.
func BuildClient(request BuildClientRequest) (*tlsapi.Client, error) {
	mode := strings.ToLower(strings.TrimSpace(request.Mode))
	if mode == "" {
		mode = config.AuthModeAK
	}
	if request.ForceStatic || mode == config.AuthModeAK {
		return tlsapi.New(
			request.Profile.Endpoint,
			request.Profile.Region,
			request.SDKProfile,
			request.Profile.AccessKeyID,
			request.Profile.SecretAccessKey,
			request.Profile.SecurityToken,
			time.Duration(request.Profile.TimeoutSeconds)*time.Second,
		)
	}
	if !config.IsProviderAuthMode(mode) {
		return nil, errors.New("unsupported auth mode: " + mode)
	}

	factory := request.Factory
	switch {
	case factory == nil:
		factory = DefaultProviderFactory{}
	case isTypedNil(factory):
		return nil, errors.New("nil auth provider factory")
	}

	cfg := configWithResolvedRuntimeProfile(request.Config, request.ProfileName, request.Profile)
	var (
		provider auth.Provider
		err      error
	)
	switch mode {
	case config.AuthModeSSO:
		provider, _, err = factory.SSO(request.ConfigPath, request.ProfileName, cfg)
	case config.AuthModeConsoleLogin:
		provider, _, err = factory.Console(request.ConfigPath, request.ProfileName, cfg)
	case config.AuthModeRamRoleARN:
		provider, err = factory.RamRoleARN(request.ConfigPath, request.ProfileName, cfg)
	case config.AuthModeOIDC:
		provider, err = factory.OIDC(request.ConfigPath, request.ProfileName, cfg)
	case config.AuthModeECSRole:
		provider, err = factory.ECSRole(request.ConfigPath, request.ProfileName, cfg)
	}
	if err != nil {
		return nil, err
	}
	if isTypedNil(provider) {
		return nil, errors.New("nil auth provider")
	}
	return tlsapi.NewWithProvider(
		request.Profile.Endpoint,
		request.Profile.Region,
		&modeAwareProvider{mode: mode, provider: provider},
		time.Duration(request.Profile.TimeoutSeconds)*time.Second,
	)
}

// DynamicAuthStatusReader resolves the offline status reader for cached login
// modes. Static and workload modes return (nil, nil) without touching factory.
func DynamicAuthStatusReader(
	mode, configPath, profileName string,
	cfg config.Config,
	factory ProviderFactory,
) (AuthStatusReader, error) {
	if !config.IsCachedLoginAuthMode(mode) {
		return nil, nil
	}
	switch {
	case factory == nil:
		factory = DefaultProviderFactory{}
	case isTypedNil(factory):
		return nil, errors.New("nil auth provider factory")
	}
	var (
		reader AuthStatusReader
		err    error
	)
	switch mode {
	case config.AuthModeSSO:
		_, reader, err = factory.SSO(configPath, profileName, cfg)
	case config.AuthModeConsoleLogin:
		_, reader, err = factory.Console(configPath, profileName, cfg)
	default:
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if isTypedNil(reader) {
		return nil, errors.New("nil auth status reader")
	}
	return reader, nil
}

// SSOAuthStatusReaderConfig contains the cache binding needed for offline SSO
// status inspection.
type SSOAuthStatusReaderConfig struct {
	Cache       sso.Cache
	StartURL    string
	SessionName string
	AccountID   string
	RoleName    string
	Region      string
	Clock       func() time.Time
}

// NewSSOAuthStatusReader constructs a read-only SSO cache status reader.
func NewSSOAuthStatusReader(cfg SSOAuthStatusReaderConfig) AuthStatusReader {
	clock := cfg.Clock
	if clock == nil {
		clock = time.Now
	}
	return &ssoAuthStatusReader{
		cache:       cfg.Cache,
		startURL:    cfg.StartURL,
		sessionName: cfg.SessionName,
		accountID:   cfg.AccountID,
		roleName:    cfg.RoleName,
		region:      cfg.Region,
		clock:       clock,
	}
}

type ssoAuthStatusReader struct {
	cache       sso.Cache
	startURL    string
	sessionName string
	accountID   string
	roleName    string
	region      string
	clock       func() time.Time
}

func (r *ssoAuthStatusReader) Status(context.Context, string) (AuthStatus, error) {
	if r == nil || r.cache == nil {
		return AuthStatus{Provider: sso.ProviderName, RefreshRequired: true}, nil
	}
	now := r.clock()

	tokenCache, tokenErr := r.cache.ReadToken(r.startURL, r.sessionName)
	tokenPresent := false
	tokenRefresh := true
	var tokenExpires time.Time
	if tokenErr == nil && tokenCache != nil {
		if status, err := sso.InspectTokenCache(tokenCache, r.startURL, r.sessionName, r.region, now); err == nil {
			tokenPresent = status.Present
			tokenRefresh = status.RefreshRequired
			tokenExpires = status.ExpiresAt
		}
	}

	stsCache, stsErr := r.cache.ReadSTS(r.sessionName, r.accountID, r.roleName)
	stsPresent := false
	stsRefresh := true
	var stsExpires time.Time
	if stsErr == nil {
		if status, err := sso.InspectSTSCache(stsCache, r.sessionName, r.accountID, r.roleName, now); err == nil {
			stsPresent = status.Present
			stsRefresh = status.RefreshRequired
			stsExpires = status.ExpiresAt
		}
	}

	present := tokenPresent && stsPresent
	refreshRequired := tokenRefresh || stsRefresh
	var expiresAt time.Time
	if present {
		if !tokenExpires.IsZero() && (stsExpires.IsZero() || tokenExpires.Before(stsExpires)) {
			expiresAt = tokenExpires
		} else if !stsExpires.IsZero() {
			expiresAt = stsExpires
		}
	}
	return AuthStatus{
		Provider:        sso.ProviderName,
		Present:         present,
		ExpiresAt:       expiresAt,
		RefreshRequired: refreshRequired,
	}, nil
}

// ConsoleAuthStatusReaderConfig contains the cache binding needed for offline
// Console Login status inspection.
type ConsoleAuthStatusReaderConfig struct {
	Cache        console.ConsoleCache
	LoginSession string
	Clock        func() time.Time
}

// NewConsoleAuthStatusReader constructs a read-only Console Login cache status
// reader.
func NewConsoleAuthStatusReader(cfg ConsoleAuthStatusReaderConfig) AuthStatusReader {
	clock := cfg.Clock
	if clock == nil {
		clock = time.Now
	}
	return &consoleAuthStatusReader{
		cache:        cfg.Cache,
		loginSession: cfg.LoginSession,
		clock:        clock,
	}
}

type consoleAuthStatusReader struct {
	cache        console.ConsoleCache
	loginSession string
	clock        func() time.Time
}

func (r *consoleAuthStatusReader) Status(context.Context, string) (AuthStatus, error) {
	if r == nil || r.cache == nil || strings.TrimSpace(r.loginSession) == "" {
		return AuthStatus{Provider: console.ProviderName, RefreshRequired: true}, nil
	}
	data, existed, err := r.cache.ReadRaw(r.loginSession)
	if err != nil || !existed || len(data) == 0 {
		return AuthStatus{Provider: console.ProviderName, RefreshRequired: true}, nil
	}
	var cache console.LoginTokenCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return AuthStatus{Provider: console.ProviderName, RefreshRequired: true}, nil
	}
	status, err := console.InspectLoginCache(&cache, r.loginSession, r.clock())
	if err != nil {
		return AuthStatus{Provider: console.ProviderName, RefreshRequired: true}, nil
	}
	return AuthStatus{
		Provider:        console.ProviderName,
		Present:         status.Present,
		ExpiresAt:       status.ExpiresAt,
		RefreshRequired: status.RefreshRequired,
	}, nil
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

func isTypedNil(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

// DynamicAuthError preserves the provider's public error text while exposing
// the selected mode to error classification.
type DynamicAuthError struct {
	mode string
	err  error
}

// NewDynamicAuthError annotates a provider error with its auth mode without
// changing the public error text.
func NewDynamicAuthError(mode string, err error) *DynamicAuthError {
	return &DynamicAuthError{
		mode: strings.ToLower(strings.TrimSpace(mode)),
		err:  err,
	}
}

func (e *DynamicAuthError) Error() string {
	if e == nil || e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e *DynamicAuthError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

// AuthMode returns the normalized provider authentication mode.
func (e *DynamicAuthError) AuthMode() string {
	if e == nil {
		return ""
	}
	return e.mode
}

type modeAwareProvider struct {
	mode     string
	provider auth.Provider
}

func (p *modeAwareProvider) Retrieve(ctx context.Context) (auth.Value, error) {
	value, err := p.provider.Retrieve(ctx)
	if err != nil {
		return auth.Value{}, NewDynamicAuthError(p.mode, err)
	}
	return value, nil
}
