// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// newSettingsCmd builds a throwaway command carrying the shared flag groups, so
// these tests exercise applySettings without mutating the package's real
// commands.
// The flag groups bind package-level variables, so building a probe command
// rewrites them. Snapshot and restore, or a value set here leaks into every
// later test in the package — which it did, and surfaced as unrelated
// "--concurrency must be >= 1" failures elsewhere.
func newSettingsCmd(t *testing.T) *cobra.Command {
	t.Helper()
	snapshot := struct {
		concurrency, limit, retryMax, cloneDepth  int
		protocol, visibility, authMode, layout    string
		repoType, repoSort, incLangs, excLangs    string
		noSync, forks, archived, blobless, single bool
		forceSync, ignoreSubs, finder             bool
	}{
		concurrency, limit, retryMax, cloneDepth,
		protocol, visibility, authMode, layout,
		repoType, repoSort, includeLanguagesCSV, excludeLanguagesCSV,
		noSync, includeForks, includeArchived, cloneBlobless, cloneSingleBranch,
		forceSync, ignoreSubmoduleErrs, finderTags,
	}
	t.Cleanup(func() {
		concurrency, limit, retryMax, cloneDepth = snapshot.concurrency, snapshot.limit, snapshot.retryMax, snapshot.cloneDepth
		protocol, visibility, authMode, layout = snapshot.protocol, snapshot.visibility, snapshot.authMode, snapshot.layout
		repoType, repoSort = snapshot.repoType, snapshot.repoSort
		includeLanguagesCSV, excludeLanguagesCSV = snapshot.incLangs, snapshot.excLangs
		noSync, includeForks, includeArchived = snapshot.noSync, snapshot.forks, snapshot.archived
		cloneBlobless, cloneSingleBranch = snapshot.blobless, snapshot.single
		forceSync, ignoreSubmoduleErrs, finderTags = snapshot.forceSync, snapshot.ignoreSubs, snapshot.finder
	})
	c := &cobra.Command{Use: "probe", Run: func(*cobra.Command, []string) {}}
	c.Flags().AddFlagSet(newFetchFlags())
	c.Flags().AddFlagSet(newCloneFlags())
	return c
}

func TestApplySettingsWritesFlagValues(t *testing.T) {
	c := newSettingsCmd(t)
	applied, err := applySettings(c, map[string]any{
		"concurrency": float64(12), // encoding/json decodes numbers as float64
		"protocol":    "ssh",
		"no-sync":     true,
		"languages":   []any{"Go", "Rust"},
	}, "test")
	if err != nil {
		t.Fatalf("applySettings: %v", err)
	}
	if len(applied) != 4 {
		t.Errorf("applied %d settings, want 4", len(applied))
	}
	for name, want := range map[string]string{
		"concurrency": "12",
		"protocol":    "ssh",
		"no-sync":     "true",
		"languages":   "Go,Rust",
	} {
		if got := c.Flags().Lookup(name).Value.String(); got != want {
			t.Errorf("--%s = %q, want %q", name, got, want)
		}
	}
}

// An explicitly-passed flag must beat the config. This is the precedence rule
// the whole layering depends on.
func TestApplySettingsDoesNotOverrideExplicitFlags(t *testing.T) {
	c := newSettingsCmd(t)
	if err := c.Flags().Parse([]string{"--concurrency", "99"}); err != nil {
		t.Fatal(err)
	}
	if _, err := applySettings(c, map[string]any{"concurrency": float64(3)}, "test"); err != nil {
		t.Fatal(err)
	}
	if got := c.Flags().Lookup("concurrency").Value.String(); got != "99" {
		t.Errorf("explicit flag lost to config: concurrency = %q, want 99", got)
	}
}

// A typo must fail loudly. A silently-ignored setting is worse than a rejected
// one, because the user believes it took effect.
func TestApplySettingsRejectsUnknownKeys(t *testing.T) {
	c := newSettingsCmd(t)
	_, err := applySettings(c, map[string]any{"concurrancy": float64(8)}, "test")
	if err == nil {
		t.Fatal("expected an error for a misspelled setting")
	}
	if !strings.Contains(err.Error(), "concurrancy") {
		t.Errorf("error should name the offending key: %v", err)
	}
}

// A key naming a flag that belongs to a different command is not a typo: one
// config file serves the whole CLI, so `mcp` legitimately sees "concurrency".
func TestApplySettingsSkipsOtherCommandsFlags(t *testing.T) {
	c := &cobra.Command{Use: "narrow", Run: func(*cobra.Command, []string) {}}
	c.Flags().Bool("only-mine", false, "")
	if _, err := applySettings(c, map[string]any{"concurrency": float64(8)}, "test"); err != nil {
		t.Fatalf("a flag owned by another command must be skipped, not rejected: %v", err)
	}
}

// An invalid value must be rejected by the flag's own parser, so a config value
// is validated exactly like a typed one.
func TestApplySettingsRejectsInvalidValues(t *testing.T) {
	c := newSettingsCmd(t)
	if _, err := applySettings(c, map[string]any{"concurrency": "not-a-number"}, "test"); err == nil {
		t.Error("expected the flag parser to reject a non-numeric concurrency")
	}
}

// TestProfileLegacyFieldsStillWork covers configs written before v0.0.21, which
// used fixed snake_case fields rather than flag-named settings.
func TestProfileLegacyFieldsStillWork(t *testing.T) {
	p := profile{
		Owners:            []string{"acme"},
		LegacyBaseDir:     "/tmp/x",
		LegacyProtocol:    "ssh",
		LegacyConcurrency: 5,
		LegacyLimit:       50,
		LegacyLayout:      "{{.Name}}",
	}
	got := p.effectiveSettings()
	for k, want := range map[string]any{
		"base-dir": "/tmp/x", "protocol": "ssh",
		"concurrency": 5, "limit": 50, "layout": "{{.Name}}",
	} {
		if got[k] != want {
			t.Errorf("legacy %s = %v, want %v", k, got[k], want)
		}
	}

	// An explicit settings entry wins over the legacy field.
	p.Settings = map[string]any{"protocol": "https"}
	if p.effectiveSettings()["protocol"] != "https" {
		t.Error("settings should take precedence over the deprecated legacy field")
	}
}

func TestSettingToString(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{"x", "x"},
		{true, "true"},
		{float64(8), "8"},
		{float64(1.5), "1.5"},
		{[]any{"a", "b"}, "a,b"},
	}
	for _, tc := range cases {
		got, err := settingToString(tc.in)
		if err != nil {
			t.Errorf("settingToString(%v): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("settingToString(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if _, err := settingToString(nil); err == nil {
		t.Error("null should be rejected")
	}
	if _, err := settingToString(map[string]any{}); err == nil {
		t.Error("object should be rejected")
	}
}

func TestWriteConfigTemplate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "config.json")
	body := defaultConfigTemplate(rootCmd)
	if err := writeConfigTemplate(path, body); err != nil {
		t.Fatalf("write: %v", err)
	}
	b, err := os.ReadFile(path) // #nosec G304 -- test-controlled path
	if err != nil {
		t.Fatal(err)
	}
	// The template must document the real flag surface, not a fixed subset.
	for _, want := range []string{"concurrency", "protocol", "clone-depth", "profiles"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("template missing %q", want)
		}
	}
	// And it must never clobber an existing file.
	if err := writeConfigTemplate(path, body); err == nil {
		t.Error("expected a refusal to overwrite an existing config")
	}
}

// knownFlagNames must span subcommands, or a setting meant for `plan` would be
// misreported as a typo when another command loads the same file.
func TestKnownFlagNamesSpansSubcommands(t *testing.T) {
	known := knownFlagNames()
	for _, name := range []string{"concurrency", "limit", "output", "enable-mutations", "config"} {
		if !known[name] {
			t.Errorf("knownFlagNames() is missing %q", name)
		}
	}
	var count int
	rootCmd.Flags().VisitAll(func(*pflag.Flag) { count++ })
	if len(known) <= count {
		t.Errorf("knownFlagNames() (%d) should exceed the root command's own flags (%d)", len(known), count)
	}
}

// TestProfileSettingsAreValidatedLikeFlags replaces the validation that used to
// live in validateProfile as a hand-written allow-list covering five fields.
// A value from a profile now goes through the flag's parser and then
// validateCommonFlags(nil), i.e. the same path as a value typed on the command
// line — so every setting is checked, not just the five that were mapped.
func TestProfileSettingsAreValidatedLikeFlags(t *testing.T) {
	cases := []struct {
		name     string
		settings map[string]any
		wantErr  string
	}{
		{"bad protocol", map[string]any{"protocol": "ftp"}, "--protocol"},
		{"negative concurrency", map[string]any{"concurrency": float64(-1)}, "--concurrency"},
		{"negative limit", map[string]any{"limit": float64(-5)}, "--limit"},
		{"bad visibility", map[string]any{"visibility": "sometimes"}, "--visibility"},
		{"bad auth", map[string]any{"auth": "telepathy"}, "--auth"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newSettingsCmd(t)
			if _, err := applySettings(c, tc.settings, "profile"); err != nil {
				// Some values are rejected by the parser itself, which is also correct.
				return
			}
			err := validateCommonFlags(nil)
			if err == nil {
				t.Fatalf("%v should have been rejected", tc.settings)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error should name %s, got: %v", tc.wantErr, err)
			}
		})
	}
}
