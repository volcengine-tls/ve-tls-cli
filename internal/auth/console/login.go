package console

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth/browser"
	"github.com/volcengine-tls/ve-tls-cli/internal/auth/oauth"
	"github.com/volcengine-tls/ve-tls-cli/internal/config"
	"github.com/volcengine-tls/ve-tls-cli/internal/securestore"
)

// isNilInterface reports whether v is nil or a typed-nil interface value (e.g.
// a nil *FileCache stored in a ConsoleCache interface). It uses reflect only on
// nil-capable kinds so it never panics on non-interface values.
func isNilInterface(v interface{}) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return rv.IsNil()
	}
	return false
}

// safeError wraps one or more causes with a fixed, safe description. Its Error()
// never renders the underlying cause text (which may contain secrets), but
// Unwrap preserves the causes so errors.Is/errors.As still match them.
type safeError struct {
	desc   string
	causes []error
}

func (e *safeError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return e.desc
}

func (e *safeError) Unwrap() []error {
	if e == nil {
		return nil
	}
	return e.causes
}

// newSafeError builds a safeError with the given description and causes. A nil
// cause is dropped so callers can pass primary/cleanup errors unconditionally.
func newSafeError(desc string, causes ...error) error {
	kept := causes[:0]
	for _, c := range causes {
		if c != nil {
			kept = append(kept, c)
		}
	}
	if len(kept) == 0 {
		return errors.New(desc)
	}
	return &safeError{desc: desc, causes: kept}
}

// ConsoleCache abstracts the login token cache storage with per-session
// cross-process locking. The production implementation (FileCache) uses
// securestore primitives; tests inject fakes.
//
// WithLock holds the per-session lock for the duration of fn. Inside fn,
// ReadRaw/WriteRaw/Delete operate on the explicit SHA-1 data path so that
// Login rollback can restore the precise pre-existing bytes.
type ConsoleCache interface {
	WithLock(ctx context.Context, loginSession string, fn func() error) error
	ReadRaw(loginSession string) ([]byte, bool, error)
	WriteRaw(loginSession string, data []byte) error
	Delete(loginSession string) error
}

// ProfileStore abstracts config load/update for the login service. The
// production implementation wraps config.Load and config.Update.
type ProfileStore interface {
	Load() (config.Config, string, error)
	Update(path string, fn func(*config.Config) error) (config.Config, error)
}

// configProfileStore is the production ProfileStore.
type configProfileStore struct{}

func (configProfileStore) Load() (config.Config, string, error) {
	return config.Load()
}

func (configProfileStore) Update(path string, fn func(*config.Config) error) (config.Config, error) {
	return config.Update(path, fn)
}

// OAuthClientFactory creates an OAuthClient for the given endpoint URL.
type OAuthClientFactory func(endpointURL string) (OAuthClient, error)

// AuthorizerFactory creates an Authorizer bound to the given client, state,
// and code challenge.
type AuthorizerFactory func(client OAuthClient, state, codeChallenge string) Authorizer

// LoginOptions holds the parameters for a Console Login attempt.
type LoginOptions struct {
	// DeviceCode explicitly selects the cross-device Device Authorization Grant.
	// The default is the same-device browser callback flow with PKCE.
	DeviceCode bool
	// Remote is retained for command-line compatibility and implies DeviceCode
	// plus NoBrowser.
	Remote bool
	// NoBrowser implies DeviceCode and suppresses best-effort browser opening
	// while retaining the printed verification URL and user code.
	NoBrowser bool
	// EndpointURL is the Console OAuth base endpoint selected by
	// --login-endpoint. When empty, DefaultEndpoint is used. The normalized
	// value is stored in the login cache so refresh uses the same issuer.
	EndpointURL string
	Profile     string
	Region      string
	Endpoint    string
}

// LoginResult is the redacted result of a successful Console Login. It never
// contains the full AK, SK, SessionToken, AccessToken, RefreshToken, IDToken,
// authorization code, state, or PKCE verifier.
type LoginResult struct {
	Profile         string
	Provider        string
	Region          string
	Endpoint        string
	ExpiresAt       time.Time
	MaskedAccessKey string
}

// LoginService orchestrates the Console Login flow: authorization, token
// exchange, STS validation, cache write, and config patch. All dependencies
// are injectable so tests run without network, browser, or real sockets.
type LoginService struct {
	oauthClientFactory OAuthClientFactory
	// DeviceFlowFactory handles the explicit cross-device login path.
	deviceFlowFactory DeviceFlowFactory
	prompt            io.Writer
	opener            browser.Opener
	sleeper           DeviceSleeper

	// localAuthorizer handles the default same-device browser callback path.
	// remoteAuthorizer remains only for source compatibility; Device Code is the
	// supported remote path.
	localAuthorizer  AuthorizerFactory
	remoteAuthorizer AuthorizerFactory
	cache            ConsoleCache
	profileStore     ProfileStore
	clock            func() time.Time
	pkceGenerator    func() (oauth.PKCE, error)
	stateGenerator   func() (string, error)
	confirm          func(profileName, currentSession, newSession string) (bool, error)
}

// LoginServiceConfig holds the injectable dependencies for LoginService.
type LoginServiceConfig struct {
	OAuthClientFactory OAuthClientFactory
	DeviceFlowFactory  DeviceFlowFactory
	Prompt             io.Writer
	Browser            browser.Opener
	Sleeper            DeviceSleeper
	LocalAuthorizer    AuthorizerFactory
	RemoteAuthorizer   AuthorizerFactory
	Cache              ConsoleCache
	ProfileStore       ProfileStore
	Clock              func() time.Time
	PKCEGenerator      func() (oauth.PKCE, error)
	StateGenerator     func() (string, error)
	Confirm            func(profileName, currentSession, newSession string) (bool, error)
}

// NewLoginService constructs a LoginService from the given config. Nil
// dependencies are replaced with production defaults where possible. A nil cfg
// is treated as an empty config.
func NewLoginService(cfg *LoginServiceConfig) *LoginService {
	if cfg == nil {
		cfg = &LoginServiceConfig{}
	}
	ls := &LoginService{
		oauthClientFactory: cfg.OAuthClientFactory,
		deviceFlowFactory:  cfg.DeviceFlowFactory,
		prompt:             cfg.Prompt,
		opener:             cfg.Browser,
		sleeper:            cfg.Sleeper,
		localAuthorizer:    cfg.LocalAuthorizer,
		remoteAuthorizer:   cfg.RemoteAuthorizer,
		cache:              cfg.Cache,
		profileStore:       cfg.ProfileStore,
		clock:              cfg.Clock,
		pkceGenerator:      cfg.PKCEGenerator,
		stateGenerator:     cfg.StateGenerator,
		confirm:            cfg.Confirm,
	}
	if ls.oauthClientFactory == nil {
		ls.oauthClientFactory = defaultOAuthClientFactory
	}
	if ls.deviceFlowFactory == nil {
		ls.deviceFlowFactory = defaultDeviceFlowFactory
	}
	if ls.prompt == nil || isNilInterface(ls.prompt) {
		ls.prompt = io.Discard
	}
	if ls.sleeper == nil {
		ls.sleeper = sleepDeviceAuthorization
	}
	if ls.profileStore == nil {
		ls.profileStore = configProfileStore{}
	}
	if ls.clock == nil {
		ls.clock = time.Now
	}
	if ls.pkceGenerator == nil {
		ls.pkceGenerator = func() (oauth.PKCE, error) { return oauth.GeneratePKCE(nil) }
	}
	if ls.stateGenerator == nil {
		ls.stateGenerator = func() (string, error) { return oauth.GenerateState(nil) }
	}
	if ls.localAuthorizer == nil {
		ls.localAuthorizer = func(client OAuthClient, state, codeChallenge string) Authorizer {
			return NewDefaultLocalAuthorizer(client, ls.opener, ls.prompt, state, codeChallenge)
		}
	}
	return ls
}

func defaultDeviceFlowFactory(client OAuthClient, prompt io.Writer, opener browser.Opener, noBrowser bool, clock func() time.Time, sleeper DeviceSleeper) DeviceFlow {
	flow := NewDeviceAuthorizationFlow(client, prompt, opener, clock, sleeper)
	flow.NoBrowser = noBrowser
	return flow
}

// defaultOAuthClientFactory wraps NewConsoleOAuthClient.
func defaultOAuthClientFactory(endpointURL string) (OAuthClient, error) {
	return NewConsoleOAuthClient(&ConsoleOAuthClientConfig{EndpointURL: endpointURL})
}

// Login runs the full Console Login flow and returns a redacted LoginResult.
//
// The sequence is frozen: resolve profile/region, run the selected authorization
// flow without fallback, obtain a token, validate STS, extract
// login-session, confirm replacement if needed, then atomically write cache and
// patch config inside the per-session cache lock with rollback on config failure.
func (s *LoginService) Login(ctx context.Context, opts LoginOptions) (*LoginResult, error) {
	if s == nil {
		return nil, errors.New("nil *LoginService")
	}
	if isNilInterface(ctx) {
		return nil, errors.New("nil context")
	}
	if isNilInterface(s.cache) {
		return nil, errors.New("nil cache")
	}
	if isNilInterface(s.profileStore) {
		return nil, errors.New("nil profile store")
	}
	if s.oauthClientFactory == nil {
		return nil, errors.New("nil oauth client factory")
	}
	if s.clock == nil {
		return nil, errors.New("nil clock")
	}

	// 1. Resolve profile and region from the latest loaded config.
	cfg, cfgPath, err := s.profileStore.Load()
	if err != nil {
		return nil, newSafeError("load config failed", err)
	}
	profileName := cfg.SelectedProfileName(opts.Profile)
	profile, _ := cfg.GetProfile(profileName)

	region := strings.TrimSpace(opts.Region)
	if region == "" {
		region = strings.TrimSpace(profile.Region)
	}
	tlsEndpoint := strings.TrimSpace(opts.Endpoint)
	if tlsEndpoint == "" {
		tlsEndpoint = strings.TrimSpace(profile.Endpoint)
	}

	// 2. Create the OAuth client. The selected flow controls the frozen client
	// ID persisted in cache and reused by refresh.
	useDeviceCode := opts.DeviceCode || opts.NoBrowser || opts.Remote
	clientID := ClientIDSameDevice
	if useDeviceCode {
		clientID = ClientIDCrossDevice
	}
	endpoint := strings.TrimSpace(opts.EndpointURL)
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	client, err := s.oauthClientFactory(endpoint)
	if err != nil {
		return nil, newSafeError("create oauth client failed", err)
	}
	if isNilInterface(client) {
		return nil, errors.New("oauth client factory returned nil client")
	}
	// Freeze the normalized endpoint from the client, not the raw input.
	endpoint = strings.TrimRight(client.EndpointURL(), "/")

	// 3. Complete the selected authorization flow. No cache lock is held while
	// the user authorizes or while network requests are in flight. A failure is
	// returned directly; login never falls back to the other flow.
	var tokenResp *ConsoleTokenResponse
	if useDeviceCode {
		if s.deviceFlowFactory == nil {
			return nil, errors.New("nil device flow factory")
		}
		flow := s.deviceFlowFactory(client, s.prompt, s.opener, opts.NoBrowser || opts.Remote, s.clock, s.sleeper)
		if isNilInterface(flow) {
			return nil, errors.New("device flow factory returned nil flow")
		}
		tokenResp, err = flow.Authorize(ctx)
		if err != nil {
			return nil, newSafeError("device authorization failed", err)
		}
	} else {
		if s.localAuthorizer == nil {
			return nil, errors.New("nil local authorizer factory")
		}
		if s.pkceGenerator == nil {
			return nil, errors.New("nil PKCE generator")
		}
		if s.stateGenerator == nil {
			return nil, errors.New("nil state generator")
		}
		pkce, pkceErr := s.pkceGenerator()
		if pkceErr != nil {
			return nil, newSafeError("generate PKCE failed", pkceErr)
		}
		state, stateErr := s.stateGenerator()
		if stateErr != nil {
			return nil, newSafeError("generate OAuth state failed", stateErr)
		}
		authorizer := s.localAuthorizer(client, state, pkce.Challenge)
		if isNilInterface(authorizer) {
			return nil, errors.New("local authorizer factory returned nil authorizer")
		}
		code, redirectURI, authorizeErr := authorizer.Authorize(ctx)
		if authorizeErr != nil {
			return nil, newSafeError("browser authorization failed", authorizeErr)
		}
		tokenResp, err = client.ExchangeToken(ctx, &ConsoleTokenRequest{
			GrantType:    GrantTypeAuthorizationCode,
			Code:         code,
			ClientID:     ClientIDSameDevice,
			Scope:        Scope,
			CodeVerifier: pkce.Verifier,
			RedirectURI:  redirectURI,
		})
		if err != nil {
			return nil, newSafeError("authorization code exchange failed", err)
		}
	}
	if tokenResp == nil {
		return nil, errors.New("token exchange returned nil response")
	}

	// 5b. Require a non-empty TokenType before any cache/config mutation. An
	// empty TokenType fails closed with a fixed safe error: no cache write and
	// no config update occur.
	if strings.TrimSpace(tokenResp.TokenType) == "" {
		return nil, errors.New("token exchange returned empty token type")
	}

	// 6. Validate STS credentials.
	sts, err := ParseSTSCredentials(tokenResp.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("validate STS credentials: %w", err)
	}

	// 7. Extract login-session from ID token.
	loginSession, err := ExtractLoginSession(tokenResp.IDToken)
	if err != nil {
		return nil, fmt.Errorf("extract login session: %w", err)
	}

	// 8. If the profile is bound to a different session, require confirmation.
	existingSession := profile.LoginSession
	if existingSession != "" && existingSession != loginSession {
		if s.confirm == nil {
			return nil, errors.New("login session replacement requires confirmation")
		}
		ok, err := s.confirm(profileName, existingSession, loginSession)
		if err != nil {
			return nil, newSafeError("login session confirmation failed", err)
		}
		if !ok {
			return nil, errors.New("login cancelled: user rejected session replacement")
		}
	}

	// 9. Compute expiration using CacheExpiration (handles overflow/invalid).
	now := s.clock().UTC()
	issuedAt := now.Format(time.RFC3339Nano)
	expiresAt, err := CacheExpiration(issuedAt, tokenResp.ExpiresIn)
	if err != nil {
		return nil, fmt.Errorf("compute token expiration: %w", err)
	}

	// 10. Determine the frozen scope: use the returned scope only if it
	// satisfies the frozen contract (non-empty and contains the frozen scope);
	// otherwise keep the frozen Scope.
	cacheScope := Scope
	if sc := strings.TrimSpace(tokenResp.Scope); sc != "" {
		if scopeSatisfiesFrozen(sc) {
			cacheScope = sc
		}
	}

	err = s.cache.WithLock(ctx, loginSession, func() error {
		// 11. Re-read and retain exact raw bytes plus prior existence.
		priorBytes, existed, rerr := s.cache.ReadRaw(loginSession)
		if rerr != nil {
			return newSafeError("read cache snapshot failed", rerr)
		}

		// 12. Atomically write the new LoginTokenCache.
		newCache := &LoginTokenCache{
			LoginSession: loginSession,
			AccessToken:  tokenResp.AccessToken,
			RefreshToken: tokenResp.RefreshToken,
			IDToken:      tokenResp.IDToken,
			Scope:        cacheScope,
			ClientID:     clientID,
			EndpointURL:  endpoint,
			IssuedAt:     issuedAt,
			ExpiresIn:    tokenResp.ExpiresIn,
			TokenType:    tokenResp.TokenType,
		}
		cacheBytes, merr := json.Marshal(newCache)
		if merr != nil {
			return newSafeError("marshal cache failed", merr)
		}
		if werr := s.cache.WriteRaw(loginSession, cacheBytes); werr != nil {
			return newSafeError("write cache failed", werr)
		}

		// 13. Patch config inside the same cache lock. The callback must not
		// acquire any cache lock (lock order: console cache -> config path).
		_, configErr := s.profileStore.Update(cfgPath, func(c *config.Config) error {
			// 14. Detect concurrent unconfirmed profile-session replacement.
			p, _ := c.GetProfile(profileName)
			if p.LoginSession != "" && p.LoginSession != loginSession && p.LoginSession != existingSession {
				return errors.New("concurrent login-session replacement detected")
			}
			// Patch the auth binding plus only the TLS runtime fields explicitly
			// supplied by the user. Omitted fields preserve the latest config
			// values, including an intentionally unconfigured empty value.
			p.Mode = config.AuthModeConsoleLogin
			p.LoginSession = loginSession
			if explicitRegion := strings.TrimSpace(opts.Region); explicitRegion != "" {
				p.Region = explicitRegion
			}
			if explicitEndpoint := strings.TrimSpace(opts.Endpoint); explicitEndpoint != "" {
				p.Endpoint = explicitEndpoint
			}
			c.PutProfile(profileName, p)
			return nil
		})

		// 15. If config update fails, restore the exact prior bytes.
		if configErr != nil {
			if existed {
				if rerr := s.cache.WriteRaw(loginSession, priorBytes); rerr != nil {
					// 16. Retain both causes via safe wrapper; do not return credentials.
					return newSafeError("config update and cache rollback failed", configErr, rerr)
				}
			} else {
				if derr := s.cache.Delete(loginSession); derr != nil {
					return newSafeError("config update and cache cleanup failed", configErr, derr)
				}
			}
			return newSafeError("config update failed", configErr)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// 17. Return only the redacted LoginResult.
	return &LoginResult{
		Profile:         profileName,
		Provider:        "console-login",
		Region:          region,
		Endpoint:        tlsEndpoint,
		ExpiresAt:       expiresAt,
		MaskedAccessKey: config.MaskAK(sts.AccessKeyID),
	}, nil
}

// scopeSatisfiesFrozen reports whether the server-returned scope satisfies the
// frozen scope contract: it must be non-empty and contain the frozen Scope as a
// space-separated subset.
func scopeSatisfiesFrozen(returnedScope string) bool {
	returnedScope = strings.TrimSpace(returnedScope)
	if returnedScope == "" {
		return false
	}
	want := strings.Fields(Scope)
	got := strings.Fields(returnedScope)
	gotSet := make(map[string]struct{}, len(got))
	for _, s := range got {
		gotSet[s] = struct{}{}
	}
	for _, w := range want {
		if _, ok := gotSet[w]; !ok {
			return false
		}
	}
	return true
}

// FileCache is the production ConsoleCache implementation. It uses
// securestore.Store.WithLock for the outer per-session lock and
// securestore.UpdateFile for the explicit SHA-1 data path, preserving
// upstream-compatible cache filenames.
type FileCache struct {
	store *securestore.Store
	dir   string
}

// NewFileCache creates a FileCache rooted at dir. The dir must be explicitly
// provided; the auth core never reads env/HOME/.volclog to resolve a default.
// The directory is immediately canonicalized through securestore so that the
// lock root and the explicit SHA-1 data path share the exact same validated
// absolute root; an invalid root is rejected at construction time rather than
// deferred to the first operation.
func NewFileCache(dir string) (*FileCache, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("cache dir is required; auth core does not read env/HOME defaults")
	}
	store := securestore.New(dir)
	root, err := store.CanonicalRoot()
	if err != nil {
		return nil, err
	}
	return &FileCache{store: store, dir: root}, nil
}

// WithLock runs fn while holding the per-session cross-process lock.
func (c *FileCache) WithLock(ctx context.Context, loginSession string, fn func() error) error {
	if c == nil {
		return errors.New("nil *FileCache")
	}
	if c.store == nil {
		return errors.New("nil *FileCache store")
	}
	if isNilInterface(ctx) {
		return errors.New("nil context")
	}
	if strings.TrimSpace(loginSession) == "" {
		return errors.New("empty login session")
	}
	if fn == nil {
		return errors.New("nil function")
	}
	// Derive the safe SHA-1 cache filename first. Real login-session values
	// (e.g. TRNs) contain "/", which securestore key validation rejects; the
	// hashed filename is slash-free and matches the explicit data path so the
	// lock and the cache file stay coupled to the same session.
	name, err := CacheFilename(loginSession)
	if err != nil {
		return err
	}
	return c.store.WithLock(ctx, "console", name, fn)
}

// dataPath returns the explicit SHA-1 cache file path for the session.
func (c *FileCache) dataPath(loginSession string) (string, error) {
	name, err := CacheFilename(loginSession)
	if err != nil {
		return "", err
	}
	return filepath.Join(c.dir, name), nil
}

// ReadRaw returns the exact raw bytes of the cache file and whether it existed.
// It rejects symlink and non-regular targets rather than following them.
func (c *FileCache) ReadRaw(loginSession string) ([]byte, bool, error) {
	if c == nil {
		return nil, false, errors.New("nil *FileCache")
	}
	if c.store == nil {
		return nil, false, errors.New("nil *FileCache store")
	}
	path, err := c.dataPath(loginSession)
	if err != nil {
		return nil, false, err
	}
	// Validate the file is a regular private (0600) file before reading.
	// Broad-permission caches fail closed and are never used on the fast path.
	if verr := securestore.ValidatePrivateFile(path); verr != nil {
		if errors.Is(verr, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, verr
	}
	data, rerr := os.ReadFile(path)
	if rerr != nil {
		if errors.Is(rerr, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, rerr
	}
	return data, true, nil
}

// WriteRaw atomically writes the cache file with 0600 permissions. It retains
// securestore atomic replacement and rejects symlink/non-regular targets before
// writing so an attacker cannot redirect the write through a pre-placed symlink.
func (c *FileCache) WriteRaw(loginSession string, data []byte) error {
	if c == nil {
		return errors.New("nil *FileCache")
	}
	if c.store == nil {
		return errors.New("nil *FileCache store")
	}
	path, err := c.dataPath(loginSession)
	if err != nil {
		return err
	}
	// Reject symlinks and non-regular targets before delegating to
	// securestore.UpdateFile, which canonicalizes an existing target symlink and
	// therefore cannot by itself enforce this contract.
	if info, lerr := os.Lstat(path); lerr == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("cache path is a symlink")
		}
		if !info.Mode().IsRegular() {
			return errors.New("cache path is not a regular file")
		}
	} else if !errors.Is(lerr, os.ErrNotExist) {
		return lerr
	}
	return securestore.UpdateFile(path, 0o600, func([]byte) ([]byte, error) {
		return data, nil
	})
}

// Delete removes the cache file; missing is idempotent. It rejects symlink
// targets rather than following them.
func (c *FileCache) Delete(loginSession string) error {
	if c == nil {
		return errors.New("nil *FileCache")
	}
	if c.store == nil {
		return errors.New("nil *FileCache store")
	}
	path, err := c.dataPath(loginSession)
	if err != nil {
		return err
	}
	// Reject symlinks before removing.
	if info, lerr := os.Lstat(path); lerr == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("cache path is a symlink")
		}
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Compile-time assertions that the production types satisfy the interfaces.
var (
	_ ConsoleCache = (*FileCache)(nil)
	_ ProfileStore = configProfileStore{}
)
