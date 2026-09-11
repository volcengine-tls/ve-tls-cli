//go:build human

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestShortcutTemplatesDoNotUseLegacyGeneratedAssets(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for _, name := range []string{"shortcut_meta.go", "api.go", "describe_helpers.go"} {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, forbidden := range []string{
			"generatedRequestTemplates",
			"specialRequestTemplate",
			"shortcutActionOps",
		} {
			if strings.Contains(string(raw), forbidden) {
				t.Errorf("%s should not reference %q", name, forbidden)
			}
		}
	}
}

func TestShortcutSpecialSamplesRemainUsable(t *testing.T) {
	for _, mode := range []string{"required", "full"} {
		t.Run("index_"+mode, func(t *testing.T) {
			template := runShortcutTemplate(t, "index", "create", mode)
			if _, ok := template["TopicId"]; ok {
				t.Fatal("index shortcut template should omit TopicId supplied by --topic-id")
			}
			for _, field := range []string{"FullText", "KeyValue"} {
				if _, ok := template[field]; !ok {
					t.Errorf("index sample is missing useful field %q", field)
				}
			}
			for _, unsupported := range []string{
				"EnablePhraseIndex",
				"LogReduce",
				"LogReduceBlackList",
				"LogReduceWhiteList",
			} {
				if _, ok := template[unsupported]; ok {
					t.Errorf("index sample contains shortcut-unsupported field %q", unsupported)
				}
			}
		})

		t.Run("log_put_"+mode, func(t *testing.T) {
			template := runShortcutTemplate(t, "log", "put", mode)
			groups, ok := template["LogGroups"].([]any)
			if !ok || len(groups) == 0 {
				t.Fatalf("LogGroups = %#v, want a non-empty array", template["LogGroups"])
			}
			group, ok := groups[0].(map[string]any)
			if !ok {
				t.Fatalf("LogGroups[0] = %#v, want object", groups[0])
			}
			logs, ok := group["Logs"].([]any)
			if !ok || len(logs) == 0 {
				t.Fatalf("Logs = %#v, want a non-empty array", group["Logs"])
			}
			logEntry, ok := logs[0].(map[string]any)
			if !ok {
				t.Fatalf("Logs[0] = %#v, want object", logs[0])
			}
			if logEntry["Time"] != float64(1710374400000) {
				t.Fatalf("Logs[0].Time = %#v, want Unix milliseconds sample", logEntry["Time"])
			}
			if _, ok := logEntry["Contents"]; !ok {
				t.Fatal("Logs[0] is missing Contents sample")
			}
		})
	}
}

func TestShortcutRequestTemplateKeepsTrailingNewline(t *testing.T) {
	spec, ok := lookupShortcutSpec("topic", "create")
	if !ok {
		t.Fatal("topic create shortcut not found")
	}
	template, err := shortcutRequestTemplateOutput(spec, "required")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(template, "\n") {
		t.Fatalf("template should end with a newline: %q", template)
	}
	if strings.HasSuffix(template, "\n\n") {
		t.Fatalf("template should contain exactly one trailing newline: %q", template)
	}
}

func runShortcutTemplate(t *testing.T, group, command, mode string) map[string]any {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(
		[]string{group, command, "--print-request-template=" + mode},
		&stdout,
		&stderr,
	)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var template map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &template); err != nil {
		t.Fatalf("decode template: %v\nstdout=%q", err, stdout.String())
	}
	return template
}
