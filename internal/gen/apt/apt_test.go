package apt_test

import (
	"strings"
	"testing"
	"time"

	"github.com/leoherzog/reprox/internal/gen/apt"
	"github.com/leoherzog/reprox/internal/parse/deb"
)

func TestGeneratePackageEntry(t *testing.T) {
	entry := apt.PackageEntry{
		ControlData: deb.DebianControlData{
			Package:       "test-pkg",
			Version:       "1.0.0",
			Architecture:  "amd64",
			Maintainer:    "Maintainer <m@example.com>",
			InstalledSize: 1024,
			Depends:       "libc6 (>= 2.14)",
			Description:   "First line\nSecond line\n\nFourth line",
		},
		Filename: "pool/main/t/test-pkg/test-pkg_1.0.0_amd64.deb",
		Size:     12345,
		SHA256:   "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
	}

	out := apt.GeneratePackageEntry(entry)
	expectedLines := []string{
		"Package: test-pkg",
		"Version: 1.0.0",
		"Architecture: amd64",
		"Maintainer: Maintainer <m@example.com>",
		"Installed-Size: 1024",
		"Depends: libc6 (>= 2.14)",
		"Filename: pool/main/t/test-pkg/test-pkg_1.0.0_amd64.deb",
		"Size: 12345",
		"SHA256: e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		"Description: First line",
		" Second line",
		" .",
		" Fourth line",
	}

	for _, line := range expectedLines {
		if !strings.Contains(out, line) {
			t.Errorf("missing expected line: %s\ngot:\n%s", line, out)
		}
	}
}

func TestGenerateReleaseFile(t *testing.T) {
	date, _ := time.Parse(time.RFC3339, "2024-01-01T12:00:00Z")
	cfg := apt.ReleaseConfig{
		Origin:        "owner/repo",
		Label:         "repo",
		Suite:         "stable",
		Codename:      "stable",
		Architectures: []string{"amd64", "arm64"},
		Components:    []string{"main"},
		Description:   "Test repository",
		Date:          date,
	}

	files := []apt.ReleaseFileEntry{
		{
			Path:   "main/binary-amd64/Packages",
			Size:   1234,
			SHA256: "aaaabbbbccccddddeeeeffff0000111122223333444455556666777788889999",
		},
		{
			Path:   "main/binary-amd64/Packages.gz",
			Size:   567,
			SHA256: "1111222233334444555566667777888899990000aaaabbbbccccddddeeeeffff",
		},
	}

	out := apt.GenerateReleaseFile(cfg, files)
	expectedHeader := `Origin: owner/repo
Label: repo
Suite: stable
Codename: stable
Date: Mon, 01 Jan 2024 12:00:00 GMT
Architectures: amd64 arm64
Components: main
Description: Test repository
Acquire-By-Hash: yes
SHA256:
 aaaabbbbccccddddeeeeffff0000111122223333444455556666777788889999     1234 main/binary-amd64/Packages
 1111222233334444555566667777888899990000aaaabbbbccccddddeeeeffff      567 main/binary-amd64/Packages.gz
`

	if out != expectedHeader {
		t.Errorf("GenerateReleaseFile mismatch.\nGot:\n%s\nWant:\n%s", out, expectedHeader)
	}
}

func TestGeneratePackagesFileSorting(t *testing.T) {
	entries := []apt.PackageEntry{
		{ControlData: deb.DebianControlData{Package: "zeta", Version: "1.0", Architecture: "amd64"}, Filename: "zeta.deb", Size: 1, SHA256: "aa"},
		{ControlData: deb.DebianControlData{Package: "alpha", Version: "2.0", Architecture: "amd64"}, Filename: "alpha.deb", Size: 1, SHA256: "bb"},
		{ControlData: deb.DebianControlData{Package: "alpha", Version: "1.0", Architecture: "amd64"}, Filename: "alpha-old.deb", Size: 1, SHA256: "cc"},
	}

	out := apt.GeneratePackagesFile(entries)
	zetaIdx := strings.Index(out, "Package: zeta")
	alphaV2 := strings.Index(out, "Package: alpha\nVersion: 2.0")
	alphaV1 := strings.Index(out, "Package: alpha\nVersion: 1.0")
	if zetaIdx == -1 || alphaV1 == -1 || alphaV2 == -1 {
		t.Fatalf("missing package stanzas in output:\n%s", out)
	}
	if !(alphaV1 < alphaV2 && alphaV2 < zetaIdx) {
		t.Errorf("Packages entries not sorted by (name, version): v1=%d v2=%d zeta=%d", alphaV1, alphaV2, zetaIdx)
	}
}

func TestGeneratePackagesFileDependencyFields(t *testing.T) {
	entry := apt.PackageEntry{
		ControlData: deb.DebianControlData{
			Package:      "dep-pkg",
			Version:      "1.0",
			Architecture: "amd64",
			PreDepends:   "libc6",
			Breaks:       "old-pkg",
			Enhances:     "other-pkg",
		},
		Filename: "dep-pkg.deb",
		Size:     1,
	}

	out := apt.GeneratePackageEntry(entry)
	for _, want := range []string{"Pre-Depends: libc6", "Breaks: old-pkg", "Enhances: other-pkg"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}
