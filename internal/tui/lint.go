package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/Solifugus/mcli/internal/core/lint"
)

// cmdLint lints a SQL file or the current statement. Static checks run inline; a
// trailing "live" also validates queries against the connected database as a
// background op (a DB round-trip), printed after the static report.
//
//	.lint <file|current> [live]
func (m *Model) cmdLint(args []string) (cmdResult, action) {
	live := false
	rest := args
	if n := len(rest); n > 0 && strings.EqualFold(rest[n-1], "live") {
		live, rest = true, rest[:n-1]
	}
	if len(rest) < 1 {
		return out(`usage: .lint <file|current> [live]`), sync()
	}
	sql, res, ok := m.sqlFromTarget(rest[0])
	if !ok {
		return res, sync()
	}

	report := lintReport(m.core.Lint(sql), "static")
	if !live {
		return cmdResult{lines: report}, sync()
	}
	if !m.core.Connected() {
		return cmdResult{lines: append(report, "live checks skipped — not connected")}, sync()
	}
	c := m.core
	return cmdResult{lines: report}, async(func(ctx context.Context) asyncResultMsg {
		fs, err := c.LiveLint(ctx, sql)
		if err != nil {
			return asyncResultMsg{err: err}
		}
		return asyncResultMsg{lines: lintReport(fs, "live")}
	})
}

// lintReport renders findings as "line:col [severity] rule: message", prefixed by
// a labeled header. A clean pass reports so explicitly.
func lintReport(fs []lint.Finding, label string) []string {
	if len(fs) == 0 {
		return []string{"lint (" + label + "): no issues"}
	}
	lines := make([]string, 0, len(fs)+1)
	lines = append(lines, fmt.Sprintf("lint (%s): %d issue(s)", label, len(fs)))
	for _, f := range fs {
		lines = append(lines, fmt.Sprintf("  %d:%d [%s] %s: %s", f.Line, f.Col, f.Severity, f.Rule, f.Message))
	}
	return lines
}
