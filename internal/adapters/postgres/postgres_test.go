package postgres

import (
	"context"
	"os"
	"strings"
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
		"simple":     "simple",
		"Donkey1!":   "Donkey1!", // no space/quote/backslash -> unquoted
		"has space":  "'has space'",
		`back\slash`: `'back\\slash'`,
		`quote'd`:    `'quote\'d'`,
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

func TestLiveSearchObjects(t *testing.T) {
	a := liveAdapter(t)
	ctx := context.Background()

	// Empty kinds = all kinds; empty substr = all names. Should return at least
	// the tables ListTables returns.
	all, err := a.SearchObjects(ctx, nil, "")
	if err != nil {
		t.Fatalf("SearchObjects(all): %v", err)
	}
	tables, err := a.ListTables(ctx)
	if err != nil {
		t.Fatalf("ListTables: %v", err)
	}
	if len(tables) == 0 {
		t.Fatal("expected at least one table in gbasic")
	}
	if len(all) < len(tables) {
		t.Errorf("SearchObjects(all)=%d should be >= ListTables=%d", len(all), len(tables))
	}

	// Kind filter: tables only should match ListTables exactly.
	onlyTables, err := a.SearchObjects(ctx, []adapter.ObjectKind{adapter.KindTable}, "")
	if err != nil {
		t.Fatalf("SearchObjects(table): %v", err)
	}
	if len(onlyTables) != len(tables) {
		t.Errorf("SearchObjects(table)=%d != ListTables=%d", len(onlyTables), len(tables))
	}
	for _, r := range onlyTables {
		if r.Type != string(adapter.KindTable) {
			t.Errorf("expected type %q, got %q for %s", adapter.KindTable, r.Type, r.Name)
		}
	}

	// Substring filter narrows the set (case-insensitive). Use the first table's
	// name as a guaranteed-present needle.
	needle := tables[0].Name
	hit, err := a.SearchObjects(ctx, []adapter.ObjectKind{adapter.KindTable}, needle)
	if err != nil {
		t.Fatalf("SearchObjects(substr): %v", err)
	}
	found := false
	for _, r := range hit {
		if r.Name == needle {
			found = true
		}
	}
	if !found {
		t.Errorf("substring %q did not return the table it was taken from", needle)
	}
	if len(hit) > len(onlyTables) {
		t.Errorf("substring result %d should not exceed unfiltered %d", len(hit), len(onlyTables))
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

func TestCapabilities(t *testing.T) {
	caps := (&Adapter{}).Capabilities()
	if !caps.Has(adapter.CapExplain) {
		t.Error("Postgres should advertise CapExplain")
	}
	if !caps.Has(adapter.CapSource) {
		t.Error("Postgres should advertise CapSource")
	}
	if !caps.Has(adapter.CapTableFunctions) {
		t.Error("Postgres should advertise CapTableFunctions")
	}
	// Postgres has no native scheduler, so it must not implement AdapterJobs or
	// advertise CapJobs — the Scheduling area greys out.
	if caps.Has(adapter.CapJobs) {
		t.Error("Postgres has no native scheduler; CapJobs must not be advertised")
	}
	if _, ok := any(&Adapter{}).(adapter.AdapterJobs); ok {
		t.Error("Postgres must not implement AdapterJobs")
	}
	if !caps.Has(adapter.CapSecurity) {
		t.Error("Postgres should advertise CapSecurity")
	}
	if !caps.Has(adapter.CapSecurityEdit) {
		t.Error("Postgres should advertise CapSecurityEdit")
	}
}

// TestLivePrincipals lists roles and describes the connecting role, confirming
// that a user (canlogin) is classified as such and carries the LOGIN attribute.
func TestLivePrincipals(t *testing.T) {
	a := liveAdapter(t)
	ctx := context.Background()

	all, err := a.ListPrincipals(ctx, "", "")
	if err != nil {
		t.Fatalf("ListPrincipals(all): %v", err)
	}
	if len(all) == 0 {
		t.Fatal("expected at least one role")
	}
	users, err := a.ListPrincipals(ctx, adapter.PrincipalKindUser, "")
	if err != nil {
		t.Fatalf("ListPrincipals(user): %v", err)
	}
	for _, u := range users {
		if u.Kind != adapter.PrincipalKindUser {
			t.Errorf("user filter returned kind %q for %s", u.Kind, u.Name)
		}
	}
	if len(users) > len(all) {
		t.Errorf("filtered users %d should not exceed all %d", len(users), len(all))
	}

	// Describe the current connecting role (a login user).
	var me string
	rs, err := a.RunQuery(ctx, "SELECT current_user")
	if err != nil {
		t.Fatalf("current_user: %v", err)
	}
	if rs.Next() {
		vals, _ := rs.Values()
		me, _ = vals[0].(string)
	}
	rs.Close()
	if me == "" {
		t.Skip("could not determine current_user")
	}
	p, err := a.DescribePrincipal(ctx, me)
	if err != nil {
		t.Fatalf("DescribePrincipal(%q): %v", me, err)
	}
	if p.Ref.Name != me {
		t.Errorf("described principal name = %q, want %q", p.Ref.Name, me)
	}
	if p.Ref.Kind != adapter.PrincipalKindUser {
		t.Errorf("connecting role should be a user (canlogin), got kind %q", p.Ref.Kind)
	}
	hasLogin := false
	for _, at := range p.Attributes {
		if at == "LOGIN" {
			hasLogin = true
		}
	}
	if !hasLogin {
		t.Errorf("a login user should carry the LOGIN attribute; got %v", p.Attributes)
	}

	// A missing principal is a clear error, not a panic.
	if _, err := a.DescribePrincipal(ctx, "mcli_definitely_absent_role"); err == nil {
		t.Error("DescribePrincipal of a missing role should error")
	}
}

// TestLiveSecurityEditDCL exercises the generated GRANT/REVOKE/CREATE/DROP against
// a real Postgres: it creates a throwaway login role, grants and revokes a role to
// it, then drops it — all reversible. It verifies the generated DCL both parses
// and takes effect (the grant shows up in pg_auth_members, the revoke removes it).
func TestLiveSecurityEditDCL(t *testing.T) {
	a := liveAdapter(t)
	ctx := context.Background()

	const user = "mcli_edit_probe_u"
	const grp = "mcli_edit_probe_g"

	// Best-effort cleanup up front and at the end.
	cleanup := func() {
		_, _ = a.RunStatement(ctx, "DROP ROLE IF EXISTS "+user)
		_, _ = a.RunStatement(ctx, "DROP ROLE IF EXISTS "+grp)
	}
	cleanup()
	t.Cleanup(cleanup)

	// A group role to grant (created directly — not the code under test). If the
	// connecting login lacks CREATEROLE this is the ErrUnauthorized case, not a
	// failure of the generated DCL — skip rather than fail.
	if _, err := a.RunStatement(ctx, "CREATE ROLE "+grp); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "permission denied") {
			t.Skipf("login lacks CREATEROLE; cannot exercise DCL round-trip: %v", err)
		}
		t.Fatalf("create group role: %v", err)
	}

	// CREATE USER via the builder.
	createSQL, err := adapter.CreateUserStatement(a.Dialect(), user, "s3kret!")
	if err != nil {
		t.Fatalf("CreateUserStatement: %v", err)
	}
	if _, err := a.RunStatement(ctx, createSQL); err != nil {
		t.Fatalf("run %q: %v", createSQL, err)
	}

	// GRANT the group role to the user, then confirm membership appears.
	grantSQL, err := adapter.GrantStatement(a.Dialect(), []string{grp}, "", user, false)
	if err != nil {
		t.Fatalf("GrantStatement: %v", err)
	}
	if _, err := a.RunStatement(ctx, grantSQL); err != nil {
		t.Fatalf("run %q: %v", grantSQL, err)
	}
	p, err := a.DescribePrincipal(ctx, user)
	if err != nil {
		t.Fatalf("describe after grant: %v", err)
	}
	if !containsStr(p.MemberOf, grp) {
		t.Errorf("after grant, %s should be a member of %s; MemberOf=%v", user, grp, p.MemberOf)
	}

	// REVOKE it, then confirm membership is gone.
	revokeSQL, err := adapter.GrantStatement(a.Dialect(), []string{grp}, "", user, true)
	if err != nil {
		t.Fatalf("revoke build: %v", err)
	}
	if _, err := a.RunStatement(ctx, revokeSQL); err != nil {
		t.Fatalf("run %q: %v", revokeSQL, err)
	}
	p, err = a.DescribePrincipal(ctx, user)
	if err != nil {
		t.Fatalf("describe after revoke: %v", err)
	}
	if containsStr(p.MemberOf, grp) {
		t.Errorf("after revoke, %s should NOT be a member of %s; MemberOf=%v", user, grp, p.MemberOf)
	}

	// DROP the user via the builder.
	dropSQL, err := adapter.DropUserStatement(a.Dialect(), user)
	if err != nil {
		t.Fatalf("DropUserStatement: %v", err)
	}
	if _, err := a.RunStatement(ctx, dropSQL); err != nil {
		t.Fatalf("run %q: %v", dropSQL, err)
	}
	if _, err := a.DescribePrincipal(ctx, user); err == nil {
		t.Errorf("after drop, describing %s should error", user)
	}
}

// TestLiveLineage builds a small table -> view -> view chain and confirms the
// one-hop pre/post lineage reads the dependency both ways, then drops it.
func TestLiveLineage(t *testing.T) {
	a := liveAdapter(t)
	ctx := context.Background()

	const base = "mcli_lin_probe_t"
	const v1 = "mcli_lin_probe_v1"
	const v2 = "mcli_lin_probe_v2"

	cleanup := func() {
		_, _ = a.RunStatement(ctx, `DROP VIEW IF EXISTS `+v2)
		_, _ = a.RunStatement(ctx, `DROP VIEW IF EXISTS `+v1)
		_, _ = a.RunStatement(ctx, `DROP TABLE IF EXISTS `+base)
	}
	cleanup()
	t.Cleanup(cleanup)

	if _, err := a.RunStatement(ctx, `CREATE TABLE `+base+` (id int)`); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "permission denied") {
			t.Skipf("login lacks CREATE on the schema; cannot exercise lineage: %v", err)
		}
		t.Fatalf("create table: %v", err)
	}
	if _, err := a.RunStatement(ctx, `CREATE VIEW `+v1+` AS SELECT id FROM `+base); err != nil {
		t.Fatalf("create v1: %v", err)
	}
	if _, err := a.RunStatement(ctx, `CREATE VIEW `+v2+` AS SELECT id FROM `+v1); err != nil {
		t.Fatalf("create v2: %v", err)
	}

	// Pre-lineage of v1: it selects from the base table.
	pre, err := a.GetPreLineage(ctx, v1)
	if err != nil {
		t.Fatalf("GetPreLineage(v1): %v", err)
	}
	if !hasObject(pre, base) {
		t.Errorf("pre-lineage of %s should include %s; got %v", v1, base, pre)
	}

	// Post-lineage of the base table: v1 depends on it.
	post, err := a.GetPostLineage(ctx, base)
	if err != nil {
		t.Fatalf("GetPostLineage(base): %v", err)
	}
	if !hasObject(post, v1) {
		t.Errorf("post-lineage of %s should include %s; got %v", base, v1, post)
	}

	// Pre-lineage of v2: it selects from v1 (a view), classified as a view.
	pre2, err := a.GetPreLineage(ctx, v2)
	if err != nil {
		t.Fatalf("GetPreLineage(v2): %v", err)
	}
	for _, r := range pre2 {
		if r.Name == v1 && r.Type != "view" {
			t.Errorf("expected %s classified as view, got %q", v1, r.Type)
		}
	}
	if !hasObject(pre2, v1) {
		t.Errorf("pre-lineage of %s should include %s; got %v", v2, v1, pre2)
	}
}

func hasObject(refs []adapter.ObjectRef, name string) bool {
	for _, r := range refs {
		if r.Name == name {
			return true
		}
	}
	return false
}

func containsStr(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// TestLiveSource creates a temporary view, reads its definition back through
// Source, and finds it via SearchRoutines-adjacent SearchViews, then drops it.
func TestLiveSource(t *testing.T) {
	a := liveAdapter(t)
	ctx := context.Background()

	const view = "mcli_src_probe_v"
	if _, err := a.RunStatement(ctx, `CREATE OR REPLACE VIEW `+view+` AS SELECT 42 AS answer`); err != nil {
		t.Fatalf("create view: %v", err)
	}
	t.Cleanup(func() { _, _ = a.RunStatement(ctx, `DROP VIEW IF EXISTS `+view) })

	src, err := a.Source(ctx, view)
	if err != nil {
		t.Fatalf("Source(view): %v", err)
	}
	if src.Ref.Name != view || src.Ref.Type != string(adapter.KindView) {
		t.Errorf("Source ref = %+v, want view %q", src.Ref, view)
	}
	if !strings.Contains(strings.ToLower(src.Body), "answer") {
		t.Errorf("view source should mention its column; got %q", src.Body)
	}

	// A non-existent object is a clear error, not a panic.
	if _, err := a.Source(ctx, "mcli_definitely_absent_object"); err == nil {
		t.Error("Source of a missing object should error")
	}
}

// TestLiveSearchRoutines creates a temporary function whose body contains a
// unique needle and confirms body search finds it by that needle.
func TestLiveSearchRoutines(t *testing.T) {
	a := liveAdapter(t)
	ctx := context.Background()

	const fn = "mcli_routine_probe_fn"
	const needle = "zqxneedle"
	create := `CREATE OR REPLACE FUNCTION ` + fn + `() RETURNS int LANGUAGE sql AS $$ SELECT 1 /* ` + needle + ` */ $$`
	if _, err := a.RunStatement(ctx, create); err != nil {
		t.Fatalf("create function: %v", err)
	}
	t.Cleanup(func() { _, _ = a.RunStatement(ctx, `DROP FUNCTION IF EXISTS `+fn+`()`) })

	refs, err := a.SearchRoutines(ctx, needle)
	if err != nil {
		t.Fatalf("SearchRoutines: %v", err)
	}
	found := false
	for _, r := range refs {
		if r.Name == fn {
			found = true
		}
	}
	if !found {
		t.Errorf("body search for %q did not find function %q; got %v", needle, fn, refs)
	}
}

// TestLiveTableFunctions creates a set-returning function and confirms
// SearchTableFunctions classifies it as table-valued (and a scalar function is
// not returned).
func TestLiveTableFunctions(t *testing.T) {
	a := liveAdapter(t)
	ctx := context.Background()

	const tvf = "mcli_tvf_probe"
	const scalar = "mcli_scalar_probe"
	if _, err := a.RunStatement(ctx,
		`CREATE OR REPLACE FUNCTION `+tvf+`() RETURNS TABLE(n int) LANGUAGE sql AS $$ SELECT 1 $$`); err != nil {
		t.Fatalf("create tvf: %v", err)
	}
	t.Cleanup(func() { _, _ = a.RunStatement(ctx, `DROP FUNCTION IF EXISTS `+tvf+`()`) })
	if _, err := a.RunStatement(ctx,
		`CREATE OR REPLACE FUNCTION `+scalar+`() RETURNS int LANGUAGE sql AS $$ SELECT 1 $$`); err != nil {
		t.Fatalf("create scalar: %v", err)
	}
	t.Cleanup(func() { _, _ = a.RunStatement(ctx, `DROP FUNCTION IF EXISTS `+scalar+`()`) })

	refs, err := a.SearchTableFunctions(ctx, "mcli_")
	if err != nil {
		t.Fatalf("SearchTableFunctions: %v", err)
	}
	var sawTVF, sawScalar bool
	for _, r := range refs {
		if r.Type != string(adapter.KindTableFunction) {
			t.Errorf("ref %s has type %q, want table_function", r.Name, r.Type)
		}
		switch r.Name {
		case tvf:
			sawTVF = true
		case scalar:
			sawScalar = true
		}
	}
	if !sawTVF {
		t.Errorf("set-returning function %q not classified as table-valued; got %v", tvf, refs)
	}
	if sawScalar {
		t.Errorf("scalar function %q must not be listed as a table function", scalar)
	}
}
