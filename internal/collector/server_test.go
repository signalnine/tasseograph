// internal/collector/server_test.go
package collector

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/signalnine/tasseograph/internal/config"
)

func TestServerRunClosesDBOnTLSError(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.CollectorConfig{
		ListenAddr:      "127.0.0.1:0",
		DBPath:          filepath.Join(dir, "test.db"),
		MaxPayloadBytes: 1 << 20,
		TLSCert:         filepath.Join(dir, "nonexistent.pem"),
		TLSKey:          filepath.Join(dir, "nonexistent.key"),
		APIKey:          "test-key",
	}

	s, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer error: %v", err)
	}

	err = s.Run(context.Background())
	if err == nil {
		t.Fatal("expected Run to return error for missing TLS cert")
	}

	// DB must be closed after Run returns. Calling a method that requires
	// the open handle should now fail with sql: database is closed.
	if pingErr := s.db.db.Ping(); pingErr == nil {
		t.Error("expected db.Ping() to fail because DB should be closed after Run returns; got nil")
	}
}
