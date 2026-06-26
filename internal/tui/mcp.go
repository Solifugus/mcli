package tui

import (
	"context"
	"io"
	"os/signal"
	"syscall"

	tea "charm.land/bubbletea/v2"

	"github.com/Solifugus/mcli/internal/core"
	"github.com/Solifugus/mcli/internal/mcp"
)

// mcpDoneMsg is delivered when the in-TUI MCP server stops.
type mcpDoneMsg struct{ err error }

// cmdMCP handles `\mcp serve`: it suspends the TUI and runs the same stdio MCP
// server as `mcli mcp serve`, over the terminal's stdio, until the client
// closes the stream (Ctrl-D) or the user interrupts (Ctrl-C). The server shares
// this session's core, so it sees the current workspace and connection.
func (m *Model) cmdMCP(args []string) (cmdResult, tea.Cmd) {
	if len(args) < 1 || args[0] != "serve" {
		return out(`usage: \mcp serve`), nil
	}
	exec := &mcpExec{core: m.core}
	return out(
		"starting MCP server on this terminal's stdio — it now speaks JSON-RPC, not the REPL.",
		"press Ctrl-C (or close stdin) to stop and return to the prompt.",
	), tea.Exec(exec, func(e error) tea.Msg { return mcpDoneMsg{err: e} })
}

// mcpExec adapts the in-process MCP server to tea.ExecCommand so Bubble Tea
// releases the terminal to it, exactly as it does for the external editor.
type mcpExec struct {
	core   *core.Core
	stdin  io.Reader
	stdout io.Writer
}

func (e *mcpExec) SetStdin(r io.Reader)  { e.stdin = r }
func (e *mcpExec) SetStdout(w io.Writer) { e.stdout = w }
func (e *mcpExec) SetStderr(io.Writer)   {} // the MCP server writes only protocol to stdout

func (e *mcpExec) Run() error {
	// Ctrl-C ends the server and returns to the REPL rather than quitting mcli.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT)
	defer stop()
	return mcp.Serve(ctx, e.core, e.stdin, e.stdout)
}
