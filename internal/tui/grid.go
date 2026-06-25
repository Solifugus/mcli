package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/table"
	"charm.land/lipgloss/v2"
)

// gridRowCap bounds how many rows a query fetches for the grid, so "open full
// result" stays memory-safe even against a huge table.
const gridRowCap = 10000

// maxGridColWidth caps a column's width so one wide column can't push the rest
// off-screen.
const maxGridColWidth = 60

// resultSet is a captured query result available to open in the grid. rows holds
// up to gridRowCap rows; truncated reports whether more existed beyond that.
type resultSet struct {
	cols      []string
	rows      [][]string
	truncated bool
}

// gridModel is the alt-screen result viewer: a paged, scrollable table.
type gridModel struct {
	table table.Model
	note  string
}

func newGrid(rs *resultSet, width, height int) gridModel {
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}

	cols := make([]table.Column, len(rs.cols))
	for i, name := range rs.cols {
		w := len(name)
		for _, r := range rs.rows {
			if i < len(r) && len(r[i]) > w {
				w = len(r[i])
			}
		}
		if w > maxGridColWidth {
			w = maxGridColWidth
		}
		cols[i] = table.Column{Title: name, Width: w}
	}
	trows := make([]table.Row, len(rs.rows))
	for i, r := range rs.rows {
		trows[i] = table.Row(r)
	}

	t := table.New(
		table.WithColumns(cols),
		table.WithRows(trows),
		table.WithFocused(true),
		table.WithHeight(gridTableHeight(height)),
	)
	t.SetWidth(width)

	note := fmt.Sprintf("%d row%s", len(rs.rows), plural(len(rs.rows)))
	if rs.truncated {
		note = fmt.Sprintf("first %d rows (more exist; refine with LIMIT)", len(rs.rows))
	}
	return gridModel{table: t, note: note}
}

// gridTableHeight reserves a line for the footer.
func gridTableHeight(termHeight int) int {
	h := termHeight - 2
	if h < 1 {
		h = 1
	}
	return h
}

func (g gridModel) resize(width, height int) gridModel {
	g.table.SetWidth(width)
	g.table.SetHeight(gridTableHeight(height))
	return g
}

func (g gridModel) Update(msg tea.Msg) (gridModel, tea.Cmd) {
	var cmd tea.Cmd
	g.table, cmd = g.table.Update(msg)
	return g, cmd
}

var gridFooterStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

func (g gridModel) View() string {
	footer := gridFooterStyle.Render(g.note + " — ↑/↓ PgUp/PgDn to scroll, Esc/q to return")
	return g.table.View() + "\n" + footer
}
