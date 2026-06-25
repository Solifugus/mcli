// Package workspace manages mcli's named working contexts. A workspace is
// task-oriented (not database-oriented): it remembers a current server and
// database, default import/export folders, and owns its SQL files and history
// log. It is UI-agnostic. See docs/mcli-design.md §7, §10, §12.
package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DefaultName is the workspace that always exists; mcli starts here unless
// configured otherwise.
const DefaultName = "default"

// Workspace is the durable working context stored in workspace.json. It is kept
// intentionally small — durable context, not every transient state detail.
type Workspace struct {
	Name            string `json:"name"`
	CurrentServer   string `json:"current_server,omitempty"`
	CurrentDatabase string `json:"current_database,omitempty"`
	AutoConnect     bool   `json:"auto_connect"`
	ImportDir       string `json:"import_dir"`
	ExportDir       string `json:"export_dir"`
}

// Manager creates and resolves workspaces under <root>/workspaces. The root is
// normally ~/.mcli; it is injectable so tests can use a temp dir.
type Manager struct {
	root string
}

// NewManager returns a Manager rooted at the given ~/.mcli directory.
func NewManager(root string) *Manager { return &Manager{root: root} }

// Dir returns the on-disk directory for a workspace.
func (m *Manager) Dir(name string) string {
	return filepath.Join(m.root, "workspaces", name)
}

// HistoryPath returns the path to a workspace's history log.
func (m *Manager) HistoryPath(name string) string {
	return filepath.Join(m.Dir(name), "history.log")
}

func (m *Manager) configPath(name string) string {
	return filepath.Join(m.Dir(name), "workspace.json")
}

// Exists reports whether a workspace with a valid workspace.json is present.
func (m *Manager) Exists(name string) bool {
	if validName(name) != nil {
		return false
	}
	_, err := os.Stat(m.configPath(name))
	return err == nil
}

// EnsureDefault creates the default workspace if it does not already exist and
// returns it. Safe to call on every startup.
func (m *Manager) EnsureDefault() (Workspace, error) {
	if m.Exists(DefaultName) {
		return m.Load(DefaultName)
	}
	return m.Create(DefaultName)
}

// Create scaffolds a new workspace directory (with import/export subfolders) and
// writes its workspace.json. It errors if the workspace already exists.
func (m *Manager) Create(name string) (Workspace, error) {
	if err := validName(name); err != nil {
		return Workspace{}, err
	}
	if m.Exists(name) {
		return Workspace{}, fmt.Errorf("workspace %q already exists", name)
	}
	ws := Workspace{
		Name:        name,
		AutoConnect: false,
		ImportDir:   "imports",
		ExportDir:   "exports",
	}
	if err := m.scaffold(ws); err != nil {
		return Workspace{}, err
	}
	return ws, nil
}

func (m *Manager) scaffold(ws Workspace) error {
	dir := m.Dir(ws.Name)
	for _, sub := range []string{"", ws.ImportDir, ws.ExportDir} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return fmt.Errorf("create workspace %q: %w", ws.Name, err)
		}
	}
	return m.Save(ws)
}

// Load reads a workspace's workspace.json.
func (m *Manager) Load(name string) (Workspace, error) {
	if err := validName(name); err != nil {
		return Workspace{}, err
	}
	ws, err := loadJSON(m.configPath(name))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Workspace{}, fmt.Errorf("workspace %q does not exist", name)
		}
		return Workspace{}, err
	}
	return ws, nil
}

// Save writes a workspace's workspace.json. The workspace directory must exist.
func (m *Manager) Save(ws Workspace) error {
	if err := validName(ws.Name); err != nil {
		return err
	}
	return saveJSON(m.configPath(ws.Name), ws)
}

// List returns the names of all workspaces, sorted.
func (m *Manager) List() ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(m.root, "workspaces"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() && m.Exists(e.Name()) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// Rename moves a workspace directory and updates the name in its workspace.json.
// The default workspace cannot be renamed.
func (m *Manager) Rename(oldName, newName string) error {
	if oldName == DefaultName {
		return errors.New("the default workspace cannot be renamed")
	}
	if err := validName(oldName); err != nil {
		return err
	}
	if err := validName(newName); err != nil {
		return err
	}
	if !m.Exists(oldName) {
		return fmt.Errorf("workspace %q does not exist", oldName)
	}
	if m.Exists(newName) {
		return fmt.Errorf("workspace %q already exists", newName)
	}
	if err := os.Rename(m.Dir(oldName), m.Dir(newName)); err != nil {
		return fmt.Errorf("rename workspace: %w", err)
	}
	ws, err := m.Load(newName)
	if err != nil {
		return err
	}
	ws.Name = newName
	return m.Save(ws)
}

// Delete removes a workspace directory and all its contents. The default
// workspace cannot be deleted.
func (m *Manager) Delete(name string) error {
	if name == DefaultName {
		return errors.New("the default workspace cannot be deleted")
	}
	if err := validName(name); err != nil {
		return err
	}
	if !m.Exists(name) {
		return fmt.Errorf("workspace %q does not exist", name)
	}
	return os.RemoveAll(m.Dir(name))
}

// validName rejects empty names and anything that could escape the workspaces
// directory (path separators, "." / "..", or names containing "..").
func validName(name string) error {
	if name == "" {
		return errors.New("workspace name is empty")
	}
	if name == "." || name == ".." ||
		strings.ContainsAny(name, `/\`) ||
		strings.Contains(name, "..") {
		return fmt.Errorf("invalid workspace name %q", name)
	}
	return nil
}
