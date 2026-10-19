package ar_test

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/leoherzog/reprox/internal/parse/ar"
)

func createTestAr(files []struct {
	name    string
	content []byte
}) []byte {
	var buf bytes.Buffer
	buf.WriteString("!<arch>\n")

	for _, f := range files {
		name := fmt.Sprintf("%-16s", f.name+"/")
		timestamp := fmt.Sprintf("%-12s", "0")
		owner := fmt.Sprintf("%-6s", "0")
		group := fmt.Sprintf("%-6s", "0")
		mode := fmt.Sprintf("%-8s", "100644")
		size := fmt.Sprintf("%-10d", len(f.content))
		fileMagic := "`\n"

		buf.WriteString(name + timestamp + owner + group + mode + size + fileMagic)
		buf.Write(f.content)
		if len(f.content)%2 != 0 {
			buf.WriteByte(0x0a)
		}
	}

	return buf.Bytes()
}

func TestArParser(t *testing.T) {
	files := []struct {
		name    string
		content []byte
	}{
		{"debian-binary", []byte("2.0\n")},
		{"control.tar.gz", []byte("fake-control-data")},
	}

	archive := createTestAr(files)
	entries, err := ar.ParseHeaders(archive)
	if err != nil {
		t.Fatalf("ParseHeaders error: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	if entries[0].Name != "debian-binary" {
		t.Errorf("expected debian-binary, got %s", entries[0].Name)
	}

	data, err := ar.Extract(archive, entries[0])
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}
	if string(data) != "2.0\n" {
		t.Errorf("expected 2.0\\n, got %q", string(data))
	}

	controlEntry := ar.FindEntryPrefix(entries, "control.tar")
	if controlEntry == nil {
		t.Fatalf("expected control.tar entry to be found")
	}
	if controlEntry.Name != "control.tar.gz" {
		t.Errorf("expected control.tar.gz, got %s", controlEntry.Name)
	}
}
