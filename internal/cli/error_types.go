package cli

import (
	"errors"
	"strings"
)

type errPayload struct {
	ErrorCode    string `json:"errorCode"`
	ErrorMessage string `json:"errorMessage"`
	RequestID    string `json:"requestId,omitempty"`
	StatusCode   int    `json:"statusCode,omitempty"`
	Kind         string `json:"kind,omitempty"`
	Hint         string `json:"hint,omitempty"`
}

type removedCommandError struct {
	Command string
	Hint    string
}

type unsupportedFeatureError struct {
	message string
	hint    string
}

func (e *unsupportedFeatureError) Error() string { return strings.TrimSpace(e.message) }

func newUnsupportedFeatureError(message, hint string) error {
	return &unsupportedFeatureError{
		message: strings.TrimSpace(message),
		hint:    strings.TrimSpace(hint),
	}
}

func (e *removedCommandError) Error() string {
	return "legacy command removed: " + strings.TrimSpace(e.Command)
}

func removedLegacyCommandError(command string, hint string) error {
	return &removedCommandError{
		Command: strings.TrimSpace(command),
		Hint:    strings.TrimSpace(hint),
	}
}

type usageError struct {
	Text     string
	ExitCode int
}

func (e *usageError) Error() string { return "usage" }

func asUsageError(err error) (*usageError, bool) {
	var ue *usageError
	if errors.As(err, &ue) {
		return ue, true
	}
	return nil, false
}

type scopedHelpError struct {
	cause error
	hint  string
}

func (e *scopedHelpError) Error() string { return e.cause.Error() }

func (e *scopedHelpError) Unwrap() error { return e.cause }

func withScopedHelpHint(err error, command string) error {
	if err == nil {
		return nil
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return err
	}
	return &scopedHelpError{
		cause: err,
		hint:  "run '" + command + " --help' to inspect accepted flags and required fields",
	}
}

func scopedHelpHint(err error) string {
	var scoped *scopedHelpError
	if errors.As(err, &scoped) {
		return strings.TrimSpace(scoped.hint)
	}
	return ""
}
