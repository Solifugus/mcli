package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Solifugus/mcli/internal/core/transfer"
)

// importBatchSize is how many rows are grouped into one multi-row INSERT.
const importBatchSize = 500

// resolveTransferPath resolves an import/export path. Relative paths are taken
// against the current workspace directory (where imports/ and exports/ live);
// absolute paths are used as-is. Parent directories are created on export.
func (c *Core) resolveTransferPath(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(c.workspaceDir(), p)
}

// ExportQueryFile runs a saved SQL file and writes its rows to destPath.
func (c *Core) ExportQueryFile(ctx context.Context, sqlName, destPath string) (int, error) {
	sql, err := c.ReadSQLFile(sqlName)
	if err != nil {
		return 0, err
	}
	return c.exportSQL(ctx, sql, destPath)
}

// ExportTable writes an entire table to destPath.
func (c *Core) ExportTable(ctx context.Context, table, destPath string) (int, error) {
	return c.exportSQL(ctx, "SELECT * FROM "+table, destPath)
}

// exportSQL streams a query's rows to destPath in the format implied by the
// file extension.
func (c *Core) exportSQL(ctx context.Context, sql, destPath string) (int, error) {
	if c.conn == nil {
		return 0, ErrNotConnected
	}
	rs, err := c.conn.RunQuery(ctx, sql)
	if err != nil {
		return 0, err
	}
	defer rs.Close()

	f, err := c.createExportFile(destPath)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	var n int
	if transfer.IsXLSX(destPath) {
		n, err = transfer.ExportXLSXStream(f, rs, "")
	} else {
		n, err = transfer.ExportStream(f, rs, transfer.DelimiterForPath(destPath))
	}
	if err != nil {
		return n, err
	}
	c.log("EXPORT", "to", filepath.Base(destPath), fmt.Sprintf("%drows", n))
	return n, nil
}

// ExportRows writes already-materialized rows (the current result) to destPath.
func (c *Core) ExportRows(cols []string, rows [][]string, destPath string) (int, error) {
	f, err := c.createExportFile(destPath)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	var n int
	if transfer.IsXLSX(destPath) {
		n, err = transfer.ExportXLSXRows(f, cols, rows, "")
	} else {
		n, err = transfer.ExportRows(f, cols, rows, transfer.DelimiterForPath(destPath))
	}
	if err != nil {
		return n, err
	}
	c.log("EXPORT", "current to", filepath.Base(destPath), fmt.Sprintf("%drows", n))
	return n, nil
}

func (c *Core) createExportFile(destPath string) (*os.File, error) {
	path := c.resolveTransferPath(destPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	return os.Create(path)
}

// ImportFile loads a delimited or .xlsx file (header row required) into a table,
// inserting rows in batches via RunStatement. The header names become the target
// columns. sheet selects the worksheet for .xlsx imports ("" = first sheet) and
// is ignored for delimited files.
func (c *Core) ImportFile(ctx context.Context, srcPath, table, sheet string) (int, error) {
	if c.conn == nil {
		return 0, ErrNotConnected
	}
	f, err := os.Open(c.resolveTransferPath(srcPath))
	if err != nil {
		return 0, err
	}
	defer f.Close()

	var header []string
	var rows [][]string
	if transfer.IsXLSX(srcPath) {
		header, rows, err = transfer.ReadXLSX(f, sheet)
	} else {
		header, rows, err = transfer.ReadDelimited(f, transfer.DelimiterForPath(srcPath))
	}
	if err != nil {
		return 0, err
	}
	if len(header) == 0 {
		return 0, fmt.Errorf("import: file has no header row")
	}

	imported := 0
	for start := 0; start < len(rows); start += importBatchSize {
		end := min(start+importBatchSize, len(rows))
		stmt, err := buildInsert(table, header, rows[start:end])
		if err != nil {
			return imported, err
		}
		if _, err := c.conn.RunStatement(ctx, stmt); err != nil {
			return imported, fmt.Errorf("import: row %d: %w", start+1, err)
		}
		imported += end - start
	}
	c.log("IMPORT", filepath.Base(srcPath), "into", table, fmt.Sprintf("%drows", imported))
	return imported, nil
}

// buildInsert constructs a multi-row INSERT. Identifiers are double-quoted and
// values are emitted as SQL literals (empty cell → NULL). This is the
// adapter-uniform path (§22); per-dialect bulk loaders can come later.
func buildInsert(table string, cols []string, rows [][]string) (string, error) {
	var b strings.Builder
	b.WriteString("INSERT INTO ")
	b.WriteString(table)
	b.WriteString(" (")
	for i, col := range cols {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(quoteIdent(col))
	}
	b.WriteString(") VALUES ")
	for r, row := range rows {
		if len(row) != len(cols) {
			return "", fmt.Errorf("import: row %d has %d fields, want %d", r+1, len(row), len(cols))
		}
		if r > 0 {
			b.WriteString(", ")
		}
		b.WriteByte('(')
		for i, v := range row {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(sqlLiteral(v))
		}
		b.WriteByte(')')
	}
	return b.String(), nil
}

// quoteIdent double-quotes an identifier, escaping embedded quotes. Standard SQL
// (and Postgres); other dialects can override once they land.
func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// sqlLiteral renders a cell as a SQL literal: an empty cell becomes NULL, and
// everything else a single-quoted string with quotes doubled. The database
// coerces the text to the column type on insert.
func sqlLiteral(s string) string {
	if s == "" {
		return "NULL"
	}
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
