// internal/agent/dmesg.go
package agent

import (
	"errors"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var timestampRe = regexp.MustCompile(`^\[([A-Za-z]{3} [A-Za-z]{3} [ \d]\d \d{2}:\d{2}:\d{2} \d{4})\]`)

// ParseDmesgTimestamp extracts the timestamp from a dmesg -T line.
// dmesg -T emits timestamps in the host's local time zone, so parse them
// in time.Local rather than UTC.
func ParseDmesgTimestamp(line string) (time.Time, error) {
	matches := timestampRe.FindStringSubmatch(line)
	if len(matches) < 2 {
		return time.Time{}, errors.New("no timestamp found")
	}

	// Parse: "Mon Feb  3 12:25:01 2026"
	return time.ParseInLocation("Mon Jan _2 15:04:05 2006", matches[1], time.Local)
}

// FilterNewLines returns lines newer than lastSeen and the latest timestamp
func FilterNewLines(lines []string, lastSeen time.Time) ([]string, time.Time) {
	var filtered []string
	var latest time.Time

	for _, line := range lines {
		ts, err := ParseDmesgTimestamp(line)
		if err != nil {
			continue // skip lines without parseable timestamps
		}

		if ts.After(lastSeen) {
			filtered = append(filtered, line)
			if ts.After(latest) {
				latest = ts
			}
		}
	}

	return filtered, latest
}

// MaxLines caps how many lines we send to the LLM to control costs
const MaxLines = 500

// GetDmesg runs dmesg -T and returns the output lines
// Uses LC_ALL=C for consistent timestamp format across locales
func GetDmesg() ([]string, error) {
	cmd := exec.Command("dmesg", "-T")
	cmd.Env = cLocaleEnv()
	out, err := cmd.Output()
	if err != nil {
		return nil, errors.New("dmesg command failed (check permissions or CAP_SYSLOG): " + err.Error())
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	return lines, nil
}

// cLocaleEnv returns the parent environment with any inherited locale
// variables stripped, plus LC_ALL=C. Without this filter, a parent
// LC_ALL/LANG/LC_TIME would produce duplicate envp keys whose precedence
// is implementation-defined; on a libc that picks first-wins we'd see
// localized month names and timestamp parsing would silently fail.
func cLocaleEnv() []string {
	env := os.Environ()
	filtered := env[:0]
	for _, e := range env {
		if strings.HasPrefix(e, "LC_ALL=") || strings.HasPrefix(e, "LANG=") || strings.HasPrefix(e, "LC_TIME=") {
			continue
		}
		filtered = append(filtered, e)
	}
	return append(filtered, "LC_ALL=C")
}

// CapLines returns at most MaxLines from the end of the slice (most recent)
// Returns true if lines were truncated
func CapLines(lines []string) ([]string, bool) {
	if len(lines) <= MaxLines {
		return lines, false
	}
	// Keep the most recent lines (end of slice)
	return lines[len(lines)-MaxLines:], true
}
