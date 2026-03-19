// Package tools provides utility functions for proxy management.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// GitHubRelease represents a GitHub release.
type GitHubRelease struct {
	TagName string
	Assets  []GitHubAsset
}

// GitHubAsset represents a release asset.
type GitHubAsset struct {
	Name string
	URL  string
}

// GitHubReleaseClient queries GitHub releases.
type GitHubReleaseClient struct {
	HTTPClient *http.Client
	BaseURL    string
}

// NewGitHubReleaseClient creates a new client.
func NewGitHubReleaseClient() *GitHubReleaseClient {
	return &GitHubReleaseClient{
		HTTPClient: &http.Client{},
		BaseURL:    "https://api.github.com",
	}
}

// FetchRelease fetches a release from GitHub.
func (c *GitHubReleaseClient) FetchRelease(ctx context.Context, owner, repo, tag string) (*GitHubRelease, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/releases/%s", c.BaseURL, owner, repo, tag)
	if tag == "latest" {
		url = fmt.Sprintf("%s/repos/%s/%s/releases/latest", c.BaseURL, owner, repo)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var release struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}

	result := &GitHubRelease{TagName: release.TagName}
	for _, a := range release.Assets {
		result.Assets = append(result.Assets, GitHubAsset{Name: a.Name, URL: a.BrowserDownloadURL})
	}
	return result, nil
}

// NormalizeTag normalizes a release tag.
func NormalizeTag(tag string) string {
	t := strings.TrimSpace(tag)
	if t == "" {
		return "latest"
	}
	return strings.TrimPrefix(t, "v")
}
