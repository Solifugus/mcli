//go:build gui

package main

import (
	"github.com/Solifugus/mcli/internal/core"
	"github.com/Solifugus/mcli/internal/core/config"
	"github.com/Solifugus/mcli/internal/gui"
)

// runGUI opens the core and launches the native front-end. Only compiled into
// the `-tags gui` build; the default binary uses the stub in gui_stub.go. Like
// the TUI and MCP server, the GUI is a thin client of the one *core.Core.
func runGUI() error {
	root, err := config.DefaultRoot()
	if err != nil {
		return err
	}
	c, err := core.Open(root)
	if err != nil {
		return err
	}
	return gui.Run(c, version)
}
