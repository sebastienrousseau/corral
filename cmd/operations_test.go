// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sebastienrousseau/corral/internal/engine"
	"github.com/sebastienrousseau/corral/internal/github"
	"github.com/spf13/cobra"
)

func makeLocalRepo(t *testing.T, root, rel, remote string) string {
	t.Helper()
	dir := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o750); err != nil {
		t.Fatal(err)
	}
	config := "[remote \"origin\"]\n\turl = " + remote + "\n"
	if err := os.WriteFile(filepath.Join(dir, ".git", "config"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestStatusCommandTextAndJSON(t *testing.T) {
	root := t.TempDir()
	makeLocalRepo(t, root, "Public/go/repo", "https://github.com/acme/repo.git")
	oldBase, oldOutput := baseDir, statusOutput
	t.Cleanup(func() { baseDir, statusOutput = oldBase, oldOutput })
	baseDir = root

	statusOutput = "text"
	out := captureStdout(t, func() {
		if err := statusCmd.RunE(statusCmd, nil); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "repo") || !strings.Contains(out, "dirty") {
		t.Fatalf("unexpected text status: %q", out)
	}

	statusOutput = "json"
	out = captureStdout(t, func() {
		if err := statusCmd.RunE(statusCmd, []string{root}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, `"remote": "https://github.com/acme/repo.git"`) {
		t.Fatalf("unexpected json status: %q", out)
	}
	statusOutput = "yaml"
	if err := statusCmd.PreRunE(statusCmd, nil); err == nil {
		t.Fatal("expected invalid status output to fail")
	}
}

func TestPlanCommandUsesDryRun(t *testing.T) {
	oldRun, oldOutput := engineRun, planOutput
	t.Cleanup(func() { engineRun, planOutput = oldRun, oldOutput })
	var got engine.RunOptions
	engineRun = func(ctx context.Context, opts engine.RunOptions) { got = opts }
	planOutput = "json"
	planCmd.Run(planCmd, []string{"acme"})
	if !got.DryRun || got.Owner != "acme" || got.Output != engine.OutputJSON {
		t.Fatalf("unexpected plan options: %+v", got)
	}
	planOutput = "xml"
	if err := planCmd.PreRunE(planCmd, nil); err == nil {
		t.Fatal("expected invalid plan output to fail")
	}
}

func TestPruneRequiresConfirmationAndScopesOwner(t *testing.T) {
	root := t.TempDir()
	orphan := makeLocalRepo(t, root, "Public/go/orphan", "git@github.com:acme/orphan.git")
	makeLocalRepo(t, root, "Public/go/other", "https://github.com/other/other.git")
	oldBase, oldDry, oldYes, oldOutput := baseDir, dryRun, assumeYes, pruneOutput
	oldFetch, oldCheck, oldRemove := opsFetchRepos, localStateCheck, removeAll
	t.Cleanup(func() {
		baseDir, dryRun, assumeYes, pruneOutput = oldBase, oldDry, oldYes, oldOutput
		opsFetchRepos, localStateCheck, removeAll = oldFetch, oldCheck, oldRemove
	})
	baseDir, dryRun, assumeYes, pruneOutput = root, false, false, "text"
	if err := pruneCmd.RunE(pruneCmd, []string{"acme"}); err == nil {
		t.Fatal("expected confirmation refusal")
	}
	opsFetchRepos = func(ctx context.Context, owner string, opts github.FetchOptions) ([]github.Repo, error) {
		return nil, nil
	}
	localStateCheck = func(ctx context.Context, path string) (bool, string) { return false, "" }
	var removed []string
	removeAll = func(path string) error { removed = append(removed, path); return nil }
	assumeYes = true
	if err := pruneCmd.RunE(pruneCmd, []string{"acme"}); err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0] != orphan {
		t.Fatalf("removed paths = %v, want only %s", removed, orphan)
	}
}

func TestPruneDryRunAndRefusalFailJSON(t *testing.T) {
	root := t.TempDir()
	orphan := makeLocalRepo(t, root, "Public/go/orphan", "https://github.com/acme/orphan.git")
	oldBase, oldDry, oldYes, oldOutput := baseDir, dryRun, assumeYes, pruneOutput
	oldFetch, oldCheck := opsFetchRepos, localStateCheck
	t.Cleanup(func() {
		baseDir, dryRun, assumeYes, pruneOutput = oldBase, oldDry, oldYes, oldOutput
		opsFetchRepos, localStateCheck = oldFetch, oldCheck
	})
	baseDir, dryRun, assumeYes = root, true, false
	opsFetchRepos = func(ctx context.Context, owner string, opts github.FetchOptions) ([]github.Repo, error) {
		return nil, nil
	}
	localStateCheck = func(ctx context.Context, path string) (bool, string) { return false, "" }
	out := captureStdout(t, func() {
		if err := pruneCmd.RunE(pruneCmd, []string{"acme"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "DRY-RUN") || !strings.Contains(out, orphan) {
		t.Fatalf("unexpected dry-run output: %q", out)
	}

	dryRun, pruneOutput = false, "json"
	assumeYes = true
	localStateCheck = func(ctx context.Context, path string) (bool, string) { return true, "local work" }
	out = captureStdout(t, func() {
		if err := pruneCmd.RunE(pruneCmd, []string{"acme"}); err == nil {
			t.Fatal("expected refused prune to fail")
		}
	})
	if !strings.Contains(out, "REFUSE") {
		t.Fatalf("unexpected refusal output: %q", out)
	}
}

func TestConfigAndProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{"profiles":{"team":{"owners":["one","two"],"base_dir":"/tmp/repos","layout":"{{.Owner}}/{{.Name}}","protocol":"ssh","concurrency":3,"limit":9}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	oldPath, oldRun, oldOutput := configPath, engineRun, output
	t.Cleanup(func() { configPath, engineRun, output = oldPath, oldRun, oldOutput })
	configPath, output = path, "json"
	var got []engine.RunOptions
	engineRun = func(ctx context.Context, opts engine.RunOptions) { got = append(got, opts) }
	if err := profileCmd.RunE(profileCmd, []string{"team"}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1].Owner != "two" || got[0].Protocol != "ssh" || got[0].Fetch.Limit != 9 {
		t.Fatalf("unexpected profile runs: %+v", got)
	}
	out := captureStdout(t, func() {
		if err := configCmd.RunE(configCmd, nil); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, `"team"`) {
		t.Fatalf("unexpected config output: %q", out)
	}
	if err := profileCmd.RunE(profileCmd, []string{"missing"}); err == nil {
		t.Fatal("expected missing profile error")
	}
}

func TestConfigValidationAndDefaults(t *testing.T) {
	// validateProfile owns the profile's own structure. Per-setting validity is
	// enforced by the flag parsers and validateCommonFlags(), so a bad protocol
	// or a negative concurrency is now rejected by exactly the same checks that
	// apply to a value typed on the command line — see
	// TestProfileSettingsAreValidatedLikeFlags below.
	for name, configured := range map[string]profile{
		"empty":         {},
		"owner":         {Owners: []string{""}},
		"blank-setting": {Owners: []string{"a"}, Settings: map[string]any{" ": 1}},
	} {
		if err := validateProfile(name, configured); err == nil {
			t.Fatalf("expected %s profile to fail", name)
		}
	}
	if err := validateProfile("ok", profile{
		Owners:   []string{"a"},
		Settings: map[string]any{"protocol": "https"},
	}); err != nil {
		t.Fatal(err)
	}
	xdgDir := filepath.Join(string(filepath.Separator), "tmp", "xdg")
	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	if got := defaultConfigPath(); got != filepath.Join(xdgDir, "corral", "config.json") {
		t.Fatalf("default config path = %q", got)
	}
}

func TestOperationalErrorBranches(t *testing.T) {
	oldBase, oldDry, oldYes, oldPrune := baseDir, dryRun, assumeYes, pruneOutput
	oldFetch, oldCheck, oldRemove := opsFetchRepos, localStateCheck, removeAll
	t.Cleanup(func() {
		baseDir, dryRun, assumeYes, pruneOutput = oldBase, oldDry, oldYes, oldPrune
		opsFetchRepos, localStateCheck, removeAll = oldFetch, oldCheck, oldRemove
	})
	baseDir = filepath.Join(t.TempDir(), "missing")
	statusOutput = "text"
	if err := statusCmd.RunE(statusCmd, nil); err == nil {
		t.Fatal("expected status scan error")
	}
	dryRun, assumeYes = false, true
	opsFetchRepos = func(context.Context, string, github.FetchOptions) ([]github.Repo, error) {
		return nil, errors.New("fetch failed")
	}
	if err := pruneCmd.RunE(pruneCmd, []string{"acme"}); err == nil || !strings.Contains(err.Error(), "fetch failed") {
		t.Fatalf("expected prune fetch error, got %v", err)
	}
	opsFetchRepos = func(context.Context, string, github.FetchOptions) ([]github.Repo, error) { return nil, nil }
	if err := pruneCmd.RunE(pruneCmd, []string{"acme"}); err == nil {
		t.Fatal("expected prune scan error")
	}

	root := t.TempDir()
	orphan := makeLocalRepo(t, root, "Public/go/orphan", "https://github.com/acme/orphan.git")
	baseDir = root
	localStateCheck = func(context.Context, string) (bool, string) { return false, "" }
	removeAll = func(path string) error { return errors.New("remove failed") }
	out := captureStdout(t, func() {
		if err := pruneCmd.RunE(pruneCmd, []string{" ACME "}); err == nil {
			t.Fatal("expected remove failure")
		}
	})
	if !strings.Contains(out, "remove failed") || !strings.Contains(out, orphan) {
		t.Fatalf("unexpected remove failure output: %q", out)
	}

	// Exercise upstream identity fallbacks and skip an extant repository.
	removeAll = func(path string) error { t.Fatalf("must not remove upstream repo %s", path); return nil }
	opsFetchRepos = func(context.Context, string, github.FetchOptions) ([]github.Repo, error) {
		return []github.Repo{{Owner: "acme", Name: "orphan"}, {Name: "ignored"}}, nil
	}
	if err := pruneCmd.RunE(pruneCmd, []string{"acme"}); err != nil {
		t.Fatal(err)
	}

	pruneOutput = "yaml"
	if err := pruneCmd.PreRunE(pruneCmd, nil); err == nil {
		t.Fatal("expected invalid prune output")
	}
}

func TestConfigLoadFailuresAndFallbacks(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.json")
	if _, err := loadConfig(missing); err == nil || !strings.Contains(err.Error(), "open config") {
		t.Fatalf("expected config open error, got %v", err)
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte(`{"unknown":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(bad); err == nil || !strings.Contains(err.Error(), "decode config") {
		t.Fatalf("expected config decode error, got %v", err)
	}

	oldHome := userHomeDir
	oldBase := baseDir
	t.Cleanup(func() { userHomeDir, baseDir = oldHome, oldBase })
	_ = os.Unsetenv("XDG_CONFIG_HOME")
	homeDir := filepath.Join(string(filepath.Separator), "home", "test")
	userHomeDir = func() (string, error) { return homeDir, nil }
	if got := defaultConfigPath(); got != filepath.Join(homeDir, ".config", "corral", "config.json") {
		t.Fatalf("home config path = %q", got)
	}
	userHomeDir = func() (string, error) { return "", errors.New("no home") }
	if got := defaultConfigPath(); got != filepath.Join(".", ".config", "corral", "config.json") {
		t.Fatalf("fallback config path = %q", got)
	}
	baseDir = ""
	if got := resolvedBaseDir(nil); got != filepath.Join(".", "Code") {
		t.Fatalf("resolved fallback base = %q", got)
	}
	if got := resolvedBaseDir([]string{"explicit"}); got != "explicit" {
		t.Fatalf("resolved explicit base = %q", got)
	}

	closed, err := os.CreateTemp(t.TempDir(), "closed")
	if err != nil {
		t.Fatal(err)
	}
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(closed, map[string]string{"a": "b"}); err == nil {
		t.Fatal("expected JSON write failure")
	}
}

func TestOperationalRunOptionsIncludesAllSettings(t *testing.T) {
	oldTimeout := apiTimeout
	oldOutput := output
	t.Cleanup(func() { apiTimeout, output = oldTimeout, oldOutput })
	apiTimeout = 17 * time.Second
	opts := operationalRunOptions("acme", true, engine.OutputNDJSON)
	if opts.Owner != "acme" || !opts.DryRun || opts.Output != engine.OutputNDJSON || opts.Fetch.Timeout != 17*time.Second {
		t.Fatalf("unexpected operational options: %+v", opts)
	}
	output = "xml"
	if err := profileCmd.PreRunE(profileCmd, nil); err == nil {
		t.Fatal("expected invalid profile output")
	}
}

func TestOperationalCommandValidationSuccess(t *testing.T) {
	oldStatus, oldPlan, oldPrune, oldOutput := statusOutput, planOutput, pruneOutput, output
	t.Cleanup(func() { statusOutput, planOutput, pruneOutput, output = oldStatus, oldPlan, oldPrune, oldOutput })
	statusOutput, planOutput, pruneOutput, output = "text", "ndjson", "json", "json"
	for name, validate := range map[string]func(*cobra.Command, []string) error{
		"status": statusCmd.PreRunE, "plan": planCmd.PreRunE,
		"prune": pruneCmd.PreRunE, "profile": profileCmd.PreRunE,
	} {
		if err := validate(nil, nil); err != nil {
			t.Fatalf("%s validation: %v", name, err)
		}
	}
}

func TestProfileAndConfigErrors(t *testing.T) {
	oldPath := configPath
	t.Cleanup(func() { configPath = oldPath })
	configPath = filepath.Join(t.TempDir(), "missing.json")
	if err := profileCmd.RunE(profileCmd, []string{"x"}); err == nil {
		t.Fatal("expected profile config load error")
	}
	if err := configCmd.RunE(configCmd, nil); err == nil {
		t.Fatal("expected config load error")
	}
	path := filepath.Join(t.TempDir(), "invalid-profile.json")
	if err := os.WriteFile(path, []byte(`{"profiles":{"bad":{"owners":[]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath = path
	if err := profileCmd.RunE(profileCmd, []string{"bad"}); err == nil {
		t.Fatal("expected selected profile validation error")
	}
	if err := configCmd.RunE(configCmd, nil); err == nil {
		t.Fatal("expected config profile validation error")
	}
}

func TestPruneJSONWriteFailure(t *testing.T) {
	root := t.TempDir()
	oldBase, oldDry, oldYes, oldOutput := baseDir, dryRun, assumeYes, pruneOutput
	oldFetch := opsFetchRepos
	t.Cleanup(func() {
		baseDir, dryRun, assumeYes, pruneOutput = oldBase, oldDry, oldYes, oldOutput
		opsFetchRepos = oldFetch
	})
	baseDir, dryRun, assumeYes, pruneOutput = root, true, false, "json"
	opsFetchRepos = func(context.Context, string, github.FetchOptions) ([]github.Repo, error) { return nil, nil }
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	oldStdout := os.Stdout
	os.Stdout = w
	err = pruneCmd.RunE(pruneCmd, []string{"acme"})
	os.Stdout = oldStdout
	_ = w.Close()
	if err == nil {
		t.Fatal("expected prune JSON write failure")
	}
}

func TestLoadConfigDefaultPath(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	path := filepath.Join(root, "corral", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"profiles":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(""); err != nil {
		t.Fatal(err)
	}
}
