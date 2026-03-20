package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
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
	if strings.TrimSpace(c.TraceRedact) == "" {
		c.TraceRedact = "strict"
	}
	return nil
}

func (c *Context) traceRequest(method, path string, query map[string]string, body []byte) {
	if strings.TrimSpace(c.TraceDir) == "" {
		return
	}
	if err := c.initTrace(); err != nil {
		return
	}
	keys := make([]string, 0, len(query))
	for k := range query {
		kk := strings.TrimSpace(k)
		if kk != "" {
			keys = append(keys, kk)
		}
	}
	sort.Strings(keys)
	evt := traceEvent{
		TS:              time.Now().UTC().Format(time.RFC3339Nano),
		Type:            "http_request",
		Method:          method,
		Path:            path,
		QueryKeys:       keys,
		HeadersRedacted: []string{"Authorization", "X-Security-Token"},
		BodySHA256:      sha256Hex(body),
	}
	_ = c.writeTrace(evt)
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

func sha256Hex(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
