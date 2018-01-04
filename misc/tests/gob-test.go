package main

import "bytes"
import "encoding/gob"
import "time"
import "fmt"

type testStruct struct {
	First  uint64
	Second float64
	Third  []byte
}

func testFunc(tickerDuration time.Duration) {
	chn := make(chan []byte)
	ticker := time.NewTicker(tickerDuration)
	defer ticker.Stop()
	send := testStruct{First: 1, Second: 2, Third: []byte{3, 4, 5}}
	buf := bytes.NewBuffer(nil)
	enc := gob.NewEncoder(buf)
	dec := gob.NewDecoder(buf)
	sendCall := func() {
		err := enc.EncodeValue(&send)
		if err != nil {
			panic(err)
		}
		bs := make([]byte, buf.Len())
		buf.Read(bs)
		fmt.Println("send:", bs)
		go func() { chn <- bs }()
	}
	recvCall := func(bs []byte) {
		buf.Write(bs)
		recv := testStruct{}
		err := dec.DecodeValue(&recv)
		fmt.Println("recv:", bs)
		if err != nil {
			panic(err)
		}
	}
	for {
		select {
		case bs := <-chn:
			recvCall(bs)
		case <-ticker.C:
			sendCall()
		}
	}
}

func main() {
	go testFunc(100 * time.Millisecond) // Does not crash
	time.Sleep(time.Second)
	go testFunc(time.Nanosecond) // Does crash
	time.Sleep(time.Second)
}
