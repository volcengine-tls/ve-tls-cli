package cli

import (
	"context"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
	"github.com/volcengine-tls/ve-tls-cli/internal/execution"
	"github.com/volcengine-tls/ve-tls-cli/internal/jsonx"
)

func (c *Context) recordSearchResult(method, path string, header map[string]string, body []byte, data any, err error) (any, error) {
	data, err = c.recordDecodedResult(data, err)
	if err != nil || strings.ToUpper(method) != "POST" || strings.SplitN(path, "?", 2)[0] != "/SearchLogs" {
		return data, err
	}
	input := execution.Input{Header: map[string]any{}}
	for name, value := range header {
		input.Header[name] = value
	}
	_ = jsonx.Unmarshal(body, &input.Body.JSON)
	return inspectSearchCompleteness(c, input, data)
}

// SearchLogs has two different completion contracts: ListOver applies only to
// pure search, while SQL analysis relies on ResultStatus.
func inspectSearchCompleteness(ctx *Context, input execution.Input, data any) (any, error) {
	response, ok := data.(map[string]any)
	if !ok {
		state := &execution.Completeness{PageCount: 1, StopReason: "unknown_completion"}
		if ctx != nil {
			ctx.Completeness, ctx.RemoteOutcome = state, execution.OutcomeUnknown
		}
		return data, &execution.IncompleteError{Data: data, State: state, Kind: "protocol", Code: "InvalidSearchResponse", Outcome: execution.OutcomeUnknown}
	}
	request, _ := input.Body.JSON.(map[string]any)
	query, _ := request["Query"].(string)
	analysis, _ := response["Analysis"].(bool)
	analysis = analysis || isAnalysisQuery(query)
	state := &execution.Completeness{PageCount: 1}
	if analysis {
		result, _ := response["AnalysisResult"].(map[string]any)
		state.ItemCount, _ = envelopeSliceLen(result["Data"])
	} else {
		state.ItemCount, _ = envelopeSliceLen(response["Logs"])
	}
	statusValue, hasStatus := response["ResultStatus"]
	status, statusString := statusValue.(string)
	status = strings.ToLower(strings.TrimSpace(status))
	nextContext, _ := response["Context"].(string)
	currentContext, _ := request["Context"].(string)
	setNext := func() {
		if analysis || strings.TrimSpace(nextContext) == "" || nextContext == currentContext {
			return
		}
		next := execution.ContinuationInput(contract.OperationID("log.search"), input)
		if next == nil {
			return
		}
		body, ok := next.Input["body"].(map[string]any)
		if !ok {
			return
		}
		body["Context"] = nextContext
		state.Next, state.Resumable = next, true
	}
	incomplete := func(reason, kind, code, outcome string) (any, error) {
		state.StopReason = reason
		if reason == "result_incomplete" || reason == "result_time_out" {
			setNext()
		}
		if ctx != nil {
			ctx.Completeness, ctx.RemoteOutcome = state, outcome
		}
		return data, &execution.IncompleteError{Data: data, State: state, Kind: kind, Code: code, Outcome: outcome}
	}
	switch status {
	case "incomplete":
		return incomplete("result_incomplete", "", "", execution.OutcomePartial)
	case "time_out":
		return incomplete("result_time_out", "", "", execution.OutcomePartial)
	case "error":
		outcome := execution.OutcomeFailed
		if state.ItemCount > 0 {
			outcome = execution.OutcomePartial
		}
		return incomplete("result_error", "server", "SearchResultError", outcome)
	case "complete", "":
	default:
		return incomplete("unknown_completion", "protocol", "UnknownSearchResultStatus", execution.OutcomeUnknown)
	}
	if hasStatus && !statusString {
		return incomplete("unknown_completion", "protocol", "InvalidSearchResultStatus", execution.OutcomeUnknown)
	}
	if analysis {
		if status != "complete" {
			return incomplete("unknown_completion", "", "", execution.OutcomePartial)
		}
		result, ok := response["AnalysisResult"].(map[string]any)
		if !ok {
			return incomplete("invalid_response", "protocol", "InvalidSearchResponse", execution.OutcomeUnknown)
		}
		if rows := result["Data"]; rows != nil {
			if _, ok := envelopeSliceLen(rows); !ok {
				return incomplete("invalid_response", "protocol", "InvalidSearchResponse", execution.OutcomeUnknown)
			}
		}
		state.Complete, state.StopReason = true, "result_complete"
	} else {
		if rows := response["Logs"]; rows != nil {
			if _, ok := envelopeSliceLen(rows); !ok {
				return incomplete("invalid_response", "protocol", "InvalidSearchResponse", execution.OutcomeUnknown)
			}
		}
		if over, ok := response["ListOver"].(bool); ok && over {
			state.Complete, state.StopReason = true, "list_over"
		} else if strings.TrimSpace(nextContext) != "" {
			if nextContext == currentContext {
				return incomplete("cursor_stalled", "", "", execution.OutcomePartial)
			}
			state.StopReason = "more_results"
			setNext()
		} else {
			reason := "unknown_completion"
			if over, ok := response["ListOver"].(bool); ok && !over {
				reason = "missing_cursor"
			}
			return incomplete(reason, "", "", execution.OutcomePartial)
		}
	}
	if ctx != nil {
		ctx.Completeness = state
	}
	return data, nil
}

type searchAwareJSONCodec struct {
	context  *Context
	fallback execution.Codec
}

type searchCodecState struct {
	input    execution.Input
	fallback any
}

func (c searchAwareJSONCodec) Encode(ctx context.Context, operation contract.Operation, request execution.Request) (execution.Request, any, error) {
	encoded, state, err := c.fallback.Encode(ctx, operation, request)
	if err != nil || operation.Wire.Path != "/SearchLogs" {
		return encoded, state, err
	}
	input := execution.Input{Header: map[string]any{}}
	for key, value := range request.Header {
		input.Header[key] = value
	}
	_ = jsonx.Unmarshal(request.Body, &input.Body.JSON)
	return encoded, searchCodecState{input: input, fallback: state}, nil
}

func (c searchAwareJSONCodec) Decode(ctx context.Context, operation contract.Operation, response execution.Response, state any) (any, error) {
	searchState, isSearch := state.(searchCodecState)
	if isSearch {
		state = searchState.fallback
	}
	data, err := c.fallback.Decode(ctx, operation, response, state)
	if err != nil || !isSearch {
		return data, err
	}
	return inspectSearchCompleteness(c.context, searchState.input, data)
}
