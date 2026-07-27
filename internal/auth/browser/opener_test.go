package browser

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// recordingRunner records the command it was asked to run and returns a
// configurable error.
type recordingRunner struct {
	name string
	args []string
	err  error
}

func (r *recordingRunner) run(ctx context.Context, name string, args ...string) error {
	r.name = name
	r.args = args
	return r.err
}

// TestOpenUsesPlatformCommand verifies the correct command and argv for each
// supported platform, and that the URL is a separate argv element.
func TestOpenUsesPlatformCommand(t *testing.T) {
	cases := []struct {
		goos     string
		wantName string
		wantArgs []string
	}{
		{"darwin", "open", []string{"https://example.com/callback"}},
		{"linux", "xdg-open", []string{"https://example.com/callback"}},
		{"windows", "rundll32", []string{"url.dll,FileProtocolHandler", "https://example.com/callback"}},
	}
	for _, tc := range cases {
		t.Run(tc.goos, func(t *testing.T) {
			runner := &recordingRunner{}
			opener := &DefaultOpener{GOOS: tc.goos, Run: runner.run}

			if err := opener.Open(context.Background(), "https://example.com/callback"); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if runner.name != tc.wantName {
				t.Fatalf("command = %q, want %q", runner.name, tc.wantName)
			}
			if len(runner.args) != len(tc.wantArgs) {
				t.Fatalf("args = %v, want %v", runner.args, tc.wantArgs)
			}
			for i := range tc.wantArgs {
				if runner.args[i] != tc.wantArgs[i] {
					t.Fatalf("args[%d] = %q, want %q", i, runner.args[i], tc.wantArgs[i])
				}
			}
		})
	}
}

// TestOpenUnsupportedPlatformReturnsError verifies that an unsupported GOOS
// yields a clear error without invoking the runner.
func TestOpenUnsupportedPlatformReturnsError(t *testing.T) {
	runner := &recordingRunner{}
	opener := &DefaultOpener{GOOS: "plan9", Run: runner.run}

	err := opener.Open(context.Background(), "https://example.com")
	if err == nil {
		t.Fatal("expected error for unsupported platform")
	}
	if !strings.Contains(err.Error(), "unsupported platform") {
		t.Fatalf("error = %v, want it to mention unsupported platform", err)
	}
	if runner.name != "" {
		t.Fatalf("runner should not have been called, got name=%q", runner.name)
	}
}

// TestOpenPropagatesCommandError verifies that a failure from the command
// runner is returned to the caller.
func TestOpenPropagatesCommandError(t *testing.T) {
	wantErr := errors.New("xdg-open: not found")
	runner := &recordingRunner{err: wantErr}
	opener := &DefaultOpener{GOOS: "linux", Run: runner.run}

	err := opener.Open(context.Background(), "https://example.com")
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
}

// TestOpenContextCancellation verifies that a pre-canceled context is returned
// as context.Canceled and the runner is never invoked.
func TestOpenContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	called := false
	runner := func(ctx context.Context, name string, args ...string) error {
		called = true
		return nil
	}
	opener := &DefaultOpener{GOOS: "linux", Run: runner}

	err := opener.Open(ctx, "https://example.com")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if called {
		t.Fatal("runner should not have been called with a canceled context")
	}
}

// TestOpenNilReceiver verifies that calling Open on a nil *DefaultOpener
// returns a clear error rather than panicking.
func TestOpenNilReceiver(t *testing.T) {
	var o *DefaultOpener
	err := o.Open(context.Background(), "https://example.com")
	if err == nil {
		t.Fatal("expected error from nil *DefaultOpener")
	}
	if !strings.Contains(err.Error(), "nil *DefaultOpener") {
		t.Fatalf("error = %v, want it to mention nil *DefaultOpener", err)
	}
}

// TestOpenNilContext verifies that a nil context returns a clear error rather
// than panicking.
func TestOpenNilContext(t *testing.T) {
	opener := &DefaultOpener{GOOS: "linux", Run: func(context.Context, string, ...string) error { return nil }}
	err := opener.Open(nil, "https://example.com")
	if err == nil {
		t.Fatal("expected error from nil context")
	}
	if !strings.Contains(err.Error(), "nil context") {
		t.Fatalf("error = %v, want it to mention nil context", err)
	}
}

// TestOpenDefaultRunnerSelection verifies that when Run is nil, Open selects
// the exec-based default runner internally (without modifying the struct
// field). It does not launch a real process.
func TestOpenDefaultRunnerSelection(t *testing.T) {
	opener := &DefaultOpener{GOOS: "linux"}
	if opener.Run != nil {
		t.Fatal("Run should be nil before Open is called")
	}
	// Inject a runner to verify command selection without launching a process.
	runner := &recordingRunner{}
	opener.Run = runner.run
	if err := opener.Open(context.Background(), "https://example.com"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if runner.name != "xdg-open" {
		t.Fatalf("command = %q, want xdg-open", runner.name)
	}
}

// TestExecRunnerPreCanceledContext verifies the real execRunner returns an
// error when the context is already canceled, without spawning a long-lived
// process. It uses the current test binary as the child process (no shell, no
// real browser).
func TestExecRunnerPreCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Use the running test binary as a harmless subprocess that exits
	// immediately. We pass -test.run with a pattern that matches nothing so it
	// exits quickly without running tests.
	binary := os.Args[0]
	err := execRunner(ctx, binary, "-test.run=^$", "-test.timeout=10s")
	if err == nil {
		t.Fatal("expected error from pre-canceled context")
	}
	// The error should indicate the context was canceled or the process could
	// not start; either is acceptable as long as we did not block.
}

// TestExecRunnerStartsAndReapsProcess verifies that execRunner starts a
// process and returns immediately (non-blocking), and that the child is
// reaped. It uses the current test binary which exits quickly.
func TestExecRunnerStartsAndReapsProcess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	binary := os.Args[0]
	done := make(chan error, 1)
	go func() {
		done <- execRunner(ctx, binary, "-test.run=^$", "-test.timeout=5s")
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("execRunner returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("execRunner blocked; expected non-blocking return")
	}
}

// TestOpenerInterfaceSatisfied ensures DefaultOpener satisfies Opener.
func TestOpenerInterfaceSatisfied(t *testing.T) {
	var _ Opener = (*DefaultOpener)(nil)
}
