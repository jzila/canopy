// Package repairagent provides repair agent functionality for fixing validation failures.
package repairagent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jzila/canopy/pkg/validation"
)

// MaxOutputLength is the maximum length of command output to include in the prompt.
// Large outputs are truncated to fit within context window limits.
const MaxOutputLength = 8000

// MaxDiffLength is the maximum length of diff content to include in the prompt.
const MaxDiffLength = 12000

// MaxPreviousAttemptLength is the maximum length of each previous attempt summary.
const MaxPreviousAttemptLength = 2000

// RepairContext holds all context needed for a repair agent to fix validation failures.
type RepairContext struct {
	// TaskID is the original task that was being validated.
	TaskID string

	// TaskTitle is the title of the original task.
	TaskTitle string

	// ValidationResult contains the failed validation result.
	ValidationResult *validation.Result

	// FailedStep contains details of the specific step that failed.
	FailedStep *validation.StepResult

	// MergedDiff is the git diff of changes that were merged before validation.
	MergedDiff string

	// PreviousAttempts contains summaries of any previous repair attempts.
	// Each entry describes what was tried and why it didn't work.
	PreviousAttempts []string

	// RepairAttempt is the current repair attempt number (1-indexed).
	RepairAttempt int

	// MaxRepairAttempts is the maximum number of repair attempts allowed.
	MaxRepairAttempts int
}

// BuildRepairPrompt constructs the prompt for a repair agent.
// The prompt provides full context about what failed and clear instructions
// for how to fix it without breaking other functionality.
func BuildRepairPrompt(ctx *RepairContext) string {
	var parts []string

	// Header explaining the agent's role
	parts = append(parts, `## Repair Agent

**You are a repair agent. A validation check failed after a merge and you need to fix it.**

Your goal is to make the validation pass without reverting the merged changes or breaking other functionality.
`)

	// What failed section
	parts = append(parts, buildFailedSection(ctx))

	// What was merged section
	parts = append(parts, buildMergedSection(ctx))

	// Previous attempts section (if any)
	if len(ctx.PreviousAttempts) > 0 {
		parts = append(parts, buildPreviousAttemptsSection(ctx))
	}

	// Instructions section
	parts = append(parts, buildInstructionsSection(ctx))

	return strings.Join(parts, "\n")
}

// buildFailedSection creates the "What Failed" section of the prompt.
func buildFailedSection(ctx *RepairContext) string {
	var sb strings.Builder

	sb.WriteString("### What Failed\n\n")

	if ctx.FailedStep != nil {
		sb.WriteString(fmt.Sprintf("**Step:** %s\n", ctx.FailedStep.Name))
		sb.WriteString(fmt.Sprintf("**Command:** `%s`\n", ctx.FailedStep.Command))
		sb.WriteString(fmt.Sprintf("**Exit Code:** %d\n", ctx.FailedStep.ExitCode))
		sb.WriteString(fmt.Sprintf("**Duration:** %s\n", ctx.FailedStep.Duration))

		if ctx.FailedStep.Output != "" {
			truncatedOutput := truncateWithContext(ctx.FailedStep.Output, MaxOutputLength)
			sb.WriteString("\n**Output:**\n```\n")
			sb.WriteString(truncatedOutput)
			sb.WriteString("\n```\n")
		}
	} else if ctx.ValidationResult != nil {
		sb.WriteString(fmt.Sprintf("**Failed Step:** %s\n", ctx.ValidationResult.FailedStep))
		sb.WriteString(fmt.Sprintf("**Error:** %s\n", ctx.ValidationResult.Error))
	}

	sb.WriteString("\n")
	return sb.String()
}

// buildMergedSection creates the "What Was Merged" section of the prompt.
func buildMergedSection(ctx *RepairContext) string {
	var sb strings.Builder

	sb.WriteString("### What Was Merged\n\n")

	if ctx.TaskTitle != "" {
		sb.WriteString(fmt.Sprintf("**Original Task:** %s\n", ctx.TaskTitle))
	}
	if ctx.TaskID != "" {
		sb.WriteString(fmt.Sprintf("**Task ID:** %s\n", ctx.TaskID))
	}

	if ctx.MergedDiff != "" {
		truncatedDiff := truncateWithContext(ctx.MergedDiff, MaxDiffLength)
		sb.WriteString("\n**Changes that were merged:**\n```diff\n")
		sb.WriteString(truncatedDiff)
		sb.WriteString("\n```\n")

		if len(ctx.MergedDiff) > MaxDiffLength {
			sb.WriteString("\n*Note: Diff truncated. Full diff available in `.canopy/repair/merged.diff`*\n")
		}
	} else {
		sb.WriteString("\n*No diff available - check `.canopy/repair/` for context files.*\n")
	}

	sb.WriteString("\n")
	return sb.String()
}

// buildPreviousAttemptsSection creates the "Previous Attempts" section of the prompt.
func buildPreviousAttemptsSection(ctx *RepairContext) string {
	var sb strings.Builder

	sb.WriteString("### Previous Repair Attempts\n\n")
	sb.WriteString("The following approaches have already been tried and did not fully fix the issue:\n\n")

	for i, attempt := range ctx.PreviousAttempts {
		truncated := truncateWithContext(attempt, MaxPreviousAttemptLength)
		sb.WriteString(fmt.Sprintf("**Attempt %d:**\n%s\n\n", i+1, truncated))
	}

	sb.WriteString("**Do NOT repeat these approaches.** Try a different solution.\n\n")
	return sb.String()
}

// buildInstructionsSection creates the instructions section of the prompt.
func buildInstructionsSection(ctx *RepairContext) string {
	var sb strings.Builder

	sb.WriteString("### Your Task\n\n")
	sb.WriteString("Fix the validation failure so that the validation step passes.\n\n")

	sb.WriteString("**DO NOT:**\n")
	sb.WriteString("- Revert the merged changes (the feature must remain)\n")
	sb.WriteString("- Break other tests or functionality\n")
	sb.WriteString("- Make unrelated changes\n")
	sb.WriteString("- Disable or skip the failing test/check\n")
	if len(ctx.PreviousAttempts) > 0 {
		sb.WriteString("- Repeat approaches from previous attempts\n")
	}
	sb.WriteString("\n")

	sb.WriteString("**DO:**\n")
	sb.WriteString("- Read the error output carefully to understand what failed\n")
	sb.WriteString("- Look at the merged changes to understand what was introduced\n")
	sb.WriteString("- Make minimal, targeted fixes\n")
	sb.WriteString("- Run the validation command locally to verify your fix\n")
	sb.WriteString("\n")

	// Show attempt context
	if ctx.RepairAttempt > 0 && ctx.MaxRepairAttempts > 0 {
		sb.WriteString(fmt.Sprintf("**Attempt:** %d of %d\n\n", ctx.RepairAttempt, ctx.MaxRepairAttempts))
	}

	sb.WriteString("### Context Files\n\n")
	sb.WriteString("Additional context is available in `.canopy/repair/`:\n")
	sb.WriteString("- `merged.diff` - Full diff of merged changes\n")
	sb.WriteString("- `validation-output.txt` - Complete validation output\n")
	sb.WriteString("- `failed-step.txt` - Details of the failed step\n")
	if len(ctx.PreviousAttempts) > 0 {
		sb.WriteString("- `previous-attempts/` - Full details of previous attempts\n")
	}
	sb.WriteString("\n")

	sb.WriteString("### Success Criteria\n\n")
	if ctx.FailedStep != nil {
		sb.WriteString(fmt.Sprintf("Your fix is complete when `%s` exits with code 0.\n\n", ctx.FailedStep.Command))
	} else {
		sb.WriteString("Your fix is complete when all validation steps pass.\n\n")
	}

	sb.WriteString("Commit your fix with message: `fix: repair validation failure")
	if ctx.FailedStep != nil {
		sb.WriteString(fmt.Sprintf(" in %s", ctx.FailedStep.Name))
	}
	sb.WriteString("`\n")

	return sb.String()
}

// truncateWithContext truncates a string intelligently, keeping both the
// beginning and end of the content to preserve context.
func truncateWithContext(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}

	// Keep 60% from the beginning, 40% from the end
	// This preserves the start of output (often headers/context)
	// and the end (often the actual error)
	headLen := (maxLen * 6) / 10
	tailLen := maxLen - headLen - 50 // Leave room for truncation message

	head := s[:headLen]
	tail := s[len(s)-tailLen:]

	// Find newline boundaries to avoid cutting mid-line
	if idx := strings.LastIndex(head, "\n"); idx > headLen/2 {
		head = head[:idx]
	}
	if idx := strings.Index(tail, "\n"); idx >= 0 && idx < tailLen/2 {
		tail = tail[idx+1:]
	}

	return head + "\n\n... [truncated " + fmt.Sprintf("%d", len(s)-len(head)-len(tail)) + " characters] ...\n\n" + tail
}

// WriteRepairContext writes repair context files to the sandbox directory.
// These files provide the repair agent with full context that may be truncated
// in the prompt itself.
func WriteRepairContext(sandboxDir string, ctx *RepairContext) error {
	repairDir := filepath.Join(sandboxDir, ".canopy", "repair")
	if err := os.MkdirAll(repairDir, 0755); err != nil {
		return fmt.Errorf("failed to create repair directory: %w", err)
	}

	// Write the full merged diff
	if ctx.MergedDiff != "" {
		if err := os.WriteFile(filepath.Join(repairDir, "merged.diff"), []byte(ctx.MergedDiff), 0644); err != nil {
			return fmt.Errorf("failed to write merged diff: %w", err)
		}
	}

	// Write full validation output
	if ctx.FailedStep != nil && ctx.FailedStep.Output != "" {
		if err := os.WriteFile(filepath.Join(repairDir, "validation-output.txt"), []byte(ctx.FailedStep.Output), 0644); err != nil {
			return fmt.Errorf("failed to write validation output: %w", err)
		}
	}

	// Write failed step details
	if ctx.FailedStep != nil {
		details := fmt.Sprintf("Step: %s\nCommand: %s\nExit Code: %d\nDuration: %s\n\nOutput:\n%s",
			ctx.FailedStep.Name,
			ctx.FailedStep.Command,
			ctx.FailedStep.ExitCode,
			ctx.FailedStep.Duration,
			ctx.FailedStep.Output,
		)
		if err := os.WriteFile(filepath.Join(repairDir, "failed-step.txt"), []byte(details), 0644); err != nil {
			return fmt.Errorf("failed to write failed step details: %w", err)
		}
	}

	// Write previous attempts
	if len(ctx.PreviousAttempts) > 0 {
		attemptsDir := filepath.Join(repairDir, "previous-attempts")
		if err := os.MkdirAll(attemptsDir, 0755); err != nil {
			return fmt.Errorf("failed to create previous attempts directory: %w", err)
		}

		for i, attempt := range ctx.PreviousAttempts {
			filename := filepath.Join(attemptsDir, fmt.Sprintf("attempt-%d.txt", i+1))
			if err := os.WriteFile(filename, []byte(attempt), 0644); err != nil {
				return fmt.Errorf("failed to write attempt %d: %w", i+1, err)
			}
		}
	}

	// Write task context
	taskContext := fmt.Sprintf("Task ID: %s\nTask Title: %s\nRepair Attempt: %d of %d\n",
		ctx.TaskID, ctx.TaskTitle, ctx.RepairAttempt, ctx.MaxRepairAttempts)
	if err := os.WriteFile(filepath.Join(repairDir, "task-context.txt"), []byte(taskContext), 0644); err != nil {
		return fmt.Errorf("failed to write task context: %w", err)
	}

	return nil
}
