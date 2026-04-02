package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFilterCapabilitiesByGroupAndAction(t *testing.T) {
	doc := apiCapabilitiesDoc{
		Commands: []apiCapabilityCommand{
			{Group: "log", Action: "SearchLogs", Method: "POST", Path: "/SearchLogs"},
			{Group: "project", Action: "DescribeProjects", Method: "GET", Path: "/DescribeProjects"},
			{Group: "project", Action: "DescribeProject", Method: "GET", Path: "/DescribeProject"},
		},
	}

	got, err := filterCapabilities(doc, "project", "DescribeProjects")
	if err != nil {
		t.Fatalf("filter error: %v", err)
	}
	if len(got.Commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(got.Commands))
	}
	if got.Commands[0].Group != "project" || got.Commands[0].Action != "DescribeProjects" {
		t.Fatalf("unexpected command: %+v", got.Commands[0])
	}
}

func TestFilterCapabilitiesActionAmbiguous(t *testing.T) {
	doc := apiCapabilitiesDoc{
		Commands: []apiCapabilityCommand{
			{Group: "project", Action: "Describe", Method: "GET", Path: "/DescribeProjects"},
			{Group: "topic", Action: "Describe", Method: "GET", Path: "/DescribeTopics"},
		},
	}

	_, err := filterCapabilities(doc, "", "Describe")
	if err == nil {
		t.Fatalf("expected ambiguous action error")
	}
}

func TestRunCapabilitiesIncludesV3MetaHints(t *testing.T) {
	out, err := runCapabilities(nil, []string{"--group", "project", "--action", "CreateProject"})
	if err != nil {
		t.Fatalf("run capabilities error: %v", err)
	}
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	s := string(b)
	for _, want := range []string{
		`"meta":`,
		`"contract_version":"stage1"`,
		`"hints_mode":"declarative_only"`,
		`"param_doc_source":"official_doc_preferred"`,
		`"supports_dry_run":true`,
		`"output_mode_hint":"envelope"`,
		`"risk_level":"high"`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in capabilities output", want)
		}
	}
	for _, notWant := range []string{`"schema_version":`, `"version":"stage1"`} {
		if strings.Contains(s, notWant) {
			t.Fatalf("capabilities output should not include %s: %s", notWant, s)
		}
	}
}

func TestCapabilitiesDefaultViewCompactHidesParams(t *testing.T) {
	out, err := runCapabilities(nil, []string{"--group", "project", "--action", "CreateProject"})
	if err != nil {
		t.Fatalf("run capabilities error: %v", err)
	}
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	s := string(b)
	for _, notWant := range []string{`"params":`, `"request_params_doc":`} {
		if strings.Contains(s, notWant) {
			t.Fatalf("compact view should hide %s: %s", notWant, s)
		}
	}
	for _, want := range []string{`"description":`, `"input_mode":`} {
		if !strings.Contains(s, want) {
			t.Fatalf("compact view should include %s: %s", want, s)
		}
	}
	for _, notWant := range []string{`"method":`, `"path":`} {
		if strings.Contains(s, notWant) {
			t.Fatalf("compact view should hide %s: %s", notWant, s)
		}
	}
}

func TestCapabilitiesFullViewIncludesParams(t *testing.T) {
	out, err := runCapabilities(nil, []string{"--group", "project", "--action", "CreateProject", "--view", "full"})
	if err != nil {
		t.Fatalf("run capabilities error: %v", err)
	}
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, `"params":`) {
		t.Fatalf("full view should include params: %s", s)
	}
	if !strings.Contains(s, `"request_params_doc":`) {
		t.Fatalf("full view should include request_params_doc: %s", s)
	}
	if !strings.Contains(s, `"group_title":"日志项目管理"`) {
		t.Fatalf("full view should include group_title: %s", s)
	}
	for _, notWant := range []string{`"ref":`, `"schema_version":`, `"version":"stage1"`} {
		if strings.Contains(s, notWant) {
			t.Fatalf("full view should not include %s: %s", notWant, s)
		}
	}
}

func TestCapabilitiesTextViewReturnsHumanReadableText(t *testing.T) {
	out, err := runCapabilities(nil, []string{"--group", "project", "--view", "text"})
	if err != nil {
		t.Fatalf("run capabilities error: %v", err)
	}
	s, ok := out.(string)
	if !ok {
		t.Fatalf("unexpected type: %T", out)
	}
	for _, want := range []string{
		"project (日志项目管理):\n",
		"DescribeProject",
		":",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("text view missing %q: %s", want, s)
		}
	}
	for _, notWant := range []string{"input:", "required flags:"} {
		if strings.Contains(s, notWant) {
			t.Fatalf("text view should stay concise and hide %q: %s", notWant, s)
		}
	}
}

func TestCapabilitiesGroupsViewReturnsGroupOverview(t *testing.T) {
	out, err := runCapabilities(nil, []string{"--view", "groups"})
	if err != nil {
		t.Fatalf("run capabilities error: %v", err)
	}
	s, ok := out.(string)
	if !ok {
		t.Fatalf("unexpected type: %T", out)
	}
	for _, want := range []string{
		"account (账号管理):",
		"账号管理相关接口",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("groups view missing %q: %s", want, s)
		}
	}
	if strings.Contains(s, "标签：") {
		t.Fatalf("groups view should stay concise and hide tags: %s", s)
	}
}

func TestUsageCapabilitiesDescribesDiscoveryEntryForAgents(t *testing.T) {
	text := usageCapabilities()
	for _, want := range []string{
		"面向 agent 与自动化程序的统一能力发现入口",
		"适合先做 group 粗分类，再做 action 筛选",
		"帮助调用方以低探索成本定位可执行接口",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in capabilities usage: %s", want, text)
		}
	}
}

func TestFilterCapabilitiesPreservesMeta(t *testing.T) {
	doc := apiCapabilitiesDoc{
		Version: "stage1",
		Meta: apiCapabilitiesMeta{
			SchemaVersion:   "v3",
			ContractVersion: "stage1",
			HintsMode:       "declarative_only",
			ParamDocSource:  "official_doc_preferred",
			SupportsDryRun:  true,
			OutputModeHint:  "envelope",
		},
		Commands: []apiCapabilityCommand{
			{
				Group:          "log",
				Action:         "SearchLogs",
				Method:         "POST",
				Path:           "/SearchLogs",
				SupportsDryRun: true,
				OutputModeHint: "envelope",
				RiskLevel:      "low",
			},
		},
	}
	got, err := filterCapabilities(doc, "log", "SearchLogs")
	if err != nil {
		t.Fatalf("filter error: %v", err)
	}
	if got.Meta.SchemaVersion != "v3" || got.Meta.ContractVersion != "stage1" || got.Meta.HintsMode != "declarative_only" || got.Meta.ParamDocSource != "official_doc_preferred" {
		t.Fatalf("unexpected meta: %+v", got.Meta)
	}
	if len(got.Commands) != 1 {
		t.Fatalf("commands=%d", len(got.Commands))
	}
	if !got.Commands[0].SupportsDryRun || got.Commands[0].OutputModeHint != "envelope" || got.Commands[0].RiskLevel != "low" {
		t.Fatalf("unexpected command hints: %+v", got.Commands[0])
	}
}

func TestRunCapabilitiesWithHintsFileOverride(t *testing.T) {
	tmp := t.TempDir()
	hintsPath := filepath.Join(tmp, "hints.json")
	hints := `{
  "rules": [
    {
      "group": "log",
      "action": "SearchLogs",
      "risk_level": "high"
    }
  ]
}`
	if err := os.WriteFile(hintsPath, []byte(hints), 0o644); err != nil {
		t.Fatalf("write hints file failed: %v", err)
	}

	out, err := runCapabilities(nil, []string{"--group", "log", "--action", "SearchLogs", "--hints-file", hintsPath})
	if err != nil {
		t.Fatalf("run capabilities error: %v", err)
	}
	doc, ok := out.(apiCapabilitiesDoc)
	if !ok {
		t.Fatalf("unexpected type: %T", out)
	}
	if len(doc.Commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(doc.Commands))
	}
	cmd := doc.Commands[0]
	if cmd.RiskLevel != "high" {
		t.Fatalf("override not applied: risk=%q", cmd.RiskLevel)
	}
}

func TestCapabilitiesAutoLoadsHintsFileFromProjectConfig(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, ".volclog"), 0o700); err != nil {
		t.Fatalf("mkdir .volclog failed: %v", err)
	}
	hintsPath := filepath.Join(tmp, "hints.json")
	hints := `{
  "rules": [
    {
      "group": "log",
      "action": "SearchLogs",
      "risk_level": "high"
    }
  ]
}`
	if err := os.WriteFile(hintsPath, []byte(hints), 0o644); err != nil {
		t.Fatalf("write hints failed: %v", err)
	}
	projectCfgPath := filepath.Join(tmp, ".volclog", "cli.config.json")
	projectCfg := `{"hints_file":"` + hintsPath + `"}`
	if err := os.WriteFile(projectCfgPath, []byte(projectCfg), 0o600); err != nil {
		t.Fatalf("write project config failed: %v", err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"capabilities", "--group", "log", "--action", "SearchLogs"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var doc apiCapabilitiesDoc
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal output failed: %v, out=%q", err, stdout.String())
	}
	if len(doc.Commands) != 1 {
		t.Fatalf("commands=%d", len(doc.Commands))
	}
	if doc.Commands[0].RiskLevel != "high" {
		t.Fatalf("auto hints not applied: risk=%q", doc.Commands[0].RiskLevel)
	}
}

func TestCapabilitiesAutoLoadsHintsFileFromEnv(t *testing.T) {
	tmp := t.TempDir()
	hintsPath := filepath.Join(tmp, "hints_env.json")
	hints := `{
  "rules": [
    {
      "group": "log",
      "action": "SearchLogs",
      "risk_level": "high"
    }
  ]
}`
	if err := os.WriteFile(hintsPath, []byte(hints), 0o644); err != nil {
		t.Fatalf("write env hints failed: %v", err)
	}
	t.Setenv("VOLCLOG_HINTS_FILE", hintsPath)

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"capabilities", "--group", "log", "--action", "SearchLogs"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var doc apiCapabilitiesDoc
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal output failed: %v, out=%q", err, stdout.String())
	}
	if len(doc.Commands) != 1 {
		t.Fatalf("commands=%d", len(doc.Commands))
	}
	if doc.Commands[0].RiskLevel != "high" {
		t.Fatalf("env hints not applied: risk=%q", doc.Commands[0].RiskLevel)
	}
}

func TestCapabilitiesHintsFlagOverridesEnvAndProject(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, ".volclog"), 0o700); err != nil {
		t.Fatalf("mkdir .volclog failed: %v", err)
	}

	projectHintsPath := filepath.Join(tmp, "hints_project.json")
	envHintsPath := filepath.Join(tmp, "hints_env.json")
	flagHintsPath := filepath.Join(tmp, "hints_flag.json")
	for p, body := range map[string]string{
		projectHintsPath: `{"rules":[{"group":"log","action":"SearchLogs","risk_level":"high"}]}`,
		envHintsPath:     `{"rules":[{"group":"log","action":"SearchLogs","risk_level":"high"}]}`,
		flagHintsPath:    `{"rules":[{"group":"log","action":"SearchLogs","risk_level":"low"}]}`,
	} {
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("write hints file failed: %v", err)
		}
	}
	projectCfgPath := filepath.Join(tmp, ".volclog", "cli.config.json")
	projectCfg := `{"hints_file":"` + projectHintsPath + `"}`
	if err := os.WriteFile(projectCfgPath, []byte(projectCfg), 0o600); err != nil {
		t.Fatalf("write project config failed: %v", err)
	}
	t.Setenv("VOLCLOG_HINTS_FILE", envHintsPath)

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"capabilities", "--group", "log", "--action", "SearchLogs", "--hints-file", flagHintsPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var doc apiCapabilitiesDoc
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal output failed: %v, out=%q", err, stdout.String())
	}
	if len(doc.Commands) != 1 {
		t.Fatalf("commands=%d", len(doc.Commands))
	}
	if doc.Commands[0].RiskLevel != "low" {
		t.Fatalf("flag hints should win: risk=%q", doc.Commands[0].RiskLevel)
	}
}

func TestRunCapabilitiesTextViewWritesPlainText(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"capabilities", "--group", "project", "--view", "text"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unexpected code: %d, stderr=%s", code, stderr.String())
	}
	if strings.HasPrefix(stdout.String(), "\"") {
		t.Fatalf("unexpected json-encoded string: %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "project (日志项目管理):\n") {
		t.Fatalf("unexpected output: %q", stdout.String())
	}
}
