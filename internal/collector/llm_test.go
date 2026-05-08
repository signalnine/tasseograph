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
	response := `{"status": "warning", "issues": [{"summary": "ECC error", "evidence": "EDAC MC0"}]}`

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
	client := NewLLMClient(endpoints, 0)
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
	client := NewLLMClient(endpoints, 0)
	result, _, err := client.Analyze(context.Background(), []string{"test"})
	if err != nil {
		t.Fatalf("Expected fallback to succeed, got: %v", err)
	}
	if result.Status != "ok" {
		t.Errorf("Status = %q, want ok", result.Status)
	}
}

func TestLLMClientFallbackOnRateLimit(t *testing.T) {
	// Primary returns 429 (rate limited); fallback should succeed.
	rateLimitServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer rateLimitServer.Close()

	successServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": `{"status": "ok", "issues": []}`}},
			},
		})
	}))
	defer successServer.Close()

	endpoints := []Endpoint{
		{URL: rateLimitServer.URL, Model: "primary", APIKey: "key1"},
		{URL: successServer.URL, Model: "fallback", APIKey: "key2"},
	}
	client := NewLLMClient(endpoints, 0)
	result, _, err := client.Analyze(context.Background(), []string{"test"})
	if err != nil {
		t.Fatalf("Expected fallback on 429, got: %v", err)
	}
	if result.Status != "ok" {
		t.Errorf("Status = %q, want ok", result.Status)
	}
}

func TestLLMClientFallbackOnRequestTimeout(t *testing.T) {
	// Primary returns 408 (request timeout); fallback should succeed.
	timeoutServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusRequestTimeout)
	}))
	defer timeoutServer.Close()

	successServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": `{"status": "ok", "issues": []}`}},
			},
		})
	}))
	defer successServer.Close()

	endpoints := []Endpoint{
		{URL: timeoutServer.URL, Model: "primary", APIKey: "key1"},
		{URL: successServer.URL, Model: "fallback", APIKey: "key2"},
	}
	client := NewLLMClient(endpoints, 0)
	result, _, err := client.Analyze(context.Background(), []string{"test"})
	if err != nil {
		t.Fatalf("Expected fallback on 408, got: %v", err)
	}
	if result.Status != "ok" {
		t.Errorf("Status = %q, want ok", result.Status)
	}
}

func TestLLMClientStripsMarkdownJSONFence(t *testing.T) {
	// LLM wraps JSON in ```json ... ``` despite the prompt asking for JSON only.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{
					"content": "```json\n{\"status\": \"ok\", \"issues\": []}\n```",
				}},
			},
		})
	}))
	defer server.Close()

	client := NewLLMClient([]Endpoint{{URL: server.URL, Model: "m", APIKey: "k"}}, 0)
	result, _, err := client.Analyze(context.Background(), []string{"line"})
	if err != nil {
		t.Fatalf("Analyze error: %v", err)
	}
	if result.Status != "ok" {
		t.Errorf("Status = %q, want %q", result.Status, "ok")
	}
}

func TestLLMClientStripsBareCodeFence(t *testing.T) {
	// Bare ``` ... ``` with no language tag.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{
					"content": "```\n{\"status\": \"warning\", \"issues\": [{\"summary\": \"x\", \"evidence\": \"y\"}]}\n```",
				}},
			},
		})
	}))
	defer server.Close()

	client := NewLLMClient([]Endpoint{{URL: server.URL, Model: "m", APIKey: "k"}}, 0)
	result, _, err := client.Analyze(context.Background(), []string{"line"})
	if err != nil {
		t.Fatalf("Analyze error: %v", err)
	}
	if result.Status != "warning" {
		t.Errorf("Status = %q, want %q", result.Status, "warning")
	}
	if len(result.Issues) != 1 {
		t.Errorf("Issues count = %d, want 1", len(result.Issues))
	}
}

func TestLLMClientRejectsNonJSON(t *testing.T) {
	// Genuinely malformed content (model refuses or replies in prose).
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": "I'm sorry, I cannot help."}},
			},
		})
	}))
	defer server.Close()

	client := NewLLMClient([]Endpoint{{URL: server.URL, Model: "m", APIKey: "k"}}, 0)
	_, _, err := client.Analyze(context.Background(), []string{"line"})
	if err == nil {
		t.Fatal("Expected parse error for non-JSON content, got nil")
	}
}

func TestLLMClientRetriesOnTransientFailure(t *testing.T) {
	// First two attempts return 503, third succeeds. With maxRetries=2 the
	// single endpoint should resolve without falling through.
	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": `{"status": "ok", "issues": []}`}},
			},
		})
	}))
	defer server.Close()

	client := NewLLMClient([]Endpoint{{URL: server.URL, Model: "m", APIKey: "k"}}, 2)
	result, _, err := client.Analyze(context.Background(), []string{"line"})
	if err != nil {
		t.Fatalf("Analyze error after retries: %v", err)
	}
	if result.Status != "ok" {
		t.Errorf("Status = %q, want ok", result.Status)
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3 (1 initial + 2 retries)", attempts)
	}
}

func TestLLMClientNoRetryOnPermanentError(t *testing.T) {
	// Non-JSON content is a permanent (parse) error, not a transient one.
	// maxRetries should not be applied.
	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": "I'm sorry, I cannot help."}},
			},
		})
	}))
	defer server.Close()

	client := NewLLMClient([]Endpoint{{URL: server.URL, Model: "m", APIKey: "k"}}, 5)
	_, _, err := client.Analyze(context.Background(), []string{"line"})
	if err == nil {
		t.Fatal("Expected parse error, got nil")
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1 (permanent errors must not retry)", attempts)
	}
}

func TestLLMClientCapsResponseSize(t *testing.T) {
	// A misbehaving endpoint that streams more than the cap. The decoder
	// should hit the LimitReader EOF before exhausting memory.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"`))
		// Stream more than the 1 MiB cap as a JSON string body.
		chunk := make([]byte, 64*1024)
		for i := range chunk {
			chunk[i] = 'a'
		}
		for i := 0; i < 32; i++ { // 2 MiB total
			w.Write(chunk)
		}
		w.Write([]byte(`"}}]}`))
	}))
	defer server.Close()

	client := NewLLMClient([]Endpoint{{URL: server.URL, Model: "m", APIKey: "k"}}, 0)
	_, _, err := client.Analyze(context.Background(), []string{"line"})
	if err == nil {
		t.Fatal("Expected error from oversized response, got nil")
	}
}

func TestLLMClientAllUnavailable(t *testing.T) {
	// All endpoints fail
	endpoints := []Endpoint{
		{URL: "http://127.0.0.1:59998", Model: "ep1", APIKey: "key"},
		{URL: "http://127.0.0.1:59999", Model: "ep2", APIKey: "key"},
	}
	client := NewLLMClient(endpoints, 0)
	_, _, err := client.Analyze(context.Background(), []string{"test"})
	if err == nil {
		t.Fatal("Expected error when all endpoints unavailable")
	}
	if !IsUnavailable(err) {
		t.Errorf("Expected ErrLLMUnavailable, got: %v", err)
	}
}
