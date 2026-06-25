package tui

import (
	"strings"
	"testing"

	"github.com/Solifugus/mcli/internal/core/adapter"
)

// stripANSI removes ANSI escape sequences so tests can assert on visible text.
func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			inEsc = true
		case inEsc && (r == 'm'):
			inEsc = false
		case inEsc:
			// skip escape body
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func TestPromptStyleByEnvironment(t *testing.T) {
	// dev/prod produce different ANSI sequences; unknown falls back to gray.
	dev := promptStyleFor("dev").Render("x")
	prod := promptStyleFor("prod").Render("x")
	unknown := promptStyleFor("").Render("x")
	if dev == prod {
		t.Error("dev and prod prompts should differ in color")
	}
	if !strings.Contains(unknown, "\x1b[") {
		t.Error("unknown env should still be styled (gray)")
	}
	// All render the same visible text.
	for _, s := range []string{dev, prod, unknown} {
		if stripANSI(s) != "x" {
			t.Errorf("visible text = %q, want x", stripANSI(s))
		}
	}
}

func TestStyledPromptRespectsColorSetting(t *testing.T) {
	m := newTestModel(t)
	m.colorPrompt = false
	if got := m.styledPrompt(); got != m.prompt {
		t.Errorf("with color off, styledPrompt = %q, want plain %q", got, m.prompt)
	}
	m.colorPrompt = true
	if got := m.styledPrompt(); !strings.Contains(got, "\x1b[") {
		t.Errorf("with color on, styledPrompt should contain ANSI: %q", got)
	}
}

func TestRenderInputPlainPreservesText(t *testing.T) {
	out := renderInput("select 1", 0, adapter.DialectGenericSQL, false)
	if stripANSI(out) != "select 1" {
		t.Errorf("visible text = %q, want %q", stripANSI(out), "select 1")
	}
}

func TestRenderInputHighlightPreservesText(t *testing.T) {
	// Highlighting must never change the visible characters, only their color.
	in := "SELECT id, name FROM users WHERE id = 42 -- note"
	out := renderInput(in, 3, adapter.DialectPostgres, true)
	if got := stripANSI(out); got != in {
		t.Errorf("highlight changed text:\n got %q\nwant %q", got, in)
	}
	if !strings.Contains(out, "\x1b[") {
		t.Error("expected ANSI color codes in highlighted output")
	}
}

func TestRenderInputCursorAtEnd(t *testing.T) {
	// Cursor past the last rune appends a reversed space.
	out := renderInput("ab", 2, adapter.DialectGenericSQL, false)
	if !strings.Contains(out, "\x1b[7m") { // reverse video
		t.Errorf("expected a reverse-video cursor: %q", out)
	}
	if stripANSI(out) != "ab " {
		t.Errorf("visible = %q, want %q", stripANSI(out), "ab ")
	}
}

func TestChromaLexerFallback(t *testing.T) {
	if chromaLexer(adapter.DialectPostgres) == nil {
		t.Error("postgres lexer should resolve")
	}
	if chromaLexer(adapter.Dialect("bogus")) == nil {
		t.Error("unknown dialect should fall back to a generic SQL lexer")
	}
}
