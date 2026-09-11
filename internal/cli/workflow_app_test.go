package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestAppWorkflowsUseCanonicalOperationMetadata(t *testing.T) {
	want := map[string]struct {
		action string
		method string
		path   string
	}{
		appDescribeOperationID:              {action: "DescribeApp", method: "GET", path: "/DescribeApp"},
		appDescribeLogAppOperationID:        {action: "DescribeLogApp", method: "GET", path: "/DescribeLogApp"},
		appDescribeTraceInstanceOperationID: {action: "DescribeTraceInstance", method: "GET", path: "/DescribeTraceInstance"},
	}
	for operationID, expected := range want {
		operation, err := appWorkflowOperation(operationID)
		if err != nil {
			t.Fatalf("resolve %s: %v", operationID, err)
		}
		if operation.Action != expected.action || operation.Wire.Method != expected.method || operation.Wire.Path != expected.path {
			t.Fatalf("%s metadata=(%s %s %s), want=(%s %s %s)", operationID, operation.Action, operation.Wire.Method, operation.Wire.Path, expected.action, expected.method, expected.path)
		}
	}
}

func TestWorkflowExecAppResolveResourcesBuildsDeduplicatedGraph(t *testing.T) {
	calls := make([]string, 0)
	srv := newAppWorkflowTestServer(t, &calls)
	defer srv.Close()
	setWorkflowAppTestEnv(t, srv.URL)

	code, out, stderr := runWorkflowAppTest(t, appResolveResourcesWorkflowID, `{"AppId":"app-1"}`)
	if code != 0 {
		t.Fatalf("unexpected exit=%d envelope=%#v stderr=%q", code, out, stderr)
	}
	data := out["data"].(map[string]any)
	if got, want := data["LogAppIds"], []any{"log-app-1", "log-app-2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("LogAppIds=%#v want=%#v", got, want)
	}
	if got, want := data["TraceInstanceIds"], []any{"trace-1", "trace-2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("TraceInstanceIds=%#v want=%#v", got, want)
	}
	if got, want := data["TopicIds"], []any{"topic-trace-1", "topic-dependency-1", "topic-direct", "topic-metric", "topic-trace-2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("TopicIds=%#v want=%#v", got, want)
	}
	if nodes, ok := data["Nodes"].([]any); !ok || len(nodes) != 10 {
		t.Fatalf("unexpected nodes: %#v", data["Nodes"])
	}
	if edges, ok := data["Edges"].([]any); !ok || len(edges) != 13 {
		t.Fatalf("unexpected edges: %#v", data["Edges"])
	}
	wantCalls := []string{"DescribeApp:app-1", "DescribeLogApp:log-app-1", "DescribeTraceInstance:trace-1", "DescribeLogApp:log-app-2", "DescribeTraceInstance:trace-2"}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("calls=%#v want=%#v", calls, wantCalls)
	}
}

func TestWorkflowExecAppResolveTopicIDsReusesGraphSemantics(t *testing.T) {
	calls := make([]string, 0)
	srv := newAppWorkflowTestServer(t, &calls)
	defer srv.Close()
	setWorkflowAppTestEnv(t, srv.URL)

	code, out, stderr := runWorkflowAppTest(t, appResolveTopicIDsWorkflowID, `{"AppId":"app-1"}`)
	if code != 0 {
		t.Fatalf("unexpected exit=%d envelope=%#v stderr=%q", code, out, stderr)
	}
	data := out["data"].(map[string]any)
	want := []any{"topic-trace-1", "topic-dependency-1", "topic-direct", "topic-metric", "topic-trace-2"}
	if !reflect.DeepEqual(data["TopicIds"], want) || len(data) != 1 {
		t.Fatalf("unexpected topic result: %#v", data)
	}
}

func TestWorkflowResolveResourcesSupportsOpaqueNonLogAppButTopicWorkflowRejectsIt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"AppType":"Dashboard","Region":"cn-beijing","Resources":[{"Id":"dashboard-1"}]}`))
	}))
	defer srv.Close()
	setWorkflowAppTestEnv(t, srv.URL)

	code, out, _ := runWorkflowAppTest(t, appResolveResourcesWorkflowID, `{"AppId":"app-1"}`)
	if code != 0 {
		t.Fatalf("resolve resources exit=%d envelope=%#v", code, out)
	}
	data := out["data"].(map[string]any)
	nodes := data["Nodes"].([]any)
	if len(nodes) != 2 || nodes[1].(map[string]any)["Kind"] != "AppResource" {
		t.Fatalf("unexpected opaque resources: %#v", nodes)
	}

	code, out, _ = runWorkflowAppTest(t, appResolveTopicIDsWorkflowID, `{"AppId":"app-1"}`)
	if code != 1 {
		t.Fatalf("resolve topics exit=%d envelope=%#v", code, out)
	}
	errObj := out["error"].(map[string]any)
	if errObj["kind"] != "unsupported_feature" || !strings.Contains(asStringOrEmpty(errObj["message"]), `AppType "Dashboard"`) {
		t.Fatalf("unexpected error: %#v", errObj)
	}
}

func TestWorkflowExecAppResolveResourcesRejectsCrossRegionAndUnknownResourceType(t *testing.T) {
	tests := []struct {
		name     string
		related  string
		contains string
	}{
		{name: "cross region", related: `{"Region":"cn-shanghai","ResourceType":1,"ResourceID":"topic-1"}`, contains: "cn-shanghai"},
		{name: "unknown type", related: `{"Region":"cn-beijing","ResourceType":3,"ResourceID":"resource-1"}`, contains: "ResourceType 3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/DescribeApp":
					_, _ = w.Write([]byte(`{"AppType":"LogApp","Region":"cn-beijing","Resources":[{"Id":"log-app-1"}]}`))
				case "/DescribeLogApp":
					_, _ = w.Write([]byte(`{"RelatedResourceList":[` + tt.related + `]}`))
				default:
					t.Fatalf("unexpected request path %q", r.URL.Path)
				}
			}))
			defer srv.Close()
			setWorkflowAppTestEnv(t, srv.URL)

			code, out, _ := runWorkflowAppTest(t, appResolveResourcesWorkflowID, `{"AppId":"app-1"}`)
			if code == 0 {
				t.Fatalf("expected failure, envelope=%#v", out)
			}
			errObj := out["error"].(map[string]any)
			if !strings.Contains(asStringOrEmpty(errObj["message"]), tt.contains) {
				t.Fatalf("unexpected error: %#v", errObj)
			}
		})
	}
}

func TestWorkflowExecAppResolversDryRunDoesNotCallServer(t *testing.T) {
	requestCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		http.Error(w, "dry-run must not call server", http.StatusInternalServerError)
	}))
	defer srv.Close()
	setWorkflowAppTestEnv(t, srv.URL)

	for _, workflowID := range []string{appResolveResourcesWorkflowID, appResolveTopicIDsWorkflowID} {
		var stdout, stderr bytes.Buffer
		code := Run([]string{"--dry-run", "workflow", "exec", workflowID, "--input", `{"AppId":"app-1"}`}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("%s exit=%d stdout=%q stderr=%q", workflowID, code, stdout.String(), stderr.String())
		}
		out := decodeWorkflowAppTestEnvelope(t, stdout.Bytes())
		data := out["data"].(map[string]any)
		if data["workflow"] != workflowID || data["type"] != "plan" {
			t.Fatalf("unexpected dry-run plan: %#v", data)
		}
		if steps, ok := data["steps"].([]any); !ok || len(steps) != 3 {
			t.Fatalf("unexpected dry-run steps: %#v", data["steps"])
		}
	}
	if requestCount != 0 {
		t.Fatalf("dry-run sent %d requests", requestCount)
	}
}

func newAppWorkflowTestServer(t *testing.T, calls *[]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-tls-requestid", "req-"+strings.TrimPrefix(r.URL.Path, "/"))
		switch r.URL.Path {
		case "/DescribeApp":
			*calls = append(*calls, "DescribeApp:"+r.URL.Query().Get("AppId"))
			_, _ = w.Write([]byte(`{"AppType":"LogApp","Region":"cn-beijing","Resources":[{"Id":"log-app-1"},{"Id":"log-app-2"},{"Id":"log-app-1"}]}`))
		case "/DescribeLogApp":
			if r.URL.Query().Has("NeedLogAppTopics") {
				t.Errorf("DescribeLogApp must not request NeedLogAppTopics")
			}
			logAppID := r.URL.Query().Get("LogAppId")
			*calls = append(*calls, "DescribeLogApp:"+logAppID)
			switch logAppID {
			case "log-app-1":
				_, _ = w.Write([]byte(`{"LogAppId":"log-app-1","LogAppName":"first","RelatedResourceList":[{"Region":"cn-beijing","ResourceType":0,"ResourceID":"trace-1","ResourceName":"trace-one"},{"Region":"cn-beijing","ResourceType":1,"ResourceID":"topic-direct"},{"Region":"cn-beijing","ResourceType":2,"ResourceID":"topic-metric"},{"Region":"cn-beijing","ResourceType":1,"ResourceID":"topic-trace-1"}]}`))
			case "log-app-2":
				_, _ = w.Write([]byte(`{"LogAppId":"log-app-2","LogAppName":"second","RelatedResourceList":[{"Region":"cn-beijing","ResourceType":0,"ResourceID":"trace-1"},{"Region":"cn-beijing","ResourceType":0,"ResourceID":"trace-2"},{"Region":"cn-beijing","ResourceType":2,"ResourceID":"topic-metric"}]}`))
			default:
				http.Error(w, "unexpected LogAppId", http.StatusBadRequest)
			}
		case "/DescribeTraceInstance":
			traceID := r.URL.Query().Get("TraceInstanceId")
			*calls = append(*calls, "DescribeTraceInstance:"+traceID)
			switch traceID {
			case "trace-1":
				_, _ = w.Write([]byte(`{"TraceTopicId":"topic-trace-1","DependencyTopicId":"topic-dependency-1"}`))
			case "trace-2":
				_, _ = w.Write([]byte(`{"TraceTopicId":"topic-trace-2","DependencyTopicId":"topic-metric"}`))
			default:
				http.Error(w, "unexpected TraceInstanceId", http.StatusBadRequest)
			}
		default:
			http.NotFound(w, r)
		}
	}))
}

func setWorkflowAppTestEnv(t *testing.T, endpoint string) {
	t.Helper()
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", endpoint)
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))
}

func runWorkflowAppTest(t *testing.T, workflowID, input string) (int, map[string]any, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run([]string{"workflow", "exec", workflowID, "--input", input}, &stdout, &stderr)
	return code, decodeWorkflowAppTestEnvelope(t, stdout.Bytes()), stderr.String()
}

func decodeWorkflowAppTestEnvelope(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("invalid JSON: %v raw=%q", err, string(raw))
	}
	return out
}
