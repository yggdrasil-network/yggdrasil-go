package util

import "runtime"

var workerPool chan func()

func init() {
	maxProcs := runtime.GOMAXPROCS(0)
	if maxProcs < 1 {
		maxProcs = 1
	}
	workerPool = make(chan func(), maxProcs)
	for idx := 0; idx < maxProcs; idx++ {
		go func() {
			for f := range workerPool {
				f()
			}
		}()
	}
}

// WorkerGo submits a job to a pool of GOMAXPROCS worker goroutines.
// This is meant for short non-blocking functions f() where you could just go f(),
// but you want some kind of backpressure to prevent spawning endless goroutines.
// WorkerGo returns as soon as the function is queued to run, not when it finishes.
// In Yggdrasil, these workers are used for certain cryptographic operations.
func WorkerGo(f func()) {
	workerPool <- f
}
