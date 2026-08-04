package runtime

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/config"
)

// SelectorSet contains the profile and secrets-file selectors accepted at the
// global CLI and operation-context layers.
type SelectorSet struct {
	GlobalProfile      string
	GlobalSecretsFile  string
	ContextProfile     string
	ContextSecretsFile string
}

// ResolvedSelectors is the single runtime selector pair after conflict checks.
type ResolvedSelectors struct {
	Profile     string
	SecretsFile string
}

// ResolveSelectors preserves the CLI's selector conflict rules and error text.
func ResolveSelectors(spec SelectorSet) (ResolvedSelectors, error) {
	globalProfile := strings.TrimSpace(spec.GlobalProfile)
	globalSecretsFile := strings.TrimSpace(spec.GlobalSecretsFile)
	contextProfile := strings.TrimSpace(spec.ContextProfile)
	contextSecretsFile := strings.TrimSpace(spec.ContextSecretsFile)

	if globalProfile != "" && globalSecretsFile != "" {
		return ResolvedSelectors{}, runtimeSelectorConflict(profileSelector("global --profile", globalProfile), secretsFileSelector("global --secrets-file", globalSecretsFile))
	}
	if contextProfile != "" && contextSecretsFile != "" {
		return ResolvedSelectors{}, runtimeSelectorConflict(profileSelector("context.profile", contextProfile), secretsFileSelector("context.secrets_file", contextSecretsFile))
	}
	if globalProfile != "" && contextSecretsFile != "" {
		return ResolvedSelectors{}, runtimeSelectorConflict(profileSelector("global --profile", globalProfile), secretsFileSelector("context.secrets_file", contextSecretsFile))
	}
	if contextProfile != "" && globalSecretsFile != "" {
		return ResolvedSelectors{}, runtimeSelectorConflict(secretsFileSelector("global --secrets-file", globalSecretsFile), profileSelector("context.profile", contextProfile))
	}
	if globalSecretsFile != "" && contextSecretsFile != "" {
		return ResolvedSelectors{}, runtimeSelectorConflict(secretsFileSelector("global --secrets-file", globalSecretsFile), secretsFileSelector("context.secrets_file", contextSecretsFile))
	}
	if globalProfile != "" && contextProfile != "" && globalProfile != contextProfile {
		return ResolvedSelectors{}, errors.New("conflicting profile selectors: global --profile=" + globalProfile + " conflicts with context.profile=" + contextProfile)
	}

	resolved := ResolvedSelectors{Profile: globalProfile, SecretsFile: globalSecretsFile}
	if contextProfile != "" {
		resolved.Profile = contextProfile
	}
	if contextSecretsFile != "" {
		resolved.SecretsFile = contextSecretsFile
	}
	return resolved, nil
}

func runtimeSelectorConflict(left, right string) error {
	return errors.New("conflicting runtime selectors: " + strings.TrimSpace(left) + " conflicts with " + strings.TrimSpace(right))
}

func profileSelector(flag, value string) string {
	return strings.TrimSpace(flag) + "=" + strings.TrimSpace(value)
}

func secretsFileSelector(flag, value string) string {
	return strings.TrimSpace(flag) + "=" + strings.TrimSpace(value)
}

// SecretsFileError identifies a failure to read or apply a secrets file while
// retaining the original cause for errors.Is/errors.As.
type SecretsFileError struct {
	message string
	cause   error
}

func (e *SecretsFileError) Error() string {
	if e == nil {
		return ""
	}
	return strings.TrimSpace(e.message)
}

func (e *SecretsFileError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

var supportedSecretsEnvKeys = []string{
	"VOLCENGINE_ACCESS_KEY_ID",
	"VOLCENGINE_ACCESS_KEY_SECRET",
	"VOLCENGINE_TOKEN",
	"VOLCENGINE_REGION",
	"VOLCENGINE_ENDPOINT",
}

// LoadSecretsFile applies supported VOLCENGINE_* assignments to the process
// environment. The side effect intentionally matches the existing CLI path.
func LoadSecretsFile(path string) error {
	p := strings.TrimSpace(path)
	if p == "" {
		return &SecretsFileError{message: "empty secrets file"}
	}
	data, err := os.ReadFile(filepath.Clean(p))
	if err != nil {
		return &SecretsFileError{message: "failed to read secrets file: " + err.Error(), cause: err}
	}
	assignments := 0
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if !isSupportedSecretsEnvKey(key) {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), "\"")
		value = strings.Trim(value, "'")
		if err := os.Setenv(key, value); err != nil {
			return &SecretsFileError{message: "failed to apply secrets file: " + err.Error(), cause: err}
		}
		assignments++
	}
	if assignments == 0 {
		return &SecretsFileError{message: "secrets file does not contain any supported VOLCENGINE_* assignments"}
	}
	return nil
}

func isSupportedSecretsEnvKey(key string) bool {
	key = strings.TrimSpace(key)
	for _, supported := range supportedSecretsEnvKeys {
		if key == supported {
			return true
		}
	}
	return false
}

// ResolveRequest contains immutable inputs for resolving one runtime profile.
type ResolveRequest struct {
	Config          config.Config
	ExplicitProfile string
	Defaults        config.ProfileDefaults
	RuntimeRegion   string
	RuntimeEndpoint string
	ForceStatic     bool
}

// Resolution is the normalized profile and auth mode selected for one
// invocation.
type Resolution struct {
	ProfileName string
	Profile     config.Profile
	Mode        string
	Dynamic     bool
	ForceStatic bool
}

// Resolver resolves process-environment precedence through an injectable
// lookup. A nil LookupEnv uses os.Getenv.
type Resolver struct {
	LookupEnv func(string) string
}

// Resolve uses the process environment.
func Resolve(request ResolveRequest) (Resolution, error) {
	return Resolver{}.Resolve(request)
}

// Resolve applies profile and runtime precedence without mutating Config.
func (r Resolver) Resolve(request ResolveRequest) (Resolution, error) {
	lookup := r.LookupEnv
	if lookup == nil {
		lookup = os.Getenv
	}
	name := request.Config.SelectedProfileName(request.ExplicitProfile)
	profile, found := request.Config.GetProfile(name)

	envAK := strings.TrimSpace(lookup("VOLCENGINE_ACCESS_KEY_ID"))
	envSK := strings.TrimSpace(lookup("VOLCENGINE_ACCESS_KEY_SECRET"))
	envToken := strings.TrimSpace(lookup("VOLCENGINE_TOKEN"))
	completeEnvCredential := envAK != "" && envSK != ""

	if !found && !completeEnvCredential {
		return Resolution{}, errors.New("profile not found: " + name)
	}

	mode := config.AuthModeAK
	if found && !request.ForceStatic {
		normalized, err := config.NormalizeAuthMode(profile.Mode)
		if err != nil {
			return Resolution{}, err
		}
		mode = normalized
	}
	dynamic := !request.ForceStatic && config.IsProviderAuthMode(mode)

	if !dynamic {
		if completeEnvCredential {
			// Preserve config.EffectiveProfile compatibility: a complete
			// environment credential pair creates an environment-only profile.
			// It must not inherit runtime or auth fields from the selected
			// profile.
			profile = config.Profile{
				AccessKeyID:     envAK,
				SecretAccessKey: envSK,
				SecurityToken:   envToken,
				Region:          strings.TrimSpace(lookup("VOLCENGINE_REGION")),
				Endpoint:        strings.TrimSpace(lookup("VOLCENGINE_ENDPOINT")),
			}
		} else if credRef := strings.TrimSpace(profile.CredRef); credRef != "" {
			credential, ok := request.Config.GetCred(credRef)
			if !ok {
				return Resolution{}, errors.New("credential not found: " + credRef)
			}
			if strings.TrimSpace(profile.AccessKeyID) == "" {
				profile.AccessKeyID = credential.AccessKeyID
			}
			if strings.TrimSpace(profile.SecretAccessKey) == "" {
				profile.SecretAccessKey = credential.SecretAccessKey
			}
		}
		profile = applyStaticRuntimeSettings(profile, request)
	} else {
		profile = applyDynamicRuntimeSettings(profile, request, lookup)
	}

	if strings.TrimSpace(profile.Region) == "" {
		return Resolution{}, errors.New("missing region")
	}
	if strings.TrimSpace(profile.Endpoint) == "" {
		return Resolution{}, errors.New("missing endpoint")
	}
	return Resolution{
		ProfileName: name,
		Profile:     profile,
		Mode:        mode,
		Dynamic:     dynamic,
		ForceStatic: request.ForceStatic,
	}, nil
}

func applyStaticRuntimeSettings(profile config.Profile, request ResolveRequest) config.Profile {
	profile.AccessKeyID = strings.TrimSpace(profile.AccessKeyID)
	profile.SecretAccessKey = strings.TrimSpace(profile.SecretAccessKey)
	profile.SecurityToken = strings.TrimSpace(profile.SecurityToken)
	profile.CredRef = strings.TrimSpace(profile.CredRef)
	profile.Region = firstNonEmpty(
		request.RuntimeRegion,
		profile.Region,
		request.Defaults.Region,
	)
	profile.Endpoint = firstNonEmpty(
		request.RuntimeEndpoint,
		profile.Endpoint,
		request.Defaults.Endpoint,
	)
	return applyTimeout(profile, request.Defaults)
}

func applyDynamicRuntimeSettings(profile config.Profile, request ResolveRequest, lookup func(string) string) config.Profile {
	profile.Region = firstNonEmpty(
		request.RuntimeRegion,
		lookup("VOLCENGINE_REGION"),
		profile.Region,
		request.Defaults.Region,
	)
	profile.Endpoint = firstNonEmpty(
		request.RuntimeEndpoint,
		lookup("VOLCENGINE_ENDPOINT"),
		profile.Endpoint,
		request.Defaults.Endpoint,
	)
	return applyTimeout(profile, request.Defaults)
}

func applyTimeout(profile config.Profile, defaults config.ProfileDefaults) config.Profile {
	if profile.TimeoutSeconds <= 0 {
		if defaults.TimeoutSeconds > 0 {
			profile.TimeoutSeconds = defaults.TimeoutSeconds
		} else {
			profile.TimeoutSeconds = 60
		}
	}
	return profile
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
