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

// TestWriteIsAtomicViaRename verifies WriteLastTimestamp writes to a temp file
// and renames it into place. A pre-existing .tmp file with garbage must not
// remain after a successful write.
func TestWriteIsAtomicViaRename(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "last_timestamp")
	tmpPath := statePath + ".tmp"

	// Simulate a crashed previous write that left a stale .tmp behind.
	if err := os.WriteFile(tmpPath, []byte("garbage from prior crash"), 0644); err != nil {
		t.Fatalf("seeding stale tmp: %v", err)
	}

	now := time.Date(2026, 4, 27, 9, 0, 0, 0, time.UTC)
	if err := WriteLastTimestamp(statePath, now); err != nil {
		t.Fatalf("WriteLastTimestamp: %v", err)
	}

	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Errorf("stale .tmp must be gone after atomic rename, stat err = %v", err)
	}

	got, err := ReadLastTimestamp(statePath)
	if err != nil {
		t.Fatalf("ReadLastTimestamp: %v", err)
	}
	if !got.Equal(now) {
		t.Errorf("ReadLastTimestamp = %v, want %v", got, now)
	}
}

// TestWriteFailurePreservesOriginal verifies that when the temp-file write
// fails, the existing state file is not corrupted or replaced.
func TestWriteFailurePreservesOriginal(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "last_timestamp")
	tmpPath := statePath + ".tmp"

	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := WriteLastTimestamp(statePath, t1); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	// Make a temp-file write impossible by squatting on the .tmp path with a directory.
	if err := os.Mkdir(tmpPath, 0755); err != nil {
		t.Fatalf("mkdir tmp squat: %v", err)
	}

	t2 := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	if err := WriteLastTimestamp(statePath, t2); err == nil {
		t.Fatalf("WriteLastTimestamp should fail when .tmp path is unwritable")
	}

	got, err := ReadLastTimestamp(statePath)
	if err != nil {
		t.Fatalf("ReadLastTimestamp: %v", err)
	}
	if !got.Equal(t1) {
		t.Errorf("original state corrupted: got %v, want %v", got, t1)
	}
}
