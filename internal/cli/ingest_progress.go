package cli

import (
	"fmt"

	"github.com/volcengine-tls/ve-tls-cli/internal/execution"
)

// ingestFailure carries only acknowledged progress, not log contents.
type ingestFailure struct {
	Progress map[string]any
	Cause    error
	Result   map[string]any
}

func (e *ingestFailure) Error() string {
	return fmt.Sprintf("ingest failed at batch %d", e.Progress["currentBatch"])
}

func (e *ingestFailure) Unwrap() error { return e.Cause }

func (e *ingestFailure) RemoteOutcome() string {
	if e.Progress["confirmedBatches"].(int) > 0 {
		return execution.OutcomePartial
	}
	outcome, _ := e.Progress["currentBatchOutcome"].(string)
	return outcome
}

func (e *ingestFailure) ErrorKind() string {
	if e.Progress["confirmedBatches"].(int) > 0 {
		return "partial_failure"
	}
	return ""
}

func (e *ingestFailure) ErrorCode() string {
	if e.Progress["confirmedBatches"].(int) > 0 {
		return "IngestPartialFailure"
	}
	return ""
}

func (e *ingestFailure) FailureData() any { return e.Result }

func newIngestProgress(totalLogs, totalBatches int) map[string]any {
	return map[string]any{
		"confirmedBatches":        0,
		"confirmedLogs":           0,
		"confirmedLogOffset":      0,
		"totalLogs":               totalLogs,
		"totalBatches":            totalBatches,
		"currentBatch":            0,
		"currentBatchStartOffset": 0,
		"currentBatchEndOffset":   0,
		"currentBatchOutcome":     execution.OutcomeNotSent,
		"lastConfirmedRequestId":  "",
	}
}

func ingestResult(cfg logIngestArgs, progress map[string]any, logs, batches int) map[string]any {
	return map[string]any{
		"topicId":      cfg.TopicID,
		"inputFormat":  cfg.InputFormat,
		"compressType": ingestCompressionSummary(cfg.CompressType),
		"logs":         logs,
		"batches":      batches,
		"progress":     progress,
	}
}

func ingestFailed(ctx *Context, cfg logIngestArgs, progress map[string]any, cause error) (any, error) {
	result := ingestResult(cfg, progress, progress["confirmedLogs"].(int), progress["confirmedBatches"].(int))
	err := &ingestFailure{Progress: progress, Cause: cause, Result: result}
	ctx.RemoteOutcome = err.RemoteOutcome()
	return result, err
}
