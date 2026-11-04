package tlsapi

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth"
)

type requestErrorRoundTripper func(*http.Request) (*http.Response, error)

func (f requestErrorRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type requestErrorBody struct {
	data   []byte
	err    error
	closed int
}

func (b *requestErrorBody) Read(p []byte) (int, error) {
	if len(b.data) == 0 {
		return 0, b.err
	}
	n := copy(p, b.data)
	b.data = b.data[n:]
	if len(b.data) == 0 {
		return n, b.err
	}
	return n, nil
}

func (b *requestErrorBody) Close() error {
	b.closed++
	return nil
}

func requestErrorTestClient(t *testing.T) *Client {
	t.Helper()
	client, err := New("https://example.invalid", "cn-test", "", "fake-ak", "fake-sk", "", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func assertRequestFailure(t *testing.T, err error, stage, outcome string) *RequestError {
	t.Helper()
	var failure *RequestError
	if !errors.As(err, &failure) {
		t.Fatalf("error %v is not a RequestError", err)
	}
	if failure.FailureStage() != stage || failure.RemoteOutcome() != outcome {
		t.Fatalf("stage=%q outcome=%q, want %q %q", failure.FailureStage(), failure.RemoteOutcome(), stage, outcome)
	}
	return failure
}

func TestRequestFailureBeforeSend(t *testing.T) {
	authCause := &auth.Error{Kind: auth.ReauthRequired, Cause: errors.New("credentials unavailable")}
	tests := []struct {
		name   string
		method string
		path   string
		stage  string
		auth   bool
	}{
		{name: "invalid URL", method: http.MethodPost, path: "/%zz", stage: "build_request"},
		{name: "invalid method", method: "bad method", path: "/", stage: "build_request"},
		{name: "signing provider", method: http.MethodPost, path: "/", stage: "sign", auth: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := requestErrorTestClient(t)
			if tc.auth {
				client.provider = &fakeProvider{retrieve: func(context.Context) (auth.Value, error) {
					return auth.Value{}, authCause
				}}
			}
			rt := &captureRoundTripper{}
			client.HTTP = &http.Client{Transport: rt}
			_, err := client.Do(context.Background(), tc.method, tc.path, nil, nil, nil)
			failure := assertRequestFailure(t, err, tc.stage, "not_sent")
			if len(rt.snapshot()) != 0 {
				t.Fatal("request reached the HTTP transport")
			}
			if tc.auth {
				var actual *auth.Error
				if !errors.As(err, &actual) || actual != authCause || !errors.Is(err, authCause.Cause) {
					t.Fatalf("authentication cause was lost: %v", err)
				}
				if failure.ErrorKind() != "" {
					t.Fatalf("signing failure hid auth classification: %q", failure.ErrorKind())
				}
			}
		})
	}
}

func TestTransportFailureIsUnknownEvenWhenDialLooking(t *testing.T) {
	cause := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("fake failure")}
	client := requestErrorTestClient(t)
	client.HTTP = &http.Client{Transport: requestErrorRoundTripper(func(*http.Request) (*http.Response, error) {
		return nil, cause
	})}
	_, err := client.Do(context.Background(), http.MethodPost, "/", nil, nil, nil)
	failure := assertRequestFailure(t, err, "transport", "unknown")
	if failure.ErrorKind() != "result_unknown" || failure.ErrorCode() != "RequestResultUnknown" || !errors.Is(err, cause) {
		t.Fatalf("unexpected classification or missing cause: %v", err)
	}
}

func TestRequestFailureMessageDoesNotExposeCause(t *testing.T) {
	const secret = "private-token-value"
	cause := errors.New("https://user:" + secret + "@example.invalid/?token=" + secret)
	tests := []struct {
		name  string
		stage string
		setup func(*Client)
		path  string
	}{
		{name: "build", stage: "build_request", path: "/%zz?token=" + secret},
		{name: "sign", stage: "sign", path: "/", setup: func(client *Client) {
			client.provider = &fakeProvider{retrieve: func(context.Context) (auth.Value, error) { return auth.Value{}, cause }}
		}},
		{name: "transport", stage: "transport", path: "/", setup: func(client *Client) {
			client.HTTP = &http.Client{Transport: requestErrorRoundTripper(func(*http.Request) (*http.Response, error) { return nil, cause })}
		}},
		{name: "read", stage: "read_response", path: "/", setup: func(client *Client) {
			client.HTTP = &http.Client{Transport: requestErrorRoundTripper(func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header),
					Body: &requestErrorBody{data: []byte("partial"), err: cause}, Request: req}, nil
			})}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := requestErrorTestClient(t)
			if tc.setup != nil {
				tc.setup(client)
			}
			_, err := client.Do(context.Background(), http.MethodPost, tc.path, nil, nil, nil)
			outcome := "not_sent"
			if tc.stage == "transport" || tc.stage == "read_response" {
				outcome = "unknown"
			}
			failure := assertRequestFailure(t, err, tc.stage, outcome)
			if strings.Contains(err.Error(), secret) || strings.Contains(failure.ErrorCode(), secret) ||
				strings.Contains(failure.ErrorKind(), secret) {
				t.Fatalf("secret leaked in request error: %v", err)
			}
			if tc.setup != nil && !errors.Is(err, cause) {
				t.Fatalf("cause was lost: %v", err)
			}
		})
	}
}

func TestResponseAndTransportErrorPreserveResponse(t *testing.T) {
	redirectCause := errors.New("redirect rejected")
	bodyCause := errors.New("partial response body")
	body := &requestErrorBody{data: []byte("partial"), err: bodyCause}
	client := requestErrorTestClient(t)
	client.HTTP = &http.Client{
		Transport: requestErrorRoundTripper(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode:    http.StatusFound,
				Header:        http.Header{"Location": {"https://example.invalid/next"}, "X-Tls-Requestid": {"redirect-id"}},
				Body:          body,
				ContentLength: 4096,
				Request:       req,
			}, nil
		}),
		CheckRedirect: func(*http.Request, []*http.Request) error { return redirectCause },
	}
	response, err := client.Do(context.Background(), http.MethodPost, "/", nil, nil, nil)
	assertRequestFailure(t, err, "transport", "unknown")
	if !errors.Is(err, redirectCause) || response.StatusCode != http.StatusFound ||
		response.Header.Get("X-Tls-Requestid") != "redirect-id" || string(response.Body) != "partial" || body.closed != 1 {
		t.Fatalf("response=%+v error=%v closed=%d", response, err, body.closed)
	}
}

func TestResponseReadFailuresPreserveEvidence(t *testing.T) {
	bodyCause := errors.New("response read failed")
	var compressed bytes.Buffer
	w := gzip.NewWriter(&compressed)
	if _, err := w.Write([]byte("partial decoded body")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	truncated := compressed.Bytes()[:compressed.Len()-4]
	tests := []struct {
		name     string
		encoding string
		data     []byte
		readErr  error
		wantBody string
		wantErr  error
	}{
		{name: "body read", data: []byte("partial body"), readErr: bodyCause, wantBody: "partial body", wantErr: bodyCause},
		{name: "gzip header", encoding: "gzip", data: []byte("not gzip data"), readErr: io.EOF, wantBody: "not gzip data"},
		{name: "gzip body", encoding: "gzip", data: truncated, readErr: io.EOF, wantBody: "partial decoded body", wantErr: io.ErrUnexpectedEOF},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := &requestErrorBody{data: tc.data, err: tc.readErr}
			client := requestErrorTestClient(t)
			client.HTTP = &http.Client{Transport: requestErrorRoundTripper(func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{
					"X-Tls-Requestid": {"response-id"}, "Content-Encoding": {tc.encoding},
				}, Body: body, Request: req}, nil
			})}
			response, err := client.Do(context.Background(), http.MethodPost, "/", nil, nil, nil)
			failure := assertRequestFailure(t, err, "read_response", "unknown")
			if failure.ErrorKind() != "result_unknown" || failure.ErrorCode() != "RequestResultUnknown" ||
				(tc.wantErr != nil && !errors.Is(err, tc.wantErr)) || response.StatusCode != http.StatusOK ||
				response.Header.Get("X-Tls-Requestid") != "response-id" || string(response.Body) != tc.wantBody || body.closed == 0 {
				t.Fatalf("response=%+v error=%v closed=%d", response, err, body.closed)
			}
		})
	}
}

func TestSuccessfulGzipResponseStillDecodes(t *testing.T) {
	var compressed bytes.Buffer
	w := gzip.NewWriter(&compressed)
	if _, err := w.Write([]byte("decoded response")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	body := &requestErrorBody{data: compressed.Bytes(), err: io.EOF}
	client := requestErrorTestClient(t)
	client.HTTP = &http.Client{Transport: requestErrorRoundTripper(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Encoding": {"gzip"}}, Body: body, Request: req}, nil
	})}
	response, err := client.Do(context.Background(), http.MethodGet, "/", nil, nil, nil)
	if err != nil || string(response.Body) != "decoded response" || body.closed != 1 {
		t.Fatalf("response=%+v error=%v closed=%d", response, err, body.closed)
	}
}

func TestServerReceivesPostThenDisconnectsWithoutReplay(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodPost {
			requests.Add(1)
		}
		_, _ = io.Copy(io.Discard, req.Body)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}))
	defer server.Close()
	client := requestErrorTestClient(t)
	client.Endpoint = server.URL
	client.HTTP = server.Client()
	_, err := client.Do(context.Background(), http.MethodPost, "/", nil, nil, []byte("payload"))
	assertRequestFailure(t, err, "transport", "unknown")
	if got := requests.Load(); got != 1 {
		t.Fatalf("server received %d POST requests, want 1", got)
	}
}
