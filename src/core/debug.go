//go:build debug
// +build debug

package core

import (
	"fmt"

	"net/http"
	_ "net/http/pprof"
	"os"
	"runtime"

	"github.com/gologme/log"
)

// Start the profiler in debug builds, if the required environment variable is set.
func init() {
	envVarName := "PPROFLISTEN"
	hostPort := os.Getenv(envVarName)
	switch {
	case hostPort == "":
		fmt.Fprintf(os.Stderr, "DEBUG: %s not set, profiler not started.\n", envVarName)
	default:
		fmt.Fprintf(os.Stderr, "DEBUG: Starting pprof on %s\n", hostPort)
		go func() { fmt.Println(http.ListenAndServe(hostPort, nil)) }()
	}
}

// Starts the function profiler. This is only supported when built with
// '-tags build'.
func StartProfiler(log *log.Logger) error {
	runtime.SetBlockProfileRate(1)
	go func() { log.Println(http.ListenAndServe("localhost:6060", nil)) }()
	return nil
}
