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
	switch spec.Mode {
	case contract.PaginationPageNumber:
		return e.executeAllPageNumber(ctx, invocation.Operation, base, codec, spec)
	case contract.PaginationCursor:
		return e.executeAllCursor(ctx, invocation.Operation, base, codec, spec)
	default:
		return Result{OperationID: invocation.Operation.ID}, fmt.Errorf("unsupported pagination mode: %s", spec.Mode)
	}
}

func (e *Executor) executeAllPageNumber(
	ctx context.Context,
	operation contract.Operation,
	base Request,
	codec Codec,
	spec *contract.PaginationSpec,
) (Result, error) {
	request, pageSize, err := prepareFirstPaginatedRequest(base, spec)
	if err != nil {
		return Result{OperationID: operation.ID}, err
	}
	all := make([]any, 0)
	var last map[string]any
	result := Result{OperationID: operation.ID}
	pageCount := 0
	for page := 1; page <= spec.MaxPages; page++ {
		current := cloneRequest(request)
		current.Query[spec.PageNumberParam] = strconv.Itoa(page)
		pageResult, err := e.executeOne(ctx, operation, current, codec)
		if err != nil {
			pageResult.Pagination = paginationProgress(pageCount, pageSize, false)
			return pageResult, err
		}
		response, ok := pageResult.Data.(map[string]any)
		if !ok {
			pageResult.Pagination = paginationProgress(pageCount, pageSize, false)
			return pageResult, errors.New("unexpected list response")
		}
		pageCount++
		rows, ok := anySlice(response[spec.ItemsField])
		if !ok {
			pageResult.Pagination = paginationProgress(pageCount, pageSize, false)
			return pageResult, errors.New("unexpected list field: " + spec.ItemsField)
		}
		last = response
		result = pageResult
		all = append(all, rows...)
		total, hasTotal := anyInt(response[spec.TotalField])
		if len(rows) == 0 || len(rows) < pageSize || (hasTotal && len(all) >= total) {
			break
		}
	}
	if last == nil {
		last = map[string]any{}
	}
	last[spec.ItemsField] = all
	if _, hasTotal := last[spec.TotalField]; hasTotal {
		last[spec.TotalField] = len(all)
	}
	result.Data = last
	result.Pagination = paginationProgress(pageCount, pageSize, true)
	return result, nil
}

func (e *Executor) executeAllCursor(
	ctx context.Context,
	operation contract.Operation,
	base Request,
	codec Codec,
	spec *contract.PaginationSpec,
) (Result, error) {
	request, pageSize, err := prepareFirstPaginatedRequest(base, spec)
	if err != nil {
		return Result{OperationID: operation.ID}, err
	}
	all := make([]any, 0)
	var last map[string]any
	result := Result{OperationID: operation.ID}
	pageCount := 0
	for page := 0; page < spec.MaxPages; page++ {
		pageResult, err := e.executeOne(ctx, operation, cloneRequest(request), codec)
		if err != nil {
			pageResult.Pagination = paginationProgress(pageCount, pageSize, false)
			return pageResult, err
		}
		response, ok := pageResult.Data.(map[string]any)
		if !ok {
			pageResult.Pagination = paginationProgress(pageCount, pageSize, false)
			return pageResult, errors.New("unexpected list response")
		}
		pageCount++
		rows, ok := anySlice(response[spec.ItemsField])
		if !ok {
			pageResult.Pagination = paginationProgress(pageCount, pageSize, false)
			return pageResult, errors.New("unexpected list field: " + spec.ItemsField)
		}
		last = response
		result = pageResult
		all = append(all, rows...)
		next, _ := response[spec.NextCursorField].(string)
		if len(rows) == 0 || strings.TrimSpace(next) == "" {
			break
		}
		request.Query[spec.CursorParam] = next
	}
	if last == nil {
		last = map[string]any{}
	}
	last[spec.ItemsField] = all
	if _, hasTotal := last[spec.TotalField]; hasTotal {
		last[spec.TotalField] = len(all)
	}
	result.Data = last
	result.Pagination = paginationProgress(pageCount, pageSize, true)
	return result, nil
}

func prepareFirstPaginatedRequest(base Request, spec *contract.PaginationSpec) (Request, int, error) {
	if spec == nil {
		return Request{}, 0, errors.New("pagination metadata is unavailable")
	}
	request := cloneRequest(base)
	switch spec.Mode {
	case contract.PaginationPageNumber:
		if value := strings.TrimSpace(request.Query[spec.PageNumberParam]); value != "" {
			return Request{}, 0, fmt.Errorf("--page-all cannot be used with %s", spec.PageNumberParam)
		}
		if spec.CursorParam != "" {
			if value := strings.TrimSpace(request.Query[spec.CursorParam]); value != "" {
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
		if value := strings.TrimSpace(request.Query[spec.CursorParam]); value != "" {
			return Request{}, 0, fmt.Errorf("--page-all cannot be used with %s", spec.CursorParam)
		}
		if spec.PageNumberParam != "" {
			if value := strings.TrimSpace(request.Query[spec.PageNumberParam]); value != "" {
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

func pageSizeFromRequest(query map[string]string, spec *contract.PaginationSpec) int {
	if spec == nil || spec.PageSizeParam == "" {
		return 0
	}
	return positiveInt(query[spec.PageSizeParam])
}

func paginationProgress(pageCount, pageSize int, merged bool) *PaginationResult {
	return &PaginationResult{
		Mode:      "page_all",
		PageCount: pageCount,
		PageSize:  pageSize,
		Merged:    merged,
	}
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

func anyInt(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	case json.Number:
		number, err := typed.Int64()
		return int(number), err == nil
	default:
		return 0, false
	}
}
