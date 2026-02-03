# Phase 1 Pilot Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a working dmesg anomaly detection pilot with agent and collector components.

**Architecture:** Host agents push dmesg deltas over HTTPS to a central collector, which sends them to an LLM inference endpoint and stores results in SQLite.

**Tech Stack:** Go 1.21+, SQLite (modernc.org/sqlite), YAML config (gopkg.in/yaml.v3), cobra for CLI

---

## Task 1: Initialize Go Module and Project Structure

**Files:**
- Create: `go.mod`
- Create: `cmd/tasseograph/main.go`
- Create: `internal/protocol/types.go`

**Step 1: Initialize Go module**

Run:
```bash
go mod init github.com/signalnine/tasseograph
```

**Step 2: Create directory structure**

Run:
```bash
mkdir -p cmd/tasseograph internal/{agent,collector,config,protocol}
```

**Step 3: Create main.go with cobra CLI skeleton**

```go
// cmd/tasseograph/main.go
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "tasseograph",
	Short: "dmesg anomaly detection via LLM",
}

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Run the host agent",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("agent not implemented")
	},
}

var collectorCmd = &cobra.Command{
	Use:   "collector",
	Short: "Run the central collector",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("collector not implemented")
	},
}

func init() {
	rootCmd.AddCommand(agentCmd)
	rootCmd.AddCommand(collectorCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

**Step 4: Create shared protocol types**

```go
// internal/protocol/types.go
package protocol

import "time"

// DmesgDelta is sent from agent to collector
type DmesgDelta struct {
	Hostname  string    `json:"hostname"`
	Timestamp time.Time `json:"timestamp"`
	Lines     []string  `json:"lines"`
}

// Issue represents a single detected anomaly
type Issue struct {
	Severity string `json:"severity"` // "warning" or "critical"
	Category string `json:"category"` // "memory", "storage", "network", "thermal", "driver"
	Summary  string `json:"summary"`
	Evidence string `json:"evidence"`
}

// AnalysisResult is the LLM response
type AnalysisResult struct {
	Status string  `json:"status"` // "ok", "warning", "critical"
	Issues []Issue `json:"issues"`
}

// StoredResult is what we persist to SQLite
type StoredResult struct {
	ID           int64     `json:"id"`
	Timestamp    time.Time `json:"timestamp"`
	Hostname     string    `json:"hostname"`
	Status       string    `json:"status"`
	Issues       []Issue   `json:"issues"`
	RawDmesg     string    `json:"raw_dmesg"`
	APILatencyMs int64     `json:"api_latency_ms"`
	CreatedAt    time.Time `json:"created_at"`
}
```

**Step 5: Add dependencies**

Run:
```bash
go get github.com/spf13/cobra
go mod tidy
```

**Step 6: Verify it builds and runs**

Run:
```bash
go build -o dist/tasseograph ./cmd/tasseograph
./dist/tasseograph --help
./dist/tasseograph agent
./dist/tasseograph collector
```

Expected: Help output shows agent and collector subcommands; each prints "not implemented"

**Step 7: Commit**

```bash
git add -A
git commit -m "feat: initialize Go module with CLI skeleton and protocol types"
```

---

## Task 2: Configuration Loading

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

**Step 1: Write failing test for config loading**

```go
// internal/config/config_test.go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAgentConfig(t *testing.T) {
	// Create temp config file
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.yaml")
	content := []byte(`
collector_url: "https://collector.internal:9311/ingest"
poll_interval: 5m
state_file: /var/lib/tasseograph/last_timestamp
hostname: "test-host"
tls_skip_verify: true
`)
	if err := os.WriteFile(configPath, content, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadAgentConfig(configPath)
	if err != nil {
		t.Fatalf("LoadAgentConfig failed: %v", err)
	}

	if cfg.CollectorURL != "https://collector.internal:9311/ingest" {
		t.Errorf("CollectorURL = %q, want %q", cfg.CollectorURL, "https://collector.internal:9311/ingest")
	}
	if cfg.PollInterval.String() != "5m0s" {
		t.Errorf("PollInterval = %v, want 5m0s", cfg.PollInterval)
	}
	if cfg.Hostname != "test-host" {
		t.Errorf("Hostname = %q, want %q", cfg.Hostname, "test-host")
	}
}

func TestLoadAgentConfigEnvOverride(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.yaml")
	content := []byte(`
collector_url: "https://collector.internal:9311/ingest"
poll_interval: 5m
state_file: /var/lib/tasseograph/last_timestamp
`)
	if err := os.WriteFile(configPath, content, 0644); err != nil {
		t.Fatal(err)
	}

	// Set env override
	t.Setenv("TASSEOGRAPH_API_KEY", "test-secret")

	cfg, err := LoadAgentConfig(configPath)
	if err != nil {
		t.Fatalf("LoadAgentConfig failed: %v", err)
	}

	if cfg.APIKey != "test-secret" {
		t.Errorf("APIKey = %q, want %q", cfg.APIKey, "test-secret")
	}
}

func TestLoadCollectorConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "collector.yaml")
	content := []byte(`
listen_addr: ":9311"
db_path: /var/lib/tasseograph/results.db
max_retries: 3
max_payload_bytes: 1048576
tls_cert: /etc/tasseograph/tls/cert.pem
tls_key: /etc/tasseograph/tls/key.pem
llm_endpoints:
  - url: "https://inference.internal/v1"
    model: "anthropic/haiku-4.5"
    api_key_env: "INTERNAL_LLM_KEY"
  - url: "https://api.openai.com/v1"
    model: "gpt-4o-mini"
    api_key_env: "OPENAI_API_KEY"
`)
	if err := os.WriteFile(configPath, content, 0644); err != nil {
		t.Fatal(err)
	}

	// Set env var for API key resolution
	t.Setenv("INTERNAL_LLM_KEY", "internal-secret")
	t.Setenv("OPENAI_API_KEY", "openai-secret")

	cfg, err := LoadCollectorConfig(configPath)
	if err != nil {
		t.Fatalf("LoadCollectorConfig failed: %v", err)
	}

	if cfg.ListenAddr != ":9311" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, ":9311")
	}
	if cfg.MaxPayloadBytes != 1048576 {
		t.Errorf("MaxPayloadBytes = %d, want %d", cfg.MaxPayloadBytes, 1048576)
	}
	if len(cfg.LLMEndpoints) != 2 {
		t.Fatalf("LLMEndpoints count = %d, want 2", len(cfg.LLMEndpoints))
	}
	if cfg.LLMEndpoints[0].URL != "https://inference.internal/v1" {
		t.Errorf("Endpoint[0].URL = %q, want %q", cfg.LLMEndpoints[0].URL, "https://inference.internal/v1")
	}
	if cfg.LLMEndpoints[0].APIKey != "internal-secret" {
		t.Errorf("Endpoint[0].APIKey = %q, want %q", cfg.LLMEndpoints[0].APIKey, "internal-secret")
	}
	if cfg.LLMEndpoints[1].APIKey != "openai-secret" {
		t.Errorf("Endpoint[1].APIKey = %q, want %q", cfg.LLMEndpoints[1].APIKey, "openai-secret")
	}
}
```

**Step 2: Run test to verify it fails**

Run:
```bash
go test ./internal/config/... -v
```

Expected: FAIL (LoadAgentConfig undefined)

**Step 3: Implement config loading**

```go
// internal/config/config.go
package config

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// AgentConfig for the host agent
type AgentConfig struct {
	CollectorURL  string        `yaml:"collector_url"`
	PollInterval  time.Duration `yaml:"poll_interval"`
	StateFile     string        `yaml:"state_file"`
	Hostname      string        `yaml:"hostname"`
	TLSSkipVerify bool          `yaml:"tls_skip_verify"`
	APIKey        string        `yaml:"-"` // from env only
}

// LLMEndpoint represents one LLM provider in the fallback chain
type LLMEndpoint struct {
	URL       string `yaml:"url"`
	Model     string `yaml:"model"`
	APIKeyEnv string `yaml:"api_key_env"` // env var name for API key
	APIKey    string `yaml:"-"`           // resolved at load time
}

// CollectorConfig for the central collector
type CollectorConfig struct {
	ListenAddr      string        `yaml:"listen_addr"`
	DBPath          string        `yaml:"db_path"`
	MaxRetries      int           `yaml:"max_retries"`
	MaxPayloadBytes int64         `yaml:"max_payload_bytes"`
	TLSCert         string        `yaml:"tls_cert"`
	TLSKey          string        `yaml:"tls_key"`
	LLMEndpoints    []LLMEndpoint `yaml:"llm_endpoints"` // fallback chain
	APIKey          string        `yaml:"-"`             // agent auth, from env
}

// LoadAgentConfig loads agent config from YAML file with env overrides
func LoadAgentConfig(path string) (*AgentConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg AgentConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	// Env overrides
	if key := os.Getenv("TASSEOGRAPH_API_KEY"); key != "" {
		cfg.APIKey = key
	}
	if hostname := os.Getenv("TASSEOGRAPH_HOSTNAME"); hostname != "" {
		cfg.Hostname = hostname
	}

	// Default hostname to os.Hostname if not set
	if cfg.Hostname == "" {
		cfg.Hostname, _ = os.Hostname()
	}

	return &cfg, nil
}

// LoadCollectorConfig loads collector config from YAML file with env overrides
func LoadCollectorConfig(path string) (*CollectorConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg CollectorConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	// Env overrides
	if key := os.Getenv("TASSEOGRAPH_API_KEY"); key != "" {
		cfg.APIKey = key
	}

	// Resolve API keys for each LLM endpoint from env vars
	for i := range cfg.LLMEndpoints {
		if cfg.LLMEndpoints[i].APIKeyEnv != "" {
			cfg.LLMEndpoints[i].APIKey = os.Getenv(cfg.LLMEndpoints[i].APIKeyEnv)
		}
	}

	return &cfg, nil
}
```

**Step 4: Add yaml dependency and run tests**

Run:
```bash
go get gopkg.in/yaml.v3
go test ./internal/config/... -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add -A
git commit -m "feat: add config loading with YAML and env overrides"
```

---

## Task 3: dmesg Parsing and State Management

**Files:**
- Create: `internal/agent/dmesg.go`
- Create: `internal/agent/dmesg_test.go`
- Create: `internal/agent/state.go`
- Create: `internal/agent/state_test.go`

**Step 1: Write failing test for dmesg timestamp parsing**

```go
// internal/agent/dmesg_test.go
package agent

import (
	"fmt"
	"testing"
	"time"
)

func TestParseDmesgTimestamp(t *testing.T) {
	tests := []struct {
		line    string
		wantErr bool
	}{
		{"[Mon Feb  3 12:25:01 2026] EXT4-fs warning: test", false},
		{"[Tue Jan 14 09:05:33 2026] kernel: test message", false},
		{"no timestamp here", true},
		{"", true},
	}

	for _, tt := range tests {
		ts, err := ParseDmesgTimestamp(tt.line)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseDmesgTimestamp(%q) expected error, got %v", tt.line, ts)
			}
		} else {
			if err != nil {
				t.Errorf("ParseDmesgTimestamp(%q) error: %v", tt.line, err)
			}
		}
	}
}

func TestFilterNewLines(t *testing.T) {
	lines := []string{
		"[Mon Feb  3 12:00:00 2026] old message",
		"[Mon Feb  3 12:05:00 2026] newer message",
		"[Mon Feb  3 12:10:00 2026] newest message",
	}

	// Parse the middle timestamp as "last seen"
	lastSeen, _ := time.Parse("Mon Jan _2 15:04:05 2006", "Mon Feb  3 12:05:00 2026")

	filtered, latestTs := FilterNewLines(lines, lastSeen)

	if len(filtered) != 1 {
		t.Errorf("FilterNewLines returned %d lines, want 1", len(filtered))
	}
	if len(filtered) > 0 && filtered[0] != "[Mon Feb  3 12:10:00 2026] newest message" {
		t.Errorf("FilterNewLines returned wrong line: %q", filtered[0])
	}

	expectedLatest, _ := time.Parse("Mon Jan _2 15:04:05 2006", "Mon Feb  3 12:10:00 2026")
	if !latestTs.Equal(expectedLatest) {
		t.Errorf("latestTs = %v, want %v", latestTs, expectedLatest)
	}
}

func TestCapLines(t *testing.T) {
	// Under limit - no truncation
	small := []string{"a", "b", "c"}
	result, truncated := CapLines(small)
	if truncated {
		t.Error("CapLines truncated when under limit")
	}
	if len(result) != 3 {
		t.Errorf("CapLines returned %d lines, want 3", len(result))
	}

	// Over limit - truncate to most recent
	big := make([]string, MaxLines+100)
	for i := range big {
		big[i] = fmt.Sprintf("line-%d", i)
	}
	result, truncated = CapLines(big)
	if !truncated {
		t.Error("CapLines did not truncate when over limit")
	}
	if len(result) != MaxLines {
		t.Errorf("CapLines returned %d lines, want %d", len(result), MaxLines)
	}
	// Should keep the last (most recent) lines
	if result[0] != "line-100" {
		t.Errorf("CapLines kept wrong lines, first = %q", result[0])
	}
}
```

**Step 2: Run test to verify it fails**

Run:
```bash
go test ./internal/agent/... -v
```

Expected: FAIL (ParseDmesgTimestamp undefined)

**Step 3: Implement dmesg parsing**

```go
// internal/agent/dmesg.go
package agent

import (
	"errors"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var timestampRe = regexp.MustCompile(`^\[([A-Za-z]{3} [A-Za-z]{3} [ \d]\d \d{2}:\d{2}:\d{2} \d{4})\]`)

// ParseDmesgTimestamp extracts the timestamp from a dmesg -T line
func ParseDmesgTimestamp(line string) (time.Time, error) {
	matches := timestampRe.FindStringSubmatch(line)
	if len(matches) < 2 {
		return time.Time{}, errors.New("no timestamp found")
	}

	// Parse: "Mon Feb  3 12:25:01 2026"
	return time.Parse("Mon Jan _2 15:04:05 2006", matches[1])
}

// FilterNewLines returns lines newer than lastSeen and the latest timestamp
func FilterNewLines(lines []string, lastSeen time.Time) ([]string, time.Time) {
	var filtered []string
	var latest time.Time

	for _, line := range lines {
		ts, err := ParseDmesgTimestamp(line)
		if err != nil {
			continue // skip lines without parseable timestamps
		}

		if ts.After(lastSeen) {
			filtered = append(filtered, line)
			if ts.After(latest) {
				latest = ts
			}
		}
	}

	return filtered, latest
}

// MaxLines caps how many lines we send to the LLM to control costs
const MaxLines = 500

// GetDmesg runs dmesg -T and returns the output lines
// Uses LC_ALL=C for consistent timestamp format across locales
func GetDmesg() ([]string, error) {
	cmd := exec.Command("dmesg", "-T")
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	return lines, nil
}

// CapLines returns at most MaxLines from the end of the slice (most recent)
// Returns true if lines were truncated
func CapLines(lines []string) ([]string, bool) {
	if len(lines) <= MaxLines {
		return lines, false
	}
	// Keep the most recent lines (end of slice)
	return lines[len(lines)-MaxLines:], true
}
```

**Step 4: Run dmesg tests**

Run:
```bash
go test ./internal/agent/... -v -run Dmesg
```

Expected: PASS

**Step 5: Write failing test for state management**

```go
// internal/agent/state_test.go
package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStateReadWrite(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "last_timestamp")

	// Initially should return zero time
	ts, err := ReadLastTimestamp(statePath)
	if err != nil {
		t.Fatalf("ReadLastTimestamp (missing file) error: %v", err)
	}
	if !ts.IsZero() {
		t.Errorf("expected zero time for missing file, got %v", ts)
	}

	// Write a timestamp
	now := time.Date(2026, 2, 3, 12, 30, 0, 0, time.UTC)
	if err := WriteLastTimestamp(statePath, now); err != nil {
		t.Fatalf("WriteLastTimestamp error: %v", err)
	}

	// Read it back
	ts, err = ReadLastTimestamp(statePath)
	if err != nil {
		t.Fatalf("ReadLastTimestamp error: %v", err)
	}
	if !ts.Equal(now) {
		t.Errorf("ReadLastTimestamp = %v, want %v", ts, now)
	}
}

func TestStateCorruptFile(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "last_timestamp")

	// Write garbage
	os.WriteFile(statePath, []byte("not a timestamp"), 0644)

	// Should return zero time (fresh start)
	ts, err := ReadLastTimestamp(statePath)
	if err != nil {
		t.Fatalf("ReadLastTimestamp (corrupt) error: %v", err)
	}
	if !ts.IsZero() {
		t.Errorf("expected zero time for corrupt file, got %v", ts)
	}
}
```

**Step 6: Run test to verify it fails**

Run:
```bash
go test ./internal/agent/... -v -run State
```

Expected: FAIL (ReadLastTimestamp undefined)

**Step 7: Implement state management**

```go
// internal/agent/state.go
package agent

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

const timestampFormat = time.RFC3339Nano

// ReadLastTimestamp reads the last processed timestamp from file.
// Returns zero time if file doesn't exist or is corrupt.
func ReadLastTimestamp(path string) (time.Time, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}

	ts, err := time.Parse(timestampFormat, strings.TrimSpace(string(data)))
	if err != nil {
		// Corrupt file - return zero time for fresh start
		return time.Time{}, nil
	}

	return ts, nil
}

// WriteLastTimestamp writes the timestamp to the state file.
// Creates parent directories if needed.
func WriteLastTimestamp(path string, ts time.Time) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	return os.WriteFile(path, []byte(ts.Format(timestampFormat)), 0644)
}
```

**Step 8: Run all agent tests**

Run:
```bash
go test ./internal/agent/... -v
```

Expected: PASS

**Step 9: Commit**

```bash
git add -A
git commit -m "feat: add dmesg parsing and state management"
```

---

## Task 4: SQLite Database Layer

**Files:**
- Create: `internal/collector/db.go`
- Create: `internal/collector/db_test.go`

**Step 1: Write failing test for database operations**

```go
// internal/collector/db_test.go
package collector

import (
	"path/filepath"
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
				Severity: "warning",
				Category: "memory",
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
```

**Step 2: Run test to verify it fails**

Run:
```bash
go test ./internal/collector/... -v
```

Expected: FAIL (NewDB undefined)

**Step 3: Implement database layer**

```go
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
```

**Step 4: Add SQLite dependency and run tests**

Run:
```bash
go get modernc.org/sqlite
go test ./internal/collector/... -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add -A
git commit -m "feat: add SQLite database layer for results storage"
```

---

## Task 5: LLM Client

**Files:**
- Create: `internal/collector/llm.go`
- Create: `internal/collector/llm_test.go`

**Step 1: Write test for LLM client response parsing**

```go
// internal/collector/llm_test.go
package collector

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/signalnine/tasseograph/internal/protocol"
)

func TestLLMClientParseResponse(t *testing.T) {
	// Test parsing the expected JSON format
	response := `{"status": "warning", "issues": [{"severity": "warning", "category": "memory", "summary": "ECC error", "evidence": "EDAC MC0"}]}`

	var result protocol.AnalysisResult
	if err := json.Unmarshal([]byte(response), &result); err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if result.Status != "warning" {
		t.Errorf("Status = %q, want %q", result.Status, "warning")
	}
	if len(result.Issues) != 1 {
		t.Fatalf("Issues count = %d, want 1", len(result.Issues))
	}
	if result.Issues[0].Category != "memory" {
		t.Errorf("Category = %q, want %q", result.Issues[0].Category, "memory")
	}
}

func TestLLMClientAnalyze(t *testing.T) {
	// Mock LLM server (OpenAI-compatible format)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/chat/completions" {
			t.Errorf("Path = %q, want /chat/completions", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("Missing or wrong Authorization header")
		}

		// Return mock response in OpenAI format
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]string{
						"content": `{"status": "ok", "issues": []}`,
					},
				},
			},
		})
	}))
	defer server.Close()

	endpoints := []Endpoint{{URL: server.URL, Model: "test-model", APIKey: "test-key"}}
	client := NewLLMClient(endpoints)
	result, latency, err := client.Analyze(context.Background(), []string{"[Mon Feb 3 12:00:00 2026] Normal message"})
	if err != nil {
		t.Fatalf("Analyze error: %v", err)
	}
	if result.Status != "ok" {
		t.Errorf("Status = %q, want %q", result.Status, "ok")
	}
	if latency <= 0 {
		t.Errorf("Latency = %d, want > 0", latency)
	}
}

func TestLLMClientFallback(t *testing.T) {
	// First server fails, second succeeds
	failServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer failServer.Close()

	successServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": `{"status": "ok", "issues": []}`}},
			},
		})
	}))
	defer successServer.Close()

	endpoints := []Endpoint{
		{URL: failServer.URL, Model: "primary", APIKey: "key1"},
		{URL: successServer.URL, Model: "fallback", APIKey: "key2"},
	}
	client := NewLLMClient(endpoints)
	result, _, err := client.Analyze(context.Background(), []string{"test"})
	if err != nil {
		t.Fatalf("Expected fallback to succeed, got: %v", err)
	}
	if result.Status != "ok" {
		t.Errorf("Status = %q, want ok", result.Status)
	}
}

func TestLLMClientAllUnavailable(t *testing.T) {
	// All endpoints fail
	endpoints := []Endpoint{
		{URL: "http://127.0.0.1:59998", Model: "ep1", APIKey: "key"},
		{URL: "http://127.0.0.1:59999", Model: "ep2", APIKey: "key"},
	}
	client := NewLLMClient(endpoints)
	_, _, err := client.Analyze(context.Background(), []string{"test"})
	if err == nil {
		t.Fatal("Expected error when all endpoints unavailable")
	}
	if !IsUnavailable(err) {
		t.Errorf("Expected ErrLLMUnavailable, got: %v", err)
	}
}
```

**Step 2: Run test to verify it fails**

Run:
```bash
go test ./internal/collector/... -v -run LLM
```

Expected: FAIL (NewLLMClient undefined)

**Step 3: Implement LLM client**

```go
// internal/collector/llm.go
package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/signalnine/tasseograph/internal/protocol"
)

const systemPrompt = `You are a Linux kernel expert reviewing dmesg output from bare metal servers. Flag messages indicating:

- Memory errors (MCE, EDAC, ECC corrections trending up)
- Storage degradation (NVMe controller warnings, SMART predictive, I/O errors)
- Network issues (link flapping, PCIe retraining, firmware errors)
- Thermal events (throttling, temperature warnings)
- Driver instability (repeated initialization, timeout patterns)

Ignore routine noise: ACPI info, systemd lifecycle, USB enumeration, normal driver init.

Respond with JSON only:
{"status": "ok" | "warning" | "critical", "issues": [{"severity": "warning" | "critical", "category": "memory" | "storage" | "network" | "thermal" | "driver", "summary": "brief description", "evidence": "relevant log snippet"}]}

If nothing notable, return {"status": "ok", "issues": []}`

// ErrLLMUnavailable indicates all LLM endpoints are down
var ErrLLMUnavailable = errors.New("all LLM endpoints unavailable")

// Endpoint represents a single LLM provider
type Endpoint struct {
	URL    string
	Model  string
	APIKey string
}

// LLMClient calls LLM inference APIs with fallback support (OpenAI-compatible format)
type LLMClient struct {
	endpoints []Endpoint
	client    *http.Client
}

// NewLLMClient creates a new LLM client with fallback chain
func NewLLMClient(endpoints []Endpoint) *LLMClient {
	return &LLMClient{
		endpoints: endpoints,
		client: &http.Client{
			Timeout: 60 * time.Second,
			Transport: &http.Transport{
				DialContext: (&net.Dialer{
					Timeout: 5 * time.Second,
				}).DialContext,
			},
		},
	}
}

// Analyze sends dmesg lines to the LLM and returns the analysis.
// Tries each endpoint in order; returns ErrLLMUnavailable only if ALL fail.
func (c *LLMClient) Analyze(ctx context.Context, lines []string) (*protocol.AnalysisResult, int64, error) {
	if len(c.endpoints) == 0 {
		return nil, 0, errors.New("no LLM endpoints configured")
	}

	var lastErr error
	var totalLatency int64

	for i, ep := range c.endpoints {
		result, latency, err := c.tryEndpoint(ctx, ep, lines)
		totalLatency += latency

		if err == nil {
			if i > 0 {
				log.Printf("LLM fallback: endpoint %d (%s) succeeded after %d failures", i+1, ep.Model, i)
			}
			return result, totalLatency, nil
		}

		lastErr = err
		if isUnavailableErr(err) {
			log.Printf("LLM endpoint %d (%s) unavailable: %v, trying next...", i+1, ep.Model, err)
			continue
		}

		// Non-availability error (e.g., parse error) - don't try fallback
		return nil, totalLatency, err
	}

	// All endpoints failed
	return nil, totalLatency, fmt.Errorf("%w: %v", ErrLLMUnavailable, lastErr)
}

func (c *LLMClient) tryEndpoint(ctx context.Context, ep Endpoint, lines []string) (*protocol.AnalysisResult, int64, error) {
	start := time.Now()

	// Build request body (OpenAI Chat Completions format)
	reqBody := map[string]interface{}{
		"model": ep.Model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": strings.Join(lines, "\n")},
		},
		"max_tokens": 1024,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, 0, err
	}

	url := strings.TrimSuffix(ep.URL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, 0, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+ep.APIKey)

	resp, err := c.client.Do(req)
	if err != nil {
		latency := time.Since(start).Milliseconds()
		// Connection errors are "unavailable"
		var netErr net.Error
		if errors.As(err, &netErr) || errors.Is(err, context.DeadlineExceeded) {
			return nil, latency, fmt.Errorf("connection failed: %w", err)
		}
		return nil, latency, err
	}
	defer resp.Body.Close()

	latency := time.Since(start).Milliseconds()

	// Service unavailable / bad gateway / gateway timeout - try next endpoint
	if resp.StatusCode == http.StatusBadGateway ||
		resp.StatusCode == http.StatusServiceUnavailable ||
		resp.StatusCode == http.StatusGatewayTimeout {
		return nil, latency, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, latency, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	// Parse OpenAI response format
	var apiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, latency, err
	}

	if len(apiResp.Choices) == 0 {
		return nil, latency, fmt.Errorf("empty response from API")
	}

	// Parse the JSON from the message content
	var result protocol.AnalysisResult
	if err := json.Unmarshal([]byte(apiResp.Choices[0].Message.Content), &result); err != nil {
		return nil, latency, fmt.Errorf("failed to parse LLM response: %w", err)
	}

	return &result, latency, nil
}

// isUnavailableErr checks if an error indicates a transient availability issue
func isUnavailableErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "connection") ||
		strings.Contains(s, "HTTP 502") ||
		strings.Contains(s, "HTTP 503") ||
		strings.Contains(s, "HTTP 504")
}

// IsUnavailable checks if the error indicates all LLM endpoints are down
func IsUnavailable(err error) bool {
	return errors.Is(err, ErrLLMUnavailable)
}
```

**Step 4: Run LLM tests**

Run:
```bash
go test ./internal/collector/... -v -run LLM
```

Expected: PASS

**Step 5: Commit**

```bash
git add -A
git commit -m "feat: add LLM inference client"
```

---

## Task 6: HTTP Handler for Collector

**Files:**
- Create: `internal/collector/handler.go`
- Create: `internal/collector/handler_test.go`

**Step 1: Write failing test for ingest handler**

```go
// internal/collector/handler_test.go
package collector

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/signalnine/tasseograph/internal/protocol"
)

func TestIngestHandlerAuth(t *testing.T) {
	dir := t.TempDir()
	db, _ := NewDB(filepath.Join(dir, "test.db"))
	defer db.Close()

	handler := NewIngestHandler(db, nil, "secret-key", 1<<20)

	// No auth header
	req := httptest.NewRequest("POST", "/ingest", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	// Wrong auth
	req = httptest.NewRequest("POST", "/ingest", nil)
	req.Header.Set("Authorization", "Bearer wrong-key")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestIngestHandlerPayloadLimit(t *testing.T) {
	dir := t.TempDir()
	db, _ := NewDB(filepath.Join(dir, "test.db"))
	defer db.Close()

	// 100 byte limit
	handler := NewIngestHandler(db, nil, "secret", 100)

	// Large payload
	bigPayload := make([]byte, 200)
	req := httptest.NewRequest("POST", "/ingest", bytes.NewReader(bigPayload))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestIngestHandlerSuccess(t *testing.T) {
	dir := t.TempDir()
	db, _ := NewDB(filepath.Join(dir, "test.db"))
	defer db.Close()

	// Mock LLM that returns ok (OpenAI format)
	mockLLM := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": `{"status": "ok", "issues": []}`}},
			},
		})
	}))
	defer mockLLM.Close()

	endpoints := []Endpoint{{URL: mockLLM.URL, Model: "test", APIKey: "key"}}
	llmClient := NewLLMClient(endpoints)
	handler := NewIngestHandler(db, llmClient, "secret", 1<<20)

	delta := protocol.DmesgDelta{
		Hostname: "test-host",
		Lines:    []string{"[Mon Feb 3 12:00:00 2026] Normal message"},
	}
	body, _ := json.Marshal(delta)

	req := httptest.NewRequest("POST", "/ingest", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d. Body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	// Verify stored in DB
	results, _ := db.QueryByHostname("test-host", 1)
	if len(results) != 1 {
		t.Errorf("DB has %d results, want 1", len(results))
	}
}
```

**Step 2: Run test to verify it fails**

Run:
```bash
go test ./internal/collector/... -v -run Ingest
```

Expected: FAIL (NewIngestHandler undefined)

**Step 3: Implement ingest handler**

```go
// internal/collector/handler.go
package collector

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/signalnine/tasseograph/internal/protocol"
)

// IngestHandler handles POST /ingest requests from agents
type IngestHandler struct {
	db              *DB
	llm             *LLMClient
	apiKey          string
	maxPayloadBytes int64
}

// NewIngestHandler creates a new ingest handler
func NewIngestHandler(db *DB, llm *LLMClient, apiKey string, maxPayloadBytes int64) *IngestHandler {
	return &IngestHandler{
		db:              db,
		llm:             llm,
		apiKey:          apiKey,
		maxPayloadBytes: maxPayloadBytes,
	}
}

func (h *IngestHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Check auth
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") || strings.TrimPrefix(auth, "Bearer ") != h.apiKey {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Check content length
	if r.ContentLength > h.maxPayloadBytes {
		http.Error(w, "Request Entity Too Large", http.StatusRequestEntityTooLarge)
		return
	}

	// Read body with limit
	body, err := io.ReadAll(io.LimitReader(r.Body, h.maxPayloadBytes+1))
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}
	if int64(len(body)) > h.maxPayloadBytes {
		http.Error(w, "Request Entity Too Large", http.StatusRequestEntityTooLarge)
		return
	}

	// Parse payload
	var delta protocol.DmesgDelta
	if err := json.Unmarshal(body, &delta); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Skip if no lines
	if len(delta.Lines) == 0 {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "skipped", "reason": "no lines"})
		return
	}

	// Call LLM
	var result *protocol.AnalysisResult
	var latency int64
	var llmErr error

	if h.llm != nil {
		result, latency, llmErr = h.llm.Analyze(r.Context(), delta.Lines)
	}

	// Store result
	stored := &protocol.StoredResult{
		Timestamp:    time.Now(),
		Hostname:     delta.Hostname,
		RawDmesg:     strings.Join(delta.Lines, "\n"),
		APILatencyMs: latency,
	}

	if llmErr != nil {
		if IsUnavailable(llmErr) {
			// LLM service is down - log but don't lose the data
			log.Printf("LLM unavailable for %s: %v (data preserved)", delta.Hostname, llmErr)
			stored.Status = "llm_unavailable"
		} else {
			log.Printf("LLM error for %s: %v", delta.Hostname, llmErr)
			stored.Status = "error"
		}
	} else if result != nil {
		stored.Status = result.Status
		stored.Issues = result.Issues
	} else {
		stored.Status = "error"
	}

	if err := h.db.InsertResult(stored); err != nil {
		log.Printf("DB error: %v", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     stored.Status,
		"latency_ms": latency,
	})
}
```

**Step 4: Run handler tests**

Run:
```bash
go test ./internal/collector/... -v -run Ingest
```

Expected: PASS

**Step 5: Commit**

```bash
git add -A
git commit -m "feat: add HTTP ingest handler with auth and size limits"
```

---

## Task 7: Agent Main Loop

**Files:**
- Create: `internal/agent/agent.go`
- Modify: `cmd/tasseograph/main.go`

**Step 1: Implement agent main loop**

```go
// internal/agent/agent.go
package agent

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/signalnine/tasseograph/internal/config"
	"github.com/signalnine/tasseograph/internal/protocol"
)

// Agent collects dmesg and sends to collector
type Agent struct {
	cfg    *config.AgentConfig
	client *http.Client
}

// New creates a new agent
func New(cfg *config.AgentConfig) *Agent {
	transport := &http.Transport{}
	if cfg.TLSSkipVerify {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	return &Agent{
		cfg: cfg,
		client: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		},
	}
}

// Run starts the agent loop
func (a *Agent) Run(ctx context.Context) error {
	log.Printf("Agent starting: hostname=%s collector=%s interval=%s",
		a.cfg.Hostname, a.cfg.CollectorURL, a.cfg.PollInterval)

	ticker := time.NewTicker(a.cfg.PollInterval)
	defer ticker.Stop()

	// Run immediately on start
	if err := a.collect(); err != nil {
		log.Printf("Collection error: %v", err)
	}

	for {
		select {
		case <-ctx.Done():
			log.Println("Agent shutting down")
			return nil
		case <-ticker.C:
			if err := a.collect(); err != nil {
				log.Printf("Collection error: %v", err)
			}
		}
	}
}

func (a *Agent) collect() error {
	// Read last timestamp
	lastSeen, err := ReadLastTimestamp(a.cfg.StateFile)
	if err != nil {
		return fmt.Errorf("read state: %w", err)
	}

	// Get dmesg
	lines, err := GetDmesg()
	if err != nil {
		return fmt.Errorf("get dmesg: %w", err)
	}

	// Filter to new lines
	newLines, latestTs := FilterNewLines(lines, lastSeen)
	if len(newLines) == 0 {
		log.Printf("No new dmesg lines since %v", lastSeen)
		return nil
	}

	// Cap lines to prevent LLM cost explosion
	newLines, truncated := CapLines(newLines)
	if truncated {
		log.Printf("WARNING: Truncated to %d lines (was %d+)", MaxLines, MaxLines)
	}

	log.Printf("Sending %d new dmesg lines", len(newLines))

	// Send to collector
	delta := protocol.DmesgDelta{
		Hostname:  a.cfg.Hostname,
		Timestamp: time.Now(),
		Lines:     newLines,
	}

	if err := a.send(delta); err != nil {
		return fmt.Errorf("send: %w", err)
	}

	// Update state
	if err := WriteLastTimestamp(a.cfg.StateFile, latestTs); err != nil {
		return fmt.Errorf("write state: %w", err)
	}

	return nil
}

func (a *Agent) send(delta protocol.DmesgDelta) error {
	body, err := json.Marshal(delta)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", a.cfg.CollectorURL, bytes.NewReader(body))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.cfg.APIKey)

	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("collector returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return nil
}
```

**Step 2: Update main.go to wire up agent**

```go
// cmd/tasseograph/main.go
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/signalnine/tasseograph/internal/agent"
	"github.com/signalnine/tasseograph/internal/config"
	"github.com/spf13/cobra"
)

var (
	agentConfigPath     string
	collectorConfigPath string
)

var rootCmd = &cobra.Command{
	Use:   "tasseograph",
	Short: "dmesg anomaly detection via LLM",
}

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Run the host agent",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadAgentConfig(agentConfigPath)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		a := agent.New(cfg)

		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()

		return a.Run(ctx)
	},
}

var collectorCmd = &cobra.Command{
	Use:   "collector",
	Short: "Run the central collector",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("collector not implemented")
		return nil
	},
}

func init() {
	agentCmd.Flags().StringVarP(&agentConfigPath, "config", "c", "/etc/tasseograph/agent.yaml", "path to config file")
	collectorCmd.Flags().StringVarP(&collectorConfigPath, "config", "c", "/etc/tasseograph/collector.yaml", "path to config file")

	rootCmd.AddCommand(agentCmd)
	rootCmd.AddCommand(collectorCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

**Step 3: Build and verify**

Run:
```bash
go build -o dist/tasseograph ./cmd/tasseograph
./dist/tasseograph agent --help
```

Expected: Shows help with --config flag

**Step 4: Commit**

```bash
git add -A
git commit -m "feat: implement agent main loop with collector integration"
```

---

## Task 8: Collector Main Server

**Files:**
- Create: `internal/collector/server.go`
- Modify: `cmd/tasseograph/main.go`

**Step 1: Implement collector server**

```go
// internal/collector/server.go
package collector

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
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
```

**Step 2: Update main.go to wire up collector**

Replace the collectorCmd in `cmd/tasseograph/main.go`:

```go
var collectorCmd = &cobra.Command{
	Use:   "collector",
	Short: "Run the central collector",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadCollectorConfig(collectorConfigPath)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		srv, err := collector.NewServer(cfg)
		if err != nil {
			return err
		}

		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()

		return srv.Run(ctx)
	},
}
```

Add import:
```go
"github.com/signalnine/tasseograph/internal/collector"
```

**Step 3: Build and verify**

Run:
```bash
go build -o dist/tasseograph ./cmd/tasseograph
./dist/tasseograph collector --help
```

Expected: Shows help with --config flag

**Step 4: Commit**

```bash
git add -A
git commit -m "feat: implement collector HTTPS server"
```

---

## Task 9: Example Configs and Systemd Units

**Files:**
- Create: `deploy/config/agent.yaml.example`
- Create: `deploy/config/collector.yaml.example`
- Create: `deploy/systemd/tasseograph-agent.service`
- Create: `deploy/systemd/tasseograph-collector.service`

**Step 1: Create example agent config**

```yaml
# deploy/config/agent.yaml.example
# Tasseograph Agent Configuration

# Collector endpoint (HTTPS)
collector_url: "https://collector.internal:9311/ingest"

# How often to check dmesg
poll_interval: 5m

# Where to store last-seen timestamp
state_file: /var/lib/tasseograph/last_timestamp

# Override hostname (optional, defaults to os.Hostname)
# hostname: "custom-hostname"

# Skip TLS verification (for self-signed certs)
tls_skip_verify: true

# API key is set via environment variable TASSEOGRAPH_API_KEY
```

**Step 2: Create example collector config**

```yaml
# deploy/config/collector.yaml.example
# Tasseograph Collector Configuration

# Listen address
listen_addr: ":9311"

# SQLite database path
db_path: /var/lib/tasseograph/results.db

# LLM endpoints with fallback chain (tried in order)
# Uses OpenAI-compatible API format
llm_endpoints:
  # Primary: internal inference gateway
  - url: "https://inference.internal/v1"
    model: "anthropic/haiku-4.5"
    api_key_env: "INTERNAL_LLM_KEY"

  # Fallback 1: different model on same gateway
  - url: "https://inference.internal/v1"
    model: "openai/gpt-5-nano"
    api_key_env: "INTERNAL_LLM_KEY"

  # Fallback 2: direct to OpenAI (if internal is down)
  - url: "https://api.openai.com/v1"
    model: "gpt-4o-mini"
    api_key_env: "OPENAI_API_KEY"

# Retry settings
max_retries: 3

# Max payload size (1MB)
max_payload_bytes: 1048576

# TLS certificate paths
tls_cert: /etc/tasseograph/tls/cert.pem
tls_key: /etc/tasseograph/tls/key.pem

# API keys are set via environment variables:
# TASSEOGRAPH_API_KEY - shared secret for agent auth
# INTERNAL_LLM_KEY - your internal inference gateway key
# OPENAI_API_KEY - fallback OpenAI key (optional)
```

**Step 3: Create agent systemd unit**

```ini
# deploy/systemd/tasseograph-agent.service
[Unit]
Description=Tasseograph dmesg Agent
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/tasseograph agent --config /etc/tasseograph/agent.yaml
Restart=always
RestartSec=10

# Security hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/tasseograph

# Environment file for API key
EnvironmentFile=-/etc/tasseograph/agent.env

[Install]
WantedBy=multi-user.target
```

**Step 4: Create collector systemd unit**

```ini
# deploy/systemd/tasseograph-collector.service
[Unit]
Description=Tasseograph dmesg Collector
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/tasseograph collector --config /etc/tasseograph/collector.yaml
Restart=always
RestartSec=10

# Security hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/tasseograph

# Environment file for API keys
EnvironmentFile=-/etc/tasseograph/collector.env

[Install]
WantedBy=multi-user.target
```

**Step 5: Commit**

```bash
mkdir -p deploy/config deploy/systemd
# (create the files above)
git add -A
git commit -m "feat: add example configs and systemd units"
```

---

## Task 10: Makefile and Build

**Files:**
- Create: `Makefile`

**Step 1: Create Makefile**

```makefile
# Makefile
.PHONY: build build-linux test clean

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

build:
	go build $(LDFLAGS) -o dist/tasseograph ./cmd/tasseograph

build-linux:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o dist/tasseograph-linux-amd64 ./cmd/tasseograph
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o dist/tasseograph-linux-arm64 ./cmd/tasseograph

test:
	go test -v ./...

test-cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

clean:
	rm -rf dist/ coverage.out coverage.html

lint:
	golangci-lint run

fmt:
	go fmt ./...
```

**Step 2: Test the build**

Run:
```bash
make build
make test
make build-linux
ls -la dist/
```

Expected: Binaries for current platform and linux amd64/arm64

**Step 3: Commit**

```bash
git add Makefile
git commit -m "feat: add Makefile for build and test"
```

---

## Task 11: Integration Test

**Files:**
- Create: `test/integration_test.go`

**Step 1: Write integration test**

```go
// test/integration_test.go
package test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/signalnine/tasseograph/internal/collector"
	"github.com/signalnine/tasseograph/internal/config"
	"github.com/signalnine/tasseograph/internal/protocol"
)

func TestIntegrationCollectorIngest(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	dir := t.TempDir()

	// Generate self-signed cert
	certPath, keyPath := generateTestCert(t, dir)

	// Mock LLM server (OpenAI format)
	llmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": `{"status": "warning", "issues": [{"severity": "warning", "category": "memory", "summary": "Test issue", "evidence": "test"}]}`}},
			},
		})
	}))
	defer llmServer.Close()

	// Write collector config
	cfg := &config.CollectorConfig{
		ListenAddr: ":0", // random port
		DBPath:     filepath.Join(dir, "test.db"),
		TLSCert:    certPath,
		TLSKey:     keyPath,
		LLMEndpoints: []config.LLMEndpoint{
			{URL: llmServer.URL, Model: "test", APIKey: "test-key"},
		},
		APIKey:          "agent-secret",
		MaxPayloadBytes: 1 << 20,
	}

	// Start collector
	db, err := collector.NewDB(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	endpoints := []collector.Endpoint{{URL: llmServer.URL, Model: "test", APIKey: "test-key"}}
	llm := collector.NewLLMClient(endpoints)
	handler := collector.NewIngestHandler(db, llm, cfg.APIKey, cfg.MaxPayloadBytes)

	// Test request
	delta := protocol.DmesgDelta{
		Hostname:  "test-host",
		Timestamp: time.Now(),
		Lines:     []string{"[Mon Feb 3 12:00:00 2026] EDAC MC0: 1 CE on DIMM0"},
	}
	body, _ := json.Marshal(delta)

	req := httptest.NewRequest("POST", "/ingest", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer agent-secret")
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Status = %d, want 200. Body: %s", rec.Code, rec.Body.String())
	}

	// Verify in DB
	results, err := db.QueryByHostname("test-host", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}
	if results[0].Status != "warning" {
		t.Errorf("Status = %q, want warning", results[0].Status)
	}
}

func generateTestCert(t *testing.T, dir string) (certPath, keyPath string) {
	t.Helper()

	priv, _ := rsa.GenerateKey(rand.Reader, 2048)

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
	}

	certDER, _ := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)

	certPath = filepath.Join(dir, "cert.pem")
	keyPath = filepath.Join(dir, "key.pem")

	certOut, _ := os.Create(certPath)
	pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	certOut.Close()

	keyOut, _ := os.Create(keyPath)
	pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})
	keyOut.Close()

	return certPath, keyPath
}
```

**Step 2: Run integration test**

Run:
```bash
go test -v ./test/...
```

Expected: PASS

**Step 3: Commit**

```bash
mkdir -p test
git add -A
git commit -m "test: add integration test for collector ingest flow"
```

---

## Task 12: Final Verification

**Step 1: Run all tests**

Run:
```bash
make test
```

Expected: All tests pass

**Step 2: Build all binaries**

Run:
```bash
make build-linux
ls -la dist/
```

Expected: Three binaries (local, linux-amd64, linux-arm64)

**Step 3: Verify help output**

Run:
```bash
./dist/tasseograph --help
./dist/tasseograph agent --help
./dist/tasseograph collector --help
```

Expected: All show appropriate help text

**Step 4: Final commit (if any changes)**

```bash
git status
# If clean, skip this step
```

---

## Summary

After completing all tasks, you will have:

1. **Go module** with CLI using cobra
2. **Protocol types** shared between agent and collector
3. **Config loading** from YAML with env overrides
4. **dmesg parsing** with timestamp extraction and filtering
5. **State management** for tracking last-seen timestamp
6. **SQLite database** for storing analysis results
7. **LLM client** for calling inference API
8. **HTTP handler** with auth and payload limits
9. **Agent loop** that collects and sends dmesg deltas
10. **Collector server** that receives, analyzes, and stores
11. **Example configs** and systemd units
12. **Makefile** for building and testing
13. **Integration test** for end-to-end verification

Total: ~12 tasks, each with 4-7 steps
