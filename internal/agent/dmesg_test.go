// internal/agent/dmesg_test.go
package agent

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestParseDmesgTimestamp(t *testing.T) {
	tests := []struct {
		line    string
		wantErr bool
	}{
		{"[Mon Feb  3 12:25:01 2026] EXT4-fs warning: test", false},
		{"[Tue Jan 14 09:05:33 2026] kernel: test message", false},
		{"no timestamp here", true},
		{"", true},
	}

	for _, tt := range tests {
		ts, err := ParseDmesgTimestamp(tt.line)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseDmesgTimestamp(%q) expected error, got %v", tt.line, ts)
			}
		} else {
			if err != nil {
				t.Errorf("ParseDmesgTimestamp(%q) error: %v", tt.line, err)
			}
		}
	}
}

func TestParseDmesgTimestampUsesLocalZone(t *testing.T) {
	// dmesg -T emits timestamps in local time; the parser must reflect that
	// so cross-system comparisons (e.g. against time.Now()) align.
	ts, err := ParseDmesgTimestamp("[Mon Feb  3 12:25:01 2026] kernel: x")
	if err != nil {
		t.Fatalf("ParseDmesgTimestamp error: %v", err)
	}
	if loc := ts.Location(); loc != time.Local {
		t.Errorf("Location = %v, want %v", loc, time.Local)
	}
}

func TestFilterNewLines(t *testing.T) {
	lines := []string{
		"[Mon Feb  3 12:00:00 2026] old message",
		"[Mon Feb  3 12:05:00 2026] newer message",
		"[Mon Feb  3 12:10:00 2026] newest message",
	}

	// Parse the middle timestamp as "last seen"
	lastSeen, _ := time.ParseInLocation("Mon Jan _2 15:04:05 2006", "Mon Feb  3 12:05:00 2026", time.Local)

	filtered, latestTs := FilterNewLines(lines, lastSeen)

	if len(filtered) != 1 {
		t.Errorf("FilterNewLines returned %d lines, want 1", len(filtered))
	}
	if len(filtered) > 0 && filtered[0] != "[Mon Feb  3 12:10:00 2026] newest message" {
		t.Errorf("FilterNewLines returned wrong line: %q", filtered[0])
	}

	expectedLatest, _ := time.ParseInLocation("Mon Jan _2 15:04:05 2006", "Mon Feb  3 12:10:00 2026", time.Local)
	if !latestTs.Equal(expectedLatest) {
		t.Errorf("latestTs = %v, want %v", latestTs, expectedLatest)
	}
}

func TestCLocaleEnvFiltersInheritedLocale(t *testing.T) {
	// A German LC_ALL inherited from systemd or the operator's shell would
	// produce duplicate envp keys; under first-wins libc precedence dmesg
	// would emit localized month names and parsing would silently fail.
	t.Setenv("LC_ALL", "de_DE.UTF-8")
	t.Setenv("LANG", "de_DE.UTF-8")
	t.Setenv("LC_TIME", "de_DE.UTF-8")

	env := cLocaleEnv()

	var lcAll []string
	for _, e := range env {
		if strings.HasPrefix(e, "LC_ALL=") {
			lcAll = append(lcAll, e)
		}
		if strings.HasPrefix(e, "LANG=") || strings.HasPrefix(e, "LC_TIME=") {
			t.Errorf("inherited locale var leaked into env: %q", e)
		}
	}
	if len(lcAll) != 1 || lcAll[0] != "LC_ALL=C" {
		t.Errorf("LC_ALL entries = %v, want exactly [\"LC_ALL=C\"]", lcAll)
	}

	// Sanity: unrelated env vars are preserved.
	t.Setenv("TASSEOGRAPH_TEST_KEEP", "yes")
	env = cLocaleEnv()
	found := false
	for _, e := range env {
		if e == "TASSEOGRAPH_TEST_KEEP=yes" {
			found = true
			break
		}
	}
	if !found {
		t.Error("unrelated env var was filtered out")
	}
	_ = os.Unsetenv("TASSEOGRAPH_TEST_KEEP")
}

func TestCapLines(t *testing.T) {
	// Under limit - no truncation
	small := []string{"a", "b", "c"}
	result, truncated := CapLines(small)
	if truncated {
		t.Error("CapLines truncated when under limit")
	}
	if len(result) != 3 {
		t.Errorf("CapLines returned %d lines, want 3", len(result))
	}

	// Over limit - truncate to most recent
	big := make([]string, MaxLines+100)
	for i := range big {
		big[i] = fmt.Sprintf("line-%d", i)
	}
	result, truncated = CapLines(big)
	if !truncated {
		t.Error("CapLines did not truncate when over limit")
	}
	if len(result) != MaxLines {
		t.Errorf("CapLines returned %d lines, want %d", len(result), MaxLines)
	}
	// Should keep the last (most recent) lines
	if result[0] != "line-100" {
		t.Errorf("CapLines kept wrong lines, first = %q", result[0])
	}
}
