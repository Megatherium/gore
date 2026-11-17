package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Megatherium/gore/internal/xutil"
)

// Entry represents a cached response or metadata blob.
type Entry struct {
	Body        []byte
	ContentType string
	ExpiresAt   time.Time
}

// Store interface for caching repository metadata and blobs.
type Store interface {
	Get(ctx context.Context, key string) (Entry, bool, error)
	Put(ctx context.Context, key string, entry Entry) error
	Delete(ctx context.Context, key string) error
	ClearRepo(ctx context.Context, owner, repo string) error
}

// CachedRpmSnapshot stores repomd.xml, detached signature, and releaseIdsHash.
type CachedRpmSnapshot struct {
	Repomd         string  `json:"repomd"`
	Signature      *string `json:"signature"`
	ReleaseIdsHash string  `json:"releaseIdsHash"`
}

// GitHubAssetMinimal is the minimal asset info needed for hashing.
type GitHubAssetMinimal struct {
	Name   string
	Size   int64
	Digest string
}

// GitHubReleaseMinimal is the minimal release info needed for hashing.
type GitHubReleaseMinimal struct {
	ID     int64
	Assets []GitHubAssetMinimal
}

// ComputeReleaseIdsHash computes a 16-character hex string hash from releases and assets.
// Releases are sorted by ID; assets are sorted by name (ASCII order).
func ComputeReleaseIdsHash(releases []GitHubReleaseMinimal) string {
	sortedReleases := make([]GitHubReleaseMinimal, len(releases))
	copy(sortedReleases, releases)
	sort.Slice(sortedReleases, func(i, j int) bool {
		return sortedReleases[i].ID < sortedReleases[j].ID
	})

	lines := make([]string, len(sortedReleases))
	for i, r := range sortedReleases {
		sortedAssets := make([]GitHubAssetMinimal, len(r.Assets))
		copy(sortedAssets, r.Assets)
		sort.Slice(sortedAssets, func(a, b int) bool {
			return sortedAssets[a].Name < sortedAssets[b].Name
		})

		assetParts := make([]string, len(sortedAssets))
		for j, a := range sortedAssets {
			digestOrSize := a.Digest
			if digestOrSize == "" {
				digestOrSize = fmt.Sprintf("%d", a.Size)
			}
			assetParts[j] = fmt.Sprintf("%s:%s", a.Name, digestOrSize)
		}
		lines[i] = fmt.Sprintf("%d:%s", r.ID, strings.Join(assetParts, "|"))
	}

	fullHash := xutil.SHA256String(strings.Join(lines, "\n"))
	if len(fullHash) > 16 {
		return fullHash[:16]
	}
	return fullHash
}

// Cache Key Builders matching gore specifications
func PackagesBlobKey(variant, owner, repo, sha256 string) string {
	return fmt.Sprintf("packages-blob/%s/%s/%s/%s", variant, owner, repo, sha256)
}

func ReleaseKey(variant, owner, repo string) string {
	return fmt.Sprintf("release/%s/%s/%s", variant, owner, repo)
}

func InReleaseKey(variant, owner, repo string) string {
	return fmt.Sprintf("inrelease/%s/%s/%s", variant, owner, repo)
}

func ReleaseGpgKey(variant, owner, repo string) string {
	return fmt.Sprintf("release-gpg/%s/%s/%s", variant, owner, repo)
}

func ReleaseIdsHashKey(variant, owner, repo string) string {
	return fmt.Sprintf("release-ids-hash/%s/%s/%s", variant, owner, repo)
}

// LastReleaseIdsHashKey is the long-TTL key holding the last known upstream
// release-ids hash, used to detect changes even after the 5-minute gate expires.
func LastReleaseIdsHashKey(variant, owner, repo string) string {
	return fmt.Sprintf("last-release-ids-hash/%s/%s/%s", variant, owner, repo)
}

func RpmSnapshotKey(variant, owner, repo string) string {
	return fmt.Sprintf("rpm/snapshot/%s/%s/%s", variant, owner, repo)
}

func RpmBlobKey(variant, owner, repo, sha256 string) string {
	return fmt.Sprintf("rpm/blob/%s/%s/%s/%s", variant, owner, repo, sha256)
}

func RpmEntryKey(owner, repo, sha256, filename string) string {
	return fmt.Sprintf("rpm/entry/%s/%s/%s/%s", owner, repo, sha256, filename)
}

func AssetURLKey(variant, owner, repo, releaseHash, filename string) string {
	return fmt.Sprintf("asset-url/%s/%s/%s/%s/%s", variant, owner, repo, releaseHash, filename)
}
