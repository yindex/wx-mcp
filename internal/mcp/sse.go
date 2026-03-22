package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// sseSession represents one connected SSE client.
type sseSession struct {
	id      string
	msgCh   chan []byte // JSON-RPC responses to push to the client
	closeCh chan struct{}
}

// SSEHandler handles both GET /sse and POST /message for the MCP SSE transport.
type SSEHandler struct {
	srv      *Server
	mu       sync.RWMutex
	sessions map[string]*sseSession
}

// NewSSEHandler creates an SSEHandler wrapping the given server.
func NewSSEHandler(srv *Server) *SSEHandler {
	return &SSEHandler{
		srv:      srv,
		sessions: make(map[string]*sseSession),
	}
}

// ServeHTTP routes GET /sse and POST /message.
func (h *SSEHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimRight(r.URL.Path, "/")
	switch {
	case r.Method == http.MethodGet && (path == "/sse" || path == ""):
		h.handleSSE(w, r)
	case r.Method == http.MethodPost && path == "/message":
		h.handleMessage(w, r)
	case r.Method == http.MethodGet && path == "/health":
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"status":"ok"}`)
	default:
		http.NotFound(w, r)
	}
}

// handleSSE opens an SSE stream for a new client.
//
// Per MCP 2024-11-05 SSE spec:
//  1. Server sends an "endpoint" event whose data is the POST URL (with sessionId).
//  2. All subsequent JSON-RPC responses are sent as "message" events.
func (h *SSEHandler) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	sess := &sseSession{
		id:      uuid.New().String(),
		msgCh:   make(chan []byte, 64),
		closeCh: make(chan struct{}),
	}

	h.mu.Lock()
	h.sessions[sess.id] = sess
	h.mu.Unlock()

	log.Printf("[sse] client connected session=%s remote=%s", sess.id, r.RemoteAddr)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Send the endpoint event so the client knows where to POST messages.
	postURL := fmt.Sprintf("/message?sessionId=%s", sess.id)
	fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", postURL)
	flusher.Flush()

	// Heartbeat ticker to keep the connection alive.
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			h.removeSession(sess.id)
			log.Printf("[sse] client disconnected session=%s", sess.id)
			return

		case <-sess.closeCh:
			h.removeSession(sess.id)
			log.Printf("[sse] session closed session=%s", sess.id)
			return

		case msg := <-sess.msgCh:
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", msg)
			flusher.Flush()

		case <-ticker.C:
			fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}

// handleMessage receives a JSON-RPC request from the client (POST /message?sessionId=xxx),
// processes it, and sends the response back via the matching SSE stream.
func (h *SSEHandler) handleMessage(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionId")
	if sessionID == "" {
		http.Error(w, "missing sessionId", http.StatusBadRequest)
		return
	}

	h.mu.RLock()
	sess, ok := h.sessions[sessionID]
	h.mu.RUnlock()
	if !ok {
		http.Error(w, "unknown session", http.StatusNotFound)
		return
	}

	// CORS preflight.
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	var raw json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Respond immediately with 202 Accepted; the actual JSON-RPC response
	// is sent asynchronously via the SSE stream.
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusAccepted)

	go func() {
		ctx := context.Background()
		resp := h.srv.Handle(ctx, raw)
		if resp == nil {
			// Notification — no response to send.
			return
		}
		data, err := json.Marshal(resp)
		if err != nil {
			log.Printf("[sse] marshal error session=%s: %v", sess.id, err)
			return
		}
		select {
		case sess.msgCh <- data:
		case <-sess.closeCh:
		case <-time.After(5 * time.Second):
			log.Printf("[sse] send timeout session=%s", sess.id)
		}
	}()
}

func (h *SSEHandler) removeSession(id string) {
	h.mu.Lock()
	delete(h.sessions, id)
	h.mu.Unlock()
}

// ServeSSE starts an HTTP server with the SSE transport on the given address.
func ServeSSE(srv *Server, addr string) error {
	handler := NewSSEHandler(srv)

	mux := http.NewServeMux()
	mux.Handle("/", handler)

	log.Printf("[sse] wx-mcp MCP server listening on http://%s", addr)
	log.Printf("[sse]   SSE stream : GET  http://%s/sse", addr)
	log.Printf("[sse]   RPC POST   : POST http://%s/message?sessionId=<id>", addr)
	log.Printf("[sse]   Health     : GET  http://%s/health", addr)

	server := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  120 * time.Second,
		WriteTimeout: 0, // SSE streams are long-lived
		IdleTimeout:  120 * time.Second,
	}
	return server.ListenAndServe()
}
