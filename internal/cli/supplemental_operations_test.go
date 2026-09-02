package cli

import (
	"strings"
	"testing"
)

func TestSupplementalTLSOperationsAreDiscoverableAndDescribable(t *testing.T) {
	wantByGroup := map[string][]string{
		"account": {
			"account.active",
		},
		"collector": {
			"collector.extract",
			"collector.generate-begin-regex",
			"collector.generate-log-regex",
			"collector.parse-path",
			"collector.parse-time",
			"collector.split",
		},
		"log": {
			"log.describe-latest-log",
			"log.preview",
		},
		"processor": {
			"processor.exec-processor",
		},
	}
	for group, ids := range wantByGroup {
		listed, err := runTool(nil, []string{"list", group})
		if err != nil {
			t.Fatalf("tool list %s: %v", group, err)
		}
		listText := asOutputString(t, listed)
		for _, id := range ids {
			if !strings.Contains(listText, id) {
				t.Errorf("tool list %s missing %s: %q", group, id, listText)
			}
			described, err := runTool(nil, []string{"describe", id})
			if err != nil {
				t.Fatalf("tool describe %s: %v", id, err)
			}
			payload, ok := described.(map[string]any)
			if !ok {
				t.Fatalf("tool describe %s returned %T", id, described)
			}
			identity, ok := payload["identity"].(map[string]any)
			if !ok || asStringOrEmpty(identity["id"]) != id {
				t.Fatalf("tool describe %s identity=%#v", id, payload["identity"])
			}
		}
	}
}
