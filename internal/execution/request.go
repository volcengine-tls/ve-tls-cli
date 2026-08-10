package execution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
)

type Request struct {
	Method     string
	Path       string
	Query      map[string]string
	Header     map[string]string
	Body       []byte
	BodyFormat BodyFormat
}

type Response struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

type Transport interface {
	Do(context.Context, Request) (Response, error)
}

func BuildRequest(operation contract.Operation, input Input) (Request, error) {
	method := strings.ToUpper(strings.TrimSpace(operation.Wire.Method))
	if method == "" {
		method = http.MethodGet
	}
	path := strings.TrimSpace(operation.Wire.Path)
	for key, value := range stringMap(input.Path) {
		path = strings.ReplaceAll(path, "{"+key+"}", value)
	}
	if strings.Contains(path, "{") && strings.Contains(path, "}") {
		return Request{}, errors.New("path still contains unresolved params")
	}

	body, format, err := payloadBytes(input.Body, operation.Wire.RequestFormat)
	if err != nil {
		return Request{}, err
	}
	return Request{
		Method:     method,
		Path:       path,
		Query:      stringMap(input.Query),
		Header:     stringMap(input.Header),
		Body:       body,
		BodyFormat: format,
	}, nil
}

func payloadBytes(payload Payload, wireFormat string) ([]byte, BodyFormat, error) {
	format := payload.Format
	if strings.TrimSpace(string(format)) == "" {
		format = wireBodyFormat(wireFormat)
	}
	if payload.Raw != nil {
		return append([]byte(nil), payload.Raw...), format, nil
	}
	value := payload.JSON
	if value == nil && !payload.Present {
		value = map[string]any{}
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, "", err
	}
	return raw, format, nil
}

func stringMap(src map[string]any) map[string]string {
	out := make(map[string]string, len(src))
	for key, value := range src {
		out[strings.TrimSpace(key)] = stringifyValue(value)
	}
	return out
}

func stringifyValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case nil:
		return ""
	default:
		raw, err := json.Marshal(value)
		if err == nil && len(raw) > 0 && string(raw) != "null" {
			if raw[0] == '"' {
				var decoded string
				if json.Unmarshal(raw, &decoded) == nil {
					return decoded
				}
			}
			return string(raw)
		}
		return fmt.Sprint(value)
	}
}

func cloneRequest(request Request) Request {
	return Request{
		Method:     request.Method,
		Path:       request.Path,
		Query:      cloneStringMap(request.Query),
		Header:     cloneStringMap(request.Header),
		Body:       append([]byte(nil), request.Body...),
		BodyFormat: request.BodyFormat,
	}
}

func cloneStringMap(src map[string]string) map[string]string {
	if src == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}

func sortedMapKeys(src map[string]string) []string {
	out := make([]string, 0, len(src))
	for key := range src {
		out = append(out, strings.TrimSpace(key))
	}
	sort.Strings(out)
	return out
}
