//go:build human

package cli

import (
	"context"
	"errors"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
	"github.com/volcengine-tls/ve-tls-cli/internal/execution"
)

type shortcutExecutionRequest struct {
	OperationID   contract.OperationID
	Input         execution.Input
	PageAll       bool
	LegacyPageAll *legacyPageAllPolicy
}

type legacyPageAllPolicy struct {
	ListField          string
	ForceTotal         bool
	PaginationOverride *contract.PaginationSpec
}

func executeShortcutOperation(ctx *Context, request shortcutExecutionRequest) (any, error) {
	if ctx == nil {
		return nil, errors.New("missing cli context")
	}
	operation, ok := loadToolOperation(string(request.OperationID))
	if !ok {
		return nil, errors.New("operation metadata is unavailable for shortcut: " + string(request.OperationID))
	}
	if request.LegacyPageAll != nil && request.LegacyPageAll.PaginationOverride != nil {
		pagination := *request.LegacyPageAll.PaginationOverride
		operation.Pagination = &pagination
	}
	codecs, err := newToolExecutionCodecRegistry(ctx)
	if err != nil {
		return nil, err
	}
	runtime := execution.RuntimeView{}
	if ctx.DryRun {
		runtime = buildToolExecutionRuntimeView(ctx)
	}
	result, executeErr := execution.NewExecutor(
		newContextExecutionTransport(ctx),
		codecs,
	).Execute(context.Background(), execution.Invocation{
		Operation: operation,
		Input:     request.Input,
		Options: execution.Options{
			DryRun:           ctx.DryRun,
			PageAll:          request.PageAll,
			ValidationPolicy: execution.ValidationCallerLegacy,
		},
		Runtime: runtime,
	})
	applyToolExecutionResult(ctx, result)
	if executeErr != nil {
		return nil, adaptShortcutExecutionError(executeErr)
	}
	if result.Plan != nil {
		if ctx != nil {
			ctx.traceToolExecutionPlan(result.Plan)
		}
		if request.PageAll && request.LegacyPageAll != nil {
			return nil, errors.New("unexpected list field: " + request.LegacyPageAll.ListField)
		}
		return shortcutExecutionPlanValue(result.Plan)
	}
	if request.PageAll && request.LegacyPageAll != nil && request.LegacyPageAll.ForceTotal {
		if data, ok := result.Data.(map[string]any); ok {
			if items, ok := toAnySlice(data[request.LegacyPageAll.ListField]); ok {
				data["Total"] = len(items)
			}
		}
	}
	return result.Data, nil
}

func shortcutExecutionPlanValue(plan *execution.DryRunPlan) (map[string]any, error) {
	value, err := toolExecutionPlanValue(plan)
	if err != nil || value == nil {
		return value, err
	}
	checks, ok := value["checks"].([]any)
	if !ok {
		return value, nil
	}
	legacyChecks := make([]map[string]any, 0, len(checks))
	for _, item := range checks {
		check, ok := item.(map[string]any)
		if !ok {
			return value, nil
		}
		legacyChecks = append(legacyChecks, check)
	}
	value["checks"] = legacyChecks
	return value, nil
}

func adaptShortcutExecutionError(err error) error {
	if err == nil {
		return nil
	}
	var httpSource *execution.HTTPError
	if errors.As(err, &httpSource) && httpSource != nil {
		return adaptToolExecutionError(err)
	}
	switch err.Error() {
	case "--page-all cannot be used with PageNumber":
		return errors.New("--all cannot be used with PageNumber")
	case "--page-all cannot be used with Cursor":
		return errors.New("--all cannot be used with Cursor")
	default:
		return adaptToolExecutionError(err)
	}
}

func shortcutQueryInput(query map[string]string) map[string]any {
	out := make(map[string]any, len(query))
	for key, value := range query {
		out[key] = value
	}
	return out
}

func shortcutJSONBodyInput(value map[string]any) execution.Payload {
	return execution.Payload{
		JSON:    value,
		Format:  execution.BodyFormatJSON,
		Present: true,
	}
}

func shortcutEmptyJSONBodyInput() execution.Payload {
	return shortcutJSONBodyInput(map[string]any{})
}

func toAnySlice(value any) ([]any, bool) {
	switch items := value.(type) {
	case []any:
		return items, true
	case []map[string]any:
		out := make([]any, 0, len(items))
		for _, item := range items {
			out = append(out, item)
		}
		return out, true
	default:
		return nil, false
	}
}
