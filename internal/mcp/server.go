package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
)

// ToolHandler is the function signature for all tool implementations.
// args is the raw JSON arguments object; returns result or error.
type ToolHandler func(ctx context.Context, args json.RawMessage) (*CallToolResult, error)

// Server is the MCP server core: registers tools and dispatches JSON-RPC requests.
type Server struct {
	name    string
	version string

	mu       sync.RWMutex
	tools    []Tool
	handlers map[string]ToolHandler
}

// NewServer creates a new MCP server with the given name and version.
func NewServer(name, version string) *Server {
	return &Server{
		name:     name,
		version:  version,
		handlers: make(map[string]ToolHandler),
	}
}

// RegisterTool registers a tool definition and its handler function.
func (s *Server) RegisterTool(tool Tool, handler ToolHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools = append(s.tools, tool)
	s.handlers[tool.Name] = handler
}

// Handle processes a single JSON-RPC request and returns the response.
// Returns nil for notifications (no response required).
func (s *Server) Handle(ctx context.Context, raw []byte) *Response {
	// Try to parse as a request first.
	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		return errResponse(nil, ErrParse, "parse error: "+err.Error())
	}

	if req.JSONRPC != "2.0" {
		return errResponse(req.ID, ErrInvalidRequest, "invalid jsonrpc version")
	}

	// Notifications have no ID — process but do not respond.
	if req.ID == nil {
		s.handleNotification(ctx, req.Method, req.Params)
		return nil
	}

	return s.dispatch(ctx, req)
}

func (s *Server) dispatch(ctx context.Context, req Request) *Response {
	log.Printf("[mcp] → %s (id=%v)", req.Method, req.ID)

	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "ping":
		return okResponse(req.ID, struct{}{})
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(ctx, req)
	default:
		return errResponse(req.ID, ErrMethodNotFound, fmt.Sprintf("method not found: %s", req.Method))
	}
}

func (s *Server) handleInitialize(req Request) *Response {
	result := InitializeResult{
		ProtocolVersion: ProtocolVersion,
		Capabilities: ServerCapabilities{
			Tools: &ToolsCapability{ListChanged: false},
		},
		ServerInfo: Implementation{Name: s.name, Version: s.version},
	}
	return okResponse(req.ID, result)
}

func (s *Server) handleToolsList(req Request) *Response {
	s.mu.RLock()
	tools := make([]Tool, len(s.tools))
	copy(tools, s.tools)
	s.mu.RUnlock()
	return okResponse(req.ID, ListToolsResult{Tools: tools})
}

func (s *Server) handleToolsCall(ctx context.Context, req Request) *Response {
	var p CallToolParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errResponse(req.ID, ErrInvalidParams, "invalid params: "+err.Error())
	}

	s.mu.RLock()
	handler, ok := s.handlers[p.Name]
	s.mu.RUnlock()

	if !ok {
		return okResponse(req.ID, toolErr(fmt.Sprintf("unknown tool: %s", p.Name)))
	}

	result, err := handler(ctx, p.Arguments)
	if err != nil {
		log.Printf("[mcp] tool %s error: %v", p.Name, err)
		return okResponse(req.ID, toolErr(err.Error()))
	}
	return okResponse(req.ID, result)
}

func (s *Server) handleNotification(_ context.Context, method string, _ json.RawMessage) {
	log.Printf("[mcp] notification: %s", method)
	// "initialized" and other notifications are acknowledged but require no action.
}
