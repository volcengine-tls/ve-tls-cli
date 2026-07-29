//go:build human

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/volcengine-tls/ve-tls-cli/internal/output"
)

type humanRequestBaselineCase struct {
	name  string
	group string
	args  []string
}

func TestUnificationBaselineHumanRequests(t *testing.T) {
	cases := []humanRequestBaselineCase{
		{
			name:  "project_list_common_query_flags",
			group: "project",
			args: []string{
				"list",
				"--page-number", "3",
				"--page-size", "25",
				"--project-name", "baseline-project",
				"--project-id", "project-list-id",
				"--fuzzy-search-key", "baseline",
				"--description", "baseline description",
				"--is-full-name",
				"--iam-project-name", "iam-baseline",
				"--tags", `[{"Key":"env","Value":"test"}]`,
				"--favourite",
				"--topic-types", "log,metric",
			},
		},
		{
			name:  "project_get",
			group: "project",
			args:  []string{"get", "--project-id", "project-get-id", "--topic-types", "log"},
		},
		{
			name:  "project_create_resolved_region",
			group: "project",
			args: []string{
				"create",
				"--project-name", "baseline-project",
				"--description", "created by baseline",
			},
		},
		{
			name:  "project_modify",
			group: "project",
			args: []string{
				"modify",
				"--project-id", "project-modify-id",
				"--project-name", "renamed-project",
				"--description", "modified by baseline",
				"--favourite",
			},
		},
		{
			name:  "project_delete",
			group: "project",
			args:  []string{"delete", "--project-id", "project-delete-id"},
		},
		{
			name:  "topic_list",
			group: "topic",
			args: []string{
				"list",
				"--page-number", "4",
				"--page-size", "50",
				"--project-id", "topic-list-project-id",
				"--topic-name", "baseline-topic",
				"--cursor", "cursor-1",
				"--region", "cn-beijing",
				"--fuzzy-search-key", "topic",
				"--description", "topic description",
				"--tags", `[{"Key":"tier","Value":"baseline"}]`,
				"--no-is-full-name",
				"--favourite",
				"--order-by-project",
			},
		},
		{
			name:  "topic_get",
			group: "topic",
			args:  []string{"get", "--topic-id", "topic-get-id"},
		},
		{
			name:  "topic_create_defaults",
			group: "topic",
			args: []string{
				"create",
				"--project-id", "topic-create-project-id",
				"--topic-name", "baseline-topic",
			},
		},
		{
			name:  "topic_create_flags_override_request",
			group: "topic",
			args: []string{
				"create",
				"--request", `{"ProjectId":"request-project","TopicName":"request-topic","Description":"request-description","Ttl":7,"ShardCount":4,"EnableTracking":true}`,
				"--project-id", "flag-project",
				"--topic-name", "flag-topic",
				"--description", "flag-description",
				"--ttl", "60",
				"--shard-count", "8",
				"--disable-tracking",
			},
		},
		{
			name:  "topic_modify_boolean_unset",
			group: "topic",
			args: []string{
				"modify",
				"--topic-id", "topic-modify-unset-id",
				"--description", "boolean remains absent",
			},
		},
		{
			name:  "topic_modify_boolean_explicit_false",
			group: "topic",
			args: []string{
				"modify",
				"--topic-id", "topic-modify-false-id",
				"--no-auto-split",
			},
		},
		{
			name:  "topic_modify_boolean_explicit_true",
			group: "topic",
			args: []string{
				"modify",
				"--topic-id", "topic-modify-true-id",
				"--auto-split",
				"--max-split-shard", "16",
			},
		},
		{
			name:  "topic_delete",
			group: "topic",
			args:  []string{"delete", "--topic-id", "topic-delete-id"},
		},
	}

	got := make(map[string]any, len(cases))
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			setHumanRequestBaselineRuntime(t)
			got[tc.name] = captureHumanRequestBaseline(t, tc)
		})
	}

	goldenPath := filepath.Join("testdata", "unification", "human_request_baseline.json")
	raw, err := os.ReadFile(goldenPath)
	if err != nil {
		captured, marshalErr := json.MarshalIndent(got, "", "  ")
		if marshalErr != nil {
			t.Fatalf("marshal captured human request baseline: %v", marshalErr)
		}
		t.Fatalf("read human request baseline golden %q: %v\ncaptured baseline:\n%s\n", goldenPath, err, captured)
	}

	var want map[string]any
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("decode human request baseline golden %q: %v", goldenPath, err)
	}
	assertCanonicalHumanRequestGolden(t, goldenPath, raw, want)
	assertHumanRequestBaselineCases(t, got, want)
}

func TestUnificationBaselineTopicIdentityConflict(t *testing.T) {
	setHumanRequestBaselineRuntime(t)

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"topic", "list",
		"--topic-name", "baseline-topic",
		"--topic-id", "baseline-topic-id",
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected TopicName/TopicId conflict, got exit=0 stdout=%q stderr=%q", stdout.String(), stderr.String())
	}

	var envelope map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("decode TopicName/TopicId conflict envelope: %v stdout=%q stderr=%q", err, stdout.String(), stderr.String())
	}
	errObject, ok := envelope["error"].(map[string]any)
	if !ok {
		t.Fatalf("TopicName/TopicId conflict missing error object: %#v", envelope)
	}
	const want = "TopicName and TopicId cannot be provided together"
	message, _ := errObject["message"].(string)
	if !strings.Contains(message, want) {
		t.Fatalf("TopicName/TopicId conflict message mismatch: got %q, want substring %q", message, want)
	}
}

func setHumanRequestBaselineRuntime(t *testing.T) {
	t.Helper()
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "baseline-ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "baseline-sk")
	t.Setenv("VOLCENGINE_TOKEN", "")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))
}

func captureHumanRequestBaseline(t *testing.T, tc humanRequestBaselineCase) map[string]any {
	t.Helper()

	// Run currently rejects public --dry-run for human project/topic groups
	// before dispatch. Exercise the same production parsers and Context.Do
	// dry-run seam directly so this characterization remains network-free.
	var stdout, stderr bytes.Buffer
	ctx := newContext(&stdout, &stderr, output.FormatJSON, "", "")
	ctx.DryRun = true
	defer ctx.Close()

	var (
		plan any
		err  error
	)
	switch tc.group {
	case "project":
		plan, err = runProject(ctx, tc.args)
	case "topic":
		plan, err = runTopic(ctx, tc.args)
	default:
		t.Fatalf("case %q has unsupported human group %q", tc.name, tc.group)
	}
	if err != nil {
		t.Fatalf("case %q capture request: %v stdout=%q stderr=%q", tc.name, err, stdout.String(), stderr.String())
	}

	envelope, err := buildAPIEnvelope(ctx, tc.group, plan, "stdout", "", output.FormatJSON)
	if err != nil {
		t.Fatalf("case %q build success envelope: %v", tc.name, err)
	}
	return stableHumanRequestEnvelope(t, tc.name, envelope)
}

func stableHumanRequestEnvelope(t *testing.T, caseName string, envelope map[string]any) map[string]any {
	t.Helper()

	data, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatalf("case %q success envelope missing data object: %#v", caseName, envelope)
	}
	preview, ok := data["request_preview"].(map[string]any)
	if !ok {
		t.Fatalf("case %q success envelope missing data.request_preview: %#v", caseName, data)
	}
	stable := map[string]any{
		"action": envelope["action"],
		"data": map[string]any{
			"request_preview": map[string]any{
				"method": data["method"],
				"path":   data["path"],
				"query":  preview["query"],
				"header": data["headers_redacted"],
				"body":   preview["body"],
			},
		},
	}

	raw, err := json.Marshal(stable)
	if err != nil {
		t.Fatalf("case %q normalize stable request envelope: %v", caseName, err)
	}
	var normalized map[string]any
	if err := json.Unmarshal(raw, &normalized); err != nil {
		t.Fatalf("case %q decode normalized stable request envelope: %v", caseName, err)
	}
	return normalized
}

func assertCanonicalHumanRequestGolden(t *testing.T, path string, raw []byte, decoded map[string]any) {
	t.Helper()
	canonicalLF, err := json.MarshalIndent(decoded, "", "  ")
	if err != nil {
		t.Fatalf("marshal canonical human request baseline golden %q: %v", path, err)
	}
	canonicalLF = append(canonicalLF, '\n')
	if !canonicalHumanRequestGoldenMatches(raw, canonicalLF) {
		t.Errorf("human request baseline golden %q must use stable key ordering, 2-space indentation, and one trailing newline", path)
	}
}

func canonicalHumanRequestGoldenMatches(raw, canonicalLF []byte) bool {
	canonicalCRLF := bytes.ReplaceAll(canonicalLF, []byte("\n"), []byte("\r\n"))
	return bytes.Equal(raw, canonicalLF) || bytes.Equal(raw, canonicalCRLF)
}

func TestUnificationBaselineHumanRequestsCanonicalCRLF(t *testing.T) {
	canonical := []byte("{\n  \"a\": 1\n}\n")
	crlf := bytes.ReplaceAll(canonical, []byte("\n"), []byte("\r\n"))
	if !canonicalHumanRequestGoldenMatches(crlf, canonical) {
		t.Fatal("CRLF golden should match canonical LF JSON")
	}
	mixed := []byte("{\r\n  \"a\": 1\n}\r\n")
	if canonicalHumanRequestGoldenMatches(mixed, canonical) {
		t.Fatal("golden with mixed LF and CRLF line endings must remain non-canonical")
	}
	if canonicalHumanRequestGoldenMatches(append(crlf, '\r', '\n'), canonical) {
		t.Fatal("golden with an extra trailing newline must remain non-canonical")
	}
}

func assertHumanRequestBaselineCases(t *testing.T, got, want map[string]any) {
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
		t.Errorf("human request baseline case set mismatch: missing=%v extra=%v", missing, extra)
	}

	names := make([]string, 0, len(got))
	for name := range got {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		gotCase := got[name]
		wantCase, ok := want[name]
		if !ok {
			continue
		}
		assertHumanRequestBaselineCase(t, name, gotCase, wantCase)
	}
}

func assertHumanRequestBaselineCase(t *testing.T, name string, gotValue, wantValue any) {
	t.Helper()

	got, ok := gotValue.(map[string]any)
	if !ok {
		t.Errorf("case %q actual envelope has type %T, want object", name, gotValue)
		return
	}
	want, ok := wantValue.(map[string]any)
	if !ok {
		t.Errorf("case %q golden envelope has type %T, want object", name, wantValue)
		return
	}
	assertHumanRequestBaselineField(t, name, "action", got["action"], want["action"])

	gotPreview := humanRequestPreviewForComparison(t, name, "actual", got)
	wantPreview := humanRequestPreviewForComparison(t, name, "golden", want)
	if gotPreview == nil || wantPreview == nil {
		return
	}
	for _, field := range []string{"method", "path", "query", "header", "body"} {
		assertHumanRequestBaselineField(t, name, "data.request_preview."+field, gotPreview[field], wantPreview[field])
	}
}

func humanRequestPreviewForComparison(t *testing.T, name, source string, envelope map[string]any) map[string]any {
	t.Helper()
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Errorf("case %q %s envelope field data has type %T, want object", name, source, envelope["data"])
		return nil
	}
	preview, ok := data["request_preview"].(map[string]any)
	if !ok {
		t.Errorf("case %q %s envelope field data.request_preview has type %T, want object", name, source, data["request_preview"])
		return nil
	}
	return preview
}

func assertHumanRequestBaselineField(t *testing.T, name, field string, got, want any) {
	t.Helper()
	if reflect.DeepEqual(got, want) {
		return
	}
	t.Errorf("case %q field %q mismatch:\n  got: %s\n want: %s", name, field, renderHumanRequestBaselineValue(got), renderHumanRequestBaselineValue(want))
}

func renderHumanRequestBaselineValue(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return "<unmarshalable>"
	}
	return string(raw)
}
