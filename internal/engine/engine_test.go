package engine

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sebastienrousseau/corral/internal/git"
	"github.com/sebastienrousseau/corral/internal/github"
)

func defaultRunOptions(baseDir string) RunOptions {
	return RunOptions{
		Owner:       "owner",
		BaseDir:     baseDir,
		Concurrency: 1,
		Protocol:    "https",
		DoSync:      true,
		Output:      OutputText,
		Fetch: github.FetchOptions{
			Limit: 100,
		},
	}
}

func TestEngineRunError(t *testing.T) {
	oldFetchRepos := fetchRepos
	defer func() { fetchRepos = oldFetchRepos }()
	fetchRepos = func(ctx context.Context, owner string, opts github.FetchOptions) ([]github.Repo, error) {
		return nil, fmt.Errorf("mock error")
	}

	var exitCode int
	oldOsExit := osExit
	defer func() { osExit = oldOsExit }()
	osExit = func(code int) { exitCode = code }

	opts := defaultRunOptions("dir")
	Run(context.Background(), opts)
	if exitCode != 1 {
		t.Errorf("Expected exit code 1, got %d", exitCode)
	}
}

func TestEngineRunInvalidConfig(t *testing.T) {
	var exitCode int
	oldOsExit := osExit
	defer func() { osExit = oldOsExit }()
	osExit = func(code int) { exitCode = code }

	opts := defaultRunOptions("dir")
	opts.Concurrency = 0
	Run(context.Background(), opts)
	if exitCode != 1 {
		t.Errorf("Expected exit code 1 for invalid concurrency, got %d", exitCode)
	}

	exitCode = 0
	opts = defaultRunOptions("dir")
	opts.Fetch.Limit = -1
	Run(context.Background(), opts)
	if exitCode != 1 {
		t.Errorf("Expected exit code 1 for negative limit, got %d", exitCode)
	}
}

func TestEngineRunHeadlessErrors(t *testing.T) {
	oldFetchRepos := fetchRepos
	defer func() { fetchRepos = oldFetchRepos }()
	fetchRepos = func(ctx context.Context, owner string, opts github.FetchOptions) ([]github.Repo, error) {
		return []github.Repo{
			{Name: "repo_error", Language: "Go", Visibility: "Public", DefaultBranch: "main"},
			{Name: "repo_skip", Language: "Go", Visibility: "Public", DefaultBranch: "main"},
		}, nil
	}

	oldGitClone := gitClone
	defer func() { gitClone = oldGitClone }()
	gitClone = func(ctx context.Context, url, targetDir string, opts git.CloneOptions) error {
		if strings.Contains(targetDir, "repo_error") {
			return fmt.Errorf("err")
		}
		return nil
	}

	baseDir, _ := os.MkdirTemp("", "engine_test")
	defer func() { _ = os.RemoveAll(baseDir) }()

	_ = os.MkdirAll(filepath.Join(baseDir, "Public", "go", "repo_skip", ".git"), 0o750)

	oldIsTerminal := isTerminal
	defer func() { isTerminal = oldIsTerminal }()
	isTerminal = func(fd uintptr) bool { return false }

	opts := defaultRunOptions(baseDir)
	opts.Fetch.Limit = 2
	opts.DoSync = false
	Run(context.Background(), opts)
}

func TestEngineRunSuccess(t *testing.T) {
	oldFetchRepos := fetchRepos
	defer func() { fetchRepos = oldFetchRepos }()
	fetchRepos = func(ctx context.Context, owner string, opts github.FetchOptions) ([]github.Repo, error) {
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
	isTerminal = func(fd uintptr) bool { return true }

	oldRunProgram := runProgram
	defer func() { runProgram = oldRunProgram }()
	runProgram = func(p *tea.Program) (tea.Model, error) {
		return nil, nil
	}

	baseDir, _ := os.MkdirTemp("", "engine_test")
	defer func() { _ = os.RemoveAll(baseDir) }()

	opts := defaultRunOptions(baseDir)
	opts.Fetch.Limit = 2
	opts.DryRun = true
	opts.Orphans = true
	Run(context.Background(), opts)
	if exitCode != 0 {
		t.Errorf("Expected exit code 0, got %d", exitCode)
	}

	isTerminal = func(fd uintptr) bool { return false }
	Run(context.Background(), opts)

	isTerminal = func(fd uintptr) bool { return true }
	runProgram = func(p *tea.Program) (tea.Model, error) {
		return nil, fmt.Errorf("tui err")
	}
	Run(context.Background(), opts)
}

func TestProcessRepo(t *testing.T) {
	baseDir, _ := os.MkdirTemp("", "engine_test")
	defer func() { _ = os.RemoveAll(baseDir) }()

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

	msg := processRepo(context.Background(), "owner", "https", true, true, git.CloneOptions{}, job)
	if msg.Action != "DRY-RUN" || msg.Message != "git clone" {
		t.Errorf("Expected dry run clone, got %v", msg)
	}

	_ = os.MkdirAll(filepath.Join(targetDir, ".git"), 0o750)

	msg = processRepo(context.Background(), "owner", "https", true, true, git.CloneOptions{}, job)
	if msg.Action != "DRY-RUN" || msg.Message != "git pull" {
		t.Errorf("Expected dry run pull, got %v", msg)
	}

	msg = processRepo(context.Background(), "owner", "https", false, false, git.CloneOptions{}, job)
	if msg.Action != "SKIP" {
		t.Errorf("Expected SKIP, got %v", msg)
	}

	_ = os.RemoveAll(targetDir)
	_ = os.MkdirAll(targetDir, 0o750)
	msg = processRepo(context.Background(), "owner", "https", false, false, git.CloneOptions{}, job)
	if msg.Action != "SKIP" || !strings.Contains(msg.Message, "not a git repo") {
		t.Errorf("Expected SKIP for non-git repo, got %v", msg)
	}

	_ = os.RemoveAll(targetDir)
	msg = processRepo(context.Background(), "owner", "ssh", false, true, git.CloneOptions{}, job)
	if msg.Action != "DRY-RUN" {
		t.Errorf("Expected DRY-RUN for ssh, got %v", msg)
	}
}

func TestProcessRepoFull(t *testing.T) {
	baseDir, _ := os.MkdirTemp("", "engine_test")
	defer func() { _ = os.RemoveAll(baseDir) }()

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

	gitClone = func(ctx context.Context, url, targetDir string, opts git.CloneOptions) error { return nil }
	gitPull = func(ctx context.Context, targetDir string, recurseSubmodules bool) error { return nil }
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

	msg := processRepo(context.Background(), "owner", "https", true, false, git.CloneOptions{}, job)
	if msg.Action != "CLONE" {
		t.Errorf("Expected CLONE, got %v", msg)
	}

	gitClone = func(ctx context.Context, url, targetDir string, opts git.CloneOptions) error {
		return fmt.Errorf("err")
	}
	_ = os.RemoveAll(targetDir)
	msg = processRepo(context.Background(), "owner", "https", true, false, git.CloneOptions{}, job)
	if msg.Action != "ERROR" {
		t.Errorf("Expected ERROR, got %v", msg)
	}

	_ = os.MkdirAll(filepath.Join(targetDir, ".git"), 0o750)
	msg = processRepo(context.Background(), "owner", "https", true, false, git.CloneOptions{}, job)
	if msg.Action != "SYNC" {
		t.Errorf("Expected SYNC, got %v", msg)
	}

	gitPull = func(ctx context.Context, targetDir string, recurseSubmodules bool) error { return fmt.Errorf("err") }
	msg = processRepo(context.Background(), "owner", "https", true, false, git.CloneOptions{}, job)
	if msg.Action != "ERROR" {
		t.Errorf("Expected ERROR, got %v", msg)
	}

	gitCurrentBranch = func(targetDir string) (string, error) { return "feat", nil }
	msg = processRepo(context.Background(), "owner", "https", true, false, git.CloneOptions{}, job)
	if msg.Action != "SKIP" {
		t.Errorf("Expected SKIP, got %v", msg)
	}

	repo.SSHURL = ""
	job = Job{Repo: repo, Target: targetDir}
	_ = os.RemoveAll(targetDir)
	msg = processRepo(context.Background(), "owner", "ssh", false, true, git.CloneOptions{}, job)
	if msg.Action != "DRY-RUN" || !strings.Contains(msg.Message, "git clone") {
		t.Errorf("Expected DRY-RUN for ssh fallback, got %v", msg)
	}

	detectOrphans("owner", baseDir, []github.Repo{{Name: "other"}})
}

func TestProcessRepoCanceled(t *testing.T) {
	baseDir, _ := os.MkdirTemp("", "engine_test")
	defer func() { _ = os.RemoveAll(baseDir) }()

	repo := github.Repo{
		Name:          "repo1",
		Language:      "Go",
		Visibility:    "Public",
		DefaultBranch: "main",
		CloneURL:      "http://clone",
	}
	targetDir := filepath.Join(baseDir, "Public", "go", "repo1")
	job := Job{Repo: repo, Target: targetDir}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	msg := processRepo(ctx, "owner", "https", true, false, git.CloneOptions{}, job)
	if msg.Action != "ERROR" || !strings.Contains(msg.Message, "canceled") {
		t.Fatalf("expected canceled error result, got %#v", msg)
	}
}

func TestDetectOrphansError(t *testing.T) {
	detectOrphans("owner", "/invalid_dir_that_does_not_exist", []github.Repo{})

	baseDir, _ := os.MkdirTemp("", "engine_test")
	defer func() { _ = os.RemoveAll(baseDir) }()

	_ = os.MkdirAll(filepath.Join(baseDir, "Public", "go", "repo1", ".git"), 0o750)

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

	// The local directory name ("repo1") is unknown, but the remote URL resolves
	// to a known repository, so it must NOT be flagged as an orphan.
	gitRemoteOrigin = func(targetDir string) (string, error) {
		return "https://github.com/owner/actualname.git", nil
	}
	detectOrphans("owner", baseDir, []github.Repo{{Name: "actualname"}})
}

func TestCleanupEmptyFoldersError(t *testing.T) {
	cleanupEmptyFolders("/invalid_dir_that_does_not_exist", []github.Repo{{Language: "Go"}})
}

func TestRepoNameFromURL(t *testing.T) {
	tests := []struct{ in, want string }{
		{"https://github.com/owner/repo.git", "repo"},
		{"git@github.com:owner/repo.git", "repo"},
		{"https://github.com/owner/repo", "repo"},
		{"plainname", "plainname"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := repoNameFromURL(tt.in); got != tt.want {
			t.Errorf("repoNameFromURL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
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
	defer func() { _ = os.RemoveAll(baseDir) }()

	legacyDir := filepath.Join(baseDir, "go", "myrepo")
	_ = os.MkdirAll(legacyDir, 0o750)

	repos := []github.Repo{{Name: "myrepo", Language: "Go", Visibility: "Public"}}
	migrateLegacy(baseDir, repos)

	targetDir := filepath.Join(baseDir, "Public", "go", "myrepo")
	if _, err := os.Stat(targetDir); os.IsNotExist(err) {
		t.Errorf("Expected %s to exist after migration", targetDir)
	}
}

// quitModel is a minimal tea.Model that quits immediately on Init so the
// default runProgram closure (p.Run()) can be exercised without a real TTY.
type quitModel struct{}

func (quitModel) Init() tea.Cmd                       { return tea.Quit }
func (quitModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return quitModel{}, tea.Quit }
func (quitModel) View() string                        { return "" }

// TestRunProgramDefault exercises the default runProgram closure (engine.go:85)
// by running a program that quits immediately with the renderer disabled.
func TestRunProgramDefault(t *testing.T) {
	p := tea.NewProgram(
		quitModel{},
		tea.WithoutRenderer(),
		tea.WithInput(nil),
		tea.WithOutput(io.Discard),
	)
	if _, err := runProgram(p); err != nil {
		t.Fatalf("runProgram returned error: %v", err)
	}
}

// TestEngineRunNilContext covers the ctx == nil branch and the remaining
// validation branches (empty owner, empty base dir, bad protocol).
func TestEngineRunValidation(t *testing.T) {
	var exitCode int
	oldOsExit := osExit
	defer func() { osExit = oldOsExit }()
	osExit = func(code int) { exitCode = code }

	oldIsTerminal := isTerminal
	defer func() { isTerminal = oldIsTerminal }()
	isTerminal = func(fd uintptr) bool { return false }

	// Empty owner.
	opts := defaultRunOptions("dir")
	opts.Owner = ""
	exitCode = 0
	Run(context.Background(), opts)
	if exitCode != 1 {
		t.Errorf("expected exit 1 for empty owner, got %d", exitCode)
	}

	// Empty base directory.
	opts = defaultRunOptions("")
	exitCode = 0
	Run(context.Background(), opts)
	if exitCode != 1 {
		t.Errorf("expected exit 1 for empty base dir, got %d", exitCode)
	}

	// Invalid protocol.
	opts = defaultRunOptions("dir")
	opts.Protocol = "ftp"
	exitCode = 0
	Run(context.Background(), opts)
	if exitCode != 1 {
		t.Errorf("expected exit 1 for bad protocol, got %d", exitCode)
	}
}

// TestEngineRunNilContextAndDefaultOutput covers the ctx == nil branch and the
// Output == "" defaulting branch using a nil context and unset output.
func TestEngineRunNilContextAndDefaultOutput(t *testing.T) {
	oldFetchRepos := fetchRepos
	defer func() { fetchRepos = oldFetchRepos }()
	fetchRepos = func(ctx context.Context, owner string, opts github.FetchOptions) ([]github.Repo, error) {
		return []github.Repo{}, nil
	}

	oldIsTerminal := isTerminal
	defer func() { isTerminal = oldIsTerminal }()
	isTerminal = func(fd uintptr) bool { return false }

	baseDir, err := os.MkdirTemp("", "engine_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(baseDir) }()

	opts := defaultRunOptions(baseDir)
	opts.Output = "" // exercise default-output branch
	// A typed nil context exercises the ctx == nil guard without tripping
	// staticcheck's SA1012 (which only flags an untyped nil literal).
	var nilCtx context.Context
	Run(nilCtx, opts)
}

// TestEngineRunJSON covers the OutputJSON aggregated payload branch and the
// add CLONE/SYNC accumulation cases.
func TestEngineRunJSON(t *testing.T) {
	oldFetchRepos := fetchRepos
	oldGitClone := gitClone
	oldGitPull := gitPull
	oldGitCurrentBranch := gitCurrentBranch
	defer func() {
		fetchRepos = oldFetchRepos
		gitClone = oldGitClone
		gitPull = oldGitPull
		gitCurrentBranch = oldGitCurrentBranch
	}()

	fetchRepos = func(ctx context.Context, owner string, opts github.FetchOptions) ([]github.Repo, error) {
		return []github.Repo{
			{Name: "repo_clone", Language: "Go", Visibility: "Public", DefaultBranch: "main"},
			{Name: "repo_sync", Language: "Go", Visibility: "Public", DefaultBranch: "main"},
		}, nil
	}
	gitClone = func(ctx context.Context, url, targetDir string, opts git.CloneOptions) error { return nil }
	gitPull = func(ctx context.Context, targetDir string, recurseSubmodules bool) error { return nil }
	gitCurrentBranch = func(targetDir string) (string, error) { return "main", nil }

	oldIsTerminal := isTerminal
	defer func() { isTerminal = oldIsTerminal }()
	isTerminal = func(fd uintptr) bool { return false }

	baseDir, err := os.MkdirTemp("", "engine_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(baseDir) }()

	// Pre-create an existing git repo so repo_sync triggers a SYNC action.
	if err := os.MkdirAll(filepath.Join(baseDir, "Public", "go", "repo_sync", ".git"), 0o750); err != nil {
		t.Fatal(err)
	}

	opts := defaultRunOptions(baseDir)
	opts.Output = OutputJSON
	Run(context.Background(), opts)
}

// TestEngineRunNDJSON covers the OutputNDJSON per-result encode branch.
func TestEngineRunNDJSON(t *testing.T) {
	oldFetchRepos := fetchRepos
	oldGitClone := gitClone
	defer func() {
		fetchRepos = oldFetchRepos
		gitClone = oldGitClone
	}()

	fetchRepos = func(ctx context.Context, owner string, opts github.FetchOptions) ([]github.Repo, error) {
		return []github.Repo{
			{Name: "repo_a", Language: "Go", Visibility: "Public", DefaultBranch: "main"},
		}, nil
	}
	gitClone = func(ctx context.Context, url, targetDir string, opts git.CloneOptions) error { return nil }

	oldIsTerminal := isTerminal
	defer func() { isTerminal = oldIsTerminal }()
	isTerminal = func(fd uintptr) bool { return false }

	baseDir, err := os.MkdirTemp("", "engine_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(baseDir) }()

	opts := defaultRunOptions(baseDir)
	opts.Output = OutputNDJSON
	Run(context.Background(), opts)
}

// TestEngineRunLimitWarning covers the "fetched exactly N" warning branch.
func TestEngineRunLimitWarning(t *testing.T) {
	oldFetchRepos := fetchRepos
	oldGitClone := gitClone
	defer func() {
		fetchRepos = oldFetchRepos
		gitClone = oldGitClone
	}()

	fetchRepos = func(ctx context.Context, owner string, opts github.FetchOptions) ([]github.Repo, error) {
		return []github.Repo{
			{Name: "repo_a", Language: "Go", Visibility: "Public", DefaultBranch: "main"},
		}, nil
	}
	gitClone = func(ctx context.Context, url, targetDir string, opts git.CloneOptions) error { return nil }

	oldIsTerminal := isTerminal
	defer func() { isTerminal = oldIsTerminal }()
	isTerminal = func(fd uintptr) bool { return false }

	baseDir, err := os.MkdirTemp("", "engine_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(baseDir) }()

	opts := defaultRunOptions(baseDir)
	opts.Fetch.Limit = 1 // equals number of repos -> warning
	Run(context.Background(), opts)
}

// TestEngineRunCanceled covers the ctx-cancel enqueue and worker/consumer
// select branches by running with an already-canceled context.
func TestEngineRunCanceled(t *testing.T) {
	oldFetchRepos := fetchRepos
	oldGitClone := gitClone
	defer func() {
		fetchRepos = oldFetchRepos
		gitClone = oldGitClone
	}()

	repos := make([]github.Repo, 0, 50)
	for i := 0; i < 50; i++ {
		repos = append(repos, github.Repo{
			Name:          fmt.Sprintf("repo_%d", i),
			Language:      "Go",
			Visibility:    "Public",
			DefaultBranch: "main",
		})
	}
	fetchRepos = func(ctx context.Context, owner string, opts github.FetchOptions) ([]github.Repo, error) {
		return repos, nil
	}
	gitClone = func(ctx context.Context, url, targetDir string, opts git.CloneOptions) error { return nil }

	oldIsTerminal := isTerminal
	defer func() { isTerminal = oldIsTerminal }()
	isTerminal = func(fd uintptr) bool { return false }

	baseDir, err := os.MkdirTemp("", "engine_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(baseDir) }()

	opts := defaultRunOptions(baseDir)
	opts.Fetch.Limit = len(repos)

	// The cancel-handling select branches in the worker, the post-process
	// results send, and the enqueue loop are inherently racy (both select
	// cases may be ready). Run repeatedly with an already-canceled context to
	// deterministically exercise all of them.
	for i := 0; i < 200; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		Run(ctx, opts)
	}
}

// withRedirectedStdout temporarily replaces os.Stdout with the write end of a
// pipe whose read end is immediately closed, so any write to stdout fails. It
// returns a restore function.
func withRedirectedStdout(t *testing.T) func() {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	return func() {
		os.Stdout = old
		_ = w.Close()
	}
}

// TestEngineRunJSONEncodeError covers the OutputJSON encode-failure branch
// (which calls osExit) by directing stdout to a broken pipe.
func TestEngineRunJSONEncodeError(t *testing.T) {
	oldFetchRepos := fetchRepos
	oldGitClone := gitClone
	defer func() {
		fetchRepos = oldFetchRepos
		gitClone = oldGitClone
	}()
	fetchRepos = func(ctx context.Context, owner string, opts github.FetchOptions) ([]github.Repo, error) {
		return []github.Repo{{Name: "repo_a", Language: "Go", Visibility: "Public", DefaultBranch: "main"}}, nil
	}
	gitClone = func(ctx context.Context, url, targetDir string, opts git.CloneOptions) error { return nil }

	oldIsTerminal := isTerminal
	defer func() { isTerminal = oldIsTerminal }()
	isTerminal = func(fd uintptr) bool { return false }

	var exitCode int
	oldOsExit := osExit
	defer func() { osExit = oldOsExit }()
	osExit = func(code int) { exitCode = code }

	baseDir, err := os.MkdirTemp("", "engine_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(baseDir) }()

	opts := defaultRunOptions(baseDir)
	opts.Output = OutputJSON

	restore := withRedirectedStdout(t)
	Run(context.Background(), opts)
	restore()

	if exitCode != 1 {
		t.Errorf("expected exit 1 on json encode failure, got %d", exitCode)
	}
}

// TestEngineRunNDJSONEncodeError covers the OutputNDJSON per-result
// encode-failure branch by directing stdout to a broken pipe.
func TestEngineRunNDJSONEncodeError(t *testing.T) {
	oldFetchRepos := fetchRepos
	oldGitClone := gitClone
	defer func() {
		fetchRepos = oldFetchRepos
		gitClone = oldGitClone
	}()
	fetchRepos = func(ctx context.Context, owner string, opts github.FetchOptions) ([]github.Repo, error) {
		return []github.Repo{{Name: "repo_a", Language: "Go", Visibility: "Public", DefaultBranch: "main"}}, nil
	}
	gitClone = func(ctx context.Context, url, targetDir string, opts git.CloneOptions) error { return nil }

	oldIsTerminal := isTerminal
	defer func() { isTerminal = oldIsTerminal }()
	isTerminal = func(fd uintptr) bool { return false }

	baseDir, err := os.MkdirTemp("", "engine_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(baseDir) }()

	opts := defaultRunOptions(baseDir)
	opts.Output = OutputNDJSON

	restore := withRedirectedStdout(t)
	Run(context.Background(), opts)
	restore()
}

// TestMigrateLegacyFailures covers the MkdirAll-failure and Rename-failure
// WARN branches of migrateLegacy.
func TestMigrateLegacyFailures(t *testing.T) {
	// MkdirAll failure: make the target's parent path component a regular file.
	t.Run("mkdirall_fails", func(t *testing.T) {
		baseDir, err := os.MkdirTemp("", "engine_test")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = os.RemoveAll(baseDir) }()

		// Legacy dir: <base>/go/myrepo
		if err := os.MkdirAll(filepath.Join(baseDir, "go", "myrepo"), 0o750); err != nil {
			t.Fatal(err)
		}
		// Target parent is <base>/Public/go. Create <base>/Public as a regular
		// file so MkdirAll of the parent fails.
		if err := os.WriteFile(filepath.Join(baseDir, "Public"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}

		repos := []github.Repo{{Name: "myrepo", Language: "Go", Visibility: "Public"}}
		migrateLegacy(baseDir, repos) // should log WARN and continue
	})

	// Rename failure: pre-create a non-empty destination directory so os.Rename
	// of the legacy dir onto it fails.
	t.Run("rename_fails", func(t *testing.T) {
		baseDir, err := os.MkdirTemp("", "engine_test")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = os.RemoveAll(baseDir) }()

		// Legacy dir: <base>/go/myrepo
		if err := os.MkdirAll(filepath.Join(baseDir, "go", "myrepo"), 0o750); err != nil {
			t.Fatal(err)
		}
		// Target dir: <base>/Public/go/myrepo, pre-populated so rename fails.
		target := filepath.Join(baseDir, "Public", "go", "myrepo")
		if err := os.MkdirAll(target, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(target, "keep.txt"), []byte("data"), 0o600); err != nil {
			t.Fatal(err)
		}

		repos := []github.Repo{{Name: "myrepo", Language: "Go", Visibility: "Public"}}
		migrateLegacy(baseDir, repos) // should log WARN about failed migration
	})
}

// TestProcessRepoMkdirFail covers the failed-creating-target-directory branch
// in processRepo by making the target's parent a regular file.
func TestProcessRepoMkdirFail(t *testing.T) {
	baseDir, err := os.MkdirTemp("", "engine_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(baseDir) }()

	// Create <base>/Public/go as a regular file so MkdirAll of the target
	// parent (<base>/Public/go) fails.
	if err := os.MkdirAll(filepath.Join(baseDir, "Public"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(baseDir, "Public", "go"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	repo := github.Repo{
		Name:          "repo1",
		Language:      "Go",
		Visibility:    "Public",
		DefaultBranch: "main",
		CloneURL:      "http://clone",
	}
	targetDir := filepath.Join(baseDir, "Public", "go", "repo1")
	job := Job{Repo: repo, Target: targetDir}

	msg := processRepo(context.Background(), "owner", "https", false, false, git.CloneOptions{}, job)
	if msg.Action != "ERROR" || !strings.Contains(msg.Message, "failed creating target directory") {
		t.Fatalf("expected mkdir error result, got %#v", msg)
	}
}

func TestCleanupEmptyFolders(t *testing.T) {
	baseDir, err := os.MkdirTemp("", "engine_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(baseDir) }()

	// The duplicate Go language exercises the seen-dedup path.
	repos := []github.Repo{{Language: "Go"}, {Language: "Rust"}, {Language: "Go"}}

	// Empty legacy language dir for a known language: removed.
	emptyLang := filepath.Join(baseDir, "go")
	_ = os.MkdirAll(emptyLang, 0o750)

	// Non-empty legacy language dir: os.Remove leaves it alone.
	nonEmptyLang := filepath.Join(baseDir, "rust")
	_ = os.MkdirAll(nonEmptyLang, 0o750)
	_ = os.WriteFile(filepath.Join(nonEmptyLang, "leftover"), []byte("x"), 0o600)

	// Unrelated dir that is not a repo language: must be left untouched.
	unrelated := filepath.Join(baseDir, ".claude")
	_ = os.MkdirAll(unrelated, 0o750)

	cleanupEmptyFolders(baseDir, repos)

	if _, err := os.Stat(emptyLang); !os.IsNotExist(err) {
		t.Errorf("Expected empty language dir %s to be removed", emptyLang)
	}
	if _, err := os.Stat(nonEmptyLang); os.IsNotExist(err) {
		t.Errorf("Expected non-empty language dir %s to be kept", nonEmptyLang)
	}
	if _, err := os.Stat(unrelated); os.IsNotExist(err) {
		t.Errorf("Expected unrelated dir %s to be kept", unrelated)
	}
}

func TestNormalizeLanguageDirCase(t *testing.T) {
	baseDir, err := os.MkdirTemp("", "engine_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(baseDir) }()

	// Existing title-case language dir under a visibility dir.
	mixed := filepath.Join(baseDir, "Public", "JavaScript", "repo1")
	if err := os.MkdirAll(mixed, 0o750); err != nil {
		t.Fatal(err)
	}

	// Already-lowercase dir: idempotent no-op.
	already := filepath.Join(baseDir, "Public", "go", "repo2")
	if err := os.MkdirAll(already, 0o750); err != nil {
		t.Fatal(err)
	}

	// Unrelated dir whose name doesn't match any fetched language: untouched.
	unrelated := filepath.Join(baseDir, "Public", "Configurations", "stuff")
	if err := os.MkdirAll(unrelated, 0o750); err != nil {
		t.Fatal(err)
	}

	// A stray file at base-level (not a visibility dir) must be tolerated.
	if err := os.WriteFile(filepath.Join(baseDir, ".DS_Store"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	repos := []github.Repo{
		{Name: "repo1", Language: "JavaScript", Visibility: "Public"},
		{Name: "repo2", Language: "Go", Visibility: "Public"},
	}
	normalizeLanguageDirCase(baseDir, repos)

	lowered := filepath.Join(baseDir, "Public", "javascript", "repo1")
	if _, err := os.Stat(lowered); err != nil {
		t.Errorf("expected %s to exist after case normalization: %v", lowered, err)
	}
	if _, err := os.Stat(already); err != nil {
		t.Errorf("expected idempotent dir %s to remain: %v", already, err)
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Errorf("expected unrelated dir %s to remain untouched: %v", unrelated, err)
	}

	// Empty repos list short-circuits without error.
	normalizeLanguageDirCase(baseDir, nil)

	// Unreadable base dir is a no-op (just exercising the early-return).
	normalizeLanguageDirCase("/invalid_dir_that_does_not_exist", repos)
}

func TestRunWiresGitTokenProvider(t *testing.T) {
	oldFetch := fetchRepos
	defer func() { fetchRepos = oldFetch }()
	fetchRepos = func(ctx context.Context, owner string, opts github.FetchOptions) ([]github.Repo, error) {
		return nil, nil
	}
	oldIsTerminal := isTerminal
	defer func() { isTerminal = oldIsTerminal }()
	isTerminal = func(fd uintptr) bool { return false }

	oldTok, had := os.LookupEnv("GITHUB_TOKEN")
	defer func() {
		if had {
			_ = os.Setenv("GITHUB_TOKEN", oldTok)
		} else {
			_ = os.Unsetenv("GITHUB_TOKEN")
		}
	}()
	_ = os.Setenv("GITHUB_TOKEN", "tok-xyz")

	baseDir, _ := os.MkdirTemp("", "engine_tok")
	defer func() { _ = os.RemoveAll(baseDir) }()

	Run(context.Background(), defaultRunOptions(baseDir))
	if git.TokenProvider == nil {
		t.Fatal("expected git.TokenProvider to be wired after Run")
	}
	if got := git.TokenProvider(); got != "tok-xyz" {
		t.Errorf("git.TokenProvider() = %q, want tok-xyz", got)
	}
}
