package tui

import (
	"strings"
	"testing"
)

func TestIsQuery(t *testing.T) {
	queries := []string{"select 1", "SELECT * FROM t", "  with x as (...) select", "EXPLAIN select 1", "values (1)", "table foo"}
	statements := []string{"insert into t values (1)", "update t set x=1", "delete from t", "create table t (...)", "drop table t", "selection"}
	for _, q := range queries {
		if !isQuery(q) {
			t.Errorf("isQuery(%q) = false, want true", q)
		}
	}
	for _, s := range statements {
		if isQuery(s) {
			t.Errorf("isQuery(%q) = true, want false", s)
		}
	}
}

func TestToStrings(t *testing.T) {
	got := toStrings([]any{1, "x", nil, true})
	want := []string{"1", "x", "NULL", "true"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("toStrings[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRenderTable(t *testing.T) {
	lines := renderTable([]string{"id", "name"}, [][]string{
		{"1", "alice"},
		{"22", "bob"},
	})
	if len(lines) != 4 { // header, separator, two rows
		t.Fatalf("got %d lines: %v", len(lines), lines)
	}
	// Header columns are padded to the widest value ("id" -> width 2, "name" -> 5).
	if !strings.HasPrefix(lines[0], "id  name") {
		t.Errorf("header = %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "--  ----") {
		t.Errorf("separator = %q", lines[1])
	}
	if !strings.HasPrefix(lines[3], "22  bob") {
		t.Errorf("row = %q", lines[3])
	}
}

func TestRenderTableEmpty(t *testing.T) {
	lines := renderTable([]string{"col"}, nil)
	if len(lines) != 2 { // header + separator only
		t.Errorf("empty table lines = %v", lines)
	}
}

func TestPlural(t *testing.T) {
	if plural(1) != "" || plural(0) != "s" || plural(2) != "s" {
		t.Error("plural wrong")
	}
}
