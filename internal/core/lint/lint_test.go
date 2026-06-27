package lint

import (
	"strings"
	"testing"

	"github.com/Solifugus/mcli/internal/core/adapter"
)

// has reports whether any finding carries the given rule.
func has(fs []Finding, rule string) bool {
	for _, f := range fs {
		if f.Rule == rule {
			return true
		}
	}
	return false
}

func lintSQL(sql string) []Finding {
	return Lint(sql, Options{Dialect: adapter.DialectPostgres, Style: true})
}

func TestDangerousRules(t *testing.T) {
	cases := []struct {
		sql  string
		rule string
		want bool
	}{
		{"delete from users", "dangerous-sql", true},
		{"delete from users where id = 1", "dangerous-sql", false},
		{"update t set x = 1", "dangerous-sql", true},
		{"update t set x = 1 where id = 2", "dangerous-sql", false},
		{"select * from t where id = 1", "dangerous-sql", false},
	}
	for _, c := range cases {
		if got := has(lintSQL(c.sql), c.rule); got != c.want {
			t.Errorf("%q: %s present = %v, want %v", c.sql, c.rule, got, c.want)
		}
	}
}

func TestSelectStar(t *testing.T) {
	if !has(lintSQL("select * from t"), "select-star") {
		t.Error("SELECT * should be flagged")
	}
	if has(lintSQL("select id, name from t"), "select-star") {
		t.Error("explicit columns should not be flagged")
	}
	// A literal asterisk in a string must not trip the rule.
	if has(lintSQL("select id from t where note = 'select * from'"), "select-star") {
		t.Error("'*' inside a string literal must not be flagged")
	}
	// With style off, no select-star finding.
	if has(Lint("select * from t", Options{Style: false}), "select-star") {
		t.Error("select-star must be gated by Style")
	}
}

func TestUnbalancedParens(t *testing.T) {
	if !has(lintSQL("select * from t where id in (1, 2"), "unbalanced-parens") {
		t.Error("missing ')' should be flagged")
	}
	if has(lintSQL("select count(*) from t"), "unbalanced-parens") {
		t.Error("balanced parens should not be flagged")
	}
	// A ')' inside a string is masked and must not count.
	if has(lintSQL("select ':)' as smiley"), "unbalanced-parens") {
		t.Error("paren inside a string must not unbalance")
	}
}

func TestUnterminated(t *testing.T) {
	if !has(lintSQL("select 'oops"), "unterminated") {
		t.Error("unterminated string should be flagged")
	}
	if !has(lintSQL("select 1 /* open"), "unterminated") {
		t.Error("unterminated block comment should be flagged")
	}
	if has(lintSQL("select 1 -- trailing line comment"), "unterminated") {
		t.Error("a line comment is never unterminated")
	}
	if has(lintSQL("select 'closed' from t"), "unterminated") {
		t.Error("a closed string must not be flagged")
	}
}

func TestMissingJoinCondition(t *testing.T) {
	if !has(lintSQL("select * from a join b"), "missing-join-condition") {
		t.Error("JOIN without ON should be flagged")
	}
	if has(lintSQL("select * from a join b on a.id = b.id"), "missing-join-condition") {
		t.Error("JOIN with ON should not be flagged")
	}
	if has(lintSQL("select * from a cross join b"), "missing-join-condition") {
		t.Error("explicit CROSS JOIN should not be flagged")
	}
}

func TestUnknownStatement(t *testing.T) {
	if !has(lintSQL("flibber from t"), "unknown-statement") {
		t.Error("an unrecognized leading keyword should be flagged")
	}
	if has(lintSQL("select 1"), "unknown-statement") {
		t.Error("SELECT is a known statement")
	}
}

func TestStyleLineRules(t *testing.T) {
	fs := lintSQL("select 1   \n\tselect 2")
	if !has(fs, "trailing-whitespace") {
		t.Error("trailing whitespace should be flagged")
	}
	if !has(fs, "tab-indent") {
		t.Error("tab indentation should be flagged")
	}
}

func TestKeywordCase(t *testing.T) {
	opts := Options{Dialect: adapter.DialectPostgres, Style: true, KeywordCase: "upper"}
	if !has(Lint("select 1", opts), "keyword-case") {
		t.Error("lowercase select should be flagged when upper is required")
	}
	if has(Lint("SELECT 1", opts), "keyword-case") {
		t.Error("uppercase SELECT should pass an upper requirement")
	}
}

func TestMultiStatementPositions(t *testing.T) {
	// The DELETE is on line 2; its finding must point there, not at line 1.
	sql := "select 1;\ndelete from users;"
	fs := lintSQL(sql)
	var found *Finding
	for i := range fs {
		if fs[i].Rule == "dangerous-sql" {
			found = &fs[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected a dangerous-sql finding for the DELETE")
	}
	if found.Line != 2 {
		t.Errorf("DELETE finding at line %d, want 2", found.Line)
	}
}

func TestSemicolonInStringDoesNotSplit(t *testing.T) {
	// The ';' lives in a string, so this is one statement, not two; a DELETE
	// keyword inside the string must not be classified as dangerous.
	fs := lintSQL("select 'a; delete from t' as note")
	if has(fs, "dangerous-sql") {
		t.Error("keyword inside a string literal must not be flagged dangerous")
	}
}

func TestLineCol(t *testing.T) {
	s := "ab\ncde"
	if l, c := LineCol(s, 0); l != 1 || c != 1 {
		t.Errorf("offset 0 = %d:%d, want 1:1", l, c)
	}
	if l, c := LineCol(s, 3); l != 2 || c != 1 {
		t.Errorf("offset 3 (after \\n) = %d:%d, want 2:1", l, c)
	}
	if l, c := LineCol(s, 5); l != 2 || c != 3 {
		t.Errorf("offset 5 = %d:%d, want 2:3", l, c)
	}
}

func TestSeverityJSON(t *testing.T) {
	for sev, want := range map[Severity]string{Info: "info", Warning: "warning", Error: "error"} {
		b, err := sev.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Trim(string(b), `"`); got != want {
			t.Errorf("severity %d marshaled %q, want %q", sev, got, want)
		}
	}
}
