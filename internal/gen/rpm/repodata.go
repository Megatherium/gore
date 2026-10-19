package rpm

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	rpmParser "github.com/leoherzog/reprox/internal/parse/rpm"
	"github.com/leoherzog/reprox/internal/xutil"
)

const (
	RPMSENSE_LESS          = 0x02
	RPMSENSE_GREATER       = 0x04
	RPMSENSE_EQUAL         = 0x08
	RPMSENSE_PREREQ        = 0x40
	RPMSENSE_SCRIPT_PRE    = 0x200
	RPMSENSE_SCRIPT_POST   = 0x400
	RPMSENSE_SCRIPT_PREUN  = 0x800
	RPMSENSE_SCRIPT_POSTUN = 0x1000
	RPMSENSE_PRE_MASK      = RPMSENSE_PREREQ | RPMSENSE_SCRIPT_PRE | RPMSENSE_SCRIPT_POST | RPMSENSE_SCRIPT_PREUN | RPMSENSE_SCRIPT_POSTUN
)

// RepomdFileSection contains uncompressed XML string and gzipped bytes.
type RepomdFileSection struct {
	XML string
	GZ  []byte
}

// RepomdFileInfo describes the 3 metadata files for repomd.xml.
type RepomdFileInfo struct {
	Primary   RepomdFileSection
	Filelists RepomdFileSection
	Other     RepomdFileSection
	Timestamp int64
}

// RepomdSectionHashes holds the uncompressed and compressed SHA256 hashes.
type RepomdSectionHashes struct {
	XML string // SHA256 of uncompressed XML
	GZ  string // SHA256 of gzipped XML
}

// RepomdHashes contains hashes for primary, filelists, and other sections.
type RepomdHashes struct {
	Primary   RepomdSectionHashes
	Filelists RepomdSectionHashes
	Other     RepomdSectionHashes
}

// RepomdBuildResult holds the generated repomd.xml content and computed hashes.
type RepomdBuildResult struct {
	XML    string
	Hashes RepomdHashes
}

// BuildRepomd generates repomd.xml with content-addressed SHA256 locations.
func BuildRepomd(files RepomdFileInfo) (*RepomdBuildResult, error) {
	primaryXmlSha := xutil.SHA256String(files.Primary.XML)
	primaryGzSha := xutil.SHA256(files.Primary.GZ)

	filelistsXmlSha := xutil.SHA256String(files.Filelists.XML)
	filelistsGzSha := xutil.SHA256(files.Filelists.GZ)

	otherXmlSha := xutil.SHA256String(files.Other.XML)
	otherGzSha := xutil.SHA256(files.Other.GZ)

	hashes := RepomdHashes{
		Primary: RepomdSectionHashes{
			XML: primaryXmlSha,
			GZ:  primaryGzSha,
		},
		Filelists: RepomdSectionHashes{
			XML: filelistsXmlSha,
			GZ:  filelistsGzSha,
		},
		Other: RepomdSectionHashes{
			XML: otherXmlSha,
			GZ:  otherGzSha,
		},
	}

	primarySize := len([]byte(files.Primary.XML))
	filelistsSize := len([]byte(files.Filelists.XML))
	otherSize := len([]byte(files.Other.XML))

	xml := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<repomd xmlns="http://linux.duke.edu/metadata/repo" xmlns:rpm="http://linux.duke.edu/metadata/rpm">
  <revision>%d</revision>
  <data type="primary">
    <checksum type="sha256">%s</checksum>
    <open-checksum type="sha256">%s</open-checksum>
    <location href="repodata/%s-primary.xml.gz"/>
    <timestamp>%d</timestamp>
    <size>%d</size>
    <open-size>%d</open-size>
  </data>
  <data type="filelists">
    <checksum type="sha256">%s</checksum>
    <open-checksum type="sha256">%s</open-checksum>
    <location href="repodata/%s-filelists.xml.gz"/>
    <timestamp>%d</timestamp>
    <size>%d</size>
    <open-size>%d</open-size>
  </data>
  <data type="other">
    <checksum type="sha256">%s</checksum>
    <open-checksum type="sha256">%s</open-checksum>
    <location href="repodata/%s-other.xml.gz"/>
    <timestamp>%d</timestamp>
    <size>%d</size>
    <open-size>%d</open-size>
  </data>
</repomd>
`,
		files.Timestamp,
		primaryGzSha, primaryXmlSha, primaryGzSha, files.Timestamp, len(files.Primary.GZ), primarySize,
		filelistsGzSha, filelistsXmlSha, filelistsGzSha, files.Timestamp, len(files.Filelists.GZ), filelistsSize,
		otherGzSha, otherXmlSha, otherGzSha, files.Timestamp, len(files.Other.GZ), otherSize,
	)

	return &RepomdBuildResult{
		XML:    xml,
		Hashes: hashes,
	}, nil
}

var reLeadingEpoch = regexp.MustCompile(`^(\d+):`)

// sortedRpmPackages returns a copy of packages sorted by name, arch, epoch,
// version, and release so that output is byte-identical for identical inputs
// regardless of GitHub API ordering. File lists are sorted by path as well.
func sortedRpmPackages(packages []rpmParser.RpmPackageEntry) []rpmParser.RpmPackageEntry {
	sorted := make([]rpmParser.RpmPackageEntry, len(packages))
	copy(sorted, packages)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i].HeaderData, sorted[j].HeaderData
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		if a.Arch != b.Arch {
			return a.Arch < b.Arch
		}
		if a.Epoch != b.Epoch {
			return a.Epoch < b.Epoch
		}
		if a.Version != b.Version {
			return a.Version < b.Version
		}
		if a.Release != b.Release {
			return a.Release < b.Release
		}
		return sorted[i].Filename < sorted[j].Filename
	})
	for i := range sorted {
		h := &sorted[i].HeaderData
		h.Files = sortedStringsCopy(h.Files)
		h.PrimaryFiles = sortedStringsCopy(h.PrimaryFiles)
	}
	return sorted
}

func sortedStringsCopy(in []string) []string {
	if len(in) < 2 {
		return in
	}
	out := make([]string, len(in))
	copy(out, in)
	sort.Strings(out)
	return out
}

// GeneratePrimaryXml generates primary.xml metadata.
func GeneratePrimaryXml(packages []rpmParser.RpmPackageEntry) string {
	packages = sortedRpmPackages(packages)
	var sb strings.Builder
	for i, pkg := range packages {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(generatePackageXml(pkg))
	}

	packageEntries := sb.String()
	if packageEntries != "" {
		return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<metadata xmlns="http://linux.duke.edu/metadata/common" xmlns:rpm="http://linux.duke.edu/metadata/rpm" packages="%d">
%s
</metadata>
`, len(packages), packageEntries)
	}

	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<metadata xmlns="http://linux.duke.edu/metadata/common" xmlns:rpm="http://linux.duke.edu/metadata/rpm" packages="%d">

</metadata>
`, len(packages))
}

// GenerateFilelistsXml generates filelists.xml metadata.
func GenerateFilelistsXml(packages []rpmParser.RpmPackageEntry) string {
	packages = sortedRpmPackages(packages)
	var sb strings.Builder
	for i, pkg := range packages {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(generateFilelistPackageXml(pkg))
	}

	packageEntries := sb.String()
	if packageEntries != "" {
		return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<filelists xmlns="http://linux.duke.edu/metadata/filelists" packages="%d">
%s
</filelists>
`, len(packages), packageEntries)
	}

	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<filelists xmlns="http://linux.duke.edu/metadata/filelists" packages="%d">

</filelists>
`, len(packages))
}

// GenerateOtherXml generates other.xml metadata.
func GenerateOtherXml(packages []rpmParser.RpmPackageEntry) string {
	packages = sortedRpmPackages(packages)
	var sb strings.Builder
	for i, pkg := range packages {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(generateOtherPackageXml(pkg))
	}

	packageEntries := sb.String()
	if packageEntries != "" {
		return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<otherdata xmlns="http://linux.duke.edu/metadata/other" packages="%d">
%s
</otherdata>
`, len(packages), packageEntries)
	}

	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<otherdata xmlns="http://linux.duke.edu/metadata/other" packages="%d">

</otherdata>
`, len(packages))
}

func rpmFlagsToString(flags int) string {
	cmp := flags & (RPMSENSE_LESS | RPMSENSE_GREATER | RPMSENSE_EQUAL)
	switch cmp {
	case RPMSENSE_LESS | RPMSENSE_EQUAL:
		return "LE"
	case RPMSENSE_GREATER | RPMSENSE_EQUAL:
		return "GE"
	case RPMSENSE_EQUAL:
		return "EQ"
	case RPMSENSE_LESS:
		return "LT"
	case RPMSENSE_GREATER:
		return "GT"
	default:
		return ""
	}
}

func formatRpmEntry(name string, flags int, version string) string {
	flagStr := rpmFlagsToString(flags)
	isPre := (flags & RPMSENSE_PRE_MASK) != 0

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("      <rpm:entry name=\"%s\"", xutil.EscapeXML(name)))

	if flagStr != "" && version != "" {
		epoch := "0"
		ver := version
		if m := reLeadingEpoch.FindStringSubmatch(version); m != nil {
			epoch = m[1]
			ver = version[len(m[0]):]
		}
		sb.WriteString(fmt.Sprintf(" flags=\"%s\" epoch=\"%s\" ver=\"%s\"", flagStr, epoch, xutil.EscapeXML(ver)))
	}

	if isPre {
		sb.WriteString(" pre=\"1\"")
	}

	sb.WriteString("/>")
	return sb.String()
}

func formatDepSection(tag string, names []string, flags []int, versions []string) string {
	if len(names) == 0 {
		return ""
	}
	var lines []string
	for i, name := range names {
		f := 0
		if i < len(flags) {
			f = flags[i]
		}
		v := ""
		if i < len(versions) {
			v = versions[i]
		}
		lines = append(lines, formatRpmEntry(name, f, v))
	}
	return fmt.Sprintf("    <rpm:%s>\n%s\n    </rpm:%s>", tag, strings.Join(lines, "\n"), tag)
}

func generatePackageXml(pkg rpmParser.RpmPackageEntry) string {
	h := pkg.HeaderData
	epoch := h.Epoch
	ver := h.Version
	rel := h.Release

	requiresXml := formatDepSection("requires", h.Requires, h.RequireFlags, h.RequireVersions)
	providesXml := formatDepSection("provides", h.Provides, h.ProvideFlags, h.ProvideVersions)
	conflictsXml := formatDepSection("conflicts", h.Conflicts, h.ConflictFlags, h.ConflictVersions)
	obsoletesXml := formatDepSection("obsoletes", h.Obsoletes, h.ObsoleteFlags, h.ObsoleteVersions)

	var filesLines []string
	for _, f := range h.PrimaryFiles {
		filesLines = append(filesLines, fmt.Sprintf("      <file>%s</file>", xutil.EscapeXML(f)))
	}

	packager := h.Packager
	if packager == "" {
		packager = h.Vendor
	}

	group := h.Group
	if group == "" {
		group = "Unspecified"
	}

	pkgidAttr := "NO"
	if pkg.Checksum != "" {
		pkgidAttr = "YES"
	}

	fileTime := h.FileTime
	if fileTime == 0 {
		fileTime = h.BuildTime
	}

	// createrepo_c format child order: provides, requires, conflicts, obsoletes,
	// then file entries.
	var formatChildren []string
	for _, section := range []string{providesXml, requiresXml, conflictsXml, obsoletesXml} {
		if section != "" {
			formatChildren = append(formatChildren, section)
		}
	}
	if len(filesLines) > 0 {
		formatChildren = append(formatChildren, strings.Join(filesLines, "\n"))
	}
	formatBody := ""
	if len(formatChildren) > 0 {
		formatBody = "\n" + strings.Join(formatChildren, "\n")
	}

	return fmt.Sprintf(`  <package type="rpm">
    <name>%s</name>
    <arch>%s</arch>
    <version epoch="%d" ver="%s" rel="%s"/>
    <checksum type="%s" pkgid="%s">%s</checksum>
    <summary>%s</summary>
    <description>%s</description>
    <packager>%s</packager>
    <url>%s</url>
    <time file="%d" build="%d"/>
    <size package="%d" installed="%d"/>
    <location href="Packages/%s"/>
    <format>
      <rpm:license>%s</rpm:license>
      <rpm:vendor>%s</rpm:vendor>
      <rpm:group>%s</rpm:group>
      <rpm:sourcerpm>%s</rpm:sourcerpm>%s
    </format>
  </package>`,
		xutil.EscapeXML(h.Name),
		xutil.EscapeXML(h.Arch),
		epoch, xutil.EscapeXML(ver), xutil.EscapeXML(rel),
		pkg.ChecksumType, pkgidAttr, pkg.Checksum,
		xutil.EscapeXML(h.Summary),
		xutil.EscapeXML(h.Description),
		xutil.EscapeXML(packager),
		xutil.EscapeXML(h.Url),
		fileTime, h.BuildTime,
		pkg.Size, h.InstalledSize,
		xutil.EscapeXML(pkg.Filename),
		xutil.EscapeXML(h.License),
		xutil.EscapeXML(h.Vendor),
		xutil.EscapeXML(group),
		xutil.EscapeXML(h.SourceRpm),
		formatBody,
	)
}

func generateFilelistPackageXml(pkg rpmParser.RpmPackageEntry) string {
	h := pkg.HeaderData
	epoch := h.Epoch

	var filesLines []string
	for i, f := range h.Files {
		typeAttr := ""
		if rpmParser.IsGhostFile(&h, i) {
			typeAttr = " type=\"ghost\""
		} else if rpmParser.IsDirFile(&h, i) {
			typeAttr = " type=\"dir\""
		}
		filesLines = append(filesLines, fmt.Sprintf("    <file%s>%s</file>", typeAttr, xutil.EscapeXML(f)))
	}
	filesXml := strings.Join(filesLines, "\n")

	return fmt.Sprintf(`  <package pkgid="%s" name="%s" arch="%s">
    <version epoch="%d" ver="%s" rel="%s"/>
%s
  </package>`,
		pkg.Checksum, xutil.EscapeXML(h.Name), xutil.EscapeXML(h.Arch),
		epoch, xutil.EscapeXML(h.Version), xutil.EscapeXML(h.Release),
		filesXml,
	)
}

func generateOtherPackageXml(pkg rpmParser.RpmPackageEntry) string {
	h := pkg.HeaderData
	epoch := h.Epoch

	var changelogLines []string
	for _, entry := range h.Changelog {
		changelogLines = append(changelogLines, fmt.Sprintf("    <changelog author=\"%s\" date=\"%d\">%s</changelog>",
			xutil.EscapeXML(entry.Author), entry.Time, xutil.EscapeXML(entry.Text)))
	}
	changelogXml := strings.Join(changelogLines, "\n")

	return fmt.Sprintf(`  <package pkgid="%s" name="%s" arch="%s">
    <version epoch="%d" ver="%s" rel="%s"/>
%s
  </package>`,
		pkg.Checksum, xutil.EscapeXML(h.Name), xutil.EscapeXML(h.Arch),
		epoch, xutil.EscapeXML(h.Version), xutil.EscapeXML(h.Release),
		changelogXml,
	)
}
