package cli

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
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
	var (
		dir   string
		names []string
		force bool
	)
	for len(args) > 0 {
		switch args[0] {
		case "--dir":
			if len(args) < 2 {
				return nil, errors.New("missing --dir value")
			}
			dir = args[1]
			args = args[2:]
		case "--name":
			if len(args) < 2 {
				return nil, errors.New("missing --name value")
			}
			names = append(names, strings.TrimSpace(args[1]))
			args = args[2:]
		case "--force":
			force = true
			args = args[1:]
		default:
			return nil, errors.New("unknown flag: " + args[0])
		}
	}
	if strings.TrimSpace(dir) == "" {
		return nil, &usageError{Text: usageSkill(), ExitCode: 1}
	}
	root, err := bundledskills.Root()
	if err != nil {
		return nil, err
	}
	selected, err := selectBundledSkillNames(names)
	if err != nil {
		return nil, err
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return nil, err
	}
	installed := make([]string, 0, len(selected))
	for _, name := range selected {
		if err := installOneBundledSkill(root, absDir, name, force); err != nil {
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

func selectBundledSkillNames(names []string) ([]string, error) {
	available, err := bundledskills.List()
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return available, nil
	}
	index := make(map[string]struct{}, len(available))
	for _, name := range available {
		index[name] = struct{}{}
	}
	selected := make([]string, 0, len(names))
	seen := map[string]struct{}{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := index[name]; !ok {
			return nil, errors.New("unknown bundled skill: " + name)
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		selected = append(selected, name)
	}
	sort.Strings(selected)
	return selected, nil
}

func installOneBundledSkill(root fs.FS, destDir, name string, force bool) error {
	skillDir := filepath.Join(destDir, name)
	if _, err := os.Stat(skillDir); err == nil {
		if !force {
			return errors.New("skill already exists: " + skillDir + " (use --force to overwrite)")
		}
		if err := os.RemoveAll(skillDir); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	sub, err := fs.Sub(root, name)
	if err != nil {
		return err
	}
	return fs.WalkDir(sub, ".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		target := filepath.Join(skillDir, path)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := fs.ReadFile(sub, path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}
