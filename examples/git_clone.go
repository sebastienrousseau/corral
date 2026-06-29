//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/sebastienrousseau/corral/internal/git"
)

func main() {
	ctx := context.Background()

	repoURL := "https://github.com/sebastienrousseau/corral.git"
	targetDir := "./tmp_corral_clone"

	defer func() {
		_ = os.RemoveAll(targetDir)
	}()

	fmt.Printf("Cloning %s into %s...\n", repoURL, targetDir)

	opts := git.CloneOptions{
		SingleBranch: true,
		Depth:        1,
	}
	err := git.Clone(ctx, repoURL, targetDir, opts)
	if err != nil {
		log.Fatalf("Git clone failed: %v", err)
	}

	branch, err := git.CurrentBranch(targetDir)
	if err != nil {
		log.Fatalf("Failed to query branch: %v", err)
	}

	remote, err := git.RemoteOrigin(targetDir)
	if err != nil {
		log.Fatalf("Failed to query remote: %v", err)
	}

	fmt.Printf("\nSuccess!\n")
	fmt.Printf(" - Active Branch: %s\n", branch)
	fmt.Printf(" - Origin URL: %s\n", remote)
}
