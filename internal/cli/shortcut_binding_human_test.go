//go:build human

package cli

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestShortcutSpecsFormAValidTypedOverlay(t *testing.T) {
	specs := shortcutSpecs()
	if got, want := len(specs), 36; got != want {
		t.Fatalf("shortcut count=%d, want %d", got, want)
	}
	if err := validateShortcutSpecs(specs); err != nil {
		t.Fatal(err)
	}
}

func TestShortcutSpecsUseExplicitTargets(t *testing.T) {
	for _, spec := range shortcutSpecs() {
		t.Run(spec.Group+"/"+spec.Command, func(t *testing.T) {
			switch spec.Kind {
			case shortcutKindOperation:
				if strings.TrimSpace(spec.OperationID) == "" {
					t.Fatal("operation shortcut must declare OperationID")
				}
				if strings.TrimSpace(spec.WorkflowID) != "" {
					t.Fatal("operation shortcut must not declare WorkflowID")
				}
			case shortcutKindWorkflow:
				if strings.TrimSpace(spec.WorkflowID) == "" {
					t.Fatal("workflow shortcut must declare WorkflowID")
				}
				if strings.TrimSpace(spec.OperationID) != "" {
					t.Fatal("workflow shortcut must not declare OperationID")
				}
			case shortcutKindSpecial:
				if strings.TrimSpace(spec.OperationID) != "" || strings.TrimSpace(spec.WorkflowID) != "" {
					t.Fatal("special shortcut must not declare an operation or workflow")
				}
			default:
				t.Fatalf("unsupported shortcut kind %q", spec.Kind)
			}
		})
	}
}

func TestShortcutBindingsHaveUniqueAliases(t *testing.T) {
	for _, spec := range shortcutSpecs() {
		t.Run(spec.Group+"/"+spec.Command, func(t *testing.T) {
			seen := map[string]string{}
			for _, binding := range spec.Bindings {
				for _, alias := range binding.Aliases {
					key := normalizeToken(alias)
					if previous, ok := seen[key]; ok {
						t.Fatalf("duplicate alias %q on %s and %s", alias, previous, binding.Name)
					}
					seen[key] = binding.Name
				}
			}
		})
	}
}

func TestShortcutPublicInterfaceComesFromTarget(t *testing.T) {
	for _, spec := range shortcutSpecs() {
		t.Run(spec.Group+"/"+spec.Command, func(t *testing.T) {
			target, err := resolveShortcutTarget(spec)
			if err != nil {
				t.Fatal(err)
			}
			if strings.TrimSpace(target.Method) == "" || strings.TrimSpace(target.Path) == "" {
				t.Fatalf("target interface is incomplete: %#v", target)
			}
			if spec.Kind == shortcutKindOperation && strings.TrimSpace(target.APIAction) == "" {
				t.Fatalf("operation target API action is empty: %#v", target)
			}
		})
	}
}

func TestShortcutBehaviorOverlayPinsLegacyDifferences(t *testing.T) {
	assertDefault := func(identity, id, binding, source string, value any) {
		t.Helper()
		spec := mustShortcutSpec(t, identity)
		for _, item := range spec.Defaults {
			if item.ID != id {
				continue
			}
			if item.Binding != binding || item.Source != source || !reflect.DeepEqual(item.Value, value) {
				t.Fatalf("%s default %s=%#v, want binding=%s source=%s value=%#v", identity, id, item, binding, source, value)
			}
			return
		}
		t.Fatalf("%s is missing default %s", identity, id)
	}
	assertValidator := func(identity, id string) {
		t.Helper()
		for _, item := range mustShortcutSpec(t, identity).Validators {
			if item.ID == id {
				return
			}
		}
		t.Fatalf("%s is missing validator %s", identity, id)
	}

	assertDefault("project.create", shortcutDefaultProfileRegion, "Region", "profile.region", nil)
	for _, identity := range []string{"topic.create", "metric-topic.create"} {
		assertDefault(identity, shortcutDefaultCreateTTL30, "Ttl", "constant", 30)
		assertDefault(identity, shortcutDefaultCreateShardCount2, "ShardCount", "constant", 2)
		assertValidator(identity, shortcutValidatorAutoSplitMax)
	}
	for _, identity := range []string{"topic.list", "metric-topic.list"} {
		assertValidator(identity, shortcutValidatorTopicNameID)
	}
	for _, identity := range []string{"topic.create", "topic.modify"} {
		assertValidator(identity, shortcutValidatorTimeKeyFormatPair)
		assertValidator(identity, shortcutValidatorHotTTLSum)
	}
	for _, identity := range []string{"index.create", "index.modify"} {
		assertValidator(identity, shortcutValidatorIndexBody)
	}
	for _, identity := range []string{"project.list", "topic.list", "metric-topic.list", "host-group.list", "collector.list"} {
		spec := mustShortcutSpec(t, identity)
		if got, want := spec.ResultTransforms, []shortcutResultTransform{{ID: shortcutTransformPageNumberList}}; !reflect.DeepEqual(got, want) {
			t.Fatalf("%s transforms=%#v, want %#v", identity, got, want)
		}
	}
}

func TestShortcutPassthroughBindingsAreExplicitlyLimited(t *testing.T) {
	var got []string
	for _, spec := range shortcutSpecs() {
		for _, binding := range spec.Bindings {
			if binding.Role != shortcutBindingPassthrough {
				continue
			}
			got = append(got, spec.OperationID+":"+binding.SchemaPath)
		}
	}
	sort.Strings(got)
	want := []string{
		"collector.modify-rule:body.TopicId",
		"host-group.modify-host-group:body.IamProjectName",
		"topic.create:body.TimeFormat",
		"topic.create:body.TimeKey",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("passthrough bindings=%v, want %v", got, want)
	}
}

func TestShortcutWorkflowTemplatesUseDeclaredOperations(t *testing.T) {
	for _, identity := range []string{"log.export", "log.export-analysis"} {
		spec := mustShortcutSpec(t, identity)
		target, err := resolveShortcutTarget(spec)
		if err != nil {
			t.Fatalf("%s target: %v", identity, err)
		}
		if !target.IsWorkflow || !target.HasOperation {
			t.Fatalf("%s target does not carry its workflow operation: %#v", identity, target)
		}
		if got, want := string(target.Operation.ID), "log.search"; got != want {
			t.Fatalf("%s backing operation=%q, want %q", identity, got, want)
		}
		if got, want := target.Workflow.OperationIDs, []string{"log.search"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("%s workflow operations=%v, want %v", identity, got, want)
		}
	}
}

func TestResolveWorkflowOperationRejectsInterfaceMetadataDrift(t *testing.T) {
	var base workflowCatalog
	for _, workflow := range workflowCatalogSource() {
		if workflow.ID == "log.export" {
			base = workflow
			break
		}
	}
	if base.ID == "" {
		t.Fatal("workflow log.export not found")
	}

	tests := map[string]func(*workflowCatalog){
		"method": func(workflow *workflowCatalog) {
			workflow.Method = "GET"
		},
		"path": func(workflow *workflowCatalog) {
			workflow.Path = "/DescribeProjects"
		},
		"api group": func(workflow *workflowCatalog) {
			workflow.APIGroup = "project"
		},
		"api action": func(workflow *workflowCatalog) {
			workflow.APIAction = "DescribeProjects"
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			workflow := base
			mutate(&workflow)
			_, err := resolveWorkflowOperation(workflow)
			if err == nil || !strings.Contains(err.Error(), "interface metadata drifts") {
				t.Fatalf("resolveWorkflowOperation() error=%v, want interface metadata drift", err)
			}
		})
	}
}

func TestValidateShortcutBindingsRejectsMissingWorkflowSchemaPath(t *testing.T) {
	spec := mustShortcutSpec(t, "log.export")
	target, err := resolveShortcutTarget(spec)
	if err != nil {
		t.Fatal(err)
	}
	spec.Bindings = append(spec.Bindings, shortcutBindingParam(
		"NotInSearchLogs",
		"--not-in-search-logs",
		"body",
		false,
		"string",
		"test-only missing field",
	))

	err = validateShortcutBindings(spec, target)
	if err == nil || !strings.Contains(err.Error(), `missing schema path "body.NotInSearchLogs"`) {
		t.Fatalf("validateShortcutBindings() error=%v, want missing workflow schema path", err)
	}
}

func TestValidateShortcutBindingsRejectsWorkflowSchemaTypeMismatch(t *testing.T) {
	spec := mustShortcutSpec(t, "log.export")
	target, err := resolveShortcutTarget(spec)
	if err != nil {
		t.Fatal(err)
	}
	spec.Bindings = append([]shortcutBinding(nil), spec.Bindings...)
	for index := range spec.Bindings {
		if spec.Bindings[index].Name == "TopicId" {
			spec.Bindings[index].Type = "integer"
			break
		}
	}

	err = validateShortcutBindings(spec, target)
	if err == nil || !strings.Contains(err.Error(), `type "integer" is incompatible with schema path "body.TopicId"`) {
		t.Fatalf("validateShortcutBindings() error=%v, want workflow schema type mismatch", err)
	}
}

func mustShortcutSpec(t *testing.T, identity string) shortcutCommandSpec {
	t.Helper()
	group, command, ok := strings.Cut(identity, ".")
	if !ok {
		t.Fatalf("invalid shortcut identity %q", identity)
	}
	spec, ok := lookupShortcutSpec(group, command)
	if !ok {
		t.Fatalf("shortcut %s not found", identity)
	}
	return spec
}
