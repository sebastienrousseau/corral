package mcp

import "testing"

// FuzzParseOwnerFromURL checks that parseOwnerFromURL is robust and does not
// panic when processing arbitrary malformed remote URL inputs.
func FuzzParseOwnerFromURL(f *testing.F) {
	for _, seed := range []string{
		"https://github.com/sebastienrousseau/corral.git",
		"git@github.com:sebastienrousseau/corral.git",
		"https://git.company.com/parent/sub/owner/repo.git",
		"git@github-personal:owner/repo.git",
		"",
		"http://",
		"://",
		"a/b/c",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, url string) {
		_ = parseOwnerFromURL(url)
	})
}
