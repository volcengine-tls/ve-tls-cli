//go:build darwin || linux

package cli

import (
	"context"
	"io"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

const promptPollMillis = 40

type unixPromptReader struct {
	ctx  context.Context
	file *os.File
	raw  syscall.RawConn
}

func newContextFilePromptReader(ctx context.Context, f *os.File, _ string) (io.Reader, error) {
	raw, err := f.SyscallConn()
	if err != nil {
		return nil, err
	}
	return &unixPromptReader{ctx: ctx, file: f, raw: raw}, nil
}

func (r *unixPromptReader) Read(p []byte) (int, error) {
	for {
		if err := r.ctx.Err(); err != nil {
			return 0, err
		}
		if len(p) == 0 {
			return 0, nil
		}
		var pollErr error
		var count int
		var events int16
		err := r.raw.Control(func(fd uintptr) {
			fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
			count, pollErr = unix.Poll(fds, promptPollMillis)
			events = fds[0].Revents
		})
		if err != nil {
			return 0, err
		}
		if pollErr == unix.EINTR {
			continue
		}
		if pollErr != nil {
			return 0, pollErr
		}
		if err := r.ctx.Err(); err != nil {
			return 0, err
		}
		if count == 0 {
			continue
		}
		if events&unix.POLLNVAL != 0 {
			return 0, unix.EBADF
		}
		if events&(unix.POLLIN|unix.POLLHUP) == 0 {
			if events&unix.POLLERR != 0 {
				return 0, unix.EIO
			}
			continue
		}
		n, readErr := r.file.Read(p)
		if ctxErr := r.ctx.Err(); ctxErr != nil {
			return 0, ctxErr
		}
		return n, readErr
	}
}
