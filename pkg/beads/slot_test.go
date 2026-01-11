package beads

import (
	"os"
	"testing"
	"time"
)

func TestEncodeSlotHolder(t *testing.T) {
	holder := EncodeSlotHolder("task-123")

	// Should contain taskID, PID, and timestamp separated by |
	info := DecodeSlotHolder(holder)
	if info == nil {
		t.Fatal("DecodeSlotHolder returned nil")
	}

	if info.TaskID != "task-123" {
		t.Errorf("TaskID = %q, want %q", info.TaskID, "task-123")
	}

	if info.PID != os.Getpid() {
		t.Errorf("PID = %d, want %d", info.PID, os.Getpid())
	}

	if info.Timestamp.IsZero() {
		t.Error("Timestamp should not be zero")
	}

	// Timestamp should be recent (within last second)
	if time.Since(info.Timestamp) > time.Second {
		t.Errorf("Timestamp too old: %v", info.Timestamp)
	}
}

func TestDecodeSlotHolder_Simple(t *testing.T) {
	// Test backward compatibility with simple holder strings
	info := DecodeSlotHolder("simple-task-id")
	if info == nil {
		t.Fatal("DecodeSlotHolder returned nil")
	}

	if info.TaskID != "simple-task-id" {
		t.Errorf("TaskID = %q, want %q", info.TaskID, "simple-task-id")
	}

	if info.PID != 0 {
		t.Errorf("PID = %d, want 0 for simple holder", info.PID)
	}

	if !info.Timestamp.IsZero() {
		t.Errorf("Timestamp = %v, want zero for simple holder", info.Timestamp)
	}
}

func TestDecodeSlotHolder_Empty(t *testing.T) {
	info := DecodeSlotHolder("")
	if info != nil {
		t.Errorf("DecodeSlotHolder(%q) = %v, want nil", "", info)
	}
}

func TestDecodeSlotHolder_Full(t *testing.T) {
	// Test full format: taskID|PID|timestamp
	holder := "canopy-abc|12345|1704067200"
	info := DecodeSlotHolder(holder)

	if info == nil {
		t.Fatal("DecodeSlotHolder returned nil")
	}

	if info.TaskID != "canopy-abc" {
		t.Errorf("TaskID = %q, want %q", info.TaskID, "canopy-abc")
	}

	if info.PID != 12345 {
		t.Errorf("PID = %d, want 12345", info.PID)
	}

	expected := time.Unix(1704067200, 0)
	if !info.Timestamp.Equal(expected) {
		t.Errorf("Timestamp = %v, want %v", info.Timestamp, expected)
	}
}

func TestDecodeSlotHolder_InvalidPID(t *testing.T) {
	holder := "task|notapid|1704067200"
	info := DecodeSlotHolder(holder)

	if info == nil {
		t.Fatal("DecodeSlotHolder returned nil")
	}

	// Should fall back to just TaskID
	if info.TaskID != "task" {
		t.Errorf("TaskID = %q, want %q", info.TaskID, "task")
	}

	if info.PID != 0 {
		t.Errorf("PID = %d, want 0 for invalid PID", info.PID)
	}
}

func TestSlotHolderInfo_IsStale(t *testing.T) {
	tests := []struct {
		name     string
		info     *SlotHolderInfo
		timeout  time.Duration
		expected bool
	}{
		{
			name:     "nil info",
			info:     nil,
			timeout:  time.Minute,
			expected: false,
		},
		{
			name: "zero timestamp",
			info: &SlotHolderInfo{
				TaskID: "test",
			},
			timeout:  time.Minute,
			expected: false,
		},
		{
			name: "recent timestamp",
			info: &SlotHolderInfo{
				TaskID:    "test",
				Timestamp: time.Now(),
			},
			timeout:  time.Minute,
			expected: false,
		},
		{
			name: "stale timestamp",
			info: &SlotHolderInfo{
				TaskID:    "test",
				Timestamp: time.Now().Add(-2 * time.Minute),
			},
			timeout:  time.Minute,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.info.IsStale(tt.timeout)
			if result != tt.expected {
				t.Errorf("IsStale(%v) = %v, want %v", tt.timeout, result, tt.expected)
			}
		})
	}
}

func TestSlotHolderInfo_IsProcessDead(t *testing.T) {
	tests := []struct {
		name     string
		info     *SlotHolderInfo
		expected bool
	}{
		{
			name:     "nil info",
			info:     nil,
			expected: false,
		},
		{
			name: "zero PID",
			info: &SlotHolderInfo{
				TaskID: "test",
				PID:    0,
			},
			expected: false,
		},
		{
			name: "current process",
			info: &SlotHolderInfo{
				TaskID: "test",
				PID:    os.Getpid(),
			},
			expected: false, // Current process should be alive
		},
		{
			name: "nonexistent PID",
			info: &SlotHolderInfo{
				TaskID: "test",
				PID:    999999999, // Very unlikely to exist
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.info.IsProcessDead()
			if result != tt.expected {
				t.Errorf("IsProcessDead() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestMergeSlotResult_GetHolderInfo(t *testing.T) {
	result := &MergeSlotResult{
		Available: false,
		Holder:    "task-123|456|1704067200",
	}

	info := result.GetHolderInfo()
	if info == nil {
		t.Fatal("GetHolderInfo returned nil")
	}

	if info.TaskID != "task-123" {
		t.Errorf("TaskID = %q, want %q", info.TaskID, "task-123")
	}

	if info.PID != 456 {
		t.Errorf("PID = %d, want 456", info.PID)
	}
}
