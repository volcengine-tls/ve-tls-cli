package cli

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
)

type errPayload struct {
	ErrorCode    string `json:"errorCode"`
	ErrorMessage string `json:"errorMessage"`
	RequestID    string `json:"requestId,omitempty"`
	StatusCode   int    `json:"statusCode,omitempty"`
	Kind         string `json:"kind,omitempty"`
	Hint         string `json:"hint,omitempty"`
}

func writeCLIError(w io.Writer, err error, requestID string, statusCode int, kind string, hint string) {
	p := errPayload{
		ErrorCode:    "CLIError",
		ErrorMessage: err.Error(),
		RequestID:    requestID,
		StatusCode:   statusCode,
		Kind:         kind,
		Hint:         hint,
	}
	b, e := json.MarshalIndent(p, "", "  ")
	if e != nil {
		_, _ = w.Write([]byte(err.Error() + "\n"))
		return
	}
	_, _ = w.Write(append(b, '\n'))
}

func classifyError(err error, requestID string, statusCode int) (errPayload, int) {
	msg := strings.TrimSpace(err.Error())
	if he, ok := isHTTPError(err); ok {
		kind := "server"
		hint := ""
		if he.statusCode == 401 || he.statusCode == 403 {
			kind = "auth"
			hint = "check credentials/region/endpoint"
		} else if he.statusCode == 400 {
			kind = "usage"
			hint = "check request parameters"
		}
		return errPayload{
			RequestID:  he.requestID,
			StatusCode: he.statusCode,
			Kind:       kind,
			Hint:       hint,
		}, 2
	}
	if ue, ok := asUsageError(err); ok {
		return errPayload{Kind: "usage"}, ue.ExitCode
	}
	if strings.HasPrefix(msg, "missing --") ||
		strings.HasPrefix(msg, "unknown flag:") ||
		strings.HasPrefix(msg, "unknown api group:") ||
		strings.HasPrefix(msg, "action not found:") ||
		strings.HasPrefix(msg, "group not found:") ||
		(strings.Contains(msg, "unknown ") && strings.Contains(msg, " command:")) {
		hint := "start with 'volclog capabilities --view text' or inspect 'volclog api <group> <action> --describe'"
		if strings.HasPrefix(msg, "missing --") || strings.HasPrefix(msg, "unknown flag:") {
			hint = "inspect constraints with 'volclog api <group> <action> --describe' or run --help"
		}
		return errPayload{
			RequestID:  requestID,
			StatusCode: statusCode,
			Kind:       "usage",
			Hint:       hint,
		}, 1
	}
	if strings.HasPrefix(msg, "filter ") || msg == "empty filter" || strings.HasPrefix(msg, "invalid --jmes-filter") || strings.HasPrefix(msg, "invalid jmes-filter expression:") {
		return errPayload{
			RequestID:  requestID,
			StatusCode: statusCode,
			Kind:       "decode",
			Hint:       "check --jmes-filter",
		}, 3
	}
	if strings.HasPrefix(msg, "profile not found:") || strings.HasPrefix(msg, "missing region") || strings.HasPrefix(msg, "missing endpoint") || strings.HasPrefix(msg, "missing access key") {
		return errPayload{
			RequestID:  requestID,
			StatusCode: statusCode,
			Kind:       "config",
			Hint:       "check env or profile config",
		}, 2
	}
	return errPayload{
		RequestID:  requestID,
		StatusCode: statusCode,
		Kind:       "unknown",
		Hint:       "",
	}, 2
}

type usageError struct {
	Text     string
	ExitCode int
}

func (e *usageError) Error() string { return "usage" }

func asUsageError(err error) (*usageError, bool) {
	var ue *usageError
	if errors.As(err, &ue) {
		return ue, true
	}
	return nil, false
}
