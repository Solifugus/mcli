//go:build db2

package db2

import (
	"strings"
	"testing"

	"github.com/Solifugus/mcli/internal/core/adapter"
)

func TestRegisteredOnImport(t *testing.T) {
	if !adapter.Registered("db2") {
		t.Fatal(`adapter "db2" not registered`)
	}
	a, err := adapter.New("db2")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if a.Dialect() != adapter.DialectDB2 {
		t.Errorf("dialect = %q", a.Dialect())
	}
}

func TestSplitName(t *testing.T) {
	cases := []struct{ in, schema, obj string }{
		{"MEMBERS", "", "MEMBERS"},
		{"DB2INST1.MEMBERS", "DB2INST1", "MEMBERS"},
	}
	for _, c := range cases {
		s, o := splitName(c.in)
		if s != c.schema || o != c.obj {
			t.Errorf("splitName(%q) = (%q,%q), want (%q,%q)", c.in, s, o, c.schema, c.obj)
		}
	}
}

func TestBuildDSNDiscrete(t *testing.T) {
	dsn, err := buildDSN(adapter.ConnectParams{
		Host: "127.0.0.1", Port: 50000, User: "db2inst1", Password: "ass_test", Database: "testdb",
		Params: map[string]string{"ssl": "false"},
	})
	if err != nil {
		t.Fatalf("buildDSN: %v", err)
	}
	for _, want := range []string{
		"hostname=127.0.0.1", "port=50000", "database=testdb", "uid=db2inst1", "pwd=ass_test", "ssl=false",
	} {
		if !strings.Contains(dsn, want) {
			t.Errorf("dsn missing %q: %s", want, dsn)
		}
	}
}

func TestBuildDSNDefaultsPort(t *testing.T) {
	dsn, err := buildDSN(adapter.ConnectParams{Host: "h", User: "u", Password: "p", Database: "d"})
	if err != nil {
		t.Fatalf("buildDSN: %v", err)
	}
	if !strings.Contains(dsn, "port=50000") {
		t.Errorf("default port not applied: %q", dsn)
	}
}

func TestBuildDSNRequiresHostAndDatabase(t *testing.T) {
	if _, err := buildDSN(adapter.ConnectParams{Host: "h", User: "u"}); err == nil {
		t.Error("buildDSN without database should error")
	}
	if _, err := buildDSN(adapter.ConnectParams{Database: "d", User: "u"}); err == nil {
		t.Error("buildDSN without host should error")
	}
}

func TestBuildDSNConnectionStringPassthrough(t *testing.T) {
	const cs = "hostname=h;port=50000;database=d;uid=u;pwd=p"
	dsn, err := buildDSN(adapter.ConnectParams{ConnectionString: cs})
	if err != nil {
		t.Fatalf("buildDSN: %v", err)
	}
	if dsn != cs {
		t.Errorf("connection string not passed through: %q", dsn)
	}
}
