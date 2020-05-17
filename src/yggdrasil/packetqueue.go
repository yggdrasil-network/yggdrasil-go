package yggdrasil

import (
	"math/rand"
	"time"
)

type pqStreamID string

type pqPacketInfo struct {
	packet []byte
	time   time.Time
}

type pqStream struct {
	infos []pqPacketInfo
	size  uint64
}

// TODO separate queues per e.g. traffic flow
type packetQueue struct {
	streams map[pqStreamID]pqStream
	size    uint64
}

// drop will remove a packet from the queue, returning it to the pool
//  returns true if a packet was removed, false otherwise
func (q *packetQueue) drop() bool {
	if q.size == 0 {
		return false
	}
	// select a random stream, odds based on stream size
	offset := rand.Uint64() % q.size
	var worst pqStreamID
	var size uint64
	for id, stream := range q.streams {
		worst = id
		size += stream.size
		if size >= offset {
			break
		}
	}
	// drop the oldest packet from the stream
	worstStream := q.streams[worst]
	packet := worstStream.infos[0].packet
	worstStream.infos = worstStream.infos[1:]
	worstStream.size -= uint64(len(packet))
	q.size -= uint64(len(packet))
	pool_putBytes(packet)
	// save the modified stream to queues
	if len(worstStream.infos) > 0 {
		q.streams[worst] = worstStream
	} else {
		delete(q.streams, worst)
	}
	return true
}

func (q *packetQueue) push(packet []byte) {
	if q.streams == nil {
		q.streams = make(map[pqStreamID]pqStream)
	}
	// get stream
	id := pqStreamID(peer_getPacketCoords(packet)) // just coords for now
	stream := q.streams[id]
	// update stream
	stream.infos = append(stream.infos, pqPacketInfo{packet, time.Now()})
	stream.size += uint64(len(packet))
	// save update to queues
	q.streams[id] = stream
	q.size += uint64(len(packet))
}

func (q *packetQueue) pop() ([]byte, bool) {
	if len(q.streams) > 0 {
		// get the stream that uses the least bandwidth
		now := time.Now()
		var best pqStreamID
		for id := range q.streams {
			best = id
			break // get a random ID to start
		}
		bestStream := q.streams[best]
		bestSize := float64(bestStream.size)
		bestAge := now.Sub(bestStream.infos[0].time).Seconds()
		for id, stream := range q.streams {
			thisSize := float64(stream.size)
			thisAge := now.Sub(stream.infos[0].time).Seconds()
			// cross multiply to avoid division by zero issues
			if bestSize*thisAge > thisSize*bestAge {
				// bestSize/bestAge > thisSize/thisAge -> this uses less bandwidth
				best = id
				bestStream = stream
				bestSize = thisSize
				bestAge = thisAge
			}
		}
		// get the oldest packet from the best stream
		packet := bestStream.infos[0].packet
		bestStream.infos = bestStream.infos[1:]
		bestStream.size -= uint64(len(packet))
		q.size -= uint64(len(packet))
		// save the modified stream to queues
		if len(bestStream.infos) > 0 {
			q.streams[best] = bestStream
		} else {
			delete(q.streams, best)
		}
		return packet, true
	}
	return nil, false
}
