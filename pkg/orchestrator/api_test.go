package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jzila/canopy/pkg/config"
	"github.com/jzila/canopy/pkg/rules"
)

// mockOrchestrator creates a minimal orchestrator for testing
func createTestOrchestrator(t *testing.T, workDir string) *Orchestrator {
	t.Helper()

	// Create .canopy directory
	canopyDir := filepath.Join(workDir, ".canopy")
	if err := os.MkdirAll(canopyDir, 0755); err != nil {
		t.Fatalf("failed to create .canopy dir: %v", err)
	}

	// Create a rules engine with some settings
	rulesSettings := &config.RulesSettings{
		PriorityMin: 0,
		PriorityMax: 2,
		Types:       []string{"bug", "task"},
	}
	engine := rules.NewEngine(rulesSettings)

	// Create a minimal orchestrator with just the rules engine
	o := &Orchestrator{
		config:     &Config{WorkDir: workDir},
		repoConfig: config.DefaultConfig(),
		rulesEngine: engine,
	}

	return o
}

func TestRepoAPI_StateTransitions(t *testing.T) {
	workDir := t.TempDir()
	o := createTestOrchestrator(t, workDir)
	api := NewRepoAPI(o, workDir)
	ctx := context.Background()

	// Initial state should be idle
	state, err := api.GetState(ctx)
	if err != nil {
		t.Fatalf("GetState() error = %v", err)
	}
	if state != StateIdle {
		t.Errorf("initial state = %v, want %v", state, StateIdle)
	}

	// Start should transition to active
	if err := api.Start(ctx, RunConfig{Concurrency: 4}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	state, _ = api.GetState(ctx)
	if state != StateActive {
		t.Errorf("state after Start = %v, want %v", state, StateActive)
	}

	// Start when active should fail
	if err := api.Start(ctx, RunConfig{}); err == nil {
		t.Error("Start() when active should return error")
	}

	// Pause should transition to paused
	if err := api.Pause(ctx); err != nil {
		t.Fatalf("Pause() error = %v", err)
	}
	state, _ = api.GetState(ctx)
	if state != StatePaused {
		t.Errorf("state after Pause = %v, want %v", state, StatePaused)
	}

	// Resume should transition back to active
	if err := api.Resume(ctx); err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	state, _ = api.GetState(ctx)
	if state != StateActive {
		t.Errorf("state after Resume = %v, want %v", state, StateActive)
	}

	// Stop should transition to idle
	if err := api.Stop(ctx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	state, _ = api.GetState(ctx)
	if state != StateIdle {
		t.Errorf("state after Stop = %v, want %v", state, StateIdle)
	}

	// Stop when idle should be no-op
	if err := api.Stop(ctx); err != nil {
		t.Errorf("Stop() when idle should not return error, got %v", err)
	}
}

func TestRepoAPI_StateTransitionErrors(t *testing.T) {
	workDir := t.TempDir()
	o := createTestOrchestrator(t, workDir)
	api := NewRepoAPI(o, workDir)
	ctx := context.Background()

	// Pause when idle should fail
	if err := api.Pause(ctx); err == nil {
		t.Error("Pause() when idle should return error")
	}

	// Resume when idle should fail
	if err := api.Resume(ctx); err == nil {
		t.Error("Resume() when idle should return error")
	}

	// Transition to paused
	_ = api.Start(ctx, RunConfig{})
	_ = api.Pause(ctx)

	// Start when paused should fail
	if err := api.Start(ctx, RunConfig{}); err == nil {
		t.Error("Start() when paused should return error")
	}

	// Pause when already paused should fail
	if err := api.Pause(ctx); err == nil {
		t.Error("Pause() when paused should return error")
	}
}

func TestRepoAPI_ListRules(t *testing.T) {
	workDir := t.TempDir()
	o := createTestOrchestrator(t, workDir)
	api := NewRepoAPI(o, workDir)
	ctx := context.Background()

	snapshot, err := api.ListRules(ctx)
	if err != nil {
		t.Fatalf("ListRules() error = %v", err)
	}
	if snapshot == nil {
		t.Fatal("ListRules() returned nil snapshot")
	}
	if snapshot.Settings == nil {
		t.Fatal("ListRules() returned nil settings")
	}
	if snapshot.Settings.PriorityMax != 2 {
		t.Errorf("PriorityMax = %d, want 2", snapshot.Settings.PriorityMax)
	}
	if len(snapshot.Settings.Types) != 2 {
		t.Errorf("Types = %v, want [bug task]", snapshot.Settings.Types)
	}
}

func TestRepoAPI_AddRule(t *testing.T) {
	workDir := t.TempDir()
	o := createTestOrchestrator(t, workDir)
	api := NewRepoAPI(o, workDir)
	ctx := context.Background()

	rule := config.CustomRule{
		Name:      "test-rule",
		Condition: "priority > 2",
		Action:    "deny",
		Reason:    "test reason",
	}

	// Add the rule
	if err := api.AddRule(ctx, rule); err != nil {
		t.Fatalf("AddRule() error = %v", err)
	}

	// Verify rule was added
	snapshot, _ := api.ListRules(ctx)
	found := false
	for _, r := range snapshot.Rules {
		if r.Name == "test-rule" {
			found = true
			if r.Condition != "priority > 2" {
				t.Errorf("rule condition = %s, want %s", r.Condition, "priority > 2")
			}
			if r.Action != "deny" {
				t.Errorf("rule action = %s, want %s", r.Action, "deny")
			}
		}
	}
	if !found {
		t.Error("AddRule() did not add the rule")
	}

	// Adding duplicate should fail
	if err := api.AddRule(ctx, rule); err == nil {
		t.Error("AddRule() with duplicate name should return error")
	}
}

func TestRepoAPI_AddRuleValidation(t *testing.T) {
	workDir := t.TempDir()
	o := createTestOrchestrator(t, workDir)
	api := NewRepoAPI(o, workDir)
	ctx := context.Background()

	tests := []struct {
		name    string
		rule    config.CustomRule
		wantErr bool
	}{
		{
			name: "valid rule",
			rule: config.CustomRule{
				Name:      "valid-rule",
				Condition: "priority > 0",
				Action:    "deny",
			},
			wantErr: false,
		},
		{
			name: "missing name",
			rule: config.CustomRule{
				Condition: "priority > 0",
				Action:    "deny",
			},
			wantErr: true,
		},
		{
			name: "missing condition",
			rule: config.CustomRule{
				Name:   "no-condition",
				Action: "deny",
			},
			wantErr: true,
		},
		{
			name: "missing action",
			rule: config.CustomRule{
				Name:      "no-action",
				Condition: "priority > 0",
			},
			wantErr: true,
		},
		{
			name: "invalid action",
			rule: config.CustomRule{
				Name:      "bad-action",
				Condition: "priority > 0",
				Action:    "invalid",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := api.AddRule(ctx, tt.rule)
			if (err != nil) != tt.wantErr {
				t.Errorf("AddRule() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestRepoAPI_UpdateRule(t *testing.T) {
	workDir := t.TempDir()
	o := createTestOrchestrator(t, workDir)
	api := NewRepoAPI(o, workDir)
	ctx := context.Background()

	// Add a rule first
	rule := config.CustomRule{
		Name:      "update-test",
		Condition: "type == bug",
		Action:    "deny",
	}
	_ = api.AddRule(ctx, rule)

	// Update enabled state
	enabled := false
	if err := api.UpdateRule(ctx, "update-test", RuleUpdate{Enabled: &enabled}); err != nil {
		t.Fatalf("UpdateRule() error = %v", err)
	}

	// Verify update
	snapshot, _ := api.ListRules(ctx)
	for _, r := range snapshot.Rules {
		if r.Name == "update-test" {
			if r.Enabled == nil || *r.Enabled != false {
				t.Error("UpdateRule() did not update enabled state")
			}
		}
	}

	// Update non-existent rule should fail
	if err := api.UpdateRule(ctx, "nonexistent", RuleUpdate{Enabled: &enabled}); err == nil {
		t.Error("UpdateRule() with nonexistent rule should return error")
	}
}

func TestRepoAPI_DeleteRule(t *testing.T) {
	workDir := t.TempDir()
	o := createTestOrchestrator(t, workDir)
	api := NewRepoAPI(o, workDir)
	ctx := context.Background()

	// Add a rule first
	rule := config.CustomRule{
		Name:      "delete-test",
		Condition: "type == chore",
		Action:    "deny",
	}
	_ = api.AddRule(ctx, rule)

	// Delete the rule
	if err := api.DeleteRule(ctx, "delete-test"); err != nil {
		t.Fatalf("DeleteRule() error = %v", err)
	}

	// Verify deletion
	snapshot, _ := api.ListRules(ctx)
	for _, r := range snapshot.Rules {
		if r.Name == "delete-test" {
			t.Error("DeleteRule() did not remove the rule")
		}
	}

	// Delete non-existent rule should fail
	if err := api.DeleteRule(ctx, "nonexistent"); err == nil {
		t.Error("DeleteRule() with nonexistent rule should return error")
	}
}

func TestRepoAPI_SetConcurrency(t *testing.T) {
	workDir := t.TempDir()
	o := createTestOrchestrator(t, workDir)
	api := NewRepoAPI(o, workDir)
	ctx := context.Background()

	// Valid concurrency
	if err := api.SetConcurrency(ctx, 8); err != nil {
		t.Fatalf("SetConcurrency(8) error = %v", err)
	}

	// Invalid concurrency (< 1)
	if err := api.SetConcurrency(ctx, 0); err == nil {
		t.Error("SetConcurrency(0) should return error")
	}
	if err := api.SetConcurrency(ctx, -1); err == nil {
		t.Error("SetConcurrency(-1) should return error")
	}
}

func TestRepoAPI_GetConfigs(t *testing.T) {
	workDir := t.TempDir()
	o := createTestOrchestrator(t, workDir)
	api := NewRepoAPI(o, workDir)
	ctx := context.Background()

	// GetSandboxConfig should return nil if no config file exists
	sandboxCfg, err := api.GetSandboxConfig(ctx)
	if err != nil {
		t.Fatalf("GetSandboxConfig() error = %v", err)
	}
	if sandboxCfg != nil {
		t.Error("GetSandboxConfig() should return nil when no config file exists")
	}

	// GetValidationConfig should return default config if no file exists
	validationCfg, err := api.GetValidationConfig(ctx)
	if err != nil {
		t.Fatalf("GetValidationConfig() error = %v", err)
	}
	if validationCfg == nil {
		t.Fatal("GetValidationConfig() should return default config, got nil")
	}
	if validationCfg.Validation.Enabled {
		t.Error("GetValidationConfig() default should have Enabled=false")
	}
}

func TestRepoAPI_PersistRules(t *testing.T) {
	workDir := t.TempDir()
	o := createTestOrchestrator(t, workDir)
	api := NewRepoAPI(o, workDir)
	ctx := context.Background()

	// Add a runtime rule
	rule := config.CustomRule{
		Name:      "persist-test",
		Condition: "type == feature",
		Action:    "allow",
	}
	_ = api.AddRule(ctx, rule)

	// Persist rules
	_, _, err := api.PersistRules(ctx)
	if err != nil {
		t.Fatalf("PersistRules() error = %v", err)
	}

	// Verify config.toml was created with the rule
	configPath := filepath.Join(workDir, ".canopy", "config.toml")
	if _, statErr := os.Stat(configPath); os.IsNotExist(statErr) {
		t.Error("PersistRules() did not create config.toml")
	}

	// Load the config and verify the rule is present
	loadedConfig, err := config.LoadConfig(workDir)
	if err != nil {
		t.Fatalf("failed to load persisted config: %v", err)
	}
	found := false
	for _, r := range loadedConfig.Rules.Custom {
		if r.Name == "persist-test" {
			found = true
		}
	}
	if !found {
		t.Error("PersistRules() did not persist the rule to config.toml")
	}
}

func TestOrchestratorState_String(t *testing.T) {
	// Test that OrchestratorState values are valid strings
	states := []OrchestratorState{StateIdle, StateActive, StatePaused}
	expected := []string{"idle", "active", "paused"}

	for i, state := range states {
		if string(state) != expected[i] {
			t.Errorf("OrchestratorState = %q, want %q", state, expected[i])
		}
	}
}
