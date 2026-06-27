package tui

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"

	"github.com/Solifugus/mcli/internal/core/adapter"
	"github.com/Solifugus/mcli/internal/core/safety"
)

// editorModel is the built-in SQL editor surface (Phase 10): an alt-screen,
// line-buffer editor with syntax highlighting, insert/overwrite, a keyboard
// selection model, and — its reason to exist — running the statement under the
// cursor against the live connection. It is decoupled from Bubble Tea: the root
// model translates keys into the methods below and owns run/save (which need the
// core). Mirrors the gridModel value-field pattern.
type editorModel struct {
	name    string // workspace SQL file name
	lines   []string
	row     int // cursor line
	col     int // cursor column, as a rune index within lines[row]
	top     int // first visible line (vertical scroll)
	left    int // first visible column (horizontal scroll)
	width   int
	height  int
	dialect adapter.Dialect
	hilite  bool
	over    bool     // overwrite (true) vs insert (false)
	sel     *selSpan // selection anchor; nil when no selection
	status  string   // transient status line
	dirty   bool
}

// selSpan is the fixed end of a selection (the anchor); the cursor is the moving
// end. Together they bound the selected text.
type selSpan struct{ row, col int }

// newEditor builds an editor over content for the named file.
func newEditor(name, content string, width, height int, dialect adapter.Dialect, hilite bool) editorModel {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		lines = []string{""}
	}
	return editorModel{
		name: name, lines: lines, width: width, height: height,
		dialect: dialect, hilite: hilite,
	}
}

func (e *editorModel) resize(w, h int) { e.width, e.height = w, h; e.ensureVisible() }

// --- geometry ---

func (e *editorModel) gutterWidth() int {
	return len(strconv.Itoa(len(e.lines))) + 2 // one space each side of the number
}
func (e *editorModel) bodyHeight() int {
	if h := e.height - 3; h > 0 { // title + status + footer
		return h
	}
	return 1
}
func (e *editorModel) textWidth() int {
	if w := e.width - e.gutterWidth(); w > 0 {
		return w
	}
	return 1
}

func (e *editorModel) curRunes() []rune { return []rune(e.lines[e.row]) }

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ensureVisible scrolls so the cursor stays on screen.
func (e *editorModel) ensureVisible() {
	rows := e.bodyHeight()
	if e.row < e.top {
		e.top = e.row
	}
	if e.row >= e.top+rows {
		e.top = e.row - rows + 1
	}
	if e.top < 0 {
		e.top = 0
	}
	tw := e.textWidth()
	if e.col < e.left {
		e.left = e.col
	}
	if e.col >= e.left+tw {
		e.left = e.col - tw + 1
	}
	if e.left < 0 {
		e.left = 0
	}
}

// --- selection ---

func (e *editorModel) hasSelection() bool {
	return e.sel != nil && !(e.sel.row == e.row && e.sel.col == e.col)
}

// orderedSel returns the selection bounds with start <= end.
func (e *editorModel) orderedSel() (sr, sc, er, ec int) {
	a, b := *e.sel, selSpan{e.row, e.col}
	if a.row > b.row || (a.row == b.row && a.col > b.col) {
		a, b = b, a
	}
	return a.row, a.col, b.row, b.col
}

func (e *editorModel) selectionText() string {
	if !e.hasSelection() {
		return ""
	}
	sr, sc, er, ec := e.orderedSel()
	if sr == er {
		return string([]rune(e.lines[sr])[sc:ec])
	}
	parts := []string{string([]rune(e.lines[sr])[sc:])}
	for r := sr + 1; r < er; r++ {
		parts = append(parts, e.lines[r])
	}
	parts = append(parts, string([]rune(e.lines[er])[:ec]))
	return strings.Join(parts, "\n")
}

func (e *editorModel) deleteSelection() {
	sr, sc, er, ec := e.orderedSel()
	srR, erR := []rune(e.lines[sr]), []rune(e.lines[er])
	merged := string(srR[:sc]) + string(erR[ec:])
	tail := append([]string{merged}, e.lines[er+1:]...)
	e.lines = append(e.lines[:sr], tail...)
	e.row, e.col, e.sel, e.dirty = sr, sc, nil, true
}

// preMove sets or clears the selection anchor before a cursor move.
func (e *editorModel) preMove(extend bool) {
	if extend {
		if e.sel == nil {
			e.sel = &selSpan{e.row, e.col}
		}
		return
	}
	e.sel = nil
}

// --- movement ---

func (e *editorModel) moveLeft(extend bool) {
	e.preMove(extend)
	if e.col > 0 {
		e.col--
	} else if e.row > 0 {
		e.row--
		e.col = len(e.curRunes())
	}
	e.ensureVisible()
}
func (e *editorModel) moveRight(extend bool) {
	e.preMove(extend)
	if e.col < len(e.curRunes()) {
		e.col++
	} else if e.row < len(e.lines)-1 {
		e.row++
		e.col = 0
	}
	e.ensureVisible()
}
func (e *editorModel) moveUp(extend bool) {
	e.preMove(extend)
	if e.row > 0 {
		e.row--
		e.col = clamp(e.col, 0, len(e.curRunes()))
	}
	e.ensureVisible()
}
func (e *editorModel) moveDown(extend bool) {
	e.preMove(extend)
	if e.row < len(e.lines)-1 {
		e.row++
		e.col = clamp(e.col, 0, len(e.curRunes()))
	}
	e.ensureVisible()
}
func (e *editorModel) moveHome(extend bool) { e.preMove(extend); e.col = 0; e.ensureVisible() }
func (e *editorModel) moveEnd(extend bool) {
	e.preMove(extend)
	e.col = len(e.curRunes())
	e.ensureVisible()
}
func (e *editorModel) movePage(dir int, extend bool) {
	e.preMove(extend)
	e.row = clamp(e.row+dir*e.bodyHeight(), 0, len(e.lines)-1)
	e.col = clamp(e.col, 0, len(e.curRunes()))
	e.ensureVisible()
}

// --- editing ---

func (e *editorModel) insertString(s string) {
	if e.hasSelection() {
		e.deleteSelection()
	}
	e.dirty = true
	runes := e.curRunes()
	before, after := string(runes[:e.col]), string(runes[e.col:])
	parts := strings.Split(s, "\n")
	if len(parts) == 1 {
		if e.over && e.col < len(runes) && utf8.RuneCountInString(s) == 1 {
			runes[e.col] = []rune(s)[0]
			e.lines[e.row] = string(runes)
			e.col++
		} else {
			e.lines[e.row] = before + s + after
			e.col += utf8.RuneCountInString(s)
		}
		e.ensureVisible()
		return
	}
	// Multi-line insert (e.g. a bracketed paste).
	out := make([]string, 0, len(e.lines)+len(parts)-1)
	out = append(out, e.lines[:e.row]...)
	out = append(out, before+parts[0])
	out = append(out, parts[1:len(parts)-1]...)
	last := parts[len(parts)-1]
	out = append(out, last+after)
	out = append(out, e.lines[e.row+1:]...)
	e.lines = out
	e.row += len(parts) - 1
	e.col = utf8.RuneCountInString(last)
	e.ensureVisible()
}

func (e *editorModel) insertNewline() {
	if e.hasSelection() {
		e.deleteSelection()
	}
	e.dirty = true
	runes := e.curRunes()
	before, after := string(runes[:e.col]), string(runes[e.col:])
	e.lines[e.row] = before
	tail := append([]string{after}, e.lines[e.row+1:]...)
	e.lines = append(e.lines[:e.row+1], tail...)
	e.row++
	e.col = 0
	e.ensureVisible()
}

func (e *editorModel) backspace() {
	if e.hasSelection() {
		e.deleteSelection()
		e.ensureVisible()
		return
	}
	e.dirty = true
	if e.col > 0 {
		runes := e.curRunes()
		e.lines[e.row] = string(runes[:e.col-1]) + string(runes[e.col:])
		e.col--
	} else if e.row > 0 {
		prev := []rune(e.lines[e.row-1])
		e.col = len(prev)
		e.lines[e.row-1] = e.lines[e.row-1] + e.lines[e.row]
		e.lines = append(e.lines[:e.row], e.lines[e.row+1:]...)
		e.row--
	}
	e.ensureVisible()
}

func (e *editorModel) deleteForward() {
	if e.hasSelection() {
		e.deleteSelection()
		e.ensureVisible()
		return
	}
	e.dirty = true
	runes := e.curRunes()
	if e.col < len(runes) {
		e.lines[e.row] = string(runes[:e.col]) + string(runes[e.col+1:])
	} else if e.row < len(e.lines)-1 {
		e.lines[e.row] = e.lines[e.row] + e.lines[e.row+1]
		e.lines = append(e.lines[:e.row+1], e.lines[e.row+2:]...)
	}
	e.ensureVisible()
}

const editorTabWidth = 2

func (e *editorModel) insertTab() { e.insertString(strings.Repeat(" ", editorTabWidth)) }

func (e *editorModel) toggleOverwrite() { e.over = !e.over }

// --- statement under cursor ---

// text returns the whole buffer as one string.
func (e *editorModel) text() string { return strings.Join(e.lines, "\n") }

// byteOffset is the cursor's byte offset within text().
func (e *editorModel) byteOffset() int {
	off := 0
	for r := 0; r < e.row; r++ {
		off += len(e.lines[r]) + 1 // +1 for the joining newline
	}
	off += len(string(e.curRunes()[:e.col]))
	return off
}

// statementAtCursor returns the SQL to run: the active selection if any,
// otherwise the statement (semicolon-delimited, comment/string-aware) that the
// cursor sits in. ok is false when there is nothing runnable.
func (e *editorModel) statementAtCursor() (string, bool) {
	if e.hasSelection() {
		s := strings.TrimSpace(e.selectionText())
		return s, s != ""
	}
	full := e.text()
	sp, ok := safety.StatementAt(full, e.byteOffset())
	if !ok {
		return "", false
	}
	return full[sp.Start:sp.End], true
}

// --- rendering ---

var (
	editorTitleStyle  = lipgloss.NewStyle().Reverse(true)
	editorFooterStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	editorGutterStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	editorStatusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
)

func underlineCursor(s lipgloss.Style) lipgloss.Style { return s.Underline(true).Reverse(true) }

// View renders the editor full-screen (alt-screen). running shows a busy footer.
func (e *editorModel) View(running bool) string {
	var b strings.Builder

	mode := "INS"
	if e.over {
		mode = "OVR"
	}
	dirty := ""
	if e.dirty {
		dirty = " *"
	}
	title := fmt.Sprintf(" .edit %s%s   builtin · %s ", e.name, dirty, mode)
	b.WriteString(editorTitleStyle.Render(padCells(title, e.width)))
	b.WriteByte('\n')

	cursorStyle := reverseCursor // OVR: block
	if !e.over {
		cursorStyle = underlineCursor // INS: underline bar
	}
	gw := e.gutterWidth()
	tw := e.textWidth()
	rows := e.bodyHeight()
	digits := gw - 2
	for i := 0; i < rows; i++ {
		ln := e.top + i
		if ln >= len(e.lines) {
			b.WriteString("\n")
			continue
		}
		gutter := editorGutterStyle.Render(fmt.Sprintf(" %*d ", digits, ln+1))
		b.WriteString(gutter)
		b.WriteString(e.renderBodyLine(ln, tw, cursorStyle))
		b.WriteByte('\n')
	}

	status := e.status
	b.WriteString(editorStatusStyle.Render(padCells(truncate(status, e.width), e.width)))
	b.WriteByte('\n')

	footer := "^R run stmt   ^S save   ^Y copy   Ins ovr   Esc quit"
	if running {
		footer = "running… (Ctrl-C to cancel)"
	}
	b.WriteString(editorFooterStyle.Render(truncate(footer, e.width)))
	return b.String()
}

// renderBodyLine renders one buffer line within the horizontal window [left,
// left+tw), with the cursor (on the cursor line) and any selection overlaid.
func (e *editorModel) renderBodyLine(ln, tw int, cursorStyle func(lipgloss.Style) lipgloss.Style) string {
	runes := []rune(e.lines[ln])
	classes := lineClasses(e.lines[ln], e.dialect, e.hilite)

	lo := clamp(e.left, 0, len(runes))
	hi := clamp(lo+tw, 0, len(runes))
	wr, wc := runes[lo:hi], classes[lo:hi]

	cursor := -1
	if ln == e.row {
		if c := e.col - lo; c >= 0 && c <= len(wr) {
			cursor = c
		}
	}
	sel := e.lineSelection(ln, lo, hi)
	return renderLineSpans(wr, wc, cursor, cursorStyle, sel)
}

// lineSelection returns the selection range for line ln, expressed in window
// coordinates (after subtracting the horizontal offset lo), or nil if the line
// is outside the selection.
func (e *editorModel) lineSelection(ln, lo, hi int) *runeRange {
	if !e.hasSelection() {
		return nil
	}
	sr, sc, er, ec := e.orderedSel()
	if ln < sr || ln > er {
		return nil
	}
	start, end := 0, hi-lo // default: whole visible window (covers full line)
	if ln == sr {
		start = clamp(sc-lo, 0, hi-lo)
	}
	if ln == er {
		end = clamp(ec-lo, 0, hi-lo)
	}
	if start >= end {
		return nil
	}
	return &runeRange{start, end}
}

func padCells(s string, w int) string {
	if n := w - lipgloss.Width(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

func truncate(s string, w int) string {
	r := []rune(s)
	if len(r) <= w || w <= 0 {
		return s
	}
	return string(r[:w])
}
