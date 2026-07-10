// Command wm is the Wintermute CLI: a Go -> Erlang/OTP transpiler.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"go.muehmer.eu/wintermute/internal/pkg/cli"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return cli.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
}
