package tui

import (
	"net/http"
	"strings"
	"testing"

	"github.com/Solifugus/mcli/internal/core"
	"github.com/Solifugus/mcli/internal/core/assist"
)

func TestAssistOffByDefault(t *testing.T) {
	c, err := core.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := New(c)
	if m.assistSrv != nil {
		t.Error("live assist should be off by default")
	}
	res, _ := m.cmdAssist([]string{"status"})
	if len(res.lines) == 0 || !strings.Contains(res.lines[0], "off") {
		t.Errorf("status when off = %v", res.lines)
	}
}

// TestAssistOnPrefillFromAgent drives the whole path: .assist on starts the
// loopback endpoint, an "agent" POSTs ui_prefill over HTTP, and the event the
// model receives stages the text on the input line without submitting it.
func TestAssistOnPrefillFromAgent(t *testing.T) {
	c, err := core.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := New(c)

	_, act := m.cmdAssist([]string{"on"})
	if m.assistSrv == nil {
		t.Fatal("expected a live endpoint after .assist on")
	}
	if act.cmd == nil {
		t.Error("expected .assist on to return a listener command")
	}
	defer m.stopAssist()

	payload := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ui_prefill","arguments":{"target":"input-line","text":"select 99"}}}`
	req, _ := http.NewRequest(http.MethodPost, m.assistSrv.URL(), strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+m.assistSrv.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("agent POST: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("agent POST status = %d", resp.StatusCode)
	}

	// The tool published to the bus synchronously before responding, so the event
	// is already queued on the subscription.
	select {
	case e := <-m.assistCh:
		model, _ := m.handleAssist(assistMsg{e: e})
		got := model.(Model)
		if got.input.Value() != "select 99" {
			t.Errorf("input line = %q, want the staged prefill (unsubmitted)", got.input.Value())
		}
	default:
		t.Fatal("no guidance event was delivered to the model")
	}
}

func TestAssistOffRemovesSession(t *testing.T) {
	c, err := core.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := New(c)
	m.cmdAssist([]string{"on"})
	if !c.LiveSession() {
		t.Fatal("expected a live subscriber while assist is on")
	}
	m.cmdAssist([]string{"off"})
	if m.assistSrv != nil || c.LiveSession() {
		t.Error("expected assist off to close the endpoint and drop the subscription")
	}
}

func TestHandleAssistDemoRenders(t *testing.T) {
	c, err := core.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := New(c)
	// A demo with no live server should still render (and simply not re-arm).
	_, cmd := m.handleAssist(assistMsg{e: assist.Event{
		Kind: assist.KindDemo,
		Steps: []assist.Step{
			{Title: "Connect", Description: "attach to the server", Action: ".connect gbasic"},
		},
	}})
	if cmd == nil {
		t.Error("expected a print command for the walkthrough")
	}
}
