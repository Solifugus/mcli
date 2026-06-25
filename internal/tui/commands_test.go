package tui

import (
	"context"
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

// dispatch runs a synchronous command and returns its immediate result. Async
// commands (which return a runner) are not exercised through this helper.
func dispatch(m *Model, line string) cmdResult {
	r, _ := m.handleLine(line)
	return r
}

func TestDispatchQuit(t *testing.T) {
	m := newTestModel(t)
	for _, in := range []string{`\quit`, `\q`, `\exit`} {
		if r := dispatch(m, in); !r.quit {
			t.Errorf("dispatch(%q) quit = false, want true", in)
		}
	}
}

func TestDispatchUnknownCommand(t *testing.T) {
	m := newTestModel(t)
	r := dispatch(m, `\bogus`)
	if !strings.Contains(joinLines(r), "unknown command") {
		t.Errorf("got %q", joinLines(r))
	}
}

func TestBareInputIsAsyncSQL(t *testing.T) {
	m := newTestModel(t)
	res, run := m.handleLine("select 1")
	if run == nil {
		t.Fatal("bare SQL should produce an async runner")
	}
	if len(res.lines) != 0 {
		t.Errorf("unexpected immediate output: %v", res.lines)
	}
	// With no connection, executing the runner reports an error.
	if msg := run(context.Background()); msg.err == nil {
		t.Error("running SQL with no connection should error")
	}
}

func TestConnectUsageAndUnknownServer(t *testing.T) {
	m := newTestModel(t)
	// Missing argument is a synchronous usage error (nil runner).
	if res, run := m.handleLine(`\connect`); run != nil || !strings.Contains(joinLines(res), "usage:") {
		t.Errorf("connect usage: res=%q run=%v", joinLines(res), run != nil)
	}
	// Unknown server errors when the runner executes.
	_, run := m.handleLine(`\connect ghost`)
	if run == nil {
		t.Fatal("expected a runner for \\connect <name>")
	}
	if msg := run(context.Background()); msg.err == nil {
		t.Error("connecting to unknown server should error")
	}
}

func TestWorkspaceCreateListEnter(t *testing.T) {
	m := newTestModel(t)

	if r := dispatch(m, `\workspace create lending`); !strings.Contains(joinLines(r), "created workspace lending") {
		t.Fatalf("create: %q", joinLines(r))
	}

	// list shows default + lending, with default marked current.
	r := dispatch(m, `\workspace list`)
	got := joinLines(r)
	if !strings.Contains(got, "* default") || !strings.Contains(got, "lending") {
		t.Fatalf("list: %q", got)
	}

	// enter lending, then list marks lending current.
	if r := dispatch(m, `\enter lending`); !strings.Contains(joinLines(r), "entered workspace lending") {
		t.Fatalf("enter: %q", joinLines(r))
	}
	if !strings.Contains(joinLines(dispatch(m, `\workspace list`)), "* lending") {
		t.Fatalf("list after enter did not mark lending current")
	}
}

func TestWorkspaceStatus(t *testing.T) {
	m := newTestModel(t)
	got := joinLines(dispatch(m, `\workspace status`))
	if !strings.Contains(got, "workspace: default") || !strings.Contains(got, "server:    (none)") {
		t.Errorf("status: %q", got)
	}
}

func TestEnterUnknownWorkspaceErrors(t *testing.T) {
	m := newTestModel(t)
	if !strings.Contains(joinLines(dispatch(m, `\enter ghost`)), "error:") {
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
		if got := joinLines(dispatch(m, in)); !strings.Contains(got, want) {
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
