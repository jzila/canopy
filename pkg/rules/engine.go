// Package rules provides a rule evaluation engine for task selection.
package rules

import (
	"fmt"
	"sync"
	"time"

	"github.com/jzila/canopy/pkg/beads"
	"github.com/jzila/canopy/pkg/config"
)

// Engine evaluates task selection rules against candidate tasks.
type Engine struct {
	config  *config.RulesSettings
	runtime []config.CustomRule // Runtime-added rules (in-memory)
	mu      sync.RWMutex
}

// NewEngine creates a new rule evaluation engine.
func NewEngine(cfg *config.RulesSettings) *Engine {
	return &Engine{
		config:  cfg,
		runtime: nil,
	}
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

// AddRule adds a runtime rule to the engine.
// Runtime rules are evaluated after config rules.
func (e *Engine) AddRule(rule config.CustomRule) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.runtime = append(e.runtime, rule)
}

// RemoveRule removes a runtime rule by name.
// Returns true if a rule was removed.
func (e *Engine) RemoveRule(name string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i, r := range e.runtime {
		if r.Name == name {
			e.runtime = append(e.runtime[:i], e.runtime[i+1:]...)
			return true
		}
	}
	return false
}

// ClearRuntimeRules removes all runtime rules.
func (e *Engine) ClearRuntimeRules() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.runtime = nil
}

// RuntimeRule extends CustomRule with runtime-specific metadata.
type RuntimeRule struct {
	config.CustomRule
	Source    string    `json:"source"`     // "config" or "runtime"
	CreatedAt time.Time `json:"created_at"` // When the rule was added (for runtime rules)
}

// RulesSnapshot contains all rules and settings for API responses.
type RulesSnapshot struct {
	ConfigRules  *config.RulesSettings `json:"config_rules"`
	CustomRules  []RuntimeRule         `json:"custom_rules"`  // Config-sourced custom rules
	RuntimeRules []RuntimeRule         `json:"runtime_rules"` // Runtime-added rules
}

// GetSnapshot returns a complete snapshot of all rules and settings.
func (e *Engine) GetSnapshot() RulesSnapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()

	snapshot := RulesSnapshot{
		ConfigRules:  e.config,
		CustomRules:  make([]RuntimeRule, 0),
		RuntimeRules: make([]RuntimeRule, 0),
	}

	// Add config-sourced custom rules
	if e.config != nil {
		for _, rule := range e.config.Custom {
			snapshot.CustomRules = append(snapshot.CustomRules, RuntimeRule{
				CustomRule: rule,
				Source:     "config",
			})
		}
	}

	// Add runtime rules
	for _, rule := range e.runtime {
		snapshot.RuntimeRules = append(snapshot.RuntimeRules, RuntimeRule{
			CustomRule: rule,
			Source:     "runtime",
		})
	}

	return snapshot
}

// GetRuntimeRules returns a copy of all runtime rules.
func (e *Engine) GetRuntimeRules() []config.CustomRule {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.runtime == nil {
		return nil
	}

	rules := make([]config.CustomRule, len(e.runtime))
	copy(rules, e.runtime)
	return rules
}

// PersistRule moves a runtime rule to the config, making it permanent.
// The rule is removed from runtime rules and added to config.Custom.
// Returns the persisted rule on success.
func (e *Engine) PersistRule(name string) (*config.CustomRule, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Find the runtime rule
	var foundIdx = -1
	var foundRule config.CustomRule
	for i, r := range e.runtime {
		if r.Name == name {
			foundIdx = i
			foundRule = r
			break
		}
	}

	if foundIdx == -1 {
		// Check if it's already a config rule
		if e.config != nil {
			for _, r := range e.config.Custom {
				if r.Name == name {
					return nil, fmt.Errorf("rule %q is already a config rule", name)
				}
			}
		}
		return nil, fmt.Errorf("runtime rule %q not found", name)
	}

	// Initialize config if needed
	if e.config == nil {
		e.config = &config.RulesSettings{}
	}

	// Add to config custom rules
	e.config.Custom = append(e.config.Custom, foundRule)

	// Remove from runtime rules
	e.runtime = append(e.runtime[:foundIdx], e.runtime[foundIdx+1:]...)

	return &foundRule, nil
}

// PersistAllRules moves all runtime rules to config.
// Returns the list of rule names that were persisted.
func (e *Engine) PersistAllRules() ([]string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if len(e.runtime) == 0 {
		return nil, nil
	}

	// Initialize config if needed
	if e.config == nil {
		e.config = &config.RulesSettings{}
	}

	var persisted []string
	for _, rule := range e.runtime {
		e.config.Custom = append(e.config.Custom, rule)
		persisted = append(persisted, rule.Name)
	}

	// Clear runtime rules
	e.runtime = nil

	return persisted, nil
}

// GetConfigForPersistence returns a copy of the current config settings
// suitable for saving to disk. This includes any recently persisted rules.
func (e *Engine) GetConfigForPersistence() *config.RulesSettings {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.config == nil {
		return nil
	}

	// Deep copy the config
	copy := *e.config
	if e.config.Custom != nil {
		copy.Custom = make([]config.CustomRule, len(e.config.Custom))
		for i, rule := range e.config.Custom {
			ruleCopy := rule
			if rule.Enabled != nil {
				enabled := *rule.Enabled
				ruleCopy.Enabled = &enabled
			}
			copy.Custom[i] = ruleCopy
		}
	}
	if e.config.MaxConcurrentPerType != nil {
		copy.MaxConcurrentPerType = make(map[string]int)
		for k, v := range e.config.MaxConcurrentPerType {
			copy.MaxConcurrentPerType[k] = v
		}
	}
	if e.config.MaxConcurrentPerLabel != nil {
		copy.MaxConcurrentPerLabel = make(map[string]int)
		for k, v := range e.config.MaxConcurrentPerLabel {
			copy.MaxConcurrentPerLabel[k] = v
		}
	}

	return &copy
}

// GetRule returns a rule by name, searching both config and runtime rules.
// Returns nil if not found.
func (e *Engine) GetRule(name string) *RuntimeRule {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// Search config rules first
	if e.config != nil {
		for _, rule := range e.config.Custom {
			if rule.Name == name {
				return &RuntimeRule{
					CustomRule: rule,
					Source:     "config",
				}
			}
		}
	}

	// Search runtime rules
	for _, rule := range e.runtime {
		if rule.Name == name {
			return &RuntimeRule{
				CustomRule: rule,
				Source:     "runtime",
			}
		}
	}

	return nil
}

// UpdateRule updates an existing rule's enabled state.
// Returns an error if the rule is not found.
// Note: Config rules can be disabled but not deleted; runtime rules can be both.
func (e *Engine) UpdateRule(name string, enabled bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Search config rules first
	if e.config != nil {
		for i := range e.config.Custom {
			if e.config.Custom[i].Name == name {
				e.config.Custom[i].Enabled = &enabled
				return nil
			}
		}
	}

	// Search runtime rules
	for i := range e.runtime {
		if e.runtime[i].Name == name {
			e.runtime[i].Enabled = &enabled
			return nil
		}
	}

	return fmt.Errorf("rule %q not found", name)
}

// AddRuleWithValidation adds a runtime rule after validating it.
// Returns an error if validation fails or if a rule with the same name exists.
func (e *Engine) AddRuleWithValidation(rule config.CustomRule) error {
	// Validate the rule
	if rule.Name == "" {
		return fmt.Errorf("rule name is required")
	}
	if rule.Condition == "" {
		return fmt.Errorf("rule condition is required")
	}
	if rule.Action == "" {
		return fmt.Errorf("rule action is required")
	}

	// Validate action format
	action := parseAction(rule.Action)
	if action.actionType == actionInclude && rule.Action != "include" && rule.Action != "allow" && rule.Action != "skip" {
		// Check if it's a valid parameterized action
		if len(rule.Action) > 6 && (rule.Action[:6] == "boost:" || rule.Action[:6] == "limit:") {
			// Valid parameterized action
		} else {
			return fmt.Errorf("invalid action %q (allowed: skip, include, allow, boost:N, limit:N)", rule.Action)
		}
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// Check for duplicate name in config rules
	if e.config != nil {
		for _, r := range e.config.Custom {
			if r.Name == rule.Name {
				return fmt.Errorf("rule %q already exists in config", rule.Name)
			}
		}
	}

	// Check for duplicate name in runtime rules
	for _, r := range e.runtime {
		if r.Name == rule.Name {
			return fmt.Errorf("rule %q already exists", rule.Name)
		}
	}

	// Set default enabled state if not specified
	if rule.Enabled == nil {
		enabled := true
		rule.Enabled = &enabled
	}

	e.runtime = append(e.runtime, rule)
	return nil
}

// GetConfigSettings returns a copy of the config-based rules settings.
// Returns nil if no config is set.
func (e *Engine) GetConfigSettings() *config.RulesSettings {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.config == nil {
		return nil
	}

	// Return a copy to prevent external modification
	copy := *e.config
	return &copy
}

// UpdateConfigSettings updates specific fields in the config settings.
// Only non-nil fields in the update are applied.
// Returns an error if validation fails.
func (e *Engine) UpdateConfigSettings(update ConfigSettingsUpdate) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.config == nil {
		e.config = &config.RulesSettings{}
	}

	// Apply updates
	if update.PriorityMin != nil {
		if *update.PriorityMin < 0 || *update.PriorityMin > 4 {
			return fmt.Errorf("priority_min must be 0-4, got %d", *update.PriorityMin)
		}
		e.config.PriorityMin = *update.PriorityMin
	}

	if update.PriorityMax != nil {
		if *update.PriorityMax != -1 && (*update.PriorityMax < 0 || *update.PriorityMax > 4) {
			return fmt.Errorf("priority_max must be -1 (no filter) or 0-4, got %d", *update.PriorityMax)
		}
		e.config.PriorityMax = *update.PriorityMax
	}

	// Validate priority range after update
	if e.config.PriorityMax != -1 && e.config.PriorityMin > e.config.PriorityMax {
		return fmt.Errorf("priority_min (%d) cannot be greater than priority_max (%d)", e.config.PriorityMin, e.config.PriorityMax)
	}

	if update.Types != nil {
		e.config.Types = *update.Types
	}

	if update.ExcludeTypes != nil {
		e.config.ExcludeTypes = *update.ExcludeTypes
	}

	if update.Labels != nil {
		e.config.Labels = *update.Labels
	}

	if update.ExcludeLabels != nil {
		e.config.ExcludeLabels = *update.ExcludeLabels
	}

	if update.Assignee != nil {
		e.config.Assignee = *update.Assignee
	}

	if update.MaxConcurrent != nil {
		if *update.MaxConcurrent < 0 {
			return fmt.Errorf("max_concurrent must be >= 0, got %d", *update.MaxConcurrent)
		}
		e.config.MaxConcurrent = *update.MaxConcurrent
	}

	if update.MaxConcurrentPerType != nil {
		e.config.MaxConcurrentPerType = *update.MaxConcurrentPerType
	}

	if update.MaxConcurrentPerLabel != nil {
		e.config.MaxConcurrentPerLabel = *update.MaxConcurrentPerLabel
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
	MaxConcurrent         *int              `json:"max_concurrent,omitempty"`
	MaxConcurrentPerType  *map[string]int   `json:"max_concurrent_per_type,omitempty"`
	MaxConcurrentPerLabel *map[string]int   `json:"max_concurrent_per_label,omitempty"`
}

// allRules returns combined config and runtime rules.
func (e *Engine) allRules() []config.CustomRule {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.config == nil {
		return e.runtime
	}

	rules := make([]config.CustomRule, 0, len(e.config.Custom)+len(e.runtime))
	rules = append(rules, e.config.Custom...)
	rules = append(rules, e.runtime...)
	return rules
}

// Evaluate returns whether a task should be selected and any modifications.
// The inFlight parameter contains task IDs of currently executing tasks.
// The inFlightTasks parameter maps task IDs to their Task objects (for type/label counting).
func (e *Engine) Evaluate(task *beads.Task, inFlight map[string]bool, inFlightTasks map[string]*beads.Task) EvalResult {
	if e.config == nil {
		return EvalResult{Allow: true}
	}

	result := EvalResult{Allow: true}

	// 1. Priority range check
	if task.Priority < e.config.PriorityMin {
		return EvalResult{Skip: true, SkipReason: "below priority minimum"}
	}
	if e.config.PriorityMax >= 0 && task.Priority > e.config.PriorityMax {
		return EvalResult{Skip: true, SkipReason: "above priority maximum"}
	}

	// 2. Type whitelist/blacklist
	if len(e.config.Types) > 0 && !contains(e.config.Types, task.Type) {
		return EvalResult{Skip: true, SkipReason: "type not in whitelist"}
	}
	if contains(e.config.ExcludeTypes, task.Type) {
		return EvalResult{Skip: true, SkipReason: "type in blacklist"}
	}

	// 3. Label whitelist/blacklist
	if len(e.config.Labels) > 0 && !hasAnyLabel(task.Labels, e.config.Labels) {
		return EvalResult{Skip: true, SkipReason: "no matching label in whitelist"}
	}
	if hasAnyLabel(task.Labels, e.config.ExcludeLabels) {
		return EvalResult{Skip: true, SkipReason: "has excluded label"}
	}

	// 4. Assignee check
	switch e.config.Assignee {
	case "":
		// Empty string = unassigned only
		if task.Assignee != "" {
			return EvalResult{Skip: true, SkipReason: "task is assigned"}
		}
	case "*":
		// Any assignee (including unassigned) - no filter
	default:
		// Specific value = exact match
		if task.Assignee != e.config.Assignee {
			return EvalResult{Skip: true, SkipReason: fmt.Sprintf("assignee is %q, want %q", task.Assignee, e.config.Assignee)}
		}
	}

	// 5. Concurrency limits (per-type)
	if limit, ok := e.config.MaxConcurrentPerType[task.Type]; ok && limit > 0 {
		typeCount := countInFlightByType(inFlightTasks, task.Type)
		if typeCount >= limit {
			return EvalResult{Skip: true, SkipReason: fmt.Sprintf("type %q at concurrency limit (%d)", task.Type, limit)}
		}
	}

	// 6. Concurrency limits (per-label)
	for _, label := range task.Labels {
		if limit, ok := e.config.MaxConcurrentPerLabel[label]; ok && limit > 0 {
			labelCount := countInFlightByLabel(inFlightTasks, label)
			if labelCount >= limit {
				return EvalResult{Skip: true, SkipReason: fmt.Sprintf("label %q at concurrency limit (%d)", label, limit)}
			}
		}
	}

	// 7. Custom rules (config + runtime)
	for _, rule := range e.allRules() {
		// Skip disabled rules
		if rule.Enabled != nil && !*rule.Enabled {
			continue
		}

		if matches := EvaluateCondition(rule.Condition, task); matches {
			action := parseAction(rule.Action)
			switch action.actionType {
			case actionSkip:
				reason := rule.Reason
				if reason == "" {
					reason = "Skipped by rule: " + rule.Name
				}
				return EvalResult{Skip: true, SkipReason: reason}

			case actionBoost:
				result.BoostAmount += action.boostAmount

			case actionLimit:
				// Check if we're at the limit for this rule
				if action.limitMax > 0 {
					limitCount := countInFlightByRule(inFlight, rule.Name)
					if limitCount >= action.limitMax {
						return EvalResult{Skip: true, SkipReason: fmt.Sprintf("rule %q at limit (%d)", rule.Name, action.limitMax)}
					}
					result.LimitKey = rule.Name
					result.LimitMax = action.limitMax
				}

			case actionAllow:
				// Explicit allow - skip remaining rules
				return EvalResult{Allow: true, BoostAmount: result.BoostAmount}

			case actionInclude:
				// Continue checking other rules (no-op)
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

// countInFlightByRule counts in-flight tasks that match a rule.
// For now, this just counts all in-flight tasks (rules don't track matches yet).
// This is a placeholder for more sophisticated tracking.
func countInFlightByRule(inFlight map[string]bool, ruleName string) int {
	// TODO: Track which tasks match which rules for accurate limit counting
	// For now, we don't have this information, so return 0
	_ = ruleName
	return 0
}

// actionType represents the type of action to take.
type actionType int

const (
	actionSkip actionType = iota
	actionInclude
	actionAllow
	actionBoost
	actionLimit
)

// parsedAction contains the parsed action and any parameters.
type parsedAction struct {
	actionType  actionType
	boostAmount int
	limitMax    int
}

// parseAction parses an action string into its type and parameters.
// Supported formats:
//   - "skip" - skip the task
//   - "include" - continue checking (no-op)
//   - "allow" - explicitly allow, skip remaining rules
//   - "boost:5" - add 5 to priority boost
//   - "limit:3" - limit concurrent tasks matching this rule to 3
func parseAction(action string) parsedAction {
	// Check for parameterized actions
	if len(action) > 6 && action[:6] == "boost:" {
		var amount int
		fmt.Sscanf(action, "boost:%d", &amount)
		return parsedAction{actionType: actionBoost, boostAmount: amount}
	}

	if len(action) > 6 && action[:6] == "limit:" {
		var limit int
		fmt.Sscanf(action, "limit:%d", &limit)
		return parsedAction{actionType: actionLimit, limitMax: limit}
	}

	switch action {
	case "skip":
		return parsedAction{actionType: actionSkip}
	case "include":
		return parsedAction{actionType: actionInclude}
	case "allow":
		return parsedAction{actionType: actionAllow}
	default:
		// Unknown action, treat as include (continue)
		return parsedAction{actionType: actionInclude}
	}
}
