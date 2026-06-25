package tui

import (
	"reflect"
	"testing"
)

func TestCompleteCommandPrefix(t *testing.T) {
	m := newTestModel(t)
	// "\w" → unique "\workspace " (trailing space).
	got, cand := m.complete(`\w`)
	if got != `\workspace ` || cand != nil {
		t.Errorf("complete(%q) = (%q, %v)", `\w`, got, cand)
	}
}

func TestCompleteAmbiguousExtendsCommonPrefix(t *testing.T) {
	m := newTestModel(t)
	// "\" matches \enter \help \quit \workspace → no common prefix beyond "\",
	// so the line is unchanged but candidates are listed.
	got, cand := m.complete(`\`)
	if got != `\` {
		t.Errorf("line changed unexpectedly: %q", got)
	}
	if len(cand) != len(replCommands) {
		t.Errorf("candidates = %v, want all commands", cand)
	}
}

func TestCompleteWorkspaceSubcommand(t *testing.T) {
	m := newTestModel(t)
	got, cand := m.complete(`\workspace cr`)
	if got != `\workspace create ` || cand != nil {
		t.Errorf("got (%q, %v)", got, cand)
	}
}

func TestCompleteWorkspaceNamesForEnter(t *testing.T) {
	m := newTestModel(t)
	if err := m.core.CreateWorkspace("lending"); err != nil {
		t.Fatalf("create: %v", err)
	}
	// "\enter le" → unique "lending".
	got, _ := m.complete(`\enter le`)
	if got != `\enter lending ` {
		t.Errorf("got %q, want %q", got, `\enter lending `)
	}
}

func TestCompleteWorkspaceNamesForDelete(t *testing.T) {
	m := newTestModel(t)
	if err := m.core.CreateWorkspace("archive"); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, _ := m.complete(`\workspace delete ar`)
	if got != `\workspace delete archive ` {
		t.Errorf("got %q, want %q", got, `\workspace delete archive `)
	}
}

func TestCompleteNoMatchLeavesLine(t *testing.T) {
	m := newTestModel(t)
	got, cand := m.complete(`\zzz`)
	if got != `\zzz` || cand != nil {
		t.Errorf("got (%q, %v)", got, cand)
	}
}

func TestCommonPrefix(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{[]string{"create", "create"}, "create"},
		{[]string{"delete", "describe"}, "de"},
		{[]string{"a", "b"}, ""},
		{nil, ""},
	}
	for _, c := range cases {
		if got := commonPrefix(c.in); got != c.want {
			t.Errorf("commonPrefix(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestReplaceLastToken(t *testing.T) {
	if got := replaceLastToken(`\enter le`, false, "lending"); got != `\enter lending` {
		t.Errorf("got %q", got)
	}
	if got := replaceLastToken(`\workspace `, true, "list"); got != `\workspace list` {
		t.Errorf("got %q", got)
	}
	if got := replaceLastToken(`\w`, false, `\workspace`); got != `\workspace` {
		t.Errorf("got %q", got)
	}
}

func TestFilterPrefix(t *testing.T) {
	got := filterPrefix([]string{"create", "delete", "describe"}, "de")
	want := []string{"delete", "describe"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
