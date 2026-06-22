package git_test

import (
	"context"
	"log"

	"github.com/sebastienrousseau/corral/internal/git"
)

// ExampleClone demonstrates a shallow, single-branch, blobless partial clone —
// the combination Corral uses to minimise bandwidth and disk for large mirrors.
func ExampleClone() {
	ctx := context.Background()
	opts := git.CloneOptions{
		SingleBranch: true,
		Blobless:     true,
		Depth:        1,
	}
	if err := git.Clone(ctx, "https://github.com/sebastienrousseau/corral.git", "/tmp/corral", opts); err != nil {
		log.Printf("clone failed: %v", err)
	}
}

// ExamplePull demonstrates updating an existing clone with rebase + autostash,
// recursing into submodules.
func ExamplePull() {
	ctx := context.Background()
	if err := git.Pull(ctx, "/tmp/corral", true); err != nil {
		log.Printf("pull failed: %v", err)
	}
}
