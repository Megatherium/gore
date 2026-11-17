# AGENTS.md

This file provides guidance to AI Agents like Claude, Gemini, Codex, and others when working with code in this repository.

## Project Overview

gore is a high-performance, standalone Go APT/RPM repository gateway daemon (`cmd/gore`) that transforms GitHub Releases into fully compliant package repositories on-the-fly. It runs as a native HTTP service (`net/http`) or Docker container, uses HTTP Range Requests to extract only package headers (avoiding full downloads), and caches metadata using an in-memory TTL store with content-addressed disk backing.

**Key Features:**
- 100% native Go application (no Node.js, wrangler, or external runtimes)
- Serves packages from **all GitHub releases** (not just the latest)
- Supports **prerelease variant** via `/prerelease` path segment
- **Private repository support** with auto-detection (proxies downloads with auth)
- Only includes packages with valid **SHA256 digests** (GitHub added this feature in June 2025)
- Excludes **draft releases** (identified by `published_at == nil`)
- Uses **pagination** with `per_page=100` (up to 5,000 releases max)
- Thread-safe caching with singleflight deduplication and XDG-compliant disk persistence

## Common Commands

```bash
make build           # Build native binary to ./gore
make test            # Run all tests with race detector (go test -v -race ./...)
make run             # Run local development server on :8080
make docker-build    # Build distroless Docker image (gore:latest)
```

## Architecture

### Request Flow
1. **Entry Point** (`cmd/gore/main.go`) - Loads configuration, initializes store, GitHub client, and GPG signer, starts HTTP server with graceful shutdown (SIGINT/SIGTERM).
2. **URL Router** (`internal/route/route.go`) - Parses incoming request paths, detects owner, repo, distribution, architecture, variant (`stable` vs `prerelease`), and route type.
3. **Application Service** (`internal/service/`) - Orchestrates handlers:
   - `apt.go`: Handles `InRelease`, `Release`, `Release.gpg`, `Packages`, and `by-hash` blobs.
   - `rpm.go`: Handles `repomd.xml`, `repomd.xml.asc`, and XML metadata blobs with Range retry ladder.
   - `binary.go`: Handles package downloads (302 redirect for public assets, authenticated streaming proxy for private repositories).
4. **GitHub Client** (`internal/github/client.go`) - Fetches and paginates GitHub Releases via `GetAllReleases()`, filters drafts and non-digest assets, extracts package headers via HTTP Range requests.
5. **Parsers** (`internal/parse/`) - Extract package metadata using HTTP Range requests:
   - `deb/deb.go`: AR archive reader (`parse/ar`) extracting `control.tar.{gz,xz,zst}` using pure-Go decoders.
   - `rpm/rpm.go`: Binary RPM Lead & Header parser with 8-byte alignment, tag index, filelists reconstruction, and `createrepo_c` filtering.
6. **Generators** (`internal/gen/`) - Generate deterministic repository metadata:
   - `apt/`: `Packages` and `Release` files with multi-architecture lists and SHA256 checksums.
   - `rpm/`: `primary.xml`, `filelists.xml`, `other.xml`, and `repomd.xml`.
7. **Signing** (`internal/sign/gpg.go`) - Pure-Go OpenPGP signing:
   - Cleartext signing for `InRelease`.
   - Detached text signing (sigclass 0x01) for `Release.gpg`.
   - Detached binary signing (sigclass 0x00) for `repomd.xml.asc`.
   - ASCII-armored public key export and 4-character space-delimited fingerprint formatting.
8. **Cache & Persistence** (`internal/store/`) - Thread-safe in-memory cache with TTL expiration, `singleflight.Group` deduplication, and optional disk persistence for content-addressed blobs (`packages-blob/` and `rpm/blob/`) complying with XDG Base Directory specification.
9. **Web & Internal APIs** (`internal/web/web.go`) - Serves embedded landing page (`readme.html`), static assets (`readme.md`, `favicon.svg`), and proxies for GitHub repository search (`/_/search`) and package detection (`/_/package`).

### URL Routes

**Standard (excludes prereleases):**
- **APT**: `/{owner}/{repo}/dists/{dist}/InRelease`, `/{owner}/{repo}/dists/{dist}/Release`, `/{owner}/{repo}/dists/{dist}/Release.gpg`, `/{owner}/{repo}/dists/{dist}/{component}/binary-{arch}/Packages[.gz]`, `/{owner}/{repo}/dists/{dist}/{component}/binary-{arch}/by-hash/SHA256/{sha256}`, `/{owner}/{repo}/pool/.../*.deb`
- **RPM**: `/{owner}/{repo}/repodata/repomd.xml`, `/{owner}/{repo}/repodata/repomd.xml.asc`, `/{owner}/{repo}/repodata/{sha256}-{primary,filelists,other}.xml.gz`, `/{owner}/{repo}/Packages/*.rpm`
- **Common**: `/{owner}/{repo}/public.key`

**Prerelease Variant (includes all releases):**
- **APT**: `/{owner}/{repo}/prerelease/dists/{dist}/InRelease`, etc.
- **RPM**: `/{owner}/{repo}/prerelease/repodata/repomd.xml`, etc.
- **Common**: `/{owner}/{repo}/prerelease/public.key`

**Internal API (for landing page search box):**
- `/_/search?q={query}` - Proxies GitHub repository search with authenticated token and store caching (5 min TTL)
- `/_/package?owner={owner}&repo={repo}` - Extracts package name from latest release `.deb`/`.rpm` asset filenames, cached (5 min TTL)

### Key Packages & Structure

```
cmd/gore/main.go            # Daemon entry point, signal handling
internal/
  config/config.go            # Environment variable parsing & XDG paths
  route/route.go              # URL routing & path decomposition
  github/client.go            # GitHub API client, pagination, Range requests
  parse/
    ar/ar.go                  # Pure-Go AR archive parser
    deb/deb.go                # Debian package & control extractor
    rpm/rpm.go                # Binary RPM header & tag parser
  gen/
    apt/                      # Packages and Release generator
    rpm/                      # repomd, primary, filelists, other XML generator
  sign/gpg.go                 # OpenPGP signing (cleartext & detached)
  store/                      # Cache store interface & in-memory/disk implementation
  service/                    # HTTP handlers (APT, RPM, binary proxy)
  web/web.go                  # Landing page handler & static assets
  xutil/                      # Cryptography, XML sanitization, arch detection
```

### Design Patterns

**Deterministic Metadata Snapshots:**
- Go map iteration is randomized by default. All package lists, architectures, and XML elements are explicitly sorted (by package name, version, architecture, and filepath) prior to serialization.
- Metadata snapshots are byte-identical across runs and server instances for the same release inputs.

**Blob-Before-Index Invariant:**
- Metadata files containing checksums (`Release`, `InRelease`, `repomd.xml`) reference content-addressed data files (`Packages`, `primary.xml.gz`, etc.).
- gore strictly writes and persists all content-addressed blobs (`packages-blob/...`, `rpm/blob/...`) to the store *before* writing the referencing index file (`Release`, `repomd.xml`). This guarantees that any client fetching an index file will never encounter a 404 or checksum mismatch when requesting child files.

**Acquire-By-Hash & Immutable Content-Addressing:**
- APT: `Acquire-By-Hash: yes` is advertised in `Release`. `Packages` files are stored as immutable blobs keyed by SHA256 (`packages-blob/{variant}/{owner}/{repo}/{sha256}`).
- RPM: Uses Fedora's `unique_md_filenames` convention — `repomd.xml` references `repodata/{sha256}-primary.xml.gz`. Blobs are stored under `rpm/blob/{variant}/{owner}/{repo}/{sha256}`.
- Legacy URLs (e.g. `binary-{arch}/Packages[.gz]`) parse the current Release file to extract the current SHA256 and serve the corresponding immutable blob, avoiding race conditions during refreshes.

**Deterministic RPM Error Handling & Range Ladder:**
- RPM headers are initially fetched with a 256KB Range request. If truncated (`ErrRpmHeaderTruncated`), the request retries with 1MB, 4MB, and 16MB ranges; a header still exceeding the ladder aborts the snapshot with `IncompleteSnapshotError` (never silently dropped).
- If a package header cannot be fetched due to transient network failures, it retries with exponential backoff (250ms base, 3 attempts). If still failing, the snapshot aborts with `IncompleteSnapshotError` (HTTP 503 + `Retry-After: 5`). gore **never silently drops a package** due to fetch failures, preventing corrupt or incomplete repository states.
- Successfully parsed headers are cached under `rpm/entry/{owner}/{repo}/{sha256}/{filename}` so retries do not repeat work.

**Private Repository Support:**
- Detects repository privacy via HEAD request.
- Public repositories: 302 redirect to GitHub's CDN (`browser_download_url`).
- Private repositories: Streams binary package data through the daemon with GitHub token authorization.

### Cache Keys

All cache keys include the release variant for proper isolation:

| Type | Key Pattern |
|------|-------------|
| APT Packages Blob (content-addressed; backs both by-hash and legacy URLs) | `packages-blob/{variant}/{owner}/{repo}/{sha256}` |
| APT Release | `release/{variant}/{owner}/{repo}` |
| APT InRelease | `inrelease/{variant}/{owner}/{repo}` |
| APT Release.gpg | `release-gpg/{variant}/{owner}/{repo}` |
| Release IDs Hash | `release-ids-hash/{variant}/{owner}/{repo}` |
| Asset URL | `asset-url/{variant}/{owner}/{repo}/{releaseHash}/{filename}` |
| RPM Snapshot (repomd.xml + signature + release IDs hash, one entry) | `rpm/snapshot/{variant}/{owner}/{repo}` |
| RPM Blob (content-addressed) | `rpm/blob/{variant}/{owner}/{repo}/{sha256}` |
| RPM Package Entry (parsed header, keyed by asset digest) | `rpm/entry/{owner}/{repo}/{sha256}/{filename}` |

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | Port for the HTTP daemon to listen on |
| `GITHUB_TOKEN` | *empty* | GitHub Personal Access Token (recommended for higher rate limits & private repos) |
| `GPG_PRIVATE_KEY` | *empty* | ASCII-armored GPG private key for signing repositories |
| `GPG_PASSPHRASE` | *empty* | Passphrase for encrypted GPG private keys |
| `GPG_PUBLIC_KEY` | *empty* | Optional override for public key (extracted from private key if omitted) |
| `CACHE_TTL` | `86400` | Cache TTL in seconds for repository content (24 hours) |
| `CACHE_DIR` | `${XDG_CACHE_HOME:-~/.cache}/gore` | Directory for persisting immutable blobs to disk |
| `MAX_CONCURRENCY` | `30` | Maximum concurrent goroutines for GitHub API asset requests |

## Testing

```bash
make test                     # Run all unit tests with race detection
go test -v ./internal/parse/deb   # Run tests for a specific package

# Integration tests with real GitHub releases
GITHUB_TOKEN=<token> RUN_INTEGRATION_TESTS=true go test -v ./...
```
