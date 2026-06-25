package transfer

import (
	"bytes"
	"testing"
)

func TestIsXLSX(t *testing.T) {
	for path, want := range map[string]bool{
		"a.xlsx": true, "a.XLSX": true, "a.csv": false, "a": false, "x.xls": false,
	} {
		if got := IsXLSX(path); got != want {
			t.Errorf("IsXLSX(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestXLSXRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	n, err := ExportXLSXRows(&buf, []string{"id", "name"}, [][]string{{"1", "alice"}, {"2", "bob"}}, "")
	if err != nil || n != 2 {
		t.Fatalf("ExportXLSXRows = (%d,%v)", n, err)
	}

	header, rows, err := ReadXLSX(bytes.NewReader(buf.Bytes()), "")
	if err != nil {
		t.Fatalf("ReadXLSX: %v", err)
	}
	if len(header) != 2 || header[0] != "id" || header[1] != "name" {
		t.Errorf("header = %v", header)
	}
	if len(rows) != 2 || rows[0][1] != "alice" || rows[1][0] != "2" {
		t.Errorf("rows = %v", rows)
	}
}

func TestReadXLSXMissingSheet(t *testing.T) {
	var buf bytes.Buffer
	_, _ = ExportXLSXRows(&buf, []string{"a"}, [][]string{{"1"}}, "")
	if _, _, err := ReadXLSX(bytes.NewReader(buf.Bytes()), "NoSuchSheet"); err == nil {
		t.Error("reading a missing sheet should error")
	}
}

func TestExportXLSXStreamNative(t *testing.T) {
	var buf bytes.Buffer
	rs := &fakeStream{cols: []string{"a", "b"}, rows: [][]any{{1, "x"}, {2, nil}}}
	n, err := ExportXLSXStream(&buf, rs, "")
	if err != nil || n != 2 {
		t.Fatalf("ExportXLSXStream = (%d,%v)", n, err)
	}
	_, rows, err := ReadXLSX(bytes.NewReader(buf.Bytes()), "")
	if err != nil {
		t.Fatalf("ReadXLSX: %v", err)
	}
	if len(rows) != 2 || rows[0][0] != "1" {
		t.Errorf("rows = %v", rows)
	}
}
