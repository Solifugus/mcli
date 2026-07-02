package adapter

import "testing"

func TestTabularQueryTablesAndViews(t *testing.T) {
	// Non-table-function objects select directly, regardless of dialect.
	ref := ObjectRef{Schema: "public", Name: "users", Type: string(KindTable)}
	if got := TabularQuery(DialectPostgres, ref); got != "SELECT * FROM public.users" {
		t.Errorf("table query = %q", got)
	}
	// Empty schema omits the qualifier.
	bare := ObjectRef{Name: "v", Type: string(KindView)}
	if got := TabularQuery(DialectMySQL, bare); got != "SELECT * FROM v" {
		t.Errorf("bare view query = %q", got)
	}
}

func TestTabularQueryTableFunctionsByDialect(t *testing.T) {
	tf := ObjectRef{Schema: "app", Name: "recent", Type: string(KindTableFunction)}
	cases := map[Dialect]string{
		DialectPostgres: "SELECT * FROM app.recent(/* args */)",
		DialectTSQL:     "SELECT * FROM app.recent(/* args */)",
		DialectOracle:   "SELECT * FROM TABLE(app.recent(/* args */))",
		DialectDB2:      "SELECT * FROM TABLE(app.recent(/* args */))",
	}
	for d, want := range cases {
		if got := TabularQuery(d, tf); got != want {
			t.Errorf("TabularQuery(%s) = %q, want %q", d, got, want)
		}
	}
}
