package safety

import "testing"

func texts(sql string, spans []Span) []string {
	out := make([]string, len(spans))
	for i, s := range spans {
		out[i] = sql[s.Start:s.End]
	}
	return out
}

func TestStatementSpans(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want []string
	}{
		{"empty", "", nil},
		{"blank", "   \n\t ", nil},
		{"single no semicolon", "select 1", []string{"select 1"}},
		{"single trailing semicolon", "select 1;", []string{"select 1"}},
		{"two statements", "select 1; select 2", []string{"select 1", "select 2"}},
		{"trims whitespace", "  select 1 ;\n\n  select 2 ; ", []string{"select 1", "select 2"}},
		{"multiline statement", "select a,\n b\nfrom t;", []string{"select a,\n b\nfrom t"}},
		{"empty segments dropped", "select 1;;;select 2;", []string{"select 1", "select 2"}},
		{
			"semicolon in string literal does not split",
			"insert into t values (';not a split');",
			[]string{"insert into t values (';not a split')"},
		},
		{
			"semicolon in line comment does not split",
			"select 1 -- ; not a split\n;select 2",
			[]string{"select 1 -- ; not a split", "select 2"},
		},
		{
			"semicolon in block comment does not split",
			"select 1 /* ; still one */ from t; select 2",
			[]string{"select 1 /* ; still one */ from t", "select 2"},
		},
		{
			"semicolon in quoted identifier does not split",
			`select "a;b" from t; select 2`,
			[]string{`select "a;b" from t`, "select 2"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := texts(c.sql, StatementSpans(c.sql))
			if len(got) != len(c.want) {
				t.Fatalf("StatementSpans(%q) = %q, want %q", c.sql, got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("statement %d = %q, want %q", i, got[i], c.want[i])
				}
			}
		})
	}
}

func TestStatementAt(t *testing.T) {
	sql := "select 1;\nupdate t set x=1 where id=2;\nselect 3"
	// Offsets: pick a point inside each statement and confirm we get that one.
	mid1 := 3                          // inside "select 1"
	mid2 := indexOf(sql, "update") + 2 // inside the update
	mid3 := indexOf(sql, "select 3")   // start of the last statement

	for _, c := range []struct {
		off  int
		want string
	}{
		{mid1, "select 1"},
		{mid2, "update t set x=1 where id=2"},
		{mid3, "select 3"},
		{len(sql), "select 3"}, // cursor at very end → last statement
	} {
		span, ok := StatementAt(sql, c.off)
		if !ok {
			t.Errorf("StatementAt(off=%d) not found, want %q", c.off, c.want)
			continue
		}
		if got := sql[span.Start:span.End]; got != c.want {
			t.Errorf("StatementAt(off=%d) = %q, want %q", c.off, got, c.want)
		}
	}
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
