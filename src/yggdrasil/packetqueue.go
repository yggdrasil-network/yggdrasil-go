package yggdrasil

import "github.com/yggdrasil-network/yggdrasil-go/src/util"

// TODO take max size from config
const MAX_PACKET_QUEUE_SIZE = 1048576 // 1 MB

// TODO separate queues per e.g. traffic flow
type packetQueue struct {
	packets [][]byte
	size    uint32
}

func (q *packetQueue) cleanup() {
	for q.size > MAX_PACKET_QUEUE_SIZE {
		if packet, success := q.pop(); success {
			util.PutBytes(packet)
		} else {
			panic("attempted to drop packet from empty queue")
			break
		}
	}
}

func (q *packetQueue) push(packet []byte) {
	q.packets = append(q.packets, packet)
	q.size += uint32(len(packet))
	q.cleanup()
}

func (q *packetQueue) pop() ([]byte, bool) {
	if len(q.packets) > 0 {
		packet := q.packets[0]
		q.packets = q.packets[1:]
		q.size -= uint32(len(packet))
		return packet, true
	}
	return nil, false
}
