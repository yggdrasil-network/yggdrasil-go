package main

import "fmt"

//import "net"
import "time"
import "runtime"
import "sync/atomic"

func poolbench() {
	nWorkers := runtime.GOMAXPROCS(0)
	work := make(chan func(), 1)
	workers := make(chan chan<- func(), nWorkers)
	makeWorker := func() chan<- func() {
		ch := make(chan func())
		go func() {
			for {
				f := <-ch
				f()
				select {
				case workers <- (ch):
				default:
					return
				}
			}
		}()
		return ch
	}
	getWorker := func() chan<- func() {
		select {
		case ch := <-workers:
			return ch
		default:
			return makeWorker()
		}
	}
	dispatcher := func() {
		for {
			w := <-work
			ch := getWorker()
			ch <- w
		}
	}
	go dispatcher()
	var count uint64
	const nCounts = 1000000
	for idx := 0; idx < nCounts; idx++ {
		f := func() { atomic.AddUint64(&count, 1) }
		work <- f
	}
	for atomic.LoadUint64(&count) < nCounts {
	}
}

func normalbench() {
	var count uint64
	const nCounts = 1000000
	ch := make(chan struct{}, 1)
	ch <- struct{}{}
	for idx := 0; idx < nCounts; idx++ {
		f := func() { atomic.AddUint64(&count, 1) }
		f()
		<-ch
		ch <- struct{}{}
	}
}

func gobench() {
	var count uint64
	const nCounts = 1000000
	for idx := 0; idx < nCounts; idx++ {
		f := func() { atomic.AddUint64(&count, 1) }
		go f()
	}
	for atomic.LoadUint64(&count) < nCounts {
	}
}

func main() {
	start := time.Now()
	poolbench()
	fmt.Println(time.Since(start))
	start = time.Now()
	normalbench()
	fmt.Println(time.Since(start))
	start = time.Now()
	gobench()
	fmt.Println(time.Since(start))
}
