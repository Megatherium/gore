package config

import (
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Config holds runtime configuration settings.
type Config struct {
	Port          string
	Host          string
	GpgPrivateKey string
	GpgPassphrase string
	GpgPublicKey  string
	GitHubToken   string
	CacheTTL      time.Duration
	CacheDir      string
}

// Load loads configuration from environment variables with sensible defaults.
func Load() *Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	host := os.Getenv("HOST")
	if host == "" {
		host = "0.0.0.0"
	}

	cacheTTLSeconds := 86400
	if ttlStr := os.Getenv("CACHE_TTL"); ttlStr != "" {
		if val, err := strconv.Atoi(ttlStr); err == nil && val > 0 {
			cacheTTLSeconds = val
		}
	}

	// XDG Cache Home fallback: "${XDG_CACHE_HOME:-$HOME/.cache}/reprox"
	cacheDir := os.Getenv("CACHE_DIR")
	if cacheDir == "" {
		xdgCache := os.Getenv("XDG_CACHE_HOME")
		if xdgCache == "" {
			home := os.Getenv("HOME")
			xdgCache = filepath.Join(home, ".cache")
		}
		cacheDir = filepath.Join(xdgCache, "reprox")
	}

	return &Config{
		Port:          port,
		Host:          host,
		GpgPrivateKey: os.Getenv("GPG_PRIVATE_KEY"),
		GpgPassphrase: os.Getenv("GPG_PASSPHRASE"),
		GpgPublicKey:  os.Getenv("GPG_PUBLIC_KEY"),
		GitHubToken:   os.Getenv("GITHUB_TOKEN"),
		CacheTTL:      time.Duration(cacheTTLSeconds) * time.Second,
		CacheDir:      cacheDir,
	}
}
