package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jzila/canopy/pkg/sandbox"
	"github.com/spf13/cobra"
)

var (
	initReconfigure bool
	initSkipBeads   bool
	initNonInteractive bool
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
  canopy init --skip-beads`,
	RunE: runInit,
}

func init() {
	initCmd.Flags().BoolVar(&initReconfigure, "reconfigure", false, "Re-run detection and reconfigure sandbox (overwrites existing config)")
	initCmd.Flags().BoolVar(&initSkipBeads, "skip-beads", false, "Skip beads initialization")
	initCmd.Flags().BoolVar(&initNonInteractive, "non-interactive", false, "Accept all defaults without prompting")
	initCmd.Flags().BoolVarP(&initNonInteractive, "yes", "y", false, "Accept all defaults without prompting (alias for --non-interactive)")

	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
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
	workDir, _ := os.Getwd()
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
