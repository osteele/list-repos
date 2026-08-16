package vcs

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGitHubClientGetUsername(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "token test-token" {
			t.Fatalf("unexpected authorization header: %s", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"login": "testuser"})
	}))
	defer server.Close()

	client := NewGitHubClient("test-token")
	client.BaseURL = server.URL

	username, err := client.GetUsername()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if username != "testuser" {
		t.Fatalf("expected testuser, got %s", username)
	}
}

func TestGitHubClientFindRepo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/testuser/myrepo" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"clone_url": "https://github.com/testuser/myrepo.git",
		})
	}))
	defer server.Close()

	client := NewGitHubClient("test-token")
	client.BaseURL = server.URL

	url, err := client.FindRepo("myrepo", "testuser")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "https://github.com/testuser/myrepo.git" {
		t.Fatalf("unexpected clone url: %s", url)
	}
}

func TestGitHubClientFindRepoNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := NewGitHubClient("test-token")
	client.BaseURL = server.URL

	_, err := client.FindRepo("missing", "testuser")
	if err == nil {
		t.Fatal("expected error for missing repo")
	}
}

func TestGitHubClientDoesNotLeakToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		auth := r.Header.Get("Authorization")
		if auth != "token secret-token" {
			t.Fatalf("unexpected authorization: %s", auth)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"login": "testuser"})
	}))
	defer server.Close()

	client := NewGitHubClient("secret-token")
	client.BaseURL = server.URL

	username, err := client.GetUsername()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if username != "testuser" {
		t.Fatalf("expected testuser, got %s", username)
	}
}
