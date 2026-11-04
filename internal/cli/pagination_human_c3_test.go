//go:build human

package cli

import (
	"fmt"
	"net/http"
	"testing"
)

func TestHumanCollectionFailureKeepsRowsAndCompleteness(t *testing.T) {
	for _, tc := range []struct{ group, path, field string }{
		{"host-group", "/DescribeHostGroupsV2", "HostGroupHostsRulesInfos"},
		{"collector", "/DescribeRulesV2", "Rules"},
	} {
		t.Run(tc.group, func(t *testing.T) {
			_, requests := outcomeCLIServer(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tc.path {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				if r.URL.Query().Get("PageNumber") == "1" {
					w.Header().Set("X-Tls-Requestid", "first-page")
					_, _ = fmt.Fprintf(w, `{"%s":[{"id":"kept"}],"Total":3}`, tc.field)
					return
				}
				w.Header().Set("X-Tls-Requestid", "failed-page")
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"ErrorCode":"Unavailable"}`))
			})
			code, env := runExportTestCLI(t, tc.group, "list", "--all", "--page-size", "1")
			if code == 0 || requests.Load() != 2 || env["requestId"] != "failed-page" {
				t.Fatalf("exit=%d requests=%d envelope=%#v", code, requests.Load(), env)
			}
			state := exportTestState(t, env)
			data := env["data"].(map[string]any)
			rows, ok := data[tc.field].([]any)
			if !ok || len(rows) != 1 || data["Total"] != float64(3) || state["complete"] != false || state["itemCount"] != float64(1) || state["stopReason"] != "request_failed" {
				t.Fatalf("partial result lost by human rendering: %#v", env)
			}
		})
	}
}
