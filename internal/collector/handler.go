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

	// The agent refuses to start without a hostname, but a leaked bearer token
	// or a manual POST could otherwise insert rows that QueryByHostname can
	// never surface. Reject at the wire instead of poisoning the DB.
	if delta.Hostname == "" {
		http.Error(w, "hostname is required", http.StatusBadRequest)
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
	var meta AnalysisMeta
	var llmErr error

	if h.llm != nil {
		result, meta, llmErr = h.llm.Analyze(r.Context(), delta.Lines)
	}

	// Honor the agent's collection timestamp so retries/queued sends record
	// when the data was gathered, not when we processed it. Reject obvious
	// clock skew (or unset/zero values) and fall back to the collector clock.
	now := time.Now()
	ts := delta.Timestamp
	if ts.IsZero() || ts.After(now.Add(5*time.Minute)) || ts.Before(now.Add(-24*time.Hour)) {
		ts = now
	}

	// Store result
	stored := &protocol.StoredResult{
		Timestamp:    ts,
		Hostname:     delta.Hostname,
		RawDmesg:     strings.Join(delta.Lines, "\n"),
		APILatencyMs: meta.LatencyMs,
		Provider:     meta.Provider,
		Model:        meta.Model,
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
		"latency_ms": meta.LatencyMs,
	})
}
