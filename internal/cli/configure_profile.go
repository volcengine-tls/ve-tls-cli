package cli

import (
	"errors"
	"strconv"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/config"
)

func configureSet(ctx *Context, args []string) (any, error) {
	if hasExplicitModeFlag(args) {
		return configureSetExplicitMode(ctx, args)
	}
	if isRuntimeOnlyProfilePatch(args) {
		return configureSetRuntimeOnly(ctx, args)
	}
	return configureSetLegacy(ctx, args)
}

func isRuntimeOnlyProfilePatch(args []string) bool {
	hasRuntimeField := false
	for len(args) > 0 {
		if len(args) < 2 {
			return false
		}
		switch args[0] {
		case "--profile":
		case "--region", "--endpoint", "--timeout-seconds":
			hasRuntimeField = true
		default:
			return false
		}
		args = args[2:]
	}
	return hasRuntimeField
}

func configureSetRuntimeOnly(ctx *Context, args []string) (any, error) {
	name := "default"
	var (
		regionSet   bool
		endpointSet bool
		timeoutSet  bool
		region      string
		endpoint    string
		timeout     int
	)
	for len(args) > 0 {
		switch args[0] {
		case "--profile":
			name = strings.TrimSpace(args[1])
			if name == "" {
				name = "default"
			}
		case "--region":
			region = strings.TrimSpace(args[1])
			if region == "" {
				return nil, errors.New("missing --region value")
			}
			regionSet = true
		case "--endpoint":
			endpoint = strings.TrimSpace(args[1])
			if endpoint == "" {
				return nil, errors.New("missing --endpoint value")
			}
			endpointSet = true
		case "--timeout-seconds":
			v, err := strconv.Atoi(args[1])
			if err != nil {
				return nil, err
			}
			timeout = v
			timeoutSet = true
		default:
			return nil, errors.New("unknown flag: " + args[0])
		}
		args = args[2:]
	}

	var final config.Profile
	if err := ctx.UpdateConfig(func(latest *config.Config) error {
		existing, ok := latest.GetProfile(name)
		if !ok {
			return errors.New("profile not found: " + name)
		}
		if regionSet {
			existing.Region = region
		}
		if endpointSet {
			existing.Endpoint = endpoint
		}
		if timeoutSet {
			existing.TimeoutSeconds = timeout
		}
		latest.PutProfile(name, existing)
		final = existing
		return nil
	}); err != nil {
		return nil, err
	}
	credStatus := config.ResolveProfileCredentialStatus(ctx.cfg, final)
	return map[string]any{
		"profile":       name,
		"region":        final.Region,
		"endpoint":      final.Endpoint,
		"timeout":       final.TimeoutSeconds,
		"cred_ref":      strings.TrimSpace(final.CredRef),
		"access_key_id": config.MaskAK(credStatus.AccessKeyID),
	}, nil
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

// validateWorkloadProfile checks only the identity requirements of the selected
// auth mode. Region and endpoint are runtime settings resolved later from
// command flags, environment, profile, or project config. ak and ramrolearn
// require a source credential (inline AK/SK or a cred-ref);
// ramrolearn additionally requires account-id and role-name; oidc requires
// oidc-token-file and role-trn; ecsrole requires role-name.
func validateWorkloadProfile(p config.Profile, mode string) error {
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
