// Package config owns the ~/.mcli on-disk layout and the three global config
// files: settings.json, servers.json, and ai.json. It is UI-agnostic and has no
// dependency on the TUI or MCP layers. See docs/mcli-design.md §7–§9.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Store reads and writes mcli's global configuration rooted at a directory
// (normally ~/.mcli). The root is injectable so tests can use a temp dir.
type Store struct {
	Root string
}

// DefaultRoot returns the documented home, ~/.mcli, resolving ~ via
// os.UserHomeDir() (which is correct on Windows, where ~ is not literal).
func DefaultRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".mcli"), nil
}

// NewStore returns a Store rooted at the given directory.
func NewStore(root string) *Store { return &Store{Root: root} }

// WorkspacesDir is the directory holding all per-workspace directories.
func (s *Store) WorkspacesDir() string { return filepath.Join(s.Root, "workspaces") }

func (s *Store) path(name string) string { return filepath.Join(s.Root, name) }

// EnsureRoot creates the root and workspaces/ directories if they are missing.
// It does not create the default workspace; that belongs to the workspace
// manager, which scaffolds workspace contents.
func (s *Store) EnsureRoot() error {
	if s.Root == "" {
		return errors.New("config: empty root")
	}
	if err := os.MkdirAll(s.WorkspacesDir(), 0o755); err != nil {
		return fmt.Errorf("create ~/.mcli layout: %w", err)
	}
	return nil
}

// --- settings.json ---

// Settings holds general CLI preferences (settings.json). See design §9.
type Settings struct {
	StartupWorkspace    string `json:"startup_workspace"`
	ColorPrompt         bool   `json:"color_prompt"`
	MaxRowsDefault      int    `json:"max_rows_default"`
	ConfirmDangerousSQL bool   `json:"confirm_dangerous_sql"`
	Editor              string `json:"editor"`
}

// DefaultSettings returns the documented defaults used on first run.
func DefaultSettings() Settings {
	return Settings{
		StartupWorkspace:    "default",
		ColorPrompt:         true,
		MaxRowsDefault:      500,
		ConfirmDangerousSQL: true,
		Editor:              "auto",
	}
}

// LoadSettings returns settings.json, or DefaultSettings if the file is absent.
func (s *Store) LoadSettings() (Settings, error) {
	return loadJSON(s.path("settings.json"), DefaultSettings())
}

// SaveSettings writes settings.json.
func (s *Store) SaveSettings(v Settings) error {
	return saveJSON(s.path("settings.json"), v)
}

// --- servers.json ---

// Server is a globally configured connection target. Passwords are never stored
// here directly; PasswordSource selects how the secret is obtained at connect
// time (prompt, env:VAR, keyring). See design §7, §9.
type Server struct {
	Type             string `json:"type"`
	Environment      string `json:"environment,omitempty"`
	Host             string `json:"host,omitempty"`
	Port             int    `json:"port,omitempty"`
	DefaultDatabase  string `json:"default_database,omitempty"`
	User             string `json:"user,omitempty"`
	ConnectionString string `json:"connection_string,omitempty"`
	PasswordSource   string `json:"password_source,omitempty"`
}

// ServersConfig is the contents of servers.json.
type ServersConfig struct {
	Servers map[string]Server `json:"servers"`
}

// DefaultServersConfig returns an empty, initialized server registry.
func DefaultServersConfig() ServersConfig {
	return ServersConfig{Servers: map[string]Server{}}
}

// LoadServers returns servers.json, or an empty registry if the file is absent.
// A present-but-null "servers" object is normalized to an empty map.
func (s *Store) LoadServers() (ServersConfig, error) {
	cfg, err := loadJSON(s.path("servers.json"), DefaultServersConfig())
	if err != nil {
		return cfg, err
	}
	if cfg.Servers == nil {
		cfg.Servers = map[string]Server{}
	}
	return cfg, nil
}

// SaveServers writes servers.json.
func (s *Store) SaveServers(v ServersConfig) error {
	return saveJSON(s.path("servers.json"), v)
}

// --- ai.json ---

// AIProvider configures one AI provider. See design §9.
type AIProvider struct {
	BaseURL      string `json:"base_url,omitempty"`
	Model        string `json:"model"`
	APIKeySource string `json:"api_key_source,omitempty"`
}

// AIConfig is the contents of ai.json. AI features are optional.
type AIConfig struct {
	Providers         map[string]AIProvider `json:"providers"`
	DefaultProvider   string                `json:"default_provider,omitempty"`
	SendSchemaContext bool                  `json:"send_schema_context"`
	SendSampleRows    bool                  `json:"send_sample_rows"`
	MaxSampleRows     int                   `json:"max_sample_rows"`
}

// DefaultAIConfig returns the documented defaults: no providers, schema context
// on, sample rows off, 20-row sample cap.
func DefaultAIConfig() AIConfig {
	return AIConfig{
		Providers:         map[string]AIProvider{},
		SendSchemaContext: true,
		SendSampleRows:    false,
		MaxSampleRows:     20,
	}
}

// LoadAI returns ai.json, or DefaultAIConfig if the file is absent.
func (s *Store) LoadAI() (AIConfig, error) {
	cfg, err := loadJSON(s.path("ai.json"), DefaultAIConfig())
	if err != nil {
		return cfg, err
	}
	if cfg.Providers == nil {
		cfg.Providers = map[string]AIProvider{}
	}
	return cfg, nil
}

// SaveAI writes ai.json.
func (s *Store) SaveAI(v AIConfig) error {
	return saveJSON(s.path("ai.json"), v)
}

// --- JSON helpers ---

// loadJSON reads and decodes a JSON file. A missing file is not an error: the
// supplied default is returned so first run works without any config present.
func loadJSON[T any](path string, def T) (T, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return def, nil
	}
	if err != nil {
		return def, fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return def, fmt.Errorf("parse %s: %w", filepath.Base(path), err)
	}
	return v, nil
}

// saveJSON writes v as indented JSON, creating parent dirs as needed. Files are
// written 0600 because servers.json/ai.json may reference connection details.
func saveJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create dir for %s: %w", filepath.Base(path), err)
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", filepath.Base(path), err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	return nil
}
