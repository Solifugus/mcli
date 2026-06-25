package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// loadJSON decodes a workspace.json file. Unlike the config package, a missing
// file is surfaced as os.ErrNotExist so callers can distinguish "no such
// workspace" from a real read error.
func loadJSON(path string) (Workspace, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Workspace{}, err
	}
	var ws Workspace
	if err := json.Unmarshal(data, &ws); err != nil {
		return Workspace{}, fmt.Errorf("parse %s: %w", filepath.Base(path), err)
	}
	return ws, nil
}

func saveJSON(path string, ws Workspace) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create workspace dir: %w", err)
	}
	data, err := json.MarshalIndent(ws, "", "  ")
	if err != nil {
		return fmt.Errorf("encode workspace.json: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write workspace.json: %w", err)
	}
	return nil
}
