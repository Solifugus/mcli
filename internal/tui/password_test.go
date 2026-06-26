package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/Solifugus/mcli/internal/core/config"
)

// TestConnectPromptsForPassword verifies that connecting to a prompt-source
// server yields a masked password prompt (no network needed: resolution fails
// fast with ErrPasswordRequired before dialing).
func TestConnectPromptsForPassword(t *testing.T) {
	m := newTestModel(t)
	if err := m.core.AddServer("pg", config.Server{Type: "postgres", PasswordSource: "prompt"}); err != nil {
		t.Fatalf("AddServer: %v", err)
	}

	_, act := m.handleLine(`\connect pg`)
	if act.async == nil {
		t.Fatal("\\connect should produce a background op")
	}
	msg := act.async(context.Background())
	if msg.pwPrompt == nil {
		t.Fatalf("prompt-source connect should request a password; got %+v", msg)
	}
	if !strings.Contains(msg.pwPrompt.label, "password for pg") {
		t.Errorf("password prompt label = %q", msg.pwPrompt.label)
	}

	// Delivering the pwPrompt to the async handler enters a masked sub-prompt.
	nm, _ := m.handleAsyncResult(msg)
	mm := nm.(Model)
	if mm.mode != modePrompt || mm.pending == nil {
		t.Fatal("pwPrompt should enter a sub-prompt")
	}
	if !mm.pending.mask {
		t.Error("password prompt should mask input")
	}
}

// TestSetPasswordPromptsMasked checks the \server set-password flow opens a
// masked prompt.
func TestSetPasswordPromptsMasked(t *testing.T) {
	m := newTestModel(t)
	if err := m.core.AddServer("pg", config.Server{Type: "postgres"}); err != nil {
		t.Fatalf("AddServer: %v", err)
	}
	_, act := m.handleLine(`\server set-password pg`)
	if act.prompt == nil {
		t.Fatal("set-password should open a prompt")
	}
	if !act.prompt.mask {
		t.Error("set-password prompt should mask input")
	}
}

// TestViewMasksPasswordInput renders the prompt view and confirms the typed
// secret is shown as asterisks, not plaintext.
func TestViewMasksPasswordInput(t *testing.T) {
	m := newTestModel(t)
	m.startPrompt(pending{label: "pw: ", mask: true})
	m.input.SetValue("hunter2")
	s := m.View().Content
	if strings.Contains(s, "hunter2") {
		t.Errorf("masked view leaked the secret: %q", s)
	}
	if !strings.Contains(s, "*******") {
		t.Errorf("masked view = %q, want asterisks", s)
	}
}
