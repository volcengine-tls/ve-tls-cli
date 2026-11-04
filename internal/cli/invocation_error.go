package cli

import (
	"io"

	"github.com/volcengine-tls/ve-tls-cli/internal/output"
)

func defaultAgentErrorProtocol(group string, rest []string) bool {
	return currentEdition() == cliEditionVolclog && !isInteractiveAuthCommand(group, rest)
}

// writeInvocationFailure is used before a request has been sent. The caller
// keeps the existing human and interactive authentication output paths.
func writeInvocationFailure(stdout, stderr io.Writer, group string, rest []string, err error, kind, hint string, code int) int {
	// A token mistaken for a command group can itself contain a secret.
	if !isRecognizedGroup(group) {
		group = "cli.invoke"
	}
	env := buildAPIErrorEnvelope(nil, group, err, "stdout")
	errObj := env["error"].(map[string]any)
	if kind != "" {
		errObj["kind"] = kind
	}
	if hint != "" {
		errObj["hint"] = hint
	}
	if writeErr := output.Write(stdout, env, output.FormatJSON); writeErr != nil {
		return writeBrokenStdout(stderr, writeErr, env)
	}
	return code
}

// invocationStdoutWriter distinguishes a failed version write from a failure
// while building the version information, including a short write with nil error.
type invocationStdoutWriter struct {
	writer io.Writer
	err    error
}

func (w *invocationStdoutWriter) Write(p []byte) (int, error) {
	n, err := w.writer.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	if err != nil {
		w.err = err
	}
	return n, err
}
