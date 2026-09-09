package cli

import (
	"strings"
	"testing"
)

func TestResolveToolByIdentityRejectsLegacyDownloadTaskIDs(t *testing.T) {
	tests := []struct {
		legacy string
		want   string
	}{
		{legacy: "create", want: "log.create-download-task"},
		{legacy: "cancel", want: "log.cancel-download-task"},
	}
	for _, tc := range tests {
		t.Run(tc.legacy, func(t *testing.T) {
			_, err := resolveToolByIdentity("log", tc.legacy)
			if err == nil {
				t.Fatalf("legacy identity log.%s unexpectedly resolved", tc.legacy)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%q, want canonical identity %q", err, tc.want)
			}
		})
	}
}

func TestToolEntryPointsRejectLegacyDownloadTaskIDs(t *testing.T) {
	for _, tc := range []struct {
		oldID string
		newID string
	}{
		{oldID: "log.create", newID: "log.create-download-task"},
		{oldID: "log.cancel", newID: "log.cancel-download-task"},
	} {
		for _, args := range [][]string{
			{tc.oldID},
			{"describe", tc.oldID},
			{"exec", tc.oldID, "--input", "{}"},
		} {
			t.Run(strings.Join(args, " "), func(t *testing.T) {
				// Rejection happens during identity resolution, before any input
				// handling or client creation; no credentials or transport are used.
				_, err := runTool(&Context{}, args)
				if err == nil || !strings.Contains(err.Error(), tc.newID) {
					t.Fatalf("runTool(%q) error=%v, want replacement hint %q", args, err, tc.newID)
				}
			})
		}
	}
}

func TestResolveToolByIdentityUsesCanonicalDownloadTaskIDsAndActions(t *testing.T) {
	tests := []struct {
		id     string
		action string
	}{
		{id: "log.create-download-task", action: "CreateDownloadTask"},
		{id: "log.cancel-download-task", action: "CancelDownloadTask"},
	}
	for _, tc := range tests {
		for _, identity := range []string{tc.id, "log." + tc.action} {
			t.Run(identity, func(t *testing.T) {
				operation, err := resolveToolByIdentity("log", strings.TrimPrefix(identity, "log."))
				if err != nil {
					t.Fatalf("resolve %s: %v", identity, err)
				}
				if string(operation.ID) != tc.id {
					t.Fatalf("resolve %s returned ID=%q, want %q", identity, operation.ID, tc.id)
				}
			})
		}
	}
}
