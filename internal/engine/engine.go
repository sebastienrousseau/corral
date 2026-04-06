package engine

import (
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

type Job struct {
	Repo   github.Repo
	Target string
	Legacy string
}

func Run(owner, baseDir string, limit, concurrency int, dryRun, orphans bool, protocol string, doSync, recurseSubmodules bool) {
	isTTY := isatty.IsTerminal(os.Stdout.Fd())
	if !isTTY {
		log.SetOutput(os.Stdout)
	}

	if isTTY {
		fmt.Println("Fetching repositories from GitHub...")
	} else {
		log.Println("Fetching repositories from GitHub...")
	}

	repos, err := github.FetchRepos(owner, limit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}

	if len(repos) == limit && limit > 0 {
		fmt.Printf("WARNING: Fetched exactly %d repositories. There may be more.\n", limit)
	}

	// 1. Migration
	migrateLegacy(baseDir, repos)

	jobs := make(chan Job, len(repos))
	results := make(chan tui.LogMsg, len(repos))
	var wg sync.WaitGroup

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				msg := processRepo(owner, baseDir, protocol, doSync, recurseSubmodules, dryRun, job)
				results <- msg
			}
		}()
	}

	for _, repo := range repos {
		langDir := normalizeLanguage(repo.Language)
		visDir := repo.Visibility
		targetDir := filepath.Join(baseDir, visDir, langDir, repo.Name)
		legacyDir := filepath.Join(baseDir, langDir, repo.Name)
		jobs <- Job{Repo: repo, Target: targetDir, Legacy: legacyDir}
	}
	close(jobs)

	var p *tea.Program
	if isTTY {
		m := tui.NewModel(len(repos))
		p = tea.NewProgram(m)
		go func() {
			for msg := range results {
				p.Send(msg)
			}
		}()
		if _, err := p.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
		}
	} else {
		go func() {
			for msg := range results {
				icon := "✓"
				if msg.Action == "ERROR" || strings.HasPrefix(msg.Action, "FAIL") {
					icon = "✗"
				} else if msg.Action == "SKIP" {
					icon = "-"
				}
				log.Printf("%s [%s] %s: %s", icon, msg.Action, msg.RepoName, msg.Message)
			}
		}()
	}

	wg.Wait()
	close(results)

	cleanupEmptyFolders(baseDir)

	if orphans {
		detectOrphans(owner, baseDir, repos)
	}
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
			os.MkdirAll(filepath.Dir(targetDir), 0755)
			os.Rename(legacyDir, targetDir)
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
			os.Remove(path) // removes only if empty
		}
	}
}

func processRepo(owner, baseDir, protocol string, doSync, recurseSubmodules, dryRun bool, job Job) tui.LogMsg {
	repo := job.Repo
	targetDir := job.Target

	if !dryRun {
		os.MkdirAll(filepath.Dir(targetDir), 0755)
	}

	gitDir := filepath.Join(targetDir, ".git")
	if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
		if doSync {
			if dryRun {
				return tui.LogMsg{RepoName: repo.Name, Action: "DRY-RUN", Message: "git pull"}
			}
			branch, err := git.CurrentBranch(targetDir)
			if err == nil && branch != repo.DefaultBranch {
				return tui.LogMsg{RepoName: repo.Name, Action: "SKIP", Message: fmt.Sprintf("on branch %s", branch)}
			}
			err = git.Pull(targetDir, recurseSubmodules)
			if err != nil {
				return tui.LogMsg{RepoName: repo.Name, Action: "ERROR", Message: "sync failed"}
			}
			return tui.LogMsg{RepoName: repo.Name, Action: "SYNC", Message: "synced successfully"}
		}
		return tui.LogMsg{RepoName: repo.Name, Action: "SKIP", Message: "already exists"}
	} else if info, err := os.Stat(targetDir); err == nil && info.IsDir() {
		return tui.LogMsg{RepoName: repo.Name, Action: "SKIP", Message: "exists but is not a git repo"}
	}

	url := repo.CloneURL
	if protocol == "ssh" && repo.SSHURL != "" {
		url = repo.SSHURL
	} else if protocol == "ssh" {
		url = fmt.Sprintf("git@github.com:%s/%s.git", owner, repo.Name)
	}

	if dryRun {
		return tui.LogMsg{RepoName: repo.Name, Action: "DRY-RUN", Message: "git clone"}
	}

	err := git.Clone(url, targetDir, recurseSubmodules)
	if err != nil {
		return tui.LogMsg{RepoName: repo.Name, Action: "ERROR", Message: "clone failed"}
	}
	return tui.LogMsg{RepoName: repo.Name, Action: "CLONE", Message: "cloned successfully"}
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
			url, err := git.RemoteOrigin(repoDir)
			if err == nil && (strings.Contains(url, "/"+owner+"/") || strings.Contains(url, ":"+owner+"/")) {
				if !repoMap[repoName] {
					orphans = append(orphans, repoDir)
				}
			}
			return filepath.SkipDir // skip going deeper into .git
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
