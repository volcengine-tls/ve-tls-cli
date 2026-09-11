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

type metricTopicRequestBaselineCase struct {
	name string
	args []string
}

func TestUnificationBaselineRequestMetricTopic(t *testing.T) {
	cases := []metricTopicRequestBaselineCase{
		{
			name: "list_common_flags",
			args: []string{
				"list",
				"--page-number", "3",
				"--page-size", "25",
				"--project-id", "metric-list-project-id",
				"--project-name", "metric-list-project",
				"--topic-name", "metric-list-topic",
				"--region", "cn-beijing",
				"--fuzzy-search-key", "metric",
				"--description", "metric topic description",
				"--tags", `[{"Key":"tier","Value":"baseline"}]`,
				"--no-is-full-name",
				"--no-favourite",
				"--no-order-by-project",
			},
		},
		{
			name: "get",
			args: []string{"get", "--topic-id", "metric-get-topic-id"},
		},
		{
			name: "create_defaults",
			args: []string{
				"create",
				"--project-id", "metric-create-project-id",
				"--topic-name", "metric-create-topic",
			},
		},
		{
			name: "create_request_only",
			args: []string{
				"create",
				"--request", `{"ProjectId":"request-only-project","TopicName":"request-only-topic","Description":"request-only-description","Ttl":91,"ShardCount":7,"AutoSplit":false,"Tags":[{"Key":"source","Value":"request"}]}`,
			},
		},
		{
			name: "create_flags_override_request",
			args: []string{
				"create",
				"--request", `{"ProjectId":"request-project","TopicName":"request-topic","Description":"request-description","Ttl":7,"ShardCount":4,"AutoSplit":false,"MaxSplitShard":8,"Tags":[{"Key":"source","Value":"request"}]}`,
				"--project-id", "flag-project",
				"--topic-name", "flag-topic",
				"--description", "flag-description",
				"--ttl", "60",
				"--shard-count", "8",
				"--auto-split",
				"--max-split-shard", "16",
				"--tags", `[{"Key":"source","Value":"flag"}]`,
			},
		},
		{
			name: "modify_auto_split_unset",
			args: []string{
				"modify",
				"--topic-id", "metric-modify-unset-id",
				"--description", "auto split remains absent",
			},
		},
		{
			name: "modify_auto_split_false",
			args: []string{
				"modify",
				"--topic-id", "metric-modify-false-id",
				"--no-auto-split",
			},
		},
		{
			name: "modify_auto_split_true",
			args: []string{
				"modify",
				"--topic-id", "metric-modify-true-id",
				"--auto-split",
				"--max-split-shard", "16",
			},
		},
		{
			name: "delete",
			args: []string{"delete", "--topic-id", "metric-delete-topic-id"},
		},
		{
			name: "search",
			args: []string{
				"search",
				"--topic-id", "metric-search-topic-id",
				"--query", "error",
				"--from", "1700000000000",
				"--to", "1700000060000",
				"--limit", "25",
				"--context", "metric-search-context",
				"--sort", "asc",
				"--highlight",
				"--no-accurate-query",
				"--must-complete",
				"--offset", "3",
			},
		},
	}

	got := make(map[string]any, len(cases)+1)
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			setHumanRequestBaselineRuntime(t)
			got[tc.name] = captureMetricTopicRequestBaseline(t, tc.args)
		})
	}
	t.Run("list_all_first_request", func(t *testing.T) {
		setHumanRequestBaselineRuntime(t)
		got["list_all_first_request"] = captureMetricTopicListAllFirstRequestBaseline(t, []string{
			"list",
			"--all",
			"--page-size", "17",
			"--project-id", "metric-list-all-project-id",
			"--favourite",
		})
	})

	goldenPath := filepath.Join("testdata", "unification", "metric_topic_request_baseline.json")
	raw, err := os.ReadFile(goldenPath)
	if err != nil {
		captured, marshalErr := json.MarshalIndent(got, "", "  ")
		if marshalErr != nil {
			t.Fatalf("marshal captured metric-topic request baseline: %v", marshalErr)
		}
		t.Fatalf("read metric-topic request baseline golden %q: %v\ncaptured baseline:\n%s\n", goldenPath, err, captured)
	}

	var want map[string]any
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("decode metric-topic request baseline golden %q: %v", goldenPath, err)
	}
	assertCanonicalHumanRequestGolden(t, goldenPath, raw, want)
	assertHumanRequestBaselineCases(t, got, want)
	assertMetricTopicListAllPaginationBaseline(t, got, want)
}

func TestUnificationBaselineRequestMetricTopicErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "missing_required",
			args: []string{"get"},
			want: "missing --topic-id",
		},
		{
			name: "cross_field_auto_split_requires_max",
			args: []string{
				"create",
				"--request", `{"ProjectId":"error-project","TopicName":"error-topic","AutoSplit":true}`,
			},
			want: "missing --max-split-shard when AutoSplit is true",
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

			plan, err := runMetricTopic(ctx, tc.args)
			if err == nil {
				t.Fatalf("expected error %q, got plan %#v", tc.want, plan)
			}
			if err.Error() != tc.want {
				t.Fatalf("error mismatch: got %q, want %q", err.Error(), tc.want)
			}
		})
	}
}

func TestUnificationBaselineRequestMetricTopicCanonicalLineEndings(t *testing.T) {
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

func TestUnificationBaselineRequestMetricTopicListAllDryRunError(t *testing.T) {
	setHumanRequestBaselineRuntime(t)

	var stdout, stderr bytes.Buffer
	ctx := newContext(&stdout, &stderr, output.FormatJSON, "", "")
	ctx.DryRun = true
	defer ctx.Close()

	plan, err := runMetricTopic(ctx, []string{
		"list",
		"--all",
		"--page-size", "17",
		"--project-id", "metric-list-all-project-id",
		"--favourite",
	})
	if err == nil {
		t.Fatalf("expected current dry-run --all decode error, got plan %#v", plan)
	}
	const want = "unexpected list field: Topics"
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

func captureMetricTopicRequestBaseline(t *testing.T, args []string) map[string]any {
	t.Helper()

	var stdout, stderr bytes.Buffer
	ctx := newContext(&stdout, &stderr, output.FormatJSON, "", "")
	ctx.DryRun = true
	defer ctx.Close()

	plan, err := runMetricTopic(ctx, args)
	if err != nil {
		t.Fatalf("capture metric-topic request: %v stdout=%q stderr=%q", err, stdout.String(), stderr.String())
	}
	envelope, err := buildAPIEnvelope(ctx, "metric-topic", plan, "stdout", "", output.FormatJSON)
	if err != nil {
		t.Fatalf("build metric-topic success envelope: %v", err)
	}
	return stableHumanRequestEnvelope(t, ctx.Action, envelope)
}

func captureMetricTopicListAllFirstRequestBaseline(t *testing.T, args []string) map[string]any {
	t.Helper()

	var stdout, stderr bytes.Buffer
	ctx := newContext(&stdout, &stderr, output.FormatJSON, "", "")
	ctx.DryRun = false
	defer ctx.Close()

	transport := &metricTopicListAllCaptureTransport{}
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
		t.Fatalf("create metric-topic list --all client: %v", err)
	}
	client.HTTP.Transport = transport
	ctx.client = client

	out, err := runMetricTopic(ctx, args)
	if err != nil {
		t.Fatalf("capture metric-topic list --all request: %v stdout=%q stderr=%q", err, stdout.String(), stderr.String())
	}
	response, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("metric-topic list --all response has type %T, want object", out)
	}
	topics, ok := response["Topics"].([]any)
	if !ok || len(topics) != 0 || response["Total"] != 0 {
		t.Fatalf("metric-topic list --all response mismatch: %#v", response)
	}
	if len(transport.requests) != 1 {
		t.Fatalf("metric-topic list --all request count = %d, want 1", len(transport.requests))
	}
	if len(transport.responseBodies) != 1 {
		t.Fatalf("metric-topic list --all response body count = %d, want 1", len(transport.responseBodies))
	}
	if got := transport.responseBodies[0].closeCalls; got != 1 {
		t.Fatalf("metric-topic list --all response body close count = %d, want 1", got)
	}
	captured := transport.requests[0]

	var body any
	if err := json.Unmarshal(captured.body, &body); err != nil {
		t.Fatalf("decode captured metric-topic list --all body: %v body=%q", err, captured.body)
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
		t.Fatalf("normalize metric-topic list --all first request: %v", err)
	}
	var normalized map[string]any
	if err := json.Unmarshal(raw, &normalized); err != nil {
		t.Fatalf("decode normalized metric-topic list --all first request: %v", err)
	}
	return normalized
}

type metricTopicListAllCapturedRequest struct {
	method     string
	path       string
	query      url.Values
	headerKeys []string
	body       []byte
}

type metricTopicListAllCaptureTransport struct {
	requests       []metricTopicListAllCapturedRequest
	responseBodies []*metricTopicListAllResponseBody
}

func (t *metricTopicListAllCaptureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body, readErr := io.ReadAll(req.Body)
	closeErr := req.Body.Close()
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
	t.requests = append(t.requests, metricTopicListAllCapturedRequest{
		method:     req.Method,
		path:       req.URL.Path,
		query:      query,
		headerKeys: headerKeys,
		body:       append([]byte(nil), body...),
	})
	responseBody := &metricTopicListAllResponseBody{
		Reader: strings.NewReader(`{"Topics":[],"Total":0}`),
	}
	t.responseBodies = append(t.responseBodies, responseBody)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"X-Tls-Requestid": []string{"metric-topic-list-all-baseline"}},
		Body:       responseBody,
	}, nil
}

type metricTopicListAllResponseBody struct {
	io.Reader
	closeCalls int
}

func (b *metricTopicListAllResponseBody) Close() error {
	b.closeCalls++
	return nil
}

func assertMetricTopicListAllPaginationBaseline(t *testing.T, got, want map[string]any) {
	t.Helper()

	gotCase, gotOK := got["list_all_first_request"].(map[string]any)
	wantCase, wantOK := want["list_all_first_request"].(map[string]any)
	if !gotOK || !wantOK {
		t.Fatalf("metric-topic list --all baseline missing object: got=%T want=%T", got["list_all_first_request"], want["list_all_first_request"])
	}
	assertHumanRequestBaselineField(
		t,
		"list_all_first_request",
		"pagination",
		gotCase["pagination"],
		wantCase["pagination"],
	)
}
