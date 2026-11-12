package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func TestTopicDataIngestSpecWrites(t *testing.T) {
	for _, spec := range []string{"D3", "D7", "D15", "D30"} {
		for _, tc := range []struct {
			action, method, path, input string
		}{
			{"topic.create", http.MethodPost, "/CreateTopic", fmt.Sprintf(`{"ProjectId":"p","TopicName":"demo","Ttl":30,"MeteringMode":"ChargeByDataIngest","DataIngestSpec":%q}`, spec)},
			{"topic.modify", http.MethodPut, "/ModifyTopic", fmt.Sprintf(`{"TopicId":"t","Ttl":30,"DataIngestSpec":%q}`, spec)},
		} {
			t.Run(tc.action+"/"+spec, func(t *testing.T) {
				_, calls := outcomeCLIServer(t, func(w http.ResponseWriter, r *http.Request) {
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Errorf("decode request: %v", err)
						return
					}
					if r.Method != tc.method || r.URL.Path != tc.path || body["DataIngestSpec"] != spec || body["Ttl"] != float64(30) {
						t.Errorf("request %s %s: %#v", r.Method, r.URL.Path, body)
					}
					_, _ = w.Write([]byte(`{"TopicId":"t"}`))
				})
				code, env := outcomeCLIRun(t, []string{"--allow-write", "tool", "exec", tc.action, "--input", tc.input})
				if code != 0 || env["status"] != "success" || calls.Load() != 1 {
					t.Fatalf("exit=%d calls=%d result=%#v", code, calls.Load(), env)
				}
			})
		}
	}
}

func TestTopicDataIngestSpecValidation(t *testing.T) {
	_, calls := outcomeCLIServer(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{}`)) })
	for _, tc := range []struct {
		name, action, input string
	}{
		{"unknown spec", "topic.create", `{"ProjectId":"p","TopicName":"demo","Ttl":30,"MeteringMode":"ChargeByDataIngest","DataIngestSpec":"D2"}`},
		{"missing metering mode", "topic.create", `{"ProjectId":"p","TopicName":"demo","Ttl":30,"DataIngestSpec":"D3"}`},
		{"wrong metering mode", "topic.create", `{"ProjectId":"p","TopicName":"demo","Ttl":30,"MeteringMode":"ChargeByFunction","DataIngestSpec":"D3"}`},
		{"empty modify spec", "topic.modify", `{"TopicId":"t","DataIngestSpec":""}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, env := outcomeCLIRun(t, []string{"--allow-write", "tool", "exec", tc.action, "--input", tc.input})
			if code == 0 || calls.Load() != 0 {
				t.Fatalf("invalid request sent or accepted: exit=%d calls=%d result=%#v", code, calls.Load(), env)
			}
			assertOutcomeCLIEnvelope(t, env, "not_sent", "success", "validation", "InvalidToolInput")
		})
	}
}

func TestTopicDataIngestSpecReads(t *testing.T) {
	_, calls := outcomeCLIServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/DescribeTopic":
			_, _ = w.Write([]byte(`{"TopicId":"t","MeteringMode":"ChargeByDataIngest","Ttl":30,"DataIngestSpec":"D7"}`))
		case "/DescribeTopics":
			_, _ = w.Write([]byte(`{"Total":2,"Topics":[{"TopicId":"t","Ttl":30,"DataIngestSpec":"D7"},{"TopicId":"legacy","Ttl":30}]}`))
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	code, single := outcomeCLIRun(t, []string{"tool", "exec", "topic.describe-topic", "--input", `{"TopicId":"t"}`})
	if code != 0 || c5Object(t, single["data"], "data")["DataIngestSpec"] != "D7" {
		t.Fatalf("describe topic exit=%d result=%#v", code, single)
	}
	code, list := outcomeCLIRun(t, []string{"tool", "exec", "topic.describe-topics", "--input", `{"ProjectId":"p"}`})
	if code != 0 || calls.Load() != 2 {
		t.Fatalf("describe topics exit=%d calls=%d result=%#v", code, calls.Load(), list)
	}
	rows := c5Array(t, c5Object(t, list["data"], "data")["Topics"], "data.Topics")
	if len(rows) != 2 || c5Object(t, rows[0], "topic")["DataIngestSpec"] != "D7" {
		t.Fatalf("unexpected topic list %#v", rows)
	}
	if _, exists := c5Object(t, rows[1], "legacy topic")["DataIngestSpec"]; exists {
		t.Fatal("CLI invented an entitlement for a topic without the field")
	}
}
