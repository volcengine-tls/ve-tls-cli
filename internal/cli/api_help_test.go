package cli

import (
	"strings"
	"testing"
)

func TestUsageAPIGeneratedIsConciseAndGuided(t *testing.T) {
	ops := []apiActionOp{
		{
			Cmd: apiCapabilityCommand{
				Group:   "log",
				Action:  "search",
				Summary: "SearchLogs",
				Method:  "POST",
				Path:    "/SearchLogs",
			},
			ParamFlags: map[string]apiCapParam{
				"--topic-id": {Name: "TopicId", In: "query", Required: true},
			},
		},
	}
	s := usageAPIGenerated("log", "search", ops)
	for _, want := range []string{
		"--describe",
		"--print-request-template[=required|full]",
		"Agent Guidance:",
		"Use --dry-run before execution",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in usage: %s", want, s)
		}
	}
}
