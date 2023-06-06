//go:build ios
// +build ios

package mobile

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Foundation
#import <Foundation/Foundation.h>
void Log(const char *text) {
  NSString *nss = [NSString stringWithUTF8String:text];
  NSLog(@"%@", nss);
}
*/
import "C"
import (
	"unsafe"

	"github.com/yggdrasil-network/yggdrasil-go/src/tun"
)

type MobileLogger struct {
}

func (nsl MobileLogger) Write(p []byte) (n int, err error) {
	p = append(p, 0)
	cstr := (*C.char)(unsafe.Pointer(&p[0]))
	C.Log(cstr)
	return len(p), nil
}

func (m *Yggdrasil) TakeOverTUN(fd int32) error {
	options := []tun.SetupOption{
		tun.FileDescriptor(fd),
		tun.InterfaceMTU(m.iprwc.MTU()),
	}
	var err error
	m.tun, err = tun.New(m.iprwc, m.logger, options...)
	return err
}
