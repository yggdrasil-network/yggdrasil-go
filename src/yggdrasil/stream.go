package yggdrasil

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
)

// Test that this matches the interface we expect
var _ = linkMsgIO(&stream{})

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
func (s *stream) writeMsgs(bss [][]byte) (int, error) {
	buf := s.outputBuffer[:0]
	var written int
	for _, bs := range bss {
		buf = append(buf, streamMsg[:])
		buf = append(buf, wire_encode_uint64(uint64(len(bs))))
		buf = append(buf, bs)
		written += len(bs)
	}
	s.outputBuffer = buf[:0] // So we can reuse the same underlying array later
	_, err := buf.WriteTo(s.rwc)
	for _, bs := range bss {
		pool_putBytes(bs)
	}
	// TODO only include number of bytes from bs *successfully* written?
	return written, err
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
	msg := pool_getBytes(int(msgLen))
	_, err = io.ReadFull(s.inputBuffer, msg)
	return msg, err
}
