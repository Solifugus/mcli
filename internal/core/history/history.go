// Package history records per-workspace action logs. Each workspace owns a
// history.log of timestamped entries in the order they happened, e.g.
//
//	2026-06-24 15:42:11 ENTER workspace consumer-lending
//	2026-06-24 15:42:12 CONNECT sqlprod01 database ETLDB
//
// See docs/mcli-design.md §7.
package history

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// timeFormat is the timestamp layout for log lines: "2006-01-02 15:04:05".
const timeFormat = "2006-01-02 15:04:05"

// Log appends entries to a single workspace's history.log. The zero value is not
// usable; construct with New.
type Log struct {
	path string
	// now is injectable for deterministic tests; defaults to time.Now.
	now func() time.Time
}

// New returns a Log that writes to the given history.log path.
func New(path string) *Log {
	return &Log{path: path, now: time.Now}
}

// Append writes one timestamped entry. The action and any args are joined with
// spaces, e.g. Append("CONNECT", "sqlprod01", "database", "ETLDB"). The file and
// its parent directory are created if missing.
func (l *Log) Append(action string, args ...string) error {
	if l.path == "" {
		return fmt.Errorf("history: empty log path")
	}
	parts := append([]string{l.now().Format(timeFormat), action}, args...)
	line := strings.Join(parts, " ") + "\n"

	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return fmt.Errorf("history: create dir: %w", err)
	}
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("history: open log: %w", err)
	}
	defer f.Close()
	if _, err := f.WriteString(line); err != nil {
		return fmt.Errorf("history: write: %w", err)
	}
	return nil
}
