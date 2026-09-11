//go:build human

package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/config"
	"github.com/volcengine-tls/ve-tls-cli/internal/execution"
	"github.com/volcengine-tls/ve-tls-cli/internal/output"
	"github.com/volcengine-tls/ve-tls-cli/internal/tlsapi"
)

func TestShortcutExecutionPreservesProjectAndTopicLegacyRequestInputs(t *testing.T) {
	tests := []struct {
		name     string
		run      func(*Context) (any, error)
		wantPath string
		wantBody map[string]any
	}{
		{
			name: "project request only empty object receives resolved region",
			run: func(ctx *Context) (any, error) {
				return projectCreate(ctx, []string{"--request", `{}`})
			},
			wantPath: "/CreateProject",
			wantBody: map[string]any{"Region": "cn-test"},
		},
		{
			name: "project explicit region and name override request while unknown passes through",
			run: func(ctx *Context) (any, error) {
				return projectCreate(ctx, []string{
					"--request", `{"ProjectName":"request","Region":"request-region","Unknown":"kept"}`,
					"--project-name", "flag",
					"--region", "flag-region",
				})
			},
			wantPath: "/CreateProject",
			wantBody: map[string]any{"ProjectName": "flag", "Region": "flag-region", "Unknown": "kept"},
		},
		{
			name: "project modify flag id overrides request",
			run: func(ctx *Context) (any, error) {
				return projectModify(ctx, []string{
					"--project-id", "flag-id",
					"--request", `{"ProjectId":"request-id","Unknown":"kept"}`,
				})
			},
			wantPath: "/ModifyProject",
			wantBody: map[string]any{"ProjectId": "flag-id", "Unknown": "kept"},
		},
		{
			name: "topic request only empty object receives defaults",
			run: func(ctx *Context) (any, error) {
				return topicCreate(ctx, []string{"--request", `{}`})
			},
			wantPath: "/CreateTopic",
			wantBody: map[string]any{"Ttl": float64(30), "ShardCount": float64(2)},
		},
		{
			name: "topic non positive flag values do not override request",
			run: func(ctx *Context) (any, error) {
				return topicCreate(ctx, []string{
					"--request", `{"ProjectId":"p","TopicName":"t","Ttl":7,"ShardCount":4}`,
					"--ttl", "0",
					"--shard-count", "0",
				})
			},
			wantPath: "/CreateTopic",
			wantBody: map[string]any{
				"ProjectId": "p", "TopicName": "t", "Ttl": float64(7), "ShardCount": float64(4),
			},
		},
		{
			name: "topic passthrough time pair and unknown field",
			run: func(ctx *Context) (any, error) {
				return topicCreate(ctx, []string{
					"--request", `{"Unknown":"kept"}`,
					"--time-key", "event_time",
					"--time-format", "%Y-%m-%d",
				})
			},
			wantPath: "/CreateTopic",
			wantBody: map[string]any{
				"Unknown": "kept", "TimeKey": "event_time", "TimeFormat": "%Y-%m-%d",
				"Ttl": float64(30), "ShardCount": float64(2),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, transport := newShortcutExecutionCaptureContext(t, nil)
			out, err := tc.run(ctx)
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if out == nil {
				t.Fatal("result is nil")
			}
			if len(transport.requests) != 1 {
				t.Fatalf("requests = %d, want 1", len(transport.requests))
			}
			request := transport.requests[0]
			if request.path != tc.wantPath {
				t.Fatalf("path = %q, want %q", request.path, tc.wantPath)
			}
			var gotBody map[string]any
			if err := json.Unmarshal(request.body, &gotBody); err != nil {
				t.Fatalf("decode body %q: %v", request.body, err)
			}
			if !reflect.DeepEqual(gotBody, tc.wantBody) {
				t.Fatalf("body = %#v, want %#v", gotBody, tc.wantBody)
			}
		})
	}
}

func TestShortcutExecutionPreservesGETEmptyBodyAndTopicTriState(t *testing.T) {
	ctx, transport := newShortcutExecutionCaptureContext(t, nil)
	if _, err := topicGet(ctx, []string{"--topic-id", "topic-1"}); err != nil {
		t.Fatalf("topic get: %v", err)
	}
	if got := strings.TrimSpace(string(transport.requests[0].body)); got != "{}" {
		t.Fatalf("GET body = %q, want {}", got)
	}

	ctx, transport = newShortcutExecutionCaptureContext(t, nil)
	if _, err := topicModify(ctx, []string{"--topic-id", "topic-1", "--no-auto-split", "--disable-tracking"}); err != nil {
		t.Fatalf("topic modify: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(transport.requests[0].body, &body); err != nil {
		t.Fatal(err)
	}
	if body["AutoSplit"] != false || body["EnableTracking"] != false {
		t.Fatalf("explicit false tri-state values missing: %#v", body)
	}
	if _, ok := body["Favourite"]; ok {
		t.Fatalf("unset tri-state value was emitted: %#v", body)
	}
}

func TestShortcutExecutionListAllLegacyAdaptation(t *testing.T) {
	t.Run("success forces total when service omits it", func(t *testing.T) {
		ctx, _ := newShortcutExecutionCaptureContext(t, []shortcutExecutionResponse{
			{body: `{"Projects":[{"ProjectId":"p1"},{"ProjectId":"p2"}]}`},
			{body: `{"Projects":[{"ProjectId":"p3"}]}`},
		})
		out, err := projectList(ctx, []string{"--all", "--page-size", "2"})
		if err != nil {
			t.Fatalf("project list --all: %v", err)
		}
		data := out.(map[string]any)
		if data["Total"] != 3 {
			t.Fatalf("Total = %#v, want 3", data["Total"])
		}
		if ctx.PaginationMeta == nil {
			t.Fatal("pagination metadata is nil")
		}
	})

	t.Run("conflict uses human flag and no pagination metadata", func(t *testing.T) {
		ctx, _ := newShortcutExecutionCaptureContext(t, nil)
		_, err := topicList(ctx, []string{"--all", "--page-number", "2"})
		if err == nil || err.Error() != "--all cannot be used with PageNumber" {
			t.Fatalf("error = %v", err)
		}
		if ctx.PaginationMeta != nil {
			t.Fatalf("pagination metadata = %#v", ctx.PaginationMeta)
		}
	})

	t.Run("transport failure keeps pagination metadata nil", func(t *testing.T) {
		ctx, _ := newShortcutExecutionCaptureContext(t, []shortcutExecutionResponse{{err: errors.New("boom")}})
		_, err := projectList(ctx, []string{"--all"})
		if err == nil || !strings.Contains(err.Error(), "boom") {
			t.Fatalf("error = %v", err)
		}
		if ctx.PaginationMeta != nil {
			t.Fatalf("pagination metadata = %#v", ctx.PaginationMeta)
		}
	})

	t.Run("dry run preserves unexpected list field", func(t *testing.T) {
		ctx, _ := newShortcutExecutionCaptureContext(t, nil)
		ctx.DryRun = true
		_, err := topicList(ctx, []string{"--all"})
		if err == nil || err.Error() != "unexpected list field: Topics" {
			t.Fatalf("error = %v", err)
		}
		if ctx.PaginationMeta != nil {
			t.Fatalf("pagination metadata = %#v", ctx.PaginationMeta)
		}
	})
}

func TestTopicListConflictPrecedesPageAllConflict(t *testing.T) {
	ctx, _ := newShortcutExecutionCaptureContext(t, nil)
	_, err := topicList(ctx, []string{
		"--all", "--page-number", "2", "--topic-name", "name", "--topic-id", "id",
	})
	if err == nil || err.Error() != "TopicName and TopicId cannot be provided together" {
		t.Fatalf("error = %v", err)
	}
}

func TestShortcutExecutionDoesNotRewriteHTTPErrorBody(t *testing.T) {
	err := adaptShortcutExecutionError(&execution.HTTPError{
		StatusCode: http.StatusBadRequest,
		Body:       []byte("--page-all cannot be used with PageNumber"),
	})
	if err == nil || err.Error() != "--page-all cannot be used with PageNumber" {
		t.Fatalf("error = %v", err)
	}
}

func TestShortcutExecutionRejectsNilContextWithoutPanic(t *testing.T) {
	_, err := executeShortcutOperation(nil, shortcutExecutionRequest{
		OperationID: "project.describe-projects",
		Input: execution.Input{
			Body: shortcutEmptyJSONBodyInput(),
		},
	})
	if err == nil || err.Error() != "missing cli context" {
		t.Fatalf("error = %v, want missing cli context", err)
	}
}

func TestShortcutExecutionRequestDeclaresLegacyValidationPolicy(t *testing.T) {
	var zero execution.ValidationPolicy
	if zero != execution.ValidationRequired {
		t.Fatalf("zero validation policy = %v", zero)
	}
}

type shortcutExecutionCapturedRequest struct {
	path string
	body []byte
}

type shortcutExecutionResponse struct {
	body string
	err  error
}

type shortcutExecutionCaptureTransport struct {
	requests  []shortcutExecutionCapturedRequest
	responses []shortcutExecutionResponse
}

func (t *shortcutExecutionCaptureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body := new(bytes.Buffer)
	_, _ = body.ReadFrom(req.Body)
	_ = req.Body.Close()
	t.requests = append(t.requests, shortcutExecutionCapturedRequest{
		path: req.URL.Path,
		body: append([]byte(nil), body.Bytes()...),
	})
	index := len(t.requests) - 1
	response := shortcutExecutionResponse{body: `{}`}
	if index < len(t.responses) {
		response = t.responses[index]
	}
	if response.err != nil {
		return nil, response.err
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"X-Tls-Requestid": []string{"req-shortcut"}},
		Body:       io.NopCloser(strings.NewReader(response.body)),
	}, nil
}

func newShortcutExecutionCaptureContext(
	t *testing.T,
	responses []shortcutExecutionResponse,
) (*Context, *shortcutExecutionCaptureTransport) {
	t.Helper()
	transport := &shortcutExecutionCaptureTransport{responses: responses}
	client, err := tlsapi.New(
		"https://shortcut.invalid",
		"cn-test",
		"",
		"ak",
		"sk",
		"",
		time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}
	client.HTTP.Transport = transport
	ctx := newContext(&bytes.Buffer{}, &bytes.Buffer{}, output.FormatJSON, "", "")
	ctx.client = client
	profile := config.Profile{
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
		Region:          "cn-test",
		Endpoint:        "https://shortcut.invalid",
	}
	ctx.cfg = config.Config{
		Version:        1,
		CurrentProfile: "default",
		Profiles:       map[string]config.Profile{"default": profile},
	}
	ctx.profile = profile
	ctx.profileResolved = true
	return ctx, transport
}
