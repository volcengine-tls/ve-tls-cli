//go:build !human

package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

type defaultEditionCLIError struct {
	ErrorCode    string `json:"errorCode"`
	ErrorMessage string `json:"errorMessage"`
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
			var payload defaultEditionCLIError
			if err := json.Unmarshal(stderr.Bytes(), &payload); err != nil {
				t.Fatalf("stderr should be cli error json for %s: %v stderr=%q", group, err, stderr.String())
			}
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
			var payload defaultEditionCLIError
			if err := json.Unmarshal(stderr.Bytes(), &payload); err != nil {
				t.Fatalf("stderr should be cli error json for %s: %v stderr=%q", group, err, stderr.String())
			}
			if !strings.Contains(payload.Hint, "tool list") {
				t.Fatalf("expected legacy migration hint for %s: %#v", group, payload)
			}
		})
	}
}

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}
