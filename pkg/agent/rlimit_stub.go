//go:build !linux

package agent

import (
	"os/exec"
	"time"
)

// setResourceLimits is a no-op on non-Linux platforms
func setResourceLimits(cmd *exec.Cmd) {
	// Resource limits via rlimit/cgroups only available on Linux
}

// killProcessGroup is a no-op on non-Linux platforms.
// On these platforms, we rely on Go's default process termination behavior
// via exec.CommandContext, which sends SIGKILL when the context is cancelled.
func killProcessGroup(pid int, gracePeriod time.Duration) error {
	// Process group management not available on non-Linux platforms
	return nil
}
