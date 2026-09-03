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
		"shard": {
			"shard.merge",
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

func TestShardMergeDescribeProvidesSafeEndToEndUsage(t *testing.T) {
	described, err := runTool(nil, []string{"describe", "shard.merge", "--view", "full"})
	if err != nil {
		t.Fatalf("tool describe shard.merge: %v", err)
	}
	payload, ok := described.(map[string]any)
	if !ok {
		t.Fatalf("tool describe shard.merge returned %T", described)
	}
	notes, ok := payload["usage_notes"].([]any)
	if !ok {
		t.Fatalf("shard.merge usage_notes=%#v", payload["usage_notes"])
	}
	joined := make([]string, 0, len(notes))
	for _, note := range notes {
		joined = append(joined, asStringOrEmpty(note))
	}
	usage := strings.Join(joined, "\n")
	for _, want := range []string{
		"--profile <profile> --region <region> --endpoint <tls-endpoint>",
		"tool exec shard.describe",
		"--page-all",
		"--dry-run tool exec shard.merge",
		"tool exec shard.merge",
		"data.Shards",
		"required-field presence",
	} {
		if !strings.Contains(usage, want) {
			t.Fatalf("shard.merge usage_notes missing %q: %q", want, usage)
		}
	}

	constraints := asStringOrEmpty(payload["usage_constraints"])
	for _, want := range []string{"Before execution", "service-side validation", "Do not retry automatically"} {
		if !strings.Contains(constraints, want) {
			t.Fatalf("shard.merge usage_constraints missing %q: %q", want, constraints)
		}
	}
}
