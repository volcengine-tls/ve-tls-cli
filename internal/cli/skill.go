package cli

import (
	"errors"
	"os"
	"sort"
	"strings"

	bundledskills "github.com/volcengine-tls/ve-tls-cli"
)

func runSkill(ctx *Context, args []string) (any, error) {
	return runSubcommandGroup(args, usageSkill(), nil, nil, func(command string, commandArgs []string) (any, error) {
		ctx.Action = "skill." + strings.TrimSpace(command)
		switch command {
		case "list":
			return skillList(commandArgs)
		case "install":
			return skillInstall(commandArgs)
		case "status":
			return skillStatus(commandArgs)
		case "update":
			return skillUpdate(commandArgs)
		case "uninstall":
			return skillUninstall(commandArgs)
		default:
			return nil, errors.New("unknown skill command: " + command)
		}
	})
}

func skillList(args []string) (any, error) {
	if len(args) > 0 {
		return nil, errors.New("unknown flag: " + args[0])
	}
	names, err := bundledskills.List()
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"Skills": names,
		"Total":  len(names),
	}, nil
}

func skillInstall(args []string) (any, error) {
	opts, err := parseSkillManageOptions(args, true)
	if err != nil {
		return nil, err
	}
	root, err := bundledskills.Root()
	if err != nil {
		return nil, err
	}
	selected, err := selectBundledSkillNames(opts.names)
	if err != nil {
		return nil, err
	}
	absDir, err := absoluteSkillDir(opts.dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return nil, err
	}
	installed := make([]string, 0, len(selected))
	for _, name := range selected {
		if err := installOneBundledSkill(root, absDir, name, opts.force); err != nil {
			return nil, err
		}
		installed = append(installed, name)
	}
	sort.Strings(installed)
	return map[string]any{
		"Dir":       absDir,
		"Installed": installed,
		"Total":     len(installed),
	}, nil
}
