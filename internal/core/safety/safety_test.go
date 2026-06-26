package safety

import "testing"

func TestClassify(t *testing.T) {
	cases := []struct {
		sql       string
		readOnly  bool
		dangerous bool
		verb      string
	}{
		{"SELECT * FROM t", true, false, "SELECT"},
		{"  with x as (select 1) select * from x", true, false, "WITH"},
		{"explain select * from t", true, false, "EXPLAIN"},
		{"DROP TABLE t", false, true, "DROP"},
		{"truncate table t", false, true, "TRUNCATE"},
		{"alter table t add col int", false, true, "ALTER"},
		{"merge into t using s on (t.id=s.id)", false, true, "MERGE"},
		{"insert into t values (1)", false, true, "INSERT"},
		{"create index ix on t(a)", false, true, "CREATE"},
		{"create table t (a int)", false, false, "CREATE"}, // CREATE TABLE not in list
		{"delete from t", false, true, "DELETE"},           // no WHERE → dangerous
		{"delete from t where id = 1", false, false, "DELETE"},
		{"update t set a = 1", false, true, "UPDATE"},
		{"update t set a = 1 where id = 2", false, false, "UPDATE"},
		{"", false, false, ""},
	}
	for _, c := range cases {
		v := Classify(c.sql, nil)
		if v.ReadOnly != c.readOnly || v.Dangerous != c.dangerous || v.Verb != c.verb {
			t.Errorf("Classify(%q) = {verb:%q ro:%v danger:%v}, want {verb:%q ro:%v danger:%v}",
				c.sql, v.Verb, v.ReadOnly, v.Dangerous, c.verb, c.readOnly, c.dangerous)
		}
	}
}

func TestClassifyWhereInLiteralIsStillDangerous(t *testing.T) {
	// A WHERE living inside a string literal must not disarm the without-WHERE rule.
	v := Classify("delete from t -- no real where\n", nil)
	if !v.Dangerous {
		t.Fatal("DELETE with WHERE only in a comment should be dangerous")
	}
	v = Classify("update notes set body = 'change where it matters'", nil)
	if !v.Dangerous {
		t.Fatal("UPDATE whose only WHERE is inside a string literal should be dangerous")
	}
}

func TestClassifyKeywordInLiteralNotMatched(t *testing.T) {
	// "DROP" inside a literal must not flag a plain INSERT-less read.
	v := Classify("select 'please DROP nothing' as note", nil)
	if v.Dangerous || !v.ReadOnly {
		t.Fatalf("literal DROP misclassified: %+v", v)
	}
}

func TestDecideReadOnlyMode(t *testing.T) {
	p := Policy{ReadOnly: true}
	if a, _ := p.Decide(Classify("select 1", nil), "dev"); a != Allow {
		t.Error("read in read-only mode should Allow")
	}
	if a, _ := p.Decide(Classify("insert into t values (1)", nil), "dev"); a != Block {
		t.Error("write in read-only mode should Block")
	}
}

func TestDecideDangerous(t *testing.T) {
	drop := Classify("drop table t", nil)

	// Confirmation disabled, non-prod: allowed.
	if a, _ := (Policy{}).Decide(drop, "dev"); a != Allow {
		t.Error("dangerous with confirm off, non-prod should Allow")
	}
	// Confirmation enabled: confirm.
	if a, _ := (Policy{ConfirmDangerous: true}).Decide(drop, "dev"); a != Confirm {
		t.Error("dangerous with confirm on should Confirm")
	}
	// Prod always confirms a dangerous statement even with confirm off.
	if a, _ := (Policy{}).Decide(drop, "prod"); a != Confirm {
		t.Error("dangerous on prod should Confirm even with confirm off")
	}
	// Prod + block-on-prod: blocked.
	if a, _ := (Policy{BlockDangerousOnProd: true}).Decide(drop, "prod"); a != Block {
		t.Error("dangerous on prod with block-on-prod should Block")
	}
}

func TestDecideProdWriteConfirms(t *testing.T) {
	upd := Classify("update t set a=1 where id=2", nil) // a write, not dangerous
	if a, _ := (Policy{}).Decide(upd, "prod"); a != Confirm {
		t.Error("plain write on prod should Confirm")
	}
	if a, _ := (Policy{}).Decide(upd, "dev"); a != Allow {
		t.Error("plain write on dev should Allow")
	}
}

func TestCustomKeywords(t *testing.T) {
	v := Classify("grant select on t to bob", []string{"GRANT"})
	if !v.Dangerous {
		t.Error("custom GRANT keyword should be dangerous")
	}
	// And the default DROP is no longer dangerous under a custom list.
	v = Classify("drop table t", []string{"GRANT"})
	if v.Dangerous {
		t.Error("DROP should not be dangerous under a GRANT-only list")
	}
}
