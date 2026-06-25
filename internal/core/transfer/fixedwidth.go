package transfer

import (
	"bufio"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/Solifugus/mcli/internal/core/adapter"
)

// IsFixedWidth reports whether a path names a fixed-width flat file (.txt/.fix).
// Fixed-width files carry no delimiter and no header, so the column boundaries
// are not inferable from the file — export derives them from the data and import
// must be given them explicitly (see WriteFixed* and ReadFixed).
func IsFixedWidth(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".txt", ".fix":
		return true
	default:
		return false
	}
}

// FixedWidths returns the column widths for a header plus already-materialized
// rows: each width is the longest rendered value in that column, never narrower
// than the header name so the header line stays aligned.
func FixedWidths(cols []string, rows [][]string) []int {
	w := make([]int, len(cols))
	for i, c := range cols {
		w[i] = len(c)
	}
	for _, r := range rows {
		for i, v := range r {
			if i < len(w) && len(v) > w[i] {
				w[i] = len(v)
			}
		}
	}
	return w
}

// WriteFixedRows writes a header line followed by every row as space-padded
// fixed-width columns separated by a single space. Widths must cover every
// column. It returns the number of data rows written.
func WriteFixedRows(w io.Writer, cols []string, rows [][]string, widths []int) (int, error) {
	bw := bufio.NewWriter(w)
	if err := writeFixedLine(bw, cols, widths); err != nil {
		return 0, err
	}
	for _, r := range rows {
		if err := writeFixedLine(bw, r, widths); err != nil {
			return 0, err
		}
	}
	if err := bw.Flush(); err != nil {
		return 0, err
	}
	return len(rows), nil
}

// MeasureFixedStream consumes rs and returns the column widths needed to hold the
// header and every row, using O(columns) memory regardless of row count. This is
// pass one of the exact two-pass export; the stream is exhausted on return.
func MeasureFixedStream(rs adapter.RowStream) ([]int, error) {
	cols := rs.Columns()
	w := make([]int, len(cols))
	for i, c := range cols {
		w[i] = len(c)
	}
	for rs.Next() {
		vals, err := rs.Values()
		if err != nil {
			return nil, err
		}
		for i, s := range cellStrings(vals) {
			if i < len(w) && len(s) > w[i] {
				w[i] = len(s)
			}
		}
	}
	if err := rs.Err(); err != nil {
		return nil, err
	}
	return w, nil
}

// WriteFixedStream writes a header line plus every row of rs using the given
// widths (typically from MeasureFixedStream over a prior pass). This is pass two
// of the exact export; it holds only one row at a time. It returns the row count.
func WriteFixedStream(w io.Writer, rs adapter.RowStream, widths []int) (int, error) {
	bw := bufio.NewWriter(w)
	if err := writeFixedLine(bw, rs.Columns(), widths); err != nil {
		return 0, err
	}
	n := 0
	for rs.Next() {
		vals, err := rs.Values()
		if err != nil {
			return n, err
		}
		if err := writeFixedLine(bw, cellStrings(vals), widths); err != nil {
			return n, err
		}
		n++
	}
	if err := rs.Err(); err != nil {
		return n, err
	}
	if err := bw.Flush(); err != nil {
		return n, err
	}
	return n, nil
}

// writeFixedLine renders one record as space-padded columns. A value longer than
// its column width is written in full (never truncated) — alignment past that
// column is sacrificed rather than corrupting the data.
func writeFixedLine(w *bufio.Writer, cells []string, widths []int) error {
	for i, width := range widths {
		if i > 0 {
			if err := w.WriteByte(' '); err != nil {
				return err
			}
		}
		v := ""
		if i < len(cells) {
			v = cells[i]
		}
		if _, err := w.WriteString(v); err != nil {
			return err
		}
		if pad := width - len(v); pad > 0 {
			if _, err := w.WriteString(strings.Repeat(" ", pad)); err != nil {
				return err
			}
		}
	}
	return w.WriteByte('\n')
}

// ReadFixed parses a fixed-width file into rows using explicit column widths.
// Each line is sliced into len(widths) fields (plus the single space that
// WriteFixed* inserts between columns), and each field is whitespace-trimmed.
// Fixed-width files are not self-describing, so the caller supplies the widths;
// there is no header row, so the values map positionally onto table columns.
func ReadFixed(r io.Reader, widths []int) ([][]string, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	var rows [][]string
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r\n")
		if line == "" {
			continue
		}
		row := make([]string, len(widths))
		pos := 0
		for i, width := range widths {
			if i > 0 {
				pos++ // skip the inter-column separator space
			}
			end := pos + width
			if pos > len(line) {
				pos = len(line)
			}
			if end > len(line) {
				end = len(line)
			}
			row[i] = strings.TrimSpace(line[pos:end])
			pos = end
		}
		rows = append(rows, row)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read fixed-width: %w", err)
	}
	return rows, nil
}
