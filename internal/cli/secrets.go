package cli

import appruntime "github.com/volcengine-tls/ve-tls-cli/internal/app/runtime"

type runtimeSelectorSet = appruntime.SelectorSet
type resolvedRuntimeSelectors = appruntime.ResolvedSelectors
type secretsFileError = appruntime.SecretsFileError

func resolveRuntimeSelectors(spec runtimeSelectorSet) (resolvedRuntimeSelectors, error) {
	return appruntime.ResolveSelectors(spec)
}

func loadSecretsFile(path string) error {
	return appruntime.LoadSecretsFile(path)
}
