package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/sebastienrousseau/corral/internal/github"
	"github.com/sebastienrousseau/corral/internal/tui"
)

func main() {
	// 1. Setup options to filter public repositories sorted by stars
	opts := github.FetchOptions{
		Limit:      10,
		Visibility: "public",
		Type:       "sources",
		Sort:       "stars",
		AuthMode:   github.AuthModeAuto,
	}

	// 2. Custom mock fetch function that fetches repositories matching a query
	mockFetch := func() ([]github.Repo, error) {
		// In a real application, you would invoke: github.FetchReposWithOptions(ctx, owner, opts)
		return []github.Repo{
			{
				Name:       "corral",
				Language:   "Go",
				Visibility: "Public",
				CloneURL:   "https://github.com/sebastienrousseau/corral.git",
				PushedAt:   time.Now(),
				Stars:      120,
			},
			{
				Name:       "openclaw",
				Language:   "C++",
				Visibility: "Public",
				CloneURL:   "https://github.com/openclaw/openclaw.git",
				PushedAt:   time.Now().Add(-24 * time.Hour),
				Stars:      450,
			},
		}, nil
	}

	fmt.Println("Starting interactive repository selector...")
	ctx := context.Background()

	// 3. Launch TUI selector in AltScreen mode
	selected, ok, err := tui.RunSelector(ctx, "sebastienrousseau", opts, mockFetch)
	if err != nil {
		log.Fatalf("Selector error: %v", err)
	}

	if !ok {
		fmt.Println("Selector cancelled silently by user.")
		os.Exit(0)
	}

	// 4. Output chosen repositories
	fmt.Printf("\nSuccessfully selected %d repositories:\n", len(selected))
	for _, repo := range selected {
		fmt.Printf(" - %s (%s, %d stars)\n", repo.Name, repo.Language, repo.Stars)
	}
}
