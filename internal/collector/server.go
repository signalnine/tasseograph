// internal/collector/server.go
package collector

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/signalnine/tasseograph/internal/config"
)

// Server is the central collector
type Server struct {
	cfg    *config.CollectorConfig
	db     *DB
	llm    *LLMClient
	server *http.Server
}

// NewServer creates a new collector server
func NewServer(cfg *config.CollectorConfig) (*Server, error) {
	db, err := NewDB(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Convert config endpoints to LLM client endpoints
	var endpoints []Endpoint
	for _, ep := range cfg.LLMEndpoints {
		endpoints = append(endpoints, Endpoint{
			URL:    ep.URL,
			Model:  ep.Model,
			APIKey: ep.APIKey,
		})
	}
	llm := NewLLMClient(endpoints)

	handler := NewIngestHandler(db, llm, cfg.APIKey, cfg.MaxPayloadBytes)

	mux := http.NewServeMux()
	mux.Handle("/ingest", handler)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	server := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	return &Server{
		cfg:    cfg,
		db:     db,
		llm:    llm,
		server: server,
	}, nil
}

// Run starts the HTTPS server
func (s *Server) Run(ctx context.Context) error {
	log.Printf("Collector starting on %s", s.cfg.ListenAddr)

	// Load TLS cert
	cert, err := tls.LoadX509KeyPair(s.cfg.TLSCert, s.cfg.TLSKey)
	if err != nil {
		return fmt.Errorf("load TLS cert: %w", err)
	}

	s.server.TLSConfig = &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	// Start server in goroutine
	errCh := make(chan error, 1)
	go func() {
		if err := s.server.ListenAndServeTLS("", ""); err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	// Wait for context cancellation or error
	select {
	case <-ctx.Done():
		log.Println("Collector shutting down...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		s.server.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}

	s.db.Close()
	return nil
}

// RunAndGetAddr starts the HTTPS server and returns the actual address.
// This is useful for tests that use port 0 for auto-assignment.
func (s *Server) RunAndGetAddr(ctx context.Context) (string, error) {
	// Load TLS cert
	cert, err := tls.LoadX509KeyPair(s.cfg.TLSCert, s.cfg.TLSKey)
	if err != nil {
		return "", fmt.Errorf("load TLS cert: %w", err)
	}

	s.server.TLSConfig = &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	// Create listener to get actual address
	ln, err := net.Listen("tcp", s.cfg.ListenAddr)
	if err != nil {
		return "", fmt.Errorf("listen: %w", err)
	}

	addr := ln.Addr().String()
	log.Printf("Collector starting on %s", addr)

	// Start server in goroutine
	go func() {
		tlsLn := tls.NewListener(ln, s.server.TLSConfig)
		if err := s.server.Serve(tlsLn); err != http.ErrServerClosed {
			log.Printf("Server error: %v", err)
		}
	}()

	// Wait for context cancellation in background
	go func() {
		<-ctx.Done()
		log.Println("Collector shutting down...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		s.server.Shutdown(shutdownCtx)
		s.db.Close()
	}()

	return addr, nil
}
