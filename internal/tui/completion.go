package tui

import "strings"

// replCommands are the commands offered by Tab completion. Aliases (.q, .exit)
// still work but are intentionally not suggested.
var replCommands = []string{
	`.ai`, `.assist`, `.caps`, `.cat`, `.clear`, `.connect`, `.copy`, `.delete`, `.describe`, `.disconnect`,
	`.edit`, `.enter`, `.export`, `.files`, `.grid`, `.help`, `.import`,
	`.createuser`, `.dropuser`, `.find`, `.grant`, `.grep`, `.job`, `.jobs`, `.lint`, `.list`, `.mcp`, `.objects`, `.post-lineage`, `.pre-lineage`, `.readonly`, `.revoke`, `.role`, `.roles`, `.run`, `.quit`, `.rename`, `.server`, `.source`, `.tablefuncs`, `.tvf`, `.user`, `.users`, `.workspace`, "use",
}

// aiSubcommands are the second-token completions for .ai.
var aiSubcommands = []string{"ask", "explain", "fix", "help", "providers"}

// mcpSubcommands are the second-token completions for .mcp.
var mcpSubcommands = []string{"serve"}

// workspaceSubcommands are the second-token completions for .workspace.
var workspaceSubcommands = []string{"create", "delete", "list", "rename", "status"}

// serverSubcommands are the second-token completions for .server.
var serverSubcommands = []string{"add", "clear-password", "edit", "list", "remove", "set-password", "show", "test"}

// listTargets are the second-token completions for .list.
var listTargets = []string{"databases", "schemas", "tables", "views"}

// objectKindTargets are the kind-filter tokens for .objects / .find. They may
// be combined in any order, so they are offered at every argument position.
var objectKindTargets = []string{"tables", "views", "procedures", "functions"}

// complete returns the line with the token under the (end-of-line) cursor
// expanded, plus any candidate list to display when the completion is ambiguous.
// It is context-aware: command names first, then workspace subcommands and
// workspace names. Workspace-file completion arrives with .edit/.run in Phase 4.
func (m *Model) complete(line string) (newLine string, candidates []string) {
	endsWithSpace := strings.HasSuffix(line, " ")
	fields := strings.Fields(line)

	var pool []string
	switch {
	case len(fields) == 0:
		pool = replCommands
	case len(fields) == 1 && !endsWithSpace:
		pool = replCommands // still typing the command token
	default:
		// Completing an argument. tokenIndex is the index (into fields) of the
		// token being completed; arg N lives at fields[N].
		tokenIndex := len(fields)
		if !endsWithSpace {
			tokenIndex = len(fields) - 1
		}
		sub := ""
		if len(fields) > 1 {
			sub = fields[1]
		}
		pool = m.argCandidates(fields[0], tokenIndex, sub)
	}

	prefix := ""
	if !endsWithSpace && len(fields) > 0 {
		prefix = fields[len(fields)-1]
	}

	matches := filterPrefix(pool, prefix)
	switch len(matches) {
	case 0:
		return line, nil
	case 1:
		return replaceLastToken(line, endsWithSpace, matches[0]) + " ", nil
	default:
		cp := commonPrefix(matches)
		if len(cp) > len(prefix) {
			line = replaceLastToken(line, endsWithSpace, cp)
		}
		return line, matches
	}
}

// argCandidates returns completion candidates for the argument at tokenIndex of
// the given command. tokenIndex 1 is the first argument.
func (m *Model) argCandidates(cmd string, tokenIndex int, subcommand string) []string {
	switch cmd {
	case `.enter`:
		if tokenIndex == 1 {
			return m.workspaceNames()
		}
	case `.workspace`:
		if tokenIndex == 1 {
			return workspaceSubcommands
		}
		if tokenIndex == 2 && (subcommand == "rename" || subcommand == "delete") {
			return m.workspaceNames()
		}
	case `.connect`:
		if tokenIndex == 1 {
			return m.serverNames()
		}
	case `.server`:
		if tokenIndex == 1 {
			return serverSubcommands
		}
		if tokenIndex == 2 {
			switch subcommand {
			case "show", "edit", "remove", "rm", "delete", "test", "set-password", "clear-password":
				return m.serverNames()
			}
		}
	case `.ai`:
		if tokenIndex == 1 {
			return aiSubcommands
		}
		if tokenIndex == 2 && (subcommand == "explain" || subcommand == "fix") {
			return append([]string{"current"}, m.sqlFileNames()...)
		}
	case `.list`:
		if tokenIndex == 1 {
			return listTargets
		}
	case `.objects`, `.find`:
		if tokenIndex >= 1 {
			return objectKindTargets
		}
	case `.mcp`:
		if tokenIndex == 1 {
			return mcpSubcommands
		}
	case `.assist`:
		if tokenIndex == 1 {
			return []string{"on", "off", "status"}
		}
	case `.edit`, `.run`, `.cat`, `.delete`:
		if tokenIndex == 1 {
			return m.sqlFileNames()
		}
	case `.lint`:
		if tokenIndex == 1 {
			return append([]string{"current"}, m.sqlFileNames()...)
		}
		if tokenIndex == 2 {
			return []string{"live"}
		}
	case `.copy`, `.rename`:
		if tokenIndex == 1 { // complete the source file; the destination is new
			return m.sqlFileNames()
		}
	}
	return nil
}

func (m *Model) sqlFileNames() []string {
	files, err := m.core.ListSQLFiles()
	if err != nil {
		return nil
	}
	return files
}

func (m *Model) serverNames() []string {
	return sortedServerNames(m.core.Servers())
}

func (m *Model) workspaceNames() []string {
	names, err := m.core.ListWorkspaces()
	if err != nil {
		return nil
	}
	return names
}

// replaceLastToken swaps the final whitespace-delimited token of line for repl.
// When the line ends in a space, repl is appended as a new token instead.
func replaceLastToken(line string, endsWithSpace bool, repl string) string {
	if endsWithSpace {
		return line + repl
	}
	if i := strings.LastIndex(line, " "); i >= 0 {
		return line[:i+1] + repl
	}
	return repl
}

func filterPrefix(pool []string, prefix string) []string {
	var out []string
	for _, c := range pool {
		if strings.HasPrefix(c, prefix) {
			out = append(out, c)
		}
	}
	return out
}

func commonPrefix(ss []string) string {
	if len(ss) == 0 {
		return ""
	}
	cp := ss[0]
	for _, s := range ss[1:] {
		for !strings.HasPrefix(s, cp) {
			cp = cp[:len(cp)-1]
			if cp == "" {
				return ""
			}
		}
	}
	return cp
}
