// Package rules provides a rule evaluation engine for task selection.
package rules

import (
	"fmt"
	"sync"

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
