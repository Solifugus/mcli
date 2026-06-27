package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"

	"github.com/Solifugus/mcli/internal/core/adapter"
)

// tokenClass is a coarse highlight category. Using a small enum (rather than
// comparing lipgloss.Style values, which are not comparable) lets renderInput
// coalesce adjacent same-class runes into a single styled span.
type tokenClass uint8

const (
	clsPlain tokenClass = iota
	clsKeyword
	clsString
	clsNumber
	clsComment
)

// ANSI base colors downsample gracefully on limited terminals.
var classStyles = map[tokenClass]lipgloss.Style{
	clsPlain:   lipgloss.NewStyle(),
	clsKeyword: lipgloss.NewStyle().Foreground(lipgloss.Color("4")), // blue
	clsString:  lipgloss.NewStyle().Foreground(lipgloss.Color("2")), // green
	clsNumber:  lipgloss.NewStyle().Foreground(lipgloss.Color("6")), // cyan
	clsComment: lipgloss.NewStyle().Foreground(lipgloss.Color("8")), // gray
}

func (c tokenClass) style() lipgloss.Style { return classStyles[c] }

// runeRange is a half-open [lo,hi) range of rune indices within a single line,
// used to render a selection highlight in the built-in editor.
type runeRange struct{ lo, hi int }

// reverseCursor is the default block cursor: reverse the cell under the cursor.
func reverseCursor(s lipgloss.Style) lipgloss.Style { return s.Reverse(true) }

// renderInput renders the REPL input line with a block cursor at pos, syntax-
// colored per the dialect when highlight is enabled.
func renderInput(value string, pos int, dialect adapter.Dialect, highlight bool) string {
	runes := []rune(value)
	return renderLineSpans(runes, lineClasses(value, dialect, highlight), pos, nil, nil)
}

// lineClasses returns the per-rune highlight class for one line; all plain when
// highlight is off. The editor tokenizes line-by-line, matching the REPL (a
// block comment spanning lines is an accepted minor mis-highlight).
func lineClasses(line string, dialect adapter.Dialect, highlight bool) []tokenClass {
	classes := make([]tokenClass, len([]rune(line)))
	if highlight {
		applyChromaClasses(line, dialect, classes)
	}
	return classes
}

// renderLineSpans renders one line's runes with per-rune token styling. A block
// cursor is drawn at index cursor (cursor == len(runes) draws a trailing-space
// cursor; cursor < 0 draws none). cursorStyle transforms the cursor cell's style
// — nil means the default reverse block; the editor passes an underline for
// insert mode vs. reverse for overwrite, giving a cursor-shape cue. An optional
// sel range is shown reversed. Adjacent runes sharing class and selection state
// are coalesced into one styled span to keep the ANSI compact.
func renderLineSpans(runes []rune, classes []tokenClass, cursor int, cursorStyle func(lipgloss.Style) lipgloss.Style, sel *runeRange) string {
	if cursorStyle == nil {
		cursorStyle = reverseCursor
	}
	inSel := func(i int) bool { return sel != nil && i >= sel.lo && i < sel.hi }

	var b strings.Builder
	for i := 0; i < len(runes); {
		if i == cursor {
			b.WriteString(cursorStyle(classes[i].style()).Render(string(runes[i])))
			i++
			continue
		}
		// Coalesce a run sharing class and selection state, stopping before the cursor.
		selHere := inSel(i)
		j := i
		for j < len(runes) && j != cursor && classes[j] == classes[i] && inSel(j) == selHere {
			j++
		}
		st := classes[i].style()
		if selHere {
			st = st.Reverse(true)
		}
		b.WriteString(st.Render(string(runes[i:j])))
		i = j
	}
	if cursor == len(runes) {
		b.WriteString(cursorStyle(lipgloss.NewStyle()).Render(" "))
	}
	return b.String()
}

// applyChromaClasses tokenizes value with the dialect's lexer and assigns a
// class to each rune index. Best-effort: on any failure classes are left plain.
func applyChromaClasses(value string, dialect adapter.Dialect, classes []tokenClass) {
	lexer := chromaLexer(dialect)
	if lexer == nil {
		return
	}
	it, err := lexer.Tokenise(nil, value)
	if err != nil {
		return
	}
	idx := 0
	for _, tok := range it.Tokens() {
		cls := classForToken(tok.Type)
		for range tok.Value { // iterate runes of the token value
			if idx < len(classes) {
				classes[idx] = cls
			}
			idx++
		}
	}
}

func classForToken(t chroma.TokenType) tokenClass {
	switch {
	case t.InCategory(chroma.Keyword):
		return clsKeyword
	case t.InCategory(chroma.Comment):
		return clsComment
	case t.InSubCategory(chroma.LiteralString):
		return clsString
	case t.InSubCategory(chroma.LiteralNumber):
		return clsNumber
	default:
		return clsPlain
	}
}

// chromaLexer selects a lexer for the dialect, falling back to generic SQL.
func chromaLexer(d adapter.Dialect) chroma.Lexer {
	name := "sql"
	switch d {
	case adapter.DialectPostgres:
		name = "postgresql"
	case adapter.DialectTSQL:
		name = "transact-sql"
	case adapter.DialectMySQL:
		name = "mysql"
	}
	if l := lexers.Get(name); l != nil {
		return l
	}
	return lexers.Get("sql")
}
