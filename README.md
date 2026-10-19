# 📦 Reprox — A Serverless Github Releases APT/RPM Gateway
https://github.com/leoherzog/reprox

There are many great Linux softwares that distribute .deb and/or .rpm packages via Github Releases only. Reprox turns any GitHub repository that uses Releases into a fully compliant APT (deb) or RPM repository. Package downloads redirect straight to GitHub's CDN, moving the trust model from the maintainer of an APT/RPM repository to the maintainer of the GitHub repository.

I made this for my own personal use, so I didn't have to "Watch" for new Github Releases and manually download/install updates. I recognize that I am man-in-the-middling the traditional trust model for package repositories. Be careful to add only trusted, official upstream Github repositories. If you're worried about me man-in-the-middleing you, skip ahead to [the Self-Hosting section](#self-hosting).

Supports all Releases since [June 2025](https://github.blog/changelog/2025-06-03-releases-now-expose-digests-for-release-assets/).

## Usage

### APT (Modern)

Debian 13+/Ubuntu 24.04+ `.sources` Format

```bash
# Replace {owner}, {repo}, and {package} with the Github repository and package name

# Optional: verify the key fingerprint before importing
curl -fsSL https://reprox.dev/{owner}/{repo}/public.key | gpg --show-keys
# Verify the instance's fingerprint by browsing to it in your web browser

# Import the signing key
curl -fsSL https://reprox.dev/{owner}/{repo}/public.key | \
  sudo gpg --dearmor -o /etc/apt/keyrings/{repo}.gpg

# Add the repository
sudo tee /etc/apt/sources.list.d/{repo}.sources << EOF
Types: deb
URIs: https://reprox.dev/{owner}/{repo}
Suites: stable
Components: main
Signed-By: /etc/apt/keyrings/{repo}.gpg
EOF
# To include Releases marked as Pre-Release, change the URL to
# https://reprox.dev/{owner}/{repo}/prerelease

# Install
sudo apt update && sudo apt install {package}
```

### APT (Legacy)

`.list` Format

```bash
# Replace {owner}, {repo}, and {package} with the Github repository and package name

# Optional: verify the key fingerprint before importing
curl -fsSL https://reprox.dev/{owner}/{repo}/public.key | gpg --show-keys
# Verify the instance's fingerprint by browsing to it in your web browser

# Import the signing key
curl -fsSL https://reprox.dev/{owner}/{repo}/public.key | \
  sudo gpg --dearmor -o /etc/apt/keyrings/{repo}.gpg

echo "deb [signed-by=/etc/apt/keyrings/{repo}.gpg] https://reprox.dev/{owner}/{repo} stable main" | \
  sudo tee /etc/apt/sources.list.d/{repo}.list
# To include Releases marked as Pre-Release, change the URL to
# https://reprox.dev/{owner}/{repo}/prerelease

# Install
sudo apt update && sudo apt install {package}
```

### RPM (Fedora/RHEL/CentOS)

```bash
# Replace {owner}, {repo}, and {package} with the Github repository and package name
# 
sudo tee /etc/yum.repos.d/{repo}.repo << EOF
[{repo}]
name={repo} from GitHub via Reprox
baseurl=https://reprox.dev/{owner}/{repo}
enabled=1
gpgcheck=0
repo_gpgcheck=1
gpgkey=https://reprox.dev/{owner}/{repo}/public.key
EOF
# To include Releases marked as Pre-Release, change the URL to
# https://reprox.dev/{owner}/{repo}/prerelease

sudo dnf install {package}
# Optional: verify the key fingerprint on first update/install
# Verify the instance's fingerprint by browsing to it in your web browser
```
> [!NOTE]
> `gpgcheck=0` disables individual package signature verification because Reprox serves packages from GitHub without re-signing. Package integrity is still verified via checksums in the signed repository metadata (`repo_gpgcheck=1`).

## Self-Hosting

Feel free to use my instance at reprox.dev, or run your own instance with Go or Docker:

### Prerequisites

- Go 1.24+ (or Docker / Podman)
- GPG (for generating signing keys)

### Quick Start

```bash
# Clone repository
git clone https://github.com/leoherzog/reprox.git && cd reprox

# Build and run with Go
make build
GPG_PRIVATE_KEY="$(cat /path/to/private-key.asc)" ./reprox

# Or build and run with Docker
docker build -t reprox:latest .
docker run -p 8080:8080 -e GPG_PRIVATE_KEY="$(cat /path/to/private-key.asc)" reprox:latest
```

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
| `CACHE_DIR` | Directory for content-addressed blob storage (default: `${XDG_CACHE_HOME:-~/.cache}/reprox`) |

## About

♥ [Leo Herzog](https://herzog.tech)

🍵 [Buy me a tea!](https://herzog.tech/$)
