package cli

import (
	"bytes"
	"encoding/json"
	"io"

	"github.com/volcengine-tls/ve-tls-cli/internal/output"
)

func writeCLIError(w io.Writer, err error, requestID string, statusCode int, kind string, hint string) {
	p := errPayload{
		ErrorCode:    "CLIError",
		ErrorMessage: err.Error(),
		RequestID:    requestID,
		StatusCode:   statusCode,
		Kind:         kind,
		Hint:         hint,
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if e := enc.Encode(p); e != nil {
		_, _ = w.Write([]byte(err.Error() + "\n"))
		return
	}
	_, _ = w.Write(buf.Bytes())
}

func writeStructuredError(stdout, stderr io.Writer, err error, requestID string, statusCode int, group string, env map[string]any) int {
	payload, code := classifyError(err, requestID, statusCode, group)
	if env != nil {
		if err2 := output.Write(stdout, env, output.FormatJSON); err2 != nil {
			return writeBrokenStdout(stderr, err2, env)
		}
		return code
	}
	writeCLIError(stderr, err, payload.RequestID, payload.StatusCode, payload.Kind, payload.Hint)
	return code
}
