package tui

import (
	"context"
	"sort"
	"strings"
)

// cmdAI dispatches the \ai subcommands (§20). ask/explain/fix run a completion
// in the background (network); providers is a synchronous read of ai.json. AI
// output is text only — mcli never executes the SQL it suggests.
func (m *Model) cmdAI(args []string) (cmdResult, action) {
	if len(args) == 0 {
		return out(`usage: \ai ask <question> | explain <file|current> | fix <file|current> | providers`), sync()
	}
	switch args[0] {
	case "providers", "provider":
		return m.aiProviders(), sync()
	case "ask":
		q := strings.TrimSpace(strings.Join(args[1:], " "))
		if q == "" {
			return out(`usage: \ai ask <question>`), sync()
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
		return out(`unknown \ai subcommand: ` + args[0] + ` (ask|explain|fix|providers)`), sync()
	}
}

// aiTargetSQL resolves the SQL a \ai explain/fix command should act on: the
// keyword "current" (the last statement run this session) or a workspace SQL
// file name. On a problem it returns ok=false and a result to print.
func (m *Model) aiTargetSQL(rest []string) (sql string, res cmdResult, ok bool) {
	if len(rest) < 1 {
		return "", out(`usage: \ai explain|fix <file|current>`), false
	}
	target := rest[0]
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
