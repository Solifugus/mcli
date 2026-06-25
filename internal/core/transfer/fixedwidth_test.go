package transfer

import (
	"bytes"
	"strings"
	"testing"
)

func TestIsFixedWidth(t *testing.T) {
	for path, want := range map[string]bool{
		"a.txt": true, "a.FIX": true, "a.fix": true, "a.csv": false, "a.xlsx": false, "a": false,
	} {
		if got := IsFixedWidth(path); got != want {
			t.Errorf("IsFixedWidth(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestFixedWidths(t *testing.T) {
	cols := []string{"id", "name"}
	rows := [][]string{{"1", "alice"}, {"100", "bo"}}
	got := FixedWidths(cols, rows)
	// id column: max(len "id"=2, "1", "100"=3) = 3; name: max("name"=4,"alice"=5,"bo") = 5
	if got[0] != 3 || got[1] != 5 {
		t.Errorf("FixedWidths = %v, want [3 5]", got)
	}
}

func TestWriteFixedRowsAlignment(t *testing.T) {
	var buf bytes.Buffer
	cols := []string{"id", "name"}
	rows := [][]string{{"1", "alice"}, {"100", "bo"}}
	n, err := WriteFixedRows(&buf, cols, rows, FixedWidths(cols, rows))
	if err != nil || n != 2 {
		t.Fatalf("WriteFixedRows = (%d,%v)", n, err)
	}
	want := "id  name \n1   alice\n100 bo   \n"
	if buf.String() != want {
		t.Errorf("output =\n%q\nwant\n%q", buf.String(), want)
	}
}

func TestFixedRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	cols := []string{"id", "name", "city"}
	rows := [][]string{{"1", "alice", "ny"}, {"100", "bob", "la"}, {"7", "", "sf"}}
	widths := FixedWidths(cols, rows)
	if _, err := WriteFixedRows(&buf, cols, rows, widths); err != nil {
		t.Fatalf("WriteFixedRows: %v", err)
	}

	// Drop the header line before re-reading: fixed-width files have no header,
	// so ReadFixed returns only data rows.
	body := buf.String()
	body = body[strings.IndexByte(body, '\n')+1:]

	got, err := ReadFixed(strings.NewReader(body), widths)
	if err != nil {
		t.Fatalf("ReadFixed: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d rows, want 3", len(got))
	}
	if got[0][1] != "alice" || got[1][0] != "100" || got[2][2] != "sf" {
		t.Errorf("rows = %v", got)
	}
	if got[2][1] != "" {
		t.Errorf("empty field = %q, want empty", got[2][1])
	}
}

func TestMeasureAndWriteFixedStream(t *testing.T) {
	measure := &fakeStream{cols: []string{"a", "bb"}, rows: [][]any{{1, "xyz"}, {2222, nil}}}
	widths, err := MeasureFixedStream(measure)
	if err != nil {
		t.Fatalf("MeasureFixedStream: %v", err)
	}
	// a: max("a"=1, "1", "2222"=4) = 4; bb: max("bb"=2, "xyz"=3, "") = 3
	if widths[0] != 4 || widths[1] != 3 {
		t.Errorf("widths = %v, want [4 3]", widths)
	}

	write := &fakeStream{cols: []string{"a", "bb"}, rows: [][]any{{1, "xyz"}, {2222, nil}}}
	var buf bytes.Buffer
	n, err := WriteFixedStream(&buf, write, widths)
	if err != nil || n != 2 {
		t.Fatalf("WriteFixedStream = (%d,%v)", n, err)
	}
	want := "a    bb \n1    xyz\n2222    \n"
	if buf.String() != want {
		t.Errorf("output =\n%q\nwant\n%q", buf.String(), want)
	}
}

func TestWriteFixedLongValueNotTruncated(t *testing.T) {
	var buf bytes.Buffer
	// width 2 for col 0 but value is longer — it must be written in full.
	if _, err := WriteFixedRows(&buf, []string{"a", "b"}, [][]string{{"toolong", "x"}}, []int{2, 1}); err != nil {
		t.Fatalf("WriteFixedRows: %v", err)
	}
	if !strings.Contains(buf.String(), "toolong") {
		t.Errorf("long value was truncated: %q", buf.String())
	}
}
