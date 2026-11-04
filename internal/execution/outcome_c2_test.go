package execution

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
	"github.com/volcengine-tls/ve-tls-cli/internal/jsonx"
)

func TestExecutorOutcomeBeforeSending(t *testing.T) {
	t.Run("validation", func(t *testing.T) {
		transport := &fakeTransport{}
		op := requestTestOperation()
		result, err := NewExecutor(transport, nil).Execute(context.Background(), Invocation{Operation: op})
		if err == nil || result.OperationID != op.ID || result.RemoteOutcome != OutcomeNotSent || len(transport.requests) != 0 {
			t.Fatalf("result=%#v err=%v requests=%d", result, err, len(transport.requests))
		}
	})

	for _, dryRun := range []bool{false, true} {
		name := "encode"
		if dryRun {
			name = "dry-run encode"
		}
		t.Run(name, func(t *testing.T) {
			sentinel := errors.New("encode failed")
			codec := &countingCodec{encode: func(Request) (Request, any, error) {
				return Request{}, nil, sentinel
			}}
			registry := NewCodecRegistry()
			if err := registry.Register(contract.CodecJSON, codec); err != nil {
				t.Fatal(err)
			}
			transport := &fakeTransport{}
			op := requestTestOperation()
			result, err := NewExecutor(transport, registry).Execute(context.Background(), Invocation{
				Operation: op,
				Input:     Input{Path: map[string]any{"ProjectId": "p1"}},
				Options:   Options{DryRun: dryRun},
			})
			if !errors.Is(err, sentinel) || result.OperationID != op.ID || result.RemoteOutcome != OutcomeNotSent ||
				len(transport.requests) != 0 || codec.decodeCalls != 0 {
				t.Fatalf("result=%#v err=%v requests=%d decode=%d", result, err, len(transport.requests), codec.decodeCalls)
			}
		})
	}

	t.Run("pagination preflight", func(t *testing.T) {
		transport := &fakeTransport{}
		op := paginatedOperation(contract.PaginationSpec{Mode: contract.PaginationPageNumber})
		result, err := NewExecutor(transport, nil).Execute(context.Background(), Invocation{
			Operation: op,
			Options:   Options{PageAll: true},
		})
		if err == nil || result.OperationID != op.ID || result.RemoteOutcome != OutcomeNotSent || len(transport.requests) != 0 {
			t.Fatalf("result=%#v err=%v requests=%d", result, err, len(transport.requests))
		}
	})

	t.Run("dry-run", func(t *testing.T) {
		transport := &fakeTransport{}
		result, err := NewExecutor(transport, nil).Execute(context.Background(), Invocation{
			Operation: requestTestOperation(),
			Input:     Input{Path: map[string]any{"ProjectId": "p1"}},
			Options:   Options{DryRun: true},
			Runtime:   RuntimeView{Endpoint: "https://tls.example.com", Region: "cn-beijing"},
		})
		if err != nil || result.Plan == nil || !result.Plan.Valid || result.RemoteOutcome != OutcomeNotSent || len(transport.requests) != 0 {
			t.Fatalf("result=%#v err=%v requests=%d", result, err, len(transport.requests))
		}
	})
}

func TestErrorOutcomeRetainsEvidenceThroughEmptyWrappers(t *testing.T) {
	unknown := &Failure{Outcome: OutcomeUnknown, Cause: errors.New("response interrupted")}
	for _, err := range []error{
		&Failure{Cause: unknown},
		errors.Join(&Failure{Cause: errors.New("no stage evidence")}, unknown),
	} {
		if got := ErrorOutcome(err, OutcomeNotSent); got != OutcomeUnknown {
			t.Fatalf("lost remote evidence: %q", got)
		}
	}
	if got := ErrorOutcome(&Failure{Outcome: OutcomePartial, Cause: unknown}, OutcomeNotSent); got != OutcomePartial {
		t.Fatalf("outer aggregate must win: %q", got)
	}
}

func TestExecutorOutcomeTransportEvidenceAndCurrentMetadata(t *testing.T) {
	sentinel := errors.New("transport failed")
	for _, tc := range []struct {
		name    string
		err     error
		outcome string
	}{
		{name: "no send evidence", err: sentinel, outcome: OutcomeUnknown},
		{name: "explicit pre-send failure", err: &Failure{Outcome: OutcomeNotSent, Cause: sentinel}, outcome: OutcomeNotSent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			codec := &countingCodec{}
			registry := NewCodecRegistry()
			if err := registry.Register(contract.CodecJSON, codec); err != nil {
				t.Fatal(err)
			}
			transport := &fakeTransport{
				responses: []Response{{StatusCode: http.StatusOK, Header: http.Header{"X-Tls-Requestid": {"req-headers"}}}},
				errors:    []error{tc.err},
			}
			result, err := NewExecutor(transport, registry).Execute(context.Background(), Invocation{
				Operation: requestTestOperation(),
				Input:     Input{Path: map[string]any{"ProjectId": "p1"}},
			})
			if !errors.Is(err, sentinel) || result.RemoteOutcome != tc.outcome || result.StatusCode != http.StatusOK ||
				result.RequestID != "req-headers" || codec.decodeCalls != 0 || len(transport.requests) != 1 {
				t.Fatalf("result=%#v err=%v requests=%d decode=%d", result, err, len(transport.requests), codec.decodeCalls)
			}
		})
	}
}

func TestExecutorJSONProtocolFailuresAndHTTPRejection(t *testing.T) {
	for _, tc := range []struct {
		name         string
		status       int
		body         string
		outcome      string
		protocol     string
		wantSyntax   bool
		wantTrailing bool
	}{
		{name: "HTML instead of JSON", status: http.StatusOK, body: "<html>error</html>", outcome: OutcomeUnknown, protocol: "InvalidJSONResponse", wantSyntax: true},
		{name: "invalid JSON", status: http.StatusOK, body: "{", outcome: OutcomeUnknown, protocol: "InvalidJSONResponse"},
		{name: "trailing JSON", status: http.StatusOK, body: `{"ok":true} {"extra":true}`, outcome: OutcomeUnknown, protocol: "InvalidJSONResponse", wantTrailing: true},
		{name: "unexpected 201", status: http.StatusCreated, body: `{"ok":true}`, outcome: OutcomeUnknown, protocol: "UnexpectedHTTPStatus"},
		{name: "unexpected 204", status: http.StatusNoContent, outcome: OutcomeUnknown, protocol: "UnexpectedHTTPStatus"},
		{name: "429 rejection", status: http.StatusTooManyRequests, body: `{"ErrorCode":"Throttling"}`, outcome: OutcomeFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transport := &fakeTransport{responses: []Response{{
				StatusCode: tc.status,
				Header:     http.Header{"X-Tls-Requestid": {"req-current"}},
				Body:       []byte(tc.body),
			}}}
			result, err := NewExecutor(transport, nil).Execute(context.Background(), Invocation{
				Operation: requestTestOperation(),
				Input:     Input{Path: map[string]any{"ProjectId": "p1"}},
			})
			if err == nil || result.RemoteOutcome != tc.outcome || result.StatusCode != tc.status ||
				result.RequestID != "req-current" || len(transport.requests) != 1 {
				t.Fatalf("result=%#v err=%v requests=%d", result, err, len(transport.requests))
			}
			if tc.protocol != "" {
				var failure *Failure
				if !errors.As(err, &failure) || failure.Kind != "protocol" || failure.Code != tc.protocol ||
					failure.Stage != "decode" || failure.Outcome != OutcomeUnknown {
					t.Fatalf("protocol failure = %#v, err=%v", failure, err)
				}
				var syntaxErr *json.SyntaxError
				if tc.wantSyntax && !errors.As(err, &syntaxErr) {
					t.Fatalf("error %v does not retain JSON syntax cause", err)
				}
				if tc.wantTrailing && !errors.Is(err, jsonx.ErrTrailingData) {
					t.Fatalf("error %v does not retain trailing-data cause", err)
				}
			} else {
				var httpErr *HTTPError
				if !errors.As(err, &httpErr) || httpErr.StatusCode != tc.status || httpErr.RequestID != "req-current" ||
					string(httpErr.Body) != tc.body || httpErr.RemoteOutcome() != OutcomeFailed {
					t.Fatalf("http error = %#v, err=%v", httpErr, err)
				}
			}
		})
	}
}

func TestExecutorJSONSuccessPreservesValuesAndRequestWire(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want any
	}{
		{name: "empty body", want: map[string]any{}},
		{name: "object and precise number", body: `{"big":9007199254740993}`, want: map[string]any{"big": json.Number("9007199254740993")}},
		{name: "string scalar", body: `"text"`, want: "text"},
		{name: "number scalar", body: `0.123456789012345678901`, want: json.Number("0.123456789012345678901")},
		{name: "array", body: `[true,null]`, want: []any{true, nil}},
		{name: "null", body: `null`, want: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transport := &fakeTransport{responses: []Response{jsonResponse("req-success", tc.body)}}
			result, err := NewExecutor(transport, nil).Execute(context.Background(), Invocation{
				Operation: requestTestOperation(),
				Input: Input{
					Path:   map[string]any{"ProjectId": "p1"},
					Query:  map[string]any{"Limit": 3, "Tags": []string{"a", "b"}},
					Header: map[string]any{"X-Test": true},
					Body:   Payload{JSON: map[string]any{"Name": "demo", "big": json.Number("9007199254740993")}, Format: BodyFormatJSON},
				},
			})
			if err != nil || result.RemoteOutcome != OutcomeSuccess || result.StatusCode != http.StatusOK ||
				result.RequestID != "req-success" || !reflect.DeepEqual(result.Data, tc.want) || len(transport.requests) != 1 {
				t.Fatalf("result=%#v err=%v requests=%d", result, err, len(transport.requests))
			}
			want := Request{
				Method: "POST", Path: "/projects/p1",
				Query: map[string]string{"Limit": "3"}, QueryMulti: map[string][]string{"Tags": {"a", "b"}},
				Header: map[string]string{"X-Test": "true"}, Body: []byte(`{"Name":"demo","big":9007199254740993}`), BodyFormat: BodyFormatJSON,
			}
			if !reflect.DeepEqual(transport.requests[0], want) {
				t.Fatalf("wire request = %#v, want %#v", transport.requests[0], want)
			}
		})
	}
}

func TestSpecialCodecAndFallbackKeepTheirResponseContract(t *testing.T) {
	op := requestTestOperation()
	op.Wire.Codec = contract.CodecPutLogs
	t.Run("custom binary codec", func(t *testing.T) {
		registry := NewCodecRegistry()
		codec := &countingCodec{decode: func(response Response, _ any) (any, error) {
			return response.Body, nil
		}}
		if err := registry.Register(contract.CodecPutLogs, codec); err != nil {
			t.Fatal(err)
		}
		transport := &fakeTransport{responses: []Response{{StatusCode: http.StatusCreated, Body: []byte("binary")}}}
		result, err := NewExecutor(transport, registry).Execute(context.Background(), Invocation{
			Operation: op,
			Input:     Input{Path: map[string]any{"ProjectId": "p1"}},
		})
		if err != nil || result.RemoteOutcome != OutcomeSuccess || !reflect.DeepEqual(result.Data, []byte("binary")) {
			t.Fatalf("result=%#v err=%v", result, err)
		}
	})
	t.Run("special JSON fallback", func(t *testing.T) {
		codec, err := NewCodecRegistry().Resolve(contract.CodecJSON)
		if err != nil {
			t.Fatal(err)
		}
		data, err := codec.Decode(context.Background(), op, Response{StatusCode: http.StatusCreated, Body: []byte("plain text")}, nil)
		if err != nil || !reflect.DeepEqual(data, map[string]any{"data": "plain text"}) {
			t.Fatalf("data=%#v err=%v", data, err)
		}
	})
}

func TestPaginationOutcomeUsesCurrentAttempt(t *testing.T) {
	for _, spec := range []contract.PaginationSpec{
		{Mode: contract.PaginationPageNumber, PageNumberParam: "Page", PageSizeParam: "Size", ItemsField: "Items", DefaultPageSize: 1, MaxPages: 2},
		{Mode: contract.PaginationCursor, CursorParam: "Cursor", NextCursorField: "Next", PageSizeParam: "Size", ItemsField: "Items", DefaultPageSize: 1, MaxPages: 2},
	} {
		for _, tc := range []struct {
			name    string
			status  int
			body    string
			err     error
			outcome string
		}{
			{name: "rejection", status: http.StatusTooManyRequests, body: `{"ErrorCode":"Throttling"}`, outcome: OutcomeFailed},
			{name: "protocol", status: http.StatusOK, body: "<html>", outcome: OutcomeUnknown},
			{name: "transport", status: http.StatusBadGateway, err: errors.New("read failed"), outcome: OutcomeUnknown},
		} {
			t.Run(string(spec.Mode)+"/"+tc.name, func(t *testing.T) {
				transport := &fakeTransport{
					responses: []Response{
						jsonResponse("req-first", `{"Items":[1],"Next":"next"}`),
						{StatusCode: tc.status, Header: http.Header{"X-Tls-Requestid": {"req-current"}}, Body: []byte(tc.body)},
					},
					errors: []error{nil, tc.err},
				}
				result, err := NewExecutor(transport, nil).Execute(context.Background(), Invocation{
					Operation: paginatedOperation(spec), Options: Options{PageAll: true},
				})
				var incomplete *IncompleteError
				if !errors.As(err, &incomplete) || ErrorOutcome(incomplete.Cause, OutcomeUnknown) != tc.outcome ||
					result.RemoteOutcome != OutcomePartial || result.RequestID != "req-current" ||
					result.StatusCode != tc.status || result.Pagination == nil || result.Pagination.PageCount != 1 ||
					!result.Pagination.Merged || result.Pagination.ItemCount != 1 || len(transport.requests) != 2 {
					t.Fatalf("result=%#v err=%v requests=%d", result, err, len(transport.requests))
				}
				if rows := result.Data.(map[string]any)["Items"]; !reflect.DeepEqual(rows, []any{json.Number("1")}) {
					t.Fatalf("partial rows = %#v", rows)
				}
			})
		}
	}

	t.Run("decoded page with missing list field", func(t *testing.T) {
		transport := &fakeTransport{responses: []Response{jsonResponse("req-current", `{"Other":[]}`)}}
		result, err := NewExecutor(transport, nil).Execute(context.Background(), Invocation{
			Operation: paginatedOperation(contract.PaginationSpec{
				Mode: contract.PaginationCursor, CursorParam: "Cursor", NextCursorField: "Next", ItemsField: "Items", MaxPages: 2,
			}),
			Options: Options{PageAll: true},
		})
		if err == nil || result.RemoteOutcome != OutcomePartial || result.RequestID != "req-current" ||
			result.StatusCode != http.StatusOK || result.Pagination == nil || result.Pagination.PageCount != 1 {
			t.Fatalf("result=%#v err=%v", result, err)
		}
	})
}
