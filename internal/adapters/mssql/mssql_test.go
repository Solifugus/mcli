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

func TestCapabilities(t *testing.T) {
	caps := (&Adapter{}).Capabilities()
	if caps.Has(adapter.CapExplain) {
		t.Error("SQL Server has no EXPLAIN; CapExplain must not be advertised")
	}
	if !caps.Has(adapter.CapJobs) {
		t.Error("SQL Server should advertise CapJobs (SQL Server Agent)")
	}
	if !caps.Has(adapter.CapSecurity) {
		t.Error("SQL Server should advertise CapSecurity")
	}
	if !caps.Has(adapter.CapSecurityEdit) {
		t.Error("SQL Server should advertise CapSecurityEdit")
	}
}

func TestPrincipalKind(t *testing.T) {
	if principalKind("R") != adapter.PrincipalKindRole {
		t.Error("type R should be a role")
	}
	for _, typ := range []string{"S", "U", "G"} {
		if principalKind(typ) != adapter.PrincipalKindUser {
			t.Errorf("type %s should be a user", typ)
		}
	}
}

func TestPrincipalTypeLabel(t *testing.T) {
	cases := map[string]string{"S": "SQL_USER", "U": "WINDOWS_USER", "G": "WINDOWS_GROUP", "R": "DATABASE_ROLE"}
	for typ, want := range cases {
		if got := principalTypeLabel(typ); got != want {
			t.Errorf("principalTypeLabel(%q) = %q, want %q", typ, got, want)
		}
	}
}

func TestAgentRunStatus(t *testing.T) {
	cases := map[int]string{0: "failed", 1: "succeeded", 2: "retry", 3: "canceled", 4: "running"}
	for in, want := range cases {
		if got := agentRunStatus(in); got != want {
			t.Errorf("agentRunStatus(%d) = %q, want %q", in, got, want)
		}
	}
	if got := agentRunStatus(9); got == "" || got == "failed" {
		t.Errorf("unknown status should be descriptive, got %q", got)
	}
}

func TestFmtAgentDateTime(t *testing.T) {
	if got := fmtAgentDateTime(20260701, 143005); got != "2026-07-01 14:30:05" {
		t.Errorf("fmtAgentDateTime = %q", got)
	}
	// Midnight and single-digit fields zero-pad.
	if got := fmtAgentDateTime(20260101, 5); got != "2026-01-01 00:00:05" {
		t.Errorf("fmtAgentDateTime pad = %q", got)
	}
	// A zero date (never run / not scheduled) is empty, not a bogus timestamp.
	if got := fmtAgentDateTime(0, 0); got != "" {
		t.Errorf("fmtAgentDateTime(0,0) = %q, want empty", got)
	}
}

func TestFmtAgentDuration(t *testing.T) {
	if got := fmtAgentDuration(13005); got != "1:30:05" {
		t.Errorf("fmtAgentDuration = %q", got)
	}
	if got := fmtAgentDuration(0); got != "" {
		t.Errorf("fmtAgentDuration(0) = %q, want empty", got)
	}
}
