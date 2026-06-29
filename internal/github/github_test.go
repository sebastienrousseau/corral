package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	gh "github.com/google/go-github/v74/github"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newTestClient(rt http.RoundTripper) *gh.Client {
	client := gh.NewClient(&http.Client{Transport: rt})
	u, _ := url.Parse("https://api.test/")
	client.BaseURL = u
	return client
}

func jsonResp(req *http.Request, status int, body string, headers map[string]string) *http.Response {
	h := make(http.Header)
	for k, v := range headers {
		h.Set(k, v)
	}
	return &http.Response{
		StatusCode: status,
		Header:     h,
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

func TestFetchReposWithOptionsAuthModes(t *testing.T) {
	oldGitHub, hadGitHub := os.LookupEnv("GITHUB_TOKEN")
	oldGH, hadGH := os.LookupEnv("GH_TOKEN")
	defer func() {
		restoreEnv(t, "GITHUB_TOKEN", oldGitHub, hadGitHub)
		restoreEnv(t, "GH_TOKEN", oldGH, hadGH)
	}()
	mustUnsetenv(t, "GITHUB_TOKEN")
	mustUnsetenv(t, "GH_TOKEN")

	_, err := resolveToken(context.Background(), AuthModeToken)
	if err == nil || !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Fatalf("expected token env error, got %v", err)
	}

	oldRunAuth := runGitHubCLIAuthToken
	defer func() { runGitHubCLIAuthToken = oldRunAuth }()
	runGitHubCLIAuthToken = func(ctx context.Context) (string, error) {
		return "gh-token", nil
	}

	token, err := resolveToken(context.Background(), AuthModeGH)
	if err != nil {
		t.Fatalf("expected gh token resolution, got %v", err)
	}
	if token != "gh-token" {
		t.Fatalf("expected gh-token, got %q", token)
	}

	mustSetenv(t, "GITHUB_TOKEN", "env-token")
	token, err = resolveToken(context.Background(), AuthModeAuto)
	if err != nil {
		t.Fatalf("expected auto token resolution, got %v", err)
	}
	if token != "env-token" {
		t.Fatalf("expected env-token, got %q", token)
	}
}

func TestFetchReposWithClientOptions(t *testing.T) {
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/users/org1":
			return jsonResp(req, http.StatusOK, `{"type":"Organization"}`, nil), nil
		case "/users/user1":
			return jsonResp(req, http.StatusOK, `{"type":"User"}`, nil), nil
		case "/users/unknown":
			return jsonResp(req, http.StatusNotFound, `{"message":"not found"}`, nil), nil
		case "/users/user_error":
			return jsonResp(req, http.StatusOK, `{"type":"User"}`, nil), nil
		case "/orgs/org1/repos":
			page := req.URL.Query().Get("page")
			if page == "2" {
				return jsonResp(req, http.StatusOK, `[
					{"name":"repo3","language":"Go","visibility":"private","default_branch":"master","clone_url":"http://clone3","ssh_url":"ssh3","archived":true}
				]`, map[string]string{"Link": `<https://api.test/orgs/org1/repos?page=2>; rel="last"`}), nil
			}
			return jsonResp(req, http.StatusOK, `[
				{"name":"repo1","language":"Go","visibility":"public","default_branch":"main","clone_url":"http://clone","ssh_url":"ssh","fork":false,"pushed_at":"2026-01-15T10:00:00Z"},
				{"name":"repo2","visibility":"internal","fork":true}
			]`, map[string]string{"Link": `<https://api.test/orgs/org1/repos?page=2>; rel="next", <https://api.test/orgs/org1/repos?page=2>; rel="last"`}), nil
		case "/users/user1/repos":
			return jsonResp(req, http.StatusOK, `[
				{"name":"userrepo","language":"Rust","visibility":"public","default_branch":"main"}
			]`, nil), nil
		case "/users/user_error/repos":
			return jsonResp(req, http.StatusInternalServerError, `{"message":"boom"}`, nil), nil
		default:
			return nil, fmt.Errorf("unexpected path: %s", req.URL.Path)
		}
	})

	client := newTestClient(rt)

	_, err := FetchReposWithClientOptions(context.Background(), client, "unknown", FetchOptions{Limit: 10})
	if err == nil || !strings.Contains(err.Error(), "failed to get user/org") {
		t.Fatalf("expected owner lookup error, got: %v", err)
	}

	_, err = FetchReposWithClientOptions(context.Background(), client, "user_error", FetchOptions{Limit: 10})
	if err == nil {
		t.Fatalf("expected repo list error")
	}

	repos, err := FetchReposWithClientOptions(context.Background(), client, "org1", FetchOptions{Limit: 10, IncludeArchived: true, IncludeForks: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repos) != 3 {
		t.Fatalf("expected 3 repos, got %d", len(repos))
	}
	if repos[1].Language != "Other" {
		t.Fatalf("expected default language Other, got %q", repos[1].Language)
	}

	want := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	if !repos[0].PushedAt.Equal(want) {
		t.Errorf("expected PushedAt %v on repo1, got %v", want, repos[0].PushedAt)
	}
	if !repos[1].PushedAt.IsZero() {
		t.Errorf("expected zero PushedAt on repo2 (no pushed_at in fixture), got %v", repos[1].PushedAt)
	}

	repos, err = FetchReposWithClientOptions(context.Background(), client, "org1", FetchOptions{Limit: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repos) != 1 {
		t.Fatalf("expected only 1 repo after default filters, got %d", len(repos))
	}

	repos, err = FetchReposWithClientOptions(context.Background(), client, "org1", FetchOptions{Limit: 10, IncludeArchived: true, Visibility: "private", IncludeForks: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repos) != 2 {
		t.Fatalf("expected 2 private repos, got %d", len(repos))
	}

	repos, err = FetchReposWithClientOptions(context.Background(), client, "user1", FetchOptions{Limit: 10, IncludeLanguages: []string{"rust"}, IncludeArchived: true, IncludeForks: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repos) != 1 {
		t.Fatalf("expected 1 repo from language include, got %d", len(repos))
	}

	repos, err = FetchReposWithClientOptions(context.Background(), client, "user1", FetchOptions{Limit: 10, ExcludeLanguages: []string{"rust"}, IncludeArchived: true, IncludeForks: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repos) != 0 {
		t.Fatalf("expected excluded language to remove all repos, got %d", len(repos))
	}

	repos, err = FetchReposWithClient(context.Background(), client, "org1", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repos) != 1 {
		t.Fatalf("expected limit=1, got %d", len(repos))
	}
}

func TestRetryTransport(t *testing.T) {
	var calls int32
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			return jsonResp(req, http.StatusServiceUnavailable, `{"message":"retry"}`, nil), nil
		}
		return jsonResp(req, http.StatusOK, `{"ok":true}`, nil), nil
	})

	client := &http.Client{
		Transport: &retryTransport{
			base:       base,
			maxRetries: 2,
			minBackoff: 10 * time.Millisecond,
			maxBackoff: 20 * time.Millisecond,
		},
		Timeout: time.Second,
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://api.test/foo", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()

	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("expected one retry, got %d calls", calls)
	}
}

func TestShouldRetryHelpers(t *testing.T) {
	resp := &http.Response{Header: make(http.Header), StatusCode: http.StatusForbidden}
	resp.Header.Set("X-RateLimit-Remaining", "0")
	resp.Header.Set("X-RateLimit-Reset", fmt.Sprintf("%d", time.Now().Add(time.Second).Unix()))
	retry, wait := shouldRetry(resp, nil, 0, 1)
	if !retry || wait <= 0 {
		t.Fatalf("expected retry with positive wait for rate limit")
	}

	retry, _ = shouldRetry(nil, errors.New("plain"), 0, 1)
	if retry {
		t.Fatalf("did not expect retry for non-network context error")
	}
}

// netError is a minimal net.Error implementation for exercising the
// retryable-network-error path deterministically.
type netError struct{}

func (netError) Error() string   { return "net error" }
func (netError) Timeout() bool   { return true }
func (netError) Temporary() bool { return true }

func TestShouldRetryAllBranches(t *testing.T) {
	// attempt >= maxRetries: no retry.
	if retry, _ := shouldRetry(nil, nil, 2, 2); retry {
		t.Fatalf("expected no retry when attempt >= maxRetries")
	}

	// Network error -> retry with zero wait.
	if retry, wait := shouldRetry(nil, netError{}, 0, 3); !retry || wait != 0 {
		t.Fatalf("expected retry with zero wait for network error, got %v %v", retry, wait)
	}

	// io.EOF -> retry.
	if retry, _ := shouldRetry(nil, io.EOF, 0, 3); !retry {
		t.Fatalf("expected retry for io.EOF")
	}

	// Non-retryable error -> no retry.
	if retry, _ := shouldRetry(nil, errors.New("boom"), 0, 3); retry {
		t.Fatalf("did not expect retry for non-network error")
	}

	// nil response and nil error -> no retry.
	if retry, _ := shouldRetry(nil, nil, 0, 3); retry {
		t.Fatalf("did not expect retry for nil response")
	}

	// Retry-After header takes precedence.
	respRA := &http.Response{Header: make(http.Header), StatusCode: http.StatusOK}
	respRA.Header.Set("Retry-After", "1")
	if retry, wait := shouldRetry(respRA, nil, 0, 3); !retry || wait != time.Second {
		t.Fatalf("expected retry with Retry-After wait, got %v %v", retry, wait)
	}

	// Retryable status codes.
	for _, code := range []int{
		http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusInternalServerError,
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout,
	} {
		resp := &http.Response{Header: make(http.Header), StatusCode: code}
		if retry, _ := shouldRetry(resp, nil, 0, 3); !retry {
			t.Fatalf("expected retry for status %d", code)
		}
	}

	// Forbidden with rate-limit remaining 0 but no reset header -> retry, 1 minute.
	respFB := &http.Response{Header: make(http.Header), StatusCode: http.StatusForbidden}
	respFB.Header.Set("X-RateLimit-Remaining", "0")
	if retry, wait := shouldRetry(respFB, nil, 0, 3); !retry || wait != time.Minute {
		t.Fatalf("expected retry with one minute wait, got %v %v", retry, wait)
	}

	// Forbidden without rate-limit exhaustion -> no retry.
	respFB2 := &http.Response{Header: make(http.Header), StatusCode: http.StatusForbidden}
	respFB2.Header.Set("X-RateLimit-Remaining", "5")
	if retry, _ := shouldRetry(respFB2, nil, 0, 3); retry {
		t.Fatalf("did not expect retry for forbidden with remaining quota")
	}

	// Non-retryable status -> no retry.
	respOK := &http.Response{Header: make(http.Header), StatusCode: http.StatusNotFound}
	if retry, _ := shouldRetry(respOK, nil, 0, 3); retry {
		t.Fatalf("did not expect retry for 404")
	}
}

func TestIsRetryableNetworkError(t *testing.T) {
	if !isRetryableNetworkError(netError{}) {
		t.Fatalf("expected net.Error to be retryable")
	}
	if !isRetryableNetworkError(io.EOF) {
		t.Fatalf("expected io.EOF to be retryable")
	}
	if isRetryableNetworkError(errors.New("plain")) {
		t.Fatalf("did not expect plain error to be retryable")
	}
}

func TestRetryAfterDuration(t *testing.T) {
	// Missing header.
	resp := &http.Response{Header: make(http.Header)}
	if _, ok := retryAfterDuration(resp); ok {
		t.Fatalf("expected no duration for missing header")
	}

	// Numeric seconds.
	resp.Header.Set("Retry-After", "5")
	if d, ok := retryAfterDuration(resp); !ok || d != 5*time.Second {
		t.Fatalf("expected 5s, got %v %v", d, ok)
	}

	// Negative numeric clamps to 0.
	resp.Header.Set("Retry-After", "-3")
	if d, ok := retryAfterDuration(resp); !ok || d != 0 {
		t.Fatalf("expected 0 for negative, got %v %v", d, ok)
	}

	// HTTP date in the future.
	resp.Header.Set("Retry-After", time.Now().Add(2*time.Hour).UTC().Format(http.TimeFormat))
	if d, ok := retryAfterDuration(resp); !ok || d <= 0 {
		t.Fatalf("expected positive duration for future date, got %v %v", d, ok)
	}

	// HTTP date in the past clamps to 0.
	resp.Header.Set("Retry-After", time.Now().Add(-2*time.Hour).UTC().Format(http.TimeFormat))
	if d, ok := retryAfterDuration(resp); !ok || d != 0 {
		t.Fatalf("expected 0 for past date, got %v %v", d, ok)
	}

	// Unparseable value.
	resp.Header.Set("Retry-After", "not-a-date")
	if _, ok := retryAfterDuration(resp); ok {
		t.Fatalf("expected no duration for unparseable value")
	}
}

func TestRateLimitResetDuration(t *testing.T) {
	resp := &http.Response{Header: make(http.Header)}
	// Missing header.
	if _, ok := rateLimitResetDuration(resp); ok {
		t.Fatalf("expected no duration for missing reset header")
	}

	// Invalid integer.
	resp.Header.Set("X-RateLimit-Reset", "abc")
	if _, ok := rateLimitResetDuration(resp); ok {
		t.Fatalf("expected no duration for invalid reset header")
	}

	// Future reset.
	resp.Header.Set("X-RateLimit-Reset", fmt.Sprintf("%d", time.Now().Add(time.Hour).Unix()))
	if d, ok := rateLimitResetDuration(resp); !ok || d <= 0 {
		t.Fatalf("expected positive reset duration, got %v %v", d, ok)
	}

	// Past reset clamps to 0.
	resp.Header.Set("X-RateLimit-Reset", fmt.Sprintf("%d", time.Now().Add(-time.Hour).Unix()))
	if d, ok := rateLimitResetDuration(resp); !ok || d != 0 {
		t.Fatalf("expected 0 reset duration for past, got %v %v", d, ok)
	}
}

func TestBackoff(t *testing.T) {
	// Defaults applied when zero values supplied.
	tr := &retryTransport{}
	d := tr.backoff(0)
	if d < defaultRetryMinBackoff {
		t.Fatalf("expected at least min backoff, got %v", d)
	}

	// Custom backoff with capping at maxBackoff.
	tr2 := &retryTransport{minBackoff: time.Millisecond, maxBackoff: 2 * time.Millisecond}
	capped := tr2.backoff(10) // 1ms << 10 far exceeds maxBackoff
	if capped < 2*time.Millisecond || capped > 2*time.Millisecond+time.Millisecond {
		t.Fatalf("expected backoff capped around maxBackoff, got %v", capped)
	}
}

func TestRoundTripContextCancellation(t *testing.T) {
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return jsonResp(req, http.StatusServiceUnavailable, `{"message":"retry"}`, nil), nil
	})
	tr := &retryTransport{
		base:       base,
		maxRetries: 5,
		minBackoff: time.Hour, // force a long wait so cancellation wins
		maxBackoff: time.Hour,
	}

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.test/foo", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	cancel() // cancel before the wait so the select picks ctx.Done

	if _, err := tr.RoundTrip(req); err == nil {
		t.Fatalf("expected context cancellation error")
	}
}

func TestRoundTripNilBase(t *testing.T) {
	// base is nil -> RoundTrip falls back to http.DefaultTransport but the
	// request fails to connect; shouldRetry sees a network error and retries
	// up to maxRetries, then returns the final error.
	tr := &retryTransport{base: nil, maxRetries: 0}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet,
		"http://127.0.0.1:0/", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if _, err := tr.RoundTrip(req); err == nil {
		t.Fatalf("expected connection error")
	}
}

func TestEnvToken(t *testing.T) {
	oldGitHub, hadGitHub := os.LookupEnv("GITHUB_TOKEN")
	oldGH, hadGH := os.LookupEnv("GH_TOKEN")
	defer func() {
		restoreEnv(t, "GITHUB_TOKEN", oldGitHub, hadGitHub)
		restoreEnv(t, "GH_TOKEN", oldGH, hadGH)
	}()

	mustUnsetenv(t, "GITHUB_TOKEN")
	mustUnsetenv(t, "GH_TOKEN")
	if got := envToken(); got != "" {
		t.Fatalf("expected empty token, got %q", got)
	}

	mustSetenv(t, "GH_TOKEN", "gh-env")
	if got := envToken(); got != "gh-env" {
		t.Fatalf("expected gh-env, got %q", got)
	}

	mustSetenv(t, "GITHUB_TOKEN", "github-env")
	if got := envToken(); got != "github-env" {
		t.Fatalf("expected github-env precedence, got %q", got)
	}
}

func TestResolveTokenAutoFallback(t *testing.T) {
	oldGitHub, hadGitHub := os.LookupEnv("GITHUB_TOKEN")
	oldGH, hadGH := os.LookupEnv("GH_TOKEN")
	oldRunAuth := runGitHubCLIAuthToken
	defer func() {
		restoreEnv(t, "GITHUB_TOKEN", oldGitHub, hadGitHub)
		restoreEnv(t, "GH_TOKEN", oldGH, hadGH)
		runGitHubCLIAuthToken = oldRunAuth
	}()

	mustUnsetenv(t, "GITHUB_TOKEN")
	mustUnsetenv(t, "GH_TOKEN")

	// Auto mode with no env falls through to gh CLI failure.
	runGitHubCLIAuthToken = func(ctx context.Context) (string, error) {
		return "", errors.New("gh failed")
	}
	if _, err := resolveToken(context.Background(), AuthModeAuto); err == nil ||
		!strings.Contains(err.Error(), "auto mode") {
		t.Fatalf("expected auto-mode failure, got %v", err)
	}

	// Auto mode with successful gh CLI fallback.
	runGitHubCLIAuthToken = func(ctx context.Context) (string, error) {
		return "cli-token", nil
	}
	tok, err := resolveToken(context.Background(), AuthModeAuto)
	if err != nil || tok != "cli-token" {
		t.Fatalf("expected cli-token, got %q %v", tok, err)
	}
}

func TestFetchReposEmptyOwner(t *testing.T) {
	if _, err := FetchRepos(context.Background(), "", 5); err == nil ||
		!strings.Contains(err.Error(), "owner must not be empty") {
		t.Fatalf("expected empty owner error, got %v", err)
	}
}

func TestFetchReposWithClientOptionsGuards(t *testing.T) {
	// Nil client guard.
	if _, err := FetchReposWithClientOptions(context.Background(), nil, "owner", FetchOptions{}); err == nil ||
		!strings.Contains(err.Error(), "github client must not be nil") {
		t.Fatalf("expected nil client error, got %v", err)
	}
	// Empty owner guard.
	client := newTestClient(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return jsonResp(req, http.StatusOK, `{}`, nil), nil
	}))
	if _, err := FetchReposWithClientOptions(context.Background(), client, "  ", FetchOptions{}); err == nil ||
		!strings.Contains(err.Error(), "owner must not be empty") {
		t.Fatalf("expected empty owner error, got %v", err)
	}
}

func TestRunGitHubCLIAuthToken(t *testing.T) {
	if runtime.GOOS == "windows" {
		// The fixture installs a POSIX shell script named "gh" on PATH, which
		// Windows cannot execute (it requires a .exe/.bat/.cmd). The closure is
		// fully covered on the POSIX CI runners.
		t.Skip("fake gh PATH executable fixture is POSIX-specific")
	}

	// Snapshot the real default implementation so other tests that swap the
	// package var cannot interfere with this one.
	realRun := runGitHubCLIAuthToken

	// Error path: a fake gh that exits non-zero.
	dirErr := t.TempDir()
	writeFakeGH(t, dirErr, "#!/bin/sh\nexit 1\n")
	withPath(t, dirErr, func() {
		if _, err := realRun(context.Background()); err == nil ||
			!strings.Contains(err.Error(), "gh auth token failed") {
			t.Fatalf("expected gh failure error, got %v", err)
		}
	})

	// Success path: a fake gh that prints a token.
	dirOK := t.TempDir()
	writeFakeGH(t, dirOK, "#!/bin/sh\necho '  my-token  '\n")
	withPath(t, dirOK, func() {
		tok, err := realRun(context.Background())
		if err != nil || tok != "my-token" {
			t.Fatalf("expected trimmed my-token, got %q %v", tok, err)
		}
	})

	// Empty-token path: a fake gh that prints only whitespace.
	dirEmpty := t.TempDir()
	writeFakeGH(t, dirEmpty, "#!/bin/sh\necho '   '\n")
	withPath(t, dirEmpty, func() {
		if _, err := realRun(context.Background()); err == nil ||
			!strings.Contains(err.Error(), "empty token") {
			t.Fatalf("expected empty token error, got %v", err)
		}
	})
}

func writeFakeGH(t *testing.T, dir, script string) {
	t.Helper()
	path := filepath.Join(dir, "gh")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil { //nolint:gosec // test fixture
		t.Fatalf("write fake gh: %v", err)
	}
}

func withPath(t *testing.T, dir string, fn func()) {
	t.Helper()
	oldPath, hadPath := os.LookupEnv("PATH")
	mustSetenv(t, "PATH", dir)
	defer restoreEnv(t, "PATH", oldPath, hadPath)
	fn()
}

func TestResolveTokenModeToken(t *testing.T) {
	oldGitHub, hadGitHub := os.LookupEnv("GITHUB_TOKEN")
	defer restoreEnv(t, "GITHUB_TOKEN", oldGitHub, hadGitHub)
	mustSetenv(t, "GITHUB_TOKEN", "token-mode")
	tok, err := resolveToken(context.Background(), AuthModeToken)
	if err != nil || tok != "token-mode" {
		t.Fatalf("expected token-mode, got %q %v", tok, err)
	}
}

func TestFetchReposWithOptionsTokenError(t *testing.T) {
	oldGitHub, hadGitHub := os.LookupEnv("GITHUB_TOKEN")
	oldGH, hadGH := os.LookupEnv("GH_TOKEN")
	defer func() {
		restoreEnv(t, "GITHUB_TOKEN", oldGitHub, hadGitHub)
		restoreEnv(t, "GH_TOKEN", oldGH, hadGH)
	}()
	mustUnsetenv(t, "GITHUB_TOKEN")
	mustUnsetenv(t, "GH_TOKEN")

	if _, err := FetchReposWithOptions(context.Background(), "someowner",
		FetchOptions{Limit: 5, AuthMode: AuthModeToken}); err == nil ||
		!strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Fatalf("expected token resolution error, got %v", err)
	}
}

func TestFetchReposWithOptionsRequestPath(t *testing.T) {
	// Drive the full FetchReposWithOptions path (token resolution, client
	// construction, retryTransport, and the ctx == nil branch) without any real
	// network by swapping http.DefaultTransport for a stub that fails fast.
	oldGitHub, hadGitHub := os.LookupEnv("GITHUB_TOKEN")
	oldRunAuth := runGitHubCLIAuthToken
	oldDefault := http.DefaultTransport
	defer func() {
		restoreEnv(t, "GITHUB_TOKEN", oldGitHub, hadGitHub)
		runGitHubCLIAuthToken = oldRunAuth
		http.DefaultTransport = oldDefault
	}()
	mustSetenv(t, "GITHUB_TOKEN", "env-token")

	http.DefaultTransport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("stubbed transport failure")
	})

	// Pass a nil context to exercise the ctx == nil branch, with zero retries
	// and tiny backoff so it returns immediately.
	opts := FetchOptions{
		Limit:           1,
		RetryMax:        -1,
		RetryMinBackoff: time.Millisecond,
		RetryMaxBackoff: time.Millisecond,
	}
	// A typed nil context exercises the ctx == nil guard without tripping
	// staticcheck's SA1012 (which only flags an untyped nil literal).
	var nilCtx context.Context
	if _, err := FetchReposWithOptions(nilCtx, "someowner", opts); err == nil {
		t.Fatalf("expected request error from stubbed transport")
	}
}

func TestNormalizeFetchOptions(t *testing.T) {
	// Negative limit -> 0, empty visibility -> all, invalid visibility -> all,
	// negative retry -> 0, retry backoff coercion (max < min).
	got := normalizeFetchOptions(FetchOptions{
		Limit:           -1,
		Visibility:      "weird",
		RetryMax:        -5,
		RetryMinBackoff: 10 * time.Millisecond,
		RetryMaxBackoff: time.Millisecond, // less than min -> bumped to min
	})
	if got.Limit != 0 {
		t.Fatalf("expected limit 0, got %d", got.Limit)
	}
	if got.Visibility != "all" {
		t.Fatalf("expected visibility all, got %q", got.Visibility)
	}
	if got.RetryMax != 0 {
		t.Fatalf("expected RetryMax 0, got %d", got.RetryMax)
	}
	if got.RetryMaxBackoff != got.RetryMinBackoff {
		t.Fatalf("expected max backoff bumped to min, got %v", got.RetryMaxBackoff)
	}

	// RetryMax == 0 -> default; default backoffs applied.
	def := normalizeFetchOptions(FetchOptions{Visibility: "PUBLIC"})
	if def.RetryMax != defaultRetryMax {
		t.Fatalf("expected default RetryMax, got %d", def.RetryMax)
	}
	if def.Visibility != "public" {
		t.Fatalf("expected lowercased visibility, got %q", def.Visibility)
	}
	if def.RetryMinBackoff != defaultRetryMinBackoff || def.RetryMaxBackoff != defaultRetryMaxBackoff {
		t.Fatalf("expected default backoffs, got %v %v", def.RetryMinBackoff, def.RetryMaxBackoff)
	}
}

func TestOrgTypeForVisibility(t *testing.T) {
	cases := map[string]string{
		"public":  "public",
		"private": "private",
		"all":     "all",
		"other":   "all",
	}
	for in, want := range cases {
		if got := orgTypeForVisibility(in); got != want {
			t.Fatalf("orgTypeForVisibility(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestToLookupSet(t *testing.T) {
	if got := toLookupSet(nil); got != nil {
		t.Fatalf("expected nil for empty input")
	}
	// All-blank entries collapse to nil.
	if got := toLookupSet([]string{"", "  "}); got != nil {
		t.Fatalf("expected nil for all-blank input, got %v", got)
	}
	got := toLookupSet([]string{"Go", " Rust ", ""})
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got))
	}
	if _, ok := got["go"]; !ok {
		t.Fatalf("expected normalized 'go' key")
	}
	if _, ok := got["rust"]; !ok {
		t.Fatalf("expected trimmed normalized 'rust' key")
	}
}

func TestMatchesFilters(t *testing.T) {
	base := normalizeFetchOptions(FetchOptions{IncludeForks: true, IncludeArchived: true})

	// Fork excluded when IncludeForks false.
	if matchesFilters(Repo{Fork: true}, nil, nil, normalizeFetchOptions(FetchOptions{})) {
		t.Fatalf("expected fork to be filtered out")
	}
	// Archived excluded when IncludeArchived false.
	if matchesFilters(Repo{Archived: true}, nil, nil, normalizeFetchOptions(FetchOptions{IncludeForks: true})) {
		t.Fatalf("expected archived to be filtered out")
	}
	// Visibility public mismatch.
	pubOpts := base
	pubOpts.Visibility = "public"
	if matchesFilters(Repo{Visibility: "Private"}, nil, nil, pubOpts) {
		t.Fatalf("expected private repo filtered when visibility public")
	}
	// Visibility private mismatch.
	privOpts := base
	privOpts.Visibility = "private"
	if matchesFilters(Repo{Visibility: "Public"}, nil, nil, privOpts) {
		t.Fatalf("expected public repo filtered when visibility private")
	}
	// Include language mismatch.
	if matchesFilters(Repo{Language: "Go"}, toLookupSet([]string{"rust"}), nil, base) {
		t.Fatalf("expected non-matching include language to filter out")
	}
	// Exclude language match.
	if matchesFilters(Repo{Language: "Go"}, nil, toLookupSet([]string{"go"}), base) {
		t.Fatalf("expected excluded language to filter out")
	}
	// Passing case.
	if !matchesFilters(Repo{Language: "Go", Visibility: "Public"},
		toLookupSet([]string{"go"}), toLookupSet([]string{"rust"}), base) {
		t.Fatalf("expected repo to pass filters")
	}
}

func restoreEnv(t *testing.T, key, val string, had bool) {
	t.Helper()
	if had {
		if err := os.Setenv(key, val); err != nil {
			t.Fatalf("restore %s: %v", key, err)
		}
		return
	}
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset %s: %v", key, err)
	}
}

func mustSetenv(t *testing.T, key, val string) {
	t.Helper()
	if err := os.Setenv(key, val); err != nil {
		t.Fatalf("setenv %s: %v", key, err)
	}
}

func mustUnsetenv(t *testing.T, key string) {
	t.Helper()
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unsetenv %s: %v", key, err)
	}
}

func TestFetchReposAuthenticatedUser(t *testing.T) {
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/user":
			// The authenticated user is "me".
			return jsonResp(req, http.StatusOK, `{"login":"me","type":"User"}`, nil), nil
		case "/users/me":
			return jsonResp(req, http.StatusOK, `{"login":"me","type":"User"}`, nil), nil
		case "/user/repos":
			// The authenticated-user endpoint exposes private repositories.
			return jsonResp(req, http.StatusOK, `[
				{"name":"secret","language":"Go","visibility":"private","default_branch":"main"}
			]`, nil), nil
		case "/users/other":
			return jsonResp(req, http.StatusOK, `{"login":"other","type":"User"}`, nil), nil
		case "/users/other/repos":
			return jsonResp(req, http.StatusOK, `[
				{"name":"public-only","language":"Go","visibility":"public","default_branch":"main"}
			]`, nil), nil
		default:
			return nil, fmt.Errorf("unexpected path: %s", req.URL.Path)
		}
	})
	client := newTestClient(rt)

	// Owner is the authenticated user: list via /user/repos so private repos appear.
	repos, err := FetchReposWithClientOptions(context.Background(), client, "me", FetchOptions{Limit: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repos) != 1 || repos[0].Name != "secret" || repos[0].Visibility != "Private" {
		t.Fatalf("expected the authenticated user's private repo, got %+v", repos)
	}

	// Owner differs from the authenticated user: fall back to the public listing.
	repos, err = FetchReposWithClientOptions(context.Background(), client, "other", FetchOptions{Limit: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repos) != 1 || repos[0].Name != "public-only" {
		t.Fatalf("expected fallback to public listing, got %+v", repos)
	}
}

func TestToken(t *testing.T) {
	oldGitHub, hadGitHub := os.LookupEnv("GITHUB_TOKEN")
	oldGH, hadGH := os.LookupEnv("GH_TOKEN")
	defer func() {
		restoreEnv(t, "GITHUB_TOKEN", oldGitHub, hadGitHub)
		restoreEnv(t, "GH_TOKEN", oldGH, hadGH)
	}()

	mustSetenv(t, "GITHUB_TOKEN", "tok-123")
	if got := Token(context.Background(), AuthModeToken); got != "tok-123" {
		t.Errorf("Token() = %q, want tok-123", got)
	}

	mustUnsetenv(t, "GITHUB_TOKEN")
	mustUnsetenv(t, "GH_TOKEN")
	if got := Token(context.Background(), AuthModeToken); got != "" {
		t.Errorf("Token() with no credentials = %q, want empty", got)
	}
}
