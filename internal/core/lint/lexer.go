package lint

import (
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"

	"github.com/Solifugus/mcli/internal/core/adapter"
)

// keywordToken is a SQL keyword and its byte offset in the source.
type keywordToken struct {
	text string
	off  int
}

// keywordTokens tokenizes sql with the dialect's lexer and returns the keyword
// tokens with their byte offsets. Best-effort: a lexer error yields no tokens, so
// the keyword-case rule simply produces no findings rather than failing the lint.
func keywordTokens(sql string, d adapter.Dialect) []keywordToken {
	lexer := chromaLexer(d)
	if lexer == nil {
		return nil
	}
	it, err := lexer.Tokenise(nil, sql)
	if err != nil {
		return nil
	}
	var toks []keywordToken
	off := 0
	for _, t := range it.Tokens() {
		if t.Type.InCategory(chroma.Keyword) {
			toks = append(toks, keywordToken{text: t.Value, off: off})
		}
		off += len(t.Value)
	}
	return toks
}

// chromaLexer selects a lexer for the dialect, falling back to generic SQL. It
// mirrors the TUI's highlighter so linting and highlighting agree on tokens.
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
