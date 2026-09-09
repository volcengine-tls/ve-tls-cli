package cli

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/upgrade"
	"github.com/volcengine-tls/ve-tls-cli/internal/version"
)

type upgradeOptions struct {
	check   bool
	version string
	yes     bool
}

func runUpgrade(ctx *Context, args []string) (any, error) {
	ctx.Action = "upgrade"
	options, err := parseUpgradeOptions(args)
	if err != nil {
		return nil, err
	}
	installation, err := upgrade.DetectInstallation()
	if err != nil {
		return nil, err
	}
	manager := upgrade.NewManager(version.Version, string(currentEdition()), installation)
	runContext, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	return manager.Run(runContext, options.version, options.yes)
}

func parseUpgradeOptions(args []string) (upgradeOptions, error) {
	var options upgradeOptions
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "-h", "--help":
			return upgradeOptions{}, &usageError{Text: usageUpgrade(), ExitCode: 0}
		case "--check":
			options.check = true
		case "--version":
			if index+1 >= len(args) {
				return upgradeOptions{}, errors.New("missing --version value")
			}
			index++
			options.version = strings.TrimSpace(args[index])
			if options.version == "" {
				return upgradeOptions{}, errors.New("missing --version value")
			}
		case "--yes":
			options.yes = true
		default:
			return upgradeOptions{}, errors.New("unknown flag: " + args[index])
		}
	}
	if options.check && options.yes {
		return upgradeOptions{}, errors.New("--check cannot be combined with --yes")
	}
	return options, nil
}
