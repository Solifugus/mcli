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
	}, quoteIdent, false)
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
	if _, err := buildInsert("t", []string{"a", "b"}, [][]string{{"1"}}, quoteIdent, false); err == nil {
		t.Error("expected error for a row with the wrong field count")
	}
}

func TestBuildInsertOracleAll(t *testing.T) {
	stmt, err := buildInsert("members", []string{"id", "name"}, [][]string{
		{"1", "alice"},
		{"2", "bob"},
	}, quoteIdent, true)
	if err != nil {
		t.Fatalf("buildInsert oracle: %v", err)
	}
	// Oracle cannot take INSERT INTO ... VALUES (a),(b); it needs INSERT ALL.
	for _, want := range []string{
		`INSERT ALL`,
		`INTO members ("id", "name") VALUES ('1', 'alice')`,
		`INTO members ("id", "name") VALUES ('2', 'bob')`,
		`SELECT 1 FROM dual`,
	} {
		if !strings.Contains(stmt, want) {
			t.Errorf("oracle insert missing %q:\n%s", want, stmt)
		}
	}
	if strings.Contains(stmt, "VALUES ('1', 'alice'), ('2', 'bob')") {
		t.Errorf("oracle insert used the unsupported multi-row VALUES form:\n%s", stmt)
	}
}

func TestBuildInsertPositionalOracle(t *testing.T) {
	std := buildInsertPositional("t", [][]string{{"1", "a"}, {"2", "b"}}, false)
	if !strings.Contains(std, "VALUES ('1', 'a'), ('2', 'b')") {
		t.Errorf("standard positional = %q", std)
	}
	ora := buildInsertPositional("t", [][]string{{"1", "a"}, {"2", "b"}}, true)
	if !strings.Contains(ora, "INSERT ALL") || !strings.Contains(ora, "SELECT 1 FROM dual") {
		t.Errorf("oracle positional = %q", ora)
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
