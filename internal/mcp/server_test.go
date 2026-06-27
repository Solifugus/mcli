package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Solifugus/mcli/internal/core"
)

// drive runs the server over a scripted sequence of request lines and returns
// the decoded responses (one per line of output).
func drive(t *testing.T, requests ...string) []rpcResponse {
	t.Helper()
	c, err := core.Open(t.TempDir())
	if err != nil {
		t.Fatalf("core.Open: %v", err)
	}
	in := strings.NewReader(strings.Join(requests, "\n") + "\n")
	var out strings.Builder
	if err := Serve(context.Background(), c, in, &out); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	return decodeResponses(t, out.String())
}

func decodeResponses(t *testing.T, raw string) []rpcResponse {
	t.Helper()
	var resps []rpcResponse
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		if line == "" {
			continue
		}
		var r rpcResponse
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("decode response %q: %v", line, err)
		}
		resps = append(resps, r)
	}
	return resps
}

// toolText extracts the text content of a tools/call result and whether it was
// flagged isError.
func toolText(t *testing.T, r rpcResponse) (string, bool) {
	t.Helper()
	b, _ := json.Marshal(r.Result)
	var res struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(b, &res); err != nil {
		t.Fatalf("decode tool result: %v", err)
	}
	if len(res.Content) == 0 {
		t.Fatalf("tool result had no content: %s", b)
	}
	return res.Content[0].Text, res.IsError
}

func call(name string, args map[string]any) string {
	argsJSON, _ := json.Marshal(args)
	params, _ := json.Marshal(map[string]any{"name": name, "arguments": json.RawMessage(argsJSON)})
	req, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": json.RawMessage(params),
	})
	return string(req)
}

func TestInitialize(t *testing.T) {
	resps := drive(t, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`)
	if len(resps) != 1 {
		t.Fatalf("got %d responses, want 1", len(resps))
	}
	b, _ := json.Marshal(resps[0].Result)
	var res struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name string `json:"name"`
		} `json:"serverInfo"`
		Capabilities map[string]any `json:"capabilities"`
	}
	_ = json.Unmarshal(b, &res)
	if res.ProtocolVersion != "2025-03-26" {
		t.Errorf("protocolVersion = %q, want the client's echoed value", res.ProtocolVersion)
	}
	if res.ServerInfo.Name != "mcli" {
		t.Errorf("serverInfo.name = %q", res.ServerInfo.Name)
	}
	if _, ok := res.Capabilities["tools"]; !ok {
		t.Errorf("capabilities missing tools: %s", b)
	}
}

func TestInitializeDefaultsVersion(t *testing.T) {
	resps := drive(t, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	b, _ := json.Marshal(resps[0].Result)
	if !strings.Contains(string(b), protocolVersion) {
		t.Errorf("blank client version should default to %q: %s", protocolVersion, b)
	}
}

func TestNotificationGetsNoReply(t *testing.T) {
	// A notification (no id) must not produce a response line.
	resps := drive(t, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if len(resps) != 0 {
		t.Errorf("notification produced %d responses, want 0", len(resps))
	}
}

func TestToolsList(t *testing.T) {
	resps := drive(t, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	b, _ := json.Marshal(resps[0].Result)
	for _, want := range []string{"list_workspaces", "connect_server", "run_query", "describe_table", "import_file"} {
		if !strings.Contains(string(b), `"`+want+`"`) {
			t.Errorf("tools/list missing %q", want)
		}
	}
}

func TestUnknownMethod(t *testing.T) {
	resps := drive(t, `{"jsonrpc":"2.0","id":3,"method":"does/not/exist"}`)
	if resps[0].Error == nil || resps[0].Error.Code != codeMethodNotFound {
		t.Errorf("unknown method error = %+v, want code %d", resps[0].Error, codeMethodNotFound)
	}
}

func TestParseError(t *testing.T) {
	resps := drive(t, `{not valid json`)
	if resps[0].Error == nil || resps[0].Error.Code != codeParseError {
		t.Errorf("parse error = %+v, want code %d", resps[0].Error, codeParseError)
	}
}

func TestCallListWorkspaces(t *testing.T) {
	resps := drive(t, call("list_workspaces", nil))
	text, isErr := toolText(t, resps[0])
	if isErr {
		t.Fatalf("list_workspaces errored: %s", text)
	}
	if !strings.Contains(text, "default") {
		t.Errorf("list_workspaces = %q, want it to include the default workspace", text)
	}
}

func TestCallWorkspaceStatus(t *testing.T) {
	resps := drive(t, call("get_workspace_status", nil))
	text, isErr := toolText(t, resps[0])
	if isErr {
		t.Fatalf("status errored: %s", text)
	}
	if !strings.Contains(text, `"connected": false`) {
		t.Errorf("status = %q, want connected:false with no connection", text)
	}
}

func TestCallUnknownTool(t *testing.T) {
	resps := drive(t, call("frobnicate", nil))
	text, isErr := toolText(t, resps[0])
	if !isErr {
		t.Errorf("unknown tool should be an isError result, got %q", text)
	}
	if !strings.Contains(text, "unknown tool") {
		t.Errorf("unknown tool text = %q", text)
	}
}

func TestCallRunQueryNoConnection(t *testing.T) {
	resps := drive(t, call("run_query", map[string]any{"sql": "select 1"}))
	text, isErr := toolText(t, resps[0])
	if !isErr {
		t.Errorf("run_query without a connection should error, got %q", text)
	}
}

// TestRunQueryDangerousRefused proves the safety guard applies headlessly: a
// dangerous statement is refused before execution unless confirm=true. This
// needs no DB connection because the guard classifies the SQL first.
func TestRunQueryDangerousRefused(t *testing.T) {
	resps := drive(t, call("run_query", map[string]any{"sql": "drop table users"}))
	text, isErr := toolText(t, resps[0])
	if !isErr {
		t.Fatalf("dropping a table should be refused, got %q", text)
	}
	if !strings.Contains(text, "confirm=true") {
		t.Errorf("refusal should mention confirm=true: %q", text)
	}
}

// TestRunQueryConfirmBypassesGate shows confirm=true passes the guard and the
// statement then fails only because there is no connection — i.e. the gate, not
// the executor, was what stopped the unconfirmed call above.
func TestRunQueryConfirmBypassesGate(t *testing.T) {
	resps := drive(t, call("run_query", map[string]any{"sql": "drop table users", "confirm": true}))
	text, isErr := toolText(t, resps[0])
	if !isErr {
		t.Fatalf("expected a no-connection error, got %q", text)
	}
	if strings.Contains(text, "confirm=true") {
		t.Errorf("confirm=true should clear the gate, not re-trigger it: %q", text)
	}
}

func TestWriteThenReadWorkspaceFile(t *testing.T) {
	c, err := core.Open(t.TempDir())
	if err != nil {
		t.Fatalf("core.Open: %v", err)
	}
	in := strings.NewReader(strings.Join([]string{
		call("write_workspace_file", map[string]any{"name": "q1", "content": "select 42"}),
		call("read_workspace_file", map[string]any{"name": "q1"}),
	}, "\n") + "\n")
	var out strings.Builder
	if err := Serve(context.Background(), c, in, &out); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	resps := decodeResponses(t, out.String())
	if len(resps) != 2 {
		t.Fatalf("got %d responses, want 2", len(resps))
	}
	text, isErr := toolText(t, resps[1])
	if isErr {
		t.Fatalf("read errored: %s", text)
	}
	if !strings.Contains(text, "select 42") {
		t.Errorf("read back %q, want the written content", text)
	}
}

func TestLintSQLTool(t *testing.T) {
	resps := drive(t, call("lint_sql", map[string]any{"sql": "delete from users"}))
	text, isErr := toolText(t, resps[0])
	if isErr {
		t.Fatalf("lint_sql reported an error: %s", text)
	}
	// The findings are a JSON array; the dangerous DELETE must appear.
	if !strings.Contains(text, "dangerous-sql") {
		t.Errorf("lint_sql output missing the dangerous-sql finding: %s", text)
	}
}

func TestLintSQLToolClean(t *testing.T) {
	resps := drive(t, call("lint_sql", map[string]any{"sql": "select id, name from t where id = 1"}))
	text, isErr := toolText(t, resps[0])
	if isErr {
		t.Fatalf("lint_sql reported an error: %s", text)
	}
	if strings.Contains(text, "dangerous-sql") || strings.Contains(text, "select-star") {
		t.Errorf("clean SQL should have no safety/style findings: %s", text)
	}
}
