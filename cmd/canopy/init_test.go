package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jzila/canopy/pkg/sandbox"
)

func TestBuildQuestionnaire(t *testing.T) {
	t.Run("with validation commands", func(t *testing.T) {
		detection := &sandbox.ValidationDetection{
			Suggested: []sandbox.ValidationCommand{
				{Name: "build", Command: "go build ./...", Confidence: "high"},
				{Name: "test", Command: "go test ./...", Confidence: "high"},
			},
		}

		questions := buildQuestionnaire(detection)

		// Should have 4 questions: confirm, mode, steps, extra
		if len(questions) != 4 {
			t.Errorf("expected 4 questions, got %d", len(questions))
		}

		// First question should mention detected commands
		if questions[0].Question != "I detected build and test commands. Enable post-merge validation?" {
			t.Errorf("unexpected first question: %s", questions[0].Question)
		}

		// Third question should be validation_steps with options
		if questions[2].ID != "validation_steps" {
			t.Errorf("expected validation_steps question at index 2, got %s", questions[2].ID)
		}
		if len(questions[2].Options) != 2 {
			t.Errorf("expected 2 options for validation_steps, got %d", len(questions[2].Options))
		}
	})

	t.Run("without validation commands", func(t *testing.T) {
		detection := &sandbox.ValidationDetection{
			Suggested: []sandbox.ValidationCommand{},
		}

		questions := buildQuestionnaire(detection)

		// Should have 3 questions: confirm, mode, extra (no steps question)
		if len(questions) != 3 {
			t.Errorf("expected 3 questions, got %d", len(questions))
		}

		// First question should not mention detected commands
		if questions[0].Question != "Enable post-merge validation?" {
			t.Errorf("unexpected first question: %s", questions[0].Question)
		}

		// Should not have validation_steps question
		for _, q := range questions {
			if q.ID == "validation_steps" {
				t.Error("should not have validation_steps question when no commands detected")
			}
		}
	})

	t.Run("question dependencies", func(t *testing.T) {
		detection := &sandbox.ValidationDetection{
			Suggested: []sandbox.ValidationCommand{
				{Name: "build", Command: "go build ./...", Confidence: "high"},
			},
		}

		questions := buildQuestionnaire(detection)

		// All questions except first should depend on confirm_validation=yes
		for i, q := range questions {
			if i == 0 {
				if q.DependsOn != nil {
					t.Error("first question should not have dependency")
				}
				continue
			}
			if q.DependsOn == nil {
				t.Errorf("question %s should have dependency", q.ID)
				continue
			}
			if q.DependsOn.QuestionID != "confirm_validation" {
				t.Errorf("question %s should depend on confirm_validation, got %s", q.ID, q.DependsOn.QuestionID)
			}
			if q.DependsOn.Value != "yes" {
				t.Errorf("question %s should depend on value yes, got %s", q.ID, q.DependsOn.Value)
			}
		}
	})
}

func TestRunApplyAnswers(t *testing.T) {
	// Set up test to skip beads
	initSkipBeads = true
	defer func() { initSkipBeads = false }()

	t.Run("validation enabled with strict mode", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create a simple Go project
		err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module testproj"), 0644)
		if err != nil {
			t.Fatal(err)
		}

		answers := ApplyAnswers{
			ConfirmValidation: "yes",
			ValidationMode:    "strict",
			ValidationSteps:   []string{"build", "test"},
			ExtraCommands:     "golangci-lint run",
		}
		answersJSON, _ := json.Marshal(answers)

		// Capture output
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		err = runApplyAnswers(tmpDir, string(answersJSON))

		_ = w.Close()
		os.Stdout = oldStdout

		if err != nil {
			t.Fatalf("runApplyAnswers failed: %v", err)
		}

		// Read the output
		var buf [4096]byte
		n, _ := r.Read(buf[:])
		output := string(buf[:n])

		// Parse output
		var result map[string]interface{}
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			t.Fatalf("failed to parse output: %v", err)
		}

		if result["success"] != true {
			t.Error("expected success to be true")
		}

		files := result["files_created"].([]interface{})
		if len(files) != 1 {
			t.Errorf("expected 1 file created (consolidated config.toml), got %d", len(files))
		}

		// Verify config.toml exists
		configPath := filepath.Join(tmpDir, ".canopy", "config.toml")
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			t.Error("config.toml was not created")
		}

		// Read and verify config
		data, _ := os.ReadFile(configPath)
		content := string(data)
		if !contains(content, "enabled = true") {
			t.Error("validation should be enabled")
		}
		if !contains(content, "strict = true") {
			t.Error("strict mode should be enabled")
		}
	})

	t.Run("validation disabled", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create a simple Go project
		err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module testproj"), 0644)
		if err != nil {
			t.Fatal(err)
		}

		answers := ApplyAnswers{
			ConfirmValidation: "no",
		}
		answersJSON, _ := json.Marshal(answers)

		// Capture output
		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		err = runApplyAnswers(tmpDir, string(answersJSON))

		_ = w.Close()
		os.Stdout = oldStdout

		if err != nil {
			t.Fatalf("runApplyAnswers failed: %v", err)
		}

		// Read the output
		var buf [4096]byte
		n, _ := r.Read(buf[:])
		output := string(buf[:n])

		// Parse output
		var result map[string]interface{}
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			t.Fatalf("failed to parse output: %v", err)
		}

		files := result["files_created"].([]interface{})
		if len(files) != 1 {
			t.Errorf("expected 1 file created, got %d", len(files))
		}

		// Verify config.toml exists
		configPath := filepath.Join(tmpDir, ".canopy", "config.toml")
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			t.Error("config.toml was not created")
		}

		// Verify config.toml has validation section with enabled = false
		data, _ := os.ReadFile(configPath)
		content := string(data)
		// Check that the validation section exists and enabled is false
		if !contains(content, "[validation]") {
			t.Error("config.toml should contain [validation] section")
		}
		// Look for the validation.enabled field specifically (note: sandbox.enabled = true is separate)
		// The TOML file will have the validation section with enabled = false
		validationSection := ""
		lines := strings.Split(content, "\n")
		inValidation := false
		for _, line := range lines {
			if line == "[validation]" {
				inValidation = true
				continue
			}
			if inValidation && strings.HasPrefix(line, "[") {
				break
			}
			if inValidation {
				validationSection += line + "\n"
			}
		}
		if contains(validationSection, "enabled = true") {
			t.Error("validation.enabled should be false")
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		tmpDir := t.TempDir()

		err := runApplyAnswers(tmpDir, "not valid json")
		if err == nil {
			t.Error("expected error for invalid JSON")
		}
		if !contains(err.Error(), "failed to parse answers JSON") {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}

func TestExtractCommandName(t *testing.T) {
	tests := []struct {
		cmd      string
		expected string
	}{
		{"golangci-lint run", "golangci-lint_run"},
		{"npm test", "npm_test"},
		{"go build ./...", "go_build"},
		{"make", "make"},
		{"", "custom"},
		{"  ", "custom"},
		{"/usr/bin/pytest", "pytest"},
		{"go test -v ./...", "go_test"},
	}

	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			result := extractCommandName(tt.cmd)
			if result != tt.expected {
				t.Errorf("extractCommandName(%q) = %q, want %q", tt.cmd, result, tt.expected)
			}
		})
	}
}

func TestAgentQuestionnaireOutput(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a Go project with a justfile
	err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module testproj"), 0644)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(tmpDir, "justfile"), []byte("build:\n\tgo build\n\ntest:\n\tgo test ./...\n"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Change to the temp directory
	oldWd, _ := os.Getwd()
	err = os.Chdir(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(oldWd) }()

	// Capture output
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err = runAgentQuestionnaire(tmpDir)

	_ = w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("runAgentQuestionnaire failed: %v", err)
	}

	// Read the output
	var buf [16384]byte
	n, _ := r.Read(buf[:])
	output := string(buf[:n])

	// Parse output
	var result AgentQuestionnaireOutput
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("failed to parse output: %v", err)
	}

	// Verify detection
	if result.Detection.Project.Type != "Go" {
		t.Errorf("expected project type Go, got %s", result.Detection.Project.Type)
	}

	// Verify questions exist
	if len(result.Questions) < 3 {
		t.Errorf("expected at least 3 questions, got %d", len(result.Questions))
	}

	// Verify first question is confirm_validation
	if result.Questions[0].ID != "confirm_validation" {
		t.Errorf("first question should be confirm_validation, got %s", result.Questions[0].ID)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
