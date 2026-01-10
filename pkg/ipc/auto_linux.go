package ipc

import "syscall"

// getSysProcAttr returns platform-specific process attributes for daemon spawning
func getSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setsid: true, // Create new session to detach from parent
	}
}
