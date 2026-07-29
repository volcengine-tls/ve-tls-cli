package cli

import (
	"errors"
	"strings"
)

func runConfigure(ctx *Context, args []string, ssoFactory ssoAdapterFactory) (any, error) {
	if err := ctx.LoadConfig(); err != nil {
		return nil, err
	}
	return runSubcommandGroup(args, usageConfigure(), nil, nil, func(command string, commandArgs []string) (any, error) {
		switch command {
		case "set":
			return configureSet(ctx, commandArgs)
		case "use":
			return configureUse(ctx, commandArgs)
		case "show":
			return configureShow(ctx, commandArgs)
		case "list":
			return configureList(ctx, commandArgs)
		case "delete":
			return configureDelete(ctx, commandArgs)
		case "profile":
			return runConfigureProfile(ctx, commandArgs)
		case "cred":
			return runConfigureCred(ctx, commandArgs)
		case "project":
			return runConfigureProject(ctx, commandArgs)
		case "sso-session":
			return runConfigureSSOSession(ctx, commandArgs)
		case "sso":
			factory := ssoFactory
			if factory == nil {
				factory = newProductionSSOAdapter
			}
			return runConfigureSSOWithFactory(ctx, commandArgs, factory)
		default:
			return nil, errors.New("unknown configure command: " + command)
		}
	})
}

func runConfigureProfile(ctx *Context, args []string) (any, error) {
	if len(args) == 0 {
		return nil, &usageError{Text: usageConfigure(), ExitCode: 1}
	}
	if args[0] == "-h" || args[0] == "--help" {
		return nil, &usageError{Text: usageConfigure(), ExitCode: 0}
	}
	switch args[0] {
	case "add":
		if len(args) < 2 {
			return nil, errors.New("missing profile name")
		}
		mapped := make([]string, 0, len(args)+1)
		mapped = append(mapped, "--profile", args[1])
		mapped = append(mapped, args[2:]...)
		return configureSet(ctx, mapped)
	case "use":
		if len(args) < 2 {
			return nil, errors.New("missing profile name")
		}
		return configureUse(ctx, []string{args[1]})
	case "show":
		if len(args) >= 2 && strings.TrimSpace(args[1]) != "" && !strings.HasPrefix(args[1], "-") {
			return configureShow(ctx, []string{"--profile", args[1]})
		}
		return configureShow(ctx, args[1:])
	case "list":
		return configureList(ctx, args[1:])
	case "delete":
		if len(args) >= 2 && strings.TrimSpace(args[1]) != "" && !strings.HasPrefix(args[1], "-") {
			return configureDelete(ctx, []string{args[1]})
		}
		return configureDelete(ctx, args[1:])
	default:
		return nil, errors.New("unknown configure profile command: " + args[0])
	}
}

func runConfigureCred(ctx *Context, args []string) (any, error) {
	if len(args) == 0 {
		return nil, &usageError{Text: usageConfigure(), ExitCode: 1}
	}
	if args[0] == "-h" || args[0] == "--help" {
		return nil, &usageError{Text: usageConfigure(), ExitCode: 0}
	}
	switch args[0] {
	case "delete":
		return configureCredDelete(ctx, args[1:])
	default:
		return nil, errors.New("unknown configure cred command: " + args[0])
	}
}
