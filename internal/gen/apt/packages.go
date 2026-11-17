package apt

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Megatherium/gore/internal/parse/deb"
)

// PackageEntry represents a single package for Debian Packages file.
type PackageEntry struct {
	ControlData deb.DebianControlData
	Filename    string
	Size        int64
	SHA256      string
}

// GeneratePackageEntry generates a Debian Packages file entry for a single package.
func GeneratePackageEntry(entry PackageEntry) string {
	var lines []string
	c := entry.ControlData

	// Required fields
	lines = append(lines, fmt.Sprintf("Package: %s", c.Package))
	lines = append(lines, fmt.Sprintf("Version: %s", c.Version))
	lines = append(lines, fmt.Sprintf("Architecture: %s", c.Architecture))

	// Optional fields
	if c.Maintainer != "" {
		lines = append(lines, fmt.Sprintf("Maintainer: %s", c.Maintainer))
	}
	if c.InstalledSize > 0 {
		lines = append(lines, fmt.Sprintf("Installed-Size: %d", c.InstalledSize))
	}
	if c.Depends != "" {
		lines = append(lines, fmt.Sprintf("Depends: %s", c.Depends))
	}
	if c.PreDepends != "" {
		lines = append(lines, fmt.Sprintf("Pre-Depends: %s", c.PreDepends))
	}
	if c.Recommends != "" {
		lines = append(lines, fmt.Sprintf("Recommends: %s", c.Recommends))
	}
	if c.Suggests != "" {
		lines = append(lines, fmt.Sprintf("Suggests: %s", c.Suggests))
	}
	if c.Breaks != "" {
		lines = append(lines, fmt.Sprintf("Breaks: %s", c.Breaks))
	}
	if c.Enhances != "" {
		lines = append(lines, fmt.Sprintf("Enhances: %s", c.Enhances))
	}
	if c.Conflicts != "" {
		lines = append(lines, fmt.Sprintf("Conflicts: %s", c.Conflicts))
	}
	if c.Replaces != "" {
		lines = append(lines, fmt.Sprintf("Replaces: %s", c.Replaces))
	}
	if c.Provides != "" {
		lines = append(lines, fmt.Sprintf("Provides: %s", c.Provides))
	}
	if c.Section != "" {
		lines = append(lines, fmt.Sprintf("Section: %s", c.Section))
	}
	if c.Priority != "" {
		lines = append(lines, fmt.Sprintf("Priority: %s", c.Priority))
	}
	if c.Homepage != "" {
		lines = append(lines, fmt.Sprintf("Homepage: %s", c.Homepage))
	}

	// File info
	lines = append(lines, fmt.Sprintf("Filename: %s", entry.Filename))
	lines = append(lines, fmt.Sprintf("Size: %d", entry.Size))
	if entry.SHA256 != "" {
		lines = append(lines, fmt.Sprintf("SHA256: %s", entry.SHA256))
	}

	// Description (can be multi-line). Continuation lines keep their relative
	// indentation; the parser already stripped exactly one leading whitespace
	// character when folding the field value.
	if c.Description != "" {
		descLines := strings.Split(c.Description, "\n")
		lines = append(lines, fmt.Sprintf("Description: %s", descLines[0]))
		for i := 1; i < len(descLines); i++ {
			if strings.TrimSpace(descLines[i]) == "" {
				lines = append(lines, " .")
			} else {
				lines = append(lines, " "+descLines[i])
			}
		}
	}

	return strings.Join(lines, "\n")
}

// GeneratePackagesFile generates a complete Packages file from multiple entries.
// Entries are sorted by package name, version, and architecture so that output
// is byte-identical for identical inputs regardless of GitHub API ordering.
func GeneratePackagesFile(entries []PackageEntry) string {
	if len(entries) == 0 {
		return ""
	}
	sorted := make([]PackageEntry, len(entries))
	copy(sorted, entries)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i].ControlData, sorted[j].ControlData
		if a.Package != b.Package {
			return a.Package < b.Package
		}
		if a.Version != b.Version {
			return a.Version < b.Version
		}
		return a.Architecture < b.Architecture
	})
	parts := make([]string, len(sorted))
	for i, e := range sorted {
		parts[i] = GeneratePackageEntry(e)
	}
	return strings.Join(parts, "\n\n") + "\n"
}
