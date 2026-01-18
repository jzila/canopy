// Package validation provides execution of post-merge validation steps.
package validation

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

// ValidationStatus represents the status of a validation run or step.
type ValidationStatus string

const (
	ValidationStatusPending ValidationStatus = "pending"
	ValidationStatusRunning ValidationStatus = "running"
	ValidationStatusPassed  ValidationStatus = "passed"
	ValidationStatusFailed  ValidationStatus = "failed"
	ValidationStatusSkipped ValidationStatus = "skipped"
)

// Result holds the result of a complete validation run.
type Result struct {
	// Status is the overall validation status.
	Status ValidationStatus
	// Steps contains results for each validation step.
	Steps []StepResult
	// Duration is the total time spent running all validation steps.
	Duration time.Duration
	// Error contains an error message if the validation failed.
	Error string
	// FailedStep is the name of the first step that failed (if any).
	FailedStep string
}

// StepResult holds the result of a single validation step.
type StepResult struct {
	// Name is the human-readable name of this step.
	Name string
	// Command is the shell command that was executed.
	Command string
	// Status is the result status of this step.
	Status ValidationStatus
	// Output is the combined stdout/stderr from the command.
	Output string
	// ExitCode is the process exit code (-1 if not started, -2 if killed).
	ExitCode int
	// Duration is the time spent running this step.
	Duration time.Duration
}

// Executor runs validation commands and captures their results.
type Executor struct {
	config  *ValidationConfig
	workDir string
	verbose bool
}

// NewExecutor creates a new validation executor.
func NewExecutor(config *ValidationConfig, workDir string, verbose bool) *Executor {
	return &Executor{
		config:  config,
		workDir: workDir,
		verbose: verbose,
	}
}

// Run executes all validation steps sequentially.
// It stops on the first required step failure but continues past optional step failures.
func (e *Executor) Run(ctx context.Context) (*Result, error) {
	if e.config == nil || !e.config.IsEnabled() {
		return &Result{
			Status: ValidationStatusSkipped,
		}, nil
	}

	steps := e.config.Validation.Steps
	if len(steps) == 0 {
		return &Result{
			Status: ValidationStatusSkipped,
		}, nil
	}

	result := &Result{
		Status: ValidationStatusRunning,
		Steps:  make([]StepResult, 0, len(steps)),
	}

	start := time.Now()

	// Create a context with global timeout if specified
	globalTimeout := e.config.GetTimeout()
	if globalTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, globalTimeout)
		defer cancel()
	}

	for _, step := range steps {
		// Check if context is already cancelled
		if ctx.Err() != nil {
			result.Status = ValidationStatusFailed
			result.Error = fmt.Sprintf("validation cancelled: %v", ctx.Err())
			result.Duration = time.Since(start)
			return result, nil
		}

		stepResult := e.runStep(ctx, &step)
		result.Steps = append(result.Steps, *stepResult)

		// If step failed and it's required, stop immediately
		if stepResult.Status == ValidationStatusFailed && step.Required {
			result.Status = ValidationStatusFailed
			result.FailedStep = step.Name
			result.Error = fmt.Sprintf("required step %q failed: exit code %d", step.Name, stepResult.ExitCode)
			result.Duration = time.Since(start)
			return result, nil
		}

		// In strict mode, any step failure stops validation
		if stepResult.Status == ValidationStatusFailed && e.config.IsStrict() {
			result.Status = ValidationStatusFailed
			result.FailedStep = step.Name
			result.Error = fmt.Sprintf("step %q failed (strict mode): exit code %d", step.Name, stepResult.ExitCode)
			result.Duration = time.Since(start)
			return result, nil
		}
	}

	// All steps completed (some optional may have failed)
	result.Status = ValidationStatusPassed
	result.Duration = time.Since(start)

	// Check if any steps failed (optional ones that didn't stop execution)
	for _, step := range result.Steps {
		if step.Status == ValidationStatusFailed {
			// At least one step failed but validation continued
			// Still mark as passed since we didn't stop
			break
		}
	}

	return result, nil
}

// runStep executes a single validation step.
func (e *Executor) runStep(ctx context.Context, step *StepConfig) *StepResult {
	result := &StepResult{
		Name:     step.Name,
		Command:  step.Command,
		Status:   ValidationStatusRunning,
		ExitCode: -1, // Default to -1 (not started)
	}

	start := time.Now()

	// Create a context with step-specific timeout
	stepTimeout := e.config.GetStepTimeout(step)
	if stepTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, stepTimeout)
		defer cancel()
	}

	// Build the command using shell to handle complex commands
	cmd := exec.Command("sh", "-c", step.Command)
	cmd.Dir = e.workDir

	// Capture combined stdout/stderr
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	// Start the command
	if err := cmd.Start(); err != nil {
		result.Status = ValidationStatusFailed
		result.Output = fmt.Sprintf("failed to start command: %v", err)
		result.Duration = time.Since(start)
		return result
	}

	// Wait for completion with context handling
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-ctx.Done():
		// Context cancelled (timeout or parent cancellation)
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		<-done // Wait for the goroutine to finish
		result.Status = ValidationStatusFailed
		result.ExitCode = -2 // Killed
		result.Output = output.String() + fmt.Sprintf("\n[killed: %v]", ctx.Err())
		result.Duration = time.Since(start)
		return result

	case err := <-done:
		result.Output = output.String()
		result.Duration = time.Since(start)

		if err != nil {
			result.Status = ValidationStatusFailed
			if exitErr, ok := err.(*exec.ExitError); ok {
				result.ExitCode = exitErr.ExitCode()
			}
		} else {
			result.Status = ValidationStatusPassed
			result.ExitCode = 0
		}
		return result
	}
}
