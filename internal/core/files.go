package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SQL files live inside the current workspace directory. The ".sql" extension
// may be omitted for convenience (§15). All names are validated to stay within
// the workspace — no path separators, no "..".

// workspaceDir is the on-disk directory of the current workspace.
func (c *Core) workspaceDir() string { return c.workspaces.Dir(c.current.Name) }

// SQLFilePath resolves a (possibly extension-less) file name to an absolute path
// inside the current workspace.
func (c *Core) SQLFilePath(name string) (string, error) {
	fn, err := sqlFileName(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(c.workspaceDir(), fn), nil
}

// ListSQLFiles returns the .sql file names in the current workspace, sorted.
func (c *Core) ListSQLFiles() ([]string, error) {
	entries, err := os.ReadDir(c.workspaceDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list files: %w", err)
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

// ReadSQLFile returns the contents of a workspace SQL file.
func (c *Core) ReadSQLFile(name string) (string, error) {
	p, err := c.SQLFilePath(name)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("no such file %q in workspace", name)
		}
		return "", err
	}
	return string(b), nil
}

// WriteSQLFile writes contents to a workspace SQL file, creating it if needed.
func (c *Core) WriteSQLFile(name, content string) error {
	p, err := c.SQLFilePath(name)
	if err != nil {
		return err
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		return err
	}
	c.log("WRITE", "file", filepath.Base(p))
	return nil
}

// EnsureSQLFile creates an empty SQL file if it does not exist, so an editor can
// open it cleanly. It records the edit intent in history.
func (c *Core) EnsureSQLFile(name string) error {
	p, err := c.SQLFilePath(name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			return err
		}
	}
	c.log("EDIT", "file", filepath.Base(p))
	return nil
}

// CopySQLFile copies one workspace SQL file to another.
func (c *Core) CopySQLFile(oldName, newName string) error {
	src, err := c.SQLFilePath(oldName)
	if err != nil {
		return err
	}
	dst, err := c.SQLFilePath(newName)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("no such file %q in workspace", oldName)
		}
		return err
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return err
	}
	c.log("COPY", "file", filepath.Base(src), "to", filepath.Base(dst))
	return nil
}

// RenameSQLFile renames a workspace SQL file.
func (c *Core) RenameSQLFile(oldName, newName string) error {
	src, err := c.SQLFilePath(oldName)
	if err != nil {
		return err
	}
	dst, err := c.SQLFilePath(newName)
	if err != nil {
		return err
	}
	if err := os.Rename(src, dst); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("no such file %q in workspace", oldName)
		}
		return err
	}
	c.log("RENAME", "file", filepath.Base(src), "to", filepath.Base(dst))
	return nil
}

// DeleteSQLFile removes a workspace SQL file.
func (c *Core) DeleteSQLFile(name string) error {
	p, err := c.SQLFilePath(name)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("no such file %q in workspace", name)
		}
		return err
	}
	c.log("DELETE", "file", filepath.Base(p))
	return nil
}

// sqlFileName validates a file name and defaults a missing extension to .sql.
func sqlFileName(name string) (string, error) {
	if name == "" {
		return "", errors.New("empty file name")
	}
	if strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return "", fmt.Errorf("invalid file name %q", name)
	}
	if filepath.Ext(name) == "" {
		name += ".sql"
	}
	return name, nil
}
