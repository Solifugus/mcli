package core

import (
	"context"
	"strings"

	"github.com/Solifugus/mcli/internal/core/lint"
	"github.com/Solifugus/mcli/internal/core/safety"
)

// Lint runs the static SQL linter (safety/correctness, lexical syntax, and — when
// enabled in settings — style) over sql. It needs no connection. Both front-ends
// call through here so the rules and configuration are identical.
func (c *Core) Lint(sql string) []lint.Finding {
	return lint.Lint(sql, lint.Options{
		Dialect:     c.dialect,
		Keywords:    c.settings.DangerousSQL,
		Style:       c.settings.Lint.Style,
		KeywordCase: c.settings.Lint.KeywordCase,
	})
}

// LiveLint validates each query statement against the connected database by
// asking it to EXPLAIN the statement — surfacing deep syntax errors and schema
// problems (unknown tables or columns, type mismatches) that the static linter
// cannot see, dialect-correctly and without executing anything. Only read-only
// statements are checked; writes and DDL are skipped, since EXPLAIN on them can
// execute or be unsupported. Requires a connection.
func (c *Core) LiveLint(ctx context.Context, sql string) ([]lint.Finding, error) {
	if c.conn == nil {
		return nil, ErrNotConnected
	}
	var fs []lint.Finding
	for _, sp := range safety.StatementSpans(sql) {
		stmt := sql[sp.Start:sp.End]
		v := safety.Classify(stmt, nil)
		if !v.ReadOnly || v.Verb == "EXPLAIN" {
			continue // skip writes/DDL and already-EXPLAIN statements
		}
		if _, err := c.conn.ExplainQuery(ctx, stmt); err != nil {
			line, col := lint.LineCol(sql, sp.Start)
			fs = append(fs, lint.Finding{
				Line: line, Col: col, Rule: "live-validation",
				Severity: lint.Error, Message: cleanDBError(err),
			})
		}
	}
	return fs, nil
}

// cleanDBError flattens a driver error to a single line for a lint finding.
func cleanDBError(err error) string {
	s := strings.TrimSpace(err.Error())
	if i := strings.IndexAny(s, "\n\r"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}
