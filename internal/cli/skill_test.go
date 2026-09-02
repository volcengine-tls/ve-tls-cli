package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	bundledskills "github.com/volcengine-tls/ve-tls-cli"
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
	if !ok {
		t.Fatalf("missing skills list: %v", out)
	}
	if gotTotal, _ := out["Total"].(float64); int(gotTotal) != len(skills) {
		t.Fatalf("total mismatch: %v", out)
	}
	if len(skills) == 0 {
		t.Fatalf("expected bundled skills in this checkout")
	}
	var foundCore, foundLogCollector bool
	for _, skill := range skills {
		name, _ := skill.(string)
		if name == "volclog-core" {
			foundCore = true
		}
		if name == "tls-logcollector" {
			foundLogCollector = true
		}
	}
	if !foundCore {
		t.Fatalf("expected volclog-core in bundled skills: %v", out)
	}
	if !foundLogCollector {
		t.Fatalf("expected tls-logcollector in bundled skills: %v", out)
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
	available, err := bundledskills.List()
	if err != nil {
		t.Fatalf("list bundled skills: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"skill", "install", "--dir", dest}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if len(available) == 0 {
		entries, err := os.ReadDir(dest)
		if err != nil {
			t.Fatalf("read dest: %v", err)
		}
		if len(entries) != 0 {
			t.Fatalf("expected no installed skills, got %d entries", len(entries))
		}
		return
	}

	for _, rel := range []string{
		filepath.Join("volclog-core", "SKILL.md"),
		filepath.Join("volclog-core", "references", "routing.md"),
		filepath.Join("volclog-core", "references", "sops.md"),
		filepath.Join("volclog-core", "references", "best-practices.md"),
		filepath.Join("tls-logcollector", "SKILL.md"),
		filepath.Join("tls-logcollector", "agents", "openai.yaml"),
		filepath.Join("tls-logcollector", "references", "config-validation.md"),
		filepath.Join("tls-logcollector", "references", "tls-resources.md"),
		filepath.Join("tls-logcollector", "references", "linux-host.md"),
		filepath.Join("tls-logcollector", "references", "kubernetes-daemonset.md"),
		filepath.Join("tls-logcollector", "references", "kubernetes-controller.md"),
		filepath.Join("tls-logcollector", "references", "verification.md"),
	} {
		if _, err := os.Stat(filepath.Join(dest, rel)); err != nil {
			t.Fatalf("missing installed file %s: %v", rel, err)
		}
	}
}

func TestSkillInstallNamedSubset(t *testing.T) {
	dest := t.TempDir()
	available, err := bundledskills.List()
	if err != nil {
		t.Fatalf("list bundled skills: %v", err)
	}
	if len(available) == 0 {
		t.Fatal("expected bundled skills in this checkout")
	}

	var stdout, stderr bytes.Buffer
	code := Run([]string{"skill", "install", "--dir", dest, "--name", "volclog-core"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}

	if _, err := os.Stat(filepath.Join(dest, "volclog-core", "SKILL.md")); err != nil {
		t.Fatalf("missing installed named skill: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "volclog-shared", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("unexpected extra skill installation, err=%v", err)
	}
}
