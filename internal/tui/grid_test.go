package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func sampleResult() *resultSet {
	return &resultSet{
		cols: []string{"id", "name"},
		rows: [][]string{{"1", "alice"}, {"2", "bob"}, {"3", "carol"}},
	}
}

func TestNewGridBuildsTable(t *testing.T) {
	g := newGrid(sampleResult(), 80, 24)
	view := g.View()
	for _, want := range []string{"id", "name", "alice", "3 rows"} {
		if !strings.Contains(view, want) {
			t.Errorf("grid view missing %q:\n%s", want, view)
		}
	}
}

func TestNewGridTruncatedNote(t *testing.T) {
	rs := sampleResult()
	rs.truncated = true
	if note := newGrid(rs, 80, 24).note; !strings.Contains(note, "more exist") {
		t.Errorf("truncated note = %q", note)
	}
}

func TestGridOpenViaCommand(t *testing.T) {
	m := newTestModel(t)
	m.width, m.height = 80, 24

	// No result yet: .grid reports nothing and stays in REPL mode.
	updated, _ := m.submitValue(`.grid`)
	if updated.mode != modeREPL {
		t.Fatal(".grid with no result should stay in REPL mode")
	}

	// With a result captured, .grid enters the grid mode.
	m.lastResult = sampleResult()
	updated, _ = m.submitValue(`.grid`)
	if updated.mode != modeGrid {
		t.Fatalf(".grid should enter grid mode; mode=%v", updated.mode)
	}

	// View renders the grid in alt-screen.
	if v := updated.View(); !v.AltScreen {
		t.Error("grid mode should set AltScreen")
	}

	// Esc returns to the REPL.
	back, _ := updated.handleGridKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if back.(Model).mode != modeREPL {
		t.Error("Esc should return to REPL mode")
	}
}

func TestQuitKeyExitsGrid(t *testing.T) {
	m := newTestModel(t)
	m.lastResult = sampleResult()
	updated, _ := m.submitValue(`.grid`)
	back, _ := updated.handleGridKey(tea.KeyPressMsg{Code: 'q'})
	if back.(Model).mode != modeREPL {
		t.Error("q should return to REPL mode")
	}
}

func TestMultiLinePasteOpensEditor(t *testing.T) {
	m := newTestModel(t)
	updated, cmd := m.handlePaste(tea.PasteMsg{Content: "select 1\nfrom t\nwhere x=1"})
	_ = updated
	if cmd == nil {
		t.Fatal("multi-line paste should produce a command (editor handoff)")
	}
	// The pasted content is parked in the scratch file.
	content, err := m.core.ReadSQLFile(pasteScratchFile)
	if err != nil || !strings.Contains(content, "from t") {
		t.Errorf("scratch content = (%q,%v)", content, err)
	}
}

func TestSingleLinePasteDoesNotOpenEditor(t *testing.T) {
	m := newTestModel(t)
	_, _ = m.handlePaste(tea.PasteMsg{Content: "select 1"})
	// A single-line paste must not create the scratch file.
	if _, err := m.core.ReadSQLFile(pasteScratchFile); err == nil {
		t.Error("single-line paste should not write the scratch file")
	}
}

// submitValue is a test helper: set the input to line and submit, returning the
// concrete Model.
func (m *Model) submitValue(line string) (Model, tea.Cmd) {
	m.input.SetValue(line)
	updated, cmd := m.submit()
	return updated.(Model), cmd
}
