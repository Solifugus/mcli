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

// renderInput renders the REPL input line with a block cursor at pos. When
// highlight is enabled the text is syntax-colored per the dialect's lexer;
// otherwise it is plain. The cursor is overlaid by reversing the character under
// it (or a trailing space when the cursor is at end of line), so it stays
// accurate regardless of token styling. Adjacent same-class runes are coalesced
// into one styled span to keep the emitted ANSI compact.
func renderInput(value string, pos int, dialect adapter.Dialect, highlight bool) string {
	runes := []rune(value)

	classes := make([]tokenClass, len(runes)) // all clsPlain by default
	if highlight {
		applyChromaClasses(value, dialect, classes)
	}

	var b strings.Builder
	for i := 0; i < len(runes); {
		if i == pos {
			b.WriteString(classes[i].style().Reverse(true).Render(string(runes[i])))
			i++
			continue
		}
		// Coalesce a run of the same class, stopping before the cursor.
		j := i
		for j < len(runes) && j != pos && classes[j] == classes[i] {
			j++
		}
		b.WriteString(classes[i].style().Render(string(runes[i:j])))
		i = j
	}
	if pos >= len(runes) {
		b.WriteString(lipgloss.NewStyle().Reverse(true).Render(" "))
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
