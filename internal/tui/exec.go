package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/Solifugus/mcli/internal/core/adapter"
)

// asyncResultMsg is delivered when a background command (connect, query, schema
// introspection) finishes. Output is pre-formatted into lines off the UI thread.
type asyncResultMsg struct {
	lines []string
	err   error
}

// asyncRun is a unit of background work returning a result message. It receives a
// cancellable context wired to Ctrl-C.
type asyncRun func(ctx context.Context) asyncResultMsg

// --- async command builders ---

func (m *Model) cmdConnect(args []string) (cmdResult, asyncRun) {
	if len(args) < 1 {
		names := sortedServerNames(m.core.Servers())
		if len(names) == 0 {
			return out(`usage: \connect <server> — no servers configured (see \server list)`), nil
		}
		return out(`usage: \connect <server>`, "available: "+strings.Join(names, ", ")), nil
	}
	name := args[0]
	c := m.core
	return cmdResult{}, func(ctx context.Context) asyncResultMsg {
		if err := c.Connect(ctx, name); err != nil {
			return asyncResultMsg{err: err}
		}
		return asyncResultMsg{lines: []string{"connected to " + name}}
	}
}

func (m *Model) cmdUse(args []string) (cmdResult, asyncRun) {
	if len(args) < 1 {
		return out(`usage: use <database>`), nil
	}
	db := args[0]
	c := m.core
	return cmdResult{}, func(ctx context.Context) asyncResultMsg {
		if err := c.Use(ctx, db); err != nil {
			return asyncResultMsg{err: err}
		}
		return asyncResultMsg{lines: []string{"using database " + db}}
	}
}

func (m *Model) cmdList(args []string) (cmdResult, asyncRun) {
	if len(args) < 1 {
		return out(`usage: \list databases|schemas|tables|views`), nil
	}
	what := args[0]
	c := m.core
	return cmdResult{}, func(ctx context.Context) asyncResultMsg {
		switch what {
		case "databases", "database", "db":
			return linesOrErr(c.ListDatabases(ctx))
		case "schemas", "schema":
			return linesOrErr(c.ListSchemas(ctx))
		case "tables", "table":
			refs, err := c.ListTables(ctx)
			return objectLines(refs, err)
		case "views", "view":
			refs, err := c.ListViews(ctx)
			return objectLines(refs, err)
		default:
			return asyncResultMsg{lines: []string{`unknown \list target: ` + what}}
		}
	}
}

func (m *Model) cmdDescribe(args []string) (cmdResult, asyncRun) {
	if len(args) < 1 {
		return out(`usage: \describe <table>`), nil
	}
	name := args[0]
	c := m.core
	return cmdResult{}, func(ctx context.Context) asyncResultMsg {
		detail, err := c.Describe(ctx, name)
		if err != nil {
			return asyncResultMsg{err: err}
		}
		rows := make([][]string, 0, len(detail.Columns))
		for _, col := range detail.Columns {
			null := "not null"
			if col.Nullable {
				null = "null"
			}
			rows = append(rows, []string{col.Name, col.DataType, null, col.Key})
		}
		return asyncResultMsg{lines: renderTable([]string{"column", "type", "nullable", "key"}, rows)}
	}
}

// sqlRunner runs bare SQL: row-returning statements stream into an aligned table
// capped at max_rows_default; everything else reports rows affected.
func (m *Model) sqlRunner(sql string) asyncRun {
	c := m.core
	maxRows := m.core.Settings().MaxRowsDefault
	return func(ctx context.Context) asyncResultMsg {
		if !isQuery(sql) {
			res, err := c.RunStatement(ctx, sql)
			if err != nil {
				return asyncResultMsg{err: err}
			}
			msg := res.Message
			if msg == "" {
				msg = fmt.Sprintf("%d row(s) affected", res.RowsAffected)
			}
			return asyncResultMsg{lines: []string{msg}}
		}

		rs, err := c.RunQuery(ctx, sql)
		if err != nil {
			return asyncResultMsg{err: err}
		}
		defer rs.Close()

		cols := rs.Columns()
		var rows [][]string
		truncated := false
		for rs.Next() {
			if maxRows > 0 && len(rows) >= maxRows {
				truncated = true
				break
			}
			vals, err := rs.Values()
			if err != nil {
				return asyncResultMsg{err: err}
			}
			rows = append(rows, toStrings(vals))
		}
		if err := rs.Err(); err != nil {
			return asyncResultMsg{err: err}
		}

		lines := renderTable(cols, rows)
		if truncated {
			lines = append(lines, fmt.Sprintf("(showing first %d rows; full-result grid view arrives in Phase 4)", maxRows))
		} else {
			lines = append(lines, fmt.Sprintf("(%d row%s)", len(rows), plural(len(rows))))
		}
		return asyncResultMsg{lines: lines}
	}
}

// --- result formatting helpers ---

func linesOrErr(items []string, err error) asyncResultMsg {
	if err != nil {
		return asyncResultMsg{err: err}
	}
	return asyncResultMsg{lines: items}
}

func objectLines(refs []adapter.ObjectRef, err error) asyncResultMsg {
	if err != nil {
		return asyncResultMsg{err: err}
	}
	lines := make([]string, 0, len(refs))
	for _, r := range refs {
		if r.Schema != "" {
			lines = append(lines, r.Schema+"."+r.Name)
		} else {
			lines = append(lines, r.Name)
		}
	}
	return asyncResultMsg{lines: lines}
}

func isQuery(sql string) bool {
	s := strings.ToLower(strings.TrimSpace(sql))
	for _, kw := range []string{"select", "with", "show", "explain", "values", "table"} {
		if s == kw || strings.HasPrefix(s, kw+" ") || strings.HasPrefix(s, kw+"\n") {
			return true
		}
	}
	return false
}

func toStrings(vals []any) []string {
	out := make([]string, len(vals))
	for i, v := range vals {
		if v == nil {
			out[i] = "NULL"
		} else {
			out[i] = fmt.Sprintf("%v", v)
		}
	}
	return out
}

func renderTable(cols []string, rows [][]string) []string {
	widths := make([]int, len(cols))
	for i, c := range cols {
		widths[i] = len(c)
	}
	for _, r := range rows {
		for i, v := range r {
			if i < len(widths) && len(v) > widths[i] {
				widths[i] = len(v)
			}
		}
	}
	lines := []string{rowLine(cols, widths)}
	seps := make([]string, len(cols))
	for i := range seps {
		seps[i] = strings.Repeat("-", widths[i])
	}
	lines = append(lines, rowLine(seps, widths))
	for _, r := range rows {
		lines = append(lines, rowLine(r, widths))
	}
	return lines
}

func rowLine(cells []string, widths []int) string {
	parts := make([]string, len(widths))
	for i := range widths {
		v := ""
		if i < len(cells) {
			v = cells[i]
		}
		parts[i] = padRight(v, widths[i])
	}
	return strings.TrimRight(strings.Join(parts, "  "), " ")
}

func padRight(s string, w int) string {
	if len(s) < w {
		return s + strings.Repeat(" ", w-len(s))
	}
	return s
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
