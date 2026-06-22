// Package engine provides the core concurrency and execution logic for Corral.
package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"
	"github.com/sebastienrousseau/corral/internal/git"
	"github.com/sebastienrousseau/corral/internal/github"
	"github.com/sebastienrousseau/corral/internal/tui"
)

// OutputFormat controls how operation results are emitted.
type OutputFormat string

const (
	// OutputText emits human-readable, line-oriented progress output.
	OutputText OutputFormat = "text"
	// OutputJSON emits a single aggregated JSON document at the end of the run.
	OutputJSON OutputFormat = "json"
	// OutputNDJSON emits one JSON object per result as a stream of newline-delimited records.
	OutputNDJSON OutputFormat = "ndjson"
)

// RunOptions contains all execution controls for a run.
type RunOptions struct {
	// Owner is the GitHub user or organization whose repositories are processed.
	Owner string
	// BaseDir is the root directory under which repositories are laid out.
	BaseDir string
	// Concurrency is the number of worker goroutines processing repositories; must be >= 1.
	Concurrency int
	// DryRun, when true, reports intended actions without performing clone or pull operations.
	DryRun bool
	// Orphans, when true, enables detection of local repositories no longer present upstream.
	Orphans bool
	// Protocol selects the clone transport and must be either "https" or "ssh".
	Protocol string
	// DoSync, when true, pulls updates into existing repositories.
	DoSync bool
	// Output selects the result emission format (text, json, or ndjson).
	Output OutputFormat

	// Fetch holds the options passed to the GitHub repository listing call.
	Fetch github.FetchOptions
	// Clone holds the options passed to each Git clone operation.
	Clone git.CloneOptions
}

// Job encapsulates a repository to be processed along with its target directories.
type Job struct {
	// Repo is the GitHub repository to be processed.
	Repo github.Repo
	// Target is the destination directory for the repository under the new layout.
	Target string
	// Legacy is the directory where the repository may exist under the old layout.
	Legacy string
}

// RepoResult represents the final status of processing a repository.
type RepoResult struct {
	// RepoName is the name of the processed repository.
	RepoName string `json:"repo"`
	// Action is the outcome verb, such as CLONE, SYNC, SKIP, ERROR, or DRY-RUN.
	Action string `json:"action"`
	// Message is a human-readable description of the outcome.
	Message string `json:"message"`
	// Target is the destination directory for the repository.
	Target string `json:"target"`
	// Visibility is the repository visibility (e.g. Public or Private).
	Visibility string `json:"visibility"`
	// Language is the normalized primary language directory name.
	Language string `json:"language"`
	// DryRun indicates whether the run was performed in dry-run mode.
	DryRun bool `json:"dry_run"`
	// Protocol is the clone transport used (https or ssh).
	Protocol string `json:"protocol"`
	// ClonedURL is the URL used for cloning, if a clone was attempted.
	ClonedURL string `json:"clone_url,omitempty"`
	// SyncAttempt indicates whether a sync (pull) was attempted.
	SyncAttempt bool `json:"sync_attempt"`
}

// Summary tracks aggregate run outcomes.
type Summary struct {
	// Total is the number of repositories scheduled for processing.
	Total int `json:"total"`
	// Cloned is the number of repositories successfully cloned.
	Cloned int `json:"cloned"`
	// Synced is the number of repositories successfully synced.
	Synced int `json:"synced"`
	// Skipped is the number of repositories skipped.
	Skipped int `json:"skipped"`
	// Failed is the number of repositories that failed to process.
	Failed int `json:"failed"`
}

var (
	fetchRepos       = github.FetchReposWithOptions
	osExit           = os.Exit
	gitPull          = git.Pull
	gitClone         = git.Clone
	gitCurrentBranch = git.CurrentBranch
	gitRemoteOrigin  = git.RemoteOrigin
	isTerminal       = isatty.IsTerminal
	runProgram       = func(p *tea.Program) (tea.Model, error) { return p.Run() }
)

// Run executes the core Corral workflow, orchestrating GitHub API fetches,
// legacy layout migrations, concurrent Git operations, and orphaned repository detection.
func Run(ctx context.Context, opts RunOptions) {
	if ctx == nil {
		ctx = context.Background()
	}
	if opts.Concurrency < 1 {
		fmt.Fprintln(os.Stderr, "ERROR: concurrency must be >= 1")
		osExit(1)
		return
	}
	if opts.Fetch.Limit < 0 {
		fmt.Fprintln(os.Stderr, "ERROR: limit must be >= 0")
		osExit(1)
		return
	}
	if opts.Owner == "" {
		fmt.Fprintln(os.Stderr, "ERROR: owner must not be empty")
		osExit(1)
		return
	}
	if opts.BaseDir == "" {
		fmt.Fprintln(os.Stderr, "ERROR: base directory must not be empty")
		osExit(1)
		return
	}
	if opts.Protocol != "https" && opts.Protocol != "ssh" {
		fmt.Fprintln(os.Stderr, "ERROR: protocol must be either ssh or https")
		osExit(1)
		return
	}
	if opts.Output == "" {
		opts.Output = OutputText
	}

	isTTY := isTerminal(os.Stdout.Fd())
	if !isTTY {
		log.SetOutput(os.Stdout)
	}

	if opts.Output == OutputText {
		if isTTY {
			fmt.Println("Fetching repositories from GitHub...")
		} else {
			log.Println("Fetching repositories from GitHub...")
		}
	}

	repos, err := fetchRepos(ctx, opts.Owner, opts.Fetch)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		osExit(1)
		return
	}

	if opts.Fetch.Limit > 0 && len(repos) == opts.Fetch.Limit && opts.Output == OutputText {
		fmt.Printf("WARNING: Fetched exactly %d repositories. There may be more.\n", opts.Fetch.Limit)
	}

	migrateLegacy(opts.BaseDir, repos)

	jobs := make(chan Job, len(repos))
	results := make(chan RepoResult, len(repos))
	var wg sync.WaitGroup

	for i := 0; i < opts.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case job, ok := <-jobs:
					if !ok {
						return
					}
					msg := processRepo(ctx, opts.Owner, opts.Protocol, opts.DoSync, opts.DryRun, opts.Clone, job)
					select {
					case <-ctx.Done():
						return
					case results <- msg:
					}
				}
			}
		}()
	}

	scheduled := 0
enqueueLoop:
	for _, repo := range repos {
		langDir := normalizeLanguage(repo.Language)
		visDir := repo.Visibility
		targetDir := filepath.Join(opts.BaseDir, visDir, langDir, repo.Name)
		legacyDir := filepath.Join(opts.BaseDir, langDir, repo.Name)
		select {
		case <-ctx.Done():
			break enqueueLoop
		case jobs <- Job{Repo: repo, Target: targetDir, Legacy: legacyDir}:
			scheduled++
		}
	}
	close(jobs)

	var (
		allResults []RepoResult
		summary    Summary
	)

	summary.Total = scheduled
	var (
		consumerWG sync.WaitGroup
		p          *tea.Program
	)
	if opts.Output == OutputText && isTTY {
		p = tea.NewProgram(tui.NewModel(scheduled))
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)

	consumerWG.Add(1)
	go func() {
		defer consumerWG.Done()
		for msg := range results {
			allResults = append(allResults, msg)
			summary.add(msg)
			if p != nil {
				go p.Send(toLogMsg(msg))
				continue
			}
			if opts.Output == OutputText {
				icon := "✓"
				if msg.Action == "ERROR" || strings.HasPrefix(msg.Action, "FAIL") {
					icon = "✗"
				} else if msg.Action == "SKIP" {
					icon = "-"
				}
				log.Printf("%s [%s] %s: %s", icon, msg.Action, msg.RepoName, msg.Message)
				continue
			}
			if opts.Output == OutputNDJSON {
				if err := encoder.Encode(msg); err != nil {
					fmt.Fprintf(os.Stderr, "ERROR: failed to encode ndjson result: %v\n", err)
				}
			}
		}
	}()

	if p != nil {
		if _, err := runProgram(p); err != nil {
			fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
		}
	}

	wg.Wait()
	close(results)
	consumerWG.Wait()

	cleanupEmptyFolders(opts.BaseDir)

	if opts.Orphans {
		detectOrphans(opts.Owner, opts.BaseDir, repos)
	}

	if opts.Output == OutputJSON {
		payload := struct {
			Summary Summary      `json:"summary"`
			Repos   []RepoResult `json:"repos"`
		}{
			Summary: summary,
			Repos:   allResults,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: failed to encode json output: %v\n", err)
			osExit(1)
			return
		}
	}
}

func (s *Summary) add(msg RepoResult) {
	switch msg.Action {
	case "CLONE":
		s.Cloned++
	case "SYNC":
		s.Synced++
	case "SKIP":
		s.Skipped++
	case "ERROR":
		s.Failed++
	}
}

func toLogMsg(msg RepoResult) tui.LogMsg {
	return tui.LogMsg{RepoName: msg.RepoName, Action: msg.Action, Message: msg.Message}
}

func normalizeLanguage(lang string) string {
	if lang == "" {
		return "other"
	}
	l := lang
	switch l {
	case "C#":
		l = "CSharp"
	case "C++":
		l = "Cpp"
	}
	l = strings.ReplaceAll(l, " ", "_")
	l = strings.ReplaceAll(l, "/", "_")
	return strings.ToLower(l)
}

func migrateLegacy(baseDir string, repos []github.Repo) {
	for _, repo := range repos {
		langDir := normalizeLanguage(repo.Language)
		visDir := repo.Visibility
		legacyDir := filepath.Join(baseDir, langDir, repo.Name)
		targetDir := filepath.Join(baseDir, visDir, langDir, repo.Name)

		if info, err := os.Stat(legacyDir); err == nil && info.IsDir() {
			if err := os.MkdirAll(filepath.Dir(targetDir), 0o750); err != nil {
				log.Printf("WARN: failed creating target parent for migration %s: %v", targetDir, err)
				continue
			}
			if err := os.Rename(legacyDir, targetDir); err != nil {
				log.Printf("WARN: failed migrating %s to %s: %v", legacyDir, targetDir, err)
			}
		}
	}
}

func cleanupEmptyFolders(baseDir string) {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() && entry.Name() != "Public" && entry.Name() != "Private" {
			path := filepath.Join(baseDir, entry.Name())
			if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, fs.ErrPermission) {
				log.Printf("WARN: failed to remove legacy folder %s: %v", path, err)
			}
		}
	}
}

func processRepo(ctx context.Context, owner, protocol string, doSync, dryRun bool, cloneOpts git.CloneOptions, job Job) RepoResult {
	repo := job.Repo
	targetDir := job.Target
	result := RepoResult{
		RepoName:   repo.Name,
		Target:     targetDir,
		Visibility: repo.Visibility,
		Language:   normalizeLanguage(repo.Language),
		DryRun:     dryRun,
		Protocol:   protocol,
	}
	if err := ctx.Err(); err != nil {
		result.Action = "ERROR"
		result.Message = "operation canceled"
		return result
	}

	if !dryRun {
		if err := os.MkdirAll(filepath.Dir(targetDir), 0o750); err != nil {
			result.Action = "ERROR"
			result.Message = "failed creating target directory"
			return result
		}
	}

	gitDir := filepath.Join(targetDir, ".git")
	if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
		if doSync {
			result.SyncAttempt = true
			if dryRun {
				result.Action = "DRY-RUN"
				result.Message = "git pull"
				return result
			}
			branch, err := gitCurrentBranch(targetDir)
			if err == nil && branch != repo.DefaultBranch {
				result.Action = "SKIP"
				result.Message = fmt.Sprintf("on branch %s", branch)
				return result
			}
			err = gitPull(ctx, targetDir, cloneOpts.RecurseSubmodules)
			if err != nil {
				result.Action = "ERROR"
				result.Message = "sync failed"
				return result
			}
			result.Action = "SYNC"
			result.Message = "synced successfully"
			return result
		}
		result.Action = "SKIP"
		result.Message = "already exists"
		return result
	} else if info, err := os.Stat(targetDir); err == nil && info.IsDir() {
		result.Action = "SKIP"
		result.Message = "exists but is not a git repo"
		return result
	}

	url := repo.CloneURL
	if protocol == "ssh" && repo.SSHURL != "" {
		url = repo.SSHURL
	} else if protocol == "ssh" {
		url = fmt.Sprintf("git@github.com:%s/%s.git", owner, repo.Name)
	}
	result.ClonedURL = url

	if dryRun {
		result.Action = "DRY-RUN"
		result.Message = "git clone"
		return result
	}

	err := gitClone(ctx, url, targetDir, cloneOpts)
	if err != nil {
		result.Action = "ERROR"
		result.Message = "clone failed"
		return result
	}
	result.Action = "CLONE"
	result.Message = "cloned successfully"
	return result
}

func detectOrphans(owner, baseDir string, repos []github.Repo) {
	fmt.Println("\n--- Orphan Detection ---")
	repoMap := make(map[string]bool)
	for _, r := range repos {
		repoMap[r.Name] = true
	}

	var orphans []string
	// Per-entry walk errors are deliberately ignored inside the callback; the
	// outer error only signals an unreadable base directory, which is non-fatal
	// for best-effort orphan detection.
	_ = filepath.WalkDir(baseDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && d.Name() == ".git" {
			repoDir := filepath.Dir(path)
			repoName := filepath.Base(repoDir)
			url, err := gitRemoteOrigin(repoDir)
			if err == nil && (strings.Contains(url, "/"+owner+"/") || strings.Contains(url, ":"+owner+"/")) {
				if !repoMap[repoName] {
					orphans = append(orphans, repoDir)
				}
			}
			return filepath.SkipDir
		}
		return nil
	})

	for _, orphan := range orphans {
		fmt.Printf("Orphan found: %s\n", orphan)
	}
	if len(orphans) == 0 {
		fmt.Printf("No orphaned repositories found for %s.\n", owner)
	}
}
