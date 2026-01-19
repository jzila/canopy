package validation

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestExecutor_NilConfig(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "executor-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	executor := NewExecutor(nil, tmpDir, false)
	result, err := executor.Run(context.Background())
	if err != nil {
		t.Errorf("Run() unexpected error: %v", err)
	}
	if result.Status != ValidationStatusSkipped {
		t.Errorf("expected status %q, got %q", ValidationStatusSkipped, result.Status)
	}
}

func TestExecutor_DisabledConfig(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "executor-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := &ValidationConfig{
		Validation: ValidationSettings{
			Enabled: false,
		},
	}

	executor := NewExecutor(config, tmpDir, false)
	result, err := executor.Run(context.Background())
	if err != nil {
		t.Errorf("Run() unexpected error: %v", err)
	}
	if result.Status != ValidationStatusSkipped {
		t.Errorf("expected status %q, got %q", ValidationStatusSkipped, result.Status)
	}
}

func TestExecutor_NoSteps(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "executor-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := &ValidationConfig{
		Validation: ValidationSettings{
			Enabled: true,
			Steps:   []StepConfig{},
		},
	}

	executor := NewExecutor(config, tmpDir, false)
	result, err := executor.Run(context.Background())
	if err != nil {
		t.Errorf("Run() unexpected error: %v", err)
	}
	if result.Status != ValidationStatusSkipped {
		t.Errorf("expected status %q, got %q", ValidationStatusSkipped, result.Status)
	}
}

func TestExecutor_SinglePassingStep(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "executor-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := &ValidationConfig{
		Validation: ValidationSettings{
			Enabled: true,
			Steps: []StepConfig{
				{Name: "echo", Command: "echo hello"},
			},
		},
	}

	executor := NewExecutor(config, tmpDir, false)
	result, err := executor.Run(context.Background())
	if err != nil {
		t.Errorf("Run() unexpected error: %v", err)
	}
	if result.Status != ValidationStatusPassed {
		t.Errorf("expected status %q, got %q", ValidationStatusPassed, result.Status)
	}
	if len(result.Steps) != 1 {
		t.Fatalf("expected 1 step result, got %d", len(result.Steps))
	}
	if result.Steps[0].Status != ValidationStatusPassed {
		t.Errorf("expected step status %q, got %q", ValidationStatusPassed, result.Steps[0].Status)
	}
	if result.Steps[0].ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.Steps[0].ExitCode)
	}
	if !strings.Contains(result.Steps[0].Output, "hello") {
		t.Errorf("expected output to contain 'hello', got %q", result.Steps[0].Output)
	}
}

func TestExecutor_SingleFailingStep(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "executor-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := &ValidationConfig{
		Validation: ValidationSettings{
			Enabled: true,
			Steps: []StepConfig{
				{Name: "fail", Command: "exit 1", Required: true},
			},
		},
	}

	executor := NewExecutor(config, tmpDir, false)
	result, err := executor.Run(context.Background())
	if err != nil {
		t.Errorf("Run() unexpected error: %v", err)
	}
	if result.Status != ValidationStatusFailed {
		t.Errorf("expected status %q, got %q", ValidationStatusFailed, result.Status)
	}
	if result.FailedStep != "fail" {
		t.Errorf("expected failed step 'fail', got %q", result.FailedStep)
	}
	if len(result.Steps) != 1 {
		t.Fatalf("expected 1 step result, got %d", len(result.Steps))
	}
	if result.Steps[0].ExitCode != 1 {
		t.Errorf("expected exit code 1, got %d", result.Steps[0].ExitCode)
	}
}

func TestExecutor_MultipleSteps_AllPass(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "executor-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := &ValidationConfig{
		Validation: ValidationSettings{
			Enabled: true,
			Steps: []StepConfig{
				{Name: "step1", Command: "echo step1"},
				{Name: "step2", Command: "echo step2"},
				{Name: "step3", Command: "echo step3"},
			},
		},
	}

	executor := NewExecutor(config, tmpDir, false)
	result, err := executor.Run(context.Background())
	if err != nil {
		t.Errorf("Run() unexpected error: %v", err)
	}
	if result.Status != ValidationStatusPassed {
		t.Errorf("expected status %q, got %q", ValidationStatusPassed, result.Status)
	}
	if len(result.Steps) != 3 {
		t.Fatalf("expected 3 step results, got %d", len(result.Steps))
	}
	for i, step := range result.Steps {
		if step.Status != ValidationStatusPassed {
			t.Errorf("step[%d] expected status %q, got %q", i, ValidationStatusPassed, step.Status)
		}
	}
}

func TestExecutor_RequiredStepFailure_StopsExecution(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "executor-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := &ValidationConfig{
		Validation: ValidationSettings{
			Enabled: true,
			Steps: []StepConfig{
				{Name: "step1", Command: "echo step1"},
				{Name: "step2", Command: "exit 1", Required: true},
				{Name: "step3", Command: "echo step3"}, // Should not run
			},
		},
	}

	executor := NewExecutor(config, tmpDir, false)
	result, err := executor.Run(context.Background())
	if err != nil {
		t.Errorf("Run() unexpected error: %v", err)
	}
	if result.Status != ValidationStatusFailed {
		t.Errorf("expected status %q, got %q", ValidationStatusFailed, result.Status)
	}
	if result.FailedStep != "step2" {
		t.Errorf("expected failed step 'step2', got %q", result.FailedStep)
	}
	// Only steps 1 and 2 should have run
	if len(result.Steps) != 2 {
		t.Fatalf("expected 2 step results (step3 should not run), got %d", len(result.Steps))
	}
}

func TestExecutor_OptionalStepFailure_ContinuesExecution(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "executor-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := &ValidationConfig{
		Validation: ValidationSettings{
			Enabled: true,
			Steps: []StepConfig{
				{Name: "step1", Command: "echo step1"},
				{Name: "step2", Command: "exit 1", Required: false}, // Optional failure
				{Name: "step3", Command: "echo step3"},              // Should still run
			},
		},
	}

	executor := NewExecutor(config, tmpDir, false)
	result, err := executor.Run(context.Background())
	if err != nil {
		t.Errorf("Run() unexpected error: %v", err)
	}
	// Overall status should be passed since required steps all passed
	if result.Status != ValidationStatusPassed {
		t.Errorf("expected status %q, got %q", ValidationStatusPassed, result.Status)
	}
	if len(result.Steps) != 3 {
		t.Fatalf("expected 3 step results, got %d", len(result.Steps))
	}
	// Step 2 should be failed
	if result.Steps[1].Status != ValidationStatusFailed {
		t.Errorf("step2 expected status %q, got %q", ValidationStatusFailed, result.Steps[1].Status)
	}
	// Step 3 should have run and passed
	if result.Steps[2].Status != ValidationStatusPassed {
		t.Errorf("step3 expected status %q, got %q", ValidationStatusPassed, result.Steps[2].Status)
	}
}

func TestExecutor_StrictMode_AnyFailureStops(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "executor-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := &ValidationConfig{
		Validation: ValidationSettings{
			Enabled: true,
			Strict:  true, // Strict mode
			Steps: []StepConfig{
				{Name: "step1", Command: "echo step1"},
				{Name: "step2", Command: "exit 1", Required: false}, // Optional, but strict mode
				{Name: "step3", Command: "echo step3"},              // Should not run
			},
		},
	}

	executor := NewExecutor(config, tmpDir, false)
	result, err := executor.Run(context.Background())
	if err != nil {
		t.Errorf("Run() unexpected error: %v", err)
	}
	if result.Status != ValidationStatusFailed {
		t.Errorf("expected status %q, got %q", ValidationStatusFailed, result.Status)
	}
	if result.FailedStep != "step2" {
		t.Errorf("expected failed step 'step2', got %q", result.FailedStep)
	}
	// Only steps 1 and 2 should have run
	if len(result.Steps) != 2 {
		t.Fatalf("expected 2 step results (step3 should not run), got %d", len(result.Steps))
	}
}

func TestExecutor_StepTimeout(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "executor-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := &ValidationConfig{
		Validation: ValidationSettings{
			Enabled: true,
			Timeout: "10s", // Global timeout
			Steps: []StepConfig{
				{Name: "slow", Command: "sleep 10", Timeout: "100ms", Required: true},
			},
		},
	}

	executor := NewExecutor(config, tmpDir, false)
	start := time.Now()
	result, err := executor.Run(context.Background())
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("Run() unexpected error: %v", err)
	}
	if result.Status != ValidationStatusFailed {
		t.Errorf("expected status %q, got %q", ValidationStatusFailed, result.Status)
	}
	// Should have timed out quickly, not waited 10 seconds
	// Use 5s tolerance for CI environments where process killing can be slow
	if elapsed > 5*time.Second {
		t.Errorf("expected timeout to trigger quickly, took %v", elapsed)
	}
	if len(result.Steps) != 1 {
		t.Fatalf("expected 1 step result, got %d", len(result.Steps))
	}
	if result.Steps[0].ExitCode != -2 {
		t.Errorf("expected exit code -2 (killed), got %d", result.Steps[0].ExitCode)
	}
	if !strings.Contains(result.Steps[0].Output, "killed") {
		t.Errorf("expected output to mention 'killed', got %q", result.Steps[0].Output)
	}
}

func TestExecutor_GlobalTimeout(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "executor-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := &ValidationConfig{
		Validation: ValidationSettings{
			Enabled: true,
			Timeout: "100ms", // Very short global timeout
			Steps: []StepConfig{
				{Name: "slow1", Command: "sleep 10"},
				{Name: "slow2", Command: "sleep 10"},
			},
		},
	}

	executor := NewExecutor(config, tmpDir, false)
	start := time.Now()
	result, err := executor.Run(context.Background())
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("Run() unexpected error: %v", err)
	}
	if result.Status != ValidationStatusFailed {
		t.Errorf("expected status %q, got %q", ValidationStatusFailed, result.Status)
	}
	// Should have timed out quickly
	// Use 5s tolerance for CI environments where process killing can be slow
	if elapsed > 5*time.Second {
		t.Errorf("expected global timeout to trigger quickly, took %v", elapsed)
	}
}

func TestExecutor_ContextCancellation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "executor-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := &ValidationConfig{
		Validation: ValidationSettings{
			Enabled: true,
			Timeout: "10s",
			Steps: []StepConfig{
				// This step will be running when context gets cancelled
				// Mark as required so failure propagates to overall status
				{Name: "slow", Command: "sleep 10", Required: true},
			},
		},
	}

	// Use a very short deadline to cancel during the sleep
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	executor := NewExecutor(config, tmpDir, false)
	start := time.Now()
	result, err := executor.Run(ctx)
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("Run() unexpected error: %v", err)
	}
	if result.Status != ValidationStatusFailed {
		t.Errorf("expected status %q, got %q", ValidationStatusFailed, result.Status)
	}
	// Should have been cancelled quickly (not waiting full 10 seconds)
	// Use 5s tolerance for CI environments where process killing can be slow
	if elapsed > 5*time.Second {
		t.Errorf("expected cancellation to trigger quickly, took %v", elapsed)
	}
	// Verify the step was killed
	if len(result.Steps) > 0 && result.Steps[0].ExitCode != -2 {
		t.Errorf("expected exit code -2 (killed), got %d", result.Steps[0].ExitCode)
	}
}

func TestExecutor_CapturesStderr(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "executor-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := &ValidationConfig{
		Validation: ValidationSettings{
			Enabled: true,
			Steps: []StepConfig{
				{Name: "stderr", Command: "echo error >&2"},
			},
		},
	}

	executor := NewExecutor(config, tmpDir, false)
	result, err := executor.Run(context.Background())
	if err != nil {
		t.Errorf("Run() unexpected error: %v", err)
	}
	if !strings.Contains(result.Steps[0].Output, "error") {
		t.Errorf("expected output to contain stderr 'error', got %q", result.Steps[0].Output)
	}
}

func TestExecutor_RunsInWorkDir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "executor-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a file in the work directory
	testFile := "test-file.txt"
	if err := os.WriteFile(tmpDir+"/"+testFile, []byte("content"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	config := &ValidationConfig{
		Validation: ValidationSettings{
			Enabled: true,
			Steps: []StepConfig{
				{Name: "pwd", Command: "ls " + testFile},
			},
		},
	}

	executor := NewExecutor(config, tmpDir, false)
	result, err := executor.Run(context.Background())
	if err != nil {
		t.Errorf("Run() unexpected error: %v", err)
	}
	if result.Status != ValidationStatusPassed {
		t.Errorf("expected status %q, got %q", ValidationStatusPassed, result.Status)
	}
	if !strings.Contains(result.Steps[0].Output, testFile) {
		t.Errorf("expected output to contain filename, got %q", result.Steps[0].Output)
	}
}

func TestExecutor_InvalidCommand(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "executor-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := &ValidationConfig{
		Validation: ValidationSettings{
			Enabled: true,
			Steps: []StepConfig{
				{Name: "invalid", Command: "nonexistent-command-xyz123", Required: true},
			},
		},
	}

	executor := NewExecutor(config, tmpDir, false)
	result, err := executor.Run(context.Background())
	if err != nil {
		t.Errorf("Run() unexpected error: %v", err)
	}
	if result.Status != ValidationStatusFailed {
		t.Errorf("expected status %q, got %q", ValidationStatusFailed, result.Status)
	}
	if result.Steps[0].ExitCode == 0 {
		t.Errorf("expected non-zero exit code for invalid command")
	}
}

func TestExecutor_Duration(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "executor-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := &ValidationConfig{
		Validation: ValidationSettings{
			Enabled: true,
			Steps: []StepConfig{
				{Name: "sleep", Command: "sleep 0.1"},
			},
		},
	}

	executor := NewExecutor(config, tmpDir, false)
	result, err := executor.Run(context.Background())
	if err != nil {
		t.Errorf("Run() unexpected error: %v", err)
	}

	// Overall duration should be at least 100ms
	if result.Duration < 100*time.Millisecond {
		t.Errorf("expected duration >= 100ms, got %v", result.Duration)
	}
	// Step duration should also be at least 100ms
	if len(result.Steps) > 0 && result.Steps[0].Duration < 100*time.Millisecond {
		t.Errorf("expected step duration >= 100ms, got %v", result.Steps[0].Duration)
	}
}

func TestNewExecutor(t *testing.T) {
	config := &ValidationConfig{
		Validation: ValidationSettings{Enabled: true},
	}
	executor := NewExecutor(config, "/test/dir", true)

	if executor == nil {
		t.Fatal("NewExecutor returned nil")
	}
	if executor.config != config {
		t.Error("executor config not set correctly")
	}
	if executor.workDir != "/test/dir" {
		t.Errorf("executor workDir = %q, want /test/dir", executor.workDir)
	}
	if !executor.verbose {
		t.Error("executor verbose not set correctly")
	}
}
