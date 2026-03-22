// Package mcp implements the Model Context Protocol (MCP) 2024-11-05.
// JSON-RPC 2.0 over stdio or HTTP/SSE transport.
package mcp

import "encoding/json"

// ─── JSON-RPC 2.0 ────────────────────────────────────────────────────────────

// Request is a JSON-RPC 2.0 request message.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"` // string | number | null
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is a JSON-RPC 2.0 response message.
type Response struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *RPCError `json:"error,omitempty"`
}

// Notification is a JSON-RPC 2.0 notification (no ID, no response expected).
type Notification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// RPCError is a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *RPCError) Error() string { return e.Message }

// Standard JSON-RPC error codes.
const (
	ErrParse          = -32700
	ErrInvalidRequest = -32600
	ErrMethodNotFound = -32601
	ErrInvalidParams  = -32602
	ErrInternal       = -32603
)

func errResponse(id any, code int, msg string) *Response {
	return &Response{JSONRPC: "2.0", ID: id, Error: &RPCError{Code: code, Message: msg}}
}

func okResponse(id any, result any) *Response {
	return &Response{JSONRPC: "2.0", ID: id, Result: result}
}

// ─── MCP Protocol Types ───────────────────────────────────────────────────────

const ProtocolVersion = "2024-11-05"

// InitializeParams — params for the "initialize" request.
type InitializeParams struct {
	ProtocolVersion string            `json:"protocolVersion"`
	Capabilities    ClientCapabilities `json:"capabilities"`
	ClientInfo      Implementation    `json:"clientInfo"`
}

// InitializeResult — result for the "initialize" request.
type InitializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ServerCapabilities `json:"capabilities"`
	ServerInfo      Implementation     `json:"serverInfo"`
}

// Implementation identifies a server or client.
type Implementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ServerCapabilities lists what the server supports.
type ServerCapabilities struct {
	Tools     *ToolsCapability     `json:"tools,omitempty"`
	Resources *ResourcesCapability `json:"resources,omitempty"`
	Prompts   *PromptsCapability   `json:"prompts,omitempty"`
}

// ClientCapabilities lists what the client supports.
type ClientCapabilities struct {
	Roots    *RootsCapability    `json:"roots,omitempty"`
	Sampling *SamplingCapability `json:"sampling,omitempty"`
}

type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}
type ResourcesCapability struct {
	Subscribe   bool `json:"subscribe,omitempty"`
	ListChanged bool `json:"listChanged,omitempty"`
}
type PromptsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}
type RootsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}
type SamplingCapability struct{}

// ─── Tools ────────────────────────────────────────────────────────────────────

// Tool describes a callable tool.
type Tool struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	InputSchema JSONSchema `json:"inputSchema"`
}

// JSONSchema is a minimal JSON Schema object.
type JSONSchema struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties,omitempty"`
	Required   []string            `json:"required,omitempty"`
}

// Property is a single JSON Schema property definition.
type Property struct {
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Default     any      `json:"default,omitempty"`
}

// ListToolsResult — result for "tools/list".
type ListToolsResult struct {
	Tools []Tool `json:"tools"`
}

// CallToolParams — params for "tools/call".
type CallToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// CallToolResult — result for "tools/call".
type CallToolResult struct {
	Content []Content `json:"content"`
	IsError bool      `json:"isError,omitempty"`
}

// Content is a piece of content returned by a tool.
type Content struct {
	Type     string    `json:"type"`               // "text" | "image" | "resource"
	Text     string    `json:"text,omitempty"`     // for type="text"
	Data     string    `json:"data,omitempty"`     // for type="image" (base64)
	MIMEType string    `json:"mimeType,omitempty"` // for type="image"
}

// textContent creates a simple text content item.
func textContent(text string) Content { return Content{Type: "text", Text: text} }

// toolOK wraps text as a successful CallToolResult (unexported, for internal use).
func toolOK(text string) *CallToolResult {
	return &CallToolResult{Content: []Content{textContent(text)}}
}

// toolErr wraps text as an error CallToolResult (unexported, for internal use).
func toolErr(text string) *CallToolResult {
	return &CallToolResult{IsError: true, Content: []Content{textContent(text)}}
}

// ToolOK returns a successful CallToolResult with the given text (exported).
func ToolOK(text string) *CallToolResult { return toolOK(text) }

// ToolErr returns an error CallToolResult with the given text (exported).
func ToolErr(text string) *CallToolResult { return toolErr(text) }
