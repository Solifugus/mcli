package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Solifugus/mcli/internal/core"
	"github.com/Solifugus/mcli/internal/core/adapter"
	"github.com/Solifugus/mcli/internal/core/assist"
	"github.com/Solifugus/mcli/internal/core/safety"
)

// uiArgs is the common argument shape for the single-target ui_* guidance tools.
type uiArgs struct {
	Target string `json:"target"`
	Text   string `json:"text"`
}

// guideHandler builds a tool handler that decodes uiArgs, maps them to an
// assist.Event via build, and publishes it through the core's guidance channel.
// A missing target is rejected; a missing live session surfaces as an error so
// the AI learns its guidance had no surface to render on.
func guideHandler(build func(uiArgs) assist.Event) func(context.Context, *core.Core, json.RawMessage) (string, error) {
	return func(_ context.Context, c *core.Core, args json.RawMessage) (string, error) {
		var a uiArgs
		if err := decode(args, &a); err != nil {
			return "", err
		}
		if a.Target == "" {
			return "", errors.New("target is required")
		}
		if err := c.Guide(build(a)); err != nil {
			return "", err
		}
		return "guidance delivered to the live session", nil
	}
}

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

func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}
func boolProp(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}

// strArrayProp describes an array-of-strings parameter, optionally constrained
// to an enum of allowed values.
func strArrayProp(desc string, enum ...string) map[string]any {
	items := map[string]any{"type": "string"}
	if len(enum) > 0 {
		items["enum"] = enum
	}
	return map[string]any{"type": "array", "items": items, "description": desc}
}

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
			Name:        "get_capabilities",
			Description: "Report which optional features the connected engine supports (explain, lineage, source, table_functions, jobs, security, security_edit). Use it to decide whether a feature is available before calling its tool.",
			Schema:      objectSchema(nil),
			Handle: func(_ context.Context, c *core.Core, _ json.RawMessage) (string, error) {
				if !c.Connected() {
					return "", core.ErrNotConnected
				}
				caps := c.Capabilities()
				all := map[string]bool{}
				for _, cap := range adapter.AllCapabilities() {
					all[string(cap)] = caps.Has(cap)
				}
				supported := make([]string, 0, len(caps))
				for _, cap := range caps.Sorted() {
					supported = append(supported, string(cap))
				}
				return jsonString(map[string]any{
					"server":    c.ConnServer(),
					"supported": supported,
					"all":       all,
				})
			},
		},
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
		{
			Name:        "search_objects",
			Description: "Find database objects by type and name substring. 'kinds' filters by object kind (any of table, view, procedure, function; omit or empty for all kinds); 'substring' matches the object name case-insensitively (omit or empty for all names).",
			Schema: objectSchema(map[string]any{
				"kinds":     strArrayProp("object kinds to include", "table", "view", "procedure", "function"),
				"substring": strProp("case-insensitive substring to match against object names"),
			}),
			Handle: func(ctx context.Context, c *core.Core, args json.RawMessage) (string, error) {
				var a struct {
					Kinds     []string `json:"kinds"`
					Substring string   `json:"substring"`
				}
				if err := decode(args, &a); err != nil {
					return "", err
				}
				kinds := make([]adapter.ObjectKind, 0, len(a.Kinds))
				for _, k := range a.Kinds {
					kinds = append(kinds, adapter.ObjectKind(k))
				}
				refs, err := c.SearchObjects(ctx, kinds, a.Substring)
				if err != nil {
					return "", err
				}
				return jsonString(nonNil(refs))
			},
		},
		{
			Name:        "get_source",
			Description: "Get the definition text (CREATE / body) of a view, procedure, or function. Requires the 'source' capability (see get_capabilities). Tables have no stored definition — use describe_table for their design.",
			Schema:      objectSchema(map[string]any{"name": strProp("object name, optionally schema-qualified (schema.name)")}, "name"),
			Handle: func(ctx context.Context, c *core.Core, args json.RawMessage) (string, error) {
				var a struct{ Name string }
				if err := decode(args, &a); err != nil {
					return "", err
				}
				if a.Name == "" {
					return "", errors.New("name is required")
				}
				src, err := c.Source(ctx, a.Name)
				if err != nil {
					return "", err
				}
				return jsonString(map[string]any{
					"schema":   src.Ref.Schema,
					"name":     src.Ref.Name,
					"type":     src.Ref.Type,
					"language": src.Language,
					"body":     src.Body,
				})
			},
		},
		{
			Name:        "search_routines",
			Description: "Find procedures and functions whose name or body contains the given text (case-insensitive). The body search that complements search_objects (which matches names only). Requires the 'source' capability.",
			Schema:      objectSchema(map[string]any{"text": strProp("substring to match against routine names and bodies")}, "text"),
			Handle: func(ctx context.Context, c *core.Core, args json.RawMessage) (string, error) {
				var a struct{ Text string }
				if err := decode(args, &a); err != nil {
					return "", err
				}
				refs, err := c.SearchRoutines(ctx, a.Text)
				if err != nil {
					return "", err
				}
				return jsonString(nonNil(refs))
			},
		},
		{
			Name:        "search_table_functions",
			Description: "Find table-valued functions (functions that return a rowset) whose name matches the given substring. Each result includes a dialect-correct query template that reads it as tabular data. Requires the 'table_functions' capability (see get_capabilities).",
			Schema:      objectSchema(map[string]any{"substring": strProp("case-insensitive substring to match against table-function names")}),
			Handle: func(ctx context.Context, c *core.Core, args json.RawMessage) (string, error) {
				var a struct {
					Substring string `json:"substring"`
				}
				if err := decode(args, &a); err != nil {
					return "", err
				}
				refs, err := c.SearchTableFunctions(ctx, a.Substring)
				if err != nil {
					return "", err
				}
				type tfView struct {
					Schema string `json:"schema"`
					Name   string `json:"name"`
					Query  string `json:"query"`
				}
				views := make([]tfView, 0, len(refs))
				for _, r := range refs {
					views = append(views, tfView{Schema: r.Schema, Name: r.Name, Query: c.TabularQuery(r)})
				}
				return jsonString(views)
			},
		},

		// --- scheduling: jobs / agents (design §29) ---
		{
			Name:        "list_jobs",
			Description: "List scheduled jobs (SQL Server Agent jobs, Oracle Scheduler jobs, or MySQL events) whose name matches the given substring (omit or empty for all). Requires the 'jobs' capability (see get_capabilities); engines without a scheduler (e.g. Postgres) do not advertise it.",
			Schema:      objectSchema(map[string]any{"substring": strProp("case-insensitive substring to match against job names")}),
			Handle: func(ctx context.Context, c *core.Core, args json.RawMessage) (string, error) {
				var a struct {
					Substring string `json:"substring"`
				}
				if err := decode(args, &a); err != nil {
					return "", err
				}
				jobs, err := c.ListJobs(ctx, a.Substring)
				if err != nil {
					return "", err
				}
				return jsonString(nonNil(jobs))
			},
		},
		{
			Name:        "describe_job",
			Description: "Describe a scheduled job's design: its owner, schedule, next/last run, comment, and ordered steps. Requires the 'jobs' capability.",
			Schema:      objectSchema(map[string]any{"name": strProp("job / event name")}, "name"),
			Handle: func(ctx context.Context, c *core.Core, args json.RawMessage) (string, error) {
				var a struct{ Name string }
				if err := decode(args, &a); err != nil {
					return "", err
				}
				if a.Name == "" {
					return "", errors.New("name is required")
				}
				job, err := c.DescribeJob(ctx, a.Name)
				if err != nil {
					return "", err
				}
				return jsonString(job)
			},
		},
		{
			Name:        "job_history",
			Description: "Return recent run records for a scheduled job, newest first (each with start, status, and message). 'limit' caps the count (omit or 0 for the engine default). MySQL events keep no run history and return an empty list. Requires the 'jobs' capability.",
			Schema: objectSchema(map[string]any{
				"name":  strProp("job / event name"),
				"limit": map[string]any{"type": "integer", "description": "maximum number of runs to return (0 = engine default)"},
			}, "name"),
			Handle: func(ctx context.Context, c *core.Core, args json.RawMessage) (string, error) {
				var a struct {
					Name  string `json:"name"`
					Limit int    `json:"limit"`
				}
				if err := decode(args, &a); err != nil {
					return "", err
				}
				if a.Name == "" {
					return "", errors.New("name is required")
				}
				runs, err := c.JobHistory(ctx, a.Name, a.Limit)
				if err != nil {
					return "", err
				}
				return jsonString(nonNil(runs))
			},
		},

		// --- security: users / roles / grants (design §29, read side) ---
		{
			Name:        "list_principals",
			Description: "List security principals (users and/or roles). 'kind' filters by 'user' or 'role' (omit or empty for both); 'substring' matches the name case-insensitively. Requires the 'security' capability (see get_capabilities).",
			Schema: objectSchema(map[string]any{
				"kind":      map[string]any{"type": "string", "enum": []string{"user", "role"}, "description": "filter to users or roles (omit for both)"},
				"substring": strProp("case-insensitive substring to match against principal names"),
			}),
			Handle: func(ctx context.Context, c *core.Core, args json.RawMessage) (string, error) {
				var a struct {
					Kind      string `json:"kind"`
					Substring string `json:"substring"`
				}
				if err := decode(args, &a); err != nil {
					return "", err
				}
				refs, err := c.ListPrincipals(ctx, a.Kind, a.Substring)
				if err != nil {
					return "", err
				}
				return jsonString(nonNil(refs))
			},
		},
		{
			Name:        "describe_principal",
			Description: "Describe a security principal (user or role): its attributes, role membership, members, and grants. Requires the 'security' capability.",
			Schema:      objectSchema(map[string]any{"name": strProp("principal name (for MySQL, \"user@host\")")}, "name"),
			Handle: func(ctx context.Context, c *core.Core, args json.RawMessage) (string, error) {
				var a struct{ Name string }
				if err := decode(args, &a); err != nil {
					return "", err
				}
				if a.Name == "" {
					return "", errors.New("name is required")
				}
				p, err := c.DescribePrincipal(ctx, a.Name)
				if err != nil {
					return "", err
				}
				return jsonString(p)
			},
		},

		// --- security editing: guarded DCL (design §21, §29) ---
		// Each tool BUILDS the dialect-correct statement in the core, then runs it
		// through runSQL — the same guarded path as run_query — so read-only mode,
		// production, and dangerous-statement guards apply identically. GRANT/CREATE
		// are plain writes (blocked in read-only, confirmed on prod); DROP is
		// dangerous and needs confirm=true.
		{
			Name:        "grant",
			Description: "Grant or revoke privileges or a role. With 'on' set, grants the 'privileges' ON that object; without 'on', 'privileges' are role names granted directly. Set 'revoke':true for REVOKE. Requires the 'security_edit' capability. Runs through the safety guard: refused in read-only mode, confirmed on production (confirm=true).",
			Schema: objectSchema(map[string]any{
				"privileges": strArrayProp("privileges to grant (e.g. SELECT, INSERT) — or role names when 'on' is omitted"),
				"on":         strProp("object to grant on (schema.table); omit for a role grant"),
				"to":         strProp("principal (for MySQL, \"user@host\")"),
				"revoke":     boolProp("REVOKE instead of GRANT"),
				"confirm":    boolProp("authorize a statement flagged for confirmation (production/dangerous)"),
			}, "privileges", "to"),
			Handle: func(ctx context.Context, c *core.Core, args json.RawMessage) (string, error) {
				var a struct {
					Privileges []string `json:"privileges"`
					On         string   `json:"on"`
					To         string   `json:"to"`
					Revoke     bool     `json:"revoke"`
					Confirm    bool     `json:"confirm"`
				}
				if err := decode(args, &a); err != nil {
					return "", err
				}
				if a.To == "" {
					return "", errors.New("to is required")
				}
				dcl, err := c.BuildGrant(a.Privileges, a.On, a.To, a.Revoke)
				if err != nil {
					return "", err
				}
				return runGeneratedDCL(ctx, c, dcl, a.Confirm)
			},
		},
		{
			Name:        "create_user",
			Description: "Create a user/login with a password (dialect-correct). Requires the 'security_edit' capability. Runs through the safety guard (refused in read-only mode, confirmed on production).",
			Schema: objectSchema(map[string]any{
				"name":     strProp("user/login name (for MySQL, \"user@host\")"),
				"password": strProp("the new user's password"),
				"confirm":  boolProp("authorize a statement flagged for confirmation (production)"),
			}, "name", "password"),
			Handle: func(ctx context.Context, c *core.Core, args json.RawMessage) (string, error) {
				var a struct {
					Name     string `json:"name"`
					Password string `json:"password"`
					Confirm  bool   `json:"confirm"`
				}
				if err := decode(args, &a); err != nil {
					return "", err
				}
				if a.Name == "" || a.Password == "" {
					return "", errors.New("name and password are required")
				}
				dcl, err := c.BuildCreateUser(a.Name, a.Password)
				if err != nil {
					return "", err
				}
				return runGeneratedDCL(ctx, c, dcl, a.Confirm)
			},
		},
		{
			Name:        "drop_user",
			Description: "Drop a user/login/role (dialect-correct). Requires the 'security_edit' capability. DROP is dangerous, so this needs confirm=true and is blocked on production servers configured to block dangerous statements.",
			Schema: objectSchema(map[string]any{
				"name":    strProp("user/login/role name (for MySQL, \"user@host\")"),
				"confirm": boolProp("authorize the DROP (required — DROP is dangerous)"),
			}, "name"),
			Handle: func(ctx context.Context, c *core.Core, args json.RawMessage) (string, error) {
				var a struct {
					Name    string `json:"name"`
					Confirm bool   `json:"confirm"`
				}
				if err := decode(args, &a); err != nil {
					return "", err
				}
				if a.Name == "" {
					return "", errors.New("name is required")
				}
				dcl, err := c.BuildDropUser(a.Name)
				if err != nil {
					return "", err
				}
				return runGeneratedDCL(ctx, c, dcl, a.Confirm)
			},
		},

		// --- UI guidance (design §26): guide the user in their live surface.
		// These publish to the assist bus and only take effect when a front-end
		// (TUI/GUI) is attached; otherwise they report no live session. All are
		// non-destructive — a prefill fills an input, it never executes. ---
		{
			Name:        "ui_describe_screen",
			Description: "Report the active front-end surface and the semantic target ids that UI guidance can address (e.g. input-line, editor, grid). Returns live:false when no TUI/GUI is attached. Call this before ui_* guidance to choose valid targets.",
			Schema:      objectSchema(nil),
			Handle: func(_ context.Context, c *core.Core, _ json.RawMessage) (string, error) {
				return jsonString(map[string]any{
					"live":    c.LiveSession(),
					"targets": []string{assist.TargetInputLine, assist.TargetEditor, assist.TargetGrid, assist.TargetResults},
				})
			},
		},
		{
			Name:        "ui_highlight",
			Description: "Draw the user's attention to an element in their live surface (the front-end pulses/blinks it). 'target' is a semantic id from ui_describe_screen.",
			Schema:      objectSchema(map[string]any{"target": strProp("semantic element id, e.g. input-line")}, "target"),
			Handle:      guideHandler(func(a uiArgs) assist.Event { return assist.Event{Kind: assist.KindHighlight, Target: a.Target} }),
		},
		{
			Name:        "ui_focus",
			Description: "Move focus to an element in the user's live surface. 'target' is a semantic id from ui_describe_screen.",
			Schema:      objectSchema(map[string]any{"target": strProp("semantic element id, e.g. input-line")}, "target"),
			Handle:      guideHandler(func(a uiArgs) assist.Event { return assist.Event{Kind: assist.KindFocus, Target: a.Target} }),
		},
		{
			Name:        "ui_prefill",
			Description: "Put text into an input in the user's live surface WITHOUT submitting it — e.g. stage a SQL statement on the REPL input line for the user to review and run. Non-destructive.",
			Schema: objectSchema(map[string]any{
				"target": strProp("semantic input id, e.g. input-line"),
				"text":   strProp("text to place in the input (not executed)"),
			}, "target", "text"),
			Handle: guideHandler(func(a uiArgs) assist.Event {
				return assist.Event{Kind: assist.KindPrefill, Target: a.Target, Text: a.Text}
			}),
		},
		{
			Name:        "ui_annotate",
			Description: "Attach an explanatory callout to an element in the user's live surface.",
			Schema: objectSchema(map[string]any{
				"target": strProp("semantic element id"),
				"text":   strProp("callout text to show the user"),
			}, "target", "text"),
			Handle: guideHandler(func(a uiArgs) assist.Event {
				return assist.Event{Kind: assist.KindAnnotate, Target: a.Target, Text: a.Text}
			}),
		},
		{
			Name:        "ui_demo",
			Description: "Walk the user through a task as an ordered, narrated sequence of steps rendered in their live surface. Each step has a title and description; optionally a target element and a suggested action (e.g. text to type).",
			Schema: objectSchema(map[string]any{
				"steps": map[string]any{
					"type":        "array",
					"description": "ordered walkthrough steps",
					"items": objectSchema(map[string]any{
						"title":       strProp("short step title"),
						"description": strProp("what the user should understand or do"),
						"target":      strProp("optional semantic element id this step concerns"),
						"action":      strProp("optional suggested action, e.g. text to type"),
					}, "title", "description"),
				},
			}, "steps"),
			Handle: func(_ context.Context, c *core.Core, args json.RawMessage) (string, error) {
				var a struct {
					Steps []assist.Step `json:"steps"`
				}
				if err := decode(args, &a); err != nil {
					return "", err
				}
				if len(a.Steps) == 0 {
					return "", errors.New("steps is required and must be non-empty")
				}
				if err := c.Guide(assist.Event{Kind: assist.KindDemo, Steps: a.Steps}); err != nil {
					return "", err
				}
				return "guidance delivered to the live session", nil
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
			Name:        "lint_sql",
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

// runGeneratedDCL executes a generated security-editing statement through the
// same guarded path as run_query (runSQL), returning the statement alongside its
// result so the caller sees exactly what ran. The safety guard is the single
// chokepoint: read-only refuses, production/dangerous requires confirm=true.
func runGeneratedDCL(ctx context.Context, c *core.Core, dcl string, confirm bool) (string, error) {
	out, err := runSQL(ctx, c, dcl, confirm)
	if err != nil {
		return "", err
	}
	return dcl + "\n" + out, nil
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
