package cli

import (
	"bytes"
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
		strings.HasPrefix(msg, "action not found:") ||
		strings.HasPrefix(msg, "group not found:") ||
		(strings.Contains(msg, "unknown ") && strings.Contains(msg, " command:")) {
		hint := "start with 'volclog capabilities --view text' or inspect 'volclog api <group> <action> --describe'"
		if strings.HasPrefix(msg, "missing --") || strings.HasPrefix(msg, "unknown flag:") {
			hint = "inspect constraints with 'volclog api <group> <action> --describe' or run --help"
			if flag := strings.TrimSpace(strings.TrimPrefix(msg, "unknown flag:")); isGlobalFlagName(flag) {
				hint = globalFlagPositionHint(flag, group)
			}
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
	example := "volclog " + usage
	if group != "" {
		example += " " + group + " ..."
		return "flag " + flag + " is global and position-sensitive; move it before '" + group + "', e.g. '" + example + "'"
	}
	return "flag " + flag + " is global and position-sensitive; move it before the group, e.g. '" + example + " <group> ...'"
}

func isGlobalFlagName(flag string) bool {
	flag = strings.TrimSpace(flag)
	if flag == "" {
		return false
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
