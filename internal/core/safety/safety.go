// Package safety implements mcli's SQL guardrails: classifying a statement as
// read-only, a write, or dangerous, and deciding — given the policy and the
// connected server's environment — whether to allow it, confirm it, or block it.
//
// It lives in the core (not a front-end) so the TUI and the future MCP server
// inherit identical behavior. The package is pure: it touches no connection and
// no UI. Classification is deliberately conservative — when in doubt it leans
// toward flagging — but it is a lexical heuristic, not a SQL parser, so the front
// end still confirms rather than silently trusting it. See design §17.
package safety

import (
	"regexp"
	"strings"
)

// DefaultDangerous is the design §17 dangerous-statement list. Entries are
// matched case-insensitively against the leading verb of a statement; the two
// "without WHERE" entries are special-cased (see Classify).
var DefaultDangerous = []string{
	"DROP",
	"TRUNCATE",
	"ALTER",
	"DELETE without WHERE",
	"UPDATE without WHERE",
	"MERGE",
	"INSERT",
	"CREATE INDEX",
}

// readOnlyVerbs lead a statement that only reads. EXPLAIN is treated as
// read-only even though it names a write statement: it analyzes, never executes.
var readOnlyVerbs = map[string]bool{
	"SELECT": true, "WITH": true, "SHOW": true, "EXPLAIN": true,
	"VALUES": true, "DESCRIBE": true, "DESC": true, "TABLE": true,
}

// Verdict is the classification of a single statement.
type Verdict struct {
	Verb      string   // leading keyword, upper-cased (e.g. "DELETE"); "" if empty
	ReadOnly  bool     // a pure read (SELECT, WITH, EXPLAIN, ...)
	Dangerous bool     // matched a dangerous rule
	Reasons   []string // human-readable rule hits, e.g. ["DELETE without WHERE"]
}

// Write reports whether the statement modifies data or schema without being
// flagged dangerous (e.g. an INSERT is dangerous by the default list, but an
// UPDATE with a WHERE clause is a plain write).
func (v Verdict) Write() bool { return !v.ReadOnly && !v.Dangerous }

var whereRE = regexp.MustCompile(`(?i)\bWHERE\b`)

// Classify examines one SQL statement against the dangerous-keyword list. A nil
// or empty list falls back to DefaultDangerous. The statement is normalized
// (leading comments dropped, string/quoted-identifier literals blanked) so that
// a WHERE or a keyword sitting inside a literal cannot fool the heuristic.
func Classify(sql string, keywords []string) Verdict {
	if len(keywords) == 0 {
		keywords = DefaultDangerous
	}
	clean := strings.ToUpper(strings.TrimSpace(blankNoise(sql)))
	if clean == "" {
		return Verdict{}
	}
	verb := firstWord(clean)
	v := Verdict{Verb: verb, ReadOnly: readOnlyVerbs[verb]}

	hasWhere := whereRE.MatchString(clean)
	for _, kw := range keywords {
		up := strings.ToUpper(strings.TrimSpace(kw))
		if up == "" {
			continue
		}
		if rest, ok := cutSuffix(up, " WITHOUT WHERE"); ok {
			// e.g. "DELETE without WHERE": dangerous only when the verb leads and
			// no WHERE qualifies the rows.
			if verb == strings.TrimSpace(rest) && !hasWhere {
				v.Dangerous = true
				v.Reasons = append(v.Reasons, up)
			}
			continue
		}
		// Plain (possibly multi-word) prefix match: "DROP", "CREATE INDEX".
		if clean == up || strings.HasPrefix(clean, up+" ") || strings.HasPrefix(clean, up+"\n") {
			v.Dangerous = true
			v.Reasons = append(v.Reasons, up)
		}
	}
	if v.Dangerous {
		v.ReadOnly = false
	}
	return v
}

// Action is the decision the policy reaches for a statement.
type Action int

const (
	Allow   Action = iota // run without ceremony
	Confirm               // ask the user first
	Block                 // refuse outright
)

// Policy is the runtime guardrail configuration, assembled from settings plus
// the live read-only toggle.
type Policy struct {
	ConfirmDangerous     bool     // confirm before a dangerous statement
	ReadOnly             bool     // refuse anything that is not a pure read
	BlockDangerousOnProd bool     // refuse (not just confirm) dangerous SQL on prod
	Keywords             []string // dangerous list (nil → DefaultDangerous)
}

// Decide returns the action for a verdict given the connected server's
// environment label (e.g. "prod"). The reason string accompanies Confirm/Block
// for display; it is empty for Allow.
func (p Policy) Decide(v Verdict, env string) (Action, string) {
	prod := strings.EqualFold(env, "prod") || strings.EqualFold(env, "production")

	if p.ReadOnly && !v.ReadOnly {
		return Block, "read-only mode is on — only read-only statements are allowed (\\readonly off to disable)"
	}
	if v.Dangerous {
		why := strings.Join(v.Reasons, ", ")
		if prod && p.BlockDangerousOnProd {
			return Block, why + " is blocked on this production server"
		}
		if prod {
			return Confirm, "dangerous statement (" + why + ") on a PRODUCTION server"
		}
		if p.ConfirmDangerous {
			return Confirm, "dangerous statement: " + why
		}
		return Allow, ""
	}
	// A plain write needs extra confirmation against production (design §17).
	if prod && v.Write() {
		return Confirm, "write (" + v.Verb + ") to a PRODUCTION server"
	}
	return Allow, ""
}

// --- normalization helpers ---

// blankNoise blanks the contents of string literals, quoted identifiers, and
// comments (line -- ... and block /* ... */) so that a keyword or a WHERE living
// inside any of them cannot fool the heuristic. Structure is preserved (noise
// bytes become spaces) so verb and clause detection still see real SQL. Doubled
// quotes collapse naturally because the scanner just toggles in/out.
func blankNoise(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	const (
		code = iota
		inStr
		inLine
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
			b.WriteByte(blank(c))
		case inLine:
			if c == '\n' {
				state = code
				b.WriteByte(c)
			} else {
				b.WriteByte(' ')
			}
		case inBlock:
			if c == '*' && i+1 < len(s) && s[i+1] == '/' {
				state = code
				b.WriteString("  ")
				i++
			} else {
				b.WriteByte(blank(c))
			}
		default: // code
			switch {
			case c == '-' && i+1 < len(s) && s[i+1] == '-':
				state = inLine
				b.WriteString("  ")
				i++
			case c == '/' && i+1 < len(s) && s[i+1] == '*':
				state = inBlock
				b.WriteString("  ")
				i++
			case c == '\'' || c == '"' || c == '`':
				state, quote = inStr, c
				b.WriteByte(' ')
			default:
				b.WriteByte(c)
			}
		}
	}
	return b.String()
}

// blank maps a byte to itself if it is whitespace, else to a space — so blanked
// regions keep their newlines (harmless) without leaking content.
func blank(c byte) byte {
	if c == '\n' || c == '\r' || c == '\t' {
		return c
	}
	return ' '
}

func firstWord(s string) string {
	if i := strings.IndexAny(s, " \t\r\n("); i >= 0 {
		return s[:i]
	}
	return s
}

// cutSuffix is strings.CutSuffix (Go 1.20+) inlined to keep intent local.
func cutSuffix(s, suffix string) (string, bool) {
	if strings.HasSuffix(s, suffix) {
		return s[:len(s)-len(suffix)], true
	}
	return s, false
}
