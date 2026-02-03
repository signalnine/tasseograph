// internal/collector/db.go
package collector

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/signalnine/tasseograph/internal/protocol"
	_ "modernc.org/sqlite"
)

// DB wraps SQLite connection
type DB struct {
	db *sql.DB
}

// NewDB opens or creates the SQLite database
func NewDB(path string) (*DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	// Enable WAL mode for better concurrent access
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, err
	}

	// Create schema
	schema := `
	CREATE TABLE IF NOT EXISTS results (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp TEXT NOT NULL,
		hostname TEXT NOT NULL,
		status TEXT NOT NULL,
		issues TEXT,
		raw_dmesg TEXT,
		api_latency_ms INTEGER,
		created_at TEXT DEFAULT (datetime('now'))
	);
	CREATE INDEX IF NOT EXISTS idx_results_hostname ON results(hostname);
	CREATE INDEX IF NOT EXISTS idx_results_status ON results(status);
	CREATE INDEX IF NOT EXISTS idx_results_timestamp ON results(timestamp);
	`

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}

	return &DB{db: db}, nil
}

// Close closes the database connection
func (d *DB) Close() error {
	return d.db.Close()
}

// InsertResult stores an analysis result
func (d *DB) InsertResult(r *protocol.StoredResult) error {
	issuesJSON, err := json.Marshal(r.Issues)
	if err != nil {
		return err
	}

	_, err = d.db.Exec(`
		INSERT INTO results (timestamp, hostname, status, issues, raw_dmesg, api_latency_ms)
		VALUES (?, ?, ?, ?, ?, ?)
	`, r.Timestamp.Format(time.RFC3339), r.Hostname, r.Status, string(issuesJSON), r.RawDmesg, r.APILatencyMs)

	return err
}

// QueryByHostname returns recent results for a host
func (d *DB) QueryByHostname(hostname string, limit int) ([]protocol.StoredResult, error) {
	rows, err := d.db.Query(`
		SELECT id, timestamp, hostname, status, issues, raw_dmesg, api_latency_ms, created_at
		FROM results
		WHERE hostname = ?
		ORDER BY timestamp DESC
		LIMIT ?
	`, hostname, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanResults(rows)
}

// QueryNonOK returns recent non-ok results
func (d *DB) QueryNonOK(limit int) ([]protocol.StoredResult, error) {
	rows, err := d.db.Query(`
		SELECT id, timestamp, hostname, status, issues, raw_dmesg, api_latency_ms, created_at
		FROM results
		WHERE status != 'ok'
		ORDER BY timestamp DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanResults(rows)
}

// StatusCounts returns count of results by status
func (d *DB) StatusCounts() (map[string]int, error) {
	rows, err := d.db.Query(`
		SELECT status, COUNT(*) FROM results GROUP BY status
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		counts[status] = count
	}
	return counts, rows.Err()
}

func scanResults(rows *sql.Rows) ([]protocol.StoredResult, error) {
	var results []protocol.StoredResult
	for rows.Next() {
		var r protocol.StoredResult
		var tsStr, createdStr string
		var issuesJSON sql.NullString
		var rawDmesg sql.NullString
		var latency sql.NullInt64

		err := rows.Scan(&r.ID, &tsStr, &r.Hostname, &r.Status, &issuesJSON, &rawDmesg, &latency, &createdStr)
		if err != nil {
			return nil, err
		}

		r.Timestamp, _ = time.Parse(time.RFC3339, tsStr)
		r.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdStr)
		if issuesJSON.Valid {
			json.Unmarshal([]byte(issuesJSON.String), &r.Issues)
		}
		if rawDmesg.Valid {
			r.RawDmesg = rawDmesg.String
		}
		if latency.Valid {
			r.APILatencyMs = latency.Int64
		}

		results = append(results, r)
	}
	return results, rows.Err()
}
