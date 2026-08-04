package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/output"
)

func runWorkflow(ctx *Context, args []string) (any, error) {
	if len(args) == 0 {
		return nil, &usageError{Text: usageWorkflow(), ExitCode: 1}
	}

	command := strings.TrimSpace(args[0])
	if command == "-h" || command == "--help" {
		return nil, &usageError{Text: usageWorkflow(), ExitCode: 0}
	}
	if len(args) == 1 {
		if _, _, ok := parseToolIdentity(command); ok {
			return runWorkflowDescribe(ctx, args)
		}
	}

	switch command {
	case "list":
		return runWorkflowList(ctx, args[1:])
	case "describe":
		return runWorkflowDescribe(ctx, args[1:])
	case "exec":
		return runWorkflowExec(ctx, args[1:])
	default:
		return nil, errors.New("unknown workflow subcommand: " + command)
	}
}

func runWorkflowList(ctx *Context, args []string) (any, error) {
	if hasHelp(args) {
		return nil, &usageError{Text: usageWorkflowList(), ExitCode: 0}
	}
	group, format, err := parseWorkflowListArgs(args)
	if err != nil {
		return nil, err
	}
	if ctx != nil && format == "json" {
		ctx.FormatOverride = output.FormatJSON
	}
	items, err := workflowCatalogEntries(group)
	if err != nil {
		return nil, err
	}
	if format == "json" {
		if strings.TrimSpace(group) != "" {
			return buildWorkflowListJSONByGroup(items, group), nil
		}
		return buildWorkflowListJSONGroups(items), nil
	}
	if strings.TrimSpace(group) != "" {
		return summarizeWorkflowGroup(items), nil
	}
	return summarizeWorkflowGroups(items), nil
}

func parseWorkflowListArgs(args []string) (group string, format string, err error) {
	format = "text"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--format":
			if i+1 >= len(args) {
				return "", "", errors.New("missing --format value")
			}
			format = strings.ToLower(strings.TrimSpace(args[i+1]))
			if format != "text" && format != "json" {
				return "", "", errors.New("invalid --format: " + strings.TrimSpace(args[i+1]))
			}
			i++
		default:
			if strings.HasPrefix(args[i], "--") {
				return "", "", errors.New("unknown flag: " + args[i])
			}
			if strings.TrimSpace(group) != "" {
				return "", "", errors.New("unexpected extra argument: " + args[i])
			}
			group = args[i]
		}
	}
	return strings.TrimSpace(group), format, nil
}

func buildWorkflowListJSONGroups(items []workflowCatalog) map[string]any {
	counts := map[string]int{}
	for _, item := range items {
		counts[item.Group]++
	}
	groups := make([]string, 0, len(counts))
	for group := range counts {
		groups = append(groups, group)
	}
	sort.Strings(groups)
	out := make([]map[string]any, 0, len(groups))
	for _, group := range groups {
		out = append(out, map[string]any{
			"group": group,
			"count": counts[group],
		})
	}
	return map[string]any{"groups": out}
}

func buildWorkflowListJSONByGroup(items []workflowCatalog, group string) map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"id":                    item.ID,
			"group":                 item.Group,
			"command":               item.Command,
			"summary":               item.Summary,
			"method":                item.Method,
			"path":                  item.Path,
			"source":                item.Source,
			"preferred_output_mode": item.PreferredOutputMode,
		})
	}
	return map[string]any{
		"group":     group,
		"workflows": out,
	}
}

func summarizeWorkflowGroups(items []workflowCatalog) string {
	counts := map[string]int{}
	for _, item := range items {
		counts[item.Group]++
	}
	groups := make([]string, 0, len(counts))
	for group := range counts {
		groups = append(groups, group)
	}
	sort.Strings(groups)
	if len(groups) == 0 {
		return "No workflows matched.\n"
	}
	lines := make([]string, 0, len(groups))
	for _, group := range groups {
		lines = append(lines, fmt.Sprintf("  - %s (%d workflows)", group, counts[group]))
	}
	return strings.Join(lines, "\n") + "\n"
}

func summarizeWorkflowGroup(items []workflowCatalog) string {
	if len(items) == 0 {
		return "No workflows matched.\n"
	}
	lines := make([]string, 0, len(items))
	for _, item := range items {
		lines = append(lines, "  - "+item.ID)
	}
	return strings.Join(lines, "\n") + "\n"
}

func runWorkflowDescribe(ctx *Context, args []string) (any, error) {
	if hasHelp(args) {
		return nil, &usageError{Text: usageWorkflowDescribe(), ExitCode: 0}
	}
	if len(args) == 0 {
		return nil, errors.New("missing workflow identity: <group.command>")
	}
	group, command, ok := parseToolIdentity(args[0])
	if !ok {
		return nil, errors.New("invalid workflow identity: must be <group.command>")
	}
	spec, err := resolveWorkflowByIdentity(group, command)
	if err != nil {
		return nil, err
	}
	return workflowDescribeOutput(spec)
}

func runWorkflowExec(ctx *Context, args []string) (any, error) {
	if ctx == nil {
		return nil, errors.New("missing cli context")
	}
	if hasHelp(args) {
		return nil, &usageError{Text: usageWorkflowExec(), ExitCode: 0}
	}
	if len(args) == 0 {
		return nil, errors.New("missing workflow identity: <group.command>")
	}

	group, command, ok := parseToolIdentity(args[0])
	if !ok {
		return nil, errors.New("invalid workflow identity: must be <group.command>")
	}
	spec, err := resolveWorkflowByIdentity(group, command)
	if err != nil {
		return nil, err
	}
	ctx.Action = "workflow." + spec.ID

	contextArg := ""
	inputArg := ""
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--context":
			if i+1 >= len(args) {
				return nil, errors.New("missing --context value")
			}
			contextArg = args[i+1]
			i++
		case "--input":
			if i+1 >= len(args) {
				return nil, errors.New("missing --input value")
			}
			inputArg = args[i+1]
			i++
		default:
			return nil, errors.New("unknown flag: " + args[i])
		}
	}

	cfg := toolExecContext{Execution: map[string]any{}}
	if strings.TrimSpace(contextArg) != "" {
		cfg, err = loadToolExecContext(contextArg)
		if err != nil {
			return nil, err
		}
	}
	input := map[string]any{}
	if strings.TrimSpace(inputArg) != "" {
		input, err = readToolJSONObjectFlag("--input", inputArg)
		if err != nil {
			return nil, err
		}
	}
	if err := validateWorkflowExecInput(spec, input); err != nil {
		return nil, err
	}
	if err := applyToolExecContext(ctx, cfg); err != nil {
		return nil, err
	}

	options := resolveToolExecutionOptions(cfg)
	if strings.TrimSpace(options.OutputDir) != "" {
		ctx.OutputDir = strings.TrimSpace(options.OutputDir)
	}
	if options.DryRun {
		ctx.DryRun = true
	}
	if options.Artifact {
		if strings.TrimSpace(ctx.Filter) != "" {
			return nil, errors.New("--jmes-filter cannot be combined with file delivery for workflow exec")
		}
		ctx.OutputMode = "file"
		if strings.TrimSpace(options.ArtifactPath) != "" {
			ctx.OutputFile = strings.TrimSpace(options.ArtifactPath)
		}
		if err := preflightOutputFilePath(ctx.OutputFile, ctx.OutputDir, "workflow", output.FormatJSON); err != nil {
			return nil, err
		}
	}

	workflowArgs, cleanup, err := workflowExecArgs(spec, input)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	out, err := dispatchWorkflowExec(ctx, spec, workflowArgs)
	if err != nil {
		return nil, err
	}
	filtered, err := applyToolExecFilters(out, options.Projection)
	if err != nil {
		return nil, err
	}
	env, err := buildAPIEnvelope(ctx, "workflow", filtered, ctx.OutputMode, ctx.OutputFile, ctx.Format)
	if err != nil {
		return nil, err
	}
	env["action"] = ctx.Action
	env, err = finalizeWorkflowExecEnvelope(ctx, filtered, env, options)
	if err != nil {
		return nil, err
	}
	return env, nil
}

func validateWorkflowExecInput(spec workflowCatalog, input map[string]any) error {
	missing := make([]string, 0, 2)
	for _, param := range workflowParamsWithDoc(spec) {
		if !param.Required {
			continue
		}
		if _, ok := workflowInputValue(input, param.Name); ok {
			continue
		}
		missing = append(missing, workflowInputFieldName(param.Name))
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	return errors.New("workflow input missing required fields: " + strings.Join(missing, ", "))
}

func workflowExecArgs(spec workflowCatalog, input map[string]any) ([]string, func(), error) {
	args := make([]string, 0, len(spec.Params)*2)
	tempFiles := make([]string, 0, 1)
	cleanup := func() {
		for _, path := range tempFiles {
			_ = os.Remove(path)
		}
	}
	for _, param := range workflowParamsWithDoc(spec) {
		value, ok := workflowInputValue(input, param.Name)
		if !ok {
			continue
		}
		flags, err := workflowArgValues(param, value)
		if err != nil {
			cleanup()
			return nil, func() {}, err
		}
		for _, flag := range flags {
			if strings.HasPrefix(flag, "file://") && strings.Contains(flag, "volclog-workflow-") {
				tempFiles = append(tempFiles, strings.TrimPrefix(flag, "file://"))
			}
		}
		args = append(args, flags...)
	}
	return args, cleanup, nil
}

func workflowArgValues(param apiCapParam, value any) ([]string, error) {
	flag := strings.TrimSpace(param.CLIFlag)
	if flag == "" {
		return nil, nil
	}
	if strings.EqualFold(flag, "--request") {
		arg, err := workflowRequestArg(value)
		if err != nil {
			return nil, err
		}
		return []string{flag, arg}, nil
	}
	if strings.Contains(flag, "/") {
		parts := strings.Split(flag, "/")
		b, ok := value.(bool)
		if !ok {
			return nil, fmt.Errorf("workflow field %s expects boolean", workflowInputFieldName(param.Name))
		}
		if b {
			return []string{strings.TrimSpace(parts[0])}, nil
		}
		if len(parts) > 1 {
			return []string{strings.TrimSpace(parts[1])}, nil
		}
		return nil, nil
	}
	switch v := value.(type) {
	case bool:
		if v {
			return []string{flag}, nil
		}
		return nil, nil
	case []any:
		out := make([]string, 0, len(v)*2)
		for _, item := range v {
			out = append(out, flag, workflowScalarString(item))
		}
		return out, nil
	case []string:
		out := make([]string, 0, len(v)*2)
		for _, item := range v {
			out = append(out, flag, item)
		}
		return out, nil
	default:
		return []string{flag, workflowScalarString(v)}, nil
	}
}

func workflowRequestArg(value any) (string, error) {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v), nil
	default:
		path, err := workflowWriteTempJSON(v)
		if err != nil {
			return "", err
		}
		return "file://" + path, nil
	}
}

func workflowWriteTempJSON(v any) (string, error) {
	f, err := os.CreateTemp("", "volclog-workflow-*.json")
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := json.Marshal(v)
	if err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	if _, err := f.Write(b); err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

func workflowInputValue(input map[string]any, rawName string) (any, bool) {
	for _, key := range []string{workflowInputFieldName(rawName), rawName} {
		value, ok := input[key]
		if ok {
			return value, true
		}
	}
	return nil, false
}

func dispatchWorkflowExec(ctx *Context, spec workflowCatalog, args []string) (any, error) {
	switch spec.ID {
	case "log.ingest":
		return logIngest(ctx, args)
	case "log.export":
		return logExport(ctx, args)
	case "log.export-analysis":
		return logExportAnalysis(ctx, args)
	default:
		return nil, fmt.Errorf("workflow execution not implemented: %s", spec.ID)
	}
}

func finalizeWorkflowExecEnvelope(ctx *Context, result any, env map[string]any, options toolExecutionOptions) (map[string]any, error) {
	if ctx == nil || ctx.OutputMode != "stdout" || ctx.OutputModeExplicit || ctx.Format != output.FormatJSON || options.Artifact || strings.TrimSpace(options.Projection) != "" {
		return env, nil
	}
	size, err := estimateOutputBytes(env, ctx.Format)
	if err != nil || size <= toolExecAutoArtifactByteLimit {
		return env, nil
	}
	filePath, err := resolveOutputFilePath("", ctx.OutputDir, "workflow", output.FormatJSON)
	if err != nil {
		if errors.Is(err, errMissingWritableOutputDir) {
			return nil, errors.New("result too large for stdout; specify --output-dir <writable-dir> to allow automatic file delivery")
		}
		return nil, err
	}
	fileArtifact := []map[string]any{{
		"path":   filePath,
		"format": string(output.FormatJSON),
	}}
	fileEnv := newAPISuccessEnvelope(ctx, "workflow", result, ctx.OutputMode, "file_auto", fileArtifact)
	if err := materializeEnvelopeFile(filePath, fileEnv); err != nil {
		return nil, err
	}
	summary, _ := env["summary"].(map[string]any)
	if summary == nil {
		summary = map[string]any{}
		env["summary"] = summary
	}
	summary["deliveryMode"] = "file_auto"
	summary["truncated"] = true
	summary["autoArtifact"] = true
	summary["fullBytes"] = size
	summary["hint"] = "full result written to artifact; use execution.projection or output-mode file for deterministic control"
	summary["totalBytes"] = fileEnv["summary"].(map[string]any)["totalBytes"]
	env["artifacts"] = fileEnv["artifacts"]
	env["data"] = buildToolExecPreview(result)
	return env, nil
}

func workflowScalarString(v any) string {
	switch value := v.(type) {
	case float64:
		if value == float64(int64(value)) {
			return fmt.Sprintf("%d", int64(value))
		}
		return fmt.Sprintf("%v", value)
	default:
		return fmt.Sprint(value)
	}
}
