//go:build human

package cli

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/volcengine-tls/ve-tls-cli/internal/auth"
	"github.com/volcengine-tls/ve-tls-cli/internal/execution"
	"github.com/volcengine-tls/ve-tls-cli/internal/tlsapi"
)

type streamCancelProvider struct{ cancel context.CancelFunc }

func (p streamCancelProvider) Retrieve(context.Context) (auth.Value, error) {
	p.cancel()
	return auth.Value{AccessKeyID: "ak", SecretAccessKey: "sk"}, nil
}

func TestC6StreamCancellationAfterSigningDoesNotSend(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	client, err := tlsapi.NewWithProvider("https://example.invalid", "cn-beijing", streamCancelProvider{cancel: cancel}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	transport := &assistantCaptureRT{}
	client.HTTP = &http.Client{Transport: transport}
	_, err = doStream(parent, client, "POST", "/DescribeSessionAnswer", nil, nil, []byte(`{}`))
	if !errors.Is(err, context.Canceled) || len(transport.snapshot()) != 0 || execution.ErrorOutcome(err, "") != execution.OutcomeNotSent {
		t.Fatalf("error=%v requests=%d", err, len(transport.snapshot()))
	}
	payload, code := classifyError(err, "", 0, "assistant")
	if code != 2 || payload.Kind != "cancelled" {
		t.Fatalf("classification=%#v exit=%d", payload, code)
	}
}
