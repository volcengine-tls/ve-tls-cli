package cli

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth/sso"
	"github.com/volcengine-tls/ve-tls-cli/internal/config"
	"github.com/volcengine-tls/ve-tls-cli/internal/output"
	"github.com/volcengine-tls/ve-tls-cli/internal/securestore"
)

// ssoLoginOpts holds the parsed flags for sso login.
type ssoLoginOpts struct {
	Profile    string
	SSOSession string
	NoBrowser  bool
}

// ssoLogoutOpts holds the parsed flags for sso logout.
type ssoLogoutOpts struct {
	Profile    string
	SSOSession string
}

// runSSOGroup dispatches `sso login` and `sso logout` subcommands.
func runSSOGroup(ctx *Context, args []string, factory ssoAdapterFactory) (any, error) {
	if len(args) == 0 {
		return nil, &usageError{Text: usageSSO(), ExitCode: 1}
	}
	if args[0] == "-h" || args[0] == "--help" {
		return nil, &usageError{Text: usageSSO(), ExitCode: 0}
	}
	switch args[0] {
	case "login":
		return runSSOLoginWithFactory(ctx, args[1:], factory)
	case "logout":
		return runSSOLogoutWithFactory(ctx, args[1:], factory)
	default:
		return nil, errors.New("unknown sso command: " + args[0])
	}
}

// parseSSOLoginFlags parses sso login flags.
func parseSSOLoginFlags(args []string) (ssoLoginOpts, error) {
	var opts ssoLoginOpts
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
		case "--no-browser":
			opts.NoBrowser = true
		case "-h", "--help":
			return opts, &usageError{Text: usageSSOLogin(), ExitCode: 0}
		default:
			if strings.HasPrefix(a, "-") {
				return opts, errors.New("unknown flag: " + a)
			}
			return opts, errors.New("unexpected argument: " + a)
		}
	}
	opts.Profile = strings.TrimSpace(opts.Profile)
	opts.SSOSession = strings.TrimSpace(opts.SSOSession)
	return opts, nil
}

// parseSSOLogoutFlags parses sso logout flags. Both --profile and --sso-session
// are accepted (intentional fix of upstream parser deviation).
func parseSSOLogoutFlags(args []string) (ssoLogoutOpts, error) {
	var opts ssoLogoutOpts
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
		case "-h", "--help":
			return opts, &usageError{Text: usageSSOLogout(), ExitCode: 0}
		default:
			if strings.HasPrefix(a, "-") {
				return opts, errors.New("unknown flag: " + a)
			}
			return opts, errors.New("unexpected argument: " + a)
		}
	}
	opts.Profile = strings.TrimSpace(opts.Profile)
	opts.SSOSession = strings.TrimSpace(opts.SSOSession)
	return opts, nil
}

// runSSOLoginWithFactory is the testable Run-level entry point for `sso login`.
func runSSOLoginWithFactory(ctx *Context, args []string, factory ssoAdapterFactory) (any, error) {
	opts, err := parseSSOLoginFlags(args)
	if err != nil {
		return nil, err
	}
	// Merge global --profile with command --profile before the mutual-exclusion
	// check so a global selector combined with --sso-session is also rejected.
	profile, err := resolveProfileSelector(ctx.Profile, opts.Profile)
	if err != nil {
		return nil, err
	}
	opts.Profile = profile
	if opts.Profile != "" && opts.SSOSession != "" {
		return nil, errors.New("--profile and --sso-session cannot be combined; use exactly one selector")
	}
	// When neither selector is provided, default to the current profile (resolved
	// inside the adapter via SelectedProfileName("")). This matches the existing
	// selector rule used by login/logout.
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
	result, err := adapter.runSSOLogin(context.Background(), opts)
	if err != nil {
		return nil, err
	}
	ctx.FormatOverride = output.FormatJSON
	return result, nil
}

// runSSOLogin re-authorizes an SSO session. When --profile is set, the session
// is read from the profile; when --sso-session is set, the session is logged in
// directly without modifying any profile. The token-lock transaction mirrors
// runConfigureSSO: snapshot -> login -> restore on failure, with the final
// token captured inside the lock.
func (a *ssoAdapter) runSSOLogin(ctx context.Context, opts ssoLoginOpts) (*ssoLoginResult, error) {
	if a == nil {
		return nil, errors.New("nil sso adapter")
	}
	if a.cache == nil {
		return nil, errors.New("nil cache")
	}
	if a.cfgStore == nil {
		return nil, errors.New("nil config store")
	}
	cfg, _, err := a.cfgStore.Load()
	if err != nil {
		return nil, newSafeCLIError("load config failed", err)
	}

	var sessionName, startURL, ssoRegion, profileName string
	var origSession config.SSOSession
	var origProfileSSOSession string
	if opts.SSOSession == "" {
		// No --sso-session: resolve via profile (defaults to current profile
		// when --profile is also omitted, per existing selector rules).
		profileName = cfg.SelectedProfileName(opts.Profile)
		p, ok := cfg.GetProfile(profileName)
		if !ok {
			return nil, fmt.Errorf("profile not found: %s", profileName)
		}
		if p.Mode != "" && p.Mode != config.AuthModeSSO {
			return nil, fmt.Errorf("profile %s is not an sso profile", profileName)
		}
		sessionName = strings.TrimSpace(p.SSOSessionName)
		if sessionName == "" {
			return nil, fmt.Errorf("profile %s has no sso-session binding", profileName)
		}
		sess, ok := cfg.SSOSessions[sessionName]
		if !ok {
			return nil, fmt.Errorf("sso session not found: %s", sessionName)
		}
		startURL = sess.StartURL
		ssoRegion = sess.Region
		origSession = sess
		origProfileSSOSession = sessionName
	} else {
		sessionName = opts.SSOSession
		sess, ok := cfg.SSOSessions[sessionName]
		if !ok {
			return nil, fmt.Errorf("sso session not found: %s", sessionName)
		}
		startURL = sess.StartURL
		ssoRegion = sess.Region
		origSession = sess
	}

	if a.deviceFlowFn == nil {
		return nil, errors.New("nil device flow factory")
	}
	df, err := a.deviceFlowFn(cfg.SSOSessions[sessionName], opts.NoBrowser)
	if err != nil {
		return nil, newSafeCLIError("build sso device flow failed", err)
	}
	if df == nil {
		return nil, errors.New("sso device flow factory returned nil")
	}

	var (
		finalToken       *sso.TokenCache
		finalExpiry      time.Time
		finalTLSRegion   string
		finalTLSEndpoint string
	)
	err = a.cache.WithTokenLock(ctx, startURL, sessionName, func() error {
		oldToken, oldExisted, rerr := a.readTokenSnapshot(startURL, sessionName)
		if rerr != nil {
			return rerr
		}
		newToken, lerr := df.Login(ctx)
		if lerr != nil {
			return a.restoreTokenOnFailure(startURL, sessionName, oldToken, oldExisted, lerr)
		}
		if newToken == nil {
			return a.restoreTokenOnFailure(startURL, sessionName, oldToken, oldExisted, errors.New("login returned nil token"))
		}
		// Re-validate the full snapshot inside the token lock after DeviceFlow.
		// A concurrent configure sso-session could have drifted the session
		// (Name/StartURL/Region/Scopes) or a concurrent configure sso could have
		// rebound the profile. Either way, fail closed and restore the old token
		// so a new-key token is never mixed with stale config.
		latestCfg, _, lerr := a.cfgStore.Load()
		if lerr != nil {
			return a.restoreTokenOnFailure(startURL, sessionName, oldToken, oldExisted, newSafeCLIError("reload config failed", lerr))
		}
		curSession, csok := latestCfg.SSOSessions[sessionName]
		if !csok {
			return a.restoreTokenOnFailure(startURL, sessionName, oldToken, oldExisted, errors.New("sso session removed during login; aborting"))
		}
		if curSession.Name != origSession.Name ||
			curSession.StartURL != origSession.StartURL ||
			curSession.Region != origSession.Region ||
			!equalStringSlice(curSession.RegistrationScopes, origSession.RegistrationScopes) {
			return a.restoreTokenOnFailure(startURL, sessionName, oldToken, oldExisted, errors.New("sso session changed during login; aborting"))
		}
		if profileName != "" {
			curProfile, cpok := latestCfg.GetProfile(profileName)
			if !cpok {
				return a.restoreTokenOnFailure(startURL, sessionName, oldToken, oldExisted, errors.New("profile removed during login; aborting"))
			}
			if strings.TrimSpace(curProfile.SSOSessionName) != origProfileSSOSession {
				return a.restoreTokenOnFailure(startURL, sessionName, oldToken, oldExisted, errors.New("profile rebind during login; aborting"))
			}
			finalTLSRegion = curProfile.Region
			finalTLSEndpoint = curProfile.Endpoint
		}
		tokenCopy := *newToken
		expiresAt, perr := time.Parse(time.RFC3339, tokenCopy.ExpiresAt)
		if perr != nil {
			return a.restoreTokenOnFailure(startURL, sessionName, oldToken, oldExisted, fmt.Errorf("token has invalid expires_at: %w", perr))
		}
		finalToken = &tokenCopy
		finalExpiry = expiresAt
		return nil
	})
	if err != nil {
		return nil, err
	}
	if finalToken == nil {
		return nil, errors.New("sso login produced no token")
	}
	return &ssoLoginResult{
		Profile:   profileName,
		Provider:  "sso",
		Session:   sessionName,
		Region:    finalTLSRegion,
		Endpoint:  finalTLSEndpoint,
		SSORegion: ssoRegion,
		ExpiresAt: finalExpiry,
	}, nil
}

// runSSOLogoutWithFactory is the testable Run-level entry point for `sso logout`.
func runSSOLogoutWithFactory(ctx *Context, args []string, factory ssoAdapterFactory) (any, error) {
	opts, err := parseSSOLogoutFlags(args)
	if err != nil {
		return nil, err
	}
	// Merge global --profile with command --profile; if both global and command
	// selectors are set and differ, resolveProfileSelector rejects. Then a
	// combined --profile (global or local) plus --sso-session is rejected before
	// any factory/side effect runs.
	profile, err := resolveProfileSelector(ctx.Profile, opts.Profile)
	if err != nil {
		return nil, err
	}
	opts.Profile = profile
	if opts.Profile != "" && opts.SSOSession != "" {
		return nil, errors.New("--profile and --sso-session cannot be combined; use exactly one selector")
	}
	// When neither selector is provided, default to the current profile (resolved
	// inside the adapter via SelectedProfileName("")). This matches the existing
	// selector rule used by login/logout.
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
	result, err := adapter.runSSOLogout(context.Background(), opts)
	if err != nil {
		return result, err
	}
	ctx.FormatOverride = output.FormatJSON
	return result, nil
}

// stsBindingKey identifies a unique STS cache by session/account/role.
type stsBindingKey struct {
	sessionName string
	accountID   string
	roleName    string
}

// runSSOLogout clears SSO state for a session.
//
// Frozen algorithm:
//  1. Outside the lock, resolve the selector to a session name + start URL +
//     region (enough to locate the token lock).
//  2. Acquire the token lock.
//  3. Inside the token lock, reload the latest config; re-resolve the session
//     and all linked profiles + STS keys. If the session's StartURL/Region
//     drifted from the lock-free snapshot, fail closed (do not use stale keys).
//  4. Deduplicate STS keys by securestore.DigestKey and sort ascending.
//  5. Acquire all STS locks nested (token -> STS[0] -> STS[1] -> ...). No state
//     is mutated before the innermost callback runs.
//  6. Inside all locks: ReadToken (ErrMissing is idempotent; other errors are
//     not silently ignored); if a refresh token exists, best-effort revoke via
//     the session-region revoker; delete the token and all STS caches;
//     config.Update clears sts-expiration on every profile still bound to the
//     session (binding/TLS/dormant fields are preserved).
//  7. Release STS locks in reverse order, then the token lock.
//  8. If an STS lock cannot be acquired, no token/STS/config state has changed.
func (a *ssoAdapter) runSSOLogout(ctx context.Context, opts ssoLogoutOpts) (*ssoLogoutResult, error) {
	if a == nil {
		return nil, errors.New("nil sso adapter")
	}
	if a.cache == nil {
		return nil, errors.New("nil cache")
	}
	if a.cfgStore == nil {
		return nil, errors.New("nil config store")
	}
	cfg, _, err := a.cfgStore.Load()
	if err != nil {
		return nil, newSafeCLIError("load config failed", err)
	}

	// Step 1: resolve the session outside the lock only to locate the token lock.
	var sessionName, startURL, region string
	var profileName string
	if opts.SSOSession == "" {
		// No --sso-session: resolve via profile (defaults to current profile
		// when --profile is also omitted, per existing selector rules).
		profileName = cfg.SelectedProfileName(opts.Profile)
		p, ok := cfg.GetProfile(profileName)
		if !ok {
			return &ssoLogoutResult{}, nil
		}
		sessionName = strings.TrimSpace(p.SSOSessionName)
		if sessionName == "" {
			return &ssoLogoutResult{}, nil
		}
	} else {
		sessionName = opts.SSOSession
	}
	session, ok := cfg.SSOSessions[sessionName]
	if !ok {
		return &ssoLogoutResult{}, nil
	}
	startURL = session.StartURL
	region = session.Region

	res := &ssoLogoutResult{ClearedSession: false, ClearedSTSCount: 0, ClearedProfiles: []string{}}

	// Step 2: acquire the token lock, then STS locks (sorted), then perform all
	// mutations — session re-check, token delete, STS deletes, profile
	// sts-expiration patch — atomically inside a single config.Update lock. The
	// held STS lock key set is verified against the latest profiles inside the
	// config lock so we never delete a key we do not hold, and each unique key is
	// deleted exactly once. Revoke runs only after the final validation passes.
	err = a.cache.WithTokenLock(ctx, startURL, sessionName, func() error {
		// Reload config inside the token lock to get the current cfgPath and
		// re-confirm the session still exists before acquiring STS locks.
		latestCfg, cfgPath, lerr := a.cfgStore.Load()
		if lerr != nil {
			return newSafeCLIError("reload config failed", lerr)
		}
		latestSession, lok := latestCfg.SSOSessions[sessionName]
		if !lok {
			return nil
		}
		if latestSession.StartURL != startURL || latestSession.Region != region {
			return errors.New("sso session changed during logout; rerun to retry")
		}
		if profileName != "" {
			curProfile, cpok := latestCfg.GetProfile(profileName)
			if !cpok {
				return errors.New("profile removed during logout; rerun to retry")
			}
			if strings.TrimSpace(curProfile.SSOSessionName) != sessionName {
				return errors.New("profile rebind during logout; rerun to retry")
			}
		}

		// Collect STS keys from the latest config, sorted by digest ascending.
		// The digest set is captured so the config.Update callback can verify the
		// held locks still match the latest profiles (no new/rebound keys are
		// deleted without a lock, no key is deleted twice).
		seen := map[string]struct{}{}
		var keys []stsBindingKey
		var heldDigests []string
		for _, p := range latestCfg.Profiles {
			if strings.TrimSpace(p.SSOSessionName) != sessionName {
				continue
			}
			acct := strings.TrimSpace(p.AccountID)
			role := strings.TrimSpace(p.RoleName)
			if acct == "" || role == "" {
				continue
			}
			digest := securestore.DigestKey(sessionName, acct, role)
			if _, ok := seen[digest]; ok {
				continue
			}
			seen[digest] = struct{}{}
			keys = append(keys, stsBindingKey{sessionName: sessionName, accountID: acct, roleName: role})
			heldDigests = append(heldDigests, digest)
		}
		sort.Slice(keys, func(i, j int) bool {
			return securestore.DigestKey(keys[i].sessionName, keys[i].accountID, keys[i].roleName) <
				securestore.DigestKey(keys[j].sessionName, keys[j].accountID, keys[j].roleName)
		})
		sort.Strings(heldDigests)

		// Acquire all STS locks, then run the atomic config.Update that re-checks
		// the session and performs all local mutations inside the config lock.
		return a.withAllSTSLocks(ctx, keys, func() error {
			// Atomic config update: re-verify the full session snapshot and the
			// held STS key set, then delete token + each unique STS key once and
			// patch profiles. If the session drifted or the key set changed
			// (new/rebound profiles), return an error WITHOUT deleting anything.
			var localDeleteErrs []error
			var configErr error
			var clearedProfiles []string
			var revokeTok *sso.TokenCache
			_, configErr = a.cfgStore.Update(cfgPath, func(c *config.Config) error {
				curSession, csok := c.SSOSessions[sessionName]
				if !csok {
					return errors.New("sso session removed during logout; aborting")
				}
				if curSession.Name != latestSession.Name ||
					curSession.StartURL != latestSession.StartURL ||
					curSession.Region != latestSession.Region ||
					!equalStringSlice(curSession.RegistrationScopes, latestSession.RegistrationScopes) {
					return errors.New("sso session changed during logout; aborting")
				}
				// Re-validate the --profile selector (if used) against the latest
				// config so a concurrent rebind cannot cause us to clear the wrong
				// session's state.
				if profileName != "" {
					cp, cok := c.GetProfile(profileName)
					if !cok {
						return errors.New("profile removed during logout; aborting")
					}
					if strings.TrimSpace(cp.SSOSessionName) != sessionName {
						return errors.New("profile rebind during logout; aborting")
					}
				}
				// Derive the latest STS key set from the latest profiles and
				// verify it exactly matches the held lock set. If a profile was
				// added/rebound with a new key, we would be deleting without a
				// lock — abort instead.
				latestSeen := map[string]struct{}{}
				var latestDigests []string
				for _, p := range c.Profiles {
					if strings.TrimSpace(p.SSOSessionName) != sessionName {
						continue
					}
					acct := strings.TrimSpace(p.AccountID)
					role := strings.TrimSpace(p.RoleName)
					if acct == "" || role == "" {
						continue
					}
					d := securestore.DigestKey(sessionName, acct, role)
					if _, ok := latestSeen[d]; ok {
						continue
					}
					latestSeen[d] = struct{}{}
					latestDigests = append(latestDigests, d)
				}
				sort.Strings(latestDigests)
				if !equalStringSlice(latestDigests, heldDigests) {
					return errors.New("sso sts key set changed during logout; aborting")
				}
				// Capture the token for best-effort revoke BEFORE deleting it, so
				// revoke can run after the config update while the token is no
				// longer on disk. A non-missing read error aborts the callback
				// without deleting anything.
				t, rerr := a.cache.ReadToken(curSession.StartURL, sessionName)
				if rerr != nil && !errors.Is(rerr, securestore.ErrMissing) {
					return newSafeCLIError("read sso token cache failed", rerr)
				}
				revokeTok = t
				// Delete the token cache.
				if derr := a.cache.DeleteToken(curSession.StartURL, sessionName); derr != nil && !errors.Is(derr, securestore.ErrMissing) {
					localDeleteErrs = append(localDeleteErrs, derr)
				} else {
					res.ClearedSession = true
				}
				// Delete each unique held STS key exactly once, then patch all
				// currently-associated profiles.
				for _, k := range keys {
					if derr := a.cache.DeleteSTS(k.sessionName, k.accountID, k.roleName); derr != nil && !errors.Is(derr, securestore.ErrMissing) {
						localDeleteErrs = append(localDeleteErrs, derr)
					} else {
						res.ClearedSTSCount++
					}
				}
				for name, p := range c.Profiles {
					if strings.TrimSpace(p.SSOSessionName) != sessionName {
						continue
					}
					clearedProfiles = append(clearedProfiles, name)
					p.STSExpiration = 0
					c.PutProfile(name, p)
				}
				return nil
			})
			if configErr == nil {
				sort.Strings(clearedProfiles)
				res.ClearedProfiles = clearedProfiles
			}
			localDeleteErr := errors.Join(localDeleteErrs...)

			// Best-effort remote revoke runs AFTER the final validation, while
			// still holding token + STS locks. revokeTok is non-nil only if the
			// config callback passed validation and read the token (before any
			// drift abort). If the config write later fails, the token was still
			// deleted on disk, so we still attempt revoke. On drift, revokeTok
			// stays nil and revoke is never attempted.
			var revokeErr error
			if revokeTok != nil && strings.TrimSpace(revokeTok.RefreshToken) != "" {
				if a.revokerFn != nil {
					revoker, rverr := a.revokerFn(latestSession.Region)
					if rverr != nil {
						revokeErr = rverr
					} else if revoker == nil {
						revokeErr = errors.New("nil sso revoker")
					} else {
						revokeErr = revoker.RevokeToken(ctx, &sso.RevokeTokenRequest{
							ClientID:     revokeTok.ClientID,
							ClientSecret: revokeTok.ClientSecret,
							Token:        revokeTok.RefreshToken,
						})
					}
				} else {
					revokeErr = errors.New("sso revoker factory not configured")
				}
			}

			// Aggregate all errors; classify by most significant kind.
			anyCleared := res.ClearedSession || res.ClearedSTSCount > 0 || len(res.ClearedProfiles) > 0
			var errs []error
			if localDeleteErr != nil {
				errs = append(errs, localDeleteErr)
			}
			if configErr != nil {
				errs = append(errs, configErr)
			}
			if revokeErr != nil {
				errs = append(errs, revokeErr)
			}
			if len(errs) == 0 {
				return nil
			}
			joined := errors.Join(errs...)
			if anyCleared {
				kind := ssoPartialRevoke
				if localDeleteErr != nil {
					kind = ssoPartialLocalDelete
				}
				if configErr != nil {
					kind = ssoPartialConfigUpdate
				}
				return newSSOLogoutPartialFailureError(joined, kind)
			}
			return newSafeCLIError("sso logout failed", joined)
		})
	})
	if err != nil {
		return res, err
	}
	return res, nil
}

// withAllSTSLocks acquires every STS lock in the order given (nested) and runs
// fn once all are held. Locks are released in reverse order as the stack
// unwinds. If any acquisition fails, fn is not invoked and no state has been
// mutated by this helper.
func (a *ssoAdapter) withAllSTSLocks(ctx context.Context, keys []stsBindingKey, fn func() error) error {
	if len(keys) == 0 {
		return fn()
	}
	var run func(idx int) error
	run = func(idx int) error {
		k := keys[idx]
		return a.cache.WithSTSLock(ctx, k.sessionName, k.accountID, k.roleName, func() error {
			if idx+1 < len(keys) {
				return run(idx + 1)
			}
			return fn()
		})
	}
	return run(0)
}

// ssoPartialKind classifies an SSO logout partial failure.
type ssoPartialKind int

const (
	// ssoPartialRevoke means local state was cleared but the remote refresh
	// token revoke failed.
	ssoPartialRevoke ssoPartialKind = iota
	// ssoPartialConfigUpdate means local caches were cleared but the config
	// metadata update (sts-expiration) failed.
	ssoPartialConfigUpdate
	// ssoPartialLocalDelete means some local cache deletion succeeded but a
	// subsequent token or STS delete failed.
	ssoPartialLocalDelete
)

// ErrSSOLogoutPartialFailure is the stable sentinel for an SSO logout that
// cleared local state but failed a remote or config step. errors.Is reaches it
// through ssoLogoutPartialFailureError.Unwrap.
var ErrSSOLogoutPartialFailure = errors.New("sso logout partial failure: local state cleared but remote/config step failed")

// ssoLogoutPartialFailureError wraps the cause with the sentinel and records the
// partial kind. Error() is fixed and safe (never renders the cause); Unwrap
// joins the sentinel with the cause so errors.Is/As can classify it.
type ssoLogoutPartialFailureError struct {
	cause error
	kind  ssoPartialKind
}

func (e *ssoLogoutPartialFailureError) Error() string { return ErrSSOLogoutPartialFailure.Error() }

func (e *ssoLogoutPartialFailureError) Unwrap() error {
	if e == nil {
		return nil
	}
	return errors.Join(ErrSSOLogoutPartialFailure, e.cause)
}

// newSSOLogoutPartialFailureError builds a classifiable SSO partial-failure error.
func newSSOLogoutPartialFailureError(cause error, kind ssoPartialKind) error {
	return &ssoLogoutPartialFailureError{cause: cause, kind: kind}
}
