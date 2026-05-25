// Package cmd provides the command-line interface for the Corral application.
package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sebastienrousseau/corral/internal/engine"
	"github.com/sebastienrousseau/corral/internal/git"
	"github.com/sebastienrousseau/corral/internal/github"
	"github.com/spf13/cobra"
)

var (
	baseDir             string
	concurrency         int
	dryRun              bool
	orphans             bool
	protocol            string
	noSync              bool
	recurseSubmodules   bool
	limit               int
	output              string
	authMode            string
	visibility          string
	includeForks        bool
	includeArchived     bool
	includeLanguagesCSV string
	excludeLanguagesCSV string
	cloneBlobless       bool
	cloneSingleBranch   bool
	cloneDepth          int
	retryMax            int
	retryMinBackoff     time.Duration
	retryMaxBackoff     time.Duration
	osExit              = os.Exit
	engineRun           = engine.Run
)

var rootCmd = &cobra.Command{
	Use:   "corral <owner> [base_dir] [limit]",
	Short: "Automatically clone and organise GitHub repositories by visibility and language.",
	Args:  cobra.MinimumNArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		protocol = strings.ToLower(strings.TrimSpace(protocol))
		output = strings.ToLower(strings.TrimSpace(output))
		authMode = strings.ToLower(strings.TrimSpace(authMode))
		visibility = strings.ToLower(strings.TrimSpace(visibility))

		if concurrency < 1 {
			return fmt.Errorf("--concurrency must be >= 1")
		}
		if limit < 0 {
			return fmt.Errorf("--limit must be >= 0")
		}
		if cloneDepth < 0 {
			return fmt.Errorf("--clone-depth must be >= 0")
		}
		if retryMax < 0 {
			return fmt.Errorf("--retry-max must be >= 0")
		}
		if retryMinBackoff <= 0 {
			return fmt.Errorf("--retry-min-backoff must be > 0")
		}
		if retryMaxBackoff <= 0 {
			return fmt.Errorf("--retry-max-backoff must be > 0")
		}
		if retryMaxBackoff < retryMinBackoff {
			return fmt.Errorf("--retry-max-backoff must be >= --retry-min-backoff")
		}
		if protocol != "https" && protocol != "ssh" {
			return fmt.Errorf("--protocol must be either ssh or https")
		}
		if output != string(engine.OutputText) && output != string(engine.OutputJSON) && output != string(engine.OutputNDJSON) {
			return fmt.Errorf("--output must be one of: text, json, ndjson")
		}
		if authMode != string(github.AuthModeAuto) && authMode != string(github.AuthModeToken) && authMode != string(github.AuthModeGH) {
			return fmt.Errorf("--auth must be one of: auto, token, gh")
		}
		if visibility != "all" && visibility != "public" && visibility != "private" {
			return fmt.Errorf("--visibility must be one of: all, public, private")
		}
		return nil
	},
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
			if lim < 0 {
				fmt.Fprintf(os.Stderr, "ERROR: limit must be >= 0\n")
				osExit(1)
				return
			}
		}

		engineRun(cmdContext(cmd), engine.RunOptions{
			Owner:       owner,
			BaseDir:     bDir,
			Concurrency: concurrency,
			DryRun:      dryRun,
			Orphans:     orphans,
			Protocol:    protocol,
			DoSync:      !noSync,
			Output:      engine.OutputFormat(output),
			Fetch: github.FetchOptions{
				Limit:            lim,
				Visibility:       visibility,
				IncludeForks:     includeForks,
				IncludeArchived:  includeArchived,
				IncludeLanguages: parseCSV(includeLanguagesCSV),
				ExcludeLanguages: parseCSV(excludeLanguagesCSV),
				AuthMode:         github.AuthMode(authMode),
				RetryMax:         retryMax,
				RetryMinBackoff:  retryMinBackoff,
				RetryMaxBackoff:  retryMaxBackoff,
			},
			Clone: git.CloneOptions{
				RecurseSubmodules: recurseSubmodules,
				SingleBranch:      cloneSingleBranch,
				Blobless:          cloneBlobless,
				Depth:             cloneDepth,
			},
		})
	},
}

func parseCSV(csv string) []string {
	if strings.TrimSpace(csv) == "" {
		return nil
	}
	parts := strings.Split(csv, ",")
	values := make([]string, 0, len(parts))
	for _, p := range parts {
		v := strings.TrimSpace(p)
		if v == "" {
			continue
		}
		values = append(values, v)
	}
	return values
}

func cmdContext(cmd *cobra.Command) context.Context {
	if cmd == nil {
		return context.Background()
	}
	if ctx := cmd.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
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
	rootCmd.Flags().IntVarP(&concurrency, "concurrency", "c", 1, "number of concurrent operations")
	rootCmd.Flags().BoolVarP(&dryRun, "dry-run", "n", false, "preview actions without making changes")
	rootCmd.Flags().BoolVarP(&orphans, "orphans", "o", false, "detect and list local repositories not on GitHub")
	rootCmd.Flags().StringVarP(&protocol, "protocol", "p", "https", "clone protocol (ssh or https)")
	rootCmd.Flags().BoolVar(&noSync, "no-sync", false, "skip pulling latest changes for existing repos")
	rootCmd.Flags().BoolVar(&recurseSubmodules, "recurse-submodules", false, "initialize submodules on clone and sync")
	rootCmd.Flags().StringVar(&output, "output", string(engine.OutputText), "output format: text, json, ndjson")
	rootCmd.Flags().StringVar(&authMode, "auth", string(github.AuthModeAuto), "authentication mode: auto, token, gh")
	rootCmd.Flags().StringVar(&visibility, "visibility", "all", "repository visibility filter: all, public, private")
	rootCmd.Flags().BoolVar(&includeForks, "include-forks", false, "include forked repositories")
	rootCmd.Flags().BoolVar(&includeArchived, "include-archived", false, "include archived repositories")
	rootCmd.Flags().StringVar(&includeLanguagesCSV, "languages", "", "comma-separated language allow list")
	rootCmd.Flags().StringVar(&excludeLanguagesCSV, "exclude-languages", "", "comma-separated language deny list")
	rootCmd.Flags().BoolVar(&cloneBlobless, "clone-blobless", false, "use partial clone filter=blob:none")
	rootCmd.Flags().BoolVar(&cloneSingleBranch, "clone-single-branch", false, "clone only the default branch")
	rootCmd.Flags().IntVar(&cloneDepth, "clone-depth", 0, "perform shallow clone with the given depth (0 disables)")
	rootCmd.Flags().IntVar(&retryMax, "retry-max", 4, "max retries for transient GitHub API failures")
	rootCmd.Flags().DurationVar(&retryMinBackoff, "retry-min-backoff", 500*time.Millisecond, "minimum retry backoff")
	rootCmd.Flags().DurationVar(&retryMaxBackoff, "retry-max-backoff", 8*time.Second, "maximum retry backoff")
}
