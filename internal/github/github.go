// Package github provides functionality to interact with the GitHub API.
package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	gh "github.com/google/go-github/v74/github"
)

// Repo represents a simplified repository structure returned by the GitHub API.
type Repo struct {
	// Name is the repository name (without the owner prefix).
	Name string
	// Language is the primary programming language, or "Other" when unknown.
	Language string
	// Visibility is the normalized visibility, either "Public" or "Private".
	Visibility string
	// DefaultBranch is the repository's default branch name.
	DefaultBranch string
	// CloneURL is the HTTPS clone URL for the repository.
	CloneURL string
	// SSHURL is the SSH clone URL for the repository.
	SSHURL string
	// Fork reports whether the repository is a fork.
	Fork bool
	// Archived reports whether the repository is archived.
	Archived bool
}

// AuthMode controls how GitHub API credentials are resolved.
type AuthMode string

const (
	// AuthModeAuto resolves credentials from the environment first, then the gh CLI.
	AuthModeAuto AuthMode = "auto"
	// AuthModeToken resolves credentials only from environment variables.
	AuthModeToken AuthMode = "token"
	// AuthModeGH resolves credentials only via the gh CLI (`gh auth token`).
	AuthModeGH AuthMode = "gh"
)

// FetchOptions configures repository fetch behavior.
type FetchOptions struct {
	// Limit caps the number of repositories returned; 0 means no limit.
	Limit int
	// Visibility filters repositories by visibility ("all", "public", or "private").
	Visibility string
	// IncludeForks includes forked repositories when true.
	IncludeForks bool
	// IncludeArchived includes archived repositories when true.
	IncludeArchived bool
	// IncludeLanguages, when non-empty, keeps only repositories matching these languages.
	IncludeLanguages []string
	// ExcludeLanguages removes repositories matching these languages.
	ExcludeLanguages []string
	// AuthMode selects how the GitHub token is resolved.
	AuthMode AuthMode
	// RetryMax is the maximum number of retry attempts for transient failures.
	RetryMax int
	// RetryMinBackoff is the minimum delay between retry attempts.
	RetryMinBackoff time.Duration
	// RetryMaxBackoff is the maximum delay between retry attempts.
	RetryMaxBackoff time.Duration
}

const (
	fetchReposTimeout      = 30 * time.Second
	defaultRetryMax        = 4
	defaultRetryMinBackoff = 500 * time.Millisecond
	defaultRetryMaxBackoff = 8 * time.Second
)

var (
	runGitHubCLIAuthToken = func(ctx context.Context) (string, error) {
		cmd := exec.CommandContext(ctx, "gh", "auth", "token")
		out, err := cmd.Output()
		if err != nil {
			return "", fmt.Errorf("gh auth token failed: %w", err)
		}
		token := strings.TrimSpace(string(out))
		if token == "" {
			return "", errors.New("gh auth token returned an empty token")
		}
		return token, nil
	}
)

// FetchRepos retrieves repositories for a given owner up to the specified limit.
func FetchRepos(ctx context.Context, owner string, limit int) ([]Repo, error) {
	return FetchReposWithOptions(ctx, owner, FetchOptions{Limit: limit})
}

// FetchReposWithOptions retrieves repositories with explicit fetch options.
func FetchReposWithOptions(ctx context.Context, owner string, opts FetchOptions) ([]Repo, error) {
	if strings.TrimSpace(owner) == "" {
		return nil, errors.New("owner must not be empty")
	}

	opts = normalizeFetchOptions(opts)
	token, err := resolveToken(ctx, opts.AuthMode)
	if err != nil {
		return nil, err
	}

	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, fetchReposTimeout)
	defer cancel()

	httpClient := &http.Client{
		Timeout: fetchReposTimeout,
		Transport: &retryTransport{
			base:       http.DefaultTransport,
			maxRetries: opts.RetryMax,
			minBackoff: opts.RetryMinBackoff,
			maxBackoff: opts.RetryMaxBackoff,
		},
	}

	client := gh.NewClient(httpClient).WithAuthToken(token)
	return FetchReposWithClientOptions(ctx, client, owner, opts)
}

// FetchReposWithClient allows injecting a GitHub client for retrieving repositories.
// This is primarily exposed for testing purposes.
func FetchReposWithClient(ctx context.Context, client *gh.Client, owner string, limit int) ([]Repo, error) {
	return FetchReposWithClientOptions(ctx, client, owner, FetchOptions{Limit: limit})
}

// FetchReposWithClientOptions allows injecting a GitHub client and advanced filtering.
func FetchReposWithClientOptions(ctx context.Context, client *gh.Client, owner string, opts FetchOptions) ([]Repo, error) {
	opts = normalizeFetchOptions(opts)

	if client == nil {
		return nil, errors.New("github client must not be nil")
	}
	if strings.TrimSpace(owner) == "" {
		return nil, errors.New("owner must not be empty")
	}

	u, _, err := client.Users.Get(ctx, owner)
	if err != nil {
		return nil, fmt.Errorf("failed to get user/org '%s': %w", owner, err)
	}

	includeLang := toLookupSet(opts.IncludeLanguages)
	excludeLang := toLookupSet(opts.ExcludeLanguages)
	isOrg := u.GetType() == "Organization"

	// When the requested owner is the authenticated user, list via the
	// authenticated-user endpoint, which returns private repositories that the
	// public ListByUser endpoint omits.
	isAuthenticatedUser := false
	if !isOrg {
		if authedUser, _, authErr := client.Users.Get(ctx, ""); authErr == nil {
			login := authedUser.GetLogin()
			isAuthenticatedUser = login != "" && login == u.GetLogin()
		}
	}

	var allRepos []Repo
	page := 1
	for {
		var (
			repos []*gh.Repository
			resp  *gh.Response
			err   error
		)

		switch {
		case isOrg:
			repos, resp, err = client.Repositories.ListByOrg(ctx, owner, &gh.RepositoryListByOrgOptions{
				Type: orgTypeForVisibility(opts.Visibility),
				Sort: "updated",
				ListOptions: gh.ListOptions{
					Page:    page,
					PerPage: 100,
				},
			})
		case isAuthenticatedUser:
			repos, resp, err = client.Repositories.ListByAuthenticatedUser(ctx, &gh.RepositoryListByAuthenticatedUserOptions{
				Visibility:  opts.Visibility,
				Affiliation: "owner",
				Sort:        "updated",
				ListOptions: gh.ListOptions{
					Page:    page,
					PerPage: 100,
				},
			})
		default:
			repos, resp, err = client.Repositories.ListByUser(ctx, owner, &gh.RepositoryListByUserOptions{
				Type: "owner",
				Sort: "updated",
				ListOptions: gh.ListOptions{
					Page:    page,
					PerPage: 100,
				},
			})
		}

		if err != nil {
			return nil, fmt.Errorf("failed listing repositories for '%s': %w", owner, err)
		}

		for _, r := range repos {
			if opts.Limit > 0 && len(allRepos) >= opts.Limit {
				break
			}

			repo := mapRepository(r)
			if !matchesFilters(repo, includeLang, excludeLang, opts) {
				continue
			}
			allRepos = append(allRepos, repo)
		}

		if resp.NextPage == 0 || (opts.Limit > 0 && len(allRepos) >= opts.Limit) {
			break
		}
		page = resp.NextPage
	}

	return allRepos, nil
}

type retryTransport struct {
	base       http.RoundTripper
	maxRetries int
	minBackoff time.Duration
	maxBackoff time.Duration
}

// RoundTrip implements http.RoundTripper, retrying transient failures and
// rate-limit responses with backoff until the request succeeds, becomes
// non-retryable, the retry budget is exhausted, or the context is cancelled.
func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}

	for attempt := 0; ; attempt++ {
		clonedReq := req.Clone(req.Context())
		resp, err := base.RoundTrip(clonedReq)

		retry, wait := shouldRetry(resp, err, attempt, t.maxRetries)
		if !retry {
			return resp, err
		}
		if resp != nil && resp.Body != nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}

		if wait <= 0 {
			wait = t.backoff(attempt)
		}
		timer := time.NewTimer(wait)
		select {
		case <-req.Context().Done():
			timer.Stop()
			return nil, req.Context().Err()
		case <-timer.C:
		}
	}
}

func (t *retryTransport) backoff(attempt int) time.Duration {
	minBackoff := t.minBackoff
	maxBackoff := t.maxBackoff
	if minBackoff <= 0 {
		minBackoff = defaultRetryMinBackoff
	}
	if maxBackoff <= 0 {
		maxBackoff = defaultRetryMaxBackoff
	}

	backoff := minBackoff << attempt
	if backoff > maxBackoff {
		backoff = maxBackoff
	}
	// Non-cryptographic randomness is acceptable for retry backoff jitter.
	jitter := time.Duration(rand.Int63n(int64(minBackoff) + 1)) //nolint:gosec // G404: jitter does not require crypto/rand
	return backoff + jitter
}

func shouldRetry(resp *http.Response, err error, attempt, maxRetries int) (bool, time.Duration) {
	if attempt >= maxRetries {
		return false, 0
	}

	if err != nil {
		if isRetryableNetworkError(err) {
			return true, 0
		}
		return false, 0
	}
	if resp == nil {
		return false, 0
	}

	if d, ok := retryAfterDuration(resp); ok {
		return true, d
	}

	switch resp.StatusCode {
	case http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusInternalServerError,
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true, 0
	case http.StatusForbidden:
		if strings.TrimSpace(resp.Header.Get("X-RateLimit-Remaining")) == "0" {
			if d, ok := rateLimitResetDuration(resp); ok {
				return true, d
			}
			return true, time.Minute
		}
	}

	return false, 0
}

func isRetryableNetworkError(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	return errors.Is(err, io.EOF)
}

func retryAfterDuration(resp *http.Response) (time.Duration, bool) {
	h := strings.TrimSpace(resp.Header.Get("Retry-After"))
	if h == "" {
		return 0, false
	}
	if seconds, err := strconv.Atoi(h); err == nil {
		if seconds < 0 {
			seconds = 0
		}
		return time.Duration(seconds) * time.Second, true
	}
	if t, err := http.ParseTime(h); err == nil {
		d := time.Until(t)
		if d < 0 {
			d = 0
		}
		return d, true
	}
	return 0, false
}

func rateLimitResetDuration(resp *http.Response) (time.Duration, bool) {
	h := strings.TrimSpace(resp.Header.Get("X-RateLimit-Reset"))
	if h == "" {
		return 0, false
	}
	sec, err := strconv.ParseInt(h, 10, 64)
	if err != nil {
		return 0, false
	}
	resetAt := time.Unix(sec, 0)
	d := time.Until(resetAt)
	if d < 0 {
		d = 0
	}
	return d, true
}

func envToken() string {
	if token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); token != "" {
		return token
	}
	if token := strings.TrimSpace(os.Getenv("GH_TOKEN")); token != "" {
		return token
	}
	return ""
}

func resolveToken(ctx context.Context, authMode AuthMode) (string, error) {
	switch normalizeAuthMode(authMode) {
	case AuthModeToken:
		token := envToken()
		if token == "" {
			return "", errors.New("GITHUB_TOKEN (or GH_TOKEN) environment variable not set")
		}
		return token, nil
	case AuthModeGH:
		return runGitHubCLIAuthToken(ctx)
	default:
		token := envToken()
		if token != "" {
			return token, nil
		}
		token, err := runGitHubCLIAuthToken(ctx)
		if err != nil {
			return "", fmt.Errorf("failed to resolve GitHub token (auto mode): %w", err)
		}
		return token, nil
	}
}

func normalizeAuthMode(authMode AuthMode) AuthMode {
	switch AuthMode(strings.ToLower(strings.TrimSpace(string(authMode)))) {
	case AuthModeToken:
		return AuthModeToken
	case AuthModeGH:
		return AuthModeGH
	default:
		return AuthModeAuto
	}
}

func normalizeFetchOptions(opts FetchOptions) FetchOptions {
	if opts.Limit < 0 {
		opts.Limit = 0
	}
	if opts.Visibility == "" {
		opts.Visibility = "all"
	}
	opts.Visibility = strings.ToLower(strings.TrimSpace(opts.Visibility))
	if opts.Visibility != "all" && opts.Visibility != "public" && opts.Visibility != "private" {
		opts.Visibility = "all"
	}
	opts.AuthMode = normalizeAuthMode(opts.AuthMode)
	if opts.RetryMax < 0 {
		opts.RetryMax = 0
	} else if opts.RetryMax == 0 {
		opts.RetryMax = defaultRetryMax
	}
	if opts.RetryMinBackoff <= 0 {
		opts.RetryMinBackoff = defaultRetryMinBackoff
	}
	if opts.RetryMaxBackoff <= 0 {
		opts.RetryMaxBackoff = defaultRetryMaxBackoff
	}
	if opts.RetryMaxBackoff < opts.RetryMinBackoff {
		opts.RetryMaxBackoff = opts.RetryMinBackoff
	}
	return opts
}

func orgTypeForVisibility(v string) string {
	switch v {
	case "public":
		return "public"
	case "private":
		return "private"
	default:
		return "all"
	}
}

func toLookupSet(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(values))
	for _, v := range values {
		norm := strings.ToLower(strings.TrimSpace(v))
		if norm == "" {
			continue
		}
		out[norm] = struct{}{}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func matchesFilters(repo Repo, includeLang, excludeLang map[string]struct{}, opts FetchOptions) bool {
	if !opts.IncludeForks && repo.Fork {
		return false
	}
	if !opts.IncludeArchived && repo.Archived {
		return false
	}
	if opts.Visibility == "public" && repo.Visibility != "Public" {
		return false
	}
	if opts.Visibility == "private" && repo.Visibility != "Private" {
		return false
	}

	lang := strings.ToLower(strings.TrimSpace(repo.Language))
	if includeLang != nil {
		if _, ok := includeLang[lang]; !ok {
			return false
		}
	}
	if excludeLang != nil {
		if _, ok := excludeLang[lang]; ok {
			return false
		}
	}

	return true
}

func mapRepository(r *gh.Repository) Repo {
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

	return Repo{
		Name:          r.GetName(),
		Language:      lang,
		Visibility:    visibility,
		DefaultBranch: defaultBranch,
		CloneURL:      cloneURL,
		SSHURL:        sshURL,
		Fork:          r.GetFork(),
		Archived:      r.GetArchived(),
	}
}
