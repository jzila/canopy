// Package orchestrator provides the RepoAPI interface and orchestration types.
package orchestrator

import (
	"context"
	"fmt"

	"github.com/jzila/canopy/pkg/config"
	"github.com/jzila/canopy/pkg/rules"
	"github.com/jzila/canopy/pkg/sandbox"
	"github.com/jzila/canopy/pkg/validation"
)

// OrchestratorState represents the current state of an orchestrator.
type OrchestratorState string

const (
	// StateIdle means the orchestrator is not actively processing tasks.
	StateIdle OrchestratorState = "idle"
	// StateActive means the orchestrator is actively processing tasks.
	StateActive OrchestratorState = "active"
	// StatePaused means the orchestrator is temporarily paused.
	StatePaused OrchestratorState = "paused"
)

// RunConfig contains configuration for starting an orchestrator run.
type RunConfig struct {
	// Concurrency is the number of parallel agents to run (default: 4)
	Concurrency int `json:"concurrency"`
	// DryRun shows what would execute without running agents
	DryRun bool `json:"dry_run"`
	// Watch enables watch mode (poll for new tasks instead of exiting)
	Watch bool `json:"watch"`
	// MaxRetries is the maximum retry count for failed tasks (-1 = infinite)
	MaxRetries int `json:"max_retries"`
	// MaxPriority filters tasks by priority (only tasks with priority <= this value)
	MaxPriority int `json:"max_priority"`
}

// RuleUpdate contains fields for updating an existing rule.
// Only non-nil fields are applied.
type RuleUpdate struct {
	// Enabled controls whether the rule is active
	Enabled *bool `json:"enabled,omitempty"`
	// Position is the new position in the rules list (0-indexed)
	Position *int `json:"position,omitempty"`
}

// RepoAPI defines the interface for interacting with a repository's orchestrator.
// The orchestrator is always running (as a goroutine) for any repo in daemon scope.
// This interface formalizes the boundary between daemon and orchestrator.
type RepoAPI interface {
	// State returns the current orchestrator state.
	// The orchestrator is always running; this returns whether it's actively
	// processing tasks (ACTIVE), paused (PAUSED), or waiting for work (IDLE).
	GetState(ctx context.Context) (OrchestratorState, error)

	// State transitions

	// Start transitions from idle → active and begins processing tasks.
	// Returns an error if the orchestrator is already active or paused.
	Start(ctx context.Context, config RunConfig) error

	// Pause transitions from active → paused, suspending task processing.
	// In-flight tasks continue to completion but no new tasks are started.
	// Returns an error if the orchestrator is not active.
	Pause(ctx context.Context) error

	// Resume transitions from paused → active, resuming task processing.
	// Returns an error if the orchestrator is not paused.
	Resume(ctx context.Context) error

	// Stop transitions any state → idle, stopping task processing.
	// In-flight tasks continue to completion but no new tasks are started.
	// This is a no-op if already idle.
	Stop(ctx context.Context) error

	// Rules management (works in any state)

	// ListRules returns a snapshot of all rules and settings.
	ListRules(ctx context.Context) (*rules.RulesSnapshot, error)

	// AddRule adds a new rule to the rules engine.
	// Returns an error if a rule with the same name already exists.
	AddRule(ctx context.Context, rule config.CustomRule) error

	// UpdateRule updates an existing rule's properties.
	// Only non-nil fields in the update are applied.
	// Returns an error if the rule is not found.
	UpdateRule(ctx context.Context, name string, update RuleUpdate) error

	// DeleteRule removes a rule by name.
	// Returns an error if the rule is not found.
	DeleteRule(ctx context.Context, name string) error

	// PersistRules saves all non-persisted rules to config.toml.
	// Returns an error if the save fails.
	PersistRules(ctx context.Context) error

	// Config queries (works in any state)

	// GetSandboxConfig returns the sandbox configuration for this repository.
	// Returns nil if no sandbox config is defined.
	GetSandboxConfig(ctx context.Context) (*sandbox.SandboxConfig, error)

	// GetValidationConfig returns the validation configuration for this repository.
	// Returns a default config with validation disabled if not defined.
	GetValidationConfig(ctx context.Context) (*validation.ValidationConfig, error)

	// Runtime adjustment (only meaningful when ACTIVE)

	// SetConcurrency updates the number of parallel agents.
	// Takes effect immediately for the running orchestrator.
	SetConcurrency(ctx context.Context, n int) error
}

// repoAPIImpl implements RepoAPI by wrapping an Orchestrator.
type repoAPIImpl struct {
	orchestrator *Orchestrator
	workDir      string
	state        OrchestratorState
}

// NewRepoAPI creates a RepoAPI wrapper around an existing Orchestrator.
func NewRepoAPI(o *Orchestrator, workDir string) RepoAPI {
	return &repoAPIImpl{
		orchestrator: o,
		workDir:      workDir,
		state:        StateIdle,
	}
}

// GetState returns the current orchestrator state.
func (r *repoAPIImpl) GetState(ctx context.Context) (OrchestratorState, error) {
	return r.state, nil
}

// Start transitions from idle → active.
func (r *repoAPIImpl) Start(ctx context.Context, cfg RunConfig) error {
	if r.state == StateActive {
		return fmt.Errorf("orchestrator is already active")
	}
	if r.state == StatePaused {
		return fmt.Errorf("orchestrator is paused; use Resume() instead")
	}
	// In the full implementation, this would:
	// 1. Apply RunConfig to the orchestrator
	// 2. Start the orchestrator's Run loop in a goroutine
	r.state = StateActive
	return nil
}

// Pause transitions from active → paused.
func (r *repoAPIImpl) Pause(ctx context.Context) error {
	if r.state != StateActive {
		return fmt.Errorf("orchestrator is not active (state: %s)", r.state)
	}
	// In the full implementation, this would signal the orchestrator to
	// stop picking up new tasks while letting in-flight tasks complete.
	r.state = StatePaused
	return nil
}

// Resume transitions from paused → active.
func (r *repoAPIImpl) Resume(ctx context.Context) error {
	if r.state != StatePaused {
		return fmt.Errorf("orchestrator is not paused (state: %s)", r.state)
	}
	// In the full implementation, this would signal the orchestrator to
	// resume picking up tasks.
	r.state = StateActive
	return nil
}

// Stop transitions any state → idle.
func (r *repoAPIImpl) Stop(ctx context.Context) error {
	if r.state == StateIdle {
		return nil // no-op if already idle
	}
	// In the full implementation, this would:
	// 1. Signal the orchestrator to stop
	// 2. Wait for in-flight tasks to complete
	r.state = StateIdle
	return nil
}

// ListRules returns a snapshot of all rules and settings.
func (r *repoAPIImpl) ListRules(ctx context.Context) (*rules.RulesSnapshot, error) {
	engine := r.orchestrator.GetRulesEngine()
	if engine == nil {
		return &rules.RulesSnapshot{}, nil
	}
	snapshot := engine.GetSnapshot()
	return &snapshot, nil
}

// AddRule adds a new rule to the rules engine.
func (r *repoAPIImpl) AddRule(ctx context.Context, rule config.CustomRule) error {
	engine := r.orchestrator.GetRulesEngine()
	if engine == nil {
		return fmt.Errorf("rules engine not initialized")
	}
	return engine.AddRuleWithValidation(rule)
}

// UpdateRule updates an existing rule's properties.
func (r *repoAPIImpl) UpdateRule(ctx context.Context, name string, update RuleUpdate) error {
	engine := r.orchestrator.GetRulesEngine()
	if engine == nil {
		return fmt.Errorf("rules engine not initialized")
	}

	// Apply enabled update
	if update.Enabled != nil {
		if err := engine.UpdateRule(name, *update.Enabled); err != nil {
			return err
		}
	}

	// Apply position update (reorder)
	if update.Position != nil {
		if err := engine.ReorderRule(name, *update.Position); err != nil {
			return err
		}
	}

	return nil
}

// DeleteRule removes a rule by name.
func (r *repoAPIImpl) DeleteRule(ctx context.Context, name string) error {
	engine := r.orchestrator.GetRulesEngine()
	if engine == nil {
		return fmt.Errorf("rules engine not initialized")
	}
	if !engine.RemoveRule(name) {
		return fmt.Errorf("rule %q not found", name)
	}
	return nil
}

// PersistRules saves all non-persisted rules to config.toml.
func (r *repoAPIImpl) PersistRules(ctx context.Context) error {
	engine := r.orchestrator.GetRulesEngine()
	if engine == nil {
		return fmt.Errorf("rules engine not initialized")
	}

	// Mark all rules as persisted
	_, err := engine.PersistAllRules()
	if err != nil {
		return fmt.Errorf("failed to persist rules: %w", err)
	}

	// Get config for persistence and save it
	rulesSettings := engine.GetConfigForPersistence()
	if rulesSettings == nil {
		return nil // Nothing to persist
	}

	// Load current config, update rules section, and save
	repoConfig := r.orchestrator.GetRepoConfig()
	if repoConfig == nil {
		repoConfig = config.DefaultConfig()
	}
	repoConfig.Rules = *rulesSettings

	return config.SaveConfig(r.workDir, repoConfig)
}

// GetSandboxConfig returns the sandbox configuration for this repository.
func (r *repoAPIImpl) GetSandboxConfig(ctx context.Context) (*sandbox.SandboxConfig, error) {
	return sandbox.LoadConfigWithoutValidation(r.workDir)
}

// GetValidationConfig returns the validation configuration for this repository.
func (r *repoAPIImpl) GetValidationConfig(ctx context.Context) (*validation.ValidationConfig, error) {
	return validation.LoadValidationConfig(r.workDir)
}

// SetConcurrency updates the number of parallel agents.
func (r *repoAPIImpl) SetConcurrency(ctx context.Context, n int) error {
	if n < 1 {
		return fmt.Errorf("concurrency must be at least 1, got %d", n)
	}
	r.orchestrator.SetConcurrency(n)
	return nil
}
