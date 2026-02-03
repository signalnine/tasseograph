---
apple_notes_id: x-coredata://975E8692-B6B0-48E2-97C5-0E5E6F550F8B/ICNote/p1379
---

# Tasseograph: dmesg Anomaly Detection via LLM

*Reading kernel tea leaves to divine the future*

Created: 2026-01-31 Status: Idea Priority: Medium
Domain: tasseograph.net (repurposed from Clearsky project)

> **Note:** Originally planned tasseograph.net for the Clearsky behavioral exhaust correlation project, but repurposing for this since it's more practical and directly applicable to work.

**Problem**

Prometheus/Grafana tells us when things are on fire. Nothing tells us when things are developing hairline cracks. Hardware degradation signals live in dmesg but require human interpretation—MCEs, EDAC corrections, NVMe controller warnings, PCIe link retraining, thermal events. These often precede failures by hours or days.

**Proposal**

Feed dmesg deltas to Haiku 4.5 periodically. Ask it to flag anything that looks like hardware degradation or impending failure. Surface results to ops.

**Architecture**

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│  600 hosts  │────▶│  collector  │────▶│  Haiku 4.5  │
│  (dmesg -w) │     │  (central)  │     │ (Anthropic) │
└─────────────┘     └─────────────┘     └─────────────┘
                                               │
                                               ▼
                                        ┌─────────────┐
                                        │ Prometheus  │
                                        │ /Alertmgr   │
                                        └─────────────┘
```

**Collection**

Pull dmesg deltas via existing config management (salt/ansible) or lightweight agent. Each host tracks last-seen timestamp, sends only new messages. Batch hosts into single API calls where practical.

**Analysis**

```python
SYSTEM_PROMPT = """You are a Linux kernel expert reviewing dmesg output
from bare metal servers. Flag messages indicating:

- Memory errors (MCE, EDAC, ECC corrections trending up)
- Storage degradation (NVMe controller warnings, SMART predictive, I/O errors)
- Network issues (link flapping, PCIe retraining, firmware errors)
- Thermal events (throttling, temperature warnings)
- Driver instability (repeated initialization, timeout patterns)

Ignore routine noise: ACPI info, systemd lifecycle, USB enumeration,
normal driver init.

Respond with JSON:
{
  "status": "ok" | "warning" | "critical",
  "issues": [
    {
      "severity": "warning" | "critical",
      "category": "memory" | "storage" | "network" | "thermal" | "driver",
      "summary": "brief description",
      "evidence": "relevant log snippet"
    }
  ]
}

If nothing notable, return {"status": "ok", "issues": []}"""
```

**Output**

- status: ok → no action
- status: warning → increment Prometheus counter, maybe page if sustained
- status: critical → immediate alert via existing Alertmanager

Expose metrics:

- dmesg_llm_issues_total{host, category, severity}
- dmesg_llm_last_check_timestamp{host}
- dmesg_llm_api_errors_total

**Cost Estimate**

Assumptions: 3KB average delta per host, 5-minute intervals, 600 hosts.

| Metric | Value |
|--------|-------|
| API calls/day | 172,800 (600 hosts × 288 intervals) |
| Input tokens/day | ~103M (600 × 3KB × 288) |
| Output tokens/day | ~1.7M (600 × 100 tokens × 288) |
| Cost/day (Haiku) | ~$103 input + $4.25 output = ~$107 |
| Cost/month | ~$3,200 |

At 1500 hosts: ~$7,750/month.

**Cost Reduction Levers**

1.  **Adaptive polling**: 15-30 min for healthy hosts, 5 min only after anomaly
2.  **Local pre-filter**: Skip API call if dmesg contains only known-boring patterns
3.  **Batching**: Multiple hosts per API call (amortize prompt overhead)
4.  **Delta compression**: Strip timestamps, dedupe repeated messages

Conservative estimate with optimizations: 60-70% reduction → $1,000-1,500/month.

**Failure Modes**

| Mode | Mitigation |
|------|------------|
| API unavailable | Queue locally, retry with backoff |
| False positives | Tune prompt, add few-shot examples |
| False negatives | Review missed incidents, update prompt |
| Cost overrun | Hard caps via API budget, adaptive polling |

**Success Criteria**

1.  Catches ≥1 hardware issue before it causes an outage (within first 90 days)
2.  False positive rate <5 alerts/day across fleet after tuning
3.  Ops team finds it useful enough to keep running

**Implementation Phases**

**Phase 1: Prove it works (1-2 days)**

- Pick 20 hosts
- Cron job pulls dmesg, calls API, logs results
- Run for a week, review what it surfaces

**Phase 2: Tune (1 week)**

- Iterate on system prompt based on Phase 1 noise
- Add few-shot examples of real issues from historical incidents
- Establish baseline false positive rate

**Phase 3: Production (1 week)**

- Central collector service
- Prometheus integration
- Alertmanager routing
- Dashboard for fleet-wide view

**Open Questions**

- Is Haiku 4.5 sufficient, or do we need Sonnet for complex patterns?
- Should we escalate ambiguous cases to a larger model (tiered approach)?
- Worth building RAG over historical incidents for few-shot retrieval?
- Journal/syslog integration, or dmesg only for v1?

**Prior Art**

LogPrompt (2023) achieved good results with zero-shot prompting on system logs. Key finding: prompt engineering matters significantly—naive prompts underperform by ~50% vs. well-crafted ones. LogLLM (2024) tested on BGL/Thunderbird HPC datasets with strong results. No existing work on this exact use case (cheap model, fleet scale, hardware degradation focus).

**Next Steps**

1.  [ ] Identify 20 hosts for Phase 1 pilot
2.  [ ] Write collection script (dmesg delta extraction)
3.  [ ] Set up API integration with Haiku
4.  [ ] Run pilot for 1 week
5.  [ ] Review results, tune prompt

---

## Future Roadmap: Hierarchical Fleet Intelligence

The v1 architecture (flat Haiku triage) could evolve into a tiered attention system:

```
                    ┌─────────────────┐
                    │  FLEET OPUS     │
                    │  Cross-cluster  │
                    │  patterns       │
                    └────────┬────────┘
                             │
         ┌───────────────────┼───────────────────┐
         ▼                   ▼                   ▼
   ┌───────────┐       ┌───────────┐       ┌───────────┐
   │  CLUSTER  │       │  CLUSTER  │       │  CLUSTER  │
   │   OPUS    │       │   OPUS    │       │   OPUS    │
   │ Root cause│       │           │       │           │
   └─────┬─────┘       └─────┬─────┘       └─────┬─────┘
         │                   │                   │
         ▼                   ▼                   ▼
   ┌───────────┐       ┌───────────┐       ┌───────────┐
   │  CLUSTER  │       │  CLUSTER  │       │  CLUSTER  │
   │  SONNET   │       │  SONNET   │       │  SONNET   │
   │ Real vs   │       │           │       │           │
   │ noise     │       │           │       │           │
   └─────┬─────┘       └─────┬─────┘       └─────┬─────┘
         │                   │                   │
    ┌────┴────┐         ┌────┴────┐         ┌────┴────┐
    ▼    ▼    ▼         ▼    ▼    ▼         ▼    ▼    ▼
  ┌───┐┌───┐┌───┐     ┌───┐┌───┐┌───┐     ┌───┐┌───┐┌───┐
  │ H ││ H ││ H │     │ H ││ H ││ H │     │ H ││ H ││ H │
  └───┘└───┘└───┘     └───┘└───┘└───┘     └───┘└───┘└───┘
   host host host      host host host      host host host
```

### Layer Responsibilities

| Layer | Scope | Window | Job |
|-------|-------|--------|-----|
| **Host Haiku** | 1 host | 5 min | "Anything weird in this dmesg chunk?" |
| **Cluster Sonnet** | ~200 hosts | Rolling | "Which Haiku flags are real vs noise?" |
| **Cluster Opus** | ~200 hosts | Hours/days | "Root cause? What's degrading?" |
| **Fleet Opus** | All clusters | Days/weeks | "Global patterns across clusters?" |

### What Each Layer Catches

**Host Haiku** (junior devops intuition):
- Obvious errors: "FAILING", "error", "warning"
- Known bad patterns from prompt
- Admits uncertainty → escalates to Sonnet

**Cluster Sonnet** (mid-level SRE):
- Filters Haiku false positives with context
- Correlates across hosts: "3 hosts in same rack"
- Understands what's normal for this cluster

**Cluster Opus** (senior SRE):
- Root cause analysis across layers
- "Disk issue → k8s scheduler behavior → service impact"
- Degradation trends over time
- Remediation suggestions

**Fleet Opus** (staff/principal):
- Cross-cluster patterns: "Same NIC errors in 3 clusters → bad driver"
- Hardware cohort analysis: "This SKU failing at 2x rate"
- Predictive: "This pattern precedes that failure by 48 hours"

### Integration with boxctl

Could extend beyond dmesg to full infrastructure state:
- boxctl scripts as collection layer (baremetal + k8s)
- Structured output feeds into same tiered analysis
- "What's wrong with this host?" → run relevant scripts → LLM synthesis

### Key Insight

This is essentially an AI SRE with ground truth about infrastructure state. The structured output schema at each layer is what makes aggregation tractable—you can't feed raw text from 600 hosts to an LLM, but structured JSON with consistent schemas works.
