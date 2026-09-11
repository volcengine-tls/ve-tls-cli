package cli

import (
	"context"
	"time"

	appruntime "github.com/volcengine-tls/ve-tls-cli/internal/app/runtime"
	"github.com/volcengine-tls/ve-tls-cli/internal/auth/console"
	"github.com/volcengine-tls/ve-tls-cli/internal/auth/sso"
	"github.com/volcengine-tls/ve-tls-cli/internal/config"
	"github.com/volcengine-tls/ve-tls-cli/internal/tlsapi"
)

type defaultAuthProviderFactory = appruntime.DefaultProviderFactory
type dynamicAuthError = appruntime.DynamicAuthError

func buildDynamicClient(
	mode, configPath, profileName string,
	cfg config.Config,
	profile config.Profile,
	factory authProviderFactory,
) (*tlsapi.Client, error) {
	return appruntime.BuildClient(appruntime.BuildClientRequest{
		Mode:        mode,
		ConfigPath:  configPath,
		ProfileName: profileName,
		Config:      cfg,
		Profile:     profile,
		Factory:     factory,
	})
}

func newDynamicAuthError(mode string, err error) *dynamicAuthError {
	return appruntime.NewDynamicAuthError(mode, err)
}

// These adapters preserve the existing CLI test fixtures while delegating all
// status semantics to the single runtime implementation.
type ssoStatusReader struct {
	cache       sso.Cache
	startURL    string
	sessionName string
	accountID   string
	roleName    string
	region      string
	clock       func() time.Time
}

func (r *ssoStatusReader) Status(ctx context.Context, profileName string) (authStatus, error) {
	if r == nil {
		return appruntime.NewSSOAuthStatusReader(appruntime.SSOAuthStatusReaderConfig{}).Status(ctx, profileName)
	}
	return appruntime.NewSSOAuthStatusReader(appruntime.SSOAuthStatusReaderConfig{
		Cache:       r.cache,
		StartURL:    r.startURL,
		SessionName: r.sessionName,
		AccountID:   r.accountID,
		RoleName:    r.roleName,
		Region:      r.region,
		Clock:       r.clock,
	}).Status(ctx, profileName)
}

type consoleStatusReader struct {
	cache        console.ConsoleCache
	loginSession string
	clock        func() time.Time
}

func (r *consoleStatusReader) Status(ctx context.Context, profileName string) (authStatus, error) {
	if r == nil {
		return appruntime.NewConsoleAuthStatusReader(appruntime.ConsoleAuthStatusReaderConfig{}).Status(ctx, profileName)
	}
	return appruntime.NewConsoleAuthStatusReader(appruntime.ConsoleAuthStatusReaderConfig{
		Cache:        r.cache,
		LoginSession: r.loginSession,
		Clock:        r.clock,
	}).Status(ctx, profileName)
}
