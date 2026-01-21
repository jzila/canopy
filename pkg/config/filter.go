// Package config provides general configuration for canopy.
package config

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/jzila/canopy/pkg/beads"
)

// TaskFilter applies rules settings to filter tasks.
type TaskFilter struct {
	rules *RulesSettings
}

// NewTaskFilter creates a new task filter from rules settings.
func NewTaskFilter(rules *RulesSettings) *TaskFilter {
	return &TaskFilter{rules: rules}
}

// FilterTasks applies all configured rules to filter tasks.
// Returns a slice of tasks that pass all filters.
func (f *TaskFilter) FilterTasks(tasks []beads.Task) []beads.Task {
	if f.rules == nil {
		return tasks
	}

	result := make([]beads.Task, 0, len(tasks))
	for _, task := range tasks {
		if f.ShouldInclude(&task) {
			result = append(result, task)
		}
	}
	return result
}

// ShouldInclude returns true if a task passes all configured filters.
func (f *TaskFilter) ShouldInclude(task *beads.Task) bool {
	if f.rules == nil {
		return true
	}

	// Priority filter
	if !f.matchesPriority(task) {
		return false
	}

	// Type filter
	if !f.matchesType(task) {
		return false
	}

	// Label filter
	if !f.matchesLabels(task) {
		return false
	}

	// Assignee filter
	if !f.matchesAssignee(task) {
		return false
	}

	// Custom rules
	if !f.matchesCustomRules(task) {
		return false
	}

	return true
}

// matchesPriority checks if task priority is within configured range.
func (f *TaskFilter) matchesPriority(task *beads.Task) bool {
	// Check minimum priority
	if task.Priority < f.rules.PriorityMin {
		return false
	}

	// Check maximum priority (-1 = no filter)
	if f.rules.PriorityMax >= 0 && task.Priority > f.rules.PriorityMax {
		return false
	}

	return true
}

// matchesType checks if task type passes type filters.
func (f *TaskFilter) matchesType(task *beads.Task) bool {
	// If types whitelist is set, task must match one
	if len(f.rules.Types) > 0 {
		found := false
		for _, t := range f.rules.Types {
			if task.Type == t {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// If exclude_types is set, task must not match any
	for _, t := range f.rules.ExcludeTypes {
		if task.Type == t {
			return false
		}
	}

	return true
}

// matchesLabels checks if task labels pass label filters.
func (f *TaskFilter) matchesLabels(task *beads.Task) bool {
	// If labels whitelist is set, task must have at least one matching label
	if len(f.rules.Labels) > 0 {
		found := false
		for _, reqLabel := range f.rules.Labels {
			for _, taskLabel := range task.Labels {
				if taskLabel == reqLabel {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			return false
		}
	}

	// If exclude_labels is set, task must not have any excluded labels
	for _, excludeLabel := range f.rules.ExcludeLabels {
		for _, taskLabel := range task.Labels {
			if taskLabel == excludeLabel {
				return false
			}
		}
	}

	return true
}

// matchesAssignee checks if task assignee passes assignee filter.
func (f *TaskFilter) matchesAssignee(task *beads.Task) bool {
	switch f.rules.Assignee {
	case "":
		// Empty string = unassigned only
		return task.Assignee == ""
	case "*":
		// Asterisk = any assignee (including unassigned)
		return true
	default:
		// Specific value = exact match
		return task.Assignee == f.rules.Assignee
	}
}

// matchesCustomRules evaluates custom rules against the task.
func (f *TaskFilter) matchesCustomRules(task *beads.Task) bool {
	for _, rule := range f.rules.Custom {
		// Skip disabled rules
		if rule.Enabled != nil && !*rule.Enabled {
			continue
		}

		matches := evaluateCondition(rule.Condition, task)
		if matches {
			switch rule.Action {
			case "skip":
				return false
			case "include":
				// Continue checking other rules
				continue
			}
		}
	}
	return true
}

// evaluateCondition evaluates a simple condition expression against a task.
// Supported syntax:
//   - "priority > 1", "priority <= 2", "priority == 0"
//   - "type == bug", "type != feature"
//   - "assignee == john", "assignee != "
func evaluateCondition(condition string, task *beads.Task) bool {
	condition = strings.TrimSpace(condition)

	// Parse condition pattern: field operator value
	// Supports: ==, !=, <, >, <=, >=
	re := regexp.MustCompile(`^(\w+)\s*(==|!=|<=|>=|<|>)\s*(.+)$`)
	matches := re.FindStringSubmatch(condition)
	if len(matches) != 4 {
		// Invalid condition format
		return false
	}

	field := matches[1]
	op := matches[2]
	value := strings.Trim(matches[3], `"'`)

	switch field {
	case "priority":
		targetPriority, err := strconv.Atoi(value)
		if err != nil {
			return false
		}
		return compareInt(task.Priority, op, targetPriority)

	case "type":
		return compareString(task.Type, op, value)

	case "assignee":
		return compareString(task.Assignee, op, value)

	default:
		// Unknown field
		return false
	}
}

// compareInt compares two integers using the given operator.
func compareInt(a int, op string, b int) bool {
	switch op {
	case "==":
		return a == b
	case "!=":
		return a != b
	case "<":
		return a < b
	case ">":
		return a > b
	case "<=":
		return a <= b
	case ">=":
		return a >= b
	default:
		return false
	}
}

// compareString compares two strings using the given operator.
func compareString(a, op, b string) bool {
	switch op {
	case "==":
		return a == b
	case "!=":
		return a != b
	default:
		// String comparison only supports == and !=
		return false
	}
}

// GetSkipReason returns the reason a task was skipped by custom rules, if any.
// Returns empty string if task would not be skipped.
func (f *TaskFilter) GetSkipReason(task *beads.Task) string {
	if f.rules == nil {
		return ""
	}

	for _, rule := range f.rules.Custom {
		// Skip disabled rules
		if rule.Enabled != nil && !*rule.Enabled {
			continue
		}

		if rule.Action == "skip" && evaluateCondition(rule.Condition, task) {
			if rule.Reason != "" {
				return rule.Reason
			}
			return "Skipped by rule: " + rule.Name
		}
	}
	return ""
}
