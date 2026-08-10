package cli

import (
	"slices"
	"testing"
)

func TestCliGroupsMatchCurrentEdition(t *testing.T) {
	names := cliGroupNames()

	switch string(currentEdition()) {
	case "volclog-human":
		for _, want := range []string{
			"configure",
			"doctor",
			"skill",
			"tool",
			"workflow",
			"raw",
			"login",
			"logout",
			"sso",
			"project",
			"topic",
			"metric-topic",
			"index",
			"log",
			"host-group",
			"collector",
		} {
			if !slices.Contains(names, want) {
				t.Fatalf("volclog-human missing group %q in %v", want, names)
			}
		}
		if slices.Contains(names, "assistant") {
			t.Fatalf("assistant shortcut group should stay hidden from top-level groups: %v", names)
		}
	case "volclog":
		want := []string{"configure", "doctor", "skill", "tool", "workflow", "raw", "login", "logout", "sso"}
		if len(names) != len(want) {
			t.Fatalf("default volclog groups = %v, want only %v", names, want)
		}
		for _, group := range want {
			if !slices.Contains(names, group) {
				t.Fatalf("default volclog missing group %q in %v", group, names)
			}
		}
	default:
		t.Fatalf("unexpected edition %q", currentEdition())
	}
}

func TestCliGroupNamesStayAlignedWithCliGroups(t *testing.T) {
	groups := cliGroups()
	names := cliGroupNames()
	if len(groups) != len(names) {
		t.Fatalf("cliGroups len=%d cliGroupNames len=%d", len(groups), len(names))
	}
	for i, group := range groups {
		if group.Name != names[i] {
			t.Fatalf("cliGroupNames[%d]=%q want %q", i, names[i], group.Name)
		}
	}
}

func TestEditionRuntimeAvailability(t *testing.T) {
	switch string(currentEdition()) {
	case "volclog-human":
		for _, group := range []string{"project", "assistant", "tool", "workflow", "raw"} {
			if !isGroupEnabledInCurrentEdition(group) {
				t.Fatalf("volclog-human should enable %q", group)
			}
		}
	case "volclog":
		for _, group := range []string{"configure", "doctor", "skill", "tool", "workflow", "raw", "login", "logout", "sso"} {
			if !isGroupEnabledInCurrentEdition(group) {
				t.Fatalf("default volclog should enable %q", group)
			}
		}
		for _, group := range []string{"project", "topic", "metric-topic", "index", "log", "host-group", "collector", "assistant"} {
			if isGroupEnabledInCurrentEdition(group) {
				t.Fatalf("default volclog should reject %q", group)
			}
		}
	default:
		t.Fatalf("unexpected edition %q", currentEdition())
	}
}
