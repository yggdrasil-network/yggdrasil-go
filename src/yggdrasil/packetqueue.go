package yggdrasil

import (
	"container/heap"
	"time"
)

// TODO separate queues per e.g. traffic flow
//  For now, we put everything in queue

type pqStreamID string

type pqPacketInfo struct {
	packet []byte
	time   time.Time
}

type pqStream struct {
	id    pqStreamID
	infos []pqPacketInfo
	size  uint64
}

type packetQueue struct {
	streams []pqStream
	size    uint64
}

// drop will remove a packet from the queue, returning it to the pool
//  returns true if a packet was removed, false otherwise
func (q *packetQueue) drop() bool {
	if q.size == 0 {
		return false
	}
	var longestIdx int
	for idx := range q.streams {
		if q.streams[idx].size > q.streams[longestIdx].size {
			longestIdx = idx
		}
	}
	stream := q.streams[longestIdx]
	info := stream.infos[0]
	if len(stream.infos) > 1 {
		stream.infos = stream.infos[1:]
		stream.size -= uint64(len(info.packet))
		q.streams[longestIdx] = stream
		q.size -= uint64(len(info.packet))
		heap.Fix(q, longestIdx)
	} else {
		heap.Remove(q, longestIdx)
	}
	pool_putBytes(info.packet)
	return true
}

func (q *packetQueue) push(packet []byte) {
	id := pqStreamID(peer_getPacketCoords(packet)) // just coords for now
	info := pqPacketInfo{packet: packet, time: time.Now()}
	for idx := range q.streams {
		if q.streams[idx].id == id {
			q.streams[idx].infos = append(q.streams[idx].infos, info)
			q.streams[idx].size += uint64(len(packet))
			q.size += uint64(len(packet))
			return
		}
	}
	stream := pqStream{id: id, size: uint64(len(packet))}
	stream.infos = append(stream.infos, info)
	heap.Push(q, stream)
}

func (q *packetQueue) pop() ([]byte, bool) {
	if q.size > 0 {
		stream := q.streams[0]
		info := stream.infos[0]
		if len(stream.infos) > 1 {
			stream.infos = stream.infos[1:]
			stream.size -= uint64(len(info.packet))
			q.streams[0] = stream
			q.size -= uint64(len(info.packet))
			heap.Fix(q, 0)
		} else {
			heap.Remove(q, 0)
		}
		return info.packet, true
	}
	return nil, false
}

////////////////////////////////////////////////////////////////////////////////

// Interface methods for packetQueue to satisfy heap.Interface

func (q *packetQueue) Len() int {
	return len(q.streams)
}

func (q *packetQueue) Less(i, j int) bool {
	return q.streams[i].infos[0].time.Before(q.streams[j].infos[0].time)
}

func (q *packetQueue) Swap(i, j int) {
	q.streams[i], q.streams[j] = q.streams[j], q.streams[i]
}

func (q *packetQueue) Push(x interface{}) {
	stream := x.(pqStream)
	q.streams = append(q.streams, stream)
	q.size += stream.size
}

func (q *packetQueue) Pop() interface{} {
	idx := len(q.streams) - 1
	stream := q.streams[idx]
	q.streams = q.streams[:idx]
	q.size -= stream.size
	return stream
}
