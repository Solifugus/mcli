package mssql

import (
	"net/url"
	"testing"

	"github.com/Solifugus/mcli/internal/core/adapter"
)

func TestRegisteredOnImport(t *testing.T) {
	if !adapter.Registered("sqlserver") {
		t.Fatal(`adapter "sqlserver" not registered`)
	}
	a, err := adapter.New("sqlserver")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if a.Dialect() != adapter.DialectTSQL {
		t.Errorf("dialect = %q", a.Dialect())
	}
}

func TestSplitName(t *testing.T) {
	cases := []struct{ in, schema, obj string }{
		{"customer", "", "customer"},
		{"dbo.customer", "dbo", "customer"},
		{"sales.fact_orders", "sales", "fact_orders"},
	}
	for _, c := range cases {
		s, o := splitName(c.in)
		if s != c.schema || o != c.obj {
			t.Errorf("splitName(%q) = (%q,%q), want (%q,%q)", c.in, s, o, c.schema, c.obj)
		}
	}
}

func TestBuildDSNDiscrete(t *testing.T) {
	u, err := buildDSN(adapter.ConnectParams{
		Host:     "192.168.122.178",
		Port:     1433,
		User:     "ass",
		Password: "PASS",
		Database: "assdb",
		Params:   map[string]string{"encrypt": "disable"},
	})
	if err != nil {
		t.Fatalf("buildDSN: %v", err)
	}
	if u.Scheme != "sqlserver" {
		t.Errorf("scheme = %q", u.Scheme)
	}
	if u.Host != "192.168.122.178:1433" {
		t.Errorf("host = %q", u.Host)
	}
	if name := u.User.Username(); name != "ass" {
		t.Errorf("user = %q", name)
	}
	if pw, ok := u.User.Password(); !ok || pw != "PASS" {
		t.Errorf("password not carried")
	}
	q := u.Query()
	if q.Get("database") != "assdb" || q.Get("encrypt") != "disable" {
		t.Errorf("query = %q", u.RawQuery)
	}
}

func TestBuildDSNNoEncryptDefault(t *testing.T) {
	// No encryption flag is imposed unless the server Options set one — a
	// production host must never be silently downgraded.
	u, err := buildDSN(adapter.ConnectParams{Host: "h", Port: 1433, User: "u", Database: "d"})
	if err != nil {
		t.Fatalf("buildDSN: %v", err)
	}
	if u.Query().Has("encrypt") {
		t.Errorf("encrypt should be unset by default, got %q", u.RawQuery)
	}
}

func TestBuildDSNConnectionStringPassthrough(t *testing.T) {
	const cs = "sqlserver://u:p@host:1433?database=d&encrypt=true"
	u, err := buildDSN(adapter.ConnectParams{ConnectionString: cs})
	if err != nil {
		t.Fatalf("buildDSN: %v", err)
	}
	want, _ := url.Parse(cs)
	if u.String() != want.String() {
		t.Errorf("connection string not passed through: %q", u.String())
	}
}
