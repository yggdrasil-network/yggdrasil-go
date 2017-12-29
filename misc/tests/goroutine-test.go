package main

import "sync"
import "time"
import "fmt"

func main () {
  const reqs = 1000000
  var wg sync.WaitGroup
  start := time.Now()
  for idx := 0 ; idx < reqs ; idx++ {
    wg.Add(1)
    go func () { wg.Done() } ()
  }
  wg.Wait()
  stop := time.Now()
  timed := stop.Sub(start)
  fmt.Printf("%d goroutines in %s (%f per second)\n",
             reqs,
             timed,
             reqs/timed.Seconds())
}
