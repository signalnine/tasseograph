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
