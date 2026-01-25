package rules

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/jzila/canopy/pkg/beads"
)

// Input length limits for defense-in-depth against resource exhaustion.
const (
	// MaxRuleNameLen is the maximum allowed length for rule names.
	MaxRuleNameLen = 128
	// MaxConditionLen is the maximum allowed length for condition expressions.
	MaxConditionLen = 1024
	// MaxReasonLen is the maximum allowed length for skip/deny reasons.
	MaxReasonLen = 512
)

// Precompiled regex patterns for validation (avoids recompilation per call).
// Patterns use alternation to require matching quotes (either 'x' or "x", not 'x").
var (
	// inLabelsPatternSingle matches 'label' in labels (single quotes)
	inLabelsPatternSingle = regexp.MustCompile(`^'([^']+)'\s+in\s+labels$`)
	// inLabelsPatternDouble matches "label" in labels (double quotes)
	inLabelsPatternDouble = regexp.MustCompile(`^"([^"]+)"\s+in\s+labels$`)
	// notInLabelsPatternSingle matches 'label' not in labels (single quotes)
	notInLabelsPatternSingle = regexp.MustCompile(`^'([^']+)'\s+not\s+in\s+labels$`)
	// notInLabelsPatternDouble matches "label" not in labels (double quotes)
	notInLabelsPatternDouble = regexp.MustCompile(`^"([^"]+)"\s+not\s+in\s+labels$`)
	// containsPatternSingle matches "field contains 'value'" (single quotes)
	containsPatternSingle = regexp.MustCompile(`^(\w+)\s+contains\s+'([^']+)'$`)
	// containsPatternDouble matches "field contains \"value\"" (double quotes)
	containsPatternDouble = regexp.MustCompile(`^(\w+)\s+contains\s+"([^"]+)"$`)
	// comparisonPattern matches "field op value" (e.g., "priority > 1")
	comparisonPattern = regexp.MustCompile(`^(\w+)\s*(==|!=|<=|>=|<|>)\s*(.+)$`)
)

// validComparisonFields is the allowlist of fields that can be used in comparison conditions.
var validComparisonFields = map[string]bool{
	"priority": true,
	"type":     true,
	"assignee": true,
	"status":   true,
	"id":       true,
}

// validContainsFields is the allowlist of fields that can be used in "contains" conditions.
var validContainsFields = map[string]bool{
	"title":       true,
	"description": true,
	"id":          true,
}

// ValidateConditionSyntax validates a condition expression without evaluating it.
// Returns nil if the syntax is valid, or an error describing the problem.
// This should be called before persisting rules to catch errors early.
func ValidateConditionSyntax(condition string) error {
	condition = strings.TrimSpace(condition)
	if condition == "" {
		return fmt.Errorf("condition cannot be empty")
	}
	if len(condition) > MaxConditionLen {
		return fmt.Errorf("condition exceeds maximum length of %d characters", MaxConditionLen)
	}

	return validateConditionRecursive(condition)
}

// validateConditionRecursive validates a single condition or compound condition.
func validateConditionRecursive(condition string) error {
	condition = strings.TrimSpace(condition)
	if condition == "" {
		return fmt.Errorf("empty condition part")
	}

	// Handle "or" (lower precedence than "and")
	orParts := splitOnOperator(condition, " or ")
	if len(orParts) > 1 {
		for _, part := range orParts {
			if err := validateConditionRecursive(part); err != nil {
				return err
			}
		}
		return nil
	}

	// Handle "and" (higher precedence)
	andParts := splitOnOperator(condition, " and ")
	if len(andParts) > 1 {
		for _, part := range andParts {
			if err := validateConditionRecursive(part); err != nil {
				return err
			}
		}
		return nil
	}

	// Validate single condition
	return validateSingleCondition(condition)
}

// validateSingleCondition validates a single (non-compound) condition syntax.
func validateSingleCondition(condition string) error {
	condition = strings.TrimSpace(condition)

	// Check "in labels" pattern (both single and double quote variants)
	if inLabelsPatternSingle.MatchString(condition) || inLabelsPatternDouble.MatchString(condition) {
		return nil
	}

	// Check "not in labels" pattern (both single and double quote variants)
	if notInLabelsPatternSingle.MatchString(condition) || notInLabelsPatternDouble.MatchString(condition) {
		return nil
	}

	// Check "contains" pattern (both single and double quote variants)
	var containsMatches []string
	if matches := containsPatternSingle.FindStringSubmatch(condition); len(matches) == 3 {
		containsMatches = matches
	} else if matches := containsPatternDouble.FindStringSubmatch(condition); len(matches) == 3 {
		containsMatches = matches
	}
	if len(containsMatches) == 3 {
		field := containsMatches[1]
		if !validContainsFields[field] {
			return fmt.Errorf("unknown field %q in contains condition (allowed: title, description, id)", field)
		}
		return nil
	}

	// Check comparison pattern
	if matches := comparisonPattern.FindStringSubmatch(condition); len(matches) == 4 {
		field := matches[1]
		op := matches[2]
		value := strings.Trim(matches[3], `"'`)

		if !validComparisonFields[field] {
			return fmt.Errorf("unknown field %q in comparison (allowed: priority, type, assignee, status, id)", field)
		}

		// For priority, validate that the value is a valid integer
		if field == "priority" {
			if _, err := strconv.Atoi(value); err != nil {
				return fmt.Errorf("priority value %q is not a valid integer", value)
			}
		}

		// For string fields, only == and != are valid
		if field != "priority" && op != "==" && op != "!=" {
			return fmt.Errorf("operator %q not valid for field %q (only == and != allowed)", op, field)
		}

		return nil
	}

	return fmt.Errorf("invalid condition syntax: %q", condition)
}

// ValidateRuleName validates that a rule name is within length limits.
func ValidateRuleName(name string) error {
	if name == "" {
		return fmt.Errorf("rule name cannot be empty")
	}
	if len(name) > MaxRuleNameLen {
		return fmt.Errorf("rule name exceeds maximum length of %d characters", MaxRuleNameLen)
	}
	return nil
}

// ValidateReason validates that a reason string is within length limits.
func ValidateReason(reason string) error {
	if len(reason) > MaxReasonLen {
		return fmt.Errorf("reason exceeds maximum length of %d characters", MaxReasonLen)
	}
	return nil
}

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
	// Uses precompiled patterns to avoid recompilation and ensure matching quotes
	if matches := inLabelsPatternSingle.FindStringSubmatch(condition); len(matches) == 2 {
		return containsLabel(task.Labels, matches[1]), true
	}
	if matches := inLabelsPatternDouble.FindStringSubmatch(condition); len(matches) == 2 {
		return containsLabel(task.Labels, matches[1]), true
	}

	// Pattern: 'value' not in labels or "value" not in labels
	if matches := notInLabelsPatternSingle.FindStringSubmatch(condition); len(matches) == 2 {
		return !containsLabel(task.Labels, matches[1]), true
	}
	if matches := notInLabelsPatternDouble.FindStringSubmatch(condition); len(matches) == 2 {
		return !containsLabel(task.Labels, matches[1]), true
	}

	return false, false
}

// evaluateContains handles "field contains 'value'" conditions.
// Returns (result, ok) where ok indicates if the pattern matched.
func evaluateContains(condition string, task *beads.Task) (bool, bool) {
	// Pattern: field contains 'value' or field contains "value"
	// Uses precompiled patterns to avoid recompilation and ensure matching quotes
	var matches []string
	if m := containsPatternSingle.FindStringSubmatch(condition); len(m) == 3 {
		matches = m
	} else if m := containsPatternDouble.FindStringSubmatch(condition); len(m) == 3 {
		matches = m
	}
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
	// Uses precompiled comparisonPattern to avoid recompilation
	matches := comparisonPattern.FindStringSubmatch(condition)
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
