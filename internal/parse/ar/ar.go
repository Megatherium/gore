package ar

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

const (
	Magic      = "!<arch>\n"
	HeaderSize = 60
)

// Entry represents an AR archive member header.
type Entry struct {
	Name      string
	Timestamp int64
	OwnerID   int
	GroupID   int
	Mode      int
	Size      int64
	Offset    int64
}

// ParseHeaders parses AR archive headers from a byte slice.
func ParseHeaders(buffer []byte) ([]Entry, error) {
	if len(buffer) < len(Magic) {
		return nil, fmt.Errorf("buffer too small for AR magic")
	}

	if string(buffer[:len(Magic)]) != Magic {
		return nil, fmt.Errorf("invalid AR archive: expected magic %q, got %q", Magic, string(buffer[:len(Magic)]))
	}

	var entries []Entry
	var longNames []byte
	offset := int64(len(Magic))

	for offset+HeaderSize <= int64(len(buffer)) {
		hdr := buffer[offset : offset+HeaderSize]

		rawName := strings.TrimSpace(string(hdr[0:16]))
		timestamp, _ := strconv.ParseInt(strings.TrimSpace(string(hdr[16:28])), 10, 64)
		ownerID, _ := strconv.Atoi(strings.TrimSpace(string(hdr[28:34])))
		groupID, _ := strconv.Atoi(strings.TrimSpace(string(hdr[34:40])))
		mode64, _ := strconv.ParseInt(strings.TrimSpace(string(hdr[40:48])), 8, 32)
		size, err := strconv.ParseInt(strings.TrimSpace(string(hdr[48:58])), 10, 64)
		fileMagic := string(hdr[58:60])

		if fileMagic != "`\n" {
			return nil, fmt.Errorf("invalid AR file header at offset %d: bad magic %q", offset, fileMagic)
		}
		if err != nil || size < 0 {
			return nil, fmt.Errorf("invalid AR file header at offset %d: bad size %q", offset, strings.TrimSpace(string(hdr[48:58])))
		}

		dataOffset := offset + HeaderSize
		dataSize := size
		name := rawName

		// BSD-style extended filenames: #1/<name_len>
		if strings.HasPrefix(name, "#1/") {
			nameLen, err := strconv.Atoi(name[3:])
			if err == nil && dataOffset+int64(nameLen) <= int64(len(buffer)) {
				extName := string(bytes.TrimRight(buffer[dataOffset:dataOffset+int64(nameLen)], "\x00"))
				name = extName
				dataOffset += int64(nameLen)
				dataSize -= int64(nameLen)
			}
		}

		// GNU-style long names: the "//" member holds a table of long names,
		// referenced by later members as "/<offset>".
		if name == "//" {
			end := dataOffset + size
			if end <= int64(len(buffer)) {
				longNames = buffer[dataOffset:end]
			}
			offset += HeaderSize + size
			if offset%2 != 0 {
				offset++
			}
			continue
		}
		if len(name) > 1 && name[0] == '/' && name != "/" {
			if nameOff, err := strconv.Atoi(name[1:]); err == nil && nameOff >= 0 && nameOff < len(longNames) {
				end := nameOff
				for end < len(longNames) && longNames[end] != '\n' {
					end++
				}
				longName := strings.TrimSuffix(string(longNames[nameOff:end]), "/")
				if longName != "" {
					name = longName
				}
			}
		}

		// GNU-style: trailing slash
		if strings.HasSuffix(name, "/") {
			name = name[:len(name)-1]
		}

		entries = append(entries, Entry{
			Name:      name,
			Timestamp: timestamp,
			OwnerID:   ownerID,
			GroupID:   groupID,
			Mode:      int(mode64),
			Size:      dataSize,
			Offset:    dataOffset,
		})

		offset += HeaderSize + size
		if offset%2 != 0 {
			offset++
		}
	}

	return entries, nil
}

// Extract extracts entry data from the buffer.
func Extract(buffer []byte, entry Entry) ([]byte, error) {
	if entry.Offset+entry.Size > int64(len(buffer)) {
		return nil, fmt.Errorf("cannot extract %q: data extends beyond buffer (need %d, have %d)",
			entry.Name, entry.Offset+entry.Size, len(buffer))
	}
	return buffer[entry.Offset : entry.Offset+entry.Size], nil
}

// FindEntry finds an entry by exact name.
func FindEntry(entries []Entry, name string) *Entry {
	for i := range entries {
		if entries[i].Name == name {
			return &entries[i]
		}
	}
	return nil
}

// FindEntryPrefix finds the first entry whose name starts with prefix.
func FindEntryPrefix(entries []Entry, prefix string) *Entry {
	for i := range entries {
		if strings.HasPrefix(entries[i].Name, prefix) {
			return &entries[i]
		}
	}
	return nil
}
