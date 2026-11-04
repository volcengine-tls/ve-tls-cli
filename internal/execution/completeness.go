package execution

import (
	"encoding/json"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
	"github.com/volcengine-tls/ve-tls-cli/internal/jsonx"
)

// Continuation describes an ordinary single-page tool invocation. Reuse the
// original runtime selectors and do not enable page-all for this invocation.
type Continuation struct {
	OperationID string         `json:"operationId"`
	Input       map[string]any `json:"input"`
	PageAll     bool           `json:"pageAll"`
}

type Completeness struct {
	Complete   bool          `json:"complete"`
	StopReason string        `json:"stopReason"`
	PageCount  int           `json:"pageCount"`
	ItemCount  int           `json:"itemCount"`
	Resumable  bool          `json:"resumable"`
	Next       *Continuation `json:"next,omitempty"`
}

// IncompleteError preserves usable data without replacing the current
// request's metadata with metadata from an earlier successful page.
type IncompleteError struct {
	Data    any
	State   *Completeness
	Cause   error
	Outcome string
	Kind    string
	Code    string
}

func (e *IncompleteError) Error() string {
	if e.State == nil {
		return "result completeness is unknown"
	}
	return "result incomplete: " + e.State.StopReason
}
func (e *IncompleteError) Unwrap() error                  { return e.Cause }
func (e *IncompleteError) FailureData() any               { return e.Data }
func (e *IncompleteError) CompletionState() *Completeness { return e.State }
func (e *IncompleteError) RemoteOutcome() string {
	if e.Outcome != "" {
		return e.Outcome
	}
	if e.State != nil && (e.State.PageCount > 0 || e.State.ItemCount > 0) {
		return OutcomePartial
	}
	return ErrorOutcome(e.Cause, OutcomeUnknown)
}
func (e *IncompleteError) ErrorKind() string {
	if e.Kind != "" {
		return e.Kind
	}
	if e.Cause != nil && (e.State == nil || e.State.PageCount == 0) {
		return ""
	}
	return "incomplete_result"
}
func (e *IncompleteError) ErrorCode() string {
	if e.Code != "" {
		return e.Code
	}
	if e.ErrorKind() == "" {
		return ""
	}
	return "IncompleteResult"
}

// ContinuationInput retains typed business inputs, never process credentials.
// Sensitive caller-supplied headers cannot be copied into result metadata.
func ContinuationInput(id contract.OperationID, input Input) *Continuation {
	for key := range input.Header {
		if sensitivePreviewKey(key) {
			return nil
		}
	}
	value := map[string]any{}
	for name, fields := range map[string]map[string]any{"path": input.Path, "query": input.Query, "header": input.Header} {
		if len(fields) > 0 {
			value[name] = fields
		}
	}
	if input.Body.JSON != nil {
		value["body"] = input.Body.JSON
	} else if len(input.Body.Raw) > 0 {
		var body any
		if input.Body.Format == BodyFormatJSONL || jsonx.Unmarshal(input.Body.Raw, &body) != nil {
			return nil
		}
		value["body"] = body
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var cloned map[string]any
	if jsonx.Unmarshal(raw, &cloned) != nil {
		return nil
	}
	return &Continuation{OperationID: string(id), Input: cloned, PageAll: false}
}
