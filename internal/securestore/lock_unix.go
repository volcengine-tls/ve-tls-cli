//go:build !windows

package securestore

import (
	"context"
	"errors"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func acquireOSFileLock(ctx context.Context, path string) (func() error, error) {
	file, err := openPrivateLockFile(path, 0o600)
	if err != nil {
		return nil, err
	}
	for {
		err = unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return func() error {
				unlockErr := unix.Flock(int(file.Fd()), unix.LOCK_UN)
				closeErr := file.Close()
				return errors.Join(unlockErr, closeErr)
			}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			_ = file.Close()
			return nil, err
		}
		lockContendedHook(path)
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			_ = file.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
