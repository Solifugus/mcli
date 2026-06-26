package core

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Solifugus/mcli/internal/core/config"
)

func TestResolveAPIKey(t *testing.T) {
	t.Setenv("MCLI_AI_KEY", "sk-xyz")
	cases := []struct {
		src     string
		want    string
		wantErr bool
	}{
		{"", "", false},
		{"none", "", false},
		{"env:MCLI_AI_KEY", "sk-xyz", false},
		{"env:MCLI_AI_UNSET", "", true}, // empty env var is an error for an explicit source
		{"env:", "", true},
		{"weird", "", true},
	}
	for _, c := range cases {
		got, err := resolveAPIKey(c.src)
		if (err != nil) != c.wantErr {
			t.Errorf("resolveAPIKey(%q) err = %v, wantErr %v", c.src, err, c.wantErr)
		}
		if got != c.want {
			t.Errorf("resolveAPIKey(%q) = %q, want %q", c.src, got, c.want)
		}
	}
}

func TestResolveProviderSelection(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// No providers configured.
	if _, _, err := c.resolveProvider(); !errors.Is(err, ErrNoAIProvider) {
		t.Errorf("no providers err = %v, want ErrNoAIProvider", err)
	}
	// One provider, no default → use it.
	c.aiCfg = config.AIConfig{Providers: map[string]config.AIProvider{
		"local": {Model: "qwen", APIKeySource: "none"},
	}}
	if _, name, err := c.resolveProvider(); err != nil || name != "local" {
		t.Errorf("single provider = (%q, %v), want local", name, err)
	}
	// Two providers, no default → ambiguous error.
	c.aiCfg.Providers["openai"] = config.AIProvider{Model: "gpt", APIKeySource: "none"}
	if _, _, err := c.resolveProvider(); err == nil {
		t.Error("two providers without default should error")
	}
	// Two providers, default set → use default.
	c.aiCfg.DefaultProvider = "openai"
	if _, name, err := c.resolveProvider(); err != nil || name != "openai" {
		t.Errorf("default provider = (%q, %v), want openai", name, err)
	}
}

func TestAIAskAgainstMockServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Use an index."}}]}`))
	}))
	defer srv.Close()

	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	c.aiCfg = config.AIConfig{
		Providers:       map[string]config.AIProvider{"mock": {BaseURL: srv.URL, Model: "m", APIKeySource: "none"}},
		DefaultProvider: "mock",
	}
	reply, err := c.AIAsk(context.Background(), "why is my query slow?")
	if err != nil {
		t.Fatalf("AIAsk: %v", err)
	}
	if reply != "Use an index." {
		t.Errorf("reply = %q", reply)
	}
}
