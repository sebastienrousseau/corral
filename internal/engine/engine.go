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
	OutputText   OutputFormat = "text"
	OutputJSON   OutputFormat = "json"
	OutputNDJSON OutputFormat = "ndjson"
)

// RunOptions contains all execution controls for a run.
type RunOptions struct {
	Owner       string
	BaseDir     string
	Concurrency int
	DryRun      bool
	Orphans     bool
	Protocol    string
	DoSync      bool
	Output      OutputFormat

	Fetch github.FetchOptions
	Clone git.CloneOptions
}

// Job encapsulates a repository to be processed along with its target directories.
type Job struct {
	Repo   github.Repo
	Target string
	Legacy string
}

// RepoResult represents the final status of processing a repository.
type RepoResult struct {
	RepoName    string `json:"repo"`
	Action      string `json:"action"`
	Message     string `json:"message"`
	Target      string `json:"target"`
	Visibility  string `json:"visibility"`
	Language    string `json:"language"`
	DryRun      bool   `json:"dry_run"`
	Protocol    string `json:"protocol"`
	ClonedURL   string `json:"clone_url,omitempty"`
	SyncAttempt bool   `json:"sync_attempt"`
}

// Summary tracks aggregate run outcomes.
type Summary struct {
	Total   int `json:"total"`
	Cloned  int `json:"cloned"`
	Synced  int `json:"synced"`
	Skipped int `json:"skipped"`
	Failed  int `json:"failed"`
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
	if l == "C#" {
		l = "CSharp"
	} else if l == "C++" {
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
			if err := os.MkdirAll(filepath.Dir(targetDir), 0o755); err != nil {
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
		if err := os.MkdirAll(filepath.Dir(targetDir), 0o755); err != nil {
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
	filepath.WalkDir(baseDir, func(path string, d fs.DirEntry, err error) error {
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
