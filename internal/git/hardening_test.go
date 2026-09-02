// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package git

import (
	"context"
	"testing"
	"time"
)

// TestWithMetadataTimeoutNilContext covers the defensive nil branch: these
// helpers are exported, so an embedder can reach them with a nil context,
// and context.WithTimeout panics on nil.
func TestWithMetadataTimeoutNilContext(t *testing.T) {
	//nolint:staticcheck // SA1012: passing nil is exactly what this guards.
	ctx, cancel := withMetadataTimeout(nil)
	defer cancel()
	if ctx == nil {
		t.Fatal("withMetadataTimeout(nil) returned a nil context")
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("withMetadataTimeout did not set a deadline")
	}
	if remaining := time.Until(deadline); remaining <= 0 || remaining > metadataTimeout {
		t.Fatalf("deadline %v is not within (0, %v]", remaining, metadataTimeout)
	}
}

// TestWithMetadataTimeoutPreservesCancellation asserts the derived context
// still unwinds when the caller's run is cancelled, which is the reason
// these calls take a context at all.
func TestWithMetadataTimeoutPreservesCancellation(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	ctx, cancel := withMetadataTimeout(parent)
	defer cancel()

	cancelParent()
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("cancelling the parent did not cancel the metadata context")
	}
}

// TestMetadataCallsRespectCancellation covers the three helpers that used to
// run with no context and could block forever on an unresponsive filesystem.
func TestMetadataCallsRespectCancellation(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	if _, err := CurrentBranch(ctx, dir); err == nil {
		t.Error("CurrentBranch ignored a cancelled context")
	}
	if _, err := RemoteOrigin(ctx, dir); err == nil {
		t.Error("RemoteOrigin ignored a cancelled context")
	}
	if !IsEmpty(ctx, dir) {
		t.Error("IsEmpty should report true when the command cannot run")
	}
}
