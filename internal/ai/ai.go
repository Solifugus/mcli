// Package ai holds the OpenAI-compatible chat client and the context assembly
// that mcli's .ai commands use. It is UI-agnostic — the core calls it, and both
// front-ends inherit the behavior. AI features are optional and never execute
// SQL: a completion is text the user reads, copies, and runs deliberately.
// See docs/mcli-design.md §9, §20.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// defaultBaseURL is the OpenAI endpoint used when a provider omits base_url.
const defaultBaseURL = "https://api.openai.com/v1"

// Role constants for chat messages.
const (
	RoleSystem = "system"
	RoleUser   = "user"
)

// Message is one chat turn.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Provider is a resolved AI provider: an OpenAI-compatible endpoint, a model,
// and (already resolved) an API key. base_url defaults to OpenAI when empty, so
// the same client serves a local Ollama (`http://localhost:11434/v1`, no key)
// and hosted OpenAI alike.
type Provider struct {
	BaseURL string
	Model   string
	APIKey  string
}

// Client performs chat completions against a provider.
type Client struct {
	HTTP *http.Client
}

// New returns a Client with a sane default timeout. Pass a custom *http.Client
// (e.g. an httptest transport) by constructing Client directly.
func New() *Client {
	return &Client{HTTP: &http.Client{Timeout: 60 * time.Second}}
}

// chatRequest/chatResponse mirror the subset of the OpenAI chat-completions API
// mcli uses. Temperature and other tuning are deliberately omitted: newer models
// reject non-default values, and the minimal body maximizes compatibility.
type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}

type chatResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Complete sends the messages to the provider and returns the assistant's reply.
func (c *Client) Complete(ctx context.Context, p Provider, msgs []Message) (string, error) {
	if p.Model == "" {
		return "", fmt.Errorf("ai: provider has no model configured")
	}
	body, err := json.Marshal(chatRequest{Model: p.Model, Messages: msgs})
	if err != nil {
		return "", err
	}
	base := p.BaseURL
	if base == "" {
		base = defaultBaseURL
	}
	url := strings.TrimRight(base, "/") + "/chat/completions"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}

	httpc := c.HTTP
	if httpc == nil {
		httpc = http.DefaultClient
	}
	resp, err := httpc.Do(req)
	if err != nil {
		return "", fmt.Errorf("ai: request failed: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var parsed chatResponse
	_ = json.Unmarshal(data, &parsed)

	if resp.StatusCode != http.StatusOK {
		if parsed.Error != nil && parsed.Error.Message != "" {
			return "", fmt.Errorf("ai: %s (%d)", parsed.Error.Message, resp.StatusCode)
		}
		return "", fmt.Errorf("ai: provider returned %d: %s", resp.StatusCode, snippet(data))
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("ai: provider returned no choices")
	}
	return strings.TrimSpace(parsed.Choices[0].Message.Content), nil
}

// snippet trims a response body for an error message.
func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		return s[:300] + "…"
	}
	return s
}
