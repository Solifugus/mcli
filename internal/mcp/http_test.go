package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Solifugus/mcli/internal/core"
)

func newHTTP(t *testing.T) (*HTTPServer, *core.Core) {
	t.Helper()
	c, err := core.Open(t.TempDir())
	if err != nil {
		t.Fatalf("core.Open: %v", err)
	}
	h, err := ServeHTTP(c, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ServeHTTP: %v", err)
	}
	t.Cleanup(func() { _ = h.Close() })
	return h, c
}

// post sends one JSON-RPC line and returns the HTTP status and decoded body.
func post(t *testing.T, h *HTTPServer, auth, origin, payload string) (int, rpcResponse) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, h.URL(), strings.NewReader(payload))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var r rpcResponse
	if len(body) > 0 {
		_ = json.Unmarshal(body, &r)
	}
	return resp.StatusCode, r
}

func bearer(h *HTTPServer) string { return "Bearer " + h.Token() }

func TestHTTPRequiresToken(t *testing.T) {
	h, _ := newHTTP(t)
	if status, _ := post(t, h, "", "", `{"jsonrpc":"2.0","id":1,"method":"ping"}`); status != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401", status)
	}
	if status, _ := post(t, h, "Bearer wrong", "", `{"jsonrpc":"2.0","id":1,"method":"ping"}`); status != http.StatusUnauthorized {
		t.Errorf("bad token: status = %d, want 401", status)
	}
}

func TestHTTPRejectsForeignOrigin(t *testing.T) {
	h, _ := newHTTP(t)
	if status, _ := post(t, h, bearer(h), "https://evil.example.com", `{"jsonrpc":"2.0","id":1,"method":"ping"}`); status != http.StatusForbidden {
		t.Errorf("foreign origin: status = %d, want 403", status)
	}
	// A loopback origin is allowed.
	if status, _ := post(t, h, bearer(h), "http://localhost:3000", `{"jsonrpc":"2.0","id":1,"method":"ping"}`); status != http.StatusOK {
		t.Errorf("loopback origin: status = %d, want 200", status)
	}
}

func TestHTTPGetNotAllowed(t *testing.T) {
	h, _ := newHTTP(t)
	req, _ := http.NewRequest(http.MethodGet, h.URL(), nil)
	req.Header.Set("Authorization", bearer(h))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET status = %d, want 405", resp.StatusCode)
	}
}

func TestHTTPInitializeAndToolsList(t *testing.T) {
	h, _ := newHTTP(t)
	status, r := post(t, h, bearer(h), "", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`)
	if status != http.StatusOK || r.Error != nil {
		t.Fatalf("initialize: status=%d err=%+v", status, r.Error)
	}
	status, _ = post(t, h, bearer(h), "", `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	if status != http.StatusOK {
		t.Fatalf("tools/list status = %d", status)
	}
}

func TestHTTPNotificationGets202(t *testing.T) {
	h, _ := newHTTP(t)
	status, _ := post(t, h, bearer(h), "", `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if status != http.StatusAccepted {
		t.Errorf("notification status = %d, want 202", status)
	}
}

func TestHTTPUIPrefillReachesLiveSession(t *testing.T) {
	h, c := newHTTP(t)
	ch, unsub := c.Assist().Subscribe()
	defer unsub()

	status, r := post(t, h, bearer(h), "",
		`{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"ui_prefill","arguments":{"target":"input-line","text":"select 7"}}}`)
	if status != http.StatusOK || r.Error != nil {
		t.Fatalf("ui_prefill over HTTP: status=%d err=%+v", status, r.Error)
	}
	select {
	case e := <-ch:
		if string(e.Kind) != "prefill" || e.Text != "select 7" || e.Target != "input-line" {
			t.Errorf("unexpected event: %+v", e)
		}
	default:
		t.Error("prefill event was not delivered to the live subscriber over HTTP")
	}
}

func TestHTTPWritesAndRemovesSessionFile(t *testing.T) {
	h, c := newHTTP(t)
	path := filepath.Join(c.ConfigRoot(), "session.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("session.json not written: %v", err)
	}
	var info sessionInfo
	if err := json.Unmarshal(b, &info); err != nil {
		t.Fatalf("session.json invalid: %v", err)
	}
	if info.Token != h.Token() || !strings.HasPrefix(info.URL, "http://127.0.0.1:") {
		t.Errorf("session.json = %+v", info)
	}
	if err := h.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("session.json should be removed on Close, stat err = %v", err)
	}
}

func TestHTTPConcurrentRequests(t *testing.T) {
	h, _ := newHTTP(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if status, _ := post(t, h, bearer(h), "", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`); status != http.StatusOK {
				t.Errorf("concurrent tools/list status = %d", status)
			}
		}()
	}
	wg.Wait()
}
