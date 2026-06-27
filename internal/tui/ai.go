package tui

import (
	"context"
	"sort"
	"strings"
)

// cmdAI dispatches the .ai subcommands (§20). ask/explain/fix run a completion
// in the background (network); providers is a synchronous read of ai.json. AI
// output is text only — mcli never executes the SQL it suggests.
func (m *Model) cmdAI(args []string) (cmdResult, action) {
	if len(args) == 0 {
		return out(`usage: .ai ask <question> | explain <file|current> | fix <file|current> | providers | help`), sync()
	}
	switch args[0] {
	case "help":
		return aiHelp(), sync()
	case "providers", "provider":
		return m.aiProviders(), sync()
	case "ask":
		q := strings.TrimSpace(strings.Join(args[1:], " "))
		if q == "" {
			return out(`usage: .ai ask <question>`), sync()
		}
		return cmdResult{}, async(m.aiRunner(func(ctx context.Context) (string, error) {
			return m.core.AIAsk(ctx, q)
		}))
	case "explain":
		sql, res, ok := m.aiTargetSQL(args[1:])
		if !ok {
			return res, sync()
		}
		return cmdResult{}, async(m.aiRunner(func(ctx context.Context) (string, error) {
			return m.core.AIExplain(ctx, sql)
		}))
	case "fix":
		sql, res, ok := m.aiTargetSQL(args[1:])
		if !ok {
			return res, sync()
		}
		lastErr := m.lastSQLErr
		return cmdResult{}, async(m.aiRunner(func(ctx context.Context) (string, error) {
			return m.core.AIFix(ctx, sql, lastErr)
		}))
	default:
		return out(`unknown .ai subcommand: ` + args[0] + ` (ask|explain|fix|providers)`), sync()
	}
}

// aiHelp prints practical, copy-pasteable examples of every .ai subcommand. The
// AI only ever suggests text — mcli never runs the SQL it returns.
func aiHelp() cmdResult {
	return out(
		`.ai — AI assistance. Replies are text only; mcli never runs the SQL it suggests.`,
		`Calls are grounded with your dialect, environment, database, and (when`,
		`send_schema_context is on and you're connected) your table names.`,
		``,
		`  ask <question>        free-form question, answered in your DB's context`,
		`    .ai ask how do I get the 10 most recent orders per customer`,
		`    .ai ask what's the Postgres equivalent of SQL Server's TOP`,
		`    .ai ask write a query to find duplicate emails in the users table`,
		``,
		`  explain <file|current>   explain a statement in plain English`,
		`    .ai explain current             explain the statement you just ran`,
		`    .ai explain monthly_report      explain the SQL in monthly_report.sql`,
		``,
		`  fix <file|current>       suggest a corrected statement`,
		`    .ai fix current                 fixes the last run — its error is sent too`,
		`    .ai fix etl_load                fix the SQL in etl_load.sql`,
		``,
		`  providers                list configured providers (default marked *)`,
		`  help                     show this`,
		``,
		`Tip: a run that errors then .ai fix current is the quick repair loop —`,
		`the failing SQL and its error are sent together so the fix targets the`,
		`actual failure. Paste the result back, or .edit the file, then re-run.`,
	)
}

// aiTargetSQL resolves the SQL a .ai explain/fix command should act on: the
// keyword "current" (the last statement run this session) or a workspace SQL
// file name. On a problem it returns ok=false and a result to print.
func (m *Model) aiTargetSQL(rest []string) (sql string, res cmdResult, ok bool) {
	if len(rest) < 1 {
		return "", out(`usage: .ai explain|fix <file|current>`), false
	}
	return m.sqlFromTarget(rest[0])
}

// sqlFromTarget resolves a "<file|current>" argument to SQL text: the keyword
// "current" (the last statement run this session) or a workspace SQL file. On a
// problem it returns ok=false with a result to print. Shared by .ai and .lint.
func (m *Model) sqlFromTarget(target string) (sql string, res cmdResult, ok bool) {
	if target == "current" {
		if strings.TrimSpace(m.lastSQL) == "" {
			return "", out("no current SQL — run a statement first, or name a file"), false
		}
		return m.lastSQL, cmdResult{}, true
	}
	content, err := m.core.ReadSQLFile(target)
	if err != nil {
		return "", errOut(err), false
	}
	if strings.TrimSpace(content) == "" {
		return "", out("file " + target + " is empty"), false
	}
	return content, cmdResult{}, true
}

// aiRunner wraps an AI completion as a background op, returning the reply as
// output lines (a heading plus the wrapped body).
func (m *Model) aiRunner(fn func(ctx context.Context) (string, error)) asyncRun {
	return func(ctx context.Context) asyncResultMsg {
		reply, err := fn(ctx)
		if err != nil {
			return asyncResultMsg{err: err}
		}
		lines := append([]string{"── ai ──"}, strings.Split(reply, "\n")...)
		return asyncResultMsg{lines: lines}
	}
}

// aiProviders lists the configured AI providers, marking the default.
func (m *Model) aiProviders() cmdResult {
	provs, def := m.core.AIProviders()
	if len(provs) == 0 {
		return out("no AI providers configured — add some to ~/.mcli/ai.json")
	}
	names := make([]string, 0, len(provs))
	for n := range provs {
		names = append(names, n)
	}
	sort.Strings(names)
	rows := make([][]string, 0, len(names))
	for _, n := range names {
		p := provs[n]
		marker := " "
		if n == def {
			marker = "*"
		}
		base := p.BaseURL
		if base == "" {
			base = "(openai default)"
		}
		rows = append(rows, []string{marker, n, p.Model, base, orNone(p.APIKeySource)})
	}
	return cmdResult{lines: renderTable([]string{"", "name", "model", "base_url", "api_key"}, rows)}
}
