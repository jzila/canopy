package orchestrator

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// PromptFilter represents parsed filtering criteria from a prompt
type PromptFilter struct {
	// Task selection filters
	Priority      int      // -1 means not specified
	Type          string   // empty means not specified
	Labels        []string // empty means not specified
	StopCondition string   // empty means no stop condition

	// Stop condition parsed values
	StopAfterPriority int  // Stop after completing all tasks with this priority or higher
	StopAfterType     string // Stop after completing all tasks of this type
}

// ParsePrompt parses a natural language prompt into filtering criteria
func ParsePrompt(prompt string) (*PromptFilter, error) {
	if prompt == "" {
		return &PromptFilter{Priority: -1, StopAfterPriority: -1}, nil
	}

	filter := &PromptFilter{
		Priority:          -1,
		StopAfterPriority: -1,
	}

	lower := strings.ToLower(prompt)

	// Parse stop conditions first to avoid false matches
	hasStopCondition := strings.Contains(lower, "stop")

	typePatterns := map[string]string{
		`\btasks?\b`:        "task",
		`\bbugs?\b`:         "bug",
		`\bfeatures?\b`:     "feature",
		`\bepics?\b`:        "epic",
		`\bchores?\b`:       "chore",
		`\bmerge.?request`: "merge-request",
	}

	if hasStopCondition {
		filter.StopCondition = prompt

		// Check for priority-based stop conditions
		if match := regexp.MustCompile(`stop.*\bp([0-4])s?\b`).FindStringSubmatch(lower); match != nil {
			p, _ := strconv.Atoi(match[1])
			filter.StopAfterPriority = p
		} else if match := regexp.MustCompile(`stop.*priority\s+([0-4])`).FindStringSubmatch(lower); match != nil {
			p, _ := strconv.Atoi(match[1])
			filter.StopAfterPriority = p
		}

		// Check for type-based stop conditions
		for pattern, typeName := range typePatterns {
			stopPattern := fmt.Sprintf(`stop.*%s`, pattern)
			if regexp.MustCompile(stopPattern).MatchString(lower) {
				filter.StopAfterType = typeName
				break
			}
		}

		// For stop-only prompts (no "work on" prefix), don't set active filters
		// Check if prompt has work directives before stop
		beforeStop := strings.Split(lower, "stop")[0]
		if strings.TrimSpace(beforeStop) == "" {
			// Only stop condition, no active work filters
			return filter, nil
		}

		// Continue parsing work filters from the part before "stop"
		lower = beforeStop
	}

	// Parse priority filters (only if not a stop-only prompt)
	// Patterns: "P0", "P1", "P2", "P3", "P4", "priority 0", "priority 1", etc.
	if match := regexp.MustCompile(`\bp([0-4])\b`).FindStringSubmatch(lower); match != nil {
		p, _ := strconv.Atoi(match[1])
		filter.Priority = p
	} else if match := regexp.MustCompile(`priority\s+([0-4])`).FindStringSubmatch(lower); match != nil {
		p, _ := strconv.Atoi(match[1])
		filter.Priority = p
	}

	// Parse type filters
	// Patterns: "only tasks", "focus on bugs", "work on features", etc.
	for pattern, typeName := range typePatterns {
		if regexp.MustCompile(pattern).MatchString(lower) {
			filter.Type = typeName
			break
		}
	}

	return filter, nil
}

// ShouldStop checks if the stop condition has been met based on remaining tasks
func (f *PromptFilter) ShouldStop(remainingTasks int) bool {
	if f.StopCondition == "" {
		return false
	}

	// If we're filtering by priority and no more tasks match that priority, stop
	if f.StopAfterPriority >= 0 && remainingTasks == 0 {
		return true
	}

	// If we're filtering by type and no more tasks match that type, stop
	if f.StopAfterType != "" && remainingTasks == 0 {
		return true
	}

	return false
}

// BuildBdReadyArgs builds arguments for the "bd ready" command based on filter
func (f *PromptFilter) BuildBdReadyArgs() []string {
	args := []string{"ready", "--json"}

	if f.Priority >= 0 {
		args = append(args, "--priority", strconv.Itoa(f.Priority))
	}

	if f.Type != "" {
		args = append(args, "--type", f.Type)
	}

	for _, label := range f.Labels {
		args = append(args, "--label", label)
	}

	return args
}
