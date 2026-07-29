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

type hostGroupRequestBaselineCase struct {
	name string
	args []string
}

func TestUnificationBaselineRequestHostGroup(t *testing.T) {
	cases := []hostGroupRequestBaselineCase{
		{
			name: "list_common_flags",
			args: []string{
				"list",
				"--host-group-id", "host-group-list-id",
				"--host-group-name", "host-group-list-name",
				"--host-identifier", "host-group-list-identifier",
				"--iam-project-name", "host-group-list-iam-project",
				"--page-number", "3",
				"--page-size", "25",
				"--no-service-logging",
				"--hidden",
			},
		},
		{
			name: "list_auto_update_unset",
			args: []string{
				"list",
				"--host-group-name", "auto-update-unset",
			},
		},
		{
			name: "list_auto_update_true",
			args: []string{
				"list",
				"--host-group-name", "auto-update-true",
				"--auto-update",
			},
		},
		{
			name: "list_auto_update_false",
			args: []string{
				"list",
				"--host-group-name", "auto-update-false",
				"--no-auto-update",
			},
		},
		{
			name: "get",
			args: []string{"get", "--host-group-id", "host-group-get-id"},
		},
		{
			name: "create_flags_only",
			args: []string{
				"create",
				"--host-group-name", "host-group-create-flags",
				"--host-group-type", "IP",
				"--host-ip-list", `["10.0.0.1","10.0.0.2"]`,
				"--host-identifier", "host-group-create-identifier",
				"--auto-update",
				"--update-start-time", "01:00",
				"--update-end-time", "03:00",
				"--service-logging",
				"--iam-project-name", "host-group-create-iam-project",
			},
		},
		{
			name: "create_request_only",
			args: []string{
				"create",
				"--request", `{"HostGroupName":"request-only-host-group","HostGroupType":"Label","HostIpList":["10.1.0.1"],"HostIdentifier":"request-only-identifier","AutoUpdate":true,"UpdateStartTime":"02:00","UpdateEndTime":"04:00","ServiceLogging":false,"IamProjectName":"request-only-iam-project"}`,
			},
		},
		{
			name: "create_flags_override_request",
			args: []string{
				"create",
				"--request", `{"HostGroupName":"request-host-group","HostGroupType":"Label","HostIpList":["10.2.0.1"],"HostIdentifier":"request-identifier","AutoUpdate":true,"UpdateStartTime":"05:00","UpdateEndTime":"06:00","ServiceLogging":true,"IamProjectName":"request-iam-project"}`,
				"--host-group-name", "flag-host-group",
				"--host-group-type", "IP",
				"--host-ip-list", `["10.3.0.1","10.3.0.2"]`,
				"--host-identifier", "flag-identifier",
				"--no-auto-update",
				"--update-start-time", "07:00",
				"--update-end-time", "08:00",
				"--no-service-logging",
				"--iam-project-name", "flag-iam-project",
			},
		},
		{
			name: "modify",
			args: []string{
				"modify",
				"--host-group-id", "host-group-modify-id",
				"--host-group-name", "host-group-modified",
				"--host-group-type", "IP",
				"--host-ip-list", `["10.4.0.1","10.4.0.2"]`,
				"--host-identifier", "host-group-modify-identifier",
				"--no-auto-update",
				"--update-start-time", "09:00",
				"--update-end-time", "10:00",
				"--no-service-logging",
				"--iam-project-name", "host-group-modify-iam-project",
			},
		},
		{
			name: "delete",
			args: []string{"delete", "--host-group-id", "host-group-delete-id"},
		},
	}

	got := make(map[string]any, len(cases)+1)
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			setHumanRequestBaselineRuntime(t)
			got[tc.name] = captureHostGroupRequestBaseline(t, tc.args)
		})
	}
	t.Run("list_all_first_request", func(t *testing.T) {
		setHumanRequestBaselineRuntime(t)
		got["list_all_first_request"] = captureHostGroupListAllFirstRequestBaseline(t, []string{
			"list",
			"--all",
			"--page-size", "17",
			"--host-group-name", "host-group-list-all",
			"--auto-update",
			"--no-hidden",
		})
	})

	goldenPath := filepath.Join("testdata", "unification", "host_group_request_baseline.json")
	raw, err := os.ReadFile(goldenPath)
	if err != nil {
		captured, marshalErr := json.MarshalIndent(got, "", "  ")
		if marshalErr != nil {
			t.Fatalf("marshal captured host-group request baseline: %v", marshalErr)
		}
		t.Fatalf("read host-group request baseline golden %q: %v\ncaptured baseline:\n%s\n", goldenPath, err, captured)
	}

	var want map[string]any
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("decode host-group request baseline golden %q: %v", goldenPath, err)
	}
	assertCanonicalHumanRequestGolden(t, goldenPath, raw, want)
	assertHumanRequestBaselineCases(t, got, want)
	assertHostGroupListAllPaginationBaseline(t, got, want)
}

func TestUnificationBaselineRequestHostGroupErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "missing_required_get_id",
			args: []string{"get"},
			want: "missing --host-group-id",
		},
		{
			name: "cross_field_create_flags_require_type",
			args: []string{
				"create",
				"--host-group-name", "missing-type-host-group",
			},
			want: "missing --host-group-type",
		},
		{
			name: "missing_required_modify_id",
			args: []string{
				"modify",
				"--host-group-name", "missing-id-host-group",
			},
			want: "missing --host-group-id",
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
				"--request", `{"HostGroupName":`,
			},
			want: "unexpected end of JSON input",
		},
		{
			name: "host_ip_list_requires_strings",
			args: []string{
				"create",
				"--host-group-name", "invalid-host-list",
				"--host-group-type", "IP",
				"--host-ip-list", `["10.5.0.1",2]`,
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

			plan, err := runHostGroup(ctx, tc.args)
			if err == nil {
				t.Fatalf("expected error %q, got plan %#v", tc.want, plan)
			}
			if err.Error() != tc.want {
				t.Fatalf("error mismatch: got %q, want %q", err.Error(), tc.want)
			}
		})
	}
}

func TestUnificationBaselineRequestHostGroupCanonicalLineEndings(t *testing.T) {
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

func TestUnificationBaselineRequestHostGroupListAllDryRunError(t *testing.T) {
	setHumanRequestBaselineRuntime(t)

	var stdout, stderr bytes.Buffer
	ctx := newContext(&stdout, &stderr, output.FormatJSON, "", "")
	ctx.DryRun = true
	defer ctx.Close()

	plan, err := runHostGroup(ctx, []string{
		"list",
		"--all",
		"--page-size", "17",
		"--host-group-name", "host-group-list-all",
		"--auto-update",
		"--no-hidden",
	})
	if err == nil {
		t.Fatalf("expected current dry-run --all decode error, got plan %#v", plan)
	}
	const want = "unexpected list field: HostGroupHostsRulesInfos"
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

func captureHostGroupRequestBaseline(t *testing.T, args []string) map[string]any {
	t.Helper()

	var stdout, stderr bytes.Buffer
	ctx := newContext(&stdout, &stderr, output.FormatJSON, "", "")
	ctx.DryRun = true
	defer ctx.Close()

	plan, err := runHostGroup(ctx, args)
	if err != nil {
		t.Fatalf("capture host-group request: %v stdout=%q stderr=%q", err, stdout.String(), stderr.String())
	}
	envelope, err := buildAPIEnvelope(ctx, "host-group", plan, "stdout", "", output.FormatJSON)
	if err != nil {
		t.Fatalf("build host-group success envelope: %v", err)
	}
	return stableHumanRequestEnvelope(t, ctx.Action, envelope)
}

func captureHostGroupListAllFirstRequestBaseline(t *testing.T, args []string) map[string]any {
	t.Helper()

	var stdout, stderr bytes.Buffer
	ctx := newContext(&stdout, &stderr, output.FormatJSON, "", "")
	ctx.DryRun = false
	defer ctx.Close()

	transport := &hostGroupListAllCaptureTransport{}
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
		t.Fatalf("create host-group list --all client: %v", err)
	}
	client.HTTP.Transport = transport
	ctx.client = client

	out, err := runHostGroup(ctx, args)
	if err != nil {
		t.Fatalf("capture host-group list --all request: %v stdout=%q stderr=%q", err, stdout.String(), stderr.String())
	}
	response, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("host-group list --all response has type %T, want object", out)
	}
	hostGroups, ok := response["HostGroupHostsRulesInfos"].([]any)
	if !ok || len(hostGroups) != 0 || response["Total"] != 0 {
		t.Fatalf("host-group list --all response mismatch: %#v", response)
	}
	if len(transport.requests) != 1 {
		t.Fatalf("host-group list --all request count = %d, want 1", len(transport.requests))
	}
	if len(transport.responseBodies) != 1 {
		t.Fatalf("host-group list --all response body count = %d, want 1", len(transport.responseBodies))
	}
	if got := transport.responseBodies[0].closeCalls; got != 1 {
		t.Fatalf("host-group list --all response body close count = %d, want 1", got)
	}
	captured := transport.requests[0]

	var body any
	if err := json.Unmarshal(captured.body, &body); err != nil {
		t.Fatalf("decode captured host-group list --all body: %v body=%q", err, captured.body)
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
		t.Fatalf("normalize host-group list --all first request: %v", err)
	}
	var normalized map[string]any
	if err := json.Unmarshal(raw, &normalized); err != nil {
		t.Fatalf("decode normalized host-group list --all first request: %v", err)
	}
	return normalized
}

type hostGroupListAllCapturedRequest struct {
	method     string
	path       string
	query      url.Values
	headerKeys []string
	body       []byte
}

type hostGroupListAllCaptureTransport struct {
	requests       []hostGroupListAllCapturedRequest
	responseBodies []*hostGroupListAllResponseBody
}

func (t *hostGroupListAllCaptureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
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
	t.requests = append(t.requests, hostGroupListAllCapturedRequest{
		method:     req.Method,
		path:       req.URL.Path,
		query:      query,
		headerKeys: headerKeys,
		body:       append([]byte(nil), body...),
	})
	responseBody := &hostGroupListAllResponseBody{
		Reader: strings.NewReader(`{"HostGroupHostsRulesInfos":[],"Total":0}`),
	}
	t.responseBodies = append(t.responseBodies, responseBody)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"X-Tls-Requestid": []string{"host-group-list-all-baseline"}},
		Body:       responseBody,
	}, nil
}

type hostGroupListAllResponseBody struct {
	io.Reader
	closeCalls int
}

func (b *hostGroupListAllResponseBody) Close() error {
	b.closeCalls++
	return nil
}

func assertHostGroupListAllPaginationBaseline(t *testing.T, got, want map[string]any) {
	t.Helper()

	gotCase, gotOK := got["list_all_first_request"].(map[string]any)
	wantCase, wantOK := want["list_all_first_request"].(map[string]any)
	if !gotOK || !wantOK {
		t.Fatalf("host-group list --all baseline missing object: got=%T want=%T", got["list_all_first_request"], want["list_all_first_request"])
	}
	assertHumanRequestBaselineField(
		t,
		"list_all_first_request",
		"pagination",
		gotCase["pagination"],
		wantCase["pagination"],
	)
}
