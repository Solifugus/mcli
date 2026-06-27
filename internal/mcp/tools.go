package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Solifugus/mcli/internal/core"
	"github.com/Solifugus/mcli/internal/core/safety"
)

// tool is one MCP tool: a JSON-schema-described wrapper over a core function.
type tool struct {
	Name        string
	Description string
	Schema      map[string]any // JSON Schema for the arguments object
	Handle      func(ctx context.Context, c *core.Core, args json.RawMessage) (string, error)
}

// tools is the registry, built once. Order is stable for tools/list.
var tools = buildTools()

func toolByName(name string) (tool, bool) {
	for _, t := range tools {
		if t.Name == name {
			return t, true
		}
	}
	return tool{}, false
}

// toolList renders the registry for a tools/list response.
func toolList() []map[string]any {
	out := make([]map[string]any, len(tools))
	for i, t := range tools {
		out[i] = map[string]any{
			"name":        t.Name,
			"description": t.Description,
			"inputSchema": t.Schema,
		}
	}
	return out
}

// callTool runs one tool and wraps the outcome as an MCP tool result. Tool
// errors become an isError result (visible to the model) rather than a
// transport error.
func (s *Server) callTool(ctx context.Context, name string, args json.RawMessage) map[string]any {
	t, ok := toolByName(name)
	if !ok {
		return errorResult("unknown tool: " + name)
	}
	text, err := t.Handle(ctx, s.core, args)
	if err != nil {
		return errorResult(err.Error())
	}
	return textResult(text)
}

func textResult(text string) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
	}
}

func errorResult(text string) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": true,
	}
}

// --- schema helpers ---

func objectSchema(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func strProp(desc string) map[string]any  { return map[string]any{"type": "string", "description": desc} }
func boolProp(desc string) map[string]any { return map[string]any{"type": "boolean", "description": desc} }

// jsonString marshals v to indented JSON for a text result.
func jsonString(v any) (string, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// nonNil coerces a nil slice to an empty one so list/search tools always render
// as a JSON array ([]) rather than null — a more consistent shape for callers.
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// decode unmarshals tool arguments, tolerating a null/empty arguments field.
func decode(args json.RawMessage, dst any) error {
	if len(args) == 0 || string(args) == "null" {
		return nil
	}
	return json.Unmarshal(args, dst)
}

// buildTools assembles the registry. Each handler is a thin wrapper over a core
// method; safety guards live in the core, so they apply identically here.
func buildTools() []tool {
	return []tool{
		// --- workspaces ---
		{
			Name:        "list_workspaces",
			Description: "List the names of all workspaces.",
			Schema:      objectSchema(nil),
			Handle: func(ctx context.Context, c *core.Core, _ json.RawMessage) (string, error) {
				names, err := c.ListWorkspaces()
				if err != nil {
					return "", err
				}
				return jsonString(nonNil(names))
			},
		},
		{
			Name:        "enter_workspace",
			Description: "Switch to the named workspace, restoring its connection context and files.",
			Schema:      objectSchema(map[string]any{"name": strProp("workspace name")}, "name"),
			Handle: func(ctx context.Context, c *core.Core, args json.RawMessage) (string, error) {
				var a struct{ Name string }
				if err := decode(args, &a); err != nil {
					return "", err
				}
				if a.Name == "" {
					return "", errors.New("name is required")
				}
				if err := c.Enter(a.Name); err != nil {
					return "", err
				}
				return "entered workspace " + a.Name, nil
			},
		},
		{
			Name:        "get_workspace_status",
			Description: "Report the current workspace, connection, environment, dialect, and read-only state.",
			Schema:      objectSchema(nil),
			Handle: func(ctx context.Context, c *core.Core, _ json.RawMessage) (string, error) {
				return jsonString(map[string]any{
					"workspace":   c.Current().Name,
					"connected":   c.Connected(),
					"server":      c.ConnServer(),
					"environment": c.Environment(),
					"dialect":     string(c.Dialect()),
					"read_only":   c.ReadOnly(),
				})
			},
		},

		// --- servers ---
		{
			Name:        "list_servers",
			Description: "List configured servers (type, environment, host, port, user, password source). Never returns secrets.",
			Schema:      objectSchema(nil),
			Handle: func(ctx context.Context, c *core.Core, _ json.RawMessage) (string, error) {
				type serverView struct {
					Name            string `json:"name"`
					Type            string `json:"type"`
					Environment     string `json:"environment,omitempty"`
					Host            string `json:"host,omitempty"`
					Port            int    `json:"port,omitempty"`
					User            string `json:"user,omitempty"`
					DefaultDatabase string `json:"default_database,omitempty"`
					PasswordSource  string `json:"password_source,omitempty"`
				}
				names := c.ServerNames()
				views := make([]serverView, 0, len(names))
				for _, name := range names {
					srv, ok := c.Server(name)
					if !ok {
						continue
					}
					views = append(views, serverView{
						Name: name, Type: srv.Type, Environment: srv.Environment,
						Host: srv.Host, Port: srv.Port, User: srv.User,
						DefaultDatabase: srv.DefaultDatabase, PasswordSource: srv.PasswordSource,
					})
				}
				return jsonString(nonNil(views))
			},
		},
		{
			Name: "connect_server",
			Description: "Connect to a configured server. Pass password only when the server's " +
				"password source requires interactive entry (prompt, or a keyring miss).",
			Schema: objectSchema(map[string]any{
				"name":     strProp("server name"),
				"password": strProp("password, only if interactive entry is required"),
			}, "name"),
			Handle: func(ctx context.Context, c *core.Core, args json.RawMessage) (string, error) {
				var a struct{ Name, Password string }
				if err := decode(args, &a); err != nil {
					return "", err
				}
				if a.Name == "" {
					return "", errors.New("name is required")
				}
				var err error
				if a.Password != "" {
					err = c.ConnectWithPassword(ctx, a.Name, a.Password)
				} else {
					err = c.Connect(ctx, a.Name)
					if errors.Is(err, core.ErrPasswordRequired) {
						return "", errors.New("this server needs a password: re-call connect_server with the password argument")
					}
				}
				if err != nil {
					return "", err
				}
				return "connected to " + a.Name + " (" + c.Environment() + ")", nil
			},
		},

		// --- schema browsing ---
		{
			Name:        "list_databases",
			Description: "List databases on the connected server.",
			Schema:      objectSchema(nil),
			Handle: func(ctx context.Context, c *core.Core, _ json.RawMessage) (string, error) {
				dbs, err := c.ListDatabases(ctx)
				if err != nil {
					return "", err
				}
				return jsonString(nonNil(dbs))
			},
		},
		{
			Name:        "use_database",
			Description: "Switch the current database on the connected server.",
			Schema:      objectSchema(map[string]any{"name": strProp("database name")}, "name"),
			Handle: func(ctx context.Context, c *core.Core, args json.RawMessage) (string, error) {
				var a struct{ Name string }
				if err := decode(args, &a); err != nil {
					return "", err
				}
				if a.Name == "" {
					return "", errors.New("name is required")
				}
				if err := c.Use(ctx, a.Name); err != nil {
					return "", err
				}
				return "using database " + a.Name, nil
			},
		},
		{
			Name:        "list_tables",
			Description: "List tables in the current database.",
			Schema:      objectSchema(nil),
			Handle: func(ctx context.Context, c *core.Core, _ json.RawMessage) (string, error) {
				refs, err := c.ListTables(ctx)
				if err != nil {
					return "", err
				}
				return jsonString(nonNil(refs))
			},
		},
		{
			Name:        "list_views",
			Description: "List views in the current database.",
			Schema:      objectSchema(nil),
			Handle: func(ctx context.Context, c *core.Core, _ json.RawMessage) (string, error) {
				refs, err := c.ListViews(ctx)
				if err != nil {
					return "", err
				}
				return jsonString(nonNil(refs))
			},
		},
		{
			Name:        "describe_table",
			Description: "Describe a table or view: its columns, data types, nullability, and keys.",
			Schema:      objectSchema(map[string]any{"name": strProp("table or view name, optionally schema-qualified")}, "name"),
			Handle: func(ctx context.Context, c *core.Core, args json.RawMessage) (string, error) {
				var a struct{ Name string }
				if err := decode(args, &a); err != nil {
					return "", err
				}
				if a.Name == "" {
					return "", errors.New("name is required")
				}
				detail, err := c.Describe(ctx, a.Name)
				if err != nil {
					return "", err
				}
				return jsonString(detail)
			},
		},
		{
			Name:        "search_columns",
			Description: "Find columns whose name matches the given text across the current database.",
			Schema:      objectSchema(map[string]any{"text": strProp("text to match against column names")}, "text"),
			Handle: func(ctx context.Context, c *core.Core, args json.RawMessage) (string, error) {
				var a struct{ Text string }
				if err := decode(args, &a); err != nil {
					return "", err
				}
				if a.Text == "" {
					return "", errors.New("text is required")
				}
				cols, err := c.SearchColumns(ctx, a.Text)
				if err != nil {
					return "", err
				}
				return jsonString(nonNil(cols))
			},
		},
		{
			Name:        "search_views",
			Description: "Find views whose name or definition matches the given text.",
			Schema:      objectSchema(map[string]any{"text": strProp("text to match against views")}, "text"),
			Handle: func(ctx context.Context, c *core.Core, args json.RawMessage) (string, error) {
				var a struct{ Text string }
				if err := decode(args, &a); err != nil {
					return "", err
				}
				if a.Text == "" {
					return "", errors.New("text is required")
				}
				views, err := c.SearchViews(ctx, a.Text)
				if err != nil {
					return "", err
				}
				return jsonString(nonNil(views))
			},
		},

		// --- workspace files ---
		{
			Name:        "list_workspace_files",
			Description: "List the SQL files saved in the current workspace.",
			Schema:      objectSchema(nil),
			Handle: func(ctx context.Context, c *core.Core, _ json.RawMessage) (string, error) {
				names, err := c.ListSQLFiles()
				if err != nil {
					return "", err
				}
				return jsonString(nonNil(names))
			},
		},
		{
			Name:        "read_workspace_file",
			Description: "Read the contents of a SQL file in the current workspace.",
			Schema:      objectSchema(map[string]any{"name": strProp("SQL file name (without extension)")}, "name"),
			Handle: func(ctx context.Context, c *core.Core, args json.RawMessage) (string, error) {
				var a struct{ Name string }
				if err := decode(args, &a); err != nil {
					return "", err
				}
				if a.Name == "" {
					return "", errors.New("name is required")
				}
				return c.ReadSQLFile(a.Name)
			},
		},
		{
			Name:        "write_workspace_file",
			Description: "Create or overwrite a SQL file in the current workspace.",
			Schema: objectSchema(map[string]any{
				"name":    strProp("SQL file name (without extension)"),
				"content": strProp("file contents"),
			}, "name", "content"),
			Handle: func(ctx context.Context, c *core.Core, args json.RawMessage) (string, error) {
				var a struct{ Name, Content string }
				if err := decode(args, &a); err != nil {
					return "", err
				}
				if a.Name == "" {
					return "", errors.New("name is required")
				}
				if err := c.WriteSQLFile(a.Name, a.Content); err != nil {
					return "", err
				}
				return "wrote " + a.Name, nil
			},
		},

		// --- query / statement execution ---
		{
			Name:        "run_query",
			Description: "Run SQL on the connected database. SELECT-style statements return rows; other statements report rows affected. Dangerous statements are refused unless confirm=true, and read-only mode / production guards apply.",
			Schema: objectSchema(map[string]any{
				"sql":     strProp("the SQL to run"),
				"confirm": boolProp("set true to authorize a statement flagged dangerous (mirrors the interactive confirmation)"),
			}, "sql"),
			Handle: func(ctx context.Context, c *core.Core, args json.RawMessage) (string, error) {
				var a struct {
					SQL     string
					Confirm bool
				}
				if err := decode(args, &a); err != nil {
					return "", err
				}
				return runSQL(ctx, c, a.SQL, a.Confirm)
			},
		},
		{
			Name:        "run_saved_sql",
			Description: "Run the SQL stored in a workspace file. Same safety rules as run_query.",
			Schema: objectSchema(map[string]any{
				"name":    strProp("SQL file name (without extension)"),
				"confirm": boolProp("set true to authorize a statement flagged dangerous"),
			}, "name"),
			Handle: func(ctx context.Context, c *core.Core, args json.RawMessage) (string, error) {
				var a struct {
					Name    string
					Confirm bool
				}
				if err := decode(args, &a); err != nil {
					return "", err
				}
				if a.Name == "" {
					return "", errors.New("name is required")
				}
				sql, err := c.ReadSQLFile(a.Name)
				if err != nil {
					return "", err
				}
				return runSQL(ctx, c, sql, a.Confirm)
			},
		},

		// --- linting ---
		{
			Name: "lint_sql",
			Description: "Lint SQL for safety/correctness, lexical syntax, and style issues. Static; needs no connection and never executes the SQL. With live=true and a connection, also validates each query against the live database (deep syntax, unknown tables/columns) via EXPLAIN. Returns a JSON array of findings.",
			Schema: objectSchema(map[string]any{
				"sql":  strProp("the SQL to lint"),
				"live": boolProp("also validate queries against the connected database (requires a connection)"),
			}, "sql"),
			Handle: func(ctx context.Context, c *core.Core, args json.RawMessage) (string, error) {
				var a struct {
					SQL  string
					Live bool
				}
				if err := decode(args, &a); err != nil {
					return "", err
				}
				if strings.TrimSpace(a.SQL) == "" {
					return "", errors.New("sql is required")
				}
				findings := c.Lint(a.SQL)
				if a.Live && c.Connected() {
					if live, err := c.LiveLint(ctx, a.SQL); err == nil {
						findings = append(findings, live...)
					}
				}
				return jsonString(nonNil(findings))
			},
		},

		// --- import / export ---
		{
			Name:        "export_query",
			Description: "Export the results of a saved SQL file to a file (format inferred from extension: .csv/.tsv/.txt/.xlsx). Returns the row count.",
			Schema: objectSchema(map[string]any{
				"sql_name": strProp("saved SQL file to run for the export"),
				"dest":     strProp("destination path (resolved within the workspace export folder)"),
				"exact":    boolProp("use an exact two-pass width computation for fixed-width output"),
			}, "sql_name", "dest"),
			Handle: func(ctx context.Context, c *core.Core, args json.RawMessage) (string, error) {
				var a struct {
					SQLName string `json:"sql_name"`
					Dest    string `json:"dest"`
					Exact   bool   `json:"exact"`
				}
				if err := decode(args, &a); err != nil {
					return "", err
				}
				if a.SQLName == "" || a.Dest == "" {
					return "", errors.New("sql_name and dest are required")
				}
				n, truncated, err := c.ExportQueryFile(ctx, a.SQLName, a.Dest, a.Exact)
				if err != nil {
					return "", err
				}
				msg := fmt.Sprintf("exported %d row(s) to %s", n, a.Dest)
				if truncated {
					msg += " (truncated at the row cap)"
				}
				return msg, nil
			},
		},
		{
			Name:        "import_file",
			Description: "Import a data file (.csv/.tsv/.txt/.xlsx) into a table. Returns the row count.",
			Schema: objectSchema(map[string]any{
				"src":   strProp("source path (resolved within the workspace import folder)"),
				"table": strProp("destination table"),
				"sheet": strProp("worksheet name for .xlsx sources (optional)"),
			}, "src", "table"),
			Handle: func(ctx context.Context, c *core.Core, args json.RawMessage) (string, error) {
				var a struct {
					Src   string `json:"src"`
					Table string `json:"table"`
					Sheet string `json:"sheet"`
				}
				if err := decode(args, &a); err != nil {
					return "", err
				}
				if a.Src == "" || a.Table == "" {
					return "", errors.New("src and table are required")
				}
				n, err := c.ImportFile(ctx, a.Src, a.Table, a.Sheet)
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("imported %d row(s) into %s", n, a.Table), nil
			},
		},
	}
}

// runSQL applies the safety policy then executes. SELECT-style reads stream
// rows (capped); writes go through RunStatement, which is the core's hard guard.
// Statements the policy flags for confirmation are refused unless confirm=true —
// the headless analogue of the interactive prompt.
func runSQL(ctx context.Context, c *core.Core, sql string, confirm bool) (string, error) {
	sql = strings.TrimSpace(sql)
	if sql == "" {
		return "", errors.New("sql is required")
	}
	if isQuery(sql) {
		return runQueryRows(ctx, c, sql)
	}
	action, _, reason := c.GuardStatement(sql)
	switch action {
	case safety.Block:
		return "", errors.New(reason)
	case safety.Confirm:
		if !confirm {
			return "", errors.New(reason + " — re-call with confirm=true to proceed")
		}
	}
	res, err := c.RunStatement(ctx, sql)
	if err != nil {
		return "", err
	}
	if res.Message != "" {
		return res.Message, nil
	}
	return fmt.Sprintf("%d row(s) affected", res.RowsAffected), nil
}

// runQueryRows streams a result set into a JSON object {columns, rows, truncated}.
func runQueryRows(ctx context.Context, c *core.Core, sql string) (string, error) {
	rs, err := c.RunQuery(ctx, sql)
	if err != nil {
		return "", err
	}
	defer rs.Close()

	rowCap := c.Settings().MaxRowsDefault
	if rowCap <= 0 {
		rowCap = 1000
	}
	cols := rs.Columns()
	rows := make([][]any, 0)
	truncated := false
	for rs.Next() {
		if len(rows) >= rowCap {
			truncated = true
			break
		}
		vals, err := rs.Values()
		if err != nil {
			return "", err
		}
		rows = append(rows, vals)
	}
	if err := rs.Err(); err != nil {
		return "", err
	}
	return jsonString(map[string]any{
		"columns":   cols,
		"rows":      rows,
		"row_count": len(rows),
		"truncated": truncated,
	})
}

// isQuery reports whether sql is a row-returning statement, mirroring the TUI's
// classification so both front-ends route reads and writes identically.
func isQuery(sql string) bool {
	s := strings.ToLower(strings.TrimSpace(sql))
	for _, kw := range []string{"select", "with", "show", "explain", "values", "table"} {
		if s == kw || strings.HasPrefix(s, kw+" ") || strings.HasPrefix(s, kw+"\n") {
			return true
		}
	}
	return false
}
