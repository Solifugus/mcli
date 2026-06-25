// Package tui is mcli's Bubble Tea v2 front-end. The root Model is a small mode
// state machine (currently only repl; grid arrives in Phase 4) layered over the
// UI-agnostic core. The REPL is inline: committed output is emitted with
// tea.Println/Printf so it scrolls naturally above the live prompt line, and
// View() renders only that live line. See docs/mcli-design.md §11.
package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/textinput"

	"github.com/Solifugus/mcli/internal/core"
)

type mode int

const (
	modeREPL mode = iota
	modeGrid // reserved for Phase 4
)

// Model is the root Bubble Tea model.
type Model struct {
	core *core.Core
	mode mode

	input    textinput.Model
	width    int
	quitting bool
}

// New builds the root model around an opened core.
func New(c *core.Core) Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.SetVirtualCursor(true)
	return Model{core: c, mode: modeREPL, input: ti}
}

// Run launches the interactive program.
func Run(c *core.Core) error {
	p := tea.NewProgram(New(c))
	_, err := p.Run()
	return err
}

// Init focuses the input and prints the welcome banner above the prompt.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.input.Focus(),
		tea.Println("mcli — type \\help for commands, \\quit to exit"),
	)
}

// Update routes messages. Today everything is REPL; the switch on mode is the
// seam where grid handling will be added.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.input.SetWidth(max(1, msg.Width-len(m.promptString())-1))
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Ctrl-C / Ctrl-D quit the app. (Ctrl-C will instead cancel a running query
	// once query execution lands in Phase 3.)
	if msg.Mod == tea.ModCtrl && (msg.Code == 'c' || msg.Code == 'd') {
		m.quitting = true
		return m, tea.Quit
	}
	if msg.Code == tea.KeyEnter {
		return m.submit()
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// submit commits the typed line to scrollback, clears the input, and dispatches.
func (m Model) submit() (tea.Model, tea.Cmd) {
	raw := m.input.Value()
	m.input.Reset()

	cmds := []tea.Cmd{tea.Printf("%s%s", m.promptString(), raw)}

	line := strings.TrimSpace(raw)
	if line != "" {
		res := m.dispatch(line)
		for _, l := range res.lines {
			cmds = append(cmds, tea.Println(l))
		}
		if res.quit {
			m.quitting = true
			cmds = append(cmds, tea.Quit)
		}
	}
	return m, tea.Sequence(cmds...)
}

// View renders the live prompt line only; committed output lives in scrollback.
func (m Model) View() tea.View {
	if m.quitting {
		return tea.NewView("")
	}
	v := tea.NewView(m.promptString() + m.input.View())
	v.AltScreen = m.mode == modeGrid
	return v
}

// promptString builds the context prompt, e.g. "consumer-lending:etl:ETLDB> ".
// Server and database segments appear only once a connection sets them.
func (m Model) promptString() string {
	ws := m.core.Current()
	p := ws.Name
	if ws.CurrentServer != "" {
		p += ":" + ws.CurrentServer
		if ws.CurrentDatabase != "" {
			p += ":" + ws.CurrentDatabase
		}
	}
	return p + "> "
}
