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
