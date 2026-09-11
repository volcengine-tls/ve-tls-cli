package cli

import (
	"errors"
	"sort"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/config"
)

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
