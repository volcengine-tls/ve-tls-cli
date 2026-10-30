package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

const filterFileProjectsResponse = `{"Projects":[{"ProjectId":"p1","Big":9007199254740993},{"ProjectId":"p2","Big":9007199254740991}],"Name":"north","Count":9007199254740993,"IsReady":true,"NullValue":null}`

func TestFilteredFileOutputRawValues(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(filterFileProjectsResponse))
	}))
	defer srv.Close()
	setWorkflowAppTestEnv(t, srv.URL)

	cases := []struct {
		name       string
		expression string
		want       string
		format     string
	}{
		{"array_jsonl_preference", "data.Projects[].ProjectId", `["p1","p2"]`, "jsonl"},
		{"string", "data.Name", `"north"`, ""},
		{"number", "data.Count", `9007199254740993`, ""},
		{"boolean", "data.IsReady", `true`, ""},
		{"null", "data.NullValue", `null`, ""},
		{"precise_comparison", "data.Projects[?Big > `9007199254740992`].Big", `[9007199254740993]`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			outDir := t.TempDir()
			args := []string{"--output-mode", "file", "--output-dir", outDir, "--jmes-filter", tc.expression}
			if tc.format != "" {
				args = append(args, "--output", tc.format)
			}
			args = append(args, "raw", "--method", "GET", "--path", "/DescribeProjects")
			raw, path := runFilteredFileOutput(t, args)
			if filepath.Dir(path) != outDir || filepath.Ext(path) != ".json" {
				t.Fatalf("unexpected output path %q", path)
			}
			assertFilteredJSON(t, raw, tc.want)
		})
	}
	if got := requests.Load(); got != int32(len(cases)) {
		t.Fatalf("requests=%d, want %d", got, len(cases))
	}
}

func TestFilteredRawFileOutputDoesNotTrustServerArtifactPath(t *testing.T) {
	victim := filepath.Join(t.TempDir(), "existing.json")
	const original = "existing content must survive\n"
	if err := os.WriteFile(victim, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]any{
			"status":    "success",
			"summary":   map[string]any{"outputMode": "file", "deliveryMode": "file_forced"},
			"artifacts": []map[string]any{{"path": victim, "format": "json"}},
			"data":      map[string]any{"message": "business result"},
			"error":     nil,
		}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer srv.Close()
	setWorkflowAppTestEnv(t, srv.URL)

	outDir := t.TempDir()
	raw, path := runFilteredFileOutput(t, []string{
		"--output-mode", "file", "--output-dir", outDir, "--jmes-filter", "data.data.message",
		"raw", "--method", "GET", "--path", "/DescribeProjects",
	})
	if filepath.Dir(path) != outDir {
		t.Fatalf("output path=%q, want directory %q", path, outDir)
	}
	assertFilteredJSON(t, raw, `"business result"`)
	remaining, err := os.ReadFile(victim)
	if err != nil || string(remaining) != original {
		t.Fatalf("server-specified file changed: content=%q err=%v", remaining, err)
	}
}

func TestFilteredFileOutputToolAndWorkflow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(filterFileProjectsResponse))
	}))
	defer srv.Close()
	setWorkflowAppTestEnv(t, srv.URL)

	t.Run("tool", func(t *testing.T) {
		raw, _ := runFilteredFileOutput(t, []string{
			"--output-mode", "file", "--output-dir", t.TempDir(), "--jmes-filter", "data.Projects[].ProjectId",
			"tool", "exec", "project.describe-projects", "--input", `{}`,
		})
		assertFilteredJSON(t, raw, `["p1","p2"]`)
	})
	t.Run("workflow_dry_run", func(t *testing.T) {
		raw, _ := runFilteredFileOutput(t, []string{
			"--dry-run", "--output-mode", "file", "--output-dir", t.TempDir(), "--jmes-filter", "data.workflow",
			"workflow", "exec", appResolveTopicIDsWorkflowID, "--input", `{"AppId":"app-1"}`,
		})
		assertFilteredJSON(t, raw, `"app.resolve-topic-ids"`)
	})
}

func TestFilteredFileOutputEnvelopeMetadata(t *testing.T) {
	setWorkflowAppTestEnv(t, "https://tls-cn-beijing.volces.com")

	raw, path := runFilteredFileOutput(t, []string{
		"--dry-run", "--output-mode", "file", "--output-dir", t.TempDir(), "--jmes-filter", "@",
		"raw", "--method", "GET", "--path", "/DescribeProjects",
	})
	var env map[string]any
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode full filtered envelope: %v", err)
	}
	if env["status"] != "success" || env["data"] == nil {
		t.Fatalf("expected complete successful envelope, got %#v", env)
	}
	summary := outputRuntimeSummary(t, env)
	if summary["outputMode"] != "file" || summary["deliveryMode"] != "file_forced" {
		t.Fatalf("unexpected delivery summary: %#v", summary)
	}
	artifacts, ok := env["artifacts"].([]any)
	if !ok || len(artifacts) != 1 {
		t.Fatalf("unexpected artifacts: %#v", env["artifacts"])
	}
	artifact, ok := artifacts[0].(map[string]any)
	if !ok || artifact["path"] != path {
		t.Fatalf("artifact=%#v, want path %q", artifacts[0], path)
	}
	if total, ok := summary["totalBytes"].(float64); !ok || total != float64(len(raw)) {
		t.Fatalf("totalBytes=%#v, want full envelope size %d", summary["totalBytes"], len(raw))
	}

	projection, projectionPath := runFilteredFileOutput(t, []string{
		"--dry-run", "--output-mode", "file", "--output-dir", t.TempDir(),
		"--jmes-filter", "[summary.outputMode, summary.deliveryMode, artifacts[0].path, summary.totalBytes]",
		"raw", "--method", "GET", "--path", "/DescribeProjects",
	})
	var values []any
	if err := json.Unmarshal(projection, &values); err != nil {
		t.Fatalf("decode metadata projection: %v", err)
	}
	if len(values) != 4 || values[0] != "file" || values[1] != "file_forced" || values[2] != projectionPath {
		t.Fatalf("unexpected metadata projection: %#v, path=%q", values, projectionPath)
	}
	if total, ok := values[3].(float64); !ok || total <= float64(len(projection)) {
		t.Fatalf("totalBytes=%#v should describe full envelope, projection bytes=%d", values[3], len(projection))
	}
}

func TestFilteredFileOutputContextArtifacts(t *testing.T) {
	setWorkflowAppTestEnv(t, "https://tls-cn-beijing.volces.com")
	cases := []struct {
		name       string
		workflow   bool
		artifact   string
		wantAction string
	}{
		{"tool_true", false, "true", "tool.project.describe-projects"},
		{"tool_path_string", false, "string", "tool.project.describe-projects"},
		{"workflow_true", true, "true", "workflow." + appResolveTopicIDsWorkflowID},
		{"workflow_path_object", true, "object", "workflow." + appResolveTopicIDsWorkflowID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			outDir := filepath.Join(tmp, "out")
			target := filepath.Join(outDir, "selected.json")
			execution := map[string]any{"dry_run": true}
			switch tc.artifact {
			case "true":
				execution["artifact"] = true
				execution["output"] = map[string]any{"dir": outDir}
			case "string":
				execution["artifact"] = target
			case "object":
				execution["artifact"] = map[string]any{"path": target}
			}
			ctxFile := writeFilterFileContext(t, tmp, execution)
			args := []string{"--jmes-filter", "action"}
			args = append(args, filterFileCommand(tc.workflow, ctxFile)...)
			raw, path := runFilteredFileOutput(t, args)
			if tc.artifact != "true" && path != target {
				t.Fatalf("artifact path=%q, want %q", path, target)
			}
			if tc.artifact == "true" && filepath.Dir(path) != outDir {
				t.Fatalf("artifact path=%q, want directory %q", path, outDir)
			}
			want, err := json.Marshal(tc.wantAction)
			if err != nil {
				t.Fatal(err)
			}
			assertFilteredJSON(t, raw, string(want))
		})
	}
}

func TestFilteredFileOutputInvalidExpressionPrecedesAPI(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte(filterFileProjectsResponse))
	}))
	defer srv.Close()
	setWorkflowAppTestEnv(t, srv.URL)

	cases := []struct {
		name string
		args []string
	}{
		{"raw", []string{"raw", "--method", "GET", "--path", "/DescribeProjects"}},
		{"tool", filterFileCommand(false, "")},
		{"workflow", filterFileCommand(true, "")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			outDir := t.TempDir()
			args := []string{"--output-mode", "file", "--output-dir", outDir, "--jmes-filter", "???"}
			args = append(args, tc.args...)
			var stdout, stderr bytes.Buffer
			code := Run(args, &stdout, &stderr)
			assertFilterFileError(t, code, stdout.String(), stderr.String(), 3, "decode", "invalid jmes-filter expression")
			assertFilterFileDirEmpty(t, outDir)
		})
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("invalid expression sent %d requests", got)
	}
}

func TestFilteredFileOutputMissDoesNotCreateOrOverwrite(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(filterFileProjectsResponse))
	}))
	defer srv.Close()
	setWorkflowAppTestEnv(t, srv.URL)

	t.Run("raw_new_file", func(t *testing.T) {
		outDir := t.TempDir()
		var stdout, stderr bytes.Buffer
		code := Run([]string{
			"--output-mode", "file", "--output-dir", outDir, "--jmes-filter", "data.missing_field",
			"raw", "--method", "GET", "--path", "/DescribeProjects",
		}, &stdout, &stderr)
		assertFilterFileError(t, code, stdout.String(), stderr.String(), 3, "decode", "matched no value")
		assertFilterFileDirEmpty(t, outDir)
	})
	for _, workflow := range []bool{false, true} {
		for _, exists := range []bool{false, true} {
			name := fmt.Sprintf("workflow=%t/existing=%t", workflow, exists)
			t.Run(name, func(t *testing.T) {
				tmp := t.TempDir()
				target := filepath.Join(tmp, "selected.json")
				const original = "existing data must survive\n"
				if exists {
					if err := os.WriteFile(target, []byte(original), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				ctxFile := writeFilterFileContext(t, tmp, map[string]any{"dry_run": true, "artifact": target})
				args := []string{"--jmes-filter", "data.missing_field"}
				args = append(args, filterFileCommand(workflow, ctxFile)...)
				var stdout, stderr bytes.Buffer
				code := Run(args, &stdout, &stderr)
				assertFilterFileError(t, code, stdout.String(), stderr.String(), 3, "decode", "matched no value")
				raw, err := os.ReadFile(target)
				if exists {
					if err != nil || string(raw) != original {
						t.Fatalf("existing artifact changed: bytes=%q err=%v", raw, err)
					}
				} else if !os.IsNotExist(err) {
					t.Fatalf("missing artifact was created: bytes=%q err=%v", raw, err)
				}
			})
		}
	}
}

func TestFilteredFileOutputServerFailureKeepsEnvelope(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		command  string
		artifact bool
	}{
		{"raw_409", http.StatusConflict, "raw", false},
		{"raw_500", http.StatusInternalServerError, "raw", false},
		{"tool_409_context_artifact", http.StatusConflict, "tool", true},
		{"tool_500", http.StatusInternalServerError, "tool", false},
		{"workflow_409", http.StatusConflict, "workflow", false},
		{"workflow_500_context_artifact", http.StatusInternalServerError, "workflow", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("x-tls-requestid", "req-filter-file-error")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"ErrorCode":"FixtureFailure","ErrorMessage":"upstream failed"}`))
			}))
			defer srv.Close()
			setWorkflowAppTestEnv(t, srv.URL)
			tmp := t.TempDir()
			outDir := filepath.Join(tmp, "out")
			if err := os.Mkdir(outDir, 0o700); err != nil {
				t.Fatal(err)
			}
			args := []string{"--jmes-filter", "data"}
			ctxFile := ""
			if tc.artifact {
				ctxFile = writeFilterFileContext(t, tmp, map[string]any{"artifact": filepath.Join(outDir, "selected.json")})
			} else {
				args = append(args, "--output-mode", "file", "--output-dir", outDir)
			}
			switch tc.command {
			case "raw":
				args = append(args, "raw", "--method", "GET", "--path", "/DescribeProjects")
			case "tool":
				args = append(args, filterFileCommand(false, ctxFile)...)
			case "workflow":
				args = append(args, filterFileCommand(true, ctxFile)...)
			}
			var stdout, stderr bytes.Buffer
			code := Run(args, &stdout, &stderr)
			if code == 0 || stderr.Len() != 0 {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			var env map[string]any
			if err := json.Unmarshal(stdout.Bytes(), &env); err != nil || env["status"] != "failed" {
				t.Fatalf("expected complete failed envelope, got %q err=%v", stdout.String(), err)
			}
			errObj, ok := env["error"].(map[string]any)
			if !ok || errObj["code"] != "FixtureFailure" || errObj["statusCode"] != float64(tc.status) {
				t.Fatalf("upstream error lost: %#v", env["error"])
			}
			if requests.Load() == 0 {
				t.Fatal("expected at least one upstream request")
			}
			assertFilterFileDirEmpty(t, outDir)
		})
	}
}

func TestWorkflowFilterDisablesAutoFile(t *testing.T) {
	setWorkflowAppTestEnv(t, "https://tls-cn-beijing.volces.com")
	previousLimit := toolExecAutoArtifactByteLimit
	toolExecAutoArtifactByteLimit = 1
	defer func() { toolExecAutoArtifactByteLimit = previousLimit }()

	outDir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"--dry-run", "--output-dir", outDir, "--jmes-filter", "summary.deliveryMode",
		"workflow", "exec", appResolveTopicIDsWorkflowID, "--input", `{"AppId":"app-1"}`,
	}, &stdout, &stderr)
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var delivery string
	if err := json.Unmarshal(stdout.Bytes(), &delivery); err != nil || delivery != "stdout" {
		t.Fatalf("delivery=%q err=%v stdout=%q", delivery, err, stdout.String())
	}
	assertFilterFileDirEmpty(t, outDir)
}

func TestFilteredFileOutputWorkflowExports(t *testing.T) {
	cases := []struct {
		name      string
		workflow  string
		input     string
		responses []string
		want      string
	}{
		{
			name:      "log_export_paged",
			workflow:  "log.export",
			input:     `{"TopicId":"tid","Query":"*","StartTime":1710374400000,"EndTime":1710378000000,"MaxPages":2}`,
			responses: []string{`{"Logs":[{"k":"a"},{"k":"b"}],"ListOver":false,"Context":"ctx-2"}`, `{"Logs":[{"k":"c"}],"ListOver":true,"Context":""}`},
			want:      `[{"k":"b"},{"k":"c"}]`,
		},
		{
			name:      "log_export_analysis",
			workflow:  "log.export-analysis",
			input:     `{"TopicId":"tid","Query":"*|select 1","StartTime":1710374400000,"EndTime":1710378000000}`,
			responses: []string{`{"Analysis":true,"AnalysisResult":{"Schema":["k","v"],"Data":[{"k":"a","v":1},{"k":"b","v":2}],"Type":{}},"ListOver":true,"Context":""}`},
			want:      `[{"k":"b","v":2}]`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			outDir := t.TempDir()
			var requests atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/SearchLogs" {
					t.Errorf("unexpected request path %q", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
					return
				}
				entries, err := os.ReadDir(outDir)
				if err != nil || len(entries) != 0 {
					t.Errorf("unfiltered output appeared during request: entries=%v err=%v", entries, err)
				}
				index := int(requests.Add(1)) - 1
				if index >= len(tc.responses) {
					t.Errorf("unexpected extra SearchLogs request %d", index+1)
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.responses[index]))
			}))
			defer srv.Close()
			setWorkflowAppTestEnv(t, srv.URL)

			raw, path := runFilteredFileOutput(t, []string{
				"--output", "jsonl", "--output-mode", "file", "--output-dir", outDir,
				"--jmes-filter", "data[?k != 'a']",
				"workflow", "exec", tc.workflow, "--input", tc.input,
			})
			assertFilteredJSON(t, raw, tc.want)
			if got := requests.Load(); got != int32(len(tc.responses)) {
				t.Fatalf("requests=%d, want %d", got, len(tc.responses))
			}
			entries, err := os.ReadDir(outDir)
			if err != nil || len(entries) != 1 || filepath.Join(outDir, entries[0].Name()) != path {
				t.Fatalf("expected only filtered output %q: entries=%v err=%v", path, entries, err)
			}
		})
	}
}

func TestHumanShortcutFilterFileKeepsEnvelope(t *testing.T) {
	if currentEdition() != cliEditionVolclogHuman {
		t.Skip("human shortcut is available only in the human edition")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(filterFileProjectsResponse))
	}))
	defer srv.Close()
	setWorkflowAppTestEnv(t, srv.URL)

	outFile := filepath.Join(t.TempDir(), "project-list.json")
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"--output-mode", "file", "--output-file", outFile, "--jmes-filter", "Projects[].ProjectId",
		"project", "list",
	}, &stdout, &stderr)
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var env map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("invalid stdout envelope: %v stdout=%q", err, stdout.String())
	}
	if env["status"] != "success" || env["action"] != "project.list" {
		t.Fatalf("unexpected stdout envelope: %#v", env)
	}
	data, err := json.Marshal(env["data"])
	if err != nil {
		t.Fatalf("marshal filtered data: %v", err)
	}
	assertFilteredJSON(t, data, `["p1","p2"]`)
	artifacts, ok := env["artifacts"].([]any)
	if !ok || len(artifacts) != 1 {
		t.Fatalf("unexpected artifacts: %#v", env["artifacts"])
	}
	artifact, ok := artifacts[0].(map[string]any)
	if !ok || artifact["path"] != outFile {
		t.Fatalf("unexpected artifact: %#v", artifacts[0])
	}
	raw, err := os.ReadFile(outFile)
	if err != nil || !bytes.Equal(raw, stdout.Bytes()) {
		t.Fatalf("file and stdout envelopes differ: file=%q stdout=%q err=%v", raw, stdout.String(), err)
	}
}

func runFilteredFileOutput(t *testing.T, args []string) ([]byte, string) {
	t.Helper()
	notice, path := runOutputRuntimeNotice(t, args)
	if want := "结果已写入文件。\n文件: " + path + "\n"; notice != want {
		t.Fatalf("notice=%q, want %q", notice, want)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read filtered output %q: %v", path, err)
	}
	if !json.Valid(raw) {
		t.Fatalf("expected one JSON document, got %q", raw)
	}
	return raw, path
}

func assertFilteredJSON(t *testing.T, raw []byte, want string) {
	t.Helper()
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		t.Fatalf("compact result: %v", err)
	}
	if compact.String() != want {
		t.Fatalf("filtered JSON=%s, want %s", compact.String(), want)
	}
}

func writeFilterFileContext(t *testing.T, dir string, execution map[string]any) string {
	t.Helper()
	path := filepath.Join(dir, "context.json")
	if err := osWriteJSON(path, map[string]any{"execution": execution}); err != nil {
		t.Fatalf("write execution context: %v", err)
	}
	return path
}

func filterFileCommand(workflow bool, contextFile string) []string {
	args := []string{"tool", "exec", "project.describe-projects", "--input", `{}`}
	if workflow {
		args = []string{"workflow", "exec", appResolveTopicIDsWorkflowID, "--input", `{"AppId":"app-1"}`}
	}
	if contextFile != "" {
		args = append(args, "--context", "file://"+contextFile)
	}
	return args
}

func assertFilterFileError(t *testing.T, code int, stdout, stderr string, wantCode int, kind, message string) {
	t.Helper()
	if code != wantCode || stderr != "" {
		t.Fatalf("exit=%d want=%d stdout=%q stderr=%q", code, wantCode, stdout, stderr)
	}
	var env map[string]any
	if err := json.Unmarshal([]byte(stdout), &env); err != nil || env["status"] != "failed" {
		t.Fatalf("expected error envelope: stdout=%q err=%v", stdout, err)
	}
	errObj, ok := env["error"].(map[string]any)
	if !ok || errObj["kind"] != kind || !strings.Contains(asStringOrEmpty(errObj["message"]), message) {
		t.Fatalf("unexpected error: %#v", env["error"])
	}
}

func assertFilterFileDirEmpty(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read output directory: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("unexpected output files: %v", entries)
	}
}
