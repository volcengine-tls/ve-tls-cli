package cli

import (
	"errors"

	"github.com/volcengine-tls/ve-tls-cli/internal/execution"
)

// exportResultError exposes streamed partial output without copying its rows
// back into memory. The underlying cause still carries transport diagnostics.
type exportResultError struct {
	*execution.IncompleteError
	Artifact *fileArtifactOutput
}

func (e *exportResultError) Error() string {
	var invalid *execution.Failure
	if errors.As(e.Cause, &invalid) && invalid.Code == "InvalidResponse" && invalid.Message != "" {
		return invalid.Error()
	}
	return e.IncompleteError.Error()
}

func (e *exportResultError) Unwrap() error { return e.IncompleteError }

func (e *exportResultError) FailureArtifact() (fileArtifactOutput, bool) {
	if e.Artifact == nil || e.Artifact.Path == "" {
		return fileArtifactOutput{}, false
	}
	return *e.Artifact, true
}

func (e *exportResultError) DeliveryExitCode() int {
	var delivery interface{ DeliveryExitCode() int }
	if errors.As(e.Cause, &delivery) {
		return delivery.DeliveryExitCode()
	}
	return 2
}

func (e *exportResultError) FailureStage() string {
	var staged interface{ FailureStage() string }
	if errors.As(e.Cause, &staged) {
		return staged.FailureStage()
	}
	return ""
}

type logExportResult struct {
	ctx    *Context
	writer *streamedLogFileWriter
	rows   []any
	state  execution.Completeness
}

func newLogExportResult(ctx *Context, writer *streamedLogFileWriter) *logExportResult {
	return &logExportResult{ctx: ctx, writer: writer, rows: make([]any, 0)}
}

func (r *logExportResult) writeRows(rows []any) error {
	if r.writer != nil {
		err := r.writer.WriteRows(rows)
		r.state.ItemCount = r.writer.RowsWritten()
		return err
	}
	r.rows = append(r.rows, rows...)
	r.state.ItemCount = len(r.rows)
	return nil
}

func (r *logExportResult) complete(reason string) (any, error) {
	r.state.Complete = true
	r.state.StopReason = reason
	return r.finish(nil, "", "")
}

func (r *logExportResult) fail(reason string, cause error, kind, code string, next *execution.Continuation) (any, error) {
	r.state.Complete = false
	r.state.StopReason = reason
	r.state.Next = next
	r.state.Resumable = next != nil
	if kind != "output_failed" {
		if r.state.PageCount > 0 {
			r.ctx.RemoteOutcome = execution.OutcomePartial
			if code == "SearchResultError" && r.state.PageCount == 1 && r.state.ItemCount == 0 {
				r.ctx.RemoteOutcome = execution.OutcomeFailed
			}
		} else if cause != nil {
			r.ctx.RemoteOutcome = execution.ErrorOutcome(cause, r.ctx.RemoteOutcome)
		}
	}
	return r.finish(cause, kind, code)
}

func (r *logExportResult) outputFailed(cause error) (any, error) {
	return r.fail("output_failed", postExecutionFailure(r.ctx, cause, "artifact"), "output_failed", "OutputFailed", nil)
}

func (r *logExportResult) finish(cause error, kind, code string) (any, error) {
	var artifact *fileArtifactOutput
	if r.writer != nil && r.state.PageCount > 0 {
		file, err := r.writer.CommitWithCompleteness(&r.state)
		if file.Path != "" {
			artifact = &file
		}
		if err != nil {
			if artifact != nil && artifact.Completeness != nil {
				r.state = *artifact.Completeness
			} else {
				r.state.Complete = false
				r.state.StopReason = "output_failed"
				r.state.Resumable = false
				r.state.Next = nil
				if artifact == nil {
					r.state.ItemCount = 0
				}
			}
			if cause != nil {
				err = errors.Join(cause, err)
			}
			cause = postExecutionFailure(r.ctx, err, "artifact")
			kind, code = "output_failed", "OutputFailed"
		}
	}
	r.ctx.Completeness = &r.state
	if r.state.Complete && cause == nil {
		if artifact != nil {
			return *artifact, nil
		}
		return r.rows, nil
	}
	var data any = r.rows
	if r.writer != nil {
		data = map[string]any{"itemCount": r.state.ItemCount}
	}
	return nil, &exportResultError{
		IncompleteError: &execution.IncompleteError{
			Data: data, State: &r.state, Cause: cause, Outcome: r.ctx.RemoteOutcome, Kind: kind, Code: code,
		},
		Artifact: artifact,
	}
}

func logExportResponse(out any, err error) (map[string]any, *execution.IncompleteError) {
	var partial *execution.IncompleteError
	if err != nil && !errors.As(err, &partial) {
		return nil, nil
	}
	if m, ok := out.(map[string]any); ok {
		return m, partial
	}
	if partial != nil {
		m, _ := partial.FailureData().(map[string]any)
		return m, partial
	}
	return nil, nil
}

func invalidLogExportResponse(message string) error {
	return &execution.Failure{Kind: "protocol", Code: "InvalidResponse", Stage: "decode", Outcome: execution.OutcomeUnknown, Message: message}
}

func cloneLogExportRequest(req map[string]any) map[string]any {
	cloned := make(map[string]any, len(req))
	for key, value := range req {
		cloned[key] = value
	}
	return cloned
}

func logExportContinuation(req map[string]any, cursor string) *execution.Continuation {
	body := cloneLogExportRequest(req)
	body["Context"] = cursor
	return &execution.Continuation{OperationID: "log.search", Input: map[string]any{"body": body}, PageAll: false}
}
