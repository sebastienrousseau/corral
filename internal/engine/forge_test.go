// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sebastienrousseau/corral/internal/github"
)

// withForgeSelection sets the run's forge and restores it afterwards. The
// seam carries the selection in package state rather than in fetchRepos's
// signature, so that widening it would not have rewritten every test that
// replaces that seam.
func withForgeSelection(t *testing.T, name, url string) {
	t.Helper()
	oldName, oldURL := fetchForgeName, fetchForgeURL
	fetchForgeName, fetchForgeURL = name, url
	t.Cleanup(func() { fetchForgeName, fetchForgeURL = oldName, oldURL })
}

// TestFetchFromForgeConvertsEveryFieldTheEngineReads is the seam's real
// job: a forge.Repo has to arrive as the shape the engine, the layout and
// the TUI all read, with nothing quietly dropped.
//
// Driven against a real Gitea-shaped server rather than GitHub, because
// GitHub's path delegates to a client this package does not own — and
// because a non-GitHub forge reaching the engine at all is the change
// being tested.
func TestFetchFromForgeConvertsEveryFieldTheEngineReads(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/v1/users/acme/repos") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`[{
			"id": 42, "name": "tool", "full_name": "acme/tool",
			"owner": {"login": "acme"},
			"language": "Rust", "private": true, "default_branch": "trunk",
			"clone_url": "https://git.example.com/acme/tool.git",
			"ssh_url": "git@git.example.com:acme/tool.git",
			"fork": false, "archived": false,
			"updated_at": "2026-05-06T07:08:09Z",
			"stars_count": 4, "template": true, "mirror": true
		}]`))
	}))
	defer srv.Close()

	withForgeSelection(t, "gitea", srv.URL)

	repos, err := fetchFromForge(context.Background(), "acme", github.FetchOptions{
		Limit: 7, Visibility: "all",
	})
	if err != nil {
		t.Fatalf("fetchFromForge: %v", err)
	}
	if len(repos) != 1 {
		t.Fatalf("got %d repositories, want 1", len(repos))
	}

	r := repos[0]
	want := github.Repo{
		ID: 42, Owner: "acme", Name: "tool", FullName: "acme/tool",
		Language: "Rust", Visibility: "Private", DefaultBranch: "trunk",
		CloneURL: "https://git.example.com/acme/tool.git",
		SSHURL:   "git@git.example.com:acme/tool.git",
		Stars:    4, IsTemplate: true, IsMirror: true,
		PushedAt: r.PushedAt, // asserted separately, below
	}
	if r != want {
		t.Errorf("a field was lost in the conversion:\n got  %+v\n want %+v", r, want)
	}
	// PushedAt drives the decision to skip a pull, so a zero here would
	// silently make every sync unconditional.
	if r.PushedAt.IsZero() {
		t.Error("PushedAt was dropped; the sync decision depends on it")
	}
	if got := r.PushedAt.UTC().Format("2006-01-02T15:04:05Z"); got != "2026-05-06T07:08:09Z" {
		t.Errorf("PushedAt = %s", got)
	}
}

// TestFetchFromForgeDefaultsToGitHub: the selection is empty when nothing
// asked for a forge, and the default must not have moved.
func TestFetchFromForgeDefaultsToGitHub(t *testing.T) {
	withForgeSelection(t, "", "")
	// Resolving is all this asserts — actually listing would reach the
	// network. An unknown forge fails before any request, which is what
	// distinguishes the two outcomes here.
	_, err := fetchFromForge(context.Background(), "", github.FetchOptions{Limit: 1})
	if err != nil && strings.Contains(err.Error(), "unknown forge") {
		t.Errorf("an empty selection should resolve to github, got %v", err)
	}
}

func TestFetchFromForgeReportsAnUnknownForge(t *testing.T) {
	withForgeSelection(t, "gitub", "")
	_, err := fetchFromForge(context.Background(), "acme", github.FetchOptions{})
	if err == nil {
		t.Fatal("an unknown forge should be an error, not a silent GitHub fetch")
	}
	if !strings.Contains(err.Error(), "gitub") {
		t.Errorf("the error should quote what was typed: %v", err)
	}
}

func TestFetchFromForgePropagatesAListingError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	withForgeSelection(t, "gitea", srv.URL)
	if _, err := fetchFromForge(context.Background(), "nobody", github.FetchOptions{}); err == nil {
		t.Error("an owner that does not exist should be an error, not an empty list")
	}
}

func TestForgeTokenSources(t *testing.T) {
	// GitHub's credential is resolved inside its own client, which knows
	// the ladder from an explicit token through the environment to the gh
	// CLI. Supplying one here would bypass all of that.
	if got := forgeToken(context.Background(), "github", github.AuthModeAuto); got != "" {
		t.Errorf("github token = %q, want empty — its client resolves its own", got)
	}

	t.Setenv("CORRAL_GITLAB_TOKEN", "")
	t.Setenv("GITLAB_TOKEN", "from-gitlab-env")
	t.Setenv("CI_JOB_TOKEN", "")
	if got := forgeToken(context.Background(), "gitlab", github.AuthModeAuto); got != "from-gitlab-env" {
		t.Errorf("gitlab token = %q", got)
	}
	// A corral-specific name wins, so somebody can point corral at a
	// different instance than their shell is already configured for.
	t.Setenv("CORRAL_GITLAB_TOKEN", "from-corral")
	if got := forgeToken(context.Background(), "gitlab", github.AuthModeAuto); got != "from-corral" {
		t.Errorf("gitlab token = %q, want the corral-specific variable to win", got)
	}

	t.Setenv("CORRAL_FORGE_TOKEN", "")
	t.Setenv("FORGEJO_TOKEN", "")
	t.Setenv("CODEBERG_TOKEN", "")
	t.Setenv("GITEA_TOKEN", "gitea-env")
	for _, name := range []string{"gitea", "forgejo", "codeberg"} {
		if got := forgeToken(context.Background(), name, github.AuthModeAuto); got != "gitea-env" {
			t.Errorf("%s token = %q", name, got)
		}
	}
}

func TestFirstEnv(t *testing.T) {
	t.Setenv("CORRAL_TEST_A", "")
	t.Setenv("CORRAL_TEST_B", "   ")
	t.Setenv("CORRAL_TEST_C", "value")
	if got := firstEnv("CORRAL_TEST_A", "CORRAL_TEST_B", "CORRAL_TEST_C"); got != "value" {
		t.Errorf("firstEnv = %q, want value — empty and blank should be skipped", got)
	}
	if got := firstEnv("CORRAL_TEST_MISSING"); got != "" {
		t.Errorf("firstEnv = %q, want empty", got)
	}
	if got := firstEnv(); got != "" {
		t.Errorf("firstEnv() = %q, want empty", got)
	}
}

// TestRunEFailsFastOnAnUnknownForge: validating at the fetch would mean a
// typo costs a workspace scan first, and then reports a failure that looks
// like it came from the network.
func TestRunEFailsFastOnAnUnknownForge(t *testing.T) {
	called := false
	old := fetchRepos
	fetchRepos = func(context.Context, string, github.FetchOptions) ([]github.Repo, error) {
		called = true
		return nil, nil
	}
	t.Cleanup(func() { fetchRepos = old })

	err := RunE(context.Background(), RunOptions{
		Owner:       "acme",
		BaseDir:     t.TempDir(),
		Concurrency: 1,
		Protocol:    "https",
		Forge:       "gitub",
	})
	if err == nil {
		t.Fatal("an unknown forge should fail the run")
	}
	if !strings.Contains(err.Error(), "gitub") {
		t.Errorf("the error should quote what was typed: %v", err)
	}
	if called {
		t.Error("the fetch should not have been attempted")
	}
}
