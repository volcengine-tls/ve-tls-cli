//go:build windows

package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"time"
)

const promptReadInterval = 40 * time.Millisecond

type windowsPromptReader struct {
	ctx  context.Context
	file *os.File
	hint string
}

func newContextFilePromptReader(ctx context.Context, f *os.File, hint string) (io.Reader, error) {
	if ctx.Done() == nil {
		return &contextPromptReader{ctx: ctx, reader: f}, nil
	}
	// Console handles normally reject deadlines. Probe before writing a prompt so
	// a cancellable invocation never enters an uninterruptible console read.
	if err := f.SetReadDeadline(time.Now().Add(promptReadInterval)); err != nil {
		return nil, &NonInteractiveInteractionError{Hint: hint}
	}
	if err := f.SetReadDeadline(time.Time{}); err != nil {
		return nil, err
	}
	return &windowsPromptReader{ctx: ctx, file: f, hint: hint}, nil
}

func (r *windowsPromptReader) Read(p []byte) (int, error) {
	for {
		if err := r.ctx.Err(); err != nil {
			return 0, err
		}
		if len(p) == 0 {
			return 0, nil
		}
		if err := r.file.SetReadDeadline(time.Now().Add(promptReadInterval)); err != nil {
			return 0, &NonInteractiveInteractionError{Hint: r.hint}
		}
		n, readErr := r.file.Read(p)
		clearErr := r.file.SetReadDeadline(time.Time{})
		if ctxErr := r.ctx.Err(); ctxErr != nil {
			return 0, ctxErr
		}
		if clearErr != nil {
			return 0, clearErr
		}
		if n == 0 && errors.Is(readErr, os.ErrDeadlineExceeded) {
			continue
		}
		return n, readErr
	}
}
