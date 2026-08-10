// Package browser provides a minimal, injectable interface for opening a URL
// in the user's default browser. It is used by interactive login flows; callers
// are expected to degrade gracefully (e.g. print the URL) when opening fails.
package browser

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
)

// Opener opens a URL in the user's default browser.
type Opener interface {
	// Open launches the default browser for url. It returns an error if the
	// browser cannot be launched; callers should degrade to printing the URL.
	Open(ctx context.Context, url string) error
}

// CommandRunner executes an external command. It is an injectable seam so
// tests never launch a real browser. The default runner uses exec.CommandContext.
type CommandRunner func(ctx context.Context, name string, args ...string) error

// DefaultOpener opens a URL using the platform-appropriate command. The URL is
// always passed as a separate argv element and never routed through a shell.
type DefaultOpener struct {
	// GOOS selects the command. If empty, runtime.GOOS is used.
	GOOS string
	// Run executes the command. If nil, the default exec-based runner is used.
	Run CommandRunner
}

// Open opens url in the default browser for the configured GOOS. It returns an
// error on unsupported platforms or if the command fails. The error never
// contains the URL beyond what the underlying exec error may include.
//
// Open is non-blocking: once the browser process is started it returns
// immediately and reaps the child process in a background goroutine. If ctx is
// already canceled, Open returns context.Canceled without invoking the runner.
func (o *DefaultOpener) Open(ctx context.Context, url string) error {
	if o == nil {
		return errors.New("browser: nil *DefaultOpener")
	}
	if ctx == nil {
		return errors.New("browser: nil context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	goos := o.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	run := o.Run
	if run == nil {
		run = execRunner
	}

	name, args, err := commandFor(goos, url)
	if err != nil {
		return err
	}
	return run(ctx, name, args...)
}

// commandFor returns the command name and arguments to open url on the given
// platform. The URL is always the last argv element.
func commandFor(goos, url string) (string, []string, error) {
	switch goos {
	case "darwin":
		return "open", []string{url}, nil
	case "linux":
		return "xdg-open", []string{url}, nil
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", url}, nil
	default:
		return "", nil, fmt.Errorf("browser: unsupported platform %q", goos)
	}
}

// execRunner is the default CommandRunner, using exec.CommandContext so that
// context cancellation propagates to the spawned process. It starts the
// process and returns immediately; a single background goroutine calls
// cmd.Wait to reap the child so it does not become a zombie.
func execRunner(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() {
		_ = cmd.Wait()
	}()
	return nil
}
