// internal/config/config.go
package config

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// AgentConfig for the host agent
type AgentConfig struct {
	CollectorURL  string        `yaml:"collector_url"`
	PollInterval  time.Duration `yaml:"poll_interval"`
	StateFile     string        `yaml:"state_file"`
	Hostname      string        `yaml:"hostname"`
	TLSSkipVerify bool          `yaml:"tls_skip_verify"`
	APIKey        string        `yaml:"-"` // from env only
}

// LLMEndpoint represents one LLM provider in the fallback chain
type LLMEndpoint struct {
	URL       string `yaml:"url"`
	Model     string `yaml:"model"`
	APIKeyEnv string `yaml:"api_key_env"` // env var name for API key
	APIKey    string `yaml:"-"`           // resolved at load time
}

// CollectorConfig for the central collector
type CollectorConfig struct {
	ListenAddr      string        `yaml:"listen_addr"`
	DBPath          string        `yaml:"db_path"`
	MaxRetries      int           `yaml:"max_retries"`
	MaxPayloadBytes int64         `yaml:"max_payload_bytes"`
	TLSCert         string        `yaml:"tls_cert"`
	TLSKey          string        `yaml:"tls_key"`
	LLMEndpoints    []LLMEndpoint `yaml:"llm_endpoints"` // fallback chain
	APIKey          string        `yaml:"-"`             // agent auth, from env
}

// LoadAgentConfig loads agent config from YAML file with env overrides
func LoadAgentConfig(path string) (*AgentConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg AgentConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	// Env overrides
	if key := os.Getenv("TASSEOGRAPH_API_KEY"); key != "" {
		cfg.APIKey = key
	}
	if hostname := os.Getenv("TASSEOGRAPH_HOSTNAME"); hostname != "" {
		cfg.Hostname = hostname
	}

	// Default hostname to os.Hostname if not set
	if cfg.Hostname == "" {
		cfg.Hostname, _ = os.Hostname()
	}

	return &cfg, nil
}

// LoadCollectorConfig loads collector config from YAML file with env overrides
func LoadCollectorConfig(path string) (*CollectorConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg CollectorConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	// Env overrides
	if key := os.Getenv("TASSEOGRAPH_API_KEY"); key != "" {
		cfg.APIKey = key
	}

	// Resolve API keys for each LLM endpoint from env vars
	for i := range cfg.LLMEndpoints {
		if cfg.LLMEndpoints[i].APIKeyEnv != "" {
			cfg.LLMEndpoints[i].APIKey = os.Getenv(cfg.LLMEndpoints[i].APIKeyEnv)
		}
	}

	return &cfg, nil
}
