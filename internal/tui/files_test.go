package tui

import (
	"strings"
	"testing"
)

func TestFilesCommandEmpty(t *testing.T) {
	m := newTestModel(t)
	if got := joinLines(dispatch(m, `\files`)); !strings.Contains(got, "no SQL files") {
		t.Errorf("\\files (empty) = %q", got)
	}
}

func TestFileCommandsRoundTrip(t *testing.T) {
	m := newTestModel(t)

	// Create a file via the core, then exercise the commands over it.
	if err := m.core.WriteSQLFile("report", "select 1;\n"); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	if got := joinLines(dispatch(m, `\files`)); !strings.Contains(got, "report.sql") {
		t.Errorf("\\files = %q", got)
	}
	if got := joinLines(dispatch(m, `\cat report`)); !strings.Contains(got, "select 1;") {
		t.Errorf("\\cat = %q", got)
	}
	if got := joinLines(dispatch(m, `\copy report report2`)); !strings.Contains(got, "copied report to report2") {
		t.Errorf("\\copy = %q", got)
	}
	if got := joinLines(dispatch(m, `\rename report2 report3`)); !strings.Contains(got, "renamed report2 to report3") {
		t.Errorf("\\rename = %q", got)
	}
	if got := joinLines(dispatch(m, `\delete report3`)); !strings.Contains(got, "deleted report3") {
		t.Errorf("\\delete = %q", got)
	}
}

func TestFileCommandUsageAndErrors(t *testing.T) {
	m := newTestModel(t)
	cases := map[string]string{
		`\cat`:           "usage:",
		`\copy one`:      "usage:",
		`\rename one`:    "usage:",
		`\delete`:        "usage:",
		`\cat nonesuch`:  "error:",
		`\run`:           "usage:",
	}
	for in, want := range cases {
		if got := joinLines(dispatch(m, in)); !strings.Contains(got, want) {
			t.Errorf("dispatch(%q) = %q, want substring %q", in, got, want)
		}
	}
}

func TestRunFileAsync(t *testing.T) {
	m := newTestModel(t)
	if err := m.core.WriteSQLFile("q", "select 1"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	res, act := m.handleLine(`\run q`)
	if act.async == nil {
		t.Fatalf("\\run should be async; res=%v", res.lines)
	}
	// Empty file is a synchronous usage-style message, not a runner.
	_ = m.core.WriteSQLFile("empty", "   \n")
	if _, act := m.handleLine(`\run empty`); act.async != nil {
		t.Error("\\run on an empty file should not produce a runner")
	}
}

func TestEditUnknownEditorReported(t *testing.T) {
	// Force the builtin editor setting, which is not implemented yet, to verify
	// \edit surfaces the error synchronously rather than launching anything.
	m := newTestModel(t)
	st := m.core.Settings()
	_ = st
	// cmdEdit reads settings.Editor; the default is "auto", so instead test the
	// resolver directly for the builtin case.
	if _, _, err := resolveEditor("builtin"); err == nil {
		t.Error("resolveEditor(builtin) should error until Phase 10")
	}
}

func TestResolveEditorPrecedence(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	// Explicit setting wins and splits args.
	cmd, args, err := resolveEditor("code --wait")
	if err != nil || cmd != "code" || len(args) != 1 || args[0] != "--wait" {
		t.Errorf("setting: (%q,%v,%v)", cmd, args, err)
	}
	// auto falls back to $EDITOR.
	t.Setenv("EDITOR", "myed -f")
	cmd, args, _ = resolveEditor("auto")
	if cmd != "myed" || len(args) != 1 || args[0] != "-f" {
		t.Errorf("auto/$EDITOR: (%q,%v)", cmd, args)
	}
}
