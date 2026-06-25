package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestSubmitPathCreatesWorkspace drives the real input→submit→dispatch path
// (what interactive typing triggers) rather than calling dispatch directly.
func TestSubmitPathCreatesWorkspace(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue(`\workspace create lending`)

	updated, _ := m.submit()
	rm := updated.(Model)

	// Input is cleared after submit.
	if rm.input.Value() != "" {
		t.Errorf("input not reset after submit: %q", rm.input.Value())
	}
	// Core state changed: the workspace now exists.
	names, err := rm.core.ListWorkspaces()
	if err != nil {
		t.Fatalf("ListWorkspaces: %v", err)
	}
	if !contains(names, "lending") {
		t.Errorf("workspace not created; have %v", names)
	}
}

// TestEnterKeyTriggersSubmit confirms handleKey routes Enter to submit.
func TestEnterKeyTriggersSubmit(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue(`\workspace create viakey`)

	updated, _ := m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	rm := updated.(Model)

	names, _ := rm.core.ListWorkspaces()
	if !contains(names, "viakey") {
		t.Errorf("Enter key did not submit; have %v", names)
	}
}

// TestCtrlCQuits confirms Ctrl-C sets quitting and returns a command.
func TestCtrlCQuits(t *testing.T) {
	m := newTestModel(t)
	updated, cmd := m.handleKey(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if !updated.(Model).quitting {
		t.Error("Ctrl-C did not set quitting")
	}
	if cmd == nil {
		t.Error("Ctrl-C returned nil command, want tea.Quit")
	}
}

// TestTypingInsertsText confirms printable keys reach the input buffer.
func TestTypingInsertsText(t *testing.T) {
	m := newTestModel(t)
	m.input.Focus() // Init() does this in real use
	var mdl tea.Model = *m
	for _, r := range "abc" {
		mdl, _ = mdl.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if got := mdl.(Model).input.Value(); got != "abc" {
		t.Errorf("input = %q, want abc", got)
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func TestTabKeyCompletes(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue(`\wo`)
	updated, _ := m.handleKey(tea.KeyPressMsg{Code: tea.KeyTab})
	if got := updated.(Model).input.Value(); got != `\workspace ` {
		t.Errorf("Tab completion = %q, want %q", got, `\workspace `)
	}
}

func TestUpKeyRecallsHistory(t *testing.T) {
	m := newTestModel(t)
	submitLine(m, `\help`)
	updated, _ := m.handleKey(tea.KeyPressMsg{Code: tea.KeyUp})
	if got := updated.(Model).input.Value(); got != `\help` {
		t.Errorf("Up recall = %q, want \\help", got)
	}
}

func TestViewRendersPromptLine(t *testing.T) {
	m := newTestModel(t)
	m.colorPrompt = false // plain output for a stable assertion
	v := m.View()
	if !strings.HasPrefix(v.Content, "default> ") {
		t.Errorf("view content = %q, want prefix %q", v.Content, "default> ")
	}
	if v.AltScreen {
		t.Error("repl mode should not use alt screen")
	}
}
