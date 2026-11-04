package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const paginationDeliveryC3Operation = "project.describe-projects"

func paginationDeliveryC3MaxPages(t *testing.T, limit int) {
	t.Helper()
	if _, err := loadOperationCatalog(); err != nil {
		t.Fatalf("load operation catalog: %v", err)
	}
	for index := range cachedCatalog.Operations {
		operation := &cachedCatalog.Operations[index]
		if string(operation.ID) != paginationDeliveryC3Operation {
			continue
		}
		if operation.Pagination == nil {
			t.Fatalf("missing pagination metadata for %s", operation.ID)
		}
		original := operation.Pagination
		modified := *original
		modified.MaxPages = limit
		operation.Pagination = &modified
		t.Cleanup(func() { operation.Pagination = original })
		return
	}
	t.Fatalf("catalog missing %s", paginationDeliveryC3Operation)
}

func paginationDeliveryC3Data(t *testing.T, env map[string]any, total float64, ids ...string) {
	t.Helper()
	data, ok := env["data"].(map[string]any)
	if !ok || data["Total"] != total {
		t.Fatalf("missing original total: %#v", env["data"])
	}
	rows, ok := data["Projects"].([]any)
	if !ok || len(rows) != len(ids) {
		t.Fatalf("projects=%#v want %d rows", data["Projects"], len(ids))
	}
	for i, id := range ids {
		row, ok := rows[i].(map[string]any)
		if !ok || row["ProjectId"] != id {
			t.Fatalf("row %d = %#v, want ProjectId %q", i, rows[i], id)
		}
	}
}

func paginationDeliveryC3Pagination(t *testing.T, env map[string]any, reason string, pages, items int) map[string]any {
	t.Helper()
	summary, ok := env["summary"].(map[string]any)
	if !ok {
		t.Fatalf("missing summary: %#v", env)
	}
	pagination, ok := summary["pagination"].(map[string]any)
	if !ok || pagination["merged"] != true || pagination["complete"] != false ||
		pagination["stopReason"] != reason || pagination["pageCount"] != float64(pages) || pagination["itemCount"] != float64(items) {
		t.Fatalf("unexpected pagination: %#v", summary["pagination"])
	}
	state := exportTestState(t, env)
	if state["complete"] != false || state["stopReason"] != reason ||
		state["pageCount"] != float64(pages) || state["itemCount"] != float64(items) {
		t.Fatalf("unexpected completeness: %#v", state)
	}
	return state
}

func paginationDeliveryC3Next(t *testing.T, state map[string]any, page, size float64) map[string]any {
	t.Helper()
	next, ok := state["next"].(map[string]any)
	if !ok || state["resumable"] != true || next["operationId"] != paginationDeliveryC3Operation || next["pageAll"] != false {
		t.Fatalf("unexpected continuation: %#v", state)
	}
	input, ok := next["input"].(map[string]any)
	if !ok {
		t.Fatalf("missing continuation input: %#v", next)
	}
	query, ok := input["query"].(map[string]any)
	if !ok || query["PageNumber"] != page || query["PageSize"] != size || query["ProjectName"] != "needle" || query["Favourite"] != true {
		t.Fatalf("unexpected continuation query: %#v", input)
	}
	return next
}

func TestToolPaginationC3MaxPagesAndConsumableContinuation(t *testing.T) {
	paginationDeliveryC3MaxPages(t, 1)
	queries := make(chan url.Values, 4)
	_, requests := outcomeCLIServer(t, func(w http.ResponseWriter, r *http.Request) {
		queries <- r.URL.Query()
		if r.Method != http.MethodGet || r.URL.Path != "/DescribeProjects" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		switch r.URL.Query().Get("PageNumber") {
		case "1":
			w.Header().Set("X-Tls-Requestid", "request-first")
			_, _ = w.Write([]byte(`{"Projects":[{"ProjectId":"p1"}],"Total":3}`))
		case "2":
			w.Header().Set("X-Tls-Requestid", "request-next")
			_, _ = w.Write([]byte(`{"Projects":[{"ProjectId":"p2"}],"Total":3}`))
		default:
			http.Error(w, "unexpected page", http.StatusBadRequest)
		}
	})
	code, env := runExportTestCLI(t, "tool", "exec", paginationDeliveryC3Operation, "--page-all", "--input", `{"query":{"PageSize":1,"ProjectName":"needle","Favourite":true}}`)
	if code == 0 || env["status"] != "failed" || requests.Load() != 1 {
		t.Fatalf("exit=%d requests=%d envelope=%#v", code, requests.Load(), env)
	}
	paginationDeliveryC3Data(t, env, 3, "p1")
	state := paginationDeliveryC3Pagination(t, env, "max_pages", 1, 1)
	next := paginationDeliveryC3Next(t, state, 2, 1)
	firstQuery := <-queries
	input, err := json.Marshal(next["input"])
	if err != nil {
		t.Fatal(err)
	}
	code, resumed := runExportTestCLI(t, "tool", "exec", next["operationId"].(string), "--input", string(input))
	if code != 0 || resumed["status"] != "success" || requests.Load() != 2 {
		t.Fatalf("resume exit=%d requests=%d envelope=%#v", code, requests.Load(), resumed)
	}
	paginationDeliveryC3Data(t, resumed, 3, "p2")
	secondQuery := <-queries
	for _, query := range []url.Values{firstQuery, secondQuery} {
		if query.Get("PageSize") != "1" || query.Get("ProjectName") != "needle" || query.Get("Favourite") != "true" {
			t.Fatalf("filters lost across continuation: %#v", query)
		}
	}
	if firstQuery.Get("PageNumber") != "1" || secondQuery.Get("PageNumber") != "2" {
		t.Fatalf("page numbers: first=%#v second=%#v", firstQuery, secondQuery)
	}
}

func TestToolPaginationC3SecondPageFailure(t *testing.T) {
	for _, failure := range []string{"transport", "http", "decode"} {
		t.Run(failure, func(t *testing.T) {
			_, requests := outcomeCLIServer(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("PageNumber") == "1" {
					w.Header().Set("X-Tls-Requestid", "request-first")
					_, _ = w.Write([]byte(`{"Projects":[{"ProjectId":"p1"}],"Total":3}`))
					return
				}
				w.Header().Set("X-Tls-Requestid", "request-current")
				switch failure {
				case "transport":
					w.Header().Set("Content-Length", "100")
					_, _ = w.Write([]byte(`{"Projects":`))
				case "http":
					w.WriteHeader(http.StatusServiceUnavailable)
					_, _ = w.Write([]byte(`{"ErrorCode":"ServiceUnavailable"}`))
				case "decode":
					_, _ = w.Write([]byte(`<html>invalid response</html>`))
				}
			})
			code, env := runExportTestCLI(t, "tool", "exec", paginationDeliveryC3Operation, "--page-all", "--input", `{"query":{"PageSize":1,"ProjectName":"needle","Favourite":true}}`)
			if code == 0 || env["requestId"] != "request-current" || requests.Load() != 2 {
				t.Fatalf("exit=%d requests=%d envelope=%#v", code, requests.Load(), env)
			}
			paginationDeliveryC3Data(t, env, 3, "p1")
			state := paginationDeliveryC3Pagination(t, env, "request_failed", 1, 1)
			paginationDeliveryC3Next(t, state, 2, 1)
			failureData, _ := env["error"].(map[string]any)
			if failureData["requestId"] != "request-current" {
				t.Fatalf("failure lost current request metadata: %#v", env)
			}
		})
	}
}

func paginationDeliveryC3FileArgs(t *testing.T, dir, completePath string) []string {
	t.Helper()
	contextData, err := json.Marshal(map[string]any{"execution": map[string]any{"artifact": map[string]any{"path": completePath, "dir": dir}}})
	if err != nil {
		t.Fatal(err)
	}
	return []string{"tool", "exec", paginationDeliveryC3Operation, "--page-all", "--context", string(contextData), "--input", `{"query":{"PageSize":2,"ProjectName":"needle","Favourite":true}}`}
}

func paginationDeliveryC3Artifact(t *testing.T, env, state map[string]any, dir, completePath string) map[string]any {
	t.Helper()
	artifacts, ok := env["artifacts"].([]any)
	if !ok || len(artifacts) != 1 {
		t.Fatalf("missing artifact: %#v", env)
	}
	artifact, ok := artifacts[0].(map[string]any)
	if !ok || artifact["complete"] != false || !reflect.DeepEqual(artifact["completeness"], state) {
		t.Fatalf("artifact completeness mismatch: %#v", artifacts[0])
	}
	path, ok := artifact["path"].(string)
	if !ok || path == "" || path == completePath || filepath.Dir(path) != dir {
		t.Fatalf("unexpected partial path: %#v", artifact)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	stored := outcomeCLIDecode(t, data)
	storedState := exportTestState(t, stored)
	if stored["status"] != "failed" || !reflect.DeepEqual(storedState, state) {
		t.Fatalf("invalid partial envelope: %#v", stored)
	}
	paginationDeliveryC3Data(t, stored, 5, "p1", "p2")
	completeData, err := os.ReadFile(completePath)
	if err != nil || string(completeData) != "existing complete result" {
		t.Fatalf("complete output was overwritten: %q err=%v", completeData, err)
	}
	return stored
}

func TestToolPaginationC3PartialFilePreservesCompletePath(t *testing.T) {
	paginationDeliveryC3MaxPages(t, 1)
	_, requests := outcomeCLIServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Tls-Requestid", "request-first")
		_, _ = w.Write([]byte(`{"Projects":[{"ProjectId":"p1"},{"ProjectId":"p2"}],"Total":5}`))
	})
	dir := t.TempDir()
	completePath := filepath.Join(dir, "complete.json")
	if err := os.WriteFile(completePath, []byte("existing complete result"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, env := runExportTestCLI(t, paginationDeliveryC3FileArgs(t, dir, completePath)...)
	if code == 0 || requests.Load() != 1 {
		t.Fatalf("exit=%d requests=%d envelope=%#v", code, requests.Load(), env)
	}
	state := paginationDeliveryC3Pagination(t, env, "max_pages", 1, 2)
	paginationDeliveryC3Next(t, state, 2, 2)
	paginationDeliveryC3Artifact(t, env, state, dir, completePath)
}

func TestToolPaginationC3PartialFileSurvivesShortStdout(t *testing.T) {
	paginationDeliveryC3MaxPages(t, 1)
	_, requests := outcomeCLIServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Tls-Requestid", "request-first")
		_, _ = w.Write([]byte(`{"Projects":[{"ProjectId":"p1"},{"ProjectId":"p2"}],"Total":5}`))
	})
	dir := t.TempDir()
	completePath := filepath.Join(dir, "complete.json")
	if err := os.WriteFile(completePath, []byte("existing complete result"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	code := Run(paginationDeliveryC3FileArgs(t, dir, completePath), outcomeCLIFailingWriter{short: true}, &stderr)
	if code != 3 || requests.Load() != 1 {
		t.Fatalf("exit=%d requests=%d stderr=%q", code, requests.Load(), stderr.String())
	}
	env := outcomeCLIDecode(t, stderr.Bytes())
	state := paginationDeliveryC3Pagination(t, env, "max_pages", 1, 2)
	paginationDeliveryC3Next(t, state, 2, 2)
	failure, _ := env["error"].(map[string]any)
	summary, _ := env["summary"].(map[string]any)
	if failure["kind"] != "output_failed" || summary["deliveryOutcome"] != "failed" || summary["remoteOutcome"] != "partial" {
		t.Fatalf("short write lost delivery status: %#v", env)
	}
	paginationDeliveryC3Artifact(t, env, state, dir, completePath)
}
