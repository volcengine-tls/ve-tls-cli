//go:build human

package cli

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestTopicDataIngestSpecShortcutFlags(t *testing.T) {
	for _, tc := range []struct {
		path, spec string
		args       []string
	}{
		{"/CreateTopic", "D7", []string{"create", "--project-id", "p", "--topic-name", "demo", "--metering-mode", "ChargeByDataIngest", "--data-ingest-spec", "D7"}},
		{"/ModifyTopic", "D15", []string{"modify", "--topic-id", "t", "--request", `{"DataIngestSpec":"D30"}`, "--data-ingest-spec", "D15"}},
	} {
		t.Run(tc.path, func(t *testing.T) {
			_, calls := outcomeCLIServer(t, func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Errorf("decode request: %v", err)
					return
				}
				if r.URL.Path != tc.path || body["DataIngestSpec"] != tc.spec {
					t.Errorf("request to %s: %#v", r.URL.Path, body)
				}
				_, _ = w.Write([]byte(`{"TopicId":"t"}`))
			})
			code, out := outcomeCLIRun(t, append([]string{"--allow-write", "topic"}, tc.args...))
			if code != 0 || calls.Load() != 1 {
				t.Fatalf("exit=%d calls=%d result=%#v", code, calls.Load(), out)
			}
		})
	}
}
