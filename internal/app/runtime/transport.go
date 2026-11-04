package runtime

import (
	"context"
	"errors"

	"github.com/volcengine-tls/ve-tls-cli/internal/execution"
	"github.com/volcengine-tls/ve-tls-cli/internal/tlsapi"
)

// ClientSource resolves the TLS client for one request.
type ClientSource func() (*tlsapi.Client, error)

// Transport adapts a TLS client source to execution.Transport.
type Transport struct {
	source ClientSource
}

// NewTransport creates a transport whose client is resolved lazily for every
// request.
func NewTransport(source ClientSource) *Transport {
	return &Transport{source: source}
}

// Do implements execution.Transport and preserves the caller's context.
func (t *Transport) Do(ctx context.Context, request execution.Request) (execution.Response, error) {
	path, err := execution.AppendMultiQuery(request.Path, request.QueryMulti)
	if err != nil {
		return execution.Response{}, &execution.Failure{
			Kind: "validation", Code: "InvalidRequest", Stage: "build_request", Outcome: execution.OutcomeNotSent,
			Message: "request construction failed", Cause: err,
		}
	}
	response, err := t.DoRaw(
		ctx,
		request.Method,
		path,
		cloneStringMap(request.Query),
		cloneStringMap(request.Header),
		append([]byte(nil), request.Body...),
	)
	return execution.Response{
		StatusCode: response.StatusCode,
		Header:     response.Header.Clone(),
		Body:       append([]byte(nil), response.Body...),
	}, err
}

// DoRaw executes an unadapted TLS API request.
func (t *Transport) DoRaw(
	ctx context.Context,
	method, path string,
	query, header map[string]string,
	body []byte,
) (tlsapi.Response, error) {
	if t == nil || t.source == nil {
		return tlsapi.Response{}, clientResolutionFailure("nil client source", errors.New("nil client source"))
	}
	client, err := t.source()
	if err != nil {
		return tlsapi.Response{}, clientResolutionFailure("TLS client resolution failed", err)
	}
	if client == nil {
		return tlsapi.Response{}, clientResolutionFailure("nil TLS client", errors.New("nil TLS client"))
	}
	return client.Do(ctx, method, path, query, header, body)
}

func clientResolutionFailure(message string, cause error) error {
	return &execution.Failure{Stage: "resolve_client", Outcome: execution.OutcomeNotSent, Message: message, Cause: cause}
}

func cloneStringMap(source map[string]string) map[string]string {
	if source == nil {
		return map[string]string{}
	}
	cloned := make(map[string]string, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

var _ execution.Transport = (*Transport)(nil)
