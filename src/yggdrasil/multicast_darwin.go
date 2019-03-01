// +build darwin

package yggdrasil

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Foundation
#import <Foundation/Foundation.h>
void WakeUpAWDL() {
	id delegateObject; // Assume this exists.
	NSNetServiceBrowser *serviceBrowser;

	serviceBrowser = [[NSNetServiceBrowser alloc] init];
	serviceBrowser.includesPeerToPeer = YES;
	[serviceBrowser searchForServicesOfType:@"_yggdrasil._tcp" inDomain:@""];
}
*/
import "C"
import "syscall"
import "golang.org/x/sys/unix"

func (m *multicast) multicastWake() {
	for _, intf := range m.interfaces() {
		if intf.Name == "awdl0" {
			m.core.log.Infoln("Multicast discovery is waking up AWDL")
			C.WakeUpAWDL()
		}
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
