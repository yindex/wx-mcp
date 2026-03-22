// Package tools registers all WeChat MCP tools onto the server.
package tools

import (
	"github.com/yindex/wx-mcp/internal/mcp"
	"github.com/yindex/wx-mcp/internal/state"
)

// RegisterAll registers every WeChat tool onto srv.
func RegisterAll(srv *mcp.Server, mgr *state.Manager) {
	registerAccountTools(srv, mgr)
	registerLoginTools(srv, mgr)
	registerMessageTools(srv, mgr)
}
