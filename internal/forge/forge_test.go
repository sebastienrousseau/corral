// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package forge

import (
	"strings"
	"testing"
	"time"
)

func TestRegisteredForges(t *testing.T) {
	want := []string{"codeberg", "forgejo", "gitea", "github", "gitlab"}
	got := Names()
	if len(got) != len(want) {
		t.Fatalf("Names = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Names = %v, want %v (sorted)", got, want)
		}
	}
}

func TestGet(t *testing.T) {
	for _, name := range Names() {
		f, err := Get(name)
		if err != nil {
			t.Fatalf("Get(%q): %v", name, err)
		}
		if f.Name() != name {
			t.Errorf("Get(%q).Name() = %q", name, f.Name())
		}
	}
	// Case and surrounding space are a user's typing, not a different
	// forge.
	if _, err := Get("  GitHub "); err != nil {
		t.Errorf("Get should normalise case and space: %v", err)
	}

	// An unknown name must say what is known. A silent fallback to GitHub
	// would clone the wrong thing from the wrong place.
	_, err := Get("gitub")
	if err == nil {
		t.Fatal("an unknown forge should be an error")
	}
	for _, want := range []string{"gitub", "github", "gitlab"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error should mention %q: %v", want, err)
		}
	}
}

func TestRegisterRejectsADuplicate(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("registering a duplicate name should panic")
		}
	}()
	Register(GitHub{})
}

func TestForHost(t *testing.T) {
	for host, want := range map[string]string{
		"github.com":       "github",
		"GitHub.com":       "github",
		"www.github.com":   "github",
		"github.com:443":   "github",
		"gitlab.com":       "gitlab",
		"codeberg.org":     "codeberg",
		"api.codeberg.org": "codeberg",
	} {
		f, ok := ForHost(host)
		if !ok {
			t.Errorf("ForHost(%q) found nothing", host)
			continue
		}
		if f.Name() != want {
			t.Errorf("ForHost(%q) = %q, want %q", host, f.Name(), want)
		}
	}

	// A self-hosted instance cannot be guessed — Gitea, Forgejo and GitLab
	// all serve arbitrary domains — so an unknown host is unknown rather
	// than assumed.
	for _, host := range []string{
		"git.example.com", "gitlab.example.com", "example.com", "", "   ", "localhost",
	} {
		if f, ok := ForHost(host); ok {
			t.Errorf("ForHost(%q) = %q, want no match", host, f.Name())
		}
	}
}

func TestForRemoteURL(t *testing.T) {
	for remote, want := range map[string]string{
		"https://github.com/owner/repo.git":   "github",
		"http://github.com/owner/repo":        "github",
		"git@github.com:owner/repo.git":       "github",
		"ssh://git@github.com/owner/repo.git": "github",
		"https://gitlab.com/group/sub/p.git":  "gitlab",
		"git@codeberg.org:owner/repo.git":     "codeberg",
	} {
		f, ok := ForRemoteURL(remote)
		if !ok {
			t.Errorf("ForRemoteURL(%q) found nothing", remote)
			continue
		}
		if f.Name() != want {
			t.Errorf("ForRemoteURL(%q) = %q, want %q", remote, f.Name(), want)
		}
	}

	for _, remote := range []string{
		"", "   ", "not a url at all", "git@git.example.com:owner/repo.git",
		"/local/path/repo", "https://",
		// A colon before the @ is not the scp form; reading it as one
		// would take a hostname out of a username.
		"weird:host@example.com",
	} {
		if f, ok := ForRemoteURL(remote); ok {
			t.Errorf("ForRemoteURL(%q) = %q, want no match", remote, f.Name())
		}
	}
}

func TestResolve(t *testing.T) {
	// An explicit name always wins.
	f, err := Resolve("gitlab", "https://codeberg.org")
	if err != nil || f.Name() != "gitlab" {
		t.Errorf("an explicit --forge should win: %v %v", f, err)
	}

	// Otherwise the URL decides, which is what makes --forge-url alone
	// work for a known host.
	f, err = Resolve("", "https://codeberg.org")
	if err != nil || f.Name() != "codeberg" {
		t.Errorf("the URL should select the forge: %v %v", f, err)
	}

	// Neither: GitHub, because that is what corral did before this
	// package existed.
	f, err = Resolve("", "")
	if err != nil || f.Name() != "github" {
		t.Errorf("the default should be github: %v %v", f, err)
	}

	// A self-hosted instance on an unrecognised domain must ask rather
	// than guess, and the message must say what to pass.
	_, err = Resolve("", "https://git.example.com")
	if err == nil {
		t.Fatal("an unrecognisable instance should be an error")
	}
	for _, want := range []string{"--forge", "gitea", "gitlab"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error should mention %q: %v", want, err)
		}
	}

	if _, err := Resolve("nope", ""); err == nil {
		t.Error("an unknown forge name should be an error")
	}
}

func TestNormalizeVisibility(t *testing.T) {
	for _, tc := range []struct {
		private bool
		raw     string
		want    string
	}{
		{true, "", "Private"},
		{true, "public", "Private"}, // the boolean wins
		{false, "private", "Private"},
		// GitLab's "internal" is visible to any signed-in user of the
		// instance. It is not public, and the layout has two directories.
		{false, "internal", "Private"},
		{false, "INTERNAL", "Private"},
		{false, "public", "Public"},
		{false, "", "Public"},
		{false, "something-new", "Public"},
	} {
		if got := NormalizeVisibility(tc.private, tc.raw); got != tc.want {
			t.Errorf("NormalizeVisibility(%t, %q) = %q, want %q", tc.private, tc.raw, got, tc.want)
		}
	}
}

func TestNormalizeLanguage(t *testing.T) {
	for in, want := range map[string]string{
		"":     "Other",
		"   ":  "Other",
		"Go":   "Go",
		"Rust": "Rust",
	} {
		if got := NormalizeLanguage(in); got != want {
			t.Errorf("NormalizeLanguage(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestFilterIsOneDefinitionForEveryForge is why filtering happens here
// rather than as API query parameters: each forge spells these
// differently, and one of them applies exclusions before pagination.
func TestFilterIsOneDefinitionForEveryForge(t *testing.T) {
	all := []Repo{
		{Name: "a", Visibility: "Public"},
		{Name: "b", Visibility: "Private"},
		{Name: "c", Visibility: "Public", Fork: true},
		{Name: "d", Visibility: "Public", Archived: true},
		{Name: "e", Visibility: "Private", Fork: true, Archived: true},
	}
	names := func(rs []Repo) string {
		var b strings.Builder
		for _, r := range rs {
			b.WriteString(r.Name)
		}
		return b.String()
	}

	for name, tc := range map[string]struct {
		opts Options
		want string
	}{
		"default excludes forks and archived": {Options{}, "ab"},
		"forks kept":                          {Options{IncludeForks: true}, "abc"},
		"archived kept":                       {Options{IncludeArchived: true}, "abd"},
		"both kept":                           {Options{IncludeForks: true, IncludeArchived: true}, "abcde"},
		"public only":                         {Options{Visibility: "public"}, "a"},
		"private only":                        {Options{Visibility: "private"}, "b"},
		"visibility all":                      {Options{Visibility: "all"}, "ab"},
		"visibility is case-insensitive":      {Options{Visibility: "PUBLIC"}, "a"},
		"limit applies after filtering":       {Options{IncludeForks: true, Limit: 2}, "ab"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := names(Filter(all, tc.opts)); got != tc.want {
				t.Errorf("Filter = %q, want %q", got, tc.want)
			}
		})
	}

	// Filter must not write into its input: the caller may still be
	// holding the full list.
	before := len(all)
	_ = Filter(all, Options{})
	if len(all) != before || all[2].Name != "c" {
		t.Error("Filter modified the slice it was given")
	}
}

func TestHostFromRemote(t *testing.T) {
	for remote, want := range map[string]string{
		"https://github.com/o/r.git":   "github.com",
		"git@github.com:o/r.git":       "github.com",
		"github.com:o/r.git":           "github.com",
		"ssh://git@host.tld:22/o/r":    "host.tld",
		"":                             "",
		"   ":                          "",
		"no-colon-no-scheme":           "",
		"https://user:pw@host.tld/o/r": "host.tld",
	} {
		if got := hostFromRemote(remote); got != want {
			t.Errorf("hostFromRemote(%q) = %q, want %q", remote, got, want)
		}
	}
	// A URL url.Parse rejects outright.
	if got := hostFromRemote("https://%zz"); got != "" {
		t.Errorf("hostFromRemote on an unparseable URL = %q, want empty", got)
	}
}

func TestTrimLastSegment(t *testing.T) {
	for in, want := range map[string]string{
		"group/sub/project": "group/sub",
		"group/project":     "group",
		"project":           "project",
		"":                  "",
	} {
		if got := trimLastSegment(in); got != want {
			t.Errorf("trimLastSegment(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBackoffIsBoundedAndGrowing(t *testing.T) {
	prev := time.Duration(0)
	for attempt := 1; attempt <= 10; attempt++ {
		d := backoff(attempt)
		if d < prev {
			t.Errorf("backoff(%d) = %v, less than backoff(%d) = %v", attempt, d, attempt-1, prev)
		}
		if d > 8*time.Second {
			t.Errorf("backoff(%d) = %v, above the cap", attempt, d)
		}
		prev = d
	}
}

func TestSnippetBoundsAndStripsControlCharacters(t *testing.T) {
	// A forge's error body is written by whoever runs the instance, and it
	// ends up in a terminal and in an agent's context.
	got := snippet([]byte("bad\x1b[2Krequest\n\x00"))
	if strings.ContainsAny(got, "\x1b\x00\n") {
		t.Errorf("control characters survived: %q", got)
	}
	if got := snippet([]byte("")); got != "(no message)" {
		t.Errorf("an empty body should say so, got %q", got)
	}
	long := snippet([]byte(strings.Repeat("x", 500)))
	if len(long) > 210 {
		t.Errorf("a long body was not bounded: %d bytes", len(long))
	}
}

func TestRedactRemovesCredentials(t *testing.T) {
	got := redact("https://user:hunter2@git.example.com/api/v1/users/x/repos")
	if strings.Contains(got, "hunter2") {
		t.Errorf("a password survived redaction: %q", got)
	}
	if got := redact("://not a url"); got != "the request URL" {
		t.Errorf("an unparseable URL should degrade, got %q", got)
	}
}
