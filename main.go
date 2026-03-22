// Command wx-mcp is a WeChat Bot MCP server.
//
// Usage:
//
//	# stdio transport (default, for Claude Desktop / Cursor / etc.)
//	wx-mcp
//
//	# SSE transport (for web clients or remote access)
//	wx-mcp -transport sse -addr :8081
package main

import (
	"flag"
	"log"
	"os"

	"github.com/yindex/wx-mcp/internal/mcp"
	"github.com/yindex/wx-mcp/internal/state"
	"github.com/yindex/wx-mcp/tools"
)

func main() {
	transport := flag.String("transport", "stdio", "transport type: stdio | sse")
	addr := flag.String("addr", ":8081", "HTTP listen address (SSE mode only)")
	flag.Parse()

	// Shared state manager (accounts, messages).
	mgr := state.NewManager()

	// MCP server core.
	srv := mcp.NewServer("wx-mcp", "1.0.0")

	// Register all WeChat tools.
	tools.RegisterAll(srv, mgr)

	switch *transport {
	case "sse":
		if err := mcp.ServeSSE(srv, *addr); err != nil {
			log.Fatalf("SSE server error: %v", err)
		}
	case "stdio":
		fallthrough
	default:
		mcp.ServeStdio(srv, os.Stdin, os.Stdout)
	}
}
