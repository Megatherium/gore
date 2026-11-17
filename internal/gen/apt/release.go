package apt

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Megatherium/gore/internal/xutil"
)

// ReleaseConfig holds repository metadata configuration for Release/InRelease generation.
type ReleaseConfig struct {
	Origin        string
	Label         string
	Suite         string
	Codename      string
	Architectures []string
	Components    []string
	Description   string
	Date          time.Time
}

// ReleaseFileEntry represents an entry in the SHA256 section of a Release file.
type ReleaseFileEntry struct {
	Path   string
	Size   int64
	SHA256 string
}

// GenerateReleaseFile generates Debian Release file content.
func GenerateReleaseFile(config ReleaseConfig, files []ReleaseFileEntry) string {
	var lines []string

	d := config.Date
	if d.IsZero() {
		d = time.Now()
	}
	dateStr := d.UTC().Format("Mon, 02 Jan 2006 15:04:05 GMT")

	lines = append(lines, fmt.Sprintf("Origin: %s", config.Origin))
	lines = append(lines, fmt.Sprintf("Label: %s", config.Label))
	lines = append(lines, fmt.Sprintf("Suite: %s", config.Suite))
	lines = append(lines, fmt.Sprintf("Codename: %s", config.Codename))
	lines = append(lines, fmt.Sprintf("Date: %s", dateStr))
	lines = append(lines, fmt.Sprintf("Architectures: %s", strings.Join(config.Architectures, " ")))
	lines = append(lines, fmt.Sprintf("Components: %s", strings.Join(config.Components, " ")))
	lines = append(lines, fmt.Sprintf("Description: %s", config.Description))
	lines = append(lines, "Acquire-By-Hash: yes")

	lines = append(lines, "SHA256:")
	for _, f := range files {
		lines = append(lines, fmt.Sprintf(" %s %8d %s", f.SHA256, f.Size, f.Path))
	}

	return strings.Join(lines, "\n") + "\n"
}

// BuildReleaseEntriesForArch builds uncompressed and gzipped Release file entries for one architecture.
func BuildReleaseEntriesForArch(packagesContent string, component string, architecture string) ([]ReleaseFileEntry, error) {
	pkgBytes := []byte(packagesContent)
	packagesSha := xutil.SHA256(pkgBytes)
	packagesPath := fmt.Sprintf("%s/binary-%s/Packages", component, architecture)

	gzBytes, err := xutil.GzipCompress(pkgBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to gzip packages content: %w", err)
	}
	packagesGzSha := xutil.SHA256(gzBytes)
	packagesGzPath := fmt.Sprintf("%s/binary-%s/Packages.gz", component, architecture)

	return []ReleaseFileEntry{
		{
			Path:   packagesPath,
			Size:   int64(len(pkgBytes)),
			SHA256: packagesSha,
		},
		{
			Path:   packagesGzPath,
			Size:   int64(len(gzBytes)),
			SHA256: packagesGzSha,
		},
	}, nil
}

// BuildReleaseEntries builds entries for multiple architectures sorted deterministically.
func BuildReleaseEntries(packagesContentByArch map[string]string, component string) ([]ReleaseFileEntry, error) {
	// Sort architectures for deterministic output
	archs := make([]string, 0, len(packagesContentByArch))
	for arch := range packagesContentByArch {
		archs = append(archs, arch)
	}
	sort.Strings(archs)

	var allEntries []ReleaseFileEntry
	for _, arch := range archs {
		content := packagesContentByArch[arch]
		entries, err := BuildReleaseEntriesForArch(content, component, arch)
		if err != nil {
			return nil, err
		}
		allEntries = append(allEntries, entries...)
	}

	return allEntries, nil
}

// DefaultReleaseConfig generates a default ReleaseConfig for an owner/repo.
func DefaultReleaseConfig(owner string, repo string) ReleaseConfig {
	return ReleaseConfig{
		Origin:        fmt.Sprintf("%s/%s", owner, repo),
		Label:         repo,
		Suite:         "stable",
		Codename:      "stable",
		Architectures: []string{"amd64", "arm64", "i386", "all"},
		Components:    []string{"main"},
		Description:   fmt.Sprintf("APT repository for %s/%s hosted on gore", owner, repo),
	}
}
