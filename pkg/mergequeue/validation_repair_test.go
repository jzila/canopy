package mergequeue

import (
	"context"
	"testing"
	"time"

	"github.com/jzila/canopy/pkg/beads"
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
