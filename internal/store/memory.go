package store

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// MemoryStore is a thread-safe in-memory cache with optional disk backing for blobs.
type MemoryStore struct {
	mu         sync.RWMutex
	items      map[string]Entry
	cacheDir   string
	restoreTTL time.Duration
	closeOnce  sync.Once
	stop       chan struct{}
}

// NewMemoryStore creates a new MemoryStore. If cacheDir is non-empty, content-addressed
// blobs will also be persisted to and restored from disk.
func NewMemoryStore(cacheDir string) (*MemoryStore, error) {
	if cacheDir != "" {
		blobDir := filepath.Join(cacheDir, "blobs")
		if err := os.MkdirAll(blobDir, 0700); err != nil {
			return nil, err
		}
	}

	ms := &MemoryStore{
		items:      make(map[string]Entry),
		cacheDir:   cacheDir,
		restoreTTL: 24 * time.Hour,
		stop:       make(chan struct{}),
	}
	go ms.janitor()

	return ms, nil
}

// SetRestoreTTL sets the TTL applied to blobs restored from disk (defaults to 24h).
func (s *MemoryStore) SetRestoreTTL(ttl time.Duration) {
	if ttl > 0 {
		s.restoreTTL = ttl
	}
}

// Close stops the background eviction janitor.
func (s *MemoryStore) Close() {
	s.closeOnce.Do(func() {
		close(s.stop)
	})
}

// janitor periodically evicts expired entries so that keys which are never read
// again do not accumulate forever.
func (s *MemoryStore) janitor() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			now := time.Now()
			s.mu.Lock()
			for key, entry := range s.items {
				if !entry.ExpiresAt.IsZero() && now.After(entry.ExpiresAt) {
					delete(s.items, key)
				}
			}
			s.mu.Unlock()
		}
	}
}

func isBlobKey(key string) bool {
	return strings.HasPrefix(key, "packages-blob/") || strings.HasPrefix(key, "rpm/blob/")
}

func extractBlobSha(key string) string {
	parts := strings.Split(key, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return ""
}

// Get retrieves an item from cache. If not found in memory, checks disk for blobs.
func (s *MemoryStore) Get(ctx context.Context, key string) (Entry, bool, error) {
	s.mu.RLock()
	entry, found := s.items[key]
	s.mu.RUnlock()

	now := time.Now()
	if found {
		if !entry.ExpiresAt.IsZero() && now.After(entry.ExpiresAt) {
			s.mu.Lock()
			delete(s.items, key)
			s.mu.Unlock()
			return Entry{}, false, nil
		}
		return entry, true, nil
	}

	// If it's a content-addressed blob and not in memory, try loading from disk
	if isBlobKey(key) && s.cacheDir != "" {
		sha := extractBlobSha(key)
		if sha != "" {
			blobPath := filepath.Join(s.cacheDir, "blobs", sha)
			data, err := os.ReadFile(blobPath)
			if err == nil {
				contentType := s.readBlobContentType(sha, data)
				loaded := Entry{
					Body:        data,
					ContentType: contentType,
					ExpiresAt:   now.Add(s.restoreTTL),
				}
				s.mu.Lock()
				s.items[key] = loaded
				s.mu.Unlock()
				return loaded, true, nil
			}
		}
	}

	return Entry{}, false, nil
}

// readBlobContentType restores the persisted content type from the sidecar file,
// falling back to sniffing the blob bytes for legacy blobs written without one.
func (s *MemoryStore) readBlobContentType(sha string, data []byte) string {
	if ct, err := os.ReadFile(filepath.Join(s.cacheDir, "blobs", sha+".ct")); err == nil {
		if ct := strings.TrimSpace(string(ct)); ct != "" {
			return ct
		}
	}
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		return "application/gzip"
	}
	if len(data) > 0 && data[0] == '<' {
		return "application/xml"
	}
	return "text/plain"
}

// Put stores an item in cache and optionally writes blobs to disk.
func (s *MemoryStore) Put(ctx context.Context, key string, entry Entry) error {
	s.mu.Lock()
	s.items[key] = entry
	s.mu.Unlock()

	// Persist blobs to disk
	if isBlobKey(key) && s.cacheDir != "" {
		sha := extractBlobSha(key)
		if sha != "" {
			blobPath := filepath.Join(s.cacheDir, "blobs", sha)
			if err := os.WriteFile(blobPath, entry.Body, 0600); err != nil {
				log.Printf("store: failed to persist blob %s to disk: %v", sha, err)
			}
			if err := os.WriteFile(blobPath+".ct", []byte(entry.ContentType), 0600); err != nil {
				log.Printf("store: failed to persist blob %s content type: %v", sha, err)
			}
		}
	}

	return nil
}

// Delete removes an item from cache.
func (s *MemoryStore) Delete(ctx context.Context, key string) error {
	s.mu.Lock()
	delete(s.items, key)
	s.mu.Unlock()
	return nil
}

// ClearRepo removes cached top-level metadata for an owner/repo across both stable and prerelease variants.
func (s *MemoryStore) ClearRepo(ctx context.Context, owner, repo string) error {
	variants := []string{"stable", "prerelease"}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, v := range variants {
		delete(s.items, ReleaseKey(v, owner, repo))
		delete(s.items, InReleaseKey(v, owner, repo))
		delete(s.items, ReleaseGpgKey(v, owner, repo))
		delete(s.items, ReleaseIdsHashKey(v, owner, repo))
		delete(s.items, RpmSnapshotKey(v, owner, repo))
	}

	return nil
}
