package execution

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
)

func TestSinglePageCompletenessAndContinuation(t *testing.T) {
	for _, tc := range []struct {
		name, body, reason string
		query              map[string]any
		complete           bool
		nextPage           int
	}{
		{"first page has more", `{"Items":[1,2],"Total":3}`, "single_page", map[string]any{"Size": 2}, false, 2},
		{"service page size stays omitted", `{"Items":[1,2],"Total":59}`, "single_page", nil, false, 2},
		{"all rows in first page", `{"Items":[1,2],"Total":2}`, "total_reached", nil, true, 0},
		{"empty collection", `{"Items":[],"Total":0}`, "total_reached", nil, true, 0},
		{"total overrides short page", `{"Items":[1],"Total":3}`, "single_page", map[string]any{"Size": 2}, false, 2},
		{"middle page", `{"Items":[3,4],"Total":5}`, "single_page", map[string]any{"Page": 2, "Size": 2}, false, 3},
		{"last page is still only a slice", `{"Items":[3],"Total":3}`, "page_slice", map[string]any{"Page": 2, "Size": 2}, false, 0},
		{"unknown size requires one more page", `{"Items":[3],"Total":3}`, "single_page", map[string]any{"Page": 2}, false, 3},
		{"empty later page", `{"Items":[],"Total":3}`, "page_slice", map[string]any{"Page": 3}, false, 0},
		{"unknown total full page", `{"Items":[1,2]}`, "single_page", map[string]any{"Size": 2}, false, 2},
		{"unknown total short page", `{"Items":[1]}`, "short_page", map[string]any{"Size": 2}, true, 0},
		{"unknown total and size", `{"Items":[1]}`, "single_page", nil, false, 2},
		{"unknown total empty page", `{"Items":[]}`, "empty_page", nil, true, 0},
		{"unknown page must not imply complete", `{"Items":[1],"Total":1}`, "unknown_page", map[string]any{"Page": 0}, false, 0},
		{"large offset does not overflow", `{"Items":[1],"Total":9e18}`, "page_slice", map[string]any{"Page": math.MaxInt, "Size": 2}, false, 0},
		{"largest page cannot wrap continuation", `{"Items":[1],"Total":9e1000}`, "page_limit", map[string]any{"Page": math.MaxInt, "Size": 2}, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query := map[string]any{"Filter": "keep"}
			for k, v := range tc.query {
				query[k] = v
			}
			input := Input{Query: query}
			before := cloneInputForTest(input)
			transport := &fakeTransport{responses: []Response{jsonResponse("single-request", tc.body)}}
			result, err := NewExecutor(transport, nil).Execute(context.Background(), Invocation{
				Operation: paginatedOperation(pageNumberC3Spec(3)), Input: input,
			})
			if err != nil || len(transport.requests) != 1 || result.Pagination == nil {
				t.Fatalf("result=%#v err=%v requests=%d", result, err, len(transport.requests))
			}
			p := result.Pagination
			if p.Mode != "single_page" || p.Merged || p.Complete != tc.complete || p.StopReason != tc.reason || p.PageCount != 1 || p.Resumable != (tc.nextPage > 0) {
				t.Fatalf("pagination=%#v", p)
			}
			if result.RequestID != "single-request" || result.StatusCode != 200 || result.RemoteOutcome != OutcomeSuccess || !reflect.DeepEqual(input, before) {
				t.Fatalf("request metadata or input changed: result=%#v input=%#v", result, input)
			}
			if _, supplied := tc.query["Size"]; !supplied {
				if _, injected := transport.requests[0].Query["Size"]; injected || p.PageSize != 0 {
					t.Fatalf("service page-size default was replaced: %#v", transport.requests[0])
				}
			}
			if tc.nextPage == 0 {
				if p.Next != nil {
					t.Fatalf("unexpected continuation: %#v", p.Next)
				}
				return
			}
			next := p.Next
			if next == nil || next.PageAll || next.OperationID != "synthetic.list" {
				t.Fatalf("continuation=%#v", next)
			}
			q := next.Input["query"].(map[string]any)
			if q["Page"] != tc.nextPage || q["Filter"] != "keep" {
				t.Fatalf("continuation query=%#v", q)
			}
			if _, supplied := tc.query["Size"]; !supplied {
				if _, injected := q["Size"]; injected {
					t.Fatalf("continuation changed service default: %#v", q)
				}
			}
		})
	}
}

func TestSinglePageMalformedResponseRetainsData(t *testing.T) {
	for _, tc := range []struct{ body, reason string }{
		{`{"Items":"bad","Total":3}`, "invalid_response"},
		{`{"Items":[1],"Total":"3"}`, "invalid_total"},
		{`{"Items":[],"Total":3}`, "inconsistent_total"},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			transport := &fakeTransport{responses: []Response{jsonResponse("bad-page", tc.body)}}
			result, err := NewExecutor(transport, nil).Execute(context.Background(), Invocation{Operation: paginatedOperation(pageNumberC3Spec(2))})
			var incomplete *IncompleteError
			if !errors.As(err, &incomplete) || result.Pagination == nil || result.Pagination.Complete || result.Pagination.StopReason != tc.reason || !reflect.DeepEqual(incomplete.Data, result.Data) || result.RequestID != "bad-page" {
				t.Fatalf("result=%#v err=%v", result, err)
			}
		})
	}
}

func TestSinglePageContinuationPreservesSchemaTypesAndProtectsHeaders(t *testing.T) {
	op := paginatedOperation(pageNumberC3Spec(3))
	op.InputSchema = contract.JSONSchema{"query": map[string]any{"type": "object", "properties": map[string]any{"Page": map[string]any{"type": "string"}, "Size": map[string]any{"type": "string"}}}}
	op.InputSchema["header"] = map[string]any{"type": "object", "properties": map[string]any{
		"X-Caller": map[string]any{"type": "string"}, "Authorization": map[string]any{"type": "string"},
	}}
	for _, sensitive := range []bool{false, true} {
		input := Input{Query: map[string]any{"Size": "2"}, Header: map[string]any{"X-Caller": "kept"}}
		if sensitive {
			input.Header["Authorization"] = "secret-canary"
		}
		transport := &fakeTransport{responses: []Response{jsonResponse("first", `{"Items":[1,2],"Total":3}`), jsonResponse("next", `{"Items":[3],"Total":3}`)}}
		executor := NewExecutor(transport, nil)
		result, err := executor.Execute(context.Background(), Invocation{Operation: op, Input: input})
		if err != nil || result.Pagination == nil || result.Pagination.Complete {
			t.Fatalf("result=%#v err=%v", result, err)
		}
		if sensitive {
			if result.Pagination.Resumable || result.Pagination.Next != nil {
				t.Fatalf("sensitive header copied into continuation: %#v", result.Pagination)
			}
			continue
		}
		next := result.Pagination.Next
		q := next.Input["query"].(map[string]any)
		if q["Page"] != "2" || q["Size"] != "2" {
			t.Fatalf("wrong schema types: %#v", q)
		}
		resumedInput, err := NormalizeInput(op, next.Input)
		if err != nil {
			t.Fatal(err)
		}
		resumed, err := executor.Execute(context.Background(), Invocation{Operation: op, Input: resumedInput})
		if err != nil || resumed.Pagination.Resumable || resumed.Pagination.Complete || len(transport.requests) != 2 || transport.requests[1].Header["X-Caller"] != "kept" {
			t.Fatalf("resumed=%#v err=%v requests=%#v", resumed, err, transport.requests)
		}
	}
}

func TestSinglePageCursorContinuation(t *testing.T) {
	op := paginatedOperation(cursorC3Spec(3))
	transport := &fakeTransport{responses: []Response{jsonResponse("first", `{"Items":[1],"Next":"next-token","Total":2}`), jsonResponse("last", `{"Items":[2],"Next":"","Total":2}`)}}
	executor := NewExecutor(transport, nil)
	result, err := executor.Execute(context.Background(), Invocation{Operation: op})
	if err != nil || result.Pagination == nil || result.Pagination.Complete || !result.Pagination.Resumable {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	next := result.Pagination.Next
	resumed, err := executor.Execute(context.Background(), Invocation{Operation: op, Input: Input{Query: next.Input["query"].(map[string]any)}})
	if err != nil || resumed.Pagination.Complete || resumed.Pagination.Resumable || resumed.Pagination.StopReason != "page_slice" || transport.requests[1].Query["After"] != "next-token" {
		t.Fatalf("resumed=%#v err=%v requests=%#v", resumed, err, transport.requests)
	}
}

func TestSinglePageOpaqueCursorDoesNotInventPageContinuation(t *testing.T) {
	spec := pageNumberC3Spec(3)
	spec.CursorParam = "Cursor"
	transport := &fakeTransport{responses: []Response{jsonResponse("cursor", `{"Items":[1],"Total":3}`)}}
	result, err := NewExecutor(transport, nil).Execute(context.Background(), Invocation{Operation: paginatedOperation(spec), Input: Input{Query: map[string]any{"Cursor": "opaque"}}})
	if err != nil || result.Pagination == nil || result.Pagination.Complete || result.Pagination.Resumable || result.Pagination.Next != nil || result.Pagination.StopReason != "cursor_unavailable" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestSinglePageCursorNeedsUsableSuccessor(t *testing.T) {
	for _, tc := range []struct{ body, cursor, reason string }{
		{`{"Items":[1]}`, "", "missing_cursor"},
		{`{"Items":[1],"Next":"same"}`, "same", "cursor_stalled"},
		{`{"Items":[1],"Next":"","Total":3}`, "", "inconsistent_total"},
	} {
		transport := &fakeTransport{responses: []Response{jsonResponse("cursor", tc.body)}}
		result, err := NewExecutor(transport, nil).Execute(context.Background(), Invocation{
			Operation: paginatedOperation(cursorC3Spec(3)), Input: Input{Query: map[string]any{"After": tc.cursor}},
		})
		var incomplete *IncompleteError
		if !errors.As(err, &incomplete) || result.Pagination == nil || result.Pagination.Complete || result.Pagination.Resumable || result.Pagination.StopReason != tc.reason {
			t.Fatalf("result=%#v err=%v", result, err)
		}
	}
}
