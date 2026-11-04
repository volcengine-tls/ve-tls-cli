package cli

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/volcengine-tls/ve-tls-cli/internal/execution"
)

// classifyTypedOperationError prioritizes the outermost nonempty kind while
// keeping the matching code and delivery exit on that same error layer.
func classifyTypedOperationError(err error, requestID string, statusCode int, group string) (errPayload, int, bool) {
	var interaction *NonInteractiveInteractionError
	if !errors.Is(err, ErrSSORollbackFailure) && !errors.Is(err, ErrSSOLogoutPartialFailure) && !errors.Is(err, ErrLogoutPartialFailure) && errors.As(err, &interaction) {
		return errPayload{Kind: "interaction_required", ErrorCode: "InteractionRequired", Hint: interaction.Hint, RequestID: requestID, StatusCode: statusCode}, 1, true
	}
	if frozenAuthClassification(err, group) {
		return errPayload{}, 0, false
	}
	if execution.ErrorOutcome(err, "") == execution.OutcomeNotSent {
		if errors.Is(err, context.Canceled) {
			return errPayload{RequestID: requestID, StatusCode: statusCode, Kind: "cancelled", ErrorCode: "RequestCancelled", Hint: "the request was cancelled before sending"}, 2, true
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return errPayload{RequestID: requestID, StatusCode: statusCode, Kind: "deadline", ErrorCode: "RequestDeadlineExceeded", Hint: "the deadline expired before sending"}, 2, true
		}
	}
	typed, kind := firstTypedOperationError(err)
	if typed == nil {
		return errPayload{}, 0, false
	}
	payload := errPayload{RequestID: requestID, StatusCode: statusCode, Kind: kind}
	if coded, ok := typed.(interface{ ErrorCode() string }); ok {
		payload.ErrorCode = strings.TrimSpace(coded.ErrorCode())
	}
	code := 2
	switch kind {
	case "permission":
		code = 1
		payload.Hint = "inspect with --dry-run; explicitly permit writes using --allow-write or context.execution.allow_write=true"
	case "validation":
		code = 1
		payload.Hint = "check --input and --context fields against the operation contract"
	case "config":
		payload.Hint = "check the profile, region, and endpoint configuration"
	case "decode":
		code = 3
		if payload.ErrorCode == "InvalidProjection" {
			payload.Hint = "check context.execution.projection"
		} else {
			payload.Hint = "check --jmes-filter"
		}
	case "protocol":
		code = 3
		payload.Hint = "inspect the response contract and verify the remote result before replaying a write"
	case "result_unknown":
		payload.Hint = "verify the remote result first; do not automatically replay the write"
	case "partial_failure":
		payload.Hint = "inspect confirmed progress and verify the current batch before taking further action"
	case "incomplete_result":
		payload.Hint = "inspect summary.completeness; when resumable, reuse the runtime selectors and call next.operationId with next.input and page-all disabled"
	case "output_failed":
		code = 3
		if delivery, ok := typed.(interface{ DeliveryExitCode() int }); ok {
			if exit := delivery.DeliveryExitCode(); exit == 2 || exit == 3 {
				code = exit
			}
		}
		payload.Hint = "fix result delivery; do not rerun the original write"
		if staged, ok := typed.(interface{ FailureStage() string }); ok {
			switch staged.FailureStage() {
			case "artifact":
				payload.Hint = "check --output-dir and filesystem permissions; do not rerun the original write"
			case "projection":
				payload.Hint = "check --jmes-filter or context.execution.projection; do not rerun the original write"
			case "stdout":
				payload.Hint = "check the stdout stream and pipe; do not rerun the original write"
			}
		}
	}
	return payload, code, true
}

func firstTypedOperationError(err error) (error, string) {
	if err == nil {
		return nil, ""
	}
	if typed, ok := err.(interface{ ErrorKind() string }); ok {
		if kind := strings.TrimSpace(typed.ErrorKind()); kind != "" {
			return err, kind
		}
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, child := range joined.Unwrap() {
			if typed, kind := firstTypedOperationError(child); typed != nil {
				return typed, kind
			}
		}
		return nil, ""
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return firstTypedOperationError(wrapped.Unwrap())
	}
	return nil, ""
}

// Authentication commands and transaction sentinels have established error
// contracts that must remain authoritative even when a cause is wrapped.
func frozenAuthClassification(err error, group string) bool {
	switch strings.TrimSpace(group) {
	case "login", "logout", "sso", "configure":
		return true
	}
	return errors.Is(err, ErrSSOLogoutPartialFailure) || errors.Is(err, ErrSSORollbackFailure) || errors.Is(err, ErrLogoutPartialFailure)
}

func classifyOperationHTTPError(he *httpError, frozenAuth bool) (string, string) {
	kind, hint := "server", ""
	switch he.statusCode {
	case http.StatusBadRequest:
		kind, hint = "usage", "check request parameters"
	case http.StatusUnauthorized, http.StatusForbidden:
		kind, hint = "auth", "check credentials/region/endpoint"
	case http.StatusNotFound:
		if !frozenAuth {
			kind, hint = "not_found", "check the requested resource and identifiers"
		}
	case http.StatusConflict:
		if !frozenAuth {
			kind, hint = "conflict", "inspect the current resource state before submitting another request"
		}
	case http.StatusTooManyRequests:
		if !frozenAuth {
			kind, hint = "throttled", "reduce request rate and verify write state before retrying"
		}
	}
	upstreamCode := httpErrorCode(he.body)
	if code, _, _ := parseHTTPErrorPayload(he); code != "" {
		upstreamCode = code
	}
	switch strings.ToLower(upstreamCode) {
	case "throttling", "throttlingexception", "limitexceeded", "requestlimitexceeded", "toomanyrequests":
		if !frozenAuth {
			kind, hint = "throttled", "reduce request rate and verify write state before retrying"
		}
	case "indexnotexists":
		hint = "index does not exist; inspect current config with 'volclog index get --topic-id <TopicId>' or create one with 'volclog index create --describe' and 'volclog index create --print-request-template=full'"
	}
	return kind, hint
}
