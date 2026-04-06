package github

import (
	"context"
	"fmt"
	"os"

	"github.com/google/go-github/v60/github"
	"golang.org/x/oauth2"
)

type Repo struct {
	Name          string
	Language      string
	Visibility    string
	DefaultBranch string
	CloneURL      string
	SSHURL        string
}

func FetchRepos(owner string, limit int) ([]Repo, error) {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("GITHUB_TOKEN environment variable not set")
	}

	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)

	// Determine if owner is an organization or a user
	u, _, err := client.Users.Get(ctx, owner)
	if err != nil {
		return nil, fmt.Errorf("failed to get user/org '%s': %v", owner, err)
	}

	var allRepos []Repo
	isOrg := u.GetType() == "Organization"

	page := 1
	for {
		var repos []*github.Repository
		var resp *github.Response
		var err error

		if isOrg {
			opt := &github.RepositoryListByOrgOptions{
				ListOptions: github.ListOptions{Page: page, PerPage: 100},
			}
			repos, resp, err = client.Repositories.ListByOrg(ctx, owner, opt)
		} else {
			opt := &github.RepositoryListByUserOptions{
				ListOptions: github.ListOptions{Page: page, PerPage: 100},
			}
			repos, resp, err = client.Repositories.ListByUser(ctx, owner, opt)
		}

		if err != nil {
			return nil, err
		}

		for _, r := range repos {
			if len(allRepos) >= limit && limit > 0 {
				break
			}
			lang := "Other"
			if r.Language != nil {
				lang = *r.Language
			}
			visibility := "Public"
			if r.Visibility != nil && (*r.Visibility == "private" || *r.Visibility == "internal") {
				visibility = "Private"
			}

			defaultBranch := "main"
			if r.DefaultBranch != nil {
				defaultBranch = *r.DefaultBranch
			}

			cloneURL := ""
			if r.CloneURL != nil {
				cloneURL = *r.CloneURL
			}
			sshURL := ""
			if r.SSHURL != nil {
				sshURL = *r.SSHURL
			}

			allRepos = append(allRepos, Repo{
				Name:          *r.Name,
				Language:      lang,
				Visibility:    visibility,
				DefaultBranch: defaultBranch,
				CloneURL:      cloneURL,
				SSHURL:        sshURL,
			})
		}

		if resp.NextPage == 0 || (limit > 0 && len(allRepos) >= limit) {
			break
		}
		page = resp.NextPage
	}

	return allRepos, nil
}
