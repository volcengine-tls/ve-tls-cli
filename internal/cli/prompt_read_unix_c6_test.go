//go:build darwin || linux

package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func fileFlagsC6(t *testing.T, f *os.File) int {
	t.Helper()
	raw, err := f.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var flags int
	var fcntlErr error
	if err := raw.Control(func(fd uintptr) { flags, fcntlErr = unix.FcntlInt(fd, unix.F_GETFL, 0) }); err != nil {
		t.Fatal(err)
	}
	if fcntlErr != nil {
		t.Fatal(fcntlErr)
	}
	return flags
}

func TestUnixPromptReadCancellationPreservesInputC6(t *testing.T) {
	for _, mode := range []string{"deadline", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			input, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			defer writer.Close()
			before := fileFlagsC6(t, input)
			if inputIsTerminal(input) {
				t.Fatal("pipe reported as terminal")
			}
			if after := fileFlagsC6(t, input); after != before {
				t.Fatalf("terminal detection changed fd flags: before=%d after=%d", before, after)
			}

			var ctx context.Context
			var cancel context.CancelFunc
			if mode == "deadline" {
				ctx, cancel = context.WithTimeout(context.Background(), 100*time.Millisecond)
			} else {
				ctx, cancel = context.WithCancel(context.Background())
			}
			defer cancel()
			reader, err := newContextFilePromptReader(ctx, input, "")
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			started := make(chan struct{})
			go func() {
				close(started)
				_, readErr := reader.Read(make([]byte, 1))
				done <- readErr
			}()
			<-started
			if mode == "cancel" {
				// Let the read enter the polling loop while the pipe stays empty.
				time.Sleep(60 * time.Millisecond)
				cancel()
			}
			select {
			case err := <-done:
				want := context.DeadlineExceeded
				if mode == "cancel" {
					want = context.Canceled
				}
				if !errors.Is(err, want) {
					t.Fatalf("read error=%v, want %v", err, want)
				}
			case <-time.After(2 * time.Second):
				// Unblock even a regressed implementation before failing the test.
				_ = writer.Close()
				select {
				case <-done:
				case <-time.After(time.Second):
				}
				t.Fatal("prompt read ignored cancellation")
			}
			if after := fileFlagsC6(t, input); after != before {
				t.Fatalf("prompt read changed fd flags: before=%d after=%d", before, after)
			}
			if _, err := writer.Write([]byte("next\n")); err != nil {
				t.Fatal(err)
			}
			got := make([]byte, len("next\n"))
			if _, err := io.ReadFull(input, got); err != nil || string(got) != "next\n" {
				t.Fatalf("subsequent input=%q err=%v", got, err)
			}
		})
	}
}

func TestUnixPromptReaderKeepsFollowingInputC6(t *testing.T) {
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	defer writer.Close()
	if _, err := writer.Write([]byte("yes\nnext\n")); err != nil {
		t.Fatal(err)
	}
	reader, err := newContextFilePromptReader(context.Background(), input, "")
	if err != nil {
		t.Fatal(err)
	}
	line, err := readConfirmLine(reader)
	if err != nil || line != "yes" {
		t.Fatalf("line=%q err=%v", line, err)
	}
	remaining := make([]byte, len("next\n"))
	if _, err := io.ReadFull(input, remaining); err != nil || string(remaining) != "next\n" {
		t.Fatalf("remaining=%q err=%v", remaining, err)
	}
}
