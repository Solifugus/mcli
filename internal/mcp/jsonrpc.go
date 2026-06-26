package mcp

import (
	"bufio"
	"encoding/json"
	"io"
)

// JSON-RPC 2.0 framing for the MCP stdio transport. Messages are newline-
// delimited JSON objects: one request or notification per line, one response
// per line. See https://www.jsonrpc.org/specification and the MCP transport
// spec (stdio).

const jsonrpcVersion = "2.0"

// rpcRequest is an incoming JSON-RPC message. A message with no ID is a
// notification and gets no reply.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"` // raw so we can echo it verbatim (number or string)
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// isNotification reports whether the message expects no response.
func (r rpcRequest) isNotification() bool { return len(r.ID) == 0 }

// rpcResponse is an outgoing JSON-RPC reply. Exactly one of Result/Error is set.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// rpcError is a JSON-RPC error object.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Standard JSON-RPC error codes.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
)

// conn reads and writes newline-delimited JSON-RPC over a stream.
type conn struct {
	r   *bufio.Scanner
	w   io.Writer
	enc *json.Encoder
}

func newConn(in io.Reader, out io.Writer) *conn {
	sc := bufio.NewScanner(in)
	// MCP messages can be large (schemas, query SQL); allow up to 8 MiB lines.
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	return &conn{r: sc, w: out, enc: json.NewEncoder(out)}
}

// read returns the next message, or io.EOF when the stream closes.
func (c *conn) read() (rpcRequest, error) {
	if !c.r.Scan() {
		if err := c.r.Err(); err != nil {
			return rpcRequest{}, err
		}
		return rpcRequest{}, io.EOF
	}
	var req rpcRequest
	if err := json.Unmarshal(c.r.Bytes(), &req); err != nil {
		// Surface as a parse-level problem; the caller decides how to respond.
		return rpcRequest{}, &parseError{err}
	}
	return req, nil
}

// parseError marks a line that was not valid JSON.
type parseError struct{ err error }

func (e *parseError) Error() string { return e.err.Error() }

// reply writes a successful response echoing the request ID.
func (c *conn) reply(id json.RawMessage, result interface{}) error {
	return c.enc.Encode(rpcResponse{JSONRPC: jsonrpcVersion, ID: id, Result: result})
}

// replyError writes an error response.
func (c *conn) replyError(id json.RawMessage, code int, msg string) error {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	return c.enc.Encode(rpcResponse{JSONRPC: jsonrpcVersion, ID: id, Error: &rpcError{Code: code, Message: msg}})
}
