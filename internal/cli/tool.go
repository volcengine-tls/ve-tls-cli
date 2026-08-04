package cli

import (
	"errors"
	"sort"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
	"github.com/volcengine-tls/ve-tls-cli/internal/output"
)

func runTool(ctx *Context, args []string) (any, error) {
	if len(args) == 0 {
		return nil, &usageError{Text: usageTool(), ExitCode: 1}
	}

	command := args[0]
	if command == "-h" || command == "--help" {
		return nil, &usageError{Text: usageTool(), ExitCode: 0}
	}
	if strings.HasPrefix(command, "--") {
		return runToolList(ctx, args)
	}
	if len(args) == 1 {
		if _, _, ok := parseToolIdentity(command); ok {
			return runToolDescribe(ctx, args)
		}
	}

	switch command {
	case "list":
		return runToolList(ctx, args[1:])
	case "describe":
		return runToolDescribe(ctx, args[1:])
	case "exec":
		return runToolExec(ctx, args[1:])
	default:
		return nil, errors.New("unknown tool subcommand: " + args[0])
	}
}

func runToolList(ctx *Context, args []string) (any, error) {
	if hasHelp(args) {
		return nil, &usageError{Text: usageToolList(), ExitCode: 0}
	}

	group, verb, format, err := parseToolListArgs(args)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(group) != "" && !toolGroupExists(group) {
		return nil, errors.New("group not found: " + strings.TrimSpace(group))
	}
	if ctx != nil && format == "json" {
		ctx.FormatOverride = output.FormatJSON
	}
	tools := loadToolOperations(group, verb, "")
	if format == "json" {
		if strings.TrimSpace(group) != "" {
			return buildToolListJSONByGroup(tools, group, verb), nil
		}
		return buildToolListJSONGroups(tools, verb), nil
	}
	if strings.TrimSpace(group) != "" {
		return summarizeToolsForGroup(tools, group), nil
	}
	return summarizeTools(tools), nil
}

func parseToolListArgs(args []string) (group string, verb string, format string, err error) {
	group = ""
	verb = ""
	format = "text"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--verb":
			if i+1 >= len(args) {
				return "", "", "", errors.New("missing --verb value")
			}
			verb = args[i+1]
			i++
		case "--format":
			if i+1 >= len(args) {
				return "", "", "", errors.New("missing --format value")
			}
			format = strings.ToLower(strings.TrimSpace(args[i+1]))
			if format != "text" && format != "json" {
				return "", "", "", errors.New("invalid --format: " + strings.TrimSpace(args[i+1]))
			}
			i++
		default:
			if strings.HasPrefix(args[i], "--") {
				return "", "", "", errors.New("unknown flag: " + args[i])
			}
			if strings.TrimSpace(group) != "" {
				return "", "", "", errors.New("unexpected extra argument: " + args[i])
			}
			group = args[i]
		}
	}
	return group, verb, format, nil
}

func buildToolListJSONGroups(tools []contract.Operation, verb string) map[string]any {
	countByGroup := map[string]int{}
	for _, tool := range tools {
		group := strings.TrimSpace(tool.Group)
		if group == "" {
			continue
		}
		countByGroup[group]++
	}
	groups := make([]string, 0, len(countByGroup))
	for group := range countByGroup {
		groups = append(groups, group)
	}
	sort.Strings(groups)
	items := make([]map[string]any, 0, len(groups))
	for _, group := range groups {
		items = append(items, map[string]any{
			"group": group,
			"count": countByGroup[group],
		})
	}
	out := map[string]any{
		"groups": items,
	}
	if v := strings.TrimSpace(verb); v != "" {
		out["verb"] = v
	}
	return out
}

func buildToolListJSONByGroup(tools []contract.Operation, group, verb string) map[string]any {
	items := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		items = append(items, map[string]any{
			"id":      strings.TrimSpace(string(tool.ID)),
			"group":   strings.TrimSpace(tool.Group),
			"action":  strings.TrimSpace(tool.Action),
			"verb":    strings.TrimSpace(semanticToolVerb(tool)),
			"family":  strings.TrimSpace(tool.Family),
			"summary": strings.TrimSpace(tool.Docs.Summary),
			"method":  strings.TrimSpace(tool.Wire.Method),
			"path":    strings.TrimSpace(tool.Wire.Path),
		})
	}
	out := map[string]any{
		"group": strings.TrimSpace(group),
		"tools": items,
	}
	if v := strings.TrimSpace(verb); v != "" {
		out["verb"] = v
	}
	return out
}

func runToolDescribe(ctx *Context, args []string) (any, error) {
	if hasHelp(args) {
		return nil, &usageError{Text: usageToolDescribe(), ExitCode: 0}
	}
	identity, view, err := parseToolDescribeArgs(args)
	if err != nil {
		return nil, err
	}
	group, action, ok := parseToolIdentity(identity)
	if !ok {
		return nil, errors.New("invalid tool identity: must be <group.action>")
	}
	tool, err := resolveToolByIdentity(group, action)
	if err != nil {
		return nil, err
	}
	return buildToolDescribeOutput(tool, resolveToolDescribeView(ctx, view))
}

func parseToolDescribeArgs(args []string) (string, toolDescribeView, error) {
	identity := ""
	view := toolDescribeView("")
	for i := 0; i < len(args); i++ {
		token := strings.TrimSpace(args[i])
		switch token {
		case "--view":
			if i+1 >= len(args) {
				return "", "", errors.New("missing --view value")
			}
			view = toolDescribeView(strings.ToLower(strings.TrimSpace(args[i+1])))
			i++
		default:
			if strings.HasPrefix(token, "--") {
				return "", "", errors.New("unknown flag: " + token)
			}
			if identity != "" {
				return "", "", errors.New("unexpected extra arguments for tool describe")
			}
			identity = token
		}
	}
	if identity == "" {
		return "", "", errors.New("missing tool identity: <group.action>")
	}
	switch view {
	case "", toolDescribeViewCompact, toolDescribeViewFull:
		return identity, view, nil
	default:
		return "", "", errors.New("invalid --view: " + string(view))
	}
}

func resolveToolDescribeView(ctx *Context, requested toolDescribeView) toolDescribeView {
	if requested != "" {
		return requested
	}
	if ctx == nil {
		return toolDescribeViewFull
	}
	if ctx.OutputExplicit && ctx.Format == output.FormatJSON {
		return toolDescribeViewFull
	}
	return toolDescribeViewCompact
}

func parseToolIdentity(v string) (group string, action string, ok bool) {
	if strings.TrimSpace(v) == "" {
		return "", "", false
	}
	parts := strings.SplitN(v, ".", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	group = strings.TrimSpace(parts[0])
	action = strings.TrimSpace(parts[1])
	if group == "" || action == "" {
		return "", "", false
	}
	return group, action, true
}
