package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

func TestC5ToolInputRejectsInvalidDeclaredValuesBeforeSending(t *testing.T) {
	_, requests := outcomeCLIServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	})
	for _, tc := range []struct{ name, operation, input string }{
		{"sectioned extra root", "project.create", `{"body":{"ProjectName":"demo","Region":"cn-beijing"},"ignored":"c5-secret-canary"}`},
		{"closed body flat extra", "project.create", `{"ProjectName":"demo","Region":"cn-beijing","Surprise":true}`},
		{"closed body sectioned extra", "project.create", `{"body":{"ProjectName":"demo","Region":"cn-beijing","Surprise":true}}`},
		{"non-object section", "project.create", `{"query":[],"body":{"ProjectName":"demo","Region":"cn-beijing"}}`},
		{"declared type", "log.search", `{"TopicId":"tid","Query":"*","StartTime":"1710374400000","EndTime":1710378000000}`},
		{"declared enum", "log.search", `{"TopicId":"tid","Query":"*","StartTime":1710374400000,"EndTime":1710378000000,"Sort":"sideways"}`},
		{"declared bound", "log.search", `{"TopicId":"tid","Query":"*","StartTime":1710374400000,"EndTime":1710378000000,"Limit":1001}`},
		{"non-negative shard", "shard.merge", `{"TopicId":"tid","ShardId":-1}`},
		{"nested array element", "project.create", `{"ProjectName":"demo","Region":"cn-beijing","Tags":[{"Key":123}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := requests.Load()
			var stdout, stderr bytes.Buffer
			code := Run([]string{"--dry-run", "tool", "exec", tc.operation, "--input", tc.input}, &stdout, &stderr)
			var env map[string]any
			if code != 1 || stderr.Len() != 0 || json.Unmarshal(stdout.Bytes(), &env) != nil || env["status"] != "failed" {
				t.Fatalf("invalid input accepted or wrong error protocol: exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			if env["error"].(map[string]any)["kind"] != "validation" || env["summary"].(map[string]any)["remoteOutcome"] != "not_sent" || requests.Load() != before {
				t.Fatalf("invalid input reached execution: %#v requests=%d", env, requests.Load()-before)
			}
			if bytes.Contains(stdout.Bytes(), []byte("c5-secret-canary")) {
				t.Fatal("validation error disclosed input value")
			}
		})
	}
}

func TestSearchLogsRequestLimitAndSQLLimit(t *testing.T) {
	for _, tc := range []struct {
		name      string
		input     string
		wantQuery string
		wantLimit any
		response  string
	}{
		{
			name:      "request limit at maximum",
			input:     `{"TopicId":"tid","Query":"*","StartTime":1710374400000,"EndTime":1710378000000,"Limit":1000}`,
			wantQuery: "*",
			wantLimit: float64(1000),
			response:  `{"ResultStatus":"complete","ListOver":true,"Logs":[]}`,
		},
		{
			name:      "larger SQL limit",
			input:     `{"TopicId":"tid","Query":"* | SELECT * LIMIT 2000","StartTime":1710374400000,"EndTime":1710378000000}`,
			wantQuery: "* | SELECT * LIMIT 2000",
			response:  `{"ResultStatus":"complete","AnalysisResult":{"Data":[]}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, requests := outcomeCLIServer(t, func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Errorf("decode request: %v", err)
					return
				}
				if body["Query"] != tc.wantQuery {
					t.Errorf("request Query=%v, want %q", body["Query"], tc.wantQuery)
				}
				if limit, present := body["Limit"]; limit != tc.wantLimit || present != (tc.wantLimit != nil) {
					t.Errorf("request Limit=%v present=%v, want %v", limit, present, tc.wantLimit)
				}
				_, _ = w.Write([]byte(tc.response))
			})
			code, env := outcomeCLIRun(t, []string{"tool", "exec", "log.search", "--input", tc.input})
			if code != 0 || env["status"] != "success" || requests.Load() != 1 {
				t.Fatalf("expected one successful search: exit=%d envelope=%#v requests=%d", code, env, requests.Load())
			}
		})
	}
}

func TestC5WorkflowRejectsUnsupportedPageControls(t *testing.T) {
	_, requests := outcomeCLIServer(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{}`)) })
	for _, context := range []string{`{"execution":{"page":{"all":true}}}`, `{"execution":{"page":{"all":false}}}`, `{"execution":{"page_all":true}}`, `{"execution":{"page_all":false}}`} {
		t.Run(context, func(t *testing.T) {
			code, env := runExportTestCLI(t, "--dry-run", "workflow", "exec", appResolveTopicIDsWorkflowID, "--input", `{"AppId":"app-1"}`, "--context", context)
			if code != 1 || env["status"] != "failed" || env["error"].(map[string]any)["kind"] != "unsupported_feature" || requests.Load() != 0 {
				t.Fatalf("workflow silently ignored page control: exit=%d envelope=%#v requests=%d", code, env, requests.Load())
			}
		})
	}
}

func TestC5GlobalValuesCannotConsumeDryRunFlag(t *testing.T) {
	_, requests := outcomeCLIServer(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{}`)) })
	for _, args := range [][]string{
		{"--output-dir", "--dry-run", "raw", "--path", "/DescribeProjects"},
		{"raw", "--path", "/DescribeProjects", "--output-dir", "--dry-run"},
	} {
		var stdout, stderr bytes.Buffer
		if code := Run(args, &stdout, &stderr); code != 1 || requests.Load() != 0 {
			t.Fatalf("flag consumed as value: args=%v exit=%d requests=%d stdout=%q stderr=%q", args, code, requests.Load(), stdout.String(), stderr.String())
		}
	}
}
