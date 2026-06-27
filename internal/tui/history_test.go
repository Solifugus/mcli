package tui

import "testing"

// submitLine drives the full submit path for a typed line.
func submitLine(m *Model, line string) {
	m.input.SetValue(line)
	updated, _ := m.submit()
	*m = updated.(Model)
}

func TestHistoryRingWalk(t *testing.T) {
	m := newTestModel(t)
	submitLine(m, `.workspace list`)
	submitLine(m, `.help`)

	// Up walks newest → oldest.
	m.historyPrev()
	if got := m.input.Value(); got != `.help` {
		t.Fatalf("first Up = %q, want .help", got)
	}
	m.historyPrev()
	if got := m.input.Value(); got != `.workspace list` {
		t.Fatalf("second Up = %q, want .workspace list", got)
	}
	// Further Up is clamped at the oldest entry.
	m.historyPrev()
	if got := m.input.Value(); got != `.workspace list` {
		t.Fatalf("clamped Up = %q", got)
	}
	// Down walks back toward the live draft.
	m.historyNext()
	if got := m.input.Value(); got != `.help` {
		t.Fatalf("Down = %q, want .help", got)
	}
	m.historyNext()
	if got := m.input.Value(); got != "" {
		t.Fatalf("Down to draft = %q, want empty", got)
	}
}

func TestHistoryPreservesDraft(t *testing.T) {
	m := newTestModel(t)
	submitLine(m, `.help`)

	// Start typing a new line, then walk up and back down.
	m.input.SetValue(`.workspace cre`)
	m.historyPrev()
	if got := m.input.Value(); got != `.help` {
		t.Fatalf("Up = %q", got)
	}
	m.historyNext()
	if got := m.input.Value(); got != `.workspace cre` {
		t.Fatalf("draft not restored: %q", got)
	}
}

func TestHistorySkipsConsecutiveDuplicates(t *testing.T) {
	m := newTestModel(t)
	submitLine(m, `.help`)
	submitLine(m, `.help`)
	if len(m.history) != 1 {
		t.Fatalf("history = %v, want one entry", m.history)
	}
}

func TestEmptyLineNotRecorded(t *testing.T) {
	m := newTestModel(t)
	submitLine(m, "   ")
	if len(m.history) != 0 {
		t.Fatalf("blank line recorded: %v", m.history)
	}
}
