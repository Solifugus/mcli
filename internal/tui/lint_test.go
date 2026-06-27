package tui

import (
	"strings"
	"testing"
)

func TestLintCurrentNoSQL(t *testing.T) {
	m := newTestModel(t)
	res, act := m.handleLine(`.lint current`)
	if act.async != nil {
		t.Error("static lint should be synchronous")
	}
	if !strings.Contains(joinLines(res), "no current SQL") {
		t.Errorf("got %q", joinLines(res))
	}
}

func TestLintFileReportsIssues(t *testing.T) {
	m := newTestModel(t)
	if err := m.core.WriteSQLFile("risky", "delete from users"); err != nil {
		t.Fatal(err)
	}
	res, act := m.handleLine(`.lint risky`)
	if act.async != nil {
		t.Error("static lint should be synchronous")
	}
	body := joinLines(res)
	if !strings.Contains(body, "dangerous-sql") {
		t.Errorf("expected a dangerous-sql finding, got %q", body)
	}
}

func TestLintClean(t *testing.T) {
	m := newTestModel(t)
	if err := m.core.WriteSQLFile("ok", "select id from t where id = 1"); err != nil {
		t.Fatal(err)
	}
	res, _ := m.handleLine(`.lint ok`)
	if !strings.Contains(joinLines(res), "no issues") {
		t.Errorf("clean file should report no issues, got %q", joinLines(res))
	}
}

func TestLintLiveSkippedWhenDisconnected(t *testing.T) {
	m := newTestModel(t)
	if err := m.core.WriteSQLFile("q", "select 1"); err != nil {
		t.Fatal(err)
	}
	res, act := m.handleLine(`.lint q live`)
	if act.async != nil {
		t.Error("live lint must not launch a background op when disconnected")
	}
	if !strings.Contains(joinLines(res), "not connected") {
		t.Errorf("expected a not-connected note, got %q", joinLines(res))
	}
}
