// internal/config/config_test.go
package config

import (
	"os"
	"path/filepath"
	"strings"
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

	// Set required env var
	t.Setenv("TASSEOGRAPH_API_KEY", "test-key")

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

func TestLoadAgentConfig_MissingPollIntervalErrors(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.yaml")
	// poll_interval omitted entirely - YAML zero value is 0, which panics
	// in time.NewTicker(0) in agent.Run.
	content := []byte(`
collector_url: "https://collector.internal:9311/ingest"
state_file: /var/lib/tasseograph/last_timestamp
`)
	if err := os.WriteFile(configPath, content, 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TASSEOGRAPH_API_KEY", "test-key")

	_, err := LoadAgentConfig(configPath)
	if err == nil {
		t.Fatal("expected error for missing poll_interval, got nil")
	}
	if !strings.Contains(err.Error(), "poll_interval") {
		t.Errorf("error %q does not mention poll_interval", err.Error())
	}
}

func TestLoadAgentConfig_ZeroPollIntervalErrors(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.yaml")
	content := []byte(`
collector_url: "https://collector.internal:9311/ingest"
poll_interval: 0s
state_file: /var/lib/tasseograph/last_timestamp
`)
	if err := os.WriteFile(configPath, content, 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TASSEOGRAPH_API_KEY", "test-key")

	_, err := LoadAgentConfig(configPath)
	if err == nil {
		t.Fatal("expected error for zero poll_interval, got nil")
	}
	if !strings.Contains(err.Error(), "poll_interval") {
		t.Errorf("error %q does not mention poll_interval", err.Error())
	}
}

func TestLoadAgentConfig_MissingStateFileErrors(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.yaml")
	content := []byte(`
collector_url: "https://collector.internal:9311/ingest"
poll_interval: 5m
`)
	if err := os.WriteFile(configPath, content, 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TASSEOGRAPH_API_KEY", "test-key")

	_, err := LoadAgentConfig(configPath)
	if err == nil {
		t.Fatal("expected error for missing state_file, got nil")
	}
	if !strings.Contains(err.Error(), "state_file") {
		t.Errorf("error %q does not mention state_file", err.Error())
	}
}

func TestLoadCollectorConfig_DefaultMaxPayloadBytesWhenUnset(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "collector.yaml")
	// max_payload_bytes omitted -- YAML zero value is 0, which makes the
	// handler reject every request with a body. Should default to 1 MB.
	content := []byte(`
listen_addr: ":9311"
db_path: /var/lib/tasseograph/results.db
tls_cert: /etc/tasseograph/tls/cert.pem
tls_key: /etc/tasseograph/tls/key.pem
llm_endpoints:
  - url: "https://inference.internal/v1"
    model: "anthropic/haiku-4.5"
    api_key_env: "INTERNAL_LLM_KEY"
`)
	if err := os.WriteFile(configPath, content, 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TASSEOGRAPH_API_KEY", "test-api-key")

	cfg, err := LoadCollectorConfig(configPath)
	if err != nil {
		t.Fatalf("LoadCollectorConfig failed: %v", err)
	}
	if cfg.MaxPayloadBytes != 1048576 {
		t.Errorf("MaxPayloadBytes = %d, want 1048576 (1MB default)", cfg.MaxPayloadBytes)
	}
}

func TestLoadCollectorConfig_DefaultMaxPayloadBytesWhenZero(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "collector.yaml")
	content := []byte(`
listen_addr: ":9311"
db_path: /var/lib/tasseograph/results.db
max_payload_bytes: 0
tls_cert: /etc/tasseograph/tls/cert.pem
tls_key: /etc/tasseograph/tls/key.pem
llm_endpoints:
  - url: "https://inference.internal/v1"
    model: "anthropic/haiku-4.5"
    api_key_env: "INTERNAL_LLM_KEY"
`)
	if err := os.WriteFile(configPath, content, 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TASSEOGRAPH_API_KEY", "test-api-key")

	cfg, err := LoadCollectorConfig(configPath)
	if err != nil {
		t.Fatalf("LoadCollectorConfig failed: %v", err)
	}
	if cfg.MaxPayloadBytes != 1048576 {
		t.Errorf("MaxPayloadBytes = %d, want 1048576 (1MB default)", cfg.MaxPayloadBytes)
	}
}

func TestLoadCollectorConfig_NegativeMaxPayloadBytesErrors(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "collector.yaml")
	content := []byte(`
listen_addr: ":9311"
db_path: /var/lib/tasseograph/results.db
max_payload_bytes: -1
tls_cert: /etc/tasseograph/tls/cert.pem
tls_key: /etc/tasseograph/tls/key.pem
llm_endpoints:
  - url: "https://inference.internal/v1"
    model: "anthropic/haiku-4.5"
    api_key_env: "INTERNAL_LLM_KEY"
`)
	if err := os.WriteFile(configPath, content, 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TASSEOGRAPH_API_KEY", "test-api-key")

	_, err := LoadCollectorConfig(configPath)
	if err == nil {
		t.Fatal("expected error for negative max_payload_bytes, got nil")
	}
	if !strings.Contains(err.Error(), "max_payload_bytes") {
		t.Errorf("error %q does not mention max_payload_bytes", err.Error())
	}
}

func TestLoadCollectorConfig_MissingTLSCertErrors(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "collector.yaml")
	// tls_cert omitted - server.go calls tls.LoadX509KeyPair which fails with
	// a confusing 'open : no such file or directory' at startup.
	content := []byte(`
listen_addr: ":9311"
db_path: /var/lib/tasseograph/results.db
tls_key: /etc/tasseograph/tls/key.pem
llm_endpoints:
  - url: "https://inference.internal/v1"
    model: "anthropic/haiku-4.5"
    api_key_env: "INTERNAL_LLM_KEY"
`)
	if err := os.WriteFile(configPath, content, 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TASSEOGRAPH_API_KEY", "test-api-key")

	_, err := LoadCollectorConfig(configPath)
	if err == nil {
		t.Fatal("expected error for missing tls_cert, got nil")
	}
	if !strings.Contains(err.Error(), "tls_cert") {
		t.Errorf("error %q does not mention tls_cert", err.Error())
	}
}

func TestLoadCollectorConfig_MissingTLSKeyErrors(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "collector.yaml")
	content := []byte(`
listen_addr: ":9311"
db_path: /var/lib/tasseograph/results.db
tls_cert: /etc/tasseograph/tls/cert.pem
llm_endpoints:
  - url: "https://inference.internal/v1"
    model: "anthropic/haiku-4.5"
    api_key_env: "INTERNAL_LLM_KEY"
`)
	if err := os.WriteFile(configPath, content, 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TASSEOGRAPH_API_KEY", "test-api-key")

	_, err := LoadCollectorConfig(configPath)
	if err == nil {
		t.Fatal("expected error for missing tls_key, got nil")
	}
	if !strings.Contains(err.Error(), "tls_key") {
		t.Errorf("error %q does not mention tls_key", err.Error())
	}
}

func TestLoadCollectorConfig_MissingDBPathErrors(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "collector.yaml")
	content := []byte(`
listen_addr: ":9311"
tls_cert: /etc/tasseograph/tls/cert.pem
tls_key: /etc/tasseograph/tls/key.pem
llm_endpoints:
  - url: "https://inference.internal/v1"
    model: "anthropic/haiku-4.5"
    api_key_env: "INTERNAL_LLM_KEY"
`)
	if err := os.WriteFile(configPath, content, 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TASSEOGRAPH_API_KEY", "test-api-key")

	_, err := LoadCollectorConfig(configPath)
	if err == nil {
		t.Fatal("expected error for missing db_path, got nil")
	}
	if !strings.Contains(err.Error(), "db_path") {
		t.Errorf("error %q does not mention db_path", err.Error())
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
	t.Setenv("TASSEOGRAPH_API_KEY", "test-api-key") // Required for validation

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
