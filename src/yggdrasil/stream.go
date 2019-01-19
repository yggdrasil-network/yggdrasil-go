package yggdrasil

import (
	"errors"
	"fmt"

	"github.com/yggdrasil-network/yggdrasil-go/src/util"
)

type stream struct {
	buffer []byte
}

const streamMsgSize = 2048 + 65535

var streamMsg = [...]byte{0xde, 0xad, 0xb1, 0x75} // "dead bits"

func (s *stream) init() {
	s.buffer = make([]byte, 2*streamMsgSize)
}

// This reads from the channel into a []byte buffer for incoming messages. It
// copies completed messages out of the cache into a new slice, and passes them
// to the peer struct via the provided `in func([]byte)` argument. Then it
// shifts the incomplete fragments of data forward so future reads won't
// overwrite it.
func (s *stream) write(bs []byte, in func([]byte)) error {
	frag := s.buffer[:0]
	if n := len(bs); n > 0 {
		frag = append(frag, bs[:n]...)
		msg, ok, err2 := stream_chopMsg(&frag)
		if err2 != nil {
			return fmt.Errorf("message error: %v", err2)
		}
		if !ok {
			// We didn't get the whole message yet
			return nil
		}
		newMsg := append(util.GetBytes(), msg...)
		in(newMsg)
		util.Yield()
	}
	return nil
}

// This takes a pointer to a slice as an argument. It checks if there's a
// complete message and, if so, slices out those parts and returns the message,
// true, and nil. If there's no error, but also no complete message, it returns
// nil, false, and nil. If there's an error, it returns nil, false, and the
// error, which the reader then handles (currently, by returning from the
// reader, which causes the connection to close).
func stream_chopMsg(bs *[]byte) ([]byte, bool, error) {
	// Returns msg, ok, err
	if len(*bs) < len(streamMsg) {
		return nil, false, nil
	}
	for idx := range streamMsg {
		if (*bs)[idx] != streamMsg[idx] {
			return nil, false, errors.New("bad message")
		}
	}
	msgLen, msgLenLen := wire_decode_uint64((*bs)[len(streamMsg):])
	if msgLen > streamMsgSize {
		return nil, false, errors.New("oversized message")
	}
	msgBegin := len(streamMsg) + msgLenLen
	msgEnd := msgBegin + int(msgLen)
	if msgLenLen == 0 || len(*bs) < msgEnd {
		// We don't have the full message
		// Need to buffer this and wait for the rest to come in
		return nil, false, nil
	}
	msg := (*bs)[msgBegin:msgEnd]
	(*bs) = (*bs)[msgEnd:]
	return msg, true, nil
}
