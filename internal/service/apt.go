package service

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Megatherium/gore/internal/gen/apt"
	"github.com/Megatherium/gore/internal/github"
	"github.com/Megatherium/gore/internal/parse/deb"
	"github.com/Megatherium/gore/internal/route"
	"github.com/Megatherium/gore/internal/store"
	"github.com/Megatherium/gore/internal/xutil"
)

var (
	reSha256Line = regexp.MustCompile(`^([0-9a-f]{64})\s+\d+\s+(\S.*)$`)
)

func (s *Service) handleInRelease(ctx context.Context, w http.ResponseWriter, r *http.Request, rInfo route.RouteInfo) error {
	owner, repo, variant := rInfo.Owner, rInfo.Repo, rInfo.ReleaseVariant

	// Stale-while-revalidate: serve cached content immediately and refresh in the
	// background only when the 5-minute release-ids-hash gate has expired.
	cachedInRelease, foundContent, _ := s.store.Get(ctx, store.InReleaseKey(variant, owner, repo))
	if foundContent {
		s.maybeTriggerAptRefresh(ctx, rInfo)
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Cache-Control", "public, max-age=300")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(cachedInRelease.Body)
		return nil
	}

	res, err, _ := s.sf.Do(fmt.Sprintf("apt:%s:%s:%s", variant, owner, repo), func() (any, error) {
		return s.generateReleaseContent(ctx, rInfo)
	})
	if err != nil {
		return err
	}
	releaseContent := res.(string)

	var inRelease string
	if s.signer != nil {
		signed, err := s.signer.SignCleartext(releaseContent)
		if err != nil {
			return fmt.Errorf("failed to sign InRelease: %w", err)
		}
		inRelease = signed
	} else {
		inRelease = releaseContent
	}

	_ = s.store.Put(ctx, store.InReleaseKey(variant, owner, repo), store.Entry{
		Body:        []byte(inRelease),
		ContentType: "text/plain",
		ExpiresAt:   time.Now().Add(s.cacheTTL),
	})
	_ = s.store.Put(ctx, store.ReleaseKey(variant, owner, repo), store.Entry{
		Body:        []byte(releaseContent),
		ContentType: "text/plain",
		ExpiresAt:   time.Now().Add(s.cacheTTL),
	})

	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(inRelease))
	return nil
}

func (s *Service) handleRelease(ctx context.Context, w http.ResponseWriter, r *http.Request, rInfo route.RouteInfo) error {
	owner, repo, variant := rInfo.Owner, rInfo.Repo, rInfo.ReleaseVariant

	cachedRelease, foundContent, _ := s.store.Get(ctx, store.ReleaseKey(variant, owner, repo))
	if foundContent {
		s.maybeTriggerAptRefresh(ctx, rInfo)
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Cache-Control", "public, max-age=300")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(cachedRelease.Body)
		return nil
	}

	res, err, _ := s.sf.Do(fmt.Sprintf("apt:%s:%s:%s", variant, owner, repo), func() (any, error) {
		return s.generateReleaseContent(ctx, rInfo)
	})
	if err != nil {
		return err
	}
	releaseContent := res.(string)

	_ = s.store.Put(ctx, store.ReleaseKey(variant, owner, repo), store.Entry{
		Body:        []byte(releaseContent),
		ContentType: "text/plain",
		ExpiresAt:   time.Now().Add(s.cacheTTL),
	})

	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(releaseContent))
	return nil
}

func (s *Service) handleReleaseGpg(ctx context.Context, w http.ResponseWriter, r *http.Request, rInfo route.RouteInfo) error {
	if s.signer == nil {
		http.Error(w, "No GPG key configured for signing", http.StatusNotFound)
		return nil
	}

	owner, repo, variant := rInfo.Owner, rInfo.Repo, rInfo.ReleaseVariant

	cachedSig, foundSig, _ := s.store.Get(ctx, store.ReleaseGpgKey(variant, owner, repo))
	if foundSig {
		s.maybeTriggerAptRefresh(ctx, rInfo)
		w.Header().Set("Content-Type", "application/pgp-signature")
		w.Header().Set("Cache-Control", "public, max-age=300")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(cachedSig.Body)
		return nil
	}

	var releaseContent string
	cachedRel, foundRel, _ := s.store.Get(ctx, store.ReleaseKey(variant, owner, repo))
	if foundRel {
		releaseContent = string(cachedRel.Body)
	} else {
		res, err, _ := s.sf.Do(fmt.Sprintf("apt:%s:%s:%s", variant, owner, repo), func() (any, error) {
			return s.generateReleaseContent(ctx, rInfo)
		})
		if err != nil {
			return err
		}
		releaseContent = res.(string)
		_ = s.store.Put(ctx, store.ReleaseKey(variant, owner, repo), store.Entry{
			Body:        []byte(releaseContent),
			ContentType: "text/plain",
			ExpiresAt:   time.Now().Add(s.cacheTTL),
		})
	}

	signature, err := s.signer.SignDetachedText(releaseContent)
	if err != nil {
		return err
	}

	_ = s.store.Put(ctx, store.ReleaseGpgKey(variant, owner, repo), store.Entry{
		Body:        []byte(signature),
		ContentType: "application/pgp-signature",
		ExpiresAt:   time.Now().Add(s.cacheTTL),
	})

	w.Header().Set("Content-Type", "application/pgp-signature")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(signature))
	return nil
}

func (s *Service) handlePackages(ctx context.Context, w http.ResponseWriter, r *http.Request, rInfo route.RouteInfo, gz bool) error {
	return s.servePackagesFromBlob(ctx, w, r, rInfo, gz)
}

func (s *Service) handleByHash(ctx context.Context, w http.ResponseWriter, r *http.Request, rInfo route.RouteInfo) error {
	if rInfo.HashType != "SHA256" {
		http.Error(w, fmt.Sprintf("Unsupported hash type: %s", rInfo.HashType), http.StatusNotFound)
		return nil
	}

	key := store.PackagesBlobKey(rInfo.ReleaseVariant, rInfo.Owner, rInfo.Repo, rInfo.Hash)
	blob, found, err := s.store.Get(ctx, key)
	if err != nil || !found {
		http.Error(w, fmt.Sprintf("Hash not found: %s", rInfo.Hash), http.StatusNotFound)
		return nil
	}

	contentType := blob.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(blob.Body)
	return nil
}

func (s *Service) servePackagesFromBlob(ctx context.Context, w http.ResponseWriter, r *http.Request, rInfo route.RouteInfo, gz bool) error {
	owner, repo, component, arch, variant := rInfo.Owner, rInfo.Repo, rInfo.Component, rInfo.Architecture, rInfo.ReleaseVariant
	contentType := "text/plain"
	emptyBody := []byte("")
	if gz {
		contentType = "application/gzip"
		gzEmpty, _ := xutil.GzipCompress(emptyBody)
		emptyBody = gzEmpty
	}

	cachedRel, foundRel, _ := s.store.Get(ctx, store.ReleaseKey(variant, owner, repo))
	var releaseContent string
	refreshed := false

	if foundRel {
		releaseContent = string(cachedRel.Body)
	} else {
		res, err, _ := s.sf.Do(fmt.Sprintf("apt:%s:%s:%s", variant, owner, repo), func() (any, error) {
			return s.generateReleaseContent(ctx, rInfo)
		})
		if err != nil {
			w.Header().Set("Content-Type", contentType)
			w.Header().Set("Cache-Control", "public, max-age=300")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(emptyBody)
			return nil
		}
		releaseContent = res.(string)
		refreshed = true
		_ = s.store.Put(ctx, store.ReleaseKey(variant, owner, repo), store.Entry{
			Body:        []byte(releaseContent),
			ContentType: "text/plain",
			ExpiresAt:   time.Now().Add(s.cacheTTL),
		})
	}

	findAndFetch := func(rel string) ([]byte, bool) {
		sha := findPackagesSha(rel, component, arch, gz)
		if sha == "" {
			return nil, false
		}
		blobKey := store.PackagesBlobKey(variant, owner, repo, sha)
		blob, found, _ := s.store.Get(ctx, blobKey)
		if !found {
			return nil, false
		}
		return blob.Body, true
	}

	data, found := findAndFetch(releaseContent)
	if !found && !refreshed {
		res, err, _ := s.sf.Do(fmt.Sprintf("apt:%s:%s:%s", variant, owner, repo), func() (any, error) {
			return s.generateReleaseContent(ctx, rInfo)
		})
		if err == nil {
			releaseContent = res.(string)
			_ = s.store.Put(ctx, store.ReleaseKey(variant, owner, repo), store.Entry{
				Body:        []byte(releaseContent),
				ContentType: "text/plain",
				ExpiresAt:   time.Now().Add(s.cacheTTL),
			})
			data, found = findAndFetch(releaseContent)
			refreshed = true
		}
	}

	if found {
		s.maybeTriggerAptRefresh(ctx, rInfo)
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "public, max-age=300")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
		return nil
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(emptyBody)
	return nil
}

func parseReleaseSha256Map(releaseContent string) map[string]string {
	lines := strings.Split(releaseContent, "\n")
	res := make(map[string]string)
	inSha256 := false

	for _, line := range lines {
		if strings.TrimSpace(line) == "SHA256:" {
			inSha256 = true
			continue
		}
		if inSha256 {
			if !strings.HasPrefix(line, " ") {
				inSha256 = false
				continue
			}
			match := reSha256Line.FindStringSubmatch(strings.TrimSpace(line))
			if len(match) == 3 {
				res[match[2]] = match[1]
			}
		}
	}
	return res
}

func findPackagesSha(releaseContent string, component string, arch string, gz bool) string {
	suffix := ""
	if gz {
		suffix = ".gz"
	}
	path := fmt.Sprintf("%s/binary-%s/Packages%s", component, arch, suffix)
	return parseReleaseSha256Map(releaseContent)[path]
}

func (s *Service) writeAptPackagesBlobs(ctx context.Context, owner, repo, variant, content string) error {
	contentBytes := []byte(content)
	gzBytes, err := xutil.GzipCompress(contentBytes)
	if err != nil {
		return err
	}

	contentSha := xutil.SHA256(contentBytes)
	gzSha := xutil.SHA256(gzBytes)

	_ = s.store.Put(ctx, store.PackagesBlobKey(variant, owner, repo, contentSha), store.Entry{
		Body:        contentBytes,
		ContentType: "text/plain",
		ExpiresAt:   time.Now().Add(s.cacheTTL),
	})
	_ = s.store.Put(ctx, store.PackagesBlobKey(variant, owner, repo, gzSha), store.Entry{
		Body:        gzBytes,
		ContentType: "application/gzip",
		ExpiresAt:   time.Now().Add(s.cacheTTL),
	})
	return nil
}

func (s *Service) generateReleaseContent(ctx context.Context, rInfo route.RouteInfo) (string, error) {
	owner, repo, component, variant := rInfo.Owner, rInfo.Repo, rInfo.Component, rInfo.ReleaseVariant
	includePrerelease := variant == "prerelease"

	releases, err := s.github.GetAllReleases(ctx, owner, repo, includePrerelease)
	if err != nil {
		return "", err
	}
	if len(releases) == 0 {
		return "", fmt.Errorf("no releases found for %s/%s", owner, repo)
	}

	var allAssets []github.GitHubAsset
	for _, rel := range releases {
		allAssets = append(allAssets, rel.Assets...)
	}

	debAssets := github.FilterDebAssets(allAssets)
	archs := github.GetArchitecturesFromAssets(debAssets)

	packagesContentByArch := make(map[string]string)
	for _, arch := range archs {
		archAssets := github.FilterByArchitecture(debAssets, arch)
		if len(archAssets) == 0 {
			continue
		}

		entries := s.buildDebPackageEntries(ctx, archAssets)
		content := apt.GeneratePackagesFile(entries)
		packagesContentByArch[arch] = content

		// Write content-addressed blobs FIRST before Release file
		_ = s.writeAptPackagesBlobs(ctx, owner, repo, variant, content)
	}

	mostRecentDate := time.Now()
	for _, rel := range releases {
		if rel.PublishedAt != nil {
			if t, err := time.Parse(time.RFC3339, *rel.PublishedAt); err == nil {
				mostRecentDate = t
				break
			}
		}
	}

	config := apt.DefaultReleaseConfig(owner, repo)
	config.Architectures = archs
	config.Date = mostRecentDate

	entries, err := apt.BuildReleaseEntries(packagesContentByArch, component)
	if err != nil {
		return "", err
	}

	releaseContent := apt.GenerateReleaseFile(config, entries)

	// Save release IDs hash
	var minimalReleases []store.GitHubReleaseMinimal
	for _, r := range releases {
		var minAssets []store.GitHubAssetMinimal
		for _, a := range r.Assets {
			minAssets = append(minAssets, store.GitHubAssetMinimal{
				Name:   a.Name,
				Size:   a.Size,
				Digest: a.Digest,
			})
		}
		minimalReleases = append(minimalReleases, store.GitHubReleaseMinimal{
			ID:     r.ID,
			Assets: minAssets,
		})
	}
	releaseHash := store.ComputeReleaseIdsHash(minimalReleases)
	_ = s.store.Put(ctx, store.ReleaseIdsHashKey(variant, owner, repo), store.Entry{
		Body:        []byte(releaseHash),
		ContentType: "text/plain",
		ExpiresAt:   time.Now().Add(5 * time.Minute),
	})
	_ = s.store.Put(ctx, store.LastReleaseIdsHashKey(variant, owner, repo), store.Entry{
		Body:        []byte(releaseHash),
		ContentType: "text/plain",
		ExpiresAt:   time.Now().Add(s.cacheTTL),
	})

	return releaseContent, nil
}

func (s *Service) buildDebPackageEntries(ctx context.Context, assets []github.GitHubAsset) []apt.PackageEntry {
	sem := make(chan struct{}, 30)
	var wg sync.WaitGroup
	results := make([]*apt.PackageEntry, len(assets))

	for i, asset := range assets {
		wg.Add(1)
		go func(idx int, a github.GitHubAsset) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			entry, err := s.fetchAndBuildDebEntry(ctx, a)
			if err == nil && entry != nil {
				results[idx] = entry
			}
		}(i, asset)
	}
	wg.Wait()

	var entries []apt.PackageEntry
	for _, r := range results {
		if r != nil {
			entries = append(entries, *r)
		}
	}
	return entries
}

func (s *Service) fetchAndBuildDebEntry(ctx context.Context, asset github.GitHubAsset) (*apt.PackageEntry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.BrowserDownloadURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header = s.github.Headers("octet-stream")
	req.Header.Set("Range", "bytes=0-65535")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return nil, fmt.Errorf("failed to fetch deb header: %s", resp.Status)
	}

	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	controlData, err := deb.ParseDebBuffer(buf)
	if err != nil {
		return nil, err
	}

	if controlData.Package == "" || controlData.Version == "" {
		return nil, fmt.Errorf("invalid .deb: missing Package or Version")
	}

	if controlData.Architecture == "all" {
		filenameArch := xutil.ExtractArchFromFilename(asset.Name)
		if filenameArch != "all" {
			controlData.Architecture = filenameArch
		}
	}

	prefix := string(controlData.Package[0])
	filename := fmt.Sprintf("pool/main/%s/%s/%s", prefix, controlData.Package, asset.Name)
	sha256Hex := strings.TrimPrefix(asset.Digest, "sha256:")

	return &apt.PackageEntry{
		ControlData: *controlData,
		Filename:    filename,
		Size:        asset.Size,
		SHA256:      sha256Hex,
	}, nil
}

// maybeTriggerAptRefresh launches an asynchronous upstream check only when the
// 5-minute release-ids-hash gate has expired. While the gate is fresh the cached
// snapshot is served with zero upstream traffic.
func (s *Service) maybeTriggerAptRefresh(ctx context.Context, rInfo route.RouteInfo) {
	if _, fresh, _ := s.store.Get(ctx, store.ReleaseIdsHashKey(rInfo.ReleaseVariant, rInfo.Owner, rInfo.Repo)); fresh {
		return
	}
	go s.triggerAptRefresh(rInfo)
}

func (s *Service) triggerAptRefresh(rInfo route.RouteInfo) {
	owner, repo, variant := rInfo.Owner, rInfo.Repo, rInfo.ReleaseVariant

	_, _, _ = s.refreshSf.Do(fmt.Sprintf("apt-refresh:%s:%s:%s", variant, owner, repo), func() (any, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		// Re-check the gate: a concurrent refresh may have just completed.
		if _, fresh, _ := s.store.Get(ctx, store.ReleaseIdsHashKey(variant, owner, repo)); fresh {
			return nil, nil
		}

		includePrerelease := variant == "prerelease"
		releases, err := s.github.GetAllReleases(ctx, owner, repo, includePrerelease)
		if err != nil {
			log.Printf("apt refresh %s/%s[%s]: failed to fetch releases: %v", owner, repo, variant, err)
			return nil, nil
		}
		if len(releases) == 0 {
			return nil, nil
		}

		var minimalReleases []store.GitHubReleaseMinimal
		for _, r := range releases {
			var minAssets []store.GitHubAssetMinimal
			for _, a := range r.Assets {
				minAssets = append(minAssets, store.GitHubAssetMinimal{
					Name:   a.Name,
					Size:   a.Size,
					Digest: a.Digest,
				})
			}
			minimalReleases = append(minimalReleases, store.GitHubReleaseMinimal{
				ID:     r.ID,
				Assets: minAssets,
			})
		}
		currentHash := store.ComputeReleaseIdsHash(minimalReleases)

		lastKnownEntry, foundLast, _ := s.store.Get(ctx, store.LastReleaseIdsHashKey(variant, owner, repo))
		if foundLast && string(lastKnownEntry.Body) == currentHash {
			// Unchanged: renew the 5-minute gate without rebuilding.
			_ = s.store.Put(ctx, store.ReleaseIdsHashKey(variant, owner, repo), store.Entry{
				Body:        []byte(currentHash),
				ContentType: "text/plain",
				ExpiresAt:   time.Now().Add(5 * time.Minute),
			})
			return nil, nil
		}

		_, err, _ = s.sf.Do(fmt.Sprintf("apt:%s:%s:%s", variant, owner, repo), func() (any, error) {
			relContent, err := s.generateReleaseContent(ctx, rInfo)
			if err != nil {
				return nil, err
			}
			_ = s.store.Put(ctx, store.ReleaseKey(variant, owner, repo), store.Entry{
				Body:        []byte(relContent),
				ContentType: "text/plain",
				ExpiresAt:   time.Now().Add(s.cacheTTL),
			})
			if s.signer != nil {
				inRelease, err := s.signer.SignCleartext(relContent)
				if err != nil {
					log.Printf("apt refresh %s/%s[%s]: InRelease signing failed: %v", owner, repo, variant, err)
				} else {
					_ = s.store.Put(ctx, store.InReleaseKey(variant, owner, repo), store.Entry{
						Body:        []byte(inRelease),
						ContentType: "text/plain",
						ExpiresAt:   time.Now().Add(s.cacheTTL),
					})
				}
				relGpg, err := s.signer.SignDetachedText(relContent)
				if err != nil {
					log.Printf("apt refresh %s/%s[%s]: Release.gpg signing failed: %v", owner, repo, variant, err)
				} else {
					_ = s.store.Put(ctx, store.ReleaseGpgKey(variant, owner, repo), store.Entry{
						Body:        []byte(relGpg),
						ContentType: "application/pgp-signature",
						ExpiresAt:   time.Now().Add(s.cacheTTL),
					})
				}
			}
			return relContent, nil
		})
		if err != nil {
			log.Printf("apt refresh %s/%s[%s]: rebuild failed, keeping cached snapshot: %v", owner, repo, variant, err)
		}
		return nil, nil
	})
}
