package tui

import (
	"strings"
	"testing"
)

func TestGuardedSQLAllowsRead(t *testing.T) {
	m := newTestModel(t)
	res, act := m.handleLine("select 1")
	if act.async == nil || act.confirm != nil {
		t.Errorf("read should be allowed (async), got %+v", act)
	}
	if len(res.lines) != 0 {
		t.Errorf("read should produce no immediate output, got %v", res.lines)
	}
}

func TestGuardedSQLConfirmsDangerous(t *testing.T) {
	m := newTestModel(t)
	// Default settings confirm dangerous SQL.
	_, act := m.handleLine("drop table t")
	if act.confirm == nil {
		t.Fatal("dangerous statement should require confirmation")
	}
	if act.confirm.run == nil {
		t.Error("confirm should carry the deferred runner")
	}
	if !strings.Contains(act.confirm.question, "DROP") {
		t.Errorf("confirm question = %q, want it to mention DROP", act.confirm.question)
	}
}

func TestGuardedSQLBlocksUnderReadOnly(t *testing.T) {
	m := newTestModel(t)
	m.core.SetReadOnly(true)
	res, act := m.handleLine("insert into t values (1)")
	if act.async != nil || act.confirm != nil {
		t.Errorf("write under read-only should not run, got %+v", act)
	}
	if !strings.Contains(joinLines(res), "blocked") {
		t.Errorf("expected a blocked message, got %v", res.lines)
	}
}

func TestReadonlyCommand(t *testing.T) {
	m := newTestModel(t)
	if got := joinLines(dispatch(m, `\readonly`)); !strings.Contains(got, "off") {
		t.Errorf("initial \\readonly = %q, want off", got)
	}
	dispatch(m, `\readonly on`)
	if !m.core.ReadOnly() {
		t.Error("\\readonly on did not engage")
	}
	if got := joinLines(dispatch(m, `\readonly`)); !strings.Contains(got, "on") {
		t.Errorf("\\readonly after on = %q, want on", got)
	}
	dispatch(m, `\readonly off`)
	if m.core.ReadOnly() {
		t.Error("\\readonly off did not disengage")
	}
}

func TestIsYes(t *testing.T) {
	for _, s := range []string{"y", "Y", "yes", "YES", " yes "} {
		if !isYes(s) {
			t.Errorf("isYes(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", "n", "no", "nope", "ya"} {
		if isYes(s) {
			t.Errorf("isYes(%q) = true, want false", s)
		}
	}
}
