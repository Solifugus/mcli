package tui

import (
	"context"
	"strings"
	"testing"
)

func TestExportUsage(t *testing.T) {
	m := newTestModel(t)
	for _, in := range []string{`\export`, `\export query`, `\export query x`, `\export bogus to f.csv`} {
		res, act := m.handleLine(in)
		if act.async != nil {
			t.Errorf("%q should be a sync usage error, not async", in)
		}
		if !strings.Contains(joinLines(res), "usage:") && !strings.Contains(joinLines(res), "no current") {
			t.Errorf("%q = %q", in, joinLines(res))
		}
	}
}

func TestExportQueryProducesRunner(t *testing.T) {
	m := newTestModel(t)
	res, act := m.handleLine(`\export query report to exports/out.csv`)
	if act.async == nil {
		t.Fatalf("\\export query should be async; res=%v", res.lines)
	}
	// No connection -> the runner reports an error rather than panicking.
	if msg := act.async(context.Background()); msg.err == nil {
		t.Error("export with no connection/file should error")
	}
}

func TestExportCurrentWithoutResult(t *testing.T) {
	m := newTestModel(t)
	_, act := m.handleLine(`\export current to exports/out.csv`)
	if act.async == nil {
		t.Fatal("\\export current should be async")
	}
	if msg := act.async(context.Background()); msg.err == nil {
		t.Error("export current with no result should error")
	}
}

func TestExportCurrentWritesLastResult(t *testing.T) {
	m := newTestModel(t)
	m.lastResult = &resultSet{cols: []string{"id"}, rows: [][]string{{"1"}, {"2"}}}
	_, act := m.handleLine(`\export current to exports/cur.csv`)
	msg := act.async(context.Background())
	if msg.err != nil {
		t.Fatalf("export current: %v", msg.err)
	}
	if !strings.Contains(strings.Join(msg.lines, "\n"), "exported 2 rows") {
		t.Errorf("export message = %v", msg.lines)
	}
	if content, err := m.core.ReadSQLFile("exports/cur"); err == nil {
		_ = content // not a SQL file; just ensure no panic path
	}
}

func TestImportUsage(t *testing.T) {
	m := newTestModel(t)
	for _, in := range []string{`\import`, `\import f.csv`, `\import f.csv table t`} {
		_, act := m.handleLine(in)
		if act.async != nil {
			t.Errorf("%q should be a sync usage error", in)
		}
	}
}

func TestImportProducesRunner(t *testing.T) {
	m := newTestModel(t)
	_, act := m.handleLine(`\import data.csv into staging.members`)
	if act.async == nil {
		t.Fatal("\\import should be async")
	}
	if msg := act.async(context.Background()); msg.err == nil {
		t.Error("import with no connection should error")
	}
}
