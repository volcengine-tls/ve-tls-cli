package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
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
	var readonlyErr *readonlyEditionError
	if errors.As(err, &readonlyErr) {
		return errPayload{
			RequestID:  requestID,
			StatusCode: statusCode,
			Kind:       "usage",
			Hint:       "default volclog only exposes readonly agent actions; use 'volclog-human' (-tags=human) for mutating or non-readonly calls",
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
		strings.HasPrefix(msg, "missing required fields:") ||
		strings.HasPrefix(msg, "workflow input missing required fields:") ||
		strings.HasPrefix(msg, "flat input contains unknown fields:") ||
		strings.HasPrefix(msg, "conflicting profile selectors:") ||
		strings.HasPrefix(msg, "conflicting runtime selectors:") ||
		strings.Contains(msg, "must be json object") ||
		strings.Contains(msg, "jsonl line must be object") ||
		strings.Contains(msg, "json-array input must be json array") ||
		strings.Contains(msg, "unsupported log contents type") ||
		strings.HasPrefix(msg, "json: cannot unmarshal ") ||
		strings.HasPrefix(msg, "result too large for stdout;") {
		hint := validationHint(group, msg)
		if strings.HasPrefix(msg, "conflicting profile selectors:") || strings.HasPrefix(msg, "conflicting runtime selectors:") {
			hint = "use exactly one runtime selector: --profile, --secrets-file, context.profile, or context.secrets_file"
		} else if strings.HasPrefix(msg, "result too large for stdout;") {
			hint = "rerun with --output-dir <writable-dir> to allow file_auto, or reduce stdout with --jmes-filter / execution.projection"
		}
		return errPayload{
			RequestID:  requestID,
			StatusCode: statusCode,
			Kind:       "validation",
			Hint:       hint,
		}, 1
	}
	if strings.HasPrefix(msg, "execution.page.all is not supported for tool:") ||
		strings.Contains(msg, "declares page.all support but runtime pagination metadata is unavailable") {
		return errPayload{
			RequestID:  requestID,
			StatusCode: statusCode,
			Kind:       "unsupported_feature",
			Hint:       "remove execution.page.all/--page-all, or switch to an action/workflow whose contract reports execution.supports_all=true",
		}, 1
	}
	if strings.Contains(msg, "cannot be combined with file delivery") ||
		strings.HasPrefix(msg, "--output-file is not supported for ") ||
		strings.Contains(msg, "requires --output-mode file") ||
		strings.Contains(msg, "cannot be used with PageNumber") ||
		strings.Contains(msg, "cannot be used with Cursor") ||
		strings.Contains(msg, "--all cannot be used with --page-number") ||
		strings.Contains(msg, "--all cannot be used with --cursor") {
		hint := "remove one of the conflicting output flags and retry"
		if strings.Contains(msg, "PageNumber") || strings.Contains(msg, "Cursor") || strings.Contains(msg, "--page-number") || strings.Contains(msg, "--cursor") {
			hint = "remove PageNumber/Cursor when execution.page.all or --page-all is enabled"
		}
		return errPayload{
			RequestID:  requestID,
			StatusCode: statusCode,
			Kind:       "incompatible_flags",
			Hint:       hint,
		}, 1
	}
	var secretsErr *secretsFileError
	if errors.As(err, &secretsErr) {
		return errPayload{
			RequestID:  requestID,
			StatusCode: statusCode,
			Kind:       "config",
			Hint:       "check --secrets-file path/content and ensure it sets supported VOLCENGINE_* variables",
		}, 2
	}
	var pathErr *os.PathError
	if msg == "missing writable output_dir" || errors.As(err, &pathErr) {
		return errPayload{
			RequestID:  requestID,
			StatusCode: statusCode,
			Kind:       "filesystem",
			Hint:       "provide a writable --output-dir or check the local file path and permissions",
		}, 2
	}
	if strings.HasPrefix(msg, "unexpected list response") ||
		strings.HasPrefix(msg, "unexpected list field:") ||
		strings.HasPrefix(msg, "cannot infer list field for --all") ||
		strings.HasPrefix(msg, "path still contains unresolved params") {
		return errPayload{
			RequestID:  requestID,
			StatusCode: statusCode,
			Kind:       "decode",
			Hint:       "inspect response shape or rerun with --dry-run / --trace-dir to confirm the request/response contract",
		}, 3
	}
	if msg == "working directory not found" {
		return errPayload{
			RequestID:  requestID,
			StatusCode: statusCode,
			Kind:       "filesystem",
			Hint:       "check the current working directory and local filesystem permissions",
		}, 2
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
		Hint:       "inspect error.message and rerun with --dry-run or --trace-dir when you need more detail",
	}, 2
}

func validationHint(group string, msg string) string {
	group = strings.TrimSpace(group)
	switch {
	case strings.HasPrefix(msg, "workflow input missing required fields:"):
		return "inspect the contract with 'volclog workflow describe <group.command>' and align the JSON input/context fields"
	case strings.HasPrefix(msg, "missing required field:"),
		strings.HasPrefix(msg, "missing required fields:"),
		strings.HasPrefix(msg, "flat input contains unknown fields:"):
		return "inspect the contract with 'volclog tool describe <group.action>' and align the JSON input/context fields"
	case group == "workflow":
		return "inspect the contract with 'volclog workflow describe <group.command>' and align the JSON input/context fields"
	case group == "tool":
		return "inspect the contract with 'volclog tool describe <group.action>' and align the JSON input/context fields"
	default:
		return "inspect the contract with 'volclog tool describe <group.action>' or 'volclog workflow describe <group.command>' and align the JSON input/context fields"
	}
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
