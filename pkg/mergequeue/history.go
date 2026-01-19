package mergequeue

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jzila/canopy/pkg/beads"
	"github.com/jzila/canopy/pkg/validation"
)

// HistoryRecorder records validation and repair history on task beads.
// It provides methods to record validation failures, repair attempts,
// and final status updates, preserving a complete audit trail.
type HistoryRecorder struct {
	beadsClient beads.BeadsClient
	verbose     bool
}

// NewHistoryRecorder creates a new history recorder.
func NewHistoryRecorder(beadsClient beads.BeadsClient, verbose bool) *HistoryRecorder {
	return &HistoryRecorder{
		beadsClient: beadsClient,
		verbose:     verbose,
	}
}

// RecordValidationFailure records a validation failure on the task bead.
// It includes the failed step name, attempt number, and relevant output.
func (r *HistoryRecorder) RecordValidationFailure(ctx context.Context, taskID string, result *validation.Result, attemptNum int) error {
	if result == nil {
		return nil
	}

	comment := formatValidationFailure(result, attemptNum)
	if err := r.beadsClient.AddComment(ctx, taskID, comment); err != nil {
		return fmt.Errorf("failed to record validation failure: %w", err)
	}
	return nil
}

// RepairAttempt represents a single repair attempt result.
type RepairAttempt struct {
	Number         int           // Attempt number (1-indexed)
	Status         string        // "success", "failed", "timeout"
	Duration       time.Duration // How long the repair took
	Output         string        // Relevant output from the repair
	Error          string        // Error message if failed
	AgentID        string        // ID of the repair agent
	CommitsApplied int           // Number of commits applied by repair
}

// RecordRepairAttempt records a repair attempt on the task bead.
func (r *HistoryRecorder) RecordRepairAttempt(ctx context.Context, taskID string, attempt *RepairAttempt) error {
	if attempt == nil {
		return nil
	}

	comment := formatRepairAttempt(attempt)
	if err := r.beadsClient.AddComment(ctx, taskID, comment); err != nil {
		return fmt.Errorf("failed to record repair attempt: %w", err)
	}
	return nil
}

// FinalValidationStatus represents the final outcome after validation/repair.
type FinalValidationStatus string

const (
	FinalStatusValidated      FinalValidationStatus = "validated"        // Validation passed
	FinalStatusRepaired       FinalValidationStatus = "repaired"         // Repair succeeded
	FinalStatusNeedsManualFix FinalValidationStatus = "needs_manual_fix" // Repair exhausted
	FinalStatusSkipped        FinalValidationStatus = "skipped"          // Validation was skipped
)

// RecordFinalStatus records the final validation/repair status on the task bead.
func (r *HistoryRecorder) RecordFinalStatus(ctx context.Context, taskID string, status FinalValidationStatus, details string) error {
	comment := formatFinalStatus(status, details)
	if err := r.beadsClient.AddComment(ctx, taskID, comment); err != nil {
		return fmt.Errorf("failed to record final status: %w", err)
	}
	return nil
}

// formatValidationFailure creates a markdown comment for a validation failure.
func formatValidationFailure(result *validation.Result, attemptNum int) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("## Validation Failed (Attempt %d)\n\n", attemptNum))

	if result.FailedStep != "" {
		sb.WriteString(fmt.Sprintf("**Failed Step:** `%s`\n\n", result.FailedStep))
	}

	if result.Error != "" {
		sb.WriteString(fmt.Sprintf("**Error:** %s\n\n", result.Error))
	}

	sb.WriteString(fmt.Sprintf("**Duration:** %s\n\n", result.Duration.Round(time.Millisecond)))

	// Include step details
	if len(result.Steps) > 0 {
		sb.WriteString("### Step Results\n\n")
		for _, step := range result.Steps {
			statusEmoji := "✅"
			switch step.Status {
			case validation.ValidationStatusFailed:
				statusEmoji = "❌"
			case validation.ValidationStatusSkipped:
				statusEmoji = "⏭️"
			}

			sb.WriteString(fmt.Sprintf("- %s **%s** (%s)", statusEmoji, step.Name, step.Duration.Round(time.Millisecond)))
			if step.ExitCode != 0 {
				sb.WriteString(fmt.Sprintf(" - exit code %d", step.ExitCode))
			}
			sb.WriteString("\n")

			// Include output for failed steps (truncated)
			if step.Status == validation.ValidationStatusFailed && step.Output != "" {
				output := truncateOutput(step.Output, 500)
				sb.WriteString(fmt.Sprintf("  ```\n%s\n  ```\n", indentOutput(output, "  ")))
			}
		}
	}

	return sb.String()
}

// formatRepairAttempt creates a markdown comment for a repair attempt.
func formatRepairAttempt(attempt *RepairAttempt) string {
	var sb strings.Builder

	statusEmoji := "🔧"
	switch attempt.Status {
	case "success":
		statusEmoji = "✅"
	case "failed":
		statusEmoji = "❌"
	case "timeout":
		statusEmoji = "⏱️"
	}

	sb.WriteString(fmt.Sprintf("## %s Repair Attempt %d\n\n", statusEmoji, attempt.Number))
	sb.WriteString(fmt.Sprintf("**Status:** %s\n", attempt.Status))
	sb.WriteString(fmt.Sprintf("**Duration:** %s\n", attempt.Duration.Round(time.Millisecond)))

	if attempt.AgentID != "" {
		sb.WriteString(fmt.Sprintf("**Agent:** `%s`\n", attempt.AgentID))
	}

	if attempt.CommitsApplied > 0 {
		sb.WriteString(fmt.Sprintf("**Commits Applied:** %d\n", attempt.CommitsApplied))
	}

	if attempt.Error != "" {
		sb.WriteString(fmt.Sprintf("\n**Error:** %s\n", attempt.Error))
	}

	if attempt.Output != "" {
		output := truncateOutput(attempt.Output, 500)
		sb.WriteString(fmt.Sprintf("\n<details>\n<summary>Output</summary>\n\n```\n%s\n```\n</details>\n", output))
	}

	return sb.String()
}

// formatFinalStatus creates a markdown comment for the final status.
func formatFinalStatus(status FinalValidationStatus, details string) string {
	var sb strings.Builder

	switch status {
	case FinalStatusValidated:
		sb.WriteString("## ✅ Validation Passed\n\n")
		sb.WriteString("All validation steps completed successfully.\n")
	case FinalStatusRepaired:
		sb.WriteString("## ✅ Repaired Successfully\n\n")
		sb.WriteString("Validation initially failed but was fixed by a repair agent.\n")
	case FinalStatusNeedsManualFix:
		sb.WriteString("## ❌ Needs Manual Repair\n\n")
		sb.WriteString("Automated repair attempts were exhausted. Manual intervention required.\n")
	case FinalStatusSkipped:
		sb.WriteString("## ⏭️ Validation Skipped\n\n")
		sb.WriteString("No validation configuration found or validation disabled.\n")
	}

	if details != "" {
		sb.WriteString(fmt.Sprintf("\n%s\n", details))
	}

	return sb.String()
}

// truncateOutput truncates output to maxLen characters, adding ellipsis if needed.
func truncateOutput(output string, maxLen int) string {
	if len(output) <= maxLen {
		return output
	}
	return output[:maxLen] + "\n... (truncated)"
}

// indentOutput indents each line of output with the given prefix.
func indentOutput(output, prefix string) string {
	lines := strings.Split(output, "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}
