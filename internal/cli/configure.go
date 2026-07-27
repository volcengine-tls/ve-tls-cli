package cli

import (
	"context"
	"errors"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/config"
)

func runConfigure(ctx *Context, args []string, ssoFactory ssoAdapterFactory) (any, error) {
	if err := ctx.LoadConfig(); err != nil {
		return nil, err
	}
	return runSubcommandGroup(args, usageConfigure(), nil, nil, func(command string, commandArgs []string) (any, error) {
		switch command {
		case "set":
			return configureSet(ctx, commandArgs)
		case "use":
			return configureUse(ctx, commandArgs)
		case "show":
			return configureShow(ctx, commandArgs)
		case "list":
			return configureList(ctx, commandArgs)
		case "delete":
			return configureDelete(ctx, commandArgs)
		case "profile":
			return runConfigureProfile(ctx, commandArgs)
		case "cred":
			return runConfigureCred(ctx, commandArgs)
		case "project":
			return runConfigureProject(ctx, commandArgs)
		case "sso-session":
			return runConfigureSSOSession(ctx, commandArgs)
		case "sso":
			factory := ssoFactory
			if factory == nil {
				factory = newProductionSSOAdapter
			}
			return runConfigureSSOWithFactory(ctx, commandArgs, factory)
		default:
			return nil, errors.New("unknown configure command: " + command)
		}
	})
}

func runConfigureProfile(ctx *Context, args []string) (any, error) {
	if len(args) == 0 {
		return nil, &usageError{Text: usageConfigure(), ExitCode: 1}
	}
	if args[0] == "-h" || args[0] == "--help" {
		return nil, &usageError{Text: usageConfigure(), ExitCode: 0}
	}
	switch args[0] {
	case "add":
		if len(args) < 2 {
			return nil, errors.New("missing profile name")
		}
		mapped := make([]string, 0, len(args)+1)
		mapped = append(mapped, "--profile", args[1])
		mapped = append(mapped, args[2:]...)
		return configureSet(ctx, mapped)
	case "use":
		if len(args) < 2 {
			return nil, errors.New("missing profile name")
		}
		return configureUse(ctx, []string{args[1]})
	case "show":
		if len(args) >= 2 && strings.TrimSpace(args[1]) != "" && !strings.HasPrefix(args[1], "-") {
			return configureShow(ctx, []string{"--profile", args[1]})
		}
		return configureShow(ctx, args[1:])
	case "list":
		return configureList(ctx, args[1:])
	case "delete":
		if len(args) >= 2 && strings.TrimSpace(args[1]) != "" && !strings.HasPrefix(args[1], "-") {
			return configureDelete(ctx, []string{args[1]})
		}
		return configureDelete(ctx, args[1:])
	default:
		return nil, errors.New("unknown configure profile command: " + args[0])
	}
}

func runConfigureCred(ctx *Context, args []string) (any, error) {
	if len(args) == 0 {
		return nil, &usageError{Text: usageConfigure(), ExitCode: 1}
	}
	if args[0] == "-h" || args[0] == "--help" {
		return nil, &usageError{Text: usageConfigure(), ExitCode: 0}
	}
	switch args[0] {
	case "delete":
		return configureCredDelete(ctx, args[1:])
	default:
		return nil, errors.New("unknown configure cred command: " + args[0])
	}
}

func configureSet(ctx *Context, args []string) (any, error) {
	if !hasExplicitModeFlag(args) {
		return configureSetLegacy(ctx, args)
	}
	return configureSetExplicitMode(ctx, args)
}

// hasExplicitModeFlag reports whether --mode (or --mode=<value>) appears as a
// flag in args, respecting flag/value positions so that a profile or cred-ref
// name literally equal to "--mode" does not select the explicit path.
func hasExplicitModeFlag(args []string) bool {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--mode" || strings.HasPrefix(a, "--mode=") {
			return true
		}
		// Flags that consume a following value: skip the value so a value equal
		// to "--mode" is not mistaken for the flag.
		switch a {
		case "--profile", "--ak", "--sk", "--token", "--region", "--endpoint",
			"--timeout-seconds", "--cred-ref", "--account-id", "--role-name",
			"--oidc-token-file", "--role-trn":
			i++
		case "--disable-ssl":
			if i+1 < len(args) && (args[i+1] == "true" || args[i+1] == "false") {
				i++
			}
		}
	}
	return false
}

// configureSetLegacy is the original static AK/SK configuration path. It is
// invoked when --mode is omitted and must preserve the historical behavior
// exactly: it always sets the profile mode to ak, requires inline AK/SK or a
// cred-ref, and overwrites the standard profile fields on every invocation.
func configureSetLegacy(ctx *Context, args []string) (any, error) {
	var (
		name     string
		ak       string
		sk       string
		token    string
		region   string
		endpoint string
		timeout  int
		credRef  string
	)
	for len(args) > 0 {
		switch args[0] {
		case "--profile":
			if len(args) < 2 {
				return nil, errors.New("missing --profile value")
			}
			name = args[1]
			args = args[2:]
		case "--ak":
			if len(args) < 2 {
				return nil, errors.New("missing --ak value")
			}
			ak = args[1]
			args = args[2:]
		case "--sk":
			if len(args) < 2 {
				return nil, errors.New("missing --sk value")
			}
			sk = args[1]
			args = args[2:]
		case "--token":
			if len(args) < 2 {
				return nil, errors.New("missing --token value")
			}
			token = args[1]
			args = args[2:]
		case "--region":
			if len(args) < 2 {
				return nil, errors.New("missing --region value")
			}
			region = args[1]
			args = args[2:]
		case "--endpoint":
			if len(args) < 2 {
				return nil, errors.New("missing --endpoint value")
			}
			endpoint = args[1]
			args = args[2:]
		case "--timeout-seconds":
			if len(args) < 2 {
				return nil, errors.New("missing --timeout-seconds value")
			}
			v, err := strconv.Atoi(args[1])
			if err != nil {
				return nil, err
			}
			timeout = v
			args = args[2:]
		case "--cred-ref":
			if len(args) < 2 {
				return nil, errors.New("missing --cred-ref value")
			}
			credRef = args[1]
			args = args[2:]
		default:
			return nil, errors.New("unknown flag: " + args[0])
		}
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "default"
	}
	p := config.Profile{
		AccessKeyID:     strings.TrimSpace(ak),
		SecretAccessKey: strings.TrimSpace(sk),
		SecurityToken:   strings.TrimSpace(token),
		Region:          strings.TrimSpace(region),
		Endpoint:        strings.TrimSpace(endpoint),
		TimeoutSeconds:  timeout,
		CredRef:         strings.TrimSpace(credRef),
		Mode:            config.AuthModeAK,
	}
	var maskedAK string
	if err := ctx.UpdateConfig(func(latest *config.Config) error {
		candidate := p
		maskedAK = config.MaskAK(candidate.AccessKeyID)
		credPresent := candidate.AccessKeyID != "" && candidate.SecretAccessKey != ""
		if strings.TrimSpace(candidate.CredRef) != "" {
			credName := strings.TrimSpace(candidate.CredRef)
			if credPresent {
				latest.PutCred(credName, config.Credential{
					AccessKeyID:     candidate.AccessKeyID,
					SecretAccessKey: candidate.SecretAccessKey,
				})
			}
			cred, ok := latest.GetCred(credName)
			if !ok {
				return errors.New("credential not found: " + credName)
			}
			maskedAK = config.MaskAK(cred.AccessKeyID)
			candidate.AccessKeyID = ""
			candidate.SecretAccessKey = ""
		}
		if strings.TrimSpace(candidate.CredRef) == "" &&
			(candidate.AccessKeyID == "" || candidate.SecretAccessKey == "") {
			return errors.New("missing required fields: --ak --sk (or --cred-ref <name>)")
		}
		if candidate.Endpoint == "" {
			return errors.New("missing required fields: --endpoint")
		}
		if candidate.Region == "" {
			return errors.New("missing required fields: --region")
		}
		if err := latest.PatchProfile(name, func(existing *config.Profile) error {
			existing.AccessKeyID = candidate.AccessKeyID
			existing.SecretAccessKey = candidate.SecretAccessKey
			existing.SecurityToken = candidate.SecurityToken
			existing.Region = candidate.Region
			existing.Endpoint = candidate.Endpoint
			existing.TimeoutSeconds = candidate.TimeoutSeconds
			existing.CredRef = candidate.CredRef
			existing.Mode = candidate.Mode
			p = *existing
			return nil
		}); err != nil {
			return err
		}
		if latest.CurrentProfile == "" {
			latest.CurrentProfile = name
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return map[string]any{
		"profile":       name,
		"region":        p.Region,
		"endpoint":      p.Endpoint,
		"cred_ref":      strings.TrimSpace(p.CredRef),
		"access_key_id": maskedAK,
	}, nil
}

// configureSetExplicitMode handles `configure set` when --mode is supplied. It
// loads the existing profile, patches only the flags explicitly provided on the
// command line (preserving dormant fields across mode switches), resolves and
// stores cred-refs with the same security guarantees as the legacy path, then
// validates the merged profile against the selected mode's requirements.
func configureSetExplicitMode(ctx *Context, args []string) (any, error) {
	var (
		name       string
		ak         string
		sk         string
		token      string
		region     string
		endpoint   string
		timeout    int
		credRef    string
		mode       string
		accountID  string
		roleName   string
		oidcToken  string
		roleTRN    string
		disableSSL *bool
	)
	set := map[string]bool{}
	for len(args) > 0 {
		switch args[0] {
		case "--profile":
			if len(args) < 2 {
				return nil, errors.New("missing --profile value")
			}
			name = args[1]
			set["profile"] = true
			args = args[2:]
		case "--ak":
			if len(args) < 2 {
				return nil, errors.New("missing --ak value")
			}
			ak = args[1]
			set["ak"] = true
			args = args[2:]
		case "--sk":
			if len(args) < 2 {
				return nil, errors.New("missing --sk value")
			}
			sk = args[1]
			set["sk"] = true
			args = args[2:]
		case "--token":
			if len(args) < 2 {
				return nil, errors.New("missing --token value")
			}
			token = args[1]
			set["token"] = true
			args = args[2:]
		case "--region":
			if len(args) < 2 {
				return nil, errors.New("missing --region value")
			}
			region = args[1]
			set["region"] = true
			args = args[2:]
		case "--endpoint":
			if len(args) < 2 {
				return nil, errors.New("missing --endpoint value")
			}
			endpoint = args[1]
			set["endpoint"] = true
			args = args[2:]
		case "--timeout-seconds":
			if len(args) < 2 {
				return nil, errors.New("missing --timeout-seconds value")
			}
			v, err := strconv.Atoi(args[1])
			if err != nil {
				return nil, err
			}
			timeout = v
			set["timeout"] = true
			args = args[2:]
		case "--cred-ref":
			if len(args) < 2 {
				return nil, errors.New("missing --cred-ref value")
			}
			credRef = args[1]
			set["cred-ref"] = true
			args = args[2:]
		case "--mode":
			if len(args) < 2 {
				return nil, errors.New("missing --mode value")
			}
			mode = args[1]
			set["mode"] = true
			args = args[2:]
		case "--account-id":
			if len(args) < 2 {
				return nil, errors.New("missing --account-id value")
			}
			accountID = args[1]
			set["account-id"] = true
			args = args[2:]
		case "--role-name":
			if len(args) < 2 {
				return nil, errors.New("missing --role-name value")
			}
			roleName = args[1]
			set["role-name"] = true
			args = args[2:]
		case "--oidc-token-file":
			if len(args) < 2 {
				return nil, errors.New("missing --oidc-token-file value")
			}
			oidcToken = args[1]
			set["oidc-token-file"] = true
			args = args[2:]
		case "--role-trn":
			if len(args) < 2 {
				return nil, errors.New("missing --role-trn value")
			}
			roleTRN = args[1]
			set["role-trn"] = true
			args = args[2:]
		case "--disable-ssl":
			// Tri-state: --disable-ssl (true), --disable-ssl=true, --disable-ssl=false.
			// An explicit false must be distinguishable from omission so it can
			// override a previously stored true value.
			if len(args) >= 2 && (args[1] == "true" || args[1] == "false") {
				v := args[1] == "true"
				disableSSL = &v
				args = args[2:]
			} else {
				v := true
				disableSSL = &v
				args = args[1:]
			}
			set["disable-ssl"] = true
		default:
			// Support the single-token form --disable-ssl=true/false.
			if strings.HasPrefix(args[0], "--disable-ssl=") {
				val := strings.TrimPrefix(args[0], "--disable-ssl=")
				if val == "true" || val == "false" {
					v := val == "true"
					disableSSL = &v
					set["disable-ssl"] = true
					args = args[1:]
					continue
				}
			}
			// Support the single-token form --mode=<value>.
			if strings.HasPrefix(args[0], "--mode=") {
				mode = strings.TrimPrefix(args[0], "--mode=")
				set["mode"] = true
				args = args[1:]
				continue
			}
			return nil, errors.New("unknown flag: " + args[0])
		}
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "default"
	}

	// An explicit --mode must carry a non-empty value. Empty (e.g. --mode= or
	// --mode '') is rejected rather than silently normalized to ak.
	if set["mode"] && strings.TrimSpace(mode) == "" {
		return nil, errors.New("missing --mode value")
	}

	normalized, err := config.NormalizeAuthMode(mode)
	if err != nil {
		return nil, err
	}
	if normalized == config.AuthModeSSO || normalized == config.AuthModeConsoleLogin {
		return nil, errors.New("mode " + normalized + " is configured via its dedicated flow; use 'volclog configure sso' or 'volclog login' instead")
	}

	var (
		maskedAK  string
		finalProf config.Profile
	)
	if err := ctx.UpdateConfig(func(latest *config.Config) error {
		existing, _ := latest.GetProfile(name)

		// Patch only the flags explicitly supplied on this invocation so that
		// dormant fields from a previous mode survive a mode switch.
		if set["ak"] {
			existing.AccessKeyID = strings.TrimSpace(ak)
		}
		if set["sk"] {
			existing.SecretAccessKey = strings.TrimSpace(sk)
		}
		if set["token"] {
			existing.SecurityToken = strings.TrimSpace(token)
		}
		if set["region"] {
			existing.Region = strings.TrimSpace(region)
		}
		if set["endpoint"] {
			existing.Endpoint = strings.TrimSpace(endpoint)
		}
		if set["timeout"] {
			existing.TimeoutSeconds = timeout
		}
		if set["cred-ref"] {
			existing.CredRef = strings.TrimSpace(credRef)
		}
		if set["mode"] {
			existing.Mode = normalized
		}
		if set["account-id"] {
			existing.AccountID = strings.TrimSpace(accountID)
		}
		if set["role-name"] {
			existing.RoleName = strings.TrimSpace(roleName)
		}
		if set["oidc-token-file"] {
			existing.OIDCTokenFile = strings.TrimSpace(oidcToken)
		}
		if set["role-trn"] {
			existing.RoleTRN = strings.TrimSpace(roleTRN)
		}
		if set["disable-ssl"] && disableSSL != nil {
			existing.DisableSSL = *disableSSL
		}

		// cred-ref resolution only applies to ak and ramrolearn modes, which
		// source static credentials from the shared store. For oidc/ecsrole the
		// cred-ref (if any) is dormant and must not be resolved, validated, or
		// mutated: a missing or broken dormant cred-ref must not block the
		// update. When --ak/--sk are supplied alongside a cred-ref in ak/ramrolearn,
		// merge the supplied parts with the stored credential, persist the merged
		// AK/SK, clear the inline fields, and mask the stored AK.
		if (normalized == config.AuthModeAK || normalized == config.AuthModeRamRoleARN) &&
			strings.TrimSpace(existing.CredRef) != "" {
			credName := strings.TrimSpace(existing.CredRef)
			stored, hasStored := latest.GetCred(credName)

			mergedAK := stored.AccessKeyID
			mergedSK := stored.SecretAccessKey
			if set["ak"] {
				mergedAK = strings.TrimSpace(ak)
			}
			if set["sk"] {
				mergedSK = strings.TrimSpace(sk)
			}

			switch {
			case hasStored:
				// Existing credential: the merged result must have a non-empty
				// AK and SK. Validate before PutCred so a partial/empty merge
				// never corrupts the store (also catches pre-existing broken
				// credentials).
				if strings.TrimSpace(mergedAK) == "" {
					return errors.New("credential " + credName + " has empty access key id; supply --ak or fix the stored credential")
				}
				if strings.TrimSpace(mergedSK) == "" {
					return errors.New("credential " + credName + " has empty secret access key; supply --sk or fix the stored credential")
				}
				latest.PutCred(credName, config.Credential{
					AccessKeyID:     mergedAK,
					SecretAccessKey: mergedSK,
				})
			case set["ak"] && set["sk"]:
				// New credential: both AK and SK must be non-empty to create it.
				if strings.TrimSpace(mergedAK) == "" {
					return errors.New("credential " + credName + " has empty access key id; supply a non-empty --ak")
				}
				if strings.TrimSpace(mergedSK) == "" {
					return errors.New("credential " + credName + " has empty secret access key; supply a non-empty --sk")
				}
				latest.PutCred(credName, config.Credential{
					AccessKeyID:     mergedAK,
					SecretAccessKey: mergedSK,
				})
			default:
				// No stored credential and not enough supplied parts to create
				// one: the referenced credential is missing.
				return errors.New("credential not found: " + credName)
			}

			cred, ok := latest.GetCred(credName)
			if !ok {
				return errors.New("credential not found: " + credName)
			}
			maskedAK = config.MaskAK(cred.AccessKeyID)
			existing.AccessKeyID = ""
			existing.SecretAccessKey = ""
		} else {
			maskedAK = config.MaskAK(existing.AccessKeyID)
		}

		if err := validateWorkloadProfile(existing, normalized); err != nil {
			return err
		}

		latest.PutProfile(name, existing)
		finalProf = existing
		if latest.CurrentProfile == "" {
			latest.CurrentProfile = name
		}
		return nil
	}); err != nil {
		return nil, err
	}

	out := map[string]any{
		"profile":       name,
		"region":        finalProf.Region,
		"endpoint":      finalProf.Endpoint,
		"cred_ref":      strings.TrimSpace(finalProf.CredRef),
		"access_key_id": maskedAK,
	}
	applyInsecureMarker(out, normalized, finalProf.DisableSSL)
	return out, nil
}

// validateWorkloadProfile checks the merged profile against the requirements of
// the selected auth mode. All modes require region and endpoint. ak and
// ramrolearn require a source credential (inline AK/SK or a cred-ref);
// ramrolearn additionally requires account-id and role-name; oidc requires
// oidc-token-file and role-trn; ecsrole requires role-name.
func validateWorkloadProfile(p config.Profile, mode string) error {
	if strings.TrimSpace(p.Region) == "" {
		return errors.New("missing required fields: --region")
	}
	if strings.TrimSpace(p.Endpoint) == "" {
		return errors.New("missing required fields: --endpoint")
	}
	switch mode {
	case config.AuthModeAK:
		hasSource := (strings.TrimSpace(p.AccessKeyID) != "" && strings.TrimSpace(p.SecretAccessKey) != "") ||
			strings.TrimSpace(p.CredRef) != ""
		if !hasSource {
			return errors.New("missing required fields: --ak --sk (or --cred-ref <name>)")
		}
	case config.AuthModeRamRoleARN:
		hasSource := (strings.TrimSpace(p.AccessKeyID) != "" && strings.TrimSpace(p.SecretAccessKey) != "") ||
			strings.TrimSpace(p.CredRef) != ""
		if !hasSource {
			return errors.New("missing required source credentials: --ak --sk (or --cred-ref <name>)")
		}
		var missing []string
		if strings.TrimSpace(p.AccountID) == "" {
			missing = append(missing, "--account-id")
		}
		if strings.TrimSpace(p.RoleName) == "" {
			missing = append(missing, "--role-name")
		}
		if len(missing) > 0 {
			return errors.New("missing required fields: " + strings.Join(missing, " "))
		}
	case config.AuthModeOIDC:
		var missing []string
		if strings.TrimSpace(p.OIDCTokenFile) == "" {
			missing = append(missing, "--oidc-token-file")
		}
		if strings.TrimSpace(p.RoleTRN) == "" {
			missing = append(missing, "--role-trn")
		}
		if len(missing) > 0 {
			return errors.New("missing required fields: " + strings.Join(missing, " "))
		}
	case config.AuthModeECSRole:
		if strings.TrimSpace(p.RoleName) == "" {
			return errors.New("missing required fields: --role-name")
		}
	}
	return nil
}

func runConfigureProject(ctx *Context, args []string) (any, error) {
	if len(args) == 0 {
		return nil, &usageError{Text: usageConfigure(), ExitCode: 1}
	}
	switch args[0] {
	case "show":
		wd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		cfg, p, err := config.LoadProjectConfig(wd)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"path":          p,
			"output":        cfg.Output,
			"output_mode":   cfg.OutputMode,
			"output_dir":    cfg.OutputDir,
			"timeout":       cfg.TimeoutSeconds,
			"trace_redact":  cfg.TraceRedact,
			"hints_file":    cfg.HintsFile,
			"region":        cfg.Region,
			"endpoint":      cfg.Endpoint,
			"effective_wd":  wd,
			"config_exists": p != "",
		}, nil
	case "set":
		var output string
		var outputMode string
		var outputDir string
		var timeout int
		var traceRedact string
		var hintsFile string
		for len(args) > 0 {
			switch args[0] {
			case "set":
				args = args[1:]
			case "--output":
				if len(args) < 2 {
					return nil, errors.New("missing --output value")
				}
				output = args[1]
				args = args[2:]
			case "--output-mode":
				if len(args) < 2 {
					return nil, errors.New("missing --output-mode value")
				}
				outputMode = args[1]
				args = args[2:]
			case "--output-dir":
				if len(args) < 2 {
					return nil, errors.New("missing --output-dir value")
				}
				outputDir = args[1]
				args = args[2:]
			case "--timeout-seconds":
				if len(args) < 2 {
					return nil, errors.New("missing --timeout-seconds value")
				}
				v, err := strconv.Atoi(args[1])
				if err != nil {
					return nil, err
				}
				timeout = v
				args = args[2:]
			case "--trace-redact":
				if len(args) < 2 {
					return nil, errors.New("missing --trace-redact value")
				}
				traceRedact = args[1]
				args = args[2:]
			case "--hints-file":
				if len(args) < 2 {
					return nil, errors.New("missing --hints-file value")
				}
				hintsFile = args[1]
				args = args[2:]
			default:
				return nil, errors.New("unknown flag: " + args[0])
			}
		}
		wd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		cfg, p, err := config.LoadProjectConfig(wd)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(output) != "" {
			cfg.Output = strings.TrimSpace(output)
		}
		if strings.TrimSpace(outputMode) != "" {
			cfg.OutputMode = strings.TrimSpace(outputMode)
		}
		if strings.TrimSpace(outputDir) != "" {
			cfg.OutputDir = strings.TrimSpace(outputDir)
		}
		if timeout != 0 {
			cfg.TimeoutSeconds = timeout
		}
		if strings.TrimSpace(traceRedact) != "" {
			cfg.TraceRedact = strings.TrimSpace(traceRedact)
		}
		if strings.TrimSpace(hintsFile) != "" {
			cfg.HintsFile = strings.TrimSpace(hintsFile)
		}
		if p == "" {
			p = ""
			if w, err := os.Getwd(); err == nil {
				p = w
			}
			if strings.TrimSpace(p) == "" {
				return nil, errors.New("working directory not found")
			}
			p = p + string(os.PathSeparator) + ".volclog" + string(os.PathSeparator) + "cli.config.json"
		}
		if err := config.SaveProjectConfigAt(p, cfg); err != nil {
			return nil, err
		}
		return map[string]any{
			"path":         p,
			"output":       cfg.Output,
			"output_mode":  cfg.OutputMode,
			"output_dir":   cfg.OutputDir,
			"timeout":      cfg.TimeoutSeconds,
			"trace_redact": cfg.TraceRedact,
			"hints_file":   cfg.HintsFile,
		}, nil
	default:
		return nil, errors.New("unknown configure project command: " + args[0])
	}
}

func configureUse(ctx *Context, args []string) (any, error) {
	var name string
	if len(args) >= 2 && args[0] == "--profile" {
		name = args[1]
	} else if len(args) >= 1 {
		name = args[0]
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("missing profile name")
	}
	var currentProfile string
	if err := ctx.UpdateConfig(func(latest *config.Config) error {
		if _, ok := latest.GetProfile(name); !ok {
			return errors.New("profile not found: " + name)
		}
		latest.CurrentProfile = name
		currentProfile = name
		return nil
	}); err != nil {
		return nil, err
	}
	return map[string]any{"current_profile": currentProfile}, nil
}

func configureShow(ctx *Context, args []string) (any, error) {
	var name string
	if len(args) >= 2 && args[0] == "--profile" {
		name = args[1]
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = strings.TrimSpace(ctx.cfg.CurrentProfile)
	}
	if name == "" {
		name = "default"
	}
	p, ok := ctx.cfg.GetProfile(name)
	if !ok {
		return nil, errors.New("profile not found: " + name)
	}
	return buildProfileOutput(ctx, name, p), nil
}

func configureList(ctx *Context, args []string) (any, error) {
	var prefix string
	for len(args) > 0 {
		switch args[0] {
		case "--prefix":
			if len(args) < 2 {
				return nil, errors.New("missing --prefix value")
			}
			prefix = strings.TrimSpace(args[1])
			args = args[2:]
		default:
			return nil, errors.New("unknown flag: " + args[0])
		}
	}

	var names []string
	for name := range ctx.cfg.Profiles {
		if prefix != "" && !strings.HasPrefix(name, prefix) {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	profiles := make([]map[string]any, 0, len(names))
	for _, name := range names {
		p, ok := ctx.cfg.GetProfile(name)
		if !ok {
			continue
		}
		profiles = append(profiles, buildProfileOutput(ctx, name, p))
	}

	return map[string]any{
		"current_profile": strings.TrimSpace(ctx.cfg.CurrentProfile),
		"profiles":        profiles,
	}, nil
}

// buildProfileOutput constructs the profile output map for configure show/list.
// Static fields remain unchanged. Cached login modes add provider/cache status
// from the read-only authStatusReader; workload modes add only on-demand source
// readiness. No secret material is ever included.
func buildProfileOutput(ctx *Context, name string, p config.Profile) map[string]any {
	credRef := strings.TrimSpace(p.CredRef)
	credStatus := config.ResolveProfileCredentialStatus(ctx.cfg, p)
	out := map[string]any{
		"profile":            name,
		"effective_profile":  name,
		"region":             p.Region,
		"endpoint":           p.Endpoint,
		"cred_ref":           credRef,
		"credential_source":  credStatus.Source,
		"credential_present": credStatus.Present,
		"access_key_id":      config.MaskAK(credStatus.AccessKeyID),
		"has_security_token": p.SecurityToken != "",
		"timeout_seconds":    p.TimeoutSeconds,
	}
	mode, _ := config.NormalizeAuthMode(p.Mode)
	applyInsecureMarker(out, mode, p.DisableSSL)
	if config.IsProviderAuthMode(mode) {
		out["auth_mode"] = mode
		out["provider"] = dynamicProviderName(mode)
		if config.IsCachedLoginAuthMode(mode) {
			// SSO/Console: safe defaults; cache treated as absent/expired until
			// a status reader proves otherwise. These fields are always present
			// for dynamic profiles so callers can rely on a stable schema.
			out["auth_present"] = false
			out["expires_at"] = ""
			out["refresh_required"] = true
			if reader, rerr := dynamicAuthStatusReader(mode, ctx.cfgPath, name, ctx.cfg, ctx.authFactory); rerr == nil && reader != nil {
				if st, serr := reader.Status(context.Background(), name); serr == nil {
					if st.Provider != "" {
						out["provider"] = st.Provider
					}
					out["auth_present"] = st.Present
					out["expires_at"] = formatExpiresAt(st.ExpiresAt)
					out["refresh_required"] = st.RefreshRequired
				}
			}
		} else {
			// Workload modes: on-demand, memory-only. No disk cache; credentials
			// are never present from configure show. Source type describes the
			// local configuration; source_ready reports local readiness.
			out["source"] = workloadSourceType(mode, p)
			out["auth_present"] = false
			out["expires_at"] = ""
			out["on_demand"] = true
			out["memory_only"] = true
			out["source_ready"] = workloadSourceReady(mode, p, ctx.cfg)
		}
	}
	return out
}

// insecureSSLWarning is the stable, secret-free warning text shown when a
// RAM/OIDC profile has DisableSSL=true. It is shared by configure and doctor
// output so the message cannot drift between surfaces.
const insecureSSLWarning = "STS requests will use HTTP; authentication material may be transmitted in plaintext. TLS business endpoint is unaffected."

// insecureSSLCondition reports whether a profile with the given normalized mode
// and DisableSSL flag must be marked insecure in output. Only RAM Role ARN and
// OIDC source temporary credentials over HTTP; ECS uses IMDS (no STS over HTTP)
// and AK/SSO/Console are unaffected.
func insecureSSLCondition(mode string, disableSSL bool) bool {
	return disableSSL && (mode == config.AuthModeRamRoleARN || mode == config.AuthModeOIDC)
}

// applyInsecureMarker adds disable_ssl/insecure/warning fields to out when the
// profile is a RAM/OIDC profile with DisableSSL=true. The warning text is stable
// and contains no secret material.
func applyInsecureMarker(out map[string]any, mode string, disableSSL bool) {
	if !insecureSSLCondition(mode, disableSSL) {
		return
	}
	out["disable_ssl"] = true
	out["insecure"] = true
	out["warning"] = insecureSSLWarning
}

// dynamicProviderName returns the canonical provider name for a dynamic auth
// mode, used as the safe default when the status reader is unavailable.
func dynamicProviderName(mode string) string {
	switch strings.TrimSpace(mode) {
	case config.AuthModeSSO:
		return "sso"
	case config.AuthModeConsoleLogin:
		return "console-login"
	case config.AuthModeRamRoleARN:
		return "ramrolearn"
	case config.AuthModeOIDC:
		return "oidc"
	case config.AuthModeECSRole:
		return "ecsrole"
	}
	return "console-login"
}

func configureDelete(ctx *Context, args []string) (any, error) {
	var (
		name   string
		prefix string
		yes    bool
	)
	for len(args) > 0 {
		switch args[0] {
		case "--profile":
			if len(args) < 2 {
				return nil, errors.New("missing --profile value")
			}
			name = args[1]
			args = args[2:]
		case "--prefix":
			if len(args) < 2 {
				return nil, errors.New("missing --prefix value")
			}
			prefix = args[1]
			args = args[2:]
		case "--yes":
			yes = true
			args = args[1:]
		default:
			if strings.HasPrefix(args[0], "--") {
				return nil, errors.New("unknown flag: " + args[0])
			}
			if name != "" {
				return nil, errors.New("too many arguments")
			}
			name = args[0]
			args = args[1:]
		}
	}

	name = strings.TrimSpace(name)
	prefix = strings.TrimSpace(prefix)
	if prefix != "" && name != "" {
		return nil, errors.New("cannot use both profile name and --prefix")
	}

	if prefix != "" {
		if !yes {
			return nil, errors.New("refuse to delete by prefix without --yes")
		}
		toDelete := make([]string, 0)
		var currentProfile string
		noChange := errors.New("no matching profiles")
		err := ctx.UpdateConfig(func(latest *config.Config) error {
			toDelete = toDelete[:0]
			for n := range latest.Profiles {
				if strings.HasPrefix(n, prefix) {
					toDelete = append(toDelete, n)
				}
			}
			sort.Strings(toDelete)
			currentProfile = strings.TrimSpace(latest.CurrentProfile)
			if len(toDelete) == 0 {
				return noChange
			}
			for _, n := range toDelete {
				delete(latest.Profiles, n)
			}
			adjustCurrentProfile(latest)
			currentProfile = strings.TrimSpace(latest.CurrentProfile)
			return nil
		})
		if err != nil && !errors.Is(err, noChange) {
			return nil, err
		}
		return map[string]any{
			"deleted":         toDelete,
			"current_profile": currentProfile,
		}, nil
	}

	if name == "" {
		return nil, errors.New("missing profile name")
	}
	var currentProfile string
	if err := ctx.UpdateConfig(func(latest *config.Config) error {
		if _, ok := latest.GetProfile(name); !ok {
			return errors.New("profile not found: " + name)
		}
		delete(latest.Profiles, name)
		adjustCurrentProfile(latest)
		currentProfile = strings.TrimSpace(latest.CurrentProfile)
		return nil
	}); err != nil {
		return nil, err
	}
	return map[string]any{
		"deleted":         name,
		"current_profile": currentProfile,
	}, nil
}

func configureCredDelete(ctx *Context, args []string) (any, error) {
	var name string
	for len(args) > 0 {
		switch {
		case strings.HasPrefix(args[0], "--"):
			return nil, errors.New("unknown flag: " + args[0])
		default:
			if name != "" {
				return nil, errors.New("too many arguments")
			}
			name = args[0]
			args = args[1:]
		}
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("missing credential name")
	}
	var currentProfile string
	if err := ctx.UpdateConfig(func(latest *config.Config) error {
		if _, ok := latest.GetCred(name); !ok {
			return errors.New("credential not found: " + name)
		}
		inUse := profilesUsingCredential(*latest, name)
		if len(inUse) > 0 {
			return errors.New("credential in use by profiles: " + strings.Join(inUse, ","))
		}
		delete(latest.Creds, name)
		currentProfile = strings.TrimSpace(latest.CurrentProfile)
		return nil
	}); err != nil {
		return nil, err
	}
	return map[string]any{
		"deleted":         name,
		"current_profile": currentProfile,
	}, nil
}

func profilesUsingCredential(cfg config.Config, credName string) []string {
	names := make([]string, 0, 8)
	for profileName, p := range cfg.Profiles {
		if strings.TrimSpace(p.CredRef) == credName {
			names = append(names, profileName)
		}
	}
	sort.Strings(names)
	return names
}

func adjustCurrentProfile(cfg *config.Config) {
	cur := strings.TrimSpace(cfg.CurrentProfile)
	if cur != "" {
		if _, ok := cfg.GetProfile(cur); ok {
			return
		}
	}
	cfg.CurrentProfile = ""
	if _, ok := cfg.GetProfile("default"); ok {
		cfg.CurrentProfile = "default"
		return
	}
	var names []string
	for name := range cfg.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) > 0 {
		cfg.CurrentProfile = names[0]
	}
}
