package execution

import (
	"math"
	"math/big"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
)

// A successful single-page request is not an attempt to collect every page.
// Preserve its response and success outcome, while distinguishing a complete
// collection from a page slice and providing the next ordinary invocation.
func annotateSinglePage(invocation Invocation, request Request, result Result) (Result, error) {
	spec := invocation.Operation.Pagination
	pageSize := pageSizeFromRequest(request.Query, spec)
	pagination := &PaginationResult{
		Completeness: Completeness{StopReason: "single_page", PageCount: 1},
		Mode:         "single_page",
		PageSize:     pageSize,
	}
	result.Pagination = pagination
	invalid := func(reason, message string) (Result, error) {
		pagination.StopReason = reason
		result.RemoteOutcome = OutcomeUnknown
		return result, &IncompleteError{
			Data: result.Data, State: &pagination.Completeness,
			Cause: invalidPaginationResponse(message), Outcome: OutcomeUnknown,
			Kind: "protocol", Code: "UnexpectedListResponse",
		}
	}
	response, ok := result.Data.(map[string]any)
	if !ok {
		return invalid("invalid_response", "unexpected list response")
	}
	rows, ok := anySlice(response[spec.ItemsField])
	if !ok {
		return invalid("invalid_response", "unexpected list field: "+spec.ItemsField)
	}
	pagination.ItemCount = len(rows)
	collector := paginationCollector{invocation: invocation, spec: spec, pageSize: pageSize}

	page, fromStart, knownOffset := 1, true, true
	if spec.Mode == contract.PaginationPageNumber {
		if spec.CursorParam != "" && hasPaginationStart(request, spec.CursorParam) {
			// This operation accepts an opaque cursor but does not declare how
			// to obtain its successor. A page number cannot safely replace it.
			pagination.StopReason = "cursor_unavailable"
			return result, nil
		}
		if raw := strings.TrimSpace(request.Query[spec.PageNumberParam]); raw != "" {
			page = positiveInt(raw)
			if page == 0 {
				pagination.StopReason = "unknown_page"
				return result, nil
			}
		}
		fromStart = page == 1
		knownOffset = fromStart || pageSize > 0
	} else {
		fromStart = !hasPaginationStart(request, spec.CursorParam)
		knownOffset = fromStart
	}

	// DefaultPageSize is the page-all request size, not necessarily the
	// service's default. Do not inject it into a single request or its next.
	endCount := big.NewInt(int64(len(rows)))
	if !knownOffset {
		endCount.SetInt64(0)
	} else if spec.Mode == contract.PaginationPageNumber && !fromStart {
		offset := new(big.Int).Mul(big.NewInt(int64(page-1)), big.NewInt(int64(pageSize)))
		endCount.Add(endCount, offset)
	}
	total, hasTotal := response[spec.TotalField]
	totalReached := false
	if spec.TotalField != "" && hasTotal {
		var valid bool
		totalReached, valid = totalAtMostDecimal(total, endCount.String())
		if !valid {
			return invalid("invalid_total", "invalid list total: "+spec.TotalField)
		}
	}

	terminal := false
	switch spec.Mode {
	case contract.PaginationPageNumber:
		switch {
		case hasTotal && totalReached:
			terminal, pagination.StopReason = true, "total_reached"
		case len(rows) == 0:
			if hasTotal && knownOffset {
				return invalid("inconsistent_total", "empty page before the reported total")
			}
			terminal, pagination.StopReason = true, "empty_page"
		case !hasTotal && pageSize > 0 && len(rows) < pageSize:
			terminal, pagination.StopReason = true, "short_page"
		case page == math.MaxInt:
			pagination.StopReason = "page_limit"
		default:
			pagination.Next = collector.continuation(page+1, "")
		}
	case contract.PaginationCursor:
		next, ok := response[spec.NextCursorField].(string)
		if !ok {
			return invalid("missing_cursor", "missing list cursor: "+spec.NextCursorField)
		}
		if next == "" {
			if hasTotal && knownOffset && !totalReached {
				return invalid("inconsistent_total", "cursor ended before the reported total")
			}
			terminal, pagination.StopReason = true, "cursor_end"
		} else if next == request.Query[spec.CursorParam] {
			return invalid("cursor_stalled", "list cursor did not advance")
		} else {
			pagination.Next = collector.continuation(0, next)
		}
	default:
		panic("unsupported pagination mode: " + string(spec.Mode))
	}
	pagination.Complete = terminal && fromStart
	if terminal && !fromStart {
		pagination.StopReason = "page_slice"
	}
	pagination.Resumable = pagination.Next != nil
	return result, nil
}
