package oracle

import (
	"strings"
	"testing"

	"github.com/Solifugus/mcli/internal/core/adapter"
)

func TestRegisteredOnImport(t *testing.T) {
	if !adapter.Registered("oracle") {
		t.Fatal(`adapter "oracle" not registered`)
	}
	a, err := adapter.New("oracle")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if a.Dialect() != adapter.DialectOracle {
		t.Errorf("dialect = %q", a.Dialect())
	}
}

func TestSplitName(t *testing.T) {
	cases := []struct{ in, schema, obj string }{
		{"EMPLOYEES", "", "EMPLOYEES"},
		{"HR.EMPLOYEES", "HR", "EMPLOYEES"},
	}
	for _, c := range cases {
		s, o := splitName(c.in)
		if s != c.schema || o != c.obj {
			t.Errorf("splitName(%q) = (%q,%q), want (%q,%q)", c.in, s, o, c.schema, c.obj)
		}
	}
}

func TestQuoteIdent(t *testing.T) {
	if got := quoteIdent("HR"); got != `"HR"` {
		t.Errorf("quoteIdent = %q", got)
	}
	if got := quoteIdent(`we"ird`); got != `"we""ird"` {
		t.Errorf("quoteIdent = %q", got)
	}
}

func TestBuildDSNDiscrete(t *testing.T) {
	dsn, err := buildDSN(adapter.ConnectParams{
		Host: "localhost", Port: 1521, User: "system", Password: "ass_test", Database: "FREEPDB1",
	})
	if err != nil {
		t.Fatalf("buildDSN: %v", err)
	}
	if !strings.HasPrefix(dsn, "oracle://") || !strings.Contains(dsn, "system") || !strings.Contains(dsn, "FREEPDB1") {
		t.Errorf("dsn = %q", dsn)
	}
}

func TestBuildDSNDefaultsPort(t *testing.T) {
	dsn, err := buildDSN(adapter.ConnectParams{Host: "h", User: "u", Password: "p", Database: "svc"})
	if err != nil {
		t.Fatalf("buildDSN: %v", err)
	}
	if !strings.Contains(dsn, ":1521") {
		t.Errorf("default port not applied: %q", dsn)
	}
}

func TestBuildDSNConnectionStringPassthrough(t *testing.T) {
	const cs = "oracle://u:p@host:1599/SVC"
	dsn, err := buildDSN(adapter.ConnectParams{ConnectionString: cs})
	if err != nil {
		t.Fatalf("buildDSN: %v", err)
	}
	if dsn != cs {
		t.Errorf("connection string not passed through: %q", dsn)
	}
}

func TestBuildDSNRequiresHost(t *testing.T) {
	if _, err := buildDSN(adapter.ConnectParams{User: "u"}); err == nil {
		t.Error("buildDSN without host or connection string should error")
	}
}
