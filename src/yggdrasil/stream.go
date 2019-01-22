package yggdrasil

import (
	"errors"
	"fmt"
	"io"

	"github.com/yggdrasil-network/yggdrasil-go/src/util"
)

// Test that this matches the interface we expect
var _ = linkInterfaceMsgIO(&stream{})

type stream struct {
	rwc          io.ReadWriteCloser
	inputBuffer  []byte // Incoming packet stream
	didFirstSend bool   // Used for metadata exchange
	didFirstRecv bool   // Used for metadata exchange
	// TODO remove the rest, it shouldn't matter in the long run
	handlePacket func([]byte)
}

func (s *stream) close() error {
	return s.rwc.Close()
}

const streamMsgSize = 2048 + 65535

var streamMsg = [...]byte{0xde, 0xad, 0xb1, 0x75} // "dead bits"

func (s *stream) init(rwc io.ReadWriteCloser, in func([]byte)) {
	// TODO have this also do the metadata handshake and create the peer struct
	s.rwc = rwc
	s.handlePacket = in

	// TODO call something to do the metadata exchange
}

// writeMsg writes a message with stream padding, and is *not* thread safe.
func (s *stream) writeMsg(bs []byte) (int, error) {
	buf := util.GetBytes()
	defer util.PutBytes(buf)
	buf = append(buf, streamMsg[:]...)
	buf = append(buf, wire_encode_uint64(uint64(len(bs)))...)
	padLen := len(buf)
	buf = append(buf, bs...)
	var bn int
	for bn < len(buf) {
		n, err := s.rwc.Write(buf[bn:])
		bn += n
		if err != nil {
			l := bn - padLen
			if l < 0 {
				l = 0
			}
			return l, err
		}
	}
	return len(bs), nil
}

// readMsg reads a message from the stream, accounting for stream padding, and is *not* thread safe.
func (s *stream) readMsg() ([]byte, error) {
	for {
		buf := s.inputBuffer
		msg, ok, err := stream_chopMsg(&buf)
		switch {
		case err != nil:
			// Something in the stream format is corrupt
			return nil, fmt.Errorf("message error: %v", err)
		case ok:
			// Copy the packet into bs, shift the buffer, and return
			msg = append(util.GetBytes(), msg...)
			s.inputBuffer = append(s.inputBuffer[:0], buf...)
			return msg, nil
		default:
			// Wait for the underlying reader to return enough info for us to proceed
			frag := make([]byte, 2*streamMsgSize)
			n, err := s.rwc.Read(frag)
			if n > 0 {
				s.inputBuffer = append(s.inputBuffer, frag[:n]...)
			} else if err != nil {
				return nil, err
			}
		}
	}
}

// Writes metadata bytes without stream padding, meant to be temporary
func (s *stream) _sendMetaBytes(metaBytes []byte) error {
	var written int
	for written < len(metaBytes) {
		n, err := s.rwc.Write(metaBytes)
		written += n
		if err != nil {
			return err
		}
	}
	return nil
}

// Reads metadata bytes without stream padding, meant to be temporary
func (s *stream) _recvMetaBytes() ([]byte, error) {
	var meta version_metadata
	frag := meta.encode()
	metaBytes := make([]byte, 0, len(frag))
	for len(metaBytes) < len(frag) {
		n, err := s.rwc.Read(frag)
		if err != nil {
			return nil, err
		}
		metaBytes = append(metaBytes, frag[:n]...)
	}
	return metaBytes, nil
}

// This reads from the channel into a []byte buffer for incoming messages. It
// copies completed messages out of the cache into a new slice, and passes them
// to the peer struct via the provided `in func([]byte)` argument. Then it
// shifts the incomplete fragments of data forward so future reads won't
// overwrite it.
func (s *stream) handleInput(bs []byte) error {
	if len(bs) > 0 {
		s.inputBuffer = append(s.inputBuffer, bs...)
		buf := s.inputBuffer
		msg, ok, err2 := stream_chopMsg(&buf)
		if err2 != nil {
			return fmt.Errorf("message error: %v", err2)
		}
		if !ok {
			// We didn't get the whole message yet
			return nil
		}
		newMsg := append(util.GetBytes(), msg...)
		s.inputBuffer = append(s.inputBuffer[:0], buf...)
		s.handlePacket(newMsg)
		util.Yield() // Make sure we give up control to the scheduler
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
