package deb_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"testing"

	"github.com/Megatherium/gore/internal/parse/deb"
	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
)

func TestParseControlFile(t *testing.T) {
	controlText := `Package: hello
Version: 2.10-1
Architecture: amd64
Maintainer: Debian QA Group <packages@qa.debian.org>
Installed-Size: 120
Depends: libc6 (>= 2.14)
Section: devel
Priority: optional
Homepage: https://www.gnu.org/software/hello/
Description: example package based on GNU hello
 The GNU hello program produces a familiar, friendly greeting. Yes,
 this is another implementation of "Hello, world!".
`

	data := deb.ParseControlFile(controlText)
	if data.Package != "hello" {
		t.Errorf("expected package hello, got %s", data.Package)
	}
	if data.Version != "2.10-1" {
		t.Errorf("expected version 2.10-1, got %s", data.Version)
	}
	if data.Architecture != "amd64" {
		t.Errorf("expected architecture amd64, got %s", data.Architecture)
	}
	if data.InstalledSize != 120 {
		t.Errorf("expected installed-size 120, got %d", data.InstalledSize)
	}
	if data.Depends != "libc6 (>= 2.14)" {
		t.Errorf("expected depends libc6, got %s", data.Depends)
	}
	if data.Homepage != "https://www.gnu.org/software/hello/" {
		t.Errorf("expected homepage, got %s", data.Homepage)
	}
	if !bytes.Contains([]byte(data.Description), []byte("The GNU hello program produces")) {
		t.Errorf("expected multiline description, got %q", data.Description)
	}
}

func createTarWithControl(controlContent []byte) []byte {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	hdr := &tar.Header{
		Name: "./control",
		Mode: 0644,
		Size: int64(len(controlContent)),
	}
	tw.WriteHeader(hdr)
	tw.Write(controlContent)
	tw.Close()
	return buf.Bytes()
}

func createArArchive(files []struct {
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

func TestParseDebBufferGzip(t *testing.T) {
	controlText := []byte("Package: my-tool\nVersion: 1.2.3\nArchitecture: arm64\nDescription: Cool tool\n")
	tarBytes := createTarWithControl(controlText)

	var gzBuf bytes.Buffer
	gw := gzip.NewWriter(&gzBuf)
	gw.Write(tarBytes)
	gw.Close()

	debBytes := createArArchive([]struct {
		name    string
		content []byte
	}{
		{"debian-binary", []byte("2.0\n")},
		{"control.tar.gz", gzBuf.Bytes()},
	})

	meta, err := deb.ParseDebBuffer(debBytes)
	if err != nil {
		t.Fatalf("ParseDebBuffer error: %v", err)
	}
	if meta.Package != "my-tool" || meta.Version != "1.2.3" || meta.Architecture != "arm64" {
		t.Errorf("unexpected metadata: %+v", meta)
	}
}

func TestParseDebBufferXz(t *testing.T) {
	controlText := []byte("Package: xz-tool\nVersion: 3.0.0\nArchitecture: amd64\n")
	tarBytes := createTarWithControl(controlText)

	var xzBuf bytes.Buffer
	xw, err := xz.NewWriter(&xzBuf)
	if err != nil {
		t.Fatalf("xz writer error: %v", err)
	}
	xw.Write(tarBytes)
	xw.Close()

	debBytes := createArArchive([]struct {
		name    string
		content []byte
	}{
		{"debian-binary", []byte("2.0\n")},
		{"control.tar.xz", xzBuf.Bytes()},
	})

	meta, err := deb.ParseDebBuffer(debBytes)
	if err != nil {
		t.Fatalf("ParseDebBuffer XZ error: %v", err)
	}
	if meta.Package != "xz-tool" || meta.Version != "3.0.0" {
		t.Errorf("unexpected metadata: %+v", meta)
	}
}

func TestParseDebBufferZstd(t *testing.T) {
	controlText := []byte("Package: zstd-tool\nVersion: 4.0.0\nArchitecture: amd64\n")
	tarBytes := createTarWithControl(controlText)

	var zstBuf bytes.Buffer
	zw, err := zstd.NewWriter(&zstBuf)
	if err != nil {
		t.Fatalf("zstd writer error: %v", err)
	}
	zw.Write(tarBytes)
	zw.Close()

	debBytes := createArArchive([]struct {
		name    string
		content []byte
	}{
		{"debian-binary", []byte("2.0\n")},
		{"control.tar.zst", zstBuf.Bytes()},
	})

	meta, err := deb.ParseDebBuffer(debBytes)
	if err != nil {
		t.Fatalf("ParseDebBuffer Zstd error: %v", err)
	}
	if meta.Package != "zstd-tool" || meta.Version != "4.0.0" {
		t.Errorf("unexpected metadata: %+v", meta)
	}
}
