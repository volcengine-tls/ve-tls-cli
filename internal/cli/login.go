package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth/browser"
	"github.com/volcengine-tls/ve-tls-cli/internal/auth/console"
	"github.com/volcengine-tls/ve-tls-cli/internal/config"
	"github.com/volcengine-tls/ve-tls-cli/internal/output"
)

// loginOpts mirrors console.LoginOptions but is local to the CLI package so the
// adapter interface does not leak the console package type into tests.
type loginOpts struct {
	Profile  string
	Region   string
	Endpoint string
	Remote   bool
}

// loginResult is the redacted result of a successful console login. It is the
// only shape written to stdout; it never contains the full AK, SK, session
// token, OAuth tokens, or login session id.
type loginResult struct {
	Profile         string    `json:"profile"`
	Provider        string    `json:"provider"`
	Region          string    `json:"region"`
	Endpoint        string    `json:"endpoint"`
	ExpiresAt       time.Time `json:"expires_at"`
	MaskedAccessKey string    `json:"masked_access_key"`
}

// loginService is the minimal interface the CLI adapter uses to run a console
// login. Production wraps *console.LoginService; tests inject a fake.
type loginService interface {
	Login(ctx context.Context, opts loginOpts) (*loginResult, error)
}

// consoleCache is the minimal interface for the per-session cache lock and
// deletion used by logout. Production wraps *console.FileCache; tests inject a
// fake. WithLock must acquire the exact same lock as console.Provider.Retrieve
// so logout and refresh are serialized.
type consoleCache interface {
	WithLock(ctx context.Context, loginSession string, fn func() error) error
	Delete(loginSession string) error
}

// configStore is the minimal interface for loading and atomically updating the
// config used by login/logout. Production wraps config.Load/config.Update;
// tests inject a fake.
type configStore interface {
	Load() (config.Config, string, error)
	Update(path string, fn func(*config.Config) error) (config.Config, error)
}

// loginAdapter holds the injectable dependencies for the login/logout commands.
// Run-level functions construct it with production defaults; tests construct it
// directly with fakes. No dependency is a process-level global.
type loginAdapter struct {
	loginSvc loginService
	cache    consoleCache
	cfgStore configStore
	stdin    io.Reader
	stdout   io.Writer
	stderr   io.Writer
}

// ErrLogoutPartialFailure is returned when logout deleted the cache but the
// config update that clears profile bindings failed. The cache stays deleted
// (fail closed); callers can detect this with errors.Is to surface a
// classifiable partial failure without rolling back AK/SK or reviving the cache.
var ErrLogoutPartialFailure = errors.New("logout partial failure: cache deleted but config binding update failed")

// logoutPartialFailureError wraps ErrLogoutPartialFailure with the underlying
// config error so errors.Is/As can classify it while preserving the cause.
type logoutPartialFailureError struct {
	cause error
}

func (e *logoutPartialFailureError) Error() string {
	return ErrLogoutPartialFailure.Error()
}

func (e *logoutPartialFailureError) Unwrap() error {
	if e == nil {
		return nil
	}
	return errors.Join(ErrLogoutPartialFailure, e.cause)
}

// newLogoutPartialFailureError builds a classifiable partial-failure error.
func newLogoutPartialFailureError(cause error) error {
	return &logoutPartialFailureError{cause: cause}
}

// safeCLIError wraps a cause with a fixed, safe description. Its Error() never
// renders the underlying cause text (which may contain sessions, paths, or
// secrets), but Unwrap preserves the cause so errors.Is/errors.As still match.
// This mirrors console.safeError for the CLI adapter layer.
type safeCLIError struct {
	desc  string
	cause error
}

func (e *safeCLIError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return e.desc
}

func (e *safeCLIError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// newSafeCLIError builds a safeCLIError. A nil cause falls back to a plain
// error with the description.
func newSafeCLIError(desc string, cause error) error {
	if cause == nil {
		return errors.New(desc)
	}
	return &safeCLIError{desc: desc, cause: cause}
}

// resolveProfileSelector resolves the effective profile name from a global
// selector (e.g. ctx.Profile from --profile before the group) and a
// command-level selector (e.g. -p/--profile after the group).
//
//   - only one provided: use it
//   - both provided and equal: allowed
//   - both provided and different: explicit conflict error
//   - neither provided: empty (caller falls back to current then default via
//     config.SelectedProfileName)
func resolveProfileSelector(global, local string) (string, error) {
	global = strings.TrimSpace(global)
	local = strings.TrimSpace(local)
	switch {
	case global != "" && local != "" && global != local:
		return "", fmt.Errorf("conflicting profile selectors: global --profile=%s conflicts with command --profile=%s", global, local)
	case local != "":
		return local, nil
	default:
		return global, nil
	}
}

// parseLoginFlags parses login command flags. It accepts -p/--profile,
// -r/--region, --endpoint, and --remote. Any --secrets-file flag is rejected
// explicitly.
func parseLoginFlags(args []string) (loginOpts, error) {
	var opts loginOpts
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "-p", "--profile":
			if i+1 >= len(args) {
				return opts, errors.New("missing value for " + a)
			}
			opts.Profile = args[i+1]
			i++
		case "-r", "--region":
			if i+1 >= len(args) {
				return opts, errors.New("missing value for " + a)
			}
			opts.Region = strings.TrimSpace(args[i+1])
			if opts.Region == "" {
				return opts, errors.New("invalid --region: empty value")
			}
			i++
		case "--remote":
			opts.Remote = true
		case "--endpoint":
			if i+1 >= len(args) {
				return opts, errors.New("missing value for --endpoint")
			}
			opts.Endpoint = strings.TrimSpace(args[i+1])
			if opts.Endpoint == "" {
				return opts, errors.New("invalid --endpoint: empty value")
			}
			i++
		case "--secrets-file":
			return opts, errors.New("--secrets-file is not supported for login; use --profile to select a dynamic login identity")
		case "-h", "--help":
			return opts, &usageError{Text: usageLogin(), ExitCode: 0}
		default:
			if strings.HasPrefix(a, "-") {
				return opts, errors.New("unknown flag: " + a)
			}
			return opts, errors.New("unexpected argument: " + a)
		}
	}
	return opts, nil
}

// parseLogoutFlags parses logout command flags. It accepts -p/--profile and
// --all. --all and --profile are mutually exclusive. Any --secrets-file flag is
// rejected explicitly.
func parseLogoutFlags(args []string) (profile string, all bool, err error) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "-p", "--profile":
			if i+1 >= len(args) {
				return "", false, errors.New("missing value for " + a)
			}
			profile = args[i+1]
			i++
		case "--all":
			all = true
		case "--secrets-file":
			return "", false, errors.New("--secrets-file is not supported for logout; use --profile to select a dynamic login identity")
		case "-h", "--help":
			return "", false, &usageError{Text: usageLogout(), ExitCode: 0}
		default:
			if strings.HasPrefix(a, "-") {
				return "", false, errors.New("unknown flag: " + a)
			}
			return "", false, errors.New("unexpected argument: " + a)
		}
	}
	if all && strings.TrimSpace(profile) != "" {
		return "", false, errors.New("--all cannot be combined with --profile")
	}
	return profile, all, nil
}

// loginAdapterFactory builds a loginAdapter. Production passes
// newProductionLoginAdapter; tests inject a fake factory so the Run-level
// dispatch, flag parsing, selector resolution, and output freezing can be
// exercised without network access or process-level globals.
type loginAdapterFactory func(ctx *Context) (*loginAdapter, error)

// rejectFrozenOutputOptions rejects global options that would rewrite, divert,
// or wrap the login/logout result after the command runs. login/logout freeze
// stdout to the exact JSON shape of their result; any generic filter/file/trace
// post-processing would either hide the result (file delivery), wrap it in a
// data/meta envelope (trace), or fail after a successful side effect
// (--jmes-filter on a non-envelope value). --secrets-file is also rejected
// because login/logout/sso manage their own dynamic identity and must not be
// handed long-lived static credentials. These are rejected before the adapter
// is constructed so no login/logout/sso side effect runs.
func rejectFrozenOutputOptions(ctx *Context) error {
	if mode := strings.ToLower(strings.TrimSpace(ctx.OutputMode)); mode != "" && mode != "stdout" {
		return errors.New("--output-mode " + mode + " is not supported for login/logout; stdout is the only delivery mode")
	}
	if strings.TrimSpace(ctx.OutputFile) != "" {
		return errors.New("--output-file is not supported for login/logout; result is written to stdout only")
	}
	if strings.TrimSpace(ctx.Filter) != "" {
		return errors.New("--jmes-filter is not supported for login/logout; result is written to stdout only")
	}
	if strings.TrimSpace(ctx.TraceDir) != "" {
		return errors.New("--trace-dir is not supported for login/logout; result is written to stdout only")
	}
	if strings.TrimSpace(ctx.GlobalSecretsFile) != "" {
		return errors.New("--secrets-file is not supported for login/logout/sso; use --profile to select a dynamic login identity")
	}
	return nil
}

// runLoginWithFactory is the testable Run-level login entry point. It parses
// flags, resolves the profile selector, freezes the output surface, builds the
// adapter via factory, and delegates to adapter.runLogin.
func runLoginWithFactory(ctx *Context, args []string, factory loginAdapterFactory) (any, error) {
	opts, err := parseLoginFlags(args)
	if err != nil {
		return nil, err
	}
	profile, err := resolveProfileSelector(ctx.Profile, opts.Profile)
	if err != nil {
		return nil, err
	}
	opts.Profile = profile
	if err := rejectFrozenOutputOptions(ctx); err != nil {
		return nil, err
	}
	adapter, err := factory(ctx)
	if err != nil {
		return nil, err
	}
	result, err := adapter.runLogin(context.Background(), opts)
	if err != nil {
		return nil, err
	}
	// Force JSON output so stdout contains only the final result regardless of
	// the global --output flag.
	ctx.FormatOverride = output.FormatJSON
	return result, nil
}

// runLogoutWithFactory is the testable Run-level logout entry point.
func runLogoutWithFactory(ctx *Context, args []string, factory loginAdapterFactory) (any, error) {
	profile, all, err := parseLogoutFlags(args)
	if err != nil {
		return nil, err
	}
	resolved, err := resolveProfileSelector(ctx.Profile, profile)
	if err != nil {
		return nil, err
	}
	// --all operates over every console-login profile and must not be combined
	// with any profile selector (global --profile or command --profile). This is
	// checked after selector resolution so a global selector is also rejected,
	// and before the production adapter is constructed.
	if all && strings.TrimSpace(resolved) != "" {
		return nil, errors.New("--all cannot be combined with --profile")
	}
	if err := rejectFrozenOutputOptions(ctx); err != nil {
		return nil, err
	}
	adapter, err := factory(ctx)
	if err != nil {
		return nil, err
	}
	result, err := adapter.runLogout(context.Background(), resolved, all)
	if err != nil {
		// On partial failure the helper returns the sessions cleared so far as
		// an internal partial result for caller/test diagnostics. The standard
		// CLI error path (run.go) classifies the error and writes a safe
		// message to stderr only; it does not render this result to stdout, so
		// no session material reaches stdout on the error path.
		if result != nil {
			ctx.FormatOverride = output.FormatJSON
		}
		return result, err
	}
	ctx.FormatOverride = output.FormatJSON
	return result, nil
}

// runLogin executes the login flow and returns the redacted result. All
// prompts and progress go to stderr via the injected authorizer writers.
func (a *loginAdapter) runLogin(ctx context.Context, opts loginOpts) (*loginResult, error) {
	if a == nil {
		return nil, errors.New("nil login adapter")
	}
	if a.loginSvc == nil {
		return nil, errors.New("nil login service")
	}
	result, err := a.loginSvc.Login(ctx, opts)
	if err != nil {
		return nil, newSafeCLIError("console login failed", err)
	}
	if result == nil {
		return nil, errors.New("login returned nil result")
	}
	return result, nil
}

// logoutResult is the JSON shape returned on successful logout. It intentionally
// never contains login-session strings: only the count of cleared sessions and
// the stable-sorted list of affected profile names. This enforces the frozen
// boundary that session material must not appear in command output or errors.
type logoutResult struct {
	ClearedSessionCount int      `json:"cleared_session_count"`
	ClearedProfiles     []string `json:"cleared_profiles"`
}

// runLogout clears console login state. When all is true it iterates every
// known mode=console-login profile; otherwise it operates on the selected
// profile. Profiles are grouped by login-session and processed in stable
// order. Each session is cleared inside the same cache lock used by
// console.Provider.Retrieve: the cache is deleted first, then config.Update
// clears the login-session binding on every matching profile. The cache lock is
// held until config.Update returns, so a concurrent refresh can never revive a
// deleted cache.
func (a *loginAdapter) runLogout(ctx context.Context, profile string, all bool) (*logoutResult, error) {
	if a == nil {
		return nil, errors.New("nil login adapter")
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

	// The initial Load snapshot only determines which sessions to process and
	// validates the explicit target. The actual profile clearing is done inside
	// the cache lock by scanning the latest config in logoutSession, so profiles
	// that appear or change between Load and the lock are handled correctly.
	var sessions []string
	if all {
		seen := map[string]struct{}{}
		for _, p := range cfg.Profiles {
			if p.Mode != config.AuthModeConsoleLogin {
				continue
			}
			s := strings.TrimSpace(p.LoginSession)
			if s == "" {
				continue
			}
			if _, ok := seen[s]; ok {
				continue
			}
			seen[s] = struct{}{}
			sessions = append(sessions, s)
		}
	} else {
		name := cfg.SelectedProfileName(profile)
		p, ok := cfg.GetProfile(name)
		if !ok {
			return nil, fmt.Errorf("profile not found: %s", name)
		}
		if p.Mode != config.AuthModeConsoleLogin {
			return nil, fmt.Errorf("profile %s is not a console-login profile", name)
		}
		s := strings.TrimSpace(p.LoginSession)
		if s == "" {
			// No binding to clear; treat as idempotent success.
			return &logoutResult{ClearedSessionCount: 0, ClearedProfiles: []string{}}, nil
		}
		sessions = []string{s}
	}

	sort.Strings(sessions)

	res := &logoutResult{ClearedSessionCount: 0, ClearedProfiles: []string{}}
	for _, session := range sessions {
		cleared, err := a.logoutSession(ctx, cfgPath, session)
		if err != nil {
			// Cache is already deleted (fail closed). Return the partial result
			// along with the classifiable error.
			return res, err
		}
		res.ClearedSessionCount++
		res.ClearedProfiles = append(res.ClearedProfiles, cleared...)
	}
	sort.Strings(res.ClearedProfiles)
	return res, nil
}

// logoutSession clears a single login-session group. It acquires the same
// per-session cache lock as console.Provider.Retrieve, deletes the cache, and
// then (still inside the lock) scans the LATEST config for every profile bound
// to this session and clears the login-session binding. The lock is not
// released until config.Update returns, so a concurrent refresh cannot recreate
// the cache between deletion and the config patch. Scanning the latest config
// (rather than the Load snapshot) ensures profiles that appeared or changed
// mode/session after Load are handled correctly: only profiles that are still
// mode=console-login and bound to this session are cleared and reported.
func (a *loginAdapter) logoutSession(ctx context.Context, cfgPath, session string) ([]string, error) {
	var cleared []string
	err := a.cache.WithLock(ctx, session, func() error {
		if err := a.cache.Delete(session); err != nil {
			return newSafeCLIError("delete console login cache failed", err)
		}
		_, err := a.cfgStore.Update(cfgPath, func(c *config.Config) error {
			// Scan the latest config's full profile set inside the lock. Only
			// console-login profiles still bound to this session are cleared;
			// AK/SK, default-chain, SSO, or profiles that changed session/mode
			// after Load are left untouched and not reported.
			for name, p := range c.Profiles {
				if p.Mode != config.AuthModeConsoleLogin {
					continue
				}
				if strings.TrimSpace(p.LoginSession) != session {
					continue
				}
				p.LoginSession = ""
				c.PutProfile(name, p)
				cleared = append(cleared, name)
			}
			return nil
		})
		if err != nil {
			// Cache is already deleted; surface a classifiable partial failure
			// that preserves the cause via errors.Is/As without rendering it.
			return newLogoutPartialFailureError(err)
		}
		return nil
	})
	if err != nil {
		// If the callback already produced a classifiable partial failure,
		// return it as-is so errors.Is(err, ErrLogoutPartialFailure) penetrates
		// to classifyError.
		if errors.Is(err, ErrLogoutPartialFailure) {
			return cleared, err
		}
		// If the callback already produced a *safeCLIError (e.g. a delete
		// failure), return it as-is so the accurate description is preserved
		// and not relabeled as a lock-acquisition error. errors.Is/As still
		// reach the original cause via Unwrap.
		var safeErr *safeCLIError
		if errors.As(err, &safeErr) {
			return cleared, err
		}
		// Only genuine lock-acquisition failures (which never delete the cache)
		// are wrapped as the safe lock error.
		return cleared, newSafeCLIError("logout failed to acquire cache lock", err)
	}
	sort.Strings(cleared)
	return cleared, nil
}

// newConfirmPrompt builds a confirmation callback suitable for
// console.LoginServiceConfig.Confirm. It writes the prompt (which may include
// the profile name but never the current or new login session) to stderr and
// reads a single line from stdin. Only "y" or "yes" (case-insensitive, trimmed)
// is accepted; anything else (including empty input) is treated as a denial so
// the default is safe. A read error or EOF returns a safe error so the login
// fails closed rather than silently replacing a bound session.
func newConfirmPrompt(stdin io.Reader, stderr io.Writer) func(profileName, currentSession, newSession string) (bool, error) {
	return func(profileName, _, _ string) (bool, error) {
		prompt := fmt.Sprintf("Profile %q is bound to a different login session. Replace it? [y/N]: ", profileName)
		if _, err := fmt.Fprint(stderr, prompt); err != nil {
			// Wrap the writer cause with a fixed, safe description so the
			// user-facing text never renders the underlying error (which may
			// contain paths or session material), while Unwrap preserves the
			// cause for errors.Is/errors.As classification.
			return false, newSafeCLIError("login session confirmation failed: cannot write prompt", err)
		}
		line, err := readConfirmLine(stdin)
		if err != nil {
			// Wrap the cause so errors.Is/As can classify it (e.g. detect
			// errConfirmLineTooLong) while keeping the user-facing description
			// fixed and free of input or session material.
			return false, newSafeCLIError("login session confirmation failed: cannot read response", err)
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "y", "yes":
			return true, nil
		default:
			return false, nil
		}
	}
}

// maxConfirmLineLength caps the number of bytes read from stdin for a login
// session confirmation. A runaway input stream (no newline) must not grow the
// read buffer without bound; this is a hard safety limit on interactive input.
const maxConfirmLineLength = 4096

// errConfirmLineTooLong is returned by readConfirmLine when the input line
// exceeds maxConfirmLineLength bytes before a newline. It is a fixed, safe
// sentinel: it never contains the user's input or any session material, and it
// forces the confirmation to fail closed rather than silently accepting a
// truncated prefix.
var errConfirmLineTooLong = errors.New("confirmation response exceeds maximum length")

// readConfirmLine reads a single line from r. It returns the line without the
// trailing newline. An empty reader (EOF before any data) returns an error so
// callers can distinguish "user typed nothing" from "input stream closed".
// Input is capped at maxConfirmLineLength bytes: a line that exceeds the limit
// before a newline returns errConfirmLineTooLong so the caller fails closed
// instead of acting on a truncated prefix.
func readConfirmLine(r io.Reader) (string, error) {
	var buf []byte
	b := make([]byte, 1)
	for {
		n, err := r.Read(b)
		if n > 0 {
			if b[0] == '\n' {
				return string(buf), nil
			}
			if len(buf) >= maxConfirmLineLength {
				// Fail closed: do not return a truncated prefix that could be
				// TrimSpace'd into an unintended confirmation. The overflow is
				// not drained because the login aborts immediately.
				return "", errConfirmLineTooLong
			}
			buf = append(buf, b[0])
		}
		if err != nil {
			if err == io.EOF {
				if len(buf) > 0 {
					return string(buf), nil
				}
				return "", err
			}
			return "", err
		}
	}
}

// newProductionLoginAdapter assembles the adapter with production dependencies:
// the real console.LoginService, FileCache, config store, browser opener, and
// os.Stdin for remote login.
func newProductionLoginAdapter(ctx *Context) (*loginAdapter, error) {
	// Resolve the cache directory through the same shared resolver used by the
	// dynamic provider factory, so login writes and TLS/Doctor/configure reads
	// target the same root. The actual config path is derived the same way the
	// CLI resolves it at startup (VOLCLOG_CONFIG or ~/.volclog/config.json).
	cfgPath, err := config.DefaultConfigPath()
	if err != nil {
		return nil, fmt.Errorf("resolve config path: %w", err)
	}
	cacheDir := resolveLoginCacheDir(cfgPath)
	cache, err := console.NewFileCache(cacheDir)
	if err != nil {
		return nil, fmt.Errorf("create file cache: %w", err)
	}
	svc := console.NewLoginService(&console.LoginServiceConfig{
		Cache:        cache,
		ProfileStore: productionConfigStore{},
		Confirm:      newConfirmPrompt(os.Stdin, ctx.Stderr),
		LocalAuthorizer: func(client console.OAuthClient, state, codeChallenge string) console.Authorizer {
			return console.NewDefaultLocalAuthorizer(client, &browser.DefaultOpener{}, ctx.Stderr, state, codeChallenge)
		},
		RemoteAuthorizer: func(client console.OAuthClient, state, codeChallenge string) console.Authorizer {
			return console.NewRemoteAuthorizer(client, os.Stdin, ctx.Stderr, state, codeChallenge)
		},
	})
	return &loginAdapter{
		loginSvc: &consoleLoginServiceAdapter{svc: svc},
		cache:    cache,
		cfgStore: productionConfigStore{},
		stdin:    os.Stdin,
		stdout:   ctx.Stdout,
		stderr:   ctx.Stderr,
	}, nil
}

// productionConfigStore wraps config.Load/config.Update as a configStore.
type productionConfigStore struct{}

func (productionConfigStore) Load() (config.Config, string, error) {
	return config.Load()
}

func (productionConfigStore) Update(path string, fn func(*config.Config) error) (config.Config, error) {
	return config.Update(path, fn)
}

// consoleLoginService is the minimal interface the adapter uses to run a console
// login. Production passes *console.LoginService; tests inject a fake that
// returns console.LoginResult directly so the option/result translation in
// consoleLoginServiceAdapter.Login is exercised without network access.
type consoleLoginService interface {
	Login(ctx context.Context, opts console.LoginOptions) (*console.LoginResult, error)
}

// consoleLoginServiceAdapter adapts consoleLoginService to the local
// loginService interface, translating between local and console option/result
// types.
type consoleLoginServiceAdapter struct {
	svc consoleLoginService
}

func (a *consoleLoginServiceAdapter) Login(ctx context.Context, opts loginOpts) (*loginResult, error) {
	res, err := a.svc.Login(ctx, console.LoginOptions{
		Profile:  opts.Profile,
		Region:   opts.Region,
		Endpoint: opts.Endpoint,
		Remote:   opts.Remote,
	})
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, errors.New("login returned nil result")
	}
	return &loginResult{
		Profile:         res.Profile,
		Provider:        res.Provider,
		Region:          res.Region,
		Endpoint:        res.Endpoint,
		ExpiresAt:       res.ExpiresAt,
		MaskedAccessKey: res.MaskedAccessKey,
	}, nil
}

// Compile-time assertions that production types satisfy the local interfaces.
var (
	_ configStore         = productionConfigStore{}
	_ consoleCache        = (*console.FileCache)(nil)
	_ consoleLoginService = (*console.LoginService)(nil)
)
