// internal/config/config.go
package config

import (
	"errors"
	"fmt"
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
	RetentionDays   int           `yaml:"retention_days"` // 0 disables pruning
	TLSCert         string        `yaml:"tls_cert"`
	TLSKey          string        `yaml:"tls_key"`
	LLMEndpoints    []LLMEndpoint `yaml:"llm_endpoints"` // fallback chain
	APIKey          string        `yaml:"-"`             // agent auth, from env

	// Email summary digest. Disabled when SummaryInterval == 0.
	SummaryInterval time.Duration `yaml:"summary_interval"`
	AlertErrorRate  float64       `yaml:"alert_error_rate"` // 0..1, fraction of rows in window that mark the digest [CRITICAL]
	SMTPHost        string        `yaml:"smtp_host"`        // host:port
	SMTPFrom        string        `yaml:"smtp_from"`
	SMTPTo          string        `yaml:"smtp_to"`
	SMTPUsername    string        `yaml:"smtp_username"`     // defaults to SMTPFrom when empty
	SMTPPasswordEnv string        `yaml:"smtp_password_env"` // env var name for password
	SMTPStartTLS    bool          `yaml:"smtp_starttls"`
	SMTPPassword    string        `yaml:"-"` // resolved at load time
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

	// Default hostname to os.Hostname if not set. An empty hostname downstream
	// would silently produce unfilterable rows in the collector DB and useless
	// QueryByHostname results, so refuse to start instead of guessing.
	if cfg.Hostname == "" {
		h, err := os.Hostname()
		if err != nil || h == "" {
			return nil, fmt.Errorf("hostname not set in config and os.Hostname() failed: %w", err)
		}
		cfg.Hostname = h
	}

	// Validate required fields
	if cfg.APIKey == "" {
		return nil, errors.New("TASSEOGRAPH_API_KEY environment variable required")
	}
	if cfg.CollectorURL == "" {
		return nil, errors.New("collector_url is required in config")
	}
	if cfg.PollInterval <= 0 {
		return nil, errors.New("poll_interval must be a positive duration (e.g. 5m)")
	}
	if cfg.StateFile == "" {
		return nil, errors.New("state_file is required in config")
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

	// Validate required fields
	if cfg.APIKey == "" {
		return nil, errors.New("TASSEOGRAPH_API_KEY environment variable required")
	}
	if len(cfg.LLMEndpoints) == 0 {
		return nil, errors.New("at least one llm_endpoints entry required")
	}
	if cfg.MaxPayloadBytes < 0 {
		return nil, errors.New("max_payload_bytes must be >= 0 (0 means use default)")
	}
	if cfg.MaxPayloadBytes == 0 {
		cfg.MaxPayloadBytes = 1 << 20 // 1 MB default -- matches deploy/config example
	}
	if cfg.RetentionDays < 0 {
		return nil, errors.New("retention_days must be >= 0 (0 disables pruning)")
	}
	if cfg.DBPath == "" {
		return nil, errors.New("db_path is required in config")
	}
	if cfg.TLSCert == "" {
		return nil, errors.New("tls_cert is required in config")
	}
	if cfg.TLSKey == "" {
		return nil, errors.New("tls_key is required in config")
	}

	// Summary/SMTP: only validate when the feature is turned on. Disabled
	// (interval == 0) is the supported zero-value default; everything else
	// must be fully specified so we fail fast instead of silently never sending.
	if cfg.SummaryInterval < 0 {
		return nil, errors.New("summary_interval must be >= 0 (0 disables digest emails)")
	}
	if cfg.SummaryInterval > 0 {
		if cfg.SummaryInterval < time.Hour {
			return nil, errors.New("summary_interval must be >= 1h when enabled")
		}
		if cfg.SMTPHost == "" {
			return nil, errors.New("smtp_host is required when summary_interval > 0")
		}
		if cfg.SMTPFrom == "" {
			return nil, errors.New("smtp_from is required when summary_interval > 0")
		}
		if cfg.SMTPTo == "" {
			return nil, errors.New("smtp_to is required when summary_interval > 0")
		}
		if cfg.SMTPPasswordEnv != "" {
			cfg.SMTPPassword = os.Getenv(cfg.SMTPPasswordEnv)
		}
		// Default the SMTP username to the from-address. Fastmail, Gmail, and
		// most other providers treat the auth username as the mailbox identifier.
		if cfg.SMTPUsername == "" {
			cfg.SMTPUsername = cfg.SMTPFrom
		}
		if cfg.AlertErrorRate < 0 || cfg.AlertErrorRate > 1 {
			return nil, errors.New("alert_error_rate must be between 0 and 1")
		}
		if cfg.AlertErrorRate == 0 {
			cfg.AlertErrorRate = 0.5 // default: >50% error rate flags the digest as critical
		}
	}

	return &cfg, nil
}
