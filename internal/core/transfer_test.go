package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildInsert(t *testing.T) {
	stmt, err := buildInsert("staging.members", []string{"id", "name"}, [][]string{
		{"1", "alice"},
		{"2", "o'brien"}, // single quote must be doubled
		{"3", ""},        // empty cell -> NULL
	}, quoteIdent)
	if err != nil {
		t.Fatalf("buildInsert: %v", err)
	}
	wantParts := []string{
		`INSERT INTO staging.members ("id", "name") VALUES `,
		`('1', 'alice')`,
		`('2', 'o''brien')`,
		`('3', NULL)`,
	}
	for _, w := range wantParts {
		if !strings.Contains(stmt, w) {
			t.Errorf("statement missing %q:\n%s", w, stmt)
		}
	}
}

func TestBuildInsertRaggedRowErrors(t *testing.T) {
	if _, err := buildInsert("t", []string{"a", "b"}, [][]string{{"1"}}, quoteIdent); err == nil {
		t.Error("expected error for a row with the wrong field count")
	}
}

func TestQuoteIdentAndLiteral(t *testing.T) {
	if got := quoteIdent(`we"ird`); got != `"we""ird"` {
		t.Errorf("quoteIdent = %q", got)
	}
	if got := quoteIdentBacktick("we`ird"); got != "`we``ird`" {
		t.Errorf("quoteIdentBacktick = %q", got)
	}
	if got := sqlLiteral(""); got != "NULL" {
		t.Errorf("empty literal = %q, want NULL", got)
	}
	if got := sqlLiteral("a'b"); got != "'a''b'" {
		t.Errorf("literal = %q", got)
	}
}

func TestResolveTransferPath(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	rel := c.resolveTransferPath("exports/x.csv")
	if !strings.HasSuffix(rel, filepath.Join("workspaces", "default", "exports", "x.csv")) {
		t.Errorf("relative path = %q", rel)
	}
	abs := c.resolveTransferPath("/tmp/y.csv")
	if abs != "/tmp/y.csv" {
		t.Errorf("absolute path = %q", abs)
	}
}

func TestExportRowsToFile(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	n, err := c.ExportRows([]string{"id", "name"}, [][]string{{"1", "alice"}}, "exports/out.csv")
	if err != nil || n != 1 {
		t.Fatalf("ExportRows = (%d,%v)", n, err)
	}
	data, err := os.ReadFile(filepath.Join(c.workspaceDir(), "exports", "out.csv"))
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	if !strings.Contains(string(data), "id,name") || !strings.Contains(string(data), "1,alice") {
		t.Errorf("export content = %q", data)
	}
}

func TestImportExportRequireConnection(t *testing.T) {
	c, _ := Open(t.TempDir())
	if _, err := c.ImportFile(t.Context(), "x.csv", "t", ""); err == nil {
		t.Error("ImportFile without connection should error")
	}
	if _, _, err := c.ExportTable(t.Context(), "t", "x.csv", false); err == nil {
		t.Error("ExportTable without connection should error")
	}
}
