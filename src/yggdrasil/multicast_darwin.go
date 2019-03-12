// +build darwin

package yggdrasil

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

var awdlGoroutineStarted bool

func (m *multicast) multicastStarted() {
	if awdlGoroutineStarted {
		return
	}
	m.core.log.Infoln("Multicast discovery will wake up AWDL if required")
	awdlGoroutineStarted = true
	for {
		C.StopAWDLBrowsing()
		for _, intf := range m.interfaces() {
			if intf.Name == "awdl0" {
				C.StartAWDLBrowsing()
				break
			}
		}
		time.Sleep(time.Minute)
	}
}

func (m *multicast) multicastReuse(network string, address string, c syscall.RawConn) error {
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
