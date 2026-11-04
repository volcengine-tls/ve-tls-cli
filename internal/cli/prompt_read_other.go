//go:build !darwin && !linux && !windows

package cli

import (
	"context"
	"io"
	"os"
)

func newContextFilePromptReader(ctx context.Context, f *os.File, hint string) (io.Reader, error) {
	if ctx.Done() != nil {
		return nil, &NonInteractiveInteractionError{Hint: hint}
	}
	return &contextPromptReader{ctx: ctx, reader: f}, nil
}
