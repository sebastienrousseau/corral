// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runGit runs a git fixture command with an explicit identity and no user or
// system configuration.
//
// The identity has to come from the environment rather than `git config`:
// a submodule's working directory has no .git directory of its own — it has
// a .git *file* pointing into the parent's .git/modules — so `git -C sub
// config user.name` does not reach the config that a commit inside the
// submodule actually reads. A developer machine with a global identity masks
// this completely; a CI runner without one fails with "empty ident name",
// which is exactly how this was found.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", full...) // #nosec G204 -- fixture arguments are literals in this file
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=Corral Test",
		"GIT_AUTHOR_EMAIL=test@example.invalid",
		"GIT_COMMITTER_NAME=Corral Test",
		"GIT_COMMITTER_EMAIL=test@example.invalid",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s failed: %v (%s)", args, dir, err, out)
	}
}

// initRepoWithCommit creates a working repository with one commit on main.
func initRepoWithCommit(t *testing.T, dir, filename string) {
	t.Helper()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.name", "Test")
	runGit(t, dir, "config", "user.email", "test@test.com")
	if err := os.WriteFile(filepath.Join(dir, filename), []byte("body\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "init")
}

// repoWithSubmodule builds a parent repository containing one submodule that
// has an upstream, and returns the parent path and the submodule's path
// inside it. Both are fully published: nothing is unpushed yet.
func repoWithSubmodule(t *testing.T) (parent, subInParent string) {
	t.Helper()
	root := t.TempDir()

	// The submodule's own upstream, so its commits can be "published".
	subUpstream := filepath.Join(root, "sub-upstream")
	if err := os.MkdirAll(subUpstream, 0o750); err != nil {
		t.Fatal(err)
	}
	runGit(t, subUpstream, "init", "--bare", "-b", "main")

	subWork := filepath.Join(root, "sub-work")
	if err := os.MkdirAll(subWork, 0o750); err != nil {
		t.Fatal(err)
	}
	initRepoWithCommit(t, subWork, "sub.txt")
	runGit(t, subWork, "remote", "add", "origin", subUpstream)
	runGit(t, subWork, "push", "-u", "origin", "main")

	parent = filepath.Join(root, "parent")
	if err := os.MkdirAll(parent, 0o750); err != nil {
		t.Fatal(err)
	}
	initRepoWithCommit(t, parent, "parent.txt")
	// -c protocol.file.allow=always: git refuses local-path submodules by
	// default. This is a fixture on a temp dir, not a clone of anything
	// untrusted.
	runGit(t, parent, "-c", "protocol.file.allow=always", "submodule", "add", subUpstream, "sub")
	runGit(t, parent, "commit", "-m", "add submodule")

	return parent, filepath.Join(parent, "sub")
}

func TestSubmodulesHaveUnpublishedWorkNoGitmodules(t *testing.T) {
	dir := t.TempDir()
	initRepoWithCommit(t, dir, "a.txt")

	unpublished, detail := submodulesHaveUnpublishedWork(context.Background(), dir)
	if unpublished {
		t.Fatalf("a repository without submodules reported unpublished work: %s", detail)
	}
	if detail != "" {
		t.Fatalf("expected no detail, got %q", detail)
	}
}

func TestSubmodulesHaveUnpublishedWorkAllPublished(t *testing.T) {
	parent, _ := repoWithSubmodule(t)

	unpublished, detail := submodulesHaveUnpublishedWork(context.Background(), parent)
	if unpublished {
		t.Fatalf("fully published submodule reported as unpublished: %s", detail)
	}
}

func TestSubmodulesHaveUnpublishedWorkDetectsLocalCommit(t *testing.T) {
	parent, sub := repoWithSubmodule(t)

	// A commit inside the submodule that exists on no remote. This is the
	// case the guard exists for: deleting the parent clone would destroy
	// work no upstream has a copy of.
	if err := os.WriteFile(filepath.Join(sub, "local-only.txt"), []byte("unpushed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, sub, "add", ".")
	runGit(t, sub, "commit", "-m", "local only")

	unpublished, detail := submodulesHaveUnpublishedWork(context.Background(), parent)
	if !unpublished {
		t.Fatal("submodule with an unpushed commit was reported as safe to delete")
	}
	if !strings.Contains(detail, "submodule has unpublished commits") {
		t.Fatalf("detail does not name the cause: %q", detail)
	}
	if !strings.Contains(detail, "sub") {
		t.Fatalf("detail does not name the submodule: %q", detail)
	}
}

func TestSubmodulesHaveUnpublishedWorkReportsGitFailure(t *testing.T) {
	dir := t.TempDir()
	// A .gitmodules file with no repository around it: `git submodule
	// foreach` cannot run, and the guard must fail closed rather than
	// reporting the tree as safe.
	if err := os.WriteFile(filepath.Join(dir, ".gitmodules"), []byte("[submodule \"x\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	unpublished, detail := submodulesHaveUnpublishedWork(context.Background(), dir)
	if !unpublished {
		t.Fatal("guard reported safe when it could not inspect submodules")
	}
	if !strings.Contains(detail, "unable to verify submodules") {
		t.Fatalf("detail does not explain the failure: %q", detail)
	}
}

func TestHasUnpublishedWorkConsultsSubmodules(t *testing.T) {
	parent, sub := repoWithSubmodule(t)

	// Publish the parent so its own commits are not the reason.
	parentUpstream := filepath.Join(t.TempDir(), "parent-upstream")
	if err := os.MkdirAll(parentUpstream, 0o750); err != nil {
		t.Fatal(err)
	}
	runGit(t, parentUpstream, "init", "--bare", "-b", "main")
	runGit(t, parent, "remote", "add", "origin", parentUpstream)
	runGit(t, parent, "push", "-u", "origin", "main")

	if unpublished, detail := HasUnpublishedWork(context.Background(), parent); unpublished {
		t.Fatalf("clean parent with published submodule reported unpublished: %s", detail)
	}

	if err := os.WriteFile(filepath.Join(sub, "local-only.txt"), []byte("unpushed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, sub, "add", ".")
	runGit(t, sub, "commit", "-m", "local only")

	// Record and publish the new submodule pointer in the parent. Without
	// this the parent's own working tree is dirty, HasUnpublishedWork stops
	// at "working tree has local changes", and the submodule branch is
	// never reached — which is exactly the state this test must avoid, so
	// that a regression in the submodule check cannot hide behind it.
	runGit(t, parent, "add", "sub")
	runGit(t, parent, "commit", "-m", "bump submodule")
	runGit(t, parent, "push", "origin", "main")

	unpublished, detail := HasUnpublishedWork(context.Background(), parent)
	if !unpublished {
		t.Fatal("HasUnpublishedWork ignored an unpublished submodule commit")
	}
	if !strings.Contains(detail, "submodule") {
		t.Fatalf("detail does not attribute the finding to a submodule: %q", detail)
	}
}

func TestHasIgnoredContentReportsFailure(t *testing.T) {
	// Not a git repository: `git status` fails, and the guard must fail
	// closed rather than reporting "no ignored content".
	ignored, detail := HasIgnoredContent(context.Background(), t.TempDir())
	if !ignored {
		t.Fatal("guard reported no ignored content when it could not inspect the tree")
	}
	if !strings.Contains(detail, "cannot inspect ignored files") {
		t.Fatalf("detail does not explain the failure: %q", detail)
	}
}

func TestHasIgnoredContentSamplesAtMostThree(t *testing.T) {
	dir := t.TempDir()
	initRepoWithCommit(t, dir, "a.txt")
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("secret*\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".gitignore")
	runGit(t, dir, "commit", "-m", "ignore")

	// Five ignored files, so the reported sample must be truncated.
	for _, name := range []string{"secret1", "secret2", "secret3", "secret4", "secret5"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	ignored, detail := HasIgnoredContent(context.Background(), dir)
	if !ignored {
		t.Fatalf("five ignored files were not reported: %q", detail)
	}
	if !strings.Contains(detail, "5 gitignored path(s) present") {
		t.Fatalf("detail does not report the full count: %q", detail)
	}
	if strings.Count(detail, "secret") != 3 {
		t.Fatalf("expected exactly 3 sampled paths, got: %q", detail)
	}
}

func TestHasIgnoredContentCleanTree(t *testing.T) {
	dir := t.TempDir()
	initRepoWithCommit(t, dir, "a.txt")

	ignored, detail := HasIgnoredContent(context.Background(), dir)
	if ignored {
		t.Fatalf("clean tree reported ignored content: %q", detail)
	}
	if detail != "" {
		t.Fatalf("expected no detail for a clean tree, got %q", detail)
	}
}

func TestDirResolvesGitDirectory(t *testing.T) {
	dir := t.TempDir()
	initRepoWithCommit(t, dir, "a.txt")

	got, err := Dir(dir)
	if err != nil {
		t.Fatalf("Dir on a repository failed: %v", err)
	}
	if filepath.Base(got) != ".git" {
		t.Fatalf("Dir = %q, want a path ending in .git", got)
	}

	if _, err := Dir(t.TempDir()); err == nil {
		t.Fatal("Dir on a non-repository should fail")
	}
}
