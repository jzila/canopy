//go:build linux

package agent

import (
	"os/exec"
	"syscall"
)

// setResourceLimits configures process isolation for agent execution.
// Currently sets:
// - Setpgid: puts agent in own process group for clean termination
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
}
