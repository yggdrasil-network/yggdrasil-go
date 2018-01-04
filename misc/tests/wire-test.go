package main

import "wire"
import "fmt"

import "time"

func main() {
	for idx := 0; idx < 64; idx++ {
		num := uint64(1) << uint(idx)
		encoded := make([]byte, 10)
		length := wire.Encode_uint64(num, encoded)
		decoded, _ := wire.Decode_uint64(encoded[:length])
		if decoded != num {
			panic(fmt.Sprintf("%d != %d", decoded, num))
		}
	}
	const count = 1000000
	start := time.Now()
	encoded := make([]byte, 10)
	//num := ^uint64(0) // Longest possible value for full uint64 range
	num := ^uint64(0) >> 1 // Largest positive int64 (real use case)
	//num := uint64(0) // Shortest possible value, most will be of this length
	length := wire.Encode_uint64(num, encoded)
	for idx := 0; idx < count; idx++ {
		wire.Encode_uint64(num, encoded)
	}
	timed := time.Since(start)
	fmt.Println("Ops:", count/timed.Seconds())
	fmt.Println("Time:", timed.Nanoseconds()/count)

	encoded = encoded[:length]
	start = time.Now()
	for idx := 0; idx < count; idx++ {
		wire.Decode_uint64(encoded)
	}
	timed = time.Since(start)
	fmt.Println("Ops:", count/timed.Seconds())
	fmt.Println("Time:", timed.Nanoseconds()/count)
}
