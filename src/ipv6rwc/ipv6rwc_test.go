package ipv6rwc

import (
	"testing"
)

// writePC indexed bs[0] before checking len(bs), so an empty buffer
// reaching it (via the mobile Send/SendBuffer API or a zero-byte TUN
// read) panicked with index out of range. Reject empty input cleanly.
func TestWritePCRejectsEmptyPacket(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("writePC must not panic on empty input, got: %v", r)
		}
	}()
	var k keyStore
	if _, err := k.writePC(nil); err == nil {
		t.Fatalf("expected an error for nil input, got nil")
	}
	if _, err := k.writePC([]byte{}); err == nil {
		t.Fatalf("expected an error for zero-length input, got nil")
	}
}

// Buffers shorter than the minimum IPv6 header (40 bytes) but non-empty
// must still be rejected without panicking.
func TestWritePCRejectsTruncatedPacket(t *testing.T) {
	var k keyStore
	for _, tc := range []struct {
		name string
		buf  []byte
	}{
		{name: "single byte ipv6 marker", buf: []byte{0x60}},
		{name: "ten bytes ipv6 marker", buf: append([]byte{0x60}, make([]byte, 9)...)},
		{name: "thirty-nine bytes ipv6 marker", buf: append([]byte{0x60}, make([]byte, 38)...)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("writePC must not panic on truncated input, got: %v", r)
				}
			}()
			if _, err := k.writePC(tc.buf); err == nil {
				t.Fatalf("expected an error for truncated input, got nil")
			}
		})
	}
}
