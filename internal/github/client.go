package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/Megatherium/gore/internal/xutil"
)

const (
	APIBaseURL = "https://api.github.com"
	MaxPages   = 50
	UserAgent  = "gore/1.0"
)

// GitHubAsset represents a binary asset uploaded to a release.
type GitHubAsset struct {
	ID                 int64  `json:"id"`
	Name               string `json:"name"`
	Size               int64  `json:"size"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Digest             string `json:"digest,omitempty"` // "sha256:..."
}

// GitHubRelease represents a GitHub release.
type GitHubRelease struct {
	ID          int64         `json:"id"`
	TagName     string        `json:"tag_name"`
	Name        string        `json:"name"`
	Body        string        `json:"body"`
	PublishedAt *string       `json:"published_at"` // null indicates draft release
	Prerelease  bool          `json:"prerelease"`
	Assets      []GitHubAsset `json:"assets"`
}

// RepoSearchResultItem represents an item returned by GitHub repository search.
type RepoSearchResultItem struct {
	FullName        string  `json:"full_name"`
	Description     *string `json:"description"`
	StargazersCount int     `json:"stargazers_count"`
}

type repoSearchResponse struct {
	Items []RepoSearchResultItem `json:"items"`
}

// Client interacts with GitHub REST API.
type Client struct {
	token      string
	httpClient *http.Client
	baseURL    string
}

// NewClient creates a new GitHub API client.
func NewClient(token string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{
		token:      token,
		httpClient: httpClient,
		baseURL:    APIBaseURL,
	}
}

// SetBaseURL overrides the base API URL (useful for mocking).
func (c *Client) SetBaseURL(url string) {
	c.baseURL = url
}

// Headers returns standard GitHub request headers.
func (c *Client) Headers(accept string) http.Header {
	h := make(http.Header)
	if accept == "" || accept == "json" {
		h.Set("Accept", "application/vnd.github.v3+json")
	} else if accept == "octet-stream" {
		h.Set("Accept", "application/octet-stream")
	} else {
		h.Set("Accept", accept)
	}
	h.Set("User-Agent", UserAgent)
	if c.token != "" {
		h.Set("Authorization", "token "+c.token)
	}
	return h
}

// GetAllReleases fetches all releases for a repository with pagination, excluding drafts.
func (c *Client) GetAllReleases(ctx context.Context, owner, repo string, includePrerelease bool) ([]GitHubRelease, error) {
	var allReleases []GitHubRelease
	page := 1

	for page <= MaxPages {
		reqURL := fmt.Sprintf("%s/repos/%s/%s/releases?per_page=100&page=%d", c.baseURL, owner, repo, page)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header = c.Headers("json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			if resp.StatusCode == http.StatusNotFound {
				return nil, fmt.Errorf("repository %s/%s not found or has no releases", owner, repo)
			}
			if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
				return nil, fmt.Errorf("GitHub API rate limit exceeded")
			}
			return nil, fmt.Errorf("GitHub API error: %s", resp.Status)
		}

		var pageReleases []GitHubRelease
		if err := json.NewDecoder(resp.Body).Decode(&pageReleases); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("failed to decode GitHub releases JSON: %w", err)
		}
		resp.Body.Close()

		if len(pageReleases) == 0 {
			break
		}

		for _, r := range pageReleases {
			// Filter 1: Drafts (published_at == nil) are always excluded
			if r.PublishedAt == nil {
				continue
			}
			// Filter 2: Prereleases unless includePrerelease is true
			if r.Prerelease && !includePrerelease {
				continue
			}
			allReleases = append(allReleases, r)
		}

		if len(pageReleases) < 100 {
			break
		}
		page++
	}

	return allReleases, nil
}

// GetLatestRelease fetches the latest release for a repository.
func (c *Client) GetLatestRelease(ctx context.Context, owner, repo string) (*GitHubRelease, error) {
	reqURL := fmt.Sprintf("%s/repos/%s/%s/releases/latest", c.baseURL, owner, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header = c.Headers("json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API error: %s", resp.Status)
	}

	var rel GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

// SearchRepositories proxies GitHub repository search API.
func (c *Client) SearchRepositories(ctx context.Context, query string) ([]RepoSearchResultItem, error) {
	reqURL := fmt.Sprintf("%s/search/repositories?q=%s+in:name&per_page=8&sort=stars", c.baseURL, url.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header = c.Headers("json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub search API error: %s", resp.Status)
	}

	var res repoSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	return res.Items, nil
}

// FilterDebAssets filters for .deb assets with a valid SHA-256 digest.
func FilterDebAssets(assets []GitHubAsset) []GitHubAsset {
	var result []GitHubAsset
	for _, a := range assets {
		if strings.HasSuffix(a.Name, ".deb") && strings.HasPrefix(a.Digest, "sha256:") {
			result = append(result, a)
		}
	}
	return result
}

// FilterRpmAssets filters for .rpm assets with a valid SHA-256 digest.
func FilterRpmAssets(assets []GitHubAsset) []GitHubAsset {
	var result []GitHubAsset
	for _, a := range assets {
		if strings.HasSuffix(a.Name, ".rpm") && strings.HasPrefix(a.Digest, "sha256:") {
			result = append(result, a)
		}
	}
	return result
}

// FilterByArchitecture filters deb assets by architecture matching.
func FilterByArchitecture(assets []GitHubAsset, arch string) []GitHubAsset {
	var result []GitHubAsset
	for _, a := range assets {
		assetArch := xutil.ExtractArchFromFilename(a.Name)
		if assetArch == arch {
			result = append(result, a)
		}
	}
	return result
}

// GetArchitecturesFromAssets extracts sorted unique architectures from Debian assets.
func GetArchitecturesFromAssets(assets []GitHubAsset) []string {
	archSet := make(map[string]bool)
	for _, a := range assets {
		if strings.HasSuffix(a.Name, ".deb") {
			archSet[xutil.ExtractArchFromFilename(a.Name)] = true
		}
	}

	archs := make([]string, 0, len(archSet))
	for arch := range archSet {
		archs = append(archs, arch)
	}
	sort.Strings(archs)
	return archs
}
