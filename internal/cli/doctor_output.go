package cli

import (
	"strings"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth/oidc"
	"github.com/volcengine-tls/ve-tls-cli/internal/config"
)

func buildDoctorOutput(state *doctorRuntimeState, online bool) (any, int, error) {
	credentials := map[string]any{
		"mode":    state.credMode,
		"source":  state.credSource,
		"present": state.credPresent,
		"ak":      state.akPresent,
		"sk":      state.skPresent,
		"token":   state.tokenPresent,
	}
	if state.isDynamic {
		if config.IsCachedLoginAuthMode(state.mode) {
			// SSO/Console: safe defaults until the offline reader proves the
			// cache valid.
			providerName := dynamicProviderName(state.mode)
			refreshRequired := true
			if state.dynamicStatus.Provider != "" {
				providerName = state.dynamicStatus.Provider
			}
			if state.dynamicStatus.Present {
				refreshRequired = state.dynamicStatus.RefreshRequired
			}
			credentials["provider"] = providerName
			credentials["expires_at"] = formatExpiresAt(state.dynamicStatus.ExpiresAt)
			credentials["refresh_required"] = refreshRequired
		} else {
			// Workload modes are on-demand and memory-only.
			state.sourceReady = workloadSourceReady(state.mode, state.profile, state.cfg)
			credentials["provider"] = dynamicProviderName(state.mode)
			credentials["source"] = workloadSourceType(state.mode, state.profile)
			credentials["present"] = state.credPresent
			credentials["ak"] = state.credPresent
			credentials["sk"] = state.credPresent
			credentials["token"] = state.credPresent
			credentials["expires_at"] = ""
			credentials["on_demand"] = true
			credentials["memory_only"] = true
			credentials["source_ready"] = state.sourceReady
		}
	}
	// source_ready remains the final check, after every optional online check.
	if state.isDynamic && !config.IsCachedLoginAuthMode(state.mode) {
		state.checks = append(state.checks, map[string]any{
			"name":   "source_ready",
			"ok":     state.sourceReady,
			"detail": workloadSourceType(state.mode, state.profile),
		})
	}

	out := map[string]any{
		"profile": map[string]any{
			"selected": state.profileName,
			"source":   state.profileSource,
		},
		"endpoint": map[string]any{
			"value":  state.endpoint,
			"source": state.endpointSource,
		},
		"region": map[string]any{
			"value":  state.region,
			"source": state.regionSource,
		},
		"credentials": credentials,
		"time": map[string]any{
			"local_unix_ms":  state.localNowMS,
			"server_unix_ms": state.serverMS,
			"skew_seconds":   state.skewSeconds,
			"skew_risk":      state.skewRisk,
		},
		"timeout": map[string]any{
			"seconds": state.timeoutSeconds,
			"source":  state.timeoutSource,
		},
		"checks": state.checks,
	}

	// DisableSSL warnings remain secret-free and do not affect the TLS business
	// endpoint.
	var warnings []map[string]any
	if insecureSSLCondition(state.mode, state.profile.DisableSSL) {
		warnings = append(warnings, map[string]any{
			"name":   "disable-ssl",
			"detail": insecureSSLWarning,
		})
	}
	if len(warnings) > 0 {
		out["warnings"] = warnings
	}

	basicOK := strings.TrimSpace(state.region) != "" && state.endpointValid
	if online {
		if basicOK && state.onlineOK {
			return out, 0, nil
		}
		return out, 2, nil
	}
	if state.isDynamic && !config.IsCachedLoginAuthMode(state.mode) {
		if !basicOK || !state.sourceReady {
			return out, 2, nil
		}
		return out, 0, nil
	}
	if !state.credPresent || !basicOK {
		return out, 2, nil
	}
	return out, 0, nil
}

// resolveDoctorRuntimeValue mirrors request-time runtime selection. Dynamic
// providers use environment runtime overrides independently of their identity.
// Static auth uses environment runtime values only when a complete environment
// AK/SK pair selected the environment identity; otherwise it stays on the
// profile. Explicit command flags always win.
func resolveDoctorRuntimeValue(flagValue, envValue, profileValue, projectValue string, isDynamic, staticUsesEnvIdentity bool) (string, string) {
	if value := strings.TrimSpace(flagValue); value != "" {
		return value, "flag"
	}
	if isDynamic {
		if value := strings.TrimSpace(envValue); value != "" {
			return value, "env"
		}
		if value := strings.TrimSpace(profileValue); value != "" {
			return value, "profile"
		}
		if value := strings.TrimSpace(projectValue); value != "" {
			return value, "project"
		}
		return "", ""
	}
	if staticUsesEnvIdentity {
		if value := strings.TrimSpace(envValue); value != "" {
			return value, "env"
		}
		if value := strings.TrimSpace(projectValue); value != "" {
			return value, "project"
		}
		return "", ""
	}
	if value := strings.TrimSpace(profileValue); value != "" {
		return value, "profile"
	}
	if value := strings.TrimSpace(projectValue); value != "" {
		return value, "project"
	}
	return "", ""
}

// formatExpiresAt renders cache expiration in RFC3339 UTC.
func formatExpiresAt(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

// workloadSourceType returns a secret-free label for the source provider.
func workloadSourceType(mode string, profile config.Profile) string {
	switch mode {
	case config.AuthModeRamRoleARN:
		if strings.TrimSpace(profile.CredRef) != "" {
			return "profile_cred_ref"
		}
		return "profile_inline"
	case config.AuthModeOIDC:
		return "token_file"
	case config.AuthModeECSRole:
		return "instance_metadata"
	}
	return "on_demand"
}

// workloadSourceReady performs local, read-only source checks only.
func workloadSourceReady(mode string, profile config.Profile, cfg config.Config) bool {
	switch mode {
	case config.AuthModeRamRoleARN:
		accessKeyID := strings.TrimSpace(profile.AccessKeyID)
		secretAccessKey := strings.TrimSpace(profile.SecretAccessKey)
		if credRef := strings.TrimSpace(profile.CredRef); credRef != "" {
			if credential, ok := cfg.GetCred(credRef); ok {
				if accessKeyID == "" {
					accessKeyID = strings.TrimSpace(credential.AccessKeyID)
				}
				if secretAccessKey == "" {
					secretAccessKey = strings.TrimSpace(credential.SecretAccessKey)
				}
			} else {
				return false
			}
		}
		return accessKeyID != "" &&
			secretAccessKey != "" &&
			strings.TrimSpace(profile.AccountID) != "" &&
			strings.TrimSpace(profile.RoleName) != ""
	case config.AuthModeOIDC:
		if strings.TrimSpace(profile.RoleTRN) == "" {
			return false
		}
		tokenFile := strings.TrimSpace(profile.OIDCTokenFile)
		if tokenFile == "" {
			return false
		}
		return oidc.InspectTokenFile(tokenFile) == nil
	case config.AuthModeECSRole:
		return strings.TrimSpace(profile.RoleName) != ""
	}
	return false
}
