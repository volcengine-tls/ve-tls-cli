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
		"Treat the execution environment as unknown until the available runtime selectors are confirmed.",
		"dominant generic operating model for `volclog`",
		"Treat profile configuration as a runtime-selector problem, not a region-discovery ritual.",
		"Think in runtime selectors: active profile, explicit `--profile <name>`, one-shot `--secrets-file`, `context.secrets_file`, or process-scoped environment credentials.",
		"Do not guess command names, tool ids, workflow ids, JSON fields, or output shape.",
		"Canonical Agent Loop",
		"Discover -> Describe -> Exec -> Read Result",
		"Operating Stance",
		"Key Rules",
		"Error Recovery Quick Map",
		"Large Result Handling",
		"Treat `tool describe` or `workflow describe` as the contract truth source.",
		"Keep surface choice and delivery choice separate.",
		"Prefer structured JSON input such as `--input '{...}'`",
		"Go back to `describe` and fix shape or selector problems first.",
		"Reference Escalation",
		"Do not read every reference file by default.",
		"`volclog doctor` checks host/runtime configuration",
		"Read `tool describe` or `workflow describe` first",
		"Do not use human shortcut commands as the primary agent flow.",
		"Do not repeat schema details that already exist in `tool describe` or `workflow describe`.",
		"Do not assume the active or default profile is the right selector for the task.",
		"Run `volclog configure list` only when local profile discovery is actually relevant.",
		"run `volclog tool list <group>` or `volclog workflow list <group>` before guessing",
		"Do not pipe `volclog` output into `jq` or `grep` just to rediscover schema or field paths.",
		"prefer `volclog` for agent or CI sessions",
		"contract_cache_hint.safe_scope",
		"contract_cache_hint.refresh_when",
		"Prefer `--dry-run` before any write or destructive change, but only on `raw`, `tool exec`, or `workflow exec`.",
		"Treat `--dry-run` as contract or plan validation, not proof that the business query is correct.",
		"`status` (`\"success\"` or `\"failed\"`)",
		"the flat `error` object (when `status` is `\"failed\"`)",
		"business fields under `data` (when `status` is `\"success\"`)",
		"`error.kind=decode`",
		"`error.kind=server`",
		"`error.kind=unknown`",
		"`403 Forbidden`",
		"Read [references/best-practices.md](references/best-practices.md) for exact `outputMode`, `deliveryMode`, file delivery, and `--jmes-filter` semantics",
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
		"not a different analysis API",
		"file-oriented full-row export",
		"Exact method/path is already known",
		"--verb list",
		"re-run `volclog tool list <group> --verb <user-intent-verb>`",
		"Human shortcut groups are for humans.",
		"Stop once one row clearly matches the current intent.",
		"For non-`log` groups and plain CRUD intent, default to `volclog tool list <group> --verb <verb>` before browsing anything else.",
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
		"Status",
		"Stop when",
			"validation query",
			"Pick one SOP, follow it until its stop condition, then stop.",
			"poll `index.describe` with a reasonable timeout",
			"first follow `SKILL.md` Error Recovery Quick Map",
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
		"does not add a new server-side analysis API or another pagination model",
		"full analysis row set written as an export file",
		"Stay on `log.search` when the user is still validating SQL",
		"let CLI `deliveryMode` decide stdout vs `file_auto`",
		"`deliveryMode`",
		"`outputMode`",
		"`--jmes-filter` runs on the complete CLI envelope",
		"`--jmes-filter` is stdout-only; do not combine it with file delivery",
		"`execution.projection` is different: it runs on the raw result before envelope wrapping",
		"stdout returns literal `null` and the command still succeeds",
		"Failed envelopes use one flat `error` object",
		"`error.source`",
		"`error.kind`",
		"`error.code`",
		"`error.details`",
		"`volclog-human`",
		"`HitCount`",
			"`Histogram.TotalCount`",
			"`ResultStatus=incomplete` means the service returned only a partial scan",
			"`--secrets-file`",
			"`context.secrets_file`",
			"VOLCENGINE_ACCESS_KEY_ID",
			"Dry-Run Scope",
			"`error.hint`",
			"`unknown tool`",
			"`missing --input`",
			"`jmes filter returned literal null`",
			"`error.kind=server` or `5xx`",
			"`TopicAlreadyExist`",
			"`ProjectAlreadyExist`",
			"`search returned empty after write`",
			"`huge stdout payload`",
			"`page-all-is-not-compression`",
			"`jmes-filter-and-projection-have-different-scope`",
			"`jmes-filter-null-is-still-success`",
			"`jmes-filter-does-not-mix-with-file-delivery`",
			"`deliverymode-belongs-to-runtime`",
			"`workflow-ids-are-not-tool-ids`",
			"`ingest-is-not-tool-put`",
			"`shortcuts-are-human-first`",
			"`default-binary-is-agent-first`",
			"`thin-client-does-not-judge-business-semantics`",
			"`env-creds-override-profile`",
			"`profile-and-secrets-file-are-exclusive`",
			"Do not use this file to reopen surface selection once routing is already clear.",
			"Doctor Boundary",
			"does not override the main skill's default first response",
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
		if principle == "prefer_volclog_default_when_available" {
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
		t.Fatalf("manifest missing prefer_volclog_default_when_available principle: %+v", m)
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
	foundServerRecovery := false
	for _, entry := range recovery.Recipes {
		if signal, _ := entry["error_signature"].(string); signal == "`error.kind=server` or `5xx`" {
			foundServerRecovery = true
			break
		}
	}
	if !foundServerRecovery {
		t.Fatalf("recovery template missing server/5xx recipe: %+v", recovery)
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

	bestPracticesBytes, err := os.ReadFile(filepath.Join(".", "skills/volclog-core/references/best-practices.md"))
	if err != nil {
		t.Fatalf("read best-practices.md: %v", err)
	}
	bestPractices := string(bestPracticesBytes)
	for _, entry := range recovery.Recipes {
		signal, _ := entry["error_signature"].(string)
		if signal == "" {
			continue
		}
		if !strings.Contains(bestPractices, signal) {
			t.Fatalf("best-practices.md missing recovery signal %q from template", signal)
		}
	}
	for _, entry := range traps.Traps {
		trap, _ := entry["trap"].(string)
		if trap == "" {
			continue
		}
		if !strings.Contains(bestPractices, trap) {
			t.Fatalf("best-practices.md missing trap %q from template", trap)
		}
	}
}
