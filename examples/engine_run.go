//go:build ignore

// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package main

import (
	"context"
	"fmt"

	"github.com/sebastienrousseau/corral/internal/engine"
	"github.com/sebastienrousseau/corral/internal/github"
)

func main() {
	ctx := context.Background()

	opts := engine.RunOptions{
		Owner:       "sebastienrousseau",
		BaseDir:     "./my_local_mirror",
		Concurrency: 4,
		DryRun:      true,
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
	engine.Run(ctx, opts)
	fmt.Println("\nDry run completed successfully.")
}
