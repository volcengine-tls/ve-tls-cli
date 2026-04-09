package main

import (
	"os"

	"github.com/volcengine-tls/ve-tls-cli/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
