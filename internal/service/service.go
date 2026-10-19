package service

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/leoherzog/reprox/internal/github"
	"github.com/leoherzog/reprox/internal/route"
	"github.com/leoherzog/reprox/internal/sign"
	"github.com/leoherzog/reprox/internal/store"
)

var (
	validNamePattern = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9._-]*[a-zA-Z0-9])?$`)
)

// IncompleteSnapshotError indicates one or more packages could not be fetched.
type IncompleteSnapshotError struct {
	Failed []string
}

func (e *IncompleteSnapshotError) Error() string {
	return fmt.Sprintf("could not fetch RPM headers for: %s", strings.Join(e.Failed, ", "))
}

// ServiceConfig holds service options.
type ServiceConfig struct {
	GitHubClient *github.Client
	Store        store.Store
	Signer       *sign.Signer
	HTTPClient   *http.Client
	CacheTTL     time.Duration
	WebHandler   http.Handler
}

// Service coordinates repository metadata building, caching, and serving.
type Service struct {
	github     *github.Client
	store      store.Store
	signer     *sign.Signer
	httpClient *http.Client
	cacheTTL   time.Duration
	webHandler http.Handler
	sf         singleflight.Group
	refreshSf  singleflight.Group
}

// NewService creates a new Service instance.
func NewService(cfg ServiceConfig) *Service {
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	cacheTTL := cfg.CacheTTL
	if cacheTTL == 0 {
		cacheTTL = 24 * time.Hour
	}

	return &Service{
		github:     cfg.GitHubClient,
		store:      cfg.Store,
		signer:     cfg.Signer,
		httpClient: httpClient,
		cacheTTL:   cacheTTL,
		webHandler: cfg.WebHandler,
	}
}

// ServeHTTP handles incoming HTTP requests.
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Root or web handler requests
	if r.URL.Path == "/" || r.URL.Path == "/favicon.ico" || r.URL.Path == "/favicon.svg" ||
		strings.HasPrefix(r.URL.Path, "/_/") {
		if s.webHandler != nil {
			s.webHandler.ServeHTTP(w, r)
			return
		}
	}

	rInfo := route.ParseRoute(r.URL.Path)

	if rInfo.Owner == "" || rInfo.Repo == "" {
		http.Error(w, "Invalid repository path. Use /{owner}/{repo}/...", http.StatusBadRequest)
		return
	}

	if !validNamePattern.MatchString(rInfo.Owner) || len(rInfo.Owner) > 39 {
		http.Error(w, "Invalid owner name", http.StatusBadRequest)
		return
	}
	if !validNamePattern.MatchString(rInfo.Repo) || len(rInfo.Repo) > 100 {
		http.Error(w, "Invalid repository name", http.StatusBadRequest)
		return
	}

	// Invalidate cache if ?cache=false
	if r.URL.Query().Get("cache") == "false" {
		_ = s.store.ClearRepo(r.Context(), rInfo.Owner, rInfo.Repo)
	}

	ctx := r.Context()
	var err error

	switch rInfo.Type {
	case route.TypePublicKey:
		err = s.handlePublicKey(w, r)
	case route.TypeInRelease:
		err = s.handleInRelease(ctx, w, r, rInfo)
	case route.TypeRelease:
		err = s.handleRelease(ctx, w, r, rInfo)
	case route.TypeReleaseGpg:
		err = s.handleReleaseGpg(ctx, w, r, rInfo)
	case route.TypePackages:
		err = s.handlePackages(ctx, w, r, rInfo, false)
	case route.TypePackagesGz:
		err = s.handlePackages(ctx, w, r, rInfo, true)
	case route.TypeByHash:
		err = s.handleByHash(ctx, w, r, rInfo)
	case route.TypeBinary:
		err = s.handleBinaryRedirect(ctx, w, r, rInfo, "deb")
	case route.TypeRepomd:
		err = s.handleRepomd(ctx, w, r, rInfo)
	case route.TypeRepomdAsc:
		err = s.handleRepomdAsc(ctx, w, r, rInfo)
	case route.TypePrimary:
		err = s.handleRpmXml(ctx, w, r, rInfo, "primary", false)
	case route.TypePrimaryGz:
		err = s.handleRpmXml(ctx, w, r, rInfo, "primary", true)
	case route.TypeFilelists:
		err = s.handleRpmXml(ctx, w, r, rInfo, "filelists", false)
	case route.TypeFilelistsGz:
		err = s.handleRpmXml(ctx, w, r, rInfo, "filelists", true)
	case route.TypeOther:
		err = s.handleRpmXml(ctx, w, r, rInfo, "other", false)
	case route.TypeOtherGz:
		err = s.handleRpmXml(ctx, w, r, rInfo, "other", true)
	case route.TypeRpmBinary:
		err = s.handleBinaryRedirect(ctx, w, r, rInfo, "rpm")
	default:
		http.NotFound(w, r)
		return
	}

	if err != nil {
		var incErr *IncompleteSnapshotError
		if errors.As(err, &incErr) {
			w.Header().Set("Retry-After", "5")
			http.Error(w, "Upstream package fetch failed, retry shortly", http.StatusServiceUnavailable)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Service) handlePublicKey(w http.ResponseWriter, r *http.Request) error {
	if s.signer == nil {
		http.Error(w, "No GPG key configured", http.StatusNotFound)
		return nil
	}

	pubKey, err := s.signer.PublicKey()
	if err != nil {
		return err
	}

	w.Header().Set("Content-Type", "application/pgp-keys")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(pubKey))
	return nil
}
