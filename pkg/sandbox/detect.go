package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ProjectType represents a detected project type
type ProjectType string

const (
	ProjectNodeJS ProjectType = "nodejs"
	ProjectRust   ProjectType = "rust"
	ProjectGo     ProjectType = "go"
	ProjectPython ProjectType = "python"
	ProjectRuby   ProjectType = "ruby"
	ProjectJava   ProjectType = "java"
)

// DetectedTool represents a discovered tool and its path
type DetectedTool struct {
	Name    string // Tool name (e.g., "node", "cargo")
	Version string // Version string if available
	Path    string // Full path to the executable
}

// DetectedConfig represents a discovered config file
type DetectedConfig struct {
	Path        string // Full path to the config file
	Description string // Human-readable description
}

// DetectedCache represents a discovered cache directory
type DetectedCache struct {
	Path        string // Full path to the cache directory
	Description string // Human-readable description
	Size        int64  // Size in bytes (0 if not calculated)
}

// DetectionResult contains all discovered paths for a project
type DetectionResult struct {
	ProjectTypes []ProjectType
	Tools        []DetectedTool
	ToolPaths    []string // Paths to bind read-only (e.g., ~/.cargo)
	ConfigPaths  []string // Config files to copy (e.g., ~/.npmrc)
	CachePaths   []string // Cache dirs for read-write mount (e.g., ~/.npm)
}

// DetectProject scans a directory and returns detected project types and paths
func DetectProject(workDir string) (*DetectionResult, error) {
	result := &DetectionResult{}

	// Detect project types
	result.ProjectTypes = detectProjectTypes(workDir)

	// Discover tools, configs, and caches based on detected types
	for _, pt := range result.ProjectTypes {
		discoverPathsForType(pt, result)
	}

	// Always add common tool paths that exist
	discoverCommonPaths(result)

	// Deduplicate paths
	result.ToolPaths = deduplicatePaths(result.ToolPaths)
	result.ConfigPaths = deduplicatePaths(result.ConfigPaths)
	result.CachePaths = deduplicatePaths(result.CachePaths)

	return result, nil
}

// detectProjectTypes examines the directory for project markers
func detectProjectTypes(workDir string) []ProjectType {
	var types []ProjectType

	// Node.js: package.json
	if fileExists(filepath.Join(workDir, "package.json")) {
		types = append(types, ProjectNodeJS)
	}

	// Rust: Cargo.toml
	if fileExists(filepath.Join(workDir, "Cargo.toml")) {
		types = append(types, ProjectRust)
	}

	// Go: go.mod
	if fileExists(filepath.Join(workDir, "go.mod")) {
		types = append(types, ProjectGo)
	}

	// Python: pyproject.toml, requirements.txt, setup.py, Pipfile
	if fileExists(filepath.Join(workDir, "pyproject.toml")) ||
		fileExists(filepath.Join(workDir, "requirements.txt")) ||
		fileExists(filepath.Join(workDir, "setup.py")) ||
		fileExists(filepath.Join(workDir, "Pipfile")) {
		types = append(types, ProjectPython)
	}

	// Ruby: Gemfile
	if fileExists(filepath.Join(workDir, "Gemfile")) {
		types = append(types, ProjectRuby)
	}

	// Java: pom.xml, build.gradle
	if fileExists(filepath.Join(workDir, "pom.xml")) ||
		fileExists(filepath.Join(workDir, "build.gradle")) ||
		fileExists(filepath.Join(workDir, "build.gradle.kts")) {
		types = append(types, ProjectJava)
	}

	return types
}

// discoverPathsForType adds paths based on project type
func discoverPathsForType(pt ProjectType, result *DetectionResult) {
	switch pt {
	case ProjectNodeJS:
		discoverNodePaths(result)
	case ProjectRust:
		discoverRustPaths(result)
	case ProjectGo:
		discoverGoPaths(result)
	case ProjectPython:
		discoverPythonPaths(result)
	case ProjectRuby:
		discoverRubyPaths(result)
	case ProjectJava:
		discoverJavaPaths(result)
	}
}

func discoverNodePaths(result *DetectionResult) {
	home := os.Getenv("HOME")
	if home == "" {
		return
	}

	// Check for Node version managers
	nvmDir := filepath.Join(home, ".nvm")
	if dirExists(nvmDir) {
		result.ToolPaths = append(result.ToolPaths, nvmDir)
		if tool := detectToolVersion("node", nvmDir); tool != nil {
			result.Tools = append(result.Tools, *tool)
		}
	}

	voltaDir := filepath.Join(home, ".volta")
	if dirExists(voltaDir) {
		result.ToolPaths = append(result.ToolPaths, voltaDir)
	}

	fnmDir := filepath.Join(home, ".fnm")
	if dirExists(fnmDir) {
		result.ToolPaths = append(result.ToolPaths, fnmDir)
	}

	// npm config
	npmrc := filepath.Join(home, ".npmrc")
	if fileExists(npmrc) {
		result.ConfigPaths = append(result.ConfigPaths, npmrc)
	}

	// npm cache
	npmCache := filepath.Join(home, ".npm")
	if dirExists(npmCache) {
		result.CachePaths = append(result.CachePaths, npmCache)
	}

	// pnpm cache
	pnpmCache := filepath.Join(home, ".local", "share", "pnpm")
	if dirExists(pnpmCache) {
		result.CachePaths = append(result.CachePaths, pnpmCache)
	}

	// yarn cache
	yarnCache := filepath.Join(home, ".yarn")
	if dirExists(yarnCache) {
		result.CachePaths = append(result.CachePaths, yarnCache)
	}
}

func discoverRustPaths(result *DetectionResult) {
	home := os.Getenv("HOME")
	if home == "" {
		return
	}

	// Cargo home
	cargoDir := filepath.Join(home, ".cargo")
	if dirExists(cargoDir) {
		result.ToolPaths = append(result.ToolPaths, cargoDir)
		if tool := detectToolVersion("cargo", cargoDir); tool != nil {
			result.Tools = append(result.Tools, *tool)
		}

		// Cargo config
		cargoConfig := filepath.Join(cargoDir, "config.toml")
		if fileExists(cargoConfig) {
			result.ConfigPaths = append(result.ConfigPaths, cargoConfig)
		}

		// Cargo registry cache (separate for read-write)
		cargoRegistry := filepath.Join(cargoDir, "registry")
		if dirExists(cargoRegistry) {
			result.CachePaths = append(result.CachePaths, cargoRegistry)
		}
	}

	// Rustup
	rustupDir := filepath.Join(home, ".rustup")
	if dirExists(rustupDir) {
		result.ToolPaths = append(result.ToolPaths, rustupDir)
	}
}

func discoverGoPaths(result *DetectionResult) {
	home := os.Getenv("HOME")
	if home == "" {
		return
	}

	// GOPATH
	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		gopath = filepath.Join(home, "go")
	}
	if dirExists(gopath) {
		result.ToolPaths = append(result.ToolPaths, gopath)
	}

	// System Go (if installed via package manager)
	for _, goRoot := range []string{"/usr/local/go", "/usr/lib/go"} {
		if dirExists(goRoot) {
			result.ToolPaths = append(result.ToolPaths, goRoot)
			break
		}
	}

	// goenv
	goenvDir := filepath.Join(home, ".goenv")
	if dirExists(goenvDir) {
		result.ToolPaths = append(result.ToolPaths, goenvDir)
	}

	// Go build cache
	goBuildCache := filepath.Join(home, ".cache", "go-build")
	if dirExists(goBuildCache) {
		result.CachePaths = append(result.CachePaths, goBuildCache)
	}

	// Go module cache
	goModCache := filepath.Join(gopath, "pkg", "mod")
	if dirExists(goModCache) {
		result.CachePaths = append(result.CachePaths, goModCache)
	}
}

func discoverPythonPaths(result *DetectionResult) {
	home := os.Getenv("HOME")
	if home == "" {
		return
	}

	// pyenv
	pyenvDir := filepath.Join(home, ".pyenv")
	if dirExists(pyenvDir) {
		result.ToolPaths = append(result.ToolPaths, pyenvDir)
	}

	// Conda
	condaDir := filepath.Join(home, ".conda")
	if dirExists(condaDir) {
		result.ToolPaths = append(result.ToolPaths, condaDir)
	}
	minicondaDir := filepath.Join(home, "miniconda3")
	if dirExists(minicondaDir) {
		result.ToolPaths = append(result.ToolPaths, minicondaDir)
	}
	anacondaDir := filepath.Join(home, "anaconda3")
	if dirExists(anacondaDir) {
		result.ToolPaths = append(result.ToolPaths, anacondaDir)
	}

	// Virtual environment tools
	poetryDir := filepath.Join(home, ".poetry")
	if dirExists(poetryDir) {
		result.ToolPaths = append(result.ToolPaths, poetryDir)
	}

	// pip cache
	pipCache := filepath.Join(home, ".cache", "pip")
	if dirExists(pipCache) {
		result.CachePaths = append(result.CachePaths, pipCache)
	}

	// pip config
	pipConf := filepath.Join(home, ".config", "pip", "pip.conf")
	if fileExists(pipConf) {
		result.ConfigPaths = append(result.ConfigPaths, pipConf)
	}
}

func discoverRubyPaths(result *DetectionResult) {
	home := os.Getenv("HOME")
	if home == "" {
		return
	}

	// rbenv
	rbenvDir := filepath.Join(home, ".rbenv")
	if dirExists(rbenvDir) {
		result.ToolPaths = append(result.ToolPaths, rbenvDir)
	}

	// rvm
	rvmDir := filepath.Join(home, ".rvm")
	if dirExists(rvmDir) {
		result.ToolPaths = append(result.ToolPaths, rvmDir)
	}

	// Bundler config
	bundleConfig := filepath.Join(home, ".bundle")
	if dirExists(bundleConfig) {
		result.ConfigPaths = append(result.ConfigPaths, bundleConfig)
	}

	// Gem cache
	gemDir := filepath.Join(home, ".gem")
	if dirExists(gemDir) {
		result.CachePaths = append(result.CachePaths, gemDir)
	}
}

func discoverJavaPaths(result *DetectionResult) {
	home := os.Getenv("HOME")
	if home == "" {
		return
	}

	// SDKMAN
	sdkmanDir := filepath.Join(home, ".sdkman")
	if dirExists(sdkmanDir) {
		result.ToolPaths = append(result.ToolPaths, sdkmanDir)
	}

	// jenv
	jenvDir := filepath.Join(home, ".jenv")
	if dirExists(jenvDir) {
		result.ToolPaths = append(result.ToolPaths, jenvDir)
	}

	// Maven settings
	m2Settings := filepath.Join(home, ".m2", "settings.xml")
	if fileExists(m2Settings) {
		result.ConfigPaths = append(result.ConfigPaths, m2Settings)
	}

	// Maven repository cache
	m2Repo := filepath.Join(home, ".m2", "repository")
	if dirExists(m2Repo) {
		result.CachePaths = append(result.CachePaths, m2Repo)
	}

	// Gradle
	gradleDir := filepath.Join(home, ".gradle")
	if dirExists(gradleDir) {
		// Gradle caches
		gradleCache := filepath.Join(gradleDir, "caches")
		if dirExists(gradleCache) {
			result.CachePaths = append(result.CachePaths, gradleCache)
		}
	}
}

// discoverCommonPaths adds paths that are always useful regardless of project type
func discoverCommonPaths(result *DetectionResult) {
	home := os.Getenv("HOME")
	if home == "" {
		return
	}

	// mise (formerly rtx) - universal version manager
	miseDir := filepath.Join(home, ".local", "share", "mise")
	if dirExists(miseDir) {
		result.ToolPaths = append(result.ToolPaths, miseDir)
	}

	// asdf - another universal version manager
	asdfDir := filepath.Join(home, ".asdf")
	if dirExists(asdfDir) {
		result.ToolPaths = append(result.ToolPaths, asdfDir)
	}

	// Git config (always needed for commits)
	gitconfig := filepath.Join(home, ".gitconfig")
	if fileExists(gitconfig) {
		result.ConfigPaths = append(result.ConfigPaths, gitconfig)
	}

	// Claude credentials (always needed)
	claudeJson := filepath.Join(home, ".claude.json")
	if fileExists(claudeJson) {
		result.ConfigPaths = append(result.ConfigPaths, claudeJson)
	}

	// GitHub CLI config
	ghConfigDir := filepath.Join(home, ".config", "gh")
	if dirExists(ghConfigDir) {
		result.ConfigPaths = append(result.ConfigPaths, ghConfigDir)
	}
}

// detectToolVersion tries to get version info for a tool
func detectToolVersion(toolName, toolPath string) *DetectedTool {
	// Try to find the executable
	var execPath string
	if path, err := exec.LookPath(toolName); err == nil {
		execPath = path
	}

	// Try to get version
	var version string
	cmd := exec.Command(toolName, "--version")
	if output, err := cmd.Output(); err == nil {
		version = strings.TrimSpace(strings.Split(string(output), "\n")[0])
	}

	if execPath == "" && version == "" {
		return nil
	}

	return &DetectedTool{
		Name:    toolName,
		Version: version,
		Path:    execPath,
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func deduplicatePaths(paths []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, p := range paths {
		if !seen[p] {
			seen[p] = true
			result = append(result, p)
		}
	}
	return result
}

// ProjectTypeString returns a human-readable description of project types
func ProjectTypeString(types []ProjectType) string {
	if len(types) == 0 {
		return "Unknown"
	}

	names := make([]string, 0, len(types))
	for _, t := range types {
		switch t {
		case ProjectNodeJS:
			names = append(names, "Node.js")
		case ProjectRust:
			names = append(names, "Rust")
		case ProjectGo:
			names = append(names, "Go")
		case ProjectPython:
			names = append(names, "Python")
		case ProjectRuby:
			names = append(names, "Ruby")
		case ProjectJava:
			names = append(names, "Java")
		default:
			names = append(names, string(t))
		}
	}

	if len(names) == 1 {
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " + " + names[len(names)-1]
}
