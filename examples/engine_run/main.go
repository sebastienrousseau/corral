package main

import (
	"context"
	"fmt"

	"github.com/sebastienrousseau/corral/internal/engine"
	"github.com/sebastienrousseau/corral/internal/github"
)

func main() {
	ctx := context.Background()

	// 1. Setup options for a concurrency-limited dry run
	opts := engine.RunOptions{
		Owner:       "sebastienrousseau",
		BaseDir:     "./my_local_mirror",
		Concurrency: 4,
		DryRun:      true, // Preview actions only, no changes will be written
		Protocol:    "https",
		DoSync:      true,
		Output:      engine.OutputText,
		Layout:      "{{.Visibility}}/{{.Language}}/{{.Name}}",
		Fetch: github.FetchOptions{
			Limit:      10,
			Visibility: "public",
			Type:       "sources",
			Sort:       "stars",
			AuthMode:   github.AuthModeAuto,
		},
	}

	fmt.Println("Running Corral organization engine in dry-run mode...")
	
	// 2. Execute the engine
	engine.Run(ctx, opts)

	fmt.Println("\nDry run completed successfully.")
}
