// +build mobile,darwin

package yggdrasil

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
	"encoding/hex"
	"errors"
	"unsafe"

	"github.com/yggdrasil-network/yggdrasil-go/src/crypto"
	"github.com/yggdrasil-network/yggdrasil-go/src/util"
)

type MobileLogger struct {
}

func (nsl MobileLogger) Write(p []byte) (n int, err error) {
	p = append(p, 0)
	cstr := (*C.char)(unsafe.Pointer(&p[0]))
	C.Log(cstr)
	return len(p), nil
}

func (c *Core) AWDLCreateInterface(boxPubKey string, sigPubKey string, name string) error {
	fromAWDL := make(chan []byte, 32)
	toAWDL := make(chan []byte, 32)

	var boxPub crypto.BoxPubKey
	var sigPub crypto.SigPubKey
	boxPubHex, err := hex.DecodeString(boxPubKey)
	if err != nil {
		c.log.Println(err)
		return err
	}
	sigPubHex, err := hex.DecodeString(sigPubKey)
	if err != nil {
		c.log.Println(err)
		return err
	}
	copy(boxPub[:], boxPubHex)
	copy(sigPub[:], sigPubHex)

	if intf, err := c.awdl.create(fromAWDL, toAWDL, &boxPub, &sigPub, name); err == nil {
		if intf != nil {
			c.log.Println(err)
			return err
		} else {
			c.log.Println("c.awdl.create didn't return an interface")
			return errors.New("c.awdl.create didn't return an interface")
		}
	} else {
		c.log.Println(err)
		return err
	}
}

func (c *Core) AWDLCreateInterfaceFromContext(context []byte, name string) error {
	if len(context) < crypto.BoxPubKeyLen+crypto.SigPubKeyLen {
		return errors.New("Not enough bytes in context")
	}
	boxPubKey := hex.EncodeToString(context[:crypto.BoxPubKeyLen])
	sigPubKey := hex.EncodeToString(context[crypto.BoxPubKeyLen:])
	return c.AWDLCreateInterface(boxPubKey, sigPubKey, name)
}

func (c *Core) AWDLShutdownInterface(name string) error {
	return c.awdl.shutdown(name)
}

func (c *Core) AWDLRecvPacket(identity string) ([]byte, error) {
	if intf := c.awdl.getInterface(identity); intf != nil {
		return <-intf.toAWDL, nil
	}
	return nil, errors.New("AWDLRecvPacket identity not known: " + identity)
}

func (c *Core) AWDLSendPacket(identity string, buf []byte) error {
	packet := append(util.GetBytes(), buf[:]...)
	if intf := c.awdl.getInterface(identity); intf != nil {
		intf.fromAWDL <- packet
		return nil
	}
	return errors.New("AWDLSendPacket identity not known: " + identity)
}

func (c *Core) AWDLConnectionContext() []byte {
	var context []byte
	context = append(context, c.boxPub[:]...)
	context = append(context, c.sigPub[:]...)
	return context
}
