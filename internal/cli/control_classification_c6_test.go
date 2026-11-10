package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/volcengine-tls/ve-tls-cli/internal/execution"
)

func TestC6InteractionDoesNotHideFailedAuthRollback(t *testing.T) {
	interaction := &NonInteractiveInteractionError{Hint: accountInteractionHint}
	for _, tc := range []struct {
		err  error
		kind string
		exit int
	}{
		{interaction, "interaction_required", 1},
		{errors.Join(ErrSSORollbackFailure, interaction), "auth", 2},
		{errors.Join(ErrSSOLogoutPartialFailure, interaction), "partial_failure", 2},
		{errors.Join(ErrLogoutPartialFailure, interaction), "partial_failure", 2},
	} {
		payload, code := classifyError(tc.err, "", 0, "configure")
		if payload.Kind != tc.kind || code != tc.exit {
			t.Fatalf("classification=%#v exit=%d, want %s/%d", payload, code, tc.kind, tc.exit)
		}
	}
}

func TestC6CancellationBeforeTransportDoesNotNeedAnErrorKind(t *testing.T) {
	for _, tc := range []struct {
		cause error
		kind  string
	}{{context.Canceled, "cancelled"}, {context.DeadlineExceeded, "deadline"}} {
		err := &execution.Failure{Stage: "sign", Outcome: execution.OutcomeNotSent, Cause: tc.cause}
		payload, code := classifyError(err, "", 0, "tool")
		if code != 2 || payload.Kind != tc.kind {
			t.Fatalf("classification=%#v exit=%d", payload, code)
		}
	}
}
