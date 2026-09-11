package cli

import (
	"errors"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/jsonx"
	"github.com/volcengine-tls/ve-tls-cli/internal/tlsapi"
	"github.com/volcengine-tls/ve-tls-cli/internal/util"
)

type httpError struct {
	statusCode int
	body       []byte
	requestID  string
}

func (e *httpError) Error() string {
	if len(e.body) == 0 {
		return "http error"
	}
	return string(e.body)
}

func decodeResponse(resp tlsapi.Response) (any, error) {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &httpError{
			statusCode: resp.StatusCode,
			body:       resp.Body,
			requestID:  resp.Header.Get("x-tls-requestid"),
		}
	}
	if len(resp.Body) == 0 {
		return map[string]any{}, nil
	}
	v, err := util.UnmarshalJSON(resp.Body)
	if err == nil {
		return v, nil
	}
	if errors.Is(err, jsonx.ErrTrailingData) {
		return nil, err
	}
	var s string
	if jsonx.Unmarshal(resp.Body, &s) == nil {
		return map[string]any{"data": s}, nil
	}
	return map[string]any{"data": string(resp.Body)}, nil
}

func isHTTPError(err error) (*httpError, bool) {
	var he *httpError
	if errors.As(err, &he) {
		return he, true
	}
	return nil, false
}

func parseHTTPErrorPayload(he *httpError) (string, string, map[string]any) {
	if he == nil || len(he.body) == 0 {
		return "", "", nil
	}
	var body map[string]any
	if err := jsonx.Unmarshal(he.body, &body); err != nil {
		return "", "", nil
	}
	errorCode := firstStringValue(body, "errorCode", "ErrorCode")
	errorMessage := firstStringValue(body, "errorMessage", "ErrorMessage")
	if errorCode == "" && errorMessage == "" {
		return "", "", nil
	}
	var details map[string]any
	if nested := parseJSONObjectString(errorMessage); nested != nil {
		details = nested
		if nestedCode := firstStringValue(nested, "errorCode", "ErrorCode"); nestedCode != "" {
			errorCode = nestedCode
		}
		if nestedMessage := firstStringValue(nested, "errorMessage", "ErrorMessage"); nestedMessage != "" {
			errorMessage = nestedMessage
		}
	}
	return errorCode, errorMessage, details
}

func parseJSONObjectString(raw string) map[string]any {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
		return nil
	}
	var out map[string]any
	if err := jsonx.Unmarshal([]byte(trimmed), &out); err != nil {
		return nil
	}
	return out
}

func firstStringValue(body map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := body[key].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
