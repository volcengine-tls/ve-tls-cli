package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth/browser"
	"github.com/volcengine-tls/ve-tls-cli/internal/auth/sso"
	"github.com/volcengine-tls/ve-tls-cli/internal/config"
	"github.com/volcengine-tls/ve-tls-cli/internal/output"
	"github.com/volcengine-tls/ve-tls-cli/internal/securestore"
)

// allowedSSOScopes is the frozen set of registration scopes permitted for
// configure sso-session. Any scope outside this set is rejected so the device
// flow never requests an unsupported or unsafe scope.
var allowedSSOScopes = map[string]struct{}{
	sso.ScopeAccountAccess: {},
	sso.ScopeOfflineAccess: {},
}

// defaultSSOScopes is the scope set used when --registration-scopes is omitted
// for a brand-new session.
var defaultSSOScopes = []string{sso.ScopeAccountAccess, sso.ScopeOfflineAccess}

// ssoSessionOpts holds the parsed flags for configure sso-session.
type ssoSessionOpts struct {
	Name               string
	StartURL           string
	Region             string
	RegistrationScopes []string
	// scopesExplicit records whether --registration-scopes was provided so patch
	// semantics can distinguish "omit -> preserve" from "set -> replace".
	scopesExplicit bool
}

// ssoConfigureOpts holds the parsed flags for configure sso.
type ssoConfigureOpts struct {
	Profile    string
	SSOSession string
	AccountID  string
	RoleName   string
	Region     string
	Endpoint   string
	NoBrowser  bool
}

// ssoDeviceFlow is the minimal interface the CLI adapter uses to run an SSO
// device authorization login. Production wraps *sso.DeviceFlow; tests inject a
// fake that returns a token cache directly.
type ssoDeviceFlow interface {
	Login(ctx context.Context) (*sso.TokenCache, error)
}

// ssoBindingService is the minimal interface the CLI adapter uses to resolve an
// account/role binding after login. Production wraps *sso.BindingService; tests
// inject a fake.
type ssoBindingService interface {
	ResolveBinding(ctx context.Context, accessToken, explicitAccountID, explicitRoleName string) (*sso.BindingResult, error)
}

// ssoCache is the minimal interface for the SSO token and STS caches with
// per-key cross-process locking used by login/configure/logout. Production
// wraps *sso.FileCache; tests inject a fake. WithTokenLock must acquire the
// exact same lock as sso.Provider.Retrieve so login/logout and refresh are
// serialized.
type ssoCache interface {
	WithTokenLock(ctx context.Context, startURL, sessionName string, fn func() error) error
	ReadToken(startURL, sessionName string) (*sso.TokenCache, error)
	WriteToken(cache *sso.TokenCache) error
	DeleteToken(startURL, sessionName string) error
	WithSTSLock(ctx context.Context, sessionName, accountID, roleName string, fn func() error) error
	ReadSTS(sessionName, accountID, roleName string) (*sso.STSCache, error)
	WriteSTS(cache *sso.STSCache) error
	DeleteSTS(sessionName, accountID, roleName string) error
}

// ssoOAuthRevoker is the minimal interface for revoking a refresh token during
// logout. Production wraps *sso.OAuthClient; tests inject a fake.
type ssoOAuthRevoker interface {
	RevokeToken(ctx context.Context, req *sso.RevokeTokenRequest) error
}

// ssoDeviceFlowFactory builds a DeviceFlow for a specific session. Production
// captures the real *sso.FileCache and builds the client with the session's
// region; tests inject a fake that records the region it was asked for.
type ssoDeviceFlowFactory func(session config.SSOSession, noBrowser bool) (ssoDeviceFlow, error)

// ssoBindingServiceFactory builds a BindingService for a specific session. The
// session's region selects the Portal endpoint; production injects the stdin
// account/role selectors.
type ssoBindingServiceFactory func(session config.SSOSession) (ssoBindingService, error)

// ssoRevokerFactory builds a refresh-token revoker for a specific region.
type ssoRevokerFactory func(region string) (ssoOAuthRevoker, error)

// ssoAdapter holds the injectable dependencies for the configure sso / sso
// login / sso logout commands. Run-level functions construct it with production
// defaults; tests construct it directly with fakes. No dependency is a
// process-level global. The DeviceFlow / BindingService / Revoker are built
// lazily per-call from the session config so the client region always matches
// the target session (never a hard-coded default).
type ssoAdapter struct {
	cache        ssoCache
	cfgStore     configStore
	deviceFlowFn ssoDeviceFlowFactory
	bindingFn    ssoBindingServiceFactory
	revokerFn    ssoRevokerFactory
	stdin        io.Reader
	stdout       io.Writer
	stderr       io.Writer
	clock        func() time.Time
}

// ssoLoginResult is the redacted result of a successful SSO login. It never
// contains OAuth token material: at this stage there is no real STS AK, so no
// masked access key is emitted.
type ssoLoginResult struct {
	Profile   string    `json:"profile,omitempty"`
	Provider  string    `json:"provider"`
	Session   string    `json:"sso_session"`
	AccountID string    `json:"account_id,omitempty"`
	RoleName  string    `json:"role_name,omitempty"`
	Region    string    `json:"region"`
	Endpoint  string    `json:"endpoint"`
	SSORegion string    `json:"sso_region"`
	ExpiresAt time.Time `json:"expires_at"`
}

// ssoLogoutResult is the JSON shape returned on successful SSO logout.
type ssoLogoutResult struct {
	ClearedSession  bool     `json:"cleared_session"`
	ClearedSTSCount int      `json:"cleared_sts_count"`
	ClearedProfiles []string `json:"cleared_profiles"`
}

// ssoAdapterFactory builds an ssoAdapter.
type ssoAdapterFactory func(ctx *Context) (*ssoAdapter, error)

// ErrSSORollbackFailure is the stable sentinel for a login/configure transaction
// that failed AND whose token-cache rollback also failed. errors.Is reaches it
// through ssoRollbackFailureError.Unwrap. Its Error() is fixed and safe.
var ErrSSORollbackFailure = errors.New("sso login failed and token cache rollback failed; run: volclog sso login")

// ssoRollbackFailureError wraps the original business cause and the rollback
// cause with the ErrSSORollbackFailure sentinel. Error() never renders either
// cause (which may contain tokens, paths, or secrets); Unwrap joins the
// sentinel with both causes so errors.Is/As can classify and inspect them.
type ssoRollbackFailureError struct {
	cause    error
	rollback error
}

func (e *ssoRollbackFailureError) Error() string { return ErrSSORollbackFailure.Error() }

func (e *ssoRollbackFailureError) Unwrap() error {
	if e == nil {
		return nil
	}
	return errors.Join(ErrSSORollbackFailure, e.cause, e.rollback)
}

// newSSORollbackFailureError builds a classifiable rollback-failure error.
func newSSORollbackFailureError(cause, rollback error) error {
	return &ssoRollbackFailureError{cause: cause, rollback: rollback}
}

// parseSSOSessionFlags parses configure sso-session flags.
func parseSSOSessionFlags(args []string) (ssoSessionOpts, error) {
	var opts ssoSessionOpts
	var scopesRaw string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "--name":
			if i+1 >= len(args) {
				return opts, errors.New("missing value for --name")
			}
			opts.Name = args[i+1]
			i++
		case "--start-url":
			if i+1 >= len(args) {
				return opts, errors.New("missing value for --start-url")
			}
			opts.StartURL = args[i+1]
			i++
		case "--region":
			if i+1 >= len(args) {
				return opts, errors.New("missing value for --region")
			}
			opts.Region = args[i+1]
			i++
		case "--registration-scopes":
			if i+1 >= len(args) {
				return opts, errors.New("missing value for --registration-scopes")
			}
			scopesRaw = args[i+1]
			opts.scopesExplicit = true
			i++
		case "-h", "--help":
			return opts, &usageError{Text: usageConfigureSSOSession(), ExitCode: 0}
		default:
			if strings.HasPrefix(a, "-") {
				return opts, errors.New("unknown flag: " + a)
			}
			return opts, errors.New("unexpected argument: " + a)
		}
	}
	opts.Name = strings.TrimSpace(opts.Name)
	opts.StartURL = strings.TrimSpace(opts.StartURL)
	opts.Region = strings.TrimSpace(opts.Region)
	// Reject an explicitly empty --registration-scopes value (e.g.
	// --registration-scopes ""). An empty string is not a valid scope list and
	// must not silently clear the session's scopes.
	if opts.scopesExplicit && strings.TrimSpace(scopesRaw) == "" {
		return opts, errors.New("invalid --registration-scopes: empty value; omit the flag to preserve existing scopes")
	}
	if scopesRaw != "" {
		for _, s := range strings.Split(scopesRaw, ",") {
			s = strings.TrimSpace(s)
			// Reject malformed lists: empty elements are not silently dropped.
			if s == "" {
				return opts, errors.New("invalid --registration-scopes: empty scope in list")
			}
			opts.RegistrationScopes = append(opts.RegistrationScopes, s)
		}
	}
	return opts, nil
}

// parseSSOConfigureFlags parses configure sso flags.
func parseSSOConfigureFlags(args []string) (ssoConfigureOpts, error) {
	var opts ssoConfigureOpts
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "--profile":
			if i+1 >= len(args) {
				return opts, errors.New("missing value for --profile")
			}
			opts.Profile = args[i+1]
			i++
		case "--sso-session":
			if i+1 >= len(args) {
				return opts, errors.New("missing value for --sso-session")
			}
			opts.SSOSession = args[i+1]
			i++
		case "--account-id":
			if i+1 >= len(args) {
				return opts, errors.New("missing value for --account-id")
			}
			opts.AccountID = args[i+1]
			i++
		case "--role-name":
			if i+1 >= len(args) {
				return opts, errors.New("missing value for --role-name")
			}
			opts.RoleName = args[i+1]
			i++
		case "--region":
			if i+1 >= len(args) {
				return opts, errors.New("missing value for --region")
			}
			opts.Region = strings.TrimSpace(args[i+1])
			if opts.Region == "" {
				return opts, errors.New("invalid --region: empty value")
			}
			i++
		case "--endpoint":
			if i+1 >= len(args) {
				return opts, errors.New("missing value for --endpoint")
			}
			opts.Endpoint = strings.TrimSpace(args[i+1])
			if opts.Endpoint == "" {
				return opts, errors.New("invalid --endpoint: empty value")
			}
			i++
		case "--no-browser":
			opts.NoBrowser = true
		case "-h", "--help":
			return opts, &usageError{Text: usageConfigureSSO(), ExitCode: 0}
		default:
			if strings.HasPrefix(a, "-") {
				return opts, errors.New("unknown flag: " + a)
			}
			return opts, errors.New("unexpected argument: " + a)
		}
	}
	opts.Profile = strings.TrimSpace(opts.Profile)
	opts.SSOSession = strings.TrimSpace(opts.SSOSession)
	opts.AccountID = strings.TrimSpace(opts.AccountID)
	opts.RoleName = strings.TrimSpace(opts.RoleName)
	opts.Region = strings.TrimSpace(opts.Region)
	opts.Endpoint = strings.TrimSpace(opts.Endpoint)
	return opts, nil
}

// runConfigureSSOSession handles `configure sso-session`.
func runConfigureSSOSession(ctx *Context, args []string) (any, error) {
	opts, err := parseSSOSessionFlags(args)
	if err != nil {
		return nil, err
	}
	if opts.Name == "" {
		return nil, errors.New("missing required field: --name")
	}
	if opts.StartURL == "" {
		return nil, errors.New("missing required field: --start-url")
	}
	if opts.Region == "" {
		return nil, errors.New("missing required field: --region")
	}
	if _, err := sso.CanonicalStartURL(opts.StartURL); err != nil {
		return nil, err
	}
	// scopesProvided tracks whether --registration-scopes was explicitly set.
	// start-url and region are always required on every invocation; only scopes
	// may be omitted to preserve the existing session's value (patch semantics).
	var scopes []string
	if opts.scopesExplicit {
		scopes = make([]string, 0, len(opts.RegistrationScopes))
		for _, s := range opts.RegistrationScopes {
			s = strings.TrimSpace(s)
			if _, ok := allowedSSOScopes[s]; !ok {
				return nil, fmt.Errorf("unknown registration scope: %s", s)
			}
			scopes = append(scopes, s)
		}
		normalized, err := sso.NormalizeScopes(scopes)
		if err != nil {
			return nil, err
		}
		scopes = normalized
	}
	var saved config.SSOSession
	if err := ctx.UpdateConfig(func(latest *config.Config) error {
		existing, ok := latest.SSOSessions[opts.Name]
		if ok {
			existing.Name = opts.Name
			existing.StartURL = opts.StartURL
			existing.Region = opts.Region
			if opts.scopesExplicit {
				existing.RegistrationScopes = scopes
			}
			latest.SSOSessions[opts.Name] = existing
			saved = existing
		} else {
			session := config.SSOSession{
				Name:               opts.Name,
				StartURL:           opts.StartURL,
				Region:             opts.Region,
				RegistrationScopes: scopes,
			}
			if !opts.scopesExplicit {
				session.RegistrationScopes = defaultSSOScopes
			}
			if latest.SSOSessions == nil {
				latest.SSOSessions = map[string]config.SSOSession{}
			}
			latest.SSOSessions[opts.Name] = session
			saved = session
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return map[string]any{
		"name":                saved.Name,
		"start_url":           saved.StartURL,
		"region":              saved.Region,
		"registration_scopes": saved.RegistrationScopes,
	}, nil
}

// runConfigureSSOWithFactory is the testable Run-level entry point for
// `configure sso`. Frozen output options are rejected before the factory is
// built so no login side effect runs when stdout would be diverted.
func runConfigureSSOWithFactory(ctx *Context, args []string, factory ssoAdapterFactory) (any, error) {
	opts, err := parseSSOConfigureFlags(args)
	if err != nil {
		return nil, err
	}
	profile, err := resolveProfileSelector(ctx.Profile, opts.Profile)
	if err != nil {
		return nil, err
	}
	opts.Profile = profile
	if opts.SSOSession == "" {
		return nil, errors.New("missing required field: --sso-session")
	}
	if err := rejectFrozenOutputOptions(ctx); err != nil {
		return nil, err
	}
	if factory == nil {
		return nil, errors.New("nil sso adapter factory")
	}
	adapter, err := factory(ctx)
	if err != nil {
		return nil, err
	}
	if adapter == nil {
		return nil, errors.New("sso adapter factory returned nil adapter")
	}
	result, err := adapter.runConfigureSSO(context.Background(), opts)
	if err != nil {
		return nil, err
	}
	ctx.FormatOverride = output.FormatJSON
	return result, nil
}

// runConfigureSSO binds a profile to an SSO session and completes the first
// device authorization. The entire transaction (snapshot -> login -> binding ->
// config commit) runs inside the token lock; the final token and binding are
// captured inside the lock so the result is assembled from a safe copy without
// re-reading the cache after the lock is released.
func (a *ssoAdapter) runConfigureSSO(ctx context.Context, opts ssoConfigureOpts) (*ssoLoginResult, error) {
	if a == nil {
		return nil, errors.New("nil sso adapter")
	}
	if a.cache == nil {
		return nil, errors.New("nil cache")
	}
	if a.cfgStore == nil {
		return nil, errors.New("nil config store")
	}
	cfg, cfgPath, err := a.cfgStore.Load()
	if err != nil {
		return nil, newSafeCLIError("load config failed", err)
	}
	session, ok := cfg.SSOSessions[opts.SSOSession]
	if !ok {
		return nil, fmt.Errorf("sso session not found: %s", opts.SSOSession)
	}
	profileName := cfg.SelectedProfileName(opts.Profile)
	// Capture the original profile snapshot so a concurrent rebind inside the
	// config lock can be detected and rolled back. Also capture the old SSO
	// binding (session/account/role) so stale STS caches can be cleared when the
	// binding changes, preventing the old Provider fast path from reusing
	// expired dynamic credentials.
	origProfile, origProfileExists := cfg.GetProfile(profileName)
	origSSOSession := strings.TrimSpace(origProfile.SSOSessionName)
	origMode := origProfile.Mode
	origAccountID := strings.TrimSpace(origProfile.AccountID)
	origRoleName := strings.TrimSpace(origProfile.RoleName)

	if a.deviceFlowFn == nil {
		return nil, errors.New("nil device flow factory")
	}
	df, err := a.deviceFlowFn(session, opts.NoBrowser)
	if err != nil {
		return nil, newSafeCLIError("build sso device flow failed", err)
	}
	if df == nil {
		return nil, errors.New("sso device flow factory returned nil")
	}

	if a.bindingFn == nil {
		return nil, errors.New("nil binding service factory")
	}
	bindingSvc, err := a.bindingFn(session)
	if err != nil {
		return nil, newSafeCLIError("build sso binding service failed", err)
	}
	if bindingSvc == nil {
		return nil, errors.New("sso binding service factory returned nil")
	}

	var (
		finalToken   *sso.TokenCache
		finalBinding *sso.BindingResult
		finalExpiry  time.Time
		finalProfile config.Profile
	)

	// For cross-session rebinds, acquire BOTH the old and new session token
	// locks in global digest order so an old Provider cannot re-create the old
	// STS cache after we delete it but before the config commit. Same-session
	// rebinds only need the single target token lock. Locks are ordered by
	// securestore.DigestKey(CanonicalStartURL, sessionName) to avoid A->B /
	// B->A deadlocks; identical keys are deduplicated.
	var lockTargets []struct{ startURL, sessionName string }
	lockTargets = append(lockTargets, struct{ startURL, sessionName string }{session.StartURL, session.Name})
	if origSSOSession != "" && origSSOSession != session.Name {
		if oldSess, ok := cfg.SSOSessions[origSSOSession]; ok {
			lockTargets = append(lockTargets, struct{ startURL, sessionName string }{oldSess.StartURL, origSSOSession})
		}
	}
	err = a.withTokenLocksOrdered(ctx, lockTargets, func() error {
		// Re-read the latest session inside the lock and confirm the DeviceFlow
		// snapshot still matches; a concurrent configure sso-session could have
		// changed Name/StartURL/Region/Scopes between our lock-free load and now.
		latestCfg, _, lerr := a.cfgStore.Load()
		if lerr != nil {
			return newSafeCLIError("reload config failed", lerr)
		}
		latestSession, lok := latestCfg.SSOSessions[opts.SSOSession]
		if !lok {
			return errors.New("sso session removed during login; aborting")
		}
		if latestSession.Name != session.Name ||
			latestSession.StartURL != session.StartURL ||
			latestSession.Region != session.Region ||
			!equalStringSlice(latestSession.RegistrationScopes, session.RegistrationScopes) {
			return errors.New("sso session changed during login; aborting")
		}

		oldToken, oldExisted, rerr := a.readTokenSnapshot(session.StartURL, session.Name)
		if rerr != nil {
			return rerr
		}
		newToken, lerr := df.Login(ctx)
		if lerr != nil {
			return a.restoreTokenOnFailure(session.StartURL, session.Name, oldToken, oldExisted, lerr)
		}
		if newToken == nil {
			return a.restoreTokenOnFailure(session.StartURL, session.Name, oldToken, oldExisted, errors.New("login returned nil token"))
		}
		// The real DeviceFlow persists the token itself; capture a copy so the
		// result is built from the exact snapshot we committed, not a later
		// re-read that could observe a concurrent logout/refresh.
		tokenCopy := *newToken

		// Validate token expiry BEFORE any binding/config commit. An invalid
		// expires_at must restore both the token and leave the profile untouched.
		expiresAt, perr := time.Parse(time.RFC3339, tokenCopy.ExpiresAt)
		if perr != nil {
			return a.restoreTokenOnFailure(session.StartURL, session.Name, oldToken, oldExisted, fmt.Errorf("token has invalid expires_at: %w", perr))
		}

		var bind *sso.BindingResult
		bind, berr := bindingSvc.ResolveBinding(ctx, tokenCopy.AccessToken, opts.AccountID, opts.RoleName)
		if berr != nil {
			return a.restoreTokenOnFailure(session.StartURL, session.Name, oldToken, oldExisted, berr)
		}
		if bind == nil {
			return a.restoreTokenOnFailure(session.StartURL, session.Name, oldToken, oldExisted, errors.New("binding service returned nil result"))
		}

		// If the profile was previously bound to a different SSO session or a
		// different account/role, clear the old STS cache so the old Provider
		// fast path cannot reuse stale dynamic credentials. The frozen lock
		// order token -> STS -> config is preserved: the STS lock is acquired
		// (still inside the token lock) and held across the config update so a
		// concurrent Provider cannot recreate the old STS cache between the
		// delete and the profile rebind.
		oldBindingChanged := origSSOSession != "" &&
			(origSSOSession != session.Name || origAccountID != bind.AccountID || origRoleName != bind.RoleName) &&
			origAccountID != "" && origRoleName != ""

		commitConfig := func() error {
			// Returns the raw config failure WITHOUT restoring the token. The
			// caller (non-rebind or rebind path) is responsible for exactly one
			// compensation call so the token is never restored twice.
			if _, cerr := a.cfgStore.Update(cfgPath, func(c *config.Config) error {
				// Re-verify the session snapshot inside the config lock: a
				// concurrent configure sso-session could have changed
				// Name/StartURL/Region/Scopes between the token-lock check and now.
				curSession, csok := c.SSOSessions[opts.SSOSession]
				if !csok {
					return errors.New("sso session removed during login; aborting")
				}
				if curSession.Name != session.Name ||
					curSession.StartURL != session.StartURL ||
					curSession.Region != session.Region ||
					!equalStringSlice(curSession.RegistrationScopes, session.RegistrationScopes) {
					return errors.New("sso session changed during login; aborting")
				}
				// Re-verify the profile has not been concurrently rebound to a
				// different session/mode. If it has, fail closed and let the
				// caller restore the old token so the profile is never silently
				// switched.
				curProfile, cpok := c.GetProfile(profileName)
				if origProfileExists {
					if !cpok {
						return errors.New("profile removed during login; aborting")
					}
					if strings.TrimSpace(curProfile.SSOSessionName) != origSSOSession || curProfile.Mode != origMode {
						return errors.New("profile rebind during login; aborting")
					}
				} else if cpok {
					return errors.New("profile created during login; aborting")
				}
				return c.PatchProfile(profileName, func(p *config.Profile) error {
					p.Mode = config.AuthModeSSO
					p.SSOSessionName = session.Name
					p.AccountID = bind.AccountID
					p.RoleName = bind.RoleName
					p.LoginSession = ""
					p.STSExpiration = 0
					if opts.Region != "" {
						p.Region = opts.Region
					}
					if opts.Endpoint != "" {
						p.Endpoint = opts.Endpoint
					}
					finalProfile = *p
					return nil
				})
			}); cerr != nil {
				return cerr
			}
			return nil
		}

		if oldBindingChanged {
			if serr := a.cache.WithSTSLock(ctx, origSSOSession, origAccountID, origRoleName, func() error {
				// Snapshot the old STS cache before deletion so a subsequent
				// config failure can restore it, preserving the "old binding +
				// cache still usable" invariant.
				oldSTS, oldSTSExisted, rerr := a.readSTSSnapshot(origSSOSession, origAccountID, origRoleName)
				if rerr != nil {
					return a.restoreTokenOnFailure(session.StartURL, session.Name, oldToken, oldExisted, rerr)
				}
				// Delete the old STS cache. ErrMissing is idempotent (no-op).
				if derr := a.cache.DeleteSTS(origSSOSession, origAccountID, origRoleName); derr != nil && !errors.Is(derr, securestore.ErrMissing) {
					return a.restoreTokenOnFailure(session.StartURL, session.Name, oldToken, oldExisted, newSafeCLIError("delete old sso sts cache failed", derr))
				}
				// Commit the new binding. If this fails, the unified
				// restoreTokenAndSTSOnFailure restores BOTH the old token and
				// the old STS cache exactly once.
				if cerr := commitConfig(); cerr != nil {
					return a.restoreTokenAndSTSOnFailure(session.StartURL, session.Name, oldToken, oldExisted, oldSTS, oldSTSExisted, cerr)
				}
				return nil
			}); serr != nil {
				return serr
			}
		} else {
			// No rebind: restore token exactly once on config failure.
			if cerr := commitConfig(); cerr != nil {
				return a.restoreTokenOnFailure(session.StartURL, session.Name, oldToken, oldExisted, cerr)
			}
		}

		finalToken = &tokenCopy
		finalBinding = bind
		finalExpiry = expiresAt
		return nil
	})
	if err != nil {
		return nil, err
	}
	if finalToken == nil {
		return nil, errors.New("sso login produced no token")
	}
	acctID := opts.AccountID
	roleName := opts.RoleName
	if finalBinding != nil {
		acctID = finalBinding.AccountID
		roleName = finalBinding.RoleName
	}
	return &ssoLoginResult{
		Profile:   profileName,
		Provider:  "sso",
		Session:   session.Name,
		AccountID: acctID,
		RoleName:  roleName,
		Region:    finalProfile.Region,
		Endpoint:  finalProfile.Endpoint,
		SSORegion: session.Region,
		ExpiresAt: finalExpiry,
	}, nil
}

// withTokenLocksOrdered acquires the token lock for each target in the order of
// their securestore digest (CanonicalStartURL + sessionName), deduplicating
// identical keys, then runs fn while all are held. Locks are released in
// reverse order as the stack unwinds. The global digest ordering prevents
// A->B / B->A deadlocks when two rebinds cross the same pair of sessions.
func (a *ssoAdapter) withTokenLocksOrdered(ctx context.Context, targets []struct{ startURL, sessionName string }, fn func() error) error {
	// Deduplicate by lock key and sort by digest ascending.
	type keyed struct {
		startURL, sessionName string
		digest                string
	}
	var ks []keyed
	seen := map[string]struct{}{}
	for _, t := range targets {
		canonical, err := sso.CanonicalStartURL(t.startURL)
		if err != nil {
			return err
		}
		d := securestore.DigestKey(canonical, t.sessionName)
		if _, ok := seen[d]; ok {
			continue
		}
		seen[d] = struct{}{}
		ks = append(ks, keyed{t.startURL, t.sessionName, d})
	}
	sort.Slice(ks, func(i, j int) bool { return ks[i].digest < ks[j].digest })

	var run func(idx int) error
	run = func(idx int) error {
		if idx >= len(ks) {
			return fn()
		}
		return a.cache.WithTokenLock(ctx, ks[idx].startURL, ks[idx].sessionName, func() error {
			return run(idx + 1)
		})
	}
	return run(0)
}

// readTokenSnapshot reads the current token inside the lock, distinguishing
// "missing" (oldExisted=false, token=nil) from a real read error.
func (a *ssoAdapter) readTokenSnapshot(startURL, sessionName string) (*sso.TokenCache, bool, error) {
	tok, rerr := a.cache.ReadToken(startURL, sessionName)
	switch {
	case rerr == nil:
		return tok, true, nil
	case errors.Is(rerr, securestore.ErrMissing):
		return nil, false, nil
	default:
		return nil, false, newSafeCLIError("read sso token cache failed", rerr)
	}
}

// restoreTokenOnFailure restores the old token cache snapshot after a failed
// login/config transaction. When the old token existed, the current token is
// read first: if it already matches the oldToken snapshot exactly, no restore
// is needed and the original business error is returned without touching disk.
// If restore is needed but WriteToken fails, ErrSSORollbackFailure is returned
// and the current disk state is preserved (DeleteToken is NEVER called when
// oldExisted=true, so the old token is never accidentally deleted). When the
// old token was missing the new token is deleted; on delete failure a single
// best-effort retry is attempted. Any rollback failure is classified via
// ErrSSORollbackFailure so callers can detect it; the original cause is
// preserved through Unwrap but never rendered in Error().
func (a *ssoAdapter) restoreTokenOnFailure(startURL, sessionName string, oldToken *sso.TokenCache, oldExisted bool, cause error) error {
	if oldExisted && oldToken != nil {
		// Read the current token to see if restore is even necessary.
		cur, rerr := a.cache.ReadToken(startURL, sessionName)
		if rerr == nil && cur != nil && tokensEqual(cur, oldToken) {
			// Current token already matches the old snapshot; nothing to do.
			return newSafeCLIError("sso login failed", cause)
		}
		// Restore needed. If WriteToken fails, preserve the current disk state
		// (do NOT delete — the old token may still be intact).
		if werr := a.cache.WriteToken(oldToken); werr != nil {
			return newSSORollbackFailureError(cause, werr)
		}
		return newSafeCLIError("sso login failed", cause)
	}
	// Old token missing: delete the new token the device flow just wrote.
	if derr := a.cache.DeleteToken(startURL, sessionName); derr != nil && !errors.Is(derr, securestore.ErrMissing) {
		// One explicit, safe best-effort retry; do not pretend success.
		if derr2 := a.cache.DeleteToken(startURL, sessionName); derr2 != nil && !errors.Is(derr2, securestore.ErrMissing) {
			return newSSORollbackFailureError(cause, derr)
		}
	}
	return newSafeCLIError("sso login failed", cause)
}

// tokensEqual reports whether two token caches are both nil or have identical
// field values.
func tokensEqual(a, b *sso.TokenCache) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// readSTSSnapshot reads the current STS cache inside the lock, distinguishing
// "missing" (existed=false, cache=nil) from a real read error.
func (a *ssoAdapter) readSTSSnapshot(sessionName, accountID, roleName string) (*sso.STSCache, bool, error) {
	sts, rerr := a.cache.ReadSTS(sessionName, accountID, roleName)
	switch {
	case rerr == nil:
		return sts, true, nil
	case errors.Is(rerr, securestore.ErrMissing):
		return nil, false, nil
	default:
		return nil, false, newSafeCLIError("read sso sts cache failed", rerr)
	}
}

// restoreTokenAndSTSOnFailure restores both the old token cache and the old STS
// cache after a failed configure-sso rebind transaction. It first restores the
// token (via restoreTokenOnFailure), then restores the STS snapshot. If the STS
// restore also fails, the combined failure is classified via
// ErrSSORollbackFailure so callers can detect it; neither cause is rendered in
// Error(). The old STS is only rewritten when it previously existed.
func (a *ssoAdapter) restoreTokenAndSTSOnFailure(startURL, sessionName string, oldToken *sso.TokenCache, oldExisted bool, oldSTS *sso.STSCache, oldSTSExisted bool, cause error) error {
	// Restore the token first; this returns a classified error if token restore
	// fails, but we still attempt the STS restore below to maximize recovery.
	tokenErr := a.restoreTokenOnFailure(startURL, sessionName, oldToken, oldExisted, cause)
	if !oldSTSExisted || oldSTS == nil {
		return tokenErr
	}
	if werr := a.cache.WriteSTS(oldSTS); werr != nil {
		// STS restore failed: join with the token-restore result so the error
		// chain preserves both causes. If token restore also failed, this is a
		// double rollback failure.
		if tokenErr != nil && errors.Is(tokenErr, ErrSSORollbackFailure) {
			return newSSORollbackFailureError(cause, errors.Join(tokenErr, werr))
		}
		return newSSORollbackFailureError(cause, werr)
	}
	return tokenErr
}

// newAccountSelector builds a numbered account selector that prints the
// non-secret account list to stderr and reads a 1-based index from stdin.
func newAccountSelector(stdin io.Reader, stderr io.Writer) sso.AccountSelector {
	return func(accounts []sso.AccountInfo) (sso.AccountInfo, error) {
		return selectNumbered("account", stdin, stderr, accounts, func(a sso.AccountInfo) string {
			if a.AccountName != "" {
				return fmt.Sprintf("%s (%s)", a.AccountID, a.AccountName)
			}
			return a.AccountID
		}, func(i int) sso.AccountInfo { return accounts[i] })
	}
}

// newRoleSelector builds a numbered role selector.
func newRoleSelector(stdin io.Reader, stderr io.Writer) sso.RoleSelector {
	return func(roles []sso.RoleInfo) (sso.RoleInfo, error) {
		return selectNumbered("role", stdin, stderr, roles, func(r sso.RoleInfo) string {
			return r.RoleName
		}, func(i int) sso.RoleInfo { return roles[i] })
	}
}

// selectNumbered is the shared numbered-selection primitive. It writes the
// prompt to stderr (never stdout), reads a single line from stdin via
// readConfirmLine (which enforces a length cap and fails on EOF/overflow), and
// accepts only a 1-based integer in range. The returned error is fixed and safe:
// it never echoes the user's input or any token material.
func selectNumbered[T any](label string, stdin io.Reader, stderr io.Writer, items []T, describe func(T) string, pick func(int) T) (T, error) {
	var zero T
	if len(items) == 0 {
		return zero, fmt.Errorf("no %s available", label)
	}
	if len(items) == 1 {
		return pick(0), nil
	}
	for i, it := range items {
		if _, err := fmt.Fprintf(stderr, "  [%d] %s\n", i+1, describe(it)); err != nil {
			return zero, newSafeCLIError("cannot write selection prompt", err)
		}
	}
	if _, err := fmt.Fprintf(stderr, "Select %s [1-%d]: ", label, len(items)); err != nil {
		return zero, newSafeCLIError("cannot write selection prompt", err)
	}
	line, err := readConfirmLine(stdin)
	if err != nil {
		return zero, newSafeCLIError("cannot read selection", err)
	}
	line = strings.TrimSpace(line)
	n, perr := strconv.Atoi(line)
	if perr != nil || n < 1 || n > len(items) {
		return zero, fmt.Errorf("invalid %s selection", label)
	}
	return pick(n - 1), nil
}

// equalStringSlice reports whether two string slices are element-wise equal.
func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// newProductionSSOAdapter assembles the adapter with production dependencies.
// The OAuth/Portal clients are NOT built here: they are constructed lazily
// inside the device-flow / binding / revoker factories using the target
// session's region, so a non-default-region session never talks to the
// cn-beijing endpoint.
func newProductionSSOAdapter(ctx *Context) (*ssoAdapter, error) {
	// Resolve the cache directory through the same shared resolver used by the
	// dynamic provider factory, so login writes and TLS/Doctor/configure reads
	// target the same root.
	cfgPath, err := config.DefaultConfigPath()
	if err != nil {
		return nil, fmt.Errorf("resolve config path: %w", err)
	}
	cacheDir := resolveSSOCacheDir(cfgPath)
	cache, err := sso.NewFileCache(cacheDir)
	if err != nil {
		return nil, fmt.Errorf("create sso file cache: %w", err)
	}
	return &ssoAdapter{
		cache:    cache,
		cfgStore: productionConfigStore{},
		deviceFlowFn: func(session config.SSOSession, noBrowser bool) (ssoDeviceFlow, error) {
			oauthClient, oerr := sso.NewOAuthClient(&sso.OAuthClientConfig{Region: session.Region})
			if oerr != nil {
				return nil, fmt.Errorf("create sso oauth client: %w", oerr)
			}
			return sso.NewDeviceFlow(&sso.DeviceFlowConfig{
				OAuth:       oauthClient,
				Cache:       cache,
				Clock:       time.Now,
				Browser:     &browser.DefaultOpener{},
				Progress:    ctx.Stderr,
				NoBrowser:   noBrowser,
				StartURL:    session.StartURL,
				SessionName: session.Name,
				Region:      session.Region,
				Scopes:      session.RegistrationScopes,
			}), nil
		},
		bindingFn: func(session config.SSOSession) (ssoBindingService, error) {
			portalClient, perr := sso.NewPortalClient(&sso.PortalClientConfig{Region: session.Region})
			if perr != nil {
				return nil, fmt.Errorf("create sso portal client: %w", perr)
			}
			return sso.NewBindingService(&sso.BindingServiceConfig{
				Portal:        portalClient,
				SelectAccount: newAccountSelector(os.Stdin, ctx.Stderr),
				SelectRole:    newRoleSelector(os.Stdin, ctx.Stderr),
			}), nil
		},
		revokerFn: func(region string) (ssoOAuthRevoker, error) {
			return sso.NewOAuthClient(&sso.OAuthClientConfig{Region: region})
		},
		stdin:  os.Stdin,
		stdout: ctx.Stdout,
		stderr: ctx.Stderr,
		clock:  time.Now,
	}, nil
}

// Compile-time assertions that production types satisfy the local interfaces.
var (
	_ ssoCache        = (*sso.FileCache)(nil)
	_ configStore     = productionConfigStore{}
	_ ssoOAuthRevoker = (*sso.OAuthClient)(nil)
)
