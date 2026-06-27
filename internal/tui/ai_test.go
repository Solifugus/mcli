package tui

import (
	"strings"
	"testing"
)

func TestAIUsage(t *testing.T) {
	m := newTestModel(t)
	if got := joinLines(dispatch(m, `.ai`)); !strings.Contains(got, "usage") {
		t.Errorf(".ai with no args = %q, want usage", got)
	}
	if got := joinLines(dispatch(m, `.ai ask`)); !strings.Contains(got, "usage") {
		t.Errorf(".ai ask with no question = %q, want usage", got)
	}
	if got := joinLines(dispatch(m, `.ai bogus`)); !strings.Contains(got, "unknown") {
		t.Errorf(".ai bogus = %q, want unknown", got)
	}
}

func TestAIProvidersEmpty(t *testing.T) {
	m := newTestModel(t)
	if got := joinLines(dispatch(m, `.ai providers`)); !strings.Contains(got, "no AI providers") {
		t.Errorf(".ai providers (none) = %q", got)
	}
}

func TestAIExplainCurrentWithoutSQL(t *testing.T) {
	m := newTestModel(t)
	res, act := m.handleLine(`.ai explain current`)
	if act.async != nil {
		t.Error("explain current with no prior SQL should not run a background op")
	}
	if !strings.Contains(joinLines(res), "no current SQL") {
		t.Errorf("explain current = %q, want 'no current SQL'", res.lines)
	}
}

func TestAITargetSQLCurrent(t *testing.T) {
	m := newTestModel(t)
	m.lastSQL = "select 1"
	sql, _, ok := m.aiTargetSQL([]string{"current"})
	if !ok || sql != "select 1" {
		t.Errorf("aiTargetSQL(current) = (%q, %v)", sql, ok)
	}
}

func TestAITargetSQLFile(t *testing.T) {
	m := newTestModel(t)
	if err := m.core.WriteSQLFile("q1", "select 42"); err != nil {
		t.Fatalf("WriteSQLFile: %v", err)
	}
	sql, _, ok := m.aiTargetSQL([]string{"q1"})
	if !ok || strings.TrimSpace(sql) != "select 42" {
		t.Errorf("aiTargetSQL(q1) = (%q, %v)", sql, ok)
	}
	if _, _, ok := m.aiTargetSQL([]string{"missing"}); ok {
		t.Error("aiTargetSQL on a missing file should fail")
	}
}

// TestLastSQLTracking confirms a submitted statement is remembered for
// .ai explain current.
func TestLastSQLTracking(t *testing.T) {
	m := newTestModel(t)
	m.handleLine("select * from t")
	if m.lastSQL != "select * from t" {
		t.Errorf("lastSQL = %q, want the submitted statement", m.lastSQL)
	}
}

func TestAIHelpHasExamples(t *testing.T) {
	m := newTestModel(t)
	res, act := m.handleLine(`.ai help`)
	if act.async != nil {
		t.Error(`.ai help should be synchronous, not a network call`)
	}
	body := joinLines(res)
	for _, want := range []string{`.ai ask`, `.ai explain current`, `.ai fix current`, "providers"} {
		if !strings.Contains(body, want) {
			t.Errorf(".ai help missing %q", want)
		}
	}
}
