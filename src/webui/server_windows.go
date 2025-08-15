//go:build windows

package webui

import (
	"fmt"
	"os"
)

// sendRestartSignal sends a restart signal to the process on Windows
func sendRestartSignal(proc *os.Process) error {
	// Windows doesn't support SIGUSR1, so we'll use a different approach
	// For now, we'll just log that manual restart is needed
	// In the future, this could be enhanced with Windows-specific restart mechanisms
	return fmt.Errorf("automatic restart not supported on Windows")
}
