package main

import "fmt"
import "time"
import "sync/atomic"
import "runtime"

func main() {

  var ops uint64 = 0
  for i := 0 ; i < 4 ; i++ {
    go func () {
      for {
        atomic.AddUint64(&ops, 1)
        runtime.Gosched()
      }
    }()
  }
  time.Sleep(1*time.Second)
  opsFinal := atomic.LoadUint64(&ops)
  fmt.Println("ops:", opsFinal)
}
