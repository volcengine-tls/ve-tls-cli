package bundledskills

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	yaml "gopkg.in/yaml.v2"
)

func TestVolclogCoreBundledSkillCoversAgentEvaluationNeeds(t *testing.T) {
	root, err := Root()
	if err != nil {
		t.Fatalf("root: %v", err)
	}
	read := func(path string) string {
		data, err := fs.ReadFile(root, strings.TrimPrefix(path, "skills/"))
		if err == nil {
			return string(data)
		}
		data, err = os.ReadFile(filepath.Join(".", path))
		if err == nil {
			return string(data)
		}
		t.Fatalf("read %s: %v", path, err)
		return ""
	}

	skill := read("skills/volclog-core/SKILL.md")
	for _, want := range []string{
		"agent-only incremental knowledge",
		"Read `tool describe` or `workflow describe` first",
		"Do not use human shortcut commands as the primary agent flow.",
		"Do not repeat schema details that already exist in `tool describe` or `workflow describe`.",
		"prefer `volclog-agent` for agent or CI sessions",
		"contract_cache_hint",
		"volclog workflow describe <group.command>",
		"references/routing.md",
		"references/sops.md",
		"references/best-practices.md",
	} {
		if !strings.Contains(skill, want) {
			t.Fatalf("SKILL.md missing %q", want)
		}
	}

	routing := read("skills/volclog-core/references/routing.md")
	for _, want := range []string{
		"Intent",
		"Prefer",
		"log.ingest",
		"tool log.put",
		"log.describe-histogram-v1",
		"log.export-analysis",
		"interactive analysis",
		"full analysis row set",
		"Exact method/path is already known",
		"--verb list",
		"re-run `volclog tool list <group> --verb <user-intent-verb>`",
		"Human shortcut groups are for humans.",
	} {
		if !strings.Contains(routing, want) {
			t.Fatalf("routing.md missing %q", want)
		}
	}

	sops := read("skills/volclog-core/references/sops.md")
	for _, want := range []string{
		"log.ingest",
		"log.export",
		"log.export-analysis",
		"log.describe-histogram-v1",
		"HitCount",
		"Histogram.TotalCount",
		"interactive SQL exploration",
		"10-30 seconds",
		"Status",
		"Stop when",
		"validation query",
	} {
		if !strings.Contains(strings.ToLower(sops), strings.ToLower(want)) {
			t.Fatalf("sops.md missing %q", want)
		}
	}

	bestPractices := read("skills/volclog-core/references/best-practices.md")
	for _, want := range []string{
		"Runtime Signals",
		"Search, Histogram, And Analysis",
		"Error Object",
		"Profile And Credential Selection",
		"403 Forbidden",
		"tool / workflow / raw",
		"filter matched no value",
		"`log.search` itself supports plain search and SQL/analysis queries",
		"`log.describe-histogram-v1` is for time-distribution preview",
		"only for pure search queries",
		"`log.export-analysis` uses the same SearchLogs SQL/analysis `Query` syntax as `log.search`",
		"let CLI `deliveryMode` decide stdout vs `file_auto`",
		"`deliveryMode`",
		"`outputMode`",
		"`--jmes-filter` runs on the complete CLI envelope",
		"`execution.projection` is different: it runs on the raw result before envelope wrapping",
		"stdout returns literal `null` and the command still succeeds",
		"Failed envelopes use one flat `error` object",
		"`error.source`",
		"`error.kind`",
		"`error.code`",
		"`error.details`",
		"`volclog-agent`",
		"`HitCount`",
		"`Histogram.TotalCount`",
		"`ResultStatus=incomplete` means the service returned only a partial scan",
		"`--secrets-file`",
		"`context.secrets_file`",
		"VOLCENGINE_ACCESS_KEY_ID",
	} {
		if !strings.Contains(bestPractices, want) {
			t.Fatalf("best-practices.md missing %q", want)
		}
	}
}

func TestVolclogCoreTemplateStaysMachineReadable(t *testing.T) {
	type manifest struct {
		Skill         string            `yaml:"skill"`
		TargetDir     string            `yaml:"target_dir"`
		Principles    []string          `yaml:"principles"`
		Sources       map[string]string `yaml:"sources"`
		RenderTargets []string          `yaml:"render_targets"`
	}
	type routingFile struct {
		Intents []map[string]any `yaml:"intents"`
	}
	type workflowsFile struct {
		Workflows []map[string]any `yaml:"workflows"`
	}
	type recoveryFile struct {
		Recipes []map[string]any `yaml:"recipes"`
	}
	type trapsFile struct {
		Traps []map[string]any `yaml:"traps"`
	}

	load := func(rel string, out any) {
		data, err := os.ReadFile(filepath.Join(".", rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if err := yaml.Unmarshal(data, out); err != nil {
			t.Fatalf("unmarshal %s: %v", rel, err)
		}
	}

	var m manifest
	load("skill-template/volclog-core/manifest.yaml", &m)
	if m.Skill != "volclog-core" {
		t.Fatalf("unexpected skill name: %+v", m)
	}
	if m.TargetDir != "skills/volclog-core" {
		t.Fatalf("unexpected target dir: %+v", m)
	}
	foundCachePrinciple := false
	foundStatelessSecretPrinciple := false
	foundAgentEditionPrinciple := false
	for _, principle := range m.Principles {
		if principle == "respect_contract_cache_hint" {
			foundCachePrinciple = true
		}
		if principle == "host_managed_secret_injection_for_stateless_agents" {
			foundStatelessSecretPrinciple = true
		}
		if principle == "prefer_volclog_agent_when_available" {
			foundAgentEditionPrinciple = true
		}
	}
	if !foundCachePrinciple {
		t.Fatalf("manifest missing respect_contract_cache_hint principle: %+v", m)
	}
	if !foundStatelessSecretPrinciple {
		t.Fatalf("manifest missing host_managed_secret_injection_for_stateless_agents principle: %+v", m)
	}
	if !foundAgentEditionPrinciple {
		t.Fatalf("manifest missing prefer_volclog_agent_when_available principle: %+v", m)
	}
	for _, want := range []string{"routing", "workflows", "recovery", "traps"} {
		if _, ok := m.Sources[want]; !ok {
			t.Fatalf("manifest missing source %q: %+v", want, m)
		}
	}
	for _, want := range []string{"SKILL.md", "references/routing.md", "references/sops.md", "references/best-practices.md"} {
		found := false
		for _, target := range m.RenderTargets {
			if target == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("manifest missing render target %q: %+v", want, m)
		}
	}

	var routing routingFile
	load("skill-template/volclog-core/routing.yaml", &routing)
	if len(routing.Intents) == 0 {
		t.Fatal("routing template missing intents")
	}
	for _, entry := range routing.Intents {
		if _, ok := entry["keywords"]; !ok {
			t.Fatalf("routing intent missing keywords: %+v", entry)
		}
		if _, ok := entry["retry_with_verb"]; !ok {
			t.Fatalf("routing intent missing retry_with_verb: %+v", entry)
		}
	}

	var workflows workflowsFile
	load("skill-template/volclog-core/workflows.yaml", &workflows)
	if len(workflows.Workflows) == 0 {
		t.Fatal("workflows template missing workflows")
	}
	for _, entry := range workflows.Workflows {
		if _, ok := entry["output_strategy"]; !ok {
			t.Fatalf("workflow missing output_strategy: %+v", entry)
		}
		if _, ok := entry["fallback"]; !ok {
			t.Fatalf("workflow missing fallback: %+v", entry)
		}
	}

	var recovery recoveryFile
	load("skill-template/volclog-core/recovery.yaml", &recovery)
	if len(recovery.Recipes) == 0 {
		t.Fatal("recovery template missing recipes")
	}
	for _, entry := range recovery.Recipes {
		if _, ok := entry["error_code"]; !ok {
			t.Fatalf("recovery recipe missing error_code: %+v", entry)
		}
		if _, ok := entry["http_status"]; !ok {
			t.Fatalf("recovery recipe missing http_status: %+v", entry)
		}
		if _, ok := entry["retry_command"]; !ok {
			t.Fatalf("recovery recipe missing retry_command: %+v", entry)
		}
	}

	var traps trapsFile
	load("skill-template/volclog-core/traps.yaml", &traps)
	if len(traps.Traps) == 0 {
		t.Fatal("traps template missing traps")
	}
	for _, entry := range traps.Traps {
		if _, ok := entry["symptom"]; !ok {
			t.Fatalf("trap missing symptom: %+v", entry)
		}
		if _, ok := entry["fix"]; !ok {
			t.Fatalf("trap missing fix: %+v", entry)
		}
	}
	foundEnvOverrideTrap := false
	for _, entry := range traps.Traps {
		if trap, _ := entry["trap"].(string); trap == "env-creds-override-profile" {
			foundEnvOverrideTrap = true
			break
		}
	}
	if !foundEnvOverrideTrap {
		t.Fatalf("traps template missing env-creds-override-profile: %+v", traps)
	}
}
