// Package rules provides a rule evaluation engine for task selection.
package rules

import (
	"fmt"
	"sync"

	"github.com/jzila/canopy/pkg/beads"
	"github.com/jzila/canopy/pkg/config"
)

// Engine evaluates task selection rules against candidate tasks.
// Rules are stored in a single unified slice, evaluated in order.
// The configSnapshot tracks the original config state for persistence tracking.
type Engine struct {
	settings       *config.RulesSettings // Filter settings (priority, types, labels, etc.)
	rules          []config.CustomRule   // Unified rules slice (config + runtime)
	configSnapshot []config.CustomRule   // Snapshot of rules loaded from config (for persistence tracking)
	mu             sync.RWMutex
}

// NewEngine creates a new rule evaluation engine.
// Rules from cfg.Custom are loaded into the unified rules slice.
func NewEngine(cfg *config.RulesSettings) *Engine {
	e := &Engine{
		settings: cfg,
		rules:    nil,
	}

	// Load config rules into unified slice and snapshot
	if cfg != nil && len(cfg.Custom) > 0 {
		e.rules = make([]config.CustomRule, len(cfg.Custom))
		copy(e.rules, cfg.Custom)
		e.configSnapshot = make([]config.CustomRule, len(cfg.Custom))
		copy(e.configSnapshot, cfg.Custom)
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
func (e *Engine) AddRule(rule config.CustomRule) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rules = append(e.rules, rule)
}

// RemoveRule removes a rule by name from the unified rules slice.
// Returns true if a rule was removed.
func (e *Engine) RemoveRule(name string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i, r := range e.rules {
		if r.Name == name {
			e.rules = append(e.rules[:i], e.rules[i+1:]...)
			return true
		}
	}
	return false
}

// ClearRuntimeRules removes all non-persisted rules (resets to config snapshot).
func (e *Engine) ClearRuntimeRules() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.configSnapshot != nil {
		e.rules = make([]config.CustomRule, len(e.configSnapshot))
		copy(e.rules, e.configSnapshot)
	} else {
		e.rules = nil
	}
}

// Action represents what a rule does when its conditions match.
type Action string

const (
	// ActionDeny stops evaluation and rejects the bead.
	ActionDeny Action = "deny"
	// ActionAllow continues to the next rule (or accepts if last).
	ActionAllow Action = "allow"
)

// RuntimeRule extends CustomRule with persistence metadata.
type RuntimeRule struct {
	config.CustomRule
	Persisted bool `json:"persisted"` // true if rule exists in config snapshot
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

	// Build unified rules list with persistence status
	for _, rule := range e.rules {
		persisted := e.isRulePersisted(rule.Name)
		rr := RuntimeRule{
			CustomRule: rule,
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

// isListPersisted checks if the entire rules list matches the config snapshot.
// Returns true only if:
// - The lists have the same length (no additions or deletions)
// - Rules appear in the same order
// - Each rule in the current list matches the corresponding rule in the snapshot
// Must be called with mu held (at least read lock).
func (e *Engine) isListPersisted() bool {
	// Different lengths means additions or deletions occurred
	if len(e.rules) != len(e.configSnapshot) {
		return false
	}

	// Compare each rule in order
	for i, rule := range e.rules {
		if rule.Name != e.configSnapshot[i].Name {
			return false
		}
	}

	return true
}

// GetRuntimeRules returns a copy of all non-persisted rules.
func (e *Engine) GetRuntimeRules() []config.CustomRule {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var result []config.CustomRule
	for _, r := range e.rules {
		if !e.isRulePersisted(r.Name) {
			result = append(result, r)
		}
	}
	return result
}

// PersistRule marks a rule as persisted by adding it to the config snapshot.
// The rule must exist in the unified rules slice and not already be persisted.
// Returns the persisted rule on success.
func (e *Engine) PersistRule(name string) (*config.CustomRule, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Find the rule in the unified slice
	var foundRule *config.CustomRule
	for i := range e.rules {
		if e.rules[i].Name == name {
			foundRule = &e.rules[i]
			break
		}
	}

	if foundRule == nil {
		return nil, fmt.Errorf("rule %q not found", name)
	}

	// Check if it's already persisted
	if e.isRulePersisted(name) {
		return nil, fmt.Errorf("rule %q is already persisted", name)
	}

	// Add to config snapshot (marks as persisted)
	e.configSnapshot = append(e.configSnapshot, *foundRule)

	// Also update settings.Custom for persistence to disk
	if e.settings == nil {
		e.settings = &config.RulesSettings{}
	}
	e.settings.Custom = append(e.settings.Custom, *foundRule)

	return foundRule, nil
}

// PersistAllRules marks all non-persisted rules as persisted.
// Returns the list of rule names that were persisted.
func (e *Engine) PersistAllRules() ([]string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	var persisted []string
	for _, rule := range e.rules {
		if !e.isRulePersisted(rule.Name) {
			e.configSnapshot = append(e.configSnapshot, rule)
			persisted = append(persisted, rule.Name)
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

// SaveRules replaces the config snapshot with the current rules list.
// This handles additions, deletions, and reorders - making the current
// in-memory state the new source of truth.
// Returns the count of rules saved.
func (e *Engine) SaveRules() int {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Replace config snapshot with current rules list
	e.configSnapshot = make([]config.CustomRule, len(e.rules))
	copy(e.configSnapshot, e.rules)

	// Update settings.Custom for persistence to disk
	if e.settings == nil {
		e.settings = &config.RulesSettings{}
	}
	e.settings.Custom = make([]config.CustomRule, len(e.rules))
	copy(e.settings.Custom, e.rules)

	return len(e.rules)
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
		if rule.Name == name {
			return &RuntimeRule{
				CustomRule: rule,
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
		if e.rules[i].Name == name {
			e.rules[i].Enabled = &enabled
			return nil
		}
	}

	return fmt.Errorf("rule %q not found", name)
}

// AddRuleWithValidation adds a rule after validating it.
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
		if r.Name == rule.Name {
			return fmt.Errorf("rule %q already exists", rule.Name)
		}
	}

	// Set default enabled state if not specified
	if rule.Enabled == nil {
		enabled := true
		rule.Enabled = &enabled
	}

	e.rules = append(e.rules, rule)
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

	if update.MaxConcurrent != nil {
		if *update.MaxConcurrent < 0 {
			return fmt.Errorf("max_concurrent must be >= 0, got %d", *update.MaxConcurrent)
		}
		e.settings.MaxConcurrent = *update.MaxConcurrent
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
	MaxConcurrent         *int              `json:"max_concurrent,omitempty"`
	MaxConcurrentPerType  *map[string]int   `json:"max_concurrent_per_type,omitempty"`
	MaxConcurrentPerLabel *map[string]int   `json:"max_concurrent_per_label,omitempty"`
}

// allRules returns a copy of all rules from the unified slice.
// Caller must hold at least a read lock.
func (e *Engine) allRules() []config.CustomRule {
	if e.rules == nil {
		return nil
	}
	result := make([]config.CustomRule, len(e.rules))
	copy(result, e.rules)
	return result
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
		// Skip disabled rules
		if rule.Enabled != nil && !*rule.Enabled {
			continue
		}

		if matches := EvaluateCondition(rule.Condition, task); matches {
			action := parseAction(rule.Action)
			switch action {
			case ActionDeny:
				reason := rule.Reason
				if reason == "" {
					reason = "Denied by rule: " + rule.Name
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

// countInFlightByRule counts in-flight tasks that match a rule.
// For now, this just counts all in-flight tasks (rules don't track matches yet).
// This is a placeholder for more sophisticated tracking.
func countInFlightByRule(inFlight map[string]bool, ruleName string) int {
	// TODO: Track which tasks match which rules for accurate limit counting
	// For now, we don't have this information, so return 0
	_ = ruleName
	return 0
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
