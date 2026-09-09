package execution

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
	"github.com/volcengine-tls/ve-tls-cli/internal/jsonx"
)

type Codec interface {
	Encode(context.Context, contract.Operation, Request) (Request, any, error)
	Decode(context.Context, contract.Operation, Response, any) (any, error)
}

type CodecRegistry struct {
	codecs map[contract.CodecID]Codec
}

func NewCodecRegistry() *CodecRegistry {
	registry := &CodecRegistry{codecs: map[contract.CodecID]Codec{
		contract.CodecJSON: jsonCodec{},
	}}
	for _, id := range []contract.CodecID{
		contract.CodecPutLogs,
		contract.CodecWebTracks,
		contract.CodecConsumeLogs,
	} {
		registry.codecs[id] = unavailableCodec{id: id}
	}
	return registry
}

func (r *CodecRegistry) Register(id contract.CodecID, codec Codec) error {
	if r == nil {
		return errors.New("nil codec registry")
	}
	if strings.TrimSpace(string(id)) == "" {
		return errors.New("codec id is required")
	}
	if isNilCodec(codec) {
		return fmt.Errorf("codec %s is nil", id)
	}
	if r.codecs == nil {
		r.codecs = map[contract.CodecID]Codec{}
	}
	r.codecs[id] = codec
	return nil
}

func (r *CodecRegistry) Resolve(id contract.CodecID) (Codec, error) {
	if r == nil {
		return nil, errors.New("nil codec registry")
	}
	codec, ok := r.codecs[id]
	if !ok || isNilCodec(codec) {
		return nil, fmt.Errorf("unsupported codec: %s", id)
	}
	return codec, nil
}

func isNilCodec(codec Codec) bool {
	if codec == nil {
		return true
	}
	value := reflect.ValueOf(codec)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

type unavailableCodec struct {
	id contract.CodecID
}

func (c unavailableCodec) Encode(_ context.Context, _ contract.Operation, _ Request) (Request, any, error) {
	return Request{}, nil, fmt.Errorf("codec %s requires an application adapter", c.id)
}

func (c unavailableCodec) Decode(_ context.Context, _ contract.Operation, _ Response, _ any) (any, error) {
	return nil, fmt.Errorf("codec %s requires an application adapter", c.id)
}

type jsonCodec struct{}

func (jsonCodec) Encode(_ context.Context, _ contract.Operation, request Request) (Request, any, error) {
	return request, nil, nil
}

func (jsonCodec) Decode(_ context.Context, _ contract.Operation, response Response, _ any) (any, error) {
	return decodeJSONResponse(response)
}

func decodeJSONResponse(response Response) (any, error) {
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, &HTTPError{
			StatusCode: response.StatusCode,
			Body:       append([]byte(nil), response.Body...),
			RequestID:  response.Header.Get("x-tls-requestid"),
		}
	}
	if len(response.Body) == 0 {
		return map[string]any{}, nil
	}
	var value any
	if err := jsonx.Unmarshal(response.Body, &value); err == nil {
		return value, nil
	} else if errors.Is(err, jsonx.ErrTrailingData) {
		return nil, err
	}
	var text string
	if jsonx.Unmarshal(response.Body, &text) == nil {
		return map[string]any{"data": text}, nil
	}
	return map[string]any{"data": string(response.Body)}, nil
}
