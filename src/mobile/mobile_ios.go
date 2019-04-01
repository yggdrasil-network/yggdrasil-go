// +build darwin

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
)

type MobileLogger struct {
}

func (nsl MobileLogger) Write(p []byte) (n int, err error) {
	p = append(p, 0)
	cstr := (*C.char)(unsafe.Pointer(&p[0]))
	C.Log(cstr)
	return len(p), nil
}

/*
func (c *Core) AWDLCreateInterface(name, local, remote string, incoming bool) error {
	if intf, err := c.link.awdl.create(name, local, remote, incoming); err != nil || intf == nil {
		c.log.Println("c.link.awdl.create:", err)
		return err
	}
	return nil
}

func (c *Core) AWDLShutdownInterface(name string) error {
	return c.link.awdl.shutdown(name)
}

func (c *Core) AWDLRecvPacket(identity string) ([]byte, error) {
	if intf := c.link.awdl.getInterface(identity); intf != nil {
		read, ok := <-intf.rwc.toAWDL
		if !ok {
			return nil, errors.New("AWDLRecvPacket: channel closed")
		}
		return read, nil
	}
	return nil, errors.New("AWDLRecvPacket identity not known: " + identity)
}

func (c *Core) AWDLSendPacket(identity string, buf []byte) error {
	packet := append(util.GetBytes(), buf[:]...)
	if intf := c.link.awdl.getInterface(identity); intf != nil {
		intf.rwc.fromAWDL <- packet
		return nil
	}
	return errors.New("AWDLSendPacket identity not known: " + identity)
}
*/
