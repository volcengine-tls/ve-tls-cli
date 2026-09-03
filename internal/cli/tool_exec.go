package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
	"github.com/volcengine-tls/ve-tls-cli/internal/execution"
	"github.com/volcengine-tls/ve-tls-cli/internal/output"
)

func runToolExec(ctx *Context, args []string) (any, error) {
	if ctx == nil {
		return nil, errors.New("missing cli context")
	}
	if hasHelp(args) {
		return nil, &usageError{Text: usageToolExec(), ExitCode: 0}
	}
	if len(args) == 0 {
		return nil, errors.New("missing tool identity: <group.action>")
	}

	identity := strings.TrimSpace(args[0])
	group, action, ok := parseToolIdentity(identity)
	if !ok {
		return nil, errors.New("invalid tool identity: must be <group.action>")
	}

	contextArg := ""
	inputArg := ""
	pageAllFlag := false
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
		case "--page-all":
			pageAllFlag = true
		default:
			return nil, errors.New("unknown flag: " + args[i])
		}
	}

	operation, err := resolveToolByIdentity(group, action)
	if err != nil {
		return nil, err
	}
	ctx.Action = "tool." + strings.TrimSpace(string(operation.ID))
	ctxCfg := toolExecContext{Execution: map[string]any{}}
	if strings.TrimSpace(contextArg) != "" {
		ctxCfg, err = loadToolExecContext(contextArg)
		if err != nil {
			return nil, err
		}
	}
	rawInput := map[string]any{}
	if strings.TrimSpace(inputArg) != "" {
		rawInput, err = readToolJSONObjectFlag("--input", inputArg)
		if err != nil {
			return nil, err
		}
	}
	input, err := execution.NormalizeInput(operation, rawInput)
	if err != nil {
		return nil, err
	}
	if err := execution.ValidateInput(operation, input); err != nil {
		return nil, err
	}
	if err := applyToolExecContext(ctx, ctxCfg); err != nil {
		return nil, err
	}

	options := resolveToolExecutionOptions(ctxCfg)
	if strings.TrimSpace(options.OutputDir) != "" {
		ctx.OutputDir = strings.TrimSpace(options.OutputDir)
	}
	if pageAllFlag {
		options.PageAll = true
	}
	if options.DryRun {
		ctx.DryRun = true
	}
	options.DryRun = ctx.DryRun
	if options.Artifact {
		if strings.TrimSpace(ctx.Filter) != "" {
			return nil, errors.New("--jmes-filter cannot be combined with file delivery for tool exec")
		}
		ctx.OutputMode = "file"
		if strings.TrimSpace(options.ArtifactPath) != "" {
			ctx.OutputFile = strings.TrimSpace(options.ArtifactPath)
		}
		if err := preflightOutputFilePath(ctx.OutputFile, ctx.OutputDir, "tool", output.FormatJSON); err != nil {
			return nil, err
		}
	}
	compiledProjection, err := output.Compile(options.Projection)
	if err != nil {
		return nil, fmt.Errorf("invalid execution.projection: %w", err)
	}

	runtime := execution.RuntimeView{}
	if options.DryRun {
		runtime = buildToolExecutionRuntimeView(ctx)
	}
	codecs, err := newToolExecutionCodecRegistry(ctx)
	if err != nil {
		return nil, err
	}
	executor := execution.NewExecutor(
		newContextExecutionTransport(ctx),
		codecs,
	)
	executionResult, err := executor.Execute(context.Background(), execution.Invocation{
		Operation: operation,
		Input:     input,
		Options: execution.Options{
			DryRun:  options.DryRun,
			PageAll: options.PageAll,
		},
		Runtime: runtime,
	})
	applyToolExecutionResult(ctx, executionResult)
	if err != nil {
		return nil, adaptToolExecutionError(err)
	}
	result := executionResult.Data
	if executionResult.Plan != nil {
		ctx.traceToolExecutionPlan(executionResult.Plan)
		result, err = toolExecutionPlanValue(executionResult.Plan)
		if err != nil {
			return nil, err
		}
	}

	warnings := toolDigestWarnings(operation, ctxCfg.ContractDigest)
	filteredResult, err := applyCompiledToolExecFilter(result, compiledProjection)
	if err != nil {
		return nil, err
	}
	filteredResult = stabilizeProjectedToolResult(result, filteredResult, options.Projection)
	env, err := buildAPIEnvelope(ctx, "tool", filteredResult, ctx.OutputMode, ctx.OutputFile, ctx.Format)
	if err != nil {
		return nil, err
	}
	env, err = finalizeToolExecEnvelope(ctx, filteredResult, env, options)
	if err != nil {
		return nil, err
	}
	env["action"] = ctx.Action
	env["contract_digest"] = buildToolContractDigestStatus(operation, ctxCfg.ContractDigest)
	if len(warnings) > 0 {
		env["warnings"] = warnings
	}
	return env, nil
}

func applyCompiledToolExecFilter(result any, filter *output.CompiledFilter) (any, error) {
	if filter == nil {
		return result, nil
	}
	return filter.Apply(result)
}

func stabilizeProjectedToolResult(raw, filtered any, expressions ...string) any {
	if !hasToolProjectionExpression(expressions...) {
		return filtered
	}
	if !toolProjectionNeedsEnvelopeWrap(filtered) {
		return filtered
	}
	out := map[string]any{
		"items": filtered,
	}
	for key, value := range extractToolProjectionMetadata(raw) {
		out[key] = value
	}
	return out
}

func hasToolProjectionExpression(expressions ...string) bool {
	for _, expr := range expressions {
		if strings.TrimSpace(expr) != "" {
			return true
		}
	}
	return false
}

func toolProjectionNeedsEnvelopeWrap(v any) bool {
	switch v.(type) {
	case []any, []map[string]any:
		return true
	default:
		return false
	}
}

func extractToolProjectionMetadata(raw any) map[string]any {
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	out := map[string]any{}
	for _, key := range []string{"Total", "Count", "HasMore", "ListOver", "Cursor", "NextToken", "PrevToken", "PageNumber", "PageSize"} {
		if value, ok := obj[key]; ok {
			out[key] = value
		}
	}
	return out
}

func toolDigestWarnings(operation contract.Operation, expected string) []map[string]any {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return nil
	}
	actual := toolContractForDigest(operation)
	if strings.EqualFold(expected, actual) {
		return nil
	}
	return []map[string]any{
		{
			"kind":     "contract_digest_mismatch",
			"expected": expected,
			"actual":   actual,
			"policy":   "soft",
			"message":  "contract digest mismatch is advisory; execution continues",
		},
	}
}

func buildToolContractDigestStatus(operation contract.Operation, expected string) map[string]any {
	actual := toolContractForDigest(operation)
	status := map[string]any{
		"value":  actual,
		"policy": "soft",
	}
	if strings.TrimSpace(expected) != "" {
		status["expected"] = strings.TrimSpace(expected)
		status["matched"] = strings.EqualFold(strings.TrimSpace(expected), actual)
	}
	return status
}
