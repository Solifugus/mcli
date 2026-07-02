// Package postgres implements the database adapter for PostgreSQL using the
// pure-Go pgx driver. It registers itself as type "postgres", so importing this
// package for its side effect is enough to make Postgres servers connectable.
// See docs/mcli-design.md §6, §22.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/Solifugus/mcli/internal/core/adapter"
)

func init() {
	adapter.Register("postgres", func() adapter.Adapter { return &Adapter{} })
}

var errNotConnected = errors.New("postgres: not connected")

// Adapter is a single live connection to a PostgreSQL server. Switching the
// current database reconnects, since Postgres binds a connection to one database.
type Adapter struct {
	conn *pgx.Conn
	cfg  *pgx.ConnConfig
}

// Connect opens a connection from discrete params or a connection string.
func (a *Adapter) Connect(ctx context.Context, p adapter.ConnectParams) error {
	cfg, err := buildConfig(p)
	if err != nil {
		return err
	}
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("postgres: connect: %w", err)
	}
	a.conn, a.cfg = conn, cfg
	return nil
}

func buildConfig(p adapter.ConnectParams) (*pgx.ConnConfig, error) {
	// Build the full connection string up front and parse once. pgx resolves
	// ~/.pgpass during ParseConfig against the final host/port/db/user, so the
	// fields must be present in the string — mutating the config afterward would
	// skip the passfile lookup (a password left empty would fail to fall back).
	connStr := p.ConnectionString
	if connStr == "" {
		connStr = discreteDSN(p)
	}
	cfg, err := pgx.ParseConfig(connStr)
	if err != nil {
		return nil, fmt.Errorf("postgres: parse config: %w", err)
	}
	return cfg, nil
}

// discreteDSN assembles a libpq keyword/value DSN from discrete params, quoting
// values as needed. An empty password is intentionally omitted so that pgx falls
// back to ~/.pgpass (or other auth) rather than sending an empty password.
func discreteDSN(p adapter.ConnectParams) string {
	var parts []string
	add := func(k, v string) {
		if v != "" {
			parts = append(parts, k+"="+kvQuote(v))
		}
	}
	add("host", p.Host)
	if p.Port != 0 {
		parts = append(parts, fmt.Sprintf("port=%d", p.Port))
	}
	add("user", p.User)
	add("password", p.Password)
	add("dbname", p.Database)
	for k, v := range p.Params {
		add(k, v)
	}
	return strings.Join(parts, " ")
}

// kvQuote quotes a libpq keyword/value DSN value when it contains a space,
// single quote, or backslash; other values pass through unchanged.
func kvQuote(s string) string {
	if !strings.ContainsAny(s, ` '\`) {
		return s
	}
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return "'" + s + "'"
}

// Disconnect closes the connection.
func (a *Adapter) Disconnect() error {
	if a.conn == nil {
		return nil
	}
	err := a.conn.Close(context.Background())
	a.conn, a.cfg = nil, nil
	return err
}

// UseDatabase reconnects to a different database on the same server.
func (a *Adapter) UseDatabase(ctx context.Context, name string) error {
	if a.cfg == nil {
		return errNotConnected
	}
	cfg := a.cfg.Copy()
	cfg.Database = name
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("postgres: use %q: %w", name, err)
	}
	_ = a.conn.Close(ctx)
	a.conn, a.cfg = conn, cfg
	return nil
}

func (a *Adapter) ListDatabases(ctx context.Context) ([]string, error) {
	return a.queryStrings(ctx,
		`SELECT datname FROM pg_database WHERE datistemplate = false ORDER BY datname`)
}

func (a *Adapter) ListSchemas(ctx context.Context) ([]string, error) {
	return a.queryStrings(ctx,
		`SELECT schema_name FROM information_schema.schemata
		 WHERE schema_name NOT LIKE 'pg\_%' AND schema_name <> 'information_schema'
		 ORDER BY schema_name`)
}

func (a *Adapter) ListTables(ctx context.Context) ([]adapter.ObjectRef, error) {
	return a.SearchObjects(ctx, []adapter.ObjectKind{adapter.KindTable}, "")
}

func (a *Adapter) ListViews(ctx context.Context) ([]adapter.ObjectRef, error) {
	return a.SearchObjects(ctx, []adapter.ObjectKind{adapter.KindView}, "")
}

// SearchObjects is the typed object finder. Each requested kind runs its own
// catalog query filtered by a case-insensitive name substring ('%'||”||'%'
// matches all when substr is empty), and the results are concatenated in kind
// order. Routines come from information_schema.routines split by routine_type.
func (a *Adapter) SearchObjects(ctx context.Context, kinds []adapter.ObjectKind, substr string) ([]adapter.ObjectRef, error) {
	if a.conn == nil {
		return nil, errNotConnected
	}
	if len(kinds) == 0 {
		kinds = adapter.AllObjectKinds()
	}
	var out []adapter.ObjectRef
	for _, k := range kinds {
		var sql string
		switch k {
		case adapter.KindTable:
			sql = `SELECT table_schema, table_name FROM information_schema.tables
			       WHERE table_type = 'BASE TABLE'
			         AND table_schema NOT IN ('pg_catalog','information_schema')
			         AND table_name ILIKE '%' || $1 || '%'
			       ORDER BY table_schema, table_name`
		case adapter.KindView:
			sql = `SELECT table_schema, table_name FROM information_schema.views
			       WHERE table_schema NOT IN ('pg_catalog','information_schema')
			         AND table_name ILIKE '%' || $1 || '%'
			       ORDER BY table_schema, table_name`
		case adapter.KindProcedure, adapter.KindFunction:
			rt := "PROCEDURE"
			if k == adapter.KindFunction {
				rt = "FUNCTION"
			}
			sql = `SELECT routine_schema, routine_name FROM information_schema.routines
			       WHERE routine_type = '` + rt + `'
			         AND routine_schema NOT IN ('pg_catalog','information_schema')
			         AND routine_name ILIKE '%' || $1 || '%'
			       ORDER BY routine_schema, routine_name`
		default:
			continue // unknown kind contributes nothing
		}
		refs, err := a.queryObjects(ctx, string(k), sql, substr)
		if err != nil {
			return nil, err
		}
		out = append(out, refs...)
	}
	return out, nil
}

// DescribeObject returns columns for a table or view, marking primary-key
// columns. The name may be schema-qualified ("schema.table"); otherwise the
// first matching non-system schema is used.
func (a *Adapter) DescribeObject(ctx context.Context, name string) (adapter.ObjectDetail, error) {
	if a.conn == nil {
		return adapter.ObjectDetail{}, errNotConnected
	}
	schema, table := splitName(name)

	rows, err := a.conn.Query(ctx,
		`SELECT table_schema, column_name, data_type, (is_nullable = 'YES')
		 FROM information_schema.columns
		 WHERE table_name = $1 AND ($2 = '' OR table_schema = $2)
		   AND table_schema NOT IN ('pg_catalog','information_schema')
		 ORDER BY table_schema, ordinal_position`, table, schema)
	if err != nil {
		return adapter.ObjectDetail{}, fmt.Errorf("postgres: describe %q: %w", name, err)
	}
	defer rows.Close()

	detail := adapter.ObjectDetail{Ref: adapter.ObjectRef{Schema: schema, Name: table}}
	for rows.Next() {
		var sch, col, typ string
		var nullable bool
		if err := rows.Scan(&sch, &col, &typ, &nullable); err != nil {
			return adapter.ObjectDetail{}, err
		}
		detail.Ref.Schema = sch
		detail.Columns = append(detail.Columns, adapter.Column{Name: col, DataType: typ, Nullable: nullable})
	}
	if err := rows.Err(); err != nil {
		return adapter.ObjectDetail{}, err
	}
	if len(detail.Columns) == 0 {
		return adapter.ObjectDetail{}, fmt.Errorf("postgres: object %q not found", name)
	}
	a.markPrimaryKeys(ctx, &detail)
	return detail, nil
}

// markPrimaryKeys is best-effort: any failure leaves columns unmarked.
func (a *Adapter) markPrimaryKeys(ctx context.Context, detail *adapter.ObjectDetail) {
	qualified := detail.Ref.Name
	if detail.Ref.Schema != "" {
		qualified = detail.Ref.Schema + "." + detail.Ref.Name
	}
	rows, err := a.conn.Query(ctx,
		`SELECT a.attname
		 FROM pg_index i
		 JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
		 WHERE i.indrelid = $1::regclass AND i.indisprimary`, qualified)
	if err != nil {
		return
	}
	defer rows.Close()
	pk := map[string]bool{}
	for rows.Next() {
		var name string
		if rows.Scan(&name) == nil {
			pk[name] = true
		}
	}
	for i := range detail.Columns {
		if pk[detail.Columns[i].Name] {
			detail.Columns[i].Key = "PK"
		}
	}
}

func (a *Adapter) RunQuery(ctx context.Context, sql string) (adapter.RowStream, error) {
	if a.conn == nil {
		return nil, errNotConnected
	}
	rows, err := a.conn.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	fds := rows.FieldDescriptions()
	cols := make([]string, len(fds))
	for i, fd := range fds {
		cols[i] = fd.Name
	}
	return &rowStream{rows: rows, cols: cols}, nil
}

func (a *Adapter) RunStatement(ctx context.Context, sql string) (adapter.Result, error) {
	if a.conn == nil {
		return adapter.Result{}, errNotConnected
	}
	tag, err := a.conn.Exec(ctx, sql)
	if err != nil {
		return adapter.Result{}, err
	}
	return adapter.Result{RowsAffected: tag.RowsAffected(), Message: tag.String()}, nil
}

func (a *Adapter) ExplainQuery(ctx context.Context, sql string) (adapter.Plan, error) {
	lines, err := a.queryStrings(ctx, "EXPLAIN "+sql)
	if err != nil {
		return adapter.Plan{}, err
	}
	return adapter.Plan{Text: strings.Join(lines, "\n")}, nil
}

func (a *Adapter) SearchColumns(ctx context.Context, name string) ([]adapter.ColumnRef, error) {
	if a.conn == nil {
		return nil, errNotConnected
	}
	rows, err := a.conn.Query(ctx,
		`SELECT table_schema, table_name, column_name, data_type
		 FROM information_schema.columns
		 WHERE column_name ILIKE '%' || $1 || '%'
		   AND table_schema NOT IN ('pg_catalog','information_schema')
		 ORDER BY table_schema, table_name, column_name`, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []adapter.ColumnRef
	for rows.Next() {
		var c adapter.ColumnRef
		if err := rows.Scan(&c.Schema, &c.Table, &c.Column, &c.DataType); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (a *Adapter) SearchViews(ctx context.Context, text string) ([]adapter.ObjectRef, error) {
	return a.queryObjects(ctx, "view",
		`SELECT schemaname, viewname FROM pg_views
		 WHERE schemaname NOT IN ('pg_catalog','information_schema')
		   AND (viewname ILIKE '%' || $1 || '%' OR definition ILIKE '%' || $1 || '%')
		 ORDER BY schemaname, viewname`, text)
}

// Lineage is not yet implemented for Postgres (design §19, a later phase).
func (a *Adapter) GetPreLineage(context.Context, string) ([]adapter.ObjectRef, error) {
	return nil, adapter.ErrUnsupported
}

func (a *Adapter) GetPostLineage(context.Context, string) ([]adapter.ObjectRef, error) {
	return nil, adapter.ErrUnsupported
}

// Source returns the definition text of a view (from pg_views) or a
// procedure/function (rendered by pg_get_functiondef). Tables have no stored
// definition and yield ErrUnsupported via a not-found path — use DescribeObject.
func (a *Adapter) Source(ctx context.Context, name string) (adapter.ObjectSource, error) {
	if a.conn == nil {
		return adapter.ObjectSource{}, errNotConnected
	}
	schema, obj := splitName(name)

	var vs, vn, vdef string
	err := a.conn.QueryRow(ctx,
		`SELECT schemaname, viewname, definition FROM pg_views
		 WHERE viewname = $1 AND ($2 = '' OR schemaname = $2)
		   AND schemaname NOT IN ('pg_catalog','information_schema')
		 ORDER BY schemaname LIMIT 1`, obj, schema).Scan(&vs, &vn, &vdef)
	if err == nil {
		return adapter.ObjectSource{
			Ref:      adapter.ObjectRef{Schema: vs, Name: vn, Type: string(adapter.KindView)},
			Language: "sql",
			Body:     fmt.Sprintf("CREATE OR REPLACE VIEW %s.%s AS\n%s", vs, vn, vdef),
		}, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return adapter.ObjectSource{}, err
	}

	var rs, rn, lang, body string
	var isProc bool
	err = a.conn.QueryRow(ctx,
		`SELECT n.nspname, p.proname, COALESCE(l.lanname,''), pg_get_functiondef(p.oid),
		        p.prokind = 'p'
		 FROM pg_proc p
		 JOIN pg_namespace n ON n.oid = p.pronamespace
		 LEFT JOIN pg_language l ON l.oid = p.prolang
		 WHERE p.proname = $1 AND ($2 = '' OR n.nspname = $2)
		   AND n.nspname NOT IN ('pg_catalog','information_schema')
		 ORDER BY n.nspname LIMIT 1`, obj, schema).Scan(&rs, &rn, &lang, &body, &isProc)
	if err == nil {
		kind := adapter.KindFunction
		if isProc {
			kind = adapter.KindProcedure
		}
		return adapter.ObjectSource{
			Ref:      adapter.ObjectRef{Schema: rs, Name: rn, Type: string(kind)},
			Language: lang,
			Body:     body,
		}, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return adapter.ObjectSource{}, err
	}
	return adapter.ObjectSource{}, fmt.Errorf("postgres: no view, procedure, or function named %q", name)
}

// SearchRoutines finds procedures/functions whose name or body matches text
// (information_schema.routines.routine_definition holds the body for SQL/plpgsql).
func (a *Adapter) SearchRoutines(ctx context.Context, text string) ([]adapter.ObjectRef, error) {
	var out []adapter.ObjectRef
	for _, rt := range []struct{ kind, typ string }{
		{string(adapter.KindProcedure), "PROCEDURE"},
		{string(adapter.KindFunction), "FUNCTION"},
	} {
		refs, err := a.queryObjects(ctx, rt.kind,
			`SELECT routine_schema, routine_name FROM information_schema.routines
			 WHERE routine_type = '`+rt.typ+`'
			   AND routine_schema NOT IN ('pg_catalog','information_schema')
			   AND (routine_name ILIKE '%' || $1 || '%' OR routine_definition ILIKE '%' || $1 || '%')
			 ORDER BY routine_schema, routine_name`, text)
		if err != nil {
			return nil, err
		}
		out = append(out, refs...)
	}
	return out, nil
}

// SearchTableFunctions finds set-returning functions (proretset = true), which
// can be read as SELECT * FROM f(...).
func (a *Adapter) SearchTableFunctions(ctx context.Context, substr string) ([]adapter.ObjectRef, error) {
	return a.queryObjects(ctx, string(adapter.KindTableFunction),
		`SELECT n.nspname, p.proname FROM pg_proc p
		 JOIN pg_namespace n ON n.oid = p.pronamespace
		 WHERE p.proretset = true AND p.prokind = 'f'
		   AND n.nspname NOT IN ('pg_catalog','information_schema')
		   AND p.proname ILIKE '%' || $1 || '%'
		 ORDER BY n.nspname, p.proname`, substr)
}

// --- security: roles (pg_roles / pg_auth_members) ---

// ListPrincipals lists roles from pg_roles. In Postgres a "user" is a role that
// can log in (rolcanlogin) and a "role" is one that cannot; the built-in pg_*
// roles are excluded. kind filters that split; substr filters by name.
func (a *Adapter) ListPrincipals(ctx context.Context, kind, substr string) ([]adapter.PrincipalRef, error) {
	if a.conn == nil {
		return nil, errNotConnected
	}
	cond := ""
	switch kind {
	case adapter.PrincipalKindUser:
		cond = " AND rolcanlogin"
	case adapter.PrincipalKindRole:
		cond = " AND NOT rolcanlogin"
	}
	rows, err := a.conn.Query(ctx,
		`SELECT rolname, rolcanlogin FROM pg_roles
		 WHERE rolname NOT LIKE 'pg\_%' AND ($1 = '' OR rolname ILIKE '%' || $1 || '%')`+cond+`
		 ORDER BY rolname`, substr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []adapter.PrincipalRef
	for rows.Next() {
		var name string
		var canLogin bool
		if err := rows.Scan(&name, &canLogin); err != nil {
			return nil, err
		}
		out = append(out, adapter.PrincipalRef{Name: name, Kind: roleKind(canLogin), Enabled: canLogin})
	}
	return out, rows.Err()
}

// DescribePrincipal returns a role's attributes, the roles it is a member of, the
// roles that are members of it, and its explicit table privileges.
func (a *Adapter) DescribePrincipal(ctx context.Context, name string) (adapter.Principal, error) {
	if a.conn == nil {
		return adapter.Principal{}, errNotConnected
	}
	var rolname string
	var super, createdb, createrole, canLogin, replication, bypassrls bool
	err := a.conn.QueryRow(ctx,
		`SELECT rolname, rolsuper, rolcreatedb, rolcreaterole, rolcanlogin, rolreplication, rolbypassrls
		 FROM pg_roles WHERE rolname = $1`, name).
		Scan(&rolname, &super, &createdb, &createrole, &canLogin, &replication, &bypassrls)
	if errors.Is(err, pgx.ErrNoRows) {
		return adapter.Principal{}, fmt.Errorf("postgres: no role named %q", name)
	}
	if err != nil {
		return adapter.Principal{}, err
	}
	p := adapter.Principal{Ref: adapter.PrincipalRef{Name: rolname, Kind: roleKind(canLogin), Enabled: canLogin}}
	for _, at := range []struct {
		on    bool
		label string
	}{
		{super, "SUPERUSER"}, {createdb, "CREATEDB"}, {createrole, "CREATEROLE"},
		{canLogin, "LOGIN"}, {replication, "REPLICATION"}, {bypassrls, "BYPASSRLS"},
	} {
		if at.on {
			p.Attributes = append(p.Attributes, at.label)
		}
	}

	if p.MemberOf, err = a.queryStrings(ctx,
		`SELECT g.rolname FROM pg_auth_members m
		 JOIN pg_roles r ON r.oid = m.member
		 JOIN pg_roles g ON g.oid = m.roleid
		 WHERE r.rolname = $1 ORDER BY g.rolname`, name); err != nil {
		return adapter.Principal{}, err
	}
	if p.Members, err = a.queryStrings(ctx,
		`SELECT r.rolname FROM pg_auth_members m
		 JOIN pg_roles r ON r.oid = m.member
		 JOIN pg_roles g ON g.oid = m.roleid
		 WHERE g.rolname = $1 ORDER BY r.rolname`, name); err != nil {
		return adapter.Principal{}, err
	}

	grantRows, err := a.conn.Query(ctx,
		`SELECT privilege_type, table_schema || '.' || table_name
		 FROM information_schema.role_table_grants
		 WHERE grantee = $1
		 ORDER BY table_schema, table_name, privilege_type`, name)
	if err != nil {
		return adapter.Principal{}, err
	}
	defer grantRows.Close()
	for grantRows.Next() {
		var g adapter.Grant
		if err := grantRows.Scan(&g.Privilege, &g.On); err != nil {
			return adapter.Principal{}, err
		}
		p.Grants = append(p.Grants, g)
	}
	return p, grantRows.Err()
}

// roleKind maps a Postgres role's login capability to a principal kind.
func roleKind(canLogin bool) string {
	if canLogin {
		return adapter.PrincipalKindUser
	}
	return adapter.PrincipalKindRole
}

// Capabilities: Postgres supports EXPLAIN, source retrieval, table functions, and
// role/security introspection (pg_roles). Lineage and job scheduling are not yet
// implemented (Postgres has no native scheduler).
func (a *Adapter) Capabilities() adapter.CapabilitySet {
	return adapter.Caps(adapter.CapExplain, adapter.CapSource, adapter.CapTableFunctions, adapter.CapSecurity, adapter.CapSecurityEdit)
}

func (a *Adapter) Dialect() adapter.Dialect { return adapter.DialectPostgres }

// --- helpers ---

func (a *Adapter) queryStrings(ctx context.Context, sql string, args ...any) ([]string, error) {
	if a.conn == nil {
		return nil, errNotConnected
	}
	rows, err := a.conn.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (a *Adapter) queryObjects(ctx context.Context, typ, sql string, args ...any) ([]adapter.ObjectRef, error) {
	if a.conn == nil {
		return nil, errNotConnected
	}
	rows, err := a.conn.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []adapter.ObjectRef
	for rows.Next() {
		ref := adapter.ObjectRef{Type: typ}
		if err := rows.Scan(&ref.Schema, &ref.Name); err != nil {
			return nil, err
		}
		out = append(out, ref)
	}
	return out, rows.Err()
}

func splitName(name string) (schema, object string) {
	if i := strings.IndexByte(name, '.'); i >= 0 {
		return name[:i], name[i+1:]
	}
	return "", name
}

// rowStream adapts pgx.Rows to adapter.RowStream.
type rowStream struct {
	rows pgx.Rows
	cols []string
}

func (r *rowStream) Columns() []string      { return r.cols }
func (r *rowStream) Next() bool             { return r.rows.Next() }
func (r *rowStream) Values() ([]any, error) { return r.rows.Values() }
func (r *rowStream) Err() error             { return r.rows.Err() }
func (r *rowStream) Close() error           { r.rows.Close(); return r.rows.Err() }
