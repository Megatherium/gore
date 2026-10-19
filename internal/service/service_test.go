package service_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/leoherzog/reprox/internal/github"
	"github.com/leoherzog/reprox/internal/service"
	"github.com/leoherzog/reprox/internal/sign"
	"github.com/leoherzog/reprox/internal/store"
)

func createTestDeb(pkg, ver, arch string) []byte {
	var buf bytes.Buffer
	buf.WriteString("!<arch>\n")

	// debian-binary
	debBin := []byte("2.0\n")
	buf.WriteString(fmt.Sprintf("%-16s%-12s%-6s%-6s%-8s%-10d`\n", "debian-binary/", "0", "0", "0", "100644", len(debBin)))
	buf.Write(debBin)

	// control.tar.gz
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	ctrlContent := []byte(fmt.Sprintf("Package: %s\nVersion: %s\nArchitecture: %s\nMaintainer: Test\nDescription: Test package\n", pkg, ver, arch))
	_ = tw.WriteHeader(&tar.Header{
		Name: "./control",
		Mode: 0644,
		Size: int64(len(ctrlContent)),
	})
	_, _ = tw.Write(ctrlContent)
	_ = tw.Close()

	var gzBuf bytes.Buffer
	gw := gzip.NewWriter(&gzBuf)
	_, _ = gw.Write(tarBuf.Bytes())
	_ = gw.Close()

	gzBytes := gzBuf.Bytes()
	buf.WriteString(fmt.Sprintf("%-16s%-12s%-6s%-6s%-8s%-10d`\n", "control.tar.gz/", "0", "0", "0", "100644", len(gzBytes)))
	buf.Write(gzBytes)
	if len(gzBytes)%2 != 0 {
		buf.WriteByte(0x0a)
	}

	return buf.Bytes()
}

func setupMockServer(t *testing.T) (*httptest.Server, []byte) {
	t.Helper()

	debFile := createTestDeb("hello", "1.0.0", "amd64")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases"):
			pub := "2024-01-01T00:00:00Z"
			releases := []github.GitHubRelease{
				{
					ID:          1,
					TagName:     "v1.0.0",
					PublishedAt: &pub,
					Assets: []github.GitHubAsset{
						{
							ID:                 10,
							Name:               "hello_1.0.0_amd64.deb",
							Size:               int64(len(debFile)),
							BrowserDownloadURL: "http://" + r.Host + "/download/hello_1.0.0_amd64.deb",
							Digest:             "sha256:1111222233334444555566667777888899990000aaaabbbbccccddddeeeeffff",
						},
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(releases)

		case strings.HasSuffix(r.URL.Path, "/hello_1.0.0_amd64.deb"):
			if r.Method == http.MethodHead {
				w.WriteHeader(http.StatusOK)
				return
			}
			rangeHdr := r.Header.Get("Range")
			if strings.HasPrefix(rangeHdr, "bytes=0-") {
				w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(debFile)-1, len(debFile)))
				w.WriteHeader(http.StatusPartialContent)
				_, _ = w.Write(debFile)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(debFile)

		default:
			http.NotFound(w, r)
		}
	}))

	return server, debFile
}

func TestServiceAptEndpoints(t *testing.T) {
	mockServer, _ := setupMockServer(t)
	defer mockServer.Close()

	ghClient := github.NewClient("fake-token", mockServer.Client())
	ghClient.SetBaseURL(mockServer.URL)

	memStore, err := store.NewMemoryStore("")
	if err != nil {
		t.Fatalf("NewMemoryStore error: %v", err)
	}

	entity, err := openpgp.NewEntity("Reprox", "Test", "reprox@example.com", nil)
	if err != nil {
		t.Fatalf("openpgp.NewEntity error: %v", err)
	}
	var privBuf bytes.Buffer
	w, err := armor.Encode(&privBuf, openpgp.PrivateKeyType, nil)
	if err != nil {
		t.Fatalf("armor.Encode error: %v", err)
	}
	_ = entity.SerializePrivate(w, nil)
	_ = w.Close()

	signer, err := sign.NewSigner(privBuf.String(), "")
	if err != nil {
		t.Fatalf("NewSigner error: %v", err)
	}

	svc := service.NewService(service.ServiceConfig{
		GitHubClient: ghClient,
		Store:        memStore,
		Signer:       signer,
		HTTPClient:   mockServer.Client(),
		CacheTTL:     1 * time.Hour,
	})

	t.Run("public-key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/owner/repo/public.key", nil)
		rec := httptest.NewRecorder()
		svc.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "-----BEGIN PGP PUBLIC KEY BLOCK-----") {
			t.Errorf("missing public key block")
		}
	})

	t.Run("InRelease and Release", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/owner/repo/dists/stable/InRelease", nil)
		rec := httptest.NewRecorder()
		svc.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		if !strings.Contains(body, "-----BEGIN PGP SIGNED MESSAGE-----") {
			t.Errorf("missing cleartext header")
		}
		if !strings.Contains(body, "Origin: owner/repo") {
			t.Errorf("missing Origin field")
		}
		if !strings.Contains(body, "Acquire-By-Hash: yes") {
			t.Errorf("missing Acquire-By-Hash")
		}

		// Subsequent Release request should serve from cache
		relReq := httptest.NewRequest(http.MethodGet, "/owner/repo/dists/stable/Release", nil)
		relRec := httptest.NewRecorder()
		svc.ServeHTTP(relRec, relReq)

		if relRec.Code != http.StatusOK {
			t.Fatalf("expected 200 for Release, got %d", relRec.Code)
		}
		if !strings.Contains(relRec.Body.String(), "Origin: owner/repo") {
			t.Errorf("Release content mismatch")
		}
	})

	t.Run("Packages and by-hash", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/owner/repo/dists/stable/main/binary-amd64/Packages", nil)
		rec := httptest.NewRecorder()
		svc.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		if !strings.Contains(body, "Package: hello") {
			t.Errorf("Packages missing Package field")
		}
		if !strings.Contains(body, "Filename: pool/main/h/hello/hello_1.0.0_amd64.deb") {
			t.Errorf("Packages missing Filename: %s", body)
		}
	})

	t.Run("binary redirect", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/owner/repo/pool/main/h/hello/hello_1.0.0_amd64.deb", nil)
		rec := httptest.NewRecorder()
		svc.ServeHTTP(rec, req)

		if rec.Code != http.StatusFound {
			t.Fatalf("expected 302 Found, got %d: %s", rec.Code, rec.Body.String())
		}
		loc := rec.Header().Get("Location")
		if !strings.Contains(loc, "hello_1.0.0_amd64.deb") {
			t.Errorf("unexpected redirect location: %s", loc)
		}
	})
}

func TestAptStaleWhileRevalidateGate(t *testing.T) {
	var releasesCalls int32
	debFile := createTestDeb("hello", "1.0.0", "amd64")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases"):
			atomic.AddInt32(&releasesCalls, 1)
			pub := "2024-01-01T00:00:00Z"
			releases := []github.GitHubRelease{
				{
					ID:          1,
					TagName:     "v1.0.0",
					PublishedAt: &pub,
					Assets: []github.GitHubAsset{
						{
							ID:                 10,
							Name:               "hello_1.0.0_amd64.deb",
							Size:               int64(len(debFile)),
							BrowserDownloadURL: "http://" + r.Host + "/download/hello_1.0.0_amd64.deb",
							Digest:             "sha256:1111222233334444555566667777888899990000aaaabbbbccccddddeeeeffff",
						},
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(releases)

		case strings.HasSuffix(r.URL.Path, "/hello_1.0.0_amd64.deb"):
			w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(debFile)-1, len(debFile)))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(debFile)

		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	ghClient := github.NewClient("fake-token", server.Client())
	ghClient.SetBaseURL(server.URL)

	memStore, err := store.NewMemoryStore("")
	if err != nil {
		t.Fatalf("NewMemoryStore error: %v", err)
	}

	svc := service.NewService(service.ServiceConfig{
		GitHubClient: ghClient,
		Store:        memStore,
		HTTPClient:   server.Client(),
		CacheTTL:     1 * time.Hour,
	})

	fetch := func() int {
		req := httptest.NewRequest(http.MethodGet, "/owner/repo/dists/stable/InRelease", nil)
		rec := httptest.NewRecorder()
		svc.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		return int(atomic.LoadInt32(&releasesCalls))
	}

	if got := fetch(); got != 1 {
		t.Fatalf("initial request should trigger exactly one upstream fetch, got %d", got)
	}

	// Requests served from cache while the 5-minute release-ids-hash gate is
	// fresh must not trigger any upstream API traffic.
	for i := 0; i < 5; i++ {
		if got := fetch(); got != 1 {
			t.Fatalf("cached request %d triggered extra upstream fetches (total %d), want 1", i+1, got)
		}
	}
}
