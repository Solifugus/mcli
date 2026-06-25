package tui

import (
	"strings"
	"testing"

	"github.com/Solifugus/mcli/internal/core"
)

// newTestModel opens a core in a temp dir and returns a model wired to it.
func newTestModel(t *testing.T) *Model {
	t.Helper()
	c, err := core.Open(t.TempDir())
	if err != nil {
		t.Fatalf("core.Open: %v", err)
	}
	m := New(c)
	return &m
}

func joinLines(r cmdResult) string { return strings.Join(r.lines, "\n") }

func TestDispatchQuit(t *testing.T) {
	m := newTestModel(t)
	for _, in := range []string{`\quit`, `\q`, `\exit`} {
		if r := m.dispatch(in); !r.quit {
			t.Errorf("dispatch(%q) quit = false, want true", in)
		}
	}
}

func TestDispatchUnknownCommand(t *testing.T) {
	m := newTestModel(t)
	r := m.dispatch(`\bogus`)
	if !strings.Contains(joinLines(r), "unknown command") {
		t.Errorf("got %q", joinLines(r))
	}
}

func TestDispatchBareInputNotConnected(t *testing.T) {
	m := newTestModel(t)
	r := m.dispatch("select 1")
	if !strings.Contains(joinLines(r), "not connected") {
		t.Errorf("got %q", joinLines(r))
	}
}

func TestWorkspaceCreateListEnter(t *testing.T) {
	m := newTestModel(t)

	if r := m.dispatch(`\workspace create lending`); !strings.Contains(joinLines(r), "created workspace lending") {
		t.Fatalf("create: %q", joinLines(r))
	}

	// list shows default + lending, with default marked current.
	r := m.dispatch(`\workspace list`)
	got := joinLines(r)
	if !strings.Contains(got, "* default") || !strings.Contains(got, "lending") {
		t.Fatalf("list: %q", got)
	}

	// enter lending, then list marks lending current.
	if r := m.dispatch(`\enter lending`); !strings.Contains(joinLines(r), "entered workspace lending") {
		t.Fatalf("enter: %q", joinLines(r))
	}
	if !strings.Contains(joinLines(m.dispatch(`\workspace list`)), "* lending") {
		t.Fatalf("list after enter did not mark lending current")
	}
}

func TestWorkspaceStatus(t *testing.T) {
	m := newTestModel(t)
	got := joinLines(m.dispatch(`\workspace status`))
	if !strings.Contains(got, "workspace: default") || !strings.Contains(got, "server:    (none)") {
		t.Errorf("status: %q", got)
	}
}

func TestEnterUnknownWorkspaceErrors(t *testing.T) {
	m := newTestModel(t)
	if !strings.Contains(joinLines(m.dispatch(`\enter ghost`)), "error:") {
		t.Error("entering unknown workspace should report an error")
	}
}

func TestWorkspaceUsageMessages(t *testing.T) {
	m := newTestModel(t)
	cases := map[string]string{
		`\workspace`:               "usage:",
		`\workspace create`:        "usage:",
		`\workspace rename only`:   "usage:",
		`\workspace delete`:        "usage:",
		`\enter`:                   "usage:",
		`\workspace frobnicate`:    "unknown",
	}
	for in, want := range cases {
		if got := joinLines(m.dispatch(in)); !strings.Contains(got, want) {
			t.Errorf("dispatch(%q) = %q, want substring %q", in, got, want)
		}
	}
}

func TestPromptStringReflectsWorkspace(t *testing.T) {
	m := newTestModel(t)
	if got := m.promptString(); got != "default> " {
		t.Errorf("prompt = %q, want %q", got, "default> ")
	}
}
