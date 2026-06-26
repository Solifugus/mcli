package tui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/Solifugus/mcli/internal/core"
	"github.com/Solifugus/mcli/internal/core/adapter"
	"github.com/Solifugus/mcli/internal/core/safety"
)

var errNoCurrentResult = errors.New("no current result — run a query first")

// asyncResultMsg is delivered when a background command (connect, query, schema
// introspection) finishes. Output is pre-formatted into lines off the UI thread.
type asyncResultMsg struct {
	lines    []string
	err      error
	result   *resultSet // non-nil for row-returning queries; openable in the grid
	pwPrompt *pwReq     // set when the op needs an interactive password to proceed
}

// pwReq asks the front-end to collect a password (masked) and then run the work
// that needs it. It is how connect/test recover from ErrPasswordRequired: the
// background op returns one of these, the UI prompts, and run(pw) is launched as
// the next background op. Keeping keyring access in the background op (not on the
// UI thread) means a slow Secret Service call never blocks rendering.
type pwReq struct {
	label string
	run   func(pw string) asyncRun
}

// asyncRun is a unit of background work returning a result message. It receives a
// cancellable context wired to Ctrl-C.
type asyncRun func(ctx context.Context) asyncResultMsg

// --- async command builders ---

func (m *Model) cmdConnect(args []string) (cmdResult, action) {
	if len(args) < 1 {
		names := sortedServerNames(m.core.Servers())
		if len(names) == 0 {
			return out(`usage: \connect <server> — no servers configured (\server add)`), sync()
		}
		return out(`usage: \connect <server>`, "available: "+strings.Join(names, ", ")), sync()
	}
	name := args[0]
	c := m.core
	connected := []string{"connected to " + name}
	return cmdResult{}, async(func(ctx context.Context) asyncResultMsg {
		switch err := c.Connect(ctx, name); {
		case err == nil:
			return asyncResultMsg{lines: connected}
		case errors.Is(err, core.ErrPasswordRequired):
			return asyncResultMsg{pwPrompt: &pwReq{
				label: "password for " + name + ": ",
				run: func(pw string) asyncRun {
					return func(ctx context.Context) asyncResultMsg {
						if err := c.ConnectWithPassword(ctx, name, pw); err != nil {
							return asyncResultMsg{err: err}
						}
						return asyncResultMsg{lines: connected}
					}
				},
			}}
		default:
			return asyncResultMsg{err: err}
		}
	})
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

// cmdExport writes a query, table, or the current result to a file (§16):
//
//	\export query <name> to <path>
//	\export table <name> to <path>
//	\export current to <path>
//
// A trailing `exact` token (fixed-width .txt/.fix only) forces the two-pass
// streaming export that measures every row before writing — nothing curtailed,
// at the cost of running the query twice. Without it, fixed-width export buffers
// up to 10000 rows and notes when the result is larger.
func (m *Model) cmdExport(args []string) (cmdResult, asyncRun) {
	const usage = `usage: \export query <name>|table <name>|current to <path> [exact]`
	exact := false
	if n := len(args); n > 0 && args[n-1] == "exact" {
		exact = true
		args = args[:n-1]
	}
	toIdx := indexOf(args, "to")
	if toIdx < 1 || toIdx == len(args)-1 {
		return out(usage), nil
	}
	head := args[:toIdx]
	dest := args[toIdx+1]
	c := m.core

	switch head[0] {
	case "query":
		if len(head) < 2 {
			return out(usage), nil
		}
		name := head[1]
		return cmdResult{}, func(ctx context.Context) asyncResultMsg {
			return exportResult(c.ExportQueryFile(ctx, name, dest, exact))(dest)
		}
	case "table":
		if len(head) < 2 {
			return out(usage), nil
		}
		name := head[1]
		return cmdResult{}, func(ctx context.Context) asyncResultMsg {
			return exportResult(c.ExportTable(ctx, name, dest, exact))(dest)
		}
	case "current":
		rs := m.lastResult
		return cmdResult{}, func(ctx context.Context) asyncResultMsg {
			if rs == nil {
				return asyncResultMsg{err: errNoCurrentResult}
			}
			n, err := c.ExportRows(rs.cols, rs.rows, dest)
			return exportResult(n, rs.truncated, err)(dest)
		}
	default:
		return out(usage), nil
	}
}

// cmdImport loads a delimited, .xlsx, or fixed-width file into a table:
//
//	\import <path> into <table>
//	\import <path> sheet <name> into <table>      (xlsx)
//	\import <path> widths 10,20,8 into <table>    (fixed-width)
//
// Fixed-width files carry no header, so `widths` is required and the comma-listed
// field widths map positionally onto the table's columns in declared order.
func (m *Model) cmdImport(args []string) (cmdResult, asyncRun) {
	const usage = `usage: \import <path> [sheet <name>|widths N,N,...] into <table>`
	intoIdx := indexOf(args, "into")
	if intoIdx < 1 || intoIdx == len(args)-1 {
		return out(usage), nil
	}
	src := args[0]
	table := args[intoIdx+1]
	c := m.core

	if wi := indexOf(args, "widths"); wi > 0 && wi+1 < intoIdx {
		widths, err := parseWidths(args[wi+1])
		if err != nil {
			return errOut(err), nil
		}
		return cmdResult{}, func(ctx context.Context) asyncResultMsg {
			n, err := c.ImportFixedFile(ctx, src, table, widths)
			if err != nil {
				return asyncResultMsg{err: err}
			}
			return asyncResultMsg{lines: []string{fmt.Sprintf("imported %d row%s into %s", n, plural(n), table)}}
		}
	}

	sheet := ""
	if si := indexOf(args, "sheet"); si > 0 && si+1 < intoIdx {
		sheet = strings.Trim(args[si+1], `"'`)
	}

	return cmdResult{}, func(ctx context.Context) asyncResultMsg {
		n, err := c.ImportFile(ctx, src, table, sheet)
		if err != nil {
			return asyncResultMsg{err: err}
		}
		return asyncResultMsg{lines: []string{fmt.Sprintf("imported %d row%s into %s", n, plural(n), table)}}
	}
}

// parseWidths parses a comma-separated list of positive column widths, e.g.
// "10,20,8", for fixed-width import.
func parseWidths(s string) ([]int, error) {
	parts := strings.Split(s, ",")
	widths := make([]int, 0, len(parts))
	for _, p := range parts {
		w, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || w <= 0 {
			return nil, fmt.Errorf("invalid width %q: widths must be positive integers like 10,20,8", p)
		}
		widths = append(widths, w)
	}
	return widths, nil
}

// exportResult turns an export outcome into a result-message builder keyed by the
// destination path. When truncated is set (a default fixed-width export that hit
// the row cap), it appends a note pointing at the exact two-pass mode.
func exportResult(n int, truncated bool, err error) func(dest string) asyncResultMsg {
	return func(dest string) asyncResultMsg {
		if err != nil {
			return asyncResultMsg{err: err}
		}
		lines := []string{fmt.Sprintf("exported %d row%s to %s", n, plural(n), dest)}
		if truncated {
			lines = append(lines, fmt.Sprintf("(stopped at %d rows; re-run with `exact` to export all with measured widths)", n))
		}
		return asyncResultMsg{lines: lines}
	}
}

func indexOf(ss []string, want string) int {
	for i, s := range ss {
		if s == want {
			return i
		}
	}
	return -1
}

// cmdRun executes a workspace SQL file against the live connection, reusing the
// bare-SQL runner (and its safety guard). Multi-statement files are a future
// enhancement.
func (m *Model) cmdRun(args []string) (cmdResult, action) {
	if len(args) < 1 {
		return out(`usage: \run <name>`), sync()
	}
	content, err := m.core.ReadSQLFile(args[0])
	if err != nil {
		return errOut(err), sync()
	}
	if strings.TrimSpace(content) == "" {
		return out("file " + args[0] + " is empty"), sync()
	}
	return m.guardedSQL(content)
}

// guardedSQL applies the safety policy (§17) to a statement before running it:
// Block reports the reason and runs nothing, Confirm wraps the run in a yes/no
// prompt, and Allow runs it as an ordinary background op. Every SQL entry point
// (bare line and \run) funnels through here so the guard cannot be bypassed.
func (m *Model) guardedSQL(sql string) (cmdResult, action) {
	runner := m.sqlRunner(sql)
	switch act, _, reason := m.core.GuardStatement(sql); act {
	case safety.Block:
		return out("blocked: " + reason), sync()
	case safety.Confirm:
		return cmdResult{}, action{confirm: &confirmReq{question: reason, run: runner}}
	default:
		return cmdResult{}, async(runner)
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

		// Fetch up to gridRowCap so the full result can be opened in the grid;
		// the inline view shows only the first maxRows.
		cols := rs.Columns()
		var rows [][]string
		capped := false
		for rs.Next() {
			if len(rows) >= gridRowCap {
				capped = true
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

		inline := rows
		inlineTrunc := false
		if maxRows > 0 && len(rows) > maxRows {
			inline = rows[:maxRows]
			inlineTrunc = true
		}
		lines := renderTable(cols, inline)
		switch {
		case capped:
			lines = append(lines, fmt.Sprintf("(showing first %d of %d+ rows; \\grid to view, refine with LIMIT)", len(inline), len(rows)))
		case inlineTrunc:
			lines = append(lines, fmt.Sprintf("(showing first %d of %d rows; \\grid to view all)", maxRows, len(rows)))
		default:
			lines = append(lines, fmt.Sprintf("(%d row%s)", len(rows), plural(len(rows))))
		}
		return asyncResultMsg{lines: lines, result: &resultSet{cols: cols, rows: rows, truncated: capped}}
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
