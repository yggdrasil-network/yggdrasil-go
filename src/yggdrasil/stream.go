package yggdrasil

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"

	"github.com/yggdrasil-network/yggdrasil-go/src/util"
)

// Test that this matches the interface we expect
var _ = linkInterfaceMsgIO(&stream{})

type stream struct {
	rwc          io.ReadWriteCloser
	inputBuffer  *bufio.Reader
	outputBuffer net.Buffers
}

func (s *stream) close() error {
	return s.rwc.Close()
}

const streamMsgSize = 2048 + 65535

var streamMsg = [...]byte{0xde, 0xad, 0xb1, 0x75} // "dead bits"

func (s *stream) init(rwc io.ReadWriteCloser) {
	// TODO have this also do the metadata handshake and create the peer struct
	s.rwc = rwc
	// TODO call something to do the metadata exchange
	s.inputBuffer = bufio.NewReaderSize(s.rwc, 2*streamMsgSize)
}

// writeMsg writes a message with stream padding, and is *not* thread safe.
func (s *stream) writeMsg(bs []byte) (int, error) {
	buf := s.outputBuffer[:0]
	buf = append(buf, streamMsg[:])
	l := wire_put_uint64(uint64(len(bs)), util.GetBytes())
	defer util.PutBytes(l)
	buf = append(buf, l)
	padLen := len(buf[0]) + len(buf[1])
	buf = append(buf, bs)
	totalLen := padLen + len(bs)
	var bn int
	for bn < totalLen {
		n, err := buf.WriteTo(s.rwc)
		bn += int(n)
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
		bs, err := s.readMsgFromBuffer()
		if err != nil {
			return nil, fmt.Errorf("message error: %v", err)
		}
		return bs, err
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

// Reads bytes from the underlying rwc and returns 1 full message
func (s *stream) readMsgFromBuffer() ([]byte, error) {
	pad := streamMsg // Copy
	_, err := io.ReadFull(s.inputBuffer, pad[:])
	if err != nil {
		return nil, err
	} else if pad != streamMsg {
		return nil, errors.New("bad message")
	}
	lenSlice := make([]byte, 0, 10)
	// FIXME this nextByte stuff depends on wire.go format, kind of ugly to have it here
	nextByte := byte(0xff)
	for nextByte > 127 {
		nextByte, err = s.inputBuffer.ReadByte()
		if err != nil {
			return nil, err
		}
		lenSlice = append(lenSlice, nextByte)
	}
	msgLen, _ := wire_decode_uint64(lenSlice)
	if msgLen > streamMsgSize {
		return nil, errors.New("oversized message")
	}
	msg := util.ResizeBytes(util.GetBytes(), int(msgLen))
	_, err = io.ReadFull(s.inputBuffer, msg)
	return msg, err
}
