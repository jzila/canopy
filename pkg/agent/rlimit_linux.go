//go:build linux

package agent

import (
	"os/exec"
	"syscall"
	"time"
)

// setResourceLimits configures process isolation for agent execution.
// Sets:
// - Setpgid: puts agent in own process group for clean termination
// - Pdeathsig: kills agent if orchestrator crashes unexpectedly
//
// Note: Full rlimits (memory, processes, files) require either:
// - prlimit wrapper command
// - bwrap sandbox (preferred, see canopy-kla)
// - cgroups v2
// Go's SysProcAttr doesn't support rlimits directly.
func setResourceLimits(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}

	// Process group isolation - enables killing all child processes
	cmd.SysProcAttr.Setpgid = true

	// Kill agent if parent (orchestrator) dies unexpectedly
	cmd.SysProcAttr.Pdeathsig = syscall.SIGKILL
}

// killProcessGroup terminates a process group gracefully:
// 1. Send SIGTERM to the process group (negative PID)
// 2. Wait up to gracePeriod for processes to exit
// 3. Send SIGKILL if processes are still running
//
// The pid should be the process group leader's PID (from cmd.Process.Pid).
// With Setpgid=true, the process becomes its own group leader.
func killProcessGroup(pid int, gracePeriod time.Duration) error {
	if pid <= 0 {
		return nil
	}

	pgid := pid // With Setpgid=true, the process is its own group leader

	// Send SIGTERM to the entire process group (negative PID)
	if err := syscall.Kill(-pgid, syscall.SIGTERM); err != nil {
		// ESRCH means process/group doesn't exist (already exited)
		if err == syscall.ESRCH {
			return nil
		}
		return err
	}

	// Wait for processes to exit gracefully
	deadline := time.Now().Add(gracePeriod)
	for time.Now().Before(deadline) {
		// Check if process group still exists by sending signal 0
		err := syscall.Kill(-pgid, 0)
		if err == syscall.ESRCH {
			// Process group no longer exists - success
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Grace period expired, send SIGKILL
	if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil {
		if err == syscall.ESRCH {
			return nil
		}
		return err
	}

	return nil
}
