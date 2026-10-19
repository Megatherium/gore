package xutil_test

import (
	"bytes"
	"compress/gzip"
	"io"
	"testing"

	"github.com/leoherzog/reprox/internal/xutil"
)

func TestCrypto(t *testing.T) {
	data := []byte("hello world")
	expected := "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"

	if hash := xutil.SHA256(data); hash != expected {
		t.Fatalf("SHA256 mismatch: got %s, want %s", hash, expected)
	}

	if hash := xutil.SHA256String("hello world"); hash != expected {
		t.Fatalf("SHA256String mismatch: got %s, want %s", hash, expected)
	}

	readerHash, err := xutil.SHA256Reader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("SHA256Reader error: %v", err)
	}
	if readerHash != expected {
		t.Fatalf("SHA256Reader mismatch: got %s, want %s", readerHash, expected)
	}

	gz, err := xutil.GzipCompress(data)
	if err != nil {
		t.Fatalf("GzipCompress error: %v", err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		t.Fatalf("gzip reader error: %v", err)
	}
	decompressed, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("decompression error: %v", err)
	}
	if string(decompressed) != string(data) {
		t.Fatalf("decompressed data mismatch: got %q, want %q", decompressed, data)
	}
}

func TestEscapeXML(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"Hello & <World>", "Hello &amp; &lt;World&gt;"},
		{"Quote \"and\" 'apostrophe'", "Quote &quot;and&quot; &apos;apostrophe&apos;"},
		{"Control \x00\x01\x08 chars \x0B\x0C removed", "Control  chars  removed"},
		{"Preserve tab\tand newline\nand cr\r", "Preserve tab\tand newline\nand cr\r"},
	}

	for _, c := range cases {
		got := xutil.EscapeXML(c.input)
		if got != c.expected {
			t.Errorf("EscapeXML(%q) = %q, want %q", c.input, got, c.expected)
		}
	}
}

func TestExtractArchFromFilename(t *testing.T) {
	cases := []struct {
		filename string
		expected string
	}{
		{"package_1.0.0_amd64.deb", "amd64"},
		{"package-1.0.0-1.x86_64.rpm", "amd64"},
		{"package_x64.deb", "amd64"},
		{"package_AMD64.deb", "amd64"},
		{"package_X86_64.rpm", "amd64"},
		{"package_1.0.0_arm64.deb", "arm64"},
		{"package-1.0.0-1.aarch64.rpm", "arm64"},
		{"package_1.0.0_i386.deb", "i386"},
		{"package-1.0.0-1.i686.rpm", "i386"},
		{"package_x86.deb", "i386"},
		{"package_x86_64.deb", "amd64"},
		{"package_1.0.0_armhf.deb", "armhf"},
		{"package-1.0.0-1.armv7.rpm", "armhf"},
		{"package_1.0.0_all.deb", "all"},
		{"package.deb", "amd64"},
		{"mystery-package-1.0.0.deb", "amd64"},
		{"go-hass-agent_11.2.0_linux_amd64.deb", "amd64"},
		{"go-hass-agent_11.2.0_linux_arm64.deb", "arm64"},
		{"obsidian_1.5.12_amd64.deb", "amd64"},
		{"obsidian-1.5.12.x86_64.rpm", "amd64"},
		{"LocalSend-1.14.0-linux-x86-64.deb", "amd64"},
		{"balena-etcher_1.18.11_amd64.deb", "amd64"},
	}

	for _, c := range cases {
		got := xutil.ExtractArchFromFilename(c.filename)
		if got != c.expected {
			t.Errorf("ExtractArchFromFilename(%q) = %q, want %q", c.filename, got, c.expected)
		}
	}
}

func TestExtractRpmArchFromFilename(t *testing.T) {
	cases := []struct {
		filename string
		expected string
	}{
		{"package-1.0.0-1.x86_64.rpm", "x86_64"},
		{"package-1.0.0-1.amd64.rpm", "x86_64"},
		{"package-1.0.0-1.aarch64.rpm", "aarch64"},
		{"package-1.0.0-1.arm64.rpm", "aarch64"},
		{"package-1.0.0-1.i686.rpm", "i686"},
		{"package-1.0.0-1.i386.rpm", "i686"},
		{"package-1.0.0-1.noarch.rpm", "noarch"},
		{"package-1.0.0-1.ppc64le.rpm", "ppc64le"},
		{"invalid", "noarch"},
	}

	for _, c := range cases {
		got := xutil.ExtractRpmArchFromFilename(c.filename)
		if got != c.expected {
			t.Errorf("ExtractRpmArchFromFilename(%q) = %q, want %q", c.filename, got, c.expected)
		}
	}
}
