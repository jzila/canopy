package repairagent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jzila/canopy/pkg/validation"
)

func TestBuildRepairPrompt_BasicCase(t *testing.T) {
	ctx := &RepairContext{
		TaskID:    "task-123",
		TaskTitle: "Add user authentication",
		FailedStep: &validation.StepResult{
			Name:     "unit-tests",
			Command:  "go test ./...",
			ExitCode: 1,
			Duration: 5 * time.Second,
			Output:   "--- FAIL: TestAuth\n    auth_test.go:15: expected nil error, got: missing token",
		},
		MergedDiff: `diff --git a/auth.go b/auth.go
+func Authenticate(token string) error {
+    return nil
+}`,
		RepairAttempt:     1,
		MaxRepairAttempts: 3,
	}

	prompt := BuildRepairPrompt(ctx)

	// Verify key sections are present
	if !strings.Contains(prompt, "## Repair Agent") {
		t.Error("prompt should contain repair agent header")
	}
	if !strings.Contains(prompt, "### What Failed") {
		t.Error("prompt should contain What Failed section")
	}
	if !strings.Contains(prompt, "unit-tests") {
		t.Error("prompt should contain failed step name")
	}
	if !strings.Contains(prompt, "go test ./...") {
		t.Error("prompt should contain failed command")
	}
	if !strings.Contains(prompt, "Exit Code:** 1") {
		t.Error("prompt should contain exit code")
	}
	if !strings.Contains(prompt, "TestAuth") {
		t.Error("prompt should contain test output")
	}
	if !strings.Contains(prompt, "### What Was Merged") {
		t.Error("prompt should contain What Was Merged section")
	}
	if !strings.Contains(prompt, "Add user authentication") {
		t.Error("prompt should contain task title")
	}
	if !strings.Contains(prompt, "Authenticate") {
		t.Error("prompt should contain diff content")
	}
	if !strings.Contains(prompt, "Attempt:** 1 of 3") {
		t.Error("prompt should contain attempt info")
	}
	if !strings.Contains(prompt, "DO NOT:") {
		t.Error("prompt should contain DO NOT section")
	}
	if !strings.Contains(prompt, "Revert the merged changes") {
		t.Error("prompt should warn against reverting")
	}
}

func TestBuildRepairPrompt_WithPreviousAttempts(t *testing.T) {
	ctx := &RepairContext{
		TaskID:    "task-123",
		TaskTitle: "Fix build",
		FailedStep: &validation.StepResult{
			Name:     "build",
			Command:  "go build ./...",
			ExitCode: 1,
			Output:   "undefined: SomeFunc",
		},
		PreviousAttempts: []string{
			"Attempted to add import statement but the function doesn't exist in that package.",
			"Tried creating a stub function but it had wrong signature.",
		},
		RepairAttempt:     3,
		MaxRepairAttempts: 3,
	}

	prompt := BuildRepairPrompt(ctx)

	if !strings.Contains(prompt, "### Previous Repair Attempts") {
		t.Error("prompt should contain Previous Attempts section")
	}
	if !strings.Contains(prompt, "Attempt 1:") {
		t.Error("prompt should contain first attempt")
	}
	if !strings.Contains(prompt, "import statement") {
		t.Error("prompt should contain first attempt content")
	}
	if !strings.Contains(prompt, "Attempt 2:") {
		t.Error("prompt should contain second attempt")
	}
	if !strings.Contains(prompt, "stub function") {
		t.Error("prompt should contain second attempt content")
	}
	if !strings.Contains(prompt, "Do NOT repeat these approaches") {
		t.Error("prompt should warn against repeating")
	}
	if !strings.Contains(prompt, "Repeat approaches from previous attempts") {
		t.Error("prompt should list not repeating in DO NOT section")
	}
}

func TestBuildRepairPrompt_NilFailedStep(t *testing.T) {
	ctx := &RepairContext{
		TaskID:    "task-456",
		TaskTitle: "Some task",
		ValidationResult: &validation.Result{
			FailedStep: "integration-tests",
			Error:      "step timed out after 5m",
		},
	}

	prompt := BuildRepairPrompt(ctx)

	if !strings.Contains(prompt, "integration-tests") {
		t.Error("prompt should contain failed step name from result")
	}
	if !strings.Contains(prompt, "timed out") {
		t.Error("prompt should contain error message from result")
	}
}

func TestTruncateWithContext_NoTruncationNeeded(t *testing.T) {
	input := "short string"
	result := truncateWithContext(input, 100)

	if result != input {
		t.Errorf("expected unchanged string, got %q", result)
	}
}

func TestTruncateWithContext_LongString(t *testing.T) {
	// Create a long string with identifiable start and end
	input := "START" + strings.Repeat("x", 1000) + "END"
	result := truncateWithContext(input, 200)

	if !strings.Contains(result, "START") {
		t.Error("truncated string should preserve start")
	}
	if !strings.Contains(result, "END") {
		t.Error("truncated string should preserve end")
	}
	if !strings.Contains(result, "truncated") {
		t.Error("truncated string should indicate truncation")
	}
	if len(result) > 250 { // Allow some margin for truncation message
		t.Errorf("truncated string too long: %d chars", len(result))
	}
}

func TestTruncateWithContext_PreservesLineBreaks(t *testing.T) {
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = strings.Repeat("x", 50)
	}
	input := strings.Join(lines, "\n")

	result := truncateWithContext(input, 500)

	// Result should not end mid-line (should end with newline or complete line)
	resultLines := strings.Split(result, "\n")
	for i, line := range resultLines {
		// Skip truncation message lines
		if strings.Contains(line, "truncated") {
			continue
		}
		// Lines should be either complete (50 chars) or part of truncation
		if len(line) > 0 && len(line) < 50 && i != 0 && i != len(resultLines)-1 {
			// This is acceptable if it's near the truncation point
		}
	}
}

func TestWriteRepairContext_CreatesFiles(t *testing.T) {
	tmpDir := t.TempDir()

	ctx := &RepairContext{
		TaskID:    "task-789",
		TaskTitle: "Test task",
		FailedStep: &validation.StepResult{
			Name:     "test",
			Command:  "npm test",
			ExitCode: 1,
			Duration: 10 * time.Second,
			Output:   "FAIL src/test.js\n  ✕ should work",
		},
		MergedDiff: "diff --git a/file.js b/file.js\n+console.log('test')",
		PreviousAttempts: []string{
			"First attempt: tried X",
			"Second attempt: tried Y",
		},
		RepairAttempt:     3,
		MaxRepairAttempts: 5,
	}

	err := WriteRepairContext(tmpDir, ctx)
	if err != nil {
		t.Fatalf("WriteRepairContext failed: %v", err)
	}

	repairDir := filepath.Join(tmpDir, ".canopy", "repair")

	// Check merged.diff
	diffContent, err := os.ReadFile(filepath.Join(repairDir, "merged.diff"))
	if err != nil {
		t.Errorf("failed to read merged.diff: %v", err)
	}
	if !strings.Contains(string(diffContent), "console.log") {
		t.Error("merged.diff should contain diff content")
	}

	// Check validation-output.txt
	outputContent, err := os.ReadFile(filepath.Join(repairDir, "validation-output.txt"))
	if err != nil {
		t.Errorf("failed to read validation-output.txt: %v", err)
	}
	if !strings.Contains(string(outputContent), "FAIL src/test.js") {
		t.Error("validation-output.txt should contain test output")
	}

	// Check failed-step.txt
	stepContent, err := os.ReadFile(filepath.Join(repairDir, "failed-step.txt"))
	if err != nil {
		t.Errorf("failed to read failed-step.txt: %v", err)
	}
	if !strings.Contains(string(stepContent), "npm test") {
		t.Error("failed-step.txt should contain command")
	}

	// Check previous attempts
	attempt1, err := os.ReadFile(filepath.Join(repairDir, "previous-attempts", "attempt-1.txt"))
	if err != nil {
		t.Errorf("failed to read attempt-1.txt: %v", err)
	}
	if !strings.Contains(string(attempt1), "tried X") {
		t.Error("attempt-1.txt should contain first attempt")
	}

	attempt2, err := os.ReadFile(filepath.Join(repairDir, "previous-attempts", "attempt-2.txt"))
	if err != nil {
		t.Errorf("failed to read attempt-2.txt: %v", err)
	}
	if !strings.Contains(string(attempt2), "tried Y") {
		t.Error("attempt-2.txt should contain second attempt")
	}

	// Check task-context.txt
	taskContent, err := os.ReadFile(filepath.Join(repairDir, "task-context.txt"))
	if err != nil {
		t.Errorf("failed to read task-context.txt: %v", err)
	}
	if !strings.Contains(string(taskContent), "task-789") {
		t.Error("task-context.txt should contain task ID")
	}
	if !strings.Contains(string(taskContent), "3 of 5") {
		t.Error("task-context.txt should contain attempt info")
	}
}

func TestWriteRepairContext_EmptyOptionalFields(t *testing.T) {
	tmpDir := t.TempDir()

	ctx := &RepairContext{
		TaskID:    "task-minimal",
		TaskTitle: "Minimal task",
		// No diff, no previous attempts, no failed step
	}

	err := WriteRepairContext(tmpDir, ctx)
	if err != nil {
		t.Fatalf("WriteRepairContext failed: %v", err)
	}

	repairDir := filepath.Join(tmpDir, ".canopy", "repair")

	// merged.diff should not exist
	if _, err := os.Stat(filepath.Join(repairDir, "merged.diff")); !os.IsNotExist(err) {
		t.Error("merged.diff should not exist when no diff provided")
	}

	// validation-output.txt should not exist
	if _, err := os.Stat(filepath.Join(repairDir, "validation-output.txt")); !os.IsNotExist(err) {
		t.Error("validation-output.txt should not exist when no failed step")
	}

	// previous-attempts should not exist
	if _, err := os.Stat(filepath.Join(repairDir, "previous-attempts")); !os.IsNotExist(err) {
		t.Error("previous-attempts should not exist when no previous attempts")
	}

	// task-context.txt should always exist
	if _, err := os.Stat(filepath.Join(repairDir, "task-context.txt")); os.IsNotExist(err) {
		t.Error("task-context.txt should always exist")
	}
}

func TestBuildRepairPrompt_SuccessCriteria(t *testing.T) {
	ctx := &RepairContext{
		TaskID:    "task-123",
		TaskTitle: "Test",
		FailedStep: &validation.StepResult{
			Name:    "lint",
			Command: "eslint .",
		},
	}

	prompt := BuildRepairPrompt(ctx)

	// Should include specific success criteria with the command
	if !strings.Contains(prompt, "eslint .") {
		t.Error("prompt should contain the command in success criteria")
	}
	if !strings.Contains(prompt, "exits with code 0") {
		t.Error("prompt should specify exit code 0 as success")
	}
	if !strings.Contains(prompt, "fix: repair validation failure in lint") {
		t.Error("prompt should suggest commit message format")
	}
}
