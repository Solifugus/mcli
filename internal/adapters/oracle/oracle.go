// Package oracle implements the database adapter for Oracle Database using the
// pure-Go sijms/go-ora driver (not the CGo godror). It registers itself as type
// "oracle", so importing this package for its side effect is enough to make
// Oracle hosts connectable. See docs/mcli-design.md §6, §22.
//
// Oracle's namespace model differs from the others: one database holds many
// schemas (each owned by a user), so here a "database" and a "schema" both map to
// an Oracle schema, and `use <name>` switches the session's CURRENT_SCHEMA rather
// than reconnecting. The pool is pinned to a single connection so that session
// state (the current schema) is stable across statements.
package oracle

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	go_ora "github.com/sijms/go-ora/v2"

	"github.com/Solifugus/mcli/internal/core/adapter"
)

func init() {
	adapter.Register("oracle", func() adapter.Adapter { return &Adapter{} })
}

var errNotConnected = errors.New("oracle: not connected")

// Adapter is a single-connection handle to an Oracle database.
type Adapter struct {
	db *sql.DB
}

// Connect opens a connection from a connection string or discrete params and
// verifies it with a ping. The pool is capped at one connection so that
// ALTER SESSION state set by UseDatabase persists across queries.
func (a *Adapter) Connect(ctx context.Context, p adapter.ConnectParams) error {
	dsn, err := buildDSN(p)
	if err != nil {
		return err
	}
	db, err := sql.Open("oracle", dsn)
	if err != nil {
		return fmt.Errorf("oracle: open: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("oracle: connect: %w", err)
	}
	// Pin ISO date/time formats so dates display consistently and, crucially,
	// so text date literals produced by the import path implicitly convert back
	// into DATE/TIMESTAMP columns. Safe on the single pinned connection.
	for _, stmt := range []string{
		`ALTER SESSION SET NLS_DATE_FORMAT = 'YYYY-MM-DD HH24:MI:SS'`,
		`ALTER SESSION SET NLS_TIMESTAMP_FORMAT = 'YYYY-MM-DD HH24:MI:SS.FF'`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			db.Close()
			return fmt.Errorf("oracle: set session format: %w", err)
		}
	}
	a.db = db
	return nil
}

// buildDSN returns a caller-supplied connection string as-is, or builds an
// oracle:// URL from discrete params. Options are passed through as URL options
// (e.g. SSL settings).
func buildDSN(p adapter.ConnectParams) (string, error) {
	if p.ConnectionString != "" {
		return p.ConnectionString, nil
	}
	if p.Host == "" {
		return "", fmt.Errorf("oracle: host required")
	}
	port := p.Port
	if port == 0 {
		port = 1521
	}
	// p.Database carries the service name (e.g. FREEPDB1) for Oracle.
	return go_ora.BuildUrl(p.Host, port, p.Database, p.User, p.Password, p.Params), nil
}

// Disconnect closes the connection.
func (a *Adapter) Disconnect() error {
	if a.db == nil {
		return nil
	}
	err := a.db.Close()
	a.db = nil
	return err
}

// UseDatabase switches the session's current schema. Oracle has no separate
// databases to reconnect to; navigating to another schema is an ALTER SESSION.
func (a *Adapter) UseDatabase(ctx context.Context, name string) error {
	if a.db == nil {
		return errNotConnected
	}
	// CURRENT_SCHEMA cannot be bound; quote the (upper-cased) identifier instead.
	stmt := `ALTER SESSION SET CURRENT_SCHEMA = ` + quoteIdent(strings.ToUpper(name))
	if _, err := a.db.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("oracle: use %q: %w", name, err)
	}
	return nil
}

// ListDatabases and ListSchemas both return the non-system schemas: in Oracle the
// navigable namespaces are schemas, and `use` targets one of them.
func (a *Adapter) ListDatabases(ctx context.Context) ([]string, error) {
	return a.queryStrings(ctx,
		`SELECT username FROM all_users WHERE oracle_maintained = 'N' ORDER BY username`)
}

func (a *Adapter) ListSchemas(ctx context.Context) ([]string, error) {
	return a.ListDatabases(ctx)
}

func (a *Adapter) ListTables(ctx context.Context) ([]adapter.ObjectRef, error) {
	return a.queryObjects(ctx, "table",
		`SELECT owner, table_name FROM all_tables
		 WHERE owner = SYS_CONTEXT('USERENV','CURRENT_SCHEMA')
		 ORDER BY owner, table_name`)
}

func (a *Adapter) ListViews(ctx context.Context) ([]adapter.ObjectRef, error) {
	return a.queryObjects(ctx, "view",
		`SELECT owner, view_name FROM all_views
		 WHERE owner = SYS_CONTEXT('USERENV','CURRENT_SCHEMA')
		 ORDER BY owner, view_name`)
}

// DescribeObject returns columns for a table or view, marking primary-key
// columns. The name may be schema-qualified ("SCHEMA.TABLE"); otherwise the
// session's current schema is used. Unquoted Oracle identifiers are upper-cased,
// so the name is folded to upper case for the catalog lookup.
func (a *Adapter) DescribeObject(ctx context.Context, name string) (adapter.ObjectDetail, error) {
	if a.db == nil {
		return adapter.ObjectDetail{}, errNotConnected
	}
	schema, table := splitName(name)
	schema, table = strings.ToUpper(schema), strings.ToUpper(table)

	rows, err := a.db.QueryContext(ctx,
		`SELECT owner, column_name, data_type, CASE WHEN nullable = 'Y' THEN 1 ELSE 0 END
		 FROM all_tab_columns
		 WHERE table_name = :1 AND owner = NVL(:2, SYS_CONTEXT('USERENV','CURRENT_SCHEMA'))
		 ORDER BY column_id`, table, schema)
	if err != nil {
		return adapter.ObjectDetail{}, fmt.Errorf("oracle: describe %q: %w", name, err)
	}
	defer rows.Close()

	detail := adapter.ObjectDetail{Ref: adapter.ObjectRef{Schema: schema, Name: table}}
	for rows.Next() {
		var owner, col, typ string
		var nullable bool
		if err := rows.Scan(&owner, &col, &typ, &nullable); err != nil {
			return adapter.ObjectDetail{}, err
		}
		detail.Ref.Schema = owner
		detail.Columns = append(detail.Columns, adapter.Column{Name: col, DataType: typ, Nullable: nullable})
	}
	if err := rows.Err(); err != nil {
		return adapter.ObjectDetail{}, err
	}
	if len(detail.Columns) == 0 {
		return adapter.ObjectDetail{}, fmt.Errorf("oracle: object %q not found", name)
	}
	a.markPrimaryKeys(ctx, &detail)
	return detail, nil
}

// markPrimaryKeys is best-effort: any failure leaves columns unmarked.
func (a *Adapter) markPrimaryKeys(ctx context.Context, detail *adapter.ObjectDetail) {
	rows, err := a.db.QueryContext(ctx,
		`SELECT cols.column_name
		 FROM all_constraints cons
		 JOIN all_cons_columns cols
		   ON cons.constraint_name = cols.constraint_name AND cons.owner = cols.owner
		 WHERE cons.constraint_type = 'P'
		   AND cols.table_name = :1
		   AND cons.owner = NVL(:2, SYS_CONTEXT('USERENV','CURRENT_SCHEMA'))`,
		strings.ToUpper(detail.Ref.Name), detail.Ref.Schema)
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

// ExplainQuery is not yet supported: Oracle's plan flow is a two-step
// EXPLAIN PLAN FOR ... / DBMS_XPLAN.DISPLAY against a PLAN_TABLE, which does not
// fit the single-query model here. Deferred to a later phase.
func (a *Adapter) ExplainQuery(context.Context, string) (adapter.Plan, error) {
	return adapter.Plan{}, adapter.ErrUnsupported
}

func (a *Adapter) SearchColumns(ctx context.Context, name string) ([]adapter.ColumnRef, error) {
	if a.db == nil {
		return nil, errNotConnected
	}
	rows, err := a.db.QueryContext(ctx,
		`SELECT owner, table_name, column_name, data_type
		 FROM all_tab_columns
		 WHERE column_name LIKE '%' || UPPER(:1) || '%'
		   AND owner IN (SELECT username FROM all_users WHERE oracle_maintained = 'N')
		 ORDER BY owner, table_name, column_name`, name)
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

// SearchViews matches by view name only; all_views.text is a LONG column that
// cannot be filtered with LIKE.
func (a *Adapter) SearchViews(ctx context.Context, text string) ([]adapter.ObjectRef, error) {
	return a.queryObjects(ctx, "view",
		`SELECT owner, view_name FROM all_views
		 WHERE view_name LIKE '%' || UPPER(:1) || '%'
		   AND owner IN (SELECT username FROM all_users WHERE oracle_maintained = 'N')
		 ORDER BY owner, view_name`, text)
}

// Lineage is not yet implemented for Oracle (design §19, a later phase).
func (a *Adapter) GetPreLineage(context.Context, string) ([]adapter.ObjectRef, error) {
	return nil, adapter.ErrUnsupported
}

func (a *Adapter) GetPostLineage(context.Context, string) ([]adapter.ObjectRef, error) {
	return nil, adapter.ErrUnsupported
}

func (a *Adapter) Dialect() adapter.Dialect { return adapter.DialectOracle }

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

// quoteIdent double-quotes an Oracle identifier, escaping embedded quotes.
func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// rowStream adapts database/sql rows to adapter.RowStream, decoding []byte values
// to strings for readable downstream formatting.
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
		switch t := v.(type) {
		case []byte:
			vals[i] = string(t)
		case time.Time:
			// Match the session NLS_DATE_FORMAT so values display cleanly and
			// round-trip through the text-literal import path.
			vals[i] = t.Format("2006-01-02 15:04:05")
		}
	}
	return vals, nil
}

func (r *rowStream) Err() error   { return r.rows.Err() }
func (r *rowStream) Close() error { return r.rows.Close() }
