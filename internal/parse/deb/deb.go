package deb

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/Megatherium/gore/internal/parse/ar"
	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
)

// DebianControlData represents parsed control file fields.
type DebianControlData struct {
	Package       string
	Version       string
	Architecture  string
	Maintainer    string
	InstalledSize int64
	Depends       string
	PreDepends    string
	Recommends    string
	Suggests      string
	Breaks        string
	Enhances      string
	Conflicts     string
	Replaces      string
	Provides      string
	Section       string
	Priority      string
	Homepage      string
	Description   string
}

// ParseDebBuffer parses a .deb file buffer (or partial buffer from a range request) to extract control data.
func ParseDebBuffer(buffer []byte) (*DebianControlData, error) {
	arEntries, err := ar.ParseHeaders(buffer)
	if err != nil {
		return nil, fmt.Errorf("failed to parse AR headers: %w", err)
	}

	controlEntry := ar.FindEntryPrefix(arEntries, "control.tar")
	if controlEntry == nil {
		return nil, fmt.Errorf("no control archive found in .deb package")
	}

	archiveData, err := ar.Extract(buffer, *controlEntry)
	if err != nil {
		return nil, fmt.Errorf("failed to extract control archive: %w", err)
	}

	var tarReader io.Reader
	switch {
	case strings.HasSuffix(controlEntry.Name, ".gz"):
		gr, err := gzip.NewReader(bytes.NewReader(archiveData))
		if err != nil {
			return nil, fmt.Errorf("failed to create gzip reader: %w", err)
		}
		defer gr.Close()
		tarReader = gr

	case strings.HasSuffix(controlEntry.Name, ".xz"):
		xr, err := xz.NewReader(bytes.NewReader(archiveData))
		if err != nil {
			return nil, fmt.Errorf("failed to create xz reader: %w", err)
		}
		tarReader = xr

	case strings.HasSuffix(controlEntry.Name, ".zst"):
		zr, err := zstd.NewReader(bytes.NewReader(archiveData))
		if err != nil {
			return nil, fmt.Errorf("failed to create zstd reader: %w", err)
		}
		defer zr.Close()
		tarReader = zr

	case controlEntry.Name == "control.tar":
		tarReader = bytes.NewReader(archiveData)

	default:
		return nil, fmt.Errorf("unknown control archive compression: %s", controlEntry.Name)
	}

	tr := tar.NewReader(tarReader)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error reading tar: %w", err)
		}

		cleanName := strings.TrimPrefix(hdr.Name, "./")
		if cleanName == "control" {
			data, err := io.ReadAll(tr)
			if err != nil {
				return nil, fmt.Errorf("failed to read control file: %w", err)
			}
			parsed := ParseControlFile(string(data))
			return &parsed, nil
		}
	}

	return nil, fmt.Errorf("no control file found in control archive")
}

// ParseControlFile parses Debian RFC 822 format control text.
func ParseControlFile(content string) DebianControlData {
	fields := make(map[string]string)
	lines := strings.Split(content, "\n")

	var currentField string
	var currentValue strings.Builder

	for _, line := range lines {
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			// Continuation line
			if currentField != "" {
				currentValue.WriteString("\n")
				currentValue.WriteString(line[1:])
			}
		} else if idx := strings.Index(line, ":"); idx != -1 {
			// Save previous field
			if currentField != "" {
				fields[strings.ToLower(currentField)] = strings.TrimSpace(currentValue.String())
			}
			currentField = line[:idx]
			currentValue.Reset()
			currentValue.WriteString(line[idx+1:])
		}
	}

	// Save last field
	if currentField != "" {
		fields[strings.ToLower(currentField)] = strings.TrimSpace(currentValue.String())
	}

	installedSize, _ := strconv.ParseInt(fields["installed-size"], 10, 64)

	arch := fields["architecture"]
	if arch == "" {
		arch = "all"
	}

	priority := fields["priority"]
	if priority == "" {
		priority = "optional"
	}

	return DebianControlData{
		Package:       fields["package"],
		Version:       fields["version"],
		Architecture:  arch,
		Maintainer:    fields["maintainer"],
		InstalledSize: installedSize,
		Depends:       fields["depends"],
		PreDepends:    fields["pre-depends"],
		Recommends:    fields["recommends"],
		Suggests:      fields["suggests"],
		Breaks:        fields["breaks"],
		Enhances:      fields["enhances"],
		Conflicts:     fields["conflicts"],
		Replaces:      fields["replaces"],
		Provides:      fields["provides"],
		Section:       fields["section"],
		Priority:      priority,
		Homepage:      fields["homepage"],
		Description:   fields["description"],
	}
}
