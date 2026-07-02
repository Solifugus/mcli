// Package mysql implements the database adapter for MySQL and MariaDB using the
// pure-Go go-sql-driver/mysql driver. It registers itself as type "mysql", so
// importing this package for its side effect is enough to make MySQL/MariaDB
// hosts connectable. See docs/mcli-design.md §6, §22.
package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/go-sql-driver/mysql"

	"github.com/Solifugus/mcli/internal/core/adapter"
)

func init() {
	adapter.Register("mysql", func() adapter.Adapter { return &Adapter{} })
}

var errNotConnected = errors.New("mysql: not connected")

// Adapter is a connection pool to one MySQL/MariaDB schema. In MySQL a "database"
// and a "schema" are the same namespace; switching reopens the pool against the
// new default schema, mirroring the other adapters.
type Adapter struct {
	db  *sql.DB
	cfg *mysql.Config
}

// Connect opens a pool from a connection string (DSN) or discrete params and
// verifies it with a ping.
func (a *Adapter) Connect(ctx context.Context, p adapter.ConnectParams) error {
	cfg, err := buildConfig(p)
	if err != nil {
		return err
	}
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return fmt.Errorf("mysql: open: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("mysql: connect: %w", err)
	}
	a.db, a.cfg = db, cfg
	return nil
}

// buildConfig assembles a mysql.Config from a DSN or discrete params. The driver
// formats and escapes the DSN, so values need no manual quoting. Options are
// carried through as connection params.
func buildConfig(p adapter.ConnectParams) (*mysql.Config, error) {
	if p.ConnectionString != "" {
		cfg, err := mysql.ParseDSN(p.ConnectionString)
		if err != nil {
			return nil, fmt.Errorf("mysql: parse DSN: %w", err)
		}
		return cfg, nil
	}
	cfg := mysql.NewConfig()
	cfg.Net = "tcp"
	host := p.Host
	if host == "" {
		host = "127.0.0.1"
	}
	if p.Port != 0 {
		cfg.Addr = fmt.Sprintf("%s:%d", host, p.Port)
	} else {
		cfg.Addr = fmt.Sprintf("%s:3306", host)
	}
	cfg.User = p.User
	cfg.Passwd = p.Password
	cfg.DBName = p.Database
	if len(p.Params) > 0 {
		cfg.Params = map[string]string{}
		for k, v := range p.Params {
			cfg.Params[k] = v
		}
	}
	return cfg, nil
}

// Disconnect closes the pool.
func (a *Adapter) Disconnect() error {
	if a.db == nil {
		return nil
	}
	err := a.db.Close()
	a.db, a.cfg = nil, nil
	return err
}

// UseDatabase reopens the pool against a different default schema.
func (a *Adapter) UseDatabase(ctx context.Context, name string) error {
	if a.cfg == nil {
		return errNotConnected
	}
	next := a.cfg.Clone()
	next.DBName = name
	db, err := sql.Open("mysql", next.FormatDSN())
	if err != nil {
		return fmt.Errorf("mysql: use %q: %w", name, err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("mysql: use %q: %w", name, err)
	}
	a.db.Close()
	a.db, a.cfg = db, next
	return nil
}

// systemSchemaFilter excludes the server-managed schemas from listings.
const systemSchemaFilter = `TABLE_SCHEMA NOT IN ('information_schema','mysql','performance_schema','sys')`

func (a *Adapter) ListDatabases(ctx context.Context) ([]string, error) {
	return a.queryStrings(ctx,
		`SELECT SCHEMA_NAME FROM information_schema.SCHEMATA
		 WHERE SCHEMA_NAME NOT IN ('information_schema','mysql','performance_schema','sys')
		 ORDER BY SCHEMA_NAME`)
}

// ListSchemas returns the same set as ListDatabases: in MySQL the two are
// synonyms, so this keeps .list schemas meaningful without inventing a layer.
func (a *Adapter) ListSchemas(ctx context.Context) ([]string, error) {
	return a.ListDatabases(ctx)
}

func (a *Adapter) ListTables(ctx context.Context) ([]adapter.ObjectRef, error) {
	return a.SearchObjects(ctx, []adapter.ObjectKind{adapter.KindTable}, "")
}

func (a *Adapter) ListViews(ctx context.Context) ([]adapter.ObjectRef, error) {
	return a.SearchObjects(ctx, []adapter.ObjectKind{adapter.KindView}, "")
}

// SearchObjects is the typed object finder (design §27). Each requested kind
// runs its own catalog query scoped to the current database and filtered by a
// case-insensitive name substring; results are concatenated in kind order.
func (a *Adapter) SearchObjects(ctx context.Context, kinds []adapter.ObjectKind, substr string) ([]adapter.ObjectRef, error) {
	if a.db == nil {
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
			sql = `SELECT TABLE_SCHEMA, TABLE_NAME FROM information_schema.TABLES
			       WHERE TABLE_TYPE = 'BASE TABLE' AND TABLE_SCHEMA = DATABASE()
			         AND TABLE_NAME LIKE CONCAT('%', ?, '%')
			       ORDER BY TABLE_SCHEMA, TABLE_NAME`
		case adapter.KindView:
			sql = `SELECT TABLE_SCHEMA, TABLE_NAME FROM information_schema.VIEWS
			       WHERE TABLE_SCHEMA = DATABASE()
			         AND TABLE_NAME LIKE CONCAT('%', ?, '%')
			       ORDER BY TABLE_SCHEMA, TABLE_NAME`
		case adapter.KindProcedure, adapter.KindFunction:
			rt := "PROCEDURE"
			if k == adapter.KindFunction {
				rt = "FUNCTION"
			}
			sql = `SELECT ROUTINE_SCHEMA, ROUTINE_NAME FROM information_schema.ROUTINES
			       WHERE ROUTINE_SCHEMA = DATABASE() AND ROUTINE_TYPE = '` + rt + `'
			         AND ROUTINE_NAME LIKE CONCAT('%', ?, '%')
			       ORDER BY ROUTINE_SCHEMA, ROUTINE_NAME`
		default:
			continue
		}
		refs, err := a.queryObjects(ctx, string(k), sql, substr)
		if err != nil {
			return nil, err
		}
		out = append(out, refs...)
	}
	return out, nil
}

// DescribeObject returns columns for a table or view, marking primary-key columns
// from information_schema's COLUMN_KEY. The name may be schema-qualified
// ("schema.table"); otherwise the current default schema (DATABASE()) is used.
func (a *Adapter) DescribeObject(ctx context.Context, name string) (adapter.ObjectDetail, error) {
	if a.db == nil {
		return adapter.ObjectDetail{}, errNotConnected
	}
	schema, table := splitName(name)

	rows, err := a.db.QueryContext(ctx,
		`SELECT TABLE_SCHEMA, COLUMN_NAME, COLUMN_TYPE,
		        (IS_NULLABLE = 'YES'), (COLUMN_KEY = 'PRI')
		 FROM information_schema.COLUMNS
		 WHERE TABLE_NAME = ? AND TABLE_SCHEMA = COALESCE(NULLIF(?, ''), DATABASE())
		 ORDER BY ORDINAL_POSITION`, table, schema)
	if err != nil {
		return adapter.ObjectDetail{}, fmt.Errorf("mysql: describe %q: %w", name, err)
	}
	defer rows.Close()

	detail := adapter.ObjectDetail{Ref: adapter.ObjectRef{Schema: schema, Name: table}}
	for rows.Next() {
		var sch, col, typ string
		var nullable, isPK bool
		if err := rows.Scan(&sch, &col, &typ, &nullable, &isPK); err != nil {
			return adapter.ObjectDetail{}, err
		}
		detail.Ref.Schema = sch
		key := ""
		if isPK {
			key = "PK"
		}
		detail.Columns = append(detail.Columns, adapter.Column{Name: col, DataType: typ, Nullable: nullable, Key: key})
	}
	if err := rows.Err(); err != nil {
		return adapter.ObjectDetail{}, err
	}
	if len(detail.Columns) == 0 {
		return adapter.ObjectDetail{}, fmt.Errorf("mysql: object %q not found", name)
	}
	return detail, nil
}

func (a *Adapter) RunQuery(ctx context.Context, sqlText string) (adapter.RowStream, error) {
	if a.db == nil {
		return nil, errNotConnected
	}
	rows, err := a.db.QueryContext(ctx, sqlText)
	if err != nil {
		return nil, err
	}
	cols, err := rows.Columns()
	if err != nil {
		rows.Close()
		return nil, err
	}
	return &rowStream{rows: rows, cols: cols}, nil
}

func (a *Adapter) RunStatement(ctx context.Context, sqlText string) (adapter.Result, error) {
	if a.db == nil {
		return adapter.Result{}, errNotConnected
	}
	res, err := a.db.ExecContext(ctx, sqlText)
	if err != nil {
		return adapter.Result{}, err
	}
	n, _ := res.RowsAffected()
	return adapter.Result{RowsAffected: n}, nil
}

// ExplainQuery runs MySQL's EXPLAIN and renders the plan rows as aligned text.
func (a *Adapter) ExplainQuery(ctx context.Context, sqlText string) (adapter.Plan, error) {
	if a.db == nil {
		return adapter.Plan{}, errNotConnected
	}
	rows, err := a.db.QueryContext(ctx, "EXPLAIN "+sqlText)
	if err != nil {
		return adapter.Plan{}, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return adapter.Plan{}, err
	}
	var b strings.Builder
	b.WriteString(strings.Join(cols, " | "))
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return adapter.Plan{}, err
		}
		cells := make([]string, len(vals))
		for i, v := range vals {
			cells[i] = cellString(v)
		}
		b.WriteString("\n")
		b.WriteString(strings.Join(cells, " | "))
	}
	return adapter.Plan{Text: b.String()}, rows.Err()
}

func (a *Adapter) SearchColumns(ctx context.Context, name string) ([]adapter.ColumnRef, error) {
	if a.db == nil {
		return nil, errNotConnected
	}
	rows, err := a.db.QueryContext(ctx,
		`SELECT TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME, COLUMN_TYPE
		 FROM information_schema.COLUMNS
		 WHERE COLUMN_NAME LIKE CONCAT('%', ?, '%') AND `+systemSchemaFilter+`
		 ORDER BY TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME`, name)
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
		`SELECT TABLE_SCHEMA, TABLE_NAME FROM information_schema.VIEWS
		 WHERE (TABLE_NAME LIKE CONCAT('%', ?, '%') OR VIEW_DEFINITION LIKE CONCAT('%', ?, '%'))
		   AND `+systemSchemaFilter+`
		 ORDER BY TABLE_SCHEMA, TABLE_NAME`, text, text)
}

// Lineage is not yet implemented for MySQL (design §19, a later phase).
func (a *Adapter) GetPreLineage(context.Context, string) ([]adapter.ObjectRef, error) {
	return nil, adapter.ErrUnsupported
}

func (a *Adapter) GetPostLineage(context.Context, string) ([]adapter.ObjectRef, error) {
	return nil, adapter.ErrUnsupported
}

// Source returns the definition text of a view (VIEW_DEFINITION) or a
// procedure/function (ROUTINE_DEFINITION). Tables have no stored definition —
// use DescribeObject.
func (a *Adapter) Source(ctx context.Context, name string) (adapter.ObjectSource, error) {
	if a.db == nil {
		return adapter.ObjectSource{}, errNotConnected
	}
	schema, obj := splitName(name)

	var vs, vn, vdef string
	err := a.db.QueryRowContext(ctx,
		`SELECT TABLE_SCHEMA, TABLE_NAME, VIEW_DEFINITION FROM information_schema.VIEWS
		 WHERE TABLE_NAME = ? AND TABLE_SCHEMA = COALESCE(NULLIF(?, ''), DATABASE())
		 LIMIT 1`, obj, schema).Scan(&vs, &vn, &vdef)
	if err == nil {
		return adapter.ObjectSource{
			Ref:      adapter.ObjectRef{Schema: vs, Name: vn, Type: string(adapter.KindView)},
			Language: "sql",
			Body:     fmt.Sprintf("CREATE OR REPLACE VIEW `%s`.`%s` AS\n%s", vs, vn, vdef),
		}, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return adapter.ObjectSource{}, err
	}

	var rs, rn, rtype, body string
	err = a.db.QueryRowContext(ctx,
		`SELECT ROUTINE_SCHEMA, ROUTINE_NAME, ROUTINE_TYPE, ROUTINE_DEFINITION
		 FROM information_schema.ROUTINES
		 WHERE ROUTINE_NAME = ? AND ROUTINE_SCHEMA = COALESCE(NULLIF(?, ''), DATABASE())
		 LIMIT 1`, obj, schema).Scan(&rs, &rn, &rtype, &body)
	if err == nil {
		kind := adapter.KindFunction
		if strings.EqualFold(rtype, "PROCEDURE") {
			kind = adapter.KindProcedure
		}
		return adapter.ObjectSource{
			Ref:      adapter.ObjectRef{Schema: rs, Name: rn, Type: string(kind)},
			Language: "sql",
			Body:     body,
		}, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return adapter.ObjectSource{}, err
	}
	return adapter.ObjectSource{}, fmt.Errorf("mysql: no view, procedure, or function named %q", name)
}

// SearchRoutines finds procedures/functions whose name or body matches text.
func (a *Adapter) SearchRoutines(ctx context.Context, text string) ([]adapter.ObjectRef, error) {
	var out []adapter.ObjectRef
	for _, rt := range []struct{ kind, typ string }{
		{string(adapter.KindProcedure), "PROCEDURE"},
		{string(adapter.KindFunction), "FUNCTION"},
	} {
		refs, err := a.queryObjects(ctx, rt.kind,
			`SELECT ROUTINE_SCHEMA, ROUTINE_NAME FROM information_schema.ROUTINES
			 WHERE ROUTINE_SCHEMA = DATABASE() AND ROUTINE_TYPE = '`+rt.typ+`'
			   AND (ROUTINE_NAME LIKE CONCAT('%', ?, '%') OR ROUTINE_DEFINITION LIKE CONCAT('%', ?, '%'))
			 ORDER BY ROUTINE_SCHEMA, ROUTINE_NAME`, text, text)
		if err != nil {
			return nil, err
		}
		out = append(out, refs...)
	}
	return out, nil
}

// --- scheduling: MySQL/MariaDB events ---

// ListJobs lists scheduled events (information_schema.EVENTS) in the current
// database whose name matches substr. MySQL's scheduler unit is the "event".
func (a *Adapter) ListJobs(ctx context.Context, substr string) ([]adapter.JobRef, error) {
	if a.db == nil {
		return nil, errNotConnected
	}
	rows, err := a.db.QueryContext(ctx,
		`SELECT EVENT_NAME, STATUS FROM information_schema.EVENTS
		 WHERE EVENT_SCHEMA = DATABASE() AND EVENT_NAME LIKE CONCAT('%', ?, '%')
		 ORDER BY EVENT_NAME`, substr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []adapter.JobRef
	for rows.Next() {
		var name, status string
		if err := rows.Scan(&name, &status); err != nil {
			return nil, err
		}
		out = append(out, adapter.JobRef{Name: name, Enabled: strings.EqualFold(status, "ENABLED")})
	}
	return out, rows.Err()
}

// DescribeJob returns an event's definer, schedule (recurring interval or a
// one-shot EXECUTE_AT), last execution time, comment, and its body (one step).
func (a *Adapter) DescribeJob(ctx context.Context, name string) (adapter.Job, error) {
	if a.db == nil {
		return adapter.Job{}, errNotConnected
	}
	var (
		evName, status, definer, body    string
		intervalVal, intervalField       sql.NullString
		executeAt, lastExecuted, comment sql.NullString
	)
	err := a.db.QueryRowContext(ctx,
		`SELECT EVENT_NAME, STATUS, DEFINER, EVENT_DEFINITION,
		        CAST(INTERVAL_VALUE AS CHAR), INTERVAL_FIELD,
		        CAST(EXECUTE_AT AS CHAR), CAST(LAST_EXECUTED AS CHAR), EVENT_COMMENT
		 FROM information_schema.EVENTS
		 WHERE EVENT_SCHEMA = DATABASE() AND EVENT_NAME = ? LIMIT 1`, name).
		Scan(&evName, &status, &definer, &body, &intervalVal, &intervalField,
			&executeAt, &lastExecuted, &comment)
	if errors.Is(err, sql.ErrNoRows) {
		return adapter.Job{}, fmt.Errorf("mysql: no event named %q", name)
	}
	if err != nil {
		return adapter.Job{}, err
	}
	schedule := ""
	switch {
	case intervalVal.Valid && intervalVal.String != "":
		schedule = "EVERY " + intervalVal.String + " " + intervalField.String
	case executeAt.Valid && executeAt.String != "":
		schedule = "AT " + executeAt.String
	}
	job := adapter.Job{
		Ref:     adapter.JobRef{Name: evName, Enabled: strings.EqualFold(status, "ENABLED"), Schedule: schedule},
		Owner:   definer,
		LastRun: lastExecuted.String,
		Comment: comment.String,
	}
	if strings.TrimSpace(body) != "" {
		job.Steps = []adapter.JobStep{{Name: "body", Command: body}}
	}
	return job, nil
}

// JobHistory returns no rows: MySQL keeps no per-event execution history in the
// catalog (only LAST_EXECUTED, surfaced by DescribeJob). An empty slice — not an
// error — is the honest answer, consistent with AdapterJobs' contract.
func (a *Adapter) JobHistory(ctx context.Context, name string, limit int) ([]adapter.JobRun, error) {
	if a.db == nil {
		return nil, errNotConnected
	}
	return nil, nil
}

// --- security: accounts (mysql.user) ---

// ListPrincipals lists MySQL accounts from mysql.user as "user@host" names. MySQL
// does not cleanly separate roles from users in the catalog, so every account is
// reported as a user; a kind=="role" filter returns nothing (see Capabilities).
// Reading mysql.user requires privilege; an unprivileged login gets the driver's
// permission error.
func (a *Adapter) ListPrincipals(ctx context.Context, kind, substr string) ([]adapter.PrincipalRef, error) {
	if a.db == nil {
		return nil, errNotConnected
	}
	if kind == adapter.PrincipalKindRole {
		return nil, nil
	}
	rows, err := a.db.QueryContext(ctx,
		`SELECT User, Host, account_locked FROM mysql.user
		 WHERE User LIKE CONCAT('%', ?, '%')
		 ORDER BY User, Host`, substr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []adapter.PrincipalRef
	for rows.Next() {
		var user, host, locked string
		if err := rows.Scan(&user, &host, &locked); err != nil {
			return nil, err
		}
		out = append(out, adapter.PrincipalRef{
			Name:    user + "@" + host,
			Kind:    adapter.PrincipalKindUser,
			Enabled: !strings.EqualFold(locked, "Y"),
		})
	}
	return out, rows.Err()
}

// DescribePrincipal returns an account's lock state and its grants (via SHOW
// GRANTS, which lists both privilege grants and any granted roles as text). The
// name is "user@host"; a bare name defaults the host to '%'.
func (a *Adapter) DescribePrincipal(ctx context.Context, name string) (adapter.Principal, error) {
	if a.db == nil {
		return adapter.Principal{}, errNotConnected
	}
	user, host := splitAccount(name)

	var locked string
	err := a.db.QueryRowContext(ctx,
		`SELECT account_locked FROM mysql.user WHERE User = ? AND Host = ?`, user, host).Scan(&locked)
	if errors.Is(err, sql.ErrNoRows) {
		return adapter.Principal{}, fmt.Errorf("mysql: no account %q", user+"@"+host)
	}
	if err != nil {
		return adapter.Principal{}, err
	}
	p := adapter.Principal{Ref: adapter.PrincipalRef{
		Name: user + "@" + host, Kind: adapter.PrincipalKindUser, Enabled: !strings.EqualFold(locked, "Y"),
	}}
	if strings.EqualFold(locked, "Y") {
		p.Attributes = append(p.Attributes, "account_locked")
	}

	// SHOW GRANTS takes an account literal, not a bind parameter, so the account is
	// quoted with single quotes escaped.
	grants, err := a.queryStrings(ctx,
		"SHOW GRANTS FOR '"+escapeMySQLLiteral(user)+"'@'"+escapeMySQLLiteral(host)+"'")
	if err != nil {
		return adapter.Principal{}, err
	}
	for _, g := range grants {
		p.Grants = append(p.Grants, adapter.Grant{Privilege: g})
	}
	return p, nil
}

// splitAccount splits a "user@host" account name, defaulting the host to '%' when
// no host is given.
func splitAccount(name string) (user, host string) {
	if i := strings.LastIndexByte(name, '@'); i >= 0 {
		return name[:i], name[i+1:]
	}
	return name, "%"
}

// escapeMySQLLiteral doubles single quotes for embedding in a SQL string literal.
func escapeMySQLLiteral(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// Capabilities: MySQL/MariaDB supports EXPLAIN, source retrieval, scheduled
// events (as jobs), and account/security introspection (mysql.user + SHOW
// GRANTS). It has no table-valued functions (only scalar), so CapTableFunctions
// is not advertised; its roles are not cleanly separable in the catalog, so the
// role listing is best-effort. Lineage is not yet implemented.
func (a *Adapter) Capabilities() adapter.CapabilitySet {
	return adapter.Caps(adapter.CapExplain, adapter.CapSource, adapter.CapJobs, adapter.CapSecurity, adapter.CapSecurityEdit)
}

func (a *Adapter) Dialect() adapter.Dialect { return adapter.DialectMySQL }

// --- helpers ---

func (a *Adapter) queryStrings(ctx context.Context, query string, args ...any) ([]string, error) {
	if a.db == nil {
		return nil, errNotConnected
	}
	rows, err := a.db.QueryContext(ctx, query, args...)
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

func (a *Adapter) queryObjects(ctx context.Context, typ, query string, args ...any) ([]adapter.ObjectRef, error) {
	if a.db == nil {
		return nil, errNotConnected
	}
	rows, err := a.db.QueryContext(ctx, query, args...)
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

// cellString renders a scanned value for text output, decoding the []byte that
// the driver returns for most non-integer types.
func cellString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case []byte:
		return string(t)
	default:
		return fmt.Sprintf("%v", t)
	}
}

// rowStream adapts database/sql rows to adapter.RowStream. The MySQL driver
// returns []byte for text, decimal, and (with parseTime off) temporal columns;
// those are decoded to strings so downstream formatting stays readable.
type rowStream struct {
	rows *sql.Rows
	cols []string
}

func (r *rowStream) Columns() []string { return r.cols }
func (r *rowStream) Next() bool        { return r.rows.Next() }

func (r *rowStream) Values() ([]any, error) {
	vals := make([]any, len(r.cols))
	ptrs := make([]any, len(r.cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if err := r.rows.Scan(ptrs...); err != nil {
		return nil, err
	}
	for i, v := range vals {
		if b, ok := v.([]byte); ok {
			vals[i] = string(b)
		}
	}
	return vals, nil
}

func (r *rowStream) Err() error   { return r.rows.Err() }
func (r *rowStream) Close() error { return r.rows.Close() }
