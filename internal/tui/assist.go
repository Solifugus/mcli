package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Solifugus/mcli/internal/core/assist"
	"github.com/Solifugus/mcli/internal/mcp"
)

// assistMsg carries one guidance event from the live session to the UI thread.
type assistMsg struct{ e assist.Event }

// assistClosedMsg signals the subscription channel closed (session stopped).
type assistClosedMsg struct{}

// waitForAssist blocks on the subscription channel and delivers the next event
// as a tea.Msg. It is re-issued after each event so the model keeps listening
// for the lifetime of the session.
func waitForAssist(ch <-chan assist.Event) tea.Cmd {
	return func() tea.Msg {
		e, ok := <-ch
		if !ok {
			return assistClosedMsg{}
		}
		return assistMsg{e: e}
	}
}

// cmdAssist handles `.assist [on|off|status]`: the opt-in switch for the live
// AI session (design §26). Off by default because mcli may be on production.
func (m *Model) cmdAssist(args []string) (cmdResult, action) {
	sub := "status"
	if len(args) > 0 {
		sub = strings.ToLower(args[0])
	}
	switch sub {
	case "on", "start":
		if m.assistSrv != nil {
			return out("live assist is already on — " + m.assistSrv.URL()), sync()
		}
		srv, err := mcp.ServeHTTP(m.core, "127.0.0.1:0")
		if err != nil {
			return errOut(err), sync()
		}
		ch, unsub := m.core.Assist().Subscribe()
		m.assistSrv = srv
		m.assistCh = ch
		m.assistUnsub = unsub
		return out(
			"live assist ON — an AI agent can now attach to this session:",
			"  url:   "+srv.URL(),
			"  token: "+srv.Token(),
			"  (also written to ~/.mcli/session.json)",
			"Guidance appears here; a prefill lands on your input line for you to review. .assist off to stop.",
		), runCmd(waitForAssist(ch))
	case "off", "stop":
		if m.assistSrv == nil {
			return out("live assist is already off"), sync()
		}
		m.stopAssist()
		return out("live assist OFF"), sync()
	case "status":
		if m.assistSrv == nil {
			return out("live assist is off — .assist on to enable"), sync()
		}
		return out("live assist is on — " + m.assistSrv.URL()), sync()
	default:
		return out("usage: .assist [on|off|status]"), sync()
	}
}

// stopAssist tears down the live endpoint (removing session.json) and the bus
// subscription. Safe to call when off. Also invoked on quit.
func (m *Model) stopAssist() {
	if m.assistUnsub != nil {
		m.assistUnsub()
		m.assistUnsub = nil
	}
	if m.assistSrv != nil {
		_ = m.assistSrv.Close()
		m.assistSrv = nil
	}
	m.assistCh = nil
}

// handleAssist renders one guidance event. A prefill stages text on the input
// line (never submitting it — the user reviews and presses Enter); everything
// else prints an annotation to scrollback. It re-arms the listener so the
// session keeps receiving.
func (m Model) handleAssist(msg assistMsg) (tea.Model, tea.Cmd) {
	e := msg.e
	var lines []string
	switch e.Kind {
	case assist.KindPrefill:
		if e.Target == assist.TargetInputLine || e.Target == "" {
			m.input.SetValue(e.Text)
			m.input.CursorEnd()
			lines = append(lines, "⟵ AI staged this on your input line (review, then Enter): "+e.Text)
		} else {
			lines = append(lines, fmt.Sprintf("⟵ AI suggests for %s: %s", e.Target, e.Text))
		}
	case assist.KindHighlight:
		lines = append(lines, "→ AI points at: "+e.Target)
	case assist.KindFocus:
		lines = append(lines, "→ AI focuses: "+e.Target)
	case assist.KindAnnotate:
		lines = append(lines, fmt.Sprintf("→ AI note [%s]: %s", e.Target, e.Text))
	case assist.KindDemo:
		lines = append(lines, "→ AI walkthrough:")
		for i, s := range e.Steps {
			line := fmt.Sprintf("   %d. %s — %s", i+1, s.Title, s.Description)
			if s.Action != "" {
				line += "  (try: " + s.Action + ")"
			}
			lines = append(lines, line)
		}
	}

	var cmds []tea.Cmd
	if len(lines) > 0 {
		cmds = append(cmds, tea.Println(strings.Join(lines, "\n")))
	}
	if m.assistSrv != nil && m.assistCh != nil { // keep listening
		cmds = append(cmds, waitForAssist(m.assistCh))
	}
	return m, tea.Batch(cmds...)
}
