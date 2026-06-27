package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Solifugus/mcli/internal/core"
	"github.com/Solifugus/mcli/internal/core/adapter"
	"github.com/Solifugus/mcli/internal/core/config"
)

func ed(content string) editorModel {
	return newEditor("q", content, 80, 24, adapter.DialectPostgres, true)
}

func TestEditorInsertString(t *testing.T) {
	e := ed("")
	e.insertString("hello")
	if e.text() != "hello" {
		t.Errorf("text = %q, want hello", e.text())
	}
	if e.col != 5 {
		t.Errorf("col = %d, want 5", e.col)
	}
	if !e.dirty {
		t.Error("buffer should be dirty after typing")
	}
}

func TestEditorNewlineThenBackspaceJoins(t *testing.T) {
	e := ed("ab")
	e.moveEnd(false)
	e.insertNewline()
	if len(e.lines) != 2 || e.row != 1 || e.col != 0 {
		t.Fatalf("after newline: lines=%q row=%d col=%d", e.lines, e.row, e.col)
	}
	e.backspace()
	if e.text() != "ab" || len(e.lines) != 1 {
		t.Errorf("after backspace-join: %q (%d lines)", e.text(), len(e.lines))
	}
}

func TestEditorSplitMidLine(t *testing.T) {
	e := ed("abcd")
	e.moveHome(false)
	e.moveRight(false)
	e.moveRight(false) // col 2
	e.insertNewline()
	if got := e.text(); got != "ab\ncd" {
		t.Errorf("split = %q, want \"ab\\ncd\"", got)
	}
}

func TestEditorOverwrite(t *testing.T) {
	e := ed("abc")
	e.moveHome(false)
	e.toggleOverwrite()
	e.insertString("X")
	e.insertString("Y")
	if e.text() != "XYc" {
		t.Errorf("overwrite = %q, want XYc", e.text())
	}
}

func TestEditorMovementBounds(t *testing.T) {
	e := ed("hi\nthere")
	// Hammer the edges; must not panic or escape the buffer.
	e.moveUp(false)
	e.moveLeft(false)
	e.moveHome(false)
	for i := 0; i < 20; i++ {
		e.moveDown(false)
		e.moveRight(false)
	}
	if e.row < 0 || e.row >= len(e.lines) {
		t.Errorf("row %d out of range", e.row)
	}
	if e.col < 0 || e.col > len([]rune(e.lines[e.row])) {
		t.Errorf("col %d out of range on %q", e.col, e.lines[e.row])
	}
}

func TestEditorSelectionCopyText(t *testing.T) {
	e := ed("select 1")
	e.moveHome(false)
	for i := 0; i < 6; i++ {
		e.moveRight(true) // select "select"
	}
	if !e.hasSelection() {
		t.Fatal("expected an active selection")
	}
	if got := e.selectionText(); got != "select" {
		t.Errorf("selectionText = %q, want select", got)
	}
}

func TestEditorStatementAtCursor(t *testing.T) {
	e := ed("select 1;\nupdate t set x=1;")
	e.row, e.col = 0, 3
	if sql, ok := e.statementAtCursor(); !ok || sql != "select 1" {
		t.Errorf("stmt 1 = (%q,%v), want select 1", sql, ok)
	}
	e.row, e.col, e.sel = 1, 3, nil
	if sql, ok := e.statementAtCursor(); !ok || sql != "update t set x=1" {
		t.Errorf("stmt 2 = (%q,%v), want update t set x=1", sql, ok)
	}
}

func TestEditorSelectionOverridesStatement(t *testing.T) {
	e := ed("select 1; select 2")
	e.moveHome(false)
	for i := 0; i < 8; i++ {
		e.moveRight(true) // select "select 1"
	}
	if sql, ok := e.statementAtCursor(); !ok || sql != "select 1" {
		t.Errorf("selected run = (%q,%v), want select 1", sql, ok)
	}
}

func TestEditorViewSmoke(t *testing.T) {
	e := ed("select 1")
	out := e.View(false)
	for _, want := range []string{"builtin", "INS", " 1 ", "^R run"} {
		if !strings.Contains(out, want) {
			t.Errorf("View output missing %q", want)
		}
	}
	e.toggleOverwrite()
	if !strings.Contains(e.View(false), "OVR") {
		t.Error("overwrite should show OVR in the title")
	}
}

// --- integration through the root model ---

func newBuiltinModel(t *testing.T) *Model {
	t.Helper()
	dir := t.TempDir()
	st := config.NewStore(dir)
	s := config.DefaultSettings()
	s.Editor = "builtin"
	if err := st.EnsureRoot(); err != nil {
		t.Fatalf("EnsureRoot: %v", err)
	}
	if err := st.SaveSettings(s); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	c, err := core.Open(dir)
	if err != nil {
		t.Fatalf("core.Open: %v", err)
	}
	m := New(c)
	m.width, m.height = 80, 24
	return &m
}

func TestEditRoutesToBuiltin(t *testing.T) {
	m := newBuiltinModel(t)
	_, act := m.handleLine(`\edit demo`)
	if act.editor == nil {
		t.Fatal(`\edit with editor=builtin should return an editor action`)
	}
	if act.editor.name != "demo" {
		t.Errorf("editor name = %q, want demo", act.editor.name)
	}
}

func feedKey(t *testing.T, m *Model, k tea.KeyPressMsg) {
	t.Helper()
	mm, _ := m.handleEditorKey(k)
	*m = mm.(Model)
}

func TestEditorCtrlSWritesFile(t *testing.T) {
	m := newBuiltinModel(t)
	_, act := m.handleLine(`\edit demo`)
	m.mode = modeEditor
	m.editor = *act.editor

	for _, r := range "select 42" {
		feedKey(t, m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if !m.editor.dirty {
		t.Error("editor should be dirty before saving")
	}
	feedKey(t, m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if m.editor.dirty {
		t.Error("editor should be clean after Ctrl-S")
	}
	got, err := m.core.ReadSQLFile("demo")
	if err != nil {
		t.Fatalf("ReadSQLFile: %v", err)
	}
	if strings.TrimSpace(got) != "select 42" {
		t.Errorf("saved file = %q, want select 42", got)
	}
}

func TestEditorRunWithoutConnection(t *testing.T) {
	m := newBuiltinModel(t)
	_, act := m.handleLine(`\edit demo`)
	m.mode = modeEditor
	m.editor = *act.editor
	for _, r := range "select 1" {
		feedKey(t, m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	feedKey(t, m, tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if !strings.Contains(m.editor.status, "not connected") {
		t.Errorf("status = %q, want a not-connected message", m.editor.status)
	}
}

func TestEditorEscClosesCleanBuffer(t *testing.T) {
	m := newBuiltinModel(t)
	_, act := m.handleLine(`\edit demo`)
	m.mode = modeEditor
	m.editor = *act.editor // freshly opened, not dirty
	feedKey(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.mode != modeREPL {
		t.Errorf("Esc on a clean buffer should return to REPL, mode = %d", m.mode)
	}
}
