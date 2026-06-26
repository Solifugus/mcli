// Package adapter defines the common interface every database driver implements
// and a registry that maps a server "type" to its adapter. Database-specific
// behavior is quarantined inside each adapter; the rest of the core (and both
// front-ends) program against this interface only. See docs/mcli-design.md §22.
package adapter

import "context"

// Dialect identifies a SQL dialect. It selects the Chroma lexer for syntax
// highlighting and informs quoting rules.
type Dialect string

const (
	DialectPostgres   Dialect = "postgres"
	DialectTSQL       Dialect = "tsql"
	DialectMySQL      Dialect = "mysql"
	DialectOracle     Dialect = "oracle"
	DialectDB2        Dialect = "db2"
	DialectGenericSQL Dialect = "sql"
)

// ConnectParams carries everything needed to open a connection. When
// ConnectionString is non-empty an adapter may use it directly; otherwise the
// discrete fields apply. The password is resolved by the core (from the
// configured password source) before it reaches an adapter — adapters never read
// secrets from disk themselves.
type ConnectParams struct {
	Host             string
	Port             int
	User             string
	Password         string
	Database         string
	ConnectionString string
	Params           map[string]string
}

// ObjectRef names a schema-qualified database object.
type ObjectRef struct {
	Schema string
	Name   string
	Type   string // "table", "view", ...
}

// Column describes one column of a table or view.
type Column struct {
	Name     string
	DataType string
	Nullable bool
	Key      string // e.g. "PK", "FK", or "" — adapter-defined, best-effort
}

// ColumnRef locates a column within a schema/table, used by column search.
type ColumnRef struct {
	Schema   string
	Table    string
	Column   string
	DataType string
}

// ObjectDetail is the result of describing an object.
type ObjectDetail struct {
	Ref     ObjectRef
	Columns []Column
}

// Result reports the outcome of a non-row-returning statement.
type Result struct {
	RowsAffected int64
	Message      string
}

// Plan is an execution plan (EXPLAIN output), rendered as text for now.
type Plan struct {
	Text string
}

// RowStream is a forward-only cursor over a query result. Callers must Close it.
// Typical use:
//
//	defer rs.Close()
//	for rs.Next() {
//	    vals, err := rs.Values()
//	    ...
//	}
//	if err := rs.Err(); err != nil { ... }
type RowStream interface {
	Columns() []string
	Next() bool
	Values() ([]any, error)
	Err() error
	Close() error
}

// Adapter is the contract every database driver implements. Methods that a given
// database cannot support should return ErrUnsupported rather than panicking.
type Adapter interface {
	Connect(ctx context.Context, p ConnectParams) error
	Disconnect() error

	ListDatabases(ctx context.Context) ([]string, error)
	UseDatabase(ctx context.Context, name string) error
	ListSchemas(ctx context.Context) ([]string, error)
	ListTables(ctx context.Context) ([]ObjectRef, error)
	ListViews(ctx context.Context) ([]ObjectRef, error)
	DescribeObject(ctx context.Context, name string) (ObjectDetail, error)

	RunQuery(ctx context.Context, sql string) (RowStream, error)
	RunStatement(ctx context.Context, sql string) (Result, error)
	ExplainQuery(ctx context.Context, sql string) (Plan, error)

	SearchColumns(ctx context.Context, name string) ([]ColumnRef, error)
	SearchViews(ctx context.Context, text string) ([]ObjectRef, error)

	GetPreLineage(ctx context.Context, name string) ([]ObjectRef, error)
	GetPostLineage(ctx context.Context, name string) ([]ObjectRef, error)

	Dialect() Dialect
}
