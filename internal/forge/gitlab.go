// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package forge

import (
	"context"
	"errors"
	"net/url"
	"time"
)

func init() { Register(GitLab{}) }

// GitLab lists repositories from gitlab.com or a self-hosted instance.
//
// GitLab calls them projects, and an owner is either a user or a group —
// two different endpoints with no way to tell in advance which one a name
// refers to. Both are tried, users first, because a personal namespace is
// the common case for somebody typing their own name.
type GitLab struct{}

// DefaultGitLabURL is the public instance.
const DefaultGitLabURL = "https://gitlab.com"

// Name implements Forge.
func (GitLab) Name() string { return "gitlab" }

// Hosts implements Forge.
func (GitLab) Hosts() []string { return []string{"gitlab.com"} }

// gitlabProject is the subset of GitLab's project representation corral
// uses.
//
// Named fields rather than a map so a shape change is a compile error
// rather than a silently empty layout directory.
type gitlabProject struct {
	ID   int64  `json:"id"`
	Name string `json:"path"`
	// PathWithNamespace is "group/subgroup/project". GitLab allows nested
	// groups, which GitHub does not, so this can have more than two
	// segments.
	PathWithNamespace string `json:"path_with_namespace"`
	Namespace         struct {
		FullPath string `json:"full_path"`
	} `json:"namespace"`
	// Visibility is "public", "internal" or "private".
	Visibility        string     `json:"visibility"`
	DefaultBranch     string     `json:"default_branch"`
	HTTPURLToRepo     string     `json:"http_url_to_repo"`
	SSHURLToRepo      string     `json:"ssh_url_to_repo"`
	ForkedFromProject *struct{}  `json:"forked_from_project"`
	Archived          bool       `json:"archived"`
	LastActivityAt    *time.Time `json:"last_activity_at"`
	StarCount         int        `json:"star_count"`
	// Mirror is only present for a licensed instance; absent means false,
	// which is the right default.
	Mirror bool `json:"mirror"`
}

// List implements Forge.
func (g GitLab) List(ctx context.Context, owner string, opts Options) ([]Repo, error) {
	base := opts.BaseURL
	if base == "" {
		base = DefaultGitLabURL
	}
	// PRIVATE-TOKEN is GitLab's own header. Authorization: Bearer also
	// works for OAuth tokens but not for personal access tokens, and a
	// personal access token is what a developer has.
	c, err := newRESTClient(base, opts.Token, "PRIVATE-TOKEN", "", opts)
	if err != nil {
		return nil, err
	}

	// A user and a group are different endpoints and a name gives no clue
	// which it is. Users first: somebody typing a bare name usually means
	// themselves.
	var (
		repos    []Repo
		lastErr  error
		anyFound bool
	)
	for _, kind := range []string{"users", "groups"} {
		got, err := g.listNamespace(ctx, c, kind, owner, opts)
		if err != nil {
			var nf *NotFoundError
			if errors.As(err, &nf) {
				// This namespace kind does not exist; the other might.
				lastErr = err
				continue
			}
			return nil, err
		}
		anyFound = true
		repos = append(repos, got...)
		// A name cannot be both a user and a group on GitLab — the
		// namespace is unique — so the first hit is the answer.
		break
	}
	if !anyFound {
		return nil, lastErr
	}
	return Filter(repos, opts), nil
}

// listNamespace pages one endpoint to exhaustion.
func (g GitLab) listNamespace(ctx context.Context, c *restClient, kind, owner string, opts Options) ([]Repo, error) {
	extra := url.Values{}
	// Newest activity first, so a limit keeps the repositories somebody is
	// most likely to want.
	extra.Set("order_by", "last_activity_at")
	extra.Set("sort", "desc")
	if !opts.IncludeArchived {
		// Applied at the API too, not only in Filter: an owner with
		// hundreds of archived projects would otherwise spend the whole
		// page budget on them.
		extra.Set("archived", "false")
	}

	var out []Repo
	for page := 1; page <= maxPages; page++ {
		var batch []gitlabProject
		path := "/api/v4/" + kind + "/" + url.PathEscape(owner) + "/projects"
		if err := c.getPage(ctx, path, pageQuery(page, extra), &batch); err != nil {
			return nil, err
		}
		for _, p := range batch {
			out = append(out, g.toRepo(p))
		}
		if len(batch) < perPage {
			// A short page is the last page. GitLab also returns
			// X-Next-Page, but a short page is true on every instance and
			// needs no header parsing.
			break
		}
		// The cap applies to what the API returns, before filtering, so a
		// limit does not stop pagination early and hide matches behind
		// archived projects.
		if opts.Limit > 0 && len(out) >= opts.Limit*2 {
			break
		}
	}
	return out, nil
}

// toRepo maps a GitLab project onto the shared shape.
func (g GitLab) toRepo(p gitlabProject) Repo {
	owner := p.Namespace.FullPath
	if owner == "" {
		owner = trimLastSegment(p.PathWithNamespace)
	}
	var pushed time.Time
	if p.LastActivityAt != nil {
		// GitLab has no per-branch push timestamp in a project listing.
		// last_activity_at is broader — an issue comment moves it — so the
		// sync decision it feeds is conservative: it can cause a pull that
		// was not needed, never skip one that was.
		pushed = *p.LastActivityAt
	}
	return Repo{
		ID:       p.ID,
		Owner:    owner,
		Name:     p.Name,
		FullName: p.PathWithNamespace,
		// GitLab's project listing carries no language field at all;
		// finding one costs a request per project, which is not worth it
		// for a directory name.
		Language:      NormalizeLanguage(""),
		Visibility:    NormalizeVisibility(false, p.Visibility),
		DefaultBranch: p.DefaultBranch,
		CloneURL:      p.HTTPURLToRepo,
		SSHURL:        p.SSHURLToRepo,
		Fork:          p.ForkedFromProject != nil,
		Archived:      p.Archived,
		PushedAt:      pushed,
		Stars:         p.StarCount,
		IsMirror:      p.Mirror,
	}
}

// trimLastSegment returns everything before the final "/", for deriving an
// owner from a full path when the namespace is absent.
func trimLastSegment(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[:i]
		}
	}
	return path
}
