package yggdrasil

import (
	"time"
)

// TODO take max size from config
const MAX_PACKET_QUEUE_SIZE = 4 * 1048576 // 4 MB

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

func (q *packetQueue) cleanup() {
	for q.size > MAX_PACKET_QUEUE_SIZE {
		// TODO? drop from a random stream
		//  odds proportional to size? bandwidth?
		//  always using the worst is exploitable -> flood 1 packet per random stream
		// find the stream that's using the most bandwidth
		now := time.Now()
		var worst pqStreamID
		for id := range q.streams {
			worst = id
			break // get a random ID to start
		}
		worstStream := q.streams[worst]
		worstSize := float64(worstStream.size)
		worstAge := now.Sub(worstStream.infos[0].time).Seconds()
		for id, stream := range q.streams {
			thisSize := float64(stream.size)
			thisAge := now.Sub(stream.infos[0].time).Seconds()
			// cross multiply to avoid division by zero issues
			if worstSize*thisAge < thisSize*worstAge {
				// worstSize/worstAge < thisSize/thisAge -> this uses more bandwidth
				worst = id
				worstStream = stream
				worstSize = thisSize
				worstAge = thisAge
			}
		}
		// Drop the oldest packet from the worst stream
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
	}
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
	q.cleanup()
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
