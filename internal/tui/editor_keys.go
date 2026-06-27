package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/Solifugus/mcli/internal/core/safety"
)

// handleEditorKey routes keys while the built-in editor is open. Control keys
// drive run/save/copy/quit (which need the core); everything else is delegated
// to the editorModel (movement and editing). Shift extends the selection.
func (m Model) handleEditorKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Ctrl-C cancels a running statement; with nothing running it quits the editor.
	if msg.Mod&tea.ModCtrl != 0 && msg.Code == 'c' {
		if m.running {
			if m.cancel != nil {
				m.cancel()
			}
			return m, nil
		}
		return m.editorQuit()
	}
	if m.running {
		return m, nil // ignore input while a statement runs (Ctrl-C handled above)
	}

	if msg.Mod&tea.ModCtrl != 0 {
		switch msg.Code {
		case 's':
			return m.editorSave()
		case 'r':
			return m.editorRunStatement()
		case 'y':
			return m.editorCopy()
		}
	}

	ext := msg.Mod&tea.ModShift != 0
	switch msg.Code {
	case tea.KeyEscape:
		return m.editorQuit()
	case tea.KeyInsert:
		m.editor.toggleOverwrite()
	case tea.KeyEnter:
		m.editor.insertNewline()
	case tea.KeyTab:
		m.editor.insertTab()
	case tea.KeyBackspace:
		m.editor.backspace()
	case tea.KeyDelete:
		m.editor.deleteForward()
	case tea.KeyLeft:
		m.editor.moveLeft(ext)
	case tea.KeyRight:
		m.editor.moveRight(ext)
	case tea.KeyUp:
		m.editor.moveUp(ext)
	case tea.KeyDown:
		m.editor.moveDown(ext)
	case tea.KeyHome:
		m.editor.moveHome(ext)
	case tea.KeyEnd:
		m.editor.moveEnd(ext)
	case tea.KeyPgUp:
		m.editor.movePage(-1, ext)
	case tea.KeyPgDown:
		m.editor.movePage(1, ext)
	default:
		if msg.Text != "" { // printable input
			m.editor.insertString(msg.Text)
		}
	}
	return m, nil
}

// editorSave writes the buffer to the workspace file.
func (m Model) editorSave() (tea.Model, tea.Cmd) {
	if err := m.core.WriteSQLFile(m.editor.name, m.editor.text()); err != nil {
		m.editor.status = "save failed: " + err.Error()
		return m, nil
	}
	m.editor.dirty = false
	m.editor.status = "saved " + m.editor.name
	return m, nil
}

// editorCopy copies the selection (or the current line when nothing is selected)
// to the system clipboard over OSC 52, so it works even over SSH.
func (m Model) editorCopy() (tea.Model, tea.Cmd) {
	text := m.editor.selectionText()
	if text == "" {
		text = m.editor.lines[m.editor.row]
	}
	if text == "" {
		m.editor.status = "nothing to copy"
		return m, nil
	}
	m.editor.status = "copied"
	return m, tea.SetClipboard(text)
}

// editorRunStatement runs the statement under the cursor (or the selection)
// against the live connection, through the same safety guard the REPL uses. The
// result returns to the editor (or the grid for a row result) via editorRun.
func (m Model) editorRunStatement() (tea.Model, tea.Cmd) {
	sql, ok := m.editor.statementAtCursor()
	if !ok {
		m.editor.status = "no statement under cursor"
		return m, nil
	}
	if !m.core.Connected() {
		m.editor.status = `not connected — use \connect first`
		return m, nil
	}
	m.lastSQL = sql
	runner := m.sqlRunner(sql)
	switch act, _, reason := m.core.GuardStatement(sql); act {
	case safety.Block:
		m.editor.status = "blocked: " + reason
		return m, nil
	case safety.Confirm:
		m.startPrompt(pending{
			label: reason + " — proceed? [y/N] ",
			resume: func(m *Model, text string, canceled bool) tea.Cmd {
				if canceled || !isYes(text) {
					m.mode = modeEditor
					m.editor.status = "canceled"
					return nil
				}
				m.editorRun = true
				return m.launchAsync(runner)
			},
		})
		return m, nil
	default:
		m.editorRun = true
		m.editor.status = ""
		return m, m.launchAsync(runner)
	}
}

// editorQuit leaves the editor, prompting to save first when there are unsaved
// changes (y save, n discard, Esc stay).
func (m Model) editorQuit() (tea.Model, tea.Cmd) {
	if !m.editor.dirty {
		name := m.editor.name
		m.mode = modeREPL
		return m, tea.Println("closed " + name)
	}
	name := m.editor.name
	content := m.editor.text()
	m.startPrompt(pending{
		label: "save changes to " + name + " before closing? [y/N, Esc cancels] ",
		resume: func(m *Model, text string, canceled bool) tea.Cmd {
			if canceled {
				m.mode = modeEditor // Esc: stay in the editor
				return nil
			}
			if isYes(text) {
				if err := m.core.WriteSQLFile(name, content); err != nil {
					m.mode = modeEditor
					m.editor.status = "save failed: " + err.Error()
					return nil
				}
				m.mode = modeREPL
				return tea.Println("saved & closed " + name)
			}
			m.mode = modeREPL
			return tea.Println("closed " + name + " (changes discarded)")
		},
	})
	return m, nil
}
