// Package main is the entry point for the Corral CLI application.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/sebastienrousseau/corral/cmd"
	"github.com/sebastienrousseau/corral/internal/git"
)

// main invokes the Cobra CLI execution.
func main() {
	if err := git.ResolveGitBinary(); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	cmd.ExecuteContext(ctx)
}
