package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jzila/canopy/pkg/logging"
	"github.com/jzila/canopy/web"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Server manages the HTTP server that serves the web UI and REST API
type Server struct {
	port          int
	httpServer    *http.Server
	hub           *Hub
	handler       *Handler
	runsHandler   *RunsHandler
	repoHandler   *RepoHandler
	beadsHandler  *BeadsHandler
	orchHandler   *OrchestrationHandler
	rulesHandler  *RulesHandler
	configHandler *ConfigHandler
	state         *RuntimeState
	eventBus      *EventBus
	upgrader      websocket.Upgrader
	daemon        *Daemon
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

	// Create beads handler for beads sync operations (requires daemon reference)
	var beadsHandler *BeadsHandler
	if daemon != nil {
		beadsHandler = NewBeadsHandler(daemon)
	}

	// Create orchestration handler for run control (requires daemon reference)
	var orchHandler *OrchestrationHandler
	if daemon != nil {
		orchHandler = NewOrchestrationHandler(daemon.GetOrchestratorManager())
	}

	// Create rules handler for runtime rule management (requires daemon reference)
	var rulesHandler *RulesHandler
	if daemon != nil {
		rulesHandler = NewRulesHandler(daemon)
	}

	// Create config handler for configuration queries (requires daemon reference)
	var configHandler *ConfigHandler
	if daemon != nil {
		configHandler = NewConfigHandler(daemon)
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
		port:          port,
		hub:           hub,
		handler:       handler,
		runsHandler:   runsHandler,
		repoHandler:   repoHandler,
		beadsHandler:  beadsHandler,
		orchHandler:   orchHandler,
		rulesHandler:  rulesHandler,
		configHandler: configHandler,
		state:         state,
		eventBus:      eventBus,
		upgrader:      upgrader,
		daemon:        daemon,
	}
}

// Start starts the HTTP server and WebSocket hub
func (s *Server) Start() error {
	// Start the WebSocket hub in a goroutine
	go s.hub.Run()

	// Setup routes
	mux := s.setupRoutes()

	// Apply middleware chain: RequestID -> Logging -> Routes
	handler := RequestIDMiddleware(LoggingMiddleware(mux))

	// Create HTTP server
	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.port),
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	logging.Info("starting HTTP server", "port", s.port)

	// Start server (blocking)
	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}

	return nil
}

// Stop gracefully shuts down the server
func (s *Server) Stop() error {
	logging.Debug("shutting down HTTP server")

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

	logging.Debug("HTTP server stopped")
	return nil
}

// setupRoutes configures all HTTP routes
func (s *Server) setupRoutes() *http.ServeMux {
	mux := http.NewServeMux()

	// REST API routes - current run state
	mux.HandleFunc("/api/state", s.handler.HandleGetState)
	mux.HandleFunc("/api/agents", s.handleAgentsRoutes)  // Handles GET and PATCH
	mux.HandleFunc("/api/agents/", s.handleAgentsRoutes) // Handles /api/agents/:id/kill
	mux.HandleFunc("/api/tasks", s.handleTasksRoutes)    // Handles GET and POST
	mux.HandleFunc("/api/stats", s.handler.HandleGetStats)
	mux.HandleFunc("/api/orch/pause", s.handler.HandlePauseOrch)
	mux.HandleFunc("/api/orch/resume", s.handler.HandleResumeOrch)
	mux.HandleFunc("/api/merge-queue", s.handler.HandleGetMergeQueue)

	// REST API routes - historical run data (persistence)
	// New: /api/daemon/runs (cross-repo run listing)
	mux.HandleFunc("/api/daemon/runs", s.handleRunsRoutes)  // Handles GET /api/daemon/runs
	mux.HandleFunc("/api/daemon/runs/", s.handleRunsRoutes) // Handles /api/daemon/runs/:id and /api/daemon/runs/:id/agents
	// Legacy routes (redirect to new paths)
	mux.HandleFunc("/api/runs", s.handleLegacyRunsRedirect)  // Redirects to /api/daemon/runs
	mux.HandleFunc("/api/runs/", s.handleLegacyRunsRedirect) // Redirects to /api/daemon/runs/:id
	mux.HandleFunc("/api/stats/history", s.handleStatsHistory) // Aggregate historical stats

	// REST API routes - repository management
	// New: /api/daemon/repositories (daemon-owned)
	mux.HandleFunc("/api/daemon/repositories", s.handleRepositoriesRoutes)  // Handles GET /api/daemon/repositories
	mux.HandleFunc("/api/daemon/repositories/", s.handleRepositoriesRoutes) // Handles /api/daemon/repositories/:id and activate
	// Legacy routes (redirect to new paths)
	mux.HandleFunc("/api/repositories", s.handleLegacyRepositoriesRedirect)  // Redirects to /api/daemon/repositories
	mux.HandleFunc("/api/repositories/", s.handleLegacyRepositoriesRedirect) // Redirects to /api/daemon/repositories/:id

	// REST API routes - beads operations
	mux.HandleFunc("/api/beads/sync", s.handleBeadsSyncRoute) // Handles POST /api/beads/sync

	// REST API routes - orchestration control (daemon-owned runs)
	// New: /api/runs/:run_id/... for run-scoped operations
	mux.HandleFunc("/api/orchestrator/", s.handleOrchestratorRoutes) // Legacy: handles all /api/orchestrator/* routes

	// REST API routes - repo-scoped rules management
	// New: /api/repos/:repo_id/rules
	mux.HandleFunc("/api/repos/", s.handleReposRoutes) // Handles /api/repos/:repo_id/rules and /api/repos/:repo_id/config/*
	// Legacy rules routes (redirect to new paths)
	mux.HandleFunc("/api/rules", s.handleLegacyRulesRedirect)  // Redirects to /api/repos/:repo_id/rules
	mux.HandleFunc("/api/rules/", s.handleLegacyRulesRedirect) // Redirects to /api/repos/:repo_id/rules/:name
	// Legacy config routes (redirect to new paths)
	mux.HandleFunc("/api/config/", s.handleLegacyConfigRedirect) // Redirects to /api/repos/:repo_id/config/*

	// Prometheus metrics endpoint
	mux.Handle("/metrics", promhttp.Handler())

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

	// Check if this is a resumable agents request: /api/agents/resumable
	if strings.HasSuffix(path, "/resumable") {
		s.handleGetResumableAgents(w, r)
		return
	}

	// Handle method-based routing for /api/agents
	switch r.Method {
	case http.MethodGet:
		s.handler.HandleGetAgents(w, r)
	case http.MethodPatch:
		s.handler.HandleUpdateAgent(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// ResumableAgentInfo contains the information needed to resume an agent
type ResumableAgentInfo struct {
	AgentID       string `json:"agent_id"`
	TaskID        string `json:"task_id"`
	TaskTitle     string `json:"task_title,omitempty"`
	RunID         string `json:"run_id"`
	SessionID     string `json:"session_id"`
	UpperDir      string `json:"upper_dir"`
	LowerDir      string `json:"lower_dir"`
	WorkDir       string `json:"work_dir"`
	MergedDir     string `json:"merged_dir"`
	InterruptedAt int64  `json:"interrupted_at"`
}

// handleGetResumableAgents returns agents that can be resumed after daemon restart
func (s *Server) handleGetResumableAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.daemon == nil {
		http.Error(w, "Daemon not available", http.StatusServiceUnavailable)
		return
	}

	// Get resumable overlays from daemon
	overlays, err := s.daemon.GetResumableOverlays(r.Context())
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get resumable agents: %v", err), http.StatusInternalServerError)
		return
	}

	// Convert to wire format
	agents := make([]ResumableAgentInfo, 0, len(overlays))
	for _, o := range overlays {
		info := ResumableAgentInfo{
			AgentID:       o.Agent.ID,
			TaskID:        o.Agent.TaskID,
			TaskTitle:     o.Agent.TaskTitle,
			RunID:         o.Agent.RunID,
			SessionID:     o.Overlay.SessionID,
			UpperDir:      o.Overlay.UpperDir,
			LowerDir:      o.Overlay.LowerDir,
			WorkDir:       o.Overlay.WorkDir,
			MergedDir:     o.Overlay.MergedDir,
			InterruptedAt: o.InterruptedAt.Unix(),
		}
		agents = append(agents, info)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(agents); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode resumable agents: %v", err), http.StatusInternalServerError)
		return
	}
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
// Handles both new /api/daemon/runs and normalized paths from legacy redirects
func (s *Server) handleRunsRoutes(w http.ResponseWriter, r *http.Request) {
	if s.runsHandler == nil {
		http.Error(w, "Persistence not enabled", http.StatusNotImplemented)
		return
	}

	path := r.URL.Path

	// GET /api/daemon/runs - list runs
	if path == "/api/daemon/runs" {
		s.runsHandler.HandleListRuns(w, r)
		return
	}

	// Parse run ID from path: /api/daemon/runs/:id or /api/daemon/runs/:id/agents
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 4 || parts[0] != "api" || parts[1] != "daemon" || parts[2] != "runs" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	runID := parts[3]
	if runID == "" {
		http.Error(w, "Run ID required", http.StatusBadRequest)
		return
	}

	// GET /api/daemon/runs/:id/agents
	if len(parts) == 5 && parts[4] == "agents" {
		s.runsHandler.HandleGetRunAgents(w, r, runID)
		return
	}

	// GET /api/daemon/runs/:id
	if len(parts) == 4 {
		s.runsHandler.HandleGetRun(w, r, runID)
		return
	}

	http.Error(w, "Not found", http.StatusNotFound)
}

// handleLegacyRunsRedirect redirects legacy /api/runs/* to /api/daemon/runs/*
func (s *Server) handleLegacyRunsRedirect(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	newPath := strings.Replace(path, "/api/runs", "/api/daemon/runs", 1)
	if r.URL.RawQuery != "" {
		newPath += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, newPath, http.StatusTemporaryRedirect)
}

// handleStatsHistory handles aggregate historical statistics
func (s *Server) handleStatsHistory(w http.ResponseWriter, r *http.Request) {
	if s.runsHandler == nil {
		http.Error(w, "Persistence not enabled", http.StatusNotImplemented)
		return
	}
	s.runsHandler.HandleGetHistoricalStats(w, r)
}

// handleBeadsSyncRoute routes beads sync requests
func (s *Server) handleBeadsSyncRoute(w http.ResponseWriter, r *http.Request) {
	if s.beadsHandler == nil {
		http.Error(w, "Beads operations not available", http.StatusServiceUnavailable)
		return
	}
	s.beadsHandler.HandleSyncBeads(w, r)
}

// handleOrchestratorRoutes routes orchestration control requests
func (s *Server) handleOrchestratorRoutes(w http.ResponseWriter, r *http.Request) {
	if s.orchHandler == nil {
		http.Error(w, "Orchestration not available", http.StatusServiceUnavailable)
		return
	}
	s.orchHandler.RouteOrchestrator(w, r)
}

// handleRulesRoutes routes rules management requests
func (s *Server) handleRulesRoutes(w http.ResponseWriter, r *http.Request) {
	if s.rulesHandler == nil {
		http.Error(w, "Rules management not available", http.StatusServiceUnavailable)
		return
	}
	s.rulesHandler.RouteRules(w, r)
}

// handleConfigRoutes routes configuration query requests
func (s *Server) handleConfigRoutes(w http.ResponseWriter, r *http.Request) {
	if s.configHandler == nil {
		http.Error(w, "Configuration queries not available", http.StatusServiceUnavailable)
		return
	}
	s.configHandler.RouteConfig(w, r)
}

// handleReposRoutes routes repo-scoped requests for rules and config
// Handles /api/repos/:repo_id/rules/* and /api/repos/:repo_id/config/*
func (s *Server) handleReposRoutes(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Parse: /api/repos/:repo_id/...
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 4 || parts[0] != "api" || parts[1] != "repos" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	repoID := parts[2]
	if repoID == "" {
		http.Error(w, "Repository ID required", http.StatusBadRequest)
		return
	}

	// Inject repo_id as query parameter for downstream handlers
	q := r.URL.Query()
	q.Set("repo_id", repoID)
	r.URL.RawQuery = q.Encode()

	resource := parts[3]
	switch resource {
	case "rules":
		if s.rulesHandler == nil {
			http.Error(w, "Rules management not available", http.StatusServiceUnavailable)
			return
		}
		s.rulesHandler.RouteRepoRules(w, r, parts[4:])
	case "config":
		if s.configHandler == nil {
			http.Error(w, "Configuration queries not available", http.StatusServiceUnavailable)
			return
		}
		s.configHandler.RouteRepoConfig(w, r, parts[4:])
	default:
		http.Error(w, "Not found", http.StatusNotFound)
	}
}

// handleLegacyRulesRedirect redirects legacy /api/rules?repo_path=X to /api/repos/:repo_id/rules
func (s *Server) handleLegacyRulesRedirect(w http.ResponseWriter, r *http.Request) {
	repoPath := r.URL.Query().Get("repo_path")
	if repoPath == "" {
		http.Error(w, "repo_path query parameter required", http.StatusBadRequest)
		return
	}

	// Use repo_path as repo_id (URL-encoded for path safety)
	repoID := repoPath // The repo path becomes the repo ID
	path := r.URL.Path

	// Transform: /api/rules -> /api/repos/:repo_id/rules
	// Transform: /api/rules/:name -> /api/repos/:repo_id/rules/:name
	// Transform: /api/rules/:name/persist -> /api/repos/:repo_id/rules/:name/persist
	var newPath string
	if path == "/api/rules" {
		newPath = "/api/repos/" + repoID + "/rules"
	} else {
		// Extract the suffix after /api/rules/
		suffix := strings.TrimPrefix(path, "/api/rules")
		newPath = "/api/repos/" + repoID + "/rules" + suffix
	}

	// Remove repo_path from query params, keep others
	q := r.URL.Query()
	q.Del("repo_path")
	if len(q) > 0 {
		newPath += "?" + q.Encode()
	}

	http.Redirect(w, r, newPath, http.StatusTemporaryRedirect)
}

// handleLegacyConfigRedirect redirects legacy /api/config/*?repo_path=X to /api/repos/:repo_id/config/*
func (s *Server) handleLegacyConfigRedirect(w http.ResponseWriter, r *http.Request) {
	repoPath := r.URL.Query().Get("repo_path")
	if repoPath == "" {
		http.Error(w, "repo_path query parameter required", http.StatusBadRequest)
		return
	}

	// Use repo_path as repo_id
	repoID := repoPath
	path := r.URL.Path

	// Transform: /api/config/sandbox -> /api/repos/:repo_id/config/sandbox
	suffix := strings.TrimPrefix(path, "/api/config")
	newPath := "/api/repos/" + repoID + "/config" + suffix

	// Remove repo_path from query params, keep others
	q := r.URL.Query()
	q.Del("repo_path")
	if len(q) > 0 {
		newPath += "?" + q.Encode()
	}

	http.Redirect(w, r, newPath, http.StatusTemporaryRedirect)
}

// handleRepositoriesRoutes routes repository management requests
// Handles /api/daemon/repositories and /api/daemon/repositories/:id/*
func (s *Server) handleRepositoriesRoutes(w http.ResponseWriter, r *http.Request) {
	if s.repoHandler == nil {
		http.Error(w, "Repository management not available", http.StatusNotImplemented)
		return
	}

	path := r.URL.Path

	// GET /api/daemon/repositories - list all repositories
	if path == "/api/daemon/repositories" {
		s.repoHandler.HandleListRepositories(w, r)
		return
	}

	// Parse repository ID from path: /api/daemon/repositories/:id or /api/daemon/repositories/:id/activate
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 4 || parts[0] != "api" || parts[1] != "daemon" || parts[2] != "repositories" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	repoID := parts[3]
	if repoID == "" {
		http.Error(w, "Repository ID required", http.StatusBadRequest)
		return
	}

	// POST /api/daemon/repositories/:id/activate
	if len(parts) == 5 && parts[4] == "activate" {
		s.repoHandler.HandleActivateRepository(w, r, repoID)
		return
	}

	// GET /api/daemon/repositories/:id
	if len(parts) == 4 {
		s.repoHandler.HandleGetRepository(w, r, repoID)
		return
	}

	http.Error(w, "Not found", http.StatusNotFound)
}

// handleLegacyRepositoriesRedirect redirects legacy /api/repositories/* to /api/daemon/repositories/*
func (s *Server) handleLegacyRepositoriesRedirect(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	newPath := strings.Replace(path, "/api/repositories", "/api/daemon/repositories", 1)
	if r.URL.RawQuery != "" {
		newPath += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, newPath, http.StatusTemporaryRedirect)
}

// handleWebSocket upgrades HTTP connections to WebSocket
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Upgrade HTTP connection to WebSocket
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		logging.Warn("WebSocket upgrade error", "error", err)
		return
	}

	// Create new client
	client := &Client{
		hub:  s.hub,
		conn: conn,
		send: make(chan []byte, defaultClientSendBuffer),
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
		logging.Error("error marshaling initial state", "error", err)
		return
	}

	// Send to client's channel (non-blocking to avoid deadlock)
	select {
	case client.send <- data:
	default:
		logging.Warn("could not send initial state to client (buffer full)")
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
