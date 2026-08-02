// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sebastienrousseau/corral/internal/engine"
	gitutil "github.com/sebastienrousseau/corral/internal/git"
	"github.com/sebastienrousseau/corral/internal/github"
	corralmcp "github.com/sebastienrousseau/corral/internal/mcp"
	"github.com/spf13/cobra"
)

type configFile struct {
	Profiles map[string]profile `json:"profiles"`
}

type profile struct {
	Owners      []string `json:"owners"`
	BaseDir     string   `json:"base_dir,omitempty"`
	Layout      string   `json:"layout,omitempty"`
	Protocol    string   `json:"protocol,omitempty"`
	Concurrency int      `json:"concurrency,omitempty"`
	Limit       int      `json:"limit,omitempty"`
}

type localStatus struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Remote      string `json:"remote,omitempty"`
	Visibility  string `json:"visibility,omitempty"`
	Language    string `json:"language,omitempty"`
	Dirty       bool   `json:"dirty"`
	DirtyDetail string `json:"dirty_detail,omitempty"`
}

type pruneResult struct {
	Path    string `json:"path"`
	Action  string `json:"action"`
	Message string `json:"message,omitempty"`
}

var (
	configPath      string
	statusOutput    string
	planOutput      string
	pruneOutput     string
	opsFetchRepos   = github.FetchReposWithOptions
	removeAll       = os.RemoveAll
	localStateCheck = gitutil.HasUnpublishedWork
)

var statusCmd = &cobra.Command{
	Use:   "status [base_dir]",
	Short: "Report the state of organised local repositories",
	Args:  cobra.MaximumNArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		if statusOutput != "text" && statusOutput != "json" {
			return errors.New("--output must be text or json")
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		root := resolvedBaseDir(args)
		idx, err := corralmcp.Scan(root)
		if err != nil {
			return err
		}
		rows := make([]localStatus, 0, len(idx.Repos))
		for _, repo := range idx.Repos {
			dirty, detail := gitutil.HasLocalChanges(cmdContext(cmd), repo.Path)
			rows = append(rows, localStatus{
				Name: repo.Name, Path: repo.Path, Remote: repo.RemoteURL,
				Visibility: repo.Visibility, Language: repo.Language,
				Dirty: dirty, DirtyDetail: detail,
			})
		}
		if statusOutput == string(engine.OutputJSON) {
			return writeJSON(os.Stdout, rows)
		}
		for _, row := range rows {
			state := "clean"
			if row.Dirty {
				state = "dirty: " + row.DirtyDetail
			}
			fmt.Printf("%-32s %-8s %s\n", row.Name, state, row.Path)
		}
		return nil
	},
}

var planCmd = &cobra.Command{
	Use:   "plan <owner|topic:<topic>|language:<language>>",
	Short: "Preview repository reconciliation without changing disk state",
	Args:  cobra.ExactArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		if !validOperationalOutput(planOutput) {
			return errors.New("--output must be text, json, or ndjson")
		}
		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		engineRun(cmdContext(cmd), operationalRunOptions(args[0], true, engine.OutputFormat(planOutput)))
	},
}

var pruneCmd = &cobra.Command{
	Use:   "prune <owner>",
	Short: "Remove safe local clones that no longer exist upstream",
	Args:  cobra.ExactArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		if pruneOutput != "text" && pruneOutput != "json" {
			return errors.New("--output must be text or json")
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		if !dryRun && !assumeYes {
			return errors.New("prune requires --yes; use --dry-run to preview")
		}
		owner := strings.ToLower(strings.TrimSpace(args[0]))
		repos, err := opsFetchRepos(cmdContext(cmd), owner, github.FetchOptions{
			Limit: limit, AuthMode: github.AuthMode(authMode), Timeout: apiTimeout,
			RetryMax: retryMax, RetryMinBackoff: retryMinBackoff, RetryMaxBackoff: retryMaxBackoff,
			IncludeArchived: true, IncludeForks: true,
		})
		if err != nil {
			return err
		}
		upstream := make(map[string]struct{}, len(repos))
		for _, repo := range repos {
			identity := repo.FullName
			if identity == "" && repo.Owner != "" {
				identity = repo.Owner + "/" + repo.Name
			}
			if identity != "" {
				upstream[strings.ToLower("github.com/"+identity)] = struct{}{}
			}
		}
		idx, err := corralmcp.Scan(resolvedBaseDir(nil))
		if err != nil {
			return err
		}
		results := make([]pruneResult, 0)
		ownerPrefix := "github.com/" + owner + "/"
		for _, local := range idx.Repos {
			identity := gitutil.CanonicalRemote(local.RemoteURL)
			if !strings.HasPrefix(identity, ownerPrefix) {
				continue
			}
			if _, exists := upstream[identity]; exists {
				continue
			}
			unsafe, detail := localStateCheck(cmdContext(cmd), local.Path)
			if unsafe {
				results = append(results, pruneResult{Path: local.Path, Action: "REFUSE", Message: detail})
				continue
			}
			if dryRun {
				results = append(results, pruneResult{Path: local.Path, Action: "DRY-RUN", Message: "remove orphan"})
				continue
			}
			if err := removeAll(local.Path); err != nil {
				results = append(results, pruneResult{Path: local.Path, Action: "ERROR", Message: err.Error()})
				continue
			}
			results = append(results, pruneResult{Path: local.Path, Action: "DELETE"})
		}
		failed := false
		if pruneOutput == string(engine.OutputJSON) {
			if err := writeJSON(os.Stdout, results); err != nil {
				return err
			}
		} else {
			for _, result := range results {
				fmt.Printf("%-8s %s", result.Action, result.Path)
				if result.Message != "" {
					fmt.Printf(": %s", result.Message)
				}
				fmt.Println()
			}
		}
		for _, result := range results {
			failed = failed || result.Action == "ERROR" || result.Action == "REFUSE"
		}
		if failed {
			return errors.New("one or more repositories could not be pruned")
		}
		return nil
	},
}

var profileCmd = &cobra.Command{
	Use:   "profile <name>",
	Short: "Reconcile every owner in a configured profile",
	Args:  cobra.ExactArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		if !validOperationalOutput(output) {
			return errors.New("--output must be text, json, or ndjson")
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig(configPath)
		if err != nil {
			return err
		}
		selected, ok := cfg.Profiles[args[0]]
		if !ok {
			return fmt.Errorf("profile %q is not configured", args[0])
		}
		if err := validateProfile(args[0], selected); err != nil {
			return err
		}
		for _, owner := range selected.Owners {
			opts := operationalRunOptions(owner, dryRun, engine.OutputFormat(output))
			if selected.BaseDir != "" {
				opts.BaseDir = selected.BaseDir
			}
			if selected.Layout != "" {
				opts.Layout = selected.Layout
			}
			if selected.Protocol != "" {
				opts.Protocol = selected.Protocol
			}
			if selected.Concurrency > 0 {
				opts.Concurrency = selected.Concurrency
			}
			if selected.Limit > 0 {
				opts.Fetch.Limit = selected.Limit
			}
			engineRun(cmdContext(cmd), opts)
		}
		return nil
	},
}

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Validate and print the active profile configuration",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig(configPath)
		if err != nil {
			return err
		}
		for name, configured := range cfg.Profiles {
			if err := validateProfile(name, configured); err != nil {
				return err
			}
		}
		return writeJSON(os.Stdout, cfg)
	},
}

func operationalRunOptions(owner string, preview bool, format engine.OutputFormat) engine.RunOptions {
	return engine.RunOptions{
		Owner: owner, BaseDir: resolvedBaseDir(nil), Concurrency: concurrency,
		DryRun: preview, Orphans: orphans, Protocol: protocol, DoSync: !noSync,
		Output: format, Interactive: false, Layout: layout, FinderTags: finderTags, Version: Version,
		Fetch: github.FetchOptions{
			Limit: limit, Visibility: visibility, IncludeForks: includeForks,
			IncludeArchived: includeArchived, IncludeLanguages: parseCSV(includeLanguagesCSV),
			ExcludeLanguages: parseCSV(excludeLanguagesCSV), AuthMode: github.AuthMode(authMode),
			RetryMax: retryMax, RetryMinBackoff: retryMinBackoff,
			RetryMaxBackoff: retryMaxBackoff, Timeout: apiTimeout,
		},
		Clone: gitutil.CloneOptions{
			RecurseSubmodules: recurseSubmodules, SingleBranch: cloneSingleBranch,
			Blobless: cloneBlobless, Depth: cloneDepth,
		},
		Sync: engine.SyncOptions{Force: forceSync, IgnoreSubmoduleFailures: ignoreSubmoduleErrs},
	}
}

func resolvedBaseDir(args []string) string {
	if len(args) > 0 && args[0] != "" {
		return args[0]
	}
	if baseDir != "" {
		return baseDir
	}
	return defaultBaseDir()
}

func loadConfig(path string) (configFile, error) {
	if path == "" {
		path = defaultConfigPath()
	}
	f, err := os.Open(path) // #nosec G304 -- path is explicitly selected by the local user
	if err != nil {
		return configFile{}, fmt.Errorf("open config %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	var cfg configFile
	decoder := json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return configFile{}, fmt.Errorf("decode config %s: %w", path, err)
	}
	return cfg, nil
}

func defaultConfigPath() string {
	if configured := os.Getenv("XDG_CONFIG_HOME"); configured != "" {
		return filepath.Join(configured, "corral", "config.json")
	}
	home, err := userHomeDir()
	if err != nil {
		return filepath.Join(".", ".config", "corral", "config.json")
	}
	return filepath.Join(home, ".config", "corral", "config.json")
}

func validateProfile(name string, configured profile) error {
	if len(configured.Owners) == 0 {
		return fmt.Errorf("profile %q must contain at least one owner", name)
	}
	for _, owner := range configured.Owners {
		if strings.TrimSpace(owner) == "" {
			return fmt.Errorf("profile %q contains an empty owner", name)
		}
	}
	if configured.Protocol != "" && configured.Protocol != "https" && configured.Protocol != "ssh" {
		return fmt.Errorf("profile %q has invalid protocol %q", name, configured.Protocol)
	}
	if configured.Concurrency < 0 || configured.Limit < 0 {
		return fmt.Errorf("profile %q has negative concurrency or limit", name)
	}
	return nil
}

func writeJSON(target *os.File, value any) error {
	encoder := json.NewEncoder(target)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func validOperationalOutput(value string) bool {
	return value == string(engine.OutputText) || value == string(engine.OutputJSON) || value == string(engine.OutputNDJSON)
}

func init() {
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", "profile configuration file")
	statusCmd.Flags().StringVar(&statusOutput, "output", "text", "output format: text or json")
	planCmd.Flags().StringVar(&planOutput, "output", "json", "output format: text, json, or ndjson")
	pruneCmd.Flags().StringVar(&pruneOutput, "output", "text", "output format: text or json")
	pruneCmd.Flags().BoolVarP(&assumeYes, "yes", "y", false, "confirm removal of safe orphaned clones")
	profileCmd.Flags().StringVar(&output, "output", "text", "output format: text, json, or ndjson")
	rootCmd.AddCommand(statusCmd, planCmd, pruneCmd, profileCmd, configCmd)
}
