// Package core is mcli's UI-agnostic domain facade. Both front-ends (the TUI and
// the MCP server) drive the application through a *Core; neither owns domain
// logic. The core bundles the config store, the workspace manager, the active
// workspace, and that workspace's history log. See docs/mcli-design.md §5, §23.
package core

import (
	"fmt"

	"github.com/Solifugus/mcli/internal/core/adapter"
	"github.com/Solifugus/mcli/internal/core/config"
	"github.com/Solifugus/mcli/internal/core/history"
	"github.com/Solifugus/mcli/internal/core/workspace"
)

// Core is the shared application state and entry point for both front-ends.
type Core struct {
	cfg        *config.Store
	workspaces *workspace.Manager
	settings   config.Settings
	servers    config.ServersConfig

	current workspace.Workspace
	hist    *history.Log

	// Live connection state (nil when disconnected).
	conn       adapter.Adapter
	connServer string
	dialect    adapter.Dialect
}

// Open initializes mcli rooted at the given ~/.mcli directory: it ensures the
// on-disk layout, loads settings, guarantees the default workspace exists, and
// enters the configured startup workspace (falling back to default).
func Open(root string) (*Core, error) {
	cfg := config.NewStore(root)
	if err := cfg.EnsureRoot(); err != nil {
		return nil, err
	}
	settings, err := cfg.LoadSettings()
	if err != nil {
		return nil, err
	}
	servers, err := cfg.LoadServers()
	if err != nil {
		return nil, err
	}
	wm := workspace.NewManager(root)
	if _, err := wm.EnsureDefault(); err != nil {
		return nil, err
	}

	c := &Core{cfg: cfg, workspaces: wm, settings: settings, servers: servers}

	start := settings.StartupWorkspace
	if start == "" || !wm.Exists(start) {
		start = workspace.DefaultName
	}
	if err := c.Enter(start); err != nil {
		return nil, err
	}
	return c, nil
}

// Current returns the active workspace.
func (c *Core) Current() workspace.Workspace { return c.current }

// Settings returns the loaded CLI settings.
func (c *Core) Settings() config.Settings { return c.settings }

// Enter switches the active workspace: it loads the workspace, makes it current,
// points history logging at that workspace's log, and records the entry.
func (c *Core) Enter(name string) error {
	ws, err := c.workspaces.Load(name)
	if err != nil {
		return err
	}
	c.current = ws
	c.hist = history.New(c.workspaces.HistoryPath(name))
	return c.hist.Append("ENTER", "workspace", name)
}

// ListWorkspaces returns all workspace names, sorted.
func (c *Core) ListWorkspaces() ([]string, error) { return c.workspaces.List() }

// CreateWorkspace scaffolds a new workspace.
func (c *Core) CreateWorkspace(name string) error {
	if _, err := c.workspaces.Create(name); err != nil {
		return err
	}
	c.log("CREATE", "workspace", name)
	return nil
}

// RenameWorkspace renames a workspace, following the rename if it is current.
func (c *Core) RenameWorkspace(oldName, newName string) error {
	if err := c.workspaces.Rename(oldName, newName); err != nil {
		return err
	}
	c.log("RENAME", "workspace", oldName, "to", newName)
	if c.current.Name == oldName {
		return c.Enter(newName)
	}
	return nil
}

// DeleteWorkspace removes a workspace. The current workspace cannot be deleted;
// enter another one first.
func (c *Core) DeleteWorkspace(name string) error {
	if name == c.current.Name {
		return fmt.Errorf("cannot delete the current workspace %q; enter another first", name)
	}
	if err := c.workspaces.Delete(name); err != nil {
		return err
	}
	c.log("DELETE", "workspace", name)
	return nil
}

// log appends to the current workspace's history, ignoring write errors (a
// failed log line must never abort a user action).
func (c *Core) log(action string, args ...string) {
	if c.hist != nil {
		_ = c.hist.Append(action, args...)
	}
}
