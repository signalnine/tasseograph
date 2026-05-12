// internal/collector/db_test.go
package collector

import (
	"bytes"
	"log"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/signalnine/tasseograph/internal/protocol"
)

func TestDBInsertAndQuery(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB error: %v", err)
	}
	defer db.Close()

	// Insert a result
	result := &protocol.StoredResult{
		Timestamp: time.Date(2026, 2, 3, 12, 30, 0, 0, time.UTC),
		Hostname:  "test-host",
		Status:    "warning",
		Issues: []protocol.Issue{
			{
				Summary:  "ECC error",
				Evidence: "EDAC MC0: 1 CE",
			},
		},
		RawDmesg:     "[Mon Feb 3 12:30:00 2026] EDAC MC0: 1 CE",
		APILatencyMs: 250,
	}

	if err := db.InsertResult(result); err != nil {
		t.Fatalf("InsertResult error: %v", err)
	}

	// Query by hostname
	results, err := db.QueryByHostname("test-host", 10)
	if err != nil {
		t.Fatalf("QueryByHostname error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("QueryByHostname returned %d results, want 1", len(results))
	}
	if results[0].Status != "warning" {
		t.Errorf("Status = %q, want %q", results[0].Status, "warning")
	}

	// Query non-ok statuses
	results, err = db.QueryNonOK(10)
	if err != nil {
		t.Fatalf("QueryNonOK error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("QueryNonOK returned %d results, want 1", len(results))
	}
}

func TestDBStatusCounts(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB error: %v", err)
	}
	defer db.Close()

	// Insert mixed results
	for _, status := range []string{"ok", "ok", "ok", "warning", "critical"} {
		db.InsertResult(&protocol.StoredResult{
			Timestamp: time.Now(),
			Hostname:  "test-host",
			Status:    status,
		})
	}

	counts, err := db.StatusCounts()
	if err != nil {
		t.Fatalf("StatusCounts error: %v", err)
	}

	if counts["ok"] != 3 {
		t.Errorf("ok count = %d, want 3", counts["ok"])
	}
	if counts["warning"] != 1 {
		t.Errorf("warning count = %d, want 1", counts["warning"])
	}
}

func TestPruneOlderThan(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB error: %v", err)
	}
	defer db.Close()

	// Two rows: one with a created_at well outside the retention window,
	// one with the default created_at (now). Direct INSERT lets us set
	// created_at without waiting real wall-clock time.
	_, err = db.db.Exec(`
		INSERT INTO results (timestamp, hostname, status, issues, raw_dmesg, api_latency_ms, created_at)
		VALUES (?, ?, ?, ?, ?, ?, datetime('now', '-40 days'))
	`, time.Now().Format(time.RFC3339), "old-host", "ok", "[]", "", 0)
	if err != nil {
		t.Fatalf("seed old row error: %v", err)
	}
	if err := db.InsertResult(&protocol.StoredResult{Timestamp: time.Now(), Hostname: "fresh-host", Status: "ok"}); err != nil {
		t.Fatalf("InsertResult: %v", err)
	}

	pruned, err := db.PruneOlderThan(30)
	if err != nil {
		t.Fatalf("PruneOlderThan error: %v", err)
	}
	if pruned != 1 {
		t.Errorf("pruned = %d, want 1", pruned)
	}

	if got, _ := db.QueryByHostname("old-host", 10); len(got) != 0 {
		t.Errorf("old-host rows = %d, want 0 after prune", len(got))
	}
	if got, _ := db.QueryByHostname("fresh-host", 10); len(got) != 1 {
		t.Errorf("fresh-host rows = %d, want 1 (unchanged)", len(got))
	}

	// Disabled: 0 days = no-op even if there is old data.
	_, err = db.db.Exec(`
		INSERT INTO results (timestamp, hostname, status, issues, raw_dmesg, api_latency_ms, created_at)
		VALUES (?, ?, ?, ?, ?, ?, datetime('now', '-100 days'))
	`, time.Now().Format(time.RFC3339), "ancient-host", "ok", "[]", "", 0)
	if err != nil {
		t.Fatalf("seed ancient row error: %v", err)
	}
	pruned, err = db.PruneOlderThan(0)
	if err != nil {
		t.Fatalf("PruneOlderThan(0) error: %v", err)
	}
	if pruned != 0 {
		t.Errorf("pruned with retention=0 = %d, want 0", pruned)
	}
	if got, _ := db.QueryByHostname("ancient-host", 10); len(got) != 1 {
		t.Errorf("ancient-host rows after disabled prune = %d, want 1", len(got))
	}
}

// TestNewDBMigratesProviderModelColumns simulates an installation that
// predates the provider/model schema additions and verifies NewDB upgrades
// the table in place rather than failing or creating duplicates.
func TestNewDBMigratesProviderModelColumns(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "old.db")

	// Build the pre-migration schema by hand, then close.
	{
		db, err := NewDB(dbPath)
		if err != nil {
			t.Fatalf("NewDB: %v", err)
		}
		if _, err := db.db.Exec(`ALTER TABLE results DROP COLUMN provider`); err != nil {
			t.Fatalf("simulate-old: drop provider: %v", err)
		}
		if _, err := db.db.Exec(`ALTER TABLE results DROP COLUMN model`); err != nil {
			t.Fatalf("simulate-old: drop model: %v", err)
		}
		db.Close()
	}

	// Re-open via the public constructor. Migration must add the columns
	// back without erroring, and inserts must work end-to-end.
	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB after migration: %v", err)
	}
	defer db.Close()

	err = db.InsertResult(&protocol.StoredResult{
		Timestamp: time.Now(),
		Hostname:  "host1",
		Status:    "ok",
		Provider:  "Google",
		Model:     "google/gemini-3-flash-preview-20251217",
	})
	if err != nil {
		t.Fatalf("InsertResult after migration: %v", err)
	}

	got, err := db.QueryByHostname("host1", 1)
	if err != nil {
		t.Fatalf("QueryByHostname: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("rows = %d, want 1", len(got))
	}
	if got[0].Provider != "Google" {
		t.Errorf("Provider = %q, want %q", got[0].Provider, "Google")
	}
	if got[0].Model != "google/gemini-3-flash-preview-20251217" {
		t.Errorf("Model = %q, want resolved model id", got[0].Model)
	}
}

func TestScanResultsLogsUnmarshalError(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB error: %v", err)
	}
	defer db.Close()

	_, err = db.db.Exec(`
		INSERT INTO results (timestamp, hostname, status, issues, raw_dmesg, api_latency_ms)
		VALUES (?, ?, ?, ?, ?, ?)
	`, time.Now().Format(time.RFC3339), "corrupt-host", "warning", "{not valid json", "", 0)
	if err != nil {
		t.Fatalf("direct insert error: %v", err)
	}

	var logBuf bytes.Buffer
	prevOutput := log.Writer()
	log.SetOutput(&logBuf)
	defer log.SetOutput(prevOutput)

	results, err := db.QueryByHostname("corrupt-host", 10)
	if err != nil {
		t.Fatalf("QueryByHostname error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	logOutput := logBuf.String()
	if !strings.Contains(strings.ToLower(logOutput), "unmarshal") {
		t.Errorf("expected log to mention unmarshal error, got: %q", logOutput)
	}
	if !strings.Contains(logOutput, "issues") {
		t.Errorf("expected log to mention issues column, got: %q", logOutput)
	}
}
