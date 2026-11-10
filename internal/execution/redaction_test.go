package execution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/volcengine-tls/ve-tls-cli/internal/contract"
)

func TestRedactRequestPreviewNestedFieldsAndEncodedQuery(t *testing.T) {
	queryObject := ` { "filters": [{"Session_Token":"query-canary","name":"demo"}], "n": 9007199254740993 } `
	input := map[string]any{
		"query": map[string]any{
			"Filter": queryObject,
			"Repeat": []string{`{"safe":"keep"}`, `[{"WebHook-Secret":"repeat-canary"}]`},
			"Query":  `* | select token from logs`,
			"Mask":   "keep-mask",
		},
		"body": map[string]any{
			"Password":      "password-canary",
			"pass-wd":       "passwd-canary",
			"WebhookSecret": "secret-canary",
			"ACCESS_KEY_ID": "access-canary",
			"Authorization": "auth-canary",
			"Credentials":   "credential-canary",
			"private-key":   "private-canary",
			"ak":            "ak-canary",
			"sK":            "sk-canary",
			"nested": []any{map[string]any{
				"SESSION-token": "token-canary",
				"ordinary":      `{"Token":"a literal body string"}`,
			}},
		},
	}
	before, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	got, fields := RedactRequestPreview(input)
	wantFields := []string{
		"body.ACCESS_KEY_ID", "body.Authorization", "body.Credentials", "body.Password",
		"body.WebhookSecret", "body.ak", "body.nested[0].SESSION-token", "body.pass-wd",
		"body.private-key", "body.sK", "query.Filter.filters[0].Session_Token",
		"query.Repeat[1][0].WebHook-Secret",
	}
	if !reflect.DeepEqual(fields, wantFields) {
		t.Fatalf("redacted fields = %#v, want %#v", fields, wantFields)
	}
	body := got["body"].(map[string]any)
	if body["Password"] != "***" || body["ak"] != "***" || body["sK"] != "***" {
		t.Fatalf("body not redacted: %#v", body)
	}
	if body["nested"].([]any)[0].(map[string]any)["ordinary"] != `{"Token":"a literal body string"}` {
		t.Fatal("ordinary body string changed")
	}
	query := got["query"].(map[string]any)
	if query["Query"] != `* | select token from logs` || query["Mask"] != "keep-mask" {
		t.Fatalf("ordinary query changed: %#v", query)
	}
	var filter map[string]any
	if err := json.Unmarshal([]byte(query["Filter"].(string)), &filter); err != nil {
		t.Fatal(err)
	}
	if filter["filters"].([]any)[0].(map[string]any)["Session_Token"] != "***" || !strings.Contains(query["Filter"].(string), "9007199254740993") {
		t.Fatalf("encoded filter = %q", query["Filter"])
	}
	repeat := query["Repeat"].([]string)
	if repeat[0] != `{"safe":"keep"}` || strings.Contains(repeat[1], "repeat-canary") {
		t.Fatalf("repeated query = %#v", repeat)
	}
	after, err := json.Marshal(input)
	if err != nil || string(before) != string(after) {
		t.Fatalf("input mutated: before=%s after=%s err=%v", before, after, err)
	}
}

func TestRedactRequestPreviewPutLogsKeyValueFields(t *testing.T) {
	input := map[string]any{"body": map[string]any{"LogGroups": []any{map[string]any{
		"LogTags": []any{map[string]any{"Key": "Access-Key", "Value": "tag-canary"}, map[string]any{"Key": "team", "Value": "keep"}},
		"Logs": []any{map[string]any{"Contents": []any{
			map[string]any{"Key": "Password", "Value": "body-canary"},
			map[string]any{"key": "session_token", "value": "body-token-canary"},
			map[string]any{"Key": "message", "Value": "keep"},
		}}},
	}}}}
	before, _ := json.Marshal(input)
	got, fields := RedactRequestPreview(input)
	want := []string{
		"body.LogGroups[0].LogTags[0].Value",
		"body.LogGroups[0].Logs[0].Contents[0].Value",
		"body.LogGroups[0].Logs[0].Contents[1].value",
	}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("fields=%#v, want %#v", fields, want)
	}
	encoded, _ := json.Marshal(got)
	if strings.Contains(string(encoded), "canary") || !strings.Contains(string(encoded), `"Value":"keep"`) {
		t.Fatalf("preview = %s", encoded)
	}
	after, _ := json.Marshal(input)
	if string(before) != string(after) {
		t.Fatal("input mutated")
	}
}

func TestRedactRequestPreviewOmitsMalformedJSONQuery(t *testing.T) {
	input := map[string]any{
		"query": map[string]any{
			"Filter": `{"Token":"query-canary",`,
			"Repeat": []string{`["array-canary",`, `{"safe":"keep"}`, "* | select token"},
			"Query":  "* | select token",
		},
		"body": map[string]any{"text": `{"Token":"body-canary",`},
	}
	before, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	got, fields := RedactRequestPreview(input)
	wantFields := []string{"query.Filter", "query.Repeat[0]"}
	if !reflect.DeepEqual(fields, wantFields) {
		t.Fatalf("fields = %#v, want %#v", fields, wantFields)
	}
	query := got["query"].(map[string]any)
	if query["Filter"] != UnparseableQueryPreview {
		t.Fatalf("malformed query preview = %#v", query["Filter"])
	}
	repeat := query["Repeat"].([]string)
	if repeat[0] != UnparseableQueryPreview || repeat[1] != `{"safe":"keep"}` || repeat[2] != "* | select token" || query["Query"] != "* | select token" {
		t.Fatalf("query = %#v", query)
	}
	if got["body"].(map[string]any)["text"] != `{"Token":"body-canary",` {
		t.Fatal("ordinary body string changed")
	}
	after, err := json.Marshal(input)
	if err != nil || string(before) != string(after) {
		t.Fatalf("input mutated: before=%s after=%s err=%v", before, after, err)
	}
	queryJSON, _ := json.Marshal(query)
	if strings.Contains(string(queryJSON), "canary") {
		t.Fatalf("malformed query leaked: %s", queryJSON)
	}
}

func TestRedactPreviewPathOmitsMalformedJSONQuery(t *testing.T) {
	original := "/action?Filter=" + url.QueryEscape(`{"Token":"path-canary",`) + "&limit=2"
	preview, fields := RedactPreviewPath(original)
	if strings.Contains(preview, "canary") || !reflect.DeepEqual(fields, []string{"path.query.Filter"}) {
		t.Fatalf("path preview = %q fields=%#v", preview, fields)
	}
	parsed, err := url.Parse(preview)
	if err != nil || parsed.Query().Get("Filter") != UnparseableQueryPreview || parsed.Query().Get("limit") != "2" {
		t.Fatalf("path preview = %q err=%v", preview, err)
	}
	if !strings.Contains(original, "path-canary") {
		t.Fatal("original path changed")
	}
}

func TestExecutorPreviewRedactionKeepsWireBodyAndHash(t *testing.T) {
	for _, special := range []bool{false, true} {
		t.Run(map[bool]string{false: "json", true: "special-codec"}[special], func(t *testing.T) {
			raw := []byte(`{"Password":"body-canary","Name":"keep"}`)
			query := map[string]any{"SessionToken": "query-canary", "Filter": `{"Secret":"nested-canary"}`}
			operation := requestTestOperation()
			encodedBody := append([]byte(nil), raw...)
			if special {
				operation.Wire.Codec = contract.CodecPutLogs
				encodedBody = []byte("wire-body-with-body-canary")
			}
			codec := &countingCodec{encode: func(request Request) (Request, any, error) {
				request.Body = append([]byte(nil), encodedBody...)
				return request, nil, nil
			}}
			registry := NewCodecRegistry()
			if err := registry.Register(operation.Wire.Codec, codec); err != nil {
				t.Fatal(err)
			}
			transport := &fakeTransport{responses: []Response{{StatusCode: http.StatusOK, Body: []byte(`{}`)}}}
			executor := NewExecutor(transport, registry)
			invocation := Invocation{
				Operation: operation,
				Input:     Input{Path: map[string]any{"ProjectId": "p1"}, Query: query, Body: Payload{Raw: raw, Format: BodyFormatJSON}},
				Options:   Options{DryRun: true, ValidationPolicy: ValidationCallerLegacy},
				Runtime:   RuntimeView{Endpoint: "https://tls.example.com", Region: "cn-beijing"},
			}
			result, err := executor.Execute(context.Background(), invocation)
			if err != nil {
				t.Fatal(err)
			}
			planJSON, _ := json.Marshal(result.Plan)
			if strings.Contains(string(planJSON), "canary") || !strings.Contains(string(planJSON), `"Name":"keep"`) {
				t.Fatalf("plan = %s", planJSON)
			}
			wantFields := []string{"body.Password", "query.Filter.Secret", "query.SessionToken"}
			if !reflect.DeepEqual(result.Plan.RedactedFields, wantFields) {
				t.Fatalf("fields = %#v", result.Plan.RedactedFields)
			}
			sum := sha256.Sum256(encodedBody)
			if result.Plan.BodySHA256 != hex.EncodeToString(sum[:]) || len(transport.requests) != 0 {
				t.Fatalf("hash=%q transport calls=%d", result.Plan.BodySHA256, len(transport.requests))
			}
			if special && result.Plan.RequestPreview["body_source"] != "input_before_special_io" {
				t.Fatalf("special preview = %#v", result.Plan.RequestPreview)
			}
			invocation.Options.DryRun = false
			if _, err := executor.Execute(context.Background(), invocation); err != nil {
				t.Fatal(err)
			}
			if len(transport.requests) != 1 || string(transport.requests[0].Body) != string(encodedBody) || transport.requests[0].Query["SessionToken"] != "query-canary" {
				t.Fatalf("wire request = %#v", transport.requests)
			}
			if string(raw) != `{"Password":"body-canary","Name":"keep"}` || query["SessionToken"] != "query-canary" {
				t.Fatal("invocation input mutated")
			}
		})
	}
}

func TestExecutorPreviewJSONLAndUnparseableBodies(t *testing.T) {
	for _, tc := range []struct {
		name       string
		body       string
		format     BodyFormat
		wantFields []string
	}{
		{"jsonl", "{\"Password\":\"jsonl-canary\",\"Name\":\"keep\"}\n{\"nested\":{\"Token\":\"second-canary\"}}", BodyFormatJSONL, []string{"body[0].Password", "body[1].nested.Token"}},
		{"broken-json", `{"Password":"broken-canary",`, BodyFormatJSON, nil},
		{"broken-jsonl", "{\"Password\":\"first-canary\"}\n{\"Token\":\"broken-canary\",", BodyFormatJSONL, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := Request{Body: []byte(tc.body), BodyFormat: tc.format}
			plan := buildDryRunPlan(Invocation{Operation: requestTestOperation(), Runtime: RuntimeView{Endpoint: "https://tls.example.com", Region: "cn-beijing"}}, request, request)
			encoded, _ := json.Marshal(plan)
			if strings.Contains(string(encoded), "canary") {
				t.Fatalf("preview leaked body: %s", encoded)
			}
			if !reflect.DeepEqual(plan.RedactedFields, tc.wantFields) {
				t.Fatalf("fields = %#v, want %#v", plan.RedactedFields, tc.wantFields)
			}
			if tc.wantFields == nil && plan.RequestPreview["body"] != UnparseableBodyPreview {
				t.Fatalf("broken body preview = %#v", plan.RequestPreview["body"])
			}
		})
	}
}

func TestDryRunURLPreviewRedaction(t *testing.T) {
	invocation := Invocation{Operation: requestTestOperation(), Runtime: RuntimeView{Endpoint: "https://user:password-canary@example.com:443/base?token=endpoint-canary#fragment-canary", Region: "cn-beijing"}}
	request := Request{Method: "POST", Path: `/action?limit=2&SessionToken=path-canary&Filter=%7B%22Password%22%3A%22nested-canary%22%7D`, Body: []byte(`{}`), BodyFormat: BodyFormatJSON}
	plan := buildDryRunPlan(invocation, request, request)
	encoded, _ := json.Marshal(plan)
	if strings.Contains(string(encoded), "canary") || !strings.Contains(plan.Path, "limit=2") {
		t.Fatalf("URL preview = %s", encoded)
	}
	want := []string{"checks.endpoint.detail", "path.query.Filter.Password", "path.query.SessionToken"}
	if !reflect.DeepEqual(plan.RedactedFields, want) {
		t.Fatalf("fields = %#v, want %#v", plan.RedactedFields, want)
	}
	if request.Path != `/action?limit=2&SessionToken=path-canary&Filter=%7B%22Password%22%3A%22nested-canary%22%7D` || !strings.Contains(invocation.Runtime.Endpoint, "endpoint-canary") {
		t.Fatal("input URL mutated")
	}
	if got, changed := RedactPreviewURL("http://bad%zz?token=canary"); !changed || strings.Contains(got, "canary") {
		t.Fatalf("invalid URL preview = %q, changed=%v", got, changed)
	}
	for _, endpoint := range []string{"user:secret-canary@example.com", "secret-canary@example.com"} {
		if got, changed := RedactPreviewURL(endpoint); !changed || strings.Contains(got, "canary") {
			t.Fatalf("ambiguous endpoint preview = %q, changed=%v", got, changed)
		}
	}
	if got, changed := RedactPreviewURL("https://example.com/base"); changed || got != "https://example.com/base" {
		t.Fatalf("safe URL preview = %q, changed=%v", got, changed)
	}
}
