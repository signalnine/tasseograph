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
make build         # Build for current platform -> dist/tasseograph
make build-linux   # Build linux/amd64 and linux/arm64
make test          # go test ./...
make test-cover    # tests with coverage profile -> coverage.out
make lint          # golangci-lint if present, else go vet ./...
make fmt           # go fmt ./...
make clean         # remove dist/ and coverage.out
```

Run a single test:

```bash
go test ./internal/collector -run TestIngestHandler_Auth
go test ./internal/agent    -run TestParseDmesgTimestamp -v
```

Cross-compiled output: `dist/tasseograph-linux-{amd64,arm64}`. SQLite is pure-Go (`modernc.org/sqlite`), so no CGO is required for cross builds.

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
max_retries: 0          # extra in-endpoint retries on transient failures (5xx/408/429/conn)
max_payload_bytes: 0    # 0 -> 1 MiB default
retention_days: 0       # 0 disables the background pruner
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

- **LLM format**: OpenAI-compatible Chat Completions API (`/chat/completions`).
- **Fallback chain**: Endpoints tried in order. Only *transient* errors (5xx/408/429, connection failures, timeouts) trigger fallback; parse errors and 4xx propagate immediately. `max_retries` adds in-endpoint retries before falling through. All-fail returns `ErrLLMUnavailable`, which the handler stores as status `llm_unavailable` (raw dmesg is preserved either way).
- **Markdown-fenced JSON**: `stripCodeFence` in `internal/collector/llm.go` tolerates models that wrap JSON in ```` ```json ``` ```` despite the prompt.
- **Line cap**: 500 dmesg lines per request (`MaxLines` in `internal/agent/dmesg.go`); the most recent are kept on truncation.
- **Locale**: agent strips inherited `LC_ALL`/`LANG`/`LC_TIME` from the dmesg env before forcing `LC_ALL=C` so libc envp precedence cannot resurrect localized month names.
- **Timestamps**: dmesg `-T` is parsed with `time.Local` (host's local zone), state file uses RFC3339Nano written atomically via `tmp` + `rename`.
- **Clock-skew window**: collector handler stores `delta.Timestamp` from the agent unless it's zero, >5m in the future, or >24h in the past (then falls back to collector clock).
- **Hostname**: rejected at both ends -- agent refuses to start with empty hostname (`internal/config/config.go`); collector handler returns 400 if `delta.Hostname == ""` (so a leaked bearer token can't write unfilterable rows).
- **Auth**: shared `TASSEOGRAPH_API_KEY` bearer token between agent and collector; checked before payload parsing.
- **Storage**: SQLite (pure-Go `modernc.org/sqlite`) with WAL mode; daily background pruner driven by `retention_days`.
- **Transport**: HTTPS with self-signed certs (TLS 1.2 minimum). `Server.RunAndGetAddr` exists for tests that bind port 0.

## Future Work (Phase 2+)

- Prometheus metrics endpoint
- Alertmanager integration
- Tiered model hierarchy (Haiku → Sonnet → Opus)
- Proper CA-signed TLS certificates
- High availability for collector

## Reference

- Design doc: `docs/plans/2026-02-03-phase1-pilot-design.md`
- Full specification: `tasseograph.md`
