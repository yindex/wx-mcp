package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log"
	"os"
)

// ServeStdio runs the MCP server over stdio (newline-delimited JSON-RPC).
// Each line from r is one JSON-RPC message; responses are written to w.
// Blocks until r is closed (EOF) or ctx is cancelled.
func ServeStdio(srv *Server, r io.Reader, w io.Writer) {
	ctx := context.Background()
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4*1024*1024), 4*1024*1024) // 4 MB max line

	enc := json.NewEncoder(w)

	log.Printf("[stdio] wx-mcp MCP server ready (stdio transport)")

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		resp := srv.Handle(ctx, line)
		if resp == nil {
			// Notification — no response.
			continue
		}

		if err := enc.Encode(resp); err != nil {
			log.Printf("[stdio] encode error: %v", err)
			return
		}

		// Flush: json.Encoder writes atomically; no explicit flush needed for os.Stdout.
		if f, ok := w.(*os.File); ok {
			_ = f.Sync()
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("[stdio] scanner error: %v", err)
	}
	log.Printf("[stdio] stdin closed, exiting")
}
