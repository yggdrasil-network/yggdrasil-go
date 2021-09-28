//go:build (darwin && cgo) || (ios && cgo)
// +build darwin,cgo ios,cgo

package multicast

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Foundation
#import <Foundation/Foundation.h>
NSNetServiceBrowser *serviceBrowser;
void StartAWDLBrowsing() {
	if (serviceBrowser == nil) {
		serviceBrowser = [[NSNetServiceBrowser alloc] init];
		serviceBrowser.includesPeerToPeer = YES;
	}
	[serviceBrowser searchForServicesOfType:@"_yggdrasil._tcp" inDomain:@""];
}
void StopAWDLBrowsing() {
	if (serviceBrowser == nil) {
		return;
	}
	[serviceBrowser stop];
}
*/
import "C"
import (
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func (m *Multicast) _multicastStarted() {
	if !m.isOpen {
		return
	}
	C.StopAWDLBrowsing()
	for intf := range m._interfaces {
		if intf == "awdl0" {
			C.StartAWDLBrowsing()
			break
		}
	}
	time.AfterFunc(time.Minute, func() {
		m.Act(nil, m._multicastStarted)
	})
}

func (m *Multicast) multicastReuse(network string, address string, c syscall.RawConn) error {
	var control error
	var reuseport error
	var recvanyif error

	control = c.Control(func(fd uintptr) {
		reuseport = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)

		// sys/socket.h: #define	SO_RECV_ANYIF	0x1104
		recvanyif = unix.SetsockoptInt(int(fd), syscall.SOL_SOCKET, 0x1104, 1)
	})

	switch {
	case reuseport != nil:
		return reuseport
	case recvanyif != nil:
		return recvanyif
	default:
		return control
	}
}
