// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

// Package cmd provides the command-line interface for the Corral application.
package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/sebastienrousseau/corral/internal/engine"
	"github.com/sebastienrousseau/corral/internal/git"
	"github.com/sebastienrousseau/corral/internal/github"
	"github.com/spf13/cobra"
)

// Version is the build version of Corral. It is overridden at release time via
// -ldflags "-X github.com/sebastienrousseau/corral/cmd.Version=<version>"
// (set by goreleaser) and by `make build` via `git describe`. The "dev"
// fallback makes an un-injected build obviously local rather than masquerading
// as a stale release tag.
var Version = "dev"

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
	forceSync           bool
	ignoreSubmoduleErrs bool
	layout              string
	interactive         bool
	finderTags          bool
	assumeYes           bool
	repoType            string
	repoSort            string
	retryMax            int
	retryMinBackoff     time.Duration
	retryMaxBackoff     time.Duration
	apiTimeout          time.Duration
	osExit              = os.Exit
	engineRun           = engine.Run
	preflightRunner     = runPreflight
)

var rootCmd = &cobra.Command{
	Use:   "corralctl <owner|topic:<topic>|language:<language>> [base_dir] [limit]",
	Short: "Automatically clone and organise GitHub repositories by owner, topic, or language.",
	Args:  validateRootArgs,
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
		if apiTimeout <= 0 {
			return fmt.Errorf("--api-timeout must be > 0")
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
		repoType = strings.ToLower(strings.TrimSpace(repoType))
		repoSort = strings.ToLower(strings.TrimSpace(repoSort))
		if repoType != "" && !slices.Contains(repoTypeValues, repoType) {
			return fmt.Errorf("--type must be one of: %s", strings.Join(repoTypeValues, ", "))
		}
		if repoSort != "" && !slices.Contains(repoSortValues, repoSort) {
			return fmt.Errorf("--sort must be one of: %s", strings.Join(repoSortValues, ", "))
		}
		// Validate the layout template before any network work so a typo
		// fails immediately instead of after a full paginated fetch.
		if layout != "" {
			if _, err := engine.ParseLayoutTemplate(layout); err != nil {
				return fmt.Errorf("--layout is not a valid template: %w", err)
			}
		}
		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		owner := args[0]
		filterType := strings.ToLower(strings.TrimSpace(repoType))
		filterSort := strings.ToLower(strings.TrimSpace(repoSort))
		bDir := baseDir
		lim := limit

		// The positional grammar is exactly what `Use` and the README
		// document: <owner> [base_dir] [limit]. Repository type and sort are
		// --type and --sort flags.
		//
		// Until v0.0.20 this parser also silently consumed args[1] as a
		// <type> and args[2] as a <sort> when they matched a keyword list,
		// which meant ten ordinary directory names — forks, stars, name,
		// public, private, templates and friends — were quietly swallowed and
		// the run fell back to $HOME/Code instead of the directory the user
		// named. validateRootArgs now rejects those instead of guessing.
		argIdx := 1
		if len(args) > argIdx {
			bDir = args[argIdx]
			argIdx++
		}
		if len(args) > argIdx {
			if _, err := fmt.Sscanf(args[argIdx], "%d", &lim); err != nil {
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

		// Preflight banner + confirm. Prints the parsed owner + resolved
		// base_dir so a `corral i sebastienrousseau`-style arg typo is
		// obvious BEFORE the network fetch. When the base_dir doesn't
		// already exist and stdin is a TTY, also prompts for a
		// confirmation; --yes bypasses it, --dry-run implies bypass.
		// Interactive TUI mode has its own confirmation via /exit and
		// doesn't need the extra prompt.
		if !interactive {
			proceed, err := preflightRunner(owner, bDir)
			if err != nil {
				// Refused (e.g. no TTY to confirm a brand-new target
				// directory). This is a failure, not a choice, so exit
				// non-zero: a script must be able to tell it did nothing.
				fmt.Fprintf(os.Stderr, "corralctl: %v\n", err)
				osExit(1)
				return
			}
			if !proceed {
				fmt.Fprintln(os.Stderr, "Aborted.")
				osExit(0)
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
			Interactive: interactive,
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
				Timeout:          apiTimeout,
				Type:             filterType,
				Sort:             filterSort,
			},
			Clone: git.CloneOptions{
				RecurseSubmodules: recurseSubmodules,
				SingleBranch:      cloneSingleBranch,
				Blobless:          cloneBlobless,
				Depth:             cloneDepth,
			},
			Sync: engine.SyncOptions{
				Force:                   forceSync,
				IgnoreSubmoduleFailures: ignoreSubmoduleErrs,
			},
			Layout:     layout,
			FinderTags: finderTags,
			Version:    Version,
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
	ExecuteContext(context.Background())
}

// ExecuteContext executes the root command with the provided context.
func ExecuteContext(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	rootCmd.SetContext(ctx)
	// Cobra prints the error and then the full usage block; this function then
	// printed the error a second time, so every mistake produced a ~52-line
	// wall with the message duplicated at both ends. Silence both and render
	// once here, with the actionable line last — clig.dev puts the most
	// important information at the end of the output.
	rootCmd.SilenceErrors = true
	rootCmd.SilenceUsage = true
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "corralctl: %v\n", err)
		fmt.Fprintf(os.Stderr, "\nRun 'corralctl --help' for usage.\n")
		osExit(1)
	}
}

// userHomeDir resolves the current user's home directory. It is indirected
// through a variable so tests can exercise the fallback path.
var userHomeDir = os.UserHomeDir

// defaultBaseDir returns the default root directory for cloned repositories,
// falling back to the current directory when the home directory cannot be
// determined.
func defaultBaseDir() string {
	home, err := userHomeDir()
	if err != nil {
		home = "." // fallback
	}
	return filepath.Join(home, "Code")
}

func init() {
	rootCmd.Version = Version

	rootCmd.PersistentFlags().StringVar(&baseDir, "base-dir", defaultBaseDir(), "root directory for cloned repos")
	rootCmd.Flags().IntVarP(&limit, "limit", "l", 1000, "max repos to list")
	rootCmd.Flags().IntVarP(&concurrency, "concurrency", "c", 1, "number of concurrent operations")
	rootCmd.PersistentFlags().BoolVarP(&dryRun, "dry-run", "n", false, "preview actions without making changes")
	rootCmd.Flags().BoolVarP(&orphans, "orphans", "o", false, "detect and list local repositories not on GitHub")
	rootCmd.Flags().StringVarP(&protocol, "protocol", "p", "https", "clone protocol (ssh or https)")
	rootCmd.Flags().BoolVar(&noSync, "no-sync", false, "skip pulling latest changes for existing repos")
	rootCmd.Flags().BoolVar(&recurseSubmodules, "recurse-submodules", false, "initialize submodules on clone and sync")
	rootCmd.Flags().StringVar(&output, "output", string(engine.OutputText), "output format: text, json, ndjson")
	rootCmd.Flags().StringVar(&authMode, "auth", string(github.AuthModeAuto), "authentication mode: auto, token, gh")
	rootCmd.Flags().StringVar(&visibility, "visibility", "all", "repository visibility filter: all, public, private")
	rootCmd.Flags().BoolVar(&includeForks, "include-forks", true, "include forked repositories under the Forks collection")
	rootCmd.Flags().BoolVar(&includeArchived, "include-archived", true, "include archived repositories and tag them On Hold")
	rootCmd.Flags().BoolVarP(&interactive, "interactive", "i", false, "display an interactive selector dashboard to pick repositories to clone/sync")
	rootCmd.Flags().BoolVar(&finderTags, "finder-tags", runtime.GOOS == "darwin", "apply managed macOS Finder Tags to repository folders")
	rootCmd.Flags().BoolVarP(&assumeYes, "yes", "y", false, "skip the preflight confirmation prompt when a new base directory would be created")
	rootCmd.Flags().StringVar(&includeLanguagesCSV, "languages", "", "comma-separated language allow list")
	rootCmd.Flags().StringVar(&excludeLanguagesCSV, "exclude-languages", "", "comma-separated language deny list")
	rootCmd.Flags().BoolVar(&cloneBlobless, "clone-blobless", false, "use partial clone filter=blob:none")
	rootCmd.Flags().BoolVar(&cloneSingleBranch, "clone-single-branch", false, "clone only the default branch")
	rootCmd.Flags().IntVar(&cloneDepth, "clone-depth", 0, "perform shallow clone with the given depth (0 disables)")
	rootCmd.Flags().BoolVar(&forceSync, "force-sync", false, "always run git pull, ignoring the cached pushed_at state")
	rootCmd.Flags().BoolVar(&ignoreSubmoduleErrs, "ignore-submodule-failures", false, "with --recurse-submodules, swallow submodule update failures so the parent repo still syncs")
	rootCmd.Flags().StringVar(&layout, "layout", "", "templated path structure for repositories (e.g. {{.Visibility}}/{{.Language}}/{{.Name}})")
	rootCmd.Flags().IntVar(&retryMax, "retry-max", 4, "max retries for transient GitHub API failures")
	rootCmd.Flags().DurationVar(&retryMinBackoff, "retry-min-backoff", 500*time.Millisecond, "minimum retry backoff")
	rootCmd.Flags().DurationVar(&retryMaxBackoff, "retry-max-backoff", 8*time.Second, "maximum retry backoff")
	rootCmd.Flags().DurationVar(&apiTimeout, "api-timeout", 30*time.Second, "GitHub API request deadline")
	rootCmd.Flags().StringVar(&repoType, "type", "", "repository type filter: "+strings.Join(repoTypeValues, ", "))
	rootCmd.Flags().StringVar(&repoSort, "sort", "", "repository sort order: "+strings.Join(repoSortValues, ", "))

	// Offer "did you mean" for near-miss subcommands. Cobra only reaches its
	// own suggestion path when the root is not runnable with arguments, which
	// corralctl is, so validateRootArgs does the work; this keeps the distance
	// consistent for the paths cobra does handle.
	rootCmd.SuggestionsMinimumDistance = 2
}

// repoTypeValues and repoSortValues are the accepted --type and --sort values.
// Both were previously matched positionally, which is what made ordinary
// directory names dangerous to pass as base_dir.
var (
	repoTypeValues = []string{
		"all", "public", "private", "sources", "forks", "archived",
		"can be sponsored", "sponsored", "mirrors", "templates",
	}
	repoSortValues = []string{"last updated", "updated", "name", "stars"}
)

// validateRootArgs replaces cobra.MinimumNArgs(1) on the root command.
//
// The root takes a bare <owner>, which means any typo'd subcommand is a
// syntactically valid invocation: `corralctl statuss` used to be read as
// "owner=statuss" and started a live GitHub fetch that cloned into
// $HOME/Code. This rejects an argument that is within edit distance 2 of a
// real subcommand and tells the user how to force it, which is clig.dev's
// "don't have a catch-all subcommand" applied to the one shape corralctl
// cannot avoid having.
func validateRootArgs(cmd *cobra.Command, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("requires at least 1 arg (the GitHub owner, topic:<topic>, or language:<language>)")
	}
	if len(args) > 3 {
		return fmt.Errorf("accepts at most 3 args (<owner> [base_dir] [limit]), received %d", len(args))
	}

	// `corralctl -- statuss` forces the owner reading.
	forced := cmd.ArgsLenAtDash() == 0
	if !forced {
		if suggestion := nearestSubcommand(cmd, args[0]); suggestion != "" {
			return fmt.Errorf("unknown command %q for %q\n\nDid you mean this?\n\t%s\n\n"+
				"If %q really is a GitHub owner, put any flags first and force it with:\n"+
				"\tcorralctl [flags] -- %s",
				args[0], cmd.Name(), suggestion, args[0], args[0])
		}
	}

	// A second positional is base_dir. Reject the old type/sort keywords
	// outright rather than guessing: before v0.0.20 they were swallowed as
	// filters and base_dir silently fell back to $HOME/Code.
	if len(args) > 1 {
		if kind, ok := legacyPositionalKeyword(args[1]); ok {
			return fmt.Errorf("%q is a --%s value, not a directory\n\n"+
				"Repository type and sort are now flags, so base_dir is never guessed:\n"+
				"\tcorralctl %s --%s %q\n\n"+
				"To use a directory of that name, qualify it:\n\tcorralctl %s ./%s",
				args[1], kind, args[0], kind, args[1], args[0], args[1])
		}
	}
	return nil
}

// nearestSubcommand returns the name of the subcommand closest to arg when arg
// looks like a misspelling of one, and "" otherwise. An exact match is not a
// typo — cobra dispatches those before Args ever runs.
func nearestSubcommand(cmd *cobra.Command, arg string) string {
	const maxDistance = 2
	arg = strings.ToLower(arg)
	best, bestDist := "", maxDistance+1
	for _, sub := range cmd.Commands() {
		if sub.Hidden {
			continue
		}
		for _, name := range append([]string{sub.Name()}, sub.Aliases...) {
			name = strings.ToLower(name)
			if name == arg {
				return "" // exact match; cobra would have dispatched it
			}
			if d := levenshtein(arg, name); d < bestDist {
				best, bestDist = name, d
			}
		}
	}
	if bestDist <= maxDistance {
		return best
	}
	return ""
}

// legacyPositionalKeyword reports whether s is one of the repository type or
// sort keywords the pre-v0.0.20 positional parser consumed.
func legacyPositionalKeyword(s string) (string, bool) {
	v := strings.ToLower(strings.TrimSpace(s))
	for _, t := range repoTypeValues {
		if v == t {
			return "type", true
		}
	}
	for _, t := range repoSortValues {
		if v == t {
			return "sort", true
		}
	}
	return "", false
}

// levenshtein is the standard edit distance, used only for "did you mean"
// suggestions over a handful of short subcommand names.
func levenshtein(a, b string) int {
	ar, br := []rune(a), []rune(b)
	prev := make([]int, len(br)+1)
	cur := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		cur[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, min(cur[j-1]+1, prev[j-1]+cost))
		}
		prev, cur = cur, prev
	}
	return prev[len(br)]
}
