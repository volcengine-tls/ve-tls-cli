package execution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
)

const fallbackPageSize = 100

func (e *Executor) executeAll(
	ctx context.Context,
	invocation Invocation,
	base Request,
	codec Codec,
) (Result, error) {
	spec := invocation.Operation.Pagination
	if spec == nil {
		return Result{OperationID: invocation.Operation.ID}, unsupportedPaginationError(invocation.Operation.ID)
	}
	if spec.MaxPages <= 0 {
		return Result{OperationID: invocation.Operation.ID}, errors.New("pagination max_pages must be positive")
	}
	if spec.Mode != contract.PaginationPageNumber && spec.Mode != contract.PaginationCursor {
		return Result{OperationID: invocation.Operation.ID}, fmt.Errorf("unsupported pagination mode: %s", spec.Mode)
	}
	request, pageSize, err := prepareFirstPaginatedRequest(base, spec)
	if err != nil {
		return Result{OperationID: invocation.Operation.ID}, err
	}

	collector := paginationCollector{invocation: invocation, spec: spec, pageSize: pageSize, rows: []any{}}
	seenCursors := make(map[string]struct{})
	cursor := ""
	for page := 1; page <= spec.MaxPages; page++ {
		current := cloneRequest(request)
		if spec.Mode == contract.PaginationPageNumber {
			current.Query[spec.PageNumberParam] = strconv.Itoa(page)
		} else if cursor != "" {
			current.Query[spec.CursorParam] = cursor
		}
		retry := func() *Continuation { return collector.continuation(page, cursor) }
		if err := ctx.Err(); err != nil {
			result := Result{OperationID: invocation.Operation.ID, RemoteOutcome: OutcomeNotSent}
			return collector.finish(result, cancellationReason(err), retry(), err)
		}
		result, err := e.executeOne(ctx, invocation.Operation, current, paginationContextCodec{Codec: codec})
		if err != nil {
			reason := "request_failed"
			if cancellation := paginationCancellation(ctx, err); cancellation != nil {
				reason = cancellationReason(cancellation)
				if !errors.Is(err, cancellation) {
					err = errors.Join(err, cancellation)
				}
			}
			return collector.finish(result, reason, retry(), err)
		}
		response, ok := result.Data.(map[string]any)
		if !ok {
			return collector.finish(result, "invalid_response", retry(), invalidPaginationResponse("unexpected list response"))
		}
		collector.pageCount++
		rows, ok := anySlice(response[spec.ItemsField])
		if !ok {
			return collector.finish(result, "invalid_response", retry(), invalidPaginationResponse("unexpected list field: "+spec.ItemsField))
		}
		collector.last = response
		collector.rows = append(collector.rows, rows...)
		if spec.TotalField != "" {
			if total, exists := response[spec.TotalField]; exists {
				collector.total, collector.hasTotal = total, true
			}
		}
		totalReached, validTotal := false, true
		if collector.hasTotal {
			totalReached, validTotal = totalAtMost(collector.total, len(collector.rows))
		}

		if spec.Mode == contract.PaginationPageNumber {
			if !validTotal {
				nextPage := page + 1
				if len(rows) == 0 {
					nextPage = page
				}
				return collector.finish(result, "invalid_total", collector.continuation(nextPage, ""), nil)
			}
			if collector.hasTotal && totalReached {
				return collector.finish(result, "total_reached", nil, nil)
			}
			if len(rows) == 0 {
				if collector.hasTotal {
					return collector.finish(result, "inconsistent_total", retry(), nil)
				}
				return collector.finish(result, "empty_page", nil, nil)
			}
			if !collector.hasTotal && len(rows) < pageSize {
				return collector.finish(result, "short_page", nil, nil)
			}
			if page == spec.MaxPages {
				return collector.finish(result, "max_pages", collector.continuation(page+1, ""), nil)
			}
			continue
		}

		next, ok := response[spec.NextCursorField].(string)
		if !ok {
			return collector.finish(result, "missing_cursor", nil, nil)
		}
		if next == "" {
			if !validTotal {
				return collector.finish(result, "invalid_total", nil, nil)
			}
			if collector.hasTotal && !totalReached {
				return collector.finish(result, "inconsistent_total", nil, nil)
			}
			return collector.finish(result, "cursor_end", nil, nil)
		}
		if _, repeated := seenCursors[next]; repeated {
			return collector.finish(result, "cursor_stalled", nil, nil)
		}
		seenCursors[next] = struct{}{}
		if page == spec.MaxPages {
			return collector.finish(result, "max_pages", collector.continuation(0, next), nil)
		}
		cursor = next
	}
	panic("unreachable pagination loop")
}

// A codec may run long enough for cancellation to arrive after the loop's
// preflight check. Check again before executeOne can send the encoded request.
type paginationContextCodec struct{ Codec }

func (c paginationContextCodec) Encode(ctx context.Context, operation contract.Operation, request Request) (Request, any, error) {
	encoded, state, err := c.Codec.Encode(ctx, operation, request)
	if err == nil && ctx.Err() != nil {
		return Request{}, nil, ctx.Err()
	}
	return encoded, state, err
}

func invalidPaginationResponse(message string) error {
	return &Failure{
		Kind: "protocol", Code: "UnexpectedListResponse", Stage: "decode", Outcome: OutcomeUnknown,
		Cause: errors.New(message),
	}
}

type paginationCollector struct {
	invocation Invocation
	spec       *contract.PaginationSpec
	pageSize   int
	pageCount  int
	rows       []any
	last       map[string]any
	total      any
	hasTotal   bool
}

func (c *paginationCollector) continuation(page int, cursor string) *Continuation {
	next := ContinuationInput(c.invocation.Operation.ID, c.invocation.Input)
	if next == nil {
		return nil
	}
	query, _ := next.Input["query"].(map[string]any)
	if query == nil {
		query = make(map[string]any)
		next.Input["query"] = query
	}
	if c.spec.Mode == contract.PaginationPageNumber {
		query[c.spec.PageNumberParam] = paginationNumberInput(c.invocation.Operation, c.spec.PageNumberParam, page)
	} else if cursor != "" {
		query[c.spec.CursorParam] = cursor
	}
	if c.spec.PageSizeParam != "" && c.pageSize > 0 {
		query[c.spec.PageSizeParam] = paginationNumberInput(c.invocation.Operation, c.spec.PageSizeParam, c.pageSize)
	}
	return next
}

func paginationNumberInput(operation contract.Operation, name string, value int) any {
	querySchema, _ := schemaMap(operation.InputSchema["query"])
	properties, _ := schemaMap(querySchema["properties"])
	property, _ := schemaMap(properties[name])
	if property["type"] == "string" {
		return strconv.Itoa(value)
	}
	return value
}

func (c *paginationCollector) finish(result Result, reason string, next *Continuation, cause error) (Result, error) {
	complete := reason == "total_reached" || reason == "short_page" || reason == "empty_page" || reason == "cursor_end"
	state := Completeness{
		Complete: complete, StopReason: reason, PageCount: c.pageCount, ItemCount: len(c.rows),
		Resumable: next != nil, Next: next,
	}
	result.Pagination = &PaginationResult{Completeness: state, Mode: "page_all", PageSize: c.pageSize, Merged: c.last != nil}
	if c.last != nil {
		data := make(map[string]any, len(c.last))
		for key, value := range c.last {
			data[key] = value
		}
		if c.hasTotal && c.spec.TotalField != "" {
			data[c.spec.TotalField] = c.total
		}
		data[c.spec.ItemsField] = append([]any{}, c.rows...)
		result.Data = data
	} else {
		result.Data = nil
	}
	if complete {
		return result, nil
	}
	incomplete := &IncompleteError{Data: result.Data, State: &result.Pagination.Completeness, Cause: cause}
	if c.pageCount > 0 {
		result.RemoteOutcome = OutcomePartial
	} else if result.RemoteOutcome == "" || result.RemoteOutcome == OutcomeSuccess {
		result.RemoteOutcome = ErrorOutcome(cause, OutcomeUnknown)
	}
	incomplete.Outcome = result.RemoteOutcome
	return result, incomplete
}

func paginationCancellation(ctx context.Context, err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	return ctx.Err()
}

func cancellationReason(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline"
	}
	return "cancelled"
}

func prepareFirstPaginatedRequest(base Request, spec *contract.PaginationSpec) (Request, int, error) {
	if spec == nil {
		return Request{}, 0, errors.New("pagination metadata is unavailable")
	}
	request := cloneRequest(base)
	switch spec.Mode {
	case contract.PaginationPageNumber:
		if hasPaginationStart(request, spec.PageNumberParam) {
			return Request{}, 0, fmt.Errorf("--page-all cannot be used with %s", spec.PageNumberParam)
		}
		if spec.CursorParam != "" {
			if hasPaginationStart(request, spec.CursorParam) {
				return Request{}, 0, fmt.Errorf("--page-all cannot be used with %s", spec.CursorParam)
			}
		}
		pageSize := positiveInt(request.Query[spec.PageSizeParam])
		if pageSize <= 0 {
			pageSize = spec.DefaultPageSize
		}
		if pageSize <= 0 {
			pageSize = fallbackPageSize
		}
		request.Query[spec.PageSizeParam] = strconv.Itoa(pageSize)
		return request, pageSize, nil
	case contract.PaginationCursor:
		if hasPaginationStart(request, spec.CursorParam) {
			return Request{}, 0, fmt.Errorf("--page-all cannot be used with %s", spec.CursorParam)
		}
		if spec.PageNumberParam != "" {
			if hasPaginationStart(request, spec.PageNumberParam) {
				return Request{}, 0, fmt.Errorf("--page-all cannot be used with %s", spec.PageNumberParam)
			}
		}
		pageSize := 0
		if spec.PageSizeParam != "" {
			pageSize = positiveInt(request.Query[spec.PageSizeParam])
			if pageSize <= 0 && spec.DefaultPageSize > 0 {
				pageSize = spec.DefaultPageSize
				request.Query[spec.PageSizeParam] = strconv.Itoa(pageSize)
			}
		}
		return request, pageSize, nil
	default:
		return Request{}, 0, fmt.Errorf("unsupported pagination mode: %s", spec.Mode)
	}
}

func hasPaginationStart(request Request, name string) bool {
	if strings.TrimSpace(request.Query[name]) != "" {
		return true
	}
	for _, value := range request.QueryMulti[name] {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func pageSizeFromRequest(query map[string]string, spec *contract.PaginationSpec) int {
	if spec == nil || spec.PageSizeParam == "" {
		return 0
	}
	return positiveInt(query[spec.PageSizeParam])
}

func positiveInt(raw string) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return 0
	}
	return value
}

func anySlice(value any) ([]any, bool) {
	switch typed := value.(type) {
	case []any:
		return typed, true
	case []map[string]any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, item)
		}
		return out, true
	default:
		return nil, false
	}
}

// totalAtMost compares a nonnegative integer total with the collected count.
// It handles arbitrarily large decimal/exponent forms without converting them
// to int or allocating a number proportional to the exponent.
func totalAtMost(value any, count int) (bool, bool) {
	return totalAtMostDecimal(value, strconv.Itoa(count))
}

func totalAtMostDecimal(value any, want string) (bool, bool) {
	var raw string
	switch typed := value.(type) {
	case int:
		raw = strconv.Itoa(typed)
	case int8:
		raw = strconv.FormatInt(int64(typed), 10)
	case int16:
		raw = strconv.FormatInt(int64(typed), 10)
	case int32:
		raw = strconv.FormatInt(int64(typed), 10)
	case int64:
		raw = strconv.FormatInt(typed, 10)
	case uint:
		raw = strconv.FormatUint(uint64(typed), 10)
	case uint8:
		raw = strconv.FormatUint(uint64(typed), 10)
	case uint16:
		raw = strconv.FormatUint(uint64(typed), 10)
	case uint32:
		raw = strconv.FormatUint(uint64(typed), 10)
	case uint64:
		raw = strconv.FormatUint(typed, 10)
	case float32:
		raw = strconv.FormatFloat(float64(typed), 'g', -1, 32)
	case float64:
		raw = strconv.FormatFloat(typed, 'g', -1, 64)
	case json.Number:
		raw = string(typed)
	default:
		return false, false
	}
	if raw == "" || raw != strings.TrimSpace(raw) || raw[0] < '0' || raw[0] > '9' || !json.Valid([]byte(raw)) {
		return false, false
	}
	coefficient := raw
	exponent := 0
	if index := strings.IndexAny(raw, "eE"); index >= 0 {
		coefficient = raw[:index]
		limit := len(raw) + 32
		parsed, err := strconv.ParseInt(raw[index+1:], 10, 64)
		switch {
		case err != nil && strings.HasPrefix(raw[index+1:], "-"):
			exponent = -limit
		case err != nil:
			exponent = limit
		case parsed > int64(limit):
			exponent = limit
		case parsed < -int64(limit):
			exponent = -limit
		default:
			exponent = int(parsed)
		}
	}
	fractionDigits := 0
	if point := strings.IndexByte(coefficient, '.'); point >= 0 {
		fractionDigits = len(coefficient) - point - 1
		coefficient = coefficient[:point] + coefficient[point+1:]
	}
	digits := strings.TrimLeft(coefficient, "0")
	if digits == "" {
		return true, true
	}
	scale := exponent - fractionDigits
	if scale < 0 {
		remove := -scale
		if remove > len(digits) || strings.Trim(digits[len(digits)-remove:], "0") != "" {
			return false, false
		}
		digits = digits[:len(digits)-remove]
		scale = 0
	}
	if len(digits)+scale != len(want) {
		return len(digits)+scale < len(want), true
	}
	for i := range want {
		digit := byte('0')
		if i < len(digits) {
			digit = digits[i]
		}
		if digit != want[i] {
			return digit < want[i], true
		}
	}
	return true, true
}
