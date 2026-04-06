package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sebastienrousseau/corral/internal/github"
)

func TestEngineRunError(t *testing.T) {
	oldFetchRepos := fetchRepos
	defer func() { fetchRepos = oldFetchRepos }()
	fetchRepos = func(owner string, limit int) ([]github.Repo, error) {
		return nil, fmt.Errorf("mock error")
	}

	var exitCode int
	oldOsExit := osExit
	defer func() { osExit = oldOsExit }()
	osExit = func(code int) { exitCode = code }

	Run("owner", "dir", 100, 1, false, false, "https", true, false)
	if exitCode != 1 {
		t.Errorf("Expected exit code 1, got %d", exitCode)
	}
}

func TestEngineRunHeadlessErrors(t *testing.T) {
	oldFetchRepos := fetchRepos
	defer func() { fetchRepos = oldFetchRepos }()
	fetchRepos = func(owner string, limit int) ([]github.Repo, error) {
		return []github.Repo{
			{Name: "repo_error", Language: "Go", Visibility: "Public", DefaultBranch: "main"},
			{Name: "repo_skip", Language: "Go", Visibility: "Public", DefaultBranch: "main"},
		}, nil
	}

	oldGitClone := gitClone
	defer func() { gitClone = oldGitClone }()
	gitClone = func(url, targetDir string, recurseSubmodules bool) error {
		if strings.Contains(targetDir, "repo_error") {
			return fmt.Errorf("err")
		}
		return nil
	}

	baseDir, _ := os.MkdirTemp("", "engine_test")
	defer os.RemoveAll(baseDir)

	// Pre-create repo_skip so it skips
	os.MkdirAll(filepath.Join(baseDir, "Public", "go", "repo_skip", ".git"), 0755)

	oldIsTerminal := isTerminal
	defer func() { isTerminal = oldIsTerminal }()
	isTerminal = func(fd uintptr) bool { return false } // Headless

	Run("owner", baseDir, 2, 1, false, false, "https", false, false)
}

func TestEngineRunSuccess(t *testing.T) {
	oldFetchRepos := fetchRepos
	defer func() { fetchRepos = oldFetchRepos }()
	fetchRepos = func(owner string, limit int) ([]github.Repo, error) {
		return []github.Repo{
			{Name: "repo1", Language: "Go", Visibility: "Public", DefaultBranch: "main"},
			{Name: "repo2", Language: "Go", Visibility: "Public", DefaultBranch: "main"},
		}, nil
	}

	var exitCode int
	oldOsExit := osExit
	defer func() { osExit = oldOsExit }()
	osExit = func(code int) { exitCode = code }

	oldIsTerminal := isTerminal
	defer func() { isTerminal = oldIsTerminal }()
	isTerminal = func(fd uintptr) bool { return true } // Mock TTY true

	oldRunProgram := runProgram
	defer func() { runProgram = oldRunProgram }()
	runProgram = func(p *tea.Program) (tea.Model, error) {
		return nil, nil // Mock successful TUI run
	}

	baseDir, _ := os.MkdirTemp("", "engine_test")
	defer os.RemoveAll(baseDir)

	// Run with TTY simulation and orphans
	Run("owner", baseDir, 2, 2, true, true, "https", true, false)
	if exitCode != 0 {
		t.Errorf("Expected exit code 0, got %d", exitCode)
	}

	// Mock TTY false
	isTerminal = func(fd uintptr) bool { return false }
	Run("owner", baseDir, 2, 2, true, true, "https", true, false)

	// Mock TTY true but runProgram fails
	isTerminal = func(fd uintptr) bool { return true }
	runProgram = func(p *tea.Program) (tea.Model, error) {
		return nil, fmt.Errorf("tui err")
	}
	Run("owner", baseDir, 2, 2, true, true, "https", true, false)
}

func TestProcessRepo(t *testing.T) {
	baseDir, _ := os.MkdirTemp("", "engine_test")
	defer os.RemoveAll(baseDir)

	repo := github.Repo{
		Name:          "repo1",
		Language:      "Go",
		Visibility:    "Public",
		DefaultBranch: "main",
		CloneURL:      "http://clone",
		SSHURL:        "ssh://clone",
	}
	targetDir := filepath.Join(baseDir, "Public", "go", "repo1")

	// Dry run clone
	job := Job{Repo: repo, Target: targetDir}
	msg := processRepo("owner", baseDir, "https", true, false, true, job)
	if msg.Action != "DRY-RUN" || msg.Message != "git clone" {
		t.Errorf("Expected dry run clone, got %v", msg)
	}

	// Create a dummy .git dir to simulate existing repo
	os.MkdirAll(filepath.Join(targetDir, ".git"), 0755)

	// Dry run sync
	msg = processRepo("owner", baseDir, "https", true, false, true, job)
	if msg.Action != "DRY-RUN" || msg.Message != "git pull" {
		t.Errorf("Expected dry run pull, got %v", msg)
	}

	// Test skip sync (noSync)
	msg = processRepo("owner", baseDir, "https", false, false, false, job)
	if msg.Action != "SKIP" {
		t.Errorf("Expected SKIP, got %v", msg)
	}

	// Test exists but not git repo
	os.RemoveAll(targetDir)
	os.MkdirAll(targetDir, 0755)
	msg = processRepo("owner", baseDir, "https", false, false, false, job)
	if msg.Action != "SKIP" || !strings.Contains(msg.Message, "not a git repo") {
		t.Errorf("Expected SKIP for non-git repo, got %v", msg)
	}

	// Test ssh protocol clone
	os.RemoveAll(targetDir)
	msg = processRepo("owner", baseDir, "ssh", false, false, true, job)
	if msg.Action != "DRY-RUN" {
		t.Errorf("Expected DRY-RUN for ssh, got %v", msg)
	}
}

func TestProcessRepoFull(t *testing.T) {
	baseDir, _ := os.MkdirTemp("", "engine_test")
	defer os.RemoveAll(baseDir)

	oldGitClone := gitClone
	oldGitPull := gitPull
	oldGitCurrentBranch := gitCurrentBranch
	oldGitRemoteOrigin := gitRemoteOrigin
	defer func() {
		gitClone = oldGitClone
		gitPull = oldGitPull
		gitCurrentBranch = oldGitCurrentBranch
		gitRemoteOrigin = oldGitRemoteOrigin
	}()

	gitClone = func(url, targetDir string, recurseSubmodules bool) error { return nil }
	gitPull = func(targetDir string, recurseSubmodules bool) error { return nil }
	gitCurrentBranch = func(targetDir string) (string, error) { return "main", nil }
	gitRemoteOrigin = func(targetDir string) (string, error) { return "https://github.com/owner/repo1.git", nil }

	repo := github.Repo{
		Name:          "repo1",
		Language:      "Go",
		Visibility:    "Public",
		DefaultBranch: "main",
		CloneURL:      "http://clone",
		SSHURL:        "ssh://clone",
	}
	targetDir := filepath.Join(baseDir, "Public", "go", "repo1")

	job := Job{Repo: repo, Target: targetDir}

	// Test clone success
	msg := processRepo("owner", baseDir, "https", true, false, false, job)
	if msg.Action != "CLONE" {
		t.Errorf("Expected CLONE, got %v", msg)
	}

	// Test clone error
	gitClone = func(url, targetDir string, recurseSubmodules bool) error { return fmt.Errorf("err") }
	os.RemoveAll(targetDir)
	msg = processRepo("owner", baseDir, "https", true, false, false, job)
	if msg.Action != "ERROR" {
		t.Errorf("Expected ERROR, got %v", msg)
	}

	// Test sync success
	os.MkdirAll(filepath.Join(targetDir, ".git"), 0755)
	msg = processRepo("owner", baseDir, "https", true, false, false, job)
	if msg.Action != "SYNC" {
		t.Errorf("Expected SYNC, got %v", msg)
	}

	// Test sync error
	gitPull = func(targetDir string, recurseSubmodules bool) error { return fmt.Errorf("err") }
	msg = processRepo("owner", baseDir, "https", true, false, false, job)
	if msg.Action != "ERROR" {
		t.Errorf("Expected ERROR, got %v", msg)
	}

	// Test skip on wrong branch
	gitCurrentBranch = func(targetDir string) (string, error) { return "feat", nil }
	msg = processRepo("owner", baseDir, "https", true, false, false, job)
	if msg.Action != "SKIP" {
		t.Errorf("Expected SKIP, got %v", msg)
	}

	// Test ssh protocol clone fallback
	repo.SSHURL = ""
	job = Job{Repo: repo, Target: targetDir}
	os.RemoveAll(targetDir)
	msg = processRepo("owner", baseDir, "ssh", false, false, true, job)
	if msg.Action != "DRY-RUN" || !strings.Contains(msg.Message, "git clone") {
		t.Errorf("Expected DRY-RUN for ssh fallback, got %v", msg)
	}

	// Test orphan detection with actual hit
	detectOrphans("owner", baseDir, []github.Repo{{Name: "other"}})
}

func TestDetectOrphansError(t *testing.T) {
	// Cover WalkDir error and RemoteOrigin error
	detectOrphans("owner", "/invalid_dir_that_does_not_exist", []github.Repo{})

	baseDir, _ := os.MkdirTemp("", "engine_test")
	defer os.RemoveAll(baseDir)

	os.MkdirAll(filepath.Join(baseDir, "Public", "go", "repo1", ".git"), 0755)

	oldGitRemoteOrigin := gitRemoteOrigin
	defer func() { gitRemoteOrigin = oldGitRemoteOrigin }()
	gitRemoteOrigin = func(targetDir string) (string, error) {
		return "", fmt.Errorf("err")
	}

	detectOrphans("owner", baseDir, []github.Repo{})

	gitRemoteOrigin = func(targetDir string) (string, error) {
		return "https://github.com/someone_else/repo.git", nil
	}
	detectOrphans("owner", baseDir, []github.Repo{})

	gitRemoteOrigin = func(targetDir string) (string, error) {
		return "git@github.com:owner/repo.git", nil
	}
	detectOrphans("owner", baseDir, []github.Repo{})
}

func TestCleanupEmptyFoldersError(t *testing.T) {
	cleanupEmptyFolders("/invalid_dir_that_does_not_exist")
}

func TestNormalizeLanguage(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", "other"},
		{"Go", "go"},
		{"C#", "csharp"},
		{"C++", "cpp"},
		{"Jupyter Notebook", "jupyter_notebook"},
		{"C/C++", "c_c++"},
	}

	for _, tt := range tests {
		result := normalizeLanguage(tt.input)
		if result != tt.expected {
			t.Errorf("normalizeLanguage(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestMigrateLegacy(t *testing.T) {
	baseDir, err := os.MkdirTemp("", "engine_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(baseDir)

	legacyDir := filepath.Join(baseDir, "go", "myrepo")
	os.MkdirAll(legacyDir, 0755)

	repos := []github.Repo{
		{Name: "myrepo", Language: "Go", Visibility: "Public"},
	}

	migrateLegacy(baseDir, repos)

	targetDir := filepath.Join(baseDir, "Public", "go", "myrepo")
	if _, err := os.Stat(targetDir); os.IsNotExist(err) {
		t.Errorf("Expected %s to exist after migration", targetDir)
	}
}

func TestCleanupEmptyFolders(t *testing.T) {
	baseDir, err := os.MkdirTemp("", "engine_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(baseDir)

	emptyDir := filepath.Join(baseDir, "empty")
	os.MkdirAll(emptyDir, 0755)

	notEmptyDir := filepath.Join(baseDir, "not_empty")
	os.MkdirAll(notEmptyDir, 0755)
	os.WriteFile(filepath.Join(notEmptyDir, "file.txt"), []byte("data"), 0644)

	// Keep Public/Private
	os.MkdirAll(filepath.Join(baseDir, "Public"), 0755)
	os.MkdirAll(filepath.Join(baseDir, "Private"), 0755)

	cleanupEmptyFolders(baseDir)

	if _, err := os.Stat(emptyDir); !os.IsNotExist(err) {
		t.Errorf("Expected %s to be removed", emptyDir)
	}
	if _, err := os.Stat(notEmptyDir); os.IsNotExist(err) {
		t.Errorf("Expected %s to be kept", notEmptyDir)
	}
	if _, err := os.Stat(filepath.Join(baseDir, "Public")); os.IsNotExist(err) {
		t.Errorf("Expected Public to be kept")
	}
}
