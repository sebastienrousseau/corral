// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	gh "github.com/google/go-github/v90/github"
)

// Repo represents a simplified repository structure returned by the GitHub API.
type Repo struct {
	// ID is GitHub's immutable repository identifier.
	ID int64
	// Owner is the repository owner's login.
	Owner string
	// FullName is the canonical owner/name identity.
	FullName string
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
	// PushedAt is the timestamp of the last push to any branch. The engine
	// compares this against the cached value in <repo>/.corral-state.json to
	// skip a `git pull` when nothing has changed upstream.
	PushedAt time.Time
	// Stars reports the stargazers count for the repository.
	Stars int
	// IsTemplate reports whether the repository is a template.
	IsTemplate bool
	// IsMirror reports whether the repository is a mirror.
	IsMirror bool
	// CanBeSponsored reports whether the repository has sponsorships enabled.
	CanBeSponsored bool
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
	// Type filters repositories by specific category (e.g. "sources", "forks", "archived", "mirrors", etc.).
	Type string
	// Sort specifies how the returned repositories list should be ordered.
	Sort string
	// RetryMax is the maximum number of retry attempts for transient failures.
	RetryMax int
	// RetryMinBackoff is the minimum delay between retry attempts.
	RetryMinBackoff time.Duration
	// RetryMaxBackoff is the maximum delay between retry attempts.
	RetryMaxBackoff time.Duration
	// Timeout bounds the complete GitHub API operation and each HTTP request.
	Timeout time.Duration
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
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	httpClient := &http.Client{
		Timeout: opts.Timeout,
		Transport: &retryTransport{
			base:       http.DefaultTransport,
			maxRetries: opts.RetryMax,
			minBackoff: opts.RetryMinBackoff,
			maxBackoff: opts.RetryMaxBackoff,
		},
	}

	// go-github v57+ replaced NewClient(*http.Client) with a variadic
	// options constructor that also returns an error, and WithAuthToken moved
	// from a chained client method to a ClientOptionsFunc.
	client, err := newGitHubClient(gh.WithHTTPClient(httpClient), gh.WithAuthToken(token))
	if err != nil {
		return nil, fmt.Errorf("construct GitHub client: %w", err)
	}
	return FetchReposWithClientOptions(ctx, client, owner, opts)
}

// newGitHubClient constructs the go-github client. A seam rather than a
// direct call so the construction-failure branch is reachable from a test:
// gh.NewClient only fails on a malformed option, which production code
// cannot produce, and an unreachable error path is an untested error path.
var newGitHubClient = gh.NewClient

// FetchReposWithClient allows injecting a GitHub client for retrieving repositories.
// This is primarily exposed for testing purposes.
func FetchReposWithClient(ctx context.Context, client *gh.Client, owner string, limit int) ([]Repo, error) {
	return FetchReposWithClientOptions(ctx, client, owner, FetchOptions{Limit: limit})
}

// pageFetcher retrieves one page of repositories from whichever GitHub
// listing endpoint suits the requested owner.
type pageFetcher func(page int) ([]*gh.Repository, *gh.Response, error)

// listingKind records which listing endpoint an owner resolves to. The
// choice is made once, before any page is fetched, because it depends on
// two API lookups that must not be repeated per page.
type listingKind struct {
	// search is true when owner is a `topic:` or `language:` search query
	// rather than a user or organisation.
	search bool
	// org is true when owner is a GitHub organisation.
	org bool
	// authenticatedUser is true when owner is the token's own account, in
	// which case the authenticated-user endpoint is used so private
	// repositories are included.
	authenticatedUser bool
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

	kind, err := resolveListingKind(ctx, client, owner)
	if err != nil {
		return nil, err
	}

	collector := &repoCollector{
		limit:       opts.Limit,
		includeLang: toLookupSet(opts.IncludeLanguages),
		excludeLang: toLookupSet(opts.ExcludeLanguages),
		opts:        opts,
	}
	fetchPage := newPageFetcher(ctx, client, owner, opts, kind)

	// Fetch the first page to learn the total page count.
	repos, resp, err := fetchPage(1)
	if err != nil {
		return nil, fmt.Errorf("failed listing repositories for '%s': %w", owner, err)
	}
	collector.absorb(repos)

	switch {
	case resp.LastPage > 1 && collector.wants():
		if err := collectConcurrently(ctx, owner, fetchPage, resp.LastPage, collector); err != nil {
			return nil, err
		}
	case resp.NextPage > 0 && collector.wants():
		// Fallback to a sequential fetch when LastPage isn't provided but
		// NextPage is.
		if err := collectSequentially(owner, fetchPage, resp.NextPage, collector); err != nil {
			return nil, err
		}
	}

	sortRepos(collector.repos, opts.Sort)
	return collector.repos, nil
}

// resolveListingKind determines which listing endpoint owner should be read
// from, performing the owner lookups that decision needs.
func resolveListingKind(ctx context.Context, client *gh.Client, owner string) (listingKind, error) {
	kind := listingKind{
		search: strings.HasPrefix(owner, "topic:") || strings.HasPrefix(owner, "language:"),
	}
	if kind.search {
		return kind, nil
	}

	u, _, err := client.Users.Get(ctx, owner)
	if err != nil {
		return kind, describeOwnerLookupError(ctx, client, owner, err)
	}
	kind.org = u.GetType() == "Organization"
	if kind.org {
		return kind, nil
	}

	// When the requested owner is the authenticated user, list via the
	// authenticated-user endpoint, which returns private repositories that the
	// public ListByUser endpoint omits.
	if authedUser, _, authErr := client.Users.Get(ctx, ""); authErr == nil {
		login := authedUser.GetLogin()
		kind.authenticatedUser = login != "" && login == u.GetLogin()
	}
	return kind, nil
}

// newPageFetcher returns the page-fetching closure for the resolved
// listing kind. Every endpoint is paged 100 at a time.
func newPageFetcher(ctx context.Context, client *gh.Client, owner string, opts FetchOptions, kind listingKind) pageFetcher {
	return func(p int) ([]*gh.Repository, *gh.Response, error) {
		listOpts := gh.ListOptions{Page: p, PerPage: 100}
		switch {
		case kind.search:
			result, resp, err := client.Search.Repositories(ctx, owner, &gh.SearchOptions{
				Sort:        "stars",
				ListOptions: listOpts,
			})
			if err != nil {
				return nil, nil, err
			}
			return result.Repositories, resp, nil
		case kind.org:
			return client.Repositories.ListByOrg(ctx, owner, &gh.RepositoryListByOrgOptions{
				Type:        orgTypeForVisibility(opts.Visibility),
				Sort:        "updated",
				ListOptions: listOpts,
			})
		case kind.authenticatedUser:
			return client.Repositories.ListByAuthenticatedUser(ctx, &gh.RepositoryListByAuthenticatedUserOptions{
				Visibility:  opts.Visibility,
				Affiliation: "owner",
				Sort:        "updated",
				ListOptions: listOpts,
			})
		default:
			return client.Repositories.ListByUser(ctx, owner, &gh.RepositoryListByUserOptions{
				Type:        "owner",
				Sort:        "updated",
				ListOptions: listOpts,
			})
		}
	}
}

// repoCollector accumulates mapped repositories, applying the language and
// visibility filters and stopping at the requested limit.
type repoCollector struct {
	repos       []Repo
	limit       int
	includeLang map[string]struct{}
	excludeLang map[string]struct{}
	opts        FetchOptions
}

// wants reports whether the collector can still take more repositories.
func (c *repoCollector) wants() bool {
	return c.limit == 0 || len(c.repos) < c.limit
}

// absorb maps and filters one page of API results into the collection.
func (c *repoCollector) absorb(page []*gh.Repository) {
	for _, r := range page {
		if !c.wants() {
			return
		}
		repo := mapRepository(r)
		if !matchesFilters(repo, c.includeLang, c.excludeLang, c.opts) {
			continue
		}
		c.repos = append(c.repos, repo)
	}
}

// maxConcurrentPageFetches bounds how many listing pages are requested at
// once. Five keeps a large organisation responsive without tripping GitHub's
// secondary rate limits.
//
// A var rather than a const so a test can shrink it to zero, which makes the
// semaphore unacquirable and the cancellation branch in collectConcurrently
// deterministic instead of a scheduling race.
var maxConcurrentPageFetches = 5

// collectConcurrently fetches pages 2..lastPage in parallel, bounded to five
// in flight, and absorbs them in page order so the result stays
// deterministic regardless of completion order.
func collectConcurrently(ctx context.Context, owner string, fetchPage pageFetcher, lastPage int, collector *repoCollector) error {
	var (
		mu         sync.Mutex
		errs       []error
		resultsMap = make(map[int][]*gh.Repository)
		sem        = make(chan struct{}, maxConcurrentPageFetches)
		wg         sync.WaitGroup
	)

	for p := 2; p <= lastPage; p++ {
		wg.Add(1)
		go func(pNum int) {
			defer wg.Done()
			if err := acquirePageSlot(ctx, sem); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
				return
			}
			defer func() { <-sem }()

			pRepos, _, pErr := fetchPage(pNum)

			mu.Lock()
			defer mu.Unlock()
			if pErr != nil {
				errs = append(errs, pErr)
				return
			}
			resultsMap[pNum] = pRepos
		}(p)
	}
	wg.Wait()

	if len(errs) > 0 {
		return fmt.Errorf("failed listing repositories for '%s' (concurrent fetch failed): %w", owner, errs[0])
	}

	for p := 2; p <= lastPage; p++ {
		collector.absorb(resultsMap[p])
	}
	return nil
}

// collectSequentially walks the NextPage chain from firstPage until the
// listing is exhausted or the collector is full.
func collectSequentially(owner string, fetchPage pageFetcher, firstPage int, collector *repoCollector) error {
	page := firstPage
	for {
		pRepos, pResp, pErr := fetchPage(page)
		if pErr != nil {
			return fmt.Errorf("failed listing repositories for '%s' (fallback fetch failed): %w", owner, pErr)
		}
		collector.absorb(pRepos)
		if pResp.NextPage == 0 || !collector.wants() {
			return nil
		}
		page = pResp.NextPage
	}
}

// sortRepos applies the requested post-fetch ordering in place. An unknown
// or empty sort key leaves the API's own ordering untouched.
func sortRepos(repos []Repo, key string) {
	switch strings.ToLower(key) {
	case "name":
		sort.Slice(repos, func(i, j int) bool {
			return strings.ToLower(repos[i].Name) < strings.ToLower(repos[j].Name)
		})
	case "stars":
		sort.Slice(repos, func(i, j int) bool {
			return repos[i].Stars > repos[j].Stars
		})
	case "last updated", "updated":
		sort.Slice(repos, func(i, j int) bool {
			return repos[i].PushedAt.After(repos[j].PushedAt)
		})
	}
}

func acquirePageSlot(ctx context.Context, sem chan struct{}) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case sem <- struct{}{}:
		return nil
	}
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

// Token resolves a GitHub token for the given auth mode, returning an empty
// string when none can be obtained. It lets the git package authenticate HTTPS
// clones and pulls of private repositories with the same credential as the API.
func Token(ctx context.Context, authMode AuthMode) string {
	tok, err := resolveToken(ctx, authMode)
	if err != nil {
		return ""
	}
	return tok
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
	if opts.Timeout <= 0 {
		opts.Timeout = fetchReposTimeout
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

	// Apply post-fetch Type filtering
	if opts.Type != "" {
		switch strings.ToLower(opts.Type) {
		case "public":
			if repo.Visibility != "Public" {
				return false
			}
		case "private":
			if repo.Visibility != "Private" {
				return false
			}
		case "sources":
			if repo.Fork {
				return false
			}
		case "forks":
			if !repo.Fork {
				return false
			}
		case "archived":
			if !repo.Archived {
				return false
			}
		case "can be sponsored", "sponsored":
			// Unreachable: rejected by cmd.validateCommonFlags before a
			// fetch is issued, because CanBeSponsored is not carried by
			// the REST listing endpoints. Kept as a guard so a future
			// caller bypassing the CLI still filters nothing in rather
			// than filtering everything out.
			return repo.CanBeSponsored
		case "mirrors":
			if !repo.IsMirror {
				return false
			}
		case "templates":
			if !repo.IsTemplate {
				return false
			}
		}
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
	owner := r.GetOwner().GetLogin()
	fullName := r.GetFullName()
	if fullName == "" && owner != "" && r.GetName() != "" {
		fullName = owner + "/" + r.GetName()
	}
	lang := "Other"
	if r.Language != nil {
		lang = *r.Language
	}

	// Private is authoritative and Visibility refines it. Reading only
	// Visibility meant a nil pointer — which some endpoints and older API
	// versions return — fell through to "Public", and visibility decides
	// the on-disk Collection. A private repository was therefore filed
	// under Public/ and reported as public by the MCP tools.
	visibility := "Public"
	if r.GetPrivate() {
		visibility = "Private"
	}
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
		ID:            r.GetID(),
		Owner:         owner,
		FullName:      fullName,
		Name:          r.GetName(),
		Language:      lang,
		Visibility:    visibility,
		DefaultBranch: defaultBranch,
		CloneURL:      cloneURL,
		SSHURL:        sshURL,
		Fork:          r.GetFork(),
		Archived:      r.GetArchived(),
		PushedAt:      r.GetPushedAt().Time,
		Stars:         r.GetStargazersCount(),
		IsTemplate:    r.GetIsTemplate(),
		IsMirror:      r.GetMirrorURL() != "",
		// Not exposed by the REST listing endpoints; populating it needs a
		// per-repository GraphQL query. Left false deliberately, and the
		// --type value that depended on it is refused at flag validation
		// rather than silently returning an empty result.
		CanBeSponsored: false,
	}
}

// describeOwnerLookupError turns a failed owner lookup into a message a person
// can act on.
//
// The raw error from go-github reads:
//
//	failed to get user/org 'sebastienrouseau': GET https://api.github.com/users/sebastienrouseau: 404 Not Found []
//
// which leaks the transport, ends in an empty bracket pair, and says nothing
// about what to do — while the overwhelmingly likely cause is a one-character
// typo in a username. clig.dev: "catch errors and rewrite them for humans".
//
// On a 404 this reports that the owner does not exist and, when the mistyped
// name is close to the authenticated user's own login, names the correction.
// Other failures (rate limits, network, auth) keep their original text, which is
// already the actionable part.
func describeOwnerLookupError(ctx context.Context, client *gh.Client, owner string, err error) error {
	var apiErr *gh.ErrorResponse
	if !errors.As(err, &apiErr) || apiErr.Response == nil ||
		apiErr.Response.StatusCode != http.StatusNotFound {
		return fmt.Errorf("cannot look up %q on GitHub: %w", owner, err)
	}

	msg := fmt.Sprintf("no GitHub user or organisation named %q", owner)

	// A 404 for an owner that does exist means it is private and the current
	// credentials cannot see it, so mention auth as well as spelling.
	if login := authenticatedLogin(ctx, client); login != "" {
		if login != owner && levenshtein(strings.ToLower(owner), strings.ToLower(login)) <= 2 {
			return fmt.Errorf("%s\n\nDid you mean your own account?\n\tcorralctl %s", msg, login)
		}
	}
	return fmt.Errorf("%s\n\nCheck the spelling. If it is a private organisation, "+
		"confirm your credentials can see it with `gh auth status`", msg)
}

// authenticatedLogin returns the login of the credentialed user, or "" when
// unauthenticated or the call fails. Never an error: this is only used to
// improve a message that is already being returned.
func authenticatedLogin(ctx context.Context, client *gh.Client) string {
	u, _, err := client.Users.Get(ctx, "")
	if err != nil {
		return ""
	}
	return u.GetLogin()
}

// levenshtein is the standard edit distance, used only to decide whether a
// mistyped owner is close enough to the authenticated user's login to suggest.
//
// Deliberately duplicated rather than shared: internal/github has no internal
// imports, and keeping that leaf property is worth more than de-duplicating
// twenty lines of arithmetic.
func levenshtein(a, b string) int {
	ar, br := []rune(a), []rune(b)
	prev := make([]int, len(br)+1)
	cur := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		cur[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, min(cur[j-1]+1, prev[j-1]+cost))
		}
		prev, cur = cur, prev
	}
	return prev[len(br)]
}
