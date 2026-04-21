package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestToolListDefaultsToGroups(t *testing.T) {
	out, err := runTool(nil, []string{"list"})
	if err != nil {
		t.Fatalf("runTool list failed: %v", err)
	}
	text := strings.TrimSpace(asOutputString(t, out))
	if text == "" {
		t.Fatalf("empty tool list output: %q", text)
	}

	catalog, err := loadToolCatalog()
	if err != nil {
		t.Fatalf("load tool catalog failed: %v", err)
	}
	if len(catalog.Tools) == 0 {
		t.Fatalf("expected non-empty tool catalog")
	}

	expectedGroups := map[string]struct{}{}
	for _, tool := range catalog.Tools {
		g := strings.TrimSpace(tool.Group)
		if g != "" {
			expectedGroups[strings.ToLower(g)] = struct{}{}
		}
	}
	for g := range expectedGroups {
		if strings.Contains(text, "  - "+g+" (") {
			return
		}
	}
	t.Fatalf("tool list output should contain at least one group summary: %q", text)
}

func TestToolRootRejectsLegacyGroupFlag(t *testing.T) {
	_, err := runTool(nil, []string{"--group", "topic"})
	if err == nil {
		t.Fatalf("expected root legacy --group alias to be rejected")
	}
	if !strings.Contains(err.Error(), "unknown flag: --group") {
		t.Fatalf("unexpected error for legacy --group alias: %v", err)
	}
}

func TestToolRootIdentityAliasDescribe(t *testing.T) {
	out, err := runTool(nil, []string{"topic.create"})
	if err != nil {
		t.Fatalf("runTool root describe alias failed: %v", err)
	}
	got, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected describe output as map, got %T", out)
	}
	identity := got["identity"].(map[string]any)
	if normalizeToken(asStringOrEmpty(identity["id"])) != "topic.create" {
		t.Fatalf("expected topic.create to resolve to topic.create, got %#v", identity["id"])
	}
}

func TestToolListFiltersByGroupAndVerb(t *testing.T) {
	catalog, err := loadToolCatalog()
	if err != nil {
		t.Fatalf("load tool catalog failed: %v", err)
	}
	if len(catalog.Tools) == 0 {
		t.Fatalf("expected non-empty tool catalog")
	}

	group := ""
	verb := ""
	for _, tool := range catalog.Tools {
		if strings.TrimSpace(tool.Group) == "" {
			continue
		}
		if strings.TrimSpace(tool.Verb) == "" {
			continue
		}
		group = tool.Group
		verb = tool.Verb
		break
	}
	if strings.TrimSpace(group) == "" {
		t.Fatalf("cannot find a tool with verb in generated catalog")
	}

	groupOut, err := runTool(nil, []string{"list", group})
	if err != nil {
		t.Fatalf("runTool list <group> failed: %v", err)
	}
	groupText := strings.TrimSpace(asOutputString(t, groupOut))
	if !strings.Contains(strings.ToLower(groupText), strings.ToLower(group)) {
		t.Fatalf("expected list by group to include group name %q: %q", group, groupText)
	}

	expectedActionsByGroup := map[string]struct{}{}
	for _, tool := range catalog.Tools {
		if strings.EqualFold(strings.TrimSpace(tool.Group), strings.TrimSpace(group)) {
			action := strings.TrimSpace(tool.ID)
			if action != "" {
				expectedActionsByGroup[action] = struct{}{}
			}
		}
	}
	gotGroupActions := parseToolActionsFromList(groupText)
	if len(gotGroupActions) == 0 {
		t.Fatalf("tool list by group should return action list: %q", groupText)
	}
	for _, action := range gotGroupActions {
		if _, ok := expectedActionsByGroup[action]; !ok {
			t.Fatalf("unexpected action %q for group %q in list output", action, group)
		}
	}

	filteredOut, err := runTool(nil, []string{"list", group, "--verb", verb})
	if err != nil {
		t.Fatalf("runTool list with filters failed: %v", err)
	}
	filteredText := strings.TrimSpace(asOutputString(t, filteredOut))
	if filteredText == "" {
		t.Fatalf("filtered list should not be empty")
	}
	expectedFiltered := map[string]struct{}{}
	for _, tool := range catalog.Tools {
		if strings.EqualFold(strings.TrimSpace(tool.Group), strings.TrimSpace(group)) &&
			strings.EqualFold(strings.TrimSpace(tool.Verb), strings.TrimSpace(verb)) {
			action := strings.TrimSpace(tool.ID)
			if action != "" {
				expectedFiltered[action] = struct{}{}
			}
		}
	}
	if len(expectedFiltered) == 0 {
		t.Fatalf("no tool matches selected filters group=%q verb=%q", group, verb)
	}
	gotFilteredActions := parseToolActionsFromList(filteredText)
	if len(gotFilteredActions) == 0 {
		t.Fatalf("tool list with group/verb should include at least one action: %q", filteredText)
	}
	if len(gotFilteredActions) > len(expectedFiltered) {
		t.Fatalf("filtered list is too broad: got %d actions, expected <=%d", len(gotFilteredActions), len(expectedFiltered))
	}
	for _, action := range gotFilteredActions {
		if _, ok := expectedFiltered[action]; !ok {
			t.Fatalf("filtered list returned unmatched action %q", action)
		}
	}
}

func TestToolListByGroupReturnsRunnableIdentities(t *testing.T) {
	out, err := runTool(nil, []string{"list", "topic"})
	if err != nil {
		t.Fatalf("runTool list <group> failed: %v", err)
	}
	text := strings.TrimSpace(asOutputString(t, out))
	for _, want := range []string{"topic.create", "topic.describe-topics"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected runnable tool identity %q in list output: %q", want, text)
		}
	}
	if strings.Contains(text, "\n  - topic.create-topic") {
		t.Fatalf("group list should emit canonical id instead of legacy long id: %q", text)
	}
	if strings.Contains(text, "\n  - CreateTopic") {
		t.Fatalf("group list should not emit raw action names anymore: %q", text)
	}
}

func TestToolListSemanticVerbMapsDescribeActions(t *testing.T) {
	listOut, err := runTool(nil, []string{"list", "topic", "--verb", "list"})
	if err != nil {
		t.Fatalf("runTool list topic --verb list failed: %v", err)
	}
	listText := strings.TrimSpace(asOutputString(t, listOut))
	listActions := parseToolActionsFromList(listText)
	if !containsToolAction(listActions, "topic.describe-topics") {
		t.Fatalf("expected --verb list to include plural describe action: %q", listText)
	}
	if containsToolAction(listActions, "topic.describe-topic") {
		t.Fatalf("expected --verb list to exclude singular describe action: %q", listText)
	}

	getOut, err := runTool(nil, []string{"list", "topic", "--verb", "get"})
	if err != nil {
		t.Fatalf("runTool list topic --verb get failed: %v", err)
	}
	getText := strings.TrimSpace(asOutputString(t, getOut))
	getActions := parseToolActionsFromList(getText)
	if !containsToolAction(getActions, "topic.describe-topic") {
		t.Fatalf("expected --verb get to include singular describe action: %q", getText)
	}
	if containsToolAction(getActions, "topic.describe-topics") {
		t.Fatalf("expected --verb get to exclude plural describe action: %q", getText)
	}
}

func TestToolListSemanticVerbHandlesPluralDescribeVariants(t *testing.T) {
	out, err := runTool(nil, []string{"list", "host-group", "--verb", "list"})
	if err != nil {
		t.Fatalf("runTool list host-group --verb list failed: %v", err)
	}
	hostGroupActions := parseToolActionsFromList(asOutputString(t, out))
	if !containsToolAction(hostGroupActions, "host-group.describe-host-groups-v2") {
		t.Fatalf("expected host-group plural describe action to map to list: %#v", hostGroupActions)
	}

	out, err = runTool(nil, []string{"list", "processor", "--verb", "list"})
	if err != nil {
		t.Fatalf("runTool list processor --verb list failed: %v", err)
	}
	processorActions := parseToolActionsFromList(asOutputString(t, out))
	if !containsToolAction(processorActions, "processor.describe-topics-by-processor") {
		t.Fatalf("expected topics-by-processor describe action to map to list: %#v", processorActions)
	}
}

func TestToolListRejectsLegacyGroupFlag(t *testing.T) {
	_, err := runTool(nil, []string{"list", "--group", "topic"})
	if err == nil {
		t.Fatalf("expected list --group to be rejected")
	}
	if !strings.Contains(err.Error(), "unknown flag: --group") {
		t.Fatalf("unexpected error for legacy --group: %v", err)
	}
}

func TestToolListRejectsLegacyFamilyFlag(t *testing.T) {
	_, err := runTool(nil, []string{"list", "topic", "--family", "topic"})
	if err == nil {
		t.Fatalf("expected list --family to be rejected")
	}
	if !strings.Contains(err.Error(), "unknown flag: --family") {
		t.Fatalf("unexpected error for legacy --family: %v", err)
	}
}

func TestToolListSupportsJSONFormatForGroups(t *testing.T) {
	out, err := runTool(nil, []string{"list", "--format", "json"})
	if err != nil {
		t.Fatalf("runTool list --format json failed: %v", err)
	}
	got, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected map output, got %T", out)
	}
	groups, ok := got["groups"].([]map[string]any)
	if !ok {
		t.Fatalf("expected groups list, got %#v", got["groups"])
	}
	if len(groups) == 0 {
		t.Fatalf("expected non-empty groups list: %#v", got)
	}
	first := groups[0]
	if strings.TrimSpace(asStringOrEmpty(first["group"])) == "" {
		t.Fatalf("expected group name in first item: %#v", first)
	}
	if _, ok := first["count"]; !ok {
		t.Fatalf("expected count in first group item: %#v", first)
	}
}

func TestToolListSupportsJSONFormatForGroupActions(t *testing.T) {
	out, err := runTool(nil, []string{"list", "topic", "--format", "json"})
	if err != nil {
		t.Fatalf("runTool list topic --format json failed: %v", err)
	}
	got, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected map output, got %T", out)
	}
	if normalizeToken(asStringOrEmpty(got["group"])) != "topic" {
		t.Fatalf("expected group topic, got %#v", got["group"])
	}
	tools, ok := got["tools"].([]map[string]any)
	if !ok {
		t.Fatalf("expected tools list, got %#v", got["tools"])
	}
	if len(tools) == 0 {
		t.Fatalf("expected non-empty tools list: %#v", got)
	}
	first := tools[0]
	if strings.TrimSpace(asStringOrEmpty(first["id"])) == "" {
		t.Fatalf("expected id in first tool item: %#v", first)
	}
	if normalizeToken(asStringOrEmpty(first["group"])) != "topic" {
		t.Fatalf("expected first tool group topic, got %#v", first["group"])
	}
}

func TestToolListRejectsUnknownFormat(t *testing.T) {
	_, err := runTool(nil, []string{"list", "--format", "yaml"})
	if err == nil {
		t.Fatalf("expected list --format yaml to fail")
	}
	if !strings.Contains(err.Error(), "invalid --format: yaml") {
		t.Fatalf("unexpected error for --format yaml: %v", err)
	}
}

func TestToolListCreateVerbIsTrustworthyForLogGroup(t *testing.T) {
	out, err := runTool(nil, []string{"list", "log", "--verb", "create", "--format", "json"})
	if err != nil {
		t.Fatalf("runTool list log --verb create failed: %v", err)
	}
	got, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected map output, got %T", out)
	}
	tools, ok := got["tools"].([]map[string]any)
	if !ok {
		t.Fatalf("expected tools list, got %#v", got["tools"])
	}
	if len(tools) != 1 {
		t.Fatalf("expected only one true create action in log group, got %#v", tools)
	}
	if normalizeToken(asStringOrEmpty(tools[0]["id"])) != "log.create" {
		t.Fatalf("unexpected create action list: %#v", tools)
	}
}

func TestToolListJSONFormatOverridesGlobalOutputTable(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--output", "table", "tool", "list", "--format", "json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unexpected exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var got map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("expected JSON stdout, got err=%v stdout=%q", err, stdout.String())
	}
	if _, ok := got["groups"]; !ok {
		t.Fatalf("expected groups payload, got %#v", got)
	}
}

func TestToolListBadGroupReturnsUsageEnvelope(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"tool", "list", "definitely-missing-group"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("unexpected exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}

	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v stdout=%q", err, stdout.String())
	}
	if out["status"] != "failed" {
		t.Fatalf("expected failed status, got %#v", out["status"])
	}
	errObj, ok := out["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object, got %#v", out["error"])
	}
	if !strings.Contains(asStringOrEmpty(errObj["message"]), "group not found: definitely-missing-group") {
		t.Fatalf("unexpected error message: %#v", errObj["message"])
	}
	if asStringOrEmpty(errObj["kind"]) != "usage" {
		t.Fatalf("unexpected error kind: %#v", errObj["kind"])
	}
}

func TestToolDescribeUnknownIdentityReturnsUsageEnvelope(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"tool", "describe", "definitely-missing-group.not-real"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("unexpected exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v stdout=%q", err, stdout.String())
	}
	if out["status"] != "failed" {
		t.Fatalf("expected failed status, got %#v", out["status"])
	}
	errObj, ok := out["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object, got %#v", out["error"])
	}
	if errObj["kind"] != "usage" {
		t.Fatalf("expected usage kind, got %#v", errObj["kind"])
	}
	if !strings.Contains(asStringOrEmpty(errObj["message"]), "unknown tool: definitely-missing-group.not-real") {
		t.Fatalf("unexpected error message: %#v", errObj["message"])
	}
}

func TestToolListVerbMissKeepsEmptyResult(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"tool", "list", "topic", "--verb", "definitely-not-a-verb"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unexpected exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "No tools matched." {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
}

func TestToolDescribeTopicCreate(t *testing.T) {
	out, err := runTool(nil, []string{"describe", "topic.create-topic"})
	if err != nil {
		t.Fatalf("runTool describe failed: %v", err)
	}
	got, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected describe output as map, got %T", out)
	}
	identity, ok := got["identity"].(map[string]any)
	if !ok {
		t.Fatalf("missing identity in describe output: %#v", got)
	}
	if normalizeToken(asStringOrEmpty(identity["id"])) != "topic.create" {
		t.Fatalf("expected canonical identity.id topic.create, got %#v", identity["id"])
	}
	if normalizeToken(asStringOrEmpty(identity["group"])) != "topic" {
		t.Fatalf("unexpected identity.group: %#v", identity["group"])
	}
	if asStringOrEmpty(identity["action"]) != "CreateTopic" {
		t.Fatalf("unexpected identity.action: %#v", identity["action"])
	}

	for _, key := range []string{"input", "context", "execution", "input_schema", "context_schema", "execution_schema", "behavior", "usage_notes", "usage_constraints", "output", "risk", "recovery", "source"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("missing %q in describe output: %#v", key, got)
		}
	}
	input, ok := got["input"].(map[string]any)
	if !ok {
		t.Fatalf("missing input schema: %#v", got)
	}
	body, ok := input["body"].(map[string]any)
	if !ok {
		t.Fatalf("missing body schema: %#v", input)
	}
	props, ok := body["properties"].(map[string]any)
	if !ok {
		t.Fatalf("missing body properties: %#v", body)
	}
	if _, ok := props["TopicName"]; !ok {
		t.Fatalf("expected flat body property TopicName, got %#v", props)
	}
	if _, ok := props["data"]; ok {
		t.Fatalf("unexpected swagger body wrapper in tool contract: %#v", props["data"])
	}
}

func TestToolDescribeCLIDefaultUsesCompactView(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"tool", "describe", "topic.create"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	if got := len(stdout.String()); got >= 8000 {
		t.Fatalf("expected compact describe output under 8000 chars, got %d", got)
	}

	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v stdout=%q", err, stdout.String())
	}
	for _, want := range []string{"identity", "context_schema", "execution_schema", "behavior", "contract_digest", "contract_cache_hint", "usage_notes", "risk", "recovery", "output_policy", "usage_constraints", "source"} {
		if _, ok := out[want]; !ok {
			t.Fatalf("missing %q in compact describe output: %#v", want, out)
		}
	}
	for _, notWant := range []string{"input", "context", "execution", "context_example_minimal", "context_example_full", "input_example_minimal", "input_example_full", "output"} {
		if _, ok := out[notWant]; ok {
			t.Fatalf("compact describe output should hide %q: %#v", notWant, out[notWant])
		}
	}
}

func TestToolDescribeExplicitJSONKeepsFullContractView(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--output", "json", "tool", "describe", "topic.create"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}

	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v stdout=%q", err, stdout.String())
	}
	for _, want := range []string{"input", "context", "execution", "output", "output_policy", "risk", "recovery", "source", "contract_cache_hint"} {
		if _, ok := out[want]; !ok {
			t.Fatalf("missing %q in full describe output: %#v", want, out)
		}
	}
	for _, notWant := range []string{"context_example_minimal", "context_example_full", "input_example_minimal", "input_example_full"} {
		if _, ok := out[notWant]; ok {
			t.Fatalf("full describe output should omit %q: %#v", notWant, out[notWant])
		}
	}
	output, ok := out["output"].(map[string]any)
	if !ok {
		t.Fatalf("expected output object, got %#v", out["output"])
	}
	if asStringOrEmpty(out["output_policy"]) != asStringOrEmpty(output["policy"]) {
		t.Fatalf("expected output_policy alias to match output.policy, got alias=%#v output=%#v", out["output_policy"], output["policy"])
	}
}

func TestToolDescribeMetadataParityAcrossViews(t *testing.T) {
	var compactStdout, compactStderr bytes.Buffer
	if code := Run([]string{"tool", "describe", "topic.delete"}, &compactStdout, &compactStderr); code != 0 {
		t.Fatalf("compact exit=%d stdout=%q stderr=%q", code, compactStdout.String(), compactStderr.String())
	}
	if strings.TrimSpace(compactStderr.String()) != "" {
		t.Fatalf("expected empty compact stderr, got %q", compactStderr.String())
	}

	var compact map[string]any
	if err := json.Unmarshal(compactStdout.Bytes(), &compact); err != nil {
		t.Fatalf("invalid compact stdout json: %v stdout=%q", err, compactStdout.String())
	}

	var fullStdout, fullStderr bytes.Buffer
	if code := Run([]string{"--output", "json", "tool", "describe", "topic.delete"}, &fullStdout, &fullStderr); code != 0 {
		t.Fatalf("full exit=%d stdout=%q stderr=%q", code, fullStdout.String(), fullStderr.String())
	}
	if strings.TrimSpace(fullStderr.String()) != "" {
		t.Fatalf("expected empty full stderr, got %q", fullStderr.String())
	}

	var full map[string]any
	if err := json.Unmarshal(fullStdout.Bytes(), &full); err != nil {
		t.Fatalf("invalid full stdout json: %v stdout=%q", err, fullStdout.String())
	}

	for _, key := range []string{"risk", "recovery", "output_policy", "usage_constraints", "source"} {
		if !reflect.DeepEqual(compact[key], full[key]) {
			t.Fatalf("expected compact/full parity for %q: compact=%#v full=%#v", key, compact[key], full[key])
		}
	}
}

func TestToolDescribeResolvesUniqueVerbAlias(t *testing.T) {
	out, err := runTool(nil, []string{"describe", "topic.create"})
	if err != nil {
		t.Fatalf("runTool describe unique verb alias failed: %v", err)
	}
	got := out.(map[string]any)
	identity := got["identity"].(map[string]any)
	if normalizeToken(asStringOrEmpty(identity["id"])) != "topic.create" {
		t.Fatalf("expected topic.create to resolve to topic.create, got %#v", identity["id"])
	}
}

func TestToolDescribeResolvesActionAlias(t *testing.T) {
	out, err := runTool(nil, []string{"describe", "topic.CreateTopic"})
	if err != nil {
		t.Fatalf("runTool describe action alias failed: %v", err)
	}
	got := out.(map[string]any)
	identity := got["identity"].(map[string]any)
	if normalizeToken(asStringOrEmpty(identity["id"])) != "topic.create" {
		t.Fatalf("expected topic.CreateTopic to resolve to topic.create, got %#v", identity["id"])
	}
}

func TestToolDescribeRejectsAmbiguousVerbAlias(t *testing.T) {
	_, err := runTool(nil, []string{"describe", "topic.describe"})
	if err == nil {
		t.Fatalf("expected ambiguous topic.describe to fail")
	}
	if !strings.Contains(err.Error(), "ambiguous tool identity") {
		t.Fatalf("expected ambiguous tool identity error, got %v", err)
	}
	if !strings.Contains(err.Error(), "topic.describe-topic") || !strings.Contains(err.Error(), "topic.describe-topics") {
		t.Fatalf("expected ambiguity suggestions, got %v", err)
	}
}

func TestToolDescribeCarriesSoftDigestPolicy(t *testing.T) {
	out, err := runTool(nil, []string{"describe", "topic.create-topic"})
	if err != nil {
		t.Fatalf("runTool describe failed: %v", err)
	}
	got, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected describe output as map, got %T", out)
	}
	digest, ok := got["contract_digest"].(map[string]any)
	if !ok {
		t.Fatalf("expected contract_digest map, got %T", got["contract_digest"])
	}
	if normalizeToken(asStringOrEmpty(digest["policy"])) != "soft" {
		t.Fatalf("expected soft contract digest policy, got %#v", digest)
	}
	if strings.TrimSpace(asStringOrEmpty(digest["warning"])) == "" {
		t.Fatalf("expected contract digest warning: %#v", digest)
	}
	execution, ok := got["execution"].(map[string]any)
	if !ok {
		t.Fatalf("expected execution map, got %T", got["execution"])
	}
	if _, ok := execution["supports_all"].(bool); !ok {
		t.Fatalf("expected execution.supports_all bool, got %#v", execution["supports_all"])
	}
	if _, ok := execution["supports_dry_run"].(bool); !ok {
		t.Fatalf("expected execution.supports_dry_run bool, got %#v", execution["supports_dry_run"])
	}
}

func TestToolDescribeOmitsExamplesAndKeepsRequiredFields(t *testing.T) {
	out, err := runTool(nil, []string{"describe", "topic.create"})
	if err != nil {
		t.Fatalf("runTool describe failed: %v", err)
	}
	got := out.(map[string]any)

	for _, key := range []string{"context_example_minimal", "context_example_full", "input_example_minimal", "input_example_full"} {
		if _, ok := got[key]; ok {
			t.Fatalf("describe output should omit %q: %#v", key, got[key])
		}
	}

	inputSchema, ok := got["input_schema"].(map[string]any)
	if !ok {
		t.Fatalf("expected input_schema map, got %#v", got["input_schema"])
	}
	body, ok := inputSchema["body"].(map[string]any)
	if !ok {
		t.Fatalf("expected input_schema.body map, got %#v", inputSchema["body"])
	}
	props, ok := body["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected input_schema.body.properties map, got %#v", body["properties"])
	}
	if topicName, ok := props["TopicName"].(map[string]any); !ok {
		t.Fatalf("expected TopicName schema map, got %#v", props["TopicName"])
	} else if _, ok := topicName["example"]; ok {
		t.Fatalf("expected TopicName schema to omit example: %#v", topicName)
	}
	required := toolRequiredFields(body["required"])
	for _, key := range []string{"ProjectId", "ShardCount", "TopicName", "Ttl"} {
		found := false
		for _, item := range required {
			if item == key {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected required field %q in input schema: %#v", key, body)
		}
	}
}

func TestToolDescribeFiltersManagedHeadersFromPublicInputSchema(t *testing.T) {
	out, err := runTool(nil, []string{"describe", "log.put-logs"})
	if err != nil {
		t.Fatalf("runTool describe failed: %v", err)
	}
	got := out.(map[string]any)
	inputSchema, ok := got["input_schema"].(map[string]any)
	if !ok {
		t.Fatalf("expected input_schema map, got %#v", got["input_schema"])
	}
	header, ok := inputSchema["header"].(map[string]any)
	if !ok {
		t.Fatalf("expected input_schema.header map, got %#v", inputSchema["header"])
	}
	props, ok := header["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected input_schema.header.properties map, got %#v", header["properties"])
	}
	for _, key := range []string{"Content-Length", "Content-Type", "Content-MD5", "x-tls-bodyrawsize", "x-tls-compresstype"} {
		if props[key] != nil {
			t.Fatalf("expected managed header %q to be filtered: %#v", key, props)
		}
	}
	if props["x-tls-hashkey"] == nil {
		t.Fatalf("expected business header x-tls-hashkey to remain: %#v", props)
	}
}

func TestToolDescribePutLogsBodyContractAlignsWithSpecialIOInput(t *testing.T) {
	out, err := runTool(nil, []string{"describe", "log.put-logs"})
	if err != nil {
		t.Fatalf("runTool describe failed: %v", err)
	}
	got := out.(map[string]any)
	inputSchema, ok := got["input_schema"].(map[string]any)
	if !ok {
		t.Fatalf("expected input_schema map, got %#v", got["input_schema"])
	}
	body, ok := inputSchema["body"].(map[string]any)
	if !ok {
		t.Fatalf("expected input_schema.body map, got %#v", inputSchema["body"])
	}
	props, ok := body["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected input_schema.body.properties map, got %#v", body["properties"])
	}
	if props["LogGroupList"] != nil {
		t.Fatalf("expected legacy LogGroupList to be removed from public contract: %#v", props["LogGroupList"])
	}
	logGroups, ok := props["LogGroups"].(map[string]any)
	if !ok {
		t.Fatalf("expected LogGroups schema map, got %#v", props["LogGroups"])
	}
	if asStringOrEmpty(logGroups["type"]) != "array" {
		t.Fatalf("expected LogGroups type=array, got %#v", logGroups)
	}
	item, ok := logGroups["items"].(map[string]any)
	if !ok {
		t.Fatalf("expected LogGroups.items object schema, got %#v", logGroups["items"])
	}
	itemProps, ok := item["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected LogGroups item properties, got %#v", item)
	}
	if itemProps["Logs"] == nil {
		t.Fatalf("expected LogGroups item to contain Logs field: %#v", itemProps)
	}
	logsSchema, ok := itemProps["Logs"].(map[string]any)
	if !ok {
		t.Fatalf("expected Logs schema map, got %#v", itemProps["Logs"])
	}
	logsItems, ok := logsSchema["items"].(map[string]any)
	if !ok {
		t.Fatalf("expected Logs.items schema, got %#v", logsSchema["items"])
	}
	logEntryProps, ok := logsItems["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected log entry properties, got %#v", logsItems)
	}
	timeField, ok := logEntryProps["Time"].(map[string]any)
	if !ok {
		t.Fatalf("expected Time schema map, got %#v", logEntryProps["Time"])
	}
	if !strings.Contains(strings.ToLower(asStringOrEmpty(timeField["description"])), "unix") || !strings.Contains(asStringOrEmpty(timeField["description"]), "毫秒") {
		t.Fatalf("expected Time description to preserve unix milliseconds semantics, got %#v", timeField["description"])
	}
	timeNsField, ok := logEntryProps["TimeNs"].(map[string]any)
	if !ok {
		t.Fatalf("expected TimeNs schema map, got %#v", logEntryProps["TimeNs"])
	}
	if !strings.Contains(strings.ToLower(asStringOrEmpty(timeNsField["description"])), "nanosecond") {
		t.Fatalf("expected TimeNs description to explain nanosecond fraction semantics, got %#v", timeNsField["description"])
	}
	contentsSchema, ok := logEntryProps["Contents"].(map[string]any)
	if !ok {
		t.Fatalf("expected Contents schema map, got %#v", logEntryProps["Contents"])
	}
	if contentsSchema["oneOf"] == nil {
		t.Fatalf("expected Contents to support both object and key/value array forms: %#v", contentsSchema)
	}
	required := toolRequiredFields(body["required"])
	foundLogGroups := false
	for _, item := range required {
		if item == "LogGroups" {
			foundLogGroups = true
			break
		}
	}
	if !foundLogGroups {
		t.Fatalf("expected LogGroups required in body schema: %#v", body["required"])
	}
}

func TestToolDescribeTraceWeakTypedFieldsAreObjectSchemas(t *testing.T) {
	for _, id := range []string{"trace.create", "trace.modify"} {
		out, err := runTool(nil, []string{"describe", id})
		if err != nil {
			t.Fatalf("runTool describe %s failed: %v", id, err)
		}
		got := out.(map[string]any)
		inputSchema, ok := got["input_schema"].(map[string]any)
		if !ok {
			t.Fatalf("%s expected input_schema map, got %#v", id, got["input_schema"])
		}
		body, ok := inputSchema["body"].(map[string]any)
		if !ok {
			t.Fatalf("%s expected input_schema.body map, got %#v", id, inputSchema["body"])
		}
		props, ok := body["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s expected body properties map, got %#v", id, body["properties"])
		}
		backend, ok := props["BackendConfig"].(map[string]any)
		if !ok {
			t.Fatalf("%s expected BackendConfig schema map, got %#v", id, props["BackendConfig"])
		}
		if asStringOrEmpty(backend["type"]) != "object" {
			t.Fatalf("%s expected BackendConfig type=object, got %#v", id, backend)
		}
		if backend["additionalProperties"] != true {
			if props, ok := backend["properties"].(map[string]any); !ok || len(props) == 0 {
				t.Fatalf("%s expected BackendConfig to be either free-form or structured object, got %#v", id, backend)
			}
		}
	}

	out, err := runTool(nil, []string{"describe", "trace.search"})
	if err != nil {
		t.Fatalf("runTool describe trace.search failed: %v", err)
	}
	got := out.(map[string]any)
	inputSchema, ok := got["input_schema"].(map[string]any)
	if !ok {
		t.Fatalf("trace.search expected input_schema map, got %#v", got["input_schema"])
	}
	body, ok := inputSchema["body"].(map[string]any)
	if !ok {
		t.Fatalf("trace.search expected input_schema.body map, got %#v", inputSchema["body"])
	}
	props, ok := body["properties"].(map[string]any)
	if !ok {
		t.Fatalf("trace.search expected body properties map, got %#v", body["properties"])
	}
	query, ok := props["Query"].(map[string]any)
	if !ok {
		t.Fatalf("trace.search expected Query schema map, got %#v", props["Query"])
	}
	if asStringOrEmpty(query["type"]) != "object" {
		t.Fatalf("trace.search expected Query type=object, got %#v", query)
	}
	if query["additionalProperties"] != true {
		if props, ok := query["properties"].(map[string]any); !ok || len(props) == 0 {
			t.Fatalf("trace.search expected Query to be either free-form or structured object, got %#v", query)
		}
	}
}

func TestToolDescribeExplainsExecutionEmbedding(t *testing.T) {
	out, err := runTool(nil, []string{"describe", "topic.describe-topics"})
	if err != nil {
		t.Fatalf("runTool describe failed: %v", err)
	}
	got := out.(map[string]any)

	contextSchema, ok := got["context_schema"].(map[string]any)
	if !ok {
		t.Fatalf("expected context_schema map, got %#v", got["context_schema"])
	}
	ctxProps, ok := contextSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected context_schema.properties map, got %#v", contextSchema["properties"])
	}
	execField, ok := ctxProps["execution"].(map[string]any)
	if !ok {
		t.Fatalf("expected context_schema.properties.execution map, got %#v", ctxProps["execution"])
	}
	if !strings.Contains(strings.ToLower(asStringOrEmpty(execField["runtime_effect"])), "context.execution") {
		t.Fatalf("expected execution runtime_effect to mention context.execution, got %#v", execField["runtime_effect"])
	}

	executionSchema, ok := got["execution_schema"].(map[string]any)
	if !ok {
		t.Fatalf("expected execution_schema map, got %#v", got["execution_schema"])
	}
	execProps, ok := executionSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected execution_schema.properties map, got %#v", executionSchema["properties"])
	}
	for _, key := range []string{"dry_run", "projection", "artifact", "page"} {
		field, ok := execProps[key].(map[string]any)
		if !ok {
			t.Fatalf("expected execution field %q to be documented as map, got %#v", key, execProps[key])
		}
		if strings.TrimSpace(asStringOrEmpty(field["description"])) == "" {
			t.Fatalf("expected execution field %q to have description: %#v", key, field)
		}
	}

	notes, ok := got["usage_notes"].([]any)
	if !ok {
		t.Fatalf("expected usage_notes list, got %#v", got["usage_notes"])
	}
	for _, note := range notes {
		if strings.Contains(strings.ToLower(asStringOrEmpty(note)), "context.execution") {
			t.Fatalf("expected common execution embedding guidance to move out of usage_notes, got %#v", notes)
		}
	}
}

func TestToolDescribeDocumentsInputEncodingHintAndFlatSchema(t *testing.T) {
	out, err := runTool(nil, []string{"describe", "project.describe-projects"})
	if err != nil {
		t.Fatalf("runTool describe failed: %v", err)
	}
	got := out.(map[string]any)

	hint, ok := got["input_encoding_hint"].(map[string]any)
	if !ok {
		t.Fatalf("expected input_encoding_hint map, got %#v", got["input_encoding_hint"])
	}
	if !strings.Contains(asStringOrEmpty(hint["transport"]), "--input") || !strings.Contains(asStringOrEmpty(hint["transport"]), "inline JSON") {
		t.Fatalf("expected transport hint to mention inline JSON support, got %#v", hint)
	}
	if !strings.Contains(asStringOrEmpty(hint["recommended"]), "flat") || !strings.Contains(asStringOrEmpty(hint["recommended"]), "query") {
		t.Fatalf("expected recommended hint to mention flat query encoding, got %#v", hint)
	}

	flat, ok := got["input_flat_schema"].(map[string]any)
	if !ok {
		t.Fatalf("expected input_flat_schema map, got %#v", got["input_flat_schema"])
	}
	props, ok := flat["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected input_flat_schema.properties map, got %#v", flat)
	}
	projectID, ok := props["ProjectId"].(map[string]any)
	if !ok {
		t.Fatalf("expected ProjectId flat schema, got %#v", props["ProjectId"])
	}
	if asStringOrEmpty(projectID["in"]) != "query" {
		t.Fatalf("expected ProjectId flat schema to mark query location, got %#v", projectID)
	}
}

func TestToolDescribeCompactPrefersFlatSchemaForSingleQuerySection(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"tool", "describe", "project.describe-projects"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v stdout=%q", err, stdout.String())
	}
	if _, ok := out["input_flat_schema"]; !ok {
		t.Fatalf("expected input_flat_schema in compact output, got %#v", out)
	}
	if _, ok := out["input_schema"]; ok {
		t.Fatalf("expected compact output to omit redundant input_schema for single query-section tool, got %#v", out["input_schema"])
	}
}

func TestToolDescribeCompactFlatSchemaRetainsQueryFieldDocs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"tool", "describe", "topic.describe-topics"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}

	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v stdout=%q", err, stdout.String())
	}
	flat, ok := out["input_flat_schema"].(map[string]any)
	if !ok {
		t.Fatalf("expected input_flat_schema map, got %#v", out["input_flat_schema"])
	}
	props, ok := flat["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected input_flat_schema.properties map, got %#v", flat["properties"])
	}
	projectID, ok := props["ProjectId"].(map[string]any)
	if !ok {
		t.Fatalf("expected ProjectId flat schema, got %#v", props["ProjectId"])
	}
	if !strings.Contains(asStringOrEmpty(projectID["description"]), "项目") {
		t.Fatalf("expected ProjectId flat schema to keep description, got %#v", projectID)
	}
	if asStringOrEmpty(projectID["in"]) != "query" {
		t.Fatalf("expected ProjectId flat schema to mark query location, got %#v", projectID)
	}
}

func TestToolDescribeIncludesContractCacheHint(t *testing.T) {
	for _, args := range [][]string{
		{"tool", "describe", "project.describe-projects"},
		{"--output", "json", "tool", "describe", "project.describe-projects"},
	} {
		var stdout, stderr bytes.Buffer
		code := Run(args, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("args=%v exit=%d stdout=%q stderr=%q", args, code, stdout.String(), stderr.String())
		}
		var out map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
			t.Fatalf("args=%v invalid stdout json: %v stdout=%q", args, err, stdout.String())
		}
		hint, ok := out["contract_cache_hint"].(map[string]any)
		if !ok {
			t.Fatalf("args=%v expected contract_cache_hint map, got %#v", args, out["contract_cache_hint"])
		}
		if !strings.Contains(strings.ToLower(asStringOrEmpty(hint["safe_scope"])), "contract_digest") {
			t.Fatalf("args=%v expected safe_scope to mention contract_digest, got %#v", args, hint)
		}
		refresh, ok := hint["refresh_when"].([]any)
		if !ok || len(refresh) == 0 {
			t.Fatalf("args=%v expected refresh_when list, got %#v", args, hint["refresh_when"])
		}
	}
}

func TestToolDescribeRecommendsArtifactAndClarifiesPageAllPayload(t *testing.T) {
	out, err := runTool(nil, []string{"describe", "topic.describe-topics"})
	if err != nil {
		t.Fatalf("runTool describe failed: %v", err)
	}
	got := out.(map[string]any)
	notes, ok := got["usage_notes"].([]any)
	if !ok || len(notes) == 0 {
		t.Fatalf("expected usage_notes list, got %#v", got["usage_notes"])
	}

	foundPageAllPayload := false
	for _, note := range notes {
		text := strings.ToLower(asStringOrEmpty(note))
		if strings.Contains(text, "page.all") && strings.Contains(text, "payload") {
			foundPageAllPayload = true
		}
	}
	if !foundPageAllPayload {
		t.Fatalf("expected usage_notes to clarify page.all payload semantics, got %#v", notes)
	}
}

func TestToolDescribeLogSearchExplainsInteractiveAnalysisVsExportAnalysis(t *testing.T) {
	out, err := runTool(nil, []string{"describe", "log.search"})
	if err != nil {
		t.Fatalf("runTool describe failed: %v", err)
	}
	got := out.(map[string]any)
	notes, ok := got["usage_notes"].([]any)
	if !ok || len(notes) == 0 {
		t.Fatalf("expected usage_notes list, got %#v", got["usage_notes"])
	}

	foundSQLSupport := false
	foundExportBoundary := false
	for _, note := range notes {
		text := strings.ToLower(asStringOrEmpty(note))
		if strings.Contains(text, "sql/analysis") || strings.Contains(text, "select") {
			foundSQLSupport = true
		}
		if strings.Contains(text, "export-analysis") && strings.Contains(text, "interactive analysis") {
			foundExportBoundary = true
		}
	}
	if !foundSQLSupport {
		t.Fatalf("expected usage_notes to explain log.search sql/analysis support, got %#v", notes)
	}
	if !foundExportBoundary {
		t.Fatalf("expected usage_notes to explain when to switch to export-analysis, got %#v", notes)
	}
}

func TestToolDescribeLogSearchExplainsCountSemantics(t *testing.T) {
	out, err := runTool(nil, []string{"describe", "log.search"})
	if err != nil {
		t.Fatalf("runTool describe failed: %v", err)
	}
	got := out.(map[string]any)
	notes, ok := got["usage_notes"].([]any)
	if !ok || len(notes) == 0 {
		t.Fatalf("expected usage_notes list, got %#v", got["usage_notes"])
	}

	foundHitCount := false
	foundHistogramTotal := false
	foundIncomplete := false
	for _, note := range notes {
		text := strings.ToLower(asStringOrEmpty(note))
		if strings.Contains(text, "hitcount") && (strings.Contains(text, "current response") || strings.Contains(text, "当前返回")) {
			foundHitCount = true
		}
		if strings.Contains(text, "histogram.totalcount") || (strings.Contains(text, "totalcount") && strings.Contains(text, "histogram")) {
			foundHistogramTotal = true
		}
		if strings.Contains(text, "resultstatus") && strings.Contains(text, "incomplete") {
			foundIncomplete = true
		}
	}
	if !foundHitCount {
		t.Fatalf("expected usage_notes to explain HitCount semantics, got %#v", notes)
	}
	if !foundHistogramTotal {
		t.Fatalf("expected usage_notes to explain Histogram.TotalCount semantics, got %#v", notes)
	}
	if !foundIncomplete {
		t.Fatalf("expected usage_notes to explain incomplete semantics, got %#v", notes)
	}
}

func TestToolDescribeLogHistogramExplainsWhenToUseWithSearch(t *testing.T) {
	out, err := runTool(nil, []string{"describe", "log.describe-histogram-v1"})
	if err != nil {
		t.Fatalf("runTool describe failed: %v", err)
	}
	got := out.(map[string]any)
	notes, ok := got["usage_notes"].([]any)
	if !ok || len(notes) == 0 {
		t.Fatalf("expected usage_notes list, got %#v", got["usage_notes"])
	}

	foundTimeDistribution := false
	foundSearchBridge := false
	foundCountSemantics := false
	foundPureSearchBoundary := false
	foundIncomplete := false
	for _, note := range notes {
		text := strings.ToLower(asStringOrEmpty(note))
		if strings.Contains(text, "time distribution") || strings.Contains(text, "时间分布") {
			foundTimeDistribution = true
		}
		if strings.Contains(text, "log.search") && (strings.Contains(text, "narrow") || strings.Contains(text, "缩小") || strings.Contains(text, "preview")) {
			foundSearchBridge = true
		}
		if strings.Contains(text, "totalcount") && (strings.Contains(text, "hits") || strings.Contains(text, "命中")) {
			foundCountSemantics = true
		}
		if strings.Contains(text, "pure search") || strings.Contains(text, "纯检索") {
			foundPureSearchBoundary = true
		}
		if strings.Contains(text, "resultstatus") && strings.Contains(text, "incomplete") {
			foundIncomplete = true
		}
	}
	if !foundTimeDistribution {
		t.Fatalf("expected usage_notes to explain histogram time-distribution role, got %#v", notes)
	}
	if !foundSearchBridge {
		t.Fatalf("expected usage_notes to bridge histogram and log.search usage, got %#v", notes)
	}
	if !foundCountSemantics {
		t.Fatalf("expected usage_notes to explain histogram count semantics, got %#v", notes)
	}
	if !foundPureSearchBoundary {
		t.Fatalf("expected usage_notes to explain histogram pure-search boundary, got %#v", notes)
	}
	if !foundIncomplete {
		t.Fatalf("expected usage_notes to explain histogram incomplete semantics, got %#v", notes)
	}
}

func TestToolDescribeCompactAvoidsDuplicatingExecutionProperties(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"tool", "describe", "topic.create"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}

	var got map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("invalid stdout json: %v stdout=%q", err, stdout.String())
	}

	contextSchema, ok := got["context_schema"].(map[string]any)
	if !ok {
		t.Fatalf("expected context_schema map, got %#v", got["context_schema"])
	}
	ctxProps, ok := contextSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected context_schema.properties map, got %#v", contextSchema["properties"])
	}
	execField, ok := ctxProps["execution"].(map[string]any)
	if !ok {
		t.Fatalf("expected context_schema.properties.execution map, got %#v", ctxProps["execution"])
	}
	if _, ok := execField["properties"]; ok {
		t.Fatalf("expected compact context.execution to avoid duplicating execution properties: %#v", execField)
	}
	if asStringOrEmpty(execField["schema_ref"]) != "execution_schema" {
		t.Fatalf("expected compact context.execution to reference execution_schema, got %#v", execField["schema_ref"])
	}
}

func TestToolDescribeCompactKeepsUsefulOptionalInputFields(t *testing.T) {
	for _, tc := range []struct {
		id      string
		section string
		fields  []string
	}{
		{id: "index.create", section: "body", fields: []string{"TopicId", "FullText", "KeyValue", "MaxTextLen"}},
		{id: "topic.describe-topics", section: "query", fields: []string{"ProjectId", "TopicName", "PageSize"}},
		{id: "host-group.create", section: "body", fields: []string{"HostGroupName", "HostGroupType", "HostIpList"}},
	} {
		var stdout, stderr bytes.Buffer
		code := Run([]string{"tool", "describe", tc.id}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("%s exit=%d stdout=%q stderr=%q", tc.id, code, stdout.String(), stderr.String())
		}
		var out map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
			t.Fatalf("%s invalid stdout json: %v stdout=%q", tc.id, err, stdout.String())
		}
		var props map[string]any
		if inputSchema, ok := out["input_schema"].(map[string]any); ok {
			section, ok := inputSchema[tc.section].(map[string]any)
			if !ok {
				t.Fatalf("%s expected input_schema.%s map, got %#v", tc.id, tc.section, inputSchema[tc.section])
			}
			props, ok = section["properties"].(map[string]any)
			if !ok {
				t.Fatalf("%s expected properties map, got %#v", tc.id, section["properties"])
			}
		} else if flatSchema, ok := out["input_flat_schema"].(map[string]any); ok && tc.section != "body" {
			props, ok = flatSchema["properties"].(map[string]any)
			if !ok {
				t.Fatalf("%s expected input_flat_schema.properties map, got %#v", tc.id, flatSchema["properties"])
			}
		} else {
			t.Fatalf("%s expected usable input schema in compact output, got %#v", tc.id, out)
		}
		for _, field := range tc.fields {
			if _, ok := props[field]; !ok {
				t.Fatalf("%s expected compact schema to keep %q, got %#v", tc.id, field, props)
			}
		}
	}
}

func TestToolDescribeCompactPreservesNestedConstraintDocs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"tool", "describe", "index.create"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}

	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v stdout=%q", err, stdout.String())
	}
	inputSchema, ok := out["input_schema"].(map[string]any)
	if !ok {
		t.Fatalf("expected input_schema map, got %#v", out["input_schema"])
	}
	body, ok := inputSchema["body"].(map[string]any)
	if !ok {
		t.Fatalf("expected input_schema.body map, got %#v", inputSchema["body"])
	}
	props, ok := body["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected body properties map, got %#v", body["properties"])
	}
	fullText, ok := props["FullText"].(map[string]any)
	if !ok {
		t.Fatalf("expected FullText schema map, got %#v", props["FullText"])
	}
	if !strings.Contains(asStringOrEmpty(fullText["description"]), "全文索引") {
		t.Fatalf("expected FullText description in compact schema, got %#v", fullText)
	}
	fullTextProps, ok := fullText["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected FullText properties map, got %#v", fullText["properties"])
	}
	delimiter, ok := fullTextProps["Delimiter"].(map[string]any)
	if !ok {
		t.Fatalf("expected FullText.Delimiter schema, got %#v", fullTextProps["Delimiter"])
	}
	if !strings.Contains(asStringOrEmpty(delimiter["description"]), "分词符") {
		t.Fatalf("expected Delimiter description in compact schema, got %#v", delimiter)
	}
	maxTextLen, ok := props["MaxTextLen"].(map[string]any)
	if !ok {
		t.Fatalf("expected MaxTextLen schema map, got %#v", props["MaxTextLen"])
	}
	if maxTextLen["default"] == nil || maxTextLen["minimum"] == nil || maxTextLen["maximum"] == nil {
		t.Fatalf("expected MaxTextLen default/minimum/maximum in compact schema, got %#v", maxTextLen)
	}
}

func TestToolDescribeCompactKeepsSmallNestedObjectsUsable(t *testing.T) {
	for _, tc := range []struct {
		id       string
		field    string
		childKey string
	}{
		{id: "shipper.create", field: "ContentInfo", childKey: "Format"},
		{id: "alarm.create-alarm-content-template", field: "DingTalk", childKey: "Title"},
	} {
		var stdout, stderr bytes.Buffer
		code := Run([]string{"tool", "describe", tc.id}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("%s exit=%d stdout=%q stderr=%q", tc.id, code, stdout.String(), stderr.String())
		}
		var out map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
			t.Fatalf("%s invalid stdout json: %v stdout=%q", tc.id, err, stdout.String())
		}
		inputSchema := out["input_schema"].(map[string]any)
		body := inputSchema["body"].(map[string]any)
		props := body["properties"].(map[string]any)
		field, ok := props[tc.field].(map[string]any)
		if !ok {
			t.Fatalf("%s expected %s schema map, got %#v", tc.id, tc.field, props[tc.field])
		}
		childProps, ok := field["properties"].(map[string]any)
		if !ok || childProps[tc.childKey] == nil {
			t.Fatalf("%s expected compact %s to keep child %q, got %#v", tc.id, tc.field, tc.childKey, field)
		}
	}
}

func TestToolDescribeCompactKeepsUsefulOptionalChildrenForSmallNestedObjects(t *testing.T) {
	for _, tc := range []struct {
		id        string
		fieldPath []string
		childKey  string
	}{
		{id: "shipper.create", fieldPath: []string{"ContentInfo", "JsonInfo"}, childKey: "Keys"},
		{id: "index.create", fieldPath: []string{"KeyValue", "Value"}, childKey: "Delimiter"},
	} {
		var stdout, stderr bytes.Buffer
		code := Run([]string{"tool", "describe", tc.id}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("%s exit=%d stdout=%q stderr=%q", tc.id, code, stdout.String(), stderr.String())
		}
		var out map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
			t.Fatalf("%s invalid stdout json: %v stdout=%q", tc.id, err, stdout.String())
		}
		current := out["input_schema"].(map[string]any)["body"].(map[string]any)["properties"].(map[string]any)
		var field map[string]any
		for i, key := range tc.fieldPath {
			raw := current[key]
			next, ok := raw.(map[string]any)
			if !ok {
				t.Fatalf("%s expected map at %v, got %#v", tc.id, tc.fieldPath[:i+1], raw)
			}
			field = next
			if i == len(tc.fieldPath)-1 {
				break
			}
			if items, ok := next["items"].(map[string]any); ok {
				props, ok := items["properties"].(map[string]any)
				if !ok {
					t.Fatalf("%s expected items.properties at %v, got %#v", tc.id, tc.fieldPath[:i+1], items)
				}
				current = props
				continue
			}
			props, ok := next["properties"].(map[string]any)
			if !ok {
				t.Fatalf("%s expected properties at %v, got %#v", tc.id, tc.fieldPath[:i+1], next)
			}
			current = props
		}
		childProps, ok := field["properties"].(map[string]any)
		if !ok || childProps[tc.childKey] == nil {
			t.Fatalf("%s expected %v to keep child %q, got %#v", tc.id, tc.fieldPath, tc.childKey, field)
		}
	}
}

func TestToolDescribeDocumentsContextFieldMeaning(t *testing.T) {
	out, err := runTool(nil, []string{"describe", "topic.create"})
	if err != nil {
		t.Fatalf("runTool describe failed: %v", err)
	}
	got := out.(map[string]any)

	contextSchema, ok := got["context_schema"].(map[string]any)
	if !ok {
		t.Fatalf("expected context_schema map, got %#v", got["context_schema"])
	}
	ctxProps, ok := contextSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected context_schema.properties map, got %#v", contextSchema["properties"])
	}

	for _, key := range []string{"profile", "secrets_file", "region", "endpoint", "trace", "contract_digest", "execution"} {
		field, ok := ctxProps[key].(map[string]any)
		if !ok {
			t.Fatalf("expected context field %q to be documented as map, got %#v", key, ctxProps[key])
		}
		for _, attr := range []string{"description", "when_to_use", "default", "runtime_effect"} {
			if _, ok := field[attr]; !ok {
				t.Fatalf("expected context field %q to include %q: %#v", key, attr, field)
			}
		}
		if _, ok := field["example"]; ok {
			t.Fatalf("expected context field %q to omit example: %#v", key, field)
		}
	}
}

func TestToolDescribeContextRuntimeEffectsMatchCurrentSelectorAndTraceSemantics(t *testing.T) {
	out, err := runTool(nil, []string{"describe", "topic.create"})
	if err != nil {
		t.Fatalf("runTool describe failed: %v", err)
	}
	got := out.(map[string]any)

	contextSchema, ok := got["context_schema"].(map[string]any)
	if !ok {
		t.Fatalf("expected context_schema map, got %#v", got["context_schema"])
	}
	ctxProps, ok := contextSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected context_schema.properties map, got %#v", contextSchema["properties"])
	}

	secretsField, ok := ctxProps["secrets_file"].(map[string]any)
	if !ok {
		t.Fatalf("expected secrets_file field map, got %#v", ctxProps["secrets_file"])
	}
	secretsRuntimeEffect := asStringOrEmpty(secretsField["runtime_effect"])
	for _, want := range []string{"selectors first", "supported VOLCENGINE_*"} {
		if !strings.Contains(secretsRuntimeEffect, want) {
			t.Fatalf("expected secrets_file runtime_effect to mention %q, got %#v", want, secretsField["runtime_effect"])
		}
	}

	traceField, ok := ctxProps["trace"].(map[string]any)
	if !ok {
		t.Fatalf("expected trace field map, got %#v", ctxProps["trace"])
	}
	traceRuntimeEffect := asStringOrEmpty(traceField["runtime_effect"])
	for _, want := range []string{"trace directory", "legacy strict/default", "on/off"} {
		if !strings.Contains(traceRuntimeEffect, want) {
			t.Fatalf("expected trace runtime_effect to mention %q, got %#v", want, traceField["runtime_effect"])
		}
	}
}

func TestToolDescribeDocumentsProfileDiscoveryHint(t *testing.T) {
	out, err := runTool(nil, []string{"describe", "topic.create"})
	if err != nil {
		t.Fatalf("runTool describe failed: %v", err)
	}
	got := out.(map[string]any)

	contextSchema, ok := got["context_schema"].(map[string]any)
	if !ok {
		t.Fatalf("expected context_schema map, got %#v", got["context_schema"])
	}
	ctxProps, ok := contextSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected context_schema.properties map, got %#v", contextSchema["properties"])
	}
	profileField, ok := ctxProps["profile"].(map[string]any)
	if !ok {
		t.Fatalf("expected context_schema.properties.profile map, got %#v", ctxProps["profile"])
	}
	hint := asStringOrEmpty(profileField["discover_values"])
	if !strings.Contains(strings.ToLower(hint), "configure list") {
		t.Fatalf("expected profile discover_values to mention configure list, got %#v", profileField["discover_values"])
	}
}

func TestToolDescribeUsesSemanticVerbForDescribeActions(t *testing.T) {
	out, err := runTool(nil, []string{"describe", "topic.describe-topics"})
	if err != nil {
		t.Fatalf("runTool describe topic.describe-topics failed: %v", err)
	}
	got := out.(map[string]any)
	identity, ok := got["identity"].(map[string]any)
	if !ok {
		t.Fatalf("expected identity map, got %#v", got["identity"])
	}
	if asStringOrEmpty(identity["verb"]) != "list" {
		t.Fatalf("expected plural Describe action to expose list verb, got %#v", identity["verb"])
	}

	out, err = runTool(nil, []string{"describe", "topic.describe-topic"})
	if err != nil {
		t.Fatalf("runTool describe topic.describe-topic failed: %v", err)
	}
	got = out.(map[string]any)
	identity, ok = got["identity"].(map[string]any)
	if !ok {
		t.Fatalf("expected identity map, got %#v", got["identity"])
	}
	if asStringOrEmpty(identity["verb"]) != "get" {
		t.Fatalf("expected singular Describe action to expose get verb, got %#v", identity["verb"])
	}
}

func TestToolDescribeKeepsPostDescribeVerbAsDescribe(t *testing.T) {
	out, err := runTool(nil, []string{"describe", "log.describe-cursor"})
	if err != nil {
		t.Fatalf("runTool describe log.describe-cursor failed: %v", err)
	}
	got := out.(map[string]any)
	identity, ok := got["identity"].(map[string]any)
	if !ok {
		t.Fatalf("expected identity map, got %#v", got["identity"])
	}
	if asStringOrEmpty(identity["verb"]) != "describe" {
		t.Fatalf("expected POST Describe action to keep describe verb, got %#v", identity["verb"])
	}
}

func TestToolDescribeDropsUnusedLegacyContextFields(t *testing.T) {
	out, err := runTool(nil, []string{"describe", "topic.create"})
	if err != nil {
		t.Fatalf("runTool describe failed: %v", err)
	}
	got := out.(map[string]any)

	contextSchema, ok := got["context_schema"].(map[string]any)
	if !ok {
		t.Fatalf("expected context_schema map, got %#v", got["context_schema"])
	}
	ctxProps, ok := contextSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected context_schema.properties map, got %#v", contextSchema["properties"])
	}
	for _, key := range []string{"project_id", "dry_run"} {
		if _, ok := ctxProps[key]; ok {
			t.Fatalf("expected legacy context field %q to be removed from schema: %#v", key, ctxProps[key])
		}
	}
}

func TestToolDescribeDocumentsPageAllAlias(t *testing.T) {
	out, err := runTool(nil, []string{"describe", "topic.describe-topics"})
	if err != nil {
		t.Fatalf("runTool describe failed: %v", err)
	}
	got := out.(map[string]any)

	executionSchema, ok := got["execution_schema"].(map[string]any)
	if !ok {
		t.Fatalf("expected execution_schema map, got %#v", got["execution_schema"])
	}
	execProps, ok := executionSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected execution_schema.properties map, got %#v", executionSchema["properties"])
	}
	pageAll, ok := execProps["page_all"].(map[string]any)
	if !ok {
		t.Fatalf("expected page_all alias to be documented, got %#v", execProps["page_all"])
	}
	if !strings.Contains(strings.ToLower(asStringOrEmpty(pageAll["description"])), "alias") {
		t.Fatalf("expected page_all description to explain alias semantics, got %#v", pageAll["description"])
	}
}

func TestToolDescribeOmitsPageAllForUnsupportedAction(t *testing.T) {
	out, err := runTool(nil, []string{"describe", "project.create"})
	if err != nil {
		t.Fatalf("runTool describe failed: %v", err)
	}
	got := out.(map[string]any)

	executionSchema, ok := got["execution_schema"].(map[string]any)
	if !ok {
		t.Fatalf("expected execution_schema map, got %#v", got["execution_schema"])
	}
	execProps, ok := executionSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected execution_schema.properties map, got %#v", executionSchema["properties"])
	}
	if _, ok := execProps["page"]; ok {
		t.Fatalf("expected unsupported action to omit execution_schema.properties.page, got %#v", execProps["page"])
	}
	if _, ok := execProps["page_all"]; ok {
		t.Fatalf("expected unsupported action to omit execution_schema.properties.page_all, got %#v", execProps["page_all"])
	}

	contextSchema, ok := got["context_schema"].(map[string]any)
	if !ok {
		t.Fatalf("expected context_schema map, got %#v", got["context_schema"])
	}
	ctxProps, ok := contextSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected context_schema.properties map, got %#v", contextSchema["properties"])
	}
	executionField, ok := ctxProps["execution"].(map[string]any)
	if !ok {
		t.Fatalf("expected context.execution field doc, got %#v", ctxProps["execution"])
	}
	if strings.Contains(strings.ToLower(asStringOrEmpty(executionField["description"])), "pagination") {
		t.Fatalf("expected unsupported action execution description to avoid pagination hint, got %#v", executionField["description"])
	}
}

func parseToolActionsFromList(text string) []string {
	tokens := strings.Split(strings.TrimSpace(text), "\n")
	actions := make([]string, 0, len(tokens))
	for _, token := range tokens {
		t := strings.TrimSpace(token)
		if !strings.HasPrefix(t, "-") {
			continue
		}
		parts := strings.SplitN(t, " ", 2)
		if len(parts) != 2 {
			continue
		}
		candidate := strings.TrimSpace(parts[1])
		if candidate == "" {
			continue
		}
		if strings.Contains(candidate, " (") {
			continue
		}
		actions = append(actions, candidate)
	}
	sort.Strings(actions)
	return actions
}

func containsToolAction(actions []string, want string) bool {
	for _, action := range actions {
		if action == want {
			return true
		}
	}
	return false
}

func asStringOrEmpty(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func asOutputString(t *testing.T, v any) string {
	t.Helper()
	s, ok := v.(string)
	if !ok {
		t.Fatalf("expected string output, got %T", v)
	}
	return strings.TrimSpace(s)
}

func TestToolDescribeDigestValueMatchesContract(t *testing.T) {
	const topicIdentity = "topic.create"
	out, err := runTool(nil, []string{"describe", topicIdentity})
	if err != nil {
		t.Fatalf("runTool describe failed: %v", err)
	}
	got := out.(map[string]any)
	identity, ok := got["identity"].(map[string]any)
	if !ok {
		t.Fatalf("missing identity in describe output: %#v", got)
	}
	group := asStringOrEmpty(identity["group"])
	action := asStringOrEmpty(identity["action"])
	if group == "" || action == "" {
		t.Fatalf("describe identity missing group/action: %#v", identity)
	}
	tool, ok := loadToolByIdentity(group, action)
	if !ok {
		t.Fatalf("tool missing in catalog: %s.%s", group, action)
	}
	digest := got["contract_digest"].(map[string]any)
	want := toolContractForDigest(tool)
	if asStringOrEmpty(digest["value"]) != want {
		t.Fatalf("unexpected contract digest, want %q got %q", want, asStringOrEmpty(digest["value"]))
	}
}

func TestToolExecRequiresFileContextAndInput(t *testing.T) {
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	var stdout, stderr bytes.Buffer
	code := Run([]string{"tool", "exec", "topic.create-topic", "--context", "ctx.json", "--input", "file://req.json"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("unexpected exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	if !strings.Contains(stdout.String(), "file://") {
		t.Fatalf("expected file:// requirement in stdout envelope: %q", stdout.String())
	}
}

func TestToolExecUsesDryRunFromExecution(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	tmp := t.TempDir()
	ctxFile := filepath.Join(tmp, "ctx.json")
	reqFile := filepath.Join(tmp, "req.json")
	if err := osWriteJSON(ctxFile, map[string]any{
		"region": "cn-beijing",
		"execution": map[string]any{
			"dry_run": true,
		},
	}); err != nil {
		t.Fatalf("write context: %v", err)
	}
	if err := osWriteJSON(reqFile, map[string]any{
		"body": map[string]any{
			"ProjectId":  "pid",
			"ShardCount": 2,
			"TopicName":  "demo",
			"Ttl":        30,
		},
	}); err != nil {
		t.Fatalf("write request: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"tool", "exec", "topic.create-topic", "--context", "file://" + ctxFile, "--input", "file://" + reqFile}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unexpected exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v", err)
	}
	if out["status"] != "success" {
		t.Fatalf("unexpected status: %v", out["status"])
	}
	summary, ok := out["summary"].(map[string]any)
	if !ok || summary["dryRun"] != true {
		t.Fatalf("expected dryRun summary, got %#v", out["summary"])
	}
}

func TestToolExecAcceptsGlobalDryRunPrefix(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	tmp := t.TempDir()
	ctxFile := filepath.Join(tmp, "ctx.json")
	reqFile := filepath.Join(tmp, "req.json")
	if err := osWriteJSON(ctxFile, map[string]any{
		"region": "cn-beijing",
	}); err != nil {
		t.Fatalf("write context: %v", err)
	}
	if err := osWriteJSON(reqFile, map[string]any{
		"body": map[string]any{
			"ProjectId":  "pid",
			"ShardCount": 2,
			"TopicName":  "demo",
			"Ttl":        30,
		},
	}); err != nil {
		t.Fatalf("write request: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--dry-run", "tool", "exec", "topic.create-topic", "--context", "file://" + ctxFile, "--input", "file://" + reqFile}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unexpected exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v", err)
	}
	summary, ok := out["summary"].(map[string]any)
	if !ok || summary["dryRun"] != true {
		t.Fatalf("expected global prefix --dry-run to take effect, got %#v", out["summary"])
	}
}

func TestToolExecAcceptsGlobalDryRunTrailing(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	tmp := t.TempDir()
	ctxFile := filepath.Join(tmp, "ctx.json")
	reqFile := filepath.Join(tmp, "req.json")
	if err := osWriteJSON(ctxFile, map[string]any{
		"region": "cn-beijing",
	}); err != nil {
		t.Fatalf("write context: %v", err)
	}
	if err := osWriteJSON(reqFile, map[string]any{
		"body": map[string]any{
			"ProjectId":  "pid",
			"ShardCount": 2,
			"TopicName":  "demo",
			"Ttl":        30,
		},
	}); err != nil {
		t.Fatalf("write request: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"tool", "exec", "topic.create-topic", "--context", "file://" + ctxFile, "--input", "file://" + reqFile, "--dry-run"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unexpected exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v", err)
	}
	summary, ok := out["summary"].(map[string]any)
	if !ok || summary["dryRun"] != true {
		t.Fatalf("expected global trailing --dry-run to take effect, got %#v", out["summary"])
	}
}

func TestToolDescribeRejectsGlobalDryRunPrefix(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"--dry-run", "tool", "describe", "topic.create"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected non-zero exit when dry-run is used with tool describe")
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v stdout=%q", err, stdout.String())
	}
	if out["status"] != "failed" {
		t.Fatalf("expected failed status, got %#v", out["status"])
	}
	errObj, ok := out["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object, got %#v", out["error"])
	}
	if errObj["kind"] != "usage" {
		t.Fatalf("expected usage kind, got %#v", errObj["kind"])
	}
	if !strings.Contains(asStringOrEmpty(errObj["message"]), "--dry-run currently supports") {
		t.Fatalf("unexpected error message: %#v", errObj["message"])
	}
	if !strings.Contains(asStringOrEmpty(errObj["hint"]), "invalid --dry-run scope") {
		t.Fatalf("unexpected error hint: %#v", errObj["hint"])
	}
}

func TestToolExecRejectsMissingRequiredFieldWithContractPath(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	tmp := t.TempDir()
	ctxFile := filepath.Join(tmp, "ctx.json")
	reqFile := filepath.Join(tmp, "req.json")
	if err := osWriteJSON(ctxFile, map[string]any{
		"execution": map[string]any{
			"dry_run": true,
		},
	}); err != nil {
		t.Fatalf("write context: %v", err)
	}
	if err := osWriteJSON(reqFile, map[string]any{
		"body": map[string]any{
			"ShardCount": 2,
			"TopicName":  "demo",
			"Ttl":        30,
		},
	}); err != nil {
		t.Fatalf("write request: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"tool", "exec", "topic.create", "--context", "file://" + ctxFile, "--input", "file://" + reqFile}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected missing required field failure stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "missing required field: input.body.ProjectId") {
		t.Fatalf("expected missing ProjectId field path in stdout envelope, got %q", out)
	}
}

func TestToolExecResolvesUniqueVerbAlias(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	tmp := t.TempDir()
	ctxFile := filepath.Join(tmp, "ctx.json")
	reqFile := filepath.Join(tmp, "req.json")
	if err := osWriteJSON(ctxFile, map[string]any{
		"region": "cn-beijing",
		"execution": map[string]any{
			"dry_run": true,
		},
	}); err != nil {
		t.Fatalf("write context: %v", err)
	}
	if err := osWriteJSON(reqFile, map[string]any{
		"body": map[string]any{
			"ProjectId":  "pid",
			"ShardCount": 2,
			"TopicName":  "demo",
			"Ttl":        30,
		},
	}); err != nil {
		t.Fatalf("write request: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"tool", "exec", "topic.create", "--context", "file://" + ctxFile, "--input", "file://" + reqFile}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unexpected exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v", err)
	}
	if out["status"] != "success" {
		t.Fatalf("unexpected status: %v", out["status"])
	}
}

func TestToolExecUsesProjectionFromExecutionOnRawResult(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	tmp := t.TempDir()
	ctxFile := filepath.Join(tmp, "ctx.json")
	reqFile := filepath.Join(tmp, "req.json")
	if err := osWriteJSON(ctxFile, map[string]any{
		"region": "cn-beijing",
		"execution": map[string]any{
			"dry_run":    true,
			"projection": "valid",
		},
	}); err != nil {
		t.Fatalf("write context: %v", err)
	}
	if err := osWriteJSON(reqFile, map[string]any{
		"body": map[string]any{
			"ProjectId":  "pid",
			"ShardCount": 2,
			"TopicName":  "demo",
			"Ttl":        30,
		},
	}); err != nil {
		t.Fatalf("write request: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"tool", "exec", "topic.create-topic", "--context", "file://" + ctxFile, "--input", "file://" + reqFile}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unexpected exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v", err)
	}
	if out["data"] != true {
		t.Fatalf("expected projection to filter raw dry-run result, got %#v", out["data"])
	}
	if _, ok := out["summary"].(map[string]any); !ok {
		t.Fatalf("expected envelope summary to remain intact, got %#v", out["summary"])
	}
}

func TestToolExecProjectionAcceptsArrayInput(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	tmp := t.TempDir()
	ctxFile := filepath.Join(tmp, "ctx.json")
	reqFile := filepath.Join(tmp, "req.json")
	if err := osWriteJSON(ctxFile, map[string]any{
		"region": "cn-beijing",
		"execution": map[string]any{
			"dry_run":    true,
			"projection": []string{"valid"},
		},
	}); err != nil {
		t.Fatalf("write context: %v", err)
	}
	if err := osWriteJSON(reqFile, map[string]any{
		"body": map[string]any{
			"ProjectId":  "pid",
			"ShardCount": 2,
			"TopicName":  "demo",
			"Ttl":        30,
		},
	}); err != nil {
		t.Fatalf("write request: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"tool", "exec", "topic.create-topic", "--context", "file://" + ctxFile, "--input", "file://" + reqFile}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unexpected exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v", err)
	}
	if out["data"] != true {
		t.Fatalf("expected array projection to be honored, got %#v", out["data"])
	}
}

func TestToolExecProjectionKeepsPaginationMetadataForArrayResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/DescribeTopics":
			_, _ = w.Write([]byte(`{"Topics":[{"TopicId":"tid","TopicName":"demo"}],"Total":1}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", srv.URL)
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	tmp := t.TempDir()
	ctxFile := filepath.Join(tmp, "ctx.json")
	reqFile := filepath.Join(tmp, "req.json")
	if err := osWriteJSON(ctxFile, map[string]any{
		"region": "cn-beijing",
		"execution": map[string]any{
			"projection": "Topics[].{TopicId: TopicId, TopicName: TopicName}",
		},
	}); err != nil {
		t.Fatalf("write context: %v", err)
	}
	if err := osWriteJSON(reqFile, map[string]any{
		"query": map[string]any{
			"ProjectId": "pid",
		},
	}); err != nil {
		t.Fatalf("write request: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"tool", "exec", "topic.describe-topics", "--context", "file://" + ctxFile, "--input", "file://" + reqFile}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unexpected exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v stdout=%q", err, stdout.String())
	}
	data, ok := out["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected projected tool data to stay object-shaped, got %#v", out["data"])
	}
	items, ok := data["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("expected projected items list, got %#v", data["items"])
	}
	if total, _ := data["Total"].(float64); total != 1 {
		t.Fatalf("expected projected data to preserve Total, got %#v", data)
	}
}

func TestToolExecDigestMismatchWarnsOnly(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	tmp := t.TempDir()
	ctxFile := filepath.Join(tmp, "ctx.json")
	reqFile := filepath.Join(tmp, "req.json")
	if err := osWriteJSON(ctxFile, map[string]any{
		"region":          "cn-beijing",
		"contract_digest": "deadbeef",
		"execution": map[string]any{
			"dry_run": true,
		},
	}); err != nil {
		t.Fatalf("write context: %v", err)
	}
	if err := osWriteJSON(reqFile, map[string]any{
		"body": map[string]any{
			"ProjectId":  "pid",
			"ShardCount": 2,
			"TopicName":  "demo",
			"Ttl":        30,
		},
	}); err != nil {
		t.Fatalf("write request: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"tool", "exec", "topic.create-topic", "--context", "file://" + ctxFile, "--input", "file://" + reqFile}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unexpected exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v", err)
	}
	warnings, ok := out["warnings"].([]any)
	if !ok || len(warnings) == 0 {
		t.Fatalf("expected warnings in stdout: %#v", out)
	}
	digest, ok := out["contract_digest"].(map[string]any)
	if !ok {
		t.Fatalf("missing contract_digest: %#v", out)
	}
	if matched, ok := digest["matched"].(bool); !ok || matched {
		t.Fatalf("expected digest mismatch status: %#v", digest)
	}
}

func TestToolExecAppliesTopLevelJMESFilterToEnvelope(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	tmp := t.TempDir()
	ctxFile := filepath.Join(tmp, "ctx.json")
	reqFile := filepath.Join(tmp, "req.json")
	if err := osWriteJSON(ctxFile, map[string]any{
		"region": "cn-beijing",
		"execution": map[string]any{
			"dry_run": true,
		},
	}); err != nil {
		t.Fatalf("write context: %v", err)
	}
	if err := osWriteJSON(reqFile, map[string]any{
		"body": map[string]any{
			"ProjectId":  "pid",
			"ShardCount": 2,
			"TopicName":  "demo",
			"Ttl":        30,
		},
	}); err != nil {
		t.Fatalf("write request: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"--jmes-filter", "summary.deliveryMode", "tool", "exec", "topic.create-topic", "--context", "file://" + ctxFile, "--input", "file://" + reqFile}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unexpected exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var out string
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v", err)
	}
	if out != "stdout" {
		t.Fatalf("expected top-level jmes filter to target envelope summary, got %#v", out)
	}
}

func TestToolExecRejectsUnsupportedPageAll(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	tmp := t.TempDir()
	ctxFile := filepath.Join(tmp, "ctx.json")
	reqFile := filepath.Join(tmp, "req.json")
	if err := osWriteJSON(ctxFile, map[string]any{
		"region": "cn-beijing",
		"execution": map[string]any{
			"dry_run": true,
			"page": map[string]any{
				"all": true,
			},
		},
	}); err != nil {
		t.Fatalf("write context: %v", err)
	}
	if err := osWriteJSON(reqFile, map[string]any{
		"body": map[string]any{
			"ProjectId":  "pid",
			"ShardCount": 2,
			"TopicName":  "demo",
			"Ttl":        30,
		},
	}); err != nil {
		t.Fatalf("write request: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"tool", "exec", "topic.create-topic", "--context", "file://" + ctxFile, "--input", "file://" + reqFile}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected unsupported page.all failure stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
	if !strings.Contains(stdout.String(), "page.all") {
		t.Fatalf("expected page.all error in stdout envelope, got %q", stdout.String())
	}
}

func TestToolExecDryRunPageAllForSupportedAction(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	tmp := t.TempDir()
	ctxFile := filepath.Join(tmp, "ctx.json")
	reqFile := filepath.Join(tmp, "req.json")
	if err := osWriteJSON(ctxFile, map[string]any{
		"region": "cn-beijing",
		"execution": map[string]any{
			"dry_run": true,
			"page": map[string]any{
				"all": true,
			},
		},
	}); err != nil {
		t.Fatalf("write context: %v", err)
	}
	if err := osWriteJSON(reqFile, map[string]any{
		"query": map[string]any{
			"PageSize": 2,
		},
	}); err != nil {
		t.Fatalf("write request: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"tool", "exec", "topic.describe-topics", "--context", "file://" + ctxFile, "--input", "file://" + reqFile}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unexpected exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v", err)
	}
	data, ok := out["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected envelope data map, got %#v", out["data"])
	}
	pageAll, ok := data["page_all"].(map[string]any)
	if !ok || pageAll["requested"] != true {
		t.Fatalf("expected dry-run page_all annotation, got %#v", data["page_all"])
	}
}

func TestToolExecAllowsTrailingJMESFilter(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	tmp := t.TempDir()
	ctxFile := filepath.Join(tmp, "ctx.json")
	reqFile := filepath.Join(tmp, "req.json")
	if err := osWriteJSON(ctxFile, map[string]any{
		"region": "cn-beijing",
		"execution": map[string]any{
			"dry_run": true,
		},
		"trace": map[string]any{
			"dir": filepath.Join(tmp, "traces"),
		},
	}); err != nil {
		t.Fatalf("write context: %v", err)
	}
	if err := osWriteJSON(reqFile, map[string]any{
		"body": map[string]any{
			"ProjectName": "demo",
			"Region":      "cn-beijing",
		},
	}); err != nil {
		t.Fatalf("write request: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"tool", "exec", "project.create-project", "--context", "file://" + ctxFile, "--input", "file://" + reqFile, "--jmes-filter", "summary.deliveryMode"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unexpected exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var out string
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid stdout json: %v", err)
	}
	if out != "stdout" {
		t.Fatalf("expected trailing jmes filter to target envelope summary, got %#v", out)
	}
}

func osWriteJSON(path string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}
