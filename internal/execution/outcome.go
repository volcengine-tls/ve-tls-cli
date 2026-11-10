package execution

import "errors"

const (
	OutcomeNotSent  = "not_sent"
	OutcomeUnknown  = "unknown"
	OutcomeSuccess  = "success"
	OutcomeFailed   = "failed"
	OutcomePartial  = "partial"
	OutcomeAccepted = "accepted"
)

// Failure describes execution evidence independently of result delivery.
// It preserves the cause so authentication and upstream diagnostics survive.
type Failure struct {
	Kind    string
	Code    string
	Stage   string
	Outcome string
	Message string
	Cause   error
}

func (e *Failure) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return e.Code
}

func (e *Failure) Unwrap() error         { return e.Cause }
func (e *Failure) RemoteOutcome() string { return e.Outcome }
func (e *Failure) FailureStage() string  { return e.Stage }
func (e *Failure) ErrorCode() string     { return e.Code }
func (e *Failure) ErrorKind() string     { return e.Kind }

// ErrorOutcome reads stage evidence without depending on a transport package.
func ErrorOutcome(err error, fallback string) string {
	if evidence, ok := err.(interface{ RemoteOutcome() string }); ok && evidence.RemoteOutcome() != "" {
		return evidence.RemoteOutcome()
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, child := range joined.Unwrap() {
			if outcome := ErrorOutcome(child, ""); outcome != "" {
				return outcome
			}
		}
	} else if child := errors.Unwrap(err); child != nil {
		return ErrorOutcome(child, fallback)
	}
	return fallback
}
