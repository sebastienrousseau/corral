package main

import (
	"context"
	"fmt"
	"log"

	"github.com/sebastienrousseau/corral/internal/github"
)

func main() {
	ctx := context.Background()

	// 1. Configure options to fetch top 5 Go repositories sorted by stars
	opts := github.FetchOptions{
		Limit:            5,
		Visibility:       "public",
		Type:             "sources",
		Sort:             "stars",
		IncludeLanguages: []string{"Go"},
		AuthMode:         github.AuthModeAuto,
	}

	fmt.Println("Fetching public Go repositories for 'sebastienrousseau'...")

	// 2. Fetch the repositories via the GitHub client API wrapper
	repos, err := github.FetchReposWithOptions(ctx, "sebastienrousseau", opts)
	if err != nil {
		log.Fatalf("Error fetching repositories: %v", err)
	}

	// 3. Print the results
	fmt.Printf("\nFetched %d repositories successfully:\n", len(repos))
	for _, r := range repos {
		fmt.Printf(" - Name: %s | Language: %s | Visibility: %s | Stars: %d | Archived: %v\n",
			r.Name, r.Language, r.Visibility, r.Stars, r.Archived)
	}
}
