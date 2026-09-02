// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package github

import (
	"testing"

	gh "github.com/google/go-github/v90/github"
)

// TestMapRepositoryVisibilityFallsClosed covers the case that made a private
// repository land in Public/ on disk: an API response carrying Private but
// not Visibility. Reading only Visibility meant nil fell through to "Public",
// and visibility decides the on-disk Collection.
func TestMapRepositoryVisibilityFallsClosed(t *testing.T) {
	str := func(s string) *string { return &s }
	bl := func(b bool) *bool { return &b }

	tests := []struct {
		name       string
		private    *bool
		visibility *string
		want       string
	}{
		{"private true, visibility absent", bl(true), nil, "Private"},
		{"private false, visibility absent", bl(false), nil, "Public"},
		{"both absent", nil, nil, "Public"},
		{"private true, visibility private", bl(true), str("private"), "Private"},
		{"private false, visibility internal", bl(false), str("internal"), "Private"},
		{"private false, visibility public", bl(false), str("public"), "Public"},
		{"private true, visibility public disagrees", bl(true), str("public"), "Private"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := mapRepository(&gh.Repository{
				Name:       str("api"),
				Private:    tc.private,
				Visibility: tc.visibility,
			})
			if repo.Visibility != tc.want {
				t.Fatalf("Visibility = %q, want %q", repo.Visibility, tc.want)
			}
		})
	}
}

// TestSponsoredFilterCannotSilentlyMatch documents the contract behind the
// --type refusal in the cmd layer: CanBeSponsored is not populated by the
// REST listing endpoints, so the filter it backs must never be reachable
// with a value that would silently drop every repository.
func TestSponsoredFilterCannotSilentlyMatch(t *testing.T) {
	str := func(s string) *string { return &s }
	repo := mapRepository(&gh.Repository{Name: str("api")})
	if repo.CanBeSponsored {
		t.Fatal("CanBeSponsored is unexpectedly populated; the cmd-layer refusal should be revisited")
	}
	if matchesFilters(repo, nil, nil, FetchOptions{Type: "sponsored"}) {
		t.Fatal("sponsored filter matched a repository with CanBeSponsored=false")
	}
}
