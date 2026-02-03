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

// WriteLastTimestamp writes the timestamp to the state file.
// Creates parent directories if needed.
func WriteLastTimestamp(path string, ts time.Time) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	return os.WriteFile(path, []byte(ts.Format(timestampFormat)), 0644)
}
