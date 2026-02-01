package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jzila/canopy/pkg/agent"
	"github.com/jzila/canopy/pkg/beads"
	"github.com/jzila/canopy/pkg/config"
	"github.com/jzila/canopy/pkg/mergecoordinator"
	"github.com/jzila/canopy/pkg/rules"
	"github.com/jzila/canopy/pkg/scheduler"
)

// stubBeadsClient is a minimal beads client for agent settings tests.
type stubBeadsClient struct {
	beads.BeadsClient
}

func (s *stubBeadsClient) Ready(ctx context.Context) ([]beads.Task, error) {
	return nil, nil
}

// createOrchestratorWithAgents creates an orchestrator with real executor,
// scheduler, and merge coordinator so that agent settings propagation can be verified.
func createOrchestratorWithAgents(t *testing.T, cfg *Config) *Orchestrator {
	t.Helper()

	workDir := cfg.WorkDir
	canopyDir := filepath.Join(workDir, ".canopy")
	if err := os.MkdirAll(canopyDir, 0755); err != nil {
		t.Fatalf("failed to create .canopy dir: %v", err)
	}

	agentSettings := cfg.AgentSettings
	if agentSettings == nil {
		defaults := config.DefaultAgentSettings()
		agentSettings = &defaults
	}

	// Determine worker model: CLI flag takes precedence
	workerModel := cfg.Model
	if workerModel == "" {
		workerModel = agentSettings.GetWorkerModel()
	}

	var workerTimeout time.Duration
	if agentSettings.Worker.Timeout != "" {
		if d, err := time.ParseDuration(agentSettings.Worker.Timeout); err == nil {
			workerTimeout = d
		}
	}

	executor := agent.NewExecutor(&agent.Config{
		Model:   workerModel,
		Timeout: workerTimeout,
	})

	bc := &stubBeadsClient{}
	sched := scheduler.NewScheduler(bc, executor, &scheduler.Config{
		Concurrency: 4,
		TempDir:     t.TempDir(),
		WorkDir:     workDir,
	})

	resolverModel := cfg.Model
	if resolverModel == "" {
		resolverModel = agentSettings.GetResolverModel()
	}
	repairModel := cfg.Model
	if repairModel == "" {
		repairModel = agentSettings.GetRepairModel()
	}

	mc, err := mergecoordinator.New(&mergecoordinator.Config{
		WorkDir:       workDir,
		OutputDir:     workDir,
		TempDir:       t.TempDir(),
		Concurrency:   4,
		ResolverModel: resolverModel,
		RepairModel:   repairModel,
	}, bc)
	if err != nil {
		t.Fatalf("failed to create merge coordinator: %v", err)
	}

	engine := rules.NewEngine(&config.RulesSettings{
		PriorityMin: 0,
		PriorityMax: 4,
	})

	return &Orchestrator{
		config:           cfg,
		repoConfig:       config.DefaultConfig(),
		scheduler:        sched,
		mergeCoordinator: mc,
		rulesEngine:      engine,
	}
}

// TestUpdateAgentSettings_WorkerModelPropagation verifies that UpdateAgentSettings
// propagates the worker model to the scheduler's executor.
func TestUpdateAgentSettings_WorkerModelPropagation(t *testing.T) {
	workDir := t.TempDir()
	o := createOrchestratorWithAgents(t, &Config{WorkDir: workDir})

	settings := &config.AgentSettings{
		Worker: config.AgentTypeSettings{
			Model: "claude-opus-4",
		},
	}
	o.UpdateAgentSettings(settings)

	model, _ := o.scheduler.GetExecutor().GetModelAndTimeout()
	if model != "claude-opus-4" {
		t.Errorf("worker model = %q, want %q", model, "claude-opus-4")
	}
}

// TestUpdateAgentSettings_ResolverModelPropagation verifies that UpdateAgentSettings
// propagates the resolver model to the merge coordinator.
func TestUpdateAgentSettings_ResolverModelPropagation(t *testing.T) {
	workDir := t.TempDir()
	o := createOrchestratorWithAgents(t, &Config{WorkDir: workDir})

	settings := &config.AgentSettings{
		Resolver: config.AgentTypeSettings{
			Model: "claude-sonnet-4",
		},
	}
	o.UpdateAgentSettings(settings)

	if got := o.mergeCoordinator.GetResolverModel(); got != "claude-sonnet-4" {
		t.Errorf("resolver model = %q, want %q", got, "claude-sonnet-4")
	}
}

// TestUpdateAgentSettings_RepairModelPropagation verifies that UpdateAgentSettings
// propagates the repair model to the merge coordinator's processor.
func TestUpdateAgentSettings_RepairModelPropagation(t *testing.T) {
	workDir := t.TempDir()
	o := createOrchestratorWithAgents(t, &Config{WorkDir: workDir})

	settings := &config.AgentSettings{
		Repair: config.AgentTypeSettings{
			Model: "claude-haiku-3",
		},
	}
	o.UpdateAgentSettings(settings)

	if got := o.mergeCoordinator.GetRepairModel(); got != "claude-haiku-3" {
		t.Errorf("repair model = %q, want %q", got, "claude-haiku-3")
	}
}

// TestUpdateAgentSettings_WorkerTimeoutPropagation verifies that UpdateAgentSettings
// parses and propagates the worker timeout to the executor.
func TestUpdateAgentSettings_WorkerTimeoutPropagation(t *testing.T) {
	workDir := t.TempDir()
	o := createOrchestratorWithAgents(t, &Config{WorkDir: workDir})

	settings := &config.AgentSettings{
		Worker: config.AgentTypeSettings{
			Timeout: "45m",
		},
	}
	o.UpdateAgentSettings(settings)

	_, timeout := o.scheduler.GetExecutor().GetModelAndTimeout()
	if timeout != 45*time.Minute {
		t.Errorf("worker timeout = %v, want %v", timeout, 45*time.Minute)
	}
}

// TestUpdateAgentSettings_CLIModelOverridePreventsConfigUpdate verifies that
// when a CLI model override (config.Model) is set, UpdateAgentSettings does NOT
// overwrite the worker/resolver/repair models.
func TestUpdateAgentSettings_CLIModelOverridePreventsConfigUpdate(t *testing.T) {
	workDir := t.TempDir()
	o := createOrchestratorWithAgents(t, &Config{
		WorkDir: workDir,
		Model:   "cli-override-model",
	})

	// The executor should have the CLI model (set during construction)
	model, _ := o.scheduler.GetExecutor().GetModelAndTimeout()
	if model != "cli-override-model" {
		t.Fatalf("initial worker model = %q, want %q", model, "cli-override-model")
	}

	// UpdateAgentSettings should NOT overwrite when CLI model is set
	settings := &config.AgentSettings{
		DefaultModel: "should-not-apply",
		Worker: config.AgentTypeSettings{
			Model: "should-not-apply",
		},
		Resolver: config.AgentTypeSettings{
			Model: "should-not-apply",
		},
		Repair: config.AgentTypeSettings{
			Model: "should-not-apply",
		},
	}
	o.UpdateAgentSettings(settings)

	model, _ = o.scheduler.GetExecutor().GetModelAndTimeout()
	if model != "cli-override-model" {
		t.Errorf("worker model after update = %q, want %q (CLI override should be preserved)", model, "cli-override-model")
	}

	if got := o.mergeCoordinator.GetResolverModel(); got != "cli-override-model" {
		t.Errorf("resolver model after update = %q, want %q (CLI override should be preserved)", got, "cli-override-model")
	}

	if got := o.mergeCoordinator.GetRepairModel(); got != "cli-override-model" {
		t.Errorf("repair model after update = %q, want %q (CLI override should be preserved)", got, "cli-override-model")
	}
}

// TestUpdateAgentSettings_CLIModelOverrideAllowsTimeout verifies that even with
// a CLI model override, worker timeout updates are still applied.
func TestUpdateAgentSettings_CLIModelOverrideAllowsTimeout(t *testing.T) {
	workDir := t.TempDir()
	o := createOrchestratorWithAgents(t, &Config{
		WorkDir: workDir,
		Model:   "cli-model",
	})

	settings := &config.AgentSettings{
		Worker: config.AgentTypeSettings{
			Timeout: "30m",
		},
	}
	o.UpdateAgentSettings(settings)

	_, timeout := o.scheduler.GetExecutor().GetModelAndTimeout()
	if timeout != 30*time.Minute {
		t.Errorf("worker timeout = %v, want %v (timeout should apply even with CLI model override)", timeout, 30*time.Minute)
	}
}

// TestUpdateAgentSettings_NilIsNoOp verifies that passing nil settings is a safe no-op.
func TestUpdateAgentSettings_NilIsNoOp(t *testing.T) {
	workDir := t.TempDir()
	o := createOrchestratorWithAgents(t, &Config{WorkDir: workDir})

	// Set a known model first
	o.scheduler.GetExecutor().SetModel("known-model")

	o.UpdateAgentSettings(nil)

	model, _ := o.scheduler.GetExecutor().GetModelAndTimeout()
	if model != "known-model" {
		t.Errorf("model after nil update = %q, want %q", model, "known-model")
	}
}

// TestUpdateAgentSettings_DefaultModelFallback verifies that when a per-agent
// model is empty, the DefaultModel is used instead.
func TestUpdateAgentSettings_DefaultModelFallback(t *testing.T) {
	workDir := t.TempDir()
	o := createOrchestratorWithAgents(t, &Config{WorkDir: workDir})

	settings := &config.AgentSettings{
		DefaultModel: "default-model",
		// Worker.Model is empty, so GetWorkerModel() returns DefaultModel
	}
	o.UpdateAgentSettings(settings)

	model, _ := o.scheduler.GetExecutor().GetModelAndTimeout()
	if model != "default-model" {
		t.Errorf("worker model = %q, want %q (should fall back to DefaultModel)", model, "default-model")
	}
}

// TestUpdateAgentSettings_InvalidTimeoutIgnored verifies that an invalid
// timeout string does not crash or change the current timeout.
func TestUpdateAgentSettings_InvalidTimeoutIgnored(t *testing.T) {
	workDir := t.TempDir()
	o := createOrchestratorWithAgents(t, &Config{WorkDir: workDir})

	// Set a known timeout first
	o.scheduler.GetExecutor().SetTimeout(20 * time.Minute)

	settings := &config.AgentSettings{
		Worker: config.AgentTypeSettings{
			Timeout: "not-a-duration",
		},
	}
	o.UpdateAgentSettings(settings)

	_, timeout := o.scheduler.GetExecutor().GetModelAndTimeout()
	if timeout != 20*time.Minute {
		t.Errorf("timeout after invalid update = %v, want %v (should be unchanged)", timeout, 20*time.Minute)
	}
}

// TestInitialAgentSettingsFromConfig verifies that when creating an orchestrator
// with AgentSettings in the config, the executor and merge coordinator are
// initialized with the correct models and timeout.
func TestInitialAgentSettingsFromConfig(t *testing.T) {
	workDir := t.TempDir()

	settings := &config.AgentSettings{
		DefaultModel: "base-model",
		Worker: config.AgentTypeSettings{
			Model:   "worker-model",
			Timeout: "15m",
		},
		Resolver: config.AgentTypeSettings{
			Model: "resolver-model",
		},
		Repair: config.AgentTypeSettings{
			Model: "repair-model",
		},
	}

	o := createOrchestratorWithAgents(t, &Config{
		WorkDir:       workDir,
		AgentSettings: settings,
	})

	// Verify worker model
	model, timeout := o.scheduler.GetExecutor().GetModelAndTimeout()
	if model != "worker-model" {
		t.Errorf("initial worker model = %q, want %q", model, "worker-model")
	}
	if timeout != 15*time.Minute {
		t.Errorf("initial worker timeout = %v, want %v", timeout, 15*time.Minute)
	}

	// Verify resolver model
	if got := o.mergeCoordinator.GetResolverModel(); got != "resolver-model" {
		t.Errorf("initial resolver model = %q, want %q", got, "resolver-model")
	}

	// Verify repair model
	if got := o.mergeCoordinator.GetRepairModel(); got != "repair-model" {
		t.Errorf("initial repair model = %q, want %q", got, "repair-model")
	}
}

// TestRepoAPI_UpdateAgentConfig_PropagatesSettings verifies that updating agent
// config through the RepoAPI correctly propagates settings to the orchestrator.
func TestRepoAPI_UpdateAgentConfig_PropagatesSettings(t *testing.T) {
	workDir := t.TempDir()
	o := createOrchestratorWithAgents(t, &Config{WorkDir: workDir})
	api := NewRepoAPI(o, workDir)
	ctx := context.Background()

	workerModel := "claude-opus-4"
	resolverModel := "claude-sonnet-4"
	repairModel := "claude-haiku-3"
	timeout := "20m"

	err := api.UpdateAgentConfig(ctx, AgentConfigUpdate{
		Worker:   &AgentTypeSettingsUpdate{Model: &workerModel, Timeout: &timeout},
		Resolver: &AgentTypeSettingsUpdate{Model: &resolverModel},
		Repair:   &AgentTypeSettingsUpdate{Model: &repairModel},
	})
	if err != nil {
		t.Fatalf("UpdateAgentConfig() error = %v", err)
	}

	// Verify via GetAgentConfig
	snapshot, err := api.GetAgentConfig(ctx)
	if err != nil {
		t.Fatalf("GetAgentConfig() error = %v", err)
	}
	if snapshot.Settings.Worker.Model != workerModel {
		t.Errorf("worker model = %q, want %q", snapshot.Settings.Worker.Model, workerModel)
	}
	if snapshot.Settings.Worker.Timeout != timeout {
		t.Errorf("worker timeout = %q, want %q", snapshot.Settings.Worker.Timeout, timeout)
	}
	if snapshot.Settings.Resolver.Model != resolverModel {
		t.Errorf("resolver model = %q, want %q", snapshot.Settings.Resolver.Model, resolverModel)
	}
	if snapshot.Settings.Repair.Model != repairModel {
		t.Errorf("repair model = %q, want %q", snapshot.Settings.Repair.Model, repairModel)
	}
	if snapshot.Persisted {
		t.Error("settings should be marked as not persisted after runtime update")
	}

	// Verify propagation to executor
	gotModel, gotTimeout := o.scheduler.GetExecutor().GetModelAndTimeout()
	if gotModel != workerModel {
		t.Errorf("executor model = %q, want %q", gotModel, workerModel)
	}
	if gotTimeout != 20*time.Minute {
		t.Errorf("executor timeout = %v, want %v", gotTimeout, 20*time.Minute)
	}

	// Verify propagation to merge coordinator
	if got := o.mergeCoordinator.GetResolverModel(); got != resolverModel {
		t.Errorf("merge coordinator resolver model = %q, want %q", got, resolverModel)
	}
	if got := o.mergeCoordinator.GetRepairModel(); got != repairModel {
		t.Errorf("merge coordinator repair model = %q, want %q", got, repairModel)
	}
}

// TestRepoAPI_UpdateAgentConfig_RejectsInvalidTimeout verifies that the API
// rejects updates with invalid timeout values.
func TestRepoAPI_UpdateAgentConfig_RejectsInvalidTimeout(t *testing.T) {
	workDir := t.TempDir()
	o := createOrchestratorWithAgents(t, &Config{WorkDir: workDir})
	api := NewRepoAPI(o, workDir)
	ctx := context.Background()

	badTimeout := "not-a-duration"
	err := api.UpdateAgentConfig(ctx, AgentConfigUpdate{
		Worker: &AgentTypeSettingsUpdate{Timeout: &badTimeout},
	})
	if err == nil {
		t.Error("UpdateAgentConfig() should reject invalid timeout")
	}
}

// TestRepoAPI_PersistAgentConfig_Roundtrip verifies that persisted agent settings
// can be loaded back from config.toml.
func TestRepoAPI_PersistAgentConfig_Roundtrip(t *testing.T) {
	workDir := t.TempDir()
	o := createOrchestratorWithAgents(t, &Config{WorkDir: workDir})
	api := NewRepoAPI(o, workDir)
	ctx := context.Background()

	// Update settings
	workerModel := "persist-worker"
	resolverModel := "persist-resolver"
	repairModel := "persist-repair"
	timeout := "25m"

	err := api.UpdateAgentConfig(ctx, AgentConfigUpdate{
		Worker:   &AgentTypeSettingsUpdate{Model: &workerModel, Timeout: &timeout},
		Resolver: &AgentTypeSettingsUpdate{Model: &resolverModel},
		Repair:   &AgentTypeSettingsUpdate{Model: &repairModel},
	})
	if err != nil {
		t.Fatalf("UpdateAgentConfig() error = %v", err)
	}

	// Persist
	configPath, err := api.PersistAgentConfig(ctx)
	if err != nil {
		t.Fatalf("PersistAgentConfig() error = %v", err)
	}

	// Verify file exists
	if _, statErr := os.Stat(configPath); os.IsNotExist(statErr) {
		t.Fatal("PersistAgentConfig() did not create config file")
	}

	// Reload and verify
	loaded, err := config.LoadConfig(workDir)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if loaded.Agents.Worker.Model != workerModel {
		t.Errorf("persisted worker model = %q, want %q", loaded.Agents.Worker.Model, workerModel)
	}
	if loaded.Agents.Worker.Timeout != timeout {
		t.Errorf("persisted worker timeout = %q, want %q", loaded.Agents.Worker.Timeout, timeout)
	}
	if loaded.Agents.Resolver.Model != resolverModel {
		t.Errorf("persisted resolver model = %q, want %q", loaded.Agents.Resolver.Model, resolverModel)
	}
	if loaded.Agents.Repair.Model != repairModel {
		t.Errorf("persisted repair model = %q, want %q", loaded.Agents.Repair.Model, repairModel)
	}
}

// TestUpdateAgentSettings_EmptyModelClearsPrevious verifies that when config
// removes a model (empty string), the running model is cleared back to empty.
func TestUpdateAgentSettings_EmptyModelClearsPrevious(t *testing.T) {
	workDir := t.TempDir()
	o := createOrchestratorWithAgents(t, &Config{WorkDir: workDir})

	// Set models first
	o.scheduler.GetExecutor().SetModel("old-model")
	o.mergeCoordinator.SetResolverModel("old-resolver")
	o.mergeCoordinator.SetModel("old-repair")

	// Update with empty models — should clear them
	settings := &config.AgentSettings{}
	o.UpdateAgentSettings(settings)

	model, _ := o.scheduler.GetExecutor().GetModelAndTimeout()
	if model != "" {
		t.Errorf("worker model = %q, want empty (should be cleared)", model)
	}
	if got := o.mergeCoordinator.GetResolverModel(); got != "" {
		t.Errorf("resolver model = %q, want empty (should be cleared)", got)
	}
	if got := o.mergeCoordinator.GetRepairModel(); got != "" {
		t.Errorf("repair model = %q, want empty (should be cleared)", got)
	}
}

// TestUpdateAgentSettings_NegativeTimeoutIgnored verifies that a negative
// timeout string is treated as invalid and does not change the current timeout.
func TestUpdateAgentSettings_NegativeTimeoutIgnored(t *testing.T) {
	workDir := t.TempDir()
	o := createOrchestratorWithAgents(t, &Config{WorkDir: workDir})

	// Set a known timeout first
	o.scheduler.GetExecutor().SetTimeout(20 * time.Minute)

	settings := &config.AgentSettings{
		Worker: config.AgentTypeSettings{
			Timeout: "-5m",
		},
	}
	o.UpdateAgentSettings(settings)

	_, timeout := o.scheduler.GetExecutor().GetModelAndTimeout()
	if timeout != 20*time.Minute {
		t.Errorf("timeout after negative update = %v, want %v (should be unchanged)", timeout, 20*time.Minute)
	}
}

// TestUpdateAgentSettings_AllThreeAgentTypes verifies that a single call to
// UpdateAgentSettings correctly updates worker, resolver, and repair simultaneously.
func TestUpdateAgentSettings_AllThreeAgentTypes(t *testing.T) {
	workDir := t.TempDir()
	o := createOrchestratorWithAgents(t, &Config{WorkDir: workDir})

	settings := &config.AgentSettings{
		Worker: config.AgentTypeSettings{
			Model:   "worker-v2",
			Timeout: "10m",
		},
		Resolver: config.AgentTypeSettings{
			Model: "resolver-v2",
		},
		Repair: config.AgentTypeSettings{
			Model: "repair-v2",
		},
	}
	o.UpdateAgentSettings(settings)

	model, timeout := o.scheduler.GetExecutor().GetModelAndTimeout()
	if model != "worker-v2" {
		t.Errorf("worker model = %q, want %q", model, "worker-v2")
	}
	if timeout != 10*time.Minute {
		t.Errorf("worker timeout = %v, want %v", timeout, 10*time.Minute)
	}
	if got := o.mergeCoordinator.GetResolverModel(); got != "resolver-v2" {
		t.Errorf("resolver model = %q, want %q", got, "resolver-v2")
	}
	if got := o.mergeCoordinator.GetRepairModel(); got != "repair-v2" {
		t.Errorf("repair model = %q, want %q", got, "repair-v2")
	}
}
