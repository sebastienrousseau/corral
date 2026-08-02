// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

//go:build !darwin

package engine

import "testing"

func TestPlatformFinderTagsUnsupported(t *testing.T) {
	tags, err := platformReadFinderTags("repo")
	if err != nil || tags != nil {
		t.Fatalf("platformReadFinderTags() = %v, %v", tags, err)
	}
	if err := platformWriteFinderTags("repo", []string{"Active\n2"}); err != nil {
		t.Fatal(err)
	}
}
