package cli

import (
	"encoding/json"
	"errors"

	"tlsctl/internal/tlsapi"
	"tlsctl/internal/util"
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
	var s string
	if json.Unmarshal(resp.Body, &s) == nil {
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
