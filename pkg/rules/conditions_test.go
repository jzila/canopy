package rules

import (
	"testing"

	"github.com/jzila/canopy/pkg/beads"
)

func TestEvaluateConditionBasic(t *testing.T) {
	task := &beads.Task{
		ID:       "test-123",
		Priority: 2,
		Type:     "bug",
		Assignee: "alice",
		Status:   "open",
		Labels:   []string{"frontend", "urgent"},
		Title:    "Fix login button",
	}

	tests := []struct {
		condition string
		expected  bool
	}{
		// Priority conditions
		{"priority == 2", true},
		{"priority != 2", false},
		{"priority < 3", true},
		{"priority > 1", true},
		{"priority <= 2", true},
		{"priority >= 2", true},
		{"priority < 2", false},
		{"priority > 2", false},

		// Type conditions
		{"type == bug", true},
		{"type != bug", false},
		{"type == feature", false},
		{"type != feature", true},

		// Assignee conditions
		{"assignee == alice", true},
		{"assignee != alice", false},
		{"assignee == bob", false},

		// Status conditions
		{"status == open", true},
		{"status != open", false},
		{"status == closed", false},

		// ID conditions
		{"id == test-123", true},
		{"id != test-123", false},

		// Invalid conditions
		{"invalid condition", false},
		{"unknown_field == value", false},
		{"priority == invalid", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.condition, func(t *testing.T) {
			result := EvaluateCondition(tt.condition, task)
			if result != tt.expected {
				t.Errorf("EvaluateCondition(%q) = %v, want %v", tt.condition, result, tt.expected)
			}
		})
	}
}

func TestEvaluateConditionLabels(t *testing.T) {
	task := &beads.Task{
		ID:     "1",
		Labels: []string{"frontend", "urgent"},
	}

	tests := []struct {
		condition string
		expected  bool
	}{
		// In labels
		{"'frontend' in labels", true},
		{"'urgent' in labels", true},
		{"'backend' in labels", false},
		{`"frontend" in labels`, true},
		{`"backend" in labels`, false},

		// Not in labels
		{"'backend' not in labels", true},
		{"'frontend' not in labels", false},
		{`"wip" not in labels`, true},
		{`"urgent" not in labels`, false},
	}

	for _, tt := range tests {
		t.Run(tt.condition, func(t *testing.T) {
			result := EvaluateCondition(tt.condition, task)
			if result != tt.expected {
				t.Errorf("EvaluateCondition(%q) = %v, want %v", tt.condition, result, tt.expected)
			}
		})
	}
}

func TestEvaluateConditionContains(t *testing.T) {
	task := &beads.Task{
		ID:          "beads-abc123",
		Title:       "Fix login button styling",
		Description: "The button is not responsive on mobile devices",
	}

	tests := []struct {
		condition string
		expected  bool
	}{
		// Title contains
		{"title contains 'login'", true},
		{"title contains 'Login'", true}, // case insensitive
		{"title contains 'button'", true},
		{"title contains 'logout'", false},

		// Description contains
		{"description contains 'mobile'", true},
		{"description contains 'responsive'", true},
		{"description contains 'desktop'", false},

		// ID contains
		{"id contains 'beads'", true},
		{"id contains 'abc'", true},
		{"id contains 'xyz'", false},
	}

	for _, tt := range tests {
		t.Run(tt.condition, func(t *testing.T) {
			result := EvaluateCondition(tt.condition, task)
			if result != tt.expected {
				t.Errorf("EvaluateCondition(%q) = %v, want %v", tt.condition, result, tt.expected)
			}
		})
	}
}

func TestEvaluateConditionAndCombinator(t *testing.T) {
	task := &beads.Task{
		ID:       "1",
		Priority: 1,
		Type:     "bug",
		Labels:   []string{"frontend", "urgent"},
	}

	tests := []struct {
		condition string
		expected  bool
	}{
		// Both true
		{"priority <= 1 and type == bug", true},
		{"type == bug and priority < 3", true},

		// First false
		{"priority > 2 and type == bug", false},

		// Second false
		{"priority <= 1 and type == feature", false},

		// Both false
		{"priority > 2 and type == feature", false},

		// Multiple ands
		{"priority <= 1 and type == bug and 'frontend' in labels", true},
		{"priority <= 1 and type == bug and 'backend' in labels", false},
	}

	for _, tt := range tests {
		t.Run(tt.condition, func(t *testing.T) {
			result := EvaluateCondition(tt.condition, task)
			if result != tt.expected {
				t.Errorf("EvaluateCondition(%q) = %v, want %v", tt.condition, result, tt.expected)
			}
		})
	}
}

func TestEvaluateConditionOrCombinator(t *testing.T) {
	task := &beads.Task{
		ID:       "1",
		Priority: 2,
		Type:     "bug",
		Labels:   []string{"frontend"},
	}

	tests := []struct {
		condition string
		expected  bool
	}{
		// First true
		{"type == bug or type == feature", true},

		// Second true
		{"type == feature or type == bug", true},

		// Both true
		{"type == bug or priority == 2", true},

		// Both false
		{"type == feature or type == chore", false},

		// Multiple ors
		{"type == feature or type == chore or type == bug", true},
		{"type == feature or type == chore or type == epic", false},
	}

	for _, tt := range tests {
		t.Run(tt.condition, func(t *testing.T) {
			result := EvaluateCondition(tt.condition, task)
			if result != tt.expected {
				t.Errorf("EvaluateCondition(%q) = %v, want %v", tt.condition, result, tt.expected)
			}
		})
	}
}

func TestEvaluateConditionMixedCombinators(t *testing.T) {
	task := &beads.Task{
		ID:       "1",
		Priority: 1,
		Type:     "bug",
		Labels:   []string{"frontend", "urgent"},
	}

	tests := []struct {
		condition string
		expected  bool
	}{
		// Or has lower precedence than and
		// "A or B and C" should be "A or (B and C)"
		{"type == feature or type == bug and priority <= 1", true},
		{"type == feature or type == bug and priority > 5", false},

		// More complex
		{"type == bug and priority <= 1 or type == feature and priority == 0", true},
		{"type == feature and priority == 0 or type == bug and priority <= 1", true},
	}

	for _, tt := range tests {
		t.Run(tt.condition, func(t *testing.T) {
			result := EvaluateCondition(tt.condition, task)
			if result != tt.expected {
				t.Errorf("EvaluateCondition(%q) = %v, want %v", tt.condition, result, tt.expected)
			}
		})
	}
}

func TestEvaluateConditionEmptyLabels(t *testing.T) {
	task := &beads.Task{
		ID:     "1",
		Labels: nil,
	}

	tests := []struct {
		condition string
		expected  bool
	}{
		{"'frontend' in labels", false},
		{"'frontend' not in labels", true},
	}

	for _, tt := range tests {
		t.Run(tt.condition, func(t *testing.T) {
			result := EvaluateCondition(tt.condition, task)
			if result != tt.expected {
				t.Errorf("EvaluateCondition(%q) = %v, want %v", tt.condition, result, tt.expected)
			}
		})
	}
}

func TestEvaluateConditionLabelsWildcard(t *testing.T) {
	task := &beads.Task{
		ID:     "1",
		Labels: []string{"frontend-auth", "frontend-ui", "team-platform", "api-deprecated", "urgent"},
	}

	tests := []struct {
		condition string
		expected  bool
	}{
		// Prefix wildcard: frontend-*
		{"'frontend-*' in labels", true},
		{"'frontend-auth' in labels", true}, // exact match still works
		{"'frontend-api' in labels", false}, // exact match, not present

		// Suffix wildcard: *-deprecated
		{"'*-deprecated' in labels", true},
		{"'*-auth' in labels", true},
		{"'*-missing' in labels", false},

		// Prefix wildcard: team-*
		{"'team-*' in labels", true},
		{"'backend-*' in labels", false},

		// Not in labels with wildcards
		{"'frontend-*' not in labels", false}, // frontend-* matches, so NOT returns false
		{"'backend-*' not in labels", true},   // backend-* doesn't match, so NOT returns true

		// Multiple wildcards
		{"'*-*' in labels", true}, // matches frontend-auth, frontend-ui, team-platform, api-deprecated

		// Single character wildcard (?)
		{"'team-????????' in labels", true}, // team-platform is 8 chars after team-
		{"'team-???' in labels", false},     // no 3-char suffix

		// Exact match (no wildcard) still uses fast path
		{"'urgent' in labels", true},
		{"'missing' in labels", false},
	}

	for _, tt := range tests {
		t.Run(tt.condition, func(t *testing.T) {
			result := EvaluateCondition(tt.condition, task)
			if result != tt.expected {
				t.Errorf("EvaluateCondition(%q) = %v, want %v", tt.condition, result, tt.expected)
			}
		})
	}
}

func TestContainsLabelWildcard(t *testing.T) {
	labels := []string{"frontend-auth", "frontend-ui", "team-platform", "api-deprecated"}

	tests := []struct {
		pattern  string
		expected bool
	}{
		// Exact matches
		{"frontend-auth", true},
		{"frontend-api", false},

		// Prefix wildcards
		{"frontend-*", true},
		{"backend-*", false},

		// Suffix wildcards
		{"*-deprecated", true},
		{"*-enabled", false},

		// Middle wildcards
		{"front*-auth", true},
		{"*-plat*", true},

		// Multiple wildcards
		{"*-*", true},

		// Invalid pattern (unmatched bracket) returns false
		{"[invalid", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			result := containsLabel(labels, tt.pattern)
			if result != tt.expected {
				t.Errorf("containsLabel(%v, %q) = %v, want %v", labels, tt.pattern, result, tt.expected)
			}
		})
	}
}

func TestSplitOnOperator(t *testing.T) {
	tests := []struct {
		condition string
		operator  string
		expected  []string
	}{
		{"a and b", " and ", []string{"a", "b"}},
		{"a and b and c", " and ", []string{"a", "b", "c"}},
		{"a or b", " or ", []string{"a", "b"}},
		{"a", " and ", []string{"a"}},
		{"a AND b", " and ", []string{"a", "b"}}, // case insensitive
	}

	for _, tt := range tests {
		t.Run(tt.condition, func(t *testing.T) {
			result := splitOnOperator(tt.condition, tt.operator)
			if len(result) != len(tt.expected) {
				t.Errorf("splitOnOperator(%q, %q) returned %d parts, want %d", tt.condition, tt.operator, len(result), len(tt.expected))
				return
			}
			for i, part := range result {
				if part != tt.expected[i] {
					t.Errorf("splitOnOperator(%q, %q)[%d] = %q, want %q", tt.condition, tt.operator, i, part, tt.expected[i])
				}
			}
		})
	}
}

func TestCompareInt(t *testing.T) {
	tests := []struct {
		a        int
		op       string
		b        int
		expected bool
	}{
		{5, "==", 5, true},
		{5, "==", 3, false},
		{5, "!=", 3, true},
		{5, "!=", 5, false},
		{5, "<", 6, true},
		{5, "<", 5, false},
		{5, ">", 4, true},
		{5, ">", 5, false},
		{5, "<=", 5, true},
		{5, "<=", 4, false},
		{5, ">=", 5, true},
		{5, ">=", 6, false},
		{5, "invalid", 5, false},
	}

	for _, tt := range tests {
		result := compareInt(tt.a, tt.op, tt.b)
		if result != tt.expected {
			t.Errorf("compareInt(%d, %q, %d) = %v, want %v", tt.a, tt.op, tt.b, result, tt.expected)
		}
	}
}

func TestCompareString(t *testing.T) {
	tests := []struct {
		a        string
		op       string
		b        string
		expected bool
	}{
		{"hello", "==", "hello", true},
		{"hello", "==", "world", false},
		{"hello", "!=", "world", true},
		{"hello", "!=", "hello", false},
		{"hello", "<", "world", false},  // String comparison not supported
		{"hello", ">", "world", false},  // String comparison not supported
	}

	for _, tt := range tests {
		result := compareString(tt.a, tt.op, tt.b)
		if result != tt.expected {
			t.Errorf("compareString(%q, %q, %q) = %v, want %v", tt.a, tt.op, tt.b, result, tt.expected)
		}
	}
}
