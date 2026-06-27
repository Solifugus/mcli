// Package lint provides static SQL analysis: safety/correctness, lexical syntax,
// and style checks. It is UI-agnostic and lives in the core so both front-ends
// (the TUI's .lint command and the MCP lint_sql tool) inherit identical results.
//
// The static checks here need no database connection. Deep validation that does
// require one — does this query parse against the live schema, do its tables and
// columns exist — is delivered separately by the core, which asks the connected
// database to EXPLAIN each query (see (*core.Core).LiveLint). That split keeps the
// offline linter free of any SQL-grammar dependency while still offering true
// schema-aware checking when a connection is available.
package lint

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/Solifugus/mcli/internal/core/adapter"
	"github.com/Solifugus/mcli/internal/core/safety"
)

// Severity ranks a finding. It marshals to its lower-case name in JSON so MCP
// callers get a stable, readable value.
type Severity int

const (
	Info Severity = iota
	Warning
	Error
)

func (s Severity) String() string {
	switch s {
	case Error:
		return "error"
	case Warning:
		return "warning"
	default:
		return "info"
	}
}

// MarshalJSON renders the severity as its name.
func (s Severity) MarshalJSON() ([]byte, error) {
	return []byte(`"` + s.String() + `"`), nil
}

// Finding is one lint result, positioned at a 1-based line and column into the
// original input.
type Finding struct {
	Line     int      `json:"line"`
	Col      int      `json:"col"`
	Rule     string   `json:"rule"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
}

// Options configure a lint run.
type Options struct {
	Dialect  adapter.Dialect // selects the lexer for keyword-case checks
	Keywords []string        // dangerous-SQL list (nil → safety defaults)
	Style    bool            // enable the style/convention rules
	// KeywordCase, when "upper" or "lower" and Style is on, flags SQL keywords
	// that are not written in that case. Empty means do not check casing.
	KeywordCase string
}

var (
	selectStarRE = regexp.MustCompile(`(?i)\bSELECT\s+\*`)
	joinRE       = regexp.MustCompile(`(?i)\bJOIN\b`)
	onRE         = regexp.MustCompile(`(?i)\b(ON|USING)\b`)
	crossNatRE   = regexp.MustCompile(`(?i)\b(CROSS|NATURAL)\s+JOIN\b`)
)

// knownVerbs is the set of statement-leading keywords the lexical syntax check
// recognizes. It is deliberately generous across dialects; an unknown verb is a
// hint, not an error.
var knownVerbs = map[string]bool{
	"SELECT": true, "INSERT": true, "UPDATE": true, "DELETE": true, "WITH": true,
	"CREATE": true, "DROP": true, "ALTER": true, "TRUNCATE": true, "MERGE": true,
	"GRANT": true, "REVOKE": true, "EXPLAIN": true, "SHOW": true, "USE": true,
	"BEGIN": true, "START": true, "COMMIT": true, "ROLLBACK": true, "SAVEPOINT": true,
	"SET": true, "CALL": true, "EXEC": true, "EXECUTE": true, "VALUES": true,
	"TABLE": true, "DECLARE": true, "COMMENT": true, "ANALYZE": true, "VACUUM": true,
	"REINDEX": true, "REFRESH": true, "COPY": true, "DESCRIBE": true, "DESC": true,
	"PRAGMA": true, "REPLACE": true, "LOCK": true, "UNLOCK": true, "RENAME": true,
}

// Lint runs every enabled static rule over sql and returns the findings ordered
// by position. It never connects to a database.
func Lint(sql string, opts Options) []Finding {
	var fs []Finding

	for _, sp := range safety.StatementSpans(sql) {
		stmt := sql[sp.Start:sp.End]
		line, col := LineCol(sql, sp.Start)
		fs = append(fs, statementRules(stmt, line, col, opts)...)
	}
	if opts.Style {
		fs = append(fs, lineStyleRules(sql)...)
		if opts.KeywordCase == "upper" || opts.KeywordCase == "lower" {
			fs = append(fs, keywordCaseRules(sql, opts)...)
		}
	}
	sortByPosition(fs)
	return fs
}

// statementRules applies the SQL-aware rules to one statement. line/col locate
// the statement's first character in the whole input; findings are reported there
// (statement-level granularity keeps positions reliable without a parser).
func statementRules(stmt string, line, col int, opts Options) []Finding {
	masked := safety.Mask(stmt)
	if strings.TrimSpace(masked) == "" {
		return nil // comment-only or blank segment
	}
	var fs []Finding
	at := func(rule string, sev Severity, msg string) {
		fs = append(fs, Finding{Line: line, Col: col, Rule: rule, Severity: sev, Message: msg})
	}

	// --- safety / correctness (reuse the classifier) ---
	v := safety.Classify(stmt, opts.Keywords)
	for _, reason := range v.Reasons {
		at("dangerous-sql", Warning, reason)
	}

	// --- lexical syntax ---
	if d := openParens(masked); d > 0 {
		at("unbalanced-parens", Error, "unbalanced parentheses (more '(' than ')')")
	} else if d < 0 {
		at("unbalanced-parens", Error, "unbalanced parentheses (more ')' than '(')")
	}
	if what := incompleteNoise(stmt); what != "" {
		at("unterminated", Error, "unterminated "+what)
	}
	if v.Verb != "" && !knownVerbs[v.Verb] {
		at("unknown-statement", Warning, "statement starts with an unrecognized keyword "+strconv(v.Verb))
	}

	// --- correctness: a JOIN with no ON/USING (and not CROSS/NATURAL) ---
	if joinRE.MatchString(masked) && !onRE.MatchString(masked) && !crossNatRE.MatchString(masked) {
		at("missing-join-condition", Warning, "JOIN without an ON or USING clause (possible accidental cross join)")
	}

	// --- style: SELECT * ---
	if opts.Style && selectStarRE.MatchString(masked) {
		at("select-star", Warning, "avoid SELECT * — list columns explicitly")
	}
	return fs
}

// lineStyleRules flags whitespace issues across the whole input.
func lineStyleRules(sql string) []Finding {
	var fs []Finding
	for i, raw := range strings.Split(sql, "\n") {
		line := i + 1
		if trimmed := strings.TrimRight(raw, " \t"); trimmed != raw {
			fs = append(fs, Finding{
				Line: line, Col: len([]rune(trimmed)) + 1,
				Rule: "trailing-whitespace", Severity: Info, Message: "trailing whitespace",
			})
		}
		if n := leadingTabs(raw); n > 0 {
			fs = append(fs, Finding{
				Line: line, Col: 1,
				Rule: "tab-indent", Severity: Info, Message: "tab indentation (prefer spaces)",
			})
		}
	}
	return fs
}

// keywordCaseRules tokenizes the input and flags SQL keywords not written in the
// configured case. It is best-effort: any lexer failure yields no findings.
func keywordCaseRules(sql string, opts Options) []Finding {
	toks := keywordTokens(sql, opts.Dialect)
	want := opts.KeywordCase
	var fs []Finding
	for _, kw := range toks {
		ok := kw.text == strings.ToUpper(kw.text)
		if want == "lower" {
			ok = kw.text == strings.ToLower(kw.text)
		}
		if ok {
			continue
		}
		line, col := LineCol(sql, kw.off)
		fs = append(fs, Finding{
			Line: line, Col: col, Rule: "keyword-case", Severity: Info,
			Message: "keyword " + strconv(kw.text) + " should be " + want + "case",
		})
	}
	return fs
}

// openParens returns the running paren balance of masked code (positive: unclosed
// '('; negative: extra ')'). masked must already have noise blanked.
func openParens(masked string) int {
	depth := 0
	for i := 0; i < len(masked); i++ {
		switch masked[i] {
		case '(':
			depth++
		case ')':
			depth--
		}
	}
	return depth
}

// incompleteNoise reports whether s ends inside an unterminated string literal or
// block comment, returning a short description ("" when well-formed). It mirrors
// the safety scanner's state machine but exposes the terminal state.
func incompleteNoise(s string) string {
	const (
		code = iota
		inStr
		inBlock
	)
	state, quote := code, byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch state {
		case inStr:
			if c == quote {
				state = code
			}
		case inBlock:
			if c == '*' && i+1 < len(s) && s[i+1] == '/' {
				state = code
				i++
			}
		default:
			switch {
			case c == '-' && i+1 < len(s) && s[i+1] == '-':
				// line comment runs to end-of-line; never "unterminated"
				for i < len(s) && s[i] != '\n' {
					i++
				}
			case c == '/' && i+1 < len(s) && s[i+1] == '*':
				state = inBlock
				i++
			case c == '\'' || c == '"' || c == '`':
				state, quote = inStr, c
			}
		}
	}
	switch state {
	case inStr:
		return "string literal or quoted identifier"
	case inBlock:
		return "block comment"
	}
	return ""
}

// leadingTabs counts tab characters at the start of a line.
func leadingTabs(s string) int {
	n := 0
	for n < len(s) && s[n] == '\t' {
		n++
	}
	return n
}

// LineCol converts a byte offset into a 1-based line and column (column counted
// in runes). An out-of-range offset clamps to the end.
func LineCol(s string, off int) (line, col int) {
	if off > len(s) {
		off = len(s)
	}
	line, col = 1, 1
	for i := 0; i < off; {
		r, sz := utf8.DecodeRuneInString(s[i:])
		if sz == 0 {
			break
		}
		if r == '\n' {
			line++
			col = 1
		} else {
			col++
		}
		i += sz
	}
	return line, col
}

func sortByPosition(fs []Finding) {
	// Small slices; a simple insertion sort keeps ordering stable by (line, col).
	for i := 1; i < len(fs); i++ {
		for j := i; j > 0; j-- {
			a, b := fs[j-1], fs[j]
			if a.Line < b.Line || (a.Line == b.Line && a.Col <= b.Col) {
				break
			}
			fs[j-1], fs[j] = fs[j], fs[j-1]
		}
	}
}

// strconv wraps a token in single quotes for messages without pulling in fmt.
func strconv(s string) string { return "'" + s + "'" }
