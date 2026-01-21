package mergequeue

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/jzila/canopy/pkg/agent"
	"github.com/jzila/canopy/pkg/beads"
	"github.com/jzila/canopy/pkg/repairagent"
	"github.com/jzila/canopy/pkg/sandbox"
	"github.com/jzila/canopy/pkg/validation"
)

func TestValidationAndRepairResult(t *testing.T) {
	// Test that the result struct captures all the expected fields
	result := &validationAndRepairResult{
		ValidationPassed:      true,
		RepairAttempted:       true,
		RepairSucceeded:       true,
		AttemptsUsed:          2,
		FinalValidationResult: &validation.Result{Status: validation.ValidationStatusPassed},
		Error:                 "",
	}

	if !result.ValidationPassed {
		t.Error("expected ValidationPassed to be true")
	}
	if !result.RepairAttempted {
		t.Error("expected RepairAttempted to be true")
	}
	if !result.RepairSucceeded {
		t.Error("expected RepairSucceeded to be true")
	}
	if result.AttemptsUsed != 2 {
		t.Errorf("expected AttemptsUsed to be 2, got %d", result.AttemptsUsed)
	}
}

func TestRunValidationAndRepair_ValidationDisabled(t *testing.T) {
	// Setup processor without validation config (disabled)
	mock := beads.NewMockClient()
	p := &Processor{
		beadsClient:      mock,
		validationConfig: nil, // Validation disabled
		verbose:          false,
	}

	ctx := context.Background()
	result := p.runValidationAndRepair(ctx, "task-1", "Test Task", "", "agent-1", false, false, 1)

	if !result.ValidationPassed {
		t.Error("expected ValidationPassed to be true when validation disabled")
	}
	if result.RepairAttempted {
		t.Error("expected no repair when validation disabled")
	}
	if result.AttemptsUsed != 0 {
		t.Errorf("expected 0 attempts used, got %d", result.AttemptsUsed)
	}
}

func TestRunValidationAndRepair_ValidationNotEnabled(t *testing.T) {
	// Setup processor with validation config but not enabled
	mock := beads.NewMockClient()
	config := &validation.ValidationConfig{
		Validation: validation.ValidationSettings{
			Enabled: false,
		},
	}

	p := &Processor{
		beadsClient:      mock,
		validationConfig: config,
		verbose:          false,
	}

	ctx := context.Background()
	result := p.runValidationAndRepair(ctx, "task-1", "Test Task", "", "agent-1", false, false, 1)

	if !result.ValidationPassed {
		t.Error("expected ValidationPassed to be true when validation not enabled")
	}
	if result.RepairAttempted {
		t.Error("expected no repair when validation not enabled")
	}
}

func TestRunValidationAndRepair_ContextCancelled(t *testing.T) {
	// Setup processor with validation enabled
	mock := beads.NewMockClient()
	config := &validation.ValidationConfig{
		Validation: validation.ValidationSettings{
			Enabled:           true,
			MaxRepairAttempts: 3,
			Steps: []validation.StepConfig{
				{Name: "test", Command: "echo pass", Required: true},
			},
		},
	}

	p := &Processor{
		beadsClient:      mock,
		validationConfig: config,
		verbose:          false,
		outputDir:        t.TempDir(),
	}

	// Create cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result := p.runValidationAndRepair(ctx, "task-1", "Test Task", "", "agent-1", false, false, 1)

	if result.ValidationPassed {
		t.Error("expected ValidationPassed to be false when context cancelled")
	}
	if result.Error == "" {
		t.Error("expected error message when context cancelled")
	}
}

func TestRunValidationAndRepair_NoRepairAgentConfigured(t *testing.T) {
	// Setup processor with validation enabled but no repair agent
	mock := beads.NewMockClient()
	config := &validation.ValidationConfig{
		Validation: validation.ValidationSettings{
			Enabled:           true,
			MaxRepairAttempts: 3,
			Steps: []validation.StepConfig{
				{Name: "test", Command: "false", Required: true}, // Always fails
			},
		},
	}

	p := &Processor{
		beadsClient:      mock,
		validationConfig: config,
		repairAgent:      nil, // No repair agent
		historyRecorder:  NewHistoryRecorder(mock, false),
		verbose:          false,
		outputDir:        t.TempDir(),
	}

	ctx := context.Background()
	result := p.runValidationAndRepair(ctx, "task-1", "Test Task", "", "agent-1", false, false, 1)

	if result.ValidationPassed {
		t.Error("expected ValidationPassed to be false when validation fails")
	}
	if result.Error == "" {
		t.Error("expected error message when no repair agent configured")
	}
	if result.RepairAttempted {
		t.Error("expected no repair attempted when no repair agent configured")
	}
}

func TestRunValidationAndRepair_AttemptsTracking(t *testing.T) {
	// This test verifies that the attempt counter is tracked correctly
	// We can't fully test the repair loop without a real repair agent,
	// but we can verify the initial state and first-pass behavior

	mock := beads.NewMockClient()
	config := &validation.ValidationConfig{
		Validation: validation.ValidationSettings{
			Enabled:           true,
			MaxRepairAttempts: 3,
			Timeout:           "1s",
			Steps: []validation.StepConfig{
				{Name: "quick", Command: "echo pass", Required: true, Timeout: "1s"},
			},
		},
	}

	p := &Processor{
		beadsClient:      mock,
		validationConfig: config,
		historyRecorder:  NewHistoryRecorder(mock, false),
		verbose:          false,
		outputDir:        t.TempDir(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result := p.runValidationAndRepair(ctx, "task-1", "Test Task", "", "agent-1", false, false, 1)

	// This command should pass on first try
	if !result.ValidationPassed {
		t.Errorf("expected ValidationPassed to be true, got error: %s", result.Error)
	}
	if result.AttemptsUsed != 0 {
		t.Errorf("expected 0 attempts (validation passed on first try), got %d", result.AttemptsUsed)
	}
	if result.RepairAttempted {
		t.Error("expected no repair attempted when validation passes")
	}
}

func TestGetMergedDiff_ZeroCommits(t *testing.T) {
	p := &Processor{
		outputDir: t.TempDir(),
		verbose:   false,
	}

	diff := p.getMergedDiff(0)
	if diff != "" {
		t.Errorf("expected empty diff for 0 commits, got: %q", diff)
	}
}

func TestGetMergedDiff_NonGitDir(t *testing.T) {
	// Testing with a non-git directory should return empty string (graceful failure)
	p := &Processor{
		outputDir: t.TempDir(),
		verbose:   false,
	}

	diff := p.getMergedDiff(5)
	if diff != "" {
		t.Errorf("expected empty diff for non-git dir, got: %q", diff)
	}
}

func TestValidationConfig_MaxRepairAttempts(t *testing.T) {
	tests := []struct {
		name           string
		config         *validation.ValidationConfig
		expectedMax    int
	}{
		{
			name:        "nil config uses default",
			config:      nil,
			expectedMax: 3,
		},
		{
			name: "zero value uses default",
			config: &validation.ValidationConfig{
				Validation: validation.ValidationSettings{
					MaxRepairAttempts: 0,
				},
			},
			expectedMax: 3,
		},
		{
			name: "custom value",
			config: &validation.ValidationConfig{
				Validation: validation.ValidationSettings{
					MaxRepairAttempts: 5,
				},
			},
			expectedMax: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got int
			if tt.config == nil {
				got = 3 // Default
			} else {
				got = tt.config.GetMaxRepairAttempts()
			}

			if got != tt.expectedMax {
				t.Errorf("GetMaxRepairAttempts() = %d, want %d", got, tt.expectedMax)
			}
		})
	}
}

func TestValidationResultStatusValues(t *testing.T) {
	// Test that our code handles all validation status values correctly
	statuses := []validation.ValidationStatus{
		validation.ValidationStatusPending,
		validation.ValidationStatusRunning,
		validation.ValidationStatusPassed,
		validation.ValidationStatusFailed,
		validation.ValidationStatusSkipped,
	}

	for _, status := range statuses {
		result := &validation.Result{Status: status}

		isPassing := result.Status == validation.ValidationStatusPassed

		// Only "passed" should be considered a successful validation
		if status == validation.ValidationStatusPassed && !isPassing {
			t.Errorf("ValidationStatusPassed should be considered passing")
		}
		if status != validation.ValidationStatusPassed && isPassing {
			t.Errorf("status %v should not be considered passing", status)
		}
	}
}

func TestRepairAttemptTracking(t *testing.T) {
	// Test that RepairAttempt struct captures all necessary fields
	attempt := &RepairAttempt{
		Number:         1,
		Status:         "success",
		Duration:       30 * time.Second,
		Output:         "repair output",
		Error:          "",
		AgentID:        "repair-agent-1",
		CommitsApplied: 2,
	}

	if attempt.Number != 1 {
		t.Error("attempt number not set correctly")
	}
	if attempt.Status != "success" {
		t.Error("attempt status not set correctly")
	}
	if attempt.CommitsApplied != 2 {
		t.Error("commits applied not set correctly")
	}
}

func TestValidationAndRepair_HistoryRecording(t *testing.T) {
	// Test that history is recorded correctly when validation passes on first try
	mock := beads.NewMockClient()
	config := &validation.ValidationConfig{
		Validation: validation.ValidationSettings{
			Enabled:           true,
			MaxRepairAttempts: 3,
			Timeout:           "1s",
			Steps: []validation.StepConfig{
				{Name: "quick", Command: "true", Required: true, Timeout: "1s"},
			},
		},
	}

	p := &Processor{
		beadsClient:      mock,
		validationConfig: config,
		historyRecorder:  NewHistoryRecorder(mock, false),
		verbose:          false,
		outputDir:        t.TempDir(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result := p.runValidationAndRepair(ctx, "task-1", "Test Task", "", "agent-1", false, false, 1)

	if !result.ValidationPassed {
		t.Errorf("expected validation to pass, got error: %s", result.Error)
	}

	// Check that final status was recorded
	if len(mock.Calls.AddComment) == 0 {
		t.Error("expected at least one history comment to be recorded")
	}
}

func TestBuildRepairAttemptSummary(t *testing.T) {
	tests := []struct {
		name            string
		attempt         *RepairAttempt
		result          *repairagent.Result
		wantContains    []string
		wantNotContains []string
	}{
		{
			name: "minimal attempt",
			attempt: &RepairAttempt{
				Number: 1,
				Status: "failed",
			},
			result:       nil,
			wantContains: []string{"Attempt 1: failed"},
		},
		{
			name: "attempt with error",
			attempt: &RepairAttempt{
				Number: 2,
				Status: "failed",
				Error:  "compilation error",
			},
			result:       nil,
			wantContains: []string{"Attempt 2: failed", "Error: compilation error"},
		},
		{
			name: "attempt with commits",
			attempt: &RepairAttempt{
				Number:         1,
				Status:         "success",
				CommitsApplied: 2,
			},
			result: &repairagent.Result{
				Success: true,
				AgentResult: &agent.Result{
					GitState: &sandbox.GitState{
						CommitMessages: []string{"fix: first fix", "fix: second fix"},
					},
				},
			},
			wantContains: []string{"Attempt 1: success", "Commits applied: 2", "fix: first fix", "fix: second fix"},
		},
		{
			name: "attempt with files changed",
			attempt: &RepairAttempt{
				Number: 1,
				Status: "failed",
			},
			result: &repairagent.Result{
				Success: false,
				AgentResult: &agent.Result{
					Changes: []sandbox.FileChange{
						{Path: "src/main.go"},
						{Path: "src/test.go"},
					},
				},
			},
			wantContains: []string{"Attempt 1: failed", "Files modified: 2", "src/main.go", "src/test.go"},
		},
		{
			name: "attempt with agent result message",
			attempt: &RepairAttempt{
				Number:         1,
				Status:         "failed",
				CommitsApplied: 1,
			},
			result: &repairagent.Result{
				Success: false,
				AgentResult: &agent.Result{
					Output: &agent.ClaudeOutput{
						ResultMessage: "I tried to fix the compilation error but the underlying issue is deeper.",
					},
					GitState: &sandbox.GitState{
						CommitMessages: []string{"fix: attempt fix"},
					},
				},
			},
			wantContains: []string{
				"Attempt 1: failed",
				"Agent summary:",
				"I tried to fix the compilation error",
			},
		},
		{
			name: "truncates long commit messages",
			attempt: &RepairAttempt{
				Number:         1,
				Status:         "success",
				CommitsApplied: 1,
			},
			result: &repairagent.Result{
				AgentResult: &agent.Result{
					GitState: &sandbox.GitState{
						CommitMessages: []string{strings.Repeat("a", 300)},
					},
				},
			},
			wantContains:    []string{"..."},
			wantNotContains: []string{strings.Repeat("a", 300)},
		},
		{
			name: "limits files shown to 5",
			attempt: &RepairAttempt{
				Number: 1,
				Status: "success",
			},
			result: &repairagent.Result{
				AgentResult: &agent.Result{
					Changes: []sandbox.FileChange{
						{Path: "file1.go"},
						{Path: "file2.go"},
						{Path: "file3.go"},
						{Path: "file4.go"},
						{Path: "file5.go"},
						{Path: "file6.go"},
						{Path: "file7.go"},
					},
				},
			},
			wantContains:    []string{"Files modified: 7", "file1.go", "file5.go", "... and 2 more files"},
			wantNotContains: []string{"file6.go", "file7.go"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildRepairAttemptSummary(tt.attempt, tt.result)

			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("buildRepairAttemptSummary() missing expected content %q\ngot:\n%s", want, got)
				}
			}

			for _, notWant := range tt.wantNotContains {
				if strings.Contains(got, notWant) {
					t.Errorf("buildRepairAttemptSummary() should not contain %q\ngot:\n%s", notWant, got)
				}
			}
		})
	}
}

func TestValidationAndRepairResult_RepairExhausted(t *testing.T) {
	// Test that the result struct captures repair exhaustion fields
	result := &validationAndRepairResult{
		ValidationPassed:       false,
		RepairAttempted:        true,
		RepairSucceeded:        false,
		RepairExhausted:        true,
		AttemptsUsed:           3,
		FinalValidationResult:  &validation.Result{Status: validation.ValidationStatusFailed},
		RepairAttemptSummaries: []string{"Attempt 1: failed", "Attempt 2: failed", "Attempt 3: failed"},
		Error:                  "validation failed after 3 repair attempts",
	}

	if result.ValidationPassed {
		t.Error("expected ValidationPassed to be false")
	}
	if !result.RepairExhausted {
		t.Error("expected RepairExhausted to be true")
	}
	if len(result.RepairAttemptSummaries) != 3 {
		t.Errorf("expected 3 repair attempt summaries, got %d", len(result.RepairAttemptSummaries))
	}
	if result.AttemptsUsed != 3 {
		t.Errorf("expected 3 attempts used, got %d", result.AttemptsUsed)
	}
}

func TestFileRepairExhaustedBead(t *testing.T) {
	mock := beads.NewMockClient()
	mock.NextCreateID = "repair-bead-1"

	p := &Processor{
		beadsClient: mock,
		verbose:     false,
	}

	ctx := context.Background()
	repairCtx := &repairExhaustedContext{
		TaskID:    "task-123",
		TaskTitle: "Fix the build",
		ValidationResult: &validation.Result{
			Status: validation.ValidationStatusFailed,
			Steps: []validation.StepResult{
				{
					Name:    "build",
					Status:  validation.ValidationStatusFailed,
					Command: "go build ./...",
					Output:  "compilation error: undefined function",
				},
			},
		},
		RepairSummaries: []string{
			"Attempt 1: failed\nError: still undefined",
			"Attempt 2: failed\nError: syntax error",
			"Attempt 3: failed\nError: type mismatch",
		},
		MergedDiff:  "diff --git a/main.go b/main.go\n...",
		MaxAttempts: 3,
	}

	beadID, err := p.fileRepairExhaustedBead(ctx, repairCtx)
	if err != nil {
		t.Fatalf("fileRepairExhaustedBead failed: %v", err)
	}

	if beadID != "repair-bead-1" {
		t.Errorf("expected bead ID 'repair-bead-1', got %q", beadID)
	}

	// Verify CreateWithDescription was called
	if len(mock.Calls.CreateWithDescription) != 1 {
		t.Fatalf("expected 1 CreateWithDescription call, got %d", len(mock.Calls.CreateWithDescription))
	}

	call := mock.Calls.CreateWithDescription[0]
	if !strings.Contains(call.Title, "Repair exhausted") {
		t.Errorf("expected title to contain 'Repair exhausted', got %q", call.Title)
	}
	if call.Priority != 1 {
		t.Errorf("expected priority 1, got %d", call.Priority)
	}

	// Verify description contains key information
	desc := call.Description
	if !strings.Contains(desc, "Validation Failure - Repair Exhausted") {
		t.Error("description missing 'Validation Failure - Repair Exhausted' header")
	}
	if !strings.Contains(desc, "task-123") {
		t.Error("description missing task ID")
	}
	if !strings.Contains(desc, "go build ./...") {
		t.Error("description missing validation command")
	}
	if !strings.Contains(desc, "compilation error") {
		t.Error("description missing validation output")
	}
	if !strings.Contains(desc, "Attempt 1: failed") {
		t.Error("description missing repair attempt summary")
	}
	if !strings.Contains(desc, "Manual intervention needed") {
		t.Error("description missing action required section")
	}
}

func TestFileRepairExhaustedBead_NoBeadsClient(t *testing.T) {
	p := &Processor{
		beadsClient: nil,
		verbose:     false,
	}

	ctx := context.Background()
	repairCtx := &repairExhaustedContext{
		TaskID:    "task-123",
		TaskTitle: "Test task",
	}

	_, err := p.fileRepairExhaustedBead(ctx, repairCtx)
	if err == nil {
		t.Error("expected error when beads client is nil")
	}
	if !strings.Contains(err.Error(), "no beads client configured") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestFileRepairExhaustedBead_TruncatesLongContent(t *testing.T) {
	mock := beads.NewMockClient()
	mock.NextCreateID = "repair-bead-2"

	p := &Processor{
		beadsClient: mock,
		verbose:     false,
	}

	ctx := context.Background()
	// Create a very long output and diff
	longOutput := strings.Repeat("error line\n", 500)  // ~5500 chars
	longDiff := strings.Repeat("+ new line\n", 1000)   // ~11000 chars

	repairCtx := &repairExhaustedContext{
		TaskID:    "task-long",
		TaskTitle: "Task with long output",
		ValidationResult: &validation.Result{
			Status: validation.ValidationStatusFailed,
			Steps: []validation.StepResult{
				{
					Name:   "test",
					Status: validation.ValidationStatusFailed,
					Output: longOutput,
				},
			},
		},
		RepairSummaries: []string{"Attempt 1: failed"},
		MergedDiff:      longDiff,
		MaxAttempts:     1,
	}

	beadID, err := p.fileRepairExhaustedBead(ctx, repairCtx)
	if err != nil {
		t.Fatalf("fileRepairExhaustedBead failed: %v", err)
	}

	if beadID != "repair-bead-2" {
		t.Errorf("expected bead ID 'repair-bead-2', got %q", beadID)
	}

	// Verify that truncation happened (description should not be extremely long)
	call := mock.Calls.CreateWithDescription[0]
	if len(call.Description) > 15000 {
		t.Errorf("description was not truncated, length: %d", len(call.Description))
	}
	if !strings.Contains(call.Description, "(truncated)") {
		t.Error("description should contain truncation marker")
	}
}

func TestGetCurrentHead(t *testing.T) {
	// Create a temp git repo
	tmpDir := t.TempDir()

	// Initialize git repo
	initCmd := strings.Join([]string{
		"cd", tmpDir, "&&",
		"git init &&",
		"git config user.email test@test.com &&",
		"git config user.name Test &&",
		"echo hello > file.txt &&",
		"git add . &&",
		"git commit -m 'initial'",
	}, " ")

	cmd := execCommand("sh", "-c", initCmd)
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to setup git repo: %v", err)
	}

	p := &Processor{
		outputDir: tmpDir,
		verbose:   false,
	}

	head, err := p.getCurrentHead()
	if err != nil {
		t.Fatalf("getCurrentHead failed: %v", err)
	}

	if len(head) != 40 {
		t.Errorf("expected 40-char SHA, got %d chars: %q", len(head), head)
	}
}

func TestGetCurrentHead_NonGitDir(t *testing.T) {
	p := &Processor{
		outputDir: t.TempDir(),
		verbose:   false,
	}

	_, err := p.getCurrentHead()
	if err == nil {
		t.Error("expected error for non-git directory")
	}
}

func TestRevertMerge(t *testing.T) {
	// Create a temp git repo with two commits
	tmpDir := t.TempDir()

	initCmd := strings.Join([]string{
		"cd", tmpDir, "&&",
		"git init &&",
		"git config user.email test@test.com &&",
		"git config user.name Test &&",
		"echo hello > file.txt &&",
		"git add . &&",
		"git commit -m 'initial'",
	}, " ")

	cmd := execCommand("sh", "-c", initCmd)
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to setup git repo: %v", err)
	}

	p := &Processor{
		outputDir: tmpDir,
		verbose:   false,
	}

	// Get initial HEAD
	initialHead, err := p.getCurrentHead()
	if err != nil {
		t.Fatalf("getCurrentHead failed: %v", err)
	}

	// Create second commit (simulating a merge)
	mergeCmd := strings.Join([]string{
		"cd", tmpDir, "&&",
		"echo world >> file.txt &&",
		"git add . &&",
		"git commit -m 'second commit'",
	}, " ")

	cmd = execCommand("sh", "-c", mergeCmd)
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to create second commit: %v", err)
	}

	// Verify we're on a different commit now
	afterMerge, err := p.getCurrentHead()
	if err != nil {
		t.Fatalf("getCurrentHead failed: %v", err)
	}
	if afterMerge == initialHead {
		t.Error("expected different commit after merge")
	}

	// Revert to initial commit
	if err := p.revertMerge(initialHead); err != nil {
		t.Fatalf("revertMerge failed: %v", err)
	}

	// Verify we're back to initial commit
	afterRevert, err := p.getCurrentHead()
	if err != nil {
		t.Fatalf("getCurrentHead failed: %v", err)
	}
	if afterRevert != initialHead {
		t.Errorf("expected HEAD to be %s after revert, got %s", initialHead, afterRevert)
	}
}

func TestRevertMerge_EmptyCommit(t *testing.T) {
	p := &Processor{
		outputDir: t.TempDir(),
		verbose:   false,
	}

	err := p.revertMerge("")
	if err == nil {
		t.Error("expected error for empty pre-merge commit")
	}
	if !strings.Contains(err.Error(), "no pre-merge commit specified") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestFileValidationFailureBead(t *testing.T) {
	mock := beads.NewMockClient()
	mock.NextCreateID = "validation-bead-1"

	p := &Processor{
		beadsClient: mock,
		verbose:     false,
	}

	ctx := context.Background()
	result := &validation.Result{
		Status: validation.ValidationStatusFailed,
		Steps: []validation.StepResult{
			{
				Name:    "build",
				Status:  validation.ValidationStatusFailed,
				Command: "go build ./...",
				Output:  "undefined: someFunction",
			},
			{
				Name:    "test",
				Status:  validation.ValidationStatusPassed,
				Command: "go test ./...",
				Output:  "ok",
			},
		},
	}

	p.fileValidationFailureBead(ctx, "task-456", "Add new feature", result)

	// Verify CreateWithDescription was called
	if len(mock.Calls.CreateWithDescription) != 1 {
		t.Fatalf("expected 1 CreateWithDescription call, got %d", len(mock.Calls.CreateWithDescription))
	}

	call := mock.Calls.CreateWithDescription[0]
	if !strings.Contains(call.Title, "Validation failed") {
		t.Errorf("expected title to contain 'Validation failed', got %q", call.Title)
	}
	if call.Priority != 1 {
		t.Errorf("expected priority 1, got %d", call.Priority)
	}

	desc := call.Description
	if !strings.Contains(desc, "Validation Failure") {
		t.Error("description missing header")
	}
	if !strings.Contains(desc, "task-456") {
		t.Error("description missing task ID")
	}
	if !strings.Contains(desc, "undefined: someFunction") {
		t.Error("description missing validation output")
	}
	if !strings.Contains(desc, "lenient mode") {
		t.Error("description missing lenient mode mention")
	}
	// Should NOT contain passed steps
	if strings.Contains(desc, "test") && strings.Contains(desc, "ok") {
		t.Error("description should only contain failed steps")
	}
}

func TestFileValidationFailureBead_NoClient(t *testing.T) {
	p := &Processor{
		beadsClient: nil,
		verbose:     false,
	}

	// Should not panic when beadsClient is nil
	p.fileValidationFailureBead(context.Background(), "task-1", "Test", nil)
}

func TestIsStrict(t *testing.T) {
	tests := []struct {
		name     string
		config   *validation.ValidationConfig
		expected bool
	}{
		{
			name:     "nil config",
			config:   nil,
			expected: false,
		},
		{
			name: "strict disabled",
			config: &validation.ValidationConfig{
				Validation: validation.ValidationSettings{
					Enabled: true,
					Strict:  false,
				},
			},
			expected: false,
		},
		{
			name: "strict enabled",
			config: &validation.ValidationConfig{
				Validation: validation.ValidationSettings{
					Enabled: true,
					Strict:  true,
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got bool
			if tt.config != nil {
				got = tt.config.IsStrict()
			}
			if got != tt.expected {
				t.Errorf("IsStrict() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// execCommand is a helper to run commands for tests
func execCommand(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}

func TestValidateOverlayAccessible(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(t *testing.T) *sandbox.Overlay
		wantErr     bool
		errContains string
	}{
		{
			name: "nil overlay",
			setup: func(t *testing.T) *sandbox.Overlay {
				return nil
			},
			wantErr:     true,
			errContains: "overlay is nil",
		},
		{
			name: "empty upper directory (direct overlay)",
			setup: func(t *testing.T) *sandbox.Overlay {
				return &sandbox.Overlay{
					UpperDir: "",
				}
			},
			wantErr: false,
		},
		{
			name: "accessible upper directory with files",
			setup: func(t *testing.T) *sandbox.Overlay {
				tmpDir := t.TempDir()
				upperDir := tmpDir + "/upper"
				if err := os.MkdirAll(upperDir, 0755); err != nil {
					t.Fatalf("failed to create upper dir: %v", err)
				}
				// Create a test file
				if err := os.WriteFile(upperDir+"/test.txt", []byte("content"), 0644); err != nil {
					t.Fatalf("failed to create test file: %v", err)
				}
				return &sandbox.Overlay{
					UpperDir: upperDir,
				}
			},
			wantErr: false,
		},
		{
			name: "accessible upper directory - empty but valid",
			setup: func(t *testing.T) *sandbox.Overlay {
				tmpDir := t.TempDir()
				upperDir := tmpDir + "/upper"
				if err := os.MkdirAll(upperDir, 0755); err != nil {
					t.Fatalf("failed to create upper dir: %v", err)
				}
				return &sandbox.Overlay{
					UpperDir: upperDir,
				}
			},
			wantErr: false,
		},
		{
			name: "non-existent upper directory",
			setup: func(t *testing.T) *sandbox.Overlay {
				return &sandbox.Overlay{
					UpperDir: "/nonexistent/path/that/does/not/exist",
				}
			},
			wantErr:     true,
			errContains: "cannot read upper directory",
		},
		{
			name: "accessible directory with only whiteout files",
			setup: func(t *testing.T) *sandbox.Overlay {
				tmpDir := t.TempDir()
				upperDir := tmpDir + "/upper"
				if err := os.MkdirAll(upperDir, 0755); err != nil {
					t.Fatalf("failed to create upper dir: %v", err)
				}
				// Create only whiteout files (should be skipped)
				if err := os.WriteFile(upperDir+"/.wh.deleted", []byte{}, 0644); err != nil {
					t.Fatalf("failed to create whiteout file: %v", err)
				}
				return &sandbox.Overlay{
					UpperDir: upperDir,
				}
			},
			wantErr: false, // Should pass because we can read the directory
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			overlay := tt.setup(t)
			p := &Processor{verbose: false}

			err := p.validateOverlayAccessible(overlay)

			if tt.wantErr {
				if err == nil {
					t.Errorf("validateOverlayAccessible() expected error, got nil")
				} else if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("validateOverlayAccessible() error = %v, want error containing %q", err, tt.errContains)
				}
			} else {
				if err != nil {
					t.Errorf("validateOverlayAccessible() unexpected error: %v", err)
				}
			}
		})
	}
}

func TestValidateOverlayAccessible_UnreadableFile(t *testing.T) {
	// This test requires creating a file that can't be read
	// Skip on Windows or if running as root
	if os.Getuid() == 0 {
		t.Skip("skipping test when running as root")
	}

	tmpDir := t.TempDir()
	upperDir := tmpDir + "/upper"
	if err := os.MkdirAll(upperDir, 0755); err != nil {
		t.Fatalf("failed to create upper dir: %v", err)
	}

	// Create a file with no read permissions
	unreadableFile := upperDir + "/unreadable.txt"
	if err := os.WriteFile(unreadableFile, []byte("content"), 0000); err != nil {
		t.Fatalf("failed to create unreadable file: %v", err)
	}
	defer os.Chmod(unreadableFile, 0644) // Restore for cleanup

	overlay := &sandbox.Overlay{
		UpperDir: upperDir,
	}
	p := &Processor{verbose: false}

	err := p.validateOverlayAccessible(overlay)
	if err == nil {
		t.Error("validateOverlayAccessible() expected error for unreadable file, got nil")
	} else if !strings.Contains(err.Error(), "cannot access file") {
		t.Errorf("validateOverlayAccessible() error = %v, want error containing 'cannot access file'", err)
	}
}
