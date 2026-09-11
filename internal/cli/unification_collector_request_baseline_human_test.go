//go:build human

package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/output"
	"github.com/volcengine-tls/ve-tls-cli/internal/tlsapi"
)

type collectorRequestBaselineCase struct {
	name string
	args []string
}

func TestUnificationBaselineRequestCollector(t *testing.T) {
	cases := []collectorRequestBaselineCase{
		{
			name: "list_common_flags",
			args: []string{
				"list",
				"--project-id", "collector-list-project-id",
				"--project-name", "collector-list-project",
				"--iam-project-name", "collector-list-iam-project",
				"--rule-id", "collector-list-rule-id",
				"--rule-name", "collector-list-rule",
				"--topic-id", "collector-list-topic-id",
				"--topic-name", "collector-list-topic",
				"--log-type", "delimiter_log",
				"--rule-type", "1",
				"--page-number", "3",
				"--page-size", "25",
				"--hidden",
			},
		},
		{
			name: "list_pause_unset",
			args: []string{
				"list",
				"--rule-name", "pause-unset",
			},
		},
		{
			name: "list_pause_true",
			args: []string{
				"list",
				"--rule-name", "pause-true",
				"--pause",
			},
		},
		{
			name: "list_pause_false",
			args: []string{
				"list",
				"--rule-name", "pause-false",
				"--no-pause",
			},
		},
		{
			name: "get",
			args: []string{"get", "--rule-id", "collector-get-rule-id"},
		},
		{
			name: "create_flags_only",
			args: []string{
				"create",
				"--topic-id", "collector-create-topic-id",
				"--rule-name", "collector-create-flags",
				"--log-type", "json_log",
				"--paths", `["/var/log/collector-a.log","/var/log/collector-b.log"]`,
				"--input-type", "2",
				"--pause",
			},
		},
		{
			name: "create_request_only",
			args: []string{
				"create",
				"--request", `{"TopicId":"request-only-topic","RuleName":"request-only-rule","LogType":"fullregex_log","Paths":["/data/request.log"],"InputType":1,"Pause":0,"Extra":{"Key":"keep"}}`,
			},
		},
		{
			name: "create_flags_override_request",
			args: []string{
				"create",
				"--request", `{"TopicId":"request-topic","RuleName":"request-rule","LogType":"delimiter_log","Paths":["/data/request.log"],"InputType":1,"Pause":1,"Extra":{"Key":"keep"}}`,
				"--topic-id", "flag-topic",
				"--rule-name", "flag-rule",
				"--log-type", "json_log",
				"--paths", `["/data/flag-a.log","/data/flag-b.log"]`,
				"--input-type", "2",
				"--no-pause",
			},
		},
		{
			name: "modify",
			args: []string{
				"modify",
				"--rule-id", "collector-modify-rule-id",
				"--topic-id", "collector-modify-topic-id",
				"--rule-name", "collector-modified",
				"--log-type", "delimiter_log",
				"--paths", `["/var/log/collector-modified.log"]`,
				"--input-type", "3",
				"--no-pause",
			},
		},
		{
			name: "delete",
			args: []string{"delete", "--rule-id", "collector-delete-rule-id"},
		},
	}

	got := make(map[string]any, len(cases)+1)
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			setHumanRequestBaselineRuntime(t)
			got[tc.name] = captureCollectorRequestBaseline(t, tc.args)
		})
	}
	t.Run("list_all_first_request", func(t *testing.T) {
		setHumanRequestBaselineRuntime(t)
		got["list_all_first_request"] = captureCollectorListAllFirstRequestBaseline(t, []string{
			"list",
			"--all",
			"--page-size", "17",
			"--project-id", "collector-list-all-project-id",
			"--pause",
			"--no-hidden",
		})
	})

	goldenPath := filepath.Join("testdata", "unification", "collector_request_baseline.json")
	raw, err := os.ReadFile(goldenPath)
	if err != nil {
		captured, marshalErr := json.MarshalIndent(got, "", "  ")
		if marshalErr != nil {
			t.Fatalf("marshal captured collector request baseline: %v", marshalErr)
		}
		t.Fatalf("read collector request baseline golden %q: %v\ncaptured baseline:\n%s\n", goldenPath, err, captured)
	}

	var want map[string]any
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("decode collector request baseline golden %q: %v", goldenPath, err)
	}
	assertCanonicalHumanRequestGolden(t, goldenPath, raw, want)
	assertHumanRequestBaselineCases(t, got, want)
	assertCollectorListAllPaginationBaseline(t, got, want)
}

func TestUnificationBaselineRequestCollectorErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "missing_required_get_id",
			args: []string{"get"},
			want: "missing --rule-id",
		},
		{
			name: "cross_field_create_flags_require_topic",
			args: []string{
				"create",
				"--rule-name", "missing-topic-rule",
			},
			want: "missing --topic-id",
		},
		{
			name: "cross_field_create_flags_require_name",
			args: []string{
				"create",
				"--topic-id", "missing-name-topic",
			},
			want: "missing --rule-name",
		},
		{
			name: "missing_required_modify_id",
			args: []string{
				"modify",
				"--rule-name", "missing-id-rule",
			},
			want: "missing --rule-id",
		},
		{
			name: "request_must_be_json_object",
			args: []string{
				"create",
				"--request", `["not","an","object"]`,
			},
			want: "json must be object",
		},
		{
			name: "malformed_request_json",
			args: []string{
				"create",
				"--request", `{"RuleName":`,
			},
			want: "unexpected end of JSON input",
		},
		{
			name: "paths_require_strings",
			args: []string{
				"create",
				"--topic-id", "invalid-paths-topic",
				"--rule-name", "invalid-paths-rule",
				"--paths", `["/var/log/valid.log",2]`,
			},
			want: "json array must contain strings",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			setHumanRequestBaselineRuntime(t)

			var stdout, stderr bytes.Buffer
			ctx := newContext(&stdout, &stderr, output.FormatJSON, "", "")
			ctx.DryRun = true
			defer ctx.Close()

			plan, err := runCollector(ctx, tc.args)
			if err == nil {
				t.Fatalf("expected error %q, got plan %#v", tc.want, plan)
			}
			if err.Error() != tc.want {
				t.Fatalf("error mismatch: got %q, want %q", err.Error(), tc.want)
			}
		})
	}
}

func TestUnificationBaselineRequestCollectorCanonicalLineEndings(t *testing.T) {
	canonical := []byte("{\n  \"a\": 1\n}\n")
	crlf := bytes.ReplaceAll(canonical, []byte("\n"), []byte("\r\n"))
	if !canonicalHumanRequestGoldenMatches(canonical, canonical) {
		t.Fatal("canonical LF golden should match")
	}
	if !canonicalHumanRequestGoldenMatches(crlf, canonical) {
		t.Fatal("canonical CRLF golden should match canonical LF JSON")
	}
	mixed := []byte("{\r\n  \"a\": 1\n}\r\n")
	if canonicalHumanRequestGoldenMatches(mixed, canonical) {
		t.Fatal("golden with mixed LF and CRLF line endings must remain non-canonical")
	}
}

func TestUnificationBaselineRequestCollectorListAllDryRunError(t *testing.T) {
	setHumanRequestBaselineRuntime(t)

	var stdout, stderr bytes.Buffer
	ctx := newContext(&stdout, &stderr, output.FormatJSON, "", "")
	ctx.DryRun = true
	defer ctx.Close()

	plan, err := runCollector(ctx, []string{
		"list",
		"--all",
		"--page-size", "17",
		"--project-id", "collector-list-all-project-id",
		"--pause",
		"--no-hidden",
	})
	if err == nil {
		t.Fatalf("expected current dry-run --all decode error, got plan %#v", plan)
	}
	const want = "unexpected list field: Rules"
	if err.Error() != want {
		t.Fatalf("dry-run --all error mismatch: got %q, want %q", err.Error(), want)
	}
	// Context.Do returns a request plan in dry-run mode. The page-all loop
	// currently tries to decode that plan as an API list response, so it stops
	// after the first request and does not set PaginationMeta.
	if ctx.PaginationMeta != nil {
		t.Fatalf("dry-run --all unexpectedly set pagination metadata: %#v", ctx.PaginationMeta)
	}
}

func captureCollectorRequestBaseline(t *testing.T, args []string) map[string]any {
	t.Helper()

	var stdout, stderr bytes.Buffer
	ctx := newContext(&stdout, &stderr, output.FormatJSON, "", "")
	ctx.DryRun = true
	defer ctx.Close()

	plan, err := runCollector(ctx, args)
	if err != nil {
		t.Fatalf("capture collector request: %v stdout=%q stderr=%q", err, stdout.String(), stderr.String())
	}
	envelope, err := buildAPIEnvelope(ctx, "collector", plan, "stdout", "", output.FormatJSON)
	if err != nil {
		t.Fatalf("build collector success envelope: %v", err)
	}
	return stableHumanRequestEnvelope(t, ctx.Action, envelope)
}

func captureCollectorListAllFirstRequestBaseline(t *testing.T, args []string) map[string]any {
	t.Helper()

	var stdout, stderr bytes.Buffer
	ctx := newContext(&stdout, &stderr, output.FormatJSON, "", "")
	ctx.DryRun = false
	defer ctx.Close()

	transport := &collectorListAllCaptureTransport{}
	client, err := tlsapi.New(
		"https://example.com",
		"cn-beijing",
		"",
		"baseline-ak",
		"baseline-sk",
		"",
		time.Second,
	)
	if err != nil {
		t.Fatalf("create collector list --all client: %v", err)
	}
	client.HTTP.Transport = transport
	ctx.client = client

	out, err := runCollector(ctx, args)
	if err != nil {
		t.Fatalf("capture collector list --all request: %v stdout=%q stderr=%q", err, stdout.String(), stderr.String())
	}
	response, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("collector list --all response has type %T, want object", out)
	}
	rules, ok := response["Rules"].([]any)
	if !ok || len(rules) != 0 || response["Total"] != 0 {
		t.Fatalf("collector list --all response mismatch: %#v", response)
	}
	if len(transport.requests) != 1 {
		t.Fatalf("collector list --all request count = %d, want 1", len(transport.requests))
	}
	if len(transport.requestBodies) != 1 {
		t.Fatalf("collector list --all request body count = %d, want 1", len(transport.requestBodies))
	}
	if got := transport.requestBodies[0].closeCalls; got != 1 {
		t.Fatalf("collector list --all request body close count = %d, want 1", got)
	}
	if len(transport.responseBodies) != 1 {
		t.Fatalf("collector list --all response body count = %d, want 1", len(transport.responseBodies))
	}
	if got := transport.responseBodies[0].closeCalls; got != 1 {
		t.Fatalf("collector list --all response body close count = %d, want 1", got)
	}
	captured := transport.requests[0]

	var body any
	if err := json.Unmarshal(captured.body, &body); err != nil {
		t.Fatalf("decode captured collector list --all body: %v body=%q", err, captured.body)
	}

	stable := map[string]any{
		"action": ctx.Action,
		"data": map[string]any{
			"request_preview": map[string]any{
				"method": captured.method,
				"path":   captured.path,
				"query":  captured.query,
				"header": captured.headerKeys,
				"body":   body,
			},
		},
		"pagination": ctx.PaginationMeta,
	}
	raw, err := json.Marshal(stable)
	if err != nil {
		t.Fatalf("normalize collector list --all first request: %v", err)
	}
	var normalized map[string]any
	if err := json.Unmarshal(raw, &normalized); err != nil {
		t.Fatalf("decode normalized collector list --all first request: %v", err)
	}
	return normalized
}

type collectorListAllCapturedRequest struct {
	method     string
	path       string
	query      url.Values
	headerKeys []string
	body       []byte
}

type collectorListAllCaptureTransport struct {
	requests       []collectorListAllCapturedRequest
	requestBodies  []*collectorListAllRequestBody
	responseBodies []*collectorListAllResponseBody
}

func (t *collectorListAllCaptureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	requestBody := &collectorListAllRequestBody{ReadCloser: req.Body}
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
	headerKeys := make([]string, 0, len(req.Header))
	for key := range req.Header {
		headerKeys = append(headerKeys, key)
	}
	sort.Strings(headerKeys)
	t.requests = append(t.requests, collectorListAllCapturedRequest{
		method:     req.Method,
		path:       req.URL.Path,
		query:      query,
		headerKeys: headerKeys,
		body:       append([]byte(nil), body...),
	})
	responseBody := &collectorListAllResponseBody{
		Reader: strings.NewReader(`{"Rules":[],"Total":0}`),
	}
	t.responseBodies = append(t.responseBodies, responseBody)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"X-Tls-Requestid": []string{"collector-list-all-baseline"}},
		Body:       responseBody,
	}, nil
}

type collectorListAllRequestBody struct {
	io.ReadCloser
	closeCalls int
}

func (b *collectorListAllRequestBody) Close() error {
	b.closeCalls++
	return b.ReadCloser.Close()
}

type collectorListAllResponseBody struct {
	io.Reader
	closeCalls int
}

func (b *collectorListAllResponseBody) Close() error {
	b.closeCalls++
	return nil
}

func assertCollectorListAllPaginationBaseline(t *testing.T, got, want map[string]any) {
	t.Helper()

	gotCase, gotOK := got["list_all_first_request"].(map[string]any)
	wantCase, wantOK := want["list_all_first_request"].(map[string]any)
	if !gotOK || !wantOK {
		t.Fatalf("collector list --all baseline missing object: got=%T want=%T", got["list_all_first_request"], want["list_all_first_request"])
	}
	assertHumanRequestBaselineField(
		t,
		"list_all_first_request",
		"pagination",
		gotCase["pagination"],
		wantCase["pagination"],
	)
}
