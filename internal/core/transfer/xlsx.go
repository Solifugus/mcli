package transfer

import (
	"fmt"
	"io"

	"github.com/xuri/excelize/v2"

	"github.com/Solifugus/mcli/internal/core/adapter"
)

// IsXLSX reports whether a path names an Excel workbook.
func IsXLSX(path string) bool {
	return len(path) >= 5 && (path[len(path)-5:] == ".xlsx" || path[len(path)-5:] == ".XLSX")
}

// ExportXLSXStream writes a header row plus every row of rs to a new workbook.
// Native cell types are preserved (numbers stay numeric) since excelize accepts
// the driver values directly.
func ExportXLSXStream(w io.Writer, rs adapter.RowStream, sheet string) (int, error) {
	f := excelize.NewFile()
	defer f.Close()
	if sheet == "" {
		sheet = f.GetSheetList()[0]
	}
	if err := f.SetSheetRow(sheet, "A1", anySlice(rs.Columns())); err != nil {
		return 0, err
	}
	n := 0
	for rs.Next() {
		vals, err := rs.Values()
		if err != nil {
			return n, err
		}
		n++
		if err := f.SetSheetRow(sheet, fmt.Sprintf("A%d", n+1), &vals); err != nil {
			return n, err
		}
	}
	if err := rs.Err(); err != nil {
		return n, err
	}
	if _, err := f.WriteTo(w); err != nil {
		return n, err
	}
	return n, nil
}

// ExportXLSXRows writes already-materialized rows to a new workbook.
func ExportXLSXRows(w io.Writer, cols []string, rows [][]string, sheet string) (int, error) {
	f := excelize.NewFile()
	defer f.Close()
	if sheet == "" {
		sheet = f.GetSheetList()[0]
	}
	if err := f.SetSheetRow(sheet, "A1", anySlice(cols)); err != nil {
		return 0, err
	}
	for i, r := range rows {
		if err := f.SetSheetRow(sheet, fmt.Sprintf("A%d", i+2), anySlice(r)); err != nil {
			return i, err
		}
	}
	if _, err := f.WriteTo(w); err != nil {
		return 0, err
	}
	return len(rows), nil
}

// ReadXLSX reads a workbook sheet (the first sheet when sheet is empty),
// returning the header row and the data rows.
func ReadXLSX(r io.Reader, sheet string) (header []string, rows [][]string, err error) {
	f, err := excelize.OpenReader(r)
	if err != nil {
		return nil, nil, fmt.Errorf("open xlsx: %w", err)
	}
	defer f.Close()

	if sheet == "" {
		list := f.GetSheetList()
		if len(list) == 0 {
			return nil, nil, fmt.Errorf("xlsx has no sheets")
		}
		sheet = list[0]
	}
	records, err := f.GetRows(sheet)
	if err != nil {
		return nil, nil, fmt.Errorf("read sheet %q: %w", sheet, err)
	}
	if len(records) == 0 {
		return nil, nil, fmt.Errorf("sheet %q is empty", sheet)
	}
	return records[0], records[1:], nil
}

// anySlice converts strings to a pointer-to-[]any for excelize SetSheetRow.
func anySlice(ss []string) *[]any {
	a := make([]any, len(ss))
	for i, s := range ss {
		a[i] = s
	}
	return &a
}
