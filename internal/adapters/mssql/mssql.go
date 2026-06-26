// Package mssql implements the database adapter for Microsoft SQL Server using
// the pure-Go microsoft/go-mssqldb driver. It registers itself as type
// "sqlserver", so importing this package for its side effect is enough to make
// SQL Server hosts connectable. See docs/mcli-design.md §6, §22.
package mssql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	_ "github.com/microsoft/go-mssqldb"

	"github.com/Solifugus/mcli/internal/core/adapter"
)

func init() {
	adapter.Register("sqlserver", func() adapter.Adapter { return &Adapter{} })
}

var errNotConnected = errors.New("sqlserver: not connected")

// Adapter is a connection pool to one SQL Server database. Switching the current
// database reopens the pool against the new catalog (a pooled `USE` would only
// affect one underlying connection), mirroring the Postgres adapter.
type Adapter struct {
	db  *sql.DB
	dsn *url.URL
}

// Connect opens a pool from a connection string or discrete params and verifies
// it with a ping.
func (a *Adapter) Connect(ctx context.Context, p adapter.ConnectParams) error {
	dsn, err := buildDSN(p)
	if err != nil {
		return err
	}
	db, err := sql.Open("sqlserver", dsn.String())
	if err != nil {
		return fmt.Errorf("sqlserver: open: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("sqlserver: connect: %w", err)
	}
	a.db, a.dsn = db, dsn
	return nil
}

// buildDSN assembles a sqlserver:// URL. A caller-supplied connection string is
// parsed and used as-is; otherwise discrete params build the URL, with Options
// (e.g. encrypt=disable) carried verbatim as query parameters. No encryption
// default is imposed here — it must be set explicitly in server Options so a
// production host is never silently downgraded.
func buildDSN(p adapter.ConnectParams) (*url.URL, error) {
	if p.ConnectionString != "" {
		u, err := url.Parse(p.ConnectionString)
		if err != nil {
			return nil, fmt.Errorf("sqlserver: parse connection string: %w", err)
		}
		return u, nil
	}
	u := &url.URL{Scheme: "sqlserver"}
	if p.User != "" {
		if p.Password != "" {
			u.User = url.UserPassword(p.User, p.Password)
		} else {
			u.User = url.User(p.User)
		}
	}
	host := p.Host
	if p.Port != 0 {
		host = host + ":" + strconv.Itoa(p.Port)
	}
	u.Host = host
	q := url.Values{}
	if p.Database != "" {
		q.Set("database", p.Database)
	}
	for k, v := range p.Params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	return u, nil
}

// Disconnect closes the pool.
func (a *Adapter) Disconnect() error {
	if a.db == nil {
		return nil
	}
	err := a.db.Close()
	a.db, a.dsn = nil, nil
	return err
}

// UseDatabase reopens the pool against a different catalog on the same server.
func (a *Adapter) UseDatabase(ctx context.Context, name string) error {
	if a.dsn == nil {
		return errNotConnected
	}
	next := *a.dsn
	q := next.Query()
	q.Set("database", name)
	next.RawQuery = q.Encode()

	db, err := sql.Open("sqlserver", next.String())
	if err != nil {
		return fmt.Errorf("sqlserver: use %q: %w", name, err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("sqlserver: use %q: %w", name, err)
	}
	a.db.Close()
	a.db, a.dsn = db, &next
	return nil
}

func (a *Adapter) ListDatabases(ctx context.Context) ([]string, error) {
	return a.queryStrings(ctx,
		`SELECT name FROM sys.databases
		 WHERE name NOT IN ('master','tempdb','model','msdb')
		 ORDER BY name`)
}

func (a *Adapter) ListSchemas(ctx context.Context) ([]string, error) {
	// Hide the built-in fixed-role and system schemas; show user schemas.
	return a.queryStrings(ctx,
		`SELECT name FROM sys.schemas
		 WHERE name NOT IN ('sys','INFORMATION_SCHEMA','guest')
		   AND name NOT LIKE 'db[_]%'
		 ORDER BY name`)
}

func (a *Adapter) ListTables(ctx context.Context) ([]adapter.ObjectRef, error) {
	return a.queryObjects(ctx, "table",
		`SELECT TABLE_SCHEMA, TABLE_NAME FROM INFORMATION_SCHEMA.TABLES
		 WHERE TABLE_TYPE = 'BASE TABLE'
		 ORDER BY TABLE_SCHEMA, TABLE_NAME`)
}

func (a *Adapter) ListViews(ctx context.Context) ([]adapter.ObjectRef, error) {
	return a.queryObjects(ctx, "view",
		`SELECT TABLE_SCHEMA, TABLE_NAME FROM INFORMATION_SCHEMA.VIEWS
		 ORDER BY TABLE_SCHEMA, TABLE_NAME`)
}

// DescribeObject returns columns for a table or view, marking primary-key
// columns. The name may be schema-qualified ("schema.table"); otherwise the
// first matching schema is used.
func (a *Adapter) DescribeObject(ctx context.Context, name string) (adapter.ObjectDetail, error) {
	if a.db == nil {
		return adapter.ObjectDetail{}, errNotConnected
	}
	schema, table := splitName(name)

	rows, err := a.db.QueryContext(ctx,
		`SELECT TABLE_SCHEMA, COLUMN_NAME, DATA_TYPE,
		        CASE WHEN IS_NULLABLE = 'YES' THEN 1 ELSE 0 END
		 FROM INFORMATION_SCHEMA.COLUMNS
		 WHERE TABLE_NAME = @p1 AND (@p2 = '' OR TABLE_SCHEMA = @p2)
		 ORDER BY TABLE_SCHEMA, ORDINAL_POSITION`, table, schema)
	if err != nil {
		return adapter.ObjectDetail{}, fmt.Errorf("sqlserver: describe %q: %w", name, err)
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
		return adapter.ObjectDetail{}, fmt.Errorf("sqlserver: object %q not found", name)
	}
	a.markPrimaryKeys(ctx, &detail)
	return detail, nil
}

// markPrimaryKeys is best-effort: any failure leaves columns unmarked.
func (a *Adapter) markPrimaryKeys(ctx context.Context, detail *adapter.ObjectDetail) {
	rows, err := a.db.QueryContext(ctx,
		`SELECT kcu.COLUMN_NAME
		 FROM INFORMATION_SCHEMA.TABLE_CONSTRAINTS tc
		 JOIN INFORMATION_SCHEMA.KEY_COLUMN_USAGE kcu
		   ON tc.CONSTRAINT_NAME = kcu.CONSTRAINT_NAME
		  AND tc.TABLE_SCHEMA = kcu.TABLE_SCHEMA
		 WHERE tc.CONSTRAINT_TYPE = 'PRIMARY KEY'
		   AND kcu.TABLE_NAME = @p1 AND (@p2 = '' OR kcu.TABLE_SCHEMA = @p2)`,
		detail.Ref.Name, detail.Ref.Schema)
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
	n, _ := res.RowsAffected() // not all statements report a count
	return adapter.Result{RowsAffected: n}, nil
}

// ExplainQuery is not yet supported: SQL Server exposes plans through session
// SET SHOWPLAN modes rather than an EXPLAIN statement, which does not fit the
// single-query model here. Deferred to a later phase.
func (a *Adapter) ExplainQuery(context.Context, string) (adapter.Plan, error) {
	return adapter.Plan{}, adapter.ErrUnsupported
}

func (a *Adapter) SearchColumns(ctx context.Context, name string) ([]adapter.ColumnRef, error) {
	if a.db == nil {
		return nil, errNotConnected
	}
	rows, err := a.db.QueryContext(ctx,
		`SELECT TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME, DATA_TYPE
		 FROM INFORMATION_SCHEMA.COLUMNS
		 WHERE COLUMN_NAME LIKE '%' + @p1 + '%'
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
		`SELECT TABLE_SCHEMA, TABLE_NAME FROM INFORMATION_SCHEMA.VIEWS
		 WHERE TABLE_NAME LIKE '%' + @p1 + '%'
		    OR VIEW_DEFINITION LIKE '%' + @p1 + '%'
		 ORDER BY TABLE_SCHEMA, TABLE_NAME`, text)
}

// Lineage is not yet implemented for SQL Server (design §19, a later phase).
func (a *Adapter) GetPreLineage(context.Context, string) ([]adapter.ObjectRef, error) {
	return nil, adapter.ErrUnsupported
}

func (a *Adapter) GetPostLineage(context.Context, string) ([]adapter.ObjectRef, error) {
	return nil, adapter.ErrUnsupported
}

func (a *Adapter) Dialect() adapter.Dialect { return adapter.DialectTSQL }

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

// rowStream adapts database/sql rows to adapter.RowStream, scanning each row into
// a fresh []any. Byte slices (e.g. varbinary, some text under certain collations)
// are rendered as strings so downstream string formatting stays readable.
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
