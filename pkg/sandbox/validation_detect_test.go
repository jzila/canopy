package sandbox

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectValidationFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "validation-detect-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	tests := []struct {
		name     string
		files    []string
		expected DetectedFiles
	}{
		{
			name:  "empty directory",
			files: []string{},
			expected: DetectedFiles{
				Justfile:    false,
				Makefile:    false,
				PackageJSON: false,
				CargoToml:   false,
				GoMod:       false,
				PyProject:   false,
				Gemfile:     false,
				PomXML:      false,
				BuildGradle: false,
			},
		},
		{
			name:  "go project",
			files: []string{"go.mod"},
			expected: DetectedFiles{
				GoMod: true,
			},
		},
		{
			name:  "node project with justfile",
			files: []string{"package.json", "justfile"},
			expected: DetectedFiles{
				PackageJSON: true,
				Justfile:    true,
			},
		},
		{
			name:  "rust project with Makefile",
			files: []string{"Cargo.toml", "Makefile"},
			expected: DetectedFiles{
				CargoToml: true,
				Makefile:  true,
			},
		},
		{
			name:  "java maven project",
			files: []string{"pom.xml"},
			expected: DetectedFiles{
				PomXML: true,
			},
		},
		{
			name:  "java gradle project",
			files: []string{"build.gradle"},
			expected: DetectedFiles{
				BuildGradle: true,
			},
		},
		{
			name:  "python project",
			files: []string{"pyproject.toml"},
			expected: DetectedFiles{
				PyProject: true,
			},
		},
		{
			name:  "ruby project",
			files: []string{"Gemfile"},
			expected: DetectedFiles{
				Gemfile: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a subdirectory for each test
			testDir := filepath.Join(tmpDir, tt.name)
			if err := os.MkdirAll(testDir, 0755); err != nil {
				t.Fatalf("failed to create test dir: %v", err)
			}

			// Create test files
			for _, f := range tt.files {
				filePath := filepath.Join(testDir, f)
				if err := os.WriteFile(filePath, []byte(""), 0644); err != nil {
					t.Fatalf("failed to create test file %s: %v", f, err)
				}
			}

			result := detectValidationFiles(testDir)

			if result.Justfile != tt.expected.Justfile {
				t.Errorf("Justfile: got %v, want %v", result.Justfile, tt.expected.Justfile)
			}
			if result.Makefile != tt.expected.Makefile {
				t.Errorf("Makefile: got %v, want %v", result.Makefile, tt.expected.Makefile)
			}
			if result.PackageJSON != tt.expected.PackageJSON {
				t.Errorf("PackageJSON: got %v, want %v", result.PackageJSON, tt.expected.PackageJSON)
			}
			if result.CargoToml != tt.expected.CargoToml {
				t.Errorf("CargoToml: got %v, want %v", result.CargoToml, tt.expected.CargoToml)
			}
			if result.GoMod != tt.expected.GoMod {
				t.Errorf("GoMod: got %v, want %v", result.GoMod, tt.expected.GoMod)
			}
			if result.PyProject != tt.expected.PyProject {
				t.Errorf("PyProject: got %v, want %v", result.PyProject, tt.expected.PyProject)
			}
			if result.Gemfile != tt.expected.Gemfile {
				t.Errorf("Gemfile: got %v, want %v", result.Gemfile, tt.expected.Gemfile)
			}
			if result.PomXML != tt.expected.PomXML {
				t.Errorf("PomXML: got %v, want %v", result.PomXML, tt.expected.PomXML)
			}
			if result.BuildGradle != tt.expected.BuildGradle {
				t.Errorf("BuildGradle: got %v, want %v", result.BuildGradle, tt.expected.BuildGradle)
			}
		})
	}
}

func TestGetCommandsForProjectType(t *testing.T) {
	tests := []struct {
		name        string
		projectType ProjectType
		files       DetectedFiles
		wantNames   []string
	}{
		{
			name:        "go project",
			projectType: ProjectGo,
			files:       DetectedFiles{GoMod: true},
			wantNames:   []string{"build", "test"},
		},
		{
			name:        "rust project",
			projectType: ProjectRust,
			files:       DetectedFiles{CargoToml: true},
			wantNames:   []string{"build", "test"},
		},
		{
			name:        "nodejs project",
			projectType: ProjectNodeJS,
			files:       DetectedFiles{PackageJSON: true},
			wantNames:   []string{"build", "test"},
		},
		{
			name:        "python project",
			projectType: ProjectPython,
			files:       DetectedFiles{PyProject: true},
			wantNames:   []string{"test"},
		},
		{
			name:        "java maven project",
			projectType: ProjectJava,
			files:       DetectedFiles{PomXML: true},
			wantNames:   []string{"build", "test"},
		},
		{
			name:        "java gradle project",
			projectType: ProjectJava,
			files:       DetectedFiles{BuildGradle: true},
			wantNames:   []string{"build", "test"},
		},
		{
			name:        "ruby project",
			projectType: ProjectRuby,
			files:       DetectedFiles{Gemfile: true},
			wantNames:   []string{"test"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			commands := getCommandsForProjectType(tt.projectType, tt.files)

			if len(commands) != len(tt.wantNames) {
				t.Errorf("got %d commands, want %d", len(commands), len(tt.wantNames))
				return
			}

			for i, cmd := range commands {
				if cmd.Name != tt.wantNames[i] {
					t.Errorf("command %d: got name %q, want %q", i, cmd.Name, tt.wantNames[i])
				}
				if cmd.Confidence == "" {
					t.Errorf("command %d: confidence should not be empty", i)
				}
				if cmd.Command == "" {
					t.Errorf("command %d: command should not be empty", i)
				}
			}
		})
	}
}

func TestParseJustfile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "justfile-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	tests := []struct {
		name     string
		content  string
		expected []ValidationCommand
	}{
		{
			name: "simple targets",
			content: `# Comment line
build:
    go build ./...

test:
    go test ./...
`,
			expected: []ValidationCommand{
				{Name: "build", Command: "just build", Confidence: "high"},
				{Name: "test", Command: "just test", Confidence: "high"},
			},
		},
		{
			name: "targets with arguments",
			content: `run arg:
    ./app {{arg}}

deploy env="prod":
    ./deploy.sh {{env}}
`,
			expected: []ValidationCommand{
				{Name: "run", Command: "just run", Confidence: "high"}, // "run" is in validationTargets
				{Name: "deploy", Command: "just deploy", Confidence: "low"},
			},
		},
		{
			name: "skip private targets",
			content: `_private:
    echo "private"

public:
    echo "public"
`,
			expected: []ValidationCommand{
				{Name: "public", Command: "just public", Confidence: "low"},
			},
		},
		{
			name: "validation targets",
			content: `lint:
    golangci-lint run

format:
    go fmt ./...

check:
    make check
`,
			expected: []ValidationCommand{
				{Name: "lint", Command: "just lint", Confidence: "high"},
				{Name: "format", Command: "just format", Confidence: "high"},
				{Name: "check", Command: "just check", Confidence: "high"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filePath := filepath.Join(tmpDir, "justfile_"+tt.name)
			if err := os.WriteFile(filePath, []byte(tt.content), 0644); err != nil {
				t.Fatalf("failed to write justfile: %v", err)
			}

			commands := parseJustfile(filePath)

			if len(commands) != len(tt.expected) {
				t.Errorf("got %d commands, want %d", len(commands), len(tt.expected))
				for _, c := range commands {
					t.Logf("  got: %s (%s)", c.Name, c.Confidence)
				}
				return
			}

			for i, cmd := range commands {
				if cmd.Name != tt.expected[i].Name {
					t.Errorf("command %d: got name %q, want %q", i, cmd.Name, tt.expected[i].Name)
				}
				if cmd.Command != tt.expected[i].Command {
					t.Errorf("command %d: got command %q, want %q", i, cmd.Command, tt.expected[i].Command)
				}
				if cmd.Confidence != tt.expected[i].Confidence {
					t.Errorf("command %d: got confidence %q, want %q", i, cmd.Confidence, tt.expected[i].Confidence)
				}
			}
		})
	}
}

func TestParseMakefile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "makefile-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	tests := []struct {
		name     string
		content  string
		expected []ValidationCommand
	}{
		{
			name: "simple targets",
			content: `# Comment line
build:
	go build ./...

test:
	go test ./...
`,
			expected: []ValidationCommand{
				{Name: "build", Command: "make build", Confidence: "high"},
				{Name: "test", Command: "make test", Confidence: "high"},
			},
		},
		{
			name: "targets with dependencies",
			content: `all: build test

build: clean
	go build ./...

clean:
	rm -rf bin/
`,
			expected: []ValidationCommand{
				{Name: "all", Command: "make all", Confidence: "high"},
				{Name: "build", Command: "make build", Confidence: "high"},
				{Name: "clean", Command: "make clean", Confidence: "high"},
			},
		},
		{
			name: "skip special targets",
			content: `.PHONY: build test

build:
	go build

.DEFAULT:
	echo "default"
`,
			expected: []ValidationCommand{
				{Name: "build", Command: "make build", Confidence: "high"},
			},
		},
		{
			name: "custom targets",
			content: `deploy:
	./deploy.sh

setup:
	./setup.sh
`,
			expected: []ValidationCommand{
				{Name: "deploy", Command: "make deploy", Confidence: "low"},
				{Name: "setup", Command: "make setup", Confidence: "low"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filePath := filepath.Join(tmpDir, "Makefile_"+tt.name)
			if err := os.WriteFile(filePath, []byte(tt.content), 0644); err != nil {
				t.Fatalf("failed to write Makefile: %v", err)
			}

			commands := parseMakefile(filePath)

			if len(commands) != len(tt.expected) {
				t.Errorf("got %d commands, want %d", len(commands), len(tt.expected))
				for _, c := range commands {
					t.Logf("  got: %s (%s)", c.Name, c.Confidence)
				}
				return
			}

			for i, cmd := range commands {
				if cmd.Name != tt.expected[i].Name {
					t.Errorf("command %d: got name %q, want %q", i, cmd.Name, tt.expected[i].Name)
				}
				if cmd.Command != tt.expected[i].Command {
					t.Errorf("command %d: got command %q, want %q", i, cmd.Command, tt.expected[i].Command)
				}
				if cmd.Confidence != tt.expected[i].Confidence {
					t.Errorf("command %d: got confidence %q, want %q", i, cmd.Confidence, tt.expected[i].Confidence)
				}
			}
		})
	}
}

func TestDeduplicateCommands(t *testing.T) {
	tests := []struct {
		name     string
		input    []ValidationCommand
		expected []ValidationCommand
	}{
		{
			name:     "empty list",
			input:    []ValidationCommand{},
			expected: []ValidationCommand{},
		},
		{
			name: "no duplicates",
			input: []ValidationCommand{
				{Name: "build", Command: "go build", Confidence: "high"},
				{Name: "test", Command: "go test", Confidence: "high"},
			},
			expected: []ValidationCommand{
				{Name: "build", Command: "go build", Confidence: "high"},
				{Name: "test", Command: "go test", Confidence: "high"},
			},
		},
		{
			name: "duplicate - keep higher confidence",
			input: []ValidationCommand{
				{Name: "build", Command: "make build", Confidence: "low"},
				{Name: "build", Command: "go build", Confidence: "high"},
			},
			expected: []ValidationCommand{
				{Name: "build", Command: "go build", Confidence: "high"},
			},
		},
		{
			name: "duplicate - first already higher",
			input: []ValidationCommand{
				{Name: "test", Command: "go test", Confidence: "high"},
				{Name: "test", Command: "make test", Confidence: "medium"},
			},
			expected: []ValidationCommand{
				{Name: "test", Command: "go test", Confidence: "high"},
			},
		},
		{
			name: "multiple duplicates",
			input: []ValidationCommand{
				{Name: "build", Command: "just build", Confidence: "high"},
				{Name: "test", Command: "make test", Confidence: "low"},
				{Name: "build", Command: "go build", Confidence: "high"},
				{Name: "test", Command: "go test", Confidence: "high"},
			},
			expected: []ValidationCommand{
				{Name: "build", Command: "just build", Confidence: "high"},
				{Name: "test", Command: "go test", Confidence: "high"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := deduplicateCommands(tt.input)

			if len(result) != len(tt.expected) {
				t.Errorf("got %d commands, want %d", len(result), len(tt.expected))
				return
			}

			for i, cmd := range result {
				if cmd.Name != tt.expected[i].Name {
					t.Errorf("command %d: got name %q, want %q", i, cmd.Name, tt.expected[i].Name)
				}
				if cmd.Command != tt.expected[i].Command {
					t.Errorf("command %d: got command %q, want %q", i, cmd.Command, tt.expected[i].Command)
				}
				if cmd.Confidence != tt.expected[i].Confidence {
					t.Errorf("command %d: got confidence %q, want %q", i, cmd.Confidence, tt.expected[i].Confidence)
				}
			}
		})
	}
}

func TestDetectValidationCommands(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "validation-commands-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create a go project with a justfile
	goModContent := `module example.com/test

go 1.21
`
	justfileContent := `build:
    go build ./...

test:
    go test ./...

lint:
    golangci-lint run
`

	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goModContent), 0644); err != nil {
		t.Fatalf("failed to write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "justfile"), []byte(justfileContent), 0644); err != nil {
		t.Fatalf("failed to write justfile: %v", err)
	}

	result := DetectValidationCommands(tmpDir, []ProjectType{ProjectGo})

	// Should detect justfile
	if !result.DetectedFiles.Justfile {
		t.Error("expected Justfile to be detected")
	}
	if !result.DetectedFiles.GoMod {
		t.Error("expected GoMod to be detected")
	}

	// Should have commands from both justfile and Go defaults
	if len(result.Suggested) == 0 {
		t.Error("expected at least some suggested commands")
	}

	// Check for expected command names
	commandNames := make(map[string]bool)
	for _, cmd := range result.Suggested {
		commandNames[cmd.Name] = true
	}

	expectedNames := []string{"build", "test", "lint"}
	for _, name := range expectedNames {
		if !commandNames[name] {
			t.Errorf("expected command %q to be in suggestions", name)
		}
	}
}

func TestDetectValidationCommands_EmptyProject(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "empty-project-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	result := DetectValidationCommands(tmpDir, []ProjectType{})

	// Should have empty suggested commands
	if len(result.Suggested) != 0 {
		t.Errorf("expected 0 suggested commands, got %d", len(result.Suggested))
	}

	// All detected files should be false
	if result.DetectedFiles.Justfile || result.DetectedFiles.Makefile ||
		result.DetectedFiles.PackageJSON || result.DetectedFiles.GoMod {
		t.Error("expected all detected files to be false for empty project")
	}
}
