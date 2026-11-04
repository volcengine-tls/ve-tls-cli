package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/volcengine-tls/ve-tls-cli/internal/cli"
)

func main() {
	// Let the result-delivery path report EPIPE and the remote outcome on
	// stderr when the stdout consumer closes its pipe.
	signal.Ignore(syscall.SIGPIPE)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := cli.RunContext(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
