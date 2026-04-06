package cmd

import (
	"fmt"
	"os"

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
			fmt.Sscanf(args[2], "%d", &lim)
		}
		engine.Run(owner, bDir, lim, concurrency, dryRun, orphans, protocol, !noSync, recurseSubmodules)
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	defaultBaseDir := os.Getenv("HOME") + "/Code"
	rootCmd.Flags().StringVar(&baseDir, "base-dir", defaultBaseDir, "root directory for cloned repos")
	rootCmd.Flags().IntVarP(&limit, "limit", "l", 1000, "max repos to list")
	rootCmd.Flags().IntVarP(&concurrency, "concurrency", "c", 1, "Number of concurrent operations")
	rootCmd.Flags().BoolVarP(&dryRun, "dry-run", "n", false, "Preview actions without making changes")
	rootCmd.Flags().BoolVarP(&orphans, "orphans", "o", false, "Detect and list local repositories not on GitHub")
	rootCmd.Flags().StringVarP(&protocol, "protocol", "p", "https", "Clone protocol (ssh or https)")
	rootCmd.Flags().BoolVar(&noSync, "no-sync", false, "Skip pulling latest changes for existing repos")
	rootCmd.Flags().BoolVar(&recurseSubmodules, "recurse-submodules", false, "Initialize submodules on clone and sync")
}
