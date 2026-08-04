//go:build human

package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/output"
	"github.com/volcengine-tls/ve-tls-cli/internal/tlsapi"
)

func TestUnificationBaselineOutput(t *testing.T) {
	got := map[string]any{
		"stdout_success_envelope":      captureUnificationOutputStdoutSuccess(t),
		"run_failed_invalid_input":     captureUnificationOutputInvalidInput(t),
		"jmes_filter_data_scope":       captureUnificationOutputJMESDataScope(t),
		"forced_file_delivery":         captureUnificationOutputForcedFile(t),
		"table_eligibility_matrix":     captureUnificationOutputTableEligibility(),
		"pagination_total_item_counts": captureUnificationOutputPagination(t),
	}
	got = normalizeUnificationOutputBaseline(t, got)

	goldenPath := filepath.Join("testdata", "unification", "output_golden.json")
	canonical := marshalCanonicalUnificationOutputGolden(t, got)
	raw, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf(
			"read output baseline golden %q: %v\ncaptured baseline:\n%s\n",
			goldenPath,
			err,
			canonical,
		)
	}

	var want map[string]any
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("decode output baseline golden %q: %v", goldenPath, err)
	}
	assertCanonicalUnificationOutputGolden(t, goldenPath, raw, want)
	assertUnificationOutputBaselineCases(t, got, want)
}

func TestUnificationBaselineOutputGoldenCanonicalLineEndings(t *testing.T) {
	canonical := []byte("{\n  \"a\": 1\n}\n")
	crlf := bytes.ReplaceAll(canonical, []byte("\n"), []byte("\r\n"))
	if !canonicalUnificationOutputGoldenMatches(crlf, canonical) {
		t.Fatal("canonical CRLF golden should match canonical LF JSON")
	}
	mixed := []byte("{\r\n  \"a\": 1\n}\r\n")
	if canonicalUnificationOutputGoldenMatches(mixed, canonical) {
		t.Fatal("golden with mixed LF and CRLF line endings must remain non-canonical")
	}
	if canonicalUnificationOutputGoldenMatches(append(crlf, '\r', '\n'), canonical) {
		t.Fatal("golden with an extra trailing newline must remain non-canonical")
	}
}

func TestUnificationBaselineOutputTimestampedArtifactName(t *testing.T) {
	cases := []struct {
		name      string
		artifact  string
		wantValid bool
	}{
		{name: "valid", artifact: "raw-2026-07-29T18-08-51.123Z.json", wantValid: true},
		{name: "random_timestamp", artifact: "raw-random.json"},
		{name: "empty_timestamp", artifact: "raw-.json"},
		{name: "wrong_prefix", artifact: "tool-2026-07-29T18-08-51.123Z.json"},
		{name: "wrong_suffix", artifact: "raw-2026-07-29T18-08-51.123Z.jsonl"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateUnificationOutputTimestampedArtifactName(tc.artifact)
			if tc.wantValid && err != nil {
				t.Fatalf("validate %q: %v", tc.artifact, err)
			}
			if !tc.wantValid && err == nil {
				t.Fatalf("validate %q unexpectedly succeeded", tc.artifact)
			}
		})
	}
}

func validateUnificationOutputTimestampedArtifactName(name string) error {
	const (
		prefix = "raw-"
		suffix = ".json"
		layout = "2006-01-02T15-04-05.000Z"
	)
	if !strings.HasPrefix(name, prefix) {
		return fmt.Errorf("missing %q prefix", prefix)
	}
	if !strings.HasSuffix(name, suffix) {
		return fmt.Errorf("missing %q suffix", suffix)
	}
	timestamp := strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix)
	if _, err := time.Parse(layout, timestamp); err != nil {
		return fmt.Errorf("invalid timestamp %q: %w", timestamp, err)
	}
	return nil
}

func captureUnificationOutputStdoutSuccess(t *testing.T) map[string]any {
	t.Helper()
	setUnificationOutputBaselineRuntime(t)

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"--region", "cn-output-baseline",
		"--endpoint", "https://tls-output-baseline.invalid",
		"--dry-run",
		"raw",
		"--method", "POST",
		"--path", "/CreateProject",
		"--query", "zeta=last",
		"--query", "alpha=1",
		"--header", "X-Baseline=redacted-value",
		"--body", `{"Description":"snapshot","ProjectName":"baseline-project"}`,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("stdout success Run exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("stdout success Run wrote stderr=%q", stderr.String())
	}

	return decodeUnificationOutputJSONMap(t, "stdout success envelope", stdout.Bytes())
}

func captureUnificationOutputInvalidInput(t *testing.T) map[string]any {
	t.Helper()
	setUnificationOutputBaselineRuntime(t)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"raw", "--method", "GET"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("invalid-input Run unexpectedly succeeded stdout=%q stderr=%q", stdout.String(), stderr.String())
	}

	return map[string]any{
		"exitCode": code,
		"stderr":   stderr.String(),
		"stdout":   decodeUnificationOutputJSONMap(t, "invalid-input envelope", stdout.Bytes()),
	}
}

func captureUnificationOutputJMESDataScope(t *testing.T) map[string]any {
	t.Helper()
	setUnificationOutputBaselineRuntime(t)

	const expression = "data.request_preview.body.ProjectName"
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"--region", "cn-output-baseline",
		"--endpoint", "https://tls-output-baseline.invalid",
		"--dry-run",
		"--jmes-filter", expression,
		"raw",
		"--method", "POST",
		"--path", "/CreateProject",
		"--body", `{"ProjectName":"baseline-filter"}`,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("JMES Run exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	var filtered any
	if err := json.Unmarshal(stdout.Bytes(), &filtered); err != nil {
		t.Fatalf("decode JMES filtered stdout: %v stdout=%q", err, stdout.String())
	}
	return map[string]any{
		"exitCode":   code,
		"expression": expression,
		"result":     filtered,
		"stderr":     stderr.String(),
	}
}

func captureUnificationOutputForcedFile(t *testing.T) map[string]any {
	t.Helper()
	setUnificationOutputBaselineRuntime(t)

	outDir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"--region", "cn-output-baseline",
		"--endpoint", "https://tls-output-baseline.invalid",
		"--dry-run",
		"--output-mode", "file",
		"--output-dir", outDir,
		"raw",
		"--method", "GET",
		"--path", "/DescribeProjects",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("forced-file Run exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("forced-file Run wrote stderr=%q", stderr.String())
	}

	actualPath := parseNoticePath(t, stdout.String())
	if filepath.Dir(actualPath) != outDir {
		t.Fatalf("forced-file artifact escaped temp dir: got %q want dir %q", actualPath, outDir)
	}
	base := filepath.Base(actualPath)
	if err := validateUnificationOutputTimestampedArtifactName(base); err != nil {
		t.Fatalf("forced-file artifact name %q does not match raw timestamped JSON convention: %v", base, err)
	}

	raw, err := os.ReadFile(actualPath)
	if err != nil {
		t.Fatalf("read forced-file envelope %q: %v", actualPath, err)
	}
	env := decodeUnificationOutputJSONMap(t, "forced-file envelope", raw)
	artifact := firstUnificationOutputArtifact(t, env)
	if artifact["path"] != actualPath {
		t.Fatalf("forced-file artifact path=%#v want %q", artifact["path"], actualPath)
	}
	fileBytes := float64(len(raw))
	if artifact["sizeBytes"] != fileBytes {
		t.Fatalf("forced-file artifact sizeBytes=%#v want %v", artifact["sizeBytes"], fileBytes)
	}
	summary := unificationOutputSummary(t, env)
	if summary["totalBytes"] != fileBytes {
		t.Fatalf("forced-file summary totalBytes=%#v want %v", summary["totalBytes"], fileBytes)
	}

	const stablePath = "<TEMP>/raw-<TIMESTAMP>.json"
	artifact["path"] = stablePath
	artifact["sizeBytes"] = "<FILE_BYTES>"
	summary["totalBytes"] = "<FILE_BYTES>"
	return map[string]any{
		"exitCode":     code,
		"fileEnvelope": env,
		"notice":       strings.ReplaceAll(stdout.String(), actualPath, stablePath),
		"stderr":       stderr.String(),
	}
}

func captureUnificationOutputTableEligibility() map[string]any {
	actions := []string{
		"project.list",
		"project.get",
		"topic.list",
		"topic.get",
		"metric-topic.list",
		"metric-topic.get",
		"index.get",
		"log.search",
		"project.create",
		"topic.delete",
		"raw.call",
	}
	got := make(map[string]any, len(actions))
	ctx := &Context{}
	for _, action := range actions {
		ctx.Action = action
		got[action] = supportsTableOutput(ctx)
	}
	return got
}

func captureUnificationOutputPagination(t *testing.T) map[string]any {
	t.Helper()

	ctx := newContext(io.Discard, io.Discard, output.FormatJSON, "", "")
	defer ctx.Close()
	ctx.OutputMode = "stdout"

	transport := &unificationOutputPaginationTransport{}
	client, err := tlsapi.New(
		"https://output-baseline.invalid",
		"cn-output-baseline",
		"",
		"baseline-ak",
		"baseline-sk",
		"",
		time.Second,
	)
	if err != nil {
		t.Fatalf("create pagination baseline client: %v", err)
	}
	client.HTTP.Transport = transport
	ctx.client = client

	out, err := runProject(ctx, []string{"list", "--all", "--page-size", "2"})
	if err != nil {
		t.Fatalf("run project list --all baseline: %v", err)
	}
	response, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("project list --all response has type %T, want object", out)
	}
	projects, ok := response["Projects"].([]any)
	if !ok || len(projects) != 3 {
		t.Fatalf("project list --all Projects=%#v, want 3 items", response["Projects"])
	}
	for i, wantID := range []string{"project-1", "project-2", "project-3"} {
		project, ok := projects[i].(map[string]any)
		if !ok || project["ProjectId"] != wantID {
			t.Fatalf("project list --all Projects[%d]=%#v, want ProjectId=%q", i, projects[i], wantID)
		}
	}
	if response["Total"] != 3 {
		t.Fatalf("project list --all Total=%#v, want 3", response["Total"])
	}
	wantPagination := map[string]any{
		"mode":      "page_all",
		"pageCount": 2,
		"pageSize":  2,
		"merged":    true,
	}
	if !reflect.DeepEqual(ctx.PaginationMeta, wantPagination) {
		t.Fatalf("project list --all pagination=%#v, want %#v", ctx.PaginationMeta, wantPagination)
	}
	if len(transport.requests) != 2 {
		t.Fatalf("project list --all request count=%d, want 2", len(transport.requests))
	}
	if len(transport.requestBodies) != 2 {
		t.Fatalf("project list --all request body count=%d, want 2", len(transport.requestBodies))
	}
	if len(transport.responseBodies) != 2 {
		t.Fatalf("project list --all response body count=%d, want 2", len(transport.responseBodies))
	}
	for i, request := range transport.requests {
		pageNumber := fmt.Sprintf("%d", i+1)
		wantQuery := url.Values{
			"PageNumber": []string{pageNumber},
			"PageSize":   []string{"2"},
		}
		if request.method != http.MethodGet || request.path != "/DescribeProjects" {
			t.Fatalf("project list --all request[%d]=%s %s, want GET /DescribeProjects", i, request.method, request.path)
		}
		if !reflect.DeepEqual(request.query, wantQuery) {
			t.Fatalf("project list --all request[%d] query=%v, want %v", i, request.query, wantQuery)
		}
		if strings.TrimSpace(string(request.body)) != "{}" {
			t.Fatalf("project list --all request[%d] body=%q, want {}", i, request.body)
		}
		if strings.TrimSpace(request.header.Get("Authorization")) == "" {
			t.Fatalf("project list --all request[%d] missing Authorization header", i)
		}
		if got := transport.requestBodies[i].closeCalls; got != 1 {
			t.Fatalf("project list --all request[%d] body close count=%d, want 1", i, got)
		}
		if got := transport.responseBodies[i].closeCalls; got != 1 {
			t.Fatalf("project list --all response[%d] body close count=%d, want 1", i, got)
		}
	}

	env, err := buildAPIEnvelope(ctx, "project", response, "stdout", "", output.FormatJSON)
	if err != nil {
		t.Fatalf("build pagination baseline envelope: %v", err)
	}
	return env
}

type unificationOutputPaginationCapturedRequest struct {
	method string
	path   string
	query  url.Values
	header http.Header
	body   []byte
}

type unificationOutputPaginationTransport struct {
	requests       []unificationOutputPaginationCapturedRequest
	requestBodies  []*unificationOutputPaginationRequestBody
	responseBodies []*unificationOutputPaginationResponseBody
}

func (t *unificationOutputPaginationTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	requestBody := &unificationOutputPaginationRequestBody{ReadCloser: req.Body}
	req.Body = requestBody
	body, readErr := io.ReadAll(req.Body)
	closeErr := req.Body.Close()
	t.requestBodies = append(t.requestBodies, requestBody)
	if err := errors.Join(readErr, closeErr); err != nil {
		return nil, err
	}

	query := make(url.Values, len(req.URL.Query()))
	for key, values := range req.URL.Query() {
		query[key] = append([]string(nil), values...)
	}
	t.requests = append(t.requests, unificationOutputPaginationCapturedRequest{
		method: req.Method,
		path:   req.URL.Path,
		query:  query,
		header: req.Header.Clone(),
		body:   append([]byte(nil), body...),
	})

	pageNumber := query.Get("PageNumber")
	var payload string
	switch pageNumber {
	case "1":
		payload = `{"Projects":[{"ProjectId":"project-1"},{"ProjectId":"project-2"}],"Total":3}`
	case "2":
		payload = `{"Projects":[{"ProjectId":"project-3"}],"Total":3}`
	default:
		return nil, fmt.Errorf("unexpected PageNumber %q", pageNumber)
	}
	responseBody := &unificationOutputPaginationResponseBody{
		Reader: strings.NewReader(payload),
	}
	t.responseBodies = append(t.responseBodies, responseBody)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"X-Tls-Requestid": []string{"req-output-baseline-page-" + pageNumber},
		},
		Body: responseBody,
	}, nil
}

type unificationOutputPaginationRequestBody struct {
	io.ReadCloser
	closeCalls int
}

func (b *unificationOutputPaginationRequestBody) Close() error {
	b.closeCalls++
	return b.ReadCloser.Close()
}

type unificationOutputPaginationResponseBody struct {
	io.Reader
	closeCalls int
}

func (b *unificationOutputPaginationResponseBody) Close() error {
	b.closeCalls++
	return nil
}

func setUnificationOutputBaselineRuntime(t *testing.T) {
	t.Helper()
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "baseline-ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "baseline-sk")
	t.Setenv("VOLCENGINE_TOKEN", "")
	t.Setenv("VOLCENGINE_REGION", "")
	t.Setenv("VOLCENGINE_ENDPOINT", "")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))
}

func decodeUnificationOutputJSONMap(t *testing.T, source string, raw []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode %s: %v output=%q", source, err, raw)
	}
	return out
}

func firstUnificationOutputArtifact(t *testing.T, env map[string]any) map[string]any {
	t.Helper()
	artifacts, ok := env["artifacts"].([]any)
	if !ok || len(artifacts) != 1 {
		t.Fatalf("expected one artifact, got %#v", env["artifacts"])
	}
	artifact, ok := artifacts[0].(map[string]any)
	if !ok {
		t.Fatalf("artifact has type %T, want object", artifacts[0])
	}
	return artifact
}

func unificationOutputSummary(t *testing.T, env map[string]any) map[string]any {
	t.Helper()
	summary, ok := env["summary"].(map[string]any)
	if !ok {
		t.Fatalf("summary has type %T, want object", env["summary"])
	}
	return summary
}

func normalizeUnificationOutputBaseline(t *testing.T, value map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal output baseline for normalization: %v", err)
	}
	var normalized map[string]any
	if err := json.Unmarshal(raw, &normalized); err != nil {
		t.Fatalf("decode normalized output baseline: %v", err)
	}
	return normalized
}

func marshalCanonicalUnificationOutputGolden(t *testing.T, value map[string]any) []byte {
	t.Helper()
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("marshal canonical output baseline golden: %v", err)
	}
	return append(raw, '\n')
}

func assertCanonicalUnificationOutputGolden(t *testing.T, path string, raw []byte, decoded map[string]any) {
	t.Helper()
	canonicalLF := marshalCanonicalUnificationOutputGolden(t, decoded)
	if !canonicalUnificationOutputGoldenMatches(raw, canonicalLF) {
		t.Errorf("output baseline golden %q must use stable key ordering, 2-space indentation, and one trailing newline", path)
	}
}

func canonicalUnificationOutputGoldenMatches(raw, canonicalLF []byte) bool {
	canonicalCRLF := bytes.ReplaceAll(canonicalLF, []byte("\n"), []byte("\r\n"))
	return bytes.Equal(raw, canonicalLF) || bytes.Equal(raw, canonicalCRLF)
}

func assertUnificationOutputBaselineCases(t *testing.T, got, want map[string]any) {
	t.Helper()
	var missing, extra []string
	for name := range want {
		if _, ok := got[name]; !ok {
			missing = append(missing, name)
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			extra = append(extra, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 || len(extra) > 0 {
		t.Errorf("output baseline case set mismatch: missing=%v extra=%v", missing, extra)
	}

	names := make([]string, 0, len(got))
	for name := range got {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		wantCase, ok := want[name]
		if !ok {
			continue
		}
		if !reflect.DeepEqual(got[name], wantCase) {
			gotRaw, _ := json.MarshalIndent(got[name], "", "  ")
			wantRaw, _ := json.MarshalIndent(wantCase, "", "  ")
			t.Errorf("output baseline case %q mismatch:\ngot:\n%s\nwant:\n%s", name, gotRaw, wantRaw)
		}
	}
}
