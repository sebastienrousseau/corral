// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

// Package engine provides the core concurrency and execution logic for Corral.
package engine

import (
	"bytes"
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
	// Layout specifies the templated path structure for repositories. Custom
	// layouts may use Collection and Bucket in addition to the legacy fields.
	Layout string
	// FinderTags enables managed macOS Finder metadata on repository folders.
	FinderTags bool
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
	// IgnoreSubmoduleFailures, when true with Clone.RecurseSubmodules, allows
	// the parent repository to update even when a submodule sync fails (e.g.
	// the submodule repo was deleted upstream or its access revoked). The
	// failure is logged as a WARN but not propagated.
	IgnoreSubmoduleFailures bool
}

// Job encapsulates a repository to be processed along with its target directories.
type Job struct {
	// Repo is the GitHub repository to be processed.
	Repo github.Repo
	// Target is the destination directory for the repository under the new layout.
	Target string
	// Legacy is the directory where the repository may exist under the old layout.
	Legacy string
	// Existing is an identity-matched clone at a previous layout path.
	Existing string
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
	// Moved indicates that an identity-matched clone was relocated.
	Moved bool `json:"moved,omitempty"`
}

// Summary tracks aggregate run outcomes.
type Summary struct {
	// Total is the number of repositories scheduled for processing.
	Total int `json:"total"`
	// Cloned is the number of repositories successfully cloned.
	Cloned int `json:"cloned"`
	// Synced is the number of repositories successfully synced.
	Synced int `json:"synced"`
	// Moved is the number of repositories relocated to their desired layout.
	Moved int `json:"moved"`
	// Skipped is the number of repositories skipped.
	Skipped int `json:"skipped"`
	// Failed is the number of repositories that failed to process.
	Failed int `json:"failed"`
	// Canceled is true when the run was interrupted by ctx cancellation
	// (typically SIGINT/SIGTERM). The result set in that case is partial:
	// a scripted consumer reading json/ndjson output should treat it as
	// "not all repositories were processed" rather than a clean run.
	Canceled bool `json:"canceled,omitempty"`
}

// cancelExitCode is the POSIX convention for "killed by SIGINT" (128 + 2).
// Used when a non-interactive corralctl run is interrupted, so scripted
// callers can distinguish cancellation from other failures.
const cancelExitCode = 130

const (
	defaultLayout          = "{{.Collection}}/{{.Bucket}}/{{.Name}}"
	searchLayout           = "{{.Owner}}/{{.Collection}}/{{.Bucket}}/{{.Name}}"
	maxLayoutTemplateBytes = 4096
)

var (
	fetchRepos       = github.FetchReposWithOptions
	osExit           = os.Exit
	gitPull          = git.Pull
	gitClone         = git.Clone
	gitCurrentBranch = git.CurrentBranch
	gitIsEmpty       = git.IsEmpty
	gitRemoteOrigin  = git.RemoteOriginFromConfig
	isTerminal       = isatty.IsTerminal
	runProgram       = func(p *tea.Program) (tea.Model, error) { return p.Run() }
	runSelector      = tui.RunSelector
	walkDir          = filepath.WalkDir
	readDir          = os.ReadDir
	statPath         = os.Stat
	sameFile         = os.SameFile
	mkdirAll         = os.MkdirAll
	renamePath       = os.Rename
	applyTags        = applyFinderTags
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
	var tokenOnce sync.Once
	var token string
	git.TokenProvider = func() string {
		tokenOnce.Do(func() { token = github.Token(ctx, opts.Fetch.AuthMode) })
		return token
	}

	isTTY := isTerminal(os.Stdout.Fd())
	// stdout is reserved for the selected output format. Diagnostics always go
	// to stderr so JSON and NDJSON remain parseable.
	log.SetOutput(os.Stderr)

	var repos []github.Repo
	var err error

	if opts.Interactive {
		var ok bool
		tui.Version = opts.Version
		repos, ok, err = runSelector(ctx, opts.Owner, opts.Fetch, func() ([]github.Repo, error) {
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

	layoutTemplate, err := parseLayoutTemplate(effectiveLayout(opts, github.Repo{}))
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: invalid layout template: %v\n", err)
		osExit(1)
		return
	}

	if usesClassicLayout(opts) && !opts.DryRun {
		if err := ensureAppleCollections(opts.BaseDir); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: failed creating repository collections: %v\n", err)
			osExit(1)
			return
		}
		migrateLegacy(opts.BaseDir, repos)
		normalizeLayoutDirCase(opts.BaseDir, repos)
	}
	existingByRemote := discoverExistingRepos(opts.BaseDir)

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
					applyJobFinderTags(opts, job, msg)
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
		existingPaths := existingByRemote[repoRemoteIdentity(repo)]
		relPath, err := executeLayout(layoutTemplate, repo, opts.Owner)
		if err != nil {
			scheduled++
			results <- RepoResult{
				RepoName: repo.Name, Action: "ERROR",
				Message:    fmt.Sprintf("failed to evaluate layout: %v", err),
				Visibility: repo.Visibility, Language: normalizeLanguage(repo.Language),
				DryRun: opts.DryRun, Protocol: opts.Protocol,
			}
			continue
		}
		targetDir := filepath.Join(opts.BaseDir, relPath)
		if len(existingPaths) > 1 {
			scheduled++
			results <- RepoResult{
				RepoName: repo.Name, Action: "ERROR", Target: targetDir,
				Message:    "duplicate clone locations: " + strings.Join(existingPaths, ", "),
				Visibility: repo.Visibility, Language: normalizeLanguage(repo.Language),
				DryRun: opts.DryRun, Protocol: opts.Protocol,
			}
			continue
		}
		legacyDir := filepath.Join(opts.BaseDir, normalizeLanguage(repo.Language), repo.Name)
		select {
		case <-ctx.Done():
			break enqueueLoop
		case jobs <- Job{Repo: repo, Target: targetDir, Legacy: legacyDir, Existing: firstPath(existingPaths)}:
			scheduled++
		}
	}
	close(jobs)

	var (
		allResults []RepoResult
		summary    Summary
		outputErr  error
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
				p.Send(toLogMsg(msg))
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
					if outputErr == nil {
						outputErr = err
					}
				}
			}
		}
	}()

	if p != nil {
		if _, err := runProgram(p); err != nil {
			fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
		}
		// Ensure Send unblocks if an injected runner or an early TUI failure
		// returns without shutting down Bubble Tea's context.
		p.Kill()
	}

	wg.Wait()
	close(results)
	consumerWG.Wait()

	// Detect cancellation after workers + consumer drain. ctx.Err() at this
	// point is the authoritative signal: it's set if and only if a SIGINT/
	// SIGTERM (or a parent's cancel()) fired during the run. The result set
	// in that case is partial and we must signal that to downstream consumers
	// so they don't mistake an aborted run for a clean one.
	if err := ctx.Err(); err != nil {
		summary.Canceled = true
		emitCancellation(opts.Output, isTTY, encoder, err)
	}

	if usesClassicLayout(opts) && !opts.DryRun {
		cleanupEmptyFolders(opts.BaseDir, repos)
	}

	var orphanPaths []string
	if opts.Orphans && !summary.Canceled {
		// Skip orphan detection on cancel: the local tree may be mid-clone
		// and the report would be misleading.
		orphanPaths = findOrphans(opts.Owner, opts.BaseDir, repos)
		switch opts.Output {
		case OutputText:
			emitOrphans(opts.Owner, orphanPaths)
		case OutputNDJSON:
			for _, orphan := range orphanPaths {
				if err := encoder.Encode(RepoResult{Action: "ORPHAN", Target: orphan, Message: "local repository is absent upstream"}); err != nil {
					fmt.Fprintf(os.Stderr, "ERROR: failed to encode orphan result: %v\n", err)
					if outputErr == nil {
						outputErr = err
					}
				}
			}
		}
	}

	if opts.Output == OutputJSON {
		payload := struct {
			Summary Summary      `json:"summary"`
			Repos   []RepoResult `json:"repos"`
			Orphans []string     `json:"orphans,omitempty"`
		}{
			Summary: summary,
			Repos:   allResults,
			Orphans: orphanPaths,
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

	if summary.Canceled {
		// POSIX 130 = killed by SIGINT. Lets scripted callers distinguish
		// cancellation from other failure modes (which exit 1).
		osExit(cancelExitCode)
		return
	}
	if outputErr != nil || summary.Failed > 0 {
		osExit(1)
	}
}

func ensureAppleCollections(baseDir string) error {
	for _, collection := range []string{"Public", "Private", "Work", "Forks"} {
		if err := mkdirAll(filepath.Join(baseDir, collection), 0o750); err != nil {
			return err
		}
	}
	return nil
}

func applyJobFinderTags(opts RunOptions, job Job, result RepoResult) {
	if !opts.FinderTags || opts.DryRun {
		return
	}
	tagPath := result.Target
	if result.Action == "ERROR" || strings.HasPrefix(result.Action, "FAIL") {
		tagPath = job.Existing
	}
	if tagPath == "" {
		return
	}
	if err := applyTags(tagPath, job.Repo, result); err != nil {
		log.Printf("WARN: failed applying Finder tags to %s: %v", tagPath, err)
	}
}

// emitCancellation writes a final cancellation marker to the active output
// channel so non-interactive consumers learn the run was interrupted. The
// interactive TUI path (text + TTY) stays silent — the TUI already redraws
// on SIGINT and the user knows they pressed Ctrl-C.
func emitCancellation(output OutputFormat, isTTY bool, encoder *json.Encoder, cause error) {
	msg := fmt.Sprintf("operation canceled (%v)", cause)
	switch output {
	case OutputNDJSON:
		// Terminal record so a consumer piping into jq sees the cancel.
		// Action: "CANCELED" matches the verb scheme used for other actions.
		final := RepoResult{Action: "CANCELED", Message: msg}
		if err := encoder.Encode(final); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: failed to encode ndjson cancellation: %v\n", err)
		}
	case OutputJSON:
		// The aggregated json payload (written below in Run) already carries
		// summary.Canceled=true; nothing to emit here.
	case OutputText:
		if !isTTY {
			// Non-TTY text path: scripted log consumers get a single line.
			// TTY users get nothing — the TUI handles its own signal display.
			log.Printf("%s", msg)
		}
	}
}

func (s *Summary) add(msg RepoResult) {
	if msg.Moved {
		s.Moved++
	}
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

func usesClassicLayout(opts RunOptions) bool {
	return (opts.Layout == "" && !isSearchOwner(opts.Owner)) || opts.Layout == defaultLayout
}

func effectiveLayout(opts RunOptions, repo github.Repo) string {
	if opts.Layout != "" {
		return opts.Layout
	}
	if isSearchOwner(opts.Owner) {
		return searchLayout
	}
	return defaultLayout
}

func isSearchOwner(owner string) bool {
	return strings.HasPrefix(owner, "topic:") || strings.HasPrefix(owner, "language:")
}

func repoRemoteIdentity(repo github.Repo) string {
	if repo.FullName != "" {
		return strings.ToLower("github.com/" + repo.FullName)
	}
	if repo.Owner != "" && repo.Name != "" {
		return strings.ToLower("github.com/" + repo.Owner + "/" + repo.Name)
	}
	return ""
}

func firstPath(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	return paths[0]
}

func discoverExistingRepos(baseDir string) map[string][]string {
	found := make(map[string][]string)
	_ = walkDir(baseDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.IsDir() || path == baseDir {
			return nil
		}
		if git.IsRepository(path) {
			if remote, err := gitRemoteOrigin(path); err == nil {
				if identity := git.CanonicalRemote(remote); identity != "" {
					found[identity] = append(found[identity], path)
				}
			}
			return filepath.SkipDir
		}
		if skipDiscoveryDirectory(d.Name()) {
			return filepath.SkipDir
		}
		return nil
	})
	return found
}

func skipDiscoveryDirectory(name string) bool {
	switch name {
	case ".git", ".cache", ".next", ".venv", "DerivedData", "Pods", "build", "dist", "node_modules", "target", "vendor", "venv":
		return true
	default:
		return false
	}
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

func repositoryBucket(repo github.Repo) string {
	if strings.HasSuffix(strings.ToLower(repo.Name), ".github.io") {
		return "Web"
	}
	return canonicalLanguage(repo.Language)
}

func canonicalLanguage(lang string) string {
	normalized := normalizeLanguage(lang)
	canonical := map[string]string{
		"c": "C", "cpp": "Cpp", "csharp": "CSharp", "css": "CSS",
		"dockerfile": "Docker", "go": "Go", "html": "HTML",
		"javascript": "JavaScript", "jupyter_notebook": "Python", "lua": "Lua",
		"objective-c": "Objective-C", "objective-c++": "Objective-Cpp", "other": "Other",
		"php": "PHP", "python": "Python", "ruby": "Ruby", "rust": "Rust",
		"scss": "SCSS", "shell": "Shell", "solidity": "Solidity", "stylus": "Stylus",
		"swift": "Swift", "tex": "TeX", "typescript": "TypeScript",
	}
	if bucket, ok := canonical[normalized]; ok {
		return bucket
	}
	parts := strings.FieldsFunc(normalized, func(r rune) bool { return r == '_' || r == '-' })
	for i, part := range parts {
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, "")
}

func repositoryCollection(repo github.Repo) string {
	if repo.Fork {
		return "Forks"
	}
	if strings.EqualFold(repo.Visibility, "private") {
		return "Private"
	}
	return "Public"
}

func migrateLegacy(baseDir string, repos []github.Repo) {
	for _, repo := range repos {
		legacyDir := filepath.Join(baseDir, normalizeLanguage(repo.Language), repo.Name)
		targetDir := filepath.Join(baseDir, repositoryCollection(repo), repositoryBucket(repo), repo.Name)

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

// normalizeLayoutDirCase applies Finder-facing capitalization to collection
// and ecosystem directories. APFS/HFS+ need an intermediate name for a
// case-only rename.
func normalizeLayoutDirCase(baseDir string, repos []github.Repo) {
	buckets := make(map[string]string, len(repos))
	for _, r := range repos {
		bucket := repositoryBucket(r)
		buckets[strings.ToLower(bucket)] = bucket
	}
	if len(buckets) == 0 {
		return
	}
	visEntries, err := readDir(baseDir)
	if err != nil {
		return
	}
	for _, ve := range visEntries {
		if !ve.IsDir() {
			continue
		}
		collection := canonicalCollectionName(ve.Name())
		if collection == "" {
			continue
		}
		visDir := filepath.Join(baseDir, ve.Name())
		if ve.Name() != collection && strings.EqualFold(ve.Name(), collection) {
			canonicalPath := filepath.Join(baseDir, collection)
			if renameCaseOnly(visDir, canonicalPath) {
				visDir = canonicalPath
			}
		}
		langEntries, err := readDir(visDir)
		if err != nil {
			continue
		}
		for _, le := range langEntries {
			if !le.IsDir() {
				continue
			}
			name := le.Name()
			canonical, ok := buckets[strings.ToLower(name)]
			if !ok || name == canonical {
				continue
			}
			src := filepath.Join(visDir, name)
			dst := filepath.Join(visDir, canonical)
			renameCaseOnly(src, dst)
		}
	}
}

func canonicalCollectionName(name string) string {
	for _, collection := range []string{"Public", "Private", "Forks", "Work"} {
		if strings.EqualFold(name, collection) {
			return collection
		}
	}
	return ""
}

func renameCaseOnly(src, dst string) bool {
	tmp := dst + ".corral-rename-tmp"
	if err := renamePath(src, tmp); err != nil {
		log.Printf("WARN: failed normalizing case for %s: %v", src, err)
		return false
	}
	if err := renamePath(tmp, dst); err != nil {
		log.Printf("WARN: failed normalizing case for %s -> %s: %v", tmp, dst, err)
		_ = renamePath(tmp, src)
		return false
	}
	return true
}

// cleanupEmptyFolders removes the now-empty legacy top-level language
// directories left behind by migrateLegacy. It only targets directories whose
// names match a repository language, and os.Remove deletes a directory only
// when it is empty, so unrelated entries under baseDir (e.g. .claude, other
// projects) are never touched.
func cleanupEmptyFolders(baseDir string, repos []github.Repo) {
	seenRoots := make(map[string]struct{})
	seenBuckets := make(map[string]struct{})
	for _, repo := range repos {
		lang := normalizeLanguage(repo.Language)
		if _, ok := seenRoots[lang]; !ok {
			seenRoots[lang] = struct{}{}
			_ = os.Remove(filepath.Join(baseDir, lang))
		}
		legacyBucket := filepath.Join(repositoryCollection(repo), lang)
		if _, ok := seenBuckets[legacyBucket]; !ok {
			seenBuckets[legacyBucket] = struct{}{}
			_ = os.Remove(filepath.Join(baseDir, legacyBucket))
		}
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

	if job.Existing != "" && filepath.Clean(job.Existing) != filepath.Clean(targetDir) {
		needsMove := true
		if targetInfo, err := statPath(targetDir); err == nil {
			existingInfo, existingErr := statPath(job.Existing)
			if existingErr != nil {
				result.Action = "ERROR"
				result.Message = fmt.Sprintf("failed checking existing clone: %v", existingErr)
				return result
			}
			if sameFile(existingInfo, targetInfo) {
				// Case-insensitive filesystems can resolve Public and public to
				// the same directory even though their cleaned strings differ.
				targetDir = job.Existing
				result.Target = targetDir
				needsMove = false
			} else {
				result.Action = "ERROR"
				result.Message = fmt.Sprintf("target collision: %s already exists while matching clone is at %s", targetDir, job.Existing)
				return result
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			result.Action = "ERROR"
			result.Message = fmt.Sprintf("failed checking target collision: %v", err)
			return result
		}
		if needsMove && dryRun {
			result.Action = "DRY-RUN"
			result.Message = fmt.Sprintf("move %s to %s", job.Existing, targetDir)
			result.Moved = true
			return result
		}
		if needsMove {
			if err := mkdirAll(filepath.Dir(targetDir), 0o750); err != nil {
				result.Action = "ERROR"
				result.Message = fmt.Sprintf("failed creating relocation target: %v", err)
				return result
			}
			if err := renamePath(job.Existing, targetDir); err != nil {
				result.Action = "ERROR"
				result.Message = fmt.Sprintf("failed moving identity-matched clone from %s: %v", job.Existing, err)
				return result
			}
			result.Moved = true
		}
	}

	if !dryRun {
		if err := os.MkdirAll(filepath.Dir(targetDir), 0o750); err != nil {
			result.Action = "ERROR"
			result.Message = fmt.Sprintf("failed creating target directory: %v", err)
			return result
		}
	}

	if git.IsRepository(targetDir) {
		wantIdentity := repoRemoteIdentity(repo)
		if wantIdentity != "" {
			remote, remoteErr := gitRemoteOrigin(targetDir)
			gotIdentity := git.CanonicalRemote(remote)
			if remoteErr != nil || gotIdentity != wantIdentity {
				result.Action = "ERROR"
				if remoteErr != nil {
					result.Message = fmt.Sprintf("cannot verify existing repository origin: %v", remoteErr)
				} else {
					result.Message = fmt.Sprintf("origin collision: target has %s, expected %s", gotIdentity, wantIdentity)
				}
				return result
			}
		}
		if doSync {
			result.SyncAttempt = true
			if dryRun {
				result.Action = "DRY-RUN"
				result.Message = "git pull"
				return result
			}
			// An empty upstream repo (created but never pushed to) results
			// in an unborn HEAD locally, and `git pull` would fail with
			// "no such ref was fetched". Detect that state cheaply and
			// SKIP with a specific reason instead of surfacing the git
			// error as a sync failure.
			if gitIsEmpty(targetDir) {
				result.Action = "SKIP"
				result.Message = "empty repository (no commits yet)"
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
			err = gitPull(ctx, targetDir, git.PullOptions{
				RecurseSubmodules:       cloneOpts.RecurseSubmodules,
				IgnoreSubmoduleFailures: syncOpts.IgnoreSubmoduleFailures,
			})
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
		if result.Moved {
			result.Message = "moved to desired layout; sync disabled"
		} else {
			result.Message = "already exists"
		}
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

func findOrphans(owner, baseDir string, repos []github.Repo) []string {
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
		if d.IsDir() && path != baseDir && git.IsRepository(path) {
			repoDir := path
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

	return orphans
}

func emitOrphans(owner string, orphans []string) {
	fmt.Println("\n--- Orphan Detection ---")
	for _, orphan := range orphans {
		fmt.Printf("Orphan found: %s\n", orphan)
	}
	if len(orphans) == 0 {
		fmt.Printf("No orphaned repositories found for %s.\n", owner)
	}
}

func detectOrphans(owner, baseDir string, repos []github.Repo) {
	emitOrphans(owner, findOrphans(owner, baseDir, repos))
}

func evaluateLayout(layoutTpl string, repo github.Repo, owner string) (string, error) {
	if layoutTpl == "" {
		layoutTpl = defaultLayout
	}
	tmpl, err := parseLayoutTemplate(layoutTpl)
	if err != nil {
		return "", err
	}
	return executeLayout(tmpl, repo, owner)
}

func parseLayoutTemplate(layout string) (*template.Template, error) {
	if len(layout) > maxLayoutTemplateBytes {
		return nil, fmt.Errorf("layout template exceeds %d bytes", maxLayoutTemplateBytes)
	}
	return template.New("layout").Parse(layout)
}

func executeLayout(tmpl *template.Template, repo github.Repo, owner string) (string, error) {
	var buf bytes.Buffer
	data := struct {
		Visibility    string
		Language      string
		Collection    string
		Bucket        string
		Name          string
		Owner         string
		FullName      string
		ID            int64
		DefaultBranch string
	}{
		Visibility:    strings.ToLower(repo.Visibility),
		Language:      normalizeLanguage(repo.Language),
		Collection:    repositoryCollection(repo),
		Bucket:        repositoryBucket(repo),
		Name:          repo.Name,
		Owner:         firstNonEmpty(repo.Owner, owner),
		FullName:      repo.FullName,
		ID:            repo.ID,
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
