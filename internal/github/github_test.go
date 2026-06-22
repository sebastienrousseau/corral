package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
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
	os.Unsetenv("GITHUB_TOKEN")
	os.Unsetenv("GH_TOKEN")

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

	os.Setenv("GITHUB_TOKEN", "env-token")
	defer os.Unsetenv("GITHUB_TOKEN")
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
				{"name":"repo1","language":"Go","visibility":"public","default_branch":"main","clone_url":"http://clone","ssh_url":"ssh","fork":false},
				{"name":"repo2","visibility":"internal","fork":true}
			]`, map[string]string{"Link": `<https://api.test/orgs/org1/repos?page=2>; rel="next"`}), nil
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
