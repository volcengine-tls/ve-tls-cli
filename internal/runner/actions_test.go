package runner

import "testing"

func TestBuildCommand_LogSearch(t *testing.T) {
	cmd, err := BuildCommand("log.search", "p", "json", map[string]any{
		"topic_id": "tid",
		"query":    "error",
		"from_ms":  "1710374400000",
		"to_ms":    "1710378000000",
	})
	if err != nil {
		t.Fatal(err)
	}
	wantPrefix := []string{"tlsctl", "--profile", "p", "--output", "json", "log", "search"}
	for i := range wantPrefix {
		if cmd[i] != wantPrefix[i] {
			t.Fatalf("cmd[%d]=%q want %q", i, cmd[i], wantPrefix[i])
		}
	}
}

func TestBuildCommand_MetricPromQueryRange(t *testing.T) {
	cmd, err := BuildCommand("metric_topic.prom.query_range", "p", "json", map[string]any{
		"topic_id": "mtid",
		"query":    "up",
		"start_ms": "1710374400000",
		"end_ms":   "1710378000000",
		"step":     15,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cmd[5] != "metric-topic" || cmd[6] != "prom" || cmd[7] != "query-range" {
		t.Fatalf("unexpected subcmd: %v", cmd[:10])
	}
}

func TestBuildCommand_UnsupportedAction(t *testing.T) {
	_, err := BuildCommand("unknown.xxx", "p", "json", map[string]any{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBuildCommand_MissingRequiredArg(t *testing.T) {
	_, err := BuildCommand("project.get", "p", "json", map[string]any{})
	if err == nil {
		t.Fatal("expected error")
	}
}
