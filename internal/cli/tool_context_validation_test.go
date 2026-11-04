package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/volcengine-tls/ve-tls-cli/internal/tlsapi"
)

func TestToolContextValidationRejectsInvalidFields(t *testing.T) {
	tests := []struct {
		name string
		json string
		path string
	}{
		{"unknown top level", `{"badField":"sensitive-value"}`, "$.badField"},
		{"null profile", `{"profile":null}`, "$.profile"},
		{"wrong secrets file", `{"secrets_file":123}`, "$.secrets_file"},
		{"wrong region", `{"region":false}`, "$.region"},
		{"wrong endpoint", `{"endpoint":null}`, "$.endpoint"},
		{"wrong contract digest", `{"contract_digest":[]}`, "$.contract_digest"},
		{"null trace", `{"trace":null}`, "$.trace"},
		{"wrong trace", `{"trace":[]}`, "$.trace"},
		{"unknown trace field", `{"trace":{"enable":true}}`, "$.trace.enable"},
		{"wrong trace enabled", `{"trace":{"enabled":"sensitive-value"}}`, "$.trace.enabled"},
		{"null trace dir", `{"trace":{"dir":null}}`, "$.trace.dir"},
		{"wrong trace redact", `{"trace":{"redact":false}}`, "$.trace.redact"},
		{"nonobject execution", `{"execution":true}`, "$.execution"},
		{"null execution", `{"execution":null}`, "$.execution"},
		{"unknown dryRun", `{"execution":{"dryRun":true}}`, "$.execution.dryRun"},
		{"wrong dry_run", `{"execution":{"dry_run":"sensitive-value"}}`, "$.execution.dry_run"},
		{"null artifact", `{"execution":{"artifact":null}}`, "$.execution.artifact"},
		{"unknown artifact field", `{"execution":{"artifact":{"directory":"sensitive-value"}}}`, "$.execution.artifact.directory"},
		{"wrong artifact path", `{"execution":{"artifact":{"path":true}}}`, "$.execution.artifact.path"},
		{"wrong artifact dir", `{"execution":{"artifact":{"dir":null}}}`, "$.execution.artifact.dir"},
		{"wrong output mode type", `{"execution":{"output_mode":null}}`, "$.execution.output_mode"},
		{"invalid output mode", `{"execution":{"output_mode":"sensitive-value"}}`, "$.execution.output_mode"},
		{"wrong output object", `{"execution":{"output":"sensitive-value"}}`, "$.execution.output"},
		{"unknown output field", `{"execution":{"output":{"directory":"sensitive-value"}}}`, "$.execution.output.directory"},
		{"invalid nested output mode", `{"execution":{"output":{"mode":"sensitive-value"}}}`, "$.execution.output.mode"},
		{"wrong output path", `{"execution":{"output":{"path":null}}}`, "$.execution.output.path"},
		{"wrong output dir", `{"execution":{"output":{"dir":true}}}`, "$.execution.output.dir"},
		{"wrong page object", `{"execution":{"page":null}}`, "$.execution.page"},
		{"unknown page field", `{"execution":{"page":{"All":true}}}`, "$.execution.page.All"},
		{"wrong page all", `{"execution":{"page":{"all":"sensitive-value"}}}`, "$.execution.page.all"},
		{"wrong page alias", `{"execution":{"page_all":null}}`, "$.execution.page_all"},
		{"wrong projection", `{"execution":{"projection":null}}`, "$.execution.projection"},
		{"unknown projection field", `{"execution":{"projection":{"expr":"sensitive-value"}}}`, "$.execution.projection.expr"},
		{"wrong projection jmes", `{"execution":{"projection":{"jmes":null}}}`, "$.execution.projection.jmes"},
		{"projection validates all members", `{"execution":{"projection":["valid",false]}}`, "$.execution.projection[1]"},
		{"nested projection validates all members", `{"execution":{"projection":["valid",["",{"jmes":42}]]}}`, "$.execution.projection[1][1].jmes"},
		{"conflicting page aliases", `{"execution":{"page":{"all":true},"page_all":false}}`, "$.execution.page_all"},
		{"conflicting artifact and mode", `{"execution":{"artifact":false,"output_mode":"file"}}`, "$.execution.output_mode"},
		{"conflicting modes", `{"execution":{"output_mode":"stdout","output":{"mode":"artifact"}}}`, "$.execution.output.mode"},
		{"conflicting paths", `{"execution":{"artifact":{"path":"one"},"output":{"path":"two"}}}`, "$.execution.output.path"},
		{"conflicting dirs", `{"execution":{"artifact":{"dir":"one"},"output":{"dir":"two"}}}`, "$.execution.output.dir"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadToolExecContext(tt.json)
			var validationErr *toolContextValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("expected toolContextValidationError, got %v", err)
			}
			if validationErr.Path != tt.path || !strings.Contains(err.Error(), tt.path) {
				t.Fatalf("path=%q error=%q, want %q", validationErr.Path, err.Error(), tt.path)
			}
			if strings.Contains(err.Error(), "sensitive-value") {
				t.Fatalf("error leaked input value: %q", err.Error())
			}
		})
	}
}

func TestToolContextValidationRejectsNullRoot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "context.json")
	if err := os.WriteFile(path, []byte("null"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := loadToolExecContext("file://" + path)
	var validationErr *toolContextValidationError
	if !errors.As(err, &validationErr) || validationErr.Path != "$" {
		t.Fatalf("expected root validation error, got %v", err)
	}
}

func TestToolContextValidationAcceptsLegacyFormsAndAliases(t *testing.T) {
	tests := []struct {
		name       string
		json       string
		artifact   bool
		path       string
		dir        string
		pageAll    bool
		projection string
	}{
		{"empty fields", `{"profile":" ","secrets_file":"","region":"","endpoint":"","contract_digest":"","trace":"","execution":{"artifact":"","output_mode":"","output":{"mode":"","path":"","dir":""},"projection":""}}`, false, "", "", false, ""},
		{"boolean trace and artifact", `{"trace":false,"execution":{"artifact":false,"output_mode":" StDoUt "}}`, false, "", "", false, ""},
		{"string artifact and equivalent aliases", `{"trace":"dir","execution":{"artifact":" result.json ","output_mode":" FILE ","output":{"mode":"Artifact","path":"result.json"},"page":{"all":true},"page_all":true,"projection":["",{"jmes":" name "}]}}`, true, "result.json", "", true, "name"},
		{"object artifact and output dir", `{"trace":{"enabled":true,"dir":"dir","redact":"strict"},"execution":{"artifact":{"path":"result.json","dir":"out"},"output_mode":"artifact","output":{"mode":"file","path":"","dir":"out"},"page":{"all":false},"page_all":false,"projection":["",["first","second"]]}}`, true, "result.json", "out", false, "first"},
		{"output dir without output mode", `{"execution":{"output_mode":"artifact","output":{"dir":"out"}}}`, true, "", "out", false, ""},
		{"output file without other alias", `{"execution":{"output":{"mode":"file","path":"file","dir":"out"}}}`, true, "file", "out", false, ""},
		{"empty nested objects", `{"trace":{},"execution":{"artifact":{},"output":{},"page":{},"projection":{}}}`, true, "", "", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := loadToolExecContext(tt.json)
			if err != nil {
				t.Fatalf("load context: %v", err)
			}
			options := resolveToolExecutionOptions(cfg)
			if options.Artifact != tt.artifact || options.ArtifactPath != tt.path || options.OutputDir != tt.dir || options.PageAll != tt.pageAll || options.Projection != tt.projection {
				t.Fatalf("options=%+v, want artifact=%t path=%q dir=%q pageAll=%t projection=%q", options, tt.artifact, tt.path, tt.dir, tt.pageAll, tt.projection)
			}
		})
	}
}

func TestApplyToolExecContextRuntimeSelectors(t *testing.T) {
	tests := []struct {
		name            string
		runtimeRegion   string
		runtimeEndpoint string
		region          string
		endpoint        string
		wantRegion      string
		wantEndpoint    string
		conflictPath    string
		invalidates     bool
	}{
		{"context overrides defaults", "", "", "context-region", "context-endpoint", "context-region", "context-endpoint", "", true},
		{"matching explicit selectors", "region", "endpoint", " region ", " endpoint ", "region", "endpoint", "", false},
		{"region conflict", "global-region", "", "context-region", "", "global-region", "", "$.region", false},
		{"endpoint conflict", "", "global-endpoint", "", "context-endpoint", "", "global-endpoint", "$.endpoint", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &Context{InvocationOptions: InvocationOptions{RuntimeRegion: tt.runtimeRegion, RuntimeEndpoint: tt.runtimeEndpoint}}
			ctx.defaults.Region = "default-region"
			ctx.defaults.Endpoint = "default-endpoint"
			ctx.profileResolved = true
			cachedClient := &tlsapi.Client{}
			ctx.client = cachedClient
			cfg := toolExecContext{Region: tt.region, Endpoint: tt.endpoint}
			if tt.conflictPath != "" {
				cfg.SecretsFile = filepath.Join(t.TempDir(), "missing-secrets")
			}
			err := applyToolExecContext(ctx, cfg)
			if tt.conflictPath != "" {
				var validationErr *toolContextValidationError
				if !errors.As(err, &validationErr) || validationErr.Path != tt.conflictPath {
					t.Fatalf("expected conflict at %q before loading secrets, got %v", tt.conflictPath, err)
				}
				for _, value := range []string{tt.runtimeRegion, tt.runtimeEndpoint, tt.region, tt.endpoint} {
					if value != "" && strings.Contains(err.Error(), value) {
						t.Fatalf("error leaked input value: %q", err)
					}
				}
			} else if err != nil {
				t.Fatalf("apply context: %v", err)
			}
			if ctx.RuntimeRegion != tt.wantRegion || ctx.RuntimeEndpoint != tt.wantEndpoint {
				t.Fatalf("runtime region=%q endpoint=%q", ctx.RuntimeRegion, ctx.RuntimeEndpoint)
			}
			if ctx.defaults.Region != "default-region" || ctx.defaults.Endpoint != "default-endpoint" {
				t.Fatalf("defaults changed: %+v", ctx.defaults)
			}
			if tt.invalidates && (ctx.profileResolved || ctx.client != nil) {
				t.Fatal("changed runtime target left stale profile or client")
			}
			if !tt.invalidates && (!ctx.profileResolved || ctx.client != cachedClient) {
				t.Fatal("unchanged or conflicting context altered cached profile/client")
			}
		})
	}
}

func TestToolAndWorkflowExecContextValidationPreventsRequests(t *testing.T) {
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("x-tls-requestid", "request-id")
		switch r.URL.Path {
		case "/CreateTopic":
			_, _ = w.Write([]byte(`{"TopicId":"topic-1"}`))
		case "/DescribeApp":
			_, _ = w.Write([]byte(`{"AppType":"LogApp","Region":"cn-beijing","Resources":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "original-ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "original-sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", srv.URL)
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	secrets := filepath.Join(t.TempDir(), "secrets")
	if err := os.WriteFile(secrets, []byte("VOLCENGINE_ACCESS_KEY_ID=changed-ak\nVOLCENGINE_ACCESS_KEY_SECRET=changed-sk\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		command []string
	}{
		{"tool write", []string{"tool", "exec", "topic.create", "--input", `{"ProjectId":"pid","TopicName":"demo","Ttl":30,"ShardCount":1}`}},
		{"workflow", []string{"workflow", "exec", appResolveTopicIDsWorkflowID, "--input", `{"AppId":"app-1"}`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cases := []struct {
				name      string
				globals   []string
				context   map[string]any
				errorPath string
			}{
				{"unknown", nil, map[string]any{"execution": map[string]any{"dryRun": true}}, "$.execution.dryRun"},
				{"wrong type", nil, map[string]any{"execution": map[string]any{"dry_run": "true"}}, "$.execution.dry_run"},
				{"region conflict", []string{"--region", "global-region"}, map[string]any{"region": "context-region"}, "$.region"},
				{"endpoint conflict", []string{"--endpoint", "http://global.example"}, map[string]any{"endpoint": "http://context.example"}, "$.endpoint"},
			}
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					tc.context["secrets_file"] = secrets
					badContext, err := json.Marshal(tc.context)
					if err != nil {
						t.Fatal(err)
					}
					args := append(append(append([]string{}, tc.globals...), tt.command...), "--context", string(badContext))
					var stdout, stderr bytes.Buffer
					code := Run(args, &stdout, &stderr)
					if code != 1 || !strings.Contains(stdout.String(), tc.errorPath) {
						t.Fatalf("expected validation failure: exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
					}
					var envelope map[string]any
					if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
						t.Fatalf("decode envelope: %v", err)
					}
					errObj, _ := envelope["error"].(map[string]any)
					if errObj["kind"] != "validation" {
						t.Fatalf("expected validation kind, got %#v", errObj)
					}
					if calls.Load() != 0 {
						t.Fatalf("invalid context sent %d requests", calls.Load())
					}
					if got := os.Getenv("VOLCENGINE_ACCESS_KEY_ID"); got != "original-ak" {
						t.Fatalf("secrets were loaded before validation: %q", got)
					}
				})
			}
		})
	}
}

func TestToolAndWorkflowExecValidContextRunsAndDryRuns(t *testing.T) {
	var defaultCalls, contextCalls atomic.Int64
	defaultServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defaultCalls.Add(1)
		http.Error(w, "unexpected default endpoint", http.StatusInternalServerError)
	}))
	defer defaultServer.Close()
	contextServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contextCalls.Add(1)
		w.Header().Set("x-tls-requestid", "request-id")
		switch r.URL.Path {
		case "/CreateTopic":
			_, _ = w.Write([]byte(`{"TopicId":"topic-1"}`))
		case "/DescribeApp":
			_, _ = w.Write([]byte(`{"AppType":"LogApp","Region":"cn-beijing","Resources":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer contextServer.Close()
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "test-ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "test-sk")
	t.Setenv("VOLCENGINE_REGION", "different-region")
	t.Setenv("VOLCENGINE_ENDPOINT", defaultServer.URL)
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	tests := []struct {
		name    string
		command []string
	}{
		{"tool", []string{"tool", "exec", "topic.create", "--input", `{"ProjectId":"pid","TopicName":"demo","Ttl":30,"ShardCount":1}`}},
		{"workflow", []string{"workflow", "exec", appResolveTopicIDsWorkflowID, "--input", `{"AppId":"app-1"}`}},
	}
	for _, tt := range tests {
		for _, dryRun := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dry_run=%t", tt.name, dryRun), func(t *testing.T) {
				contextJSON, err := json.Marshal(map[string]any{"region": "cn-beijing", "endpoint": contextServer.URL, "execution": map[string]any{"dry_run": dryRun, "allow_write": true}})
				if err != nil {
					t.Fatal(err)
				}
				args := append(append([]string{}, tt.command...), "--context", string(contextJSON))
				before := contextCalls.Load()
				var stdout, stderr bytes.Buffer
				code := Run(args, &stdout, &stderr)
				if code != 0 {
					t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
				}
				var envelope map[string]any
				if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
					t.Fatalf("decode envelope: %v", err)
				}
				summary, _ := envelope["summary"].(map[string]any)
				if summary["dryRun"] != dryRun {
					t.Fatalf("unexpected dryRun summary: %#v", summary)
				}
				wantCalls := int64(1)
				if dryRun {
					wantCalls = 0
				}
				if got := contextCalls.Load() - before; got != wantCalls || defaultCalls.Load() != 0 {
					t.Fatalf("context calls=%d default calls=%d, want %d and 0", got, defaultCalls.Load(), wantCalls)
				}
			})
		}
	}
}
