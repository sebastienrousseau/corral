// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package forge

import (
	"strings"
	"testing"
	"time"
)

func TestRegisteredForges(t *testing.T) {
	want := []string{"bitbucket", "codeberg", "forgejo", "gitea", "github", "gitlab"}
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
		"bitbucket.org":    "bitbucket",
		// The API host and the web host both resolve, because --forge-url
		// is matched by host and a user may name either.
		"api.bitbucket.org": "bitbucket",
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
	// Comma-separated rather than concatenated: it reads as a list in a
	// failure message, and joining single letters with nothing between
	// them produces words a spell checker flags as typos.
	names := func(rs []Repo) string {
		out := make([]string, 0, len(rs))
		for _, r := range rs {
			out = append(out, r.Name)
		}
		return strings.Join(out, ",")
	}

	for name, tc := range map[string]struct {
		opts Options
		want string
	}{
		"default excludes forks and archived": {Options{}, "a,b"},
		"forks kept":                          {Options{IncludeForks: true}, "a,b,c"},
		"archived kept":                       {Options{IncludeArchived: true}, "a,b,d"},
		"both kept":                           {Options{IncludeForks: true, IncludeArchived: true}, "a,b,c,d,e"},
		"public only":                         {Options{Visibility: "public"}, "a"},
		"private only":                        {Options{Visibility: "private"}, "b"},
		"visibility all":                      {Options{Visibility: "all"}, "a,b"},
		"visibility is case-insensitive":      {Options{Visibility: "PUBLIC"}, "a"},
		"limit applies after filtering":       {Options{IncludeForks: true, Limit: 2}, "a,b"},
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

// TestOwnerPrefixesFromTheListing is the property prune and orphan
// detection rest on: which local clones belong to this owner, on this
// host. Derived from the clone URLs the listing returned, because those
// come from the instance and are therefore right for a self-hosted
// deployment and for GitLab's nested groups without anyone configuring
// anything.
func TestOwnerPrefixesFromTheListing(t *testing.T) {
	gh, _ := Get("github")
	for name, tc := range map[string]struct {
		urls []string
		want []string
	}{
		"one host": {
			urls: []string{"https://codeberg.org/forgejo/meta.git", "https://codeberg.org/forgejo/docs.git"},
			want: []string{"codeberg.org/forgejo/"},
		},
		// The owner a user types is "group"; the project lives under
		// "group/subgroup". No host-plus-owner string could produce this.
		"gitlab nested group": {
			urls: []string{"https://gitlab.com/group/subgroup/proj.git"},
			want: []string{"gitlab.com/group/subgroup/"},
		},
		"scp-style remote": {
			urls: []string{"git@codeberg.org:forgejo/meta.git"},
			want: []string{"codeberg.org/forgejo/"},
		},
		// The port is not part of the identity: git.CanonicalRemote drops
		// it, so a prefix that kept it would never match.
		"self-hosted with a port": {
			urls: []string{"https://git.example.com:8443/team/tool.git"},
			want: []string{"git.example.com/team/"},
		},
		"mixed namespaces are all kept": {
			urls: []string{
				"https://gitlab.com/a/one.git",
				"https://gitlab.com/b/two.git",
			},
			want: []string{"gitlab.com/a/", "gitlab.com/b/"},
		},
		"unusable urls are ignored": {
			urls: []string{"", "   ", "not-a-url", "https://host-only.example", "https://codeberg.org/forgejo/meta.git"},
			want: []string{"codeberg.org/forgejo/"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			got := OwnerPrefixes(gh, "ignored", "", tc.urls)
			if len(got) != len(tc.want) {
				t.Fatalf("OwnerPrefixes = %v, want %v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("OwnerPrefixes = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

// TestOwnerPrefixesFallsBackToTheForgeHosts covers the case that matters
// most: an empty listing is exactly when every local clone is an orphan,
// so a prefix that fails to match there does not miss one repository, it
// silently disables the comparison.
func TestOwnerPrefixesFallsBackToTheForgeHosts(t *testing.T) {
	gh, _ := Get("github")
	if got := OwnerPrefixes(gh, "acme", "", nil); len(got) != 1 || got[0] != "github.com/acme/" {
		t.Errorf("OwnerPrefixes = %v, want [github.com/acme/]", got)
	}

	// A self-hosted instance has no declared host, so the base URL is the
	// only thing that can supply one.
	gitea, _ := Get("gitea")
	got := OwnerPrefixes(gitea, "acme", "https://git.example.com", nil)
	if len(got) != 1 || got[0] != "git.example.com/acme/" {
		t.Errorf("OwnerPrefixes = %v, want [git.example.com/acme/]", got)
	}

	// Nothing to go on at all: no prefixes, which matches nothing, which
	// is the safe direction for an operation whose answer is rm -rf.
	if got := OwnerPrefixes(gitea, "acme", "", nil); len(got) != 0 {
		t.Errorf("OwnerPrefixes = %v, want none", got)
	}
	if got := OwnerPrefixes(nil, "", "", nil); len(got) != 0 {
		t.Errorf("OwnerPrefixes = %v, want none", got)
	}
	if got := OwnerPrefixes(nil, "  /  ", "", nil); len(got) != 0 {
		t.Errorf("a blank owner should yield nothing, got %v", got)
	}
}

func TestMatchesOwner(t *testing.T) {
	prefixes := []string{"codeberg.org/forgejo/", "gitlab.com/group/sub/"}

	for _, id := range []string{
		"codeberg.org/forgejo/meta",
		"CODEBERG.ORG/forgejo/meta",
		"gitlab.com/group/sub/proj",
	} {
		if !MatchesOwner(id, prefixes) {
			t.Errorf("MatchesOwner(%q) = false, want true", id)
		}
	}

	for _, id := range []string{
		// The same owner name on a different host is a different
		// repository. Matching it is how a GitLab clone once counted as a
		// GitHub orphan.
		"github.com/forgejo/meta",
		"codeberg.org/someone-else/meta",
		"gitlab.com/group/other/proj",
		"",
	} {
		if MatchesOwner(id, prefixes) {
			t.Errorf("MatchesOwner(%q) = true, want false", id)
		}
	}

	// No prefixes matches nothing, whatever the identity.
	if MatchesOwner("codeberg.org/forgejo/meta", nil) {
		t.Error("an empty prefix set must match nothing")
	}
}
