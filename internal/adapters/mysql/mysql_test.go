package mysql

import (
	"strings"
	"testing"

	"github.com/Solifugus/mcli/internal/core/adapter"
)

func TestRegisteredOnImport(t *testing.T) {
	if !adapter.Registered("mysql") {
		t.Fatal(`adapter "mysql" not registered`)
	}
	a, err := adapter.New("mysql")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if a.Dialect() != adapter.DialectMySQL {
		t.Errorf("dialect = %q", a.Dialect())
	}
}

func TestSplitName(t *testing.T) {
	cases := []struct{ in, schema, obj string }{
		{"members", "", "members"},
		{"shop.orders", "shop", "orders"},
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
		Host: "127.0.0.1", Port: 3306, User: "root", Password: "secret", Database: "shop",
		Params: map[string]string{"charset": "utf8mb4"},
	})
	if err != nil {
		t.Fatalf("buildConfig: %v", err)
	}
	if cfg.Addr != "127.0.0.1:3306" || cfg.User != "root" || cfg.Passwd != "secret" || cfg.DBName != "shop" {
		t.Errorf("config = %+v", cfg)
	}
	dsn := cfg.FormatDSN()
	if !strings.Contains(dsn, "@tcp(127.0.0.1:3306)/shop") || !strings.Contains(dsn, "charset=utf8mb4") {
		t.Errorf("DSN = %q", dsn)
	}
}

func TestBuildConfigDefaultsHostPort(t *testing.T) {
	cfg, err := buildConfig(adapter.ConnectParams{User: "root", Database: "d"})
	if err != nil {
		t.Fatalf("buildConfig: %v", err)
	}
	if cfg.Addr != "127.0.0.1:3306" {
		t.Errorf("default addr = %q", cfg.Addr)
	}
}

func TestBuildConfigDSNPassthrough(t *testing.T) {
	cfg, err := buildConfig(adapter.ConnectParams{ConnectionString: "root:pw@tcp(db:3307)/app?parseTime=true"})
	if err != nil {
		t.Fatalf("buildConfig: %v", err)
	}
	if cfg.Addr != "db:3307" || cfg.DBName != "app" {
		t.Errorf("parsed DSN = %+v", cfg)
	}
}
