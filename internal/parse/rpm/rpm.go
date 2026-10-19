package rpm

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
)

var (
	RpmMagic    = []byte{0xed, 0xab, 0xee, 0xdb}
	HeaderMagic = []byte{0x8e, 0xad, 0xe8, 0x01}
)

const (
	RpmLeadSize      = 96
	HeaderHeaderSize = 16

	// RPM Tag IDs
	TagNAME            = 1000
	TagVERSION         = 1001
	TagRELEASE         = 1002
	TagEPOCH           = 1003
	TagSUMMARY         = 1004
	TagDESCRIPTION     = 1005
	TagBUILDTIME       = 1006
	TagBUILDHOST       = 1007
	TagSIZE            = 1009
	TagVENDOR          = 1011
	TagLICENSE         = 1014
	TagPACKAGER        = 1015
	TagGROUP           = 1016
	TagURL             = 1020
	TagOS              = 1021
	TagARCH            = 1022
	TagSOURCERPM       = 1044
	TagPROVIDENAME     = 1047
	TagPROVIDEFLAGS    = 1112
	TagPROVIDEVERSION  = 1113
	TagREQUIREFLAGS    = 1048
	TagREQUIRENAME     = 1049
	TagREQUIREVERSION  = 1050
	TagCONFLICTFLAGS   = 1053
	TagCONFLICTNAME    = 1054
	TagCONFLICTVERSION = 1055
	TagOBSOLETENAME    = 1090
	TagOBSOLETEFLAGS   = 1114
	TagOBSOLETEVERSION = 1115
	TagCHANGELOGTIME   = 1080
	TagCHANGELOGNAME   = 1081
	TagCHANGELOGTEXT   = 1082
	TagDIRINDEXES      = 1116
	TagBASENAMES       = 1117
	TagDIRNAMES        = 1118
	TagFILEMODES       = 1030
	TagFILEFLAGS       = 1037
	TagFILEMTIMES      = 1038

	// Tag Types
	TypeNULL         = 0
	TypeCHAR         = 1
	TypeINT8         = 2
	TypeINT16        = 3
	TypeINT32        = 4
	TypeINT64        = 5
	TypeSTRING       = 6
	TypeBIN          = 7
	TypeSTRING_ARRAY = 8
	TypeI18NSTRING   = 9
)

// ErrRpmHeaderTruncated indicates that the buffer was too small for the declared header.
type ErrRpmHeaderTruncated struct {
	Required  int
	Available int
}

func (e *ErrRpmHeaderTruncated) Error() string {
	return fmt.Sprintf("RPM header truncated: need %d bytes, have %d", e.Required, e.Available)
}

// ErrRpmFetch indicates a network or transport failure while fetching an RPM.
type ErrRpmFetch struct {
	Message string
}

func (e *ErrRpmFetch) Error() string {
	return fmt.Sprintf("RPM fetch error: %s", e.Message)
}

// RPMFILE_GHOST file flag (ghost files are not packed in the payload).
const RPMFILEGHOST = 1 << 6

// Unix stat mode masks.
const (
	sIfmt  = 0o170000
	sIfdir = 0o040000
)

// RpmChangelogEntry represents one entry in an RPM changelog.
type RpmChangelogEntry struct {
	Time   int64  `json:"time"`
	Author string `json:"author"`
	Text   string `json:"text"`
}

// RpmHeaderData contains parsed package metadata.
type RpmHeaderData struct {
	Name             string              `json:"name"`
	Version          string              `json:"version"`
	Release          string              `json:"release"`
	Epoch            int                 `json:"epoch"`
	Summary          string              `json:"summary"`
	Description      string              `json:"description"`
	Arch             string              `json:"arch"`
	License          string              `json:"license"`
	Group            string              `json:"group"`
	Url              string              `json:"url"`
	Vendor           string              `json:"vendor"`
	Packager         string              `json:"packager"`
	BuildTime        int64               `json:"buildTime"`
	SourceRpm        string              `json:"sourceRpm"`
	InstalledSize    int64               `json:"installedSize"`
	Requires         []string            `json:"requires"`
	RequireVersions  []string            `json:"requireVersions"`
	RequireFlags     []int               `json:"requireFlags"`
	Provides         []string            `json:"provides"`
	ProvideVersions  []string            `json:"provideVersions"`
	ProvideFlags     []int               `json:"provideFlags"`
	Conflicts        []string            `json:"conflicts"`
	ConflictVersions []string            `json:"conflictVersions"`
	ConflictFlags    []int               `json:"conflictFlags"`
	Obsoletes        []string            `json:"obsoletes"`
	ObsoleteVersions []string            `json:"obsoleteVersions"`
	ObsoleteFlags    []int               `json:"obsoleteFlags"`
	Files            []string            `json:"files"`
	PrimaryFiles     []string            `json:"primaryFiles"`
	FileModes        []int               `json:"fileModes"`
	FileFlags        []int               `json:"fileFlags"`
	FileTime         int64               `json:"fileTime"`
	Changelog        []RpmChangelogEntry `json:"changelog"`
}

// RpmPackageEntry represents an entry stored in repodata.
type RpmPackageEntry struct {
	HeaderData   RpmHeaderData `json:"headerData"`
	Filename     string        `json:"filename"`
	Size         int64         `json:"size"`
	Checksum     string        `json:"checksum"`
	ChecksumType string        `json:"checksumType"`
}

var primaryFilePrefixes = []string{
	"/etc/",
	"/boot/",
	"/bin/",
	"/sbin/",
	"/lib/",
	"/lib64/",
	"/usr/bin/",
	"/usr/sbin/",
	"/usr/etc/",
	"/usr/lib/",
	"/usr/lib64/",
	"/usr/libexec/",
	"/usr/games/",
	"/usr/share/dict/",
}

var primaryFileExact = map[string]bool{
	"/usr/share/magic.mime": true,
	"/usr/share/magic":      true,
}

// IsPrimaryFile matches createrepo_c's filter for files emitted in primary.xml <format><file>.
func IsPrimaryFile(path string) bool {
	if primaryFileExact[path] {
		return true
	}
	for _, p := range primaryFilePrefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// IsDirFile reports whether the i-th file entry is a directory.
func IsDirFile(h *RpmHeaderData, i int) bool {
	if i < 0 || i >= len(h.FileModes) {
		return false
	}
	return h.FileModes[i]&sIfmt == sIfdir
}

// IsGhostFile reports whether the i-th file entry is a ghost file.
func IsGhostFile(h *RpmHeaderData, i int) bool {
	if i < 0 || i >= len(h.FileFlags) {
		return false
	}
	return h.FileFlags[i]&RPMFILEGHOST != 0
}

// ParseStringArray extracts count null-terminated strings from bytes starting at offset.
// Iteration is bounded by the buffer length, so a corrupt count cannot cause
// unbounded work.
func ParseStringArray(b []byte, offset int, count int) []string {
	stringsList := make([]string, 0, count)
	pos := offset
	for i := 0; i < count && pos >= 0 && pos < len(b); i++ {
		end := pos
		for end < len(b) && b[end] != 0 {
			end++
		}
		stringsList = append(stringsList, string(b[pos:end]))
		pos = end + 1
	}
	return stringsList
}

// ParseRpmBuffer parses an RPM buffer (lead, signature header, main header) and extracts RpmHeaderData.
func ParseRpmBuffer(buffer []byte) (*RpmHeaderData, error) {
	if len(buffer) < RpmLeadSize+HeaderHeaderSize {
		return nil, fmt.Errorf("invalid RPM file: buffer too small")
	}

	if !bytes.Equal(buffer[:4], RpmMagic) {
		return nil, fmt.Errorf("invalid RPM file: bad magic")
	}

	offset := RpmLeadSize

	// Skip signature header
	nextOffset, err := skipHeader(buffer, offset)
	if err != nil {
		return nil, err
	}
	offset = nextOffset

	// Align to 8-byte boundary
	if rem := offset % 8; rem != 0 {
		offset += 8 - rem
	}

	rawTags, err := parseHeader(buffer, offset)
	if err != nil {
		return nil, err
	}

	files := buildFileList(rawTags)
	fileModes := getIntSlice(rawTags, TagFILEMODES)
	fileFlags := getIntSlice(rawTags, TagFILEFLAGS)

	tmpHeader := RpmHeaderData{FileModes: fileModes}
	var primaryFiles []string
	for i, f := range files {
		if IsPrimaryFile(f) && !IsDirFile(&tmpHeader, i) {
			primaryFiles = append(primaryFiles, f)
		}
	}

	fileTime := maxIntSlice(getIntSlice(rawTags, TagFILEMTIMES))
	changelog := buildChangelog(rawTags)

	epoch := getInt(rawTags, TagEPOCH, 0)
	buildTime := int64(getInt(rawTags, TagBUILDTIME, 0))
	installedSize := int64(getInt(rawTags, TagSIZE, 0))

	arch := getString(rawTags, TagARCH, "noarch")
	if arch == "" {
		arch = "noarch"
	}

	return &RpmHeaderData{
		Name:             getString(rawTags, TagNAME, ""),
		Version:          getString(rawTags, TagVERSION, ""),
		Release:          getString(rawTags, TagRELEASE, ""),
		Epoch:            epoch,
		Summary:          getString(rawTags, TagSUMMARY, ""),
		Description:      getString(rawTags, TagDESCRIPTION, ""),
		Arch:             arch,
		License:          getString(rawTags, TagLICENSE, ""),
		Group:            getString(rawTags, TagGROUP, ""),
		Url:              getString(rawTags, TagURL, ""),
		Vendor:           getString(rawTags, TagVENDOR, ""),
		Packager:         getString(rawTags, TagPACKAGER, ""),
		BuildTime:        buildTime,
		SourceRpm:        getString(rawTags, TagSOURCERPM, ""),
		InstalledSize:    installedSize,
		Requires:         getStringSlice(rawTags, TagREQUIRENAME),
		RequireVersions:  getStringSlice(rawTags, TagREQUIREVERSION),
		RequireFlags:     getIntSlice(rawTags, TagREQUIREFLAGS),
		Provides:         getStringSlice(rawTags, TagPROVIDENAME),
		ProvideVersions:  getStringSlice(rawTags, TagPROVIDEVERSION),
		ProvideFlags:     getIntSlice(rawTags, TagPROVIDEFLAGS),
		Conflicts:        getStringSlice(rawTags, TagCONFLICTNAME),
		ConflictVersions: getStringSlice(rawTags, TagCONFLICTVERSION),
		ConflictFlags:    getIntSlice(rawTags, TagCONFLICTFLAGS),
		Obsoletes:        getStringSlice(rawTags, TagOBSOLETENAME),
		ObsoleteVersions: getStringSlice(rawTags, TagOBSOLETEVERSION),
		ObsoleteFlags:    getIntSlice(rawTags, TagOBSOLETEFLAGS),
		Files:            files,
		PrimaryFiles:     primaryFiles,
		FileModes:        fileModes,
		FileFlags:        fileFlags,
		FileTime:         fileTime,
		Changelog:        changelog,
	}, nil
}

func skipHeader(b []byte, offset int) (int, error) {
	if offset+HeaderHeaderSize > len(b) {
		return 0, &ErrRpmHeaderTruncated{Required: offset + HeaderHeaderSize, Available: len(b)}
	}

	if !bytes.Equal(b[offset:offset+len(HeaderMagic)], HeaderMagic) {
		return 0, fmt.Errorf("invalid RPM header magic at offset %d", offset)
	}

	nindex := int(binary.BigEndian.Uint32(b[offset+8 : offset+12]))
	hsize := int(binary.BigEndian.Uint32(b[offset+12 : offset+16]))

	end := offset + HeaderHeaderSize + (nindex * 16) + hsize
	if end > len(b) {
		return 0, &ErrRpmHeaderTruncated{Required: end, Available: len(b)}
	}
	return end, nil
}

func parseHeader(b []byte, offset int) (map[int]any, error) {
	if offset+HeaderHeaderSize > len(b) {
		return nil, &ErrRpmHeaderTruncated{Required: offset + HeaderHeaderSize, Available: len(b)}
	}

	if !bytes.Equal(b[offset:offset+len(HeaderMagic)], HeaderMagic) {
		return nil, fmt.Errorf("invalid RPM header magic at offset %d", offset)
	}

	nindex := int(binary.BigEndian.Uint32(b[offset+8 : offset+12]))
	hsize := int(binary.BigEndian.Uint32(b[offset+12 : offset+16]))

	indexStart := offset + HeaderHeaderSize
	dataStart := indexStart + (nindex * 16)
	headerEnd := dataStart + hsize

	if headerEnd > len(b) {
		return nil, &ErrRpmHeaderTruncated{Required: headerEnd, Available: len(b)}
	}

	results := make(map[int]any)

	for i := 0; i < nindex; i++ {
		entryOffset := indexStart + (i * 16)
		tag := int(binary.BigEndian.Uint32(b[entryOffset : entryOffset+4]))
		tagType := int(binary.BigEndian.Uint32(b[entryOffset+4 : entryOffset+8]))
		dataOffset := int(binary.BigEndian.Uint32(b[entryOffset+8 : entryOffset+12]))
		count := int(binary.BigEndian.Uint32(b[entryOffset+12 : entryOffset+16]))

		valOffset := dataStart + dataOffset
		if valOffset >= len(b) {
			continue
		}

		val := readTagValue(b, valOffset, tagType, count)
		if val != nil {
			results[tag] = val
		}
	}

	return results, nil
}

func readTagValue(b []byte, offset int, tagType int, count int) any {
	switch tagType {
	case TypeINT32:
		if count > 1 {
			ints := make([]int, count)
			for i := 0; i < count; i++ {
				pos := offset + (i * 4)
				if pos+4 <= len(b) {
					ints[i] = int(binary.BigEndian.Uint32(b[pos : pos+4]))
				}
			}
			return ints
		}
		if offset+4 <= len(b) {
			return int(binary.BigEndian.Uint32(b[offset : offset+4]))
		}
		return 0

	case TypeINT64:
		if count > 1 {
			ints := make([]int64, count)
			for i := 0; i < count; i++ {
				pos := offset + (i * 8)
				if pos+8 <= len(b) {
					ints[i] = int64(binary.BigEndian.Uint64(b[pos : pos+8]))
				}
			}
			return ints
		}
		if offset+8 <= len(b) {
			return int64(binary.BigEndian.Uint64(b[offset : offset+8]))
		}
		return int64(0)

	case TypeINT16:
		if count > 1 {
			ints := make([]int, count)
			for i := 0; i < count; i++ {
				pos := offset + (i * 2)
				if pos+2 <= len(b) {
					ints[i] = int(binary.BigEndian.Uint16(b[pos : pos+2]))
				}
			}
			return ints
		}
		if offset+2 <= len(b) {
			return int(binary.BigEndian.Uint16(b[offset : offset+2]))
		}
		return 0

	case TypeINT8, TypeCHAR:
		if count > 1 {
			ints := make([]int, count)
			for i := 0; i < count; i++ {
				if offset+i < len(b) {
					ints[i] = int(b[offset+i])
				}
			}
			return ints
		}
		if offset < len(b) {
			return int(b[offset])
		}
		return 0

	case TypeSTRING, TypeI18NSTRING:
		end := offset
		for end < len(b) && b[end] != 0 {
			end++
		}
		return string(b[offset:end])

	case TypeSTRING_ARRAY:
		return ParseStringArray(b, offset, count)

	default:
		return nil
	}
}

func getString(m map[int]any, tag int, fallback string) string {
	v, ok := m[tag]
	if !ok || v == nil {
		return fallback
	}
	switch s := v.(type) {
	case string:
		return s
	case []string:
		if len(s) > 0 {
			return s[0]
		}
	}
	return fallback
}

func getInt(m map[int]any, tag int, fallback int) int {
	v, ok := m[tag]
	if !ok || v == nil {
		return fallback
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case []int:
		if len(n) > 0 {
			return n[0]
		}
	case []int64:
		if len(n) > 0 {
			return int(n[0])
		}
	}
	return fallback
}

// maxIntSlice returns the maximum value in s, or 0 when empty.
func maxIntSlice(s []int) int64 {
	var max int64
	for _, v := range s {
		if int64(v) > max {
			max = int64(v)
		}
	}
	return max
}

func getStringSlice(m map[int]any, tag int) []string {
	v, ok := m[tag]
	if !ok || v == nil {
		return []string{}
	}
	switch s := v.(type) {
	case []string:
		return s
	case string:
		if s == "" {
			return []string{}
		}
		return []string{s}
	}
	return []string{}
}

func getIntSlice(m map[int]any, tag int) []int {
	v, ok := m[tag]
	if !ok || v == nil {
		return []int{}
	}
	switch n := v.(type) {
	case []int:
		return n
	case []int64:
		ints := make([]int, len(n))
		for i, x := range n {
			ints[i] = int(x)
		}
		return ints
	case int:
		return []int{n}
	case int64:
		return []int{int(n)}
	}
	return []int{}
}

func buildFileList(tags map[int]any) []string {
	basenames := getStringSlice(tags, TagBASENAMES)
	dirnames := getStringSlice(tags, TagDIRNAMES)
	dirindexes := getIntSlice(tags, TagDIRINDEXES)

	if len(basenames) == 0 {
		return []string{}
	}

	files := make([]string, len(basenames))
	for i := range basenames {
		dirIndex := 0
		if i < len(dirindexes) {
			dirIndex = dirindexes[i]
		}
		dir := "/"
		if dirIndex >= 0 && dirIndex < len(dirnames) {
			dir = dirnames[dirIndex]
		}
		files[i] = dir + basenames[i]
	}
	return files
}

func buildChangelog(tags map[int]any) []RpmChangelogEntry {
	times := getIntSlice(tags, TagCHANGELOGTIME)
	names := getStringSlice(tags, TagCHANGELOGNAME)
	texts := getStringSlice(tags, TagCHANGELOGTEXT)

	count := len(times)
	if len(names) < count {
		count = len(names)
	}
	if len(texts) < count {
		count = len(texts)
	}
	if count > 10 {
		count = 10
	}

	entries := make([]RpmChangelogEntry, count)
	for i := 0; i < count; i++ {
		entries[i] = RpmChangelogEntry{
			Time:   int64(times[i]),
			Author: names[i],
			Text:   texts[i],
		}
	}
	return entries
}
