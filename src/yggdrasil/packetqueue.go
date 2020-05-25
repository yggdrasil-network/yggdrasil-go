package yggdrasil

/*
import (
	"math/rand"
	"time"
)
*/

// TODO separate queues per e.g. traffic flow
//  For now, we put everything in queue
/*
type pqStreamID string

type pqPacketInfo struct {
	packet []byte
	time   time.Time
}

type pqStream struct {
  id   string
	infos []pqPacketInfo
	size  int
}
*/

type packetQueue struct {
	//streams []pqStream
	packets [][]byte
	size    uint64
}

// drop will remove a packet from the queue, returning it to the pool
//  returns true if a packet was removed, false otherwise
func (q *packetQueue) drop() bool {
	if q.size == 0 {
		return false
	}
	packet := q.packets[0]
	q.packets = q.packets[1:]
	q.size -= uint64(len(packet))
	pool_putBytes(packet)
	return true
}

func (q *packetQueue) push(packet []byte) {
	q.packets = append(q.packets, packet)
	q.size += uint64(len(packet))
}

func (q *packetQueue) pop() ([]byte, bool) {
	if q.size > 0 {
		packet := q.packets[0]
		q.packets = q.packets[1:]
		q.size -= uint64(len(packet))
		return packet, true
	}
	return nil, false
}
