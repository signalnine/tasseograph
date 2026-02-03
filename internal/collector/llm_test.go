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
	response := `{"status": "warning", "issues": [{"severity": "warning", "summary": "ECC error", "evidence": "EDAC MC0"}]}`

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
	// Latency can be 0 for very fast mock responses (sub-millisecond)
	if latency < 0 {
		t.Errorf("Latency = %d, want >= 0", latency)
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
