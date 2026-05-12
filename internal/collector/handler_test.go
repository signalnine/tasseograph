// internal/collector/handler_test.go
package collector

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

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
	llmClient := NewLLMClient(endpoints, 0)
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

func TestIngestHandlerHonorsAgentTimestamp(t *testing.T) {
	dir := t.TempDir()
	db, _ := NewDB(filepath.Join(dir, "test.db"))
	defer db.Close()

	mockLLM := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": `{"status": "ok", "issues": []}`}},
			},
		})
	}))
	defer mockLLM.Close()

	llmClient := NewLLMClient([]Endpoint{{URL: mockLLM.URL, Model: "test", APIKey: "key"}}, 0)
	handler := NewIngestHandler(db, llmClient, "secret", 1<<20)

	agentTs := time.Now().Add(-2 * time.Minute).UTC().Truncate(time.Second)
	delta := protocol.DmesgDelta{
		Hostname:  "ts-host",
		Timestamp: agentTs,
		Lines:     []string{"[Mon Feb 3 12:00:00 2026] msg"},
	}
	body, _ := json.Marshal(delta)
	req := httptest.NewRequest("POST", "/ingest", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Status = %d", rec.Code)
	}
	results, _ := db.QueryByHostname("ts-host", 1)
	if len(results) != 1 {
		t.Fatalf("DB has %d results, want 1", len(results))
	}
	if !results[0].Timestamp.Equal(agentTs) {
		t.Errorf("Stored timestamp = %v, want %v (agent's)", results[0].Timestamp, agentTs)
	}
}

func TestIngestHandlerRejectsEmptyHostname(t *testing.T) {
	// The agent refuses to start without a hostname (config validation), but
	// a leaked bearer token or a manual POST could still send hostname="".
	// Such rows are unreachable via QueryByHostname; the handler must reject
	// them at the wire rather than storing unfilterable data.
	dir := t.TempDir()
	db, _ := NewDB(filepath.Join(dir, "test.db"))
	defer db.Close()

	handler := NewIngestHandler(db, nil, "secret", 1<<20)

	delta := protocol.DmesgDelta{
		Hostname: "",
		Lines:    []string{"[Mon Feb 3 12:00:00 2026] msg"},
	}
	body, _ := json.Marshal(delta)
	req := httptest.NewRequest("POST", "/ingest", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	// Nothing should be stored.
	results, _ := db.QueryByHostname("", 10)
	if len(results) != 0 {
		t.Errorf("DB has %d rows for empty hostname, want 0", len(results))
	}
}

func TestIngestHandlerRejectsSkewedTimestamp(t *testing.T) {
	dir := t.TempDir()
	db, _ := NewDB(filepath.Join(dir, "test.db"))
	defer db.Close()

	mockLLM := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": `{"status": "ok", "issues": []}`}},
			},
		})
	}))
	defer mockLLM.Close()

	llmClient := NewLLMClient([]Endpoint{{URL: mockLLM.URL, Model: "test", APIKey: "key"}}, 0)
	handler := NewIngestHandler(db, llmClient, "secret", 1<<20)

	// 10 years in the future - obvious clock skew, must be ignored.
	skewed := time.Now().Add(10 * 365 * 24 * time.Hour)
	delta := protocol.DmesgDelta{
		Hostname:  "skew-host",
		Timestamp: skewed,
		Lines:     []string{"[Mon Feb 3 12:00:00 2026] msg"},
	}
	body, _ := json.Marshal(delta)
	req := httptest.NewRequest("POST", "/ingest", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Status = %d", rec.Code)
	}
	results, _ := db.QueryByHostname("skew-host", 1)
	if len(results) != 1 {
		t.Fatalf("DB has %d results, want 1", len(results))
	}
	// Stored timestamp should be near now (collector clock), not the skewed value.
	if delta := time.Since(results[0].Timestamp); delta > time.Minute || delta < -time.Minute {
		t.Errorf("Stored timestamp = %v, expected fallback to ~now", results[0].Timestamp)
	}
}
