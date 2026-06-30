// Package engine provides the core concurrency and execution logic for Corral.
package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/template"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	// Interactive, when true, displays an interactive selector before processing.
	Interactive bool

	// Fetch holds the options passed to the GitHub repository listing call.
	Fetch github.FetchOptions
	// Clone holds the options passed to each Git clone operation.
	Clone git.CloneOptions
	// Sync controls when an already-cloned repository is actually pulled.
	Sync SyncOptions
	// Layout specifies the templated path structure for repositories.
	Layout string
	// Version is the build version of Corral.
	Version string
}

// SyncOptions configures the engine's per-repo sync decision. Kept separate
// from git.CloneOptions because forcing a sync is a corral-level policy
// choice, not a clone-time git flag.
type SyncOptions struct {
	// Force, when true, runs `git pull` even when the cached state shows
	// the upstream pushed_at is unchanged.
	Force bool
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
	gitRemoteOrigin  = git.RemoteOriginFromConfig
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

	// Allow git to authenticate HTTPS clones/pulls of private repositories using
	// the same credential resolved for the GitHub API.
	git.TokenProvider = func() string { return github.Token(ctx, opts.Fetch.AuthMode) }

	isTTY := isTerminal(os.Stdout.Fd())
	if !isTTY {
		log.SetOutput(os.Stdout)
	}

	var repos []github.Repo
	var err error

	if opts.Interactive {
		var ok bool
		tui.Version = opts.Version
		repos, ok, err = tui.RunSelector(ctx, opts.Owner, opts.Fetch, func() ([]github.Repo, error) {
			return fetchRepos(ctx, opts.Owner, opts.Fetch)
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
			osExit(1)
			return
		}
		if !ok {
			return
		}
		if len(repos) == 0 {
			if opts.Output == OutputText {
				fmt.Println("No repositories selected.")
			}
			return
		}
		if opts.Output == OutputText && isTTY {
			fmt.Print("\033[2J\033[H")
		}
	} else {
		if opts.Output == OutputText {
			if isTTY {
				if os.Getenv("CORRAL_SHOW_LOGO") != "0" {
					fmt.Print(tui.GetStyledLogo())
					fmt.Print(lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Render("   ⧇ Organising Repositories") + "\n")
					fmt.Print(lipgloss.NewStyle().Foreground(lipgloss.Color("238")).Render("   "+strings.Repeat("─", 58)) + "\n\n")
				}
				fmt.Println("Fetching repositories from GitHub...")
			} else {
				log.Println("Fetching repositories from GitHub...")
			}
		}
		repos, err = fetchRepos(ctx, opts.Owner, opts.Fetch)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
			osExit(1)
			return
		}
	}

	if opts.Fetch.Limit > 0 && len(repos) == opts.Fetch.Limit && opts.Output == OutputText {
		fmt.Printf("WARNING: Fetched exactly %d repositories. There may be more.\n", opts.Fetch.Limit)
	}

	// Pre-validate layout template if provided
	if opts.Layout != "" {
		if _, err := template.New("layout").Parse(opts.Layout); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: invalid layout template: %v\n", err)
			osExit(1)
			return
		}
	}

	if opts.Layout == "" || opts.Layout == "{{.Visibility}}/{{.Language}}/{{.Name}}" {
		migrateLegacy(opts.BaseDir, repos)
		normalizeLanguageDirCase(opts.BaseDir, repos)
	}

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
					msg := processRepo(ctx, opts.Owner, opts.Protocol, opts.DoSync, opts.DryRun, opts.Clone, opts.Sync, job)
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
		relPath, err := evaluateLayout(opts.Layout, repo, opts.Owner)
		if err != nil {
			log.Printf("WARN: failed to evaluate layout for %s: %v", repo.Name, err)
			continue
		}
		targetDir := filepath.Join(opts.BaseDir, relPath)
		legacyDir := filepath.Join(opts.BaseDir, normalizeLanguage(repo.Language), repo.Name)
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
				// p.Send writes to an unbuffered channel in bubbletea
				// v1.x and blocks when the program isn't draining (e.g.
				// when runProgram is stubbed out in tests, or after the
				// TUI has quit). The goroutine wrapper decouples the
				// consumer from the TUI's lifecycle so close(results)
				// can still unblock this loop even if a Send is hung.
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

	if opts.Layout == "" || opts.Layout == "{{.Visibility}}/{{.Language}}/{{.Name}}" {
		cleanupEmptyFolders(opts.BaseDir, repos)
	}

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

// normalizeLanguageDirCase renames any case-variant language subdirectory
// under each visibility directory to its lowercase form. On case-insensitive
// filesystems (APFS, HFS+, NTFS) a direct os.Rename("JavaScript","javascript")
// is a silent no-op, so the rename is performed via a temporary name. Only
// directories whose lowercased name matches a normalized language from the
// fetched repos are touched, so unrelated entries (e.g. "Configurations") are
// left alone.
func normalizeLanguageDirCase(baseDir string, repos []github.Repo) {
	languages := make(map[string]struct{}, len(repos))
	for _, r := range repos {
		languages[normalizeLanguage(r.Language)] = struct{}{}
	}
	if len(languages) == 0 {
		return
	}
	visEntries, err := os.ReadDir(baseDir)
	if err != nil {
		return
	}
	for _, ve := range visEntries {
		if !ve.IsDir() {
			continue
		}
		visDir := filepath.Join(baseDir, ve.Name())
		langEntries, err := os.ReadDir(visDir)
		if err != nil {
			continue
		}
		for _, le := range langEntries {
			if !le.IsDir() {
				continue
			}
			name := le.Name()
			lower := strings.ToLower(name)
			if name == lower {
				continue
			}
			if _, ok := languages[lower]; !ok {
				continue
			}
			src := filepath.Join(visDir, name)
			dst := filepath.Join(visDir, lower)
			tmp := filepath.Join(visDir, lower+".corral-rename-tmp")
			if err := os.Rename(src, tmp); err != nil {
				log.Printf("WARN: failed normalizing case for %s: %v", src, err)
				continue
			}
			if err := os.Rename(tmp, dst); err != nil {
				log.Printf("WARN: failed normalizing case for %s -> %s: %v", tmp, dst, err)
				_ = os.Rename(tmp, src) // best-effort revert
			}
		}
	}
}

// cleanupEmptyFolders removes the now-empty legacy top-level language
// directories left behind by migrateLegacy. It only targets directories whose
// names match a repository language, and os.Remove deletes a directory only
// when it is empty, so unrelated entries under baseDir (e.g. .claude, other
// projects) are never touched.
func cleanupEmptyFolders(baseDir string, repos []github.Repo) {
	seen := make(map[string]struct{})
	for _, repo := range repos {
		lang := normalizeLanguage(repo.Language)
		if _, ok := seen[lang]; ok {
			continue
		}
		seen[lang] = struct{}{}
		_ = os.Remove(filepath.Join(baseDir, lang)) // removes only when empty
	}
}

func processRepo(ctx context.Context, owner, protocol string, doSync, dryRun bool, cloneOpts git.CloneOptions, syncOpts SyncOptions, job Job) RepoResult {
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
			result.Message = fmt.Sprintf("failed creating target directory: %v", err)
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
			// Skip the network round-trip when the upstream pushed_at is
			// unchanged since the last successful sync. The cached value
			// lives in <targetDir>/.corral-state.json. A read error or a
			// zero state falls through to the original pull-always
			// behaviour, so a missing/corrupt sidecar can never cause a
			// stale working tree.
			if !syncOpts.Force && !repo.PushedAt.IsZero() {
				if st, err := readCloneState(targetDir); err == nil &&
					!st.LastSyncedPushedAt.IsZero() &&
					!repo.PushedAt.After(st.LastSyncedPushedAt) {
					result.Action = "SKIP"
					result.Message = "up-to-date (pushed_at unchanged)"
					return result
				}
			}
			err = gitPull(ctx, targetDir, cloneOpts.RecurseSubmodules)
			if err != nil {
				result.Action = "ERROR"
				result.Message = fmt.Sprintf("sync failed: %v", err)
				return result
			}
			stampCloneState(targetDir, repo)
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
		result.Message = fmt.Sprintf("clone failed: %v", err)
		return result
	}
	stampCloneState(targetDir, repo)
	result.Action = "CLONE"
	result.Message = "cloned successfully"
	return result
}

// stampCloneState records the upstream pushed_at in the per-clone state
// sidecar so the next run can skip a no-op git pull. Best-effort: a write
// failure is logged but does not fail the operation, since the sidecar is
// purely an optimization (a missing or stale file falls through to the
// pre-sidecar behaviour of always pulling).
func stampCloneState(targetDir string, repo github.Repo) {
	if err := writeCloneState(targetDir, cloneState{
		LastSyncedPushedAt: repo.PushedAt,
		LastSyncedAt:       time.Now().UTC(),
	}); err != nil {
		log.Printf("WARN: failed writing %s in %s: %v", StateFileName, targetDir, err)
	}
}

// repoNameFromURL extracts the repository name from a git remote URL, stripping
// any trailing ".git" suffix. It returns an empty string when no segment exists.
func repoNameFromURL(url string) string {
	url = strings.TrimSuffix(strings.TrimSpace(url), ".git")
	if i := strings.LastIndexAny(url, "/:"); i >= 0 {
		return url[i+1:]
	}
	return url
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
			url, err := gitRemoteOrigin(repoDir)
			if err == nil && (strings.Contains(url, "/"+owner+"/") || strings.Contains(url, ":"+owner+"/")) {
				// Match against both the directory name and the name encoded in
				// the remote URL, so a locally-renamed directory whose remote
				// still points at a known repository is not flagged as an orphan.
				if !repoMap[filepath.Base(repoDir)] && !repoMap[repoNameFromURL(url)] {
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

func evaluateLayout(layoutTpl string, repo github.Repo, owner string) (string, error) {
	if layoutTpl == "" {
		layoutTpl = "{{.Visibility}}/{{.Language}}/{{.Name}}"
	}
	tmpl, err := template.New("layout").Parse(layoutTpl)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	data := struct {
		Visibility    string
		Language      string
		Name          string
		Owner         string
		DefaultBranch string
	}{
		Visibility:    strings.ToLower(repo.Visibility),
		Language:      normalizeLanguage(repo.Language),
		Name:          repo.Name,
		Owner:         owner,
		DefaultBranch: repo.DefaultBranch,
	}
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	cleanPath := filepath.Clean(buf.String())
	if strings.HasPrefix(cleanPath, "..") || cleanPath == "." || filepath.IsAbs(cleanPath) || strings.HasPrefix(buf.String(), "/") || strings.HasPrefix(buf.String(), "\\") {
		return "", fmt.Errorf("layout escapes base directory: %s", cleanPath)
	}
	return filepath.ToSlash(cleanPath), nil
}
