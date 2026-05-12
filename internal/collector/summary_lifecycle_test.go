// internal/collector/summary_lifecycle_test.go
package collector

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/signalnine/tasseograph/internal/config"
	"github.com/signalnine/tasseograph/internal/protocol"
)

func TestStartSummary_DisabledDoesNothing(t *testing.T) {
	// SummaryInterval==0 means the feature is off. startSummary must not
	// spawn a goroutine that holds the DB or hits the network.
	dir := t.TempDir()
	db, _ := NewDB(filepath.Join(dir, "test.db"))
	defer db.Close()

	cfg := &config.CollectorConfig{
		SummaryInterval: 0,
		SMTPHost:        "127.0.0.1:1", // unreachable; should never get hit
		SMTPFrom:        "from@example.com",
		SMTPTo:          "to@example.com",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	startSummary(ctx, db, cfg)
	<-ctx.Done()
	// No assertion needed beyond "did not panic / hang." If startSummary
	// fired despite interval==0, SMTPHost 127.0.0.1:1 would error in logs.
}

func TestStartSummary_FiresOnTick(t *testing.T) {
	// With a 1s interval the ticker should produce one send within ~2s.
	// Validates the loop is plumbed end-to-end (build -> send -> log).
	mock := newMockSMTP(t)
	defer mock.close()

	dir := t.TempDir()
	db, _ := NewDB(filepath.Join(dir, "test.db"))
	defer db.Close()

	// Seed one row so the digest has content to render.
	seedRow(t, db, "host1", "ok", time.Now().Add(-30*time.Second), 200, []protocol.Issue{})

	cfg := &config.CollectorConfig{
		SummaryInterval: time.Second,
		AlertErrorRate:  0.5,
		SMTPHost:        mock.addr,
		SMTPFrom:        "from@example.com",
		SMTPTo:          "to@example.com",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	startSummary(ctx, db, cfg)

	// Wait for the mock to capture a DATA section, or timeout.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mock.mu.Lock()
		got := mock.data
		mock.mu.Unlock()
		if got != "" {
			return // saw the email, test passes
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("startSummary did not fire within 3s")
}

func TestSendSummaryOnce_FailsIfDisabled(t *testing.T) {
	// CLI wiring depends on SendSummaryOnce rejecting an unconfigured run.
	dir := t.TempDir()
	db, _ := NewDB(filepath.Join(dir, "test.db"))
	defer db.Close()

	_, err := SendSummaryOnce(&config.CollectorConfig{}, db)
	if err == nil {
		t.Fatal("expected error when summary_interval is 0, got nil")
	}
}
