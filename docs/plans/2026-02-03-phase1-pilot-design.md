# Tasseograph Phase 1 Pilot Design

*Design finalized: 2026-02-03*

## Overview

Tasseograph Phase 1 is a pilot system to validate LLM-based dmesg anomaly detection across 20 hosts. The goal is to prove the concept works before scaling to the full 600-host fleet.

## Architecture

```
┌─────────────────┐         ┌─────────────────┐         ┌─────────────────┐
│  Host (×20)     │  HTTPS  │    Collector    │  HTTPS  │  LLM Inference  │
│  agent process  │────────▶│  single server  │────────▶│  (Haiku 4.5)    │
│  (every 5 min)  │  POST   │  (port 9311)    │         │                 │
└─────────────────┘         └────────┬────────┘         └─────────────────┘
                                     │
                                     ▼
                            ┌─────────────────┐
                            │     SQLite      │
                            │  (results.db)   │
                            └─────────────────┘
```

**Components:**
- **`tasseograph agent`** - Runs on each host, collects dmesg deltas, POSTs to the collector over HTTPS
- **`tasseograph collector`** - Central service that receives deltas, calls LLM inference, stores results in SQLite

**Key decisions:**
- Single Go binary with subcommands for both components
- Agent tracks last-seen dmesg timestamp locally, sends only new lines
- No batching - one API call per host per push (simplicity for pilot)
- SQLite storage for results (handles concurrent writes, easy querying)
- Log-only output for Phase 1 - manual review to establish baseline
- Shared secret auth between agent and collector
- HTTPS with self-signed cert for transport encryption
- Payload size limit of 1MB per request

## Agent Component

**`tasseograph agent`** runs on each of the 20 pilot hosts as a systemd service.

**Responsibilities:**
- Parse dmesg output with timestamps (`dmesg -T`)
- Track last-seen timestamp in a state file (`/var/lib/tasseograph/last_timestamp`)
- Extract only new lines since last run
- POST delta to collector with hostname identifier
- Update state file on successful send

**Configuration** (`/etc/tasseograph/agent.yaml`):
```yaml
collector_url: "https://collector.internal:9311/ingest"
poll_interval: 5m
state_file: /var/lib/tasseograph/last_timestamp
hostname: ""  # defaults to os.Hostname()
tls_skip_verify: true  # for self-signed certs during pilot
```

Environment override for the shared secret: `TASSEOGRAPH_API_KEY`

**HTTP payload:**
```json
{
  "hostname": "web-prod-01",
  "timestamp": "2026-02-03T12:30:00Z",
  "lines": [
    "[Mon Feb  3 12:25:01 2026] EXT4-fs warning: mounted filesystem with ordered data mode",
    "[Mon Feb  3 12:28:33 2026] EDAC MC0: 1 CE on DIMM0"
  ]
}
```

Systemd unit runs the agent in a loop with the configured interval. On failure, it logs and retries next interval.

## Collector Component

**`tasseograph collector`** runs on a single server, receives deltas, and calls the LLM inference.

**Responsibilities:**
- HTTPS server listening for POST `/ingest` on port 9311
- Validate shared secret from `Authorization: Bearer <token>` header
- Enforce 1MB max payload size
- For each incoming delta, call Haiku 4.5 with the system prompt
- Parse JSON response (status, issues array)
- Store results in SQLite database
- Retry API calls up to 3 times with exponential backoff

**Configuration** (`/etc/tasseograph/collector.yaml`):
```yaml
listen_addr: ":9311"
db_path: /var/lib/tasseograph/results.db
max_retries: 3
max_payload_bytes: 1048576  # 1MB
tls_cert: /etc/tasseograph/tls/cert.pem
tls_key: /etc/tasseograph/tls/key.pem

# LLM inference endpoint (internal service or public API)
llm_base_url: "https://inference.internal/v1"
llm_model: "claude-3-5-haiku-20241022"
```

Environment overrides:
- `TASSEOGRAPH_API_KEY` - shared secret for agent auth
- `TASSEOGRAPH_LLM_API_KEY` - inference service API key

**SQLite schema:**
```sql
CREATE TABLE results (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp TEXT NOT NULL,
    hostname TEXT NOT NULL,
    status TEXT NOT NULL,  -- 'ok', 'warning', 'critical', 'error'
    issues TEXT,           -- JSON array of issues
    raw_dmesg TEXT,        -- original dmesg lines for debugging
    api_latency_ms INTEGER,
    created_at TEXT DEFAULT (datetime('now'))
);

CREATE INDEX idx_results_hostname ON results(hostname);
CREATE INDEX idx_results_status ON results(status);
CREATE INDEX idx_results_timestamp ON results(timestamp);
```

For `status: ok` responses, store a minimal record (hostname, timestamp, status) to track that the check ran.

## Project Structure

```
tasseograph/
├── cmd/
│   └── tasseograph/
│       └── main.go           # CLI entry point, subcommand routing
├── internal/
│   ├── agent/
│   │   ├── agent.go          # Main agent loop
│   │   ├── dmesg.go          # dmesg parsing, timestamp extraction
│   │   └── state.go          # State file read/write
│   ├── collector/
│   │   ├── collector.go      # Main collector server
│   │   ├── handler.go        # HTTP handler for /ingest
│   │   ├── llm.go            # LLM inference client (internal or public API)
│   │   └── db.go             # SQLite operations
│   ├── config/
│   │   └── config.go         # YAML loading, env var overrides
│   └── protocol/
│       └── types.go          # Shared types (DmesgDelta, AnalysisResult)
├── deploy/
│   ├── ansible/              # Ansible playbook for deployment
│   ├── systemd/
│   │   ├── tasseograph-agent.service
│   │   └── tasseograph-collector.service
│   └── config/
│       ├── agent.yaml.example
│       └── collector.yaml.example
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

**Build:**
- `make build` - builds for current platform
- `make build-linux` - builds both `linux/amd64` and `linux/arm64` binaries
- Output: `dist/tasseograph-linux-amd64` and `dist/tasseograph-linux-arm64`

## Deployment

**Collector deployment:**
- Single server (VM or existing ops host)
- Systemd service: `tasseograph-collector.service`
- Generate self-signed TLS cert: `openssl req -x509 -newkey rsa:4096 -keyout key.pem -out cert.pem -days 365 -nodes`
- Firewall: allow inbound on port 9311 from pilot hosts

**Agent deployment via Ansible:**
1. Copy correct binary based on `ansible_architecture`
2. Create `/etc/tasseograph/agent.yaml` from template
3. Create `/var/lib/tasseograph/` for state file
4. Install systemd unit, enable and start service

**Pilot host selection:**
- Pick 20 hosts across different roles/hardware types
- Include some known "noisy" hosts if available
- Mix of amd64 and arm64 to validate both builds

## Operations

**Day-to-day during pilot:**
```bash
# View recent warnings/criticals
sqlite3 /var/lib/tasseograph/results.db \
  "SELECT timestamp, hostname, status, issues FROM results WHERE status != 'ok' ORDER BY timestamp DESC LIMIT 20;"

# Count by status
sqlite3 /var/lib/tasseograph/results.db \
  "SELECT status, COUNT(*) FROM results GROUP BY status;"

# Check specific host
sqlite3 /var/lib/tasseograph/results.db \
  "SELECT * FROM results WHERE hostname = 'web-prod-01' ORDER BY timestamp DESC LIMIT 10;"

# Export to JSON for analysis
sqlite3 -json /var/lib/tasseograph/results.db \
  "SELECT * FROM results WHERE status = 'warning';" > warnings.json
```

- Check agent health: `systemctl status tasseograph-agent` on hosts
- Monitor errors: `sqlite3 results.db "SELECT COUNT(*) FROM results WHERE status = 'error';"`

**Success criteria for Phase 1:**
- Agents successfully reporting from all 20 hosts
- Collector processing requests without errors
- At least one week of logged results to analyze
- Initial assessment of false positive rate

## Error Handling

**Agent failure modes:**

| Scenario | Behavior |
|----------|----------|
| Collector unreachable | Log error, retry next interval; state file NOT updated (will resend same delta) |
| Empty dmesg delta | Skip API call, log debug message, update timestamp |
| State file missing/corrupt | Start fresh from current dmesg timestamp |
| dmesg command fails | Log error, retry next interval |
| Payload exceeds 1MB | Truncate oldest lines to fit, log warning |

**Collector failure modes:**

| Scenario | Behavior |
|----------|----------|
| LLM inference down | Retry 3x with backoff (1s, 2s, 4s), then store error record and return 502 to agent |
| LLM rate limited | Respect `Retry-After` header, return 503 to agent |
| Invalid API response | Store raw response in DB for debugging, return 500 |
| Malformed agent payload | Return 400, log details |
| Auth failure | Return 401, don't log payload contents |
| Payload too large | Return 413 Request Entity Too Large |

## Out of Scope for Phase 1

- High availability (single collector is fine for 20 hosts)
- Persistent queue for failed requests
- Alerting/paging (log-only, manual review)
- Rate limiting at the collector
- Prometheus metrics endpoint
- Batching multiple hosts per API call
- Proper CA-signed TLS certificates (self-signed is fine for pilot)

These will be addressed in Phase 3 when scaling to production.
