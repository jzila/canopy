package rules

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/jzila/canopy/pkg/beads"
)

// EvaluateCondition evaluates a condition expression against a task.
// Supported syntax:
//   - "priority > 1", "priority <= 2", "priority == 0"
//   - "type == bug", "type != feature"
//   - "assignee == john", "assignee != \"\""
//   - "'frontend' in labels", "'urgent' not in labels"
//   - "'frontend-*' in labels" (wildcard matching with *)
//   - "title contains 'fix'"
//   - Conditions can be combined with "and" / "or"
//
// Examples:
//   - "priority <= 1 and type == bug"
//   - "type == bug or type == feature"
//   - "'urgent' in labels and priority <= 1"
//   - "'team-*' in labels" (matches team-frontend, team-backend, etc.)
func EvaluateCondition(condition string, task *beads.Task) bool {
	condition = strings.TrimSpace(condition)
	if condition == "" {
		return false
	}

	// Handle "or" (lower precedence than "and")
	orParts := splitOnOperator(condition, " or ")
	if len(orParts) > 1 {
		for _, part := range orParts {
			if EvaluateCondition(part, task) {
				return true
			}
		}
		return false
	}

	// Handle "and" (higher precedence)
	andParts := splitOnOperator(condition, " and ")
	if len(andParts) > 1 {
		for _, part := range andParts {
			if !EvaluateCondition(part, task) {
				return false
			}
		}
		return true
	}

	// Single condition evaluation
	return evaluateSingleCondition(condition, task)
}

// splitOnOperator splits a condition on an operator, respecting quotes.
// Returns the original condition as a single element if no split occurred.
func splitOnOperator(condition, op string) []string {
	// Simple split for now - doesn't handle quoted strings containing the operator
	// This is sufficient for most use cases
	lowerCondition := strings.ToLower(condition)
	lowerOp := strings.ToLower(op)

	idx := strings.Index(lowerCondition, lowerOp)
	if idx == -1 {
		return []string{condition}
	}

	// Split at the operator
	parts := []string{
		strings.TrimSpace(condition[:idx]),
		strings.TrimSpace(condition[idx+len(op):]),
	}

	// If there are more operators, recursively split the rest
	rest := splitOnOperator(parts[1], op)
	result := make([]string, 0, 1+len(rest))
	result = append(result, parts[0])
	result = append(result, rest...)
	return result
}

// evaluateSingleCondition evaluates a single (non-compound) condition.
func evaluateSingleCondition(condition string, task *beads.Task) bool {
	condition = strings.TrimSpace(condition)

	// Handle "in labels" / "not in labels" patterns
	if inLabelsResult, ok := evaluateInLabels(condition, task); ok {
		return inLabelsResult
	}

	// Handle "contains" pattern
	if containsResult, ok := evaluateContains(condition, task); ok {
		return containsResult
	}

	// Handle standard comparison pattern: field operator value
	return evaluateComparison(condition, task)
}

// evaluateInLabels handles "'label' in labels" and "'label' not in labels" conditions.
// Returns (result, ok) where ok indicates if the pattern matched.
func evaluateInLabels(condition string, task *beads.Task) (bool, bool) {
	// Pattern: 'value' in labels or "value" in labels
	inLabelsRe := regexp.MustCompile(`^['"](.+)['"]\s+in\s+labels$`)
	if matches := inLabelsRe.FindStringSubmatch(condition); len(matches) == 2 {
		label := matches[1]
		return containsLabel(task.Labels, label), true
	}

	// Pattern: 'value' not in labels or "value" not in labels
	notInLabelsRe := regexp.MustCompile(`^['"](.+)['"]\s+not\s+in\s+labels$`)
	if matches := notInLabelsRe.FindStringSubmatch(condition); len(matches) == 2 {
		label := matches[1]
		return !containsLabel(task.Labels, label), true
	}

	return false, false
}

// evaluateContains handles "field contains 'value'" conditions.
// Returns (result, ok) where ok indicates if the pattern matched.
func evaluateContains(condition string, task *beads.Task) (bool, bool) {
	// Pattern: field contains 'value' or field contains "value"
	containsRe := regexp.MustCompile(`^(\w+)\s+contains\s+['"](.+)['"]$`)
	matches := containsRe.FindStringSubmatch(condition)
	if len(matches) != 3 {
		return false, false
	}

	field := matches[1]
	value := matches[2]

	switch field {
	case "title":
		return strings.Contains(strings.ToLower(task.Title), strings.ToLower(value)), true
	case "description":
		return strings.Contains(strings.ToLower(task.Description), strings.ToLower(value)), true
	case "id":
		return strings.Contains(strings.ToLower(task.ID), strings.ToLower(value)), true
	default:
		return false, true // Pattern matched but unknown field
	}
}

// evaluateComparison handles standard comparison conditions: field operator value
func evaluateComparison(condition string, task *beads.Task) bool {
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

	case "status":
		return compareString(task.Status, op, value)

	case "id":
		return compareString(task.ID, op, value)

	default:
		// Unknown field
		return false
	}
}

// containsLabel checks if labels contains a specific label.
// Supports wildcard patterns using * and ? (e.g., "frontend-*", "*-deprecated", "team-*").
func containsLabel(labels []string, pattern string) bool {
	// Fast path: no wildcards, use exact matching
	if !strings.ContainsAny(pattern, "*?[") {
		for _, l := range labels {
			if l == pattern {
				return true
			}
		}
		return false
	}

	// Wildcard path: use glob matching
	for _, l := range labels {
		matched, err := filepath.Match(pattern, l)
		if err != nil {
			// Invalid pattern, treat as no match
			return false
		}
		if matched {
			return true
		}
	}
	return false
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
