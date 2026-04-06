// Package cmd provides the command-line interface for the Corral application.
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/sebastienrousseau/corral/internal/engine"
	"github.com/spf13/cobra"
)

var (
	baseDir           string
	concurrency       int
	dryRun            bool
	orphans           bool
	protocol          string
	noSync            bool
	recurseSubmodules bool
	limit             int
	osExit            = os.Exit
	engineRun         = engine.Run
)

var rootCmd = &cobra.Command{
	Use:   "corral <owner> [base_dir] [limit]",
	Short: "Automatically clone and organise GitHub repositories by visibility and language.",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		owner := args[0]
		bDir := baseDir
		if len(args) > 1 {
			bDir = args[1]
		}
		lim := limit
		if len(args) > 2 {
			if _, err := fmt.Sscanf(args[2], "%d", &lim); err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: limit must be a valid integer\n")
				osExit(1)
				return
			}
		}
		engineRun(owner, bDir, lim, concurrency, dryRun, orphans, protocol, !noSync, recurseSubmodules)
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		osExit(1)
	}
}

func init() {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "." // fallback
	}
	defaultBaseDir := filepath.Join(home, "Code")
	rootCmd.Flags().StringVar(&baseDir, "base-dir", defaultBaseDir, "root directory for cloned repos")
	rootCmd.Flags().IntVarP(&limit, "limit", "l", 1000, "max repos to list")
	rootCmd.Flags().IntVarP(&concurrency, "concurrency", "c", 1, "Number of concurrent operations")
	rootCmd.Flags().BoolVarP(&dryRun, "dry-run", "n", false, "Preview actions without making changes")
	rootCmd.Flags().BoolVarP(&orphans, "orphans", "o", false, "Detect and list local repositories not on GitHub")
	rootCmd.Flags().StringVarP(&protocol, "protocol", "p", "https", "Clone protocol (ssh or https)")
	rootCmd.Flags().BoolVar(&noSync, "no-sync", false, "Skip pulling latest changes for existing repos")
	rootCmd.Flags().BoolVar(&recurseSubmodules, "recurse-submodules", false, "Initialize submodules on clone and sync")
}
