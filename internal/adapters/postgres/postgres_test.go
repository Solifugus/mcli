package postgres

import (
	"context"
	"os"
	"testing"

	"github.com/Solifugus/mcli/internal/core/adapter"
)

func TestRegisteredOnImport(t *testing.T) {
	if !adapter.Registered("postgres") {
		t.Fatal(`adapter "postgres" not registered`)
	}
	a, err := adapter.New("postgres")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if a.Dialect() != adapter.DialectPostgres {
		t.Errorf("dialect = %q", a.Dialect())
	}
}

func TestSplitName(t *testing.T) {
	cases := []struct{ in, schema, obj string }{
		{"customer", "", "customer"},
		{"public.customer", "public", "customer"},
		{"sales.fact_orders", "sales", "fact_orders"},
	}
	for _, c := range cases {
		s, o := splitName(c.in)
		if s != c.schema || o != c.obj {
			t.Errorf("splitName(%q) = (%q,%q), want (%q,%q)", c.in, s, o, c.schema, c.obj)
		}
	}
}

func TestBuildConfigDiscrete(t *testing.T) {
	cfg, err := buildConfig(adapter.ConnectParams{
		Host: "db.example.com", Port: 5433, User: "mathew",
		Password: "secret", Database: "etldb",
	})
	if err != nil {
		t.Fatalf("buildConfig: %v", err)
	}
	if cfg.Host != "db.example.com" || cfg.Port != 5433 ||
		cfg.User != "mathew" || cfg.Password != "secret" || cfg.Database != "etldb" {
		t.Errorf("config = %+v", cfg)
	}
}

func TestBuildConfigConnString(t *testing.T) {
	cfg, err := buildConfig(adapter.ConnectParams{
		ConnectionString: "postgres://u:p@host:6000/mydb",
	})
	if err != nil {
		t.Fatalf("buildConfig: %v", err)
	}
	if cfg.Host != "host" || cfg.Port != 6000 || cfg.Database != "mydb" {
		t.Errorf("config = %+v", cfg)
	}
}

func TestKVQuote(t *testing.T) {
	cases := map[string]string{
		"simple":      "simple",
		"Donkey1!":    "Donkey1!",          // no space/quote/backslash -> unquoted
		"has space":   "'has space'",
		`back\slash`:  `'back\\slash'`,
		`quote'd`:     `'quote\'d'`,
	}
	for in, want := range cases {
		if got := kvQuote(in); got != want {
			t.Errorf("kvQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDiscreteDSNOmitsEmptyPassword(t *testing.T) {
	// With no password, the DSN must not carry "password=" so pgx can fall back
	// to ~/.pgpass.
	dsn := discreteDSN(adapter.ConnectParams{Host: "h", Port: 5432, User: "u", Database: "d"})
	if contains(dsn, "password=") {
		t.Errorf("DSN should omit empty password: %q", dsn)
	}
	withPw := discreteDSN(adapter.ConnectParams{Host: "h", User: "u", Password: "p"})
	if !contains(withPw, "password=p") {
		t.Errorf("DSN should include password: %q", withPw)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestDisconnectWhenNotConnected(t *testing.T) {
	if err := (&Adapter{}).Disconnect(); err != nil {
		t.Errorf("Disconnect on fresh adapter = %v, want nil", err)
	}
}

func TestMethodsRequireConnection(t *testing.T) {
	a := &Adapter{}
	ctx := context.Background()
	if _, err := a.ListDatabases(ctx); err == nil {
		t.Error("ListDatabases without connection should error")
	}
	if _, err := a.RunQuery(ctx, "select 1"); err == nil {
		t.Error("RunQuery without connection should error")
	}
}

// --- integration: runs only when MCLI_PG_DSN points at a real database ---

func liveAdapter(t *testing.T) *Adapter {
	t.Helper()
	dsn := os.Getenv("MCLI_PG_DSN")
	if dsn == "" {
		t.Skip("set MCLI_PG_DSN to run Postgres integration tests")
	}
	a := &Adapter{}
	if err := a.Connect(context.Background(), adapter.ConnectParams{ConnectionString: dsn}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = a.Disconnect() })
	return a
}

func TestLiveSelectOne(t *testing.T) {
	a := liveAdapter(t)
	rs, err := a.RunQuery(context.Background(), "SELECT 1 AS n")
	if err != nil {
		t.Fatalf("RunQuery: %v", err)
	}
	defer rs.Close()
	if cols := rs.Columns(); len(cols) != 1 || cols[0] != "n" {
		t.Fatalf("columns = %v", rs.Columns())
	}
	if !rs.Next() {
		t.Fatal("expected a row")
	}
	vals, err := rs.Values()
	if err != nil {
		t.Fatalf("Values: %v", err)
	}
	if len(vals) != 1 {
		t.Fatalf("values = %v", vals)
	}
}

func TestLiveListDatabases(t *testing.T) {
	a := liveAdapter(t)
	dbs, err := a.ListDatabases(context.Background())
	if err != nil {
		t.Fatalf("ListDatabases: %v", err)
	}
	if len(dbs) == 0 {
		t.Error("expected at least one database")
	}
}
