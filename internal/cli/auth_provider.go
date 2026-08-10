package cli

import (
	appruntime "github.com/volcengine-tls/ve-tls-cli/internal/app/runtime"
	"github.com/volcengine-tls/ve-tls-cli/internal/config"
)

type authStatus = appruntime.AuthStatus
type authStatusReader = appruntime.AuthStatusReader
type authProviderFactory = appruntime.ProviderFactory

func resolveSSOCacheDir(configPath string) string {
	return appruntime.ResolveSSOCacheDir(configPath)
}

func resolveLoginCacheDir(configPath string) string {
	return appruntime.ResolveLoginCacheDir(configPath)
}

func dynamicAuthStatusReader(
	mode, configPath, profileName string,
	cfg config.Config,
	factory authProviderFactory,
) (authStatusReader, error) {
	return appruntime.DynamicAuthStatusReader(mode, configPath, profileName, cfg, factory)
}
