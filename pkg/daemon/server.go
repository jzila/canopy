package daemon

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/john/canopy/web"
)

// Server manages the HTTP server that serves the web UI and REST API
type Server struct {
	port       int
	httpServer *http.Server
	hub        *Hub
	handler    *Handler
	state      *RuntimeState
	eventBus   *EventBus
	upgrader   websocket.Upgrader
}

// NewServer creates a new HTTP server instance
func NewServer(port int, state *RuntimeState, eventBus *EventBus, scheduler SchedulerInterface, beadsClient BeadsClientInterface) *Server {
	// Create WebSocket hub
	hub := NewHub(eventBus)

	// Create HTTP handler
	handler := NewHandler(state, scheduler, beadsClient)

	// Configure WebSocket upgrader
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		// Allow all origins for development (TODO: configure for production)
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	return &Server{
		port:     port,
		hub:      hub,
		handler:  handler,
		state:    state,
		eventBus: eventBus,
		upgrader: upgrader,
	}
}

// Start starts the HTTP server and WebSocket hub
func (s *Server) Start() error {
	// Start the WebSocket hub in a goroutine
	go s.hub.Run()

	// Setup routes
	mux := s.setupRoutes()

	// Create HTTP server
	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.port),
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Printf("Starting HTTP server on port %d", s.port)

	// Start server (blocking)
	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}

	return nil
}

// Stop gracefully shuts down the server
func (s *Server) Stop() error {
	log.Println("Shutting down HTTP server...")

	// Create shutdown context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Shutdown HTTP server
	if s.httpServer != nil {
		if err := s.httpServer.Shutdown(ctx); err != nil {
			return fmt.Errorf("server shutdown error: %w", err)
		}
	}

	// Shutdown WebSocket hub
	if s.hub != nil {
		s.hub.Shutdown()
	}

	log.Println("HTTP server stopped")
	return nil
}

// setupRoutes configures all HTTP routes
func (s *Server) setupRoutes() *http.ServeMux {
	mux := http.NewServeMux()

	// REST API routes
	mux.HandleFunc("/api/state", s.handler.HandleGetState)
	mux.HandleFunc("/api/agents", s.handler.HandleGetAgents)
	mux.HandleFunc("/api/agents/", s.handleAgentsRoutes) // Handles /api/agents/:id/kill
	mux.HandleFunc("/api/tasks", s.handleTasksRoutes)    // Handles GET and POST
	mux.HandleFunc("/api/stats", s.handler.HandleGetStats)
	mux.HandleFunc("/api/orch/pause", s.handler.HandlePauseOrch)
	mux.HandleFunc("/api/orch/resume", s.handler.HandleResumeOrch)

	// WebSocket endpoint
	mux.HandleFunc("/ws", s.handleWebSocket)

	// Static files (placeholder for embedded React build)
	mux.HandleFunc("/", s.handleStaticFiles)

	return mux
}

// handleAgentsRoutes routes agent-related requests
func (s *Server) handleAgentsRoutes(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Check if this is a kill request: /api/agents/:id/kill
	if strings.HasSuffix(path, "/kill") {
		s.handler.HandleKillAgent(w, r)
		return
	}

	// Otherwise, it's the main agents endpoint
	s.handler.HandleGetAgents(w, r)
}

// handleTasksRoutes routes task-related requests based on method
func (s *Server) handleTasksRoutes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handler.HandleGetTasks(w, r)
	case http.MethodPost:
		s.handler.HandleCreateTask(w, r)
	case http.MethodPatch:
		s.handler.HandleUpdateTask(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleWebSocket upgrades HTTP connections to WebSocket
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Upgrade HTTP connection to WebSocket
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	// Create new client
	client := &Client{
		hub:  s.hub,
		conn: conn,
		send: make(chan []byte, 256),
	}

	// Register client with hub
	s.hub.register <- client

	// Start client read and write pumps in goroutines
	go client.WritePump()
	go client.ReadPump()
}

// handleStaticFiles serves static files for the web UI
func (s *Server) handleStaticFiles(w http.ResponseWriter, r *http.Request) {
	webFS, err := web.GetFS()
	if err != nil {
		http.Error(w, "Dashboard not available: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Serve files using http.FileServer
	fileServer := http.FileServer(http.FS(webFS))

	// For SPA routing: serve index.html for non-existent paths
	path := r.URL.Path
	if path != "/" {
		// Check if file exists
		if _, err := fs.Stat(webFS, strings.TrimPrefix(path, "/")); err != nil {
			// File doesn't exist, serve index.html for SPA routing
			r.URL.Path = "/"
		}
	}

	fileServer.ServeHTTP(w, r)
}
