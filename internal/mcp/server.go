// Package mcp is the stdio MCP front-end. Each tool is a thin wrapper over a
// core function and inherits the same safety guardrails as the TUI. The server
// speaks JSON-RPC 2.0 over newline-delimited stdio (the MCP stdio transport).
// See docs/mcli-design.md §21.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/Solifugus/mcli/internal/core"
)

// protocolVersion is the MCP revision this server defaults to advertising. If a
// client requests a specific version in initialize, we echo it back (the
// pragmatic interoperable choice), falling back to this when none is given.
const protocolVersion = "2025-06-18"

const serverName = "mcli"

// Version is the server version reported in initialize. main wires it to the
// binary version; tests and direct callers get a sensible default.
var Version = "dev"

// Server is a stateful MCP session over one stdio connection. It holds the core
// so tools can mutate shared state (current workspace, connection) between calls.
type Server struct {
	core *core.Core
	conn *conn
}

// Serve runs the MCP server loop until the input stream closes (EOF) or ctx is
// cancelled. It is used by both `mcli mcp serve` (over os.Stdin/os.Stdout) and
// `\mcp serve` in the TUI (over the suspended terminal's stdio).
func Serve(ctx context.Context, c *core.Core, in io.Reader, out io.Writer) error {
	s := &Server{core: c, conn: newConn(in, out)}
	return s.loop(ctx)
}

func (s *Server) loop(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return nil // cancellation is a clean shutdown, not an error
		}
		req, err := s.conn.read()
		switch {
		case errors.Is(err, io.EOF):
			return nil
		case err != nil:
			var pe *parseError
			if errors.As(err, &pe) {
				_ = s.conn.replyError(nil, codeParseError, "parse error: "+pe.Error())
				continue
			}
			return err // a real I/O error on the transport
		}
		s.dispatch(ctx, req)
	}
}

// dispatch routes one message. Notifications (no ID) never get a reply.
func (s *Server) dispatch(ctx context.Context, req rpcRequest) {
	switch req.Method {
	case "initialize":
		s.handleInitialize(req)
	case "notifications/initialized", "notifications/cancelled":
		// Notifications: nothing to acknowledge.
	case "ping":
		if !req.isNotification() {
			_ = s.conn.reply(req.ID, struct{}{})
		}
	case "tools/list":
		if !req.isNotification() {
			_ = s.conn.reply(req.ID, map[string]any{"tools": toolList()})
		}
	case "tools/call":
		s.handleToolsCall(ctx, req)
	default:
		if !req.isNotification() {
			_ = s.conn.replyError(req.ID, codeMethodNotFound, "unknown method: "+req.Method)
		}
	}
}

// initializeParams is the subset of the initialize request we read.
type initializeParams struct {
	ProtocolVersion string `json:"protocolVersion"`
}

func (s *Server) handleInitialize(req rpcRequest) {
	if req.isNotification() {
		return
	}
	var p initializeParams
	_ = json.Unmarshal(req.Params, &p) // tolerate a missing/blank version
	version := p.ProtocolVersion
	if version == "" {
		version = protocolVersion
	}
	_ = s.conn.reply(req.ID, map[string]any{
		"protocolVersion": version,
		"capabilities":    map[string]any{"tools": map[string]any{}},
		"serverInfo":      map[string]any{"name": serverName, "version": Version},
	})
}

// toolCallParams is the params object of a tools/call request.
type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func (s *Server) handleToolsCall(ctx context.Context, req rpcRequest) {
	if req.isNotification() {
		return
	}
	var p toolCallParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		_ = s.conn.replyError(req.ID, codeInvalidParams, "invalid tools/call params: "+err.Error())
		return
	}
	// Tool-layer outcomes (unknown tool, bad args, execution failure) are
	// reported as an isError result so the calling model can see and react to
	// them, per the MCP convention — not as JSON-RPC protocol errors.
	_ = s.conn.reply(req.ID, s.callTool(ctx, p.Name, p.Arguments))
}
