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

	input         textinput.Model
	width, height int
	quitting      bool

	// Grid surface (alt-screen). lastResult is the most recent query result,
	// openable with \grid.
	grid       gridModel
	lastResult *resultSet

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
	// Focus here, not in Init: Init has a value receiver, so focusing there would
	// only focus a discarded copy, leaving the real textinput unable to type.
	m.input.Focus()
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
		m.width, m.height = msg.Width, msg.Height
		m.input.SetWidth(max(1, msg.Width-len(m.prompt)-1))
		if m.mode == modeGrid {
			m.grid = m.grid.resize(msg.Width, msg.Height)
		}
		return m, nil
	case tea.KeyPressMsg:
		if m.mode == modeGrid {
			return m.handleGridKey(msg)
		}
		return m.handleKey(msg)
	case tea.PasteMsg:
		if m.mode == modeREPL && !m.running {
			return m.handlePaste(msg)
		}
		return m, nil
	case asyncResultMsg:
		return m.handleAsyncResult(msg)
	case editDoneMsg:
		m.refreshPrompt()
		if msg.err != nil {
			return m, tea.Println("editor error: " + msg.err.Error())
		}
		return m, tea.Println("edited " + msg.name)
	}
	if m.mode == modeGrid {
		var cmd tea.Cmd
		m.grid, cmd = m.grid.Update(msg)
		return m, cmd
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// pasteScratchFile is where a multi-line paste is parked before editing.
const pasteScratchFile = "scratch"

// handlePaste routes bracketed paste (§11): a single-line paste lands in the
// input like typing, while a paste containing newlines is written to the
// scratch file and opened in the editor — so it does not fire as several
// partial executions under the Enter-executes rule.
func (m Model) handlePaste(msg tea.PasteMsg) (tea.Model, tea.Cmd) {
	if !strings.Contains(msg.Content, "\n") {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	if err := m.core.WriteSQLFile(pasteScratchFile, msg.Content); err != nil {
		return m, tea.Println("paste error: " + err.Error())
	}
	res, c := m.cmdEdit([]string{pasteScratchFile})
	var cmds []tea.Cmd
	cmds = append(cmds, tea.Printf("(multi-line paste opened in editor as %s.sql)", pasteScratchFile))
	if len(res.lines) > 0 {
		cmds = append(cmds, tea.Println(strings.Join(res.lines, "\n")))
	}
	if c != nil {
		cmds = append(cmds, c)
	}
	return m, tea.Sequence(cmds...)
}

// handleGridKey routes keys while the grid is open. Esc/q/Ctrl-C return to the
// REPL; everything else drives the table (scroll, paging).
func (m Model) handleGridKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.Code == tea.KeyEscape || msg.Code == 'q' ||
		(msg.Mod == tea.ModCtrl && msg.Code == 'c') {
		m.mode = modeREPL
		return m, nil
	}
	var cmd tea.Cmd
	m.grid, cmd = m.grid.Update(msg)
	return m, cmd
}

// handleAsyncResult clears the running state and commits the command's output.
// The whole block is emitted as a single tea.Println: tea.Batch runs commands
// concurrently with no ordering guarantee, which would let a trailing summary
// like "(2 rows)" race ahead of the table it summarizes.
func (m Model) handleAsyncResult(msg asyncResultMsg) (tea.Model, tea.Cmd) {
	m.running = false
	m.cancel = nil
	m.refreshPrompt() // connection/database may have changed
	if msg.result != nil {
		m.lastResult = msg.result
	}

	var lines []string
	if msg.err != nil {
		if errors.Is(msg.err, context.Canceled) {
			lines = append(lines, "canceled")
		} else {
			lines = append(lines, "error: "+msg.err.Error())
		}
	}
	lines = append(lines, msg.lines...)
	if len(lines) == 0 {
		return m, nil
	}
	return m, tea.Println(strings.Join(lines, "\n"))
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
	res, act := m.handleLine(line)
	if len(res.lines) > 0 {
		cmds = append(cmds, tea.Println(strings.Join(res.lines, "\n")))
	}
	if res.quit {
		m.quitting = true
		cmds = append(cmds, tea.Quit)
		return m, tea.Sequence(cmds...)
	}

	switch {
	case act.grid:
		if m.lastResult == nil || len(m.lastResult.cols) == 0 {
			cmds = append(cmds, tea.Println("no result to show — run a query first"))
			break
		}
		m.mode = modeGrid
		m.grid = newGrid(m.lastResult, m.width, m.height)
	case act.async != nil:
		ctx, cancel := context.WithCancel(context.Background())
		m.running = true
		m.cancel = cancel
		run := act.async
		cmds = append(cmds, func() tea.Msg { return run(ctx) })
	case act.cmd != nil:
		// One-shot command such as the \edit editor handoff (no cancel spinner).
		cmds = append(cmds, act.cmd)
	default:
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
	if m.mode == modeGrid {
		v := tea.NewView(m.grid.View())
		v.AltScreen = true
		return v
	}
	content := m.styledPrompt() + renderInput(m.input.Value(), m.input.Position(), m.dialect, m.colorPrompt)
	if m.running {
		content = "running… (Ctrl-C to cancel)"
	}
	return tea.NewView(content)
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
