//go:build db2

// Package db2 implements the database adapter for IBM Db2 LUW using the pure-Go
// obaydullahmhs/go-db2 driver (DRDA protocol, no CGo / no IBM CLI driver). It is
// gated behind the "db2" build tag so the default cross-platform build never
// pulls it in; build with `go build -tags db2`. It registers itself as type
// "db2". See docs/mcli-design.md §6, §22.
//
// Db2's namespace model resembles Oracle's: one database, many schemas, so a
// "database" and a "schema" both map to a Db2 schema and `use <name>` issues
// SET CURRENT SCHEMA rather than reconnecting. The pool is pinned to a single
// connection so that the current-schema register stays stable across statements.
package db2

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "github.com/obaydullahmhs/go-db2"

	"github.com/Solifugus/mcli/internal/core/adapter"
)

func init() {
	adapter.Register("db2", func() adapter.Adapter { return &Adapter{} })
}

var errNotConnected = errors.New("db2: not connected")

// Adapter is a single-connection handle to a Db2 database.
type Adapter struct {
	db *sql.DB
}

// Connect opens a connection from a connection string or discrete params and
// verifies it with a ping. The pool is capped at one connection so SET CURRENT
// SCHEMA set by UseDatabase persists across queries.
func (a *Adapter) Connect(ctx context.Context, p adapter.ConnectParams) error {
	dsn, err := buildDSN(p)
	if err != nil {
		return err
	}
	db, err := sql.Open("db2", dsn)
	if err != nil {
		return fmt.Errorf("db2: open: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("db2: connect: %w", err)
	}
	a.db = db
	return nil
}

// buildDSN returns a caller-supplied connection string as-is, or builds the
// go-db2 key=value DSN from discrete params. Options are appended verbatim.
func buildDSN(p adapter.ConnectParams) (string, error) {
	if p.ConnectionString != "" {
		return p.ConnectionString, nil
	}
	if p.Host == "" || p.Database == "" {
		return "", fmt.Errorf("db2: host and database required")
	}
	port := p.Port
	if port == 0 {
		port = 50000
	}
	parts := []string{
		"hostname=" + p.Host,
		fmt.Sprintf("port=%d", port),
		"database=" + p.Database,
		"uid=" + p.User,
		"pwd=" + p.Password,
	}
	for k, v := range p.Params {
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, ";"), nil
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

// UseDatabase switches the current schema. Db2 has no separate databases to
// reconnect to; navigating to another schema is a SET CURRENT SCHEMA.
func (a *Adapter) UseDatabase(ctx context.Context, name string) error {
	if a.db == nil {
		return errNotConnected
	}
	// The schema register takes a string constant; fold to upper case (unquoted
	// Db2 names are upper-cased) and single-quote-escape.
	lit := "'" + strings.ReplaceAll(strings.ToUpper(name), "'", "''") + "'"
	if _, err := a.db.ExecContext(ctx, "SET CURRENT SCHEMA "+lit); err != nil {
		return fmt.Errorf("db2: use %q: %w", name, err)
	}
	return nil
}

// systemSchemaFilter excludes Db2's catalog and tooling schemas.
const systemSchemaFilter = `schemaname NOT LIKE 'SYS%' AND schemaname NOT IN ('NULLID','SQLJ','SYSTOOLS','SYSPUBLIC','SYSIBMADM','SYSCAT','SYSSTAT')`

func (a *Adapter) ListDatabases(ctx context.Context) ([]string, error) {
	return a.queryStrings(ctx,
		`SELECT TRIM(schemaname) FROM syscat.schemata WHERE `+systemSchemaFilter+` ORDER BY schemaname`)
}

// ListSchemas returns the same set as ListDatabases: in Db2 the navigable
// namespaces are schemas, and `use` targets one of them.
func (a *Adapter) ListSchemas(ctx context.Context) ([]string, error) {
	return a.ListDatabases(ctx)
}

func (a *Adapter) ListTables(ctx context.Context) ([]adapter.ObjectRef, error) {
	return a.queryObjects(ctx, "table",
		`SELECT TRIM(tabschema), TRIM(tabname) FROM syscat.tables
		 WHERE type = 'T' AND tabschema = CURRENT SCHEMA
		 ORDER BY tabschema, tabname`)
}

func (a *Adapter) ListViews(ctx context.Context) ([]adapter.ObjectRef, error) {
	return a.queryObjects(ctx, "view",
		`SELECT TRIM(tabschema), TRIM(tabname) FROM syscat.tables
		 WHERE type = 'V' AND tabschema = CURRENT SCHEMA
		 ORDER BY tabschema, tabname`)
}

// DescribeObject returns columns for a table or view, marking primary-key
// columns. The name may be schema-qualified ("SCHEMA.TABLE"); otherwise the
// current schema is resolved and used. Unquoted Db2 identifiers are upper-cased,
// so the name is folded to upper case for the catalog lookup.
func (a *Adapter) DescribeObject(ctx context.Context, name string) (adapter.ObjectDetail, error) {
	if a.db == nil {
		return adapter.ObjectDetail{}, errNotConnected
	}
	schema, table := splitName(name)
	table = strings.ToUpper(table)
	schema, err := a.resolveSchema(ctx, schema)
	if err != nil {
		return adapter.ObjectDetail{}, err
	}

	rows, err := a.db.QueryContext(ctx,
		`SELECT TRIM(colname), TRIM(typename), CASE WHEN nulls = 'Y' THEN 1 ELSE 0 END
		 FROM syscat.columns
		 WHERE tabname = ? AND tabschema = ?
		 ORDER BY colno`, table, schema)
	if err != nil {
		return adapter.ObjectDetail{}, fmt.Errorf("db2: describe %q: %w", name, err)
	}
	defer rows.Close()

	detail := adapter.ObjectDetail{Ref: adapter.ObjectRef{Schema: schema, Name: table}}
	for rows.Next() {
		var col, typ string
		var nullable bool
		if err := rows.Scan(&col, &typ, &nullable); err != nil {
			return adapter.ObjectDetail{}, err
		}
		detail.Columns = append(detail.Columns, adapter.Column{Name: col, DataType: typ, Nullable: nullable})
	}
	if err := rows.Err(); err != nil {
		return adapter.ObjectDetail{}, err
	}
	if len(detail.Columns) == 0 {
		return adapter.ObjectDetail{}, fmt.Errorf("db2: object %q not found", name)
	}
	a.markPrimaryKeys(ctx, &detail, table, schema)
	return detail, nil
}

// markPrimaryKeys is best-effort: any failure leaves columns unmarked.
func (a *Adapter) markPrimaryKeys(ctx context.Context, detail *adapter.ObjectDetail, table, schema string) {
	rows, err := a.db.QueryContext(ctx,
		`SELECT TRIM(kc.colname)
		 FROM syscat.keycoluse kc
		 JOIN syscat.tabconst tc ON kc.constname = tc.constname AND kc.tabschema = tc.tabschema
		 WHERE tc.type = 'P' AND kc.tabname = ? AND kc.tabschema = ?`, table, schema)
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

// resolveSchema returns the upper-cased schema, or the connection's current
// schema when none was given. Resolving it in Go avoids an untyped parameter
// marker (which Db2 rejects) in a COALESCE.
func (a *Adapter) resolveSchema(ctx context.Context, schema string) (string, error) {
	if schema != "" {
		return strings.ToUpper(schema), nil
	}
	var cur string
	if err := a.db.QueryRowContext(ctx, "SELECT TRIM(CURRENT SCHEMA) FROM SYSIBM.SYSDUMMY1").Scan(&cur); err != nil {
		return "", fmt.Errorf("db2: resolve current schema: %w", err)
	}
	return cur, nil
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

// ExplainQuery is not yet supported: Db2's plan flow writes to explain tables and
// is read back separately, which does not fit the single-query model here.
func (a *Adapter) ExplainQuery(context.Context, string) (adapter.Plan, error) {
	return adapter.Plan{}, adapter.ErrUnsupported
}

func (a *Adapter) SearchColumns(ctx context.Context, name string) ([]adapter.ColumnRef, error) {
	if a.db == nil {
		return nil, errNotConnected
	}
	rows, err := a.db.QueryContext(ctx,
		`SELECT TRIM(tabschema), TRIM(tabname), TRIM(colname), TRIM(typename)
		 FROM syscat.columns
		 WHERE colname LIKE '%' || UCASE(?) || '%' AND tabschema NOT LIKE 'SYS%'
		 ORDER BY tabschema, tabname, colname`, name)
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
		`SELECT TRIM(viewschema), TRIM(viewname) FROM syscat.views
		 WHERE viewname LIKE '%' || UCASE(?) || '%' AND viewschema NOT LIKE 'SYS%'
		 ORDER BY viewschema, viewname`, text)
}

// Lineage is not yet implemented for Db2 (design §19, a later phase).
func (a *Adapter) GetPreLineage(context.Context, string) ([]adapter.ObjectRef, error) {
	return nil, adapter.ErrUnsupported
}

func (a *Adapter) GetPostLineage(context.Context, string) ([]adapter.ObjectRef, error) {
	return nil, adapter.ErrUnsupported
}

func (a *Adapter) Dialect() adapter.Dialect { return adapter.DialectDB2 }

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

// rowStream adapts database/sql rows to adapter.RowStream. Byte slices are
// decoded to strings; temporal values render date-only when the clock is zero so
// DATE columns round-trip through the text-literal import path.
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
			if t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 && t.Nanosecond() == 0 {
				vals[i] = t.Format("2006-01-02")
			} else {
				vals[i] = t.Format("2006-01-02 15:04:05")
			}
		}
	}
	return vals, nil
}

func (r *rowStream) Err() error   { return r.rows.Err() }
func (r *rowStream) Close() error { return r.rows.Close() }
