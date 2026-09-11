//go:build human

package cli

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestMetricTopicExecutorPreservesLegacyRequestOverlay(t *testing.T) {
	tests := []struct {
		name     string
		run      func(*Context) (any, error)
		wantPath string
		wantBody map[string]any
	}{
		{
			name: "create keeps request unknowns and non-positive flags do not override",
			run: func(ctx *Context) (any, error) {
				return metricTopicCreate(ctx, []string{
					"--request", `{"ProjectId":"p","TopicName":"t","Ttl":91,"ShardCount":7,"Unknown":"kept"}`,
					"--ttl", "0",
					"--shard-count", "0",
				})
			},
			wantPath: "/CreateMetricTopic",
			wantBody: map[string]any{
				"ProjectId": "p", "TopicName": "t", "Ttl": float64(91),
				"ShardCount": float64(7), "Unknown": "kept",
			},
		},
		{
			name: "modify preserves clear description and explicit false tri-state",
			run: func(ctx *Context) (any, error) {
				return metricTopicModify(ctx, []string{
					"--topic-id", "topic-1",
					"--request", `{"Ttl":7,"Unknown":"kept"}`,
					"--clear-description",
					"--no-favourite",
					"--no-auto-split",
					"--ttl", "0",
				})
			},
			wantPath: "/ModifyMetricTopic",
			wantBody: map[string]any{
				"TopicId": "topic-1", "Description": "", "Favourite": false,
				"AutoSplit": false, "Ttl": float64(7), "Unknown": "kept",
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
				t.Fatalf("requests = %d, want 1", len(transport.requests))
			}
			if got := transport.requests[0].path; got != tc.wantPath {
				t.Fatalf("path = %q, want %q", got, tc.wantPath)
			}
			var gotBody map[string]any
			if err := json.Unmarshal(transport.requests[0].body, &gotBody); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if !reflect.DeepEqual(gotBody, tc.wantBody) {
				t.Fatalf("body = %#v, want %#v", gotBody, tc.wantBody)
			}
		})
	}
}

func TestMetricTopicExecutorPreservesGETBodyAndListAllTotal(t *testing.T) {
	ctx, transport := newShortcutExecutionCaptureContext(t, nil)
	if _, err := metricTopicGet(ctx, []string{"--topic-id", "topic-1"}); err != nil {
		t.Fatalf("metric topic get: %v", err)
	}
	if got := string(transport.requests[0].body); got != "{}" {
		t.Fatalf("GET body = %q, want {}", got)
	}

	ctx, _ = newShortcutExecutionCaptureContext(t, []shortcutExecutionResponse{
		{body: `{"Topics":[{"TopicId":"t1"},{"TopicId":"t2"}]}`},
	})
	out, err := metricTopicList(ctx, []string{"--all"})
	if err != nil {
		t.Fatalf("metric topic list --all: %v", err)
	}
	data, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("result = %T, want object", out)
	}
	if data["Total"] != 2 {
		t.Fatalf("Total = %#v, want 2", data["Total"])
	}
	if ctx.PaginationMeta == nil {
		t.Fatal("pagination metadata is nil")
	}
}

func TestLogExecutorRoutesSearchHistogramAndContextOperations(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantPath string
	}{
		{
			name: "search",
			args: []string{
				"search", "--topic-id", "topic-1", "--query", "*",
				"--from", "1700000000000", "--to", "1700000060000",
			},
			wantPath: "/SearchLogs",
		},
		{
			name: "histogram",
			args: []string{
				"histogram", "--topic-id", "topic-1", "--query", "*",
				"--from", "1700000000", "--to", "1700000060",
			},
			wantPath: "/DescribeHistogramV1",
		},
		{
			name: "context",
			args: []string{
				"context", "--topic-id", "topic-1", "--context-flow", "flow",
				"--source", "source", "--package-offset", "0",
			},
			wantPath: "/DescribeLogContext",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, transport := newShortcutExecutionCaptureContext(t, nil)
			if _, err := runLog(ctx, tc.args); err != nil {
				t.Fatalf("run log: %v", err)
			}
			if got := ctx.Action; got != "log."+tc.name {
				t.Fatalf("action = %q, want %q", got, "log."+tc.name)
			}
			if len(transport.requests) != 1 {
				t.Fatalf("requests = %d, want 1", len(transport.requests))
			}
			if got := transport.requests[0].path; got != tc.wantPath {
				t.Fatalf("path = %q, want %q", got, tc.wantPath)
			}
		})
	}
}
