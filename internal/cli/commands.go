package cli

import (
	"errors"
	"sort"
	"strings"
)

func runCommands(ctx *Context, args []string) (any, error) {
	if hasHelp(args) {
		return nil, &usageError{Text: usageCommands(), ExitCode: 0}
	}
	group := ""
	action := ""
	for len(args) > 0 {
		switch args[0] {
		case "--group":
			if len(args) < 2 {
				return nil, errors.New("missing --group value")
			}
			group = args[1]
			args = args[2:]
		case "--action":
			if len(args) < 2 {
				return nil, errors.New("missing --action value")
			}
			action = args[1]
			args = args[2:]
		default:
			return nil, errors.New("unknown flag: " + args[0])
		}
	}
	doc, err := loadAPICapabilities()
	if err != nil {
		return nil, err
	}
	filtered, err := filterCapabilities(doc, group, action)
	if err != nil {
		return nil, err
	}
	if len(filtered.Commands) == 0 {
		return "No commands matched.\n", nil
	}
	grouped := map[string][]apiCapabilityCommand{}
	for _, c := range filtered.Commands {
		g := normalizeToken(c.Group)
		grouped[g] = append(grouped[g], c)
	}
	groups := make([]string, 0, len(grouped))
	for g := range grouped {
		groups = append(groups, g)
	}
	sort.Strings(groups)
	var b strings.Builder
	for _, g := range groups {
		b.WriteString(g)
		b.WriteString(":\n")
		cmds := grouped[g]
		sortCapabilities(cmds)
		for _, c := range cmds {
			b.WriteString("  - ")
			b.WriteString(c.Action)
			b.WriteString("  ")
			b.WriteString(strings.ToUpper(strings.TrimSpace(c.Method)))
			b.WriteString(" ")
			b.WriteString(strings.TrimSpace(c.Path))
			if s := strings.TrimSpace(c.Summary); s != "" {
				b.WriteString("  # ")
				b.WriteString(s)
			}
			b.WriteString("\n")
		}
	}
	return b.String(), nil
}
