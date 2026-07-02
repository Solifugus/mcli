//go:build gui

package gui

import (
	"context"
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/Solifugus/mcli/internal/core/safety"
)

// gridFetchCap bounds how many rows the GUI pulls into the in-memory grid, so a
// runaway result can't exhaust memory. The table itself virtual-scrolls, so
// displaying this many is cheap; the cap is purely on retention. A hit is
// reported in the status line.
const gridFetchCap = 100_000

// editorPane is the query surface: a multi-line SQL editor over a paged result
// grid. Running SQL funnels through the core's safety guard (§17) exactly like
// the TUI's bare-line path, so read-only mode, dangerous-SQL confirmation, and
// production guards behave identically — they live in the core, not here.
type editorPane struct {
	app *App

	entry  *widget.Entry
	status *widget.Label

	grid  *widget.Table
	model gridModel

	root fyne.CanvasObject
}

// gridModel is the data backing the result table: a header plus string rows,
// already materialized from a RowStream.
type gridModel struct {
	cols   []string
	rows   [][]string
	widths []float32
}

func newEditorPane(a *App) *editorPane {
	e := &editorPane{app: a}

	e.entry = widget.NewMultiLineEntry()
	e.entry.SetPlaceHolder("Write SQL here, then Run (Ctrl+Enter)…")
	e.entry.Wrapping = fyne.TextWrapOff

	runBtn := widget.NewButton("Run", e.run)
	clearBtn := widget.NewButton("Clear", func() {
		e.setText("")
		e.setModel(gridModel{})
		e.status.SetText("")
	})
	e.status = widget.NewLabel("")

	e.grid = widget.NewTable(
		func() (int, int) {
			rows := len(e.model.rows) + 1 // +1 header row
			cols := len(e.model.cols)
			if cols == 0 {
				return 0, 0
			}
			return rows, cols
		},
		func() fyne.CanvasObject {
			l := widget.NewLabel("")
			l.Truncation = fyne.TextTruncateEllipsis
			return l
		},
		func(id widget.TableCellID, o fyne.CanvasObject) {
			l := o.(*widget.Label)
			if id.Row == 0 { // header
				if id.Col < len(e.model.cols) {
					l.SetText(e.model.cols[id.Col])
					l.TextStyle = fyne.TextStyle{Bold: true}
				}
				return
			}
			l.TextStyle = fyne.TextStyle{}
			r := id.Row - 1
			if r < len(e.model.rows) && id.Col < len(e.model.rows[r]) {
				l.SetText(e.model.rows[r][id.Col])
			} else {
				l.SetText("")
			}
		},
	)

	// Ctrl+Enter runs, matching the "Enter executes" spirit of the REPL without
	// stealing plain Enter from the multi-line editor.
	e.entry.OnSubmitted = func(string) {} // keep Enter inserting newlines
	toolbar := container.NewHBox(runBtn, clearBtn, e.status)

	editorBox := container.NewBorder(toolbar, nil, nil, nil, e.entry)
	split := container.NewVSplit(editorBox, e.grid)
	split.SetOffset(0.38)
	e.root = split
	return e
}

func (e *editorPane) object() fyne.CanvasObject { return e.root }

// setText replaces the editor contents (used by the browser to stage a SELECT).
func (e *editorPane) setText(s string) {
	e.entry.SetText(s)
	e.app.win.Canvas().Focus(e.entry)
}

// run applies the safety guard, then executes. Block reports and runs nothing;
// Confirm wraps the run in a yes/no dialog; Allow runs immediately. This is the
// GUI mirror of the TUI's guardedSQL — the decision is the core's.
func (e *editorPane) run() {
	sql := strings.TrimSpace(e.entry.Text)
	if sql == "" {
		return
	}
	if !e.app.core.Connected() {
		dialog.ShowInformation("Run", "Not connected. Use Connection ▸ Connect first.", e.app.win)
		return
	}
	switch act, _, reason := e.app.core.GuardStatement(sql); act {
	case safety.Block:
		dialog.ShowInformation("Blocked", reason, e.app.win)
	case safety.Confirm:
		dialog.ShowConfirm("Confirm", reason+"\n\nRun it anyway?", func(ok bool) {
			if ok {
				e.execSQL(sql)
			}
		}, e.app.win)
	default:
		e.execSQL(sql)
	}
}

// execSQL runs the statement off the UI thread. Row-returning statements stream
// into the grid; everything else reports rows affected — same split the TUI
// makes via isQuery.
func (e *editorPane) execSQL(sql string) {
	e.status.SetText("running…")
	go func() {
		ctx := e.app.ctx()
		if !isQuery(sql) {
			res, err := e.app.core.RunStatement(ctx, sql)
			if err != nil {
				onUI(func() { e.status.SetText("") })
				e.app.bgErr("run", err)
				return
			}
			msg := res.Message
			if msg == "" {
				msg = fmt.Sprintf("%d row(s) affected", res.RowsAffected)
			}
			onUI(func() {
				e.setModel(gridModel{})
				e.status.SetText(msg)
			})
			return
		}
		model, truncated, err := e.fetch(ctx, sql)
		if err != nil {
			onUI(func() { e.status.SetText("") })
			e.app.bgErr("query", err)
			return
		}
		status := fmt.Sprintf("%d row(s)", len(model.rows))
		if truncated {
			status = fmt.Sprintf("%d row(s) (capped at %d)", len(model.rows), gridFetchCap)
		}
		onUI(func() {
			e.setModel(model)
			e.status.SetText(status)
		})
	}()
}

// fetch materializes a query into a gridModel, capped at gridFetchCap rows.
func (e *editorPane) fetch(ctx context.Context, sql string) (gridModel, bool, error) {
	rs, err := e.app.core.RunQuery(ctx, sql)
	if err != nil {
		return gridModel{}, false, err
	}
	defer rs.Close()

	m := gridModel{cols: rs.Columns()}
	truncated := false
	for rs.Next() {
		if len(m.rows) >= gridFetchCap {
			truncated = true
			break
		}
		vals, err := rs.Values()
		if err != nil {
			return gridModel{}, false, err
		}
		m.rows = append(m.rows, toStrings(vals))
	}
	if err := rs.Err(); err != nil {
		return gridModel{}, false, err
	}
	m.widths = columnWidths(m)
	return m, truncated, nil
}

// setModel swaps the grid's data and re-applies column widths. Must run on the
// UI thread.
func (e *editorPane) setModel(m gridModel) {
	if m.widths == nil {
		m.widths = columnWidths(m)
	}
	e.model = m
	for i, w := range m.widths {
		e.grid.SetColumnWidth(i, w)
	}
	e.grid.Refresh()
	e.grid.ScrollToTop()
}

// columnWidths sizes each column to its widest cell (header included), clamped
// so one long text/JSON column can't dominate. Values are approximate pixel
// widths for a monospace-ish label.
func columnWidths(m gridModel) []float32 {
	const (
		perRune = 8.0
		pad     = 24.0
		min     = 60.0
		max     = 420.0
	)
	widths := make([]float32, len(m.cols))
	for c, name := range m.cols {
		w := len([]rune(name))
		for _, row := range m.rows {
			if c < len(row) {
				if n := len([]rune(row[c])); n > w {
					w = n
				}
			}
		}
		px := float32(w)*perRune + pad
		if px < min {
			px = min
		}
		if px > max {
			px = max
		}
		widths[c] = px
	}
	return widths
}

// isQuery decides whether a statement returns rows. Duplicated from the TUI's
// same-named helper (a lexical heuristic, not domain logic — §28 keeps *domain*
// logic single-sourced; this is a few keywords) so the GUI need not import the
// TUI package.
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
