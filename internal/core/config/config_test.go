package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestEnsureRootCreatesLayout(t *testing.T) {
	root := t.TempDir()
	s := NewStore(root)
	if err := s.EnsureRoot(); err != nil {
		t.Fatalf("EnsureRoot: %v", err)
	}
	if fi, err := os.Stat(s.WorkspacesDir()); err != nil || !fi.IsDir() {
		t.Fatalf("workspaces dir not created: err=%v", err)
	}
}

func TestEnsureRootEmptyRootErrors(t *testing.T) {
	if err := NewStore("").EnsureRoot(); err == nil {
		t.Fatal("expected error for empty root")
	}
}

func TestLoadDefaultsWhenAbsent(t *testing.T) {
	s := NewStore(t.TempDir())

	got, err := s.LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if want := DefaultSettings(); got != want {
		t.Errorf("settings defaults = %+v, want %+v", got, want)
	}

	srv, err := s.LoadServers()
	if err != nil {
		t.Fatalf("LoadServers: %v", err)
	}
	if srv.Servers == nil {
		t.Error("LoadServers should return a non-nil map when absent")
	}

	ai, err := s.LoadAI()
	if err != nil {
		t.Fatalf("LoadAI: %v", err)
	}
	if !ai.SendSchemaContext || ai.MaxSampleRows != 20 || ai.Providers == nil {
		t.Errorf("ai defaults = %+v", ai)
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	s := NewStore(t.TempDir())
	want := Settings{
		StartupWorkspace:    "consumer-lending",
		ColorPrompt:         false,
		MaxRowsDefault:      1000,
		ConfirmDangerousSQL: false,
		Editor:              "builtin",
	}
	if err := s.SaveSettings(want); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	got, err := s.LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if got != want {
		t.Errorf("round trip = %+v, want %+v", got, want)
	}
}

func TestServersRoundTrip(t *testing.T) {
	s := NewStore(t.TempDir())
	want := ServersConfig{Servers: map[string]Server{
		"local_pg": {
			Type:            "postgres",
			Environment:     "dev",
			Host:            "localhost",
			Port:            5432,
			DefaultDatabase: "postgres",
			User:            "mathew",
			PasswordSource:  "keyring",
		},
		"etl_sqlserver": {
			Type:             "sqlserver",
			Environment:      "prod",
			ConnectionString: "Server=sqlprod01;Database=ETLDB;",
			PasswordSource:   "prompt",
		},
	}}
	if err := s.SaveServers(want); err != nil {
		t.Fatalf("SaveServers: %v", err)
	}
	got, err := s.LoadServers()
	if err != nil {
		t.Fatalf("LoadServers: %v", err)
	}
	if len(got.Servers) != 2 || !reflect.DeepEqual(got.Servers["local_pg"], want.Servers["local_pg"]) {
		t.Errorf("round trip = %+v", got)
	}
}

func TestSaveDoesNotStorePlaintextPassword(t *testing.T) {
	// A Server has no password field at all, so serialized config can never
	// carry a plaintext secret. Guard that invariant against future changes.
	s := NewStore(t.TempDir())
	cfg := ServersConfig{Servers: map[string]Server{
		"x": {Type: "postgres", PasswordSource: "keyring"},
	}}
	if err := s.SaveServers(cfg); err != nil {
		t.Fatalf("SaveServers: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(s.Root, "servers.json"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if containsAny(string(data), "password\"", "\"secret") {
		t.Errorf("servers.json appears to contain a password field:\n%s", data)
	}
}

func TestSavedFilePermissions(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.SaveServers(DefaultServersConfig()); err != nil {
		t.Fatalf("SaveServers: %v", err)
	}
	fi, err := os.Stat(filepath.Join(s.Root, "servers.json"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("servers.json perm = %o, want 600", perm)
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
	}
	return false
}
