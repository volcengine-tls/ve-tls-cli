package cli

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/config"
)

type doctorRuntimeState struct {
	cfg           config.Config
	cfgPath       string
	profile       config.Profile
	profileName   string
	profileSource string
	mode          string
	isDynamic     bool

	dynamicStatus authStatus
	dynamicReader authStatusReader
	credStatus    config.CredentialStatus
	credMode      string
	credSource    string
	credPresent   bool
	akPresent     bool
	skPresent     bool
	tokenPresent  bool
	sourceReady   bool

	region         string
	regionSource   string
	endpoint       string
	endpointSource string
	timeoutSeconds int
	timeoutSource  string

	credentialPresentCheck map[string]any
	checks                 []map[string]any

	skewRisk      string
	localNowMS    int64
	serverMS      int64
	skewSeconds   int64
	endpointValid bool
	onlineOK      bool
}

func collectDoctorRuntimeState(ctx *Context) (*doctorRuntimeState, error) {
	localStartMS := time.Now().UnixMilli()

	cfg, cfgPath, err := config.Load()
	if err != nil {
		return nil, err
	}
	wd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	projectCfg, _, err := config.LoadProjectConfig(wd)
	if err != nil {
		return nil, err
	}

	profileName := strings.TrimSpace(ctx.Profile)
	profileSource := "flag"
	if profileName == "" {
		profileName = strings.TrimSpace(cfg.CurrentProfile)
		profileSource = "current"
	}
	if profileName == "" {
		profileName = "default"
		profileSource = "default"
	}

	envRegion := strings.TrimSpace(os.Getenv("VOLCENGINE_REGION"))
	envEndpoint := strings.TrimSpace(os.Getenv("VOLCENGINE_ENDPOINT"))
	runtimeRegion := strings.TrimSpace(ctx.RuntimeRegion)
	runtimeEndpoint := strings.TrimSpace(ctx.RuntimeEndpoint)

	var profile config.Profile
	if resolved, ok := cfg.GetProfile(profileName); ok {
		profile = resolved
	}
	mode, _ := config.NormalizeAuthMode(profile.Mode)
	isDynamic := config.IsProviderAuthMode(mode)

	// credStatus holds the resolved credential status. Provider modes ignore
	// environment AK/SK. Cached login modes derive status from the read-only
	// authStatusReader; workload readiness is reported separately below.
	var credStatus config.CredentialStatus
	var dynamicStatus authStatus
	var dynamicReader authStatusReader
	staticUsesEnvIdentity := false
	if isDynamic {
		credStatus = config.CredentialStatus{Mode: mode, Source: "profile"}
		if reader, readerErr := dynamicAuthStatusReader(mode, cfgPath, profileName, cfg, ctx.authFactory); readerErr == nil && reader != nil {
			dynamicReader = reader
			if status, statusErr := reader.Status(context.Background(), profileName); statusErr == nil {
				dynamicStatus = status
				credStatus.Present = status.Present
			}
		}
	} else {
		credStatus = config.ResolveProfileCredentialStatus(cfg, profile)
		if envCredStatus := config.ResolveEnvCredentialStatus(); envCredStatus.Present {
			credStatus = envCredStatus
			staticUsesEnvIdentity = true
		}
	}

	region, regionSource := resolveDoctorRuntimeValue(
		runtimeRegion,
		envRegion,
		profile.Region,
		projectCfg.Region,
		isDynamic,
		staticUsesEnvIdentity,
	)
	endpoint, endpointSource := resolveDoctorRuntimeValue(
		runtimeEndpoint,
		envEndpoint,
		profile.Endpoint,
		projectCfg.Endpoint,
		isDynamic,
		staticUsesEnvIdentity,
	)

	timeoutSeconds := 0
	timeoutSource := ""
	if !staticUsesEnvIdentity && profile.TimeoutSeconds > 0 {
		timeoutSeconds = profile.TimeoutSeconds
		timeoutSource = "profile"
	} else if projectCfg.TimeoutSeconds > 0 {
		timeoutSeconds = projectCfg.TimeoutSeconds
		timeoutSource = "project"
	} else {
		timeoutSeconds = 60
		timeoutSource = "default"
	}

	credentialPresentCheck := map[string]any{
		"name":   "credentials_present",
		"ok":     credStatus.Present,
		"detail": credStatus.Source,
	}
	// Workload modes use source_ready as the readiness gate, not
	// credentials_present. Static/SSO/Console keep the credential check.
	includeCredentialCheck := !isDynamic || config.IsCachedLoginAuthMode(mode)
	checks := []map[string]any{
		{"name": "config_load", "ok": true, "detail": cfgPath},
	}
	if includeCredentialCheck {
		checks = append(checks, credentialPresentCheck)
	}
	checks = append(checks,
		map[string]any{"name": "region_present", "ok": strings.TrimSpace(region) != "", "detail": regionSource},
		map[string]any{"name": "endpoint_present", "ok": strings.TrimSpace(endpoint) != "", "detail": endpointSource},
	)

	return &doctorRuntimeState{
		cfg:                    cfg,
		cfgPath:                cfgPath,
		profile:                profile,
		profileName:            profileName,
		profileSource:          profileSource,
		mode:                   mode,
		isDynamic:              isDynamic,
		dynamicStatus:          dynamicStatus,
		dynamicReader:          dynamicReader,
		credStatus:             credStatus,
		credMode:               credStatus.Mode,
		credSource:             credStatus.Source,
		credPresent:            credStatus.Present,
		akPresent:              credStatus.AK,
		skPresent:              credStatus.SK,
		tokenPresent:           credStatus.Token,
		region:                 region,
		regionSource:           regionSource,
		endpoint:               endpoint,
		endpointSource:         endpointSource,
		timeoutSeconds:         timeoutSeconds,
		timeoutSource:          timeoutSource,
		credentialPresentCheck: credentialPresentCheck,
		checks:                 checks,
		skewRisk:               "unknown",
		localNowMS:             localStartMS,
		endpointValid:          strings.TrimSpace(endpoint) != "",
	}, nil
}
