//go:build human

package cli

import (
	"sort"
	"strings"
)

func relatedShortcutDescribesForAPI(group string, action string) []string {
	ng := normalizeToken(group)
	na := normalizeActionToken(action)
	if ng == "" || na == "" {
		return nil
	}
	specs := shortcutSpecsForGroup(ng)
	out := make([]string, 0, len(specs))
	for _, spec := range specs {
		if spec.HiddenInHelp {
			continue
		}
		if normalizeToken(spec.APIGroup) != ng {
			continue
		}
		if normalizeActionToken(spec.APIAction) != na {
			continue
		}
		out = append(out, "volclog "+spec.Group+" "+spec.Command+" --describe")
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func relatedShortcutLabelsForAPI(group string, action string) []string {
	describes := relatedShortcutDescribesForAPI(group, action)
	if len(describes) == 0 {
		return nil
	}
	out := make([]string, 0, len(describes))
	for _, cmd := range describes {
		cmd = strings.TrimPrefix(strings.TrimSpace(cmd), "volclog ")
		cmd = strings.TrimSuffix(cmd, " --describe")
		out = append(out, cmd)
	}
	return out
}

func relatedShortcutDescribesForShortcut(group string, command string) []string {
	specs := shortcutSpecsForGroup(group)
	if len(specs) == 0 {
		return nil
	}
	current := normalizeToken(command)
	out := make([]string, 0, len(specs))
	for _, spec := range specs {
		if spec.HiddenInHelp {
			continue
		}
		if normalizeToken(spec.Command) == current {
			continue
		}
		out = append(out, "volclog "+spec.Group+" "+spec.Command+" --describe")
		if len(out) >= 3 {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func defaultShortcutDescribeForGroup(group string) string {
	specs := shortcutSpecsForGroup(group)
	if len(specs) == 0 {
		return ""
	}
	for _, spec := range specs {
		if spec.HiddenInHelp {
			continue
		}
		return "volclog " + spec.Group + " " + spec.Command + " --describe"
	}
	return ""
}

func groupAgentEntry(group string) (string, string) {
	if cmd := defaultShortcutDescribeForGroup(group); cmd != "" {
		return "shortcut", cmd
	}
	group = normalizeToken(group)
	if group == "" {
		return "", ""
	}
	return "api", "volclog capabilities --group " + group + " --view text"
}

func shortcutSpecsForGroup(group string) []shortcutCommandSpec {
	ng := normalizeToken(group)
	if ng == "" {
		return nil
	}
	specs := make([]shortcutCommandSpec, 0, 8)
	for _, spec := range shortcutSpecs() {
		if normalizeToken(spec.Group) != ng {
			continue
		}
		specs = append(specs, spec)
	}
	sort.Slice(specs, func(i, j int) bool {
		li := shortcutCommandPriority(specs[i].Command)
		lj := shortcutCommandPriority(specs[j].Command)
		if li != lj {
			return li < lj
		}
		return specs[i].Command < specs[j].Command
	})
	return specs
}

func shortcutCommandPriority(command string) int {
	switch normalizeToken(command) {
	case "list":
		return 10
	case "get":
		return 20
	case "search":
		return 30
	case "histogram":
		return 35
	case "context":
		return 36
	case "put":
		return 37
	case "ingest":
		return 38
	case "bind-rules":
		return 40
	case "unbind-rules":
		return 41
	case "delete-host":
		return 42
	case "bind-host-groups":
		return 43
	case "unbind-host-groups":
		return 44
	case "describe-session-answer":
		return 45
	case "create":
		return 50
	case "modify":
		return 60
	case "delete":
		return 70
	case "export":
		return 80
	case "export-analysis":
		return 90
	default:
		return 100
	}
}
