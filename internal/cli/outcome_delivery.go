package cli

import (
	"errors"
	"io"
	"os"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/config"
	"github.com/volcengine-tls/ve-tls-cli/internal/execution"
	"github.com/volcengine-tls/ve-tls-cli/internal/output"
)

func (c *Context) beginRequest() {
	c.RequestID = ""
	c.StatusCode = 0
	c.RemoteOutcome = execution.OutcomeNotSent
	c.Completeness = nil
	c.TaskMeta = nil
}

func (c *Context) recordDecodedResult(data any, err error) (any, error) {
	if err == nil {
		c.RemoteOutcome = execution.OutcomeSuccess
		return data, nil
	}
	if _, ok := isHTTPError(err); ok {
		c.RemoteOutcome = execution.OutcomeFailed
		return data, err
	}
	if kind, _ := firstTypedOperationError(err); kind == "output_failed" {
		c.RemoteOutcome = execution.ErrorOutcome(err, execution.OutcomeSuccess)
		return data, err
	}
	c.RemoteOutcome = execution.ErrorOutcome(err, execution.OutcomeUnknown)
	return data, &execution.Failure{Kind: "protocol", Code: "InvalidResponse", Stage: "decode", Outcome: c.RemoteOutcome, Message: "response does not match the operation protocol", Cause: err}
}

func (c *Context) captureTargetSnapshot() {
	source := "profile_inline"
	env := config.ResolveEnvCredentialStatus()
	if c.resolution.Dynamic {
		source = "provider"
	} else if strings.TrimSpace(c.Profile) == "" && env.Present {
		source = "environment"
	} else if strings.TrimSpace(c.Profile) == "" && ((c.profile.AccessKeyID == "" && env.AK) || (c.profile.SecretAccessKey == "" && env.SK)) {
		source = "mixed"
	} else if strings.TrimSpace(c.profile.CredRef) != "" {
		source = "profile_cred_ref"
	}
	endpoint, _ := execution.RedactPreviewURL(c.profile.Endpoint)
	c.TargetSnapshot = map[string]any{
		"profile": c.resolution.ProfileName, "authMode": c.resolution.Mode,
		"credentialSource": source, "region": c.profile.Region, "endpoint": endpoint,
	}
}

func currentRemoteOutcome(ctx *Context, err error) string {
	fallback := execution.OutcomeNotSent
	if ctx != nil && ctx.RemoteOutcome != "" {
		fallback = ctx.RemoteOutcome
	}
	return execution.ErrorOutcome(err, fallback)
}

func augmentOutcomeSummary(summary map[string]any, ctx *Context, err error) {
	summary["remoteOutcome"] = currentRemoteOutcome(ctx, err)
	summary["deliveryOutcome"] = "success"
	var delivery *outputFailureError
	kind, _ := firstTypedOperationError(err)
	if errors.As(err, &delivery) || kind == "output_failed" {
		summary["deliveryOutcome"] = "failed"
	}
	if ctx != nil {
		if ctx.TargetSnapshot != nil {
			summary["target"] = cloneOutcomeMap(ctx.TargetSnapshot)
		}
		if ctx.Progress != nil {
			summary["progress"] = cloneOutcomeMap(ctx.Progress)
		}
		if ctx.Completeness != nil {
			summary["completeness"] = ctx.Completeness
			summary["itemCount"] = ctx.Completeness.ItemCount
		}
		if ctx.PaginationMeta != nil {
			summary["pagination"] = ctx.PaginationMeta
		}
		for name, value := range ctx.TaskMeta {
			summary[name] = value
		}
	}
	var incomplete interface {
		CompletionState() *execution.Completeness
	}
	if errors.As(err, &incomplete) && incomplete.CompletionState() != nil {
		summary["completeness"] = incomplete.CompletionState()
		summary["itemCount"] = incomplete.CompletionState().ItemCount
	}
}

func validationFailure(err error, code string) error {
	return &execution.Failure{Kind: "validation", Code: code, Stage: "validate", Outcome: execution.OutcomeNotSent, Cause: err}
}

func cloneOutcomeMap(source map[string]any) map[string]any {
	copy := make(map[string]any, len(source))
	for key, value := range source {
		copy[key] = value
	}
	return copy
}

type outputFailureError struct {
	Cause   error
	Stage   string
	Outcome string
	Exit    int
}

func (e *outputFailureError) Error() string {
	var filter *output.FilterError
	if errors.As(e.Cause, &filter) && filter.Code == "FilterNoValue" {
		return filter.Error()
	}
	if errors.Is(e.Cause, errMissingWritableOutputDir) {
		return "result too large for stdout; specify --output-dir <writable-dir> to allow automatic file delivery"
	}
	return "output delivery failed during " + e.Stage
}
func (e *outputFailureError) Unwrap() error         { return e.Cause }
func (e *outputFailureError) ErrorKind() string     { return "output_failed" }
func (e *outputFailureError) ErrorCode() string     { return "OutputFailed" }
func (e *outputFailureError) RemoteOutcome() string { return e.Outcome }
func (e *outputFailureError) FailureStage() string  { return e.Stage }
func (e *outputFailureError) DeliveryExitCode() int { return e.Exit }

func outputFailure(ctx *Context, err error, stage string, code int) error {
	var existing *outputFailureError
	if errors.As(err, &existing) {
		return err
	}
	return &outputFailureError{Cause: err, Stage: stage, Outcome: currentRemoteOutcome(ctx, nil), Exit: code}
}

func postExecutionFailure(ctx *Context, err error, stage string) error {
	code := 3
	var pathErr *os.PathError
	if errors.Is(err, errMissingWritableOutputDir) || errors.As(err, &pathErr) || stage == "artifact" {
		code = 2
	}
	return outputFailure(ctx, err, stage, code)
}

// stdout may already be partially written. Preserve the result dimensions in
// stderr as a best effort, without rerunning any business operation.
func writeBrokenStdout(stderr io.Writer, cause error, env map[string]any) int {
	ctx := &Context{}
	group := "output"
	if env != nil {
		ctx.RequestID, _ = env["requestId"].(string)
		ctx.Action, _ = env["action"].(string)
		if summary, ok := env["summary"].(map[string]any); ok {
			ctx.RemoteOutcome, _ = summary["remoteOutcome"].(string)
			ctx.DryRun, _ = summary["dryRun"].(bool)
			ctx.TargetSnapshot, _ = summary["target"].(map[string]any)
			ctx.Progress, _ = summary["progress"].(map[string]any)
			ctx.Completeness = completenessValue(summary["completeness"])
			ctx.PaginationMeta, _ = summary["pagination"].(map[string]any)
			for _, name := range []string{"async", "asyncTasks"} {
				if value, ok := summary[name]; ok {
					if ctx.TaskMeta == nil {
						ctx.TaskMeta = map[string]any{}
					}
					ctx.TaskMeta[name] = value
				}
			}
		}
		if original, ok := env["error"].(map[string]any); ok {
			switch status := original["statusCode"].(type) {
			case int:
				ctx.StatusCode = status
			case float64:
				ctx.StatusCode = int(status)
			}
		}
	}
	failure := outputFailure(ctx, cause, "stdout", 3)
	failed := buildAPIErrorEnvelope(ctx, group, failure, "stdout")
	preserveDeliveredArtifacts(failed, env)
	if env != nil {
		if original, ok := env["summary"].(map[string]any); ok {
			if progress, ok := original["progress"]; ok {
				failed["summary"].(map[string]any)["progress"] = progress
			}
		}
	}
	_ = output.Write(stderr, failed, output.FormatJSON)
	return 3
}

func writeDeliveryFailure(ctx *Context, stdout, stderr io.Writer, group string, err error, stage string, code int, delivered ...map[string]any) int {
	failure := outputFailure(ctx, err, stage, code)
	env := buildAPIErrorEnvelope(ctx, group, failure, ctx.OutputMode)
	if len(delivered) > 0 {
		preserveDeliveredArtifacts(env, delivered[0])
	}
	if writeErr := output.Write(stdout, env, output.FormatJSON); writeErr != nil {
		return writeBrokenStdout(stderr, writeErr, env)
	}
	return code
}

func writeDeliveryFailureToStderr(ctx *Context, stderr io.Writer, group string, cause error, delivered ...map[string]any) int {
	env := buildAPIErrorEnvelope(ctx, group, outputFailure(ctx, cause, "stdout", 3), ctx.OutputMode)
	if len(delivered) > 0 {
		preserveDeliveredArtifacts(env, delivered[0])
	}
	_ = output.Write(stderr, env, output.FormatJSON)
	return 3
}

func preserveDeliveredArtifacts(failed, delivered map[string]any) {
	if delivered == nil {
		return
	}
	if artifacts, ok := delivered["artifacts"]; ok {
		failed["artifacts"] = artifacts
	}
}

func writeOutputNotice(w io.Writer, text string) error {
	n, err := io.WriteString(w, text)
	if err == nil && n != len(text) {
		return io.ErrShortWrite
	}
	return err
}
