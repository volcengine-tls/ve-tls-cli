//go:build !human

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultEditionRunSourceDoesNotReferenceShortcutRunners(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	src, err := os.ReadFile(filepath.Join(root, "run.go"))
	if err != nil {
		t.Fatalf("read run.go: %v", err)
	}
	text := string(src)
	for _, forbidden := range []string{
		"runProject(",
		"runTopic(",
		"runMetricTopic(",
		"runIndex(",
		"runLog(",
		"runHostGroup(",
		"runCollector(",
		"runAssistant(",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("default volclog shared run.go should not directly reference %q", forbidden)
		}
	}
}

func TestDefaultEditionSharedUsageSourceDoesNotDefineShortcutUsage(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	src, err := os.ReadFile(filepath.Join(root, "usage.go"))
	if err != nil {
		t.Fatalf("read usage.go: %v", err)
	}
	text := string(src)
	for _, forbidden := range []string{
		"func usageProject()",
		"func usageTopic()",
		"func usageMetricTopic()",
		"func usageIndex()",
		"func usageLog()",
		"func usageHostGroup()",
		"func usageCollector()",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("default volclog shared usage.go should not define %q", forbidden)
		}
	}
}
