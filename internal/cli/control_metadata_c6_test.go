package cli

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestC6DescribeControlsAndCompactBudget(t *testing.T) {
	for _, id := range []string{"topic.create", "topic.describe-topics"} {
		for _, view := range []string{"compact", "full"} {
			t.Run(id+"/"+view, func(t *testing.T) {
				var stdout, stderr bytes.Buffer
				code := Run([]string{"tool", "describe", id, "--view", view}, &stdout, &stderr)
				if code != 0 || stderr.Len() != 0 {
					t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
				}
				if view == "compact" && stdout.Len() > 8192 {
					t.Fatalf("compact describe is %d bytes, want at most 8192", stdout.Len())
				}
				var output map[string]any
				if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
					t.Fatal(err)
				}
				object := func(value any, name string) map[string]any {
					t.Helper()
					result, ok := value.(map[string]any)
					if !ok {
						t.Fatalf("%s is not an object: %#v", name, value)
					}
					return result
				}
				execution := object(output["execution_schema"], "execution_schema")
				props := object(execution["properties"], "execution_schema.properties")
				for name, spec := range map[string]struct {
					kind  string
					terms []string
				}{
					"non_interactive": {"boolean", []string{"false", "non-TTY"}},
					"allow_write":     {"boolean", []string{"CLI", "IAM", "POST log.search", "global", "false"}},
					"timeout":         {"string", []string{"Go duration", "invocation", "default", "profile HTTP", "global"}},
				} {
					field := object(props[name], "execution."+name)
					if field["type"] != spec.kind {
						t.Fatalf("execution.%s type=%#v, want %s", name, field["type"], spec.kind)
					}
					description, _ := field["description"].(string)
					for _, term := range spec.terms {
						if !strings.Contains(description, term) {
							t.Fatalf("execution.%s description=%q missing %q", name, description, term)
						}
					}
				}
				for _, name := range []string{"dry_run", "projection", "artifact", "output", "output_mode"} {
					if _, exists := props[name]; !exists {
						t.Fatalf("existing execution control %s missing", name)
					}
				}
				if view == "compact" {
					for name, forms := range map[string][]any{
						"projection": {"string", "array", "object{jmes:string}"},
						"artifact":   {"boolean", "string", "object{dir:string, path:string}"},
					} {
						accepted := object(props[name], name)["accepts"]
						if !reflect.DeepEqual(accepted, forms) {
							t.Fatalf("%s accepts=%#v, want %#v", name, accepted, forms)
						}
					}
				}
				context := object(output["context_schema"], "context_schema")
				contextProps := object(context["properties"], "context_schema.properties")
				trace := object(contextProps["trace"], "context.trace")
				effect, _ := trace["runtime_effect"].(string)
				for _, term := range []string{"false or enabled:false", "on/true/1/yes/enabled/strict/default", "off", "rejected"} {
					if !strings.Contains(effect, term) {
						t.Fatalf("trace runtime effect=%q missing %q", effect, term)
					}
				}
				contextExecution := object(contextProps["execution"], "context.execution")
				if view == "compact" {
					if contextExecution["schema_ref"] != "execution_schema" {
						t.Fatalf("compact context.execution=%#v", contextExecution)
					}
				} else {
					contextExecutionProps := object(contextExecution["properties"], "context.execution.properties")
					for _, name := range []string{"non_interactive", "allow_write", "timeout"} {
						if !reflect.DeepEqual(contextExecutionProps[name], props[name]) {
							t.Fatalf("context.execution.%s differs from execution_schema", name)
						}
					}
					variants, ok := trace["oneOf"].([]any)
					if !ok {
						t.Fatalf("trace.oneOf=%#v", trace["oneOf"])
					}
					found := false
					for _, raw := range variants {
						form := object(raw, "trace form")
						if form["type"] != "object" {
							continue
						}
						redact := object(object(form["properties"], "trace properties")["redact"], "trace.redact")
						want := []any{"on", "true", "1", "yes", "enabled", "strict", "default"}
						if !reflect.DeepEqual(redact["enum"], want) {
							t.Fatalf("trace.redact enum=%#v, want %#v", redact["enum"], want)
						}
						found = true
					}
					if !found {
						t.Fatal("trace object form missing")
					}
				}
			})
		}
	}
}

func TestC6GlobalControlFlagMetadata(t *testing.T) {
	flags := make(map[string]cliGlobalFlagSpec)
	for _, flag := range cliGlobalFlagSpecs() {
		flags[flag.Name] = flag
	}
	for name, takesValue := range map[string]bool{
		"--non-interactive": false,
		"--allow-write":     false,
		"--timeout":         true,
		"--trace-redact":    true,
	} {
		flag, ok := flags[name]
		if !ok || flag.TakesValue != takesValue || flag.Usage == "" || flag.Description == "" {
			t.Fatalf("global flag %s metadata=%#v", name, flag)
		}
	}
	if flags["--trace-redact"].Usage != "--trace-redact <on>" || !strings.Contains(flags["--trace-redact"].Description, "off") {
		t.Fatalf("trace redact metadata=%#v", flags["--trace-redact"])
	}
	if !strings.Contains(flags["--allow-write"].Description, "IAM") || !strings.Contains(flags["--timeout"].Description, "Go duration") {
		t.Fatalf("write/timeout metadata=%#v / %#v", flags["--allow-write"], flags["--timeout"])
	}
}
