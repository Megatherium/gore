package github_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/leoherzog/reprox/internal/github"
)

func TestGitHubClientPaginationAndFiltering(t *testing.T) {
	published := "2024-01-01T00:00:00Z"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		if page == "1" {
			// Page 1: 100 releases (including 1 draft and 1 prerelease)
			var releases []github.GitHubRelease
			for i := 1; i <= 100; i++ {
				rel := github.GitHubRelease{
					ID:          int64(i),
					TagName:     fmt.Sprintf("v%d.0.0", i),
					PublishedAt: &published,
				}
				if i == 50 {
					rel.PublishedAt = nil // Draft
				}
				if i == 51 {
					rel.Prerelease = true
				}
				releases = append(releases, rel)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(releases)
		} else if page == "2" {
			// Page 2: 50 releases
			var releases []github.GitHubRelease
			for i := 101; i <= 150; i++ {
				releases = append(releases, github.GitHubRelease{
					ID:          int64(i),
					TagName:     fmt.Sprintf("v%d.0.0", i),
					PublishedAt: &published,
				})
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(releases)
		} else {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]github.GitHubRelease{})
		}
	}))
	defer server.Close()

	client := github.NewClient("fake-token", server.Client())
	client.SetBaseURL(server.URL)

	ctx := context.Background()

	t.Run("stable only", func(t *testing.T) {
		releases, err := client.GetAllReleases(ctx, "owner", "repo", false)
		if err != nil {
			t.Fatalf("GetAllReleases error: %v", err)
		}
		// 150 total - 1 draft (i=50) - 1 prerelease (i=51) = 148 releases
		if len(releases) != 148 {
			t.Errorf("expected 148 releases, got %d", len(releases))
		}
	})

	t.Run("include prereleases", func(t *testing.T) {
		releases, err := client.GetAllReleases(ctx, "owner", "repo", true)
		if err != nil {
			t.Fatalf("GetAllReleases error: %v", err)
		}
		// 150 total - 1 draft = 149 releases
		if len(releases) != 149 {
			t.Errorf("expected 149 releases, got %d", len(releases))
		}
	})
}

func TestAssetFilters(t *testing.T) {
	assets := []github.GitHubAsset{
		{Name: "app_1.0_amd64.deb", Digest: "sha256:1234"},
		{Name: "old_1.0_amd64.deb", Digest: ""}, // No digest -> excluded
		{Name: "app-1.0-1.x86_64.rpm", Digest: "sha256:5678"},
		{Name: "old-1.0-1.x86_64.rpm", Digest: ""}, // No digest -> excluded
		{Name: "app_1.0_arm64.deb", Digest: "sha256:9999"},
		{Name: "source.tar.gz", Digest: "sha256:aaaa"},
	}

	debAssets := github.FilterDebAssets(assets)
	if len(debAssets) != 2 {
		t.Fatalf("expected 2 deb assets, got %d", len(debAssets))
	}

	rpmAssets := github.FilterRpmAssets(assets)
	if len(rpmAssets) != 1 {
		t.Fatalf("expected 1 rpm asset, got %d", len(rpmAssets))
	}

	archs := github.GetArchitecturesFromAssets(debAssets)
	if len(archs) != 2 || archs[0] != "amd64" || archs[1] != "arm64" {
		t.Errorf("unexpected architectures: %v", archs)
	}
}
