// Package rules provides a rule evaluation engine for task selection.
package rules

import (
	"fmt"
	"sync"

	"github.com/jzila/canopy/pkg/beads"
	"github.com/jzila/canopy/pkg/config"
)

// internalRule wraps a CustomRule with its source for internal tracking.
type internalRule struct {
	config.CustomRule
	Source config.RuleSource
}

// Engine evaluates task selection rules against candidate tasks.
// Rules are stored in a single unified slice, evaluated in order.
// The three-tier precedence hierarchy is:
//  1. Override rules (run-configured, highest priority)
//  2. Config rules (from .canopy/config.toml)
//  3. Default rules (built-in, lowest priority)
//
// The configSnapshot tracks the original config state for persistence tracking.
type Engine struct {
	settings       *config.RulesSettings // Filter settings (priority, types, labels, etc.)
	rules          []internalRule        // Unified rules slice with source tracking
	configSnapshot []config.CustomRule   // Snapshot of rules loaded from config (for persistence tracking)
	defaultRules   []config.CustomRule   // Default rules (formalized baseline)
	mu             sync.RWMutex
}

// NewEngine creates a new rule evaluation engine.
// Rules from cfg.Custom are loaded into the unified rules slice.
func NewEngine(cfg *config.RulesSettings) *Engine {
	e := &Engine{
		settings:     cfg,
		rules:        nil,
		defaultRules: nil,
	}

	// Load config rules into unified slice and snapshot with source tracking
	if cfg != nil && len(cfg.Custom) > 0 {
		e.rules = make([]internalRule, len(cfg.Custom))
		for i, r := range cfg.Custom {
			e.rules[i] = internalRule{
				CustomRule: r,
				Source:     config.RuleSourceConfig,
			}
		}
		e.configSnapshot = make([]config.CustomRule, len(cfg.Custom))
		copy(e.configSnapshot, cfg.Custom)
	}

	return e
}

// NewEngineWithDefaults creates a new rule evaluation engine with explicit default rules.
// This allows establishing a baseline ruleset that is always present.
// Precedence: override rules > config rules > default rules.
func NewEngineWithDefaults(cfg *config.RulesSettings, defaults []config.CustomRule) *Engine {
	e := NewEngine(cfg)

	// Store default rules
	if len(defaults) > 0 {
		e.defaultRules = make([]config.CustomRule, len(defaults))
		copy(e.defaultRules, defaults)

		// Prepend defaults to rules slice (lowest priority, evaluated first)
		// Note: rules are evaluated in order, so defaults come first
		defaultInternals := make([]internalRule, len(defaults))
		for i, r := range defaults {
			defaultInternals[i] = internalRule{
				CustomRule: r,
				Source:     config.RuleSourceDefault,
			}
		}
		e.rules = append(defaultInternals, e.rules...)
	}

	return e
}

// EvalResult contains the result of evaluating a task against rules.
type EvalResult struct {
	// Allow indicates the task passed all rules and should be considered
	Allow bool
	// Skip indicates the task should be skipped
	Skip bool
	// SkipReason explains why the task was skipped
	SkipReason string
	// BoostAmount is the cumulative priority boost from matching rules
	BoostAmount int
	// LimitKey is the key for concurrency limiting (rule name)
	LimitKey string
	// LimitMax is the maximum concurrent tasks for LimitKey
	LimitMax int
}

// AddRule adds a rule to the end of the unified rules slice.
// Rules are evaluated in order: DENY stops and rejects, ALLOW continues.
// New rules added at runtime are marked as override source.
func (e *Engine) AddRule(rule config.CustomRule) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rules = append(e.rules, internalRule{
		CustomRule: rule,
		Source:     config.RuleSourceOverride,
	})
}

// AddRuleWithSource adds a rule with an explicit source.
func (e *Engine) AddRuleWithSource(rule config.CustomRule, source config.RuleSource) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rules = append(e.rules, internalRule{
		CustomRule: rule,
		Source:     source,
	})
}

// RemoveRule removes a rule by name from the unified rules slice.
// Returns true if a rule was removed.
func (e *Engine) RemoveRule(name string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i, r := range e.rules {
		if r.CustomRule.Name == name {
			e.rules = append(e.rules[:i], e.rules[i+1:]...)
			return true
		}
	}
	return false
}

// ClearRuntimeRules removes all override rules, keeping only default and config rules.
// This is called when a run ends to discard run-specific rule overrides.
func (e *Engine) ClearRuntimeRules() {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Rebuild rules slice with only default and config rules
	var newRules []internalRule

	// Add back default rules first
	for _, r := range e.defaultRules {
		newRules = append(newRules, internalRule{
			CustomRule: r,
			Source:     config.RuleSourceDefault,
		})
	}

	// Add back config rules from snapshot
	for _, r := range e.configSnapshot {
		newRules = append(newRules, internalRule{
			CustomRule: r,
			Source:     config.RuleSourceConfig,
		})
	}

	e.rules = newRules
}

// ClearOverrides removes only override rules, keeping default and config rules.
// This is an alias for ClearRuntimeRules for clarity.
func (e *Engine) ClearOverrides() {
	e.ClearRuntimeRules()
}

// ApplyOverrides adds a set of override rules to the engine.
// These rules are added with RuleSourceOverride and will be removed
// when ClearOverrides() is called (typically at run end).
// Rules with the same name as existing rules will be skipped.
func (e *Engine) ApplyOverrides(rules []config.CustomRule) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Build a set of existing rule names for dedup
	existingNames := make(map[string]bool)
	for _, r := range e.rules {
		existingNames[r.CustomRule.Name] = true
	}

	// Add override rules
	for _, rule := range rules {
		if existingNames[rule.Name] {
			// Skip duplicate - could also return error but skipping is more lenient
			continue
		}
		// Set default enabled if not specified
		if rule.Enabled == nil {
			enabled := true
			rule.Enabled = &enabled
		}
		e.rules = append(e.rules, internalRule{
			CustomRule: rule,
			Source:     config.RuleSourceOverride,
		})
		existingNames[rule.Name] = true
	}

	return nil
}

// HasOverrides returns true if there are any override rules currently active.
func (e *Engine) HasOverrides() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	for _, r := range e.rules {
		if r.Source == config.RuleSourceOverride {
			return true
		}
	}
	return false
}

// Action represents what a rule does when its conditions match.
type Action string

const (
	// ActionDeny stops evaluation and rejects the bead.
	ActionDeny Action = "deny"
	// ActionAllow continues to the next rule (or accepts if last).
	ActionAllow Action = "allow"
)

// RuntimeRule extends CustomRule with source and persistence metadata.
type RuntimeRule struct {
	config.CustomRule
	Source    config.RuleSource `json:"source"`    // where the rule comes from (default/config/override)
	Persisted bool              `json:"persisted"` // true if rule exists in config snapshot
}

// RulesSnapshot contains all rules and settings for API responses.
type RulesSnapshot struct {
	Settings  *config.RulesSettings `json:"settings"`  // Filter settings
	Rules     []RuntimeRule         `json:"rules"`     // Unified rules list with persistence status
	Persisted bool                  `json:"persisted"` // true if entire list matches config snapshot (no additions, deletions, or reorders)
	// Deprecated: use Rules instead. Kept for API backwards compatibility.
	ConfigRules  *config.RulesSettings `json:"config_rules,omitempty"`
	CustomRules  []RuntimeRule         `json:"custom_rules,omitempty"`
	RuntimeRules []RuntimeRule         `json:"runtime_rules,omitempty"`
}

// GetSnapshot returns a complete snapshot of all rules and settings.
func (e *Engine) GetSnapshot() RulesSnapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()

	snapshot := RulesSnapshot{
		Settings:  e.settings,
		Rules:     make([]RuntimeRule, 0, len(e.rules)),
		Persisted: e.isListPersisted(),
		// Deprecated fields for backwards compatibility
		ConfigRules:  e.settings,
		CustomRules:  make([]RuntimeRule, 0),
		RuntimeRules: make([]RuntimeRule, 0),
	}

	// Build unified rules list with source and persistence status
	for _, rule := range e.rules {
		persisted := e.isRulePersisted(rule.CustomRule.Name)
		rr := RuntimeRule{
			CustomRule: rule.CustomRule,
			Source:     rule.Source,
			Persisted:  persisted,
		}
		snapshot.Rules = append(snapshot.Rules, rr)

		// Also populate deprecated fields for backwards compatibility
		if persisted {
			snapshot.CustomRules = append(snapshot.CustomRules, rr)
		} else {
			snapshot.RuntimeRules = append(snapshot.RuntimeRules, rr)
		}
	}

	return snapshot
}

// isRulePersisted checks if a rule with the given name exists in the config snapshot.
// Must be called with mu held (at least read lock).
func (e *Engine) isRulePersisted(name string) bool {
	for _, r := range e.configSnapshot {
		if r.Name == name {
			return true
		}
	}
	return false
}

// isListPersisted checks if the rules list matches the config snapshot.
// Returns true only if there are no override rules and the config rules
// match the snapshot (no additions, deletions, or reorders).
// Must be called with mu held (at least read lock).
func (e *Engine) isListPersisted() bool {
	// Count non-default rules (config + override)
	var nonDefaultRules []internalRule
	for _, r := range e.rules {
		if r.Source != config.RuleSourceDefault {
			nonDefaultRules = append(nonDefaultRules, r)
		}
	}

	// Different lengths means additions or deletions occurred
	if len(nonDefaultRules) != len(e.configSnapshot) {
		return false
	}

	// Compare each non-default rule in order
	for i, rule := range nonDefaultRules {
		if rule.CustomRule.Name != e.configSnapshot[i].Name {
			return false
		}
		// If there are any override rules, not fully persisted
		if rule.Source == config.RuleSourceOverride {
			return false
		}
	}

	return true
}

// GetRuntimeRules returns a copy of all override rules (non-persisted, run-specific).
func (e *Engine) GetRuntimeRules() []config.CustomRule {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var result []config.CustomRule
	for _, r := range e.rules {
		if r.Source == config.RuleSourceOverride {
			result = append(result, r.CustomRule)
		}
	}
	return result
}

// GetOverrideRules returns a copy of all override rules (alias for GetRuntimeRules).
func (e *Engine) GetOverrideRules() []config.CustomRule {
	return e.GetRuntimeRules()
}

// PersistRule marks a rule as persisted by adding it to the config snapshot.
// The rule must exist in the unified rules slice and not already be persisted.
// When an override rule is persisted, its source changes to config.
// Returns the persisted rule on success.
func (e *Engine) PersistRule(name string) (*config.CustomRule, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Find the rule in the unified slice
	var foundIndex int = -1
	for i := range e.rules {
		if e.rules[i].CustomRule.Name == name {
			foundIndex = i
			break
		}
	}

	if foundIndex == -1 {
		return nil, fmt.Errorf("rule %q not found", name)
	}

	// Check if it's already persisted
	if e.isRulePersisted(name) {
		return nil, fmt.Errorf("rule %q is already persisted", name)
	}

	foundRule := &e.rules[foundIndex].CustomRule

	// Add to config snapshot (marks as persisted)
	e.configSnapshot = append(e.configSnapshot, *foundRule)

	// Change source from override to config
	e.rules[foundIndex].Source = config.RuleSourceConfig

	// Also update settings.Custom for persistence to disk
	if e.settings == nil {
		e.settings = &config.RulesSettings{}
	}
	e.settings.Custom = append(e.settings.Custom, *foundRule)

	return foundRule, nil
}

// PersistAllRules marks all override rules as persisted (changes source to config).
// Returns the list of rule names that were persisted.
func (e *Engine) PersistAllRules() ([]string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	var persisted []string
	for i := range e.rules {
		rule := &e.rules[i]
		if rule.Source == config.RuleSourceOverride {
			e.configSnapshot = append(e.configSnapshot, rule.CustomRule)
			rule.Source = config.RuleSourceConfig
			persisted = append(persisted, rule.CustomRule.Name)
		}
	}

	if len(persisted) == 0 {
		return nil, nil
	}

	// Update settings.Custom for persistence to disk
	if e.settings == nil {
		e.settings = &config.RulesSettings{}
	}
	e.settings.Custom = make([]config.CustomRule, len(e.configSnapshot))
	copy(e.settings.Custom, e.configSnapshot)

	return persisted, nil
}

// GetConfigForPersistence returns a copy of the current config settings
// suitable for saving to disk. This includes any recently persisted rules.
func (e *Engine) GetConfigForPersistence() *config.RulesSettings {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.settings == nil {
		return nil
	}

	// Deep copy the settings
	result := *e.settings
	if e.configSnapshot != nil {
		result.Custom = make([]config.CustomRule, len(e.configSnapshot))
		for i, rule := range e.configSnapshot {
			ruleCopy := rule
			if rule.Enabled != nil {
				enabled := *rule.Enabled
				ruleCopy.Enabled = &enabled
			}
			result.Custom[i] = ruleCopy
		}
	}
	if e.settings.MaxConcurrentPerType != nil {
		result.MaxConcurrentPerType = make(map[string]int)
		for k, v := range e.settings.MaxConcurrentPerType {
			result.MaxConcurrentPerType[k] = v
		}
	}
	if e.settings.MaxConcurrentPerLabel != nil {
		result.MaxConcurrentPerLabel = make(map[string]int)
		for k, v := range e.settings.MaxConcurrentPerLabel {
			result.MaxConcurrentPerLabel[k] = v
		}
	}

	return &result
}

// GetRule returns a rule by name from the unified rules slice.
// Returns nil if not found.
func (e *Engine) GetRule(name string) *RuntimeRule {
	e.mu.RLock()
	defer e.mu.RUnlock()

	for _, rule := range e.rules {
		if rule.CustomRule.Name == name {
			return &RuntimeRule{
				CustomRule: rule.CustomRule,
				Source:     rule.Source,
				Persisted:  e.isRulePersisted(name),
			}
		}
	}

	return nil
}

// UpdateRule updates an existing rule's enabled state.
// Returns an error if the rule is not found.
func (e *Engine) UpdateRule(name string, enabled bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	for i := range e.rules {
		if e.rules[i].CustomRule.Name == name {
			e.rules[i].CustomRule.Enabled = &enabled
			return nil
		}
	}

	return fmt.Errorf("rule %q not found", name)
}

// ReorderRule moves a rule to a new position in the rules slice.
// Position is 0-indexed. Returns an error if the rule is not found or
// the position is out of range.
func (e *Engine) ReorderRule(name string, newPosition int) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Find the rule first
	currentIndex := -1
	for i, r := range e.rules {
		if r.CustomRule.Name == name {
			currentIndex = i
			break
		}
	}

	if currentIndex == -1 {
		return fmt.Errorf("rule %q not found", name)
	}

	// Validate position
	if newPosition < 0 || newPosition >= len(e.rules) {
		return fmt.Errorf("position %d out of range (0-%d)", newPosition, len(e.rules)-1)
	}

	// If already at target position, no-op
	if currentIndex == newPosition {
		return nil
	}

	// Remove rule from current position
	rule := e.rules[currentIndex]
	e.rules = append(e.rules[:currentIndex], e.rules[currentIndex+1:]...)

	// Insert at new position
	e.rules = append(e.rules[:newPosition], append([]internalRule{rule}, e.rules[newPosition:]...)...)

	return nil
}

// AddRuleWithValidation adds a rule after validating it.
// Returns an error if validation fails or if a rule with the same name exists.
// New rules are added as override source by default.
func (e *Engine) AddRuleWithValidation(rule config.CustomRule) error {
	return e.AddRuleWithValidationAndSource(rule, config.RuleSourceOverride)
}

// AddRuleWithValidationAndSource adds a rule with explicit source after validating it.
// Returns an error if validation fails or if a rule with the same name exists.
// Validates rule name length, condition syntax, and reason length before adding.
func (e *Engine) AddRuleWithValidationAndSource(rule config.CustomRule, source config.RuleSource) error {
	// Validate rule name (required and length limit)
	if err := ValidateRuleName(rule.Name); err != nil {
		return err
	}

	// Validate condition (required and syntax)
	if rule.Condition == "" {
		return fmt.Errorf("rule condition is required")
	}
	if err := ValidateConditionSyntax(rule.Condition); err != nil {
		return fmt.Errorf("invalid condition: %w", err)
	}

	// Validate reason length if present
	if err := ValidateReason(rule.Reason); err != nil {
		return err
	}

	// Validate action format
	if rule.Action == "" {
		return fmt.Errorf("rule action is required")
	}

	// Validate action format - only deny and allow are supported
	// For backwards compatibility, skip and include are also accepted
	switch rule.Action {
	case "deny", "allow", "skip", "include":
		// Valid actions
	default:
		return fmt.Errorf("invalid action %q (allowed: deny, allow)", rule.Action)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// Check for duplicate name in unified rules slice
	for _, r := range e.rules {
		if r.CustomRule.Name == rule.Name {
			return fmt.Errorf("rule %q already exists", rule.Name)
		}
	}

	// Set default enabled state if not specified
	if rule.Enabled == nil {
		enabled := true
		rule.Enabled = &enabled
	}

	e.rules = append(e.rules, internalRule{
		CustomRule: rule,
		Source:     source,
	})
	return nil
}

// GetConfigSettings returns a copy of the filter settings.
// Returns nil if no settings are set.
func (e *Engine) GetConfigSettings() *config.RulesSettings {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.settings == nil {
		return nil
	}

	// Return a copy to prevent external modification
	result := *e.settings
	return &result
}

// UpdateConfigSettings updates specific fields in the filter settings.
// Only non-nil fields in the update are applied.
// Returns an error if validation fails.
func (e *Engine) UpdateConfigSettings(update ConfigSettingsUpdate) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.settings == nil {
		e.settings = &config.RulesSettings{}
	}

	// Apply updates
	if update.PriorityMin != nil {
		if *update.PriorityMin < 0 || *update.PriorityMin > 4 {
			return fmt.Errorf("priority_min must be 0-4, got %d", *update.PriorityMin)
		}
		e.settings.PriorityMin = *update.PriorityMin
	}

	if update.PriorityMax != nil {
		if *update.PriorityMax != -1 && (*update.PriorityMax < 0 || *update.PriorityMax > 4) {
			return fmt.Errorf("priority_max must be -1 (no filter) or 0-4, got %d", *update.PriorityMax)
		}
		e.settings.PriorityMax = *update.PriorityMax
	}

	// Validate priority range after update
	if e.settings.PriorityMax != -1 && e.settings.PriorityMin > e.settings.PriorityMax {
		return fmt.Errorf("priority_min (%d) cannot be greater than priority_max (%d)", e.settings.PriorityMin, e.settings.PriorityMax)
	}

	if update.Types != nil {
		e.settings.Types = *update.Types
	}

	if update.ExcludeTypes != nil {
		e.settings.ExcludeTypes = *update.ExcludeTypes
	}

	if update.Labels != nil {
		e.settings.Labels = *update.Labels
	}

	if update.ExcludeLabels != nil {
		e.settings.ExcludeLabels = *update.ExcludeLabels
	}

	if update.Assignee != nil {
		e.settings.Assignee = *update.Assignee
	}

	if update.MaxConcurrentTasks != nil {
		if *update.MaxConcurrentTasks < 0 {
			return fmt.Errorf("max_concurrent_tasks must be >= 0, got %d", *update.MaxConcurrentTasks)
		}
		e.settings.MaxConcurrentTasks = *update.MaxConcurrentTasks
	}

	if update.MaxConcurrentPerType != nil {
		e.settings.MaxConcurrentPerType = *update.MaxConcurrentPerType
	}

	if update.MaxConcurrentPerLabel != nil {
		e.settings.MaxConcurrentPerLabel = *update.MaxConcurrentPerLabel
	}

	return nil
}

// ConfigSettingsUpdate contains optional updates to config settings.
// Only non-nil fields are applied.
type ConfigSettingsUpdate struct {
	PriorityMin           *int              `json:"priority_min,omitempty"`
	PriorityMax           *int              `json:"priority_max,omitempty"`
	Types                 *[]string         `json:"types,omitempty"`
	ExcludeTypes          *[]string         `json:"exclude_types,omitempty"`
	Labels                *[]string         `json:"labels,omitempty"`
	ExcludeLabels         *[]string         `json:"exclude_labels,omitempty"`
	Assignee              *string           `json:"assignee,omitempty"`
	MaxConcurrentTasks    *int              `json:"max_concurrent_tasks,omitempty"`
	MaxConcurrentPerType  *map[string]int   `json:"max_concurrent_per_type,omitempty"`
	MaxConcurrentPerLabel *map[string]int   `json:"max_concurrent_per_label,omitempty"`
}

// Evaluate returns whether a task should be selected and any modifications.
// The inFlight parameter contains task IDs of currently executing tasks.
// The inFlightTasks parameter maps task IDs to their Task objects (for type/label counting).
func (e *Engine) Evaluate(task *beads.Task, inFlight map[string]bool, inFlightTasks map[string]*beads.Task) EvalResult {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.settings == nil {
		return EvalResult{Allow: true}
	}

	result := EvalResult{Allow: true}

	// 1. Priority range check
	if task.Priority < e.settings.PriorityMin {
		return EvalResult{Skip: true, SkipReason: "below priority minimum"}
	}
	if e.settings.PriorityMax >= 0 && task.Priority > e.settings.PriorityMax {
		return EvalResult{Skip: true, SkipReason: "above priority maximum"}
	}

	// 2. Type whitelist/blacklist
	if len(e.settings.Types) > 0 && !contains(e.settings.Types, task.Type) {
		return EvalResult{Skip: true, SkipReason: "type not in whitelist"}
	}
	if contains(e.settings.ExcludeTypes, task.Type) {
		return EvalResult{Skip: true, SkipReason: "type in blacklist"}
	}

	// 3. Label whitelist/blacklist
	if len(e.settings.Labels) > 0 && !hasAnyLabel(task.Labels, e.settings.Labels) {
		return EvalResult{Skip: true, SkipReason: "no matching label in whitelist"}
	}
	if hasAnyLabel(task.Labels, e.settings.ExcludeLabels) {
		return EvalResult{Skip: true, SkipReason: "has excluded label"}
	}

	// 4. Assignee check
	switch e.settings.Assignee {
	case "":
		// Empty string = unassigned only
		if task.Assignee != "" {
			return EvalResult{Skip: true, SkipReason: "task is assigned"}
		}
	case "*":
		// Any assignee (including unassigned) - no filter
	default:
		// Specific value = exact match
		if task.Assignee != e.settings.Assignee {
			return EvalResult{Skip: true, SkipReason: fmt.Sprintf("assignee is %q, want %q", task.Assignee, e.settings.Assignee)}
		}
	}

	// 5. Concurrency limits (per-type)
	if limit, ok := e.settings.MaxConcurrentPerType[task.Type]; ok && limit > 0 {
		typeCount := countInFlightByType(inFlightTasks, task.Type)
		if typeCount >= limit {
			return EvalResult{Skip: true, SkipReason: fmt.Sprintf("type %q at concurrency limit (%d)", task.Type, limit)}
		}
	}

	// 6. Concurrency limits (per-label)
	for _, label := range task.Labels {
		if limit, ok := e.settings.MaxConcurrentPerLabel[label]; ok && limit > 0 {
			labelCount := countInFlightByLabel(inFlightTasks, label)
			if labelCount >= limit {
				return EvalResult{Skip: true, SkipReason: fmt.Sprintf("label %q at concurrency limit (%d)", label, limit)}
			}
		}
	}

	// 7. Custom rules (unified slice)
	// Rules are evaluated in order. DENY stops evaluation and rejects.
	// ALLOW continues to the next rule (or accepts if last).
	for _, rule := range e.rules {
		r := rule.CustomRule
		// Skip disabled rules
		if r.Enabled != nil && !*r.Enabled {
			continue
		}

		if matches := EvaluateCondition(r.Condition, task); matches {
			action := parseAction(r.Action)
			switch action {
			case ActionDeny:
				reason := r.Reason
				if reason == "" {
					reason = "Denied by rule: " + r.Name
				}
				return EvalResult{Skip: true, SkipReason: reason}

			case ActionAllow:
				// Continue to next rule (or accept if last)
				continue
			}
		}
	}

	return result
}

// contains checks if a slice contains a string.
func contains(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

// hasAnyLabel checks if taskLabels contains any of the targetLabels.
func hasAnyLabel(taskLabels, targetLabels []string) bool {
	for _, target := range targetLabels {
		for _, label := range taskLabels {
			if label == target {
				return true
			}
		}
	}
	return false
}

// countInFlightByType counts in-flight tasks of a given type.
func countInFlightByType(inFlightTasks map[string]*beads.Task, taskType string) int {
	count := 0
	for _, task := range inFlightTasks {
		if task != nil && task.Type == taskType {
			count++
		}
	}
	return count
}

// countInFlightByLabel counts in-flight tasks that have a given label.
func countInFlightByLabel(inFlightTasks map[string]*beads.Task, label string) int {
	count := 0
	for _, task := range inFlightTasks {
		if task == nil {
			continue
		}
		for _, l := range task.Labels {
			if l == label {
				count++
				break
			}
		}
	}
	return count
}

// parseAction parses an action string into an Action type.
// Supported formats:
//   - "deny" - stop evaluation and reject the bead
//   - "allow" - continue to next rule (or accept if last)
//
// For backwards compatibility, "skip" is treated as "deny".
// Unknown actions default to "allow" (continue evaluation).
func parseAction(action string) Action {
	switch action {
	case "deny", "skip":
		return ActionDeny
	case "allow", "include":
		return ActionAllow
	default:
		// Unknown action, treat as allow (continue evaluation)
		return ActionAllow
	}
}
