// Command mcli is a multi-database command-line workbench. One binary selects
// its run mode by invocation: the interactive TUI by default, or a headless
// stdio MCP server via `mcli mcp serve`. See docs/mcli-design.md §5.
package main

import (
	"fmt"
	"os"

	_ "github.com/Solifugus/mcli/internal/adapters" // register default DB adapters
	"github.com/Solifugus/mcli/internal/core"
	"github.com/Solifugus/mcli/internal/core/config"
	"github.com/Solifugus/mcli/internal/tui"
)

// version is the build version. Wire this to ldflags / build info later.
const version = "0.0.0-dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "mcli:", err)
		os.Exit(1)
	}
}

// run dispatches by invocation. Kept separate from main so it is testable and
// returns an error rather than calling os.Exit.
func run(args []string) error {
	if len(args) == 0 {
		return runTUI()
	}

	switch args[0] {
	case "mcp":
		if len(args) >= 2 && args[1] == "serve" {
			return runMCP()
		}
		return fmt.Errorf("unknown mcp subcommand %q; try: mcli mcp serve", joinRest(args[1:]))
	case "help", "-h", "--help":
		printUsage()
		return nil
	case "version", "-v", "--version":
		fmt.Println("mcli", version)
		return nil
	default:
		return fmt.Errorf("unknown command %q; try: mcli help", args[0])
	}
}

func joinRest(args []string) string {
	if len(args) == 0 {
		return ""
	}
	out := args[0]
	for _, a := range args[1:] {
		out += " " + a
	}
	return out
}

func printUsage() {
	fmt.Print(`mcli — multi-database command-line workbench

Usage:
  mcli                 launch the interactive TUI (default)
  mcli mcp serve       run the headless stdio MCP server
  mcli help            show this help
  mcli version         print the version
`)
}

// runTUI opens the core and launches the Bubble Tea v2 front-end.
func runTUI() error {
	root, err := config.DefaultRoot()
	if err != nil {
		return err
	}
	c, err := core.Open(root)
	if err != nil {
		return err
	}
	return tui.Run(c)
}

// runMCP runs the headless MCP server over stdio. Implemented in Phase 9.
func runMCP() error {
	return fmt.Errorf("MCP server not yet implemented (Phase 9); see PLAN.md")
}
