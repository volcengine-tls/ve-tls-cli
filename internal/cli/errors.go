package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/output"
)

type errPayload struct {
	ErrorCode    string `json:"errorCode"`
	ErrorMessage string `json:"errorMessage"`
	RequestID    string `json:"requestId,omitempty"`
	StatusCode   int    `json:"statusCode,omitempty"`
	Kind         string `json:"kind,omitempty"`
	Hint         string `json:"hint,omitempty"`
}

type removedCommandError struct {
	Command string
	Hint    string
}

func (e *removedCommandError) Error() string {
	return "legacy command removed: " + strings.TrimSpace(e.Command)
}

func removedLegacyCommandError(command string, hint string) error {
	return &removedCommandError{
		Command: strings.TrimSpace(command),
		Hint:    strings.TrimSpace(hint),
	}
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

func classifyError(err error, requestID string, statusCode int, group string) (errPayload, int) {
	msg := strings.TrimSpace(err.Error())
	var removed *removedCommandError
	if errors.As(err, &removed) {
		hint := strings.TrimSpace(removed.Hint)
		if hint == "" {
			hint = "use 'volclog tool list' or 'volclog raw --method <METHOD> --path <PATH>'"
		}
		return errPayload{
			RequestID:  requestID,
			StatusCode: statusCode,
			Kind:       "usage",
			Hint:       hint,
		}, 1
	}
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
		if strings.EqualFold(httpErrorCode(he.body), "IndexNotExists") {
			hint = "index does not exist; inspect current config with 'volclog index get --topic-id <TopicId>' or create one with 'volclog index create --describe' and 'volclog index create --print-request-template=full'"
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
		strings.Contains(msg, "must use file://") ||
		strings.Contains(msg, "inline JSON object") ||
		strings.Contains(msg, "json must be object") ||
		strings.HasPrefix(msg, "action not found:") ||
		strings.HasPrefix(msg, "group not found:") ||
		(strings.Contains(msg, "unknown ") && strings.Contains(msg, " command:")) {
		hint := "start with 'volclog tool list' or inspect 'volclog tool describe <group.action>'"
		if strings.HasPrefix(msg, "missing --") || strings.HasPrefix(msg, "unknown flag:") {
			hint = "inspect constraints with 'volclog tool describe <group.action>' or run --help"
			if flag := strings.TrimSpace(strings.TrimPrefix(msg, "unknown flag:")); isGlobalFlagName(flag) {
				hint = globalFlagPositionHint(flag, group)
			}
		} else if strings.Contains(msg, "must use file://") || strings.Contains(msg, "inline JSON object") || strings.Contains(msg, "json must be object") {
			hint = "use file://ctx.json, -, or inline JSON object with 'volclog tool exec <group.action>'; tool exec also accepts flat JSON when fields map cleanly to query/path/header/body"
		}
		return errPayload{
			RequestID:  requestID,
			StatusCode: statusCode,
			Kind:       "usage",
			Hint:       hint,
		}, 1
	}
	if strings.HasPrefix(msg, "missing required field:") ||
		strings.HasPrefix(msg, "workflow input missing required fields:") ||
		strings.HasPrefix(msg, "flat input contains unknown fields:") ||
		strings.HasPrefix(msg, "conflicting profile selectors:") {
		hint := "inspect the contract with 'volclog tool describe <group.action>' or 'volclog workflow describe <group.command>' and align the JSON input/context fields"
		if strings.HasPrefix(msg, "conflicting profile selectors:") {
			hint = "use exactly one profile selector: global --profile or context.profile"
		}
		return errPayload{
			RequestID:  requestID,
			StatusCode: statusCode,
			Kind:       "validation",
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

func httpErrorCode(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var v struct {
		ErrorCode string `json:"ErrorCode"`
		Code      string `json:"errorCode"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return ""
	}
	if strings.TrimSpace(v.ErrorCode) != "" {
		return strings.TrimSpace(v.ErrorCode)
	}
	return strings.TrimSpace(v.Code)
}

func globalFlagPositionHint(flag string, group string) string {
	flag = strings.TrimSpace(flag)
	group = strings.TrimSpace(group)
	usage := flag
	for _, spec := range cliGlobalFlagSpecs() {
		if strings.TrimSpace(spec.Name) == flag {
			usage = strings.TrimSpace(spec.Usage)
			break
		}
	}
	if flag == "--output-file" {
		usage = "--output-file <path>"
	}
	example := "volclog " + usage
	if group != "" {
		example += " " + group + " ..."
		return "flag " + flag + " is global and position-sensitive; move it before '" + group + "', e.g. '" + example + "'"
	}
	return "flag " + flag + " is global and position-sensitive; move it before the group, e.g. '" + example + " <group> ...'"
}

func writeStructuredError(stdout, stderr io.Writer, err error, requestID string, statusCode int, group string, env map[string]any) int {
	payload, code := classifyError(err, requestID, statusCode, group)
	if env != nil {
		if err2 := output.Write(stdout, env, output.FormatJSON); err2 != nil {
			writeCLIError(stderr, err2, payload.RequestID, payload.StatusCode, "decode", "output write failed")
			return 3
		}
		return code
	}
	writeCLIError(stderr, err, payload.RequestID, payload.StatusCode, payload.Kind, payload.Hint)
	return code
}

func isGlobalFlagName(flag string) bool {
	flag = strings.TrimSpace(flag)
	if flag == "" {
		return false
	}
	if flag == "--output-file" {
		return true
	}
	for _, name := range cliGlobalFlags() {
		if flag == name {
			return true
		}
	}
	return false
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
