package execution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
	"github.com/volcengine-tls/ve-tls-cli/internal/jsonx"
)

type Request struct {
	Method     string
	Path       string
	Query      map[string]string
	QueryMulti map[string][]string
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
	query, queryMulti := queryMaps(input.Query)
	return Request{
		Method:     method,
		Path:       path,
		Query:      query,
		QueryMulti: queryMulti,
		Header:     stringMap(input.Header),
		Body:       body,
		BodyFormat: format,
	}, nil
}

func queryMaps(src map[string]any) (map[string]string, map[string][]string) {
	single := make(map[string]string, len(src))
	multi := make(map[string][]string)
	for key, value := range src {
		key = strings.TrimSpace(key)
		switch typed := value.(type) {
		case []any:
			values := make([]string, len(typed))
			for index, item := range typed {
				values[index] = stringifyValue(item)
			}
			multi[key] = values
		case []string:
			multi[key] = append([]string(nil), typed...)
		default:
			single[key] = stringifyValue(value)
		}
	}
	return single, multi
}

// AppendMultiQuery appends repeated query parameters to path without changing
// scalar query handling. Callers can still pass the scalar map to tlsapi.Client.
func AppendMultiQuery(path string, values map[string][]string) (string, error) {
	if len(values) == 0 {
		return path, nil
	}
	parsed, err := url.Parse(path)
	if err != nil {
		return "", err
	}
	query := parsed.Query()
	for key, items := range values {
		for _, item := range items {
			query.Add(key, item)
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
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
				if jsonx.Unmarshal(raw, &decoded) == nil {
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
		QueryMulti: cloneMultiStringMap(request.QueryMulti),
		Header:     cloneStringMap(request.Header),
		Body:       append([]byte(nil), request.Body...),
		BodyFormat: request.BodyFormat,
	}
}

func cloneMultiStringMap(src map[string][]string) map[string][]string {
	if src == nil {
		return map[string][]string{}
	}
	out := make(map[string][]string, len(src))
	for key, values := range src {
		out[key] = append([]string(nil), values...)
	}
	return out
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

func sortedQueryKeys(single map[string]string, multi map[string][]string) []string {
	keys := make(map[string]struct{}, len(single)+len(multi))
	for key := range single {
		keys[strings.TrimSpace(key)] = struct{}{}
	}
	for key := range multi {
		keys[strings.TrimSpace(key)] = struct{}{}
	}
	out := make([]string, 0, len(keys))
	for key := range keys {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func previewQuery(single map[string]string, multi map[string][]string) map[string]any {
	out := make(map[string]any, len(single)+len(multi))
	for key, value := range single {
		out[key] = value
	}
	for key, values := range multi {
		out[key] = append([]string(nil), values...)
	}
	return out
}
