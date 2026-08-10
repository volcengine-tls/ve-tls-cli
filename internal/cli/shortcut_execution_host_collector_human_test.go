//go:build human

package cli

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestHostGroupListAllUsesShortcutOnlyPaginationOverride(t *testing.T) {
	before, ok := loadToolOperation("host-group.describe-host-groups-v2")
	if !ok {
		t.Fatal("host-group list operation is unavailable")
	}
	if before.Pagination != nil {
		t.Fatalf("public host-group list pagination = %#v, want nil", before.Pagination)
	}

	ctx, transport := newShortcutExecutionCaptureContext(t, []shortcutExecutionResponse{
		{body: `{"HostGroupHostsRulesInfos":[{"HostGroupId":"h1"},{"HostGroupId":"h2"}]}`},
		{body: `{"HostGroupHostsRulesInfos":[{"HostGroupId":"h3"}]}`},
	})
	out, err := hostGroupList(ctx, []string{"--all", "--page-size", "2"})
	if err != nil {
		t.Fatalf("host-group list --all: %v", err)
	}
	data, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want object", out)
	}
	if data["Total"] != 3 {
		t.Fatalf("Total = %#v, want 3", data["Total"])
	}
	if len(transport.requests) != 2 {
		t.Fatalf("request count = %d, want 2", len(transport.requests))
	}
	if ctx.PaginationMeta == nil || ctx.PaginationMeta["pageCount"] != 2 {
		t.Fatalf("pagination metadata = %#v, want two merged pages", ctx.PaginationMeta)
	}

	after, ok := loadToolOperation("host-group.describe-host-groups-v2")
	if !ok {
		t.Fatal("host-group list operation disappeared")
	}
	if after.Pagination != nil {
		t.Fatalf("public host-group list pagination was mutated: %#v", after.Pagination)
	}
}

func TestCollectorListAllForcesTotalAndPreservesPaginationFailures(t *testing.T) {
	t.Run("success forces total when service omits it", func(t *testing.T) {
		ctx, transport := newShortcutExecutionCaptureContext(t, []shortcutExecutionResponse{
			{body: `{"Rules":[{"RuleId":"r1"},{"RuleId":"r2"}]}`},
			{body: `{"Rules":[{"RuleId":"r3"}]}`},
		})
		out, err := collectorList(ctx, []string{"--all", "--page-size", "2"})
		if err != nil {
			t.Fatalf("collector list --all: %v", err)
		}
		data, ok := out.(map[string]any)
		if !ok {
			t.Fatalf("result type = %T, want object", out)
		}
		if data["Total"] != 3 {
			t.Fatalf("Total = %#v, want 3", data["Total"])
		}
		if len(transport.requests) != 2 {
			t.Fatalf("request count = %d, want 2", len(transport.requests))
		}
		if ctx.PaginationMeta == nil || ctx.PaginationMeta["pageCount"] != 2 {
			t.Fatalf("pagination metadata = %#v, want two merged pages", ctx.PaginationMeta)
		}
	})

	t.Run("page-number conflict keeps human wording and no metadata", func(t *testing.T) {
		for _, run := range []func(*Context) (any, error){
			func(ctx *Context) (any, error) {
				return hostGroupList(ctx, []string{"--all", "--page-number", "2"})
			},
			func(ctx *Context) (any, error) {
				return collectorList(ctx, []string{"--all", "--page-number", "2"})
			},
		} {
			ctx, _ := newShortcutExecutionCaptureContext(t, nil)
			_, err := run(ctx)
			if err == nil || err.Error() != "--all cannot be used with PageNumber" {
				t.Fatalf("error = %v", err)
			}
			if ctx.PaginationMeta != nil {
				t.Fatalf("pagination metadata = %#v, want nil", ctx.PaginationMeta)
			}
		}
	})

	t.Run("transport error keeps pagination metadata nil", func(t *testing.T) {
		ctx, _ := newShortcutExecutionCaptureContext(t, []shortcutExecutionResponse{
			{err: errors.New("collector transport failed")},
		})
		_, err := collectorList(ctx, []string{"--all"})
		if err == nil || !strings.Contains(err.Error(), "collector transport failed") {
			t.Fatalf("error = %v", err)
		}
		if ctx.PaginationMeta != nil {
			t.Fatalf("pagination metadata = %#v, want nil", ctx.PaginationMeta)
		}
	})
}

func TestHostGroupAndCollectorGETPreserveEmptyJSONBody(t *testing.T) {
	tests := []struct {
		name string
		run  func(*Context) (any, error)
	}{
		{
			name: "host-group",
			run: func(ctx *Context) (any, error) {
				return hostGroupGet(ctx, []string{"--host-group-id", "host-group-id"})
			},
		},
		{
			name: "collector",
			run: func(ctx *Context) (any, error) {
				return collectorGet(ctx, []string{"--rule-id", "rule-id"})
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, transport := newShortcutExecutionCaptureContext(t, nil)
			if _, err := tc.run(ctx); err != nil {
				t.Fatalf("run: %v", err)
			}
			if len(transport.requests) != 1 {
				t.Fatalf("request count = %d, want 1", len(transport.requests))
			}
			if got := strings.TrimSpace(string(transport.requests[0].body)); got != "{}" {
				t.Fatalf("GET body = %q, want {}", got)
			}
		})
	}
}

func TestShortcutDryRunRestoresLegacyChecksConcreteType(t *testing.T) {
	ctx, _ := newShortcutExecutionCaptureContext(t, nil)
	ctx.DryRun = true
	out, err := hostGroupGet(ctx, []string{"--host-group-id", "host-group-id"})
	if err != nil {
		t.Fatalf("host-group get dry-run: %v", err)
	}
	plan, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("plan type = %T, want object", out)
	}
	checks, ok := plan["checks"].([]map[string]any)
	if !ok {
		t.Fatalf("checks type = %T, want []map[string]any", plan["checks"])
	}
	if len(checks) != 2 {
		t.Fatalf("checks = %#v, want endpoint and region checks", checks)
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal legacy plan: %v", err)
	}
	var normalized map[string]any
	if err := json.Unmarshal(raw, &normalized); err != nil {
		t.Fatalf("decode legacy plan JSON: %v", err)
	}
	if _, ok := normalized["checks"].([]any); !ok {
		t.Fatalf("JSON checks type = %T, want array", normalized["checks"])
	}
	normalizedRaw, err := json.Marshal(normalized)
	if err != nil {
		t.Fatalf("marshal normalized plan: %v", err)
	}
	if string(normalizedRaw) != string(raw) {
		t.Fatalf("legacy concrete-type restoration changed JSON:\ngot  %s\nwant %s", raw, normalizedRaw)
	}
}

func TestHostGroupAndCollectorShortcutExecutionPreservesPassthroughFields(t *testing.T) {
	tests := []struct {
		name     string
		run      func(*Context) (any, error)
		wantPath string
		wantBody map[string]any
	}{
		{
			name: "host-group modify keeps iam project and unknown request field",
			run: func(ctx *Context) (any, error) {
				return hostGroupModify(ctx, []string{
					"--request", `{"HostGroupId":"request-id","Unknown":{"Keep":true}}`,
					"--host-group-id", "flag-id",
					"--iam-project-name", "iam-project",
				})
			},
			wantPath: "/ModifyHostGroup",
			wantBody: map[string]any{
				"HostGroupId":    "flag-id",
				"IamProjectName": "iam-project",
				"Unknown":        map[string]any{"Keep": true},
			},
		},
		{
			name: "collector modify keeps topic id and unknown request field",
			run: func(ctx *Context) (any, error) {
				return collectorModify(ctx, []string{
					"--request", `{"RuleId":"request-id","Unknown":{"Keep":true}}`,
					"--rule-id", "flag-id",
					"--topic-id", "topic-id",
				})
			},
			wantPath: "/ModifyRule",
			wantBody: map[string]any{
				"RuleId":  "flag-id",
				"TopicId": "topic-id",
				"Unknown": map[string]any{"Keep": true},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, transport := newShortcutExecutionCaptureContext(t, nil)
			if _, err := tc.run(ctx); err != nil {
				t.Fatalf("run: %v", err)
			}
			if len(transport.requests) != 1 {
				t.Fatalf("request count = %d, want 1", len(transport.requests))
			}
			if transport.requests[0].path != tc.wantPath {
				t.Fatalf("path = %q, want %q", transport.requests[0].path, tc.wantPath)
			}
			var got map[string]any
			if err := json.Unmarshal(transport.requests[0].body, &got); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if !reflect.DeepEqual(got, tc.wantBody) {
				t.Fatalf("body = %#v, want %#v", got, tc.wantBody)
			}
		})
	}
}
