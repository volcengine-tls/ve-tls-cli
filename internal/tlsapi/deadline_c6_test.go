package tlsapi

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/volcengine/volc-sdk-golang/base"
)

func TestC6RequestCancellationBeforeSend(t *testing.T) {
	for _, stage := range []string{"before request", "after signing"} {
		t.Run(stage, func(t *testing.T) {
			client := requestErrorTestClient(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			client.HTTP = &http.Client{Transport: requestErrorRoundTripper(func(*http.Request) (*http.Response, error) {
				calls++
				return nil, errors.New("unexpected transport call")
			})}
			wantStage := "build_request"
			if stage == "before request" {
				cancel()
			} else {
				wantStage = "sign"
				client.requestSigner = func(_ base.Credentials, req *http.Request) *http.Request {
					req.Header.Set("Authorization", "test")
					cancel()
					return req
				}
			}
			_, err := client.Do(ctx, http.MethodPost, "/", nil, nil, nil)
			assertRequestFailure(t, err, wantStage, "not_sent")
			if !errors.Is(err, context.Canceled) || calls != 0 {
				t.Fatalf("error=%v transport calls=%d", err, calls)
			}
		})
	}
}

func TestC6CancellationDuringTransportRemainsUnknown(t *testing.T) {
	client := requestErrorTestClient(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	client.HTTP = &http.Client{Transport: requestErrorRoundTripper(func(req *http.Request) (*http.Response, error) {
		calls++
		cancel()
		return nil, req.Context().Err()
	})}
	_, err := client.Do(ctx, http.MethodPost, "/", nil, nil, nil)
	assertRequestFailure(t, err, "transport", "unknown")
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("error=%v transport calls=%d", err, calls)
	}
}
