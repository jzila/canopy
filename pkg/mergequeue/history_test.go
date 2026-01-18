package mergequeue

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jzila/canopy/pkg/beads"
	"github.com/jzila/canopy/pkg/validation"
)

func TestHistoryRecorder_RecordValidationFailure(t *testing.T) {
	tests := []struct {
		name       string
		result     *validation.Result
		attemptNum int
		wantErr    bool
		checkFn    func(t *testing.T, comment string)
	}{
		{
			name:       "nil result",
			result:     nil,
			attemptNum: 1,
			wantErr:    false,
		},
		{
			name: "simple failure",
			result: &validation.Result{
				Status:     validation.ValidationStatusFailed,
				FailedStep: "test",
				Error:      "test failed with exit code 1",
				Duration:   5 * time.Second,
			},
			attemptNum: 1,
			wantErr:    false,
			checkFn: func(t *testing.T, comment string) {
				if !strings.Contains(comment, "Validation Failed (Attempt 1)") {
					t.Error("expected attempt number in comment")
				}
				if !strings.Contains(comment, "`test`") {
					t.Error("expected failed step name in comment")
				}
				if !strings.Contains(comment, "test failed with exit code 1") {
					t.Error("expected error message in comment")
				}
			},
		},
		{
			name: "failure with steps",
			result: &validation.Result{
				Status:     validation.ValidationStatusFailed,
				FailedStep: "build",
				Error:      "build step failed",
				Duration:   10 * time.Second,
				Steps: []validation.StepResult{
					{
						Name:     "lint",
						Status:   validation.ValidationStatusPassed,
						Duration: 2 * time.Second,
						ExitCode: 0,
					},
					{
						Name:     "build",
						Status:   validation.ValidationStatusFailed,
						Duration: 8 * time.Second,
						ExitCode: 1,
						Output:   "compilation error: undefined variable",
					},
				},
			},
			attemptNum: 2,
			wantErr:    false,
			checkFn: func(t *testing.T, comment string) {
				if !strings.Contains(comment, "Attempt 2") {
					t.Error("expected attempt number 2")
				}
				if !strings.Contains(comment, "✅") || !strings.Contains(comment, "lint") {
					t.Error("expected passed lint step with checkmark")
				}
				if !strings.Contains(comment, "❌") || !strings.Contains(comment, "build") {
					t.Error("expected failed build step with X")
				}
				if !strings.Contains(comment, "compilation error") {
					t.Error("expected output in failed step")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := beads.NewMockClient()
			recorder := NewHistoryRecorder(mock, false)

			err := recorder.RecordValidationFailure(context.Background(), "task-1", tt.result, tt.attemptNum)

			if (err != nil) != tt.wantErr {
				t.Errorf("RecordValidationFailure() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.result == nil {
				if len(mock.Calls.AddComment) != 0 {
					t.Error("expected no AddComment call for nil result")
				}
				return
			}

			if len(mock.Calls.AddComment) != 1 {
				t.Fatalf("expected 1 AddComment call, got %d", len(mock.Calls.AddComment))
			}

			call := mock.Calls.AddComment[0]
			if call.TaskID != "task-1" {
				t.Errorf("expected taskID 'task-1', got %q", call.TaskID)
			}

			if tt.checkFn != nil {
				tt.checkFn(t, call.Comment)
			}
		})
	}
}

func TestHistoryRecorder_RecordValidationFailure_Error(t *testing.T) {
	mock := beads.NewMockClient()
	mock.Errors.AddComment = errors.New("beads error")
	recorder := NewHistoryRecorder(mock, false)

	result := &validation.Result{
		Status:     validation.ValidationStatusFailed,
		FailedStep: "test",
	}

	err := recorder.RecordValidationFailure(context.Background(), "task-1", result, 1)
	if err == nil {
		t.Error("expected error when AddComment fails")
	}
	if !strings.Contains(err.Error(), "beads error") {
		t.Errorf("expected wrapped error, got: %v", err)
	}
}

func TestHistoryRecorder_RecordRepairAttempt(t *testing.T) {
	tests := []struct {
		name    string
		attempt *RepairAttempt
		wantErr bool
		checkFn func(t *testing.T, comment string)
	}{
		{
			name:    "nil attempt",
			attempt: nil,
			wantErr: false,
		},
		{
			name: "successful repair",
			attempt: &RepairAttempt{
				Number:         1,
				Status:         "success",
				Duration:       30 * time.Second,
				AgentID:        "agent-abc123-task-1",
				CommitsApplied: 2,
			},
			wantErr: false,
			checkFn: func(t *testing.T, comment string) {
				if !strings.Contains(comment, "✅ Repair Attempt 1") {
					t.Error("expected success emoji and attempt number")
				}
				if !strings.Contains(comment, "agent-abc123-task-1") {
					t.Error("expected agent ID")
				}
				if !strings.Contains(comment, "Commits Applied:** 2") {
					t.Error("expected commits applied count")
				}
			},
		},
		{
			name: "failed repair",
			attempt: &RepairAttempt{
				Number:   2,
				Status:   "failed",
				Duration: 45 * time.Second,
				Error:    "could not resolve merge conflict",
				Output:   "CONFLICT (content): Merge conflict in main.go",
			},
			wantErr: false,
			checkFn: func(t *testing.T, comment string) {
				if !strings.Contains(comment, "❌ Repair Attempt 2") {
					t.Error("expected failure emoji and attempt number")
				}
				if !strings.Contains(comment, "could not resolve merge conflict") {
					t.Error("expected error message")
				}
				if !strings.Contains(comment, "CONFLICT") {
					t.Error("expected output")
				}
			},
		},
		{
			name: "timeout repair",
			attempt: &RepairAttempt{
				Number:   3,
				Status:   "timeout",
				Duration: 10 * time.Minute,
				Error:    "repair timeout after 10m",
			},
			wantErr: false,
			checkFn: func(t *testing.T, comment string) {
				if !strings.Contains(comment, "⏱️ Repair Attempt 3") {
					t.Error("expected timeout emoji and attempt number")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := beads.NewMockClient()
			recorder := NewHistoryRecorder(mock, false)

			err := recorder.RecordRepairAttempt(context.Background(), "task-1", tt.attempt)

			if (err != nil) != tt.wantErr {
				t.Errorf("RecordRepairAttempt() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.attempt == nil {
				if len(mock.Calls.AddComment) != 0 {
					t.Error("expected no AddComment call for nil attempt")
				}
				return
			}

			if len(mock.Calls.AddComment) != 1 {
				t.Fatalf("expected 1 AddComment call, got %d", len(mock.Calls.AddComment))
			}

			if tt.checkFn != nil {
				tt.checkFn(t, mock.Calls.AddComment[0].Comment)
			}
		})
	}
}

func TestHistoryRecorder_RecordFinalStatus(t *testing.T) {
	tests := []struct {
		name    string
		status  FinalValidationStatus
		details string
		checkFn func(t *testing.T, comment string)
	}{
		{
			name:    "validated",
			status:  FinalStatusValidated,
			details: "",
			checkFn: func(t *testing.T, comment string) {
				if !strings.Contains(comment, "✅ Validation Passed") {
					t.Error("expected validation passed header")
				}
			},
		},
		{
			name:    "repaired",
			status:  FinalStatusRepaired,
			details: "Fixed by repair agent after 2 attempts",
			checkFn: func(t *testing.T, comment string) {
				if !strings.Contains(comment, "✅ Repaired Successfully") {
					t.Error("expected repaired header")
				}
				if !strings.Contains(comment, "2 attempts") {
					t.Error("expected details")
				}
			},
		},
		{
			name:    "needs manual fix",
			status:  FinalStatusNeedsManualFix,
			details: "See validation failures above for details",
			checkFn: func(t *testing.T, comment string) {
				if !strings.Contains(comment, "❌ Needs Manual Repair") {
					t.Error("expected needs manual repair header")
				}
				if !strings.Contains(comment, "Manual intervention required") {
					t.Error("expected manual intervention message")
				}
			},
		},
		{
			name:    "skipped",
			status:  FinalStatusSkipped,
			details: "",
			checkFn: func(t *testing.T, comment string) {
				if !strings.Contains(comment, "⏭️ Validation Skipped") {
					t.Error("expected skipped header")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := beads.NewMockClient()
			recorder := NewHistoryRecorder(mock, false)

			err := recorder.RecordFinalStatus(context.Background(), "task-1", tt.status, tt.details)
			if err != nil {
				t.Errorf("RecordFinalStatus() error = %v", err)
				return
			}

			if len(mock.Calls.AddComment) != 1 {
				t.Fatalf("expected 1 AddComment call, got %d", len(mock.Calls.AddComment))
			}

			if tt.checkFn != nil {
				tt.checkFn(t, mock.Calls.AddComment[0].Comment)
			}
		})
	}
}

func TestTruncateOutput(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{
			name:   "short string",
			input:  "hello",
			maxLen: 10,
			want:   "hello",
		},
		{
			name:   "exact length",
			input:  "hello",
			maxLen: 5,
			want:   "hello",
		},
		{
			name:   "long string",
			input:  "hello world",
			maxLen: 5,
			want:   "hello\n... (truncated)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateOutput(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateOutput() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIndentOutput(t *testing.T) {
	input := "line1\nline2\nline3"
	want := "  line1\n  line2\n  line3"
	got := indentOutput(input, "  ")
	if got != want {
		t.Errorf("indentOutput() = %q, want %q", got, want)
	}
}
