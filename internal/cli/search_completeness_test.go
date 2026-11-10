package cli

import (
	"encoding/json"
	"testing"
)

func singleSearchTestArgs(kind, query string) []string {
	input := exportTestInput(query, 0)
	if kind == "raw" {
		return []string{"raw", "--method", "POST", "--path", "/SearchLogs", "--body", input}
	}
	return []string{"tool", "exec", "log.search", "--input", `{"body":` + input + `}`}
}

func TestSingleSearchCompletionSignals(t *testing.T) {
	for _, kind := range []string{"raw", "tool"} {
		for _, tc := range []struct {
			name, query, response, reason, errorKind string
			complete, success, resumable             bool
			items                                    int
		}{
			{"complete search", "*", `{"Logs":[{"n":1}],"ListOver":true}`, "list_over", "", true, true, false, 1},
			{"normal next page", "*", `{"Logs":[{"n":1}],"ListOver":false,"Context":"next"}`, "more_results", "", false, true, true, 1},
			{"incomplete overrides end", "*", `{"Logs":[{"n":1}],"ListOver":true,"Context":"next","ResultStatus":"incomplete"}`, "result_incomplete", "incomplete_result", false, false, true, 1},
			{"timeout", "*", `{"Logs":[{"n":1}],"ListOver":true,"ResultStatus":"time_out"}`, "result_time_out", "incomplete_result", false, false, false, 1},
			{"error retains rows", "*", `{"Logs":[{"n":1}],"ResultStatus":"error","Context":"unverified"}`, "result_error", "server", false, false, false, 1},
			{"missing cursor", "*", `{"Logs":[{"n":1}],"ListOver":false}`, "missing_cursor", "incomplete_result", false, false, false, 1},
			{"no end evidence", "*", `{"Logs":[{"n":1}]}`, "unknown_completion", "incomplete_result", false, false, false, 1},
			{"sql complete", "* | select 1", `{"AnalysisResult":{"Data":[{"n":1}]},"ResultStatus":"complete","ListOver":false}`, "result_complete", "", true, true, false, 1},
			{"sql missing status ignores list over", "* | select 1", `{"AnalysisResult":{"Data":[{"n":1}]},"ListOver":true,"Context":"not-sql-pagination"}`, "unknown_completion", "incomplete_result", false, false, false, 1},
			{"sql incomplete never uses context", "* | select 1", `{"AnalysisResult":{"Data":[{"n":1}]},"ResultStatus":"incomplete","Context":"not-sql-pagination"}`, "result_incomplete", "incomplete_result", false, false, false, 1},
			{"unknown status", "*", `{"Logs":[{"n":1}],"ListOver":true,"ResultStatus":"new-status","Context":"unverified"}`, "unknown_completion", "protocol", false, false, false, 1},
			{"invalid status", "*", `{"Logs":[{"n":1}],"ListOver":true,"ResultStatus":false}`, "unknown_completion", "protocol", false, false, false, 1},
			{"invalid shape", "*", `[]`, "unknown_completion", "protocol", false, false, false, 0},
			{"invalid logs", "*", `{"Logs":{},"ListOver":true,"ResultStatus":"complete"}`, "invalid_response", "protocol", false, false, false, 0},
			{"sql missing result", "* | select 1", `{"ResultStatus":"complete"}`, "invalid_response", "protocol", false, false, false, 0},
			{"sql invalid rows", "* | select 1", `{"AnalysisResult":{"Data":{}},"ResultStatus":"complete"}`, "invalid_response", "protocol", false, false, false, 0},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				srv := startExportTestServer(t, exportTestResponse{body: tc.response})
				code, env := runExportTestCLI(t, singleSearchTestArgs(kind, tc.query)...)
				if (code == 0) != tc.success || (env["status"] == "success") != tc.success {
					t.Fatalf("unexpected exit=%d envelope=%#v", code, env)
				}
				state := exportTestState(t, env)
				if state["complete"] != tc.complete || state["stopReason"] != tc.reason || state["itemCount"] != float64(tc.items) || state["pageCount"] != float64(1) || state["resumable"] != tc.resumable {
					t.Fatalf("incorrect completeness: %#v", state)
				}
				if tc.errorKind != "" && env["error"].(map[string]any)["kind"] != tc.errorKind {
					t.Fatalf("incorrect error kind: %#v", env["error"])
				}
				if tc.reason == "result_error" && env["summary"].(map[string]any)["remoteOutcome"] != "partial" {
					t.Fatalf("lost partial remote result: %#v", env)
				}
				if len(srv.captured()) != 1 {
					t.Fatal("single search unexpectedly issued multiple requests")
				}
			})
		}
	}
}

func TestSingleSearchContinuationPreservesInputAndExecutes(t *testing.T) {
	srv := startExportTestServer(t,
		exportTestResponse{body: `{"Logs":[{"n":1}],"ListOver":false,"Context":"next"}`},
		exportTestResponse{body: `{"Logs":[{"n":2}],"ListOver":true}`},
	)
	code, env := runExportTestCLI(t, singleSearchTestArgs("tool", "source:api")...)
	if code != 0 {
		t.Fatalf("single page failed: %#v", env)
	}
	next := exportTestState(t, env)["next"].(map[string]any)
	if next["operationId"] != "log.search" || next["pageAll"] != false {
		t.Fatalf("invalid continuation: %#v", next)
	}
	raw, err := json.Marshal(next["input"])
	if err != nil {
		t.Fatal(err)
	}
	code, env = runExportTestCLI(t, "tool", "exec", next["operationId"].(string), "--input", string(raw))
	if code != 0 || exportTestState(t, env)["complete"] != true {
		t.Fatalf("continuation failed: exit=%d env=%#v", code, env)
	}
	requests := srv.captured()
	if len(requests) != 2 || requests[1]["Context"] != "next" || requests[1]["Query"] != "source:api" || requests[1]["TopicId"] != "tid" || requests[1]["StartTime"] != requests[0]["StartTime"] {
		t.Fatalf("continuation changed original request: %#v", requests)
	}
}
