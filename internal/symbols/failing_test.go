// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package symbols

import (
	"context"
	"errors"
	"testing"
)

// stubExtractor stands in for a language whose parser can fail, or whose
// parse is slow enough to be interrupted. The real Go extractor never
// returns an error by design, so these branches are otherwise unreachable —
// and they are exactly the ones that matter when a second language lands.
type stubExtractor struct {
	err    error
	onCall func()
}

func (stubExtractor) Language() string     { return "stub" }
func (stubExtractor) Extensions() []string { return []string{".stub"} }

func (s stubExtractor) Extract(string, []byte) ([]Symbol, error) {
	if s.onCall != nil {
		s.onCall()
	}
	if s.err != nil {
		return nil, s.err
	}
	return []Symbol{{Name: "Stubbed", Kind: KindFunc, Language: "stub"}}, nil
}

// withStub installs an extractor for .stub for the duration of a test.
func withStub(t *testing.T, e Extractor) {
	t.Helper()
	registry[".stub"] = e
	t.Cleanup(func() { delete(registry, ".stub") })
}

// TestExtractorErrorContributesNothing covers the branch where an extractor
// fails: that file yields nothing, and the rest of the repository still
// indexes.
func TestExtractorErrorContributesNothing(t *testing.T) {
	withStub(t, stubExtractor{err: errors.New("parser exploded")})

	root := writeRepo(t, map[string]string{
		"broken.stub": "whatever",
		"fine.go":     "package p\n\nfunc Fine() {}\n",
	})
	res, err := ExtractRepo(context.Background(), root)
	if err != nil {
		t.Fatalf("an extractor error must not fail the walk: %v", err)
	}
	for _, s := range res.Symbols {
		if s.Language == "stub" {
			t.Error("a failing extractor should contribute nothing")
		}
	}
	if len(res.Symbols) != 1 || res.Symbols[0].Name != "Fine" {
		t.Errorf("the rest of the repository should still index, got %v", names(res.Symbols))
	}
}

// TestExtractRepoCancelledMidParse covers the worker-loop and post-wait
// cancellation checks, which a context cancelled before the call never
// reaches — discover returns first.
func TestExtractRepoCancelledMidParse(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	// Cancel from inside the first parse, so the workers are already
	// running when the context goes down.
	withStub(t, stubExtractor{onCall: cancel})

	files := map[string]string{}
	for i := 0; i < 40; i++ {
		files["f"+itoa(i)+".stub"] = "x"
	}
	root := writeRepo(t, files)

	if _, err := ExtractRepo(ctx, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

// TestStubExtractorIsRegisteredAndFound guards the harness itself: if the
// registry override stopped working, the tests above would pass vacuously.
func TestStubExtractorIsRegisteredAndFound(t *testing.T) {
	if _, ok := ExtractorFor("x.stub"); ok {
		t.Fatal(".stub must not be registered outside a test that installs it")
	}
	withStub(t, stubExtractor{})
	e, ok := ExtractorFor("x.stub")
	if !ok {
		t.Fatal(".stub should be registered inside the test")
	}
	if e.Language() != "stub" {
		t.Errorf("language = %q, want stub", e.Language())
	}
	syms, err := e.Extract("x.stub", nil)
	if err != nil || len(syms) != 1 {
		t.Errorf("stub extraction = (%v, %v)", syms, err)
	}
}
