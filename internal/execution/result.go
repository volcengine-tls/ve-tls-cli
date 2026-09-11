package execution

import (
	"fmt"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
)

type Options struct {
	DryRun           bool
	PageAll          bool
	ValidationPolicy ValidationPolicy
}

type ValidationPolicy uint8

const (
	// ValidationRequired keeps executor validation enabled. It is the zero
	// value so existing callers, including tool exec, remain strict.
	ValidationRequired ValidationPolicy = iota
	// ValidationCallerLegacy is reserved for compatibility adapters whose
	// existing command parser has already applied its historical validation.
	ValidationCallerLegacy
)

type PreflightCheck struct {
	Name   string  `json:"name"`
	OK     bool    `json:"ok"`
	Detail *string `json:"detail,omitempty"`
}

type RuntimeView struct {
	Region   string
	Endpoint string
	// Checks carries failed runtime checks (for example profile resolution) or
	// additional application checks. A successful profile check is omitted to
	// preserve the public dry-run plan shape.
	Checks []PreflightCheck
}

type Invocation struct {
	Operation contract.Operation
	Input     Input
	Options   Options
	Runtime   RuntimeView
}

type Result struct {
	OperationID contract.OperationID
	Data        any
	RequestID   string
	StatusCode  int
	Pagination  *PaginationResult
	Plan        *DryRunPlan
}

type PaginationResult struct {
	Mode      string `json:"mode"`
	PageCount int    `json:"pageCount"`
	PageSize  int    `json:"pageSize,omitempty"`
	Merged    bool   `json:"merged"`
}

type DryRunPaginationPlan struct {
	Requested bool   `json:"requested"`
	Mode      string `json:"mode"`
	Note      string `json:"note"`
	PageSize  int    `json:"page_size,omitempty"`
}

type DryRunPlan struct {
	Type            string                `json:"type"`
	Method          string                `json:"method"`
	Path            string                `json:"path"`
	QueryKeys       []string              `json:"query_keys"`
	HeadersRedacted []string              `json:"headers_redacted"`
	BodySHA256      string                `json:"body_sha256"`
	Checks          []PreflightCheck      `json:"checks"`
	Valid           bool                  `json:"valid"`
	RequestPreview  map[string]any        `json:"request_preview"`
	PageAll         *DryRunPaginationPlan `json:"page_all,omitempty"`
}

type HTTPError struct {
	StatusCode int
	Body       []byte
	RequestID  string
}

func (e *HTTPError) Error() string {
	if e == nil || len(e.Body) == 0 {
		return "http error"
	}
	return string(e.Body)
}

func resultForResponse(operationID contract.OperationID, response Response) Result {
	return Result{
		OperationID: operationID,
		RequestID:   response.Header.Get("x-tls-requestid"),
		StatusCode:  response.StatusCode,
	}
}

func unsupportedPaginationError(operationID contract.OperationID) error {
	return fmt.Errorf("execution.page.all is not supported for tool: %s", operationID)
}
