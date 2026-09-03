// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package github

import (
	"context"
	"sync/atomic"
	"testing"

	gh "github.com/google/go-github/v90/github"
)

// pageSource fakes a large listing and counts how many pages were actually
// requested. The count is the whole point: the cost of over-fetching is
// round-trips against a rate limit, which no timing benchmark would show.
type pageSource struct {
	lastPage int
	fetched  atomic.Int64
}

func (p *pageSource) fetcher() pageFetcher {
	return func(page int) ([]*gh.Repository, *gh.Response, error) {
		p.fetched.Add(1)
		repos := make([]*gh.Repository, 0, perPage)
		for i := 0; i < perPage; i++ {
			name := "repo"
			repos = append(repos, &gh.Repository{
				Name:     gh.Ptr(name),
				FullName: gh.Ptr("acme/" + name),
				Owner:    &gh.User{Login: gh.Ptr("acme")},
			})
		}
		next := page + 1
		if page >= p.lastPage {
			next = 0
		}
		return repos, &gh.Response{NextPage: next, LastPage: p.lastPage}, nil
	}
}

// TestConcurrentFetchStopsAtTheLimit is the PERF-3 regression. A small
// --limit against a large organisation used to fetch every page: 50 round
// trips to keep 10 repositories, on every run.
func TestConcurrentFetchStopsAtTheLimit(t *testing.T) {
	tests := []struct {
		name      string
		limit     int
		lastPage  int
		wantMax   int64
		wantRepos int
	}{
		// A limit smaller than one page is satisfied by page 1 alone, so
		// the collector arrives full and nothing more is fetched.
		{name: "tiny limit against a huge org", limit: 10, lastPage: 50, wantMax: 0, wantRepos: 10},
		{name: "limit spanning two pages", limit: 150, lastPage: 50, wantMax: 4, wantRepos: 150},
		{name: "no limit fetches everything", limit: 0, lastPage: 5, wantMax: 4, wantRepos: 500},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := &pageSource{lastPage: tc.lastPage}
			collector := &repoCollector{limit: tc.limit, opts: normalizeFetchOptions(FetchOptions{Limit: tc.limit})}
			// Page 1 is absorbed by the caller before collectConcurrently runs.
			first, _, err := src.fetcher()(1)
			if err != nil {
				t.Fatal(err)
			}
			collector.absorb(first)

			if err := collectConcurrently(context.Background(), "acme", src.fetcher(), tc.lastPage, collector); err != nil {
				t.Fatalf("collectConcurrently: %v", err)
			}

			// -1 for the page-1 fetch the harness did itself.
			concurrent := src.fetched.Load() - 1
			if concurrent > tc.wantMax {
				t.Errorf("fetched %d pages concurrently, want at most %d", concurrent, tc.wantMax)
			}
			if len(collector.repos) != tc.wantRepos {
				t.Errorf("collected %d repositories, want %d", len(collector.repos), tc.wantRepos)
			}
		})
	}
}

// TestConcurrentFetchPreservesPageOrder guards the property the old
// map-of-pages existed for: results must not depend on which worker
// finished first.
func TestConcurrentFetchPreservesPageOrder(t *testing.T) {
	names := []string{"", "", "pc", "pd", "pe", "pf"} // indexed by page number
	fetch := func(p int) ([]*gh.Repository, *gh.Response, error) {
		name := names[p]
		return []*gh.Repository{{
			Name:     gh.Ptr(name),
			FullName: gh.Ptr("acme/" + name),
			Owner:    &gh.User{Login: gh.Ptr("acme")},
		}}, &gh.Response{LastPage: 5}, nil
	}
	collector := &repoCollector{opts: normalizeFetchOptions(FetchOptions{})}
	if err := collectConcurrently(context.Background(), "acme", fetch, 5, collector); err != nil {
		t.Fatal(err)
	}
	want := names[2:] // pages 2..5, in page order
	if len(collector.repos) != len(want) {
		t.Fatalf("got %d repositories, want %d", len(collector.repos), len(want))
	}
	for i, w := range want {
		if collector.repos[i].Name != w {
			t.Errorf("position %d = %q, want %q (page order not preserved)", i, collector.repos[i].Name, w)
		}
	}
}

func TestCollectorRemaining(t *testing.T) {
	unlimited := &repoCollector{limit: 0}
	if got := unlimited.remaining(); got != 0 {
		t.Errorf("unlimited remaining = %d, want 0", got)
	}
	partial := &repoCollector{limit: 10, repos: make([]Repo, 4)}
	if got := partial.remaining(); got != 6 {
		t.Errorf("remaining = %d, want 6", got)
	}
	full := &repoCollector{limit: 10, repos: make([]Repo, 10)}
	if got := full.remaining(); got != 0 {
		t.Errorf("full remaining = %d, want 0", got)
	}
	over := &repoCollector{limit: 10, repos: make([]Repo, 12)}
	if got := over.remaining(); got != 0 {
		t.Errorf("over-full remaining = %d, want 0", got)
	}
}
