package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillListOutputsBundledSkills(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"skill", "list"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid json: %v stdout=%s", err, stdout.String())
	}
	skills, ok := out["Skills"].([]any)
	if !ok || len(skills) == 0 {
		t.Fatalf("missing skills list: %v", out)
	}
	found := false
	for _, item := range skills {
		if item == "volclog-shared" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected bundled skill volclog-shared: %v", skills)
	}
}

func TestSkillInstallRequiresDir(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"skill", "install"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "volclog skill install --dir /path/to/agent/skills") {
		t.Fatalf("unexpected usage text: %s", stdout.String())
	}
}

func TestSkillInstallAllToTargetDir(t *testing.T) {
	dest := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := Run([]string{"skill", "install", "--dir", dest}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}

	for _, rel := range []string{
		filepath.Join("volclog-shared", "SKILL.md"),
		filepath.Join("volclog-log", "references", "log-playbook.md"),
	} {
		if _, err := os.Stat(filepath.Join(dest, rel)); err != nil {
			t.Fatalf("missing installed file %s: %v", rel, err)
		}
	}
}

func TestSkillInstallNamedSubset(t *testing.T) {
	dest := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := Run([]string{"skill", "install", "--dir", dest, "--name", "volclog-project"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}

	if _, err := os.Stat(filepath.Join(dest, "volclog-project", "SKILL.md")); err != nil {
		t.Fatalf("missing installed named skill: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "volclog-log", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("unexpected extra skill installation, err=%v", err)
	}
}
