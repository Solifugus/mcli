package tui

import "strings"

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

// dispatch interprets a non-empty submitted line and mutates core state.
func (m *Model) dispatch(line string) cmdResult {
	cmd, args := tokenize(line)
	switch cmd {
	case `\quit`, `\q`, `\exit`:
		return cmdResult{quit: true}
	case `\help`:
		return helpText()
	case `\workspace`:
		return m.cmdWorkspace(args)
	case `\enter`:
		return m.cmdEnter(args)
	default:
		if strings.HasPrefix(cmd, `\`) {
			return out("unknown command: " + cmd + " (try \\help)")
		}
		// Bare input is SQL or `use <db>`; both need a live connection, which
		// arrives in Phase 3.
		return out("not connected — database commands arrive in Phase 3")
	}
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
		`  \help                                         this help`,
		`  \quit                                         exit (also Ctrl-C / Ctrl-D)`,
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
