package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/leoherzog/reprox/internal/route"
	"github.com/leoherzog/reprox/internal/store"
	"github.com/leoherzog/reprox/internal/xutil"
)

// hopByHopHeaders must not be forwarded when proxying upstream responses.
var hopByHopHeaders = map[string]bool{
	"Connection":          true,
	"Proxy-Connection":    true,
	"Keep-Alive":          true,
	"Proxy-Authenticate":  true,
	"Proxy-Authorization": true,
	"Te":                  true,
	"Trailer":             true,
	"Transfer-Encoding":   true,
	"Upgrade":             true,
}

func (s *Service) handleBinaryRedirect(ctx context.Context, w http.ResponseWriter, r *http.Request, rInfo route.RouteInfo, packageType string) error {
	owner, repo, filename, variant := rInfo.Owner, rInfo.Repo, rInfo.Filename, rInfo.ReleaseVariant
	typeName := "Asset"
	if packageType == "rpm" {
		typeName = "RPM package"
	}

	cachedHashEntry, foundHash, _ := s.store.Get(ctx, store.ReleaseIdsHashKey(variant, owner, repo))
	if foundHash && string(cachedHashEntry.Body) != "" {
		releaseHash := string(cachedHashEntry.Body)
		urlKey := store.AssetURLKey(variant, owner, repo, releaseHash, filename)
		cachedUrl, foundUrl, _ := s.store.Get(ctx, urlKey)
		if foundUrl && string(cachedUrl.Body) != "" {
			return s.serveAsset(ctx, w, r, string(cachedUrl.Body))
		}
	}

	// Cache miss: fetch releases
	includePrerelease := variant == "prerelease"
	releases, err := s.github.GetAllReleases(ctx, owner, repo, includePrerelease)
	if err != nil || len(releases) == 0 {
		http.Error(w, fmt.Sprintf("%s not found: %s", typeName, filename), http.StatusNotFound)
		return nil
	}

	var minimalReleases []store.GitHubReleaseMinimal
	for _, rel := range releases {
		var minAssets []store.GitHubAssetMinimal
		for _, a := range rel.Assets {
			minAssets = append(minAssets, store.GitHubAssetMinimal{
				Name:   a.Name,
				Size:   a.Size,
				Digest: a.Digest,
			})
		}
		minimalReleases = append(minimalReleases, store.GitHubReleaseMinimal{
			ID:     rel.ID,
			Assets: minAssets,
		})
	}
	releaseHash := store.ComputeReleaseIdsHash(minimalReleases)

	for _, rel := range releases {
		for _, asset := range rel.Assets {
			if asset.Name == filename {
				urlKey := store.AssetURLKey(variant, owner, repo, releaseHash, filename)
				_ = s.store.Put(ctx, urlKey, store.Entry{
					Body:        []byte(asset.BrowserDownloadURL),
					ContentType: "text/plain",
					ExpiresAt:   time.Now().Add(s.cacheTTL),
				})
				return s.serveAsset(ctx, w, r, asset.BrowserDownloadURL)
			}
		}
	}

	http.Error(w, fmt.Sprintf("%s not found: %s", typeName, filename), http.StatusNotFound)
	return nil
}

func (s *Service) isPubliclyAccessible(ctx context.Context, downloadURL string) bool {
	// Accessibility rarely changes; cache the probe result to avoid a HEAD
	// request on every package download.
	cacheKey := "asset-public/" + xutil.SHA256String(downloadURL)
	if cached, ok, _ := s.store.Get(ctx, cacheKey); ok {
		return string(cached.Body) == "1"
	}

	accessible := s.probePubliclyAccessible(ctx, downloadURL)

	marker := "0"
	if accessible {
		marker = "1"
	}
	_ = s.store.Put(ctx, cacheKey, store.Entry{
		Body:        []byte(marker),
		ContentType: "text/plain",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	})

	return accessible
}

func (s *Service) probePubliclyAccessible(ctx context.Context, downloadURL string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, downloadURL, nil)
	if err != nil {
		return false
	}
	// Follow redirects up to 10
	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

func (s *Service) serveAsset(ctx context.Context, w http.ResponseWriter, r *http.Request, downloadURL string) error {
	if s.isPubliclyAccessible(ctx, downloadURL) {
		http.Redirect(w, r, downloadURL, http.StatusFound)
		return nil
	}

	// Private repo: proxy through service with auth header
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return err
	}
	req.Header = s.github.Headers("octet-stream")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		http.Error(w, fmt.Sprintf("Failed to fetch asset: %d", resp.StatusCode), resp.StatusCode)
		return nil
	}

	for k, vv := range resp.Header {
		if hopByHopHeaders[http.CanonicalHeaderKey(k)] {
			continue
		}
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.WriteHeader(http.StatusOK)

	_, err = io.Copy(w, resp.Body)
	return err
}
