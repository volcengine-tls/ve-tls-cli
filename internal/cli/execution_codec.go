package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
	"github.com/volcengine-tls/ve-tls-cli/internal/execution"
	"github.com/volcengine-tls/ve-tls-cli/internal/tlsapi"
)

type toolExecutionCodec struct {
	profile  specialIOProfile
	context  *Context
	fallback execution.Codec
}

func newToolExecutionCodecRegistry(ctx *Context) (*execution.CodecRegistry, error) {
	registry := execution.NewCodecRegistry()
	fallback, err := registry.Resolve(contract.CodecJSON)
	if err != nil {
		return nil, err
	}
	for id, profile := range map[contract.CodecID]specialIOProfile{
		contract.CodecPutLogs:     specialIOProfilePutLogs,
		contract.CodecWebTracks:   specialIOProfileWebTracks,
		contract.CodecConsumeLogs: specialIOProfileConsumeLogs,
	} {
		if err := registry.Register(id, toolExecutionCodec{
			profile:  profile,
			context:  ctx,
			fallback: fallback,
		}); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func (c toolExecutionCodec) Encode(
	_ context.Context,
	operation contract.Operation,
	request execution.Request,
) (execution.Request, any, error) {
	meta := c.meta(operation, request)
	header, body, state, handled, err := prepareSpecialIORequestForProfile(
		c.profile,
		meta,
		request.Header,
		request.Body,
	)
	if err != nil {
		return execution.Request{}, nil, err
	}
	if !handled {
		return execution.Request{}, nil, fmt.Errorf("special I/O codec %s did not handle operation %s", operation.Wire.Codec, operation.ID)
	}
	request.Header = header
	request.Body = body
	return request, state, nil
}

func (c toolExecutionCodec) Decode(
	ctx context.Context,
	operation contract.Operation,
	response execution.Response,
	state any,
) (any, error) {
	var specialState *specialIOState
	if state != nil {
		var ok bool
		specialState, ok = state.(*specialIOState)
		if !ok {
			return nil, errors.New("invalid special I/O codec state")
		}
	}
	meta := c.meta(operation, execution.Request{})
	out, handled, err := decodeSpecialIOResponseForProfile(c.profile, meta, specialState, tlsapi.Response{
		StatusCode: response.StatusCode,
		Header:     response.Header.Clone(),
		Body:       append([]byte(nil), response.Body...),
	})
	if handled {
		return out, err
	}
	return c.fallback.Decode(ctx, operation, response, state)
}

func (c toolExecutionCodec) meta(operation contract.Operation, request execution.Request) apiIOMeta {
	meta := apiIOMeta{
		Group:         operation.Group,
		Action:        operation.Action,
		Method:        operation.Wire.Method,
		Path:          operation.Wire.Path,
		RequestFormat: requestFormat(operation.Wire.RequestFormat),
	}
	if request.Method != "" {
		meta.Method = request.Method
	}
	if request.Path != "" {
		meta.Path = request.Path
	}
	if request.BodyFormat != "" {
		meta.RequestFormat = requestFormat(request.BodyFormat)
	}
	if c.context != nil {
		meta.OutputFormat = c.context.Format
		meta.OutputMode = c.context.OutputMode
	}
	return meta
}
