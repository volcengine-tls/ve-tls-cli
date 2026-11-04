package cli

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/execution"
	"github.com/volcengine-tls/ve-tls-cli/internal/jsonx"
)

func buildToolExecutionRuntimeView(ctx *Context) execution.RuntimeView {
	if ctx == nil {
		detail := "missing cli context"
		return execution.RuntimeView{
			Checks: []execution.PreflightCheck{{Name: "profile", OK: false, Detail: &detail}},
		}
	}
	if err := ctx.ResolveProfile(); err != nil {
		detail := err.Error()
		return execution.RuntimeView{
			Checks: []execution.PreflightCheck{{Name: "profile", OK: false, Detail: &detail}},
		}
	}
	return execution.RuntimeView{
		Region:   strings.TrimSpace(ctx.profile.Region),
		Endpoint: strings.TrimSpace(ctx.profile.Endpoint),
	}
}

func applyToolExecutionResult(ctx *Context, result execution.Result) {
	if ctx == nil {
		return
	}
	ctx.RequestID = strings.TrimSpace(result.RequestID)
	ctx.StatusCode = result.StatusCode
	ctx.RemoteOutcome = result.RemoteOutcome
	if ctx.RemoteOutcome == "" {
		ctx.RemoteOutcome = execution.OutcomeNotSent
	}
	ctx.PaginationMeta = nil
	if result.Pagination == nil {
		if result.Plan != nil {
			ctx.Completeness = nil
		}
		return
	}
	state := result.Pagination.Completeness
	ctx.Completeness = &state
	meta := map[string]any{
		"mode":       result.Pagination.Mode,
		"pageCount":  result.Pagination.PageCount,
		"merged":     result.Pagination.Merged,
		"complete":   state.Complete,
		"stopReason": state.StopReason,
		"itemCount":  state.ItemCount,
		"resumable":  state.Resumable,
	}
	if state.Next != nil {
		meta["next"] = state.Next
	}
	if result.Pagination.PageSize > 0 {
		meta["pageSize"] = result.Pagination.PageSize
	}
	ctx.PaginationMeta = meta
}

func adaptToolExecutionError(err error) error {
	if err == nil {
		return nil
	}
	if typed, _ := firstTypedOperationError(err); typed != nil {
		return err
	}
	var source *execution.HTTPError
	if !errors.As(err, &source) || source == nil {
		return err
	}
	return &httpError{
		statusCode: source.StatusCode,
		body:       append([]byte(nil), source.Body...),
		requestID:  strings.TrimSpace(source.RequestID),
	}
}

func toolExecutionPlanValue(plan *execution.DryRunPlan) (map[string]any, error) {
	if plan == nil {
		return nil, nil
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if err := jsonx.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}
