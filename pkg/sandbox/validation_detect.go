package sandbox

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ValidationCommand represents a suggested validation command
type ValidationCommand struct {
	Name       string `json:"name"`
	Command    string `json:"command"`
	Confidence string `json:"confidence"` // "high", "medium", "low"
}

// DetectedFiles tracks which validation-related files were found
type DetectedFiles struct {
	Justfile    bool `json:"justfile"`
	Makefile    bool `json:"makefile"`
	PackageJSON bool `json:"package_json"`
	CargoToml   bool `json:"cargo_toml"`
	GoMod       bool `json:"go_mod"`
	PyProject   bool `json:"pyproject_toml"`
	Gemfile     bool `json:"gemfile"`
	PomXML      bool `json:"pom_xml"`
	BuildGradle bool `json:"build_gradle"`
}

// ValidationDetection contains validation command suggestions
type ValidationDetection struct {
	Suggested     []ValidationCommand `json:"suggested"`
	DetectedFiles DetectedFiles       `json:"detected_files"`
}

// DetectValidationCommands discovers validation commands for a project
func DetectValidationCommands(workDir string, projectTypes []ProjectType) *ValidationDetection {
	result := &ValidationDetection{
		Suggested:     []ValidationCommand{},
		DetectedFiles: DetectedFiles{},
	}

	// Check for validation-related files
	result.DetectedFiles = detectValidationFiles(workDir)

	// Parse justfile for targets if present
	if result.DetectedFiles.Justfile {
		justfileCommands := parseJustfile(filepath.Join(workDir, "justfile"))
		result.Suggested = append(result.Suggested, justfileCommands...)
	}

	// Parse Makefile for targets if present
	if result.DetectedFiles.Makefile {
		makefileCommands := parseMakefile(filepath.Join(workDir, "Makefile"))
		result.Suggested = append(result.Suggested, makefileCommands...)
	}

	// Add project-type specific commands
	for _, pt := range projectTypes {
		commands := getCommandsForProjectType(pt, result.DetectedFiles)
		result.Suggested = append(result.Suggested, commands...)
	}

	// Deduplicate commands by name
	result.Suggested = deduplicateCommands(result.Suggested)

	return result
}

// detectValidationFiles checks for common build/validation files
func detectValidationFiles(workDir string) DetectedFiles {
	return DetectedFiles{
		Justfile:    fileExists(filepath.Join(workDir, "justfile")) || fileExists(filepath.Join(workDir, "Justfile")),
		Makefile:    fileExists(filepath.Join(workDir, "Makefile")) || fileExists(filepath.Join(workDir, "makefile")),
		PackageJSON: fileExists(filepath.Join(workDir, "package.json")),
		CargoToml:   fileExists(filepath.Join(workDir, "Cargo.toml")),
		GoMod:       fileExists(filepath.Join(workDir, "go.mod")),
		PyProject:   fileExists(filepath.Join(workDir, "pyproject.toml")),
		Gemfile:     fileExists(filepath.Join(workDir, "Gemfile")),
		PomXML:      fileExists(filepath.Join(workDir, "pom.xml")),
		BuildGradle: fileExists(filepath.Join(workDir, "build.gradle")) || fileExists(filepath.Join(workDir, "build.gradle.kts")),
	}
}

// getCommandsForProjectType returns standard commands for a project type
func getCommandsForProjectType(pt ProjectType, files DetectedFiles) []ValidationCommand {
	var commands []ValidationCommand

	switch pt {
	case ProjectGo:
		commands = append(commands,
			ValidationCommand{Name: "build", Command: "go build ./...", Confidence: "high"},
			ValidationCommand{Name: "test", Command: "go test ./...", Confidence: "high"},
		)
	case ProjectNodeJS:
		if files.PackageJSON {
			// Check for common npm scripts
			commands = append(commands,
				ValidationCommand{Name: "build", Command: "npm run build", Confidence: "medium"},
				ValidationCommand{Name: "test", Command: "npm test", Confidence: "medium"},
			)
		}
	case ProjectRust:
		commands = append(commands,
			ValidationCommand{Name: "build", Command: "cargo build", Confidence: "high"},
			ValidationCommand{Name: "test", Command: "cargo test", Confidence: "high"},
		)
	case ProjectPython:
		commands = append(commands,
			ValidationCommand{Name: "test", Command: "pytest", Confidence: "medium"},
		)
	case ProjectRuby:
		commands = append(commands,
			ValidationCommand{Name: "test", Command: "bundle exec rake test", Confidence: "medium"},
		)
	case ProjectJava:
		if files.PomXML {
			commands = append(commands,
				ValidationCommand{Name: "build", Command: "mvn compile", Confidence: "high"},
				ValidationCommand{Name: "test", Command: "mvn test", Confidence: "high"},
			)
		} else if files.BuildGradle {
			commands = append(commands,
				ValidationCommand{Name: "build", Command: "gradle build", Confidence: "high"},
				ValidationCommand{Name: "test", Command: "gradle test", Confidence: "high"},
			)
		}
	}

	return commands
}

// parseJustfile extracts targets from a justfile
func parseJustfile(path string) []ValidationCommand {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()

	var commands []ValidationCommand
	scanner := bufio.NewScanner(file)

	// Regex to match justfile target definitions
	// Targets look like: target_name: or target_name arg:
	targetRegex := regexp.MustCompile(`^([a-zA-Z_][a-zA-Z0-9_-]*)\s*(?:[^:]*)?:`)

	// Common validation-related target names
	validationTargets := map[string]bool{
		"build":   true,
		"test":    true,
		"check":   true,
		"lint":    true,
		"format":  true,
		"fmt":     true,
		"verify":  true,
		"compile": true,
		"run":     true,
	}

	for scanner.Scan() {
		line := scanner.Text()
		// Skip comments and empty lines
		if strings.HasPrefix(strings.TrimSpace(line), "#") || strings.TrimSpace(line) == "" {
			continue
		}

		matches := targetRegex.FindStringSubmatch(line)
		if len(matches) >= 2 {
			target := matches[1]
			// Skip private targets (starting with _)
			if strings.HasPrefix(target, "_") {
				continue
			}

			confidence := "low"
			if validationTargets[target] {
				confidence = "high"
			}

			commands = append(commands, ValidationCommand{
				Name:       target,
				Command:    "just " + target,
				Confidence: confidence,
			})
		}
	}

	return commands
}

// parseMakefile extracts targets from a Makefile
func parseMakefile(path string) []ValidationCommand {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()

	var commands []ValidationCommand
	scanner := bufio.NewScanner(file)

	// Regex to match Makefile target definitions
	// Targets look like: target_name: [dependencies]
	targetRegex := regexp.MustCompile(`^([a-zA-Z_][a-zA-Z0-9_-]*)\s*:`)

	// Common validation-related target names
	validationTargets := map[string]bool{
		"build":   true,
		"test":    true,
		"check":   true,
		"lint":    true,
		"format":  true,
		"fmt":     true,
		"verify":  true,
		"compile": true,
		"all":     true,
		"clean":   true,
	}

	for scanner.Scan() {
		line := scanner.Text()
		// Skip comments and empty lines
		if strings.HasPrefix(strings.TrimSpace(line), "#") || strings.TrimSpace(line) == "" {
			continue
		}
		// Skip lines that start with whitespace (recipe lines)
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			continue
		}

		matches := targetRegex.FindStringSubmatch(line)
		if len(matches) >= 2 {
			target := matches[1]
			// Skip special targets
			if strings.HasPrefix(target, ".") {
				continue
			}

			confidence := "low"
			if validationTargets[target] {
				confidence = "high"
			}

			commands = append(commands, ValidationCommand{
				Name:       target,
				Command:    "make " + target,
				Confidence: confidence,
			})
		}
	}

	return commands
}

// deduplicateCommands removes duplicate commands by name, preferring higher confidence
func deduplicateCommands(commands []ValidationCommand) []ValidationCommand {
	seen := make(map[string]int) // name -> index in result
	var result []ValidationCommand

	confidenceRank := map[string]int{
		"high":   3,
		"medium": 2,
		"low":    1,
	}

	for _, cmd := range commands {
		if idx, exists := seen[cmd.Name]; exists {
			// Replace if new command has higher confidence
			if confidenceRank[cmd.Confidence] > confidenceRank[result[idx].Confidence] {
				result[idx] = cmd
			}
		} else {
			seen[cmd.Name] = len(result)
			result = append(result, cmd)
		}
	}

	return result
}
