package rpm_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/Megatherium/gore/internal/parse/rpm"
)

type tagEntry struct {
	tag      int
	tagType  int
	strVal   string
	intVal   int
	strSlice []string
	intSlice []int
}

func createTestRpm(tags []tagEntry) []byte {
	var buf bytes.Buffer

	// Lead (96 bytes)
	lead := make([]byte, 96)
	copy(lead[:4], rpm.RpmMagic)
	lead[4] = 3 // Version 3.0
	buf.Write(lead)

	// Signature Header: 16-byte header with 0 tags
	sigHdr := make([]byte, 16)
	copy(sigHdr[:3], rpm.HeaderMagic)
	sigHdr[3] = 1
	buf.Write(sigHdr)

	// Align to 8 bytes
	offset := 96 + 16
	if rem := offset % 8; rem != 0 {
		buf.Write(make([]byte, 8-rem))
	}

	// Main Header: index entries + data section
	var dataBuf bytes.Buffer
	type indexEntry struct {
		tag    int
		tType  int
		offset int
		count  int
	}
	var indexList []indexEntry

	for _, t := range tags {
		dataOffset := dataBuf.Len()
		count := 1

		switch t.tagType {
		case rpm.TypeSTRING, rpm.TypeI18NSTRING:
			dataBuf.WriteString(t.strVal)
			dataBuf.WriteByte(0)
		case rpm.TypeSTRING_ARRAY:
			count = len(t.strSlice)
			for _, s := range t.strSlice {
				dataBuf.WriteString(s)
				dataBuf.WriteByte(0)
			}
		case rpm.TypeINT32:
			if len(t.intSlice) > 0 {
				count = len(t.intSlice)
				for _, v := range t.intSlice {
					var b [4]byte
					binary.BigEndian.PutUint32(b[:], uint32(v))
					dataBuf.Write(b[:])
				}
			} else {
				var b [4]byte
				binary.BigEndian.PutUint32(b[:], uint32(t.intVal))
				dataBuf.Write(b[:])
			}
		}

		indexList = append(indexList, indexEntry{
			tag:    t.tag,
			tType:  t.tagType,
			offset: dataOffset,
			count:  count,
		})
	}

	// Main header header (16 bytes)
	mainHdr := make([]byte, 16)
	copy(mainHdr[:3], rpm.HeaderMagic)
	mainHdr[3] = 1
	binary.BigEndian.PutUint32(mainHdr[8:12], uint32(len(indexList)))
	binary.BigEndian.PutUint32(mainHdr[12:16], uint32(dataBuf.Len()))
	buf.Write(mainHdr)

	// Write index entries (16 bytes each)
	for _, idx := range indexList {
		var b [16]byte
		binary.BigEndian.PutUint32(b[0:4], uint32(idx.tag))
		binary.BigEndian.PutUint32(b[4:8], uint32(idx.tType))
		binary.BigEndian.PutUint32(b[8:12], uint32(idx.offset))
		binary.BigEndian.PutUint32(b[12:16], uint32(idx.count))
		buf.Write(b[:])
	}

	// Write data section
	buf.Write(dataBuf.Bytes())

	return buf.Bytes()
}

func TestParseRpmBuffer(t *testing.T) {
	tags := []tagEntry{
		{tag: rpm.TagNAME, tagType: rpm.TypeSTRING, strVal: "hello-world"},
		{tag: rpm.TagVERSION, tagType: rpm.TypeSTRING, strVal: "1.2.3"},
		{tag: rpm.TagRELEASE, tagType: rpm.TypeSTRING, strVal: "1.fc39"},
		{tag: rpm.TagEPOCH, tagType: rpm.TypeINT32, intVal: 1},
		{tag: rpm.TagSUMMARY, tagType: rpm.TypeSTRING, strVal: "Hello world utility"},
		{tag: rpm.TagARCH, tagType: rpm.TypeSTRING, strVal: "x86_64"},
		{tag: rpm.TagDIRNAMES, tagType: rpm.TypeSTRING_ARRAY, strSlice: []string{"/usr/bin/", "/etc/"}},
		{tag: rpm.TagBASENAMES, tagType: rpm.TypeSTRING_ARRAY, strSlice: []string{"hello", "hello.conf"}},
		{tag: rpm.TagDIRINDEXES, tagType: rpm.TypeINT32, intSlice: []int{0, 1}},
	}

	raw := createTestRpm(tags)
	header, err := rpm.ParseRpmBuffer(raw)
	if err != nil {
		t.Fatalf("ParseRpmBuffer error: %v", err)
	}

	if header.Name != "hello-world" {
		t.Errorf("expected name hello-world, got %s", header.Name)
	}
	if header.Version != "1.2.3" || header.Release != "1.fc39" {
		t.Errorf("version/release mismatch: %s-%s", header.Version, header.Release)
	}
	if header.Epoch != 1 {
		t.Errorf("expected epoch 1, got %d", header.Epoch)
	}
	if len(header.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(header.Files))
	}
	if header.Files[0] != "/usr/bin/hello" || header.Files[1] != "/etc/hello.conf" {
		t.Errorf("unexpected files: %v", header.Files)
	}
	if len(header.PrimaryFiles) != 2 {
		t.Errorf("expected 2 primary files, got %d (%v)", len(header.PrimaryFiles), header.PrimaryFiles)
	}
}

func TestRpmTruncated(t *testing.T) {
	tags := []tagEntry{
		{tag: rpm.TagNAME, tagType: rpm.TypeSTRING, strVal: "hello-world"},
	}
	raw := createTestRpm(tags)
	// Truncate the buffer
	truncated := raw[:len(raw)-5]

	_, err := rpm.ParseRpmBuffer(truncated)
	if err == nil {
		t.Fatalf("expected error on truncated buffer")
	}

	var truncErr *rpm.ErrRpmHeaderTruncated
	if !errors.As(err, &truncErr) {
		t.Errorf("expected ErrRpmHeaderTruncated, got: %v", err)
	}
}

func TestParseStringArrayUTF8(t *testing.T) {
	data := []byte("hello\x00world\x00café\x00")
	res := rpm.ParseStringArray(data, 0, 3)
	if len(res) != 3 {
		t.Fatalf("expected 3 strings, got %d", len(res))
	}
	if res[0] != "hello" || res[1] != "world" || res[2] != "café" {
		t.Errorf("unexpected strings: %v", res)
	}
}
