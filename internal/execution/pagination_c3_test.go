package execution

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
)

func pageNumberC3Spec(maxPages int) contract.PaginationSpec {
	return contract.PaginationSpec{
		Mode: contract.PaginationPageNumber, PageNumberParam: "Page", PageSizeParam: "Size",
		ItemsField: "Items", TotalField: "Total", DefaultPageSize: 2, MaxPages: maxPages,
	}
}

func cursorC3Spec(maxPages int) contract.PaginationSpec {
	return contract.PaginationSpec{
		Mode: contract.PaginationCursor, CursorParam: "After", NextCursorField: "Next",
		ItemsField: "Items", TotalField: "Total", MaxPages: maxPages,
	}
}

func TestPageNumberC3TerminalEvidence(t *testing.T) {
	tests := []struct {
		name, first, second, reason string
		wantErr                     bool
	}{
		{"total overrides short pages", `{"Items":[1],"Total":2}`, `{"Items":[2],"Total":2}`, "total_reached", false},
		{"short at exact budget", `{"Items":[1,2]}`, `{"Items":[3]}`, "short_page", false},
		{"empty at exact budget", `{"Items":[1,2]}`, `{"Items":[]}`, "empty_page", false},
		{"contradictory empty page", `{"Items":[1],"Total":3}`, `{"Items":[]}`, "inconsistent_total", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			transport := &fakeTransport{responses: []Response{jsonResponse("one", tc.first), jsonResponse("two", tc.second)}}
			result, err := NewExecutor(transport, nil).Execute(context.Background(), Invocation{
				Operation: paginatedOperation(pageNumberC3Spec(2)), Options: Options{PageAll: true},
			})
			if (err != nil) != tc.wantErr || result.Pagination == nil || result.Pagination.StopReason != tc.reason ||
				result.Pagination.Complete == tc.wantErr || result.Pagination.PageCount != 2 || len(transport.requests) != 2 {
				t.Fatalf("result=%#v err=%v requests=%d", result, err, len(transport.requests))
			}
			if tc.wantErr && (result.Pagination.ItemCount != 1 || result.RemoteOutcome != OutcomePartial) {
				t.Fatalf("partial result=%#v", result)
			}
		})
	}
}

func TestPageNumberC3PreservesPreviouslyReportedTotal(t *testing.T) {
	for _, tc := range []struct {
		name       string
		pages      []string
		total      json.Number
		stopReason string
		items      int
		complete   bool
	}{
		{
			name:  "later pages omit total",
			pages: []string{`{"Items":[1],"Total":3.0}`, `{"Items":[2]}`, `{"Items":[3]}`},
			total: json.Number("3.0"), stopReason: "total_reached", items: 3, complete: true,
		},
		{
			name:  "latest reported total wins",
			pages: []string{`{"Items":[1],"Total":6}`, `{"Items":[2],"Total":3.0}`, `{"Items":[3]}`},
			total: json.Number("3.0"), stopReason: "total_reached", items: 3, complete: true,
		},
		{
			name:  "empty page below known total",
			pages: []string{`{"Items":[1],"Total":3.0}`, `{"Items":[]}`},
			total: json.Number("3.0"), stopReason: "inconsistent_total", items: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			responses := make([]Response, 0, len(tc.pages))
			for _, body := range tc.pages {
				responses = append(responses, jsonResponse("page", body))
			}
			transport := &fakeTransport{responses: responses}
			result, err := NewExecutor(transport, nil).Execute(context.Background(), Invocation{
				Operation: paginatedOperation(pageNumberC3Spec(3)), Options: Options{PageAll: true},
			})
			if tc.complete && err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if !tc.complete {
				var incomplete *IncompleteError
				if !errors.As(err, &incomplete) {
					t.Fatalf("expected incomplete error, got %v", err)
				}
			}
			if result.Pagination == nil || result.Pagination.Complete != tc.complete ||
				result.Pagination.StopReason != tc.stopReason || result.Pagination.ItemCount != tc.items ||
				result.Pagination.PageCount != len(tc.pages) || len(transport.requests) != len(tc.pages) {
				t.Fatalf("result=%#v err=%v requests=%d", result, err, len(transport.requests))
			}
			data, ok := result.Data.(map[string]any)
			rows, rowsOK := data["Items"].([]any)
			if !ok || data["Total"] != tc.total || !rowsOK || len(rows) != tc.items {
				t.Fatalf("data=%#v", result.Data)
			}
		})
	}
}

func TestPageNumberC3TotalValidation(t *testing.T) {
	for _, tc := range []struct{ total, reason string }{
		{"9223372036854775808", "max_pages"},
		{"1e100000000000000000000", "max_pages"},
		{"1e2", "max_pages"},
		{"-1", "invalid_total"},
		{"1.5", "invalid_total"},
		{`"not a number"`, "invalid_total"},
	} {
		t.Run(tc.total, func(t *testing.T) {
			transport := &fakeTransport{responses: []Response{jsonResponse("one", `{"Items":[1],"Total":`+tc.total+`}`)}}
			result, err := NewExecutor(transport, nil).Execute(context.Background(), Invocation{
				Operation: paginatedOperation(pageNumberC3Spec(1)), Options: Options{PageAll: true},
			})
			var incomplete *IncompleteError
			if !errors.As(err, &incomplete) || result.Pagination.Complete || result.Pagination.StopReason != tc.reason {
				t.Fatalf("result=%#v err=%v", result, err)
			}
		})
	}
}

func TestPageNumberC3InvalidNumberNeverCompletes(t *testing.T) {
	for _, value := range []any{json.Number("not-a-number"), json.Number("1 "), json.Number("-1"), json.Number("1.2"), json.Number("1e-100000000000000000")} {
		if _, valid := totalAtMost(value, 1); valid {
			t.Fatalf("invalid total accepted: %#v", value)
		}
	}
	for _, value := range []any{json.Number("9223372036854775808"), json.Number("1e100000000000000000")} {
		if atMost, valid := totalAtMost(value, 1); !valid || atMost {
			t.Fatalf("large total = %#v, atMost=%t valid=%t", value, atMost, valid)
		}
	}
}

func TestCursorC3EmptyIntermediateAndTerminal(t *testing.T) {
	transport := &fakeTransport{responses: []Response{
		jsonResponse("one", `{"Items":[1],"Next":"a","Total":1}`),
		jsonResponse("two", `{"Items":[],"Next":"b","Total":1}`),
		jsonResponse("three", `{"Items":[2],"Next":"","Total":2}`),
	}}
	result, err := NewExecutor(transport, nil).Execute(context.Background(), Invocation{
		Operation: paginatedOperation(cursorC3Spec(3)), Options: Options{PageAll: true},
	})
	if err != nil || !result.Pagination.Complete || result.Pagination.StopReason != "cursor_end" ||
		result.Pagination.ItemCount != 2 || len(transport.requests) != 3 ||
		transport.requests[1].Query["After"] != "a" || transport.requests[2].Query["After"] != "b" {
		t.Fatalf("result=%#v err=%v requests=%#v", result, err, transport.requests)
	}
	if data := result.Data.(map[string]any); data["Total"] != json.Number("2") {
		t.Fatalf("server total changed: %#v", data)
	}
}

func TestCursorC3MissingStalledAndContradictory(t *testing.T) {
	for _, tc := range []struct {
		name, first, second, reason string
		count                       int
	}{
		{"missing", `{"Items":[1]}`, "", "missing_cursor", 1},
		{"wrong type", `{"Items":[1],"Next":12}`, "", "missing_cursor", 1},
		{"repeated", `{"Items":[1],"Next":"a"}`, `{"Items":[2],"Next":"a"}`, "cursor_stalled", 2},
		{"contradictory total", `{"Items":[1],"Next":"","Total":2}`, "", "inconsistent_total", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			responses := []Response{jsonResponse("one", tc.first)}
			if tc.second != "" {
				responses = append(responses, jsonResponse("two", tc.second))
			}
			transport := &fakeTransport{responses: responses}
			result, err := NewExecutor(transport, nil).Execute(context.Background(), Invocation{
				Operation: paginatedOperation(cursorC3Spec(4)), Options: Options{PageAll: true},
			})
			var incomplete *IncompleteError
			if !errors.As(err, &incomplete) || result.Pagination.StopReason != tc.reason || result.Pagination.Complete ||
				result.Pagination.Resumable || result.Pagination.Next != nil || len(transport.requests) != tc.count ||
				result.Pagination.ItemCount != tc.count {
				t.Fatalf("result=%#v err=%v requests=%d", result, err, len(transport.requests))
			}
		})
	}
}

func TestCursorC3ContinuationCanBeConsumed(t *testing.T) {
	op := paginatedOperation(cursorC3Spec(1))
	transport := &fakeTransport{responses: []Response{
		jsonResponse("one", `{"Items":[1],"Next":"cursor-2"}`),
		jsonResponse("two", `{"Items":[2],"Next":""}`),
	}}
	executor := NewExecutor(transport, nil)
	result, err := executor.Execute(context.Background(), Invocation{
		Operation: op, Input: Input{Query: map[string]any{"Enabled": true}}, Options: Options{PageAll: true},
	})
	var incomplete *IncompleteError
	if !errors.As(err, &incomplete) || !result.Pagination.Resumable || result.Pagination.Next.PageAll {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	next, err := NormalizeInput(op, result.Pagination.Next.Input)
	if err != nil {
		t.Fatal(err)
	}
	continued, err := executor.Execute(context.Background(), Invocation{Operation: op, Input: next})
	if err != nil || continued.Pagination != nil || len(transport.requests) != 2 ||
		transport.requests[1].Query["After"] != "cursor-2" || transport.requests[1].Query["Enabled"] != "true" {
		t.Fatalf("continued=%#v err=%v requests=%#v", continued, err, transport.requests)
	}
}

func TestPageNumberC3ContinuationCanBeConsumed(t *testing.T) {
	op := paginatedOperation(pageNumberC3Spec(1))
	op.Wire.Path = "/projects/{ProjectID}/List"
	input := Input{
		Path:   map[string]any{"ProjectID": "project-1"},
		Query:  map[string]any{"Enabled": true, "Limit": json.Number("9007199254740993"), "Tags": []any{"a", "b"}},
		Header: map[string]any{"X-Trace": "trace-1"},
		Body:   Payload{JSON: map[string]any{"filter": map[string]any{"enabled": true}}, Present: true},
	}
	snapshot := cloneInputForTest(input)
	transport := &fakeTransport{responses: []Response{
		jsonResponse("one", `{"Items":[1,2],"Total":4}`),
		jsonResponse("two", `{"Items":[3,4],"Total":4}`),
	}}
	executor := NewExecutor(transport, nil)
	result, err := executor.Execute(context.Background(), Invocation{Operation: op, Input: input, Options: Options{PageAll: true}})
	var incomplete *IncompleteError
	if !errors.As(err, &incomplete) || result.Pagination.StopReason != "max_pages" || !result.Pagination.Resumable {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	next := result.Pagination.Next
	query := next.Input["query"].(map[string]any)
	if next.PageAll || next.OperationID != string(op.ID) || query["Page"] != 2 || query["Size"] != 2 ||
		query["Enabled"] != true || query["Limit"] != json.Number("9007199254740993") {
		t.Fatalf("continuation=%#v", next)
	}
	continuedInput, err := NormalizeInput(op, next.Input)
	if err != nil {
		t.Fatal(err)
	}
	continued, err := executor.Execute(context.Background(), Invocation{Operation: op, Input: continuedInput, Options: Options{PageAll: next.PageAll}})
	if err != nil || continued.Pagination != nil || len(transport.requests) != 2 {
		t.Fatalf("continued=%#v err=%v", continued, err)
	}
	first, second := transport.requests[0], transport.requests[1]
	if second.Query["Page"] != "2" || second.Query["Size"] != "2" || second.Query["Enabled"] != "true" ||
		second.Path != first.Path || !reflect.DeepEqual(second.QueryMulti, first.QueryMulti) ||
		!reflect.DeepEqual(second.Header, first.Header) || string(second.Body) != string(first.Body) ||
		!reflect.DeepEqual(input, snapshot) {
		t.Fatalf("first=%#v second=%#v input=%#v", first, second, input)
	}
}

func TestC3SecondPageFailureContinuationAndMalformedList(t *testing.T) {
	for _, tc := range []struct {
		name         string
		response     Response
		transportErr error
		pages        int
	}{
		{"transport", Response{StatusCode: http.StatusBadGateway, Header: http.Header{"X-Tls-Requestid": {"failed"}}}, errors.New("connection reset"), 1},
		{"decode", Response{StatusCode: http.StatusOK, Header: http.Header{"X-Tls-Requestid": {"failed"}}, Body: []byte("bad")}, nil, 1},
		{"malformed list", jsonResponse("failed", `{"Items":{}}`), nil, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transport := &fakeTransport{responses: []Response{jsonResponse("first", `{"Items":[1,2],"Total":4}`), tc.response, jsonResponse("retry", `{"Items":[3,4],"Total":4}`)}, errors: []error{nil, tc.transportErr}}
			op := paginatedOperation(pageNumberC3Spec(3))
			executor := NewExecutor(transport, nil)
			result, err := executor.Execute(context.Background(), Invocation{Operation: op, Options: Options{PageAll: true}})
			var incomplete *IncompleteError
			if !errors.As(err, &incomplete) || result.RequestID != "failed" || result.StatusCode != tc.response.StatusCode ||
				result.RemoteOutcome != OutcomePartial || result.Pagination.PageCount != tc.pages ||
				result.Pagination.ItemCount != 2 || !result.Pagination.Resumable || result.Pagination.Next.Input["query"].(map[string]any)["Page"] != 2 ||
				!reflect.DeepEqual(result.Data.(map[string]any)["Items"], []any{json.Number("1"), json.Number("2")}) {
				t.Fatalf("result=%#v err=%v", result, err)
			}
			retryInput, normalizeErr := NormalizeInput(op, result.Pagination.Next.Input)
			if normalizeErr != nil {
				t.Fatal(normalizeErr)
			}
			if _, retryErr := executor.Execute(context.Background(), Invocation{Operation: op, Input: retryInput}); retryErr != nil || transport.requests[2].Query["Page"] != "2" {
				t.Fatalf("retry error=%v requests=%#v", retryErr, transport.requests)
			}
		})
	}
}

func TestC3FirstPageFailurePreservesCauseAndOutcome(t *testing.T) {
	transport := &fakeTransport{responses: []Response{{StatusCode: http.StatusTooManyRequests, Header: http.Header{"X-Tls-Requestid": {"failed"}}, Body: []byte(`{"ErrorCode":"Throttling"}`)}}}
	result, err := NewExecutor(transport, nil).Execute(context.Background(), Invocation{
		Operation: paginatedOperation(pageNumberC3Spec(2)), Options: Options{PageAll: true},
	})
	var incomplete *IncompleteError
	var httpErr *HTTPError
	if !errors.As(err, &incomplete) || !errors.As(err, &httpErr) || incomplete.ErrorKind() != "" ||
		result.RemoteOutcome != OutcomeFailed || result.StatusCode != http.StatusTooManyRequests || result.RequestID != "failed" ||
		result.Pagination.PageCount != 0 || result.Pagination.ItemCount != 0 || result.Data != nil {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestC3FirstPageNonObjectIsProtocolFailure(t *testing.T) {
	transport := &fakeTransport{responses: []Response{jsonResponse("bad-shape", `[1,2]`)}}
	result, err := NewExecutor(transport, nil).Execute(context.Background(), Invocation{
		Operation: paginatedOperation(pageNumberC3Spec(2)), Options: Options{PageAll: true},
	})
	var incomplete *IncompleteError
	var failure *Failure
	if !errors.As(err, &incomplete) || !errors.As(err, &failure) || failure.Kind != "protocol" ||
		result.RemoteOutcome != OutcomeUnknown || result.Pagination.PageCount != 0 || result.Pagination.StopReason != "invalid_response" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

type cancelOnFirstPageTransport struct {
	cancel context.CancelFunc
	calls  int
}

func (t *cancelOnFirstPageTransport) Do(_ context.Context, _ Request) (Response, error) {
	t.calls++
	t.cancel()
	return jsonResponse("first", `{"Items":[1,2],"Total":4}`), nil
}

func TestC3CancellationStopsBeforeNextSend(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	transport := &cancelOnFirstPageTransport{cancel: cancel}
	result, err := NewExecutor(transport, nil).Execute(ctx, Invocation{
		Operation: paginatedOperation(pageNumberC3Spec(5)), Options: Options{PageAll: true},
	})
	if !errors.Is(err, context.Canceled) || result.Pagination.StopReason != "cancelled" || transport.calls != 1 ||
		result.Pagination.ItemCount != 2 || result.RemoteOutcome != OutcomePartial {
		t.Fatalf("result=%#v err=%v calls=%d", result, err, transport.calls)
	}

	expired, done := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer done()
	before := &fakeTransport{}
	result, err = NewExecutor(before, nil).Execute(expired, Invocation{
		Operation: paginatedOperation(pageNumberC3Spec(5)), Options: Options{PageAll: true},
	})
	if !errors.Is(err, context.DeadlineExceeded) || result.Pagination.StopReason != "deadline" ||
		len(before.requests) != 0 || result.RemoteOutcome != OutcomeNotSent {
		t.Fatalf("result=%#v err=%v requests=%d", result, err, len(before.requests))
	}
}

func TestC3CancellationDuringEncodeStopsSend(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	codec := &countingCodec{encode: func(request Request) (Request, any, error) {
		cancel()
		return request, nil, nil
	}}
	registry := NewCodecRegistry()
	if err := registry.Register(contract.CodecJSON, codec); err != nil {
		t.Fatal(err)
	}
	transport := &fakeTransport{}
	result, err := NewExecutor(transport, registry).Execute(ctx, Invocation{
		Operation: paginatedOperation(pageNumberC3Spec(2)), Options: Options{PageAll: true},
	})
	if !errors.Is(err, context.Canceled) || result.Pagination.StopReason != "cancelled" ||
		len(transport.requests) != 0 || result.RemoteOutcome != OutcomeNotSent {
		t.Fatalf("result=%#v err=%v requests=%d", result, err, len(transport.requests))
	}
}

func TestC3RejectsMultiValueStart(t *testing.T) {
	for _, tc := range []struct {
		spec  contract.PaginationSpec
		query map[string]any
	}{
		{pageNumberC3Spec(2), map[string]any{"Page": []any{2}}},
		{cursorC3Spec(2), map[string]any{"After": []string{"cursor"}}},
	} {
		transport := &fakeTransport{}
		_, err := NewExecutor(transport, nil).Execute(context.Background(), Invocation{
			Operation: paginatedOperation(tc.spec), Input: Input{Query: tc.query}, Options: Options{PageAll: true},
		})
		if err == nil || len(transport.requests) != 0 {
			t.Fatalf("spec=%#v err=%v requests=%d", tc.spec, err, len(transport.requests))
		}
	}
}
