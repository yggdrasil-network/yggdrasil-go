package main

import "time"
import "fmt"
import "sync"

func main() {
	fmt.Println("Testing speed of recv+send loop")
	const count = 10000000
	c := make(chan []byte, 1)
	c <- []byte{}
	var wg sync.WaitGroup
	worker := func() {
		for idx := 0; idx < count; idx++ {
			p := <-c
			select {
			case c <- p:
			default:
			}
		}
		wg.Done()
	}
	nIter := 0
	start := time.Now()
	for idx := 0; idx < 1; idx++ {
		go worker()
		nIter += count
		wg.Add(1)
	}
	wg.Wait()
	stop := time.Now()
	timed := stop.Sub(start)
	fmt.Printf("%d iterations in %s\n", nIter, timed)
	fmt.Printf("%f iterations per second\n", float64(nIter)/timed.Seconds())
	fmt.Printf("%s per iteration\n", timed/time.Duration(nIter))
}
