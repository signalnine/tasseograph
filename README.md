# Tasseograph

Dmesg anomaly detection via LLM analysis. Reads kernel tea leaves to divine hardware failures before they happen.

## Overview

Tasseograph monitors Linux kernel logs (dmesg) across a fleet of servers, using LLM analysis to identify hardware degradation signals like memory errors, storage failures, and network issues.

```
Hosts (agent) → Collector → LLM inference → SQLite
```

## Components

- **`tasseograph agent`** - Runs on each host, collects dmesg deltas, sends to collector
- **`tasseograph collector`** - Central service, calls LLM API, stores results

## Quick Start

### Build

```bash
make build          # Current platform
make build-linux    # linux/amd64 and linux/arm64
```

### Collector Setup

1. Generate TLS cert:
   ```bash
   openssl req -x509 -newkey rsa:4096 -keyout key.pem -out cert.pem -days 365 -nodes
   ```

2. Create `/etc/tasseograph/collector.yaml`:
   ```yaml
   listen_addr: ":9311"
   db_path: /var/lib/tasseograph/results.db
   tls_cert: /etc/tasseograph/tls/cert.pem
   tls_key: /etc/tasseograph/tls/key.pem
   llm_endpoints:
     - url: "https://api.openai.com/v1"
       model: "gpt-4o-mini"
       api_key_env: "OPENAI_API_KEY"
   ```

3. Run:
   ```bash
   export TASSEOGRAPH_API_KEY="your-shared-secret"
   export OPENAI_API_KEY="your-openai-key"
   ./tasseograph collector -c /etc/tasseograph/collector.yaml
   ```

### Agent Setup

1. Create `/etc/tasseograph/agent.yaml`:
   ```yaml
   collector_url: "https://collector.example.com:9311/ingest"
   poll_interval: 5m
   state_file: /var/lib/tasseograph/last_timestamp
   tls_skip_verify: true  # for self-signed certs
   ```

2. Run (requires root for dmesg):
   ```bash
   export TASSEOGRAPH_API_KEY="your-shared-secret"
   sudo -E ./tasseograph agent -c /etc/tasseograph/agent.yaml
   ```

## LLM Response Format

The LLM analyzes dmesg and returns:

```json
{
  "status": "warning",
  "issues": [
    {
      "severity": "warning",
      "category": "memory",
      "summary": "ECC error detected on DIMM0",
      "evidence": "EDAC MC0: 1 CE memory error"
    }
  ]
}
```

**Status**: `ok`, `warning`, `critical`

**Categories**: `memory`, `storage`, `network`, `thermal`, `driver`

## Querying Results

```bash
# Recent warnings
sqlite3 /var/lib/tasseograph/results.db \
  "SELECT timestamp, hostname, status, issues FROM results WHERE status != 'ok' ORDER BY timestamp DESC LIMIT 20;"

# Count by status
sqlite3 /var/lib/tasseograph/results.db \
  "SELECT status, COUNT(*) FROM results GROUP BY status;"
```

## Configuration

### Agent

| Field | Description | Default |
|-------|-------------|---------|
| `collector_url` | Collector endpoint | required |
| `poll_interval` | How often to check dmesg | `5m` |
| `state_file` | Tracks last-seen timestamp | `/var/lib/tasseograph/last_timestamp` |
| `hostname` | Override hostname | `os.Hostname()` |
| `tls_skip_verify` | Skip TLS verification | `false` |

### Collector

| Field | Description | Default |
|-------|-------------|---------|
| `listen_addr` | Listen address | `:9311` |
| `db_path` | SQLite database path | required |
| `tls_cert` | TLS certificate path | required |
| `tls_key` | TLS key path | required |
| `llm_endpoints` | LLM fallback chain | required |
| `max_payload_bytes` | Max request size | `1048576` (1MB) |

### Environment Variables

- `TASSEOGRAPH_API_KEY` - Shared secret for agent/collector auth (required)
- LLM API keys as specified in `api_key_env` fields

## LLM Fallback Chain

The collector tries LLM endpoints in order. If one fails (502/503/504), it tries the next:

```yaml
llm_endpoints:
  - url: "https://internal-gateway/v1"
    model: "anthropic/claude-3-5-haiku"
    api_key_env: "INTERNAL_KEY"
  - url: "https://api.openai.com/v1"
    model: "gpt-4o-mini"
    api_key_env: "OPENAI_API_KEY"
```

Uses OpenAI-compatible Chat Completions API format, works with most inference gateways.

## License

MIT
