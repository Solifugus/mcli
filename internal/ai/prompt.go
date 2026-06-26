package ai

import (
	"fmt"
	"strings"
)

// Context is the database situation handed to the model so its answers are
// grounded in the user's actual connection. Fields are best-effort; empty ones
// are omitted from the prompt.
type Context struct {
	Dialect     string   // e.g. "postgres", "tsql"
	Environment string   // e.g. "dev", "prod"
	Database    string   // current database
	Tables      []string // optional schema hint (table names), already capped
}

// systemPrompt frames the assistant: SQL-focused, dialect-aware, concise, and —
// importantly — never under the impression it can run anything itself.
func systemPrompt(cx Context) string {
	var b strings.Builder
	b.WriteString("You are a SQL assistant embedded in mcli, a database CLI. ")
	b.WriteString("Help with SQL and data tasks. Be concise and practical. ")
	b.WriteString("You cannot execute SQL or access the database yourself — the user runs statements deliberately, so never claim to have run anything; offer SQL for them to review and run. ")
	if cx.Dialect != "" {
		fmt.Fprintf(&b, "The active SQL dialect is %s; prefer its syntax. ", cx.Dialect)
	}
	if cx.Environment != "" {
		fmt.Fprintf(&b, "The server environment is %q — be extra careful with destructive statements. ", cx.Environment)
	}
	if cx.Database != "" {
		fmt.Fprintf(&b, "The current database is %s. ", cx.Database)
	}
	if len(cx.Tables) > 0 {
		fmt.Fprintf(&b, "Some tables available: %s. ", strings.Join(cx.Tables, ", "))
	}
	return strings.TrimSpace(b.String())
}

// AskMessages builds a free-form question conversation.
func AskMessages(cx Context, question string) []Message {
	return []Message{
		{Role: RoleSystem, Content: systemPrompt(cx)},
		{Role: RoleUser, Content: question},
	}
}

// ExplainMessages asks the model to explain a SQL statement.
func ExplainMessages(cx Context, sql string) []Message {
	user := "Explain what this SQL does, clearly and concisely:\n\n" + fence(sql)
	return []Message{
		{Role: RoleSystem, Content: systemPrompt(cx)},
		{Role: RoleUser, Content: user},
	}
}

// FixMessages asks the model to diagnose and rewrite a SQL statement, including
// the error message when one is available.
func FixMessages(cx Context, sql, lastErr string) []Message {
	var b strings.Builder
	b.WriteString("This SQL is not working as intended. Diagnose the problem and provide a corrected version.\n\n")
	b.WriteString(fence(sql))
	if strings.TrimSpace(lastErr) != "" {
		b.WriteString("\n\nThe database returned this error:\n\n")
		b.WriteString(fence(lastErr))
	}
	return []Message{
		{Role: RoleSystem, Content: systemPrompt(cx)},
		{Role: RoleUser, Content: b.String()},
	}
}

// fence wraps text in a Markdown code fence for the model.
func fence(s string) string {
	return "```sql\n" + strings.TrimSpace(s) + "\n```"
}
