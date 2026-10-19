package route

import (
	"regexp"
	"strings"
)

// RouteType identifies the parsed Reprox route.
type RouteType string

const (
	TypePublicKey   RouteType = "public-key"
	TypeInRelease   RouteType = "inrelease"
	TypeRelease     RouteType = "release"
	TypeReleaseGpg  RouteType = "release-gpg"
	TypePackages    RouteType = "packages"
	TypePackagesGz  RouteType = "packages-gz"
	TypeByHash      RouteType = "by-hash"
	TypeBinary      RouteType = "binary"
	TypeRepomd      RouteType = "repomd"
	TypeRepomdAsc   RouteType = "repomd-asc"
	TypePrimary     RouteType = "primary"
	TypePrimaryGz   RouteType = "primary-gz"
	TypeFilelists   RouteType = "filelists"
	TypeFilelistsGz RouteType = "filelists-gz"
	TypeOther       RouteType = "other"
	TypeOtherGz     RouteType = "other-gz"
	TypeRpmBinary   RouteType = "rpm-binary"
	TypeUnknown     RouteType = "unknown"
)

// RouteInfo stores the parsed path parameters.
type RouteInfo struct {
	Owner          string
	Repo           string
	Distribution   string
	Component      string
	Architecture   string
	Filename       string
	HashType       string // SHA256, SHA512, etc.
	Hash           string // Hash string
	ReleaseVariant string // "stable" or "prerelease"
	Type           RouteType
}

var (
	reHashedRpm = regexp.MustCompile(`^([0-9a-f]{64})-(primary|filelists|other)\.xml(\.gz)?$`)
)

// ParseRoute parses an incoming URL pathname into RouteInfo.
func ParseRoute(pathname string) RouteInfo {
	var parts []string
	for _, segment := range strings.Split(pathname, "/") {
		if segment != "" {
			parts = append(parts, segment)
		}
	}

	route := RouteInfo{
		Distribution:   "stable",
		Component:      "main",
		Architecture:   "amd64",
		ReleaseVariant: "stable",
		Type:           TypeUnknown,
	}

	if len(parts) > 0 {
		route.Owner = parts[0]
	}
	if len(parts) > 1 {
		route.Repo = parts[1]
	}

	hasPrerelease := len(parts) > 2 && parts[2] == "prerelease"
	if hasPrerelease {
		route.ReleaseVariant = "prerelease"
	}

	offset := 0
	if hasPrerelease {
		offset = 1
	}

	p := func(index int) string {
		actualIndex := index + offset
		if actualIndex >= 0 && actualIndex < len(parts) {
			return parts[actualIndex]
		}
		return ""
	}

	// /{owner}/{repo}(/prerelease)?/public.key
	if p(2) == "public.key" {
		route.Type = TypePublicKey
		return route
	}

	// /{owner}/{repo}(/prerelease)?/dists/{dist}/...
	if p(2) == "dists" && p(3) != "" {
		route.Distribution = p(3)

		if p(4) == "InRelease" {
			route.Type = TypeInRelease
			return route
		}
		if p(4) == "Release.gpg" {
			route.Type = TypeReleaseGpg
			return route
		}
		if p(4) == "Release" {
			route.Type = TypeRelease
			return route
		}

		if p(4) != "" && strings.HasPrefix(p(5), "binary-") {
			route.Component = p(4)
			route.Architecture = strings.TrimPrefix(p(5), "binary-")

			if p(6) == "Packages" {
				route.Type = TypePackages
				return route
			}
			if p(6) == "Packages.gz" {
				route.Type = TypePackagesGz
				return route
			}
			if p(6) == "by-hash" && p(7) != "" && p(8) != "" {
				route.Type = TypeByHash
				route.HashType = p(7)
				route.Hash = p(8)
				return route
			}
		}
	}

	// /{owner}/{repo}(/prerelease)?/pool/{component}/{prefix}/{package}/{file}.deb
	if p(2) == "pool" {
		filename := parts[len(parts)-1]
		if strings.HasSuffix(filename, ".deb") {
			route.Type = TypeBinary
			route.Filename = filename
			return route
		}
	}

	// RPM Repository Routes
	// /{owner}/{repo}(/prerelease)?/repodata/{file}
	if p(2) == "repodata" {
		file := p(3)

		staticRoutes := map[string]RouteType{
			"repomd.xml":       TypeRepomd,
			"repomd.xml.asc":   TypeRepomdAsc,
			"primary.xml":      TypePrimary,
			"primary.xml.gz":   TypePrimaryGz,
			"filelists.xml":    TypeFilelists,
			"filelists.xml.gz": TypeFilelistsGz,
			"other.xml":        TypeOther,
			"other.xml.gz":     TypeOtherGz,
		}

		if t, ok := staticRoutes[file]; ok {
			route.Type = t
			return route
		}

		// Hashed metadata files: {sha256}-(primary|filelists|other).xml(.gz)?
		if match := reHashedRpm.FindStringSubmatch(file); len(match) > 0 {
			hash := match[1]
			kind := match[2]
			gzSuffix := match[3]

			switch kind {
			case "primary":
				if gzSuffix != "" {
					route.Type = TypePrimaryGz
				} else {
					route.Type = TypePrimary
				}
			case "filelists":
				if gzSuffix != "" {
					route.Type = TypeFilelistsGz
				} else {
					route.Type = TypeFilelists
				}
			case "other":
				if gzSuffix != "" {
					route.Type = TypeOtherGz
				} else {
					route.Type = TypeOther
				}
			}
			route.Hash = hash
			return route
		}
	}

	// /{owner}/{repo}(/prerelease)?/Packages/{file}.rpm
	if p(2) == "Packages" {
		filename := parts[len(parts)-1]
		if strings.HasSuffix(filename, ".rpm") {
			route.Type = TypeRpmBinary
			route.Filename = filename
			return route
		}
	}

	return route
}
