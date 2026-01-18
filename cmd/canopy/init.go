package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jzila/canopy/pkg/sandbox"
	"github.com/jzila/canopy/pkg/validation"
	"github.com/spf13/cobra"
)

var (
	initReconfigure    bool
	initSkipBeads      bool
	initNonInteractive bool
	initDetect         bool
	initAgent          bool
	initApply          string
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize canopy workspace with sandbox configuration",
	Long: `Initializes a canopy workspace in the current directory.

This command:
1. Initializes beads (bd init) for task tracking if not already done
2. Scans the project to detect tools and languages used
3. Creates .canopy/sandbox.toml with recommended sandbox configuration

The sandbox configuration controls what paths are exposed to agents
when running with --sandbox enabled. It supports:
- Read-only tool paths (e.g., ~/.nvm, ~/.cargo)
- Config files to copy (e.g., ~/.npmrc, ~/.gitconfig)
- Cache directories for read-write access (e.g., ~/.npm)

Examples:
  # Interactive initialization
  canopy init

  # Re-run detection and reconfigure sandbox
  canopy init --reconfigure

  # Non-interactive mode (accept all defaults)
  canopy init --non-interactive

  # Skip beads initialization
  canopy init --skip-beads

  # Output detection results as JSON (for tooling integration)
  canopy init --detect

  # Output questionnaire JSON for agentic integration
  canopy init --agent

  # Apply answers from agentic questionnaire
  canopy init --apply '{"confirm_validation": "yes", ...}'`,
	RunE: runInit,
}

func init() {
	initCmd.Flags().BoolVar(&initReconfigure, "reconfigure", false, "Re-run detection and reconfigure sandbox (overwrites existing config)")
	initCmd.Flags().BoolVar(&initSkipBeads, "skip-beads", false, "Skip beads initialization")
	initCmd.Flags().BoolVar(&initNonInteractive, "non-interactive", false, "Accept all defaults without prompting")
	initCmd.Flags().BoolVarP(&initNonInteractive, "yes", "y", false, "Accept all defaults without prompting (alias for --non-interactive)")
	initCmd.Flags().BoolVar(&initDetect, "detect", false, "Output project detection results as JSON (does not create config)")
	initCmd.Flags().BoolVar(&initAgent, "agent", false, "Output questionnaire JSON for agentic integration (does not create config)")
	initCmd.Flags().StringVar(&initApply, "apply", "", "Apply answers from agentic questionnaire JSON (creates config files)")

	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	workDir, _ := os.Getwd()

	// Handle --detect flag: output JSON and exit
	if initDetect {
		return runDetect(workDir)
	}

	// Handle --agent flag: output questionnaire JSON and exit
	if initAgent {
		return runAgentQuestionnaire(workDir)
	}

	// Handle --apply flag: apply answers from JSON and create config files
	if initApply != "" {
		return runApplyAnswers(workDir, initApply)
	}

	// Step 1: Initialize beads if needed
	if !initSkipBeads {
		if _, err := os.Stat(".beads"); os.IsNotExist(err) {
			fmt.Println("Initializing beads for task tracking...")
			bdCmd := exec.Command("bd", "init")
			bdCmd.Stdout = os.Stdout
			bdCmd.Stderr = os.Stderr

			if err := bdCmd.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: bd init failed: %v\n", err)
				fmt.Println("You can run 'bd init' manually later.")
			} else {
				fmt.Println("Beads initialized.")
			}
		} else {
			fmt.Println("Beads already initialized.")
		}
	}

	// Step 2: Check for existing sandbox config
	configPath := filepath.Join(workDir, ".canopy", "sandbox.toml")

	if _, err := os.Stat(configPath); err == nil && !initReconfigure {
		fmt.Printf("\nSandbox configuration already exists at %s\n", configPath)
		fmt.Println("Use --reconfigure to regenerate the configuration.")
		return nil
	}

	// Step 3: Run interactive sandbox bootstrapper
	fmt.Println("\n" + strings.Repeat("=", 50))
	fmt.Println("Canopy Sandbox Bootstrapper")
	fmt.Println(strings.Repeat("=", 50))

	fmt.Println("\nScanning project for tool requirements...")

	// Detect project
	detection, err := sandbox.DetectProject(workDir)
	if err != nil {
		return fmt.Errorf("project detection failed: %w", err)
	}

	// Display detection results
	fmt.Printf("\nDetected Project Type: %s\n", sandbox.ProjectTypeString(detection.ProjectTypes))

	if len(detection.Tools) > 0 {
		fmt.Println("\nTools Found:")
		for _, tool := range detection.Tools {
			version := tool.Version
			if version == "" {
				version = "(version unknown)"
			}
			path := tool.Path
			if path == "" {
				path = "(path unknown)"
			}
			fmt.Printf("  [x] %s %s (%s)\n", tool.Name, version, path)
		}
	}

	// Build recommended config
	config := sandbox.DefaultSandboxConfig()
	config.Paths.ReadOnly = collapsePaths(detection.ToolPaths)
	config.Paths.CopyConfigs = collapsePaths(detection.ConfigPaths)
	config.Paths.CacheMounts = collapsePaths(detection.CachePaths)

	// Display recommended configuration
	fmt.Println("\nRecommended Sandbox Configuration:")
	fmt.Println(strings.Repeat("-", 40))

	if len(config.Paths.ReadOnly) > 0 {
		fmt.Println("\nRead-Only Tool Paths:")
		fmt.Println("  These paths will be visible to agents but not writable.")
		for _, p := range config.Paths.ReadOnly {
			fmt.Printf("  %s\n", p)
		}
	}

	if len(config.Paths.CopyConfigs) > 0 {
		fmt.Println("\nConfig Copies:")
		fmt.Println("  These files will be COPIED to each agent's workspace.")
		fmt.Println("  Agents can modify the copy but not your original.")
		for _, p := range config.Paths.CopyConfigs {
			fmt.Printf("  %s\n", p)
		}
	}

	if len(config.Paths.CacheMounts) > 0 {
		fmt.Println("\nShared Cache Mounts:")
		fmt.Println("  These are mounted read-write for performance.")
		fmt.Println("  Warning: Agents CAN modify these shared caches.")
		for _, p := range config.Paths.CacheMounts {
			fmt.Printf("  %s\n", p)
		}
	}

	fmt.Println("\nSecurity Blocklist (never exposed):")
	for _, p := range sandbox.SecurityBlocklist[:5] { // Show first 5
		fmt.Printf("  %s - BLOCKED\n", p)
	}
	if len(sandbox.SecurityBlocklist) > 5 {
		fmt.Printf("  ... and %d more\n", len(sandbox.SecurityBlocklist)-5)
	}

	// Confirm or customize
	if !initNonInteractive {
		fmt.Print("\nProceed with this configuration? [Y/n/customize] ")
		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.TrimSpace(strings.ToLower(response))

		switch response {
		case "", "y", "yes":
			// Accept defaults
		case "n", "no":
			fmt.Println("Aborted.")
			return nil
		case "c", "customize":
			config = customizeConfig(config, reader)
		default:
			fmt.Println("Unknown response, proceeding with defaults.")
		}
	}

	// Save config
	if err := sandbox.SaveConfig(workDir, config); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("\nWriting %s...\n", configPath)
	fmt.Println("Done! Run 'canopy run --sandbox' to start parallel agents with sandboxing.")
	fmt.Println("\nTip: Edit .canopy/sandbox.toml to customize paths.")
	fmt.Println("     Run 'canopy init --reconfigure' to re-run this wizard.")

	return nil
}

// collapsePaths converts absolute paths to ~ notation where applicable
func collapsePaths(paths []string) []string {
	home, _ := os.UserHomeDir()
	if home == "" {
		return paths
	}

	result := make([]string, 0, len(paths))
	for _, p := range paths {
		if strings.HasPrefix(p, home+"/") {
			p = "~" + p[len(home):]
		} else if p == home {
			p = "~"
		}
		result = append(result, p)
	}
	return result
}

// customizeConfig allows interactive customization of the config
func customizeConfig(config *sandbox.SandboxConfig, reader *bufio.Reader) *sandbox.SandboxConfig {
	fmt.Println("\n--- Customize Configuration ---")

	// Customize read-only paths
	fmt.Println("\nRead-only tool paths (comma-separated, or 'keep' to keep current):")
	fmt.Printf("Current: %s\n", strings.Join(config.Paths.ReadOnly, ", "))
	fmt.Print("> ")
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input != "" && input != "keep" {
		config.Paths.ReadOnly = splitAndTrim(input)
	}

	// Customize copy configs
	fmt.Println("\nConfig files to copy (comma-separated, or 'keep'):")
	fmt.Printf("Current: %s\n", strings.Join(config.Paths.CopyConfigs, ", "))
	fmt.Print("> ")
	input, _ = reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input != "" && input != "keep" {
		config.Paths.CopyConfigs = splitAndTrim(input)
	}

	// Customize cache mounts
	fmt.Println("\nCache mounts (comma-separated, or 'keep'):")
	fmt.Printf("Current: %s\n", strings.Join(config.Paths.CacheMounts, ", "))
	fmt.Print("> ")
	input, _ = reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input != "" && input != "keep" {
		config.Paths.CacheMounts = splitAndTrim(input)
	}

	// Network policy
	fmt.Println("\nNetwork policy (allow/deny):")
	fmt.Printf("Current: %s\n", config.Sandbox.Network)
	fmt.Print("> ")
	input, _ = reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))
	if input == "allow" || input == "deny" {
		config.Sandbox.Network = input
	}

	return config
}

func splitAndTrim(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// DetectOutput represents the JSON output for --detect flag
type DetectOutput struct {
	Project    ProjectInfo    `json:"project"`
	Sandbox    SandboxInfo    `json:"sandbox"`
	Validation ValidationInfo `json:"validation"`
}

// ProjectInfo contains detected project information
type ProjectInfo struct {
	Type    string   `json:"type"`
	Root    string   `json:"root"`
	Markers []string `json:"markers"`
}

// SandboxInfo contains sandbox configuration suggestions
type SandboxInfo struct {
	Tools   []string `json:"tools"`
	Configs []string `json:"configs"`
	Caches  []string `json:"caches"`
}

// ValidationInfo contains validation command suggestions
type ValidationInfo struct {
	Suggested     []sandbox.ValidationCommand `json:"suggested"`
	DetectedFiles sandbox.DetectedFiles       `json:"detected_files"`
}

// runDetect performs project detection and outputs JSON
func runDetect(workDir string) error {
	// Detect project type and tools
	detection, err := sandbox.DetectProject(workDir)
	if err != nil {
		return fmt.Errorf("project detection failed: %w", err)
	}

	// Detect validation commands
	validationDetection := sandbox.DetectValidationCommands(workDir, detection.ProjectTypes)

	// Build project markers list
	markers := getProjectMarkers(workDir, detection.ProjectTypes)

	// Build output structure
	output := DetectOutput{
		Project: ProjectInfo{
			Type:    sandbox.ProjectTypeString(detection.ProjectTypes),
			Root:    workDir,
			Markers: markers,
		},
		Sandbox: SandboxInfo{
			Tools:   collapsePaths(detection.ToolPaths),
			Configs: collapsePaths(detection.ConfigPaths),
			Caches:  collapsePaths(detection.CachePaths),
		},
		Validation: ValidationInfo{
			Suggested:     validationDetection.Suggested,
			DetectedFiles: validationDetection.DetectedFiles,
		},
	}

	// Output as JSON
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(output)
}

// getProjectMarkers returns the file markers that were found for detected project types
func getProjectMarkers(workDir string, projectTypes []sandbox.ProjectType) []string {
	var markers []string

	markerFiles := map[sandbox.ProjectType][]string{
		sandbox.ProjectNodeJS: {"package.json"},
		sandbox.ProjectRust:   {"Cargo.toml"},
		sandbox.ProjectGo:     {"go.mod", "go.sum"},
		sandbox.ProjectPython: {"pyproject.toml", "requirements.txt", "setup.py", "Pipfile"},
		sandbox.ProjectRuby:   {"Gemfile"},
		sandbox.ProjectJava:   {"pom.xml", "build.gradle", "build.gradle.kts"},
	}

	for _, pt := range projectTypes {
		if files, ok := markerFiles[pt]; ok {
			for _, f := range files {
				if _, err := os.Stat(filepath.Join(workDir, f)); err == nil {
					markers = append(markers, f)
				}
			}
		}
	}

	return markers
}

// AgentQuestionnaireOutput represents the JSON output for --agent flag
type AgentQuestionnaireOutput struct {
	Detection DetectOutput  `json:"detection"`
	Questions []Question    `json:"questions"`
}

// Question represents a single question in the questionnaire
type Question struct {
	ID        string           `json:"id"`
	Question  string           `json:"question"`
	Type      string           `json:"type"` // "single_select" or "freeform"
	Options   []QuestionOption `json:"options,omitempty"`
	Default   string           `json:"default,omitempty"`
	DependsOn *Dependency      `json:"depends_on,omitempty"`
}

// QuestionOption represents an option for single_select questions
type QuestionOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Dependency represents a conditional dependency for a question
type Dependency struct {
	QuestionID string `json:"question_id"`
	Value      string `json:"value"`
}

// ApplyAnswers represents the expected JSON input for --apply flag
type ApplyAnswers struct {
	ConfirmValidation string   `json:"confirm_validation"`
	ValidationMode    string   `json:"validation_mode"`
	ExtraCommands     string   `json:"extra_commands"`
	ValidationSteps   []string `json:"validation_steps"`
}

// runAgentQuestionnaire performs project detection and outputs questionnaire JSON
func runAgentQuestionnaire(workDir string) error {
	// Detect project type and tools
	detection, err := sandbox.DetectProject(workDir)
	if err != nil {
		return fmt.Errorf("project detection failed: %w", err)
	}

	// Detect validation commands
	validationDetection := sandbox.DetectValidationCommands(workDir, detection.ProjectTypes)

	// Build project markers list
	markers := getProjectMarkers(workDir, detection.ProjectTypes)

	// Build detection output
	detectOutput := DetectOutput{
		Project: ProjectInfo{
			Type:    sandbox.ProjectTypeString(detection.ProjectTypes),
			Root:    workDir,
			Markers: markers,
		},
		Sandbox: SandboxInfo{
			Tools:   collapsePaths(detection.ToolPaths),
			Configs: collapsePaths(detection.ConfigPaths),
			Caches:  collapsePaths(detection.CachePaths),
		},
		Validation: ValidationInfo{
			Suggested:     validationDetection.Suggested,
			DetectedFiles: validationDetection.DetectedFiles,
		},
	}

	// Build questions based on detection
	questions := buildQuestionnaire(validationDetection)

	// Build output structure
	output := AgentQuestionnaireOutput{
		Detection: detectOutput,
		Questions: questions,
	}

	// Output as JSON
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(output)
}

// buildQuestionnaire constructs the questionnaire based on detection results
func buildQuestionnaire(validationDetection *sandbox.ValidationDetection) []Question {
	questions := []Question{}

	// Question 1: Confirm validation
	hasValidationCommands := len(validationDetection.Suggested) > 0
	confirmQuestion := "Enable post-merge validation?"
	if hasValidationCommands {
		confirmQuestion = "I detected build and test commands. Enable post-merge validation?"
	}

	questions = append(questions, Question{
		ID:       "confirm_validation",
		Question: confirmQuestion,
		Type:     "single_select",
		Options: []QuestionOption{
			{Value: "yes", Label: "Yes, validate after each merge"},
			{Value: "no", Label: "No, skip validation"},
		},
		Default: "yes",
	})

	// Question 2: Validation mode (depends on confirm_validation=yes)
	questions = append(questions, Question{
		ID:       "validation_mode",
		Question: "How should validation failures be handled?",
		Type:     "single_select",
		Options: []QuestionOption{
			{Value: "strict", Label: "Strict - revert merge on failure"},
			{Value: "lenient", Label: "Lenient - file issue, keep merge"},
		},
		Default: "strict",
		DependsOn: &Dependency{
			QuestionID: "confirm_validation",
			Value:      "yes",
		},
	})

	// Question 3: Select validation steps (depends on confirm_validation=yes)
	if hasValidationCommands {
		// Build options from detected commands
		var options []QuestionOption
		for _, cmd := range validationDetection.Suggested {
			label := fmt.Sprintf("%s (%s)", cmd.Name, cmd.Command)
			options = append(options, QuestionOption{
				Value: cmd.Name,
				Label: label,
			})
		}

		// Only add question if there are detected commands
		if len(options) > 0 {
			questions = append(questions, Question{
				ID:       "validation_steps",
				Question: "Which validation steps should run after each merge?",
				Type:     "multi_select",
				Options:  options,
				DependsOn: &Dependency{
					QuestionID: "confirm_validation",
					Value:      "yes",
				},
			})
		}
	}

	// Question 4: Extra commands (freeform, depends on confirm_validation=yes)
	questions = append(questions, Question{
		ID:       "extra_commands",
		Question: "Any additional validation commands? (comma-separated, or leave empty)",
		Type:     "freeform",
		DependsOn: &Dependency{
			QuestionID: "confirm_validation",
			Value:      "yes",
		},
	})

	return questions
}

// runApplyAnswers applies the questionnaire answers and creates config files
func runApplyAnswers(workDir string, answersJSON string) error {
	// Parse the answers JSON
	var answers ApplyAnswers
	if err := json.Unmarshal([]byte(answersJSON), &answers); err != nil {
		return fmt.Errorf("failed to parse answers JSON: %w", err)
	}

	// Initialize beads if needed
	if !initSkipBeads {
		if _, err := os.Stat(filepath.Join(workDir, ".beads")); os.IsNotExist(err) {
			fmt.Fprintln(os.Stderr, "Initializing beads for task tracking...")
			bdCmd := exec.Command("bd", "init")
			bdCmd.Dir = workDir
			bdCmd.Stdout = os.Stderr // Send to stderr so JSON stdout is clean
			bdCmd.Stderr = os.Stderr

			if err := bdCmd.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: bd init failed: %v\n", err)
				fmt.Fprintln(os.Stderr, "You can run 'bd init' manually later.")
			}
		}
	}

	// Detect project to get tool paths for sandbox config
	detection, err := sandbox.DetectProject(workDir)
	if err != nil {
		return fmt.Errorf("project detection failed: %w", err)
	}

	// Create sandbox config
	sandboxConfig := sandbox.DefaultSandboxConfig()
	sandboxConfig.Paths.ReadOnly = collapsePaths(detection.ToolPaths)
	sandboxConfig.Paths.CopyConfigs = collapsePaths(detection.ConfigPaths)
	sandboxConfig.Paths.CacheMounts = collapsePaths(detection.CachePaths)

	if err := sandbox.SaveConfig(workDir, sandboxConfig); err != nil {
		return fmt.Errorf("failed to save sandbox config: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Created .canopy/sandbox.toml\n")

	// Create validation config if enabled
	if answers.ConfirmValidation == "yes" {
		validationConfig := validation.DefaultValidationConfig()
		validationConfig.Validation.Enabled = true
		validationConfig.Validation.Strict = answers.ValidationMode == "strict"

		// Get validation detection for command mapping
		validationDetection := sandbox.DetectValidationCommands(workDir, detection.ProjectTypes)
		commandMap := make(map[string]string)
		for _, cmd := range validationDetection.Suggested {
			commandMap[cmd.Name] = cmd.Command
		}

		// Add selected validation steps
		for _, stepName := range answers.ValidationSteps {
			if cmd, ok := commandMap[stepName]; ok {
				validationConfig.Validation.Steps = append(validationConfig.Validation.Steps, validation.StepConfig{
					Name:     stepName,
					Command:  cmd,
					Required: true,
				})
			}
		}

		// Add extra commands
		if answers.ExtraCommands != "" {
			extras := splitAndTrim(answers.ExtraCommands)
			for _, extra := range extras {
				validationConfig.Validation.Steps = append(validationConfig.Validation.Steps, validation.StepConfig{
					Name:     extractCommandName(extra),
					Command:  extra,
					Required: false,
				})
			}
		}

		if err := validation.SaveConfig(workDir, validationConfig); err != nil {
			return fmt.Errorf("failed to save validation config: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Created .canopy/validation.toml\n")
	}

	// Output success message
	result := map[string]interface{}{
		"success": true,
		"files_created": []string{
			".canopy/sandbox.toml",
		},
	}

	if answers.ConfirmValidation == "yes" {
		result["files_created"] = append(result["files_created"].([]string), ".canopy/validation.toml")
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

// extractCommandName extracts a short name from a command
func extractCommandName(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return "custom"
	}
	// Use first word as base, strip any path
	name := filepath.Base(parts[0])
	// If there's a subcommand, append it
	if len(parts) > 1 && !strings.HasPrefix(parts[1], "-") {
		name = name + "_" + parts[1]
	}
	return name
}
