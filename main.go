// Package main is the entry point for the Corral CLI application.
package main

import (
	"github.com/sebastienrousseau/corral/cmd"
)

// main invokes the Cobra CLI execution.
func main() {
	cmd.Execute()
}
