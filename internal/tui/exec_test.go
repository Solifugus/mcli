package tui

import (
	"strings"
	"testing"

	"github.com/Solifugus/mcli/internal/core/adapter"
)

func TestParseObjectArgs(t *testing.T) {
	cases := []struct {
		name      string
		args      []string
		wantKinds []adapter.ObjectKind
		wantSub   string
		wantErr   bool
	}{
		{"empty", nil, nil, "", false},
		{"substring only", []string{"order"}, nil, "order", false},
		{"single kind", []string{"tables"}, []adapter.ObjectKind{adapter.KindTable}, "", false},
		{"singular form", []string{"view"}, []adapter.ObjectKind{adapter.KindView}, "", false},
		{"short form", []string{"procs", "funcs"}, []adapter.ObjectKind{adapter.KindProcedure, adapter.KindFunction}, "", false},
		{"kinds + substring", []string{"views", "procedures", "order"}, []adapter.ObjectKind{adapter.KindView, adapter.KindProcedure}, "order", false},
		{"substring before kinds", []string{"order", "tables"}, []adapter.ObjectKind{adapter.KindTable}, "order", false},
		{"dedupe kinds", []string{"tables", "table"}, []adapter.ObjectKind{adapter.KindTable}, "", false},
		{"case insensitive kind", []string{"TABLES"}, []adapter.ObjectKind{adapter.KindTable}, "", false},
		{"two substrings error", []string{"foo", "bar"}, nil, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kinds, sub, errMsg := parseObjectArgs(c.args)
			if (errMsg != "") != c.wantErr {
				t.Fatalf("errMsg=%q wantErr=%v", errMsg, c.wantErr)
			}
			if c.wantErr {
				return
			}
			if sub != c.wantSub {
				t.Errorf("substr=%q want %q", sub, c.wantSub)
			}
			if len(kinds) != len(c.wantKinds) {
				t.Fatalf("kinds=%v want %v", kinds, c.wantKinds)
			}
			for i := range kinds {
				if kinds[i] != c.wantKinds[i] {
					t.Errorf("kinds[%d]=%q want %q", i, kinds[i], c.wantKinds[i])
				}
			}
		})
	}
}

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

func TestRenderResultTableNoClip(t *testing.T) {
	lines, clipped := renderResultTable([]string{"id", "name"}, [][]string{
		{"1", "ada"}, {"2", "bob"},
	}, 80)
	if clipped {
		t.Error("a narrow table should not be clipped")
	}
	if len(lines) != 4 { // header, sep, 2 rows
		t.Fatalf("expected 4 lines, got %d: %v", len(lines), lines)
	}
	for _, l := range lines {
		if dispWidth(l) > 80 {
			t.Errorf("line exceeds width: %q", l)
		}
	}
}

func TestRenderResultTableColumnCap(t *testing.T) {
	long := strings.Repeat("x", 100)
	lines, clipped := renderResultTable([]string{"blob"}, [][]string{{long}}, 200)
	if !clipped {
		t.Error("a value beyond resultColCap should be reported clipped")
	}
	// The data row is the third line; it must be capped and end with the ellipsis.
	row := lines[2]
	if dispWidth(row) > resultColCap {
		t.Errorf("capped column wider than %d: %q", resultColCap, row)
	}
	if !strings.HasSuffix(row, "…") {
		t.Errorf("truncated cell should end with '…': %q", row)
	}
}

func TestRenderResultTableWidthClip(t *testing.T) {
	cols := []string{"a", "b", "c", "d"}
	row := []string{"11111", "22222", "33333", "44444"}
	lines, clipped := renderResultTable(cols, [][]string{row}, 12)
	if !clipped {
		t.Error("a row wider than maxWidth should be clipped")
	}
	for _, l := range lines {
		if dispWidth(l) > 12 {
			t.Errorf("line exceeds maxWidth 12: %q (%d)", l, dispWidth(l))
		}
	}
	if !strings.HasSuffix(lines[2], "›") {
		t.Errorf("width-clipped row should end with '›': %q", lines[2])
	}
}

func TestResultSummary(t *testing.T) {
	cases := []struct {
		total, shown, maxRows         int
		capped, rowTrunc, clipped     bool
		want                          string
	}{
		{3, 3, 500, false, false, false, "(3 rows)"},
		{1, 1, 500, false, false, false, "(1 row)"},
		{3, 3, 500, false, false, true, "(3 rows, columns clipped to width) — .grid for the full view"},
		{1200, 500, 500, false, true, false, "(first 500 of 1200 rows) — .grid for the full view"},
		{50000, 10000, 500, true, false, true, "(first 10000 of 50000+ rows, columns clipped to width) — .grid for the full view"},
	}
	for _, c := range cases {
		got := resultSummary(c.total, c.shown, c.maxRows, c.capped, c.rowTrunc, c.clipped)
		if got != c.want {
			t.Errorf("resultSummary(%+v) = %q, want %q", c, got, c.want)
		}
	}
}
