package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/volcengine-tls/ve-tls-cli/internal/execution"
)

const downloadTaskC5Input = `{"body":{"TopicId":"topic-1","TaskName":"task-one","TaskType":0,"StartTime":1710374400000,"EndTime":1710378000000,"DataFormat":"json","Compression":"gzip","Limit":100,"Sort":"asc"}}`

func TestDownloadTaskCreateResultC5(t *testing.T) {
	body := map[string]any{"TopicId": "topic-1", "TaskName": "task-one", "Query": "*"}
	input := execution.Input{Body: execution.Payload{JSON: body}, Header: map[string]any{"Authorization": "secret"}}
	data := map[string]any{"TaskId": "task-1"}
	extra, outcome, err := inspectDownloadTaskResult("log.create-download-task", input, data)
	if err != nil || outcome != execution.OutcomeAccepted {
		t.Fatalf("outcome=%q err=%v", outcome, err)
	}
	async := extra["async"].(map[string]any)
	if async["taskId"] != "task-1" || async["state"] != "accepted" || async["terminal"] != false {
		t.Fatalf("async=%#v", async)
	}
	if !reflect.DeepEqual(async["match"], map[string]any{"field": "Tasks[].TaskId", "equals": "task-1"}) {
		t.Fatalf("match=%#v", async["match"])
	}
	if scope, _ := async["scope"].(string); !strings.Contains(scope, "fuzzy") || !strings.Contains(scope, "pages") {
		t.Fatalf("scope=%#v", async["scope"])
	}
	next, ok := async["next"].(*execution.Continuation)
	if !ok || next.OperationID != "log.describe-download-tasks" || next.PageAll {
		t.Fatalf("next=%#v", async["next"])
	}
	want := map[string]any{"query": map[string]any{"TopicId": "topic-1", "TaskName": "task-one", "PageNumber": 1, "PageSize": 100}}
	if !reflect.DeepEqual(next.Input, want) {
		t.Fatalf("next input=%#v", next.Input)
	}
	encoded, _ := json.Marshal(next)
	if strings.Contains(string(encoded), "secret") {
		t.Fatalf("next includes request credentials: %s", encoded)
	}
	next.Input["query"].(map[string]any)["TopicId"] = "changed"
	if !reflect.DeepEqual(body, map[string]any{"TopicId": "topic-1", "TaskName": "task-one", "Query": "*"}) || !reflect.DeepEqual(data, map[string]any{"TaskId": "task-1"}) {
		t.Fatalf("input or data changed: input=%#v data=%#v", body, data)
	}
}

func TestDownloadTaskCreateNextRequiresTopicC5(t *testing.T) {
	for _, tc := range []struct {
		name     string
		input    execution.Input
		wantNext bool
		wantName bool
	}{
		{"missing topic", execution.Input{Body: execution.Payload{JSON: map[string]any{"TaskName": "task-one"}}}, false, false},
		{"invalid topic", execution.Input{Body: execution.Payload{JSON: map[string]any{"TopicId": 7}}}, false, false},
		{"blank topic", execution.Input{Body: execution.Payload{JSON: map[string]any{"TopicId": "  "}}}, false, false},
		{"optional name", execution.Input{Body: execution.Payload{JSON: map[string]any{"TopicId": "topic-1"}}}, true, false},
		{"raw body", execution.Input{Body: execution.Payload{Raw: []byte(`{"TopicId":"topic-1","TaskName":"task-one"}`)}}, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			extra, outcome, err := inspectDownloadTaskResult("log.create-download-task", tc.input, map[string]any{"TaskId": "task-1"})
			if err != nil || outcome != execution.OutcomeAccepted {
				t.Fatalf("outcome=%q err=%v", outcome, err)
			}
			async := extra["async"].(map[string]any)
			next, exists := async["next"]
			if exists != tc.wantNext {
				t.Fatalf("next=%#v exists=%v", next, exists)
			}
			if exists {
				query := next.(*execution.Continuation).Input["query"].(map[string]any)
				_, hasName := query["TaskName"]
				if hasName != tc.wantName {
					t.Fatalf("query=%#v", query)
				}
			}
		})
	}
}

func TestDownloadTaskInvalidResponseC5(t *testing.T) {
	for _, tc := range []struct {
		operation string
		data      any
	}{
		{"log.create-download-task", nil},
		{"log.create-download-task", map[string]any{}},
		{"log.create-download-task", map[string]any{"TaskId": "  "}},
		{"log.create-download-task", map[string]any{"TaskId": 1}},
		{"log.describe-download-tasks", nil},
		{"log.describe-download-tasks", map[string]any{}},
		{"log.describe-download-tasks", map[string]any{"Tasks": nil}},
		{"log.describe-download-tasks", map[string]any{"Tasks": "secret-invalid-tasks"}},
	} {
		extra, outcome, err := inspectDownloadTaskResult(tc.operation, execution.Input{}, tc.data)
		var failure *execution.Failure
		if extra != nil || outcome != "" || !errors.As(err, &failure) || failure.Kind != "protocol" || failure.Code != "InvalidTaskResponse" || failure.Stage != "decode" || failure.Outcome != execution.OutcomeUnknown {
			t.Fatalf("operation=%s extra=%#v outcome=%q err=%#v", tc.operation, extra, outcome, err)
		}
		if strings.Contains(err.Error(), "secret-invalid-tasks") {
			t.Fatalf("invalid value was echoed: %v", err)
		}
	}
	extra, outcome, err := inspectDownloadTaskResult("log.search", execution.Input{}, nil)
	if extra != nil || outcome != "" || err != nil {
		t.Fatalf("unrelated operation: extra=%#v outcome=%q err=%v", extra, outcome, err)
	}
}

func TestDownloadTaskListStatesC5(t *testing.T) {
	for _, tc := range []struct {
		name, status, state string
		terminal            bool
		nextKey             string
	}{
		{"wait", "wait", "pending", false, ""},
		{"creating", "creating", "running", false, ""},
		{"success", "success", "succeeded", true, "next"},
		{"truncated", "created_cut", "partial", true, "possibleNext"},
		{"failure", "fail", "failed", true, ""},
		{"unknown", "future_status", "unknown", false, ""},
		{"no case guessing", "SUCCESS", "unknown", false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := map[string]any{"TaskId": "task-1", "TaskStatus": tc.status}
			data := map[string]any{"Tasks": []any{row}, "Total": 1}
			extra, outcome, err := inspectDownloadTaskResult("log.describe-download-tasks", execution.Input{}, data)
			if err != nil || outcome != "" {
				t.Fatalf("outcome=%q err=%v", outcome, err)
			}
			items := extra["asyncTasks"].([]map[string]any)
			item := items[0]
			if item["taskId"] != "task-1" || item["rawStatus"] != tc.status || item["state"] != tc.state || item["terminal"] != tc.terminal {
				t.Fatalf("item=%#v", item)
			}
			for _, key := range []string{"next", "possibleNext"} {
				value, exists := item[key]
				if exists != (key == tc.nextKey) {
					t.Fatalf("item=%#v key=%s", item, key)
				}
				if exists {
					next := value.(*execution.Continuation)
					want := map[string]any{"query": map[string]any{"TaskId": "task-1"}}
					if next.OperationID != "log.describe-download-url" || next.PageAll || !reflect.DeepEqual(next.Input, want) {
						t.Fatalf("next=%#v", next)
					}
				}
			}
			if tc.status == "created_cut" {
				condition, ok := item["possibleNextCondition"].(string)
				if item["resultComplete"] != false || !ok || !strings.Contains(condition, "not guaranteed") {
					t.Fatalf("truncated item=%#v", item)
				}
			} else if _, exists := item["resultComplete"]; exists {
				t.Fatalf("unexpected result completeness: %#v", item)
			}
			if !reflect.DeepEqual(row, map[string]any{"TaskId": "task-1", "TaskStatus": tc.status}) {
				t.Fatalf("row mutated: %#v", row)
			}
		})
	}
}

func TestDownloadTaskListDoesNotGuessC5(t *testing.T) {
	rows := []any{
		map[string]any{"TaskId": "other", "TaskStatus": "success"},
		map[string]any{"TaskId": "target", "TaskStatus": "creating"},
		map[string]any{"TaskStatus": "success"},
		map[string]any{"TaskId": 12, "TaskStatus": "success"},
		map[string]any{"TaskId": "missing-status"},
		map[string]any{"TaskId": "invalid-status", "TaskStatus": false},
		nil,
	}
	extra, outcome, err := inspectDownloadTaskResult("log.describe-download-tasks", execution.Input{}, map[string]any{"Tasks": rows})
	if err != nil || outcome != "" {
		t.Fatalf("outcome=%q err=%v", outcome, err)
	}
	items := extra["asyncTasks"].([]map[string]any)
	if len(items) != len(rows) || items[0]["state"] != "succeeded" || items[1]["state"] != "running" {
		t.Fatalf("items=%#v", items)
	}
	for _, item := range items[2:] {
		if item["state"] != "unknown" || item["terminal"] != false {
			t.Fatalf("guessed an unknown state: %#v", item)
		}
		if _, ok := item["next"]; ok {
			t.Fatalf("unexpected next: %#v", item)
		}
	}
	if items[5]["rawStatus"] != false {
		t.Fatalf("invalid status was not preserved: %#v", items[5])
	}
}

func TestDownloadTaskRunConsumableNextC5(t *testing.T) {
	var listCalls atomic.Int32
	_, requests := outcomeCLIServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/CreateDownloadTask":
			_, _ = w.Write([]byte(`{"TaskId":"task-1"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/DescribeDownloadTasks":
			q := r.URL.Query()
			if q.Get("TopicId") != "topic-1" || q.Get("TaskName") != "task-one" || q.Get("PageNumber") != "1" || q.Get("PageSize") != "100" || q.Has("TaskId") {
				t.Errorf("unexpected status query: %v", q)
			}
			status := "wait"
			if listCalls.Add(1) > 1 {
				status = "success"
			}
			_, _ = fmt.Fprintf(w, `{"Tasks":[{"TaskId":"other","TaskStatus":"success"},{"TaskId":"task-1","TaskStatus":%q}],"Total":2}`, status)
		case r.Method == http.MethodGet && r.URL.Path == "/DescribeDownloadUrl":
			if r.URL.Query().Get("TaskId") != "task-1" {
				t.Errorf("unexpected URL query: %v", r.URL.Query())
			}
			_, _ = w.Write([]byte(`{"DownloadUrl":"https://example.invalid/file"}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", http.StatusBadRequest)
		}
	})
	code, env := outcomeCLIRun(t, []string{"tool", "exec", "log.create-download-task", "--input", downloadTaskC5Input})
	if code != 0 || env["status"] != "success" || requests.Load() != 1 {
		t.Fatalf("create exit=%d requests=%d env=%#v", code, requests.Load(), env)
	}
	summary := env["summary"].(map[string]any)
	async := summary["async"].(map[string]any)
	if summary["remoteOutcome"] != "accepted" || async["taskId"] != "task-1" || async["terminal"] != false {
		t.Fatalf("create summary=%#v", summary)
	}
	statusNext := async["next"].(map[string]any)
	statusInput, err := json.Marshal(statusNext["input"])
	if err != nil || statusNext["operationId"] != "log.describe-download-tasks" || statusNext["pageAll"] != false {
		t.Fatalf("status next=%#v err=%v", statusNext, err)
	}
	for i, wantState := range []string{"pending", "succeeded"} {
		code, env = outcomeCLIRun(t, []string{"tool", "exec", statusNext["operationId"].(string), "--input", string(statusInput)})
		if code != 0 || env["status"] != "success" || requests.Load() != int32(i+2) {
			t.Fatalf("list exit=%d requests=%d env=%#v", code, requests.Load(), env)
		}
		summary = env["summary"].(map[string]any)
		tasks := summary["asyncTasks"].([]any)
		other := tasks[0].(map[string]any)
		target := tasks[1].(map[string]any)
		if summary["remoteOutcome"] != "success" || other["state"] != "succeeded" || target["taskId"] != "task-1" || target["state"] != wantState {
			t.Fatalf("unexpected list summary=%#v", summary)
		}
		if i == 0 {
			if _, exists := target["next"]; exists {
				t.Fatalf("pending task has next: %#v", target)
			}
			continue
		}
		urlNext := target["next"].(map[string]any)
		urlInput, err := json.Marshal(urlNext["input"])
		if err != nil || urlNext["operationId"] != "log.describe-download-url" || urlNext["pageAll"] != false {
			t.Fatalf("url next=%#v err=%v", urlNext, err)
		}
		code, env = outcomeCLIRun(t, []string{"tool", "exec", urlNext["operationId"].(string), "--input", string(urlInput)})
		if code != 0 || env["status"] != "success" || requests.Load() != 4 || env["data"].(map[string]any)["DownloadUrl"] != "https://example.invalid/file" {
			t.Fatalf("url exit=%d requests=%d env=%#v", code, requests.Load(), env)
		}
	}
}

func TestDownloadTaskRunInvalidIDAndProjectionC5(t *testing.T) {
	for _, tc := range []struct {
		name       string
		response   string
		projection bool
		wantError  bool
	}{
		{"invalid id", `{"TaskId":false}`, false, true},
		{"projected data", `{"TaskId":"task-1"}`, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, requests := outcomeCLIServer(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tc.response))
			})
			args := []string{"tool", "exec", "log.create-download-task", "--input", downloadTaskC5Input}
			if tc.projection {
				args = append(args, "--context", `{"execution":{"projection":"TaskId"}}`)
			}
			code, env := outcomeCLIRun(t, args)
			if requests.Load() != 1 {
				t.Fatalf("unexpected request count: %d", requests.Load())
			}
			summary := env["summary"].(map[string]any)
			if tc.wantError {
				failure := env["error"].(map[string]any)
				if code == 0 || env["status"] != "failed" || summary["remoteOutcome"] != "unknown" || failure["kind"] != "protocol" || failure["code"] != "InvalidTaskResponse" {
					t.Fatalf("exit=%d env=%#v", code, env)
				}
				return
			}
			async := summary["async"].(map[string]any)
			if code != 0 || summary["remoteOutcome"] != "accepted" || async["taskId"] != "task-1" || async["next"] == nil {
				t.Fatalf("exit=%d env=%#v", code, env)
			}
		})
	}
}

func TestDownloadTaskRawCreateC5(t *testing.T) {
	_, requests := outcomeCLIServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/CreateDownloadTask" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"TaskId":"task-1"}`))
	})
	code, env := outcomeCLIRun(t, []string{"raw", "--method", "POST", "--path", "/CreateDownloadTask", "--body", `{"TopicId":"topic-1","TaskName":"task-one"}`})
	if code != 0 || env["status"] != "success" || requests.Load() != 1 {
		t.Fatalf("exit=%d requests=%d env=%#v", code, requests.Load(), env)
	}
	summary := env["summary"].(map[string]any)
	async := summary["async"].(map[string]any)
	if summary["remoteOutcome"] != "accepted" || async["taskId"] != "task-1" || async["next"] == nil {
		t.Fatalf("summary=%#v", summary)
	}
}
