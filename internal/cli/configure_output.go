package cli

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/config"
)

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
