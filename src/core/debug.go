package core

import (
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
)

// Start the profiler if the required environment variable is set.
func init() {
	envVarName := "PPROFLISTEN"
	hostPort := os.Getenv(envVarName)
	switch {
	case hostPort == "":
		fmt.Fprintf(os.Stderr, "DEBUG: %s not set, profiler not started.\n", envVarName)
	default:
		fmt.Fprintf(os.Stderr, "DEBUG: Starting pprof on %s\n", hostPort)
		go fmt.Println(http.ListenAndServe(hostPort, nil))
	}
}
