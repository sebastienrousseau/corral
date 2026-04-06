package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/go-github/v60/github"
)

func TestFetchRepos(t *testing.T) {
	// Test missing token
	os.Unsetenv("GITHUB_TOKEN")
	_, err := FetchRepos("owner", 10)
	if err == nil || err.Error() != "GITHUB_TOKEN environment variable not set" {
		t.Errorf("Expected error for missing GITHUB_TOKEN")
	}

	os.Setenv("GITHUB_TOKEN", "dummy")
	defer os.Unsetenv("GITHUB_TOKEN")

	// Call FetchRepos directly to cover its initialization
	_, _ = FetchRepos("owner", 10)

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	client := github.NewClient(server.Client())
	u, _ := url.Parse(server.URL + "/")
	client.BaseURL = u

	// Mock endpoints
	mux.HandleFunc("/users/org1", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"type": "Organization"}`))
	})
	mux.HandleFunc("/users/user1", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"type": "User"}`))
	})
	mux.HandleFunc("/users/unknown", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	mux.HandleFunc("/orgs/org1/repos", func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		if page == "2" {
			w.Header().Set("Link", `<`+server.URL+`/orgs/org1/repos?page=2>; rel="last"`)
			w.Write([]byte(`[
				{"name": "repo3", "language": "Go", "visibility": "private", "default_branch": "master", "clone_url": "http://clone3", "ssh_url": "ssh3"}
			]`))
			return
		}
		w.Header().Set("Link", `<`+server.URL+`/orgs/org1/repos?page=2>; rel="next"`)
		w.Write([]byte(`[
			{"name": "repo1", "language": "Go", "visibility": "public", "default_branch": "main", "clone_url": "http://clone", "ssh_url": "ssh"},
			{"name": "repo2", "visibility": "internal"}
		]`))
	})

	mux.HandleFunc("/users/user1/repos", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[
			{"name": "userrepo", "language": "Go", "visibility": "public", "default_branch": "main"}
		]`))
	})

	mux.HandleFunc("/users/user_error/repos", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	mux.HandleFunc("/users/user_error", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"type": "User"}`))
	})

	// Test user fetch error
	_, err = FetchReposWithClient(context.Background(), client, "unknown", 10)
	if err == nil || !strings.Contains(err.Error(), "failed to get user/org") {
		t.Errorf("Expected fetch error for unknown user, got: %v", err)
	}

	// Test repo list error
	_, err = FetchReposWithClient(context.Background(), client, "user_error", 10)
	if err == nil {
		t.Errorf("Expected fetch error for user_error")
	}

	// Test Org fetch success
	repos, err := FetchReposWithClient(context.Background(), client, "org1", 10)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if len(repos) != 3 {
		t.Errorf("Expected 3 repos, got %d", len(repos))
	}
	if repos[0].Visibility != "Public" || repos[1].Visibility != "Private" || repos[2].Visibility != "Private" {
		t.Errorf("Visibility parsing incorrect")
	}
	if repos[1].Language != "Other" {
		t.Errorf("Language default incorrect")
	}

	// Test User fetch success with limit
	repos, err = FetchReposWithClient(context.Background(), client, "user1", 1)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if len(repos) != 1 {
		t.Errorf("Expected 1 repo, got %d", len(repos))
	}

	// Test limit logic
	repos, err = FetchReposWithClient(context.Background(), client, "org1", 1)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if len(repos) != 1 {
		t.Errorf("Expected 1 repo due to limit, got %d", len(repos))
	}
}
