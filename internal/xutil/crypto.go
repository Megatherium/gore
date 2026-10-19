package xutil

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
)

// SHA256 returns the lowercase hex-encoded SHA-256 digest of data.
func SHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// SHA256String returns the lowercase hex-encoded SHA-256 digest of s.
func SHA256String(s string) string {
	return SHA256([]byte(s))
}

// SHA256Reader computes the SHA-256 digest of an io.Reader.
func SHA256Reader(r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// GzipCompress compresses data with gzip.
// To ensure deterministic output matching web standards, Header fields (like ModTime and Name) are left zeroed.
func GzipCompress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(data); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
