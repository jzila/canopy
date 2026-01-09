//go:build !linux

package agent

import "os/exec"

// setResourceLimits is a no-op on non-Linux platforms
func setResourceLimits(cmd *exec.Cmd) {
	// Resource limits via rlimit/cgroups only available on Linux
}
