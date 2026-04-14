package cli

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tlssdk "github.com/volcengine/volc-sdk-golang/service/tls"
	tlspb "github.com/volcengine/volc-sdk-golang/service/tls/pb"

	"github.com/volcengine-tls/ve-tls-cli/internal/output"
	"github.com/volcengine-tls/ve-tls-cli/internal/tlsapi"
)

type capturedPutLogsRequest struct {
	TopicID      string
	Path         string
	Compression  string
	LogCount     string
	EarliestTime string
	LatestTime   string
	Body         []byte
	RawSize      int64
}

type captureRoundTripper struct {
	mu       sync.Mutex
	requests []capturedPutLogsRequest
}

func (rt *captureRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	_ = req.Body.Close()
	rt.mu.Lock()
	rawSize, err := parseInt64String(req.Header.Get("x-tls-bodyrawsize"))
	if err != nil {
		rt.mu.Unlock()
		return nil, err
	}
	rt.requests = append(rt.requests, capturedPutLogsRequest{
		TopicID:      req.URL.Query().Get("TopicId"),
		Path:         req.URL.Path,
		Compression:  req.Header.Get("x-tls-compresstype"),
		LogCount:     req.Header.Get("log-count"),
		EarliestTime: req.Header.Get("earliest-log-time"),
		LatestTime:   req.Header.Get("latest-log-time"),
		Body:         body,
		RawSize:      rawSize,
	})
	rt.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"Status":"OK"}`)),
	}, nil
}

func (rt *captureRoundTripper) snapshot() []capturedPutLogsRequest {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	out := make([]capturedPutLogsRequest, len(rt.requests))
	copy(out, rt.requests)
	return out
}

func TestLogIngest_LinesBatchesAndUsesDefaultContentField(t *testing.T) {
	inputPath := filepath.Join(t.TempDir(), "app.log")
	if err := os.WriteFile(inputPath, []byte("alpha\nbeta\ngamma\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rt := &captureRoundTripper{}
	ctx := newTestLogContext(t, rt)

	out, err := runLog(ctx, []string{
		"ingest",
		"--topic-id", "tid",
		"--input", "file://" + inputPath,
		"--input-format", "lines",
		"--source", "host-a",
		"--file-name", "app.log",
		"--tag", "env=test",
		"--batch-max-count", "2",
	})
	if err != nil {
		t.Fatalf("runLog error: %v", err)
	}
	summary, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("unexpected summary: %#v", out)
	}
	if summary["logs"] != 3 || summary["batches"] != 2 {
		t.Fatalf("unexpected summary: %#v", summary)
	}

	gotRequests := rt.snapshot()
	if len(gotRequests) != 2 {
		t.Fatalf("expected 2 putlogs requests, got %d", len(gotRequests))
	}
	if gotRequests[0].Path != "/PutLogs" || gotRequests[1].Path != "/PutLogs" {
		t.Fatalf("unexpected request paths: %#v", gotRequests)
	}
	if gotRequests[0].Compression != tlssdk.CompressLz4 || gotRequests[1].Compression != tlssdk.CompressLz4 {
		t.Fatalf("expected lz4 compression, got %#v", gotRequests)
	}
	if gotRequests[0].LogCount != "2" || gotRequests[1].LogCount != "1" {
		t.Fatalf("unexpected log-count headers: %#v", gotRequests)
	}

	first := mustDecodeCapturedPutLogs(t, gotRequests[0])
	second := mustDecodeCapturedPutLogs(t, gotRequests[1])
	if len(first.LogGroups) != 1 || len(first.LogGroups[0].Logs) != 2 {
		t.Fatalf("unexpected first batch: %+v", first)
	}
	if len(second.LogGroups) != 1 || len(second.LogGroups[0].Logs) != 1 {
		t.Fatalf("unexpected second batch: %+v", second)
	}
	if first.LogGroups[0].Source != "host-a" || first.LogGroups[0].FileName != "app.log" {
		t.Fatalf("unexpected group metadata: %+v", first.LogGroups[0])
	}
	if len(first.LogGroups[0].LogTags) != 1 || first.LogGroups[0].LogTags[0].Key != "env" || first.LogGroups[0].LogTags[0].Value != "test" {
		t.Fatalf("unexpected group tags: %+v", first.LogGroups[0].LogTags)
	}
	gotContents := []string{
		first.LogGroups[0].Logs[0].Contents[0].Value,
		first.LogGroups[0].Logs[1].Contents[0].Value,
		second.LogGroups[0].Logs[0].Contents[0].Value,
	}
	if strings.Join(gotContents, ",") != "alpha,beta,gamma" {
		t.Fatalf("unexpected contents: %v", gotContents)
	}
	for _, batch := range gotRequests {
		decoded := mustDecodeCapturedPutLogs(t, batch)
		for _, item := range decoded.LogGroups[0].Logs {
			if len(item.Contents) != 1 || item.Contents[0].Key != "__content__" {
				t.Fatalf("expected __content__ field, got %+v", item.Contents)
			}
			if item.Time == 0 {
				t.Fatalf("expected default time to be set")
			}
		}
		if batch.EarliestTime != batch.LatestTime {
			t.Fatalf("expected identical earliest/latest for default timestamps, got %+v", batch)
		}
	}
}

func TestLogIngest_JSONLPreservesFieldsAndUsesTimeField(t *testing.T) {
	inputPath := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.WriteFile(inputPath, []byte(
		`{"ts":1710000002000,"level":"info","msg":"hello","count":3}`+"\n"+
			`{"ts":1710000001000,"level":"warn","msg":"world","extra":{"k":"v"}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rt := &captureRoundTripper{}
	ctx := newTestLogContext(t, rt)

	out, err := runLog(ctx, []string{
		"ingest",
		"--topic-id", "tid",
		"--input", "file://" + inputPath,
		"--input-format", "jsonl",
		"--time-field", "ts",
		"--time-format", "unix_ms",
	})
	if err != nil {
		t.Fatalf("runLog error: %v", err)
	}
	summary, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("unexpected summary: %#v", out)
	}
	if summary["logs"] != 2 || summary["batches"] != 1 {
		t.Fatalf("unexpected summary: %#v", summary)
	}

	gotRequests := rt.snapshot()
	if len(gotRequests) != 1 {
		t.Fatalf("expected 1 putlogs request, got %d", len(gotRequests))
	}
	if gotRequests[0].Compression != tlssdk.CompressLz4 {
		t.Fatalf("expected lz4 compression, got %+v", gotRequests[0])
	}
	if gotRequests[0].LogCount != "2" || gotRequests[0].EarliestTime != "1710000001000" || gotRequests[0].LatestTime != "1710000002000" {
		t.Fatalf("unexpected stats headers: %+v", gotRequests[0])
	}

	decoded := mustDecodeCapturedPutLogs(t, gotRequests[0])
	if len(decoded.LogGroups) != 1 || len(decoded.LogGroups[0].Logs) != 2 {
		t.Fatalf("unexpected decoded batches: %+v", decoded)
	}
	gotTimes := []int64{decoded.LogGroups[0].Logs[0].Time, decoded.LogGroups[0].Logs[1].Time}
	if gotTimes[0] != 1710000002000 || gotTimes[1] != 1710000001000 {
		t.Fatalf("unexpected log times: %v", gotTimes)
	}
	firstContents := flattenContents(decoded.LogGroups[0].Logs[0].Contents)
	if firstContents["ts"] != "1710000002000" || firstContents["level"] != "info" || firstContents["msg"] != "hello" || firstContents["count"] != "3" {
		t.Fatalf("unexpected first contents: %#v", firstContents)
	}
	secondContents := flattenContents(decoded.LogGroups[0].Logs[1].Contents)
	if secondContents["ts"] != "1710000001000" || secondContents["level"] != "warn" || secondContents["msg"] != "world" || secondContents["extra"] != `{"k":"v"}` {
		t.Fatalf("unexpected second contents: %#v", secondContents)
	}
}

func TestLogIngest_JSONArrayDefaultsMissingTimeToInvocationTime(t *testing.T) {
	inputPath := filepath.Join(t.TempDir(), "events.json")
	if err := os.WriteFile(inputPath, []byte(
		`[`+
			`{"level":"info","msg":"hello"},`+
			`{"level":"warn","msg":"world"}`+
			`]`), 0o600); err != nil {
		t.Fatal(err)
	}
	rt := &captureRoundTripper{}
	ctx := newTestLogContext(t, rt)

	out, err := runLog(ctx, []string{
		"ingest",
		"--topic-id", "tid",
		"--input", "file://" + inputPath,
		"--input-format", "json-array",
	})
	if err != nil {
		t.Fatalf("runLog error: %v", err)
	}
	summary, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("unexpected summary: %#v", out)
	}
	if summary["logs"] != 2 || summary["batches"] != 1 {
		t.Fatalf("unexpected summary: %#v", summary)
	}

	gotRequests := rt.snapshot()
	if len(gotRequests) != 1 {
		t.Fatalf("expected 1 putlogs request, got %d", len(gotRequests))
	}
	if gotRequests[0].EarliestTime != gotRequests[0].LatestTime {
		t.Fatalf("expected identical earliest/latest for default timestamps, got %+v", gotRequests[0])
	}
	decoded := mustDecodeCapturedPutLogs(t, gotRequests[0])
	if len(decoded.LogGroups) != 1 || len(decoded.LogGroups[0].Logs) != 2 {
		t.Fatalf("unexpected decoded batches: %+v", decoded)
	}
	for _, item := range decoded.LogGroups[0].Logs {
		if item.Time == 0 {
			t.Fatalf("expected default time to be set")
		}
	}
	firstContents := flattenContents(decoded.LogGroups[0].Logs[0].Contents)
	secondContents := flattenContents(decoded.LogGroups[0].Logs[1].Contents)
	if firstContents["level"] != "info" || firstContents["msg"] != "hello" {
		t.Fatalf("unexpected first contents: %#v", firstContents)
	}
	if secondContents["level"] != "warn" || secondContents["msg"] != "world" {
		t.Fatalf("unexpected second contents: %#v", secondContents)
	}
}

func newTestLogContext(t *testing.T, rt http.RoundTripper) *Context {
	t.Helper()
	ctx := newContext(&bytes.Buffer{}, &bytes.Buffer{}, output.FormatJSON, "", "")
	cl, err := tlsapi.New("https://example.com", "cn-beijing", "", "ak", "sk", "", time.Second)
	if err != nil {
		t.Fatalf("new tls client: %v", err)
	}
	cl.HTTP = &http.Client{Transport: rt}
	ctx.client = cl
	return ctx
}

func mustDecodeCapturedPutLogs(t *testing.T, req capturedPutLogsRequest) *tlspb.LogGroupList {
	t.Helper()
	out, err := tlssdk.GetLogGroupList(req.Compression, req.RawSize, req.Body)
	if err != nil {
		t.Fatalf("decode putlogs body: %v", err)
	}
	return out
}
