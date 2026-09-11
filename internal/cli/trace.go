package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	appruntime "github.com/volcengine-tls/ve-tls-cli/internal/app/runtime"
	"github.com/volcengine-tls/ve-tls-cli/internal/execution"
)

type traceEvent struct {
	TS              string   `json:"ts"`
	Type            string   `json:"type"`
	Method          string   `json:"method,omitempty"`
	Path            string   `json:"path,omitempty"`
	QueryKeys       []string `json:"query_keys,omitempty"`
	HeadersRedacted []string `json:"headers_redacted,omitempty"`
	BodySHA256      string   `json:"body_sha256,omitempty"`
	Status          int      `json:"status,omitempty"`
	RequestID       string   `json:"request_id,omitempty"`
	ElapsedMS       int64    `json:"elapsed_ms,omitempty"`
	RespSHA256      string   `json:"resp_body_sha256,omitempty"`
	ErrorMessage    string   `json:"error_message,omitempty"`
}

type contextRuntimeTracer struct {
	context *Context
}

func (t contextRuntimeTracer) TraceRequest(_ context.Context, request execution.Request) {
	if t.context == nil {
		return
	}
	t.context.traceRequest(request.Method, request.Path, request.Query, request.Body)
}

func (t contextRuntimeTracer) TraceResponse(_ context.Context, response execution.Response, elapsed time.Duration, err error) {
	if t.context == nil {
		return
	}
	if err != nil {
		t.context.traceResponse(0, "", elapsed, nil, err)
		return
	}
	t.context.traceResponse(
		response.StatusCode,
		response.Header.Get("x-tls-requestid"),
		elapsed,
		response.Body,
		nil,
	)
}

var _ appruntime.Tracer = contextRuntimeTracer{}

func (c *Context) initTrace() error {
	if strings.TrimSpace(c.TraceDir) == "" {
		return nil
	}
	if c.traceW != nil {
		return nil
	}
	dir := filepath.Clean(c.TraceDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	name := "trace-" + time.Now().UTC().Format("2006-01-02T15-04-05.000Z") + ".jsonl"
	p := filepath.Join(dir, name)
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	c.traceW = f
	c.TracePath = p
	c.TraceRedact = normalizeTraceRedactValue(c.TraceRedact)
	return nil
}

func (c *Context) traceRequest(method, path string, query map[string]string, body []byte) {
	if strings.TrimSpace(c.TraceDir) == "" {
		return
	}
	if err := c.initTrace(); err != nil {
		return
	}
	path, pathQueryKeys := tracePath(path)
	keySet := make(map[string]struct{}, len(query)+len(pathQueryKeys))
	for k := range query {
		kk := strings.TrimSpace(k)
		if kk != "" {
			keySet[kk] = struct{}{}
		}
	}
	for _, key := range pathQueryKeys {
		keySet[key] = struct{}{}
	}
	keys := make([]string, 0, len(keySet))
	for key := range keySet {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	evt := traceEvent{
		TS:              time.Now().UTC().Format(time.RFC3339Nano),
		Type:            "http_request",
		Method:          method,
		Path:            path,
		QueryKeys:       keys,
		HeadersRedacted: redactedHeaderKeys(nil),
		BodySHA256:      sha256Hex(body),
	}
	_ = c.writeTrace(evt)
}

func tracePath(raw string) (string, []string) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.RawQuery == "" {
		return raw, nil
	}
	keys := make([]string, 0, len(parsed.Query()))
	for key := range parsed.Query() {
		key = strings.TrimSpace(key)
		if key != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	parsed.RawQuery = ""
	return parsed.String(), keys
}

func (c *Context) traceResponse(status int, requestID string, elapsed time.Duration, body []byte, err error) {
	if strings.TrimSpace(c.TraceDir) == "" {
		return
	}
	if err2 := c.initTrace(); err2 != nil {
		return
	}
	evt := traceEvent{
		TS:         time.Now().UTC().Format(time.RFC3339Nano),
		Type:       "http_response",
		Status:     status,
		RequestID:  requestID,
		ElapsedMS:  elapsed.Milliseconds(),
		RespSHA256: sha256Hex(body),
	}
	if err != nil {
		evt.ErrorMessage = err.Error()
	}
	_ = c.writeTrace(evt)
}

func (c *Context) writeTrace(evt traceEvent) error {
	if c.traceW == nil {
		return nil
	}
	b, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	_, err = c.traceW.Write(append(b, '\n'))
	return err
}

func (c *Context) tracePlan(method, path string, query map[string]string, header map[string]string, body []byte, plan map[string]any) {
	if strings.TrimSpace(c.TraceDir) == "" {
		return
	}
	if err := c.initTrace(); err != nil {
		return
	}
	queryKeys := make([]string, 0, len(query))
	for k := range query {
		kk := strings.TrimSpace(k)
		if kk != "" {
			queryKeys = append(queryKeys, kk)
		}
	}
	sort.Strings(queryKeys)
	evt := traceEvent{
		TS:              time.Now().UTC().Format(time.RFC3339Nano),
		Type:            "plan",
		Method:          method,
		Path:            path,
		QueryKeys:       queryKeys,
		HeadersRedacted: redactedHeaderKeys(header),
		BodySHA256:      sha256Hex(body),
	}
	if valid, ok := plan["valid"].(bool); ok && !valid {
		evt.ErrorMessage = "dry-run local checks failed"
	}
	_ = c.writeTrace(evt)
}

func (c *Context) traceToolExecutionPlan(plan *execution.DryRunPlan) {
	if plan == nil || strings.TrimSpace(c.TraceDir) == "" {
		return
	}
	if err := c.initTrace(); err != nil {
		return
	}
	evt := traceEvent{
		TS:              time.Now().UTC().Format(time.RFC3339Nano),
		Type:            "plan",
		Method:          plan.Method,
		Path:            plan.Path,
		QueryKeys:       append([]string(nil), plan.QueryKeys...),
		HeadersRedacted: append([]string(nil), plan.HeadersRedacted...),
		BodySHA256:      plan.BodySHA256,
	}
	if !plan.Valid {
		evt.ErrorMessage = "dry-run local checks failed"
	}
	_ = c.writeTrace(evt)
}

func sha256Hex(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func normalizeTraceRedactValue(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "on", "true", "1", "yes", "enabled", "strict", "default":
		return "on"
	case "off", "false", "0", "no", "disabled":
		return "off"
	default:
		return "on"
	}
}
