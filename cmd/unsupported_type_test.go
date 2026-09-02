// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package cmd

import (
	"strings"
	"testing"
)

// TestUnsupportedRepoTypeIsRefused covers the replacement of a silently
// empty result with a refusal.
//
// --type sponsored validated, ran, hit the API and returned nothing, because
// CanBeSponsored is not carried by the REST listing endpoints. An empty
// result is indistinguishable from a correct one, so the filter is now
// rejected at validation with a reason.
func TestUnsupportedRepoTypeIsRefused(t *testing.T) {
	t.Cleanup(resetFlagState)

	for _, value := range []string{"sponsored", "can be sponsored", "SPONSORED", "  Sponsored  "} {
		resetFlagState()
		repoType = value

		err := validateCommonFlags()
		if err == nil {
			t.Errorf("--type %q was accepted; it can never match", value)
			continue
		}
		msg := err.Error()
		if !strings.Contains(msg, "not supported") {
			t.Errorf("--type %q error %q does not say it is unsupported", value, msg)
		}
		if !strings.Contains(msg, "not returned by the GitHub repository listing API") {
			t.Errorf("--type %q error %q does not explain why", value, msg)
		}
		// The refusal has to point at what does work.
		for _, want := range []string{"all", "public", "forks"} {
			if !strings.Contains(msg, want) {
				t.Errorf("--type %q error %q omits supported value %q", value, msg, want)
			}
		}
	}
}

// TestSupportedRepoTypesStillValidate guards against the refusal being
// over-broad.
func TestSupportedRepoTypesStillValidate(t *testing.T) {
	t.Cleanup(resetFlagState)
	for _, value := range append([]string{""}, repoTypeValues...) {
		resetFlagState()
		repoType = value
		if err := validateCommonFlags(); err != nil {
			t.Errorf("--type %q was rejected: %v", value, err)
		}
	}
}

// TestUnsupportedTypesAreNotAlsoAdvertised keeps the two lists from
// disagreeing: a value cannot be both refused and offered as supported.
func TestUnsupportedTypesAreNotAlsoAdvertised(t *testing.T) {
	for _, supported := range repoTypeValues {
		if _, clash := unsupportedRepoTypes[supported]; clash {
			t.Errorf("%q appears in both repoTypeValues and unsupportedRepoTypes", supported)
		}
	}
}

// resetFlagState restores the package-level flag vars validateCommonFlags
// reads to values that pass, so each case exercises only what it sets.
func resetFlagState() {
	repoType = ""
	repoSort = ""
	protocol = "https"
	output = "text"
	authMode = "auto"
	visibility = "all"
	layout = ""
	concurrency = 1
	limit = 0
	cloneDepth = 0
	retryMax = 4
	retryMinBackoff = 500000000
	retryMaxBackoff = 8000000000
	apiTimeout = 30000000000
}
