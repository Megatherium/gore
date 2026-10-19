package rpm_test

import (
	"strings"
	"testing"

	rpmGen "github.com/leoherzog/reprox/internal/gen/rpm"
	rpmParser "github.com/leoherzog/reprox/internal/parse/rpm"
)

func TestRepodataGenerators(t *testing.T) {
	pkg := rpmParser.RpmPackageEntry{
		HeaderData: rpmParser.RpmHeaderData{
			Name:          "test-pkg",
			Version:       "1.0.0",
			Release:       "1",
			Epoch:         0,
			Arch:          "x86_64",
			Summary:       "A test package",
			Description:   "Description of test package",
			Packager:      "Reprox",
			Vendor:        "Reprox Vendor",
			License:       "MIT",
			Group:         "Utilities",
			Url:           "https://example.com",
			BuildTime:     1700000000,
			InstalledSize: 2048,
			SourceRpm:     "test-pkg-1.0.0-1.src.rpm",
			Requires:      []string{"bash"},
			RequireFlags:  []int{rpmGen.RPMSENSE_GREATER | rpmGen.RPMSENSE_EQUAL},
			RequireVersions: []string{
				"4.0",
			},
			Provides:     []string{"test-pkg = 1.0.0"},
			ProvideFlags: []int{0},
			Files:        []string{"/usr/bin/test-pkg", "/etc/test-pkg.conf"},
			PrimaryFiles: []string{"/usr/bin/test-pkg", "/etc/test-pkg.conf"},
			Changelog: []rpmParser.RpmChangelogEntry{
				{Time: 1700000000, Author: "Author <a@example.com>", Text: "Initial release"},
			},
		},
		Filename:     "test-pkg-1.0.0-1.x86_64.rpm",
		Size:         50000,
		Checksum:     "abc123def456",
		ChecksumType: "sha256",
	}

	packages := []rpmParser.RpmPackageEntry{pkg}

	t.Run("GeneratePrimaryXml", func(t *testing.T) {
		xml := rpmGen.GeneratePrimaryXml(packages)
		if !strings.Contains(xml, `packages="1"`) {
			t.Errorf("missing packages count")
		}
		if !strings.Contains(xml, `<name>test-pkg</name>`) {
			t.Errorf("missing name tag")
		}
		if !strings.Contains(xml, `<file>/usr/bin/test-pkg</file>`) {
			t.Errorf("missing primary file")
		}
		if !strings.Contains(xml, `flags="GE"`) {
			t.Errorf("missing dependency flag GE")
		}
	})

	t.Run("GenerateFilelistsXml", func(t *testing.T) {
		xml := rpmGen.GenerateFilelistsXml(packages)
		if !strings.Contains(xml, `<file>/usr/bin/test-pkg</file>`) || !strings.Contains(xml, `<file>/etc/test-pkg.conf</file>`) {
			t.Errorf("missing files in filelists: %s", xml)
		}
	})

	t.Run("GenerateOtherXml", func(t *testing.T) {
		xml := rpmGen.GenerateOtherXml(packages)
		if !strings.Contains(xml, `author="Author &lt;a@example.com&gt;"`) {
			t.Errorf("missing escaped changelog author: %s", xml)
		}
		if !strings.Contains(xml, `Initial release</changelog>`) {
			t.Errorf("missing changelog text")
		}
	})

	t.Run("BuildRepomd", func(t *testing.T) {
		files := rpmGen.RepomdFileInfo{
			Primary:   rpmGen.RepomdFileSection{XML: "<primary/>", GZ: []byte("gz-primary")},
			Filelists: rpmGen.RepomdFileSection{XML: "<filelists/>", GZ: []byte("gz-filelists")},
			Other:     rpmGen.RepomdFileSection{XML: "<other/>", GZ: []byte("gz-other")},
			Timestamp: 1700000000,
		}

		res, err := rpmGen.BuildRepomd(files)
		if err != nil {
			t.Fatalf("BuildRepomd error: %v", err)
		}

		if !strings.Contains(res.XML, `<revision>1700000000</revision>`) {
			t.Errorf("missing revision")
		}
		if !strings.Contains(res.XML, res.Hashes.Primary.GZ+"-primary.xml.gz") {
			t.Errorf("missing primary hashed location href")
		}
	})
}

func TestRpmXmlSorting(t *testing.T) {
	packages := []rpmParser.RpmPackageEntry{
		{HeaderData: rpmParser.RpmHeaderData{Name: "zeta", Arch: "x86_64", Version: "1", Release: "1",
			Files: []string{"/usr/bin/b", "/usr/bin/a"}, PrimaryFiles: []string{"/usr/bin/b", "/usr/bin/a"}}, Filename: "zeta-1-1.x86_64.rpm"},
		{HeaderData: rpmParser.RpmHeaderData{Name: "alpha", Arch: "x86_64", Version: "1", Release: "1"}, Filename: "alpha-1-1.x86_64.rpm"},
		{HeaderData: rpmParser.RpmHeaderData{Name: "mid", Arch: "i686", Version: "1", Release: "1"}, Filename: "mid-1-1.i686.rpm"},
	}

	primary := rpmGen.GeneratePrimaryXml(packages)
	alphaIdx := strings.Index(primary, "<name>alpha</name>")
	midIdx := strings.Index(primary, "<name>mid</name>")
	zetaIdx := strings.Index(primary, "<name>zeta</name>")
	if !(alphaIdx < midIdx && midIdx < zetaIdx) {
		t.Errorf("primary.xml packages not sorted by name: alpha=%d mid=%d zeta=%d", alphaIdx, midIdx, zetaIdx)
	}

	if aIdx := strings.Index(primary, "<file>/usr/bin/a</file>"); aIdx == -1 {
		t.Errorf("primary.xml missing sorted file /usr/bin/a")
	} else if bIdx := strings.Index(primary, "<file>/usr/bin/b</file>"); bIdx < aIdx {
		t.Errorf("primary.xml file entries not sorted by path")
	}

	filelists := rpmGen.GenerateFilelistsXml(packages)
	flAlpha := strings.Index(filelists, `name="alpha"`)
	flZeta := strings.Index(filelists, `name="zeta"`)
	if flAlpha == -1 || flZeta == -1 || flAlpha > flZeta {
		t.Errorf("filelists.xml packages not sorted by name")
	}

	other := rpmGen.GenerateOtherXml(packages)
	oAlpha := strings.Index(other, `name="alpha"`)
	oZeta := strings.Index(other, `name="zeta"`)
	if oAlpha == -1 || oZeta == -1 || oAlpha > oZeta {
		t.Errorf("other.xml packages not sorted by name")
	}
}
