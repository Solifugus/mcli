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
	return a.queryObjects(ctx, "table",
		`SELECT TABLE_SCHEMA, TABLE_NAME FROM information_schema.TABLES
		 WHERE TABLE_TYPE = 'BASE TABLE' AND TABLE_SCHEMA = DATABASE()
		 ORDER BY TABLE_SCHEMA, TABLE_NAME`)
}

func (a *Adapter) ListViews(ctx context.Context) ([]adapter.ObjectRef, error) {
	return a.queryObjects(ctx, "view",
		`SELECT TABLE_SCHEMA, TABLE_NAME FROM information_schema.VIEWS
		 WHERE TABLE_SCHEMA = DATABASE()
		 ORDER BY TABLE_SCHEMA, TABLE_NAME`)
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
