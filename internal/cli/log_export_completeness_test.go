package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/volcengine-tls/ve-tls-cli/internal/execution"
	"github.com/volcengine-tls/ve-tls-cli/internal/output"
)

type exportTestResponse struct {
	status int
	body   string
}

type exportTestServer struct {
	mu        sync.Mutex
	responses []exportTestResponse
	requests  []map[string]any
}

func (s *exportTestServer) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	index := len(s.requests)
	s.requests = append(s.requests, body)
	var response exportTestResponse
	if index < len(s.responses) {
		response = s.responses[index]
	} else {
		response = exportTestResponse{status: http.StatusInternalServerError, body: `{"errorCode":"UnexpectedRequest"}`}
	}
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Tls-Requestid", fmt.Sprintf("request-%d", index+1))
	if response.status != 0 {
		w.WriteHeader(response.status)
	}
	_, _ = w.Write([]byte(response.body))
}

func (s *exportTestServer) captured() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]any(nil), s.requests...)
}

func startExportTestServer(t *testing.T, responses ...exportTestResponse) *exportTestServer {
	t.Helper()
	handler := &exportTestServer{responses: responses}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	setWorkflowAppTestEnv(t, server.URL)
	return handler
}

func exportTestInput(query string, maxPages int) string {
	input := map[string]any{"TopicId": "tid", "Query": query, "StartTime": 1710374400000, "EndTime": 1710378000000}
	if maxPages != 0 {
		input["MaxPages"] = maxPages
	}
	b, _ := json.Marshal(input)
	return string(b)
}

func runExportTestCLI(t *testing.T, args ...string) (int, map[string]any) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(args, &stdout, &stderr)
	var envelope map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("exit=%d invalid envelope: %v stdout=%q stderr=%q", code, err, stdout.String(), stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr %q", stderr.String())
	}
	return code, envelope
}

func exportTestState(t *testing.T, env map[string]any) map[string]any {
	t.Helper()
	summary, ok := env["summary"].(map[string]any)
	if !ok {
		t.Fatalf("missing summary: %#v", env)
	}
	state, ok := summary["completeness"].(map[string]any)
	if !ok {
		t.Fatalf("missing completeness: %#v", env)
	}
	return state
}

func assertExportTestState(t *testing.T, code int, env map[string]any, complete bool, reason string, pages, items int, resumable bool) map[string]any {
	t.Helper()
	if (code == 0) != complete || (env["status"] == "success") != complete {
		t.Fatalf("exit=%d envelope=%#v", code, env)
	}
	state := exportTestState(t, env)
	if state["complete"] != complete || state["stopReason"] != reason || state["pageCount"] != float64(pages) || state["itemCount"] != float64(items) || state["resumable"] != resumable {
		t.Fatalf("unexpected completeness: %#v", state)
	}
	if _, exists := state["next"]; exists != resumable {
		t.Fatalf("unexpected continuation: %#v", state)
	}
	return state
}

func TestWorkflowLogExportCompletionAndCursorEvidence(t *testing.T) {
	tests := []struct {
		name      string
		responses []exportTestResponse
		maxPages  int
		reason    string
		complete  bool
		pages     int
		items     int
		resumable bool
	}{
		{"max pages", []exportTestResponse{{body: `{"Logs":[{"n":1}],"ListOver":false,"Context":"cursor-1"}`}}, 1, "max_pages", false, 1, 1, true},
		{"end at budget", []exportTestResponse{{body: `{"Logs":[{"n":1}],"ListOver":false,"Context":"cursor-1"}`}, {body: `{"Logs":[{"n":2}],"ListOver":true}`}}, 2, "list_over", true, 2, 2, false},
		{"absent end evidence", []exportTestResponse{{body: `{"Logs":[{"n":1}]}`}}, 2, "unknown_completion", false, 1, 1, false},
		{"missing cursor", []exportTestResponse{{body: `{"Logs":[{"n":1}],"ListOver":false,"Context":" "}`}}, 2, "missing_cursor", false, 1, 1, false},
		{"stalled cursor", []exportTestResponse{{body: `{"Logs":[{"n":1}],"ListOver":false,"Context":"cursor-1"}`}, {body: `{"Logs":[{"n":2}],"ListOver":false,"Context":"cursor-1"}`}}, 3, "cursor_stalled", false, 2, 2, false},
		{"cursor cycle", []exportTestResponse{{body: `{"Logs":[{"n":1}],"ListOver":false,"Context":"cursor-1"}`}, {body: `{"Logs":[{"n":2}],"Context":"cursor-2"}`}, {body: `{"Logs":[{"n":3}],"Context":"cursor-1"}`}}, 4, "cursor_stalled", false, 3, 3, false},
		{"cursor without list over", []exportTestResponse{{body: `{"Logs":[{"n":1}],"Context":"cursor-1"}`}, {body: `{"Logs":[{"n":2}],"ListOver":true}`}}, 2, "list_over", true, 2, 2, false},
		{"status complete", []exportTestResponse{{body: `{"Logs":[{"n":1}],"ListOver":true,"ResultStatus":"complete"}`}}, 1, "list_over", true, 1, 1, false},
		{"status complete with more pages", []exportTestResponse{{body: `{"Logs":[{"n":1}],"ListOver":false,"Context":"cursor-1","ResultStatus":"complete"}`}}, 1, "max_pages", false, 1, 1, true},
		{"status incomplete", []exportTestResponse{{body: `{"Logs":[{"n":1}],"ListOver":true,"Context":"cursor-1","ResultStatus":"incomplete"}`}}, 1, "result_incomplete", false, 1, 1, true},
		{"status timeout", []exportTestResponse{{body: `{"Logs":[{"n":1}],"ListOver":true,"Context":"cursor-1","ResultStatus":"time_out"}`}}, 1, "result_time_out", false, 1, 1, true},
		{"status error", []exportTestResponse{{body: `{"Logs":[{"n":1}],"ListOver":true,"ResultStatus":"error"}`}}, 1, "result_error", false, 1, 1, false},
		{"unknown status", []exportTestResponse{{body: `{"Logs":[{"n":1}],"ListOver":true,"ResultStatus":"future_status"}`}}, 1, "unknown_completion", false, 1, 1, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := startExportTestServer(t, tc.responses...)
			code, env := runExportTestCLI(t, "workflow", "exec", "log.export", "--input", exportTestInput("*", tc.maxPages))
			assertExportTestState(t, code, env, tc.complete, tc.reason, tc.pages, tc.items, tc.resumable)
			rows, ok := env["data"].([]any)
			if !ok || len(rows) != tc.items {
				t.Fatalf("rows lost or duplicated: %#v", env)
			}
			requests := srv.captured()
			if len(requests) != len(tc.responses) || requests[0]["Limit"] != float64(500) {
				t.Fatalf("unexpected requests: %#v", requests)
			}
			if len(requests) > 1 && requests[1]["Context"] != "cursor-1" {
				t.Fatalf("cursor was not sent to second page: %#v", requests)
			}
			if tc.reason == "result_error" {
				err, _ := env["error"].(map[string]any)
				if err["code"] != "SearchResultError" || err["kind"] != "server" {
					t.Fatalf("unexpected search error: %#v", err)
				}
			}
		})
	}
}

func TestWorkflowLogExportContinuationExecutesSingleSearch(t *testing.T) {
	srv := startExportTestServer(t,
		exportTestResponse{body: `{"Logs":[{"n":1}],"ListOver":false,"Context":"cursor-1"}`},
		exportTestResponse{body: `{"Logs":[{"n":2}],"ListOver":true,"ResultStatus":"complete"}`},
	)
	code, env := runExportTestCLI(t, "workflow", "exec", "log.export", "--input", exportTestInput("*", 1))
	state := assertExportTestState(t, code, env, false, "max_pages", 1, 1, true)
	next, ok := state["next"].(map[string]any)
	if !ok || next["operationId"] != "log.search" || next["pageAll"] != false {
		t.Fatalf("invalid continuation: %#v", state)
	}
	input, err := json.Marshal(next["input"])
	if err != nil {
		t.Fatal(err)
	}
	code, resumed := runExportTestCLI(t, "tool", "exec", "log.search", "--input", string(input))
	if code != 0 || resumed["status"] != "success" {
		t.Fatalf("continuation failed: exit=%d envelope=%#v", code, resumed)
	}
	requests := srv.captured()
	if len(requests) != 2 || requests[1]["Context"] != "cursor-1" || requests[1]["Limit"] != float64(500) || requests[1]["Query"] != "*" {
		t.Fatalf("continuation changed the original input: %#v", requests)
	}
}

func TestWorkflowLogExportDefaultPageBudget(t *testing.T) {
	responses := make([]exportTestResponse, 100)
	for i := range responses {
		responses[i] = exportTestResponse{body: fmt.Sprintf(`{"Logs":[{"n":%d}],"ListOver":false,"Context":"cursor-%d"}`, i+1, i+1)}
	}
	srv := startExportTestServer(t, responses...)
	code, env := runExportTestCLI(t, "workflow", "exec", "log.export", "--input", exportTestInput("*", 0))
	assertExportTestState(t, code, env, false, "max_pages", 100, 100, true)
	requests := srv.captured()
	if len(requests) != 100 || requests[0]["Limit"] != float64(500) || requests[99]["Context"] != "cursor-99" {
		t.Fatalf("unexpected default page budget: count=%d", len(requests))
	}
}

func TestWorkflowLogExportPreservesRowsOnLaterFailure(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response exportTestResponse
	}{
		{"http", exportTestResponse{status: http.StatusInternalServerError, body: `{"errorCode":"InternalError"}`}},
		{"decode", exportTestResponse{body: `{} {}`}},
		{"invalid logs", exportTestResponse{body: `{"Logs":"invalid","ListOver":false,"Context":"cursor-2"}`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := startExportTestServer(t,
				exportTestResponse{body: `{"Logs":[{"n":1}],"ListOver":false,"Context":"cursor-1"}`},
				tc.response,
				exportTestResponse{body: `{"Logs":[{"n":2}],"ListOver":true,"ResultStatus":"complete"}`},
			)
			code, env := runExportTestCLI(t, "workflow", "exec", "log.export", "--input", exportTestInput("*", 2))
			state := assertExportTestState(t, code, env, false, "request_failed", 1, 1, true)
			rows, _ := env["data"].([]any)
			summary, _ := env["summary"].(map[string]any)
			if len(rows) != 1 || env["requestId"] != "request-2" || summary["remoteOutcome"] != "partial" {
				t.Fatalf("previous rows or current request metadata lost: %#v", env)
			}
			requests := srv.captured()
			if len(requests) != 2 {
				t.Fatalf("export retried the failed page automatically: %#v", requests)
			}
			next, _ := state["next"].(map[string]any)
			if next["operationId"] != "log.search" || next["pageAll"] != false {
				t.Fatalf("invalid continuation: %#v", state)
			}
			input, err := json.Marshal(next["input"])
			if err != nil {
				t.Fatal(err)
			}
			code, resumed := runExportTestCLI(t, "tool", "exec", "log.search", "--input", string(input))
			data, _ := resumed["data"].(map[string]any)
			resumedRows, _ := data["Logs"].([]any)
			if code != 0 || resumed["status"] != "success" || len(resumedRows) != 1 {
				t.Fatalf("continuation did not fetch the failed page: exit=%d envelope=%#v", code, resumed)
			}
			requests = srv.captured()
			if len(requests) != 3 || requests[1]["Context"] != "cursor-1" || !reflect.DeepEqual(requests[1], requests[2]) {
				t.Fatalf("continuation changed the failed page request: %#v", requests)
			}
			original := cloneLogExportRequest(requests[1])
			delete(original, "Context")
			if !reflect.DeepEqual(requests[0], original) {
				t.Fatalf("page request changed the original input: %#v", requests)
			}
		})
	}
}

func TestWorkflowLogExportAnalysisStatus(t *testing.T) {
	for _, tc := range []struct {
		status   string
		reason   string
		complete bool
	}{
		{"complete", "result_complete", true},
		{"incomplete", "result_incomplete", false},
		{"time_out", "result_time_out", false},
		{"error", "result_error", false},
		{"", "unknown_completion", false},
		{"future_status", "unknown_completion", false},
	} {
		t.Run(tc.reason+tc.status, func(t *testing.T) {
			response := map[string]any{"AnalysisResult": map[string]any{"Data": []any{map[string]any{"n": 1}}}, "ListOver": true, "Context": "must-not-resume"}
			if tc.status != "" {
				response["ResultStatus"] = tc.status
			}
			body, _ := json.Marshal(response)
			startExportTestServer(t, exportTestResponse{body: string(body)})
			code, env := runExportTestCLI(t, "workflow", "exec", "log.export-analysis", "--input", exportTestInput("* | select 1", 0))
			assertExportTestState(t, code, env, tc.complete, tc.reason, 1, 1, false)
			rows, _ := env["data"].([]any)
			if len(rows) != 1 {
				t.Fatalf("analysis rows lost: %#v", env)
			}
			if tc.status == "error" {
				err, _ := env["error"].(map[string]any)
				if err["code"] != "SearchResultError" || err["kind"] != "server" {
					t.Fatalf("unexpected search error: %#v", err)
				}
			}
		})
	}
}

func TestWorkflowLogExportAnalysisErrorRetainsValidRows(t *testing.T) {
	startExportTestServer(t, exportTestResponse{body: `{"ResultStatus":"error","AnalysisResult":{"Data":[{"n":1},null,{"n":2}]}}`})
	code, env := runExportTestCLI(t, "workflow", "exec", "log.export-analysis", "--input", exportTestInput("* | select 1", 0))
	assertExportTestState(t, code, env, false, "result_error", 1, 2, false)
	rows, _ := env["data"].([]any)
	err, _ := env["error"].(map[string]any)
	if len(rows) != 2 || err["code"] != "SearchResultError" {
		t.Fatalf("search error lost valid rows or status: %#v", env)
	}
}

func TestWorkflowLogExportAnalysisErrorWithoutRows(t *testing.T) {
	startExportTestServer(t, exportTestResponse{body: `{"ResultStatus":"error"}`})
	code, env := runExportTestCLI(t, "workflow", "exec", "log.export-analysis", "--input", exportTestInput("* | select 1", 0))
	assertExportTestState(t, code, env, false, "result_error", 1, 0, false)
	err, _ := env["error"].(map[string]any)
	summary, _ := env["summary"].(map[string]any)
	if err["code"] != "SearchResultError" || summary["remoteOutcome"] != "failed" {
		t.Fatalf("search error was not preserved: %#v", env)
	}
}

func TestWorkflowLogExportInitialFailureDoesNotCreateArtifact(t *testing.T) {
	startExportTestServer(t, exportTestResponse{status: http.StatusInternalServerError, body: `{"errorCode":"InternalError"}`})
	dir := t.TempDir()
	code, env := runExportTestCLI(t, "--output-mode", "file", "--output-dir", dir, "workflow", "exec", "log.export", "--input", exportTestInput("*", 1))
	assertExportTestState(t, code, env, false, "request_failed", 0, 0, false)
	artifacts, _ := env["artifacts"].([]any)
	entries, err := os.ReadDir(dir)
	if err != nil || len(artifacts) != 0 || len(entries) != 0 {
		t.Fatalf("initial failure created output: artifacts=%#v entries=%v err=%v", artifacts, entries, err)
	}
}

func TestWorkflowLogExportPartialFileAndMetadata(t *testing.T) {
	for _, tc := range []struct {
		name      string
		workflow  string
		query     string
		response  string
		reason    string
		resumable bool
	}{
		{"search", "log.export", "*", `{"Logs":[{"n":1},{"n":2}],"ListOver":false,"Context":"cursor-1"}`, "max_pages", true},
		{"analysis", "log.export-analysis", "* | select 1", `{"AnalysisResult":{"Data":[{"n":1},{"n":2}]},"ResultStatus":"time_out","Context":"ignored"}`, "result_time_out", false},
	} {
		for _, format := range []string{"json", "jsonl"} {
			t.Run(tc.name+"/"+format, func(t *testing.T) {
				startExportTestServer(t, exportTestResponse{body: tc.response})
				dir := t.TempDir()
				maxPages := 0
				if tc.workflow == "log.export" {
					maxPages = 1
				}
				code, env := runExportTestCLI(t, "--output", format, "--output-mode", "file", "--output-dir", dir, "workflow", "exec", tc.workflow, "--input", exportTestInput(tc.query, maxPages))
				state := assertExportTestState(t, code, env, false, tc.reason, 1, 2, tc.resumable)
				artifacts, ok := env["artifacts"].([]any)
				if !ok || len(artifacts) != 1 {
					t.Fatalf("missing partial artifact: %#v", env)
				}
				artifact, _ := artifacts[0].(map[string]any)
				path, _ := artifact["path"].(string)
				metadataPath, _ := artifact["metadataPath"].(string)
				if path == "" || metadataPath == "" || filepath.Dir(path) != dir {
					t.Fatalf("invalid artifact descriptor: %#v", artifact)
				}
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var rows []any
				if format == "json" {
					err = json.Unmarshal(raw, &rows)
				} else {
					for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
						var row any
						if err = json.Unmarshal([]byte(line), &row); err != nil {
							break
						}
						rows = append(rows, row)
					}
				}
				if err != nil || len(rows) != 2 {
					t.Fatalf("invalid partial rows %q: %v", raw, err)
				}
				metaRaw, err := os.ReadFile(metadataPath)
				if err != nil {
					t.Fatal(err)
				}
				var meta map[string]any
				if err := json.Unmarshal(metaRaw, &meta); err != nil || !reflect.DeepEqual(meta["completeness"], state) {
					t.Fatalf("sidecar does not match envelope: %s (%v), state=%#v", metaRaw, err, state)
				}
				if data, ok := env["data"].(map[string]any); !ok || data["itemCount"] != float64(2) {
					t.Fatalf("streamed rows unexpectedly copied into envelope: %#v", env["data"])
				}
			})
		}
	}
}

func TestWorkflowLogExportFileRetainsEarlierPageAfterHTTPFailure(t *testing.T) {
	startExportTestServer(t,
		exportTestResponse{body: `{"Logs":[{"n":1}],"ListOver":false,"Context":"cursor-1"}`},
		exportTestResponse{status: http.StatusInternalServerError, body: `{"errorCode":"InternalError"}`},
	)
	dir := t.TempDir()
	code, env := runExportTestCLI(t, "--output", "jsonl", "--output-mode", "file", "--output-dir", dir, "workflow", "exec", "log.export", "--input", exportTestInput("*", 2))
	state := assertExportTestState(t, code, env, false, "request_failed", 1, 1, true)
	artifacts, _ := env["artifacts"].([]any)
	if len(artifacts) != 1 || env["requestId"] != "request-2" {
		t.Fatalf("partial artifact or current request lost: %#v", env)
	}
	artifact, _ := artifacts[0].(map[string]any)
	path, _ := artifact["path"].(string)
	metadataPath, _ := artifact["metadataPath"].(string)
	raw, err := os.ReadFile(path)
	if err != nil || strings.TrimSpace(string(raw)) != `{"n":1}` {
		t.Fatalf("failed to retain previous rows: %q %v", raw, err)
	}
	metaRaw, err := os.ReadFile(metadataPath)
	var meta map[string]any
	if err != nil || json.Unmarshal(metaRaw, &meta) != nil || !reflect.DeepEqual(meta["completeness"], state) {
		t.Fatalf("partial metadata mismatch: %q %v", metaRaw, err)
	}
}

func TestLogExportDryRunDoesNotClaimCompletion(t *testing.T) {
	for _, workflow := range []string{"log.export", "log.export-analysis"} {
		t.Run(workflow, func(t *testing.T) {
			srv := startExportTestServer(t)
			query := "*"
			if workflow == "log.export-analysis" {
				query = "* | select 1"
			}
			code, env := runExportTestCLI(t, "--dry-run", "workflow", "exec", workflow, "--input", exportTestInput(query, 0))
			summary, _ := env["summary"].(map[string]any)
			if code != 0 || summary["dryRun"] != true || summary["completeness"] != nil || len(srv.captured()) != 0 {
				t.Fatalf("dry run claimed remote completion: exit=%d envelope=%#v", code, env)
			}
			if _, ok := env["data"].(map[string]any); !ok {
				t.Fatalf("dry run lost its request plan: %#v", env)
			}
		})
	}
}

func TestHumanLogExportCompleteness(t *testing.T) {
	if currentEdition() != cliEditionVolclogHuman {
		t.Skip("human shortcut is not enabled")
	}
	startExportTestServer(t, exportTestResponse{body: `{"Logs":[{"n":1}],"ListOver":false,"Context":"cursor-1"}`})
	code, env := runExportTestCLI(t, "log", "export", "--topic-id", "tid", "--query", "*", "--from", "1710374400000", "--to", "1710378000000", "--max-pages", "1")
	assertExportTestState(t, code, env, false, "max_pages", 1, 1, true)
}

func TestLogExportWriterFailureNeverOffersForwardCursor(t *testing.T) {
	dir := t.TempDir()
	writer, err := newStreamedLogFileWriter(filepath.Join(dir, "out.json"), dir, "log", output.FormatJSON)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.abort()
	ctx := &Context{ResponseState: ResponseState{RemoteOutcome: execution.OutcomeSuccess}}
	result := newLogExportResult(ctx, writer)
	result.state.PageCount = 1
	if err := result.writeRows([]any{map[string]any{"n": 1}}); err != nil {
		t.Fatal(err)
	}
	// A non-JSON value simulates an encoding failure after a valid row; the
	// writer can still commit that valid prefix without skipping the failed row.
	writeErr := result.writeRows([]any{make(chan int)})
	if writeErr == nil {
		t.Fatal("expected row encoding failure")
	}
	_, err = result.outputFailed(writeErr)
	var partial *exportResultError
	if !errors.As(err, &partial) || partial.ErrorKind() != "output_failed" || partial.CompletionState().Resumable || partial.CompletionState().Next != nil {
		t.Fatalf("writer failure lost its delivery classification or offered a cursor: %#v", err)
	}
	artifact, ok := partial.FailureArtifact()
	if !ok || artifact.Path == "" || partial.CompletionState().ItemCount != 1 {
		t.Fatalf("valid prefix not preserved: %#v", partial)
	}
}

func TestLogExportCommitFailurePreservesArtifactCompleteness(t *testing.T) {
	for _, tc := range []struct {
		name            string
		metadataFailure bool
	}{
		{name: "rename failure with sidecar"},
		{name: "sidecar failure with continuation", metadataFailure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writer, err := newStreamedLogFileWriter(filepath.Join(dir, "out.json"), dir, "log", output.FormatJSON)
			if err != nil {
				t.Fatal(err)
			}
			defer writer.abort()
			ctx := &Context{ResponseState: ResponseState{RemoteOutcome: execution.OutcomeSuccess}}
			result := newLogExportResult(ctx, writer)
			result.state.PageCount = 1
			if err := result.writeRows([]any{map[string]any{"n": 1}}); err != nil {
				t.Fatal(err)
			}
			var next *execution.Continuation
			if tc.metadataFailure {
				if err := os.Mkdir(writer.tempPath+".meta.json", 0o700); err != nil {
					t.Fatal(err)
				}
				next = logExportContinuation(map[string]any{"TopicId": "tid", "Query": "*"}, "cursor-1")
				_, err = result.fail("max_pages", nil, "", "", next)
			} else {
				writer.rename = func(string, string) error { return errors.New("rename failed") }
				_, err = result.complete("list_over")
			}
			var partial *exportResultError
			if !errors.As(err, &partial) || partial.ErrorKind() != "output_failed" || partial.ErrorCode() != "OutputFailed" {
				t.Fatalf("expected output delivery failure: %v", err)
			}
			file, ok := partial.FailureArtifact()
			if !ok || file.Completeness == nil || !reflect.DeepEqual(file.Completeness, partial.CompletionState()) || !reflect.DeepEqual(file.Completeness, ctx.Completeness) {
				t.Fatalf("artifact and result state differ: artifact=%#v result=%#v context=%#v", file, partial.CompletionState(), ctx.Completeness)
			}
			if _, err := os.ReadFile(file.Path); err != nil {
				t.Fatalf("retained data file is unavailable: %v", err)
			}
			env := buildAPIErrorEnvelope(ctx, "workflow", err, "file")
			summary, _ := env["summary"].(map[string]any)
			artifacts, _ := env["artifacts"].([]map[string]any)
			if len(artifacts) != 1 || !reflect.DeepEqual(summary["completeness"], file.Completeness) || !reflect.DeepEqual(artifacts[0]["completeness"], file.Completeness) {
				t.Fatalf("envelope and artifact completeness differ: %#v", env)
			}
			if tc.metadataFailure {
				if file.MetadataPath != "" || artifacts[0]["metadataPath"] != nil || file.Completeness.StopReason != "max_pages" || !file.Completeness.Resumable || !reflect.DeepEqual(file.Completeness.Next, next) {
					t.Fatalf("sidecar failure lost retained data or its read continuation: %#v", file)
				}
				return
			}
			if file.MetadataPath == "" || file.Completeness.StopReason != "artifact_commit_error" || file.Completeness.Resumable {
				t.Fatalf("commit failure metadata missing or inconsistent: %#v", file)
			}
			metadata, err := os.ReadFile(file.MetadataPath)
			if err != nil {
				t.Fatal(err)
			}
			var decoded struct {
				Completeness *execution.Completeness `json:"completeness"`
			}
			if err := json.Unmarshal(metadata, &decoded); err != nil || !reflect.DeepEqual(decoded.Completeness, file.Completeness) {
				t.Fatalf("sidecar and artifact completeness differ: %s (%v)", metadata, err)
			}
		})
	}
}

func TestLogExportCommitFailureWithoutArtifactDropsUnretainedRows(t *testing.T) {
	dir := t.TempDir()
	writer, err := newStreamedLogFileWriter(filepath.Join(dir, "out.json"), dir, "log", output.FormatJSON)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.abort()
	ctx := &Context{ResponseState: ResponseState{RemoteOutcome: execution.OutcomeSuccess}}
	result := newLogExportResult(ctx, writer)
	result.state.PageCount = 1
	if err := result.writeRows([]any{map[string]any{"n": 1}}); err != nil {
		t.Fatal(err)
	}
	if err := writer.file.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = result.fail("max_pages", nil, "", "", logExportContinuation(map[string]any{"TopicId": "tid"}, "cursor-1"))
	var partial *exportResultError
	if !errors.As(err, &partial) || partial.ErrorKind() != "output_failed" || partial.CompletionState().StopReason != "output_failed" || partial.CompletionState().ItemCount != 0 || partial.CompletionState().Resumable || partial.CompletionState().Next != nil {
		t.Fatalf("unretained rows or unsafe continuation reported: %#v", err)
	}
	if _, ok := partial.FailureArtifact(); ok {
		t.Fatal("unexpected artifact for an unusable file")
	}
}
