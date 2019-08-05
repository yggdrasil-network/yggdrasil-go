// +build darwin

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

var awdlGoroutineStarted bool

func (m *Multicast) multicastStarted() {
	if awdlGoroutineStarted {
		return
	}
	awdlGoroutineStarted = true
	for {
		C.StopAWDLBrowsing()
		for intf := range m.Interfaces() {
			if intf == "awdl0" {
				m.log.Infoln("Multicast discovery is using AWDL discovery")
				C.StartAWDLBrowsing()
				break
			}
		}
		time.Sleep(time.Minute)
	}
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
