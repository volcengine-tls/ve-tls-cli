package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type runtimeSelectorSet struct {
	GlobalProfile      string
	GlobalSecretsFile  string
	ContextProfile     string
	ContextSecretsFile string
}

type resolvedRuntimeSelectors struct {
	Profile     string
	SecretsFile string
}

type secretsFileError struct {
	message string
	cause   error
}

func (e *secretsFileError) Error() string {
	if e == nil {
		return ""
	}
	return strings.TrimSpace(e.message)
}

func (e *secretsFileError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func runtimeSelectorConflict(left string, right string) error {
	return errors.New("conflicting runtime selectors: " + strings.TrimSpace(left) + " conflicts with " + strings.TrimSpace(right))
}

func profileSelector(flag string, value string) string {
	return strings.TrimSpace(flag) + "=" + strings.TrimSpace(value)
}

func secretsFileSelector(flag string, value string) string {
	return strings.TrimSpace(flag) + "=" + strings.TrimSpace(value)
}

func resolveRuntimeSelectors(spec runtimeSelectorSet) (resolvedRuntimeSelectors, error) {
	globalProfile := strings.TrimSpace(spec.GlobalProfile)
	globalSecretsFile := strings.TrimSpace(spec.GlobalSecretsFile)
	contextProfile := strings.TrimSpace(spec.ContextProfile)
	contextSecretsFile := strings.TrimSpace(spec.ContextSecretsFile)

	if globalProfile != "" && globalSecretsFile != "" {
		return resolvedRuntimeSelectors{}, runtimeSelectorConflict(profileSelector("global --profile", globalProfile), secretsFileSelector("global --secrets-file", globalSecretsFile))
	}
	if contextProfile != "" && contextSecretsFile != "" {
		return resolvedRuntimeSelectors{}, runtimeSelectorConflict(profileSelector("context.profile", contextProfile), secretsFileSelector("context.secrets_file", contextSecretsFile))
	}
	if globalProfile != "" && contextSecretsFile != "" {
		return resolvedRuntimeSelectors{}, runtimeSelectorConflict(profileSelector("global --profile", globalProfile), secretsFileSelector("context.secrets_file", contextSecretsFile))
	}
	if contextProfile != "" && globalSecretsFile != "" {
		return resolvedRuntimeSelectors{}, runtimeSelectorConflict(secretsFileSelector("global --secrets-file", globalSecretsFile), profileSelector("context.profile", contextProfile))
	}
	if globalSecretsFile != "" && contextSecretsFile != "" {
		return resolvedRuntimeSelectors{}, runtimeSelectorConflict(secretsFileSelector("global --secrets-file", globalSecretsFile), secretsFileSelector("context.secrets_file", contextSecretsFile))
	}
	if globalProfile != "" && contextProfile != "" && globalProfile != contextProfile {
		return resolvedRuntimeSelectors{}, errors.New("conflicting profile selectors: global --profile=" + globalProfile + " conflicts with context.profile=" + contextProfile)
	}

	resolved := resolvedRuntimeSelectors{}
	if contextProfile != "" {
		resolved.Profile = contextProfile
	} else {
		resolved.Profile = globalProfile
	}
	if contextSecretsFile != "" {
		resolved.SecretsFile = contextSecretsFile
	} else {
		resolved.SecretsFile = globalSecretsFile
	}
	return resolved, nil
}

func loadSecretsFile(path string) error {
	p := strings.TrimSpace(path)
	if p == "" {
		return &secretsFileError{message: "empty secrets file"}
	}
	b, err := os.ReadFile(filepath.Clean(p))
	if err != nil {
		return &secretsFileError{message: "failed to read secrets file: " + err.Error(), cause: err}
	}
	lines := strings.Split(string(b), "\n")
	supportedAssignments := 0
	for _, line := range lines {
		l := strings.TrimSpace(line)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		if strings.HasPrefix(l, "export ") {
			l = strings.TrimSpace(strings.TrimPrefix(l, "export "))
		}
		k, v, ok := strings.Cut(l, "=")
		if !ok {
			continue
		}
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		val := strings.TrimSpace(v)
		val = strings.Trim(val, "\"")
		val = strings.Trim(val, "'")
		if !isSupportedSecretsEnvKey(key) {
			continue
		}
		if err := os.Setenv(key, val); err != nil {
			return &secretsFileError{message: "failed to apply secrets file: " + err.Error(), cause: err}
		}
		supportedAssignments++
	}
	if supportedAssignments == 0 {
		return &secretsFileError{message: "secrets file does not contain any supported VOLCENGINE_* assignments"}
	}
	return nil
}

func isSupportedSecretsEnvKey(key string) bool {
	switch strings.TrimSpace(key) {
	case "VOLCENGINE_ACCESS_KEY_ID",
		"VOLCENGINE_ACCESS_KEY_SECRET",
		"VOLCENGINE_TOKEN",
		"VOLCENGINE_REGION",
		"VOLCENGINE_ENDPOINT":
		return true
	default:
		return false
	}
}
