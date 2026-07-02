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
	return a.SearchObjects(ctx, []adapter.ObjectKind{adapter.KindTable}, "")
}

func (a *Adapter) ListViews(ctx context.Context) ([]adapter.ObjectRef, error) {
	return a.SearchObjects(ctx, []adapter.ObjectKind{adapter.KindView}, "")
}

// SearchObjects is the typed object finder (design §27). Each requested kind
// runs its own catalog query scoped to the current schema and filtered by a
// case-insensitive name substring (Oracle identifiers are upper-cased, so the
// needle is UPPER()'d, matching SearchColumns/SearchViews); results are
// concatenated in kind order. Procedures/functions come from all_objects.
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
			sql = `SELECT owner, table_name FROM all_tables
			       WHERE owner = SYS_CONTEXT('USERENV','CURRENT_SCHEMA')
			         AND table_name LIKE '%' || UPPER(:1) || '%'
			       ORDER BY owner, table_name`
		case adapter.KindView:
			sql = `SELECT owner, view_name FROM all_views
			       WHERE owner = SYS_CONTEXT('USERENV','CURRENT_SCHEMA')
			         AND view_name LIKE '%' || UPPER(:1) || '%'
			       ORDER BY owner, view_name`
		case adapter.KindProcedure, adapter.KindFunction:
			ot := "PROCEDURE"
			if k == adapter.KindFunction {
				ot = "FUNCTION"
			}
			sql = `SELECT owner, object_name FROM all_objects
			       WHERE owner = SYS_CONTEXT('USERENV','CURRENT_SCHEMA')
			         AND object_type = '` + ot + `'
			         AND object_name LIKE '%' || UPPER(:1) || '%'
			       ORDER BY owner, object_name`
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

// depKindCase maps all_dependencies TYPE / REFERENCED_TYPE to our ObjectKind
// labels; the %s picks which column.
const depKindCase = `CASE %s WHEN 'VIEW' THEN 'view' WHEN 'TABLE' THEN 'table'
       WHEN 'PROCEDURE' THEN 'procedure' WHEN 'FUNCTION' THEN 'function'
       ELSE LOWER(%s) END`

// GetPreLineage returns the objects the named object depends on (its inputs),
// one hop, from all_dependencies (objects visible to the current user).
func (a *Adapter) GetPreLineage(ctx context.Context, name string) ([]adapter.ObjectRef, error) {
	q := `SELECT referenced_owner, referenced_name, ` +
		fmt.Sprintf(depKindCase, "referenced_type", "referenced_type") + `
FROM all_dependencies
WHERE name = :1 AND owner = NVL(:2, SYS_CONTEXT('USERENV','CURRENT_SCHEMA'))
ORDER BY 1, 2`
	return a.queryLineage(ctx, q, name)
}

// GetPostLineage returns the objects that depend on the named object (its
// consumers), one hop.
func (a *Adapter) GetPostLineage(ctx context.Context, name string) ([]adapter.ObjectRef, error) {
	q := `SELECT owner, name, ` + fmt.Sprintf(depKindCase, "type", "type") + `
FROM all_dependencies
WHERE referenced_name = :1 AND referenced_owner = NVL(:2, SYS_CONTEXT('USERENV','CURRENT_SCHEMA'))
ORDER BY 1, 2`
	return a.queryLineage(ctx, q, name)
}

// queryLineage runs a lineage query whose binds are (:1) the upper-cased object
// name and (:2) the optional owner, returning (owner, name, kind) rows.
func (a *Adapter) queryLineage(ctx context.Context, query, name string) ([]adapter.ObjectRef, error) {
	if a.db == nil {
		return nil, errNotConnected
	}
	schema, obj := splitName(name)
	obj = strings.ToUpper(obj)
	schemaArg := sql.NullString{String: strings.ToUpper(schema), Valid: schema != ""}
	rows, err := a.db.QueryContext(ctx, query, obj, schemaArg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []adapter.ObjectRef
	for rows.Next() {
		var ref adapter.ObjectRef
		if err := rows.Scan(&ref.Schema, &ref.Name, &ref.Type); err != nil {
			return nil, err
		}
		out = append(out, ref)
	}
	return out, rows.Err()
}

// Source returns the definition text of a view, procedure, or function via
// DBMS_METADATA.GET_DDL (a CLOB, avoiding the LONG all_views.text column). Tables
// have no routine/view DDL here — use DescribeObject.
func (a *Adapter) Source(ctx context.Context, name string) (adapter.ObjectSource, error) {
	if a.db == nil {
		return adapter.ObjectSource{}, errNotConnected
	}
	schema, obj := splitName(name)
	obj = strings.ToUpper(obj)
	schemaArg := sql.NullString{String: strings.ToUpper(schema), Valid: schema != ""}

	var owner, otype string
	err := a.db.QueryRowContext(ctx,
		`SELECT owner, object_type FROM all_objects
		 WHERE object_name = :1 AND owner = NVL(:2, SYS_CONTEXT('USERENV','CURRENT_SCHEMA'))
		   AND object_type IN ('VIEW','PROCEDURE','FUNCTION') AND ROWNUM = 1`,
		obj, schemaArg).Scan(&owner, &otype)
	if errors.Is(err, sql.ErrNoRows) {
		return adapter.ObjectSource{}, fmt.Errorf("oracle: no view, procedure, or function named %q", name)
	}
	if err != nil {
		return adapter.ObjectSource{}, err
	}
	var ddl string
	if err := a.db.QueryRowContext(ctx,
		`SELECT DBMS_METADATA.GET_DDL(:1, :2, :3) FROM dual`, otype, obj, owner).Scan(&ddl); err != nil {
		return adapter.ObjectSource{}, err
	}
	kind := adapter.KindFunction
	switch otype {
	case "VIEW":
		kind = adapter.KindView
	case "PROCEDURE":
		kind = adapter.KindProcedure
	}
	return adapter.ObjectSource{
		Ref:      adapter.ObjectRef{Schema: owner, Name: obj, Type: string(kind)},
		Language: "plsql",
		Body:     strings.TrimSpace(ddl),
	}, nil
}

// SearchRoutines finds procedures/functions whose name or body matches text
// (all_source holds the body one line per row).
func (a *Adapter) SearchRoutines(ctx context.Context, text string) ([]adapter.ObjectRef, error) {
	var out []adapter.ObjectRef
	for _, rt := range []struct{ kind, typ string }{
		{string(adapter.KindProcedure), "PROCEDURE"},
		{string(adapter.KindFunction), "FUNCTION"},
	} {
		refs, err := a.queryObjects(ctx, rt.kind,
			`SELECT DISTINCT owner, name FROM all_source
			 WHERE type = '`+rt.typ+`'
			   AND owner = SYS_CONTEXT('USERENV','CURRENT_SCHEMA')
			   AND (name LIKE '%' || UPPER(:1) || '%' OR UPPER(text) LIKE '%' || UPPER(:1) || '%')
			 ORDER BY owner, name`, text)
		if err != nil {
			return nil, err
		}
		out = append(out, refs...)
	}
	return out, nil
}

// --- scheduling: DBMS_SCHEDULER jobs ---

// defaultJobHistoryLimit bounds JobHistory when the caller passes limit <= 0.
const defaultJobHistoryLimit = 50

// ListJobs lists DBMS_SCHEDULER jobs (all_scheduler_jobs) whose name matches
// substr. Oracle job names are typically uppercase, so the match is folded.
func (a *Adapter) ListJobs(ctx context.Context, substr string) ([]adapter.JobRef, error) {
	if a.db == nil {
		return nil, errNotConnected
	}
	rows, err := a.db.QueryContext(ctx,
		`SELECT job_name, enabled FROM all_scheduler_jobs
		 WHERE UPPER(job_name) LIKE '%' || UPPER(:1) || '%'
		 ORDER BY job_name`, substr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []adapter.JobRef
	for rows.Next() {
		var name, enabled string
		if err := rows.Scan(&name, &enabled); err != nil {
			return nil, err
		}
		out = append(out, adapter.JobRef{Name: name, Enabled: strings.EqualFold(enabled, "TRUE")})
	}
	return out, rows.Err()
}

// DescribeJob returns a scheduler job's owner, repeat interval (as its schedule),
// next run time, comment, and its single action (represented as one step).
func (a *Adapter) DescribeJob(ctx context.Context, name string) (adapter.Job, error) {
	if a.db == nil {
		return adapter.Job{}, errNotConnected
	}
	var jobName, enabled, owner, repeat, nextRun, action, comments string
	err := a.db.QueryRowContext(ctx,
		`SELECT job_name, enabled, owner, NVL(repeat_interval,''),
		        NVL(TO_CHAR(next_run_date),''), NVL(job_action,''), NVL(comments,'')
		 FROM all_scheduler_jobs
		 WHERE UPPER(job_name) = UPPER(:1) AND ROWNUM = 1`, name).
		Scan(&jobName, &enabled, &owner, &repeat, &nextRun, &action, &comments)
	if errors.Is(err, sql.ErrNoRows) {
		return adapter.Job{}, fmt.Errorf("oracle: no scheduler job named %q", name)
	}
	if err != nil {
		return adapter.Job{}, err
	}
	job := adapter.Job{
		Ref:     adapter.JobRef{Name: jobName, Enabled: strings.EqualFold(enabled, "TRUE"), Schedule: repeat},
		Owner:   owner,
		NextRun: nextRun,
		Comment: comments,
	}
	if strings.TrimSpace(action) != "" {
		job.Steps = []adapter.JobStep{{Name: "action", Command: action}}
	}
	return job, nil
}

// JobHistory returns run records from all_scheduler_job_run_details, newest
// first. actual_start_date is the run start; log_date is when the outcome was
// logged (≈ end).
func (a *Adapter) JobHistory(ctx context.Context, name string, limit int) ([]adapter.JobRun, error) {
	if a.db == nil {
		return nil, errNotConnected
	}
	if limit <= 0 {
		limit = defaultJobHistoryLimit
	}
	rows, err := a.db.QueryContext(ctx,
		`SELECT * FROM (
		   SELECT NVL(TO_CHAR(actual_start_date),''), NVL(TO_CHAR(log_date),''),
		          LOWER(status), NVL(additional_info,'')
		   FROM all_scheduler_job_run_details
		   WHERE UPPER(job_name) = UPPER(:1)
		   ORDER BY log_date DESC
		 ) WHERE ROWNUM <= :2`, name, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []adapter.JobRun
	for rows.Next() {
		var run adapter.JobRun
		var status sql.NullString
		if err := rows.Scan(&run.Start, &run.End, &status, &run.Message); err != nil {
			return nil, err
		}
		run.Status = status.String
		out = append(out, run)
	}
	return out, rows.Err()
}

// --- security: users and roles (dba_* catalog views) ---

// ListPrincipals lists database users (dba_users) and roles (dba_roles). These
// dba_* views require catalog privileges (e.g. SELECT_CATALOG_ROLE); a login
// without them gets the driver's permission error. kind filters the user/role
// split; substr filters by name (case-folded, as Oracle names are uppercase).
func (a *Adapter) ListPrincipals(ctx context.Context, kind, substr string) ([]adapter.PrincipalRef, error) {
	if a.db == nil {
		return nil, errNotConnected
	}
	var out []adapter.PrincipalRef
	if kind != adapter.PrincipalKindRole {
		rows, err := a.db.QueryContext(ctx,
			`SELECT username, account_status FROM dba_users
			 WHERE UPPER(username) LIKE '%' || UPPER(:1) || '%'
			 ORDER BY username`, substr)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var name, status string
			if err := rows.Scan(&name, &status); err != nil {
				rows.Close()
				return nil, err
			}
			out = append(out, adapter.PrincipalRef{Name: name, Kind: adapter.PrincipalKindUser, Enabled: status == "OPEN"})
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	if kind != adapter.PrincipalKindUser {
		refs, err := a.queryStrings(ctx,
			`SELECT role FROM dba_roles
			 WHERE UPPER(role) LIKE '%' || UPPER(:1) || '%'
			 ORDER BY role`, substr)
		if err != nil {
			return nil, err
		}
		for _, r := range refs {
			out = append(out, adapter.PrincipalRef{Name: r, Kind: adapter.PrincipalKindRole, Enabled: true})
		}
	}
	return out, nil
}

// DescribePrincipal returns a user's or role's granted roles, its members (for a
// role), and its system and object privileges.
func (a *Adapter) DescribePrincipal(ctx context.Context, name string) (adapter.Principal, error) {
	if a.db == nil {
		return adapter.Principal{}, errNotConnected
	}
	up := strings.ToUpper(name)

	var status string
	err := a.db.QueryRowContext(ctx,
		`SELECT account_status FROM dba_users WHERE username = :1`, up).Scan(&status)
	var p adapter.Principal
	switch {
	case err == nil:
		p.Ref = adapter.PrincipalRef{Name: up, Kind: adapter.PrincipalKindUser, Enabled: status == "OPEN"}
		p.Attributes = append(p.Attributes, "account_status="+status)
	case errors.Is(err, sql.ErrNoRows):
		var role string
		if err := a.db.QueryRowContext(ctx,
			`SELECT role FROM dba_roles WHERE role = :1`, up).Scan(&role); errors.Is(err, sql.ErrNoRows) {
			return adapter.Principal{}, fmt.Errorf("oracle: no user or role named %q", name)
		} else if err != nil {
			return adapter.Principal{}, err
		}
		p.Ref = adapter.PrincipalRef{Name: up, Kind: adapter.PrincipalKindRole, Enabled: true}
	default:
		return adapter.Principal{}, err
	}

	if p.MemberOf, err = a.queryStrings(ctx,
		`SELECT granted_role FROM dba_role_privs WHERE grantee = :1 ORDER BY granted_role`, up); err != nil {
		return adapter.Principal{}, err
	}
	if p.Members, err = a.queryStrings(ctx,
		`SELECT grantee FROM dba_role_privs WHERE granted_role = :1 ORDER BY grantee`, up); err != nil {
		return adapter.Principal{}, err
	}

	grantRows, err := a.db.QueryContext(ctx,
		`SELECT privilege, '' AS obj FROM dba_sys_privs WHERE grantee = :1
		 UNION ALL
		 SELECT privilege, owner || '.' || table_name FROM dba_tab_privs WHERE grantee = :1
		 ORDER BY 1, 2`, up)
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

// Capabilities: Oracle's plan flow is a two-step EXPLAIN PLAN FOR /
// DBMS_XPLAN.DISPLAY that does not fit the single-query model, so ExplainQuery
// stays unsupported. It supports source retrieval (DBMS_METADATA),
// DBMS_SCHEDULER job introspection, and user/role security introspection (dba_*
// views — needs catalog privileges). Table-function detection (pipelined /
// collection-returning functions) is deferred — reliably classifying them from
// the catalog is non-trivial — so CapTableFunctions is not advertised yet, though
// TabularQuery already emits Oracle's TABLE(...) syntax. Other features arrive in
// later phases.
func (a *Adapter) Capabilities() adapter.CapabilitySet {
	return adapter.Caps(adapter.CapLineage, adapter.CapSource, adapter.CapJobs, adapter.CapSecurity, adapter.CapSecurityEdit)
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
