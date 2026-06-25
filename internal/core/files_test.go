package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSQLFileNameValidation(t *testing.T) {
	ok := map[string]string{
		"funded-refresh":     "funded-refresh.sql",
		"funded-refresh.sql": "funded-refresh.sql",
		"notes.md":           "notes.md", // explicit extension kept
	}
	for in, want := range ok {
		got, err := sqlFileName(in)
		if err != nil || got != want {
			t.Errorf("sqlFileName(%q) = (%q,%v), want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "a/b", `a\b`, "../escape", "x..y"} {
		if _, err := sqlFileName(bad); err == nil {
			t.Errorf("sqlFileName(%q) should fail", bad)
		}
	}
}

func TestSQLFileLifecycle(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if err := c.WriteSQLFile("report", "select 1;\n"); err != nil {
		t.Fatalf("WriteSQLFile: %v", err)
	}
	// Written with the default .sql extension inside the workspace dir.
	if _, err := os.Stat(filepath.Join(c.workspaceDir(), "report.sql")); err != nil {
		t.Fatalf("file not created: %v", err)
	}

	content, err := c.ReadSQLFile("report")
	if err != nil || content != "select 1;\n" {
		t.Fatalf("ReadSQLFile = (%q,%v)", content, err)
	}

	files, err := c.ListSQLFiles()
	if err != nil || len(files) != 1 || files[0] != "report.sql" {
		t.Fatalf("ListSQLFiles = (%v,%v)", files, err)
	}

	if err := c.CopySQLFile("report", "report2"); err != nil {
		t.Fatalf("CopySQLFile: %v", err)
	}
	if err := c.RenameSQLFile("report2", "report3"); err != nil {
		t.Fatalf("RenameSQLFile: %v", err)
	}
	if err := c.DeleteSQLFile("report3"); err != nil {
		t.Fatalf("DeleteSQLFile: %v", err)
	}
	files, _ = c.ListSQLFiles()
	if len(files) != 1 || files[0] != "report.sql" {
		t.Fatalf("after copy/rename/delete, files = %v", files)
	}
}

func TestEnsureSQLFileCreatesEmpty(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := c.EnsureSQLFile("scratch"); err != nil {
		t.Fatalf("EnsureSQLFile: %v", err)
	}
	content, err := c.ReadSQLFile("scratch")
	if err != nil || content != "" {
		t.Errorf("ensured file = (%q,%v), want empty", content, err)
	}
	// EnsureSQLFile must not clobber existing content.
	_ = c.WriteSQLFile("scratch", "keep me")
	_ = c.EnsureSQLFile("scratch")
	if content, _ := c.ReadSQLFile("scratch"); content != "keep me" {
		t.Errorf("EnsureSQLFile clobbered content: %q", content)
	}
}

func TestReadMissingFile(t *testing.T) {
	c, _ := Open(t.TempDir())
	if _, err := c.ReadSQLFile("nope"); err == nil {
		t.Error("reading a missing file should error")
	}
}
