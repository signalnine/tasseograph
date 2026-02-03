# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Tasseograph is a dmesg anomaly detection system that uses LLM analysis (Anthropic's Haiku model) to identify hardware degradation signals in Linux kernel logs across a fleet of 600+ bare metal servers. The name comes from "reading kernel tea leaves to divine the future."

**Current Status**: Planning/specification phase. No implementation code exists yet.

## Planned Architecture

```
600 hosts (dmesg -w) → collector (central) → Haiku 4.5 (Anthropic) → Prometheus/Alertmanager
```

Key components to implement:
- **Collection agent**: Extracts dmesg deltas from hosts, tracks last-seen timestamps
- **Central collector**: Batches and sends to Anthropic API
- **Metrics exporter**: Publishes to Prometheus (`dmesg_llm_issues_total`, `dmesg_llm_last_check_timestamp`, `dmesg_llm_api_errors_total`)

## Implementation Language

Go (indicated by .gitignore patterns for Go binaries and test artifacts).

## Key Design Decisions

- **Model**: Haiku 4.5 for cost efficiency at scale (~$1,000-1,500/month with optimizations)
- **Polling interval**: 5 minutes (adaptive: 15-30 min for healthy hosts)
- **API response schema**: JSON with `status` (ok/warning/critical) and `issues[]` array containing severity, category, summary, evidence
- **Categories**: memory, storage, network, thermal, driver
- **Integration**: Existing config management (salt/ansible) for agent deployment

## Future Architecture

Tiered model hierarchy for fleet-scale intelligence:
- Host Haiku (600 hosts, 5-min chunks)
- Cluster Sonnet (~200 hosts, rolling window, filters noise)
- Cluster Opus (root cause analysis, hours/days)
- Fleet Opus (cross-cluster patterns, days/weeks)

## Reference

Full specification: `tasseograph.md`
