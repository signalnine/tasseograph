// internal/collector/summary.go
package collector

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/signalnine/tasseograph/internal/protocol"
)

// SummaryWindow aggregates everything the digest needs about a time window.
// Populated by (*DB).SummaryWindow; consumed by BuildSummary.
type SummaryWindow struct {
	Since, Until time.Time
	Total        int
	StatusCounts map[string]int
	Hostnames    []HostnameStat
	TopIssues    []IssueCount
	LatencyAvgMs int64
	LatencyMaxMs int64
	Criticals    []protocol.StoredResult
}

type HostnameStat struct {
	Hostname string
	Total    int
	LastSeen time.Time
}

type IssueCount struct {
	Summary string
	Count   int
}

// pipelineErrorStatuses are the result-row statuses that mean "the LLM didn't
// give us a usable analysis." If these dominate the window, the digest is
// flagged critical even when zero hardware issues were found.
var pipelineErrorStatuses = map[string]bool{
	"error":           true,
	"llm_unavailable": true,
}

// SummaryWindow runs one query per dimension instead of trying to cram
// everything into a single SQL statement. Volumes are small (one row per
// agent poll per host), so the I/O is negligible and the code stays readable.
func (d *DB) SummaryWindow(since, until time.Time) (*SummaryWindow, error) {
	w := &SummaryWindow{
		Since:        since,
		Until:        until,
		StatusCounts: map[string]int{},
	}
	sinceStr := since.UTC().Format(time.RFC3339)
	untilStr := until.UTC().Format(time.RFC3339)

	// Total + status counts.
	rows, err := d.db.Query(
		`SELECT status, COUNT(*) FROM results
		 WHERE timestamp >= ? AND timestamp <= ?
		 GROUP BY status`,
		sinceStr, untilStr,
	)
	if err != nil {
		return nil, fmt.Errorf("status counts: %w", err)
	}
	for rows.Next() {
		var st string
		var n int
		if err := rows.Scan(&st, &n); err != nil {
			rows.Close()
			return nil, err
		}
		w.StatusCounts[st] = n
		w.Total += n
	}
	rows.Close()

	// Per-hostname activity. Useful both for fleet visibility and for spotting
	// agents that have gone silent (low Total relative to the rest).
	rows, err = d.db.Query(
		`SELECT hostname, COUNT(*) AS n, MAX(timestamp) AS last_seen
		 FROM results
		 WHERE timestamp >= ? AND timestamp <= ?
		 GROUP BY hostname
		 ORDER BY n DESC`,
		sinceStr, untilStr,
	)
	if err != nil {
		return nil, fmt.Errorf("hostname stats: %w", err)
	}
	for rows.Next() {
		var hs HostnameStat
		var lastSeen string
		if err := rows.Scan(&hs.Hostname, &hs.Total, &lastSeen); err != nil {
			rows.Close()
			return nil, err
		}
		hs.LastSeen, _ = time.Parse(time.RFC3339, lastSeen)
		w.Hostnames = append(w.Hostnames, hs)
	}
	rows.Close()

	// Latency stats. Restrict to successful analyses; error rows have
	// latency too but it's noise for "how fast is the LLM."
	var avg, maxLat sql.NullFloat64
	err = d.db.QueryRow(
		`SELECT AVG(api_latency_ms), MAX(api_latency_ms) FROM results
		 WHERE timestamp >= ? AND timestamp <= ?
		   AND api_latency_ms > 0
		   AND status IN ('ok','warning','critical')`,
		sinceStr, untilStr,
	).Scan(&avg, &maxLat)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("latency stats: %w", err)
	}
	if avg.Valid {
		w.LatencyAvgMs = int64(avg.Float64)
	}
	if maxLat.Valid {
		w.LatencyMaxMs = int64(maxLat.Float64)
	}

	// Issue summaries -- exploded out of the JSON column and grouped. The
	// json_each table-valued function ships with modernc.org/sqlite by default.
	rows, err = d.db.Query(
		`SELECT json_extract(value, '$.summary') AS summary, COUNT(*) AS n
		 FROM results, json_each(results.issues)
		 WHERE timestamp >= ? AND timestamp <= ?
		   AND status IN ('warning','critical')
		   AND issues IS NOT NULL AND issues != '' AND issues != 'null'
		 GROUP BY summary
		 ORDER BY n DESC
		 LIMIT 20`,
		sinceStr, untilStr,
	)
	if err != nil {
		return nil, fmt.Errorf("issue counts: %w", err)
	}
	for rows.Next() {
		var ic IssueCount
		if err := rows.Scan(&ic.Summary, &ic.Count); err != nil {
			rows.Close()
			return nil, err
		}
		w.TopIssues = append(w.TopIssues, ic)
	}
	rows.Close()

	// Full criticals (small N, we want all of them in the email body).
	rows, err = d.db.Query(
		`SELECT `+resultColumns+`
		 FROM results
		 WHERE timestamp >= ? AND timestamp <= ?
		   AND status = 'critical'
		 ORDER BY timestamp DESC`,
		sinceStr, untilStr,
	)
	if err != nil {
		return nil, fmt.Errorf("criticals: %w", err)
	}
	w.Criticals, err = scanResults(rows)
	if err != nil {
		return nil, err
	}

	return w, nil
}

// BuildSummary renders an email subject and body for the window between
// `since` and `now`. alertErrorRate is the threshold (0..1) at which
// pipeline-error dominance escalates the digest to [CRITICAL].
func BuildSummary(db *DB, since, now time.Time, alertErrorRate float64) (subject, body string, err error) {
	w, err := db.SummaryWindow(since, now)
	if err != nil {
		return "", "", err
	}

	criticalCount := w.StatusCounts["critical"]
	warningCount := w.StatusCounts["warning"]
	errorCount := 0
	for st := range w.StatusCounts {
		if pipelineErrorStatuses[st] {
			errorCount += w.StatusCounts[st]
		}
	}
	errorRate := 0.0
	if w.Total > 0 {
		errorRate = float64(errorCount) / float64(w.Total)
	}

	severity := "OK"
	headline := "no issues"
	switch {
	case w.Total == 0:
		severity = "CRITICAL"
		headline = "no data ingested -- pipeline silent"
	case criticalCount > 0:
		severity = "CRITICAL"
		headline = fmt.Sprintf("%d critical, %d warning", criticalCount, warningCount)
	case errorRate >= alertErrorRate && errorCount > 0:
		severity = "CRITICAL"
		headline = fmt.Sprintf("LLM error rate %.0f%% (%d/%d) -- check collector logs",
			errorRate*100, errorCount, w.Total)
	case warningCount > 0:
		severity = "WARN"
		headline = fmt.Sprintf("%d warning", warningCount)
	}

	subject = fmt.Sprintf("[%s] tasseograph %s -- %s",
		severity, formatWindow(since, now), headline)

	body = renderBody(w, errorRate)
	return subject, body, nil
}

// formatWindow renders the digest window concisely. A 24h window becomes
// "24h ending 2026-05-12 22:00 UTC"; other windows fall back to a date range.
func formatWindow(since, until time.Time) string {
	dur := until.Sub(since)
	if dur > 0 && dur <= 24*time.Hour+time.Minute && dur >= 24*time.Hour-time.Minute {
		return fmt.Sprintf("24h ending %s", until.UTC().Format("2006-01-02 15:04 UTC"))
	}
	if dur > 0 && dur <= 7*24*time.Hour+time.Minute && dur >= 7*24*time.Hour-time.Minute {
		return fmt.Sprintf("7d ending %s", until.UTC().Format("2006-01-02 15:04 UTC"))
	}
	return fmt.Sprintf("%s to %s",
		since.UTC().Format("2006-01-02 15:04"),
		until.UTC().Format("2006-01-02 15:04 UTC"))
}

func renderBody(w *SummaryWindow, errorRate float64) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "Window: %s to %s (UTC)\n",
		w.Since.UTC().Format(time.RFC3339),
		w.Until.UTC().Format(time.RFC3339))
	fmt.Fprintf(&sb, "Total rows: %d\n", w.Total)
	if w.Total == 0 {
		sb.WriteString("\nNo data ingested in this window. Either no agents are reporting, ")
		sb.WriteString("or the collector is rejecting their POSTs. Check `systemctl status ")
		sb.WriteString("tasseograph-collector tasseograph-agent` on each host.\n")
	}
	sb.WriteString("\n")

	// Status mix, sorted by count desc for readability.
	if len(w.StatusCounts) > 0 {
		sb.WriteString("Status mix:\n")
		type kv struct {
			k string
			v int
		}
		mix := make([]kv, 0, len(w.StatusCounts))
		for k, v := range w.StatusCounts {
			mix = append(mix, kv{k, v})
		}
		sort.Slice(mix, func(i, j int) bool { return mix[i].v > mix[j].v })
		for _, e := range mix {
			fmt.Fprintf(&sb, "  %-18s %d\n", e.k, e.v)
		}
		sb.WriteString("\n")
	}

	// Pipeline-health callout: surface this near the top so a 100%-error
	// window doesn't get buried below a "0 issues found" lede.
	if errorRate > 0 {
		fmt.Fprintf(&sb, "LLM error rate: %.1f%%\n", errorRate*100)
		if errorRate >= 0.5 {
			sb.WriteString("  -> check `journalctl -u tasseograph-collector` -- the LLM endpoint is likely broken.\n")
		}
		sb.WriteString("\n")
	}

	if w.LatencyAvgMs > 0 {
		fmt.Fprintf(&sb, "LLM latency: avg=%dms max=%dms\n\n", w.LatencyAvgMs, w.LatencyMaxMs)
	}

	if len(w.Criticals) > 0 {
		sb.WriteString("CRITICAL events:\n")
		for _, r := range w.Criticals {
			fmt.Fprintf(&sb, "  %s  %s\n", r.Timestamp.UTC().Format(time.RFC3339), r.Hostname)
			for _, iss := range r.Issues {
				fmt.Fprintf(&sb, "    - %s\n      evidence: %s\n", iss.Summary, truncate(iss.Evidence, 200))
			}
		}
		sb.WriteString("\n")
	}

	if len(w.TopIssues) > 0 {
		sb.WriteString("Top issue summaries (warning + critical):\n")
		for _, ic := range w.TopIssues {
			fmt.Fprintf(&sb, "  %3d  %s\n", ic.Count, truncate(ic.Summary, 100))
		}
		sb.WriteString("\n")
	}

	if len(w.Hostnames) > 0 {
		sb.WriteString("Per-host activity:\n")
		for _, h := range w.Hostnames {
			fmt.Fprintf(&sb, "  %-32s rows=%-5d last_seen=%s\n",
				h.Hostname, h.Total, h.LastSeen.UTC().Format(time.RFC3339))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}

