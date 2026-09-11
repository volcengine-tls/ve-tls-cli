package cli

import (
	"context"
	"errors"
	"reflect"

	"github.com/volcengine-tls/ve-tls-cli/internal/execution"
	"github.com/volcengine-tls/ve-tls-cli/internal/tlsapi"
)

type rawExecutionContext interface {
	DoRaw(
		method, path string,
		query map[string]string,
		header map[string]string,
		body []byte,
	) (tlsapi.Response, error)
}

type contextualRawExecutionContext interface {
	doRaw(
		ctx context.Context,
		method, path string,
		query map[string]string,
		header map[string]string,
		body []byte,
	) (tlsapi.Response, error)
}

type contextExecutionTransport struct {
	raw rawExecutionContext
}

func newContextExecutionTransport(raw rawExecutionContext) execution.Transport {
	return contextExecutionTransport{raw: raw}
}

func (t contextExecutionTransport) Do(ctx context.Context, request execution.Request) (execution.Response, error) {
	if isNilRawExecutionContext(t.raw) {
		return execution.Response{}, errors.New("nil raw execution context")
	}
	var (
		response tlsapi.Response
		err      error
	)
	path, pathErr := execution.AppendMultiQuery(request.Path, request.QueryMulti)
	if pathErr != nil {
		return execution.Response{}, pathErr
	}
	if contextual, ok := t.raw.(contextualRawExecutionContext); ok {
		// Context.doRaw delegates to runtime.Transport, which already owns the
		// request/response copies. Avoid cloning the production path twice.
		response, err = contextual.doRaw(ctx, request.Method, path, request.Query, request.Header, request.Body)
		return execution.Response{
			StatusCode: response.StatusCode,
			Header:     response.Header,
			Body:       response.Body,
		}, err
	} else {
		response, err = t.raw.DoRaw(
			request.Method,
			path,
			cloneExecutionStringMap(request.Query),
			cloneExecutionStringMap(request.Header),
			append([]byte(nil), request.Body...),
		)
	}
	return execution.Response{
		StatusCode: response.StatusCode,
		Header:     response.Header.Clone(),
		Body:       append([]byte(nil), response.Body...),
	}, err
}

func isNilRawExecutionContext(raw rawExecutionContext) bool {
	if raw == nil {
		return true
	}
	value := reflect.ValueOf(raw)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func cloneExecutionStringMap(src map[string]string) map[string]string {
	if src == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}
