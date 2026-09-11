//go:build human

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/volcengine-tls/ve-tls-cli/internal/config"
	"github.com/volcengine-tls/ve-tls-cli/internal/output"
)

type indexLogRequestBaselineCase struct {
	name                 string
	group                string
	args                 []string
	useShanghaiProfile   bool
	wantResolvedEndpoint string
	wantResolvedRegion   string
}

func TestUnificationBaselineRequestIndexLog(t *testing.T) {
	const (
		indexBodyPrimary   = `{"EnableAutoIndex":true,"FullText":{"CaseSensitive":false,"Delimiter":" ,","IncludeChinese":true},"KeyValue":[],"MaxTextLen":2048}`
		indexBodySecondary = `{"EnableAutoIndex":false,"FullText":{"CaseSensitive":true,"Delimiter":"|","IncludeChinese":false},"KeyValue":[],"MaxTextLen":4096}`
	)

	cases := []indexLogRequestBaselineCase{
		{
			name:  "index_get",
			group: "index",
			args:  []string{"get", "--topic-id", "index-get-topic-id"},
		},
		{
			name:  "index_create_body",
			group: "index",
			args:  []string{"create", "--topic-id", "index-create-body-topic-id", "--body", indexBodyPrimary},
		},
		{
			name:  "index_create_request",
			group: "index",
			args:  []string{"create", "--topic-id", "index-create-request-topic-id", "--request", indexBodySecondary},
		},
		{
			name:  "index_create_body_then_request_last_wins",
			group: "index",
			args: []string{
				"create",
				"--topic-id", "index-create-body-then-request-topic-id",
				"--body", indexBodyPrimary,
				"--request", indexBodySecondary,
			},
		},
		{
			name:  "index_create_request_then_body_last_wins",
			group: "index",
			args: []string{
				"create",
				"--topic-id", "index-create-request-then-body-topic-id",
				"--request", indexBodySecondary,
				"--body", indexBodyPrimary,
			},
		},
		{
			name:  "index_modify",
			group: "index",
			args:  []string{"modify", "--topic-id", "index-modify-topic-id", "--request", indexBodySecondary},
		},
		{
			name:  "log_search_flags_only_default_limit",
			group: "log",
			args: []string{
				"search",
				"--topic-id", "log-flags-topic-id",
				"--query", "level:error",
				"--from", "1710374400000",
				"--to", "1710378000000",
			},
		},
		{
			name:  "log_search_request_only",
			group: "log",
			args: []string{
				"search",
				"--request", `{"TopicId":"log-request-topic-id","Query":"service:request","StartTime":1710374400000,"EndTime":1710378000000,"Limit":7,"Context":"request-context","Sort":"desc","Offset":2,"HighLight":true,"AccurateQuery":false,"MustComplete":true}`,
			},
		},
		{
			name:  "log_search_flags_override_request",
			group: "log",
			args: []string{
				"search",
				"--request", `{"TopicId":"request-topic-id","Query":"request-query","StartTime":111,"EndTime":222,"Limit":9,"Context":"request-context","Sort":"desc","Offset":1,"HighLight":false,"AccurateQuery":true,"MustComplete":true,"Extra":"preserved"}`,
				"--topic-id", "flag-topic-id",
				"--query", "flag-query",
				"--from", "1710374400000",
				"--to", "1710378000000",
				"--limit", "25",
				"--context", "flag-context",
				"--sort", "asc",
				"--offset", "3",
				"--highlight",
				"--no-accurate-query",
				"--no-must-complete",
			},
		},
		{
			name:  "log_search_plain_query_fields",
			group: "log",
			args: []string{
				"search",
				"--topic-id", "log-query-shape-topic-id",
				"--query", "*",
				"--from", "1710374400000",
				"--to", "1710378000000",
			},
		},
		{
			name:  "log_search_analysis_query_fields",
			group: "log",
			args: []string{
				"search",
				"--topic-id", "log-query-shape-topic-id",
				"--query", "* | select count(*) as count limit 5",
				"--from", "1710374400000",
				"--to", "1710378000000",
			},
		},
		{
			name:                 "profile_region_injection_index_get",
			group:                "index",
			args:                 []string{"get", "--topic-id", "profile-region-topic-id"},
			useShanghaiProfile:   true,
			wantResolvedEndpoint: "https://tls-cn-shanghai.volces.com",
			wantResolvedRegion:   "cn-shanghai",
		},
	}

	got := make(map[string]any, len(cases))
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			profile := ""
			if tc.useShanghaiProfile {
				profile = setIndexLogShanghaiProfileRuntime(t)
			} else {
				setHumanRequestBaselineRuntime(t)
			}
			got[tc.name] = captureIndexLogRequestBaseline(t, tc, profile)
		})
	}

	goldenPath := filepath.Join("testdata", "unification", "index_log_request_baseline.json")
	raw, err := os.ReadFile(goldenPath)
	if err != nil {
		captured, marshalErr := json.MarshalIndent(got, "", "  ")
		if marshalErr != nil {
			t.Fatalf("marshal captured index/log request baseline: %v", marshalErr)
		}
		t.Fatalf("read index/log request baseline golden %q: %v\ncaptured baseline:\n%s\n", goldenPath, err, captured)
	}

	var want map[string]any
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("decode index/log request baseline golden %q: %v", goldenPath, err)
	}
	assertCanonicalHumanRequestGolden(t, goldenPath, raw, want)
	assertHumanRequestBaselineCases(t, got, want)
	assertIndexLogProfileChecksBaseline(t, got, want)
}

func TestUnificationBaselineRequestIndexLogErrors(t *testing.T) {
	const validIndexBody = `{"FullText":{"CaseSensitive":false,"Delimiter":" ,","IncludeChinese":true},"KeyValue":[]}`

	cases := []struct {
		name  string
		group string
		args  []string
		want  string
	}{
		{
			name:  "index_delete_unknown_command",
			group: "index",
			args:  []string{"delete", "--topic-id", "index-delete-topic-id"},
			want:  "unknown index command: delete",
		},
		{
			name:  "index_get_missing_topic",
			group: "index",
			args:  []string{"get"},
			want:  "missing --topic-id",
		},
		{
			name:  "index_create_missing_topic",
			group: "index",
			args:  []string{"create", "--body", validIndexBody},
			want:  "missing --topic-id",
		},
		{
			name:  "index_create_missing_body",
			group: "index",
			args:  []string{"create", "--topic-id", "index-missing-body-topic-id"},
			want:  "missing --body",
		},
		{
			name:  "index_create_body_non_object",
			group: "index",
			args:  []string{"create", "--topic-id", "index-array-topic-id", "--body", `["not","an","object"]`},
			want:  "index body must be JSON object",
		},
		{
			name:  "index_create_malformed_json",
			group: "index",
			args:  []string{"create", "--topic-id", "index-malformed-topic-id", "--body", `{"FullText":`},
			want:  "unexpected end of JSON input",
		},
		{
			name:  "index_create_unknown_schema_field",
			group: "index",
			args: []string{
				"create",
				"--topic-id", "index-unknown-field-topic-id",
				"--body", `{"EnableAutoIndexes":true,"FullText":{},"KeyValue":[]}`,
			},
			want: "unknown body field: EnableAutoIndexes (did you mean EnableAutoIndex?)",
		},
		{
			name:  "index_create_enable_phrase_index_rejected",
			group: "index",
			args: []string{
				"create",
				"--topic-id", "index-phrase-field-topic-id",
				"--body", `{"EnablePhraseIndex":true,"FullText":{},"KeyValue":[]}`,
			},
			want: "unknown body field: EnablePhraseIndex",
		},
		{
			name:  "log_search_missing_topic",
			group: "log",
			args:  []string{"search", "--query", "*", "--from", "1710374400000", "--to", "1710378000000"},
			want:  "missing --topic-id or request.TopicId",
		},
		{
			name:  "log_search_missing_query",
			group: "log",
			args:  []string{"search", "--topic-id", "log-missing-query-topic-id", "--from", "1710374400000", "--to", "1710378000000"},
			want:  "missing --query or request.Query",
		},
		{
			name:  "log_search_missing_from",
			group: "log",
			args:  []string{"search", "--topic-id", "log-missing-from-topic-id", "--query", "*", "--to", "1710378000000"},
			want:  "missing --from or request.StartTime",
		},
		{
			name:  "log_search_missing_to",
			group: "log",
			args:  []string{"search", "--topic-id", "log-missing-to-topic-id", "--query", "*", "--from", "1710374400000"},
			want:  "missing --to or request.EndTime",
		},
		{
			name:  "log_search_invalid_from_time",
			group: "log",
			args:  []string{"search", "--topic-id", "log-invalid-time-topic-id", "--query", "*", "--from", "not-a-time", "--to", "1710378000000"},
			want:  "unsupported time format: not-a-time",
		},
		{
			name:  "log_search_malformed_request_json",
			group: "log",
			args:  []string{"search", "--request", `{"TopicId":`},
			want:  "unexpected end of JSON input",
		},
		{
			name:  "log_search_request_non_object",
			group: "log",
			args:  []string{"search", "--request", `["not","an","object"]`},
			want:  "json must be object",
		},
		{
			name:  "log_search_analysis_limit_conflict",
			group: "log",
			args: []string{
				"search",
				"--topic-id", "log-analysis-limit-topic-id",
				"--query", "* | select count(*)",
				"--from", "1710374400000",
				"--to", "1710378000000",
				"--limit", "10",
			},
			want: "for analysis query, do not use --limit/--context/--sort/--offset; use SQL limit/offset in --query (analysis does not support Context pagination)",
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

			var (
				plan any
				err  error
			)
			switch tc.group {
			case "index":
				plan, err = runIndex(ctx, tc.args)
			case "log":
				plan, err = runLog(ctx, tc.args)
			default:
				t.Fatalf("case %q has unsupported group %q", tc.name, tc.group)
			}
			if err == nil {
				t.Fatalf("expected error %q, got plan %#v", tc.want, plan)
			}
			if err.Error() != tc.want {
				t.Fatalf("error mismatch: got %q, want %q", err.Error(), tc.want)
			}
		})
	}
}

func captureIndexLogRequestBaseline(t *testing.T, tc indexLogRequestBaselineCase, profile string) map[string]any {
	t.Helper()

	var stdout, stderr bytes.Buffer
	ctx := newContext(&stdout, &stderr, output.FormatJSON, profile, "")
	ctx.DryRun = true
	defer ctx.Close()

	var (
		out any
		err error
	)
	switch tc.group {
	case "index":
		out, err = runIndex(ctx, tc.args)
	case "log":
		out, err = runLog(ctx, tc.args)
	default:
		t.Fatalf("case %q has unsupported group %q", tc.name, tc.group)
	}
	if err != nil {
		t.Fatalf("case %q capture request: %v stdout=%q stderr=%q", tc.name, err, stdout.String(), stderr.String())
	}
	plan, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("case %q dry-run result has type %T, want object", tc.name, out)
	}
	preview, ok := plan["request_preview"].(map[string]any)
	if !ok {
		t.Fatalf("case %q dry-run result missing request_preview object: %#v", tc.name, plan)
	}

	stable := map[string]any{
		"action": ctx.Action,
		"data": map[string]any{
			"request_preview": map[string]any{
				"method": plan["method"],
				"path":   plan["path"],
				"query":  preview["query"],
				"header": plan["headers_redacted"],
				"body":   preview["body"],
			},
		},
	}
	if tc.useShanghaiProfile {
		stable["checks"] = assertIndexLogResolvedProfileChecks(
			t,
			tc.name,
			plan,
			tc.wantResolvedEndpoint,
			tc.wantResolvedRegion,
		)
	}

	raw, err := json.Marshal(stable)
	if err != nil {
		t.Fatalf("case %q normalize stable request: %v", tc.name, err)
	}
	var normalized map[string]any
	if err := json.Unmarshal(raw, &normalized); err != nil {
		t.Fatalf("case %q decode normalized stable request: %v", tc.name, err)
	}
	return normalized
}

func assertIndexLogProfileChecksBaseline(t *testing.T, got, want map[string]any) {
	t.Helper()

	const caseName = "profile_region_injection_index_get"
	gotCase, ok := got[caseName].(map[string]any)
	if !ok {
		t.Errorf("case %q actual envelope has type %T, want object", caseName, got[caseName])
		return
	}
	wantCase, ok := want[caseName].(map[string]any)
	if !ok {
		t.Errorf("case %q golden envelope has type %T, want object", caseName, want[caseName])
		return
	}
	assertHumanRequestBaselineField(t, caseName, "checks", gotCase["checks"], wantCase["checks"])
}

func assertIndexLogResolvedProfileChecks(t *testing.T, caseName string, plan map[string]any, wantEndpoint, wantRegion string) map[string]any {
	t.Helper()

	checks, ok := plan["checks"].([]map[string]any)
	if !ok {
		t.Fatalf("case %q dry-run checks have type %T, want []map[string]any", caseName, plan["checks"])
	}
	details := make(map[string]string, len(checks))
	for _, check := range checks {
		name, _ := check["name"].(string)
		detail, _ := check["detail"].(string)
		okValue, _ := check["ok"].(bool)
		if name == "endpoint" || name == "region" {
			if !okValue {
				t.Fatalf("case %q dry-run %s check is not ok: %#v", caseName, name, check)
			}
			details[name] = detail
		}
	}
	if got := details["endpoint"]; got != wantEndpoint {
		t.Fatalf("case %q endpoint check mismatch: got %q, want %q", caseName, got, wantEndpoint)
	}
	if got := details["region"]; got != wantRegion {
		t.Fatalf("case %q region check mismatch: got %q, want %q", caseName, got, wantRegion)
	}
	return map[string]any{
		"endpoint": details["endpoint"],
		"region":   details["region"],
	}
}

func setIndexLogShanghaiProfileRuntime(t *testing.T) string {
	t.Helper()

	const profileName = "baseline-shanghai"
	cfg := config.DefaultConfig()
	cfg.CurrentProfile = "not-the-selected-profile"
	cfg.Profiles[profileName] = config.Profile{
		Mode:            config.AuthModeAK,
		AccessKeyID:     "profile-placeholder-ak",
		SecretAccessKey: "profile-placeholder-sk",
		Region:          "cn-shanghai",
		Endpoint:        "https://tls-cn-shanghai.volces.com",
		TimeoutSeconds:  60,
	}
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := config.Save(cfg, cfgPath); err != nil {
		t.Fatalf("save profile-region baseline config: %v", err)
	}

	t.Setenv("VOLCLOG_CONFIG", cfgPath)
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "")
	t.Setenv("VOLCENGINE_TOKEN", "")
	t.Setenv("VOLCENGINE_REGION", "")
	t.Setenv("VOLCENGINE_ENDPOINT", "")
	for _, key := range []string{
		"AWS_ACCESS_KEY_ID",
		"AWS_SECRET_ACCESS_KEY",
		"AWS_SESSION_TOKEN",
	} {
		t.Setenv(key, "")
	}
	return profileName
}
