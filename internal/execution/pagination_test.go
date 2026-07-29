package execution

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
)

func TestExecutorPageNumberPaginationUsesOnlyOperationMetadata(t *testing.T) {
	op := paginatedOperation(contract.PaginationSpec{
		Mode:            contract.PaginationPageNumber,
		PageNumberParam: "Number",
		PageSizeParam:   "Size",
		ItemsField:      "Rows",
		TotalField:      "TotalCount",
		DefaultPageSize: 2,
		MaxPages:        5,
	})
	transport := &fakeTransport{responses: []Response{
		jsonResponse("req-page-1", `{"Rows":[{"id":1},{"id":2}],"TotalCount":3,"Keep":"first"}`),
		jsonResponse("req-page-2", `{"Rows":[{"id":3}],"TotalCount":3,"Keep":"last"}`),
	}}
	input := Input{Query: map[string]any{"Filter": "x"}}
	snapshot := cloneInputForTest(input)
	result, err := NewExecutor(transport, NewCodecRegistry()).Execute(context.Background(), Invocation{
		Operation: op,
		Input:     input,
		Options:   Options{PageAll: true},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(transport.requests) != 2 ||
		transport.requests[0].Query["Number"] != "1" ||
		transport.requests[0].Query["Size"] != "2" ||
		transport.requests[1].Query["Number"] != "2" {
		t.Fatalf("requests = %#v", transport.requests)
	}
	data := result.Data.(map[string]any)
	if len(data["Rows"].([]any)) != 3 || data["TotalCount"] != 3 || data["Keep"] != "last" {
		t.Fatalf("data = %#v", data)
	}
	if result.RequestID != "req-page-2" || result.Pagination.PageCount != 2 ||
		result.Pagination.PageSize != 2 || !result.Pagination.Merged {
		t.Fatalf("result = %#v", result)
	}
	if !reflect.DeepEqual(input, snapshot) {
		t.Fatalf("pagination mutated input: got %#v want %#v", input, snapshot)
	}
}

func TestExecutorCursorPaginationAndMaxPageGuard(t *testing.T) {
	op := paginatedOperation(contract.PaginationSpec{
		Mode:            contract.PaginationCursor,
		CursorParam:     "After",
		NextCursorField: "Next",
		ItemsField:      "Items",
		TotalField:      "Total",
		MaxPages:        2,
	})
	transport := &fakeTransport{responses: []Response{
		jsonResponse("req-cursor-1", `{"Items":[1,2],"Next":"cursor-2","Total":4}`),
		jsonResponse("req-cursor-2", `{"Items":[3,4],"Next":"cursor-3","Total":4}`),
		jsonResponse("must-not-run", `{"Items":[5]}`),
	}}
	result, err := NewExecutor(transport, NewCodecRegistry()).Execute(context.Background(), Invocation{
		Operation: op,
		Input:     Input{},
		Options:   Options{PageAll: true},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(transport.requests) != 2 ||
		transport.requests[0].Query["After"] != "" ||
		transport.requests[1].Query["After"] != "cursor-2" {
		t.Fatalf("requests = %#v", transport.requests)
	}
	data := result.Data.(map[string]any)
	if !reflect.DeepEqual(data["Items"], []any{float64(1), float64(2), float64(3), float64(4)}) ||
		data["Total"] != 4 {
		t.Fatalf("data = %#v", data)
	}
	if result.Pagination.PageCount != 2 {
		t.Fatalf("pagination = %#v", result.Pagination)
	}
}

func TestExecutorPaginationErrorsUseCurrentAttemptMetadata(t *testing.T) {
	op := paginatedOperation(contract.PaginationSpec{
		Mode:            contract.PaginationPageNumber,
		PageNumberParam: "PageNumber",
		PageSizeParam:   "PageSize",
		ItemsField:      "Rows",
		DefaultPageSize: 1,
		MaxPages:        5,
	})
	sentinel := errors.New("second page transport failure")
	transport := &fakeTransport{
		responses: []Response{
			jsonResponse("req-first", `{"Rows":[1],"Total":2}`),
			{},
		},
		errors: []error{nil, sentinel},
	}
	result, err := NewExecutor(transport, NewCodecRegistry()).Execute(context.Background(), Invocation{
		Operation: op,
		Options:   Options{PageAll: true},
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v", err)
	}
	if result.RequestID != "" || result.StatusCode != 0 {
		t.Fatalf("stale metadata leaked: %#v", result)
	}
	if result.Pagination == nil || result.Pagination.PageCount != 1 {
		t.Fatalf("pagination progress = %#v", result.Pagination)
	}
	if result.Pagination.Merged {
		t.Fatalf("failed pagination incorrectly reports merged: %#v", result.Pagination)
	}
}

func TestExecutorPaginationRejectsExplicitStartAndMissingItems(t *testing.T) {
	op := paginatedOperation(contract.PaginationSpec{
		Mode:            contract.PaginationCursor,
		CursorParam:     "Cursor",
		NextCursorField: "Next",
		ItemsField:      "Items",
		MaxPages:        2,
	})
	executor := NewExecutor(&fakeTransport{}, NewCodecRegistry())
	_, err := executor.Execute(context.Background(), Invocation{
		Operation: op,
		Input:     Input{Query: map[string]any{"Cursor": "already-started"}},
		Options:   Options{PageAll: true},
	})
	if err == nil || err.Error() != "--page-all cannot be used with Cursor" {
		t.Fatalf("explicit cursor error = %v", err)
	}

	transport := &fakeTransport{responses: []Response{jsonResponse("req", `{"Other":[]}`)}}
	result, err := NewExecutor(transport, NewCodecRegistry()).Execute(context.Background(), Invocation{
		Operation: op,
		Options:   Options{PageAll: true},
	})
	if err == nil || err.Error() != "unexpected list field: Items" {
		t.Fatalf("items error = %v", err)
	}
	if result.Pagination == nil || result.Pagination.PageCount != 1 {
		t.Fatalf("received page was not counted: %#v", result.Pagination)
	}
	if result.Pagination.Merged {
		t.Fatalf("failed pagination incorrectly reports merged: %#v", result.Pagination)
	}
}

func TestExecutorPaginationRejectsCrossModeStartFieldsFromMetadata(t *testing.T) {
	pageNumber := paginatedOperation(contract.PaginationSpec{
		Mode:            contract.PaginationPageNumber,
		PageNumberParam: "PageNumber",
		PageSizeParam:   "PageSize",
		CursorParam:     "Cursor",
		ItemsField:      "Items",
		DefaultPageSize: 100,
		MaxPages:        2,
	})
	_, err := NewExecutor(&fakeTransport{}, NewCodecRegistry()).Execute(context.Background(), Invocation{
		Operation: pageNumber,
		Input:     Input{Query: map[string]any{"Cursor": "cursor-1"}},
		Options:   Options{PageAll: true},
	})
	if err == nil || err.Error() != "--page-all cannot be used with Cursor" {
		t.Fatalf("page-number cursor conflict = %v", err)
	}

	cursor := paginatedOperation(contract.PaginationSpec{
		Mode:            contract.PaginationCursor,
		PageNumberParam: "PageNumber",
		CursorParam:     "Cursor",
		NextCursorField: "Next",
		ItemsField:      "Items",
		MaxPages:        2,
	})
	_, err = NewExecutor(&fakeTransport{}, NewCodecRegistry()).Execute(context.Background(), Invocation{
		Operation: cursor,
		Input:     Input{Query: map[string]any{"PageNumber": 2}},
		Options:   Options{PageAll: true},
	})
	if err == nil || err.Error() != "--page-all cannot be used with PageNumber" {
		t.Fatalf("cursor page-number conflict = %v", err)
	}
}

func TestEmbeddedDescribeTopicsPageAllRejectsCursorFromCatalogMetadata(t *testing.T) {
	catalog, err := contract.LoadEmbedded()
	if err != nil {
		t.Fatalf("LoadEmbedded: %v", err)
	}
	var operation contract.Operation
	for _, candidate := range catalog.Operations {
		if candidate.ID == "topic.describe-topics" {
			operation = candidate
			break
		}
	}
	if operation.ID == "" {
		t.Fatal("embedded catalog is missing topic.describe-topics")
	}
	transport := &fakeTransport{}
	_, err = NewExecutor(transport, NewCodecRegistry()).Execute(context.Background(), Invocation{
		Operation: operation,
		Input:     Input{Query: map[string]any{"Cursor": "cursor-1"}},
		Options:   Options{PageAll: true},
	})
	if err == nil || err.Error() != "--page-all cannot be used with Cursor" {
		t.Fatalf("error = %v", err)
	}
	if len(transport.requests) != 0 {
		t.Fatalf("transport calls = %d", len(transport.requests))
	}
}

func paginatedOperation(spec contract.PaginationSpec) contract.Operation {
	return contract.Operation{
		ID: "synthetic.list",
		Wire: contract.WireSpec{
			Method:        "GET",
			Path:          "/List",
			RequestFormat: "json",
			Codec:         contract.CodecJSON,
		},
		InputSchema: contract.JSONSchema{},
		Pagination:  &spec,
	}
}

func jsonResponse(requestID, body string) Response {
	return Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"X-Tls-Requestid": []string{requestID}},
		Body:       []byte(body),
	}
}
