package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Solifugus/mcli/internal/core/config"
)

// cmdResult is the outcome of handling one submitted line: the output lines to
// commit to scrollback, and whether the app should quit. Keeping dispatch pure
// (no Bubble Tea types) makes the command layer unit-testable without a terminal.
type cmdResult struct {
	lines []string
	quit  bool
}

func out(lines ...string) cmdResult      { return cmdResult{lines: lines} }
func errOut(err error) cmdResult         { return cmdResult{lines: []string{"error: " + err.Error()}} }
func (r cmdResult) add(s string) cmdResult { r.lines = append(r.lines, s); return r }

// action is what submit should do after a command's immediate output. At most
// one field is set: async for a cancellable background DB op, cmd for a one-shot
// command (e.g. the \edit editor handoff), neither for a purely synchronous
// command.
type action struct {
	async asyncRun
	cmd   tea.Cmd
	grid  bool // open the last result in the alt-screen grid
}

func sync() action            { return action{} }
func async(r asyncRun) action { return action{async: r} }
func runCmd(c tea.Cmd) action { return action{cmd: c} }
func gridAction() action      { return action{grid: true} }

// handleLine interprets a non-empty submitted line, returning the immediate
// output and an action describing any follow-up work.
func (m *Model) handleLine(line string) (cmdResult, action) {
	cmd, args := tokenize(line)
	switch cmd {
	case `\quit`, `\q`, `\exit`:
		return cmdResult{quit: true}, sync()
	case `\help`:
		return helpText(), sync()
	case `\workspace`:
		return m.cmdWorkspace(args), sync()
	case `\enter`:
		return m.cmdEnter(args), sync()
	case `\server`:
		return m.cmdServer(args), sync()
	case `\connect`:
		res, run := m.cmdConnect(args)
		return res, async(run)
	case `\disconnect`:
		return m.cmdDisconnect(), sync()
	case `\list`:
		res, run := m.cmdList(args)
		return res, async(run)
	case `\describe`:
		res, run := m.cmdDescribe(args)
		return res, async(run)
	case "use":
		res, run := m.cmdUse(args)
		return res, async(run)
	case `\files`:
		return m.cmdFiles(), sync()
	case `\cat`:
		return m.cmdCat(args), sync()
	case `\copy`:
		return m.cmdCopy(args), sync()
	case `\rename`:
		return m.cmdRenameFile(args), sync()
	case `\delete`:
		return m.cmdDeleteFile(args), sync()
	case `\edit`:
		res, c := m.cmdEdit(args)
		return res, runCmd(c)
	case `\run`:
		res, run := m.cmdRun(args)
		return res, async(run)
	case `\grid`:
		return cmdResult{}, gridAction()
	case `\export`:
		res, run := m.cmdExport(args)
		return res, async(run)
	case `\import`:
		res, run := m.cmdImport(args)
		return res, async(run)
	default:
		if strings.HasPrefix(cmd, `\`) {
			return out("unknown command: " + cmd + " (try \\help)"), sync()
		}
		// Bare input is SQL, run against the live connection.
		return cmdResult{}, async(m.sqlRunner(line))
	}
}

// --- SQL file commands (§15) ---

func (m *Model) cmdFiles() cmdResult {
	files, err := m.core.ListSQLFiles()
	if err != nil {
		return errOut(err)
	}
	if len(files) == 0 {
		return out("no SQL files in this workspace")
	}
	return cmdResult{lines: files}
}

func (m *Model) cmdCat(args []string) cmdResult {
	if len(args) < 1 {
		return out(`usage: \cat <name>`)
	}
	content, err := m.core.ReadSQLFile(args[0])
	if err != nil {
		return errOut(err)
	}
	if content == "" {
		return out("(empty file)")
	}
	return cmdResult{lines: strings.Split(strings.TrimRight(content, "\n"), "\n")}
}

func (m *Model) cmdCopy(args []string) cmdResult {
	if len(args) < 2 {
		return out(`usage: \copy <old> <new>`)
	}
	if err := m.core.CopySQLFile(args[0], args[1]); err != nil {
		return errOut(err)
	}
	return out("copied " + args[0] + " to " + args[1])
}

func (m *Model) cmdRenameFile(args []string) cmdResult {
	if len(args) < 2 {
		return out(`usage: \rename <old> <new>`)
	}
	if err := m.core.RenameSQLFile(args[0], args[1]); err != nil {
		return errOut(err)
	}
	return out("renamed " + args[0] + " to " + args[1])
}

func (m *Model) cmdDeleteFile(args []string) cmdResult {
	if len(args) < 1 {
		return out(`usage: \delete <name>`)
	}
	if err := m.core.DeleteSQLFile(args[0]); err != nil {
		return errOut(err)
	}
	return out("deleted " + args[0])
}

// cmdDisconnect closes the connection. It is synchronous: closing is quick and
// touching Core off the UI thread is unnecessary here.
func (m *Model) cmdDisconnect() cmdResult {
	if !m.core.Connected() {
		return out("not connected")
	}
	server := m.core.ConnServer()
	if err := m.core.Disconnect(); err != nil {
		return errOut(err)
	}
	return out("disconnected from " + server)
}

// cmdServer lists configured servers or shows one's details. It is read-only;
// add/edit/remove/test arrive in Phase 7.
func (m *Model) cmdServer(args []string) cmdResult {
	sub := "list"
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "list":
		names := sortedServerNames(m.core.Servers())
		if len(names) == 0 {
			return out(`no servers configured — add one to ~/.mcli/servers.json (\server add arrives in Phase 7)`)
		}
		servers := m.core.Servers()
		cur := m.core.ConnServer()
		rows := make([][]string, 0, len(names))
		for _, n := range names {
			s := servers[n]
			marker := " "
			if n == cur {
				marker = "*"
			}
			rows = append(rows, []string{marker, n, s.Type, orNone(s.Environment), serverTarget(s)})
		}
		return cmdResult{lines: renderTable([]string{"", "name", "type", "env", "target"}, rows)}
	case "show":
		if len(args) < 2 {
			return out(`usage: \server show <name>`)
		}
		s, ok := m.core.Servers()[args[1]]
		if !ok {
			return out("no server named " + args[1])
		}
		return out(serverDetails(args[1], s)...)
	default:
		return out(`usage: \server list|show`)
	}
}

// serverTarget renders a one-line connection target for the server list.
func serverTarget(s config.Server) string {
	if s.ConnectionString != "" {
		return s.ConnectionString
	}
	t := s.Host
	if s.Port != 0 {
		t += fmt.Sprintf(":%d", s.Port)
	}
	if s.DefaultDatabase != "" {
		t += "/" + s.DefaultDatabase
	}
	return t
}

// serverDetails renders the per-field view for \server show. It never prints a
// password — only the password source.
func serverDetails(name string, s config.Server) []string {
	lines := []string{
		"server:   " + name,
		"type:     " + s.Type,
		"env:      " + orNone(s.Environment),
	}
	if s.ConnectionString != "" {
		lines = append(lines, "conn:     "+s.ConnectionString)
	} else {
		lines = append(lines, "host:     "+orNone(s.Host))
		if s.Port != 0 {
			lines = append(lines, fmt.Sprintf("port:     %d", s.Port))
		}
		lines = append(lines, "database: "+orNone(s.DefaultDatabase))
		lines = append(lines, "user:     "+orNone(s.User))
	}
	return append(lines, "password: "+orNone(s.PasswordSource))
}

// sortedServerNames returns the configured server names, sorted.
func sortedServerNames(servers map[string]config.Server) []string {
	names := make([]string, 0, len(servers))
	for n := range servers {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func (m *Model) cmdWorkspace(args []string) cmdResult {
	if len(args) == 0 {
		return out(`usage: \workspace list|create|rename|delete|status`)
	}
	switch args[0] {
	case "list":
		names, err := m.core.ListWorkspaces()
		if err != nil {
			return errOut(err)
		}
		cur := m.core.Current().Name
		res := cmdResult{}
		for _, n := range names {
			marker := "  "
			if n == cur {
				marker = "* "
			}
			res = res.add(marker + n)
		}
		return res
	case "create":
		if len(args) < 2 {
			return out(`usage: \workspace create <name>`)
		}
		if err := m.core.CreateWorkspace(args[1]); err != nil {
			return errOut(err)
		}
		return out("created workspace " + args[1])
	case "rename":
		if len(args) < 3 {
			return out(`usage: \workspace rename <old> <new>`)
		}
		if err := m.core.RenameWorkspace(args[1], args[2]); err != nil {
			return errOut(err)
		}
		return out("renamed " + args[1] + " to " + args[2])
	case "delete":
		if len(args) < 2 {
			return out(`usage: \workspace delete <name>`)
		}
		if err := m.core.DeleteWorkspace(args[1]); err != nil {
			return errOut(err)
		}
		return out("deleted workspace " + args[1])
	case "status":
		ws := m.core.Current()
		return out(
			"workspace: "+ws.Name,
			"server:    "+orNone(ws.CurrentServer),
			"database:  "+orNone(ws.CurrentDatabase),
		)
	default:
		return out(`unknown \workspace subcommand: ` + args[0])
	}
}

func (m *Model) cmdEnter(args []string) cmdResult {
	if len(args) < 1 {
		return out(`usage: \enter <workspace>`)
	}
	if err := m.core.Enter(args[0]); err != nil {
		return errOut(err)
	}
	return out("entered workspace " + args[0])
}

func helpText() cmdResult {
	return out(
		`commands:`,
		`  \workspace list|create|rename|delete|status   manage workspaces`,
		`  \enter <name>                                 switch workspace`,
		`  \server list|show <name>                      list configured servers`,
		`  \connect <server>                             connect to a configured server`,
		`  \disconnect                                   close the connection`,
		`  use <database>                                switch current database`,
		`  \list databases|schemas|tables|views          list objects`,
		`  \describe <table>                             show columns`,
		`  <sql>                                         run SQL on the connection`,
		`  \grid                                         open the last result in a scrollable grid`,
		`  \export query <name>|table <name>|current to <path> [exact]   export to CSV/TSV/pipe/xlsx/fixed`,
		`  \import <path> [sheet <name>|widths N,N,...] into <table>   load a delimited/xlsx/fixed file`,
		`  \files                                        list workspace SQL files`,
		`  \edit <name>                                  edit a SQL file ($EDITOR)`,
		`  \run <name>                                   run a SQL file`,
		`  \cat <name>                                   print a SQL file`,
		`  \copy <old> <new> / \rename <old> <new>       copy or rename a file`,
		`  \delete <name>                                delete a SQL file`,
		`  \help                                         this help`,
		`  \quit                                         exit (also Ctrl-C / Ctrl-D)`,
		``,
		`Ctrl-C cancels a running query; Ctrl-D quits.`,
	)
}

// tokenize splits a line into a command token and whitespace-separated args.
func tokenize(line string) (cmd string, args []string) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "", nil
	}
	return fields[0], fields[1:]
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}
