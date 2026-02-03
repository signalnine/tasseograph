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
