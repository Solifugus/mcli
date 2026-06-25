// Package transfer implements import and export of tabular data in delimited
// formats (CSV, TSV, pipe-delimited). It is written once against the adapter's
// RowStream (export) and plain row slices, so format support is uniform across
// every database rather than reimplemented per adapter. See docs/mcli-design.md
// §16, §22.
package transfer

import (
	"encoding/csv"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/Solifugus/mcli/internal/core/adapter"
)

// DelimiterForPath infers the field delimiter from a file extension: .tsv → tab,
// .psv/.pipe → '|', everything else (incl. .csv) → comma.
func DelimiterForPath(path string) rune {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".tsv":
		return '\t'
	case ".psv", ".pipe":
		return '|'
	default:
		return ','
	}
}

// ExportStream writes a header row of column names followed by every row of rs
// to w in the given delimited format. It returns the number of data rows
// written. The caller owns rs and w.
func ExportStream(w io.Writer, rs adapter.RowStream, delim rune) (int, error) {
	cw := csv.NewWriter(w)
	cw.Comma = delim
	if err := cw.Write(rs.Columns()); err != nil {
		return 0, err
	}
	n := 0
	for rs.Next() {
		vals, err := rs.Values()
		if err != nil {
			return n, err
		}
		if err := cw.Write(cellStrings(vals)); err != nil {
			return n, err
		}
		n++
	}
	if err := rs.Err(); err != nil {
		return n, err
	}
	cw.Flush()
	return n, cw.Error()
}

// ExportRows writes already-materialized rows (e.g. the current in-memory result)
// to w. It returns the number of data rows written.
func ExportRows(w io.Writer, cols []string, rows [][]string, delim rune) (int, error) {
	cw := csv.NewWriter(w)
	cw.Comma = delim
	if err := cw.Write(cols); err != nil {
		return 0, err
	}
	for _, r := range rows {
		if err := cw.Write(r); err != nil {
			return 0, err
		}
	}
	cw.Flush()
	return len(rows), cw.Error()
}

// ReadDelimited reads a delimited file with a header row, returning the header
// and the data rows. Field counts are not required to match the header (ragged
// rows are allowed) so callers can validate as they see fit.
func ReadDelimited(r io.Reader, delim rune) (header []string, rows [][]string, err error) {
	cr := csv.NewReader(r)
	cr.Comma = delim
	cr.FieldsPerRecord = -1 // allow ragged rows
	records, err := cr.ReadAll()
	if err != nil {
		return nil, nil, fmt.Errorf("read delimited: %w", err)
	}
	if len(records) == 0 {
		return nil, nil, fmt.Errorf("file is empty")
	}
	return records[0], records[1:], nil
}

// cellStrings renders driver values for output: nil becomes an empty field;
// everything else uses its default string form.
func cellStrings(vals []any) []string {
	out := make([]string, len(vals))
	for i, v := range vals {
		if v != nil {
			out[i] = fmt.Sprintf("%v", v)
		}
	}
	return out
}
