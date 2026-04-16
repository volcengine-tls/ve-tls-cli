package cli

import (
	"strings"
	"testing"
)

func TestUsageTextIncludesAllCliGroupsAndGlobalFlags(t *testing.T) {
	text := usageText()
	for _, group := range cliGroups() {
		if !strings.Contains(text, group.Name) {
			t.Fatalf("usageText missing group %q", group.Name)
		}
	}
	for _, flag := range cliGlobalFlagSpecs() {
		if flag.Name == "-h" {
			continue
		}
		if !strings.Contains(text, flag.Usage) {
			t.Fatalf("usageText missing flag usage %q", flag.Usage)
		}
		if strings.TrimSpace(flag.Description) != "" && !strings.Contains(text, flag.Description) {
			t.Fatalf("usageText missing flag description %q", flag.Description)
		}
	}
}

func TestToolCommandAppearsInTopLevelUsage(t *testing.T) {
	text := usageText()
	if !strings.Contains(text, "  tool") {
		t.Fatalf("tool group should appear in top-level usage: %s", text)
	}
	if !strings.Contains(text, "  raw") {
		t.Fatalf("raw group should appear in top-level usage: %s", text)
	}
	if strings.Contains(text, "  capabilities") {
		t.Fatalf("capabilities should not appear in top-level usage anymore: %s", text)
	}
}

func TestUsageTextPrioritizesPrimaryGroupsForAgents(t *testing.T) {
	text := usageText()
	if !strings.Contains(text, "主入口（Agent / 自动化优先）:") {
		t.Fatalf("missing primary groups section: %q", text)
	}
	if !strings.Contains(text, "次级入口（仅在你已明确目标资源时使用）:") {
		t.Fatalf("missing secondary groups section: %q", text)
	}
	if !strings.Contains(text, "需要结构化执行时使用 tool exec") || !strings.Contains(text, "原始 transport 调用使用 raw") {
		t.Fatalf("missing agent boundary guidance: %q", text)
	}
	if strings.Contains(text, "推荐流程:") {
		t.Fatalf("top-level usage should not contain recommended flow: %q", text)
	}
	if strings.Contains(text, "capabilities") {
		t.Fatalf("top-level usage should hide capabilities from agent-facing entrypoints: %q", text)
	}
	if strings.Index(text, "tool") > strings.Index(text, "project") {
		t.Fatalf("expected tool to appear before project in top-level usage: %q", text)
	}
}

func TestCliGroupsNoLongerExposeCommandsGroup(t *testing.T) {
	for _, group := range cliGroups() {
		if group.Name == "commands" {
			t.Fatalf("unexpected deprecated group: %q", group.Name)
		}
	}
}

func TestCliGroupsIncludeHostGroupAndCollector(t *testing.T) {
	groups := cliGroupNames()
	joined := strings.Join(groups, ",")
	for _, want := range []string{"host-group", "collector"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing group %q in %q", want, joined)
		}
	}
}

func TestCliGroupsHideAssistantShortcutGroup(t *testing.T) {
	groups := strings.Join(cliGroupNames(), ",")
	if strings.Contains(groups, "assistant") {
		t.Fatalf("assistant shortcut group should stay hidden from top-level cli groups: %q", groups)
	}
}

func TestParseTopicListQueryRejectsConflictingTopicSelectors(t *testing.T) {
	_, err := parseTopicListQuery([]string{"--topic-name", "demo", "--topic-id", "tid"}, false)
	if err == nil || !strings.Contains(err.Error(), "TopicName and TopicId") {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestParseTopicListQueryParsesSharedFlags(t *testing.T) {
	query, err := parseTopicListQuery([]string{"--project-id", "pid", "--topic-name", "demo", "--is-full-name", "--order-by-project"}, false)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if query["ProjectId"] != "pid" {
		t.Fatalf("unexpected ProjectId: %q", query["ProjectId"])
	}
	if query["TopicName"] != "demo" {
		t.Fatalf("unexpected TopicName: %q", query["TopicName"])
	}
	if query["IsFullName"] != "true" {
		t.Fatalf("unexpected IsFullName: %q", query["IsFullName"])
	}
	if query["OrderByProject"] != "true" {
		t.Fatalf("unexpected OrderByProject: %q", query["OrderByProject"])
	}
}

func TestParseTopicListQueryAllowsCursorWhenEnabled(t *testing.T) {
	query, err := parseTopicListQuery([]string{"--cursor", "next"}, true)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if query["Cursor"] != "next" {
		t.Fatalf("unexpected Cursor: %q", query["Cursor"])
	}
}
