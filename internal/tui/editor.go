package tui

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// editDoneMsg is delivered when the external editor process exits.
type editDoneMsg struct {
	name string
	err  error
}

// cmdEdit opens a workspace SQL file in the external editor via a tea.ExecProcess
// round-trip: the TUI suspends, the editor takes the terminal, and the REPL
// resumes on exit (§11). The file is created if missing so the editor opens
// cleanly. This is not a "mode" — it is a one-shot command.
func (m *Model) cmdEdit(args []string) (cmdResult, tea.Cmd) {
	if len(args) < 1 {
		return out(`usage: \edit <name>`), nil
	}
	name := args[0]
	path, err := m.core.SQLFilePath(name)
	if err != nil {
		return errOut(err), nil
	}
	if err := m.core.EnsureSQLFile(name); err != nil {
		return errOut(err), nil
	}
	editor, eargs, err := resolveEditor(m.core.Settings().Editor)
	if err != nil {
		return errOut(err), nil
	}
	c := exec.Command(editor, append(eargs, path)...) //nolint:gosec // user-configured editor
	return cmdResult{}, tea.ExecProcess(c, func(e error) tea.Msg {
		return editDoneMsg{name: name, err: e}
	})
}

// resolveEditor determines the editor command (and any leading args) to launch
// for \edit, per §11. The setting wins; then $VISUAL, $EDITOR, then a platform
// default. A multi-word setting like "code --wait" is split into command+args.
// The "builtin" setting is reserved for the future internal editor (Phase 10).
func resolveEditor(setting string) (cmd string, args []string, err error) {
	switch setting {
	case "builtin":
		return "", nil, fmt.Errorf(`the built-in editor is not implemented yet (Phase 10); set "editor" to "auto" or a command`)
	case "", "auto":
		// fall through to environment / platform default
	default:
		fields := strings.Fields(setting)
		return fields[0], fields[1:], nil
	}

	if v := os.Getenv("VISUAL"); v != "" {
		fields := strings.Fields(v)
		return fields[0], fields[1:], nil
	}
	if e := os.Getenv("EDITOR"); e != "" {
		fields := strings.Fields(e)
		return fields[0], fields[1:], nil
	}
	return platformDefaultEditor(), nil, nil
}

// platformDefaultEditor picks a reasonable editor when none is configured. The
// design warns not to assume a Unix editor exists on Windows (§4, §11).
func platformDefaultEditor() string {
	if runtime.GOOS == "windows" {
		return "notepad"
	}
	if p, err := exec.LookPath("nano"); err == nil {
		return p
	}
	return "vi"
}
