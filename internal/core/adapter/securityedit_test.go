package adapter

import (
	"errors"
	"testing"
)

func TestGrantStatementPrivileges(t *testing.T) {
	got, err := GrantStatement(DialectPostgres, []string{"SELECT", "INSERT"}, "sales.orders", "bob", false)
	if err != nil {
		t.Fatalf("GrantStatement: %v", err)
	}
	if got != "GRANT SELECT, INSERT ON sales.orders TO bob" {
		t.Errorf("grant = %q", got)
	}
	got, err = GrantStatement(DialectPostgres, []string{"SELECT"}, "sales.orders", "bob", true)
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if got != "REVOKE SELECT ON sales.orders FROM bob" {
		t.Errorf("revoke = %q", got)
	}
}

func TestGrantStatementRole(t *testing.T) {
	// Empty object => role grant; items are role names.
	got, err := GrantStatement(DialectPostgres, []string{"read_role"}, "", "bob", false)
	if err != nil {
		t.Fatalf("role grant: %v", err)
	}
	if got != "GRANT read_role TO bob" {
		t.Errorf("role grant = %q", got)
	}
}

func TestGrantStatementMySQLPrincipal(t *testing.T) {
	got, err := GrantStatement(DialectMySQL, []string{"SELECT"}, "app.t", "bob", false)
	if err != nil {
		t.Fatalf("mysql grant: %v", err)
	}
	if got != "GRANT SELECT ON app.t TO 'bob'@'%'" {
		t.Errorf("mysql grant = %q", got)
	}
	got, err = GrantStatement(DialectMySQL, []string{"SELECT"}, "app.t", "bob@localhost", false)
	if err != nil {
		t.Fatalf("mysql grant host: %v", err)
	}
	if got != "GRANT SELECT ON app.t TO 'bob'@'localhost'" {
		t.Errorf("mysql grant host = %q", got)
	}
}

func TestGrantStatementRejectsInjection(t *testing.T) {
	cases := []struct {
		name              string
		items             []string
		object, principal string
	}{
		{"privilege injection", []string{"SELECT; DROP TABLE t"}, "t", "bob"},
		{"object injection", []string{"SELECT"}, "t; DROP TABLE x", "bob"},
		{"principal injection", []string{"SELECT"}, "t", "bob; --"},
		{"empty items", nil, "t", "bob"},
		{"quote in principal", []string{"SELECT"}, "t", "bo'b"},
	}
	for _, c := range cases {
		if _, err := GrantStatement(DialectPostgres, c.items, c.object, c.principal, false); err == nil {
			t.Errorf("%s: expected an error, got none", c.name)
		}
	}
}

func TestCreateUserStatement(t *testing.T) {
	cases := map[Dialect]string{
		DialectPostgres: "CREATE USER bob PASSWORD 'sekret'",
		DialectMySQL:    "CREATE USER 'bob'@'%' IDENTIFIED BY 'sekret'",
		DialectTSQL:     "CREATE LOGIN bob WITH PASSWORD = 'sekret'",
		DialectOracle:   `CREATE USER bob IDENTIFIED BY "sekret"`,
	}
	for d, want := range cases {
		got, err := CreateUserStatement(d, "bob", "sekret")
		if err != nil {
			t.Errorf("CreateUserStatement(%s): %v", d, err)
			continue
		}
		if got != want {
			t.Errorf("CreateUserStatement(%s) = %q, want %q", d, got, want)
		}
	}
	// DB2 / generic have no portable form.
	if _, err := CreateUserStatement(DialectDB2, "bob", "x"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("DB2 CreateUser = %v, want ErrUnsupported", err)
	}
	// A password is required.
	if _, err := CreateUserStatement(DialectPostgres, "bob", ""); err == nil {
		t.Error("empty password should error")
	}
	// The password is escaped as a SQL string literal (Postgres/MySQL/T-SQL).
	got, err := CreateUserStatement(DialectPostgres, "bob", "o'brien")
	if err != nil {
		t.Fatalf("escape: %v", err)
	}
	if got != "CREATE USER bob PASSWORD 'o''brien'" {
		t.Errorf("password not escaped: %q", got)
	}
	// An injected identifier is refused.
	if _, err := CreateUserStatement(DialectPostgres, "bob; DROP", "x"); err == nil {
		t.Error("injected user name should error")
	}
}

func TestDropUserStatement(t *testing.T) {
	cases := map[Dialect]string{
		DialectPostgres: "DROP ROLE bob",
		DialectMySQL:    "DROP USER 'bob'@'%'",
		DialectTSQL:     "DROP LOGIN bob",
		DialectOracle:   "DROP USER bob",
	}
	for d, want := range cases {
		got, err := DropUserStatement(d, "bob")
		if err != nil {
			t.Errorf("DropUserStatement(%s): %v", d, err)
			continue
		}
		if got != want {
			t.Errorf("DropUserStatement(%s) = %q, want %q", d, got, want)
		}
	}
	if _, err := DropUserStatement(DialectDB2, "bob"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("DB2 DropUser = %v, want ErrUnsupported", err)
	}
	if _, err := DropUserStatement(DialectPostgres, "bob; DROP"); err == nil {
		t.Error("injected name should error")
	}
}
