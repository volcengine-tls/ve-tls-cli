//go:build windows

package cli

import (
	"context"
	"os"
	"testing"
)

func TestWindowsUnsupportedPromptDeadlineFailsClosedC6(t *testing.T) {
	file, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err = newContextFilePromptReader(ctx, file, accountInteractionHint)
	requireInteractionC6(t, err, "--account-id")
}
