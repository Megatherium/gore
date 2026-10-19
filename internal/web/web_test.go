package web_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/leoherzog/reprox/internal/github"
	"github.com/leoherzog/reprox/internal/store"
	"github.com/leoherzog/reprox/internal/web"
)

func TestWebHandler(t *testing.T) {
	memStore, err := store.NewMemoryStore("")
	if err != nil {
		t.Fatalf("NewMemoryStore error: %v", err)
	}

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/search/repositories"):
			resp := map[string]any{
				"items": []map[string]any{
					{
						"full_name":        "joshuar/go-hass-agent",
						"description":      "A Home Assistant agent",
						"stargazers_count": 150,
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)

		case strings.HasSuffix(r.URL.Path, "/releases/latest"):
			pub := "2024-01-01T00:00:00Z"
			rel := github.GitHubRelease{
				ID:          1,
				TagName:     "v1.0.0",
				PublishedAt: &pub,
				Assets: []github.GitHubAsset{
					{Name: "go-hass-agent_1.0.0_amd64.deb"},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(rel)

		default:
			http.NotFound(w, r)
		}
	}))
	defer mockServer.Close()

	gh := github.NewClient("fake-token", mockServer.Client())
	gh.SetBaseURL(mockServer.URL)

	handler := web.NewHandler(gh, memStore, nil)

	t.Run("root HTML content negotiation", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Accept", "text/html,application/xhtml+xml")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		if !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
			t.Errorf("expected text/html Content-Type")
		}
		if !strings.Contains(rec.Body.String(), "<!DOCTYPE html>") {
			t.Errorf("missing DOCTYPE in HTML output")
		}
	})

	t.Run("root Markdown content negotiation", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		if !strings.Contains(rec.Header().Get("Content-Type"), "text/markdown") {
			t.Errorf("expected text/markdown Content-Type")
		}
		if !strings.Contains(rec.Body.String(), "# 📦 Reprox") {
			t.Errorf("missing Markdown title in output")
		}
	})

	t.Run("favicon", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/favicon.svg", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		if !strings.Contains(rec.Header().Get("Content-Type"), "image/svg+xml") {
			t.Errorf("expected image/svg+xml Content-Type")
		}
		if !strings.Contains(rec.Body.String(), "<svg") {
			t.Errorf("missing svg element")
		}
	})

	t.Run("search API", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/_/search?q=go-hass", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var searchRes web.SearchResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &searchRes); err != nil {
			t.Fatalf("failed to decode search json: %v", err)
		}
		if len(searchRes.Items) != 1 || searchRes.Items[0].FullName != "joshuar/go-hass-agent" {
			t.Errorf("unexpected search items: %+v", searchRes.Items)
		}
	})

	t.Run("package API", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/_/package?owner=joshuar&repo=go-hass-agent", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var pkgRes web.PackageResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &pkgRes); err != nil {
			t.Fatalf("failed to decode pkg json: %v", err)
		}
		if pkgRes.Package != "go-hass-agent" || !pkgRes.HasPackages {
			t.Errorf("unexpected package response: %+v", pkgRes)
		}
	})
}
