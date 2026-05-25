// Package main is the entry point for the Corral CLI application.
package main

import (
	"context"
	"os/signal"
	"syscall"

	"github.com/sebastienrousseau/corral/cmd"
)

// main invokes the Cobra CLI execution.
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	cmd.ExecuteContext(ctx)
}
