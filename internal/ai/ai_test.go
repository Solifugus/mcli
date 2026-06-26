package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCompleteSuccess(t *testing.T) {
	var gotAuth, gotPath string
	var gotReq chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotReq)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"  hello world  "}}]}`))
	}))
	defer srv.Close()

	c := New()
	reply, err := c.Complete(context.Background(),
		Provider{BaseURL: srv.URL, Model: "test-model", APIKey: "sk-abc"},
		[]Message{{Role: RoleUser, Content: "hi"}})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if reply != "hello world" {
		t.Errorf("reply = %q, want trimmed 'hello world'", reply)
	}
	if gotAuth != "Bearer sk-abc" {
		t.Errorf("auth header = %q", gotAuth)
	}
	if !strings.HasSuffix(gotPath, "/chat/completions") {
		t.Errorf("path = %q", gotPath)
	}
	if gotReq.Model != "test-model" || len(gotReq.Messages) != 1 {
		t.Errorf("request body = %+v", gotReq)
	}
}

func TestCompleteNoAuthHeaderWhenKeyless(t *testing.T) {
	got := "unset"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()
	if _, err := New().Complete(context.Background(), Provider{BaseURL: srv.URL, Model: "m"}, nil); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got != "" {
		t.Errorf("keyless request sent Authorization = %q", got)
	}
}

func TestCompleteAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"bad key"}}`))
	}))
	defer srv.Close()
	_, err := New().Complete(context.Background(), Provider{BaseURL: srv.URL, Model: "m"}, nil)
	if err == nil || !strings.Contains(err.Error(), "bad key") {
		t.Errorf("error = %v, want it to surface 'bad key'", err)
	}
}

func TestCompleteRequiresModel(t *testing.T) {
	if _, err := New().Complete(context.Background(), Provider{}, nil); err == nil {
		t.Error("Complete without a model should error")
	}
}

func TestSystemPromptIncludesContext(t *testing.T) {
	s := systemPrompt(Context{Dialect: "postgres", Environment: "prod", Database: "etl", Tables: []string{"a", "b"}})
	for _, want := range []string{"postgres", "prod", "etl", "a, b", "cannot execute"} {
		if !strings.Contains(s, want) {
			t.Errorf("system prompt missing %q: %s", want, s)
		}
	}
}

func TestMessageBuilders(t *testing.T) {
	cx := Context{Dialect: "mysql"}
	if msgs := AskMessages(cx, "why slow?"); len(msgs) != 2 || msgs[1].Content != "why slow?" {
		t.Errorf("AskMessages = %+v", msgs)
	}
	ex := ExplainMessages(cx, "select 1")
	if !strings.Contains(ex[1].Content, "```sql") || !strings.Contains(ex[1].Content, "select 1") {
		t.Errorf("ExplainMessages body = %q", ex[1].Content)
	}
	fx := FixMessages(cx, "selct 1", "syntax error near selct")
	if !strings.Contains(fx[1].Content, "selct 1") || !strings.Contains(fx[1].Content, "syntax error") {
		t.Errorf("FixMessages body = %q", fx[1].Content)
	}
}
