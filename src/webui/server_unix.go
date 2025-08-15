//go:build !windows

package webui

import (
	"os"
	"syscall"
)

// sendRestartSignal sends a restart signal to the process on Unix-like systems
func sendRestartSignal(proc *os.Process) error {
	return proc.Signal(syscall.SIGUSR1)
}
