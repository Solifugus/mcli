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
	base := p.ConnectionString
	cfg, err := pgx.ParseConfig(base)
	if err != nil {
		return nil, fmt.Errorf("postgres: parse config: %w", err)
	}
	if p.ConnectionString == "" {
		if p.Host != "" {
			cfg.Host = p.Host
		}
		if p.Port != 0 {
			cfg.Port = uint16(p.Port)
		}
		if p.User != "" {
			cfg.User = p.User
		}
		cfg.Password = p.Password
		if p.Database != "" {
			cfg.Database = p.Database
		}
	}
	return cfg, nil
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
	return a.queryObjects(ctx, "table",
		`SELECT table_schema, table_name FROM information_schema.tables
		 WHERE table_type = 'BASE TABLE'
		   AND table_schema NOT IN ('pg_catalog','information_schema')
		 ORDER BY table_schema, table_name`)
}

func (a *Adapter) ListViews(ctx context.Context) ([]adapter.ObjectRef, error) {
	return a.queryObjects(ctx, "view",
		`SELECT table_schema, table_name FROM information_schema.views
		 WHERE table_schema NOT IN ('pg_catalog','information_schema')
		 ORDER BY table_schema, table_name`)
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

func (r *rowStream) Columns() []string        { return r.cols }
func (r *rowStream) Next() bool               { return r.rows.Next() }
func (r *rowStream) Values() ([]any, error)   { return r.rows.Values() }
func (r *rowStream) Err() error               { return r.rows.Err() }
func (r *rowStream) Close() error             { r.rows.Close(); return r.rows.Err() }
