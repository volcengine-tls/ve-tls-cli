package cli

import (
	"context"
	"io"
	"os"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth/sso"
)

// NonInteractiveInteractionError identifies an action that needs user input.
// Hint contains a fixed instruction; it never includes user or token values.
type NonInteractiveInteractionError struct {
	Hint string
}

func (e *NonInteractiveInteractionError) Error() string {
	return "interaction required: " + e.Hint
}

func (e *NonInteractiveInteractionError) ErrorKind() string { return "interaction_required" }
func (e *NonInteractiveInteractionError) ErrorCode() string { return "InteractionRequired" }

func stdinIsTerminal() bool { return inputIsTerminal(os.Stdin) }

const (
	confirmInteractionHint = "use another --profile, or run login in an interactive terminal to confirm session replacement"
	accountInteractionHint = "specify --account-id to select an account"
	roleInteractionHint    = "specify --role-name to select a role"
)

// invocationPromptReader protects terminal reads without creating a reader
// goroutine. Other readers retain their existing behavior with context checks.
func invocationPromptReader(ctx context.Context, stdin io.Reader, hint string) (io.Reader, error) {
	if f, ok := stdin.(*os.File); ok && inputIsTerminal(f) {
		return newContextFilePromptReader(ctx, f, hint)
	}
	return &contextPromptReader{ctx: ctx, reader: stdin}, nil
}

type contextPromptReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextPromptReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(p)
	if ctxErr := r.ctx.Err(); ctxErr != nil {
		return 0, ctxErr
	}
	return n, err
}

func newInvocationConfirmPrompt(ctx context.Context, nonInteractive bool, stdin io.Reader, stderr io.Writer) func(string, string, string) (bool, error) {
	return func(profile, current, next string) (bool, error) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if nonInteractive {
			return false, &NonInteractiveInteractionError{Hint: confirmInteractionHint}
		}
		reader, err := invocationPromptReader(ctx, stdin, confirmInteractionHint)
		if err != nil {
			return false, err
		}
		ok, err := newConfirmPrompt(reader, stderr)(profile, current, next)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return false, ctxErr
		}
		return ok, err
	}
}

func newInvocationAccountSelector(ctx context.Context, nonInteractive bool, stdin io.Reader, stderr io.Writer) sso.AccountSelector {
	return func(accounts []sso.AccountInfo) (sso.AccountInfo, error) {
		if err := ctx.Err(); err != nil {
			return sso.AccountInfo{}, err
		}
		if nonInteractive && len(accounts) > 1 {
			return sso.AccountInfo{}, &NonInteractiveInteractionError{Hint: accountInteractionHint}
		}
		reader := stdin
		if len(accounts) > 1 {
			var err error
			reader, err = invocationPromptReader(ctx, stdin, accountInteractionHint)
			if err != nil {
				return sso.AccountInfo{}, err
			}
		}
		selected, err := newAccountSelector(reader, stderr)(accounts)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return sso.AccountInfo{}, ctxErr
		}
		return selected, err
	}
}

func newInvocationRoleSelector(ctx context.Context, nonInteractive bool, stdin io.Reader, stderr io.Writer) sso.RoleSelector {
	return func(roles []sso.RoleInfo) (sso.RoleInfo, error) {
		if err := ctx.Err(); err != nil {
			return sso.RoleInfo{}, err
		}
		if nonInteractive && len(roles) > 1 {
			return sso.RoleInfo{}, &NonInteractiveInteractionError{Hint: roleInteractionHint}
		}
		reader := stdin
		if len(roles) > 1 {
			var err error
			reader, err = invocationPromptReader(ctx, stdin, roleInteractionHint)
			if err != nil {
				return sso.RoleInfo{}, err
			}
		}
		selected, err := newRoleSelector(reader, stderr)(roles)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return sso.RoleInfo{}, ctxErr
		}
		return selected, err
	}
}
