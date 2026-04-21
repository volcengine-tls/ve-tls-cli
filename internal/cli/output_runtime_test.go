package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestOutputRuntimeStdoutEnvelopeDeliveryMode(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	env := runOutputRuntimeEnvelope(t, []string{
		"--dry-run",
		"raw",
		"--method", "GET",
		"--path", "/DescribeProjects",
	})

	summary := outputRuntimeSummary(t, env)
	if got := summary["deliveryMode"]; got != "stdout" {
		t.Fatalf("expected deliveryMode=stdout, got %#v", got)
	}
	if _, ok := summary["totalBytes"].(float64); !ok {
		t.Fatalf("expected totalBytes number, got %#v", summary["totalBytes"])
	}
	if _, ok := summary["itemCount"].(float64); !ok {
		t.Fatalf("expected itemCount number, got %#v", summary["itemCount"])
	}
}

func TestOutputRuntimeForcedFileWritesFullEnvelope(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	outDir := t.TempDir()
	notice, outFile := runOutputRuntimeNotice(t, []string{
		"--dry-run",
		"--output-mode", "file",
		"--output-dir", outDir,
		"raw",
		"--method", "GET",
		"--path", "/DescribeProjects",
	})
	if want := "结果已写入文件。"; !strings.Contains(notice, want) {
		t.Fatalf("expected forced-file notice %q, got %q", want, notice)
	}
	fileEnv := outputRuntimeReadEnvelopeFile(t, outFile)

	fileSummary := outputRuntimeSummary(t, fileEnv)
	if got := fileSummary["deliveryMode"]; got != "file_forced" {
		t.Fatalf("expected file deliveryMode=file_forced, got %#v", got)
	}
	if _, ok := fileEnv["data"].(map[string]any); !ok {
		t.Fatalf("expected file envelope to keep full data, got %#v", fileEnv["data"])
	}
}

func TestOutputRuntimeAutoFileWritesFullEnvelope(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	prevLimit := toolExecAutoArtifactByteLimit
	toolExecAutoArtifactByteLimit = 400
	defer func() { toolExecAutoArtifactByteLimit = prevLimit }()

	tmp := t.TempDir()
	ctxFile := filepath.Join(tmp, "ctx.json")
	reqFile := filepath.Join(tmp, "req.json")
	outDir := filepath.Join(tmp, "out")
	if err := osWriteJSON(ctxFile, map[string]any{
		"region": "cn-beijing",
		"execution": map[string]any{
			"dry_run": true,
			"output": map[string]any{
				"dir": outDir,
			},
		},
	}); err != nil {
		t.Fatalf("write context: %v", err)
	}
	if err := osWriteJSON(reqFile, map[string]any{
		"TopicId":   "t",
		"Query":     "*",
		"StartTime": 1710374400000,
		"EndTime":   1710378000000,
	}); err != nil {
		t.Fatalf("write request: %v", err)
	}

	notice, artifactPath := runOutputRuntimeNotice(t, []string{
		"tool", "exec", "log.search-logs",
		"--context", "file://" + ctxFile,
		"--input", "file://" + reqFile,
	})
	if want := "结果过大，已写入文件。"; !strings.Contains(notice, want) {
		t.Fatalf("expected auto-file notice %q, got %q", want, notice)
	}
	fileEnv := outputRuntimeReadEnvelopeFile(t, artifactPath)
	fileSummary := outputRuntimeSummary(t, fileEnv)
	if got := fileSummary["deliveryMode"]; got != "file_auto" {
		t.Fatalf("expected file deliveryMode=file_auto, got %#v", got)
	}
	data, ok := fileEnv["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected full file data map, got %#v", fileEnv["data"])
	}
	if _, ok := data["omitted"]; ok {
		t.Fatalf("expected file envelope to keep full data instead of preview, got %#v", data)
	}
}

func TestOutputRuntimeFileEnvelopeMatchesStdoutSchema(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	stdoutEnv := runOutputRuntimeEnvelope(t, []string{
		"--dry-run",
		"raw",
		"--method", "GET",
		"--path", "/DescribeProjects",
	})

	outDir := t.TempDir()
	_, outFile := runOutputRuntimeNotice(t, []string{
		"--dry-run",
		"--output-mode", "file",
		"--output-dir", outDir,
		"raw",
		"--method", "GET",
		"--path", "/DescribeProjects",
	})
	fileEnv := outputRuntimeReadEnvelopeFile(t, outFile)

	if got, want := outputRuntimeMapKeys(stdoutEnv), outputRuntimeMapKeys(fileEnv); !equalStringSlices(got, want) {
		t.Fatalf("stdout/file envelope schema mismatch: stdout=%v file=%v", got, want)
	}
	stdoutSummary := outputRuntimeSummary(t, stdoutEnv)
	fileSummary := outputRuntimeSummary(t, fileEnv)
	if got, want := outputRuntimeMapKeys(stdoutSummary), outputRuntimeMapKeys(fileSummary); !equalStringSlices(got, want) {
		t.Fatalf("stdout/file summary schema mismatch: stdout=%v file=%v", got, want)
	}
}

func TestOutputRuntimePreflightRejectsMissingWritableDirForKnownFileDelivery(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("x-tls-requestid", "req-project-list")
		_, _ = w.Write([]byte(`{"Projects":[],"Total":0}`))
	}))
	defer srv.Close()

	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", srv.URL)
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"--output-mode", "file",
		"raw",
		"--method", "GET",
		"--path", "/DescribeProjects",
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected failure stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if requests != 0 {
		t.Fatalf("expected preflight to fail before request, got requests=%d", requests)
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v stdout=%q", err, stdout.String())
	}
	errObj, ok := out["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object, got %#v", out["error"])
	}
	if got := errObj["kind"]; got != "filesystem" {
		t.Fatalf("expected filesystem kind, got %#v", got)
	}
}

func TestToolWorkflowRawRejectOutputFile(t *testing.T) {
	cases := [][]string{
		{"tool", "describe", "project.create", "--output-file", filepath.Join(t.TempDir(), "tool.json")},
		{"workflow", "describe", "log.export", "--output-file", filepath.Join(t.TempDir(), "workflow.json")},
		{"raw", "--method", "GET", "--path", "/DescribeProjects", "--output-file", filepath.Join(t.TempDir(), "raw.json")},
	}
	for _, args := range cases {
		var stdout, stderr bytes.Buffer
		code := Run(args, &stdout, &stderr)
		if code == 0 {
			t.Fatalf("expected output-file rejection for args=%v stdout=%q stderr=%q", args, stdout.String(), stderr.String())
		}
	}
}

func TestToolExecPreflightOutputDirFailureUsesEnvelope(t *testing.T) {
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("write blocker: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"--output-mode", "file",
		"--output-dir", filepath.Join(blocker, "child"),
		"tool", "exec", "project.describe-projects",
		"--input", `{}`,
	}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("expected filesystem exit=2, got %d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v stdout=%q", err, stdout.String())
	}
	if out["status"] != "failed" {
		t.Fatalf("expected failed status, got %#v", out["status"])
	}
	errObj, ok := out["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object, got %#v", out["error"])
	}
	if errObj["kind"] != "filesystem" {
		t.Fatalf("expected filesystem kind, got %#v", errObj["kind"])
	}
}

func TestOutputRuntimeJMESFilterTargetsEnvelope(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"--dry-run",
		"--jmes-filter", "summary.deliveryMode",
		"raw",
		"--method", "GET",
		"--path", "/DescribeProjects",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	var delivery string
	if err := json.Unmarshal(stdout.Bytes(), &delivery); err != nil {
		t.Fatalf("invalid filtered stdout json: %v stdout=%q", err, stdout.String())
	}
	if delivery != "stdout" {
		t.Fatalf("expected summary.deliveryMode filter result, got %q", delivery)
	}
}

func TestOutputRuntimeJMESFilterExistingNullEnvelopeFieldReturnsNull(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"--dry-run",
		"--jmes-filter", "error",
		"raw",
		"--method", "GET",
		"--path", "/DescribeProjects",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != "null" {
		t.Fatalf("expected filtered null stdout, got %q", got)
	}
}

func TestToolExecValidationFailureUsesEnvelopeAndSupportsEnvelopeFilter(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"--jmes-filter", "error.kind",
		"tool", "exec", "project.describe-project",
		"--input", `{}`,
	}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected validation exit=1, got %d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	var kind string
	if err := json.Unmarshal(stdout.Bytes(), &kind); err != nil {
		t.Fatalf("invalid filtered stdout json: %v stdout=%q", err, stdout.String())
	}
	if kind != "validation" {
		t.Fatalf("expected validation kind, got %q", kind)
	}
}

func TestWorkflowExecValidationFailureUsesEnvelopeAndSupportsEnvelopeFilter(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"--jmes-filter", "error.kind",
		"workflow", "exec", "log.export",
		"--input", `{}`,
	}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected validation exit=1, got %d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	var kind string
	if err := json.Unmarshal(stdout.Bytes(), &kind); err != nil {
		t.Fatalf("invalid filtered stdout json: %v stdout=%q", err, stdout.String())
	}
	if kind != "validation" {
		t.Fatalf("expected validation kind, got %q", kind)
	}
}

func TestToolExecTopLevelJMESFilterSyntaxErrorUsesEnvelope(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"--dry-run",
		"--jmes-filter", "???",
		"tool", "exec", "project.describe-projects",
		"--input", `{}`,
	}, &stdout, &stderr)
	if code != 3 {
		t.Fatalf("expected decode exit=3, got %d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v stdout=%q", err, stdout.String())
	}
	if out["status"] != "failed" {
		t.Fatalf("expected failed status, got %#v", out["status"])
	}
	errObj, ok := out["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object, got %#v", out["error"])
	}
	if got := errObj["kind"]; got != "decode" {
		t.Fatalf("expected decode kind, got %#v", got)
	}
	if !strings.Contains(asStringOrEmpty(errObj["message"]), "invalid jmes-filter expression") {
		t.Fatalf("unexpected error message: %#v", errObj["message"])
	}
}

func TestWorkflowExecTopLevelJMESFilterMissUsesEnvelope(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"--dry-run",
		"--jmes-filter", "missing.path",
		"workflow", "exec", "log.export",
		"--input", `{"TopicId":"tid","Query":"*","StartTime":1,"EndTime":2,"MaxPages":1}`,
	}, &stdout, &stderr)
	if code != 3 {
		t.Fatalf("expected decode exit=3, got %d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v stdout=%q", err, stdout.String())
	}
	if out["status"] != "failed" {
		t.Fatalf("expected failed status, got %#v", out["status"])
	}
	errObj, ok := out["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object, got %#v", out["error"])
	}
	if got := errObj["kind"]; got != "decode" {
		t.Fatalf("expected decode kind, got %#v", got)
	}
	if !strings.Contains(asStringOrEmpty(errObj["message"]), "matched no value") {
		t.Fatalf("unexpected error message: %#v", errObj["message"])
	}
}

func TestOutputRuntimeFailedEnvelopeFilterMissPreservesOriginalError(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"--jmes-filter", "error.missing",
		"tool", "exec", "project.describe-project",
		"--input", `{}`,
	}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected validation exit=1, got %d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v stdout=%q", err, stdout.String())
	}
	if out["status"] != "failed" {
		t.Fatalf("expected failed status, got %#v", out["status"])
	}
	errObj, ok := out["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object, got %#v", out["error"])
	}
	if got := errObj["kind"]; got != "validation" {
		t.Fatalf("expected original validation kind, got %#v", got)
	}
	warnings, ok := out["warnings"].([]any)
	if !ok || len(warnings) == 0 {
		t.Fatalf("expected filter warning, got %#v", out["warnings"])
	}
}

func TestOutputRuntimeFilterDisablesAutoFile(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	prevLimit := toolExecAutoArtifactByteLimit
	toolExecAutoArtifactByteLimit = 400
	defer func() { toolExecAutoArtifactByteLimit = prevLimit }()

	tmp := t.TempDir()
	outDir := filepath.Join(tmp, "out")
	ctxFile := filepath.Join(tmp, "ctx.json")
	reqFile := filepath.Join(tmp, "req.json")
	if err := osWriteJSON(ctxFile, map[string]any{
		"region": "cn-beijing",
		"execution": map[string]any{
			"dry_run": true,
			"output": map[string]any{
				"dir": outDir,
			},
		},
	}); err != nil {
		t.Fatalf("write context: %v", err)
	}
	if err := osWriteJSON(reqFile, map[string]any{
		"TopicId":   "t",
		"Query":     "*",
		"StartTime": 1710374400000,
		"EndTime":   1710378000000,
	}); err != nil {
		t.Fatalf("write request: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"--jmes-filter", "summary.deliveryMode",
		"tool", "exec", "log.search-logs",
		"--context", "file://" + ctxFile,
		"--input", "file://" + reqFile,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	var delivery string
	if err := json.Unmarshal(stdout.Bytes(), &delivery); err != nil {
		t.Fatalf("invalid filtered stdout json: %v stdout=%q", err, stdout.String())
	}
	if delivery != "stdout" {
		t.Fatalf("expected filter to see stdout delivery mode, got %q", delivery)
	}
	entries, err := os.ReadDir(outDir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read output dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected filter to disable auto file spill, found %d entries", len(entries))
	}
}

func TestOutputRuntimeFilterRejectsForcedFile(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"--dry-run",
		"--output-mode", "file",
		"--output-dir", t.TempDir(),
		"--jmes-filter", "summary.deliveryMode",
		"raw",
		"--method", "GET",
		"--path", "/DescribeProjects",
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected forced file + filter rejection stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v stdout=%q", err, stdout.String())
	}
	errObj, ok := out["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object, got %#v", out["error"])
	}
	if got := errObj["kind"]; got != "incompatible_flags" {
		t.Fatalf("expected incompatible_flags kind, got %#v", got)
	}
	if msg, _ := errObj["message"].(string); !strings.Contains(msg, "--jmes-filter") {
		t.Fatalf("expected filter/file error, got %#v", errObj)
	}
}

func TestRawServerFailureWithEnvelopeFilterKeepsNonZeroExit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-tls-requestid", "req-topic-missing")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"ErrorCode":"TopicNotExist","ErrorMessage":"topic not exist"}`))
	}))
	defer srv.Close()

	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", srv.URL)
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"--jmes-filter", "error.code",
		"raw",
		"--method", "GET",
		"--path", "/DescribeTopic",
	}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("expected filtered failed-envelope exit=2, got %d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	var errCode string
	if err := json.Unmarshal(stdout.Bytes(), &errCode); err != nil {
		t.Fatalf("invalid stdout json: %v stdout=%q", err, stdout.String())
	}
	if errCode != "TopicNotExist" {
		t.Fatalf("expected TopicNotExist, got %q", errCode)
	}
}

func TestOutputRuntimeAutoFileStdoutNotice(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	prevLimit := toolExecAutoArtifactByteLimit
	toolExecAutoArtifactByteLimit = 400
	defer func() { toolExecAutoArtifactByteLimit = prevLimit }()

	tmp := t.TempDir()
	outDir := filepath.Join(tmp, "out")
	ctxFile := filepath.Join(tmp, "ctx.json")
	reqFile := filepath.Join(tmp, "req.json")
	if err := osWriteJSON(ctxFile, map[string]any{
		"region": "cn-beijing",
		"execution": map[string]any{
			"dry_run": true,
			"output": map[string]any{
				"dir": outDir,
			},
		},
	}); err != nil {
		t.Fatalf("write context: %v", err)
	}
	if err := osWriteJSON(reqFile, map[string]any{
		"TopicId":   "t",
		"Query":     "*",
		"StartTime": 1710374400000,
		"EndTime":   1710378000000,
	}); err != nil {
		t.Fatalf("write request: %v", err)
	}

	notice, _ := runOutputRuntimeNotice(t, []string{
		"tool", "exec", "log.search-logs",
		"--context", "file://" + ctxFile,
		"--input", "file://" + reqFile,
	})
	for _, want := range []string{"结果过大，已写入文件。", "文件: "} {
		if !strings.Contains(notice, want) {
			t.Fatalf("missing %q in notice %q", want, notice)
		}
	}
}

func TestOutputRuntimeForcedFileStdoutNotice(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	notice, _ := runOutputRuntimeNotice(t, []string{
		"--dry-run",
		"--output-mode", "file",
		"--output-dir", t.TempDir(),
		"raw",
		"--method", "GET",
		"--path", "/DescribeProjects",
	})
	for _, want := range []string{"结果已写入文件。", "文件: "} {
		if !strings.Contains(notice, want) {
			t.Fatalf("missing %q in notice %q", want, notice)
		}
	}
}

func TestOutputRuntimeNoPreviewEnvelopeWhenSpilled(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	prevLimit := toolExecAutoArtifactByteLimit
	toolExecAutoArtifactByteLimit = 400
	defer func() { toolExecAutoArtifactByteLimit = prevLimit }()

	tmp := t.TempDir()
	outDir := filepath.Join(tmp, "out")
	ctxFile := filepath.Join(tmp, "ctx.json")
	reqFile := filepath.Join(tmp, "req.json")
	if err := osWriteJSON(ctxFile, map[string]any{
		"region": "cn-beijing",
		"execution": map[string]any{
			"dry_run": true,
			"output": map[string]any{
				"dir": outDir,
			},
		},
	}); err != nil {
		t.Fatalf("write context: %v", err)
	}
	if err := osWriteJSON(reqFile, map[string]any{
		"TopicId":   "t",
		"Query":     "*",
		"StartTime": 1710374400000,
		"EndTime":   1710378000000,
	}); err != nil {
		t.Fatalf("write request: %v", err)
	}

	notice, _ := runOutputRuntimeNotice(t, []string{
		"tool", "exec", "log.search-logs",
		"--context", "file://" + ctxFile,
		"--input", "file://" + reqFile,
	})
	if strings.Contains(notice, `"status"`) || strings.Contains(notice, `"summary"`) {
		t.Fatalf("expected fixed text notice instead of preview envelope, got %q", notice)
	}
}

func runOutputRuntimeEnvelope(t *testing.T, args []string) map[string]any {
	t.Helper()

	var stdout, stderr bytes.Buffer
	code := Run(args, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	var env map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("invalid stdout json: %v stdout=%q", err, stdout.String())
	}
	return env
}

func runOutputRuntimeNotice(t *testing.T, args []string) (string, string) {
	t.Helper()

	var stdout, stderr bytes.Buffer
	code := Run(args, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	notice := stdout.String()
	const prefix = "文件: "
	idx := strings.LastIndex(notice, prefix)
	if idx < 0 {
		t.Fatalf("missing file notice in stdout: %q", notice)
	}
	path := strings.TrimSpace(notice[idx+len(prefix):])
	if path == "" {
		t.Fatalf("missing file path in notice: %q", notice)
	}
	return notice, path
}

func outputRuntimeReadEnvelopeFile(t *testing.T, path string) map[string]any {
	t.Helper()

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read envelope file: %v", err)
	}
	var env map[string]any
	if err := json.Unmarshal(b, &env); err != nil {
		t.Fatalf("invalid file json: %v content=%q", err, string(b))
	}
	return env
}

func outputRuntimeSummary(t *testing.T, env map[string]any) map[string]any {
	t.Helper()

	summary, ok := env["summary"].(map[string]any)
	if !ok {
		t.Fatalf("missing summary: %#v", env["summary"])
	}
	return summary
}

func outputRuntimeArtifactPath(t *testing.T, env map[string]any) string {
	t.Helper()

	artifacts, ok := env["artifacts"].([]any)
	if !ok || len(artifacts) != 1 {
		t.Fatalf("unexpected artifacts: %#v", env["artifacts"])
	}
	artifact, ok := artifacts[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected artifact: %#v", artifacts[0])
	}
	path, _ := artifact["path"].(string)
	if path == "" {
		t.Fatalf("missing artifact path: %#v", artifact)
	}
	return path
}

func outputRuntimeMapKeys(v map[string]any) []string {
	keys := make([]string, 0, len(v))
	for key := range v {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
