package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jzila/canopy/web"
)

// Server manages the HTTP server that serves the web UI and REST API
type Server struct {
	port        int
	httpServer  *http.Server
	hub         *Hub
	handler     *Handler
	runsHandler *RunsHandler
	repoHandler *RepoHandler
	state       *RuntimeState
	eventBus    *EventBus
	upgrader    websocket.Upgrader
	daemon      *Daemon
}

// NewServer creates a new HTTP server instance
func NewServer(port int, state *RuntimeState, eventBus *EventBus, scheduler SchedulerInterface, beadsClient BeadsClientInterface) *Server {
	return NewServerWithPersistence(port, state, eventBus, scheduler, beadsClient, nil)
}

// NewServerWithPersistence creates a new HTTP server instance with optional persistence store
func NewServerWithPersistence(port int, state *RuntimeState, eventBus *EventBus, scheduler SchedulerInterface, beadsClient BeadsClientInterface, persistenceStore PersistenceStoreInterface) *Server {
	return NewServerWithDaemon(port, state, eventBus, scheduler, beadsClient, persistenceStore, nil)
}

// NewServerWithDaemon creates a new HTTP server instance with daemon reference for repository management
func NewServerWithDaemon(port int, state *RuntimeState, eventBus *EventBus, scheduler SchedulerInterface, beadsClient BeadsClientInterface, persistenceStore PersistenceStoreInterface, daemon *Daemon) *Server {
	// Create WebSocket hub
	hub := NewHub(eventBus)

	// Create HTTP handler
	handler := NewHandler(state, scheduler, beadsClient, eventBus)

	// Wire up daemon reference for handlers that need access to daemon state
	if daemon != nil {
		handler.SetDaemon(daemon)
	}

	// Wire up persistence store for handlers that need to persist state
	if persistenceStore != nil {
		handler.SetPersistenceStore(persistenceStore)
	}

	// Create runs handler for persistence queries (may be nil if persistence disabled)
	var runsHandler *RunsHandler
	if persistenceStore != nil {
		runsHandler = NewRunsHandler(persistenceStore)
	}

	// Create repo handler for repository management (requires daemon reference)
	var repoHandler *RepoHandler
	if daemon != nil {
		// Cast persistence store to RepositoryStoreInterface if available
		var repoStore RepositoryStoreInterface
		if persistenceStore != nil {
			// The persistence.Store implements RepositoryStoreInterface
			if store, ok := persistenceStore.(RepositoryStoreInterface); ok {
				repoStore = store
			}
		}
		repoHandler = NewRepoHandler(daemon, repoStore)
	}

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
		port:        port,
		hub:         hub,
		handler:     handler,
		runsHandler: runsHandler,
		repoHandler: repoHandler,
		state:       state,
		eventBus:    eventBus,
		upgrader:    upgrader,
		daemon:      daemon,
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

	// REST API routes - current run state
	mux.HandleFunc("/api/state", s.handler.HandleGetState)
	mux.HandleFunc("/api/agents", s.handler.HandleGetAgents)
	mux.HandleFunc("/api/agents/", s.handleAgentsRoutes) // Handles /api/agents/:id/kill
	mux.HandleFunc("/api/tasks", s.handleTasksRoutes)    // Handles GET and POST
	mux.HandleFunc("/api/stats", s.handler.HandleGetStats)
	mux.HandleFunc("/api/orch/pause", s.handler.HandlePauseOrch)
	mux.HandleFunc("/api/orch/resume", s.handler.HandleResumeOrch)

	// REST API routes - historical run data (persistence)
	mux.HandleFunc("/api/runs", s.handleRunsRoutes)       // Handles GET /api/runs
	mux.HandleFunc("/api/runs/", s.handleRunsRoutes)      // Handles /api/runs/:id and /api/runs/:id/agents
	mux.HandleFunc("/api/stats/history", s.handleStatsHistory) // Aggregate historical stats

	// REST API routes - repository management
	mux.HandleFunc("/api/repositories", s.handleRepositoriesRoutes)  // Handles GET /api/repositories
	mux.HandleFunc("/api/repositories/", s.handleRepositoriesRoutes) // Handles /api/repositories/:id and /api/repositories/:id/activate

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

	// Handle PATCH for updating agents (e.g., archiving)
	if r.Method == http.MethodPatch {
		s.handler.HandleUpdateAgent(w, r)
		return
	}

	// Otherwise, it's the main agents endpoint (GET)
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

// handleRunsRoutes routes run history requests
func (s *Server) handleRunsRoutes(w http.ResponseWriter, r *http.Request) {
	if s.runsHandler == nil {
		http.Error(w, "Persistence not enabled", http.StatusNotImplemented)
		return
	}

	path := r.URL.Path

	// GET /api/runs - list runs
	if path == "/api/runs" {
		s.runsHandler.HandleListRuns(w, r)
		return
	}

	// Parse run ID from path: /api/runs/:id or /api/runs/:id/agents
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 3 || parts[0] != "api" || parts[1] != "runs" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	runID := parts[2]
	if runID == "" {
		http.Error(w, "Run ID required", http.StatusBadRequest)
		return
	}

	// GET /api/runs/:id/agents
	if len(parts) == 4 && parts[3] == "agents" {
		s.runsHandler.HandleGetRunAgents(w, r, runID)
		return
	}

	// GET /api/runs/:id
	if len(parts) == 3 {
		s.runsHandler.HandleGetRun(w, r, runID)
		return
	}

	http.Error(w, "Not found", http.StatusNotFound)
}

// handleStatsHistory handles aggregate historical statistics
func (s *Server) handleStatsHistory(w http.ResponseWriter, r *http.Request) {
	if s.runsHandler == nil {
		http.Error(w, "Persistence not enabled", http.StatusNotImplemented)
		return
	}
	s.runsHandler.HandleGetHistoricalStats(w, r)
}

// handleRepositoriesRoutes routes repository management requests
func (s *Server) handleRepositoriesRoutes(w http.ResponseWriter, r *http.Request) {
	if s.repoHandler == nil {
		http.Error(w, "Repository management not available", http.StatusNotImplemented)
		return
	}

	path := r.URL.Path

	// GET /api/repositories - list all repositories
	if path == "/api/repositories" {
		s.repoHandler.HandleListRepositories(w, r)
		return
	}

	// Parse repository ID from path: /api/repositories/:id or /api/repositories/:id/activate
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 3 || parts[0] != "api" || parts[1] != "repositories" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	repoID := parts[2]
	if repoID == "" {
		http.Error(w, "Repository ID required", http.StatusBadRequest)
		return
	}

	// POST /api/repositories/:id/activate
	if len(parts) == 4 && parts[3] == "activate" {
		s.repoHandler.HandleActivateRepository(w, r, repoID)
		return
	}

	// GET /api/repositories/:id
	if len(parts) == 3 {
		s.repoHandler.HandleGetRepository(w, r, repoID)
		return
	}

	http.Error(w, "Not found", http.StatusNotFound)
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

	// Send initial state snapshot to the new client
	s.sendInitialState(client)

	// Start client read and write pumps in goroutines
	go client.WritePump()
	go client.ReadPump()
}

// sendInitialState sends the current runtime state to a newly connected client
func (s *Server) sendInitialState(client *Client) {
	// Get a snapshot of the current state
	snapshot := s.state.GetSnapshot()

	// Create state sync event
	event := Event{
		Type:      EventStateSync,
		Timestamp: time.Now(),
		Payload:   snapshot,
	}

	// Marshal and send
	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("Error marshaling initial state: %v", err)
		return
	}

	// Send to client's channel (non-blocking to avoid deadlock)
	select {
	case client.send <- data:
	default:
		log.Printf("Warning: could not send initial state to client (buffer full)")
	}
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
