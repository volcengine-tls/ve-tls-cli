package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth"
	"github.com/volcengine-tls/ve-tls-cli/internal/config"
	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
	"github.com/volcengine-tls/ve-tls-cli/internal/execution"
	"github.com/volcengine-tls/ve-tls-cli/internal/output"
)

func TestToolCatalogLoadsEmbeddedV2OperationsWithoutChangingLegacyDigest(t *testing.T) {
	operations := loadToolOperations("", "", "")
	if got := len(operations); got != 125 {
		t.Fatalf("tool count=%d, want 125", got)
	}
	operationCatalog, err := contract.LoadEmbedded()
	if err != nil {
		t.Fatalf("load operation catalog: %v", err)
	}
	for _, operation := range operations {
		rebuilt, err := contract.RebuildLegacyToolV1(operationCatalog, operation)
		if err != nil {
			t.Fatalf("rebuild %q: %v", operation.ID, err)
		}
		if got, want := toolContractForDigest(operation), contract.LegacyToolDigestV1(rebuilt); got != want {
			t.Fatalf("%q digest=%s, want %s", operation.ID, got, want)
		}
	}
}

func TestToolCatalogIndexesInternalOperationsWithoutListingThem(t *testing.T) {
	for _, tool := range loadToolOperations("", "", "") {
		if tool.Visibility != "public" {
			t.Fatalf("listed non-public tool %q with visibility %q", tool.ID, tool.Visibility)
		}
	}
	for _, id := range []string{
		"metric-topic.describe-metric-topics",
		"metric-topic.describe-metric-topic",
		"metric-topic.create",
		"metric-topic.modify",
		"metric-topic.delete",
		"collector.describe-rules-v2",
	} {
		operation, ok := loadToolOperation(id)
		if !ok {
			t.Fatalf("missing internal operation %q", id)
		}
		if operation.Visibility != "internal" {
			t.Fatalf("%s visibility=%q, want internal", id, operation.Visibility)
		}
	}
}

func TestToolExecutionCodecUsesExplicitCodecWhenActionAndPathAreWrong(t *testing.T) {
	ctx := newContext(&bytes.Buffer{}, &bytes.Buffer{}, output.FormatJSON, "", "")
	registry, err := newToolExecutionCodecRegistry(ctx)
	if err != nil {
		t.Fatalf("build codec registry: %v", err)
	}
	operation := contract.Operation{
		ID:     "test.wrong-metadata",
		Group:  "wrong",
		Action: "WrongAction",
		Wire: contract.WireSpec{
			Method:        "POST",
			Path:          "/WrongPath",
			RequestFormat: "json",
			Codec:         contract.CodecPutLogs,
		},
	}
	var encoded execution.Request
	executor := execution.NewExecutor(migrationTransportFunc(func(_ context.Context, request execution.Request) (execution.Response, error) {
		encoded = request
		return execution.Response{
			StatusCode: 200,
			Header:     http.Header{},
			Body:       []byte(`{}`),
		}, nil
	}), registry)
	_, err = executor.Execute(context.Background(), execution.Invocation{
		Operation: operation,
		Input: execution.Input{
			Body: execution.Payload{
				JSON:    map[string]any{"LogGroups": []any{}},
				Format:  execution.BodyFormatJSON,
				Present: true,
			},
		},
	})
	if err != nil {
		t.Fatalf("execute explicit putlogs codec: %v", err)
	}
	if got := encoded.Header["Content-Type"]; got != "application/x-protobuf" {
		t.Fatalf("content type=%q, want application/x-protobuf", got)
	}
}

type migrationTransportFunc func(context.Context, execution.Request) (execution.Response, error)

func (fn migrationTransportFunc) Do(ctx context.Context, request execution.Request) (execution.Response, error) {
	return fn(ctx, request)
}

func TestToolExecutionHTTPErrorAdaptsToLegacyHTTPError(t *testing.T) {
	source := &execution.HTTPError{
		StatusCode: 403,
		RequestID:  "req-denied",
		Body:       []byte(`{"ErrorCode":"AccessDenied","ErrorMessage":"denied"}`),
	}
	err := adaptToolExecutionError(source)
	legacy, ok := isHTTPError(err)
	if !ok {
		t.Fatalf("adapted error type=%T, want *httpError", err)
	}
	if legacy.statusCode != 403 || legacy.requestID != "req-denied" {
		t.Fatalf("legacy error=%#v", legacy)
	}
	if !bytes.Equal(legacy.body, source.Body) {
		t.Fatalf("legacy body=%q, want %q", legacy.body, source.Body)
	}
	payload, exitCode := classifyError(err, "", 0, "tool")
	if exitCode != 2 || payload.Kind != "auth" || payload.StatusCode != 403 || payload.RequestID != "req-denied" {
		t.Fatalf("classified envelope error=%#v exit=%d", payload, exitCode)
	}
}

func TestToolDryRunRuntimeViewDoesNotRetrieveDynamicCredentials(t *testing.T) {
	provider := &fakeProvider{
		value: auth.Value{
			AccessKeyID:     "temporary-ak",
			SecretAccessKey: "temporary-sk",
		},
	}
	ctx := newTestContext(t, config.Config{
		Version: 1,
		Profiles: map[string]config.Profile{
			"dynamic": {
				Mode:     config.AuthModeSSO,
				Region:   "cn-beijing",
				Endpoint: "https://tls-cn-beijing.volces.com",
			},
		},
	}, filepath.Join(t.TempDir(), "config.json"))
	ctx.Profile = "dynamic"
	ctx.authFactory = &fakeAuthFactory{ssoProvider: provider}

	view := buildToolExecutionRuntimeView(ctx)
	if provider.calls != 0 {
		t.Fatalf("dry-run runtime resolution retrieved credentials %d times", provider.calls)
	}
	if view.Region != "cn-beijing" || view.Endpoint == "" {
		t.Fatalf("runtime view=%#v", view)
	}
	if len(view.Checks) != 0 {
		t.Fatalf("successful runtime checks=%#v, want omitted", view.Checks)
	}
}

func TestApplyToolExecutionResultOnlyPublishesSuccessfulMergedPagination(t *testing.T) {
	ctx := &Context{}
	ctx.PaginationMeta = map[string]any{"stale": true}
	applyToolExecutionResult(ctx, execution.Result{
		RequestID:  "req-ok",
		StatusCode: 200,
		Pagination: &execution.PaginationResult{
			Mode:      "page_all",
			PageCount: 2,
			PageSize:  50,
			Merged:    true,
		},
	})
	want := map[string]any{
		"mode":      "page_all",
		"pageCount": 2,
		"pageSize":  50,
		"merged":    true,
	}
	if !mapsEqualJSON(ctx.PaginationMeta, want) {
		t.Fatalf("pagination=%#v, want %#v", ctx.PaginationMeta, want)
	}

	applyToolExecutionResult(ctx, execution.Result{
		Pagination: &execution.PaginationResult{
			Mode:      "page_all",
			PageCount: 1,
			Merged:    false,
		},
	})
	if ctx.PaginationMeta != nil {
		t.Fatalf("failed pagination metadata=%#v, want nil", ctx.PaginationMeta)
	}
	applyToolExecutionResult(ctx, execution.Result{Plan: &execution.DryRunPlan{Type: "plan"}})
	if ctx.PaginationMeta != nil {
		t.Fatalf("dry-run pagination metadata=%#v, want nil", ctx.PaginationMeta)
	}
}

func TestToolExecDryRunProjectionCanReadRequestPreviewAndWritesTrace(t *testing.T) {
	t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
	t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
	t.Setenv("VOLCENGINE_REGION", "cn-beijing")
	t.Setenv("VOLCENGINE_ENDPOINT", "https://tls-cn-beijing.volces.com")
	t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))

	traceDir := filepath.Join(t.TempDir(), "trace")
	contextArg, err := json.Marshal(map[string]any{
		"trace": map[string]any{"enabled": true, "dir": traceDir},
		"execution": map[string]any{
			"dry_run":    true,
			"projection": "request_preview.body.ProjectName",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	inputArg := `{"body":{"ProjectName":"internal-log","Region":"cn-beijing","Description":"demo"}}`
	ctx := newContext(&bytes.Buffer{}, &bytes.Buffer{}, output.FormatJSON, "", "")
	ctx.defaults = config.ProfileDefaults{
		Region:   "cn-beijing",
		Endpoint: "https://tls-cn-beijing.volces.com",
	}
	out, err := runToolExec(ctx, []string{
		"project.create-project",
		"--context", string(contextArg),
		"--input", inputArg,
	})
	if err != nil {
		t.Fatalf("run tool exec: %v", err)
	}
	envelope, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("output type=%T, want map", out)
	}
	if got := envelope["data"]; got != "internal-log" {
		t.Fatalf("projected data=%#v, want internal-log", got)
	}
	if ctx.TracePath == "" {
		t.Fatal("dry-run trace path is empty")
	}
	raw, err := os.ReadFile(ctx.TracePath)
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}
	if !bytes.Contains(raw, []byte(`"type":"plan"`)) {
		t.Fatalf("trace missing plan event: %s", raw)
	}
}

func mapsEqualJSON(left, right map[string]any) bool {
	l, lerr := json.Marshal(left)
	r, rerr := json.Marshal(right)
	return lerr == nil && rerr == nil && bytes.Equal(l, r)
}

func TestAdaptToolExecutionErrorLeavesUnrelatedErrorsUntouched(t *testing.T) {
	source := errors.New("sentinel")
	if got := adaptToolExecutionError(source); !errors.Is(got, source) {
		t.Fatalf("adapted unrelated error=%v", got)
	}
}

func TestAdaptToolExecutionErrorHandlesTypedNilHTTPError(t *testing.T) {
	var source *execution.HTTPError
	var err error = source

	got := adaptToolExecutionError(err)
	if got == nil {
		t.Fatal("adapted typed-nil error is nil")
	}
	if got != err {
		t.Fatalf("adapted typed-nil error=%T %v, want original", got, got)
	}
	if got.Error() != "http error" {
		t.Fatalf("adapted typed-nil message=%q, want http error", got.Error())
	}
}
