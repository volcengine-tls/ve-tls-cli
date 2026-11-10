//go:build !human

package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

type defaultEditionCLIError struct {
	ErrorCode    string `json:"code"`
	ErrorMessage string `json:"message"`
	Kind         string `json:"kind"`
	Hint         string `json:"hint"`
}

func TestDefaultEditionRejectsShortcutGroups(t *testing.T) {
	for _, group := range []string{"project", "topic", "metric-topic", "index", "log", "host-group", "collector", "assistant"} {
		t.Run(group, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run([]string{group, "-h"}, &stdout, &stderr)
			if code != 1 {
				t.Fatalf("expected exit=1 for %s, got=%d stdout=%q stderr=%q", group, code, stdout.String(), stderr.String())
			}
			payload := decodeDefaultEditionError(t, stdout.Bytes(), stderr.Bytes())
			if payload.ErrorCode != "CLIError" || payload.Kind != "usage" {
				t.Fatalf("unexpected cli error payload for %s: %#v", group, payload)
			}
			if !strings.Contains(payload.ErrorMessage, group) || !strings.Contains(payload.ErrorMessage, "group not available") {
				t.Fatalf("unexpected error message for %s: %#v", group, payload)
			}
			if !strings.Contains(payload.Hint, "tool list") || !strings.Contains(payload.Hint, "workflow list") {
				t.Fatalf("unexpected hint for %s: %#v", group, payload)
			}
		})
	}
}

func TestDefaultEditionStillRoutesRemovedLegacyCommandsToMigrationHint(t *testing.T) {
	for _, group := range []string{"api", "capabilities"} {
		t.Run(group, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run([]string{group}, &stdout, &stderr)
			if code != 1 {
				t.Fatalf("expected exit=1 for %s, got=%d stdout=%q stderr=%q", group, code, stdout.String(), stderr.String())
			}
			payload := decodeDefaultEditionError(t, stdout.Bytes(), stderr.Bytes())
			if !strings.Contains(payload.Hint, "tool list") {
				t.Fatalf("expected legacy migration hint for %s: %#v", group, payload)
			}
		})
	}
}

func decodeDefaultEditionError(t *testing.T, stdout, stderr []byte) defaultEditionCLIError {
	t.Helper()
	var env struct {
		Status  string                 `json:"status"`
		Error   defaultEditionCLIError `json:"error"`
		Summary struct {
			RemoteOutcome string `json:"remoteOutcome"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(stdout, &env); err != nil || len(stderr) != 0 || env.Status != "failed" || env.Summary.RemoteOutcome != "not_sent" {
		t.Fatalf("expected early failure envelope: error=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	return env.Error
}

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}
