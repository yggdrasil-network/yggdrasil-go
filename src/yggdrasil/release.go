// +build !debug

package yggdrasil

import (
	"errors"
	"log"
)

// Starts the function profiler. This is only supported when built with
// '-tags build'.
func StartProfiler(_ *log.Logger) error {
	return errors.New("Release builds do not support -pprof, build using '-tags debug'")
}
