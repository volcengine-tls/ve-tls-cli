package cli

import "github.com/volcengine-tls/ve-tls-cli/internal/execution"

func applyTaskResult(ctx *Context, operationID string, input execution.Input, data any) error {
	meta, outcome, err := inspectDownloadTaskResult(operationID, input, data)
	ctx.TaskMeta = meta
	if err != nil {
		ctx.RemoteOutcome = execution.ErrorOutcome(err, ctx.RemoteOutcome)
		return err
	}
	if outcome != "" {
		ctx.RemoteOutcome = outcome
	}
	return nil
}
