# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Tasseograph is a dmesg anomaly detection system that uses LLM analysis to identify hardware degradation signals in Linux kernel logs across a fleet of bare metal servers. The name comes from "reading kernel tea leaves to divine the future."

**Current Status**: Phase 1 pilot implemented. Ready for deployment to 20 test hosts.

## Architecture

```
20 hosts (agent) → collector (central) → LLM inference (fallback chain) → SQLite
```

**Components:**
- **`tasseograph agent`** - Runs on each host, collects dmesg deltas, POSTs to collector over HTTPS
- **`tasseograph collector`** - Central service that receives deltas, calls LLM with fallback chain, stores results in SQLite

## Build & Test

```bash
make test          # Run all tests
make build         # Build for current platform
make build-linux   # Build linux/amd64 and linux/arm64
```

Output binaries: `dist/tasseograph-linux-{amd64,arm64}`

## Configuration

**Agent** (`/etc/tasseograph/agent.yaml`):
```yaml
collector_url: "https://collector.internal:9311/ingest"
poll_interval: 5m
state_file: /var/lib/tasseograph/last_timestamp
tls_skip_verify: true  # for self-signed certs
```

**Collector** (`/etc/tasseograph/collector.yaml`):
```yaml
listen_addr: ":9311"
db_path: /var/lib/tasseograph/results.db
tls_cert: /etc/tasseograph/tls/cert.pem
tls_key: /etc/tasseograph/tls/key.pem
llm_endpoints:  # fallback chain - tries in order
  - url: "https://inference.internal/v1"
    model: "anthropic/haiku-4.5"
    api_key_env: "INTERNAL_LLM_KEY"
  - url: "https://api.openai.com/v1"
    model: "gpt-4o-mini"
    api_key_env: "OPENAI_API_KEY"
```

**Required environment variables:**
- `TASSEOGRAPH_API_KEY` - Shared secret for agent-collector auth
- LLM API keys as specified in `api_key_env` fields

## Project Structure

```
cmd/tasseograph/       # CLI entry point
internal/
  agent/               # Host agent (dmesg collection, state tracking)
  collector/           # Central collector (HTTP server, LLM client, SQLite)
  config/              # YAML config loading with env overrides
  protocol/            # Shared types (DmesgDelta, AnalysisResult)
deploy/
  config/              # Example YAML configs
  systemd/             # Service unit files
```

## Key Design Decisions

- **LLM format**: OpenAI-compatible Chat Completions API (`/chat/completions`)
- **Fallback chain**: Multiple LLM endpoints tried in order; fails gracefully if all unavailable
- **Line cap**: Max 500 dmesg lines per request to control LLM costs
- **Locale**: `LC_ALL=C` for consistent dmesg timestamp parsing
- **Storage**: SQLite with WAL mode for concurrent access
- **Transport**: HTTPS with self-signed certs (TLS 1.2 minimum)

## Future Work (Phase 2+)

- Prometheus metrics endpoint
- Alertmanager integration
- Tiered model hierarchy (Haiku → Sonnet → Opus)
- Proper CA-signed TLS certificates
- High availability for collector

## Reference

- Design doc: `docs/plans/2026-02-03-phase1-pilot-design.md`
- Full specification: `tasseograph.md`
