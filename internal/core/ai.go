package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/Solifugus/mcli/internal/ai"
	"github.com/Solifugus/mcli/internal/core/config"
)

// aiSchemaTableCap bounds how many table names are injected as schema context,
// so a large database does not bloat the prompt.
const aiSchemaTableCap = 60

// ErrNoAIProvider is returned when no AI provider is configured in ai.json.
var ErrNoAIProvider = errors.New("no AI provider configured — add one to ~/.mcli/ai.json")

// AIProviders returns the configured providers and the default provider name.
func (c *Core) AIProviders() (map[string]config.AIProvider, string) {
	return c.aiCfg.Providers, c.aiCfg.DefaultProvider
}

// AIAsk answers a free-form question with database context.
func (c *Core) AIAsk(ctx context.Context, question string) (string, error) {
	return c.aiComplete(ctx, ai.AskMessages(c.aiContext(ctx), question))
}

// AIExplain explains a SQL statement.
func (c *Core) AIExplain(ctx context.Context, sql string) (string, error) {
	return c.aiComplete(ctx, ai.ExplainMessages(c.aiContext(ctx), sql))
}

// AIFix diagnoses a SQL statement and proposes a correction, optionally using the
// last error the database returned for it.
func (c *Core) AIFix(ctx context.Context, sql, lastErr string) (string, error) {
	return c.aiComplete(ctx, ai.FixMessages(c.aiContext(ctx), sql, lastErr))
}

// aiComplete resolves the active provider and runs a completion. AI never
// executes SQL; the returned text is for the user to read and run deliberately.
func (c *Core) aiComplete(ctx context.Context, msgs []ai.Message) (string, error) {
	p, name, err := c.resolveProvider()
	if err != nil {
		return "", err
	}
	reply, err := c.aiClient.Complete(ctx, p, msgs)
	if err != nil {
		return "", err
	}
	c.log("AI", name)
	return reply, nil
}

// resolveProvider picks the active provider (the configured default, or the sole
// provider if there is exactly one) and resolves its API key from api_key_source.
func (c *Core) resolveProvider() (ai.Provider, string, error) {
	provs := c.aiCfg.Providers
	if len(provs) == 0 {
		return ai.Provider{}, "", ErrNoAIProvider
	}
	name := c.aiCfg.DefaultProvider
	if name == "" {
		if len(provs) == 1 {
			for n := range provs {
				name = n
			}
		} else {
			return ai.Provider{}, "", fmt.Errorf("no default_provider set in ai.json (have: %s)", strings.Join(providerNames(provs), ", "))
		}
	}
	cfgp, ok := provs[name]
	if !ok {
		return ai.Provider{}, "", fmt.Errorf("default_provider %q is not defined in ai.json", name)
	}
	key, err := resolveAPIKey(cfgp.APIKeySource)
	if err != nil {
		return ai.Provider{}, "", err
	}
	return ai.Provider{BaseURL: cfgp.BaseURL, Model: cfgp.Model, APIKey: key}, name, nil
}

// aiContext gathers the database situation for the prompt. Schema context (a
// capped list of table names) is included only when send_schema_context is on and
// a connection is live; failures to list tables degrade silently to no hint.
func (c *Core) aiContext(ctx context.Context) ai.Context {
	cx := ai.Context{
		Dialect:     string(c.dialect),
		Environment: c.Environment(),
		Database:    c.current.CurrentDatabase,
	}
	if c.aiCfg.SendSchemaContext && c.conn != nil {
		if refs, err := c.conn.ListTables(ctx); err == nil {
			for i, r := range refs {
				if i >= aiSchemaTableCap {
					break
				}
				if r.Schema != "" {
					cx.Tables = append(cx.Tables, r.Schema+"."+r.Name)
				} else {
					cx.Tables = append(cx.Tables, r.Name)
				}
			}
		}
	}
	return cx
}

// resolveAPIKey obtains an AI provider's API key from its api_key_source:
// ""/"none" → no key (local models), "env:VAR" → that environment variable.
func resolveAPIKey(src string) (string, error) {
	switch {
	case src == "" || src == "none":
		return "", nil
	case strings.HasPrefix(src, "env:"):
		name := strings.TrimPrefix(src, "env:")
		if name == "" {
			return "", errors.New(`api_key_source "env:" is missing a variable name`)
		}
		v := os.Getenv(name)
		if v == "" {
			return "", fmt.Errorf("environment variable %s (api_key_source) is empty", name)
		}
		return v, nil
	default:
		return "", fmt.Errorf("unknown api_key_source %q (use none or env:VAR)", src)
	}
}

func providerNames(provs map[string]config.AIProvider) []string {
	names := make([]string, 0, len(provs))
	for n := range provs {
		names = append(names, n)
	}
	return names
}
