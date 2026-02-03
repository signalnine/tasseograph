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
