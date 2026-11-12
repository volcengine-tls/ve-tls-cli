package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func assertTopicIngestSpecHidden(t *testing.T, output string) {
	t.Helper()
	lower := strings.ToLower(output)
	if strings.Contains(lower, "dataingestspec") || strings.Contains(lower, "data-ingest-spec") {
		t.Fatalf("Topic storage entitlement exposed in %s", output)
	}
}

func TestTopicDataIngestSpecHiddenFromContract(t *testing.T) {
	for _, action := range []string{"topic.create", "topic.modify", "topic.describe-topic", "topic.describe-topics"} {
		for _, view := range []string{"compact", "full", "input"} {
			t.Run(action+"/"+view, func(t *testing.T) {
				contract, err := json.Marshal(c5Describe(t, action, view))
				if err != nil {
					t.Fatal(err)
				}
				assertTopicIngestSpecHidden(t, string(contract))
			})
		}
	}
}

func TestTopicDataIngestSpecWritesRejectedBeforeSend(t *testing.T) {
	_, calls := outcomeCLIServer(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{}`)) })
	requestFile := filepath.Join(t.TempDir(), "modify.json")
	if err := os.WriteFile(requestFile, []byte(`{"TopicId":"t","DataIngestSpec":null}`), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name, kind, errorCode string
		args                  []string
	}{
		{"tool create", "validation", "InvalidToolInput", []string{"tool", "exec", "topic.create", "--input", `{"ProjectId":"p","TopicName":"demo","Ttl":30,"MeteringMode":"ChargeByDataIngest","DataIngestSpec":"D7"}`}},
		{"tool modify", "validation", "InvalidToolInput", []string{"tool", "exec", "topic.modify", "--input", `{"body":{"TopicId":"t","DataIngestSpec":"D15"}}`}},
		{"raw create", "unsupported_feature", "", []string{"raw", "--method", "POST", "--path", "/CreateTopic?x=1", "--body", `{"TopicName":"demo","dATaINGESTSpec":""}`}},
		{"raw modify file", "unsupported_feature", "", []string{"raw", "--method", "PUT", "--path", "/%4dodifyTopic", "--input", "file://" + requestFile}},
		{"raw dry-run", "unsupported_feature", "", []string{"--dry-run", "raw", "--method", "POST", "--path", "/CreateTopic", "--body", `{"DataIngestSpec":"D7"}`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, env := outcomeCLIRun(t, append([]string{"--allow-write"}, tc.args...))
			if code == 0 || calls.Load() != 0 {
				t.Fatalf("restricted request sent or accepted: exit=%d calls=%d result=%#v", code, calls.Load(), env)
			}
			assertOutcomeCLIEnvelope(t, env, "not_sent", "success", tc.kind, tc.errorCode)
		})
	}
}

func TestTopicWritesKeepPublicRetentionFields(t *testing.T) {
	for _, tc := range []struct {
		action, method, path, input string
	}{
		{"topic.create", http.MethodPost, "/CreateTopic", `{"ProjectId":"p","TopicName":"demo","Ttl":30,"MeteringMode":"ChargeByDataIngest"}`},
		{"topic.modify", http.MethodPut, "/ModifyTopic", `{"TopicId":"t","Ttl":30}`},
	} {
		t.Run(tc.action, func(t *testing.T) {
			_, calls := outcomeCLIServer(t, func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Errorf("decode request: %v", err)
					return
				}
				if r.Method != tc.method || r.URL.Path != tc.path || body["Ttl"] != float64(30) {
					t.Errorf("unexpected request %s %s: %#v", r.Method, r.URL.Path, body)
				}
				if tc.action == "topic.create" && body["MeteringMode"] != "ChargeByDataIngest" {
					t.Errorf("missing metering mode: %#v", body)
				}
				if _, exists := body["DataIngestSpec"]; exists {
					t.Errorf("hidden field sent: %#v", body)
				}
				_, _ = w.Write([]byte(`{"TopicId":"t"}`))
			})
			code, env := outcomeCLIRun(t, []string{"--allow-write", "tool", "exec", tc.action, "--input", tc.input})
			if code != 0 || calls.Load() != 1 {
				t.Fatalf("exit=%d calls=%d result=%#v", code, calls.Load(), env)
			}
			assertOutcomeCLIEnvelope(t, env, "success", "success", "", "")
		})
	}
}

func TestTopicDataIngestSpecHiddenFromReadResults(t *testing.T) {
	outcomeCLIServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/DescribeTopic":
			_, _ = w.Write([]byte(`{"TopicId":"t","MeteringMode":"ChargeByDataIngest","Ttl":7,"DataIngestSpec":"D7"}`))
		case "/DescribeTopics":
			_, _ = w.Write([]byte(`{"Total":1,"Topics":[{"TopicId":"t","Ttl":7,"DataIngestSpec":"D7"}]}`))
		case "/DescribeProjects":
			_, _ = w.Write([]byte(`{"DataIngestSpec":"unrelated data"}`))
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	for _, tc := range []struct {
		name, listField string
		args            []string
	}{
		{"tool detail", "", []string{"tool", "exec", "topic.describe-topic", "--input", `{"TopicId":"t"}`}},
		{"tool list", "Topics", []string{"tool", "exec", "topic.describe-topics", "--input", `{"ProjectId":"p"}`}},
		{"raw detail", "", []string{"raw", "--path", "/DescribeTopic", "--query", "TopicId=t"}},
		{"raw list", "Topics", []string{"raw", "--path", "/DescribeTopics", "--query", "ProjectId=p"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, env := outcomeCLIRun(t, tc.args)
			if code != 0 {
				t.Fatalf("exit=%d result=%#v", code, env)
			}
			data := c5Object(t, env["data"], "data")
			if tc.listField != "" {
				rows := c5Array(t, data[tc.listField], "data.Topics")
				if len(rows) != 1 {
					t.Fatalf("unexpected rows: %#v", rows)
				}
				data = c5Object(t, rows[0], "topic")
			}
			if _, exists := data["DataIngestSpec"]; exists || data["TopicId"] != "t" || data["Ttl"] != float64(7) {
				t.Fatalf("unexpected topic result: %#v", data)
			}
		})
	}
	code, env := outcomeCLIRun(t, []string{"raw", "--path", "/DescribeProjects"})
	if code != 0 || c5Object(t, env["data"], "data")["DataIngestSpec"] != "unrelated data" {
		t.Fatalf("non-Topic response modified: exit=%d result=%#v", code, env)
	}
}

func TestTopicDataIngestSpecHiddenFromPaginatedExport(t *testing.T) {
	_, calls := outcomeCLIServer(t, func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("PageNumber")
		switch page {
		case "1", "2":
			_, _ = fmt.Fprintf(w, `{"Total":2,"Topics":[{"TopicId":"t%s","Ttl":7,"DataIngestSpec":"D7"}]}`, page)
		default:
			t.Errorf("unexpected page %q", page)
			w.WriteHeader(http.StatusBadRequest)
		}
	})
	base := []string{"--output-mode", "file", "--output-dir", t.TempDir()}
	query := []string{"tool", "exec", "topic.describe-topics", "--page-all", "--input", `{"ProjectId":"p","PageSize":1}`}
	raw, _ := runFilteredFileOutput(t, append(base, query...))
	env := outcomeCLIDecode(t, raw)
	state := exportTestState(t, env)
	rows := c5Array(t, c5Object(t, env["data"], "data")["Topics"], "data.Topics")
	if calls.Load() != 2 || state["complete"] != true || len(rows) != 2 {
		t.Fatalf("export lost results: calls=%d result=%#v", calls.Load(), env)
	}
	for _, row := range rows {
		topic := c5Object(t, row, "exported topic")
		if _, exists := topic["DataIngestSpec"]; exists || topic["Ttl"] != float64(7) {
			t.Fatalf("unexpected exported topic: %#v", topic)
		}
	}

	args := append(base, "--jmes-filter", "data.Topics[].{id: TopicId, spec: DataIngestSpec}")
	projected, _ := runFilteredFileOutput(t, append(args, query...))
	assertFilteredJSON(t, projected, `[{"id":"t1","spec":null},{"id":"t2","spec":null}]`)
}
