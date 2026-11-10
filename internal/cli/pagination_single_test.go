package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
)

func TestToolSinglePageCompletenessAndConsumableNext(t *testing.T) {
	queries := make(chan url.Values, 4)
	_, requests := outcomeCLIServer(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		queries <- q
		page := q.Get("PageNumber")
		rows := make([]map[string]any, 20)
		if page == "3" {
			rows = rows[:19]
		} else if page == "4" {
			rows = rows[:0]
		}
		for i := range rows {
			rows[i] = map[string]any{"ProjectId": fmt.Sprintf("p-%s-%d", page, i)}
		}
		w.Header().Set("X-Tls-Requestid", "single-page")
		_ = json.NewEncoder(w).Encode(map[string]any{"Projects": rows, "Total": 59})
	})
	input := `{"ProjectName":"needle","Favourite":true}`
	for page := 1; page <= 4; page++ {
		code, env := runExportTestCLI(t, "tool", "exec", "project.describe-projects", "--input", input)
		if code != 0 || env["status"] != "success" || int(requests.Load()) != page || env["requestId"] != "single-page" {
			t.Fatalf("page=%d exit=%d requests=%d envelope=%#v", page, code, requests.Load(), env)
		}
		summary := env["summary"].(map[string]any)
		state := exportTestState(t, env)
		pagination := summary["pagination"].(map[string]any)
		if state["complete"] != false || state["pageCount"] != float64(1) || pagination["mode"] != "single_page" || pagination["merged"] != false {
			t.Fatalf("single page misreported as a full collection: %#v", summary)
		}
		data := env["data"].(map[string]any)
		wantRows := 20
		if page == 3 {
			wantRows = 19
		} else if page == 4 {
			wantRows = 0
		}
		if data["Total"] != float64(59) || len(data["Projects"].([]any)) != wantRows || summary["itemCount"] != float64(wantRows) {
			t.Fatalf("page data changed: %#v", env)
		}
		q := <-queries
		if q.Get("PageSize") != "" || q.Get("ProjectName") != "needle" || q.Get("Favourite") != "true" {
			t.Fatalf("request defaults or filters changed: %#v", q)
		}
		if page == 4 {
			if state["resumable"] != false || state["next"] != nil || state["stopReason"] != "page_slice" {
				t.Fatalf("empty tail has another continuation: %#v", state)
			}
			continue
		}
		if state["resumable"] != true {
			t.Fatalf("missing continuation: %#v", state)
		}
		next := state["next"].(map[string]any)
		if next["pageAll"] != false || next["operationId"] != "project.describe-projects" {
			t.Fatalf("wrong continuation: %#v", next)
		}
		query := next["input"].(map[string]any)["query"].(map[string]any)
		if query["PageNumber"] != float64(page+1) {
			t.Fatalf("wrong next page: %#v", query)
		}
		if _, injected := query["PageSize"]; injected {
			t.Fatalf("continuation injects page-all size: %#v", query)
		}
		raw, err := json.Marshal(next["input"])
		if err != nil {
			t.Fatal(err)
		}
		input = string(raw)
	}
}

func TestToolSinglePageKnownSizeLastPageIsSlice(t *testing.T) {
	_, requests := outcomeCLIServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"Projects":[{"ProjectId":"last"}],"Total":3}`))
	})
	code, env := runExportTestCLI(t, "tool", "exec", "project.describe-projects", "--input", `{"PageNumber":2,"PageSize":2}`)
	if code != 0 || requests.Load() != 1 {
		t.Fatalf("exit=%d requests=%d envelope=%#v", code, requests.Load(), env)
	}
	state := exportTestState(t, env)
	if state["complete"] != false || state["resumable"] != false || state["stopReason"] != "page_slice" || state["next"] != nil {
		t.Fatalf("tail page reported as entire collection: %#v", state)
	}
}

func TestToolSinglePageCompleteCollection(t *testing.T) {
	_, _ = outcomeCLIServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"Projects":[{"ProjectId":"only"}],"Total":1}`))
	})
	code, env := runExportTestCLI(t, "tool", "exec", "project.describe-projects")
	state := exportTestState(t, env)
	if code != 0 || state["complete"] != true || state["resumable"] != false || state["next"] != nil {
		t.Fatalf("exit=%d state=%#v", code, state)
	}
}

func TestToolSinglePageBadTotalPreservesRows(t *testing.T) {
	_, _ = outcomeCLIServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Tls-Requestid", "bad-total")
		_, _ = w.Write([]byte(`{"Projects":[{"ProjectId":"kept"}],"Total":"invalid"}`))
	})
	code, env := runExportTestCLI(t, "tool", "exec", "project.describe-projects")
	state := exportTestState(t, env)
	data := env["data"].(map[string]any)
	if code == 0 || state["complete"] != false || state["stopReason"] != "invalid_total" || env["requestId"] != "bad-total" || len(data["Projects"].([]any)) != 1 || data["Total"] != "invalid" {
		t.Fatalf("exit=%d envelope=%#v", code, env)
	}
}
