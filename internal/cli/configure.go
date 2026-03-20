package cli

import (
	"errors"
	"sort"
	"strconv"
	"strings"

	"volclog/internal/config"
)

func runConfigure(ctx *Context, args []string) (any, error) {
	if err := ctx.LoadConfig(); err != nil {
		return nil, err
	}
	if len(args) == 0 {
		return nil, &usageError{Text: usageConfigure(), ExitCode: 1}
	}
	if args[0] == "-h" || args[0] == "--help" {
		return nil, &usageError{Text: usageConfigure(), ExitCode: 0}
	}
	if hasHelp(args[1:]) {
		return nil, &usageError{Text: usageConfigure(), ExitCode: 0}
	}
	switch args[0] {
	case "set":
		return configureSet(ctx, args[1:])
	case "use":
		return configureUse(ctx, args[1:])
	case "show":
		return configureShow(ctx, args[1:])
	case "list":
		return configureList(ctx, args[1:])
	case "delete":
		return configureDelete(ctx, args[1:])
	default:
		return nil, errors.New("unknown configure command: " + args[0])
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
	if credRef != "" {
		if cred, ok := ctx.cfg.GetCred(credRef); ok {
			maskedAK = config.MaskAK(cred.AccessKeyID)
			credOK = strings.TrimSpace(cred.AccessKeyID) != "" && strings.TrimSpace(cred.SecretAccessKey) != ""
		} else {
			credOK = false
		}
	}
	return map[string]any{
		"profile":            name,
		"region":             p.Region,
		"endpoint":           p.Endpoint,
		"cred_ref":           credRef,
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
		if credRef != "" {
			if cred, ok := ctx.cfg.GetCred(credRef); ok {
				maskedAK = config.MaskAK(cred.AccessKeyID)
				credOK = strings.TrimSpace(cred.AccessKeyID) != "" && strings.TrimSpace(cred.SecretAccessKey) != ""
			} else {
				credOK = false
			}
		}
		profiles = append(profiles, map[string]any{
			"profile":            name,
			"region":             p.Region,
			"endpoint":           p.Endpoint,
			"cred_ref":           credRef,
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
