package history

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fixedClock(ts string) func() time.Time {
	t, _ := time.Parse(timeFormat, ts)
	return func() time.Time { return t }
}

func TestAppendWritesTimestampedLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ws", "history.log")
	l := New(path)
	l.now = fixedClock("2026-06-24 15:42:11")

	if err := l.Append("ENTER", "workspace", "consumer-lending"); err != nil {
		t.Fatalf("Append: %v", err)
	}
	l.now = fixedClock("2026-06-24 15:42:12")
	if err := l.Append("CONNECT", "sqlprod01", "database", "ETLDB"); err != nil {
		t.Fatalf("Append: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	want := "2026-06-24 15:42:11 ENTER workspace consumer-lending\n" +
		"2026-06-24 15:42:12 CONNECT sqlprod01 database ETLDB\n"
	if string(data) != want {
		t.Errorf("log =\n%q\nwant\n%q", data, want)
	}
}

func TestAppendCreatesParentDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "history.log")
	if err := New(path).Append("ENTER"); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("log not created: %v", err)
	}
}

func TestAppendEmptyPathErrors(t *testing.T) {
	if err := New("").Append("ENTER"); err == nil {
		t.Error("expected error for empty path")
	}
}
