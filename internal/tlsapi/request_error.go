package tlsapi

// RequestError reports what is known about a failed request while preserving
// the underlying cause for diagnostics and errors.Is/errors.As. Its message is
// intentionally fixed because URLs and transport errors can contain secrets.
type RequestError struct {
	stage   string
	outcome string
	cause   error
}

func (e *RequestError) Error() string {
	switch e.stage {
	case "build_request":
		return "request construction failed"
	case "sign":
		return "request signing or credential provider failed"
	case "transport":
		return "request result unknown after transport failure"
	case "read_response":
		return "request result unknown after response read failure"
	default:
		return "request failed"
	}
}

func (e *RequestError) Unwrap() error         { return e.cause }
func (e *RequestError) RemoteOutcome() string { return e.outcome }
func (e *RequestError) FailureStage() string  { return e.stage }

func (e *RequestError) ErrorKind() string {
	switch e.stage {
	case "build_request":
		return "validation"
	case "transport", "read_response":
		return "result_unknown"
	default:
		return ""
	}
}

func (e *RequestError) ErrorCode() string {
	switch e.stage {
	case "build_request":
		return "InvalidRequest"
	case "transport", "read_response":
		return "RequestResultUnknown"
	default:
		return ""
	}
}

func requestFailure(stage, outcome string, cause error) error {
	return &RequestError{stage: stage, outcome: outcome, cause: cause}
}
