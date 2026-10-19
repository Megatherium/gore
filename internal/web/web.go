package web

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/leoherzog/reprox/internal/github"
	"github.com/leoherzog/reprox/internal/sign"
	"github.com/leoherzog/reprox/internal/store"
)

//go:embed static/readme.html
var readmeHTML string

//go:embed static/readme.md
var readmeMD string

//go:embed static/favicon.svg
var faviconSVG []byte

var (
	reDebPkgName = regexp.MustCompile(`^([a-z0-9][a-z0-9.+\-]*?)_\d`)
	reRpmPkgName = regexp.MustCompile(`^([a-zA-Z0-9][a-zA-Z0-9._+\-]*?)-\d`)
	reValidName  = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9._-]*[a-zA-Z0-9])?$`)
)

type PackageResponse struct {
	Package     string `json:"package"`
	HasPackages bool   `json:"hasPackages"`
}

type SearchResponse struct {
	Items []github.RepoSearchResultItem `json:"items"`
}

// Handler handles web landing page, search API, package API, and favicon requests.
type Handler struct {
	github *github.Client
	store  store.Store
	signer *sign.Signer
}

// NewHandler creates a new Handler instance.
func NewHandler(gh *github.Client, st store.Store, sn *sign.Signer) *Handler {
	return &Handler{
		github: gh,
		store:  st,
		signer: sn,
	}
}

// ServeHTTP dispatches requests.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	switch path {
	case "/":
		h.handleRoot(w, r)
	case "/favicon.ico", "/favicon.svg":
		h.handleFavicon(w, r)
	case "/_/search":
		h.handleSearch(w, r)
	case "/_/package":
		h.handlePackage(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) handleFavicon(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(faviconSVG)
}

func (h *Handler) handleRoot(w http.ResponseWriter, r *http.Request) {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	baseURL := fmt.Sprintf("%s://%s", scheme, r.Host)

	var fingerprintComment, fingerprintFooter string
	if h.signer != nil {
		fp := h.signer.Fingerprint()
		fingerprintComment = fmt.Sprintf("This instance's fingerprint: %s", fp)
		fingerprintFooter = fmt.Sprintf(" · Fingerprint: %s", fp)
	}

	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "text/html") {
		content := readmeHTML
		content = strings.ReplaceAll(content, "{{BASE_URL}}", baseURL)
		content = strings.ReplaceAll(content, "{{FINGERPRINT_COMMENT}}", fingerprintComment)
		content = strings.ReplaceAll(content, "{{FINGERPRINT_FOOTER}}", fingerprintFooter)

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=300")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(content))
		return
	}

	content := readmeMD
	content = strings.ReplaceAll(content, "{{BASE_URL}}", baseURL)
	content = strings.ReplaceAll(content, "{{FINGERPRINT_COMMENT}}", fingerprintComment)

	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(content))
}

func (h *Handler) handleSearch(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=300")

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) < 2 || len(q) > 256 {
		_ = json.NewEncoder(w).Encode(SearchResponse{Items: []github.RepoSearchResultItem{}})
		return
	}

	ctx := r.Context()
	cacheKey := fmt.Sprintf("search/%s", q)
	if cached, ok, _ := h.store.Get(ctx, cacheKey); ok {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(cached.Body)
		return
	}

	items, err := h.github.SearchRepositories(ctx, q)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Search failed"})
		return
	}

	resBytes, _ := json.Marshal(SearchResponse{Items: items})
	_ = h.store.Put(ctx, cacheKey, store.Entry{
		Body:        resBytes,
		ContentType: "application/json",
		ExpiresAt:   time.Now().Add(5 * time.Minute),
	})

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(resBytes)
}

func (h *Handler) handlePackage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=300")

	owner := strings.TrimSpace(r.URL.Query().Get("owner"))
	repo := strings.TrimSpace(r.URL.Query().Get("repo"))

	if owner == "" || repo == "" || !reValidName.MatchString(owner) || len(owner) > 39 ||
		!reValidName.MatchString(repo) || len(repo) > 100 {
		_ = json.NewEncoder(w).Encode(PackageResponse{Package: "", HasPackages: false})
		return
	}

	ctx := r.Context()
	cacheKey := fmt.Sprintf("package/%s/%s", owner, repo)
	if cached, ok, _ := h.store.Get(ctx, cacheKey); ok {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(cached.Body)
		return
	}

	rel, err := h.github.GetLatestRelease(ctx, owner, repo)
	if err != nil {
		_ = json.NewEncoder(w).Encode(PackageResponse{Package: repo, HasPackages: false})
		return
	}

	pkgName := repo
	hasPackages := false

	for _, a := range rel.Assets {
		if strings.HasSuffix(a.Name, ".deb") {
			hasPackages = true
			if match := reDebPkgName.FindStringSubmatch(a.Name); len(match) > 1 {
				pkgName = match[1]
				break
			}
		} else if strings.HasSuffix(a.Name, ".rpm") {
			hasPackages = true
			if match := reRpmPkgName.FindStringSubmatch(a.Name); len(match) > 1 {
				pkgName = match[1]
				break
			}
		}
	}

	res := PackageResponse{
		Package:     pkgName,
		HasPackages: hasPackages,
	}
	resBytes, _ := json.Marshal(res)
	_ = h.store.Put(ctx, cacheKey, store.Entry{
		Body:        resBytes,
		ContentType: "application/json",
		ExpiresAt:   time.Now().Add(5 * time.Minute),
	})

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(resBytes)
}
