package tui

import (
	"strings"
	"testing"
)

// TestHelpPlainListsEveryCommand checks that with color off the help text is
// unstyled and still names every command from the section tables.
func TestHelpPlainListsEveryCommand(t *testing.T) {
	m := newTestModel(t)
	m.colorPrompt = false
	m.width = 100
	out := joinLines(m.helpText())
	if strings.Contains(out, "\x1b[") {
		t.Errorf("plain help should contain no ANSI: %q", out)
	}
	for _, sec := range helpSections {
		if !strings.Contains(out, sec.title) {
			t.Errorf("help missing section %q", sec.title)
		}
		for _, e := range sec.entries {
			if !strings.Contains(out, e.name) || !strings.Contains(out, e.desc) {
				t.Errorf("help missing entry %q / %q", e.name, e.desc)
			}
		}
	}
}

// TestHelpColorStylesAndPreservesText confirms color mode emits ANSI but the
// visible text is unchanged from the plain rendering.
func TestHelpColorStylesAndPreservesText(t *testing.T) {
	m := newTestModel(t)
	m.width = 100

	m.colorPrompt = false
	plain := joinLines(m.helpText())

	m.colorPrompt = true
	colored := joinLines(m.helpText())
	if !strings.Contains(colored, "\x1b[") {
		t.Error("colored help should contain ANSI")
	}
	// Striped rows are padded to full width, so compare line-by-line ignoring the
	// trailing spaces that form the background band.
	pl := strings.Split(plain, "\n")
	cl := strings.Split(stripANSI(colored), "\n")
	if len(pl) != len(cl) {
		t.Fatalf("line count differs: plain=%d colored=%d", len(pl), len(cl))
	}
	for i := range pl {
		if strings.TrimRight(cl[i], " ") != strings.TrimRight(pl[i], " ") {
			t.Errorf("line %d visible text differs:\n plain=%q\n strip=%q", i, pl[i], cl[i])
		}
	}
}

// TestHelpStripeSpansFullWidth checks that striped rows are padded to the
// terminal width so the background band reaches the right edge.
func TestHelpStripeSpansFullWidth(t *testing.T) {
	m := newTestModel(t)
	m.colorPrompt = true
	m.width = 100
	// The second entry of the first section is a striped (odd-index) row.
	e := helpSections[0].entries[1]
	line := m.helpLine(e, 30, 100, true)
	if w := dispWidth(line); w != 100 {
		t.Errorf("striped row width = %d, want 100", w)
	}
}

// TestStyleTablePlainPassThrough verifies styleTable is a no-op without color,
// keeping plain-output assertions elsewhere stable.
func TestStyleTablePlainPassThrough(t *testing.T) {
	lines := []string{"a  b", "-  -", "1  2", "3  4"}
	got := styleTable(lines, false, true)
	if strings.Join(got, "\n") != strings.Join(lines, "\n") {
		t.Errorf("styleTable with color off changed lines: %q", got)
	}
}

// TestStyleTableColorsHeaderAndStripes verifies the header is styled, the visible
// text is preserved, and only alternate data rows carry a background.
func TestStyleTableColorsHeaderAndStripes(t *testing.T) {
	lines := []string{"id  name", "--  ----", "1   alice", "2   bob"}
	got := styleTable(lines, true, true)
	if !strings.Contains(got[0], "\x1b[") {
		t.Error("header row should be styled")
	}
	for i := range lines {
		if stripANSI(got[i]) == "" || !strings.Contains(stripANSI(got[i]), strings.TrimRight(lines[i], " ")) {
			t.Errorf("row %d lost its visible text: %q", i, stripANSI(got[i]))
		}
	}
	// First data row (index 2) is unstriped; second (index 3) is striped.
	if strings.Contains(got[2], "\x1b[4") {
		t.Errorf("first data row should not be striped: %q", got[2])
	}
	if !strings.Contains(got[3], "\x1b[") {
		t.Errorf("second data row should be striped: %q", got[3])
	}
}
