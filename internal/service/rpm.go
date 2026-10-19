package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	rpmGen "github.com/leoherzog/reprox/internal/gen/rpm"
	"github.com/leoherzog/reprox/internal/github"
	rpmParser "github.com/leoherzog/reprox/internal/parse/rpm"
	"github.com/leoherzog/reprox/internal/route"
	"github.com/leoherzog/reprox/internal/store"
	"github.com/leoherzog/reprox/internal/xutil"
)

var rpmRangeLadder = []int{
	256 * 1024,
	1024 * 1024,
	4 * 1024 * 1024,
	16 * 1024 * 1024,
}

var reRepomdLocation = regexp.MustCompile(`<location href="repodata/([0-9a-f]{64})-(primary|filelists|other)\.xml(\.gz)?"/>`)

// findRepomdLocationHash extracts the content-addressed hash for the given
// metadata kind from a repomd.xml document.
func findRepomdLocationHash(repomdXml, kind string, gz bool) string {
	suffix := ""
	if gz {
		suffix = ".gz"
	}
	for _, m := range reRepomdLocation.FindAllStringSubmatch(repomdXml, -1) {
		if m[2] == kind && m[3] == suffix {
			return m[1]
		}
	}
	return ""
}

const (
	rpmFetchAttempts  = 3
	rpmFetchBackoffMs = 250
)

type rpmSnapshot struct {
	RepomdXml      string
	Signature      *string
	ReleaseIdsHash string
	Blobs          map[string]store.Entry
	Hashes         rpmGen.RepomdHashes
}

func (s *Service) handleRepomd(ctx context.Context, w http.ResponseWriter, r *http.Request, rInfo route.RouteInfo) error {
	snap, err := s.getOrBuildRpmSnapshot(ctx, rInfo)
	if err != nil {
		return err
	}

	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(snap.Repomd))
	return nil
}

func (s *Service) handleRepomdAsc(ctx context.Context, w http.ResponseWriter, r *http.Request, rInfo route.RouteInfo) error {
	if s.signer == nil {
		http.Error(w, "GPG signing not configured", http.StatusNotFound)
		return nil
	}

	snap, err := s.getOrBuildRpmSnapshot(ctx, rInfo)
	if err != nil {
		return err
	}

	if snap.Signature == nil || *snap.Signature == "" {
		http.Error(w, "Signature generation failed", http.StatusInternalServerError)
		return nil
	}

	w.Header().Set("Content-Type", "application/pgp-signature")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(*snap.Signature))
	return nil
}

func (s *Service) handleRpmXml(ctx context.Context, w http.ResponseWriter, r *http.Request, rInfo route.RouteInfo, xmlType string, compressed bool) error {
	owner, repo, variant, hash := rInfo.Owner, rInfo.Repo, rInfo.ReleaseVariant, rInfo.Hash

	if hash == "" {
		// Legacy unhashed path: resolve the current blob hash from the (cached,
		// singleflighted) repomd.xml snapshot, then serve the immutable blob so
		// refreshes never race with index updates.
		snap, err := s.getOrBuildRpmSnapshot(ctx, rInfo)
		if err != nil {
			return err
		}
		hash = findRepomdLocationHash(snap.Repomd, xmlType, compressed)
		if hash == "" {
			http.Error(w, "Metadata file not found", http.StatusNotFound)
			return nil
		}
	}

	// Content-addressed blob lookup
	cached, found, err := s.store.Get(ctx, store.RpmBlobKey(variant, owner, repo, hash))
	if err == nil && found {
		w.Header().Set("Content-Type", cached.ContentType)
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(cached.Body)
		return nil
	}

	// Blob expired from cache: rebuild snapshot and serve if hash matches current
	res, err, _ := s.sf.Do(fmt.Sprintf("rpm:%s:%s:%s", variant, owner, repo), func() (any, error) {
		built, err := s.buildRpmSnapshot(ctx, rInfo)
		if err != nil {
			return nil, err
		}
		if built == nil {
			return nil, nil
		}
		if err := s.cacheRpmSnapshot(ctx, rInfo, built); err != nil {
			return nil, err
		}
		return built, nil
	})
	if err != nil {
		return err
	}
	if built, _ := res.(*rpmSnapshot); built != nil {
		if blob, ok := built.Blobs[hash]; ok {
			w.Header().Set("Content-Type", blob.ContentType)
			w.Header().Set("Cache-Control", "public, max-age=86400")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(blob.Body)
			return nil
		}
	}

	http.Error(w, fmt.Sprintf("Metadata file not found: %s", hash), http.StatusNotFound)
	return nil
}

func (s *Service) getOrBuildRpmSnapshot(ctx context.Context, rInfo route.RouteInfo) (*store.CachedRpmSnapshot, error) {
	owner, repo, variant := rInfo.Owner, rInfo.Repo, rInfo.ReleaseVariant

	cachedEntry, found, _ := s.store.Get(ctx, store.RpmSnapshotKey(variant, owner, repo))
	if found {
		var snap store.CachedRpmSnapshot
		if err := json.Unmarshal(cachedEntry.Body, &snap); err == nil {
			if snap.Signature != nil || s.signer == nil {
				s.maybeTriggerRpmRefresh(ctx, rInfo, snap.ReleaseIdsHash)
				return &snap, nil
			}
		}
	}

	res, err, _ := s.sf.Do(fmt.Sprintf("rpm:%s:%s:%s", variant, owner, repo), func() (any, error) {
		built, err := s.buildRpmSnapshot(ctx, rInfo)
		if err != nil {
			return nil, err
		}
		if built == nil {
			// No releases: build and cache an empty snapshot so that the
			// locations advertised in its repomd.xml resolve to real blobs.
			built, err = s.buildEmptyRpmSnapshot()
			if err != nil {
				return nil, err
			}
		}

		if err := s.cacheRpmSnapshot(ctx, rInfo, built); err != nil {
			return nil, err
		}

		return &store.CachedRpmSnapshot{
			Repomd:         built.RepomdXml,
			Signature:      built.Signature,
			ReleaseIdsHash: built.ReleaseIdsHash,
		}, nil
	})

	if err != nil {
		return nil, err
	}
	return res.(*store.CachedRpmSnapshot), nil
}

// buildEmptyRpmSnapshot creates a snapshot with empty metadata for repositories
// that have no releases yet.
func (s *Service) buildEmptyRpmSnapshot() (*rpmSnapshot, error) {
	primaryXml := rpmGen.GeneratePrimaryXml(nil)
	filelistsXml := rpmGen.GenerateFilelistsXml(nil)
	otherXml := rpmGen.GenerateOtherXml(nil)

	primaryGz, err := xutil.GzipCompress([]byte(primaryXml))
	if err != nil {
		return nil, err
	}
	filelistsGz, err := xutil.GzipCompress([]byte(filelistsXml))
	if err != nil {
		return nil, err
	}
	otherGz, err := xutil.GzipCompress([]byte(otherXml))
	if err != nil {
		return nil, err
	}

	repomdResult, err := rpmGen.BuildRepomd(rpmGen.RepomdFileInfo{
		Primary:   rpmGen.RepomdFileSection{XML: primaryXml, GZ: primaryGz},
		Filelists: rpmGen.RepomdFileSection{XML: filelistsXml, GZ: filelistsGz},
		Other:     rpmGen.RepomdFileSection{XML: otherXml, GZ: otherGz},
		Timestamp: time.Now().Unix(),
	})
	if err != nil {
		return nil, err
	}

	var sig *string
	if s.signer != nil {
		sigStr, err := s.signer.SignDetachedBinary([]byte(repomdResult.XML))
		if err != nil {
			return nil, fmt.Errorf("failed to sign empty repomd.xml: %w", err)
		}
		sig = &sigStr
	}

	return &rpmSnapshot{
		RepomdXml:      repomdResult.XML,
		Signature:      sig,
		ReleaseIdsHash: store.ComputeReleaseIdsHash(nil),
		Blobs: map[string]store.Entry{
			repomdResult.Hashes.Primary.XML:   {Body: []byte(primaryXml), ContentType: "application/xml"},
			repomdResult.Hashes.Primary.GZ:    {Body: primaryGz, ContentType: "application/gzip"},
			repomdResult.Hashes.Filelists.XML: {Body: []byte(filelistsXml), ContentType: "application/xml"},
			repomdResult.Hashes.Filelists.GZ:  {Body: filelistsGz, ContentType: "application/gzip"},
			repomdResult.Hashes.Other.XML:     {Body: []byte(otherXml), ContentType: "application/xml"},
			repomdResult.Hashes.Other.GZ:      {Body: otherGz, ContentType: "application/gzip"},
		},
		Hashes: repomdResult.Hashes,
	}, nil
}

func (s *Service) buildRpmSnapshot(ctx context.Context, rInfo route.RouteInfo) (*rpmSnapshot, error) {
	owner, repo, variant := rInfo.Owner, rInfo.Repo, rInfo.ReleaseVariant
	includePrerelease := variant == "prerelease"

	releases, err := s.github.GetAllReleases(ctx, owner, repo, includePrerelease)
	if err != nil {
		return nil, err
	}
	if len(releases) == 0 {
		return nil, nil
	}

	var allAssets []github.GitHubAsset
	for _, rel := range releases {
		allAssets = append(allAssets, rel.Assets...)
	}

	packages, err := s.buildRpmPackages(ctx, owner, repo, allAssets)
	if err != nil {
		return nil, err
	}

	primaryXml := rpmGen.GeneratePrimaryXml(packages)
	filelistsXml := rpmGen.GenerateFilelistsXml(packages)
	otherXml := rpmGen.GenerateOtherXml(packages)

	primaryGz, _ := xutil.GzipCompress([]byte(primaryXml))
	filelistsGz, _ := xutil.GzipCompress([]byte(filelistsXml))
	otherGz, _ := xutil.GzipCompress([]byte(otherXml))

	timestamp := time.Now().Unix()
	for _, rel := range releases {
		if rel.PublishedAt != nil {
			if t, err := time.Parse(time.RFC3339, *rel.PublishedAt); err == nil {
				timestamp = t.Unix()
				break
			}
		}
	}

	repomdResult, err := rpmGen.BuildRepomd(rpmGen.RepomdFileInfo{
		Primary:   rpmGen.RepomdFileSection{XML: primaryXml, GZ: primaryGz},
		Filelists: rpmGen.RepomdFileSection{XML: filelistsXml, GZ: filelistsGz},
		Other:     rpmGen.RepomdFileSection{XML: otherXml, GZ: otherGz},
		Timestamp: timestamp,
	})
	if err != nil {
		return nil, err
	}

	var sig *string
	if s.signer != nil {
		sigStr, err := s.signer.SignDetachedBinary([]byte(repomdResult.XML))
		if err != nil {
			// Fail the snapshot build rather than caching a signature-less
			// snapshot, which would force a full rebuild on every request.
			return nil, fmt.Errorf("failed to sign repomd.xml: %w", err)
		}
		sig = &sigStr
	}

	blobs := make(map[string]store.Entry)
	blobs[repomdResult.Hashes.Primary.XML] = store.Entry{Body: []byte(primaryXml), ContentType: "application/xml"}
	blobs[repomdResult.Hashes.Primary.GZ] = store.Entry{Body: primaryGz, ContentType: "application/gzip"}
	blobs[repomdResult.Hashes.Filelists.XML] = store.Entry{Body: []byte(filelistsXml), ContentType: "application/xml"}
	blobs[repomdResult.Hashes.Filelists.GZ] = store.Entry{Body: filelistsGz, ContentType: "application/gzip"}
	blobs[repomdResult.Hashes.Other.XML] = store.Entry{Body: []byte(otherXml), ContentType: "application/xml"}
	blobs[repomdResult.Hashes.Other.GZ] = store.Entry{Body: otherGz, ContentType: "application/gzip"}

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

	return &rpmSnapshot{
		RepomdXml:      repomdResult.XML,
		Signature:      sig,
		ReleaseIdsHash: releaseHash,
		Blobs:          blobs,
		Hashes:         repomdResult.Hashes,
	}, nil
}

func (s *Service) cacheRpmSnapshot(ctx context.Context, rInfo route.RouteInfo, snap *rpmSnapshot) error {
	owner, repo, variant := rInfo.Owner, rInfo.Repo, rInfo.ReleaseVariant

	// Write blobs FIRST
	for sha, blob := range snap.Blobs {
		blob.ExpiresAt = time.Now().Add(s.cacheTTL)
		_ = s.store.Put(ctx, store.RpmBlobKey(variant, owner, repo, sha), blob)
	}

	cachedSnap := store.CachedRpmSnapshot{
		Repomd:         snap.RepomdXml,
		Signature:      snap.Signature,
		ReleaseIdsHash: snap.ReleaseIdsHash,
	}
	snapBytes, _ := json.Marshal(cachedSnap)

	_ = s.store.Put(ctx, store.RpmSnapshotKey(variant, owner, repo), store.Entry{
		Body:        snapBytes,
		ContentType: "application/json",
		ExpiresAt:   time.Now().Add(s.cacheTTL),
	})
	_ = s.store.Put(ctx, store.ReleaseIdsHashKey(variant, owner, repo), store.Entry{
		Body:        []byte(snap.ReleaseIdsHash),
		ContentType: "text/plain",
		ExpiresAt:   time.Now().Add(5 * time.Minute),
	})

	return nil
}

func (s *Service) buildRpmPackages(ctx context.Context, owner, repo string, assets []github.GitHubAsset) ([]rpmParser.RpmPackageEntry, error) {
	rpmAssets := github.FilterRpmAssets(assets)
	sem := make(chan struct{}, 30)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var failed []string
	results := make([]*rpmParser.RpmPackageEntry, len(rpmAssets))

	for i, asset := range rpmAssets {
		wg.Add(1)
		go func(idx int, a github.GitHubAsset) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			sha := strings.TrimPrefix(a.Digest, "sha256:")
			cacheKey := store.RpmEntryKey(owner, repo, sha, a.Name)
			cachedEntry, found, _ := s.store.Get(ctx, cacheKey)
			if found {
				var entry rpmParser.RpmPackageEntry
				if err := json.Unmarshal(cachedEntry.Body, &entry); err == nil {
					results[idx] = &entry
					return
				}
			}

			entry, err := s.buildRpmPackageEntryWithRetry(ctx, a)
			if err != nil {
				mu.Lock()
				failed = append(failed, a.Name)
				mu.Unlock()
				return
			}
			if entry != nil {
				entryBytes, _ := json.Marshal(entry)
				_ = s.store.Put(ctx, cacheKey, store.Entry{
					Body:        entryBytes,
					ContentType: "application/json",
					ExpiresAt:   time.Now().Add(s.cacheTTL),
				})
				results[idx] = entry
			}
		}(i, asset)
	}
	wg.Wait()

	if len(failed) > 0 {
		log.Printf("rpm %s/%s: failed to fetch headers for %d package(s): %s", owner, repo, len(failed), strings.Join(failed, ", "))
		return nil, &IncompleteSnapshotError{Failed: failed}
	}

	var packages []rpmParser.RpmPackageEntry
	for _, r := range results {
		if r != nil {
			packages = append(packages, *r)
		}
	}
	return packages, nil
}

func (s *Service) buildRpmPackageEntryWithRetry(ctx context.Context, asset github.GitHubAsset) (*rpmParser.RpmPackageEntry, error) {
	for attempt := 1; attempt <= rpmFetchAttempts; attempt++ {
		entry, err := s.fetchAndExtractRpm(ctx, asset)
		if err == nil {
			return entry, nil
		}

		var fetchErr *rpmParser.ErrRpmFetch
		if !errors.As(err, &fetchErr) {
			// Malformed or corrupt RPM: drop package (return nil)
			log.Printf("rpm %s: malformed or corrupt package, skipping: %v", asset.Name, err)
			return nil, nil
		}

		if attempt == rpmFetchAttempts {
			return nil, err
		}

		backoff := time.Duration(rpmFetchBackoffMs*(1<<(attempt-1))) * time.Millisecond
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return nil, nil
}

func (s *Service) fetchAndExtractRpm(ctx context.Context, asset github.GitHubAsset) (*rpmParser.RpmPackageEntry, error) {
	var headerData *rpmParser.RpmHeaderData

	for _, rangeSize := range rpmRangeLadder {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.BrowserDownloadURL, nil)
		if err != nil {
			return nil, &rpmParser.ErrRpmFetch{Message: err.Error()}
		}
		req.Header = s.github.Headers("octet-stream")
		req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", rangeSize-1))

		resp, err := s.httpClient.Do(req)
		if err != nil {
			return nil, &rpmParser.ErrRpmFetch{Message: err.Error()}
		}

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
			resp.Body.Close()
			return nil, &rpmParser.ErrRpmFetch{Message: fmt.Sprintf("HTTP %d", resp.StatusCode)}
		}

		buf, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, &rpmParser.ErrRpmFetch{Message: err.Error()}
		}

		hdr, err := rpmParser.ParseRpmBuffer(buf)
		if err != nil {
			var truncErr *rpmParser.ErrRpmHeaderTruncated
			if errors.As(err, &truncErr) {
				continue // Try larger ladder range
			}
			return nil, err // Bad magic / malformed
		}
		headerData = hdr
		break
	}

	if headerData == nil {
		maxRange := rpmRangeLadder[len(rpmRangeLadder)-1]
		return nil, &rpmParser.ErrRpmFetch{Message: fmt.Sprintf("RPM header exceeded maximum ladder range (%d bytes)", maxRange)}
	}

	if headerData.Arch == "" {
		headerData.Arch = xutil.ExtractRpmArchFromFilename(asset.Name)
	}

	checksum := strings.TrimPrefix(asset.Digest, "sha256:")
	return &rpmParser.RpmPackageEntry{
		HeaderData:   *headerData,
		Filename:     asset.Name,
		Size:         asset.Size,
		Checksum:     checksum,
		ChecksumType: "sha256",
	}, nil
}

// maybeTriggerRpmRefresh launches an asynchronous upstream check only when the
// 5-minute release-ids-hash gate has expired. While the gate is fresh the cached
// snapshot is served with zero upstream traffic.
func (s *Service) maybeTriggerRpmRefresh(ctx context.Context, rInfo route.RouteInfo, cachedReleaseIdsHash string) {
	if _, fresh, _ := s.store.Get(ctx, store.ReleaseIdsHashKey(rInfo.ReleaseVariant, rInfo.Owner, rInfo.Repo)); fresh {
		return
	}
	go s.triggerRpmRefresh(rInfo, cachedReleaseIdsHash)
}

func (s *Service) triggerRpmRefresh(rInfo route.RouteInfo, cachedReleaseIdsHash string) {
	owner, repo, variant := rInfo.Owner, rInfo.Repo, rInfo.ReleaseVariant

	_, _, _ = s.refreshSf.Do(fmt.Sprintf("rpm-refresh:%s:%s:%s", variant, owner, repo), func() (any, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		// Re-check the gate: a concurrent refresh may have just completed.
		if _, fresh, _ := s.store.Get(ctx, store.ReleaseIdsHashKey(variant, owner, repo)); fresh {
			return nil, nil
		}

		includePrerelease := variant == "prerelease"
		releases, err := s.github.GetAllReleases(ctx, owner, repo, includePrerelease)
		if err != nil {
			log.Printf("rpm refresh %s/%s[%s]: failed to fetch releases: %v", owner, repo, variant, err)
			return nil, nil
		}
		if len(releases) == 0 {
			// Keep serving the (possibly empty) cached snapshot; renew the gate so
			// empty repositories do not hammer the GitHub API on every request.
			_ = s.store.Put(ctx, store.ReleaseIdsHashKey(variant, owner, repo), store.Entry{
				Body:        []byte(cachedReleaseIdsHash),
				ContentType: "text/plain",
				ExpiresAt:   time.Now().Add(5 * time.Minute),
			})
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
		if currentHash == cachedReleaseIdsHash {
			_ = s.store.Put(ctx, store.ReleaseIdsHashKey(variant, owner, repo), store.Entry{
				Body:        []byte(currentHash),
				ContentType: "text/plain",
				ExpiresAt:   time.Now().Add(5 * time.Minute),
			})
			return nil, nil
		}

		_, err, _ = s.sf.Do(fmt.Sprintf("rpm:%s:%s:%s", variant, owner, repo), func() (any, error) {
			snap, err := s.buildRpmSnapshot(ctx, rInfo)
			if err != nil || snap == nil {
				return nil, err
			}
			return nil, s.cacheRpmSnapshot(ctx, rInfo, snap)
		})
		if err != nil {
			log.Printf("rpm refresh %s/%s[%s]: rebuild failed, keeping cached snapshot: %v", owner, repo, variant, err)
		}
		return nil, nil
	})
}
