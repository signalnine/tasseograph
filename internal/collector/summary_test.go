// internal/collector/summary_test.go
package collector

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/signalnine/tasseograph/internal/protocol"
)

// seedRow inserts a row with an explicit created_at so summary tests can
// place data inside or outside the digest window without sleeping.
func seedRow(t *testing.T, db *DB, hostname, status string, createdAt time.Time, latencyMs int64, issues []protocol.Issue) {
	t.Helper()
	issuesJSON := "[]"
	if len(issues) > 0 {
		buf, err := json.Marshal(issues)
		if err != nil {
			t.Fatalf("marshal issues: %v", err)
		}
		issuesJSON = string(buf)
	}
	_, err := db.db.Exec(
		`INSERT INTO results (timestamp, hostname, status, issues, raw_dmesg, api_latency_ms, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		createdAt.UTC().Format(time.RFC3339),
		hostname, status, issuesJSON, "", latencyMs,
		createdAt.UTC().Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		t.Fatalf("seed row: %v", err)
	}
}

func TestBuildSummary_AllOK(t *testing.T) {
	dir := t.TempDir()
	db, _ := NewDB(filepath.Join(dir, "test.db"))
	defer db.Close()

	now := time.Date(2026, 5, 12, 22, 0, 0, 0, time.UTC)
	since := now.Add(-24 * time.Hour)

	for i := 0; i < 6; i++ {
		seedRow(t, db, "host1", "ok", since.Add(time.Hour*time.Duration(i)), 200, nil)
	}

	subj, body, err := BuildSummary(db, since, now, 0.5)
	if err != nil {
		t.Fatalf("BuildSummary: %v", err)
	}
	if !strings.HasPrefix(subj, "[OK]") {
		t.Errorf("subject = %q, want [OK] prefix", subj)
	}
	if !strings.Contains(body, "host1") {
		t.Errorf("body must mention host1\n%s", body)
	}
	if !strings.Contains(body, "ok") {
		t.Errorf("body must mention ok status\n%s", body)
	}
}

func TestBuildSummary_WithWarnings(t *testing.T) {
	dir := t.TempDir()
	db, _ := NewDB(filepath.Join(dir, "test.db"))
	defer db.Close()

	now := time.Date(2026, 5, 12, 22, 0, 0, 0, time.UTC)
	since := now.Add(-24 * time.Hour)

	seedRow(t, db, "host1", "ok", since.Add(time.Hour), 200, nil)
	seedRow(t, db, "host1", "warning", since.Add(2*time.Hour), 300, []protocol.Issue{
		{Summary: "EDAC correctable error", Evidence: "EDAC MC0: 1 CE"},
	})
	seedRow(t, db, "host2", "warning", since.Add(3*time.Hour), 250, []protocol.Issue{
		{Summary: "EDAC correctable error", Evidence: "EDAC MC0: 1 CE"},
	})

	subj, body, err := BuildSummary(db, since, now, 0.5)
	if err != nil {
		t.Fatalf("BuildSummary: %v", err)
	}
	if !strings.HasPrefix(subj, "[WARN]") {
		t.Errorf("subject = %q, want [WARN] prefix", subj)
	}
	if !strings.Contains(body, "EDAC correctable error") {
		t.Errorf("body must include the warning summary text\n%s", body)
	}
	// Issue appears twice across two hosts -- the dedup view should show count >= 2.
	if !strings.Contains(body, "2 ") && !strings.Contains(body, "(2)") {
		t.Errorf("body should aggregate duplicate issue counts\n%s", body)
	}
}

func TestBuildSummary_WithCritical(t *testing.T) {
	dir := t.TempDir()
	db, _ := NewDB(filepath.Join(dir, "test.db"))
	defer db.Close()

	now := time.Date(2026, 5, 12, 22, 0, 0, 0, time.UTC)
	since := now.Add(-24 * time.Hour)

	// Mostly ok, one critical -- subject must escalate to [CRITICAL].
	for i := 0; i < 5; i++ {
		seedRow(t, db, "host1", "ok", since.Add(time.Hour*time.Duration(i)), 200, nil)
	}
	seedRow(t, db, "host1", "critical", since.Add(6*time.Hour), 400, []protocol.Issue{
		{Summary: "OOM kill of large Python process", Evidence: "Out of memory: Killed process 1690274"},
	})

	subj, body, err := BuildSummary(db, since, now, 0.5)
	if err != nil {
		t.Fatalf("BuildSummary: %v", err)
	}
	if !strings.HasPrefix(subj, "[CRITICAL]") {
		t.Errorf("subject = %q, want [CRITICAL] prefix", subj)
	}
	// Critical evidence must appear verbatim so the on-call gets context without
	// having to query the DB.
	if !strings.Contains(body, "Killed process 1690274") {
		t.Errorf("body must include critical evidence\n%s", body)
	}
}

func TestBuildSummary_PipelineErrorsTriggerCritical(t *testing.T) {
	// This is the regression we're protecting against: 80 days of pure-error
	// rows must produce a CRITICAL digest, not a quiet OK.
	dir := t.TempDir()
	db, _ := NewDB(filepath.Join(dir, "test.db"))
	defer db.Close()

	now := time.Date(2026, 5, 12, 22, 0, 0, 0, time.UTC)
	since := now.Add(-24 * time.Hour)

	for i := 0; i < 20; i++ {
		seedRow(t, db, "host1", "error", since.Add(time.Hour*time.Duration(i%24)), 500, nil)
	}

	subj, body, err := BuildSummary(db, since, now, 0.5)
	if err != nil {
		t.Fatalf("BuildSummary: %v", err)
	}
	if !strings.HasPrefix(subj, "[CRITICAL]") {
		t.Errorf("subject = %q, want [CRITICAL] for high error rate", subj)
	}
	if !strings.Contains(strings.ToLower(body), "error rate") {
		t.Errorf("body should explain the error rate\n%s", body)
	}
}

func TestBuildSummary_SilentPipelineIsCritical(t *testing.T) {
	// Zero rows in the window means no agents are reporting -- worse than
	// errors. Must surface, not silently look like a healthy [OK].
	dir := t.TempDir()
	db, _ := NewDB(filepath.Join(dir, "test.db"))
	defer db.Close()

	now := time.Date(2026, 5, 12, 22, 0, 0, 0, time.UTC)
	since := now.Add(-24 * time.Hour)

	subj, body, err := BuildSummary(db, since, now, 0.5)
	if err != nil {
		t.Fatalf("BuildSummary: %v", err)
	}
	if !strings.HasPrefix(subj, "[CRITICAL]") {
		t.Errorf("subject = %q, want [CRITICAL] for silent pipeline", subj)
	}
	if !strings.Contains(strings.ToLower(body), "no") {
		t.Errorf("body should explain that no data was ingested\n%s", body)
	}
}

func TestBuildSummary_RespectsWindow(t *testing.T) {
	// Rows older than `since` must not influence the digest. Old criticals
	// shouldn't keep paging on-call forever.
	dir := t.TempDir()
	db, _ := NewDB(filepath.Join(dir, "test.db"))
	defer db.Close()

	now := time.Date(2026, 5, 12, 22, 0, 0, 0, time.UTC)
	since := now.Add(-24 * time.Hour)

	// Old critical, well outside the window.
	seedRow(t, db, "host1", "critical", since.Add(-72*time.Hour), 400, []protocol.Issue{
		{Summary: "OLD critical", Evidence: "ancient"},
	})
	// Recent ok rows inside the window.
	for i := 0; i < 5; i++ {
		seedRow(t, db, "host1", "ok", since.Add(time.Hour*time.Duration(i)), 200, nil)
	}

	subj, body, err := BuildSummary(db, since, now, 0.5)
	if err != nil {
		t.Fatalf("BuildSummary: %v", err)
	}
	if !strings.HasPrefix(subj, "[OK]") {
		t.Errorf("subject = %q, want [OK] (old critical is out of window)", subj)
	}
	if strings.Contains(body, "OLD critical") {
		t.Errorf("body must not include rows outside the window\n%s", body)
	}
}
