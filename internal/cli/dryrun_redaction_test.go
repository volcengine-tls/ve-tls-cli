package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/volcengine-tls/ve-tls-cli/internal/execution"
	"github.com/volcengine-tls/ve-tls-cli/internal/output"
)

func setDryRunRedactionEnv(t *testing.T) {
	t.Helper()
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "test-ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "test-sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://user:password-canary@example.com?token=endpoint-canary#fragment-canary")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))
}

func runDryRunRedactionCase(t *testing.T, args ...string) (map[string]any, map[string]any) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if code := Run(args, &stdout, &stderr); code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), "canary") || strings.Contains(stderr.String(), "canary") {
		t.Fatalf("dry-run leaked canary: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	var envelope map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	plan, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatalf("missing plan: %#v", envelope)
	}
	return envelope, plan
}

func TestRawDryRunRedactsPreviewURLQueryBodyAndTrace(t *testing.T) {
	setDryRunRedactionEnv(t)
	envelope, plan := runDryRunRedactionCase(t,
		"--dry-run", "--trace-dir", t.TempDir(), "raw", "--method", "POST",
		"--path", "/Action?token=path-canary&limit=2", "--query", "SessionToken=query-canary",
		"--query", `Filter={"nested":[{"Password":"nested-canary"}]}`,
		"--body", `{"WebhookSecret":"body-canary","Query":"* | select token","normal":"keep"}`,
	)
	if !strings.Contains(plan["path"].(string), "limit=2") {
		t.Fatalf("path = %#v", plan["path"])
	}
	fields := plan["redacted_fields"].([]any)
	want := []any{"body.WebhookSecret", "checks.endpoint.detail", "path.query.token", "query.Filter.nested[0].Password", "query.SessionToken"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("fields=%#v want=%#v", fields, want)
	}
	preview := plan["request_preview"].(map[string]any)
	body := preview["body"].(map[string]any)
	if body["WebhookSecret"] != "***" || body["Query"] != "* | select token" || body["normal"] != "keep" {
		t.Fatalf("body = %#v", body)
	}
	summary := envelope["summary"].(map[string]any)
	trace, err := os.ReadFile(summary["tracePath"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(trace), "canary") {
		t.Fatalf("trace leaked canary: %s", trace)
	}
}

func TestToolDryRunRedactsPutLogsPreview(t *testing.T) {
	setDryRunRedactionEnv(t)
	input := `{"query":{"TopicId":"tid"},"body":{"LogGroups":[{"LogTags":[{"Key":"Secret","Value":"tag-canary"}],"Logs":[{"Time":1710000000000,"Contents":[{"Key":"Password","Value":"body-canary"},{"Key":"level","Value":"keep"}]}]}]}}`
	_, plan := runDryRunRedactionCase(t, "--dry-run", "tool", "exec", "log.put", "--input", input)
	preview := plan["request_preview"].(map[string]any)
	if preview["body_source"] != "input_before_special_io" {
		t.Fatalf("preview = %#v", preview)
	}
	fields := plan["redacted_fields"].([]any)
	want := []any{"body.LogGroups[0].LogTags[0].Value", "body.LogGroups[0].Logs[0].Contents[0].Value", "checks.endpoint.detail"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("fields=%#v want=%#v", fields, want)
	}
}

func TestWorkflowDryRunRedactsEncodedQueryPreview(t *testing.T) {
	setDryRunRedactionEnv(t)
	_, plan := runDryRunRedactionCase(t, "--dry-run", "workflow", "exec", appResolveTopicIDsWorkflowID,
		"--input", `{"AppId":"{\"SessionToken\":\"workflow-canary\",\"Id\":\"keep\"}"}`)
	fields := plan["redacted_fields"].([]any)
	want := []any{"checks.endpoint.detail", "query.AppId.SessionToken"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("fields=%#v want=%#v", fields, want)
	}
}

func TestLegacyDryRunRedactsJSONLAndKeepsEncodedBodyHash(t *testing.T) {
	setDryRunRedactionEnv(t)
	body := []byte("{\"Source\":\"browser\",\"Contents\":{\"Password\":\"jsonl-canary\",\"level\":\"keep\"}}\n" +
		"{\"Source\":\"browser\",\"Contents\":{\"Token\":\"second-canary\"}}")
	meta := apiIOMeta{Method: "POST", Path: "/WebTracks", RequestFormat: requestFormatJSONL}
	_, encoded, _, handled, err := prepareSpecialIORequest(meta, nil, body)
	if err != nil || !handled {
		t.Fatalf("prepare special I/O: handled=%v err=%v", handled, err)
	}
	var stdout, stderr bytes.Buffer
	ctx := newContext(&stdout, &stderr, output.FormatJSON, "", "")
	defer ctx.Close()
	ctx.DryRun = true
	ctx.apiIOMeta = meta
	out, err := ctx.Do("POST", "/WebTracks", nil, nil, body)
	if err != nil {
		t.Fatal(err)
	}
	plan := out.(map[string]any)
	planJSON, _ := json.Marshal(plan)
	if strings.Contains(string(planJSON), "canary") || plan["body_sha256"] != sha256Hex(encoded) {
		t.Fatalf("plan = %s", planJSON)
	}
	want := []string{"body[0].Contents.Password", "body[1].Contents.Token", "checks.endpoint.detail"}
	if !reflect.DeepEqual(plan["redacted_fields"], want) {
		t.Fatalf("fields=%#v want=%#v", plan["redacted_fields"], want)
	}
	if !strings.Contains(string(body), "jsonl-canary") || plan["request_preview"].(map[string]any)["body_source"] != "input_before_special_io" {
		t.Fatal("body mutated or special input preview not marked")
	}
}

func TestLegacyDryRunOmitsUnparseableBodies(t *testing.T) {
	setDryRunRedactionEnv(t)
	for _, tc := range []struct {
		name   string
		body   string
		format requestFormat
	}{
		{"json", `{"Password":"broken-canary",`, requestFormatJSON},
		{"jsonl", "{\"Password\":\"first-canary\"}\n{\"Token\":\"broken-canary\",", requestFormatJSONL},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			ctx := newContext(&stdout, &stderr, output.FormatJSON, "", "")
			defer ctx.Close()
			plan := ctx.buildDryRunPlan("POST", "/Action", nil, nil, []byte(tc.body), []byte(tc.body), tc.format, false)
			encoded, _ := json.Marshal(plan)
			if strings.Contains(string(encoded), "canary") {
				t.Fatalf("plan leaked unparseable body: %s", encoded)
			}
			if got := plan["request_preview"].(map[string]any)["body"]; got != execution.UnparseableBodyPreview {
				t.Fatalf("body preview = %#v", got)
			}
		})
	}
}

func TestRawDryRunWithoutSensitiveInputOmitsRedactedFields(t *testing.T) {
	setDryRunRedactionEnv(t)
	t.Setenv("VOLCENGINE_ENDPOINT", "https://example.com")
	_, plan := runDryRunRedactionCase(t, "--dry-run", "raw", "--path", "/Action?limit=2", "--body", `{"Query":"* | select token"}`)
	if _, ok := plan["redacted_fields"]; ok {
		t.Fatalf("unexpected redacted_fields: %#v", plan["redacted_fields"])
	}
	if plan["path"] != "/Action?limit=2" {
		t.Fatalf("path changed: %#v", plan["path"])
	}
}
