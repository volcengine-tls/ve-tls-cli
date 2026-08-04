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
	response, err := t.DoRaw(
		ctx,
		request.Method,
		request.Path,
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
		return tlsapi.Response{}, errors.New("nil client source")
	}
	client, err := t.source()
	if err != nil {
		return tlsapi.Response{}, err
	}
	if client == nil {
		return tlsapi.Response{}, errors.New("nil TLS client")
	}
	return client.Do(ctx, method, path, query, header, body)
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
