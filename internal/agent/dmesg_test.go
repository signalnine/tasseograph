// internal/agent/dmesg_test.go
package agent

import (
	"fmt"
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

func TestFilterNewLines(t *testing.T) {
	lines := []string{
		"[Mon Feb  3 12:00:00 2026] old message",
		"[Mon Feb  3 12:05:00 2026] newer message",
		"[Mon Feb  3 12:10:00 2026] newest message",
	}

	// Parse the middle timestamp as "last seen"
	lastSeen, _ := time.Parse("Mon Jan _2 15:04:05 2006", "Mon Feb  3 12:05:00 2026")

	filtered, latestTs := FilterNewLines(lines, lastSeen)

	if len(filtered) != 1 {
		t.Errorf("FilterNewLines returned %d lines, want 1", len(filtered))
	}
	if len(filtered) > 0 && filtered[0] != "[Mon Feb  3 12:10:00 2026] newest message" {
		t.Errorf("FilterNewLines returned wrong line: %q", filtered[0])
	}

	expectedLatest, _ := time.Parse("Mon Jan _2 15:04:05 2006", "Mon Feb  3 12:10:00 2026")
	if !latestTs.Equal(expectedLatest) {
		t.Errorf("latestTs = %v, want %v", latestTs, expectedLatest)
	}
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
