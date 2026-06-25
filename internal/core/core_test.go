package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Solifugus/mcli/internal/core/config"
)

func TestOpenCreatesLayoutAndEntersDefault(t *testing.T) {
	root := t.TempDir()
	c, err := Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if c.Current().Name != "default" {
		t.Errorf("current = %q, want default", c.Current().Name)
	}
	if _, err := os.Stat(filepath.Join(root, "workspaces", "default", "workspace.json")); err != nil {
		t.Errorf("default workspace not scaffolded: %v", err)
	}
	// Entering the default workspace should have written a history line.
	data, err := os.ReadFile(filepath.Join(root, "workspaces", "default", "history.log"))
	if err != nil {
		t.Fatalf("read history: %v", err)
	}
	if !strings.Contains(string(data), "ENTER workspace default") {
		t.Errorf("history missing ENTER line:\n%s", data)
	}
}

func TestStartupWorkspaceFallsBackToDefault(t *testing.T) {
	root := t.TempDir()
	// Configure a startup workspace that does not exist.
	writeStartupWorkspace(t, root, "ghost")
	c, err := Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if c.Current().Name != "default" {
		t.Errorf("current = %q, want default fallback", c.Current().Name)
	}
}

func TestWorkspaceLifecycle(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := c.CreateWorkspace("lending"); err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	if err := c.Enter("lending"); err != nil {
		t.Fatalf("Enter: %v", err)
	}
	if c.Current().Name != "lending" {
		t.Fatalf("current = %q, want lending", c.Current().Name)
	}
	// Cannot delete the current workspace.
	if err := c.DeleteWorkspace("lending"); err == nil {
		t.Error("expected error deleting current workspace")
	}
	// Switch away, then delete.
	if err := c.Enter("default"); err != nil {
		t.Fatalf("Enter default: %v", err)
	}
	if err := c.DeleteWorkspace("lending"); err != nil {
		t.Fatalf("DeleteWorkspace: %v", err)
	}
	names, _ := c.ListWorkspaces()
	for _, n := range names {
		if n == "lending" {
			t.Error("lending still listed after delete")
		}
	}
}

func TestRenameCurrentFollowsWorkspace(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := c.CreateWorkspace("old"); err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	if err := c.Enter("old"); err != nil {
		t.Fatalf("Enter: %v", err)
	}
	if err := c.RenameWorkspace("old", "new"); err != nil {
		t.Fatalf("RenameWorkspace: %v", err)
	}
	if c.Current().Name != "new" {
		t.Errorf("current = %q, want new after renaming current workspace", c.Current().Name)
	}
}

// writeStartupWorkspace writes a settings.json with the given startup workspace.
func writeStartupWorkspace(t *testing.T, root, startup string) {
	t.Helper()
	s := config.NewStore(root)
	if err := s.EnsureRoot(); err != nil {
		t.Fatalf("EnsureRoot: %v", err)
	}
	st := config.DefaultSettings()
	st.StartupWorkspace = startup
	if err := s.SaveSettings(st); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
}
