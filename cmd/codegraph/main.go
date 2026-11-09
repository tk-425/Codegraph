package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/tk-425/Codegraph/internal/cli"
)

func main() {
	// Interrupts cancel the running command so it can clean up its servers.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := cli.Execute(ctx)
	stop()
	if err != nil {
		os.Exit(1)
	}
}
