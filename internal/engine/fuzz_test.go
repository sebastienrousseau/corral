package engine

import (
	"strings"
	"testing"
)

// FuzzNormalizeLanguage checks that normalizeLanguage never panics and always
// returns a non-empty, lowercase directory name free of path separators and
// spaces, for any language string the GitHub API might return.
func FuzzNormalizeLanguage(f *testing.F) {
	for _, seed := range []string{
		"", "Go", "C#", "C++", "Jupyter Notebook", "C/C++",
		"Objective-C++", "F*", "Visual Basic .NET", "  ", "/",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, lang string) {
		got := normalizeLanguage(lang)
		if got == "" {
			t.Fatalf("normalizeLanguage(%q) returned an empty directory name", lang)
		}
		if got != strings.ToLower(got) {
			t.Fatalf("normalizeLanguage(%q) = %q is not lowercase", lang, got)
		}
		if strings.ContainsAny(got, " /") {
			t.Fatalf("normalizeLanguage(%q) = %q contains a space or path separator", lang, got)
		}
	})
}
