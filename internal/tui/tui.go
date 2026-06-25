// Package tui is mcli's Bubble Tea v2 front-end. The root Model is a small mode
// state machine (currently only repl; grid arrives in Phase 4) layered over the
// UI-agnostic core. The REPL is inline: committed output is emitted with
// tea.Println/Printf so it scrolls naturally above the live prompt line, and
// View() renders only that live line. See docs/mcli-design.md §11.
package tui

import (
	"context"
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/textinput"

	"github.com/Solifugus/mcli/internal/core"
	"github.com/Solifugus/mcli/internal/core/adapter"
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

	// Snapshots of Core state read at safe points on the UI thread. View renders
	// from these rather than reading Core, so the render path never races the
	// background goroutine that mutates connection state.
	prompt    string          // context prompt text
	promptEnv string          // environment label, drives prompt color (§18)
	dialect   adapter.Dialect // active SQL dialect, drives syntax highlighting

	// Color preferences from settings.
	colorPrompt bool

	// Background command state. While running, new submissions are refused and
	// Ctrl-C cancels via cancel() instead of quitting.
	running bool
	cancel  context.CancelFunc

	// In-memory command history ring (distinct from the persistent action log).
	// histIdx walks history; histIdx == len(history) means "the live draft line".
	history []string
	histIdx int
	draft   string
}

// New builds the root model around an opened core.
func New(c *core.Core) Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.SetVirtualCursor(true)
	m := Model{core: c, mode: modeREPL, input: ti, colorPrompt: c.Settings().ColorPrompt}
	m.refreshPrompt()
	return m
}

// refreshPrompt snapshots prompt-related Core state. Call only on the UI thread
// (New, after a sync command, on an async result) — never concurrently with a
// running background command.
func (m *Model) refreshPrompt() {
	m.prompt = m.promptString()
	m.promptEnv = m.core.Environment()
	m.dialect = m.core.Dialect()
}

// styledPrompt colors the prompt by environment when color is enabled.
func (m Model) styledPrompt() string {
	if !m.colorPrompt {
		return m.prompt
	}
	return promptStyleFor(m.promptEnv).Render(m.prompt)
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
		m.input.SetWidth(max(1, msg.Width-len(m.prompt)-1))
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case asyncResultMsg:
		return m.handleAsyncResult(msg)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// handleAsyncResult clears the running state and commits the command's output.
func (m Model) handleAsyncResult(msg asyncResultMsg) (tea.Model, tea.Cmd) {
	m.running = false
	m.cancel = nil
	m.refreshPrompt() // connection/database may have changed

	var cmds []tea.Cmd
	if msg.err != nil {
		if errors.Is(msg.err, context.Canceled) {
			cmds = append(cmds, tea.Println("canceled"))
		} else {
			cmds = append(cmds, tea.Println("error: "+msg.err.Error()))
		}
	}
	for _, l := range msg.lines {
		cmds = append(cmds, tea.Println(l))
	}
	return m, tea.Batch(cmds...)
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.Mod == tea.ModCtrl && (msg.Code == 'c' || msg.Code == 'd') {
		// Ctrl-C cancels a running command rather than quitting; Ctrl-D always
		// quits. With nothing running, Ctrl-C quits too.
		if m.running && msg.Code == 'c' {
			if m.cancel != nil {
				m.cancel()
			}
			return m, nil
		}
		m.quitting = true
		return m, tea.Quit
	}
	switch msg.Code {
	case tea.KeyEnter:
		return m.submit()
	case tea.KeyUp:
		m.historyPrev()
		return m, nil
	case tea.KeyDown:
		m.historyNext()
		return m, nil
	case tea.KeyTab:
		newLine, candidates := m.complete(m.input.Value())
		m.input.SetValue(newLine)
		m.input.CursorEnd()
		if len(candidates) > 0 {
			return m, tea.Println(strings.Join(candidates, "   "))
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// historyPrev walks one step back into the command history, stashing the live
// draft the first time so it can be restored on the way back down.
func (m *Model) historyPrev() {
	if m.histIdx == len(m.history) {
		m.draft = m.input.Value()
	}
	if m.histIdx > 0 {
		m.histIdx--
		m.input.SetValue(m.history[m.histIdx])
		m.input.CursorEnd()
	}
}

// historyNext walks one step forward, restoring the draft past the newest entry.
func (m *Model) historyNext() {
	if m.histIdx >= len(m.history) {
		return
	}
	m.histIdx++
	if m.histIdx == len(m.history) {
		m.input.SetValue(m.draft)
	} else {
		m.input.SetValue(m.history[m.histIdx])
	}
	m.input.CursorEnd()
}

// addHistory appends a submitted line, skipping consecutive duplicates, and
// resets the ring cursor to the live draft position.
func (m *Model) addHistory(line string) {
	if n := len(m.history); n == 0 || m.history[n-1] != line {
		m.history = append(m.history, line)
	}
	m.histIdx = len(m.history)
	m.draft = ""
}

// submit commits the typed line to scrollback, clears the input, and either
// runs a synchronous command inline or launches a background command.
func (m Model) submit() (tea.Model, tea.Cmd) {
	raw := m.input.Value()
	m.input.Reset()

	cmds := []tea.Cmd{tea.Printf("%s%s", m.prompt, raw)}

	line := strings.TrimSpace(raw)
	if line == "" {
		return m, tea.Sequence(cmds...)
	}
	if m.running {
		cmds = append(cmds, tea.Println("busy — a command is running (Ctrl-C to cancel)"))
		return m, tea.Sequence(cmds...)
	}

	m.addHistory(line)
	res, run := m.handleLine(line)
	for _, l := range res.lines {
		cmds = append(cmds, tea.Println(l))
	}
	if res.quit {
		m.quitting = true
		cmds = append(cmds, tea.Quit)
		return m, tea.Sequence(cmds...)
	}

	if run != nil {
		ctx, cancel := context.WithCancel(context.Background())
		m.running = true
		m.cancel = cancel
		cmds = append(cmds, func() tea.Msg { return run(ctx) })
	} else {
		// A sync command may have changed the workspace; refresh the snapshot.
		m.refreshPrompt()
	}
	return m, tea.Sequence(cmds...)
}

// View renders the live line only; committed output lives in scrollback. While a
// background command runs, the input is replaced by a status indicator.
func (m Model) View() tea.View {
	if m.quitting {
		return tea.NewView("")
	}
	content := m.styledPrompt() + renderInput(m.input.Value(), m.input.Position(), m.dialect, m.colorPrompt)
	if m.running {
		content = "running… (Ctrl-C to cancel)"
	}
	v := tea.NewView(content)
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
