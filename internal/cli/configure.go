package cli

import (
	"errors"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/config"
)

func runConfigure(ctx *Context, args []string) (any, error) {
	if err := ctx.LoadConfig(); err != nil {
		return nil, err
	}
	return runSubcommandGroup(args, usageConfigure(), nil, func(command string, commandArgs []string) (any, error) {
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
	}
	if p.Region == "" && p.Endpoint != "" {
		p.Region = config.DeriveRegionFromEndpoint(p.Endpoint)
	}
	maskedAK := config.MaskAK(p.AccessKeyID)
	credPresent := p.AccessKeyID != "" && p.SecretAccessKey != ""
	if strings.TrimSpace(p.CredRef) != "" {
		credName := strings.TrimSpace(p.CredRef)
		if credPresent {
			ctx.cfg.PutCred(credName, config.Credential{
				AccessKeyID:     p.AccessKeyID,
				SecretAccessKey: p.SecretAccessKey,
			})
		}
		cred, ok := ctx.cfg.GetCred(credName)
		if !ok {
			return nil, errors.New("credential not found: " + credName)
		}
		maskedAK = config.MaskAK(cred.AccessKeyID)
		p.AccessKeyID = ""
		p.SecretAccessKey = ""
	}
	if strings.TrimSpace(p.CredRef) == "" && (p.AccessKeyID == "" || p.SecretAccessKey == "") {
		return nil, errors.New("missing required fields: --ak --sk (or --cred-ref <name>)")
	}
	if p.Endpoint == "" {
		return nil, errors.New("missing required fields: --endpoint")
	}
	if p.Region == "" {
		return nil, errors.New("missing required fields: --region (or use tls-<region>.volces.com endpoint)")
	}
	ctx.cfg.PutProfile(name, p)
	if ctx.cfg.CurrentProfile == "" {
		ctx.cfg.CurrentProfile = name
	}
	if err := ctx.SaveConfig(); err != nil {
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
	if _, ok := ctx.cfg.GetProfile(name); !ok {
		return nil, errors.New("profile not found: " + name)
	}
	ctx.cfg.CurrentProfile = name
	if err := ctx.SaveConfig(); err != nil {
		return nil, err
	}
	return map[string]any{"current_profile": name}, nil
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
	maskedAK := config.MaskAK(p.AccessKeyID)
	credRef := strings.TrimSpace(p.CredRef)
	credOK := p.AccessKeyID != "" && p.SecretAccessKey != ""
	credentialSource := "profile_inline"
	if !credOK {
		credentialSource = "profile_missing"
	}
	if credRef != "" {
		credentialSource = "profile_cred_ref"
		if cred, ok := ctx.cfg.GetCred(credRef); ok {
			maskedAK = config.MaskAK(cred.AccessKeyID)
			credOK = strings.TrimSpace(cred.AccessKeyID) != "" && strings.TrimSpace(cred.SecretAccessKey) != ""
			if !credOK {
				credentialSource = "profile_cred_ref_missing"
			}
		} else {
			credOK = false
			credentialSource = "profile_cred_ref_missing"
		}
	}
	return map[string]any{
		"profile":            name,
		"effective_profile":  name,
		"region":             p.Region,
		"endpoint":           p.Endpoint,
		"cred_ref":           credRef,
		"credential_source":  credentialSource,
		"credential_present": credOK,
		"access_key_id":      maskedAK,
		"has_security_token": p.SecurityToken != "",
		"timeout_seconds":    p.TimeoutSeconds,
	}, nil
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
		maskedAK := config.MaskAK(p.AccessKeyID)
		credRef := strings.TrimSpace(p.CredRef)
		credOK := p.AccessKeyID != "" && p.SecretAccessKey != ""
		credentialSource := "profile_inline"
		if !credOK {
			credentialSource = "profile_missing"
		}
		if credRef != "" {
			credentialSource = "profile_cred_ref"
			if cred, ok := ctx.cfg.GetCred(credRef); ok {
				maskedAK = config.MaskAK(cred.AccessKeyID)
				credOK = strings.TrimSpace(cred.AccessKeyID) != "" && strings.TrimSpace(cred.SecretAccessKey) != ""
				if !credOK {
					credentialSource = "profile_cred_ref_missing"
				}
			} else {
				credOK = false
				credentialSource = "profile_cred_ref_missing"
			}
		}
		profiles = append(profiles, map[string]any{
			"profile":            name,
			"effective_profile":  name,
			"region":             p.Region,
			"endpoint":           p.Endpoint,
			"cred_ref":           credRef,
			"credential_source":  credentialSource,
			"credential_present": credOK,
			"access_key_id":      maskedAK,
			"has_security_token": p.SecurityToken != "",
			"timeout_seconds":    p.TimeoutSeconds,
		})
	}

	return map[string]any{
		"current_profile": strings.TrimSpace(ctx.cfg.CurrentProfile),
		"profiles":        profiles,
	}, nil
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
		var toDelete []string
		for n := range ctx.cfg.Profiles {
			if strings.HasPrefix(n, prefix) {
				toDelete = append(toDelete, n)
			}
		}
		sort.Strings(toDelete)
		if len(toDelete) == 0 {
			return map[string]any{
				"deleted":         []string{},
				"current_profile": strings.TrimSpace(ctx.cfg.CurrentProfile),
			}, nil
		}
		for _, n := range toDelete {
			delete(ctx.cfg.Profiles, n)
		}
		adjustCurrentProfile(&ctx.cfg)
		if err := ctx.SaveConfig(); err != nil {
			return nil, err
		}
		return map[string]any{
			"deleted":         toDelete,
			"current_profile": strings.TrimSpace(ctx.cfg.CurrentProfile),
		}, nil
	}

	if name == "" {
		return nil, errors.New("missing profile name")
	}
	if _, ok := ctx.cfg.GetProfile(name); !ok {
		return nil, errors.New("profile not found: " + name)
	}
	delete(ctx.cfg.Profiles, name)
	adjustCurrentProfile(&ctx.cfg)
	if err := ctx.SaveConfig(); err != nil {
		return nil, err
	}
	return map[string]any{
		"deleted":         name,
		"current_profile": strings.TrimSpace(ctx.cfg.CurrentProfile),
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
	if _, ok := ctx.cfg.GetCred(name); !ok {
		return nil, errors.New("credential not found: " + name)
	}
	inUse := profilesUsingCredential(ctx.cfg, name)
	if len(inUse) > 0 {
		return nil, errors.New("credential in use by profiles: " + strings.Join(inUse, ","))
	}
	delete(ctx.cfg.Creds, name)
	if err := ctx.SaveConfig(); err != nil {
		return nil, err
	}
	return map[string]any{
		"deleted":         name,
		"current_profile": strings.TrimSpace(ctx.cfg.CurrentProfile),
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
