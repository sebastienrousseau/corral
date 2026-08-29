// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package github

import (
	"context"
	"errors"
	"strings"
	"testing"

	gh "github.com/google/go-github/v90/github"
)

func TestFetchReposWithOptionsReportsClientConstructionFailure(t *testing.T) {
	original := newGitHubClient
	t.Cleanup(func() { newGitHubClient = original })
	newGitHubClient = func(...gh.ClientOptionsFunc) (*gh.Client, error) {
		return nil, errors.New("bad option")
	}
	// Set the token explicitly rather than relying on the ambient
	// environment: token resolution runs before client construction, so a
	// machine without GITHUB_TOKEN would never reach the branch under test
	// and the gap would reopen silently in CI.
	t.Setenv("GITHUB_TOKEN", "test-token")

	_, err := FetchReposWithOptions(context.Background(), "someone", FetchOptions{
		AuthMode: AuthModeToken,
		Limit:    1,
	})
	if err == nil {
		t.Fatal("expected client construction failure to surface")
	}
	if !strings.Contains(err.Error(), "construct GitHub client") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestCollectConcurrentlyAbortsOnCancellation drives the cancellation branch
// inside the concurrent page fetcher.
//
// Determinism comes from the semaphore, not from timing. With a capacity of
// zero the send in acquirePageSlot can never proceed, so a cancelled context
// leaves ctx.Done as the only ready case in its select. Every page goroutine
// therefore takes the cancellation branch on every run and every platform;
// fetchPage is never reached, which the fixture asserts by failing if it is.
func TestCollectConcurrentlyAbortsOnCancellation(t *testing.T) {
	original := maxConcurrentPageFetches
	t.Cleanup(func() { maxConcurrentPageFetches = original })
	maxConcurrentPageFetches = 0

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	fetchPage := func(page int) ([]*gh.Repository, *gh.Response, error) {
		t.Errorf("fetchPage(%d) ran despite a cancelled context and no free slot", page)
		return nil, &gh.Response{}, nil
	}

	err := collectConcurrently(ctx, "owner", fetchPage, 12, &repoCollector{})
	if err == nil {
		t.Fatal("expected a cancellation error from the concurrent fetch")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if !strings.Contains(err.Error(), "concurrent fetch failed") {
		t.Fatalf("error does not identify the concurrent fetch: %v", err)
	}
}
