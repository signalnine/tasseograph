// internal/agent/state_test.go
package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStateReadWrite(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "last_timestamp")

	// Initially should return zero time
	ts, err := ReadLastTimestamp(statePath)
	if err != nil {
		t.Fatalf("ReadLastTimestamp (missing file) error: %v", err)
	}
	if !ts.IsZero() {
		t.Errorf("expected zero time for missing file, got %v", ts)
	}

	// Write a timestamp
	now := time.Date(2026, 2, 3, 12, 30, 0, 0, time.UTC)
	if err := WriteLastTimestamp(statePath, now); err != nil {
		t.Fatalf("WriteLastTimestamp error: %v", err)
	}

	// Read it back
	ts, err = ReadLastTimestamp(statePath)
	if err != nil {
		t.Fatalf("ReadLastTimestamp error: %v", err)
	}
	if !ts.Equal(now) {
		t.Errorf("ReadLastTimestamp = %v, want %v", ts, now)
	}
}

func TestStateCorruptFile(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "last_timestamp")

	// Write garbage
	os.WriteFile(statePath, []byte("not a timestamp"), 0644)

	// Should return zero time (fresh start)
	ts, err := ReadLastTimestamp(statePath)
	if err != nil {
		t.Fatalf("ReadLastTimestamp (corrupt) error: %v", err)
	}
	if !ts.IsZero() {
		t.Errorf("expected zero time for corrupt file, got %v", ts)
	}
}
