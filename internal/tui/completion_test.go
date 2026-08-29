// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package tui

import "testing"

func TestCompleteSlashCommand(t *testing.T) {
	cases := []struct {
		name     string
		filter   string
		want     string
		wantOK   bool
		whyItMat string
	}{
		{
			name:     "completes a unique prefix",
			filter:   "/ex",
			want:     "/exit",
			wantOK:   true,
			whyItMat: "tab on a partial command is the whole point of the feature",
		},
		{
			name:     "ordinary text is not a command",
			filter:   "corral",
			wantOK:   false,
			whyItMat: "tab while filtering by name must fall through to the table",
		},
		{
			name:     "empty filter is not a command",
			filter:   "",
			wantOK:   false,
			whyItMat: "tab with nothing typed must not invent a command",
		},
		{
			name:     "an already-complete command does not re-complete",
			filter:   "/help",
			wantOK:   false,
			whyItMat: "completion requires a strictly longer candidate, so /help is final",
		},
		{
			name:     "an unknown command completes to nothing",
			filter:   "/zzz",
			wantOK:   false,
			whyItMat: "no command starts with /zzz, so there is nothing to offer",
		},
		{
			name:     "a bare slash takes the first command",
			filter:   "/",
			want:     "/exit",
			wantOK:   true,
			whyItMat: "every command matches, and the first is a defensible choice",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := completeSlashCommand(tc.filter)
			if ok != tc.wantOK {
				t.Fatalf("completeSlashCommand(%q) ok = %v, want %v (%s)", tc.filter, ok, tc.wantOK, tc.whyItMat)
			}
			if ok && got != tc.want {
				t.Fatalf("completeSlashCommand(%q) = %q, want %q", tc.filter, got, tc.want)
			}
			if !ok && got != "" {
				t.Fatalf("completeSlashCommand(%q) returned %q alongside ok=false", tc.filter, got)
			}
		})
	}
}
