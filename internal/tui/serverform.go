package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Solifugus/mcli/internal/core/adapter"
	"github.com/Solifugus/mcli/internal/core/config"
)

// formField is one step of an interactive form. An empty answer falls back to
// def; validate (optional) re-asks the field on error.
type formField struct {
	key      string
	prompt   string
	def      string
	validate func(string) error
}

// startForm walks the fields in order via the pending sub-prompt primitive,
// accumulating answers into vals, then calls done. Each resume either advances
// to the next field, re-asks the current one on a validation error, or finishes.
// Esc/Ctrl-C at any step cancels the whole form.
func (m *Model) startForm(fields []formField, vals map[string]string, done func(*Model, map[string]string) tea.Cmd) {
	f := fields[0]
	rest := fields[1:]
	label := f.prompt
	if f.def != "" {
		label += " [" + f.def + "]"
	}
	label += ": "
	m.startPrompt(pending{
		label: label,
		resume: func(m *Model, text string, canceled bool) tea.Cmd {
			if canceled {
				return tea.Println("canceled")
			}
			v := strings.TrimSpace(text)
			if v == "" {
				v = f.def
			}
			if f.validate != nil {
				if err := f.validate(v); err != nil {
					m.startForm(fields, vals, done) // re-ask this field
					return tea.Println("  " + err.Error())
				}
			}
			vals[f.key] = v
			if len(rest) == 0 {
				return done(m, vals)
			}
			m.startForm(rest, vals, done)
			return nil
		},
	})
}

// serverFields builds the wizard steps. existing seeds the defaults for an edit;
// includeName adds a leading name step (used by \server add without an argument).
func serverFields(existing config.Server, includeName bool) []formField {
	port := ""
	if existing.Port != 0 {
		port = strconv.Itoa(existing.Port)
	}
	fields := []formField{
		{key: "type", prompt: "type (" + strings.Join(adapter.Types(), "/") + ")", def: existing.Type, validate: validateType},
		{key: "environment", prompt: "environment (dev/test/stage/prod)", def: orDefault(existing.Environment, "dev")},
		{key: "host", prompt: "host", def: orDefault(existing.Host, "localhost")},
		{key: "port", prompt: "port (blank = driver default)", def: port, validate: validatePort},
		{key: "database", prompt: "default database", def: existing.DefaultDatabase},
		{key: "user", prompt: "user", def: existing.User},
		{key: "password_source", prompt: "password source (none/prompt/keyring/env:VAR)", def: orDefault(existing.PasswordSource, "prompt"), validate: validatePasswordSource},
		{key: "options", prompt: "options k=v,k=v (e.g. encrypt=disable)", def: optionsString(existing.Options)},
	}
	if includeName {
		name := formField{key: "name", prompt: "server name", validate: validateNonEmpty}
		fields = append([]formField{name}, fields...)
	}
	return fields
}

// serverFromVals assembles a config.Server from collected wizard answers.
func serverFromVals(vals map[string]string) (config.Server, error) {
	s := config.Server{
		Type:            vals["type"],
		Environment:     vals["environment"],
		Host:            vals["host"],
		DefaultDatabase: vals["database"],
		User:            vals["user"],
		PasswordSource:  vals["password_source"],
	}
	if p := vals["port"]; p != "" && p != "0" {
		n, err := strconv.Atoi(p)
		if err != nil {
			return s, fmt.Errorf("invalid port %q", p)
		}
		s.Port = n
	}
	if o := strings.TrimSpace(vals["options"]); o != "" {
		opts, err := parseOptions(o)
		if err != nil {
			return s, err
		}
		s.Options = opts
	}
	return s, nil
}

// parseOptions parses "k=v,k=v" into a map; empty pairs are skipped.
func parseOptions(s string) (map[string]string, error) {
	out := map[string]string{}
	for _, pair := range strings.Split(s, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		k, v, ok := strings.Cut(pair, "=")
		k = strings.TrimSpace(k)
		if !ok || k == "" {
			return nil, fmt.Errorf("invalid option %q (want k=v)", pair)
		}
		out[k] = strings.TrimSpace(v)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func optionsString(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// stable order for a predictable default display
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+m[k])
	}
	return strings.Join(parts, ",")
}

// --- field validators ---

func validateNonEmpty(s string) error {
	if strings.TrimSpace(s) == "" {
		return fmt.Errorf("required")
	}
	return nil
}

func validateType(s string) error {
	if !adapter.Registered(s) {
		return fmt.Errorf("unknown type %q (one of: %s)", s, strings.Join(adapter.Types(), ", "))
	}
	return nil
}

func validatePort(s string) error {
	if s == "" {
		return nil
	}
	if n, err := strconv.Atoi(s); err != nil || n <= 0 || n > 65535 {
		return fmt.Errorf("port must be a number 1–65535 (or blank)")
	}
	return nil
}

func validatePasswordSource(s string) error {
	switch {
	case s == "" || s == "none" || s == "prompt" || s == "keyring":
		return nil
	case strings.HasPrefix(s, "env:") && len(s) > len("env:"):
		return nil
	default:
		return fmt.Errorf("source must be none, prompt, keyring, or env:VAR")
	}
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
