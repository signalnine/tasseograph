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

// maxLLMResponseBytes caps the size of an LLM response body so a misbehaving
// or compromised endpoint cannot exhaust collector memory. Real responses are
// bounded by max_tokens=1024 in the request and run well under 100KB.
const maxLLMResponseBytes = 1 << 20 // 1 MiB

// Endpoint represents a single LLM provider
type Endpoint struct {
	URL    string
	Model  string
	APIKey string
}

// LLMClient calls LLM inference APIs with fallback support (OpenAI-compatible format)
type LLMClient struct {
	endpoints  []Endpoint
	maxRetries int
	client     *http.Client
}

// NewLLMClient creates a new LLM client with fallback chain.
// maxRetries is the number of additional in-endpoint retries on transient
// failures (5xx, 408, 429, connection errors) before falling through to the
// next endpoint. 0 preserves the original "one shot per endpoint" behavior.
func NewLLMClient(endpoints []Endpoint, maxRetries int) *LLMClient {
	if maxRetries < 0 {
		maxRetries = 0
	}
	return &LLMClient{
		endpoints:  endpoints,
		maxRetries: maxRetries,
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

// AnalysisMeta is per-call metadata returned alongside the parsed result.
// Provider/Model come from the upstream's response body when available
// (OpenRouter returns both); empty strings for endpoints that don't.
type AnalysisMeta struct {
	LatencyMs int64
	Provider  string
	Model     string
}

// Analyze sends dmesg lines to the LLM and returns the analysis.
// Tries each endpoint in order; returns ErrLLMUnavailable only if ALL fail.
func (c *LLMClient) Analyze(ctx context.Context, lines []string) (*protocol.AnalysisResult, AnalysisMeta, error) {
	if len(c.endpoints) == 0 {
		return nil, AnalysisMeta{}, errors.New("no LLM endpoints configured")
	}

	var lastErr error
	var meta AnalysisMeta

	for i, ep := range c.endpoints {
		var (
			result      *protocol.AnalysisResult
			attemptMeta AnalysisMeta
			err         error
		)
		// One initial attempt plus up to maxRetries retries against this
		// endpoint, but only retry on transient (availability) errors.
		for attempt := 0; attempt <= c.maxRetries; attempt++ {
			result, attemptMeta, err = c.tryEndpoint(ctx, ep, lines)
			meta.LatencyMs += attemptMeta.LatencyMs

			if err == nil || !isUnavailableErr(err) {
				break
			}
			if attempt < c.maxRetries {
				log.Printf("LLM endpoint %d (%s) attempt %d failed: %v, retrying...", i+1, ep.Model, attempt+1, err)
			}
		}

		if err == nil {
			if i > 0 {
				log.Printf("LLM fallback: endpoint %d (%s) succeeded after %d failures", i+1, ep.Model, i)
			}
			// Adopt the successful attempt's provider/model; the accumulated
			// latency stays so the row reflects total wall time including
			// any failed primary attempts.
			meta.Provider = attemptMeta.Provider
			meta.Model = attemptMeta.Model
			return result, meta, nil
		}

		lastErr = err
		if isUnavailableErr(err) {
			log.Printf("LLM endpoint %d (%s) unavailable after %d attempts: %v, trying next...", i+1, ep.Model, c.maxRetries+1, err)
			continue
		}

		// Non-availability error (e.g., parse error) - don't try fallback
		return nil, meta, err
	}

	// All endpoints failed
	return nil, meta, fmt.Errorf("%w: %v", ErrLLMUnavailable, lastErr)
}

func (c *LLMClient) tryEndpoint(ctx context.Context, ep Endpoint, lines []string) (*protocol.AnalysisResult, AnalysisMeta, error) {
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
		return nil, AnalysisMeta{}, err
	}

	url := strings.TrimSuffix(ep.URL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, AnalysisMeta{}, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+ep.APIKey)

	resp, err := c.client.Do(req)
	if err != nil {
		meta := AnalysisMeta{LatencyMs: time.Since(start).Milliseconds()}
		var netErr net.Error
		if errors.As(err, &netErr) || errors.Is(err, context.DeadlineExceeded) {
			return nil, meta, fmt.Errorf("connection failed: %w", err)
		}
		return nil, meta, err
	}
	defer resp.Body.Close()

	meta := AnalysisMeta{LatencyMs: time.Since(start).Milliseconds()}

	// Transient errors - try next endpoint
	if resp.StatusCode == http.StatusRequestTimeout ||
		resp.StatusCode == http.StatusTooManyRequests ||
		resp.StatusCode == http.StatusBadGateway ||
		resp.StatusCode == http.StatusServiceUnavailable ||
		resp.StatusCode == http.StatusGatewayTimeout {
		return nil, meta, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	limitedBody := io.LimitReader(resp.Body, maxLLMResponseBytes)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(limitedBody)
		return nil, meta, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	// Parse the OpenAI-compatible envelope plus the optional model/provider
	// fields that OpenRouter (and a few other gateways) tack on.
	var apiResp struct {
		Model    string `json:"model"`
		Provider string `json:"provider"`
		Choices  []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(limitedBody).Decode(&apiResp); err != nil {
		return nil, meta, err
	}
	meta.Model = apiResp.Model
	meta.Provider = apiResp.Provider

	if len(apiResp.Choices) == 0 {
		return nil, meta, fmt.Errorf("empty response from API")
	}

	// Parse the JSON from the message content. Some models wrap structured
	// output in markdown code fences despite prompts that say "JSON only".
	content := stripCodeFence(apiResp.Choices[0].Message.Content)
	var result protocol.AnalysisResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil, meta, fmt.Errorf("failed to parse LLM response: %w", err)
	}

	return &result, meta, nil
}

// stripCodeFence removes a leading ``` (optionally followed by a language tag
// like "json") and trailing ``` from s. Surrounding whitespace is trimmed.
// If no fence is found, the trimmed string is returned unchanged.
func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```")
	if nl := strings.IndexByte(s, '\n'); nl >= 0 {
		// Drop the language tag (or empty string) on the opening line.
		s = s[nl+1:]
	}
	s = strings.TrimSuffix(strings.TrimRight(s, " \t\n\r"), "```")
	return strings.TrimSpace(s)
}

// isUnavailableErr checks if an error indicates a transient availability issue
func isUnavailableErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "connection") ||
		strings.Contains(s, "HTTP 408") ||
		strings.Contains(s, "HTTP 429") ||
		strings.Contains(s, "HTTP 502") ||
		strings.Contains(s, "HTTP 503") ||
		strings.Contains(s, "HTTP 504")
}

// IsUnavailable checks if the error indicates all LLM endpoints are down
func IsUnavailable(err error) bool {
	return errors.Is(err, ErrLLMUnavailable)
}
