// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package forge

import (
	"context"
	"net/url"
	"strings"
	"time"
)

func init() { Register(Bitbucket{}) }

// Bitbucket lists repositories from a Bitbucket Cloud workspace.
//
// Bitbucket calls an owner a workspace, and unlike GitHub or Gitea it does
// not split users from organisations across two endpoints — a workspace is
// a workspace, so there is one path and no fallback to try.
//
// Bitbucket Server (the self-hosted product, now Data Center) speaks a
// different API at a different path and is not this. Pointing --forge-url
// at one would fail on the first request rather than silently return
// nothing, which is the right failure but not a supported configuration.
type Bitbucket struct{}

// DefaultBitbucketURL is the public cloud instance.
const DefaultBitbucketURL = "https://api.bitbucket.org"

// Name implements Forge.
func (Bitbucket) Name() string { return "bitbucket" }

// Hosts implements Forge.
//
// Both hosts matter: the API lives on api.bitbucket.org while clone URLs
// and anything a user pastes point at bitbucket.org, and --forge-url is
// resolved by host.
func (Bitbucket) Hosts() []string { return []string{"bitbucket.org", "api.bitbucket.org"} }

// bitbucketRepo is the subset of Bitbucket's repository representation
// corral uses.
type bitbucketRepo struct {
	UUID string `json:"uuid"`
	// Slug is the URL name. Name is a display name and may contain spaces
	// and capitals — "Atlassian Event" for the repository cloned as
	// atlassian-event — so the slug is what a directory is called.
	Slug     string `json:"slug"`
	Name     string `json:"name"`
	FullName string `json:"full_name"`
	// SCM is "git" or, for a long-dead repository, "hg". Mercurial hosting
	// ended in 2020, but an old workspace can still list one and corral
	// cannot clone it.
	SCM        string `json:"scm"`
	IsPrivate  bool   `json:"is_private"`
	Language   string `json:"language"`
	MainBranch *struct {
		Name string `json:"name"`
	} `json:"mainbranch"`
	// Parent is present only on a fork, and is the repository it was
	// forked from. Bitbucket has no boolean for this.
	Parent    *struct{}  `json:"parent"`
	UpdatedOn *time.Time `json:"updated_on"`
	Links     struct {
		Clone []struct {
			Name string `json:"name"`
			HRef string `json:"href"`
		} `json:"clone"`
	} `json:"links"`
}

// bitbucketPage is one page of a Bitbucket listing.
//
// Bitbucket paginates with an absolute `next` URL rather than a page
// number, so a short page is not a reliable end-of-listing signal the way
// it is for GitLab and Gitea.
type bitbucketPage struct {
	Values []bitbucketRepo `json:"values"`
	Next   string          `json:"next"`
}

// List implements Forge.
func (bb Bitbucket) List(ctx context.Context, owner string, opts Options) ([]Repo, error) {
	base := bitbucketAPIBase(opts.BaseURL)
	// Bitbucket takes an app password as HTTP Basic, but also accepts a
	// bearer token, which is the form that does not require also knowing
	// the username.
	c, err := newRESTClient(base, opts.Token, "Authorization", "Bearer ", opts)
	if err != nil {
		return nil, err
	}

	extra := url.Values{}
	// Newest activity first, so a limit keeps the repositories somebody is
	// most likely to want.
	extra.Set("sort", "-updated_on")

	var out []Repo
	path := "/2.0/repositories/" + url.PathEscape(owner)
	for page := 1; page <= maxPages; page++ {
		var batch bitbucketPage
		if err := c.getPage(ctx, path, pageQueryNamed(page, "pagelen", extra), &batch); err != nil {
			return nil, err
		}
		for _, r := range batch.Values {
			if r.SCM != "" && r.SCM != "git" {
				// A Mercurial repository has no git clone URL and cannot
				// be cloned. Skipping it silently is right: it is not an
				// error, and reporting it would be noise on every run.
				continue
			}
			out = append(out, bb.toRepo(r))
		}
		// Bitbucket signals the end by omitting `next`, not by returning a
		// short page — a filtered listing can be short and still have more.
		if batch.Next == "" {
			break
		}
		if opts.Limit > 0 && len(out) >= opts.Limit*2 {
			break
		}
	}
	return Filter(out, opts), nil
}

// bitbucketAPIBase resolves the address to talk to.
//
// Bitbucket splits its web host from its API host, and a user naming the
// forge by URL names the one they can see: --forge-url
// https://bitbucket.org. Sent as-is that 404s on every request, because
// the API is on api.bitbucket.org — a confusing failure for an input that
// was perfectly reasonable. The web host is rewritten to the API host, and
// anything else is left alone so a proxy or a mirror still works.
func bitbucketAPIBase(base string) string {
	if base == "" {
		return DefaultBitbucketURL
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		// Left to newRESTClient, which reports an unusable base URL far
		// better than a silent default would.
		return base
	}
	if strings.EqualFold(u.Hostname(), "bitbucket.org") ||
		strings.EqualFold(u.Hostname(), "www.bitbucket.org") {
		u.Host = "api.bitbucket.org"
		u.Path = ""
		return u.String()
	}
	return base
}

// toRepo maps a Bitbucket repository onto the shared shape.
func (bb Bitbucket) toRepo(r bitbucketRepo) Repo {
	var https, ssh string
	for _, l := range r.Links.Clone {
		switch l.Name {
		case "https":
			https = l.HRef
		case "ssh":
			ssh = l.HRef
		}
	}
	var pushed time.Time
	if r.UpdatedOn != nil {
		// updated_on moves on metadata changes as well as pushes, like
		// GitLab's and Gitea's, so the sync decision it feeds is
		// conservative: it can cause a pull that was not needed, never
		// skip one that was.
		pushed = *r.UpdatedOn
	}
	var branch string
	if r.MainBranch != nil {
		branch = r.MainBranch.Name
	}
	name := r.Slug
	if name == "" {
		name = r.Name
	}
	return Repo{
		// Bitbucket's identifier is a UUID string, and Repo.ID is numeric
		// for the forges that have one. Nothing in corral reads ID for
		// anything but debugging, so it is left zero rather than hashed
		// into a number that would look meaningful and not be.
		Owner:         trimLastSegment(r.FullName),
		Name:          name,
		FullName:      r.FullName,
		Language:      NormalizeLanguage(r.Language),
		Visibility:    NormalizeVisibility(r.IsPrivate, ""),
		DefaultBranch: branch,
		CloneURL:      https,
		SSHURL:        ssh,
		Fork:          r.Parent != nil,
		// Bitbucket Cloud has no archived flag on a repository.
		Archived: false,
		PushedAt: pushed,
	}
}
