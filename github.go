package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const githubAPIURL = "https://api.github.com"

// GitHubClient provides a small wrapper around the GitHub REST API.
type GitHubClient struct {
	Token      string
	HTTPClient *http.Client
	BaseURL    string
}

// NewGitHubClient creates a client using the provided personal access token.
func NewGitHubClient(token string) *GitHubClient {
	return &GitHubClient{
		Token:      token,
		HTTPClient: http.DefaultClient,
		BaseURL:    githubAPIURL,
	}
}

func (c *GitHubClient) request(method, path string) (*http.Response, error) {
	url := c.BaseURL + path
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "token "+c.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	return c.HTTPClient.Do(req)
}

// GetUsername returns the login of the authenticated user.
func (c *GitHubClient) GetUsername() (string, error) {
	resp, err := c.request("GET", "/user")
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, body)
	}

	var user struct {
		Login string `json:"login"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return "", err
	}
	if user.Login == "" {
		return "", fmt.Errorf("GitHub user login is empty")
	}
	return user.Login, nil
}

// FindRepo searches for a repository owned by username with the given name
// and returns its HTTPS clone URL. It requires the repo to exist and be visible
// to the authenticated token.
func (c *GitHubClient) FindRepo(name, username string) (string, error) {
	path := fmt.Sprintf("/repos/%s/%s", username, name)
	resp, err := c.request("GET", path)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("repository %s/%s not found", username, name)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, body)
	}

	var repo struct {
		CloneURL string `json:"clone_url"`
		HTMLURL  string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&repo); err != nil {
		return "", err
	}
	if repo.CloneURL == "" {
		return "", fmt.Errorf("repository has no clone_url")
	}
	return repo.CloneURL, nil
}
