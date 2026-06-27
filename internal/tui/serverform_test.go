package tui

import (
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Solifugus/mcli/internal/core/config"

	// Register real adapters so type validation has known types.
	_ "github.com/Solifugus/mcli/internal/adapters"
)

// feedPromptLine simulates typing text and pressing Enter at an active prompt.
func feedPromptLine(m *Model, text string) {
	m.input.SetValue(text)
	nm, _ := m.handlePromptKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	*m = nm.(Model)
}

// cancelPrompt simulates pressing Esc at an active prompt.
func cancelPrompt(m *Model) {
	nm, _ := m.handlePromptKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	*m = nm.(Model)
}

func TestServerFromVals(t *testing.T) {
	s, err := serverFromVals(map[string]string{
		"type":            "postgres",
		"environment":     "prod",
		"host":            "db1",
		"port":            "5433",
		"database":        "etl",
		"user":            "mat",
		"password_source": "env:PGPASS",
		"options":         "sslmode=require, application_name=mcli",
	})
	if err != nil {
		t.Fatalf("serverFromVals: %v", err)
	}
	want := config.Server{
		Type: "postgres", Environment: "prod", Host: "db1", Port: 5433,
		DefaultDatabase: "etl", User: "mat", PasswordSource: "env:PGPASS",
		Options: map[string]string{"sslmode": "require", "application_name": "mcli"},
	}
	if !reflect.DeepEqual(s, want) {
		t.Errorf("serverFromVals = %+v, want %+v", s, want)
	}
}

func TestServerFromValsBlankPort(t *testing.T) {
	s, err := serverFromVals(map[string]string{"type": "mysql", "port": ""})
	if err != nil {
		t.Fatalf("serverFromVals: %v", err)
	}
	if s.Port != 0 {
		t.Errorf("blank port = %d, want 0 (driver default)", s.Port)
	}
}

func TestParseOptions(t *testing.T) {
	got, err := parseOptions("a=1, b=2 ,c=")
	if err != nil {
		t.Fatalf("parseOptions: %v", err)
	}
	want := map[string]string{"a": "1", "b": "2", "c": ""}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseOptions = %v, want %v", got, want)
	}
	if _, err := parseOptions("noequals"); err == nil {
		t.Error("option without = should error")
	}
}

func TestValidators(t *testing.T) {
	if err := validateType("postgres"); err != nil {
		t.Errorf("postgres should be a valid type: %v", err)
	}
	if err := validateType("nope"); err == nil {
		t.Error("unknown type should fail validation")
	}
	for _, ok := range []string{"", "none", "prompt", "keyring", "env:X"} {
		if err := validatePasswordSource(ok); err != nil {
			t.Errorf("password source %q should be valid: %v", ok, err)
		}
	}
	for _, bad := range []string{"env:", "secret", "vault"} {
		if err := validatePasswordSource(bad); err == nil {
			t.Errorf("password source %q should be invalid", bad)
		}
	}
	if err := validatePort("70000"); err == nil {
		t.Error("port 70000 should be invalid")
	}
	if err := validatePort(""); err != nil {
		t.Error("blank port should be valid")
	}
}

// TestServerAddWizardFlow drives the wizard end to end through the prompt
// primitive, verifying the server is created.
func TestServerAddWizardFlow(t *testing.T) {
	m := newTestModel(t)
	res, act := m.handleLine(`.server add pg`)
	if act.prompt == nil {
		t.Fatalf(".server add should start a wizard prompt; res=%v", res.lines)
	}
	m.startPrompt(*act.prompt)

	// Answer each field; "" takes the default.
	answers := []string{
		"postgres", // type
		"dev",      // environment
		"localhost", // host
		"",          // port (default blank)
		"appdb",     // database
		"mat",       // user
		"prompt",    // password source
		"",          // options
	}
	for i, a := range answers {
		if m.pending == nil {
			t.Fatalf("wizard ended early at step %d", i)
		}
		feedPromptLine(m, a)
	}
	if m.pending != nil {
		t.Fatal("wizard did not finish after all fields")
	}
	s, ok := m.core.Server("pg")
	if !ok {
		t.Fatal("server pg was not created")
	}
	if s.Type != "postgres" || s.DefaultDatabase != "appdb" || s.User != "mat" {
		t.Errorf("created server = %+v", s)
	}
}

// TestServerAddWizardCancel verifies Esc aborts without creating a server.
func TestServerAddWizardCancel(t *testing.T) {
	m := newTestModel(t)
	_, act := m.handleLine(`.server add pg`)
	if act.prompt == nil {
		t.Fatal("expected wizard prompt")
	}
	m.startPrompt(*act.prompt)
	cancelPrompt(m)
	if m.pending != nil {
		t.Error("cancel should clear the pending prompt")
	}
	if _, ok := m.core.Server("pg"); ok {
		t.Error("canceled wizard should not create a server")
	}
}
