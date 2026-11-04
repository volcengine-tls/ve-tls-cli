package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth"
	"github.com/volcengine-tls/ve-tls-cli/internal/execution"
	"github.com/volcengine-tls/ve-tls-cli/internal/jsonx"
	"github.com/volcengine-tls/ve-tls-cli/internal/output"
	"github.com/volcengine-tls/ve-tls-cli/internal/tlsapi"
)

const ingestTestSecret = "private-log-payload-canary"

type ingestTestTransport struct {
	calls     int
	failAt    int
	failHTTP  bool
	badJSONAt int
}

func (rt *ingestTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	_ = req.Body.Close()
	rt.calls++
	if rt.calls == rt.badJSONAt {
		header := make(http.Header)
		header.Set("x-tls-requestid", "req-malformed")
		return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader(`{}{}`))}, nil
	}
	if rt.calls == rt.failAt {
		if !rt.failHTTP {
			return nil, io.EOF
		}
		header := make(http.Header)
		header.Set("x-tls-requestid", "req-failed")
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Header:     header,
			Body:       io.NopCloser(strings.NewReader(`{"ErrorCode":"BadRequest","ErrorMessage":"` + ingestTestSecret + `"}`)),
		}, nil
	}
	header := make(http.Header)
	header.Set("x-tls-requestid", "req-confirmed")
	return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
}

func ingestTestContext(t *testing.T, rt http.RoundTripper) *Context {
	t.Helper()
	client, err := tlsapi.New("https://example.com", "cn-beijing", "", "ak", "sk", "", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	client.HTTP = &http.Client{Transport: rt}
	ctx := newContext(io.Discard, io.Discard, output.FormatJSON, "", "")
	ctx.client = client
	return ctx
}

func ingestTestArgs() []string {
	return []string{"--topic-id", "tid", "--input", ingestTestSecret + "\nsecond\nthird\nfourth\nfifth", "--batch-max-count", "2"}
}

func TestIngestProgressFailures(t *testing.T) {
	tests := []struct {
		name       string
		failAt     int
		failHTTP   bool
		confirmed  int
		outcome    string
		requestID  string
		statusCode int
	}{
		{name: "first_http_failure", failAt: 1, failHTTP: true, outcome: execution.OutcomeFailed, requestID: "req-failed", statusCode: http.StatusBadRequest},
		{name: "second_http_failure", failAt: 2, failHTTP: true, confirmed: 1, outcome: execution.OutcomeFailed, requestID: "req-failed", statusCode: http.StatusBadRequest},
		{name: "second_transport_eof", failAt: 2, confirmed: 1, outcome: execution.OutcomeUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := &ingestTestTransport{failAt: tt.failAt, failHTTP: tt.failHTTP}
			ctx := ingestTestContext(t, rt)
			out, err := logIngest(ctx, ingestTestArgs())
			var failure *ingestFailure
			if !errors.As(err, &failure) {
				t.Fatalf("expected ingestFailure, got %v", err)
			}
			if rt.calls != tt.failAt {
				t.Fatalf("requests=%d, want %d without replay", rt.calls, tt.failAt)
			}
			confirmedLogs := tt.confirmed * 2
			lastID := ""
			if tt.confirmed > 0 {
				lastID = "req-confirmed"
			}
			wantProgress := map[string]any{
				"confirmedBatches": tt.confirmed, "confirmedLogs": confirmedLogs, "confirmedLogOffset": confirmedLogs,
				"totalLogs": 5, "totalBatches": 3, "currentBatch": tt.failAt,
				"currentBatchStartOffset": confirmedLogs, "currentBatchEndOffset": confirmedLogs + 2,
				"currentBatchOutcome": tt.outcome, "lastConfirmedRequestId": lastID,
			}
			if !reflect.DeepEqual(failure.Progress, wantProgress) {
				t.Fatalf("progress=%#v want=%#v", failure.Progress, wantProgress)
			}
			result, ok := out.(map[string]any)
			if !ok || result["logs"] != confirmedLogs || result["batches"] != tt.confirmed || !reflect.DeepEqual(result, failure.FailureData()) {
				t.Fatalf("unexpected failure data: %#v", out)
			}
			wantAggregate := tt.outcome
			wantKind, wantCode := "", ""
			if tt.confirmed > 0 {
				wantAggregate, wantKind, wantCode = execution.OutcomePartial, "partial_failure", "IngestPartialFailure"
			}
			if failure.RemoteOutcome() != wantAggregate || ctx.RemoteOutcome != wantAggregate || failure.ErrorKind() != wantKind || failure.ErrorCode() != wantCode {
				t.Fatalf("outcome=%q context=%q kind=%q code=%q", failure.RemoteOutcome(), ctx.RemoteOutcome, failure.ErrorKind(), failure.ErrorCode())
			}
			if ctx.RequestID != tt.requestID || ctx.StatusCode != tt.statusCode {
				t.Fatalf("current response id=%q status=%d", ctx.RequestID, ctx.StatusCode)
			}
			if tt.failHTTP {
				var he *httpError
				if !errors.As(err, &he) || he.statusCode != http.StatusBadRequest {
					t.Fatalf("HTTP cause lost: %v", err)
				}
				if tt.confirmed == 0 {
					wrapped, _ := classifyError(err, ctx.RequestID, ctx.StatusCode, "workflow")
					underlying, _ := classifyError(failure.Cause, ctx.RequestID, ctx.StatusCode, "workflow")
					if wrapped.Kind != underlying.Kind {
						t.Fatalf("first failure kind=%q, underlying kind=%q", wrapped.Kind, underlying.Kind)
					}
				}
			}
			data, _ := json.Marshal(result)
			if strings.Contains(err.Error(), ingestTestSecret) || bytes.Contains(data, []byte(ingestTestSecret)) {
				t.Fatal("failure exposed log data")
			}
		})
	}
}

func TestIngestProgressMalformedSuccessResponseIsUnknown(t *testing.T) {
	rt := &ingestTestTransport{badJSONAt: 2}
	ctx := ingestTestContext(t, rt)
	out, err := logIngest(ctx, ingestTestArgs())
	var failure *ingestFailure
	if !errors.As(err, &failure) || !errors.Is(err, jsonx.ErrTrailingData) {
		t.Fatalf("expected wrapped response decoding failure, got %v", err)
	}
	result := out.(map[string]any)
	if rt.calls != 2 || result["batches"] != 1 || result["logs"] != 2 || failure.Progress["confirmedLogOffset"] != 2 || failure.Progress["currentBatchOutcome"] != execution.OutcomeUnknown {
		t.Fatalf("requests=%d result=%#v", rt.calls, result)
	}
	if failure.Progress["lastConfirmedRequestId"] != "req-confirmed" || ctx.RequestID != "req-malformed" || ctx.StatusCode != http.StatusOK || ctx.RemoteOutcome != execution.OutcomePartial {
		t.Fatalf("current id=%q status=%d outcome=%q progress=%#v", ctx.RequestID, ctx.StatusCode, ctx.RemoteOutcome, failure.Progress)
	}
}

type ingestFailSecondAuth struct{ calls int }

func (p *ingestFailSecondAuth) Retrieve(context.Context) (auth.Value, error) {
	p.calls++
	if p.calls == 2 {
		return auth.Value{}, &auth.Error{Kind: auth.ConfigInvalid, Description: "test credential failure"}
	}
	return auth.Value{AccessKeyID: "ak", SecretAccessKey: "sk"}, nil
}

func TestIngestProgressSecondBatchAuthFailureNotSent(t *testing.T) {
	rt := &ingestTestTransport{}
	provider := &ingestFailSecondAuth{}
	client, err := tlsapi.NewWithProvider("https://example.com", "cn-beijing", provider, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	client.HTTP = &http.Client{Transport: rt}
	ctx := newContext(io.Discard, io.Discard, output.FormatJSON, "", "")
	ctx.client = client
	_, err = logIngest(ctx, ingestTestArgs())
	var failure *ingestFailure
	var authErr *auth.Error
	if !errors.As(err, &failure) || !errors.As(err, &authErr) {
		t.Fatalf("expected wrapped auth failure, got %v", err)
	}
	if provider.calls != 2 || rt.calls != 1 {
		t.Fatalf("provider calls=%d requests=%d", provider.calls, rt.calls)
	}
	if failure.Progress["confirmedLogs"] != 2 || failure.Progress["currentBatchOutcome"] != execution.OutcomeNotSent || failure.RemoteOutcome() != execution.OutcomePartial {
		t.Fatalf("unexpected progress: %#v", failure.Progress)
	}
	if ctx.RequestID != "" || ctx.StatusCode != 0 || failure.Progress["lastConfirmedRequestId"] != "req-confirmed" {
		t.Fatalf("stale current response: id=%q status=%d progress=%#v", ctx.RequestID, ctx.StatusCode, failure.Progress)
	}
}

func TestIngestProgressSuccessDryRunAndEmpty(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		dryRun    bool
		requests  int
		logs      int
		batches   int
		confirmed int
		current   int
		start     int
		end       int
		outcome   string
		lastID    string
	}{
		{name: "success", args: ingestTestArgs(), requests: 3, logs: 5, batches: 3, confirmed: 3, current: 3, start: 4, end: 5, outcome: execution.OutcomeSuccess, lastID: "req-confirmed"},
		{name: "dry_run", args: ingestTestArgs(), dryRun: true, logs: 5, batches: 3, current: 3, start: 4, end: 5, outcome: execution.OutcomeNotSent},
		{name: "empty", args: []string{"--topic-id", "tid", "--input", "[]", "--input-format", "json-array"}, outcome: execution.OutcomeNotSent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := &ingestTestTransport{}
			ctx := ingestTestContext(t, rt)
			ctx.DryRun = tt.dryRun
			if tt.name == "empty" {
				ctx.RequestID = "previous-request"
				ctx.StatusCode = http.StatusOK
			}
			out, err := logIngest(ctx, tt.args)
			if err != nil || rt.calls != tt.requests {
				t.Fatalf("error=%v requests=%d, want %d", err, rt.calls, tt.requests)
			}
			result := out.(map[string]any)
			progress := result["progress"].(map[string]any)
			confirmedLogs := 0
			if tt.confirmed > 0 {
				confirmedLogs = tt.logs
			}
			want := map[string]any{
				"confirmedBatches": tt.confirmed, "confirmedLogs": confirmedLogs, "confirmedLogOffset": confirmedLogs,
				"totalLogs": tt.logs, "totalBatches": tt.batches, "currentBatch": tt.current,
				"currentBatchStartOffset": tt.start, "currentBatchEndOffset": tt.end,
				"currentBatchOutcome": tt.outcome, "lastConfirmedRequestId": tt.lastID,
			}
			if result["logs"] != tt.logs || result["batches"] != tt.batches || !reflect.DeepEqual(progress, want) {
				t.Fatalf("result=%#v want progress=%#v", result, want)
			}
			if tt.name == "empty" && (ctx.RequestID != "" || ctx.StatusCode != 0 || ctx.RemoteOutcome != execution.OutcomeNotSent) {
				t.Fatalf("empty input has stale request state: id=%q status=%d outcome=%q", ctx.RequestID, ctx.StatusCode, ctx.RemoteOutcome)
			}
		})
	}
}

func TestWorkflowIngestFailureEnvelopePreservesProgress(t *testing.T) {
	for _, mode := range []string{"http", "eof", "first_http"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				call := calls.Add(1)
				if call == 1 && mode != "first_http" {
					w.Header().Set("x-tls-requestid", "req-confirmed")
					_, _ = io.WriteString(w, `{}`)
					return
				}
				if mode == "eof" {
					conn, _, err := w.(http.Hijacker).Hijack()
					if err == nil {
						_ = conn.Close()
					}
					return
				}
				w.Header().Set("x-tls-requestid", "req-failed")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"ErrorCode":"BadRequest","ErrorMessage":"`+ingestTestSecret+`"}`)
			}))
			defer server.Close()
			t.Setenv("VOLCENGINE_ACCESS_KEY_ID", "ak")
			t.Setenv("VOLCENGINE_ACCESS_KEY_SECRET", "sk")
			t.Setenv("VOLCENGINE_REGION", "cn-beijing")
			t.Setenv("VOLCENGINE_ENDPOINT", server.URL)
			t.Setenv("VOLCLOG_CONFIG", filepath.Join(t.TempDir(), "config.json"))
			input, _ := json.Marshal(map[string]any{"TopicId": "tid", "Input": ingestTestSecret + "\nsecond\nthird\nfourth\nfifth", "BatchMaxCount": 2})
			var stdout, stderr bytes.Buffer
			code := Run([]string{"workflow", "exec", "log.ingest", "--input", string(input)}, &stdout, &stderr)
			wantRequests := int32(2)
			if mode == "first_http" {
				wantRequests = 1
			}
			if code == 0 || calls.Load() != wantRequests {
				t.Fatalf("exit=%d requests=%d stdout=%s stderr=%s", code, calls.Load(), stdout.String(), stderr.String())
			}
			if strings.Contains(stdout.String()+stderr.String(), ingestTestSecret) {
				t.Fatal("workflow error exposed log body")
			}
			var env map[string]any
			if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
				t.Fatalf("decode envelope: %v", err)
			}
			data, _ := env["data"].(map[string]any)
			summary, _ := env["summary"].(map[string]any)
			progress, _ := data["progress"].(map[string]any)
			if !reflect.DeepEqual(progress, summary["progress"]) || progress == nil {
				t.Fatalf("progress missing from data or summary: %#v", env)
			}
			wantBatches, wantLogs, wantBatch, wantID, wantCurrent := float64(1), float64(2), float64(2), "req-failed", execution.OutcomeFailed
			if mode == "first_http" {
				wantBatches, wantLogs, wantBatch = 0, 0, 1
			} else if mode == "eof" {
				wantID, wantCurrent = "", execution.OutcomeUnknown
			}
			if data["batches"] != wantBatches || data["logs"] != wantLogs || progress["confirmedLogOffset"] != wantLogs || progress["currentBatch"] != wantBatch || progress["currentBatchOutcome"] != wantCurrent || env["requestId"] != wantID {
				t.Fatalf("unexpected envelope: %#v", env)
			}
			wantAggregate := execution.OutcomePartial
			if mode == "first_http" {
				wantAggregate = execution.OutcomeFailed
			}
			if summary["remoteOutcome"] != wantAggregate || summary["deliveryOutcome"] != execution.OutcomeSuccess {
				t.Fatalf("unexpected outcomes: %#v", summary)
			}
			errData, _ := env["error"].(map[string]any)
			if mode == "first_http" {
				if errData["kind"] != "usage" || errData["code"] == "IngestPartialFailure" {
					t.Fatalf("first batch classification changed: %#v", errData)
				}
			} else if errData["kind"] != "partial_failure" || errData["code"] != "IngestPartialFailure" || progress["lastConfirmedRequestId"] != "req-confirmed" {
				t.Fatalf("partial failure metadata missing: %#v", env)
			}
			if mode == "eof" {
				hint, _ := errData["hint"].(string)
				if strings.Contains(strings.ToLower(hint), "retry") || strings.Contains(strings.ToLower(hint), "resume") {
					t.Fatalf("unsafe retry hint: %q", hint)
				}
			}
		})
	}
}

func TestIngestTimeErrorsDoNotExposeInput(t *testing.T) {
	for _, format := range []string{"auto", "unix", "unix_ms", "rfc3339"} {
		t.Run(format, func(t *testing.T) {
			_, err := parseIngestTimeValue(ingestTestSecret, format)
			if err == nil || strings.Contains(err.Error(), ingestTestSecret) {
				t.Fatalf("unsafe or missing parse error: %v", err)
			}
		})
	}
}
