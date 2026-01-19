package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var agentMode bool

var helpCmd = &cobra.Command{
	Use:   "help [command]",
	Short: "Help about any command",
	Long: `Help provides help for any command in the application.
Simply type canopy help [path to command] for full details.

Use --agent for detailed workflow explanation for AI agents.`,
	Run: func(cmd *cobra.Command, args []string) {
		if agentMode {
			printAgentHelp()
			return
		}
		// Default help behavior
		if len(args) == 0 {
			_ = rootCmd.Help()
			return
		}
		// Find and show help for subcommand
		c, _, err := rootCmd.Find(args)
		if err != nil {
			fmt.Println(err)
			return
		}
		_ = c.Help()
	},
}

func init() {
	helpCmd.Flags().BoolVar(&agentMode, "agent", false, "Show detailed workflow explanation for AI agents")
	rootCmd.SetHelpCommand(helpCmd)
}

func printAgentHelp() {
	fmt.Print(`CANOPY: PARALLEL AGENT ORCHESTRATION

ARCHITECTURE
  Human -> Root Claude (you) -> canopy run -> [Worker Claude 1, Worker Claude 2, ...N]

  You are the orchestrator. You decompose tasks, create beads issues, run canopy.
  Workers run in isolated OverlayFS sandboxes (CoW clones of working directory).

EXECUTION LOOP
  bd ready -> spawn N workers in parallel -> wait -> bd done -> merge changes -> repeat

INITIAL SETUP (for new repos)
  canopy init --agent       Get detection results + questionnaire JSON
  canopy init --apply ...   Apply answers JSON to create config files

  Three-step flow:
  1. Run "canopy init --agent" to get project detection and questions
  2. Present questions to user (use AskUserQuestion for each)
  3. Run "canopy init --apply '<answers JSON>'" to create config

  Creates:
  - .canopy/sandbox.toml   (sandbox paths and resource limits)
  - .canopy/validation.toml (post-merge validation steps)

  Detection-only mode (no questions): canopy init --detect

SANDBOX ISOLATION
  Each worker sees: lowerdir (read-only base) + upperdir (private writes) = merged view
  Workers can't see each other's changes. Merge happens after completion (last-writer-wins).

SECURITY
  Default protections (always on):
  - Env filtering: only ANTHROPIC_*, PATH, LANG, LC_*, TERM, TMPDIR, TZ passed
  - .claude/ hidden: parent session settings/approvals not visible to workers
  - Process groups: Pdeathsig ensures workers die if orchestrator dies

  With --sandbox flag (requires bwrap installed):
  - Namespace isolation: user, PID, IPC, UTS namespaces
  - Capability drop: all capabilities removed
  - Resource limits: 4GB memory, 100 processes, 1024 file descriptors
  - Filesystem: read-only /nix, /usr, /lib, /bin, /etc/ssl; only /workspace writable
  - No network filtering (workers can still reach Anthropic API)

WORKER INVOCATION
  claude --print --output-format json --dangerously-skip-permissions "<task prompt>"

ORCHESTRATOR WORKFLOW
  1. Analyze task -> identify subtasks and dependencies
  2. bd create "subtask" -p <priority> -> for each subtask
  3. bd dep add <child> <parent> -> set dependencies
  4. canopy run [-c N] [--sandbox] -> execute (N=concurrency, default 4)
  5. Review results -> handle failures -> iterate

EXAMPLE
  Task: "Add auth with login, signup, tests, docs"

  bd create "Design auth API" -p 0        -> bd-a1b2
  bd create "Implement login" -p 1        -> bd-c3d4
  bd create "Implement signup" -p 1       -> bd-e5f6
  bd create "Write tests" -p 2            -> bd-g7h8
  bd create "Write docs" -p 2             -> bd-i9j0

  bd dep add bd-c3d4 bd-a1b2   # login depends on design
  bd dep add bd-e5f6 bd-a1b2   # signup depends on design
  bd dep add bd-g7h8 bd-c3d4   # tests depend on login
  bd dep add bd-i9j0 bd-c3d4   # docs depend on login

  canopy run -v --sandbox      # full isolation

  Execution: design -> [login, signup] (parallel) -> [tests, docs] (parallel)

WHEN TO USE
  - 3+ independent subtasks
  - Work that can be parallelized
  - You want isolated sandboxes

WHEN NOT TO USE
  - Simple tasks (<3 subtasks)
  - Fully sequential work
  - Need human review between steps

`)
}
