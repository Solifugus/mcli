package tui

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/Solifugus/mcli/internal/core"
	"github.com/Solifugus/mcli/internal/core/adapter"
	"github.com/Solifugus/mcli/internal/core/config"
)

// cmdResult is the outcome of handling one submitted line: the output lines to
// commit to scrollback, and whether the app should quit. Keeping dispatch pure
// (no Bubble Tea types) makes the command layer unit-testable without a terminal.
type cmdResult struct {
	lines []string
	quit  bool
}

func out(lines ...string) cmdResult        { return cmdResult{lines: lines} }
func errOut(err error) cmdResult           { return cmdResult{lines: []string{"error: " + err.Error()}} }
func (r cmdResult) add(s string) cmdResult { r.lines = append(r.lines, s); return r }

// action is what submit should do after a command's immediate output. At most
// one field is set: async for a cancellable background DB op, cmd for a one-shot
// command (e.g. the .edit editor handoff), neither for a purely synchronous
// command.
type action struct {
	async   asyncRun
	cmd     tea.Cmd
	grid    bool         // open the last result in the alt-screen grid
	confirm *confirmReq  // ask before running confirm.run as a background op
	prompt  *pending     // enter an interactive sub-prompt
	editor  *editorModel // open the built-in editor (alt-screen)
}

// confirmReq asks the user a yes/no question before launching a background op.
// It is how the safety layer (§17) gates dangerous SQL and production writes.
type confirmReq struct {
	question string
	run      asyncRun
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
	case `.quit`, `.q`, `.exit`:
		return cmdResult{quit: true}, sync()
	case `.help`:
		return m.helpText(), sync()
	case `.clear`, `.cls`:
		return cmdResult{}, runCmd(clearScreenCmd())
	case `.workspace`:
		return m.cmdWorkspace(args), sync()
	case `.enter`:
		return m.cmdEnter(args), sync()
	case `.server`:
		return m.cmdServer(args)
	case `.connect`:
		return m.cmdConnect(args)
	case `.disconnect`:
		return m.cmdDisconnect(), sync()
	case `.list`:
		res, run := m.cmdList(args)
		return res, async(run)
	case `.describe`:
		res, run := m.cmdDescribe(args)
		return res, async(run)
	case `.objects`, `.find`:
		res, run := m.cmdObjects(args)
		return res, async(run)
	case `.source`:
		res, run := m.cmdSource(args)
		return res, async(run)
	case `.grep`:
		res, run := m.cmdGrep(args)
		return res, async(run)
	case `.pre-lineage`:
		res, run := m.cmdLineage(args, core.LineagePre)
		return res, async(run)
	case `.post-lineage`:
		res, run := m.cmdLineage(args, core.LineagePost)
		return res, async(run)
	case `.tablefuncs`, `.tvf`:
		res, run := m.cmdTableFuncs(args)
		return res, async(run)
	case `.jobs`:
		res, run := m.cmdJobs(args)
		return res, async(run)
	case `.job`:
		res, run := m.cmdJob(args)
		return res, async(run)
	case `.users`:
		res, run := m.cmdPrincipals(args, adapter.PrincipalKindUser)
		return res, async(run)
	case `.roles`:
		res, run := m.cmdPrincipals(args, adapter.PrincipalKindRole)
		return res, async(run)
	case `.user`, `.role`:
		res, run := m.cmdPrincipal(args)
		return res, async(run)
	case `.grant`:
		return m.cmdGrant(args, false)
	case `.revoke`:
		return m.cmdGrant(args, true)
	case `.createuser`:
		return m.cmdCreateUser(args)
	case `.dropuser`:
		return m.cmdDropUser(args)
	case "use":
		res, run := m.cmdUse(args)
		return res, async(run)
	case `.files`:
		return m.cmdFiles(), sync()
	case `.cat`:
		return m.cmdCat(args), sync()
	case `.copy`:
		return m.cmdCopy(args), sync()
	case `.rename`:
		return m.cmdRenameFile(args), sync()
	case `.delete`:
		return m.cmdDeleteFile(args), sync()
	case `.edit`:
		if m.core.Settings().Editor == "builtin" {
			return m.cmdEditBuiltin(args)
		}
		res, c := m.cmdEdit(args)
		return res, runCmd(c)
	case `.mcp`:
		res, c := m.cmdMCP(args)
		return res, runCmd(c)
	case `.assist`:
		return m.cmdAssist(args)
	case `.run`:
		return m.cmdRun(args)
	case `.lint`:
		return m.cmdLint(args)
	case `.readonly`:
		return m.cmdReadonly(args), sync()
	case `.caps`:
		return m.cmdCaps(), sync()
	case `.ai`:
		return m.cmdAI(args)
	case `.grid`:
		return cmdResult{}, gridAction()
	case `.export`:
		res, run := m.cmdExport(args)
		return res, async(run)
	case `.import`:
		res, run := m.cmdImport(args)
		return res, async(run)
	default:
		// Commands are dot-prefixed. A leading '.' (or a legacy '\') that matched
		// no case is an unknown command, not SQL — no valid statement starts with
		// either. Everything else is SQL, run against the connection (behind the
		// guard).
		if strings.HasPrefix(cmd, ".") || strings.HasPrefix(cmd, `\`) {
			hint := ""
			if strings.HasPrefix(cmd, `\`) {
				hint = " (commands now start with '.', e.g. .help)"
			} else {
				hint = " (try .help)"
			}
			return out("unknown command: " + cmd + hint), sync()
		}
		return m.guardedSQL(line)
	}
}

// clearScreenCmd clears the terminal for the inline REPL. tea.ClearScreen alone
// is not enough: in inline (non-alt-screen) mode the renderer manages only the
// prompt's own line, so it erases that line and nothing else. So we first write
// the real terminal clear sequence with tea.Raw — ESC[H home, ESC[2J clear the
// screen, ESC[3J clear the scrollback — and then issue tea.ClearScreen to force
// the renderer to repaint the prompt at the top (it otherwise short-circuits the
// redraw because the View content is unchanged).
func clearScreenCmd() tea.Cmd {
	return tea.Sequence(tea.Raw("\x1b[H\x1b[2J\x1b[3J"), tea.ClearScreen)
}

// cmdReadonly shows or toggles the session read-only guard (§17).
func (m *Model) cmdReadonly(args []string) cmdResult {
	if len(args) == 0 {
		return out("read-only mode is " + onOff(m.core.ReadOnly()))
	}
	switch strings.ToLower(args[0]) {
	case "on", "true", "1":
		m.core.SetReadOnly(true)
		return out("read-only mode on — only read-only statements will run")
	case "off", "false", "0":
		m.core.SetReadOnly(false)
		return out("read-only mode off")
	default:
		return out(`usage: .readonly [on|off]`)
	}
}

// capRows is the display order and labels for .caps: the full optional-feature
// surface, so the user sees both what this engine supports and what it doesn't.
var capRows = []struct {
	cap   adapter.Capability
	label string
}{
	{adapter.CapExplain, "explain — execution plans (.explain)"},
	{adapter.CapLineage, "lineage — object dependency graph"},
	{adapter.CapSource, "source — CREATE / definition text"},
	{adapter.CapTableFunctions, "table_functions — table-valued functions as data"},
	{adapter.CapJobs, "jobs — scheduler / agent introspection"},
	{adapter.CapSecurity, "security — users, roles, grants"},
	{adapter.CapSecurityEdit, "security_edit — generate GRANT/REVOKE/CREATE USER"},
}

// cmdCaps reports the optional features the connected engine supports. Both
// front-ends read the same core.Capabilities(); this is the CLI face of it.
func (m *Model) cmdCaps() cmdResult {
	if !m.core.Connected() {
		return out("not connected — capabilities depend on the connected server (use .connect)")
	}
	caps := m.core.Capabilities()
	lines := []string{"capabilities of " + m.core.ConnServer() + ":"}
	for _, r := range capRows {
		mark := "  ✗ "
		if caps.Has(r.cap) {
			mark = "  ✓ "
		}
		lines = append(lines, mark+r.label)
	}
	return cmdResult{lines: lines}
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
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
		return out(`usage: .cat <name>`)
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
		return out(`usage: .copy <old> <new>`)
	}
	if err := m.core.CopySQLFile(args[0], args[1]); err != nil {
		return errOut(err)
	}
	return out("copied " + args[0] + " to " + args[1])
}

func (m *Model) cmdRenameFile(args []string) cmdResult {
	if len(args) < 2 {
		return out(`usage: .rename <old> <new>`)
	}
	if err := m.core.RenameSQLFile(args[0], args[1]); err != nil {
		return errOut(err)
	}
	return out("renamed " + args[0] + " to " + args[1])
}

func (m *Model) cmdDeleteFile(args []string) cmdResult {
	if len(args) < 1 {
		return out(`usage: .delete <name>`)
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

// cmdServer dispatches the .server subcommands: list/show are read-only; add and
// edit launch the interactive wizard (an action prompt); remove is synchronous;
// test connects in the background.
func (m *Model) cmdServer(args []string) (cmdResult, action) {
	sub := "list"
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "list":
		return m.serverList(), sync()
	case "show":
		if len(args) < 2 {
			return out(`usage: .server show <name>`), sync()
		}
		s, ok := m.core.Servers()[args[1]]
		if !ok {
			return out("no server named " + args[1]), sync()
		}
		return out(serverDetails(args[1], s)...), sync()
	case "add":
		return m.serverAdd(args[1:])
	case "edit":
		return m.serverEdit(args[1:])
	case "remove", "rm", "delete":
		return m.serverRemove(args[1:]), sync()
	case "test":
		return m.serverTest(args[1:])
	case "set-password", "passwd", "password":
		return m.serverSetPassword(args[1:])
	case "clear-password":
		return m.serverClearPassword(args[1:]), sync()
	default:
		return out(`usage: .server list|show|add|edit|remove|test|set-password|clear-password`), sync()
	}
}

func (m *Model) serverList() cmdResult {
	names := sortedServerNames(m.core.Servers())
	if len(names) == 0 {
		return out(`no servers configured — .server add to create one`)
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
	return cmdResult{lines: styleTable(renderTable([]string{"", "name", "type", "env", "target"}, rows), m.colorPrompt, m.darkBG)}
}

// serverAdd launches the add wizard. An optional name argument pre-sets the name
// so that step is skipped.
func (m *Model) serverAdd(rest []string) (cmdResult, action) {
	name := ""
	if len(rest) > 0 {
		name = rest[0]
		if _, exists := m.core.Server(name); exists {
			return out("server " + name + " already exists (use .server edit)"), sync()
		}
	}
	fields := serverFields(config.Server{}, name == "")
	done := func(m *Model, vals map[string]string) tea.Cmd {
		nm := name
		if nm == "" {
			nm = vals["name"]
		}
		s, err := serverFromVals(vals)
		if err != nil {
			return tea.Println("error: " + err.Error())
		}
		if err := m.core.AddServer(nm, s); err != nil {
			return tea.Println("error: " + err.Error())
		}
		return tea.Println("added server " + nm + " — .connect " + nm + " to use it")
	}
	intro := "adding a server (Esc to cancel)"
	return out(intro), action{prompt: m.formPrompt(fields, done)}
}

// serverEdit launches the edit wizard for an existing server, pre-filling the
// current values as defaults.
func (m *Model) serverEdit(rest []string) (cmdResult, action) {
	if len(rest) < 1 {
		return out(`usage: .server edit <name>`), sync()
	}
	name := rest[0]
	existing, ok := m.core.Server(name)
	if !ok {
		return out("no server named " + name), sync()
	}
	fields := serverFields(existing, false)
	done := func(m *Model, vals map[string]string) tea.Cmd {
		s, err := serverFromVals(vals)
		if err != nil {
			return tea.Println("error: " + err.Error())
		}
		if err := m.core.EditServer(name, s); err != nil {
			return tea.Println("error: " + err.Error())
		}
		return tea.Println("updated server " + name)
	}
	return out("editing " + name + " (Enter keeps the shown value; Esc cancels)"), action{prompt: m.formPrompt(fields, done)}
}

// formPrompt builds the first pending of a form without entering it (submit's
// dispatcher calls startPrompt on the returned action.prompt).
func (m *Model) formPrompt(fields []formField, done func(*Model, map[string]string) tea.Cmd) *pending {
	probe := *m
	probe.startForm(fields, map[string]string{}, done)
	return probe.pending
}

func (m *Model) serverRemove(rest []string) cmdResult {
	if len(rest) < 1 {
		return out(`usage: .server remove <name>`)
	}
	name := rest[0]
	if err := m.core.RemoveServer(name); err != nil {
		return errOut(err)
	}
	return out("removed server " + name)
}

// serverTest connects to a server in the background and reports reachability,
// prompting for a password if the source requires it.
func (m *Model) serverTest(rest []string) (cmdResult, action) {
	if len(rest) < 1 {
		return out(`usage: .server test <name>`), sync()
	}
	name := rest[0]
	c := m.core
	ok := []string{name + ": connection OK"}
	return cmdResult{}, async(func(ctx context.Context) asyncResultMsg {
		switch err := c.TestServer(ctx, name); {
		case err == nil:
			return asyncResultMsg{lines: ok}
		case errors.Is(err, core.ErrPasswordRequired):
			return asyncResultMsg{pwPrompt: &pwReq{
				label: "password for " + name + ": ",
				run: func(pw string) asyncRun {
					return func(ctx context.Context) asyncResultMsg {
						if err := c.TestServerWith(ctx, name, pw); err != nil {
							return asyncResultMsg{err: fmt.Errorf("%s: %w", name, err)}
						}
						return asyncResultMsg{lines: ok}
					}
				},
			}}
		default:
			return asyncResultMsg{err: fmt.Errorf("%s: %w", name, err)}
		}
	})
}

// serverSetPassword prompts (masked) for a secret and stores it in the OS
// keyring under the server name. Pair with password_source "keyring".
func (m *Model) serverSetPassword(rest []string) (cmdResult, action) {
	if len(rest) < 1 {
		return out(`usage: .server set-password <name>`), sync()
	}
	name := rest[0]
	if _, ok := m.core.Server(name); !ok {
		return out("no server named " + name), sync()
	}
	c := m.core
	p := &pending{
		label: "keyring password for " + name + ": ",
		mask:  true,
		resume: func(m *Model, text string, canceled bool) tea.Cmd {
			if canceled {
				return tea.Println("canceled")
			}
			return m.launchAsync(func(ctx context.Context) asyncResultMsg {
				if err := c.SetServerPassword(name, text); err != nil {
					return asyncResultMsg{err: err}
				}
				return asyncResultMsg{lines: []string{"stored keyring secret for " + name + " (set its password_source to keyring to use it)"}}
			})
		},
	}
	return out("setting keyring password for " + name + " (Esc cancels)"), action{prompt: p}
}

// serverClearPassword removes a server's keyring secret.
func (m *Model) serverClearPassword(rest []string) cmdResult {
	if len(rest) < 1 {
		return out(`usage: .server clear-password <name>`)
	}
	if err := m.core.DeleteServerPassword(rest[0]); err != nil {
		return errOut(err)
	}
	return out("cleared keyring secret for " + rest[0])
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

// serverDetails renders the per-field view for .server show. It never prints a
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
		return out(`usage: .workspace list|create|rename|delete|status`)
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
			return out(`usage: .workspace create <name>`)
		}
		if err := m.core.CreateWorkspace(args[1]); err != nil {
			return errOut(err)
		}
		return out("created workspace " + args[1])
	case "rename":
		if len(args) < 3 {
			return out(`usage: .workspace rename <old> <new>`)
		}
		if err := m.core.RenameWorkspace(args[1], args[2]); err != nil {
			return errOut(err)
		}
		return out("renamed " + args[1] + " to " + args[2])
	case "delete":
		if len(args) < 2 {
			return out(`usage: .workspace delete <name>`)
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
		return out(`unknown .workspace subcommand: ` + args[0])
	}
}

func (m *Model) cmdEnter(args []string) cmdResult {
	if len(args) < 1 {
		return out(`usage: .enter <workspace>`)
	}
	if err := m.core.Enter(args[0]); err != nil {
		return errOut(err)
	}
	return out("entered workspace " + args[0])
}

// helpEntry is one command line: a name, its argument syntax, and a description.
type helpEntry struct{ name, args, desc string }

// helpSection groups related commands under a heading.
type helpSection struct {
	title   string
	entries []helpEntry
}

var helpSections = []helpSection{
	{"Workspaces", []helpEntry{
		{".workspace", "list|create|rename|delete|status", "manage workspaces"},
		{".enter", "<name>", "switch workspace"},
	}},
	{"Servers & connections", []helpEntry{
		{".server", "list|show|add|edit|remove|test", "manage configured servers"},
		{".server", "set-password|clear-password <name>", "store/remove a keyring secret"},
		{".connect", "<server>", "connect to a configured server"},
		{".disconnect", "", "close the connection"},
	}},
	{"Databases & objects", []helpEntry{
		{"use", "<database>", "switch current database"},
		{".list", "databases|schemas|tables|views", "list objects"},
		{".objects", "[tables] [views] [procedures] [functions] [<substr>]", "find objects by type + name (alias .find)"},
		{".describe", "<table>", "show columns"},
		{".source", "<view|procedure|function>", "show an object's definition text"},
		{".grep", "<text>", "search procedure/function names and bodies"},
		{".pre-lineage", "<object> [depth]", "objects this one depends on (its inputs)"},
		{".post-lineage", "<object> [depth]", "objects that depend on this one (its consumers)"},
		{".tablefuncs", "[<substr>]", "list table-valued functions + query template (alias .tvf)"},
		{".caps", "", "show what the connected engine supports"},
	}},
	{"Scheduling", []helpEntry{
		{".jobs", "[<substr>]", "list scheduler / agent jobs"},
		{".job", "<name> [--history [N]]", "show a job's design, or its recent run history"},
	}},
	{"Security", []helpEntry{
		{".users", "[<substr>]", "list database users"},
		{".roles", "[<substr>]", "list database roles"},
		{".user", "<name>", "show a principal's config (also .role)"},
		{".grant", "<privs|role> [ON <obj>] TO <who>", "grant privileges or a role (guarded)"},
		{".revoke", "<privs|role> [ON <obj>] FROM <who>", "revoke privileges or a role (guarded)"},
		{".createuser", "<name> <password>", "create a user/login (guarded)"},
		{".dropuser", "<name>", "drop a user/login/role (guarded, dangerous)"},
	}},
	{"Running SQL", []helpEntry{
		{"<sql>", "", "run SQL on the connection"},
		{".readonly", "[on|off]", "show or toggle read-only guard"},
		{".grid", "", "open the last result in a scrollable grid"},
		{".lint", "<name|current> [live]", "check SQL for safety/syntax/style issues"},
	}},
	{"SQL files", []helpEntry{
		{".files", "", "list workspace SQL files"},
		{".edit", "<name>", "edit a SQL file ($EDITOR, or builtin editor)"},
		{".run", "<name>", "run a SQL file"},
		{".cat", "<name>", "print a SQL file"},
		{".copy", "<old> <new>", "copy a file"},
		{".rename", "<old> <new>", "rename a file"},
		{".delete", "<name>", "delete a SQL file"},
	}},
	{"Import / export", []helpEntry{
		{".export", "query <name>|table <name>|current to <path> [exact]", "export to CSV/TSV/pipe/xlsx/fixed"},
		{".import", "<path> [sheet <name>|widths N,N,...] into <table>", "load a delimited/xlsx/fixed file"},
	}},
	{"AI assistant", []helpEntry{
		{".ai", "ask <q>|explain <f|current>|fix <f|current>|providers|help", "ask the AI assistant"},
		{".assist", "on|off|status", "let an external AI attach and guide you live (§26)"},
	}},
	{"System", []helpEntry{
		{".mcp", "serve", "run the MCP server on this terminal's stdio"},
		{".clear", "", "clear the screen"},
		{".help", "", "this help"},
		{".quit", "", "exit (also Ctrl-C / Ctrl-D)"},
	}},
}

func (m *Model) helpText() cmdResult {
	width := m.width
	if width <= 0 {
		width = 80
	}

	// Align descriptions to a common column, derived from the longest signature
	// that is short enough to leave room for its description (the few very long
	// signatures fall back to an inline gap rather than pushing the column out).
	descCol := 0
	for _, sec := range helpSections {
		for _, e := range sec.entries {
			if w := sigWidth(e); w <= 44 && w+2 > descCol {
				descCol = w + 2
			}
		}
	}
	if descCol < 20 {
		descCol = 20
	}

	var lines []string
	for si, sec := range helpSections {
		if si > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, m.styleIf(helpTitleStyle, sec.title))
		for i, e := range sec.entries {
			lines = append(lines, m.helpLine(e, descCol, width, i%2 == 1))
		}
	}
	lines = append(lines,
		"",
		m.styleIf(helpFootStyle, "Ctrl-C cancels a running query; Ctrl-D quits."),
	)
	return out(lines...)
}

// sigWidth is the display width of an entry's "name args" signature.
func sigWidth(e helpEntry) int {
	w := len(e.name)
	if e.args != "" {
		w += 1 + len(e.args)
	}
	return w
}

// helpLine renders one command row: colored name, dimmed argument syntax, and
// description aligned to descCol. Striped rows get a faint full-width background;
// the stripe is built by concatenating per-segment styles (each carrying the
// background) rather than wrapping, because lipgloss resets the background at the
// end of every nested Render.
func (m *Model) helpLine(e helpEntry, descCol, width int, striped bool) string {
	if !m.colorPrompt {
		sig := e.name
		if e.args != "" {
			sig += " " + e.args
		}
		line := "  " + sig
		if e.desc != "" {
			if w := sigWidth(e); w < descCol {
				line += strings.Repeat(" ", descCol-w)
			} else {
				line += "  "
			}
			line += e.desc
		}
		return strings.TrimRight(line, " ")
	}

	nameSt, argSt, descSt := helpCmdStyle, helpArgStyle, helpDescStyle
	padSt := lipgloss.NewStyle()
	if striped {
		bg := stripeColor(m.darkBG)
		nameSt = nameSt.Background(bg)
		argSt = argSt.Background(bg)
		descSt = descSt.Background(bg)
		padSt = padSt.Background(bg)
	}

	var b strings.Builder
	b.WriteString("  ")
	b.WriteString(nameSt.Render(e.name))
	if e.args != "" {
		b.WriteString(argSt.Render(" " + e.args))
	}
	if e.desc != "" {
		gap := descCol - sigWidth(e)
		if gap < 2 {
			gap = 2
		}
		b.WriteString(padSt.Render(strings.Repeat(" ", gap)))
		b.WriteString(descSt.Render(e.desc))
	}
	if striped {
		if pad := width - lipgloss.Width(b.String()); pad > 0 {
			b.WriteString(padSt.Render(strings.Repeat(" ", pad)))
		}
	}
	return b.String()
}

// styleIf applies a style only when color output is enabled, so plain mode (and
// the tests that assert on it) keeps unstyled text.
func (m *Model) styleIf(st lipgloss.Style, s string) string {
	if !m.colorPrompt {
		return s
	}
	return st.Render(s)
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
