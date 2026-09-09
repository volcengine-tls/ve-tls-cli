package execution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
)

type fakeTransport struct {
	requests  []Request
	responses []Response
	errors    []error
}

func (f *fakeTransport) Do(_ context.Context, request Request) (Response, error) {
	f.requests = append(f.requests, cloneRequest(request))
	index := len(f.requests) - 1
	var response Response
	if index < len(f.responses) {
		response = f.responses[index]
	}
	var err error
	if index < len(f.errors) {
		err = f.errors[index]
	}
	return response, err
}

type countingCodec struct {
	encodeCalls int
	decodeCalls int
	encode      func(Request) (Request, any, error)
	decode      func(Response, any) (any, error)
}

func (c *countingCodec) Encode(_ context.Context, _ contract.Operation, request Request) (Request, any, error) {
	c.encodeCalls++
	if c.encode != nil {
		return c.encode(request)
	}
	return request, nil, nil
}

func (c *countingCodec) Decode(_ context.Context, _ contract.Operation, response Response, state any) (any, error) {
	c.decodeCalls++
	if c.decode != nil {
		return c.decode(response, state)
	}
	return map[string]any{"ok": true}, nil
}

func TestExecutorExecutesOneRequestAndReturnsMetadata(t *testing.T) {
	transport := &fakeTransport{responses: []Response{{
		StatusCode: http.StatusOK,
		Header:     http.Header{"X-Tls-Requestid": []string{"req-1"}},
		Body:       []byte(`{"Projects":[{"ProjectId":"p1"}]}`),
	}}}
	codec := &countingCodec{decode: func(response Response, _ any) (any, error) {
		return decodeJSONResponse(response)
	}}
	registry := NewCodecRegistry()
	if err := registry.Register(contract.CodecJSON, codec); err != nil {
		t.Fatalf("Register: %v", err)
	}
	executor := NewExecutor(transport, registry)

	op := requestTestOperation()
	result, err := executor.Execute(context.Background(), Invocation{
		Operation: op,
		Input: Input{
			Path: map[string]any{"ProjectId": "project-1"},
			Body: Payload{JSON: map[string]any{"Name": "demo"}, Format: BodyFormatJSON},
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.OperationID != op.ID || result.RequestID != "req-1" || result.StatusCode != http.StatusOK {
		t.Fatalf("metadata = %#v", result)
	}
	if codec.encodeCalls != 1 || codec.decodeCalls != 1 || len(transport.requests) != 1 {
		t.Fatalf("calls encode=%d decode=%d transport=%d", codec.encodeCalls, codec.decodeCalls, len(transport.requests))
	}
}

func TestExecutorValidationPolicyDefaultsToRequired(t *testing.T) {
	transport := &fakeTransport{}
	_, err := NewExecutor(transport, NewCodecRegistry()).Execute(context.Background(), Invocation{
		Operation: requestTestOperation(),
		Input:     Input{},
	})
	if err == nil || err.Error() != "missing required field: input.path.ProjectId" {
		t.Fatalf("error = %v", err)
	}
	if len(transport.requests) != 0 {
		t.Fatalf("transport calls = %d, want 0", len(transport.requests))
	}
}

func TestExecutorCallerLegacyValidationPolicyUsesCallerValidation(t *testing.T) {
	transport := &fakeTransport{responses: []Response{{
		StatusCode: http.StatusOK,
		Body:       []byte(`{"ok":true}`),
	}}}
	operation := requestTestOperation()
	operation.InputSchema["body"] = map[string]any{
		"type":       "object",
		"required":   []any{"Name"},
		"properties": map[string]any{"Name": map[string]any{"type": "string"}},
	}
	result, err := NewExecutor(transport, NewCodecRegistry()).Execute(context.Background(), Invocation{
		Operation: operation,
		Input: Input{
			Path: map[string]any{"ProjectId": "project-1"},
		},
		Options: Options{
			ValidationPolicy: ValidationCallerLegacy,
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(transport.requests) != 1 {
		t.Fatalf("transport calls = %d, want 1", len(transport.requests))
	}
	if got, ok := result.Data.(map[string]any); !ok || got["ok"] != true {
		t.Fatalf("result data = %#v", result.Data)
	}
}

func TestExecutorReturnsCurrentResponseMetadataOnErrors(t *testing.T) {
	t.Run("transport", func(t *testing.T) {
		sentinel := errors.New("dial failed")
		transport := &fakeTransport{
			responses: []Response{{
				StatusCode: 599,
				Header:     http.Header{"X-Tls-Requestid": []string{"req-transport"}},
			}},
			errors: []error{sentinel},
		}
		codec := &countingCodec{}
		registry := NewCodecRegistry()
		if err := registry.Register(contract.CodecJSON, codec); err != nil {
			t.Fatal(err)
		}
		result, err := NewExecutor(transport, registry).Execute(context.Background(), Invocation{
			Operation: requestTestOperation(),
			Input: Input{
				Path: map[string]any{"ProjectId": "p1"},
			},
		})
		if !errors.Is(err, sentinel) {
			t.Fatalf("error = %v", err)
		}
		if result.StatusCode != 599 || result.RequestID != "req-transport" || codec.decodeCalls != 0 {
			t.Fatalf("result=%#v decode=%d", result, codec.decodeCalls)
		}
	})

	t.Run("http", func(t *testing.T) {
		transport := &fakeTransport{responses: []Response{{
			StatusCode: http.StatusForbidden,
			Header:     http.Header{"X-Tls-Requestid": []string{"req-denied"}},
			Body:       []byte(`{"ErrorCode":"AccessDenied"}`),
		}}}
		result, err := NewExecutor(transport, NewCodecRegistry()).Execute(context.Background(), Invocation{
			Operation: requestTestOperation(),
			Input:     Input{Path: map[string]any{"ProjectId": "p1"}},
		})
		var httpErr *HTTPError
		if !errors.As(err, &httpErr) {
			t.Fatalf("error = %T %v", err, err)
		}
		if httpErr.StatusCode != http.StatusForbidden ||
			httpErr.RequestID != "req-denied" ||
			string(httpErr.Body) != `{"ErrorCode":"AccessDenied"}` {
			t.Fatalf("http error = %#v", httpErr)
		}
		if result.StatusCode != http.StatusForbidden || result.RequestID != "req-denied" {
			t.Fatalf("result metadata = %#v", result)
		}
	})
}

func TestExecutorDryRunEncodesExactlyOnceWithoutDecodeOrTransport(t *testing.T) {
	transport := &fakeTransport{}
	codec := &countingCodec{encode: func(request Request) (Request, any, error) {
		request.Header["Content-Type"] = "application/x-protobuf"
		request.Body = []byte("encoded-wire-body")
		return request, "state", nil
	}}
	registry := NewCodecRegistry()
	if err := registry.Register(contract.CodecPutLogs, codec); err != nil {
		t.Fatal(err)
	}
	op := requestTestOperation()
	op.Wire.Codec = contract.CodecPutLogs
	op.Pagination = &contract.PaginationSpec{
		Mode:            contract.PaginationPageNumber,
		PageNumberParam: "PageNumber",
		PageSizeParam:   "PageSize",
		ItemsField:      "Items",
		DefaultPageSize: 100,
		MaxPages:        1000,
	}
	original := map[string]any{"Name": "before-codec"}

	result, err := NewExecutor(transport, registry).Execute(context.Background(), Invocation{
		Operation: op,
		Input: Input{
			Path: map[string]any{"ProjectId": "p1"},
			Body: Payload{JSON: original, Format: BodyFormatJSON},
		},
		Options: Options{DryRun: true, PageAll: true},
		Runtime: RuntimeView{
			Endpoint: "https://tls.example.com",
			Region:   "cn-beijing",
		},
	})
	if err != nil {
		t.Fatalf("Execute(dry-run): %v", err)
	}
	if len(transport.requests) != 0 || codec.encodeCalls != 1 || codec.decodeCalls != 0 {
		t.Fatalf("calls transport=%d encode=%d decode=%d", len(transport.requests), codec.encodeCalls, codec.decodeCalls)
	}
	if result.Plan == nil || !result.Plan.Valid {
		t.Fatalf("plan = %#v", result.Plan)
	}
	hash := sha256.Sum256([]byte("encoded-wire-body"))
	if result.Plan.BodySHA256 != hex.EncodeToString(hash[:]) {
		t.Fatalf("body hash = %q", result.Plan.BodySHA256)
	}
	if got := result.Plan.RequestPreview["body"]; !reflect.DeepEqual(got, original) {
		t.Fatalf("request preview body = %#v", got)
	}
	if result.Plan.RequestPreview["body_source"] != "input_before_special_io" {
		t.Fatalf("request preview = %#v", result.Plan.RequestPreview)
	}
	if result.Plan.PageAll == nil || !result.Plan.PageAll.Requested {
		t.Fatalf("page-all plan = %#v", result.Plan.PageAll)
	}
	if !hasCheck(result.Plan.Checks, "endpoint", true) ||
		!hasCheck(result.Plan.Checks, "region", true) {
		t.Fatalf("checks = %#v", result.Plan.Checks)
	}
	if hasCheck(result.Plan.Checks, "profile", true) {
		t.Fatalf("successful profile check leaked into plan: %#v", result.Plan.Checks)
	}
}

func TestExecutorDryRunPreviewPreservesJSONNumberLexemes(t *testing.T) {
	op := requestTestOperation()
	result, err := NewExecutor(&fakeTransport{}, NewCodecRegistry()).Execute(context.Background(), Invocation{
		Operation: op,
		Input: Input{
			Path: map[string]any{"ProjectId": "p1"},
			Body: Payload{JSON: map[string]any{
				"big":     json.Number("9007199254740993"),
				"decimal": json.Number("0.12345678901234567890123456789"),
			}},
		},
		Options: Options{DryRun: true},
		Runtime: RuntimeView{Endpoint: "https://tls.example.com", Region: "cn-beijing"},
	})
	if err != nil {
		t.Fatalf("Execute(dry-run) error = %v", err)
	}
	body, ok := result.Plan.RequestPreview["body"].(map[string]any)
	if !ok {
		t.Fatalf("preview body type = %T", result.Plan.RequestPreview["body"])
	}
	for key, want := range map[string]string{
		"big":     "9007199254740993",
		"decimal": "0.12345678901234567890123456789",
	} {
		number, ok := body[key].(json.Number)
		if !ok || number.String() != want {
			t.Errorf("preview %s = %#v, want json.Number(%q)", key, body[key], want)
		}
	}
}

func TestExecutorRejectsPageAllWithoutPaginationMetadata(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		_, err := NewExecutor(&fakeTransport{}, NewCodecRegistry()).Execute(context.Background(), Invocation{
			Operation: requestTestOperation(),
			Input:     Input{Path: map[string]any{"ProjectId": "p1"}},
			Options:   Options{PageAll: true, DryRun: dryRun},
		})
		if err == nil || err.Error() != "execution.page.all is not supported for tool: project.describe" {
			t.Fatalf("dryRun=%v error = %v", dryRun, err)
		}
	}
}

func TestExecutorDryRunMarksInvalidJSONBodyWithoutTransport(t *testing.T) {
	transport := &fakeTransport{}
	op := requestTestOperation()
	result, err := NewExecutor(transport, NewCodecRegistry()).Execute(context.Background(), Invocation{
		Operation: op,
		Input: Input{
			Path: map[string]any{"ProjectId": "p1"},
			Body: Payload{Raw: []byte(`{"broken":`), Format: BodyFormatJSON},
		},
		Options: Options{DryRun: true},
		Runtime: RuntimeView{
			Endpoint: "https://tls.example.com",
			Region:   "cn-beijing",
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Plan == nil || result.Plan.Valid || !hasCheck(result.Plan.Checks, "body_json", false) {
		t.Fatalf("plan = %#v", result.Plan)
	}
	if len(transport.requests) != 0 {
		t.Fatalf("transport calls = %d", len(transport.requests))
	}
}

func TestExecutorDryRunStopsRuntimeChecksAfterProfileFailure(t *testing.T) {
	op := requestTestOperation()
	result, err := NewExecutor(&fakeTransport{}, NewCodecRegistry()).Execute(context.Background(), Invocation{
		Operation: op,
		Input:     Input{Path: map[string]any{"ProjectId": "p1"}},
		Options:   Options{DryRun: true},
		Runtime: RuntimeView{
			Endpoint: "must-not-be-rendered",
			Region:   "must-not-be-rendered",
			Checks: []PreflightCheck{{
				Name:   "profile",
				OK:     false,
				Detail: stringPointer("profile resolution failed"),
			}},
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Plan == nil || result.Plan.Valid || len(result.Plan.Checks) != 1 ||
		result.Plan.Checks[0].Name != "profile" {
		t.Fatalf("checks = %#v", result.Plan)
	}
}

func TestZeroValueExecutorUsesLocalDefaultRegistry(t *testing.T) {
	op := requestTestOperation()
	input := Input{Path: map[string]any{"ProjectId": "p1"}}
	var executor Executor

	_, err := executor.Execute(context.Background(), Invocation{
		Operation: op,
		Input:     input,
	})
	if err == nil || err.Error() != "nil transport" {
		t.Fatalf("real execution error = %v, want nil transport", err)
	}

	result, err := executor.Execute(context.Background(), Invocation{
		Operation: op,
		Input:     input,
		Options:   Options{DryRun: true},
		Runtime:   RuntimeView{Endpoint: "endpoint", Region: "region"},
	})
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if result.Plan == nil || !result.Plan.Valid {
		t.Fatalf("dry-run plan = %#v", result.Plan)
	}
	if executor.codecs != nil {
		t.Fatal("zero-value Execute mutated executor codec registry")
	}
}

func TestExecutorRejectsTypedNilTransport(t *testing.T) {
	var transport *fakeTransport
	result, err := NewExecutor(transport, NewCodecRegistry()).Execute(context.Background(), Invocation{
		Operation: requestTestOperation(),
		Input:     Input{Path: map[string]any{"ProjectId": "p1"}},
	})
	if err == nil || err.Error() != "nil transport" {
		t.Fatalf("result=%#v error=%v, want nil transport", result, err)
	}
}

func TestZeroValueExecutorConcurrentDryRunDoesNotInitializeSharedState(t *testing.T) {
	var executor Executor
	op := requestTestOperation()
	results := make(chan error, 16)
	for i := 0; i < cap(results); i++ {
		go func() {
			result, err := executor.Execute(context.Background(), Invocation{
				Operation: op,
				Input:     Input{Path: map[string]any{"ProjectId": "p1"}},
				Options:   Options{DryRun: true},
				Runtime:   RuntimeView{Endpoint: "endpoint", Region: "region"},
			})
			if err == nil && result.Plan == nil {
				err = errors.New("missing dry-run plan")
			}
			results <- err
		}()
	}
	for i := 0; i < cap(results); i++ {
		if err := <-results; err != nil {
			t.Fatalf("concurrent dry-run: %v", err)
		}
	}
	if executor.codecs != nil {
		t.Fatal("concurrent dry-run mutated executor codec registry")
	}
}

func TestDryRunPlanCheckDetailPresenceMatchesLegacyShape(t *testing.T) {
	tests := []struct {
		name       string
		invocation Invocation
		preview    Request
		encoded    Request
		want       []map[string]any
	}{
		{
			name: "empty endpoint and region retain empty detail",
			invocation: Invocation{
				Operation: contract.Operation{Wire: contract.WireSpec{Codec: contract.CodecJSON}},
			},
			preview: Request{Body: []byte(`{}`), BodyFormat: BodyFormatJSON},
			encoded: Request{Body: []byte(`{}`), BodyFormat: BodyFormatJSON},
			want: []map[string]any{
				{"name": "endpoint", "ok": false, "detail": ""},
				{"name": "region", "ok": false, "detail": ""},
			},
		},
		{
			name: "successful body json omits detail",
			invocation: Invocation{
				Operation: contract.Operation{Wire: contract.WireSpec{Codec: contract.CodecJSON}},
				Runtime:   RuntimeView{Endpoint: "endpoint", Region: "region"},
			},
			preview: Request{Body: []byte(`{"ok":true}`), BodyFormat: BodyFormatJSON},
			encoded: Request{Body: []byte(`{"ok":true}`), BodyFormat: BodyFormatJSON},
			want: []map[string]any{
				{"name": "endpoint", "ok": true, "detail": "endpoint"},
				{"name": "region", "ok": true, "detail": "region"},
				{"name": "body_json", "ok": true},
			},
		},
		{
			name: "successful body codec omits detail",
			invocation: Invocation{
				Operation: contract.Operation{Wire: contract.WireSpec{Codec: contract.CodecPutLogs}},
				Runtime:   RuntimeView{Endpoint: "endpoint", Region: "region"},
			},
			preview: Request{Body: []byte(`{"Logs":[]}`), BodyFormat: BodyFormatJSON},
			encoded: Request{Body: []byte("protobuf"), BodyFormat: BodyFormatJSON},
			want: []map[string]any{
				{"name": "endpoint", "ok": true, "detail": "endpoint"},
				{"name": "region", "ok": true, "detail": "region"},
				{"name": "body_codec", "ok": true},
			},
		},
		{
			name: "failed body json includes detail",
			invocation: Invocation{
				Operation: contract.Operation{Wire: contract.WireSpec{Codec: contract.CodecJSON}},
				Runtime:   RuntimeView{Endpoint: "endpoint", Region: "region"},
			},
			preview: Request{Body: []byte(`{"broken":`), BodyFormat: BodyFormatJSON},
			encoded: Request{Body: []byte(`{"broken":`), BodyFormat: BodyFormatJSON},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := buildDryRunPlan(tt.invocation, tt.preview, tt.encoded)
			raw, err := json.Marshal(plan)
			if err != nil {
				t.Fatalf("marshal plan: %v", err)
			}
			var decoded map[string]any
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatalf("decode plan: %v", err)
			}
			checkValues, _ := decoded["checks"].([]any)
			checks := make([]map[string]any, 0, len(checkValues))
			for _, value := range checkValues {
				check, _ := value.(map[string]any)
				checks = append(checks, check)
			}
			if tt.name == "failed body json includes detail" {
				if len(checks) != 3 || checks[2]["name"] != "body_json" ||
					checks[2]["ok"] != false || strings.TrimSpace(checks[2]["detail"].(string)) == "" {
					t.Fatalf("checks = %#v", checks)
				}
				return
			}
			if !reflect.DeepEqual(checks, tt.want) {
				t.Fatalf("checks = %#v, want %#v", checks, tt.want)
			}
		})
	}
}

func TestCodecRegistryFailsClosedForUnregisteredCatalogSpecialCodecs(t *testing.T) {
	registry := NewCodecRegistry()
	for _, id := range []contract.CodecID{
		contract.CodecPutLogs,
		contract.CodecWebTracks,
		contract.CodecConsumeLogs,
	} {
		codec, err := registry.Resolve(id)
		if err != nil {
			t.Fatalf("Resolve(%s): %v", id, err)
		}
		_, _, err = codec.Encode(context.Background(), contract.Operation{}, Request{})
		if err == nil || !strings.Contains(err.Error(), string(id)) {
			t.Fatalf("codec %s error = %v", id, err)
		}
	}
}

func hasCheck(checks []PreflightCheck, name string, ok bool) bool {
	for _, check := range checks {
		if check.Name == name && check.OK == ok {
			return true
		}
	}
	return false
}

func stringPointer(value string) *string {
	return &value
}
