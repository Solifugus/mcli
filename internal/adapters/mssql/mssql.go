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
	return a.SearchObjects(ctx, []adapter.ObjectKind{adapter.KindTable}, "")
}

func (a *Adapter) ListViews(ctx context.Context) ([]adapter.ObjectRef, error) {
	return a.SearchObjects(ctx, []adapter.ObjectKind{adapter.KindView}, "")
}

// SearchObjects is the typed object finder (design §27). Each requested kind
// runs its own catalog query filtered by a case-insensitive name substring;
// results are concatenated in kind order. Routines come from
// INFORMATION_SCHEMA.ROUTINES split by ROUTINE_TYPE.
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
			sql = `SELECT TABLE_SCHEMA, TABLE_NAME FROM INFORMATION_SCHEMA.TABLES
			       WHERE TABLE_TYPE = 'BASE TABLE' AND TABLE_NAME LIKE '%' + @p1 + '%'
			       ORDER BY TABLE_SCHEMA, TABLE_NAME`
		case adapter.KindView:
			sql = `SELECT TABLE_SCHEMA, TABLE_NAME FROM INFORMATION_SCHEMA.VIEWS
			       WHERE TABLE_NAME LIKE '%' + @p1 + '%'
			       ORDER BY TABLE_SCHEMA, TABLE_NAME`
		case adapter.KindProcedure, adapter.KindFunction:
			rt := "PROCEDURE"
			if k == adapter.KindFunction {
				rt = "FUNCTION"
			}
			sql = `SELECT ROUTINE_SCHEMA, ROUTINE_NAME FROM INFORMATION_SCHEMA.ROUTINES
			       WHERE ROUTINE_TYPE = '` + rt + `' AND ROUTINE_NAME LIKE '%' + @p1 + '%'
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

// Source returns the full definition text of a view, procedure, or function from
// sys.sql_modules (which, unlike INFORMATION_SCHEMA, is not truncated at 4000
// chars). Tables have no module definition — use DescribeObject.
func (a *Adapter) Source(ctx context.Context, name string) (adapter.ObjectSource, error) {
	if a.db == nil {
		return adapter.ObjectSource{}, errNotConnected
	}
	schema, obj := splitName(name)
	var sch, nm, typ, body string
	err := a.db.QueryRowContext(ctx,
		`SELECT s.name, o.name, o.type, m.definition
		 FROM sys.sql_modules m
		 JOIN sys.objects o ON o.object_id = m.object_id
		 JOIN sys.schemas s ON s.schema_id = o.schema_id
		 WHERE o.name = @p1 AND (@p2 = '' OR s.name = @p2)
		 ORDER BY s.name`, obj, schema).Scan(&sch, &nm, &typ, &body)
	if errors.Is(err, sql.ErrNoRows) {
		return adapter.ObjectSource{}, fmt.Errorf("sqlserver: no view, procedure, or function named %q", name)
	}
	if err != nil {
		return adapter.ObjectSource{}, err
	}
	kind := adapter.KindFunction
	switch strings.TrimSpace(typ) {
	case "V":
		kind = adapter.KindView
	case "P":
		kind = adapter.KindProcedure
	}
	return adapter.ObjectSource{
		Ref:      adapter.ObjectRef{Schema: sch, Name: nm, Type: string(kind)},
		Language: "tsql",
		Body:     body,
	}, nil
}

// SearchRoutines finds procedures/functions whose name or body matches text.
func (a *Adapter) SearchRoutines(ctx context.Context, text string) ([]adapter.ObjectRef, error) {
	var out []adapter.ObjectRef
	for _, rt := range []struct{ kind, filter string }{
		{string(adapter.KindProcedure), "o.type = 'P'"},
		{string(adapter.KindFunction), "o.type IN ('FN','IF','TF','FS','FT')"},
	} {
		refs, err := a.queryObjects(ctx, rt.kind,
			`SELECT s.name, o.name FROM sys.sql_modules m
			 JOIN sys.objects o ON o.object_id = m.object_id
			 JOIN sys.schemas s ON s.schema_id = o.schema_id
			 WHERE `+rt.filter+`
			   AND (o.name LIKE '%' + @p1 + '%' OR m.definition LIKE '%' + @p1 + '%')
			 ORDER BY s.name, o.name`, text)
		if err != nil {
			return nil, err
		}
		out = append(out, refs...)
	}
	return out, nil
}

// SearchTableFunctions finds table-valued functions: inline (IF), multi-statement
// (TF), and CLR (FT). Read as SELECT * FROM schema.f(...).
func (a *Adapter) SearchTableFunctions(ctx context.Context, substr string) ([]adapter.ObjectRef, error) {
	return a.queryObjects(ctx, string(adapter.KindTableFunction),
		`SELECT s.name, o.name FROM sys.objects o
		 JOIN sys.schemas s ON s.schema_id = o.schema_id
		 WHERE o.type IN ('IF','TF','FT') AND o.name LIKE '%' + @p1 + '%'
		 ORDER BY s.name, o.name`, substr)
}

// --- scheduling: SQL Server Agent jobs (msdb) ---

// defaultJobHistoryLimit bounds JobHistory when the caller passes limit <= 0.
const defaultJobHistoryLimit = 50

// ListJobs lists SQL Server Agent jobs (msdb.dbo.sysjobs) whose name matches
// substr. Schedule is left empty at the list level; DescribeJob fills it in.
func (a *Adapter) ListJobs(ctx context.Context, substr string) ([]adapter.JobRef, error) {
	if a.db == nil {
		return nil, errNotConnected
	}
	rows, err := a.db.QueryContext(ctx,
		`SELECT name, enabled FROM msdb.dbo.sysjobs
		 WHERE name LIKE '%' + @p1 + '%'
		 ORDER BY name`, substr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []adapter.JobRef
	for rows.Next() {
		var (
			name    string
			enabled int
		)
		if err := rows.Scan(&name, &enabled); err != nil {
			return nil, err
		}
		out = append(out, adapter.JobRef{Name: name, Enabled: enabled != 0})
	}
	return out, rows.Err()
}

// DescribeJob returns a job's owner, description, its schedule name(s) and next
// run time, and its ordered steps.
func (a *Adapter) DescribeJob(ctx context.Context, name string) (adapter.Job, error) {
	if a.db == nil {
		return adapter.Job{}, errNotConnected
	}
	var (
		nm      string
		enabled int
		owner   sql.NullString
		descr   string
	)
	err := a.db.QueryRowContext(ctx,
		`SELECT j.name, j.enabled, SUSER_SNAME(j.owner_sid), ISNULL(j.description,'')
		 FROM msdb.dbo.sysjobs j WHERE j.name = @p1`, name).Scan(&nm, &enabled, &owner, &descr)
	if errors.Is(err, sql.ErrNoRows) {
		return adapter.Job{}, fmt.Errorf("sqlserver: no agent job named %q", name)
	}
	if err != nil {
		return adapter.Job{}, err
	}
	job := adapter.Job{
		Ref:     adapter.JobRef{Name: nm, Enabled: enabled != 0},
		Owner:   owner.String,
		Comment: descr,
	}

	// Schedule name(s) and the earliest upcoming run.
	schedRows, err := a.db.QueryContext(ctx,
		`SELECT sch.name, js.next_run_date, js.next_run_time
		 FROM msdb.dbo.sysjobschedules js
		 JOIN msdb.dbo.sysschedules sch ON sch.schedule_id = js.schedule_id
		 JOIN msdb.dbo.sysjobs j ON j.job_id = js.job_id
		 WHERE j.name = @p1
		 ORDER BY js.next_run_date, js.next_run_time`, name)
	if err != nil {
		return adapter.Job{}, err
	}
	defer schedRows.Close()
	var schedNames []string
	for schedRows.Next() {
		var (
			sname            string
			nextDate, nextTm int
		)
		if err := schedRows.Scan(&sname, &nextDate, &nextTm); err != nil {
			return adapter.Job{}, err
		}
		schedNames = append(schedNames, sname)
		if job.NextRun == "" {
			job.NextRun = fmtAgentDateTime(nextDate, nextTm)
		}
	}
	if err := schedRows.Err(); err != nil {
		return adapter.Job{}, err
	}
	job.Ref.Schedule = strings.Join(schedNames, ", ")

	// Ordered steps.
	stepRows, err := a.db.QueryContext(ctx,
		`SELECT s.step_name, s.command
		 FROM msdb.dbo.sysjobsteps s
		 JOIN msdb.dbo.sysjobs j ON j.job_id = s.job_id
		 WHERE j.name = @p1 ORDER BY s.step_id`, name)
	if err != nil {
		return adapter.Job{}, err
	}
	defer stepRows.Close()
	for stepRows.Next() {
		var st adapter.JobStep
		if err := stepRows.Scan(&st.Name, &st.Command); err != nil {
			return adapter.Job{}, err
		}
		job.Steps = append(job.Steps, st)
	}
	return job, stepRows.Err()
}

// JobHistory returns the job-outcome rows (step_id = 0) from sysjobhistory,
// newest first. SQL Server stores date/time/duration as packed integers, which
// fmtAgentDateTime / fmtAgentDuration render as text.
func (a *Adapter) JobHistory(ctx context.Context, name string, limit int) ([]adapter.JobRun, error) {
	if a.db == nil {
		return nil, errNotConnected
	}
	if limit <= 0 {
		limit = defaultJobHistoryLimit
	}
	rows, err := a.db.QueryContext(ctx,
		`SELECT TOP (@p2) h.run_date, h.run_time, h.run_duration, h.run_status, h.message
		 FROM msdb.dbo.sysjobhistory h
		 JOIN msdb.dbo.sysjobs j ON j.job_id = h.job_id
		 WHERE j.name = @p1 AND h.step_id = 0
		 ORDER BY h.instance_id DESC`, name, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []adapter.JobRun
	for rows.Next() {
		var (
			runDate, runTime, runDur, status int
			message                          sql.NullString
		)
		if err := rows.Scan(&runDate, &runTime, &runDur, &status, &message); err != nil {
			return nil, err
		}
		msg := message.String
		if d := fmtAgentDuration(runDur); d != "" {
			if msg != "" {
				msg += " "
			}
			msg += "(duration " + d + ")"
		}
		out = append(out, adapter.JobRun{
			Start:   fmtAgentDateTime(runDate, runTime),
			Status:  agentRunStatus(status),
			Message: msg,
		})
	}
	return out, rows.Err()
}

// agentRunStatus maps sysjobhistory.run_status to a word.
func agentRunStatus(s int) string {
	switch s {
	case 0:
		return "failed"
	case 1:
		return "succeeded"
	case 2:
		return "retry"
	case 3:
		return "canceled"
	case 4:
		return "running"
	default:
		return fmt.Sprintf("status %d", s)
	}
}

// fmtAgentDateTime renders SQL Server Agent's packed integer date (yyyymmdd) and
// time (hhmmss) as "YYYY-MM-DD HH:MM:SS". A zero date (no run / not scheduled)
// yields "".
func fmtAgentDateTime(date, tm int) string {
	if date == 0 {
		return ""
	}
	y, m, d := date/10000, (date/100)%100, date%100
	hh, mm, ss := tm/10000, (tm/100)%100, tm%100
	return fmt.Sprintf("%04d-%02d-%02d %02d:%02d:%02d", y, m, d, hh, mm, ss)
}

// fmtAgentDuration renders sysjobhistory.run_duration (packed HHMMSS) as
// "H:MM:SS". Zero yields "".
func fmtAgentDuration(dur int) string {
	if dur == 0 {
		return ""
	}
	hh, mm, ss := dur/10000, (dur/100)%100, dur%100
	return fmt.Sprintf("%d:%02d:%02d", hh, mm, ss)
}

// --- security: database principals (sys.database_principals) ---

// ListPrincipals lists database users (types S/U/G) and roles (type R). kind
// filters that split; substr filters by name.
func (a *Adapter) ListPrincipals(ctx context.Context, kind, substr string) ([]adapter.PrincipalRef, error) {
	if a.db == nil {
		return nil, errNotConnected
	}
	cond := "type IN ('S','U','G','R')"
	switch kind {
	case adapter.PrincipalKindUser:
		cond = "type IN ('S','U','G')"
	case adapter.PrincipalKindRole:
		cond = "type = 'R'"
	}
	rows, err := a.db.QueryContext(ctx,
		`SELECT name, type FROM sys.database_principals
		 WHERE `+cond+` AND (@p1 = '' OR name LIKE '%' + @p1 + '%')
		 ORDER BY name`, substr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []adapter.PrincipalRef
	for rows.Next() {
		var name, typ string
		if err := rows.Scan(&name, &typ); err != nil {
			return nil, err
		}
		out = append(out, adapter.PrincipalRef{Name: name, Kind: principalKind(typ), Enabled: true})
	}
	return out, rows.Err()
}

// DescribePrincipal returns a principal's type, default schema, role membership,
// members (for a role), and granted permissions.
func (a *Adapter) DescribePrincipal(ctx context.Context, name string) (adapter.Principal, error) {
	if a.db == nil {
		return adapter.Principal{}, errNotConnected
	}
	var nm, typ string
	var defSchema sql.NullString
	err := a.db.QueryRowContext(ctx,
		`SELECT name, type, default_schema_name FROM sys.database_principals WHERE name = @p1`, name).
		Scan(&nm, &typ, &defSchema)
	if errors.Is(err, sql.ErrNoRows) {
		return adapter.Principal{}, fmt.Errorf("sqlserver: no principal named %q", name)
	}
	if err != nil {
		return adapter.Principal{}, err
	}
	p := adapter.Principal{Ref: adapter.PrincipalRef{Name: nm, Kind: principalKind(typ), Enabled: true}}
	p.Attributes = append(p.Attributes, "type="+principalTypeLabel(typ))
	if defSchema.String != "" {
		p.Attributes = append(p.Attributes, "default_schema="+defSchema.String)
	}

	if p.MemberOf, err = a.queryStrings(ctx,
		`SELECT r.name FROM sys.database_role_members m
		 JOIN sys.database_principals r ON r.principal_id = m.role_principal_id
		 JOIN sys.database_principals u ON u.principal_id = m.member_principal_id
		 WHERE u.name = @p1 ORDER BY r.name`, name); err != nil {
		return adapter.Principal{}, err
	}
	if p.Members, err = a.queryStrings(ctx,
		`SELECT u.name FROM sys.database_role_members m
		 JOIN sys.database_principals r ON r.principal_id = m.role_principal_id
		 JOIN sys.database_principals u ON u.principal_id = m.member_principal_id
		 WHERE r.name = @p1 ORDER BY u.name`, name); err != nil {
		return adapter.Principal{}, err
	}

	grantRows, err := a.db.QueryContext(ctx,
		`SELECT p.permission_name,
		        CASE WHEN p.class = 1 THEN ISNULL(SCHEMA_NAME(o.schema_id) + '.' + o.name, '')
		             ELSE '' END
		 FROM sys.database_permissions p
		 JOIN sys.database_principals dp ON dp.principal_id = p.grantee_principal_id
		 LEFT JOIN sys.objects o ON o.object_id = p.major_id AND p.class = 1
		 WHERE dp.name = @p1 AND p.state IN ('G','W')
		 ORDER BY p.permission_name`, name)
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

// principalKind maps a sys.database_principals type code to a principal kind.
func principalKind(typ string) string {
	if strings.TrimSpace(typ) == "R" {
		return adapter.PrincipalKindRole
	}
	return adapter.PrincipalKindUser
}

// principalTypeLabel gives a human label for a principal type code.
func principalTypeLabel(typ string) string {
	switch strings.TrimSpace(typ) {
	case "S":
		return "SQL_USER"
	case "U":
		return "WINDOWS_USER"
	case "G":
		return "WINDOWS_GROUP"
	case "R":
		return "DATABASE_ROLE"
	default:
		return typ
	}
}

// Capabilities: SQL Server exposes plans through SET SHOWPLAN session modes
// rather than an EXPLAIN statement, so ExplainQuery stays unsupported. It
// supports source retrieval (sys.sql_modules), table functions, SQL Server Agent
// job introspection (msdb), and principal/security introspection
// (sys.database_principals). Other features arrive later.
func (a *Adapter) Capabilities() adapter.CapabilitySet {
	return adapter.Caps(adapter.CapSource, adapter.CapTableFunctions, adapter.CapJobs, adapter.CapSecurity, adapter.CapSecurityEdit)
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
