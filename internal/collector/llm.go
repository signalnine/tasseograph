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
{"status": "ok" | "warning" | "critical", "issues": [{"summary": "brief description", "evidence": "relevant log snippet"}]}

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
