# 📦 gore — A GitHub Releases APT/RPM Gateway
https://github.com/Megatherium/gore

There are many great Linux softwares that distribute .deb and/or .rpm packages via GitHub Releases only. gore turns any GitHub repository that uses Releases into a fully compliant APT (deb) or RPM repository. Package downloads redirect straight to GitHub's CDN, moving the trust model from the maintainer of an APT/RPM repository to the maintainer of the GitHub repository.

gore is a from-scratch [Go](https://go.dev) rewrite of [Reprox](https://github.com/leoherzog/reprox), the original Cloudflare Workers gateway by [Leo Herzog](https://herzog.tech). It keeps the same URL layout and behavior, but ships as a single static binary or Docker container you run yourself — no serverless platform, no Node.js, no external runtimes.

A repository gateway sits between you and GitHub, so add only trusted, official upstream GitHub repositories. Package integrity is still guaranteed by the signed repository metadata; since you host gore yourself, you control the signing key and the trust root.

Supports all Releases since [June 2025](https://github.blog/changelog/2025-06-03-releases-now-expose-digests-for-release-assets/) (when GitHub started exposing SHA256 digests for release assets).

## Usage

The examples below assume your gore instance is reachable at `https://gore.example.com` — replace that with your own instance's base URL.

### APT (Modern)

Debian 13+/Ubuntu 24.04+ `.sources` Format

```bash
# Replace {owner}, {repo}, and {package} with the Github repository and package name

# Optional: verify the key fingerprint before importing
curl -fsSL https://gore.example.com/{owner}/{repo}/public.key | gpg --show-keys
# Verify the instance's fingerprint by browsing to it in your web browser

# Import the signing key
curl -fsSL https://gore.example.com/{owner}/{repo}/public.key | \
  sudo gpg --dearmor -o /etc/apt/keyrings/{repo}.gpg

# Add the repository
sudo tee /etc/apt/sources.list.d/{repo}.sources << EOF
Types: deb
URIs: https://gore.example.com/{owner}/{repo}
Suites: stable
Components: main
Signed-By: /etc/apt/keyrings/{repo}.gpg
EOF
# To include Releases marked as Pre-Release, change the URL to
# https://gore.example.com/{owner}/{repo}/prerelease

# Install
sudo apt update && sudo apt install {package}
```

### APT (Legacy)

`.list` Format

```bash
# Replace {owner}, {repo}, and {package} with the Github repository and package name

# Optional: verify the key fingerprint before importing
curl -fsSL https://gore.example.com/{owner}/{repo}/public.key | gpg --show-keys
# Verify the instance's fingerprint by browsing to it in your web browser

# Import the signing key
curl -fsSL https://gore.example.com/{owner}/{repo}/public.key | \
  sudo gpg --dearmor -o /etc/apt/keyrings/{repo}.gpg

echo "deb [signed-by=/etc/apt/keyrings/{repo}.gpg] https://gore.example.com/{owner}/{repo} stable main" | \
  sudo tee /etc/apt/sources.list.d/{repo}.list
# To include Releases marked as Pre-Release, change the URL to
# https://gore.example.com/{owner}/{repo}/prerelease

# Install
sudo apt update && sudo apt install {package}
```

### RPM (Fedora/RHEL/CentOS)

```bash
# Replace {owner}, {repo}, and {package} with the Github repository and package name
# 
sudo tee /etc/yum.repos.d/{repo}.repo << EOF
[{repo}]
name={repo} from GitHub via gore
baseurl=https://gore.example.com/{owner}/{repo}
enabled=1
gpgcheck=0
repo_gpgcheck=1
gpgkey=https://gore.example.com/{owner}/{repo}/public.key
EOF
# To include Releases marked as Pre-Release, change the URL to
# https://gore.example.com/{owner}/{repo}/prerelease

sudo dnf install {package}
# Optional: verify the key fingerprint on first update/install
# Verify the instance's fingerprint by browsing to it in your web browser
```
> [!NOTE]
> `gpgcheck=0` disables individual package signature verification because gore serves packages from GitHub without re-signing. Package integrity is still verified via checksums in the signed repository metadata (`repo_gpgcheck=1`).

## Self-Hosting

gore is a self-hosted daemon: a single static binary (or distroless Docker image) that listens on HTTP and serves repositories for any GitHub owner/repo pair.

### Prerequisites

- Go 1.27+ (or Docker / Podman)
- GPG (for generating signing keys)

### Quick Start

```bash
# Clone and build
git clone https://github.com/Megatherium/gore.git && cd gore
make build

# Generate a signing key (optional; without one, repositories are served unsigned)
gpg --quick-gen-key "gore" rsa4096 sign never

# Run
GPG_PRIVATE_KEY="$(gpg --armor --export-secret-keys 'gore')" ./gore
```

Or with Docker:

```bash
docker build -t gore:latest .
docker run -p 8080:8080 \
  -e GPG_PRIVATE_KEY="$(gpg --armor --export-secret-keys 'gore')" \
  gore:latest
```

For production, a hardened systemd unit is provided in [`examples/gore.service`](examples/gore.service).

### Environment Variables

| Variable | Description |
|---|---|
| `PORT` | Port to listen on (default: `8080`) |
| `HOST` | Address to listen on (default: `0.0.0.0`) |
| `GPG_PRIVATE_KEY` | ASCII-armored GPG private key for repository signing |
| `GPG_PASSPHRASE` | Passphrase for encrypted GPG private keys (optional) |
| `GPG_PUBLIC_KEY` | Public key override (auto-extracted from private key if omitted) |
| `GITHUB_TOKEN` | GitHub PAT for higher API rate limits (5,000 req/hr) and private repo access |
| `CACHE_TTL` | Cache TTL in seconds for content (default: `86400`) |
| `CACHE_DIR` | Directory for content-addressed blob storage (default: `${XDG_CACHE_HOME:-~/.cache}/gore`) |
| `MAX_CONCURRENCY` | Maximum concurrent goroutines for GitHub API asset requests (default: `30`) |

## About

gore is a Go rewrite of [Reprox](https://github.com/leoherzog/reprox) by [Leo Herzog](https://herzog.tech), preserving its routes, metadata formats, and signing behavior. The original is MIT-licensed; so is this rewrite.
