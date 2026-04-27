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
