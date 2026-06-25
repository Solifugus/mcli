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

// fixedDefaultCap bounds how many rows the default (non-exact) fixed-width export
// buffers in memory to measure column widths. Beyond it the export is truncated
// and the caller is told to re-run with `exact`, which streams in two passes with
// flat memory and nothing curtailed.
const fixedDefaultCap = 10000

// resolveTransferPath resolves an import/export path. Relative paths are taken
// against the current workspace directory (where imports/ and exports/ live);
// absolute paths are used as-is. Parent directories are created on export.
func (c *Core) resolveTransferPath(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(c.workspaceDir(), p)
}

// ExportQueryFile runs a saved SQL file and writes its rows to destPath. exact
// only affects fixed-width output (see exportSQL); truncated reports whether a
// default fixed-width export hit fixedDefaultCap and dropped rows.
func (c *Core) ExportQueryFile(ctx context.Context, sqlName, destPath string, exact bool) (n int, truncated bool, err error) {
	sql, err := c.ReadSQLFile(sqlName)
	if err != nil {
		return 0, false, err
	}
	return c.exportSQL(ctx, sql, destPath, exact)
}

// ExportTable writes an entire table to destPath.
func (c *Core) ExportTable(ctx context.Context, table, destPath string, exact bool) (n int, truncated bool, err error) {
	return c.exportSQL(ctx, "SELECT * FROM "+table, destPath, exact)
}

// exportSQL writes a query's rows to destPath in the format implied by the file
// extension. Delimited and xlsx formats stream row-at-a-time. Fixed-width needs
// the column widths before the first line, so it either buffers up to
// fixedDefaultCap rows (default) or, when exact is set, runs two streaming passes
// (measure, then write) with flat memory and no row cap.
func (c *Core) exportSQL(ctx context.Context, sql, destPath string, exact bool) (n int, truncated bool, err error) {
	if c.conn == nil {
		return 0, false, ErrNotConnected
	}
	if transfer.IsFixedWidth(destPath) {
		return c.exportFixed(ctx, sql, destPath, exact)
	}

	rs, err := c.conn.RunQuery(ctx, sql)
	if err != nil {
		return 0, false, err
	}
	defer rs.Close()

	f, err := c.createExportFile(destPath)
	if err != nil {
		return 0, false, err
	}
	defer f.Close()

	if transfer.IsXLSX(destPath) {
		n, err = transfer.ExportXLSXStream(f, rs, "")
	} else {
		n, err = transfer.ExportStream(f, rs, transfer.DelimiterForPath(destPath))
	}
	if err != nil {
		return n, false, err
	}
	c.log("EXPORT", "to", filepath.Base(destPath), fmt.Sprintf("%drows", n))
	return n, false, nil
}

// exportFixed writes a fixed-width file. The default path buffers rows (capped at
// fixedDefaultCap) to derive exact-for-what-it-holds widths; if more rows remain
// it stops and reports truncated so the caller can suggest exact. The exact path
// measures every row in a first pass, then re-runs the query and writes in a
// second, holding one row at a time.
func (c *Core) exportFixed(ctx context.Context, sql, destPath string, exact bool) (n int, truncated bool, err error) {
	f, err := c.createExportFile(destPath)
	if err != nil {
		return 0, false, err
	}
	defer f.Close()

	if exact {
		measure, err := c.conn.RunQuery(ctx, sql)
		if err != nil {
			return 0, false, err
		}
		widths, err := transfer.MeasureFixedStream(measure)
		measure.Close()
		if err != nil {
			return 0, false, err
		}
		write, err := c.conn.RunQuery(ctx, sql)
		if err != nil {
			return 0, false, err
		}
		defer write.Close()
		n, err = transfer.WriteFixedStream(f, write, widths)
		if err != nil {
			return n, false, err
		}
		c.log("EXPORT", "to", filepath.Base(destPath), fmt.Sprintf("%drows", n), "exact")
		return n, false, nil
	}

	rs, err := c.conn.RunQuery(ctx, sql)
	if err != nil {
		return 0, false, err
	}
	defer rs.Close()

	cols := rs.Columns()
	var rows [][]string
	for rs.Next() {
		if len(rows) >= fixedDefaultCap {
			truncated = true
			break
		}
		vals, err := rs.Values()
		if err != nil {
			return 0, false, err
		}
		rows = append(rows, toCells(vals))
	}
	if err := rs.Err(); err != nil {
		return 0, false, err
	}
	n, err = transfer.WriteFixedRows(f, cols, rows, transfer.FixedWidths(cols, rows))
	if err != nil {
		return n, truncated, err
	}
	c.log("EXPORT", "to", filepath.Base(destPath), fmt.Sprintf("%drows", n))
	return n, truncated, nil
}

// ExportRows writes already-materialized rows (the current result) to destPath.
func (c *Core) ExportRows(cols []string, rows [][]string, destPath string) (int, error) {
	f, err := c.createExportFile(destPath)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	var n int
	switch {
	case transfer.IsXLSX(destPath):
		n, err = transfer.ExportXLSXRows(f, cols, rows, "")
	case transfer.IsFixedWidth(destPath):
		n, err = transfer.WriteFixedRows(f, cols, rows, transfer.FixedWidths(cols, rows))
	default:
		n, err = transfer.ExportRows(f, cols, rows, transfer.DelimiterForPath(destPath))
	}
	if err != nil {
		return n, err
	}
	c.log("EXPORT", "current to", filepath.Base(destPath), fmt.Sprintf("%drows", n))
	return n, nil
}

// toCells renders driver values for export: nil becomes an empty field (matching
// the delimited exporters), everything else its default string form.
func toCells(vals []any) []string {
	out := make([]string, len(vals))
	for i, v := range vals {
		if v != nil {
			out[i] = fmt.Sprintf("%v", v)
		}
	}
	return out
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

// ImportFixedFile loads a fixed-width flat file into a table. Because the file is
// not self-describing (no delimiter, no header), the caller supplies the column
// widths and the fields are inserted positionally into the table's columns in
// declared order. Each field is whitespace-trimmed; an empty field becomes NULL.
func (c *Core) ImportFixedFile(ctx context.Context, srcPath, table string, widths []int) (int, error) {
	if c.conn == nil {
		return 0, ErrNotConnected
	}
	if len(widths) == 0 {
		return 0, fmt.Errorf("import: fixed-width requires column widths")
	}
	f, err := os.Open(c.resolveTransferPath(srcPath))
	if err != nil {
		return 0, err
	}
	defer f.Close()

	rows, err := transfer.ReadFixed(f, widths)
	if err != nil {
		return 0, err
	}

	imported := 0
	for start := 0; start < len(rows); start += importBatchSize {
		end := min(start+importBatchSize, len(rows))
		stmt := buildInsertPositional(table, rows[start:end])
		if _, err := c.conn.RunStatement(ctx, stmt); err != nil {
			return imported, fmt.Errorf("import: row %d: %w", start+1, err)
		}
		imported += end - start
	}
	c.log("IMPORT", filepath.Base(srcPath), "into", table, fmt.Sprintf("%drows", imported), "fixed")
	return imported, nil
}

// buildInsertPositional constructs a multi-row INSERT without a column list, so
// values map onto the table's columns in declared order. Used for fixed-width
// import, which has no header to name columns.
func buildInsertPositional(table string, rows [][]string) string {
	var b strings.Builder
	b.WriteString("INSERT INTO ")
	b.WriteString(table)
	b.WriteString(" VALUES ")
	for r, row := range rows {
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
	return b.String()
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
