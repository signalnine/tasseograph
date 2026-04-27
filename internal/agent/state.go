// internal/agent/state.go
package agent

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

const timestampFormat = time.RFC3339Nano

// ReadLastTimestamp reads the last processed timestamp from file.
// Returns zero time if file doesn't exist or is corrupt.
func ReadLastTimestamp(path string) (time.Time, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}

	ts, err := time.Parse(timestampFormat, strings.TrimSpace(string(data)))
	if err != nil {
		// Corrupt file - return zero time for fresh start
		return time.Time{}, nil
	}

	return ts, nil
}

// WriteLastTimestamp writes the timestamp to the state file atomically.
// Writes to <path>.tmp then renames into place; POSIX rename(2) guarantees
// the destination is either the old content or the new content, never partial.
func WriteLastTimestamp(path string, ts time.Time) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(ts.Format(timestampFormat)), 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
