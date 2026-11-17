package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Megatherium/gore/internal/config"
	"github.com/Megatherium/gore/internal/github"
	"github.com/Megatherium/gore/internal/service"
	"github.com/Megatherium/gore/internal/sign"
	"github.com/Megatherium/gore/internal/store"
	"github.com/Megatherium/gore/internal/web"
)

func main() {
	cfg := config.Load()

	log.Printf("Starting gore server on %s:%s (CacheDir=%s, CacheTTL=%s)",
		cfg.Host, cfg.Port, cfg.CacheDir, cfg.CacheTTL)

	// Initialize GitHub Client
	ghClient := github.NewClient(cfg.GitHubToken, nil)

	// Initialize Store with local disk blob backing
	memStore, err := store.NewMemoryStore(cfg.CacheDir)
	if err != nil {
		log.Fatalf("Failed to initialize store: %v", err)
	}
	defer memStore.Close()
	memStore.SetRestoreTTL(cfg.CacheTTL)

	// Initialize GPG Signer if configured
	var signer *sign.Signer
	if cfg.GpgPrivateKey != "" {
		s, err := sign.NewSigner(cfg.GpgPrivateKey, cfg.GpgPassphrase)
		if err != nil {
			log.Fatalf("Failed to initialize GPG signer: %v", err)
		}
		if cfg.GpgPublicKey != "" {
			if err := s.SetPublicKeyOverride(cfg.GpgPublicKey); err != nil {
				log.Fatalf("Failed to configure GPG_PUBLIC_KEY override: %v", err)
			}
		}
		signer = s
		log.Printf("GPG signing enabled (Fingerprint: %s)", signer.Fingerprint())
	} else {
		log.Println("GPG signing is NOT enabled (no GPG_PRIVATE_KEY provided)")
	}

	// Initialize Web and Service Handlers
	webHandler := web.NewHandler(ghClient, memStore, signer)
	svc := service.NewService(service.ServiceConfig{
		GitHubClient: ghClient,
		Store:        memStore,
		Signer:       signer,
		CacheTTL:     cfg.CacheTTL,
		WebHandler:   webHandler,
	})

	server := &http.Server{
		Addr:         net.JoinHostPort(cfg.Host, cfg.Port),
		Handler:      svc,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown channel
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	log.Printf("gore listening on http://%s:%s", cfg.Host, cfg.Port)

	<-shutdown
	log.Println("Shutting down gore...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("gore stopped gracefully.")
}
