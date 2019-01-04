package yggdrasil

import (
  "sync"
  "github.com/yggdrasil-network/yggdrasil-go/src/crypto"
)

type awdl struct {
	core *Core
  mutex sync.RWMutex // protects interfaces below
  interfaces map[string]*awdlInterface
}

type awdlInterface struct {
  awdl *awdl
  recv <-chan []byte // traffic received from the network
  send chan<- []byte // traffic to send to the network
  shutdown chan bool
  peer *peer
}

func (l *awdl) init(c *Core) error {
  l.core = c
  l.mutex.Lock()
  l.interfaces = make(map[string]*awdlInterface)
  l.mutex.Unlock()

  return nil
}

func (l *awdl) create(boxPubKey *crypto.BoxPubKey, sigPubKey *crypto.SigPubKey, name string) *awdlInterface {
  shared := crypto.GetSharedKey(&l.core.boxPriv, boxPubKey)
  intf := awdlInterface{
    recv: make(<-chan []byte),
    send: make(chan<- []byte),
    shutdown: make(chan bool),
    peer: l.core.peers.newPeer(boxPubKey, sigPubKey, shared, name),
  }
  if intf.peer != nil {
    l.mutex.Lock()
    l.interfaces[name] = &intf
    l.mutex.Unlock()
    intf.peer.linkOut = make(chan []byte, 1) // protocol traffic
    intf.peer.out = func(msg []byte) {
  		defer func() { recover() }()
  		intf.send <- msg
  	}
    go intf.handler()
    l.core.switchTable.idleIn <- intf.peer.port
    return &intf
  }
  return nil
}

func (l *awdl) getInterface(identity string) *awdlInterface {
  l.mutex.RLock()
  defer l.mutex.RUnlock()
  if intf, ok := l.interfaces[identity]; ok {
    return intf
  }
  return nil
}

func (l *awdl) shutdown(identity string) {
  if intf, ok := l.interfaces[identity]; ok {
    intf.shutdown <- true
    l.core.peers.removePeer(intf.peer.port)
    l.mutex.Lock()
    delete(l.interfaces, identity)
    l.mutex.Unlock()
  }
}

func (ai *awdlInterface) handler() {
  for {
    select {
    case p := <-ai.peer.linkOut:
      ai.send <- p
    case r := <-ai.recv: // traffic received from AWDL
      ai.peer.handlePacket(r)
    case <-ai.shutdown:
      return
    default:
    }
  }
}
