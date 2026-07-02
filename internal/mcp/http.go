package mcp

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Solifugus/mcli/internal/core"
)

// The live-session HTTP transport (design §26). Unlike `mcli mcp serve` (a
// separate process with its own core over stdio), this endpoint is hosted by the
// *running* app and bound to the *live* core, so an external agent can guide the
// user in the surface they are looking at. It speaks the MCP Streamable HTTP
// transport: JSON-RPC 2.0 over HTTP POST to a single endpoint, returning an
// application/json response (server→client SSE streaming is a later addition and
// is not required for tool calls).
//
// Security, because mcli may be connected to production:
//   - bound to loopback only (never 0.0.0.0),
//   - every request must carry the per-session bearer token, and
//   - the Origin header is validated to defeat DNS-rebinding from a browser.
//
// The core's safety guards (read-only, dangerous-SQL, prod writes) apply
// unchanged: every tool call routes through the same core as the TUI.

const (
	// mcpPath is the single Streamable HTTP endpoint.
	mcpPath = "/mcp"
	// sessionFile is written under ~/.mcli so a co-operating agent (e.g. Conatus)
	// can discover the live endpoint and its token.
	sessionFile = "session.json"
	// maxBody caps a single request body (schemas / SQL can be large).
	maxBody = 8 << 20
)

// HTTPServer is a running live-session endpoint over the shared core.
type HTTPServer struct {
	srv     *http.Server
	ln      net.Listener
	core    *core.Core
	token   string
	rootDir string

	// mu serializes dispatch so concurrent agent requests do not interleave
	// against the shared live core. (Cross-goroutine mutation between the TUI
	// event loop and this endpoint is a separate, tracked concern.)
	mu sync.Mutex
}

// ServeHTTP starts the live-session endpoint on a loopback port and writes the
// discovery descriptor (session.json). host is typically "127.0.0.1:0" to pick a
// free port. Call Close to stop it and remove the descriptor.
func ServeHTTP(c *core.Core, host string) (*HTTPServer, error) {
	if host == "" {
		host = "127.0.0.1:0"
	}
	if !isLoopbackHost(host) {
		return nil, fmt.Errorf("mcp: live endpoint must bind loopback, got %q", host)
	}
	ln, err := net.Listen("tcp", host)
	if err != nil {
		return nil, err
	}
	tok, err := randomToken()
	if err != nil {
		ln.Close()
		return nil, err
	}
	h := &HTTPServer{ln: ln, core: c, token: tok, rootDir: c.ConfigRoot()}
	mux := http.NewServeMux()
	mux.HandleFunc(mcpPath, h.handle)
	h.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	if err := h.writeSession(); err != nil {
		ln.Close()
		return nil, err
	}
	go h.srv.Serve(ln)
	return h, nil
}

// URL is the endpoint an agent connects to.
func (h *HTTPServer) URL() string { return "http://" + h.ln.Addr().String() + mcpPath }

// Token is the per-session bearer token.
func (h *HTTPServer) Token() string { return h.token }

// Close stops the server and removes the discovery descriptor.
func (h *HTTPServer) Close() error {
	_ = os.Remove(filepath.Join(h.rootDir, sessionFile))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return h.srv.Shutdown(ctx)
}

func (h *HTTPServer) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		// GET would open a server→client SSE stream in full Streamable HTTP; not
		// implemented yet. Everything else is unsupported.
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.authOK(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !originOK(r) {
		http.Error(w, "forbidden origin", http.StatusForbidden)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}
	var req rpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusOK, rpcResponse{
			JSONRPC: jsonrpcVersion, ID: json.RawMessage("null"),
			Error: &rpcError{Code: codeParseError, Message: "parse error: " + err.Error()},
		})
		return
	}

	// Dispatch through the same handlers as the stdio server, capturing the reply
	// (if any) into a buffer. Serialized against other live-session requests.
	var buf bytes.Buffer
	s := &Server{core: h.core, conn: newConn(nil, &buf)}
	h.mu.Lock()
	s.dispatch(r.Context(), req)
	h.mu.Unlock()

	// A notification (or ping notification) produces no reply body.
	if buf.Len() == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

// authOK checks the bearer token in constant time.
func (h *HTTPServer) authOK(r *http.Request) bool {
	const prefix = "Bearer "
	got := r.Header.Get("Authorization")
	if !strings.HasPrefix(got, prefix) {
		return false
	}
	got = strings.TrimPrefix(got, prefix)
	return subtle.ConstantTimeCompare([]byte(got), []byte(h.token)) == 1
}

// originOK allows requests with no Origin (non-browser clients) and requests
// whose Origin is a loopback host; anything else is rejected to prevent a web
// page from driving the live session via DNS rebinding.
func originOK(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	i := strings.Index(o, "://")
	if i < 0 {
		return false
	}
	host := o[i+3:]
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "[::1]"
}

// sessionInfo is the discovery descriptor written to ~/.mcli/session.json.
type sessionInfo struct {
	URL       string `json:"url"`
	Token     string `json:"token"`
	Transport string `json:"transport"`
	PID       int    `json:"pid"`
}

func (h *HTTPServer) writeSession() error {
	info := sessionInfo{URL: h.URL(), Token: h.token, Transport: "streamable-http", PID: os.Getpid()}
	b, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	// 0600: the token is a credential.
	return os.WriteFile(filepath.Join(h.rootDir, sessionFile), b, 0o600)
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func isLoopbackHost(hostport string) bool {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = hostport
	}
	if host == "localhost" || host == "" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
