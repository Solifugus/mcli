package transfer

import (
	"bytes"
	"strings"
	"testing"
)

func TestDelimiterForPath(t *testing.T) {
	cases := map[string]rune{
		"a.csv": ',', "a.tsv": '\t', "a.psv": '|', "a.pipe": '|', "a.txt": ',', "a": ',',
	}
	for path, want := range cases {
		if got := DelimiterForPath(path); got != want {
			t.Errorf("DelimiterForPath(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestExportRows(t *testing.T) {
	var buf bytes.Buffer
	n, err := ExportRows(&buf, []string{"id", "name"}, [][]string{{"1", "alice"}, {"2", "bob, jr"}}, ',')
	if err != nil || n != 2 {
		t.Fatalf("ExportRows = (%d,%v)", n, err)
	}
	out := buf.String()
	if !strings.Contains(out, "id,name") {
		t.Errorf("missing header: %q", out)
	}
	// A value containing the delimiter must be quoted by encoding/csv.
	if !strings.Contains(out, `"bob, jr"`) {
		t.Errorf("delimiter in value not quoted: %q", out)
	}
}

func TestExportRowsTSV(t *testing.T) {
	var buf bytes.Buffer
	_, _ = ExportRows(&buf, []string{"a", "b"}, [][]string{{"1", "2"}}, '\t')
	if !strings.Contains(buf.String(), "a\tb") {
		t.Errorf("tsv header = %q", buf.String())
	}
}

func TestReadDelimitedRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	_, _ = ExportRows(&buf, []string{"id", "name"}, [][]string{{"1", "alice"}, {"2", "bob, jr"}}, ',')

	header, rows, err := ReadDelimited(&buf, ',')
	if err != nil {
		t.Fatalf("ReadDelimited: %v", err)
	}
	if len(header) != 2 || header[0] != "id" || header[1] != "name" {
		t.Errorf("header = %v", header)
	}
	if len(rows) != 2 || rows[1][1] != "bob, jr" {
		t.Errorf("rows = %v", rows)
	}
}

func TestReadDelimitedEmpty(t *testing.T) {
	if _, _, err := ReadDelimited(strings.NewReader(""), ','); err == nil {
		t.Error("empty input should error")
	}
}

// fakeStream implements adapter.RowStream over in-memory rows.
type fakeStream struct {
	cols []string
	rows [][]any
	i    int
}

func (f *fakeStream) Columns() []string { return f.cols }
func (f *fakeStream) Next() bool         { f.i++; return f.i <= len(f.rows) }
func (f *fakeStream) Values() ([]any, error) { return f.rows[f.i-1], nil }
func (f *fakeStream) Err() error  { return nil }
func (f *fakeStream) Close() error { return nil }

func TestExportStreamNilBecomesEmpty(t *testing.T) {
	var buf bytes.Buffer
	rs := &fakeStream{cols: []string{"a", "b"}, rows: [][]any{{1, nil}, {nil, "x"}}}
	n, err := ExportStream(&buf, rs, ',')
	if err != nil || n != 2 {
		t.Fatalf("ExportStream = (%d,%v)", n, err)
	}
	// nil renders as an empty field: "1," and ",x".
	out := buf.String()
	if !strings.Contains(out, "1,\n") || !strings.Contains(out, ",x\n") {
		t.Errorf("nil handling: %q", out)
	}
}
