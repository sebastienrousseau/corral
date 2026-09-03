// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package cmd

import (
	"fmt"
	"strings"
	"testing"
)

// readOnlyTools is the read-only tool set the MCP server registers.
//
// Kept here as the one place the help text is checked against. The count
// in that text has now drifted twice — it said "five" while seven were
// registered, and the two symbol tools were absent entirely — which is a
// documentation bug that matters more than most: `corralctl mcp --help` is
// how someone discovers what the server can do, and it is the text the
// generated manpage carries.
//
// Adding a tool means adding it here, which fails this test until the help
// text is updated too.
var readOnlyTools = []string{
	"corral_find_symbol",
	"corral_repo_overview",
	"corral_list_repos",
	"corral_find_repo",
	"corral_get_repo_metadata",
	"corral_status_summary",
	"corral_workspace_index",
}

// mutationTools are registered only behind --enable-mutations.
var mutationTools = []string{
	"corral_sync_repo",
	"corral_clone_repo",
	"corral_delete_repo",
}

func TestMCPHelpListsEveryTool(t *testing.T) {
	help := mcpCmd.Long

	for _, name := range append(append([]string{}, readOnlyTools...), mutationTools...) {
		if !strings.Contains(help, name) {
			t.Errorf("`corralctl mcp --help` does not mention %s", name)
		}
	}
}

// TestMCPHelpCountIsAccurate pins the numeral, which is the part that
// actually went wrong: the list can be right while the sentence above it
// says something else.
func TestMCPHelpCountIsAccurate(t *testing.T) {
	help := mcpCmd.Long

	spelled := map[int]string{
		4: "four", 5: "five", 6: "six", 7: "seven",
		8: "eight", 9: "nine", 10: "ten",
	}
	want, ok := spelled[len(readOnlyTools)]
	if !ok {
		t.Fatalf("no spelling for %d tools; extend the table", len(readOnlyTools))
	}
	claim := fmt.Sprintf("%s read-only tools", want)
	if !strings.Contains(help, claim) {
		t.Errorf("help should say %q for %d read-only tools", claim, len(readOnlyTools))
	}

	// Any other spelled number in front of "read-only tools" is a stale
	// claim that happens to sit beside a correct list.
	for n, word := range spelled {
		if n == len(readOnlyTools) {
			continue
		}
		if strings.Contains(help, word+" read-only tools") {
			t.Errorf("help still claims %q read-only tools", word)
		}
	}
}

// TestMCPHelpNamesTheDifferentiator: the cross-repository lookup is the
// capability a single-repository code index cannot offer, and the help is
// where someone finds that out.
func TestMCPHelpNamesTheDifferentiator(t *testing.T) {
	help := mcpCmd.Long
	for _, want := range []string{"corral_find_symbol", "across"} {
		if !strings.Contains(help, want) {
			t.Errorf("help should explain the cross-repository lookup; missing %q", want)
		}
	}
}
