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

func TestCliGroupsNoLongerExposeCommandsGroup(t *testing.T) {
	for _, group := range cliGroups() {
		if group.Name == "commands" {
			t.Fatalf("unexpected deprecated group: %q", group.Name)
		}
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
