package store_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Megatherium/gore/internal/store"
)

func TestComputeReleaseIdsHash(t *testing.T) {
	releases := []store.GitHubReleaseMinimal{
		{
			ID: 2,
			Assets: []store.GitHubAssetMinimal{
				{Name: "b.deb", Digest: "sha256:bbbb"},
				{Name: "a.deb", Digest: "sha256:aaaa"},
			},
		},
		{
			ID: 1,
			Assets: []store.GitHubAssetMinimal{
				{Name: "z.rpm", Size: 100},
			},
		},
	}

	hash1 := store.ComputeReleaseIdsHash(releases)
	if len(hash1) != 16 {
		t.Fatalf("expected 16 chars, got %d (%s)", len(hash1), hash1)
	}

	// Order of releases or assets should not affect result (deterministic sorting)
	releasesReversed := []store.GitHubReleaseMinimal{
		releases[1],
		releases[0],
	}
	hash2 := store.ComputeReleaseIdsHash(releasesReversed)
	if hash1 != hash2 {
		t.Errorf("expected deterministic hash: %s vs %s", hash1, hash2)
	}
}

func TestMemoryStore(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gore-store-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp error: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	s, err := store.NewMemoryStore(tmpDir)
	if err != nil {
		t.Fatalf("NewMemoryStore error: %v", err)
	}

	ctx := context.Background()

	t.Run("basic put and get", func(t *testing.T) {
		key := "test-key"
		entry := store.Entry{
			Body:        []byte("hello world"),
			ContentType: "text/plain",
		}

		if err := s.Put(ctx, key, entry); err != nil {
			t.Fatalf("Put error: %v", err)
		}

		got, ok, err := s.Get(ctx, key)
		if err != nil || !ok {
			t.Fatalf("Get error or not found: %v, %v", err, ok)
		}
		if string(got.Body) != "hello world" {
			t.Errorf("got %s, want hello world", string(got.Body))
		}
	})

	t.Run("expiry", func(t *testing.T) {
		key := "expired-key"
		entry := store.Entry{
			Body:        []byte("expired"),
			ContentType: "text/plain",
			ExpiresAt:   time.Now().Add(-1 * time.Second),
		}

		_ = s.Put(ctx, key, entry)
		_, ok, _ := s.Get(ctx, key)
		if ok {
			t.Errorf("expected expired item to return false")
		}
	})

	t.Run("blob disk persistence", func(t *testing.T) {
		sha := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
		blobKey := store.PackagesBlobKey("stable", "owner", "repo", sha)
		blobBody := []byte("immutable blob bytes")

		if err := s.Put(ctx, blobKey, store.Entry{
			Body:        blobBody,
			ContentType: "application/gzip",
		}); err != nil {
			t.Fatalf("Put blob error: %v", err)
		}

		// Create a fresh store instance pointing to same tmpDir
		s2, err := store.NewMemoryStore(tmpDir)
		if err != nil {
			t.Fatalf("NewMemoryStore s2 error: %v", err)
		}

		// Should find blob by reading from disk
		got, ok, err := s2.Get(ctx, blobKey)
		if err != nil || !ok {
			t.Fatalf("expected blob to load from disk: %v, %v", err, ok)
		}
		if string(got.Body) != string(blobBody) {
			t.Errorf("blob content mismatch: got %q, want %q", got.Body, blobBody)
		}
		if got.ContentType != "application/gzip" {
			t.Errorf("blob content type not restored from disk: got %q, want application/gzip", got.ContentType)
		}

		// Restored blobs honor the configured restore TTL
		s3, err := store.NewMemoryStore(tmpDir)
		if err != nil {
			t.Fatalf("NewMemoryStore s3 error: %v", err)
		}
		s3.SetRestoreTTL(2 * time.Second)
		restored, ok, err := s3.Get(ctx, blobKey)
		if err != nil || !ok {
			t.Fatalf("expected blob to restore from disk: %v, %v", err, ok)
		}
		if ttl := time.Until(restored.ExpiresAt); ttl > 3*time.Second {
			t.Errorf("restored blob TTL = %v, want ~2s (SetRestoreTTL not honored)", ttl)
		}
	})

	t.Run("ClearRepo", func(t *testing.T) {
		owner, repo := "owner", "myrepo"
		relKey := store.ReleaseKey("stable", owner, repo)
		_ = s.Put(ctx, relKey, store.Entry{Body: []byte("release")})

		if err := s.ClearRepo(ctx, owner, repo); err != nil {
			t.Fatalf("ClearRepo error: %v", err)
		}

		_, ok, _ := s.Get(ctx, relKey)
		if ok {
			t.Errorf("expected ReleaseKey to be deleted after ClearRepo")
		}
	})
}
