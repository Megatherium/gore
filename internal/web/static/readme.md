# 📦 gore — A GitHub Releases APT/RPM Gateway
https://github.com/Megatherium/gore

There are many great Linux softwares that distribute .deb and/or .rpm packages via Github Releases only. gore turns any GitHub repository that uses Releases into a fully compliant APT (deb) or RPM repository. Package downloads redirect straight to GitHub's CDN, moving the trust model from the maintainer of an APT/RPM repository to the maintainer of the GitHub repository.

gore is a from-scratch Go rewrite of [Reprox](https://github.com/leoherzog/reprox), the original Cloudflare Workers gateway by [Leo Herzog](https://herzog.tech). It keeps the same URL layout and behavior, but ships as a single static binary or Docker container you run yourself — no serverless platform, no Node.js, no external runtimes.

A repository gateway sits between you and GitHub, so add only trusted, official upstream Github repositories. Package integrity is still guaranteed by the signed repository metadata; since you host gore yourself, you control the signing key and the trust root.

Supports all Releases since [June 2025](https://github.blog/changelog/2025-06-03-releases-now-expose-digests-for-release-assets/).

## Usage

### APT (Modern)

Debian 13+/Ubuntu 24.04+ `.sources` Format

```bash
# Replace {owner}, {repo}, and {package} with the Github repository and package name

# Optional: verify the key fingerprint before importing
curl -fsSL {{BASE_URL}}/{owner}/{repo}/public.key | gpg --show-keys
# {{FINGERPRINT_COMMENT}}

# Import the signing key
curl -fsSL {{BASE_URL}}/{owner}/{repo}/public.key | \
  sudo gpg --dearmor -o /etc/apt/keyrings/{repo}.gpg

# Add the repository
sudo tee /etc/apt/sources.list.d/{repo}.sources << EOF
Types: deb
URIs: {{BASE_URL}}/{owner}/{repo}
Suites: stable
Components: main
Signed-By: /etc/apt/keyrings/{repo}.gpg
EOF
# To include Releases marked as Pre-Release, change the URL to
# {{BASE_URL}}/{owner}/{repo}/prerelease

# Install
sudo apt update && sudo apt install {package}
```

### APT (Legacy)

`.list` Format

```bash
# Replace {owner}, {repo}, and {package} with the Github repository and package name

# Optional: verify the key fingerprint before importing
curl -fsSL {{BASE_URL}}/{owner}/{repo}/public.key | gpg --show-keys
# {{FINGERPRINT_COMMENT}}

# Import the signing key
curl -fsSL {{BASE_URL}}/{owner}/{repo}/public.key | \
  sudo gpg --dearmor -o /etc/apt/keyrings/{repo}.gpg

echo "deb [signed-by=/etc/apt/keyrings/{repo}.gpg] {{BASE_URL}}/{owner}/{repo} stable main" | \
  sudo tee /etc/apt/sources.list.d/{repo}.list
# To include Releases marked as Pre-Release, change the URL to
# {{BASE_URL}}/{owner}/{repo}/prerelease

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
baseurl={{BASE_URL}}/{owner}/{repo}
enabled=1
gpgcheck=0
repo_gpgcheck=1
gpgkey={{BASE_URL}}/{owner}/{repo}/public.key
EOF
# To include Releases marked as Pre-Release, change the URL to
# {{BASE_URL}}/{owner}/{repo}/prerelease

sudo dnf install {package}
# Optional: verify the key fingerprint on first update/install
# {{FINGERPRINT_COMMENT}}
```
> [!NOTE]
> `gpgcheck=0` disables individual package signature verification because gore serves packages from GitHub without re-signing. Package integrity is still verified via checksums in the signed repository metadata (`repo_gpgcheck=1`).

## Self-Hosting

gore is a self-hosted daemon: a single static binary (or distroless Docker image) that listens on HTTP and serves repositories for any GitHub owner/repo pair. This page is served by one such instance — clone and run your own:

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

A hardened systemd unit is included in `examples/gore.service`, and configuration is done via environment variables (`PORT`, `GITHUB_TOKEN`, `CACHE_TTL`, and friends — see the README).

## About

gore is a Go rewrite of [Reprox](https://github.com/leoherzog/reprox) by [Leo Herzog](https://herzog.tech), preserving its routes, metadata formats, and signing behavior. The original is MIT-licensed; so is this rewrite.
